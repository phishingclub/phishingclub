package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// keptLocalEdit marks a skipped entry for a resource that kept its local edit.
// Each entry reads "<type> <name> kept (local edit...)". The Bait Box page
// matches this text, so keep the two in step.
const keptLocalEdit = "kept (local edit"

// keptCurrent starts the skipped entry for a resource the operator kept as it is
// although it has no local edit: an API sender keeping its OAuth provider or its
// request, or an authorized OAuth provider keeping its URLs. The Bait Box page
// matches it too.
const keptCurrent = "kept (current"

const (
	keptCurrentOAuth   = keptCurrent + " OAuth provider)"
	keptCurrentURLs    = keptCurrent + " OAuth provider URLs)"
	keptCurrentRequest = keptCurrent + " request)"
)

// unresolvedConflicts returns the conflicts the caller has not chosen a
// resolution for, so an update that would touch an unresolved conflict is
// stopped before any write.
func unresolvedConflicts(conflicts []UpdateConflict, resolutions map[string]string) []UpdateConflict {
	var out []UpdateConflict
	for _, c := range conflicts {
		if _, ok := resolutions[c.Key()]; !ok {
			out = append(out, c)
		}
	}
	return out
}

// updateUndo collects compensating actions for an in progress update so a write
// failure reverses everything applied so far. The entity services each own their
// own database handle and take no transaction, so a single transaction cannot
// span them; this mirrors the manual rollback Install uses for the same reason.
// Actions run newest first. failures counts actions that reported an error, so
// the caller can tell a complete rollback from a partial one.
type updateUndo struct {
	fns      []func()
	failures int
}

// pushErr pushes an action whose error counts as a rollback failure.
func (u *updateUndo) pushErr(f func() error) {
	u.fns = append(u.fns, func() {
		if err := f(); err != nil {
			u.failures++
		}
	})
}

func (u *updateUndo) run() {
	for i := len(u.fns) - 1; i >= 0; i-- {
		u.fns[i]()
	}
	u.fns = nil
}

// declaredNames maps a resource type to the names the new package version
// declares for it, so the plan phase can accept a reference to an entity the
// update itself creates.
type declaredNames map[string]map[string]bool

func (d declaredNames) add(resourceType, name string) {
	if d[resourceType] == nil {
		d[resourceType] = map[string]bool{}
	}
	d[resourceType][declaredKey(resourceType, name)] = true
}

func (d declaredNames) has(resourceType, name string) bool {
	return d[resourceType][declaredKey(resourceType, name)]
}

// declaredKey is the name a declaration is looked up by; domain names compare
// in lower case, the form the installer stores them in.
func declaredKey(resourceType, name string) string {
	if resourceType == "domain" {
		return installDomainName(name)
	}
	return strings.TrimSpace(name)
}

// reconcileItem is one resource the new package version declares, for a type the
// update reconciles generically. match is the provenance source name used to find
// the installed entity (surviving a local rename).
type reconcileItem struct {
	match      string
	hasContent bool   // false when the declaration's file is missing: skip, never delete
	upstreamFP string // fingerprint the new version would install
	// create installs the entity fresh and returns its id and stored fingerprint.
	create func() (*uuid.UUID, string, error)
	// update applies the new content onto the existing entity in place and returns
	// the new stored fingerprint.
	update func(id *uuid.UUID) (string, error)
	// preflight checks, before any write, that create or update can succeed: the
	// declared model is valid and every entity it names exists or is declared by
	// the new version. nil when there is nothing to check.
	preflight func(declared declaredNames) error
	// rebindReason is set when updating the entity in place would point it at a
	// different referenced entity (an API sender at another OAuth provider) or move
	// where an OAuth token or the secrets of an authorized OAuth provider are sent.
	// Such a change always waits for the operator's choice, like a local edit does.
	rebindReason string
	// keptNote is the skipped text when the operator keeps an item that waited
	// only for its rebindReason.
	keptNote string
	// keepOnly is set when keep is the only resolution that can apply.
	keepOnly bool
	// replaced returns, after create ran, the content create overwrote in an
	// existing entity instead of creating one (a global report template), or nil.
	// nil func when create always makes a new entity.
	replaced func() *string
}

// checkBuild returns a preflight that reports err, the result of building the
// declared model, naming the resource it belongs to. nil when err is nil.
func checkBuild(kind, name string, err error) func(declaredNames) error {
	if err == nil {
		return nil
	}
	return func(declaredNames) error {
		return errs.NewValidationError(fmt.Errorf("%s %q in the new version is invalid: %w", kind, name, err))
	}
}

// reconcileCurrent is one resource this install currently owns of a type.
type reconcileCurrent struct {
	res         *database.InstalledResource
	currentFP   string
	currentName string
	// detach renames the local copy to a free name so the fresh version can take
	// the original, for the "both" resolution, and pushes its own undo. nil when the
	// type has no name to rename (the local copy is just unlinked).
	detach func() error
	// deleteEntity removes the entity, for the remove path.
	deleteEntity func() error
	// restore writes the fields the entity had when it was loaded back onto it, so
	// a failed update can reverse an in place update.
	restore func() error
	// stale is set when the entity was deleted out of band. loadErr is set when it
	// could not be read. takeStale removes both kinds before the update plans.
	stale   bool
	loadErr error
}

// loadedEntity is what a per type loader reads for one installed resource.
type loadedEntity struct {
	fp           string
	name         string
	detach       func() error
	deleteEntity func() error
	restore      func() error
}

// simpleItem wraps per type create and update closures into a reconcileItem,
// routing a create collision through the friendly error and reading the stored
// fingerprint back after each write so the recorded baseline matches a later
// drift read.
func simpleItem(resourceType, match string, hasContent bool, upstreamFP string,
	create func() (*uuid.UUID, error),
	update func(id *uuid.UUID) error,
	storedHash func(id *uuid.UUID) string,
) reconcileItem {
	return reconcileItem{
		match:      match,
		hasContent: hasContent,
		upstreamFP: upstreamFP,
		create: func() (*uuid.UUID, string, error) {
			id, err := create()
			if err != nil {
				return nil, "", collisionErr(resourceType, match, err)
			}
			return id, storedHash(id), nil
		},
		update: func(id *uuid.UUID) (string, error) {
			if err := update(id); err != nil {
				return "", errs.Wrap(err)
			}
			return storedHash(id), nil
		},
	}
}

// collectCurrents indexes the install's resources of one type by match key. A
// row whose entity was deleted out of band (the loader returns
// gorm.ErrRecordNotFound) is marked stale and one that could not be read carries
// the error; both sit under a key no declaration matches, and takeStale pulls
// them out so the update fails on a read error and drops a stale row only in
// the write phase. driftless forces the drift fingerprint to the recorded
// baseline, for a type that is reconciled as pure wiring with no local edit
// conflict (campaign templates).
func (s *ContentRegistry) collectCurrents(
	ctx context.Context,
	resources []*database.InstalledResource,
	resourceType string,
	driftless bool,
	load func(r *database.InstalledResource) (*loadedEntity, error),
) map[string]reconcileCurrent {
	out := map[string]reconcileCurrent{}
	for _, r := range resources {
		if r.ResourceType != resourceType {
			continue
		}
		le, err := load(r)
		if err != nil || le == nil {
			cur := reconcileCurrent{res: r, stale: true}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				cur = reconcileCurrent{res: r, loadErr: err}
			}
			out[staleKey(r)] = cur
			continue
		}
		fp := le.fp
		if driftless {
			fp = r.InstalledContentHash
		}
		key := strings.TrimSpace(r.SourceName)
		if key == "" {
			key = le.name
		}
		out[key] = reconcileCurrent{
			res: r, currentFP: fp, currentName: le.name,
			detach: le.detach, deleteEntity: le.deleteEntity, restore: le.restore,
		}
	}
	return out
}

// staleKey is the currents key of a row whose entity is gone or unreadable. It
// starts with a byte no declared name holds, so no declaration matches it.
func staleKey(r *database.InstalledResource) string {
	return "\x00" + r.ID.String()
}

// takeStale removes the stale and unreadable rows from currents. It returns the
// first read error, which fails the update before any write, and appends the
// stale rows to stale for the write phase to drop.
func takeStale(currents map[string]reconcileCurrent, stale *[]*database.InstalledResource) error {
	for k, cur := range currents {
		if cur.loadErr != nil {
			return errs.Wrap(fmt.Errorf(
				"read installed %s %q: %w", cur.res.ResourceType, cur.res.SourceName, cur.loadErr,
			))
		}
		if cur.stale {
			*stale = append(*stale, cur.res)
			delete(currents, k)
		}
	}
	return nil
}

// reconcileConflicts returns the conflicts for one resource type: a match whose
// upstream content changed AND whose local entity drifted from the installed
// baseline or whose change has a rebindReason. A local edit is not flagged on an
// update that did not touch it, and an untouched resource is updated silently.
func reconcileConflicts(
	resourceType string, items []reconcileItem, currents map[string]reconcileCurrent,
) []UpdateConflict {
	var out []UpdateConflict
	for _, it := range items {
		if !it.hasContent {
			continue
		}
		cur, ok := currents[it.match]
		if !ok {
			continue
		}
		upstreamChanged := it.upstreamFP != cur.res.InstalledContentHash
		if upstreamChanged && needsChoice(it, cur) {
			out = append(out, UpdateConflict{
				ResourceType: resourceType, SourceName: it.match, CurrentName: cur.currentName,
				Reason: it.rebindReason, KeepOnly: it.keepOnly,
			})
		}
	}
	return out
}

// willWrite reports whether reconcileAddUpdate will create or update the entity
// for an item, following the same three way rule and resolution, so the plan
// phase checks exactly the writes the update is about to make.
func willWrite(
	resourceType string, it reconcileItem, currents map[string]reconcileCurrent, resolutions map[string]string,
) bool {
	if !it.hasContent {
		return false
	}
	cur, ok := currents[it.match]
	if !ok {
		return true
	}
	upstreamChanged := it.upstreamFP != cur.res.InstalledContentHash
	if upstreamChanged && needsChoice(it, cur) {
		return resolutionFor(resolutions, resourceType, it.match) != resolveKeep
	}
	return upstreamChanged
}

// needsChoice reports whether a changed item waits for the operator's resolution:
// the local entity drifted from its installed baseline, or the update would
// rebind what it references.
func needsChoice(it reconcileItem, cur reconcileCurrent) bool {
	return cur.currentFP != cur.res.InstalledContentHash || it.rebindReason != ""
}

// noBothTypes are the resource types the "both" resolution cannot apply to: a
// domain is not renamed by its update, an oauth provider never returns its
// client secret, so neither can be set aside as a renamed local copy, and a
// report template has one global slot per kind, so a second copy cannot exist.
var noBothTypes = map[string]string{
	"domain":         "domain",
	"oauthprovider":  "OAuth provider",
	"reporttemplate": "report template",
}

// validateResolutions rejects a "both" resolution for a type that cannot keep a
// renamed local copy, so the caller chooses keep or overwrite instead. A dropped
// referenced package takes only keep or remove.
func validateResolutions(resolutions map[string]string) error {
	for k, v := range resolutions {
		if pkgID, ok := strings.CutPrefix(k, memberConflictType+":"); ok {
			if v != resolveKeep && v != resolveRemove {
				return errs.NewValidationError(
					fmt.Errorf("resolution for dropped package %q must be keep or remove", pkgID),
				)
			}
			continue
		}
		if v != resolveBoth {
			continue
		}
		resourceType, name, _ := strings.Cut(k, ":")
		if label, ok := noBothTypes[resourceType]; ok {
			return errs.NewValidationError(fmt.Errorf(
				"keep both is not available for %s %q because it cannot be renamed to a local copy; "+
					"choose keep or overwrite",
				label, name,
			))
		}
	}
	return nil
}

// reconcileAddUpdate adds resources new in the new version and updates ones that
// changed, honoring the three way rule and the keep/overwrite/both resolution. It
// deletes every handled key from currents so the caller's remove pass sees only
// the resources the new version dropped. Every entity and provenance write pushes
// its reverse onto undo: a created entity is deleted, an in place update has the
// loaded fields written back, and provenance rows are restored.
func (s *ContentRegistry) reconcileAddUpdate(
	ctx context.Context, session *model.Session, ipID *uuid.UUID,
	resourceType string, items []reconcileItem, currents map[string]reconcileCurrent,
	resolutions map[string]string, boundVars string, result *InstallResult, report *[]string, undo *updateUndo,
) error {
	for _, it := range items {
		cur, matched := currents[it.match]
		if !it.hasContent {
			if matched {
				result.Skipped = append(result.Skipped,
					resourceType+" "+it.match+" skipped (file missing in new version)")
				delete(currents, it.match)
			}
			continue
		}
		if !matched {
			id, fp, cerr := it.create()
			if cerr != nil {
				return cerr
			}
			rid := id
			var replaced *string
			if it.replaced != nil {
				replaced = it.replaced()
			}
			undo.pushErr(func() error { return s.releaseResourceEntity(ctx, session, resourceType, rid, replaced) })
			rerr := s.recordResourceTracked(ctx, ipID, resourceType, it.match, id, fp, boundVars, undo, replaced)
			if rerr != nil {
				return errs.Wrap(rerr)
			}
			*report = append(*report, it.match)
			continue
		}
		delete(currents, it.match)
		upstreamChanged := it.upstreamFP != cur.res.InstalledContentHash
		if upstreamChanged && needsChoice(it, cur) {
			switch resolutionFor(resolutions, resourceType, it.match) {
			case resolveKeep:
				kept := keptLocalEdit + ")"
				if it.keptNote != "" && cur.currentFP == cur.res.InstalledContentHash {
					kept = it.keptNote
				}
				result.Skipped = append(result.Skipped, resourceType+" "+it.match+" "+kept)
				continue
			case resolveBoth:
				if _, blocked := noBothTypes[resourceType]; blocked {
					return validateResolutions(map[string]string{resourceType + ":" + it.match: resolveBoth})
				}
				if cur.detach != nil {
					if derr := cur.detach(); derr != nil {
						return errs.Wrap(derr)
					}
				}
				// the local copy is now unmanaged, so drop its provenance row.
				if derr := s.deleteResourceTracked(ctx, cur.res, undo); derr != nil {
					return errs.Wrap(derr)
				}
				id, fp, cerr := it.create()
				if cerr != nil {
					return cerr
				}
				rid := id
				undo.pushErr(func() error { return s.deleteResourceEntity(ctx, session, resourceType, rid) })
				rerr := s.recordResourceTracked(ctx, ipID, resourceType, it.match, id, fp, boundVars, undo, nil)
				if rerr != nil {
					return errs.Wrap(rerr)
				}
				*report = append(*report, it.match)
				continue
			}
			// resolveOverwrite falls through to the in place update below.
		} else if !upstreamChanged {
			// the new version did not change this resource, so keep the local copy
			// (edited or not) exactly as it is.
			continue
		}
		// apply upstream in place: silent when only the upstream changed, or the
		// overwrite resolution. Matching by id keeps campaigns that reference it working.
		// the restore is pushed first so a partly applied update is reversed as well.
		if cur.restore != nil {
			undo.pushErr(cur.restore)
		}
		newFP, uerr := it.update(cur.res.ResourceID)
		if uerr != nil {
			return uerr
		}
		if rerr := s.rebaselineResourceTracked(ctx, cur.res, newFP, boundVars, undo); rerr != nil {
			return errs.Wrap(rerr)
		}
		*report = append(*report, it.match)
	}
	return nil
}

// reconcileRemove deletes the resources the new version dropped. A resource the
// user locally edited is kept and stays tracked. A resource still used outside
// this install (see outsideUsers) is kept too, and its provenance row is dropped
// so it becomes the user's own and a later uninstall leaves it alone. currents
// holds only the unmatched resources after reconcileAddUpdate ran. It runs after
// every other write succeeded and never fails the update: a resource that cannot
// be checked or deleted keeps its provenance row, is reported, and the next
// update tries again.
func (s *ContentRegistry) reconcileRemove(
	ctx context.Context, ipID *uuid.UUID, resourceType string, currents map[string]reconcileCurrent,
	result *InstallResult,
) {
	if len(currents) == 0 {
		return
	}
	// read the rows now, so ones dropped by an earlier type are not counted as
	// this install's own.
	own, oerr := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, ipID)
	for name, cur := range currents {
		if cur.currentFP != cur.res.InstalledContentHash {
			result.Skipped = append(result.Skipped, resourceType+" "+name+" "+keptLocalEdit+"; dropped upstream)")
			continue
		}
		if cur.deleteEntity == nil {
			continue
		}
		var users []string
		uerr := oerr
		if uerr == nil {
			users, uerr = s.outsideUsers(ctx, ipID, own, cur.res)
		}
		if uerr != nil {
			s.Logger.Warnw("failed to check whether a dropped resource is still used",
				"type", resourceType, "name", name, "error", uerr)
			result.Skipped = append(result.Skipped,
				resourceType+" "+name+" could not be checked for use and stays tracked")
			continue
		}
		if len(users) > 0 {
			kept := resourceType + " " + name + " kept (no longer in package, still used by " +
				strings.Join(users, ", ") + ")"
			if rerr := s.RegistryRepository.DeleteInstalledResource(ctx, cur.res.ID); rerr != nil {
				s.Logger.Warnw("failed to untrack a dropped resource that is still used",
					"type", resourceType, "name", name, "error", rerr)
				result.Skipped = append(result.Skipped, kept+" and stays tracked")
				continue
			}
			result.Skipped = append(result.Skipped, kept)
			continue
		}
		if derr := cur.deleteEntity(); derr != nil {
			s.Logger.Warnw("failed to remove a resource dropped by the new package version",
				"type", resourceType, "name", name, "error", derr)
			result.Skipped = append(result.Skipped,
				resourceType+" "+name+" could not be removed and stays tracked")
			continue
		}
		if rerr := s.RegistryRepository.DeleteInstalledResource(ctx, cur.res.ID); rerr != nil {
			s.Logger.Warnw("failed to delete provenance of a removed resource",
				"type", resourceType, "name", name, "error", rerr)
		}
		result.Skipped = append(result.Skipped, resourceType+" "+name+" removed (no longer in package)")
	}
}

// outsideUsersCap caps how many names one group of users lists.
const outsideUsersCap = 3

// outsideUsers describes what outside the install ipID still uses the entity of
// r, using the same checks the uninstall preview does: another installed
// package tracking it, campaign templates whose content the delete strips,
// campaigns that reference it, api senders on an oauth provider, proxy domains a
// proxy delete takes along, and assets on a domain the install did not add. own
// is the install's current provenance; templates, api senders and assets it
// tracks are not outside use. It returns nil when nothing outside uses it.
func (s *ContentRegistry) outsideUsers(
	ctx context.Context, ipID *uuid.UUID, own []*database.InstalledResource, r *database.InstalledResource,
) ([]string, error) {
	if r.ResourceID == nil {
		return nil, nil
	}
	ownIDs := map[string]map[uuid.UUID]bool{}
	for _, o := range own {
		if o.ResourceID == nil {
			continue
		}
		if ownIDs[o.ResourceType] == nil {
			ownIDs[o.ResourceType] = map[uuid.UUID]bool{}
		}
		ownIDs[o.ResourceType][*o.ResourceID] = true
	}
	var users []string

	n, err := s.RegistryRepository.CountInstalledResourcesByResource(ctx, r.ResourceID, ipID)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		users = append(users, "another installed package")
	}

	// campaign templates whose content the delete strips, and the domains a proxy
	// delete takes along with it.
	var templateIDs []uuid.UUID
	domainIDs := []uuid.UUID{}
	if r.ResourceType != "campaigntemplate" {
		ids, terr := s.templatesClosedByDelete(ctx, r.ResourceType, r.ResourceID)
		if terr != nil {
			return nil, terr
		}
		templateIDs = append(templateIDs, ids...)
	}
	switch r.ResourceType {
	case "domain":
		domainIDs = append(domainIDs, *r.ResourceID)
	case "proxy":
		pds, perr := s.proxyDomainsOf(ctx, r.ResourceID)
		if perr != nil {
			return nil, perr
		}
		if len(pds) > 0 {
			users = append(users, countLabel(len(pds), "proxy domain", "proxy domains"))
		}
		for _, pd := range pds {
			id := pd.ID
			ids, terr := s.templatesClosedByDelete(ctx, "domain", &id)
			if terr != nil {
				return nil, terr
			}
			templateIDs = append(templateIDs, ids...)
			domainIDs = append(domainIDs, id)
		}
	}
	var templateNames []string
	seen := map[uuid.UUID]bool{}
	for _, id := range templateIDs {
		if seen[id] || ownIDs["campaigntemplate"][id] {
			continue
		}
		seen[id] = true
		templateNames = append(templateNames, s.campaignTemplateName(ctx, id))
	}
	if len(templateNames) > 0 {
		users = append(users, namedLabel("campaign template", "campaign templates", templateNames))
	}

	// campaigns that reference the entity directly. Finished campaigns count too,
	// since the delete also clears the reference from their history.
	if repo := s.campaignRepository(); repo != nil {
		var campaigns []*model.Campaign
		var cerr error
		switch r.ResourceType {
		case "campaigntemplate":
			campaigns, cerr = repo.GetByTemplateIDs(ctx, []*uuid.UUID{r.ResourceID})
		case "allowdeny":
			campaigns, cerr = repo.GetByAllowDenyID(ctx, r.ResourceID)
		case "page":
			campaigns, cerr = repo.GetByDenyPageID(ctx, r.ResourceID)
		case "webhook":
			campaigns, cerr = repo.GetByWebhookID(ctx, r.ResourceID)
		case "script":
			campaigns, cerr = repo.GetByScriptID(ctx, r.ResourceID)
		}
		if cerr != nil {
			return nil, cerr
		}
		names := make([]string, 0, len(campaigns))
		for _, c := range campaigns {
			names = append(names, previewCampaign(c).Name)
		}
		if len(names) > 0 {
			users = append(users, namedLabel("campaign", "campaigns", names))
		}
	}

	if r.ResourceType == "oauthprovider" {
		exclude := make([]uuid.UUID, 0, len(ownIDs["apisender"]))
		for id := range ownIDs["apisender"] {
			exclude = append(exclude, id)
		}
		names, aerr := s.APISenderRepository.NamesByOAuthProviderIDExcluding(ctx, *r.ResourceID, exclude)
		if aerr != nil {
			return nil, aerr
		}
		if len(names) > 0 {
			users = append(users, namedLabel("API sender", "API senders", names))
		}
	}

	// deleting a domain deletes every asset on it.
	if len(domainIDs) > 0 && s.Asset != nil && s.Asset.AssetRepository != nil {
		count := 0
		for _, did := range domainIDs {
			id := did
			assets, aerr := s.Asset.AssetRepository.GetAllByDomainID(ctx, &id)
			if aerr != nil {
				return nil, aerr
			}
			for _, a := range assets {
				if aid, ierr := a.ID.Get(); ierr == nil && !ownIDs["asset"][aid] {
					count++
				}
			}
		}
		if count > 0 {
			users = append(users, countLabel(count, "asset", "assets"))
		}
	}
	return users, nil
}

// countLabel is a count with the singular or plural noun.
func countLabel(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// namedLabel lists names after the singular or plural noun, at most
// outsideUsersCap of them, with the rest counted.
func namedLabel(one, many string, names []string) string {
	label := one
	if len(names) > 1 {
		label = many
	}
	shown := names
	if len(shown) > outsideUsersCap {
		shown = shown[:outsideUsersCap]
	}
	quoted := make([]string, len(shown))
	for i, n := range shown {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	out := label + " " + strings.Join(quoted, ", ")
	if rest := len(names) - len(shown); rest > 0 {
		out += fmt.Sprintf(" and %d more", rest)
	}
	return out
}

// recordResourceTracked inserts a provenance row and pushes its delete onto undo.
// replaced is the content the write overwrote, nil when it created the entity.
func (s *ContentRegistry) recordResourceTracked(
	ctx context.Context,
	ipID *uuid.UUID,
	resourceType, sourceName string,
	resourceID *uuid.UUID,
	hash, boundVars string,
	undo *updateUndo,
	replaced *string,
) error {
	rowID, err := s.insertResourceRow(ctx, ipID, resourceType, sourceName, resourceID, hash, boundVars, replaced)
	if err != nil {
		return err
	}
	undo.pushErr(func() error { return s.RegistryRepository.DeleteInstalledResource(ctx, rowID) })
	return nil
}

// rebaselineResourceTracked records a new content hash for a resource and pushes
// the restore of the old hash onto undo.
func (s *ContentRegistry) rebaselineResourceTracked(
	ctx context.Context, r *database.InstalledResource, hash, boundVars string, undo *updateUndo,
) error {
	oldHash, oldBound := r.InstalledContentHash, r.BoundVariables
	rowID := r.ID
	if err := s.RegistryRepository.UpdateInstalledResource(ctx, rowID, hash, boundVars); err != nil {
		return err
	}
	undo.pushErr(func() error { return s.RegistryRepository.UpdateInstalledResource(ctx, rowID, oldHash, oldBound) })
	return nil
}

// deleteResourceTracked removes a provenance row and pushes its reinsert onto undo.
func (s *ContentRegistry) deleteResourceTracked(
	ctx context.Context, r *database.InstalledResource, undo *updateUndo,
) error {
	row := *r
	if err := s.RegistryRepository.DeleteInstalledResource(ctx, r.ID); err != nil {
		return err
	}
	undo.pushErr(func() error {
		_, err := s.insertResourceRow(
			ctx, row.InstalledPackageID, row.ResourceType, row.SourceName, row.ResourceID,
			row.InstalledContentHash, row.BoundVariables, row.ReplacedContent,
		)
		return err
	})
	return nil
}

// ErrUninstalledDuringUpdate is returned when the package was uninstalled while
// its update was downloaded and checked.
var ErrUninstalledDuringUpdate = errors.New("this package was uninstalled while the update was prepared")

// Update resolves a package at current again and reconciles every content bearing
// resource type the install created: it adds resources new in the version,
// updates ones that changed in place (matched by provenance source name so a
// locally renamed entity is still recognized and campaigns keep working), and
// removes ones the version dropped. A locally edited resource raises a conflict
// only when the new version also changed it, resolved per resource (keep,
// overwrite or both). See applyUpdate for how a failure is contained.
//
// A report template the version adds, which the install does not track yet,
// overwrites the global report template of its kind and needs that kind in
// confirmReportTemplates, as for Install.
func (s *ContentRegistry) Update(
	ctx context.Context,
	session *model.Session,
	installedPackageID *uuid.UUID,
	vars map[string]string,
	resolutions map[string]string,
	confirmReportTemplates []string,
) (_ *InstallResult, err error) {
	ae, err := s.authorize(session, "Registry.PackageUpdate")
	if err != nil {
		return nil, err
	}
	defer s.auditFailure(ae, &err)
	ae.Details["installedPackageId"] = installedPackageID.String()
	ip, err := s.RegistryRepository.GetInstalledPackageByID(ctx, installedPackageID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	ae.Details["registryId"] = ip.RegistryID.String()
	ae.Details["packageId"] = ip.PackageID
	reg, err := s.RegistryRepository.GetRegistryByID(ctx, ip.RegistryID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	client := s.clientFor(reg)
	defer client.CloseIdle()
	ns, nm := splitPackageID(ip.PackageID)

	// an id of the form "ns/name@version" fetches that exact version, used to read
	// what the installed version declared.
	cache := newPackageCache(func(id string) (*registry.VerifiedPackage, error) {
		if base, version, ok := strings.Cut(id, "@"); ok {
			fns, fnm := splitPackageID(base)
			if fns == "" || fnm == "" {
				return nil, fmt.Errorf("invalid package id")
			}
			return client.FetchVerifiedVersion(ctx, fns, fnm, version)
		}
		return s.fetchPackage(ctx, client, id)
	})
	pkg, err := cache.get(ns + "/" + nm)
	if err != nil {
		return nil, registryError("could not fetch the package", err)
	}
	if isDowngrade(ip.Version, pkg.Manifest.Version) {
		return nil, errs.NewValidationError(fmt.Errorf(
			"the latest version on the Bait Box, v%s, is older than the installed v%s",
			pkg.Manifest.Version, ip.Version,
		))
	}
	// download everything the update reads before taking the lock, so a slow
	// registry never keeps other installs, updates or uninstalls waiting.
	if err := s.prefetchUpdate(ctx, ip, pkg, cache); err != nil {
		return nil, err
	}

	unlock, err := s.lockOps(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	// the package may have been updated or removed while downloading. Check again
	// under the lock and work only from what was downloaded.
	cur, err := s.RegistryRepository.GetInstalledPackageByID(ctx, installedPackageID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUninstalledDuringUpdate
	}
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if cur.Version != ip.Version || cur.ContentHash != ip.ContentHash {
		return nil, errs.NewValidationError(errNotDownloaded)
	}
	cache.offline = true
	// once writing starts it runs to the end even if the request ends, so an
	// update and its undo are never cut off halfway.
	return s.applyUpdate(
		context.WithoutCancel(ctx), session, ae, cur, pkg, cache.get, vars, resolutions, confirmReportTemplates,
	)
}

// oauthPicksForUpdate returns the values an update binds OAuth provider picks
// with. A value given in this update always counts. A stored value counts only
// when the install recorded it as the operator's pick and no API sender this
// update adds names it, so the update never binds a provider the operator did not
// choose for that sender. A stored value that does not count, for a pick an API
// sender names, is returned as missing so the operator chooses again.
func (s *ContentRegistry) oauthPicksForUpdate(
	ctx context.Context,
	ip *database.InstalledPackage,
	pkg *registry.VerifiedPackage,
	docs []contentDoc,
	resources []*database.InstalledResource,
	vars, merged map[string]string,
	share bool,
) (map[string]string, []MissingVariable) {
	picks := map[string]string{}
	for k, v := range merged {
		picks[k] = v
	}
	named := senderPickTokens(docs)
	fresh := map[string]bool{}
	for _, doc := range docs {
		for _, as := range doc.dy.APISenders {
			v, ok := singleToken(as.OAuthProvider)
			if ok && !s.apiSenderInstalled(ctx, resources, strings.TrimSpace(as.Name)) {
				fresh[v] = true
			}
		}
	}
	old := installedOAuthPicks(ip)
	var missing []MissingVariable
	for _, v := range pkg.Manifest.Variables {
		if v.Pick != pickOAuthProvider {
			continue
		}
		key := varKey(pkg.Manifest.ID, v.Name, share)
		if strings.TrimSpace(vars[key]) != "" {
			continue
		}
		if old[v.Name] && !fresh[v.Name] {
			continue
		}
		delete(picks, key)
		if named[v.Name] && strings.TrimSpace(merged[key]) != "" {
			missing = append(missing, missingVar(pkg.Manifest.ID, v, share))
		}
	}
	return picks, missing
}

// memberOAuthPicksMissing returns the OAuth provider picks an API sender of a
// package the update adds names that have no value in vars, the values given in
// this update. A stored value never binds a sender the operator has not seen.
func memberOAuthPicksMissing(pkgs []*registry.VerifiedPackage, vars map[string]string, share bool) []MissingVariable {
	var missing []MissingVariable
	for _, dp := range pkgs {
		docs, err := contentDocs(dp)
		if err != nil {
			continue
		}
		named := senderPickTokens(docs)
		for _, v := range dp.Manifest.Variables {
			if v.Pick != pickOAuthProvider || !named[v.Name] {
				continue
			}
			if strings.TrimSpace(vars[varKey(dp.Manifest.ID, v.Name, share)]) != "" {
				continue
			}
			missing = append(missing, missingVar(dp.Manifest.ID, v, share))
		}
	}
	return missing
}

// senderPickTokens returns the variable names that API senders in docs use as
// their whole OAuth provider reference.
func senderPickTokens(docs []contentDoc) map[string]bool {
	named := map[string]bool{}
	for _, doc := range docs {
		for _, as := range doc.dy.APISenders {
			if v, ok := singleToken(as.OAuthProvider); ok {
				named[v] = true
			}
		}
	}
	return named
}

// appendMissing adds the variables of more whose key is not in list yet.
func appendMissing(list, more []MissingVariable) []MissingVariable {
	seen := map[string]bool{}
	for _, m := range list {
		seen[m.Key] = true
	}
	for _, m := range more {
		if !seen[m.Key] {
			seen[m.Key] = true
			list = append(list, m)
		}
	}
	return list
}

// installedOAuthPicks returns the names of the OAuth provider picks the install
// recorded as given by the operator.
func installedOAuthPicks(ip *database.InstalledPackage) map[string]bool {
	out := map[string]bool{}
	var names []string
	if json.Unmarshal([]byte(ip.OAuthPicks), &names) != nil {
		return out
	}
	for _, n := range names {
		out[n] = true
	}
	return out
}

// suppliedOAuthPicks returns the json list of the OAuth provider pick variables
// of manifest that have a value in supplied.
func suppliedOAuthPicks(manifest *registry.Manifest, supplied map[string]string, share bool) string {
	names := []string{}
	for _, v := range manifest.Variables {
		if v.Pick == pickOAuthProvider && strings.TrimSpace(supplied[varKey(manifest.ID, v.Name, share)]) != "" {
			names = append(names, v.Name)
		}
	}
	out, _ := json.Marshal(names)
	return string(out)
}

// apiSenderInstalled reports whether the install tracks an API sender under the
// source name and that sender still exists.
func (s *ContentRegistry) apiSenderInstalled(
	ctx context.Context, resources []*database.InstalledResource, name string,
) bool {
	r := trackedResource(resources, "apisender", name)
	if r == nil {
		return false
	}
	_, err := s.APISenderRepository.GetByID(ctx, r.ResourceID, &repository.APISenderOption{})
	return err == nil
}

// trackedResource returns the row the install tracks for resourceType under the
// source name, or nil.
func trackedResource(
	resources []*database.InstalledResource, resourceType, name string,
) *database.InstalledResource {
	for _, r := range resources {
		if r.ResourceType == resourceType && r.ResourceID != nil && strings.TrimSpace(r.SourceName) == name {
			return r
		}
	}
	return nil
}

// prefetchUpdate downloads the packages applyUpdate reads through fetch: each
// bundle member and each newly referenced package that is already installed,
// kept as manifest only since only their variables are read; each newly
// referenced package that is not installed yet, in full, since the update
// installs it; and the installed version when the package tracks an smtp
// configuration whose headers are compared. A download failure is kept in the
// cache and handled where the package is read. It fails when the new version
// references too many packages or the packages it installs exceed
// maxBundleBytes.
func (s *ContentRegistry) prefetchUpdate(
	ctx context.Context, ip *database.InstalledPackage, pkg *registry.VerifiedPackage, cache *packageCache,
) error {
	var deps []registry.Dependency
	if pkg.Manifest != nil {
		deps = pkg.Manifest.Dependencies
	}
	if len(deps) > maxBundleDependencies {
		return errs.NewValidationError(fmt.Errorf("package references too many packages"))
	}
	linked := map[string]bool{}
	var current []*database.InstalledPackage
	if members, err := s.RegistryRepository.GetPackageMembers(ctx, ip.ID); err == nil {
		for _, m := range members {
			if mp, merr := s.RegistryRepository.GetInstalledPackageByID(ctx, m); merr == nil && mp != nil {
				linked[mp.PackageID] = true
				current = append(current, mp)
				_, _ = cache.getManifest(mp.PackageID)
			}
		}
	}
	// a dropped member is checked for local edits, which for an smtp configuration
	// compares against the headers its installed version declared.
	wanted := map[string]bool{}
	for _, d := range deps {
		wanted[d.ID] = true
	}
	for _, mp := range current {
		if !wanted[mp.PackageID] {
			s.prefetchSMTPBaseline(ctx, mp, cache)
		}
	}
	total := packageBytes(pkg)
	for _, d := range deps {
		if linked[d.ID] {
			continue
		}
		linked[d.ID] = true
		if ex, err := s.RegistryRepository.GetInstalledPackage(ctx, ip.RegistryID, d.ID); err == nil && ex != nil {
			_, _ = cache.getManifest(d.ID)
			continue
		}
		dp, err := cache.get(d.ID)
		if err != nil || dp == nil {
			continue
		}
		total += packageBytes(dp)
		if total > maxBundleBytes {
			return errs.NewValidationError(fmt.Errorf(
				"referenced packages exceed the %d MiB install size limit", maxBundleBytes>>20,
			))
		}
	}
	s.prefetchSMTPBaseline(ctx, ip, cache)
	return nil
}

// prefetchSMTPBaseline downloads the installed version of a package that tracks
// an smtp configuration, whose declared headers a drift check compares.
func (s *ContentRegistry) prefetchSMTPBaseline(
	ctx context.Context, ip *database.InstalledPackage, cache *packageCache,
) {
	if ip.Version == "" {
		return
	}
	if resources, err := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, ip.ID); err == nil {
		for _, r := range resources {
			if r.ResourceType == "smtp" {
				_, _ = cache.get(ip.PackageID + "@" + ip.Version)
				return
			}
		}
	}
}

// memberPlan is how an update changes the packages a bundle references.
type memberPlan struct {
	// toInstall are newly referenced packages not installed yet; the update
	// installs each as a dependency of the bundle.
	toInstall []*registry.VerifiedPackage
	// reuse are newly referenced packages already installed from the same
	// registry; the update only links them.
	reuse []*uuid.UUID
	// linked maps each package id the new version references to its installed
	// package, for those installed before the update.
	linked map[string]*uuid.UUID
	// dropped are members the new version no longer references.
	dropped []*uuid.UUID
}

// planMembers diffs the packages the new version references against the
// bundle's member rows, writing nothing. A newly referenced package that is not
// installed must be in fetch with its files, as prefetchUpdate leaves it.
func (s *ContentRegistry) planMembers(
	ctx context.Context,
	ip *database.InstalledPackage,
	pkg *registry.VerifiedPackage,
	members []*uuid.UUID,
	fetch func(id string) (*registry.VerifiedPackage, error),
) (*memberPlan, error) {
	var deps []registry.Dependency
	if pkg.Manifest != nil {
		deps = pkg.Manifest.Dependencies
	}
	if len(deps) > maxBundleDependencies {
		return nil, errs.NewValidationError(fmt.Errorf("package references too many packages"))
	}
	plan := &memberPlan{linked: map[string]*uuid.UUID{}}
	memberByPkg := map[string]*uuid.UUID{}
	var memberOrder []string
	for _, m := range members {
		if m == nil {
			continue
		}
		mp, err := s.RegistryRepository.GetInstalledPackageByID(ctx, m)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// a link to a package that is gone; drop the link.
			plan.dropped = append(plan.dropped, m)
			continue
		}
		if err != nil {
			return nil, errs.Wrap(err)
		}
		memberByPkg[mp.PackageID] = m
		memberOrder = append(memberOrder, mp.PackageID)
	}
	wanted := map[string]bool{}
	total := packageBytes(pkg)
	for _, d := range deps {
		if wanted[d.ID] {
			continue
		}
		wanted[d.ID] = true
		if m, ok := memberByPkg[d.ID]; ok {
			plan.linked[d.ID] = m
			continue
		}
		ex, err := s.RegistryRepository.GetInstalledPackage(ctx, ip.RegistryID, d.ID)
		if err == nil && ex != nil {
			plan.reuse = append(plan.reuse, ex.ID)
			plan.linked[d.ID] = ex.ID
			continue
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.Wrap(err)
		}
		if fetch == nil {
			return nil, errs.NewValidationError(errNotDownloaded)
		}
		dp, ferr := fetch(d.ID)
		if errors.Is(ferr, errNotDownloaded) || (ferr == nil && manifestOnly(dp)) {
			return nil, errs.NewValidationError(errNotDownloaded)
		}
		if ferr != nil {
			return nil, registryError("could not fetch referenced package "+d.ID, ferr)
		}
		total += packageBytes(dp)
		if total > maxBundleBytes {
			return nil, errs.NewValidationError(fmt.Errorf(
				"referenced packages exceed the %d MiB install size limit", maxBundleBytes>>20,
			))
		}
		plan.toInstall = append(plan.toInstall, dp)
	}
	for _, pid := range memberOrder {
		if !wanted[pid] {
			plan.dropped = append(plan.dropped, memberByPkg[pid])
		}
	}
	// install an oauth provider before the api sender that names it, as install does.
	sort.SliceStable(plan.toInstall, func(i, j int) bool {
		return installPriority(plan.toInstall[i].Manifest.Type) < installPriority(plan.toInstall[j].Manifest.Type)
	})
	return plan, nil
}

// memberVarsInOrder merges the variable values of the packages a bundle
// references in the order its manifest lists them, a later one overriding an
// earlier one on a shared name, the order install uses. A package installed
// before the update contributes the values it bound; one the update installs
// contributes the values supplied now.
func (s *ContentRegistry) memberVarsInOrder(
	ctx context.Context,
	pkg *registry.VerifiedPackage,
	plan *memberPlan,
	vars map[string]string,
	share bool,
	fetch func(id string) (*registry.VerifiedPackage, error),
) (map[string]string, error) {
	out := map[string]string{}
	if pkg.Manifest == nil {
		return out, nil
	}
	fresh := map[string]*registry.VerifiedPackage{}
	for _, dp := range plan.toInstall {
		fresh[dp.Manifest.ID] = dp
	}
	for _, d := range pkg.Manifest.Dependencies {
		var mv map[string]string
		if dp, ok := fresh[d.ID]; ok {
			mv = effectiveVarsFor(dp, vars, share)
		} else if m, ok := plan.linked[d.ID]; ok {
			var err error
			if mv, err = s.memberVarsFor(ctx, fetch, []*uuid.UUID{m}); err != nil {
				return nil, err
			}
		}
		for k, v := range mv {
			out[k] = v
		}
	}
	// a dropped member is still installed while the bundle's content is written, so
	// its values come last as orderMembers ranks an unlisted member.
	if len(plan.dropped) > 0 {
		mv, err := s.memberVarsFor(ctx, fetch, s.orderMembers(ctx, nil, plan.dropped))
		if err != nil {
			return nil, err
		}
		for k, v := range mv {
			out[k] = v
		}
	}
	return out, nil
}

// declareInstallNames adds the names a fresh install of pkg creates for the
// types a campaign template can name, so the plan phase accepts a reference to
// an entity a newly referenced package will create.
func (s *ContentRegistry) declareInstallNames(declared declaredNames, pkg *registry.VerifiedPackage, vars map[string]string) error {
	docs, err := contentDocs(pkg)
	if err != nil {
		return s.contentDocsErr(err)
	}
	for _, doc := range docs {
		dy := doc.dy
		for _, p := range dy.Pages {
			declared.add("page", p.Name)
		}
		for _, e := range dy.Emails {
			declared.add("email", e.Name)
		}
		for _, px := range dy.Proxies {
			declared.add("proxy", px.Name)
		}
		for _, d := range dy.Domains {
			if !strings.EqualFold(d.Type, "proxy") {
				declared.add("domain", substitute(d.Name, vars))
			}
		}
		for _, sm := range dy.SMTPConfigurations {
			declared.add("smtp", sm.Name)
		}
		for _, as := range dy.APISenders {
			declared.add("apisender", as.Name)
		}
		for _, idn := range dy.Identifiers {
			declared.add("identifier", idn.Name)
		}
	}
	return nil
}

// installMembersTracked installs the newly referenced packages of a bundle
// update as dependencies and links them and the reused ones to the bundle, the
// same steps applyInstall takes. An OAuth provider pick of a new member counts
// only with a value in picks, the values given in this update. Each step pushes
// its reverse onto undo.
func (s *ContentRegistry) installMembersTracked(
	ctx context.Context,
	session *model.Session,
	bundle *database.InstalledPackage,
	plan *memberPlan,
	vars map[string]string,
	share bool,
	boundVars string,
	picks map[string]string,
	result *InstallResult,
	undo *updateUndo,
) error {
	link := func(m *uuid.UUID) error {
		if err := s.RegistryRepository.InsertPackageMember(ctx, bundle.ID, m); err != nil {
			return errs.Wrap(err)
		}
		undo.pushErr(func() error { return s.RegistryRepository.DeletePackageMember(ctx, bundle.ID, m) })
		return nil
	}
	for _, dp := range plan.toInstall {
		depIP, err := s.RegistryRepository.InsertInstalledPackage(ctx, &database.InstalledPackage{
			RegistryID: bundle.RegistryID, PackageID: dp.Manifest.ID, PackageType: dp.Manifest.Type,
			Version: dp.Manifest.Version, ContentHash: dp.Manifest.ContentHash, InstalledAsDependency: true,
			VariablesScoped: !share, OAuthPicks: suppliedOAuthPicks(dp.Manifest, picks, share),
		})
		if err != nil {
			return errs.Wrap(err)
		}
		undo.pushErr(func() error {
			if !s.rollbackInstall(ctx, session, depIP, false) {
				return fmt.Errorf("referenced package %s was not fully removed", dp.Manifest.ID)
			}
			return nil
		})
		depVars := effectiveVarsFor(dp, vars, share)
		if err := s.installEntities(ctx, session, dp, depVars, depIP, boundVars, picks, result); err != nil {
			return err
		}
		if err := s.installCampaignTemplates(ctx, session, dp, depVars, depIP, boundVars, result); err != nil {
			return err
		}
		if err := link(depIP); err != nil {
			return err
		}
	}
	for _, m := range plan.reuse {
		if err := link(m); err != nil {
			return err
		}
	}
	return nil
}

// unlinkDroppedMembers unlinks the members a bundle update no longer references,
// with the same rules as uninstall: a member no other bundle links that was
// installed only as a dependency is removed through remove, so its entities go
// the same way an uninstall takes them, unless keep reports true for it. A kept
// member is unlinked and stays installed on its own. It runs last and never
// fails the update; a member that cannot be removed or kept keeps its link and
// the next update tries again. Each outcome is reported in result. It returns
// the package ids of the kept members.
func (s *ContentRegistry) unlinkDroppedMembers(
	ctx context.Context,
	session *model.Session,
	bundleID *uuid.UUID,
	dropped []*uuid.UUID,
	keep func(packageID string) bool,
	result *InstallResult,
) []string {
	var keptIDs []string
	for _, m := range dropped {
		label := m.String()
		removeMember, gone, kept := false, false, false
		mp, gerr := s.RegistryRepository.GetInstalledPackageByID(ctx, m)
		switch {
		case errors.Is(gerr, gorm.ErrRecordNotFound):
			gone = true
		case gerr != nil:
			s.Logger.Warnw("failed to read a package the new bundle version no longer references",
				"package", label, "error", gerr)
			result.Skipped = append(result.Skipped,
				"referenced package "+label+" could not be read and stays linked")
			continue
		default:
			label = mp.PackageID
			n, cerr := s.RegistryRepository.CountBundlesForMember(ctx, m, bundleID)
			if cerr != nil {
				s.Logger.Warnw("failed to count the bundles that reference a package",
					"package", label, "error", cerr)
				result.Skipped = append(result.Skipped,
					"referenced package "+label+" could not be checked and stays linked")
				continue
			}
			removeMember = n == 0 && mp.InstalledAsDependency
			if removeMember && keep != nil && keep(mp.PackageID) {
				if serr := s.RegistryRepository.SetInstalledAsDependency(ctx, m, false); serr != nil {
					s.Logger.Warnw("failed to keep a package on its own",
						"package", label, "error", serr)
					result.Skipped = append(result.Skipped,
						"referenced package "+label+" could not be kept on its own and stays linked")
					continue
				}
				removeMember, kept = false, true
			}
		}
		if removeMember {
			if rerr := s.remove(ctx, session, m); rerr != nil {
				s.Logger.Warnw("failed to remove a package the new bundle version no longer references",
					"package", label, "error", rerr)
				result.Skipped = append(result.Skipped, "referenced package "+label+
					" is no longer in the bundle but could not be removed and stays installed")
				continue
			}
		}
		if lerr := s.RegistryRepository.DeletePackageMember(ctx, bundleID, m); lerr != nil {
			s.Logger.Warnw("failed to unlink a package from its bundle",
				"package", label, "error", lerr)
			result.Skipped = append(result.Skipped,
				"referenced package "+label+" could not be unlinked")
			continue
		}
		switch {
		case gone:
			// a link to a package that no longer exists, nothing to report.
		case removeMember:
			result.Skipped = append(result.Skipped, "referenced package "+label+" removed (no longer in bundle)")
		case kept:
			keptIDs = append(keptIDs, label)
			result.Skipped = append(result.Skipped,
				"referenced package "+label+" unlinked (no longer in bundle, kept installed on its own)")
		default:
			result.Skipped = append(result.Skipped,
				"referenced package "+label+" unlinked (no longer in bundle, kept installed)")
		}
	}
	return keptIDs
}

// droppedMemberEntities lists the entities the members a bundle update drops
// would remove, by resource type and id, each mapped to the package id that owns
// it. It follows the rules of unlinkDroppedMembers and remove: a member is removed
// only when no other bundle links it and it was installed as a dependency, and a
// removed bundle member removes its own members by the same rule. A dropped
// member keep reports true for stays installed and is left out. Identifiers and
// assets are left out since removal keeps them or no template names them.
func (s *ContentRegistry) droppedMemberEntities(
	ctx context.Context, bundleID *uuid.UUID, dropped []*uuid.UUID, keep func(packageID string) bool,
) (map[string]map[uuid.UUID]string, error) {
	owned := map[string]map[uuid.UUID]string{}
	seen := map[uuid.UUID]bool{}
	var walk func(parent *uuid.UUID, members []*uuid.UUID) error
	walk = func(parent *uuid.UUID, members []*uuid.UUID) error {
		for _, m := range members {
			if m == nil || seen[*m] {
				continue
			}
			mp, gerr := s.RegistryRepository.GetInstalledPackageByID(ctx, m)
			if errors.Is(gerr, gorm.ErrRecordNotFound) {
				continue
			}
			if gerr != nil {
				return errs.Wrap(gerr)
			}
			n, cerr := s.RegistryRepository.CountBundlesForMember(ctx, m, parent)
			if cerr != nil {
				return errs.Wrap(cerr)
			}
			if n != 0 || !mp.InstalledAsDependency {
				continue
			}
			if parent == bundleID && keep != nil && keep(mp.PackageID) {
				continue
			}
			seen[*m] = true
			rows, rerr := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, m)
			if rerr != nil {
				return errs.Wrap(rerr)
			}
			for _, r := range rows {
				if r.ResourceID == nil || r.ResourceType == "identifier" || r.ResourceType == "asset" {
					continue
				}
				if owned[r.ResourceType] == nil {
					owned[r.ResourceType] = map[uuid.UUID]string{}
				}
				owned[r.ResourceType][*r.ResourceID] = mp.PackageID
			}
			sub, merr := s.RegistryRepository.GetPackageMembers(ctx, m)
			if merr != nil {
				return errs.Wrap(merr)
			}
			if err := walk(m, sub); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(bundleID, dropped); err != nil {
		return nil, err
	}
	return owned, nil
}

// checkDroppedMemberRefs refuses an update whose campaign templates still name an
// entity in owned, the entities a dropped member takes with it.
func (s *ContentRegistry) checkDroppedMemberRefs(
	ctx context.Context, docs []contentDoc, effective map[string]string, owned map[string]map[uuid.UUID]string,
) error {
	if len(owned) == 0 {
		return nil
	}
	for _, doc := range docs {
		for _, ct := range doc.dy.CampaignTemplates {
			for _, r := range campaignTemplateRefs(ct, effective) {
				ids := owned[r.resourceType]
				if ids == nil {
					continue
				}
				id, err := s.resolveReferenceID(ctx, r.resourceType, r.name)
				if err != nil || id == nil {
					continue
				}
				if pkgID, ok := ids[*id]; ok {
					name := strings.TrimSpace(substitute(ct.Name, effective))
					return errs.NewValidationError(fmt.Errorf(
						"campaign template %q in the new version names %s %q, which belongs to "+
							"referenced package %s that this update removes",
						name, r.label, r.name, pkgID,
					))
				}
			}
		}
	}
	return nil
}

// applyUpdate reconciles an installed package against its fetched and verified
// new version. fetch resolves a bundle member by package id, to read the
// variables the member declares.
//
// It runs in phases. The plan phase writes nothing: it checks variables and
// resolutions, raises local edit conflicts, and checks every model the update
// will write and every entity a campaign template or api sender names, so a bad
// declaration or a missing reference fails before the first change. The write
// phase then applies assets, identifiers, creates and in place updates, each
// pushing its reverse onto undo; when a write still fails, undo deletes what was
// created, writes the loaded fields back onto entities updated in place, and
// restores provenance, so content and its recorded baseline stay consistent and
// no false local edit is left behind. The recorded version advances only when
// every write succeeded. Removals of resources the version dropped run last and
// never fail the update: one that cannot be removed stays tracked and the next
// update tries again.
func (s *ContentRegistry) applyUpdate(
	ctx context.Context, session *model.Session, ae *AuditEvent,
	ip *database.InstalledPackage, pkg *registry.VerifiedPackage,
	fetch func(id string) (*registry.VerifiedPackage, error),
	vars map[string]string, resolutions map[string]string, confirmReportTemplates []string,
) (*InstallResult, error) {
	installedPackageID := ip.ID
	// the undo steps keep this ctx. cancelUndo ends it rollbackTimeout after a
	// rollback starts, so undoing a failed update has the same time limit as
	// undoing a failed install.
	ctx, cancelUndo := context.WithCancel(ctx)
	defer cancelUndo()
	if err := validateResolutions(resolutions); err != nil {
		return nil, err
	}
	pkgs := []*registry.VerifiedPackage{pkg}
	// use the share mode the install used. A bundle installed with sharing off stored
	// each package's values, its members' too, under package scoped keys, so an
	// update must look them up the same way or a supplied value reads as missing and
	// the author default is substituted instead.
	share := installedShare(ip, pkg.Manifest)
	// a bundle version can reference packages the installed one did not, and drop
	// some it did. The new ones are installed with the bundle's values and checked
	// with it below; the dropped ones are unlinked after every write succeeded.
	members, merr := s.RegistryRepository.GetPackageMembers(ctx, installedPackageID)
	if merr != nil {
		return nil, errs.Wrap(merr)
	}
	mplan, err := s.planMembers(ctx, ip, pkg, members, fetch)
	if err != nil {
		return nil, err
	}
	if len(mplan.toInstall) > 0 {
		added := make([]string, 0, len(mplan.toInstall))
		for _, dp := range mplan.toInstall {
			added = append(added, dp.Manifest.ID)
		}
		ae.Details["membersInstalled"] = added
	}
	if len(mplan.dropped) > 0 {
		ae.Details["membersDropped"] = len(mplan.dropped)
	}
	checkPkgs := append(append([]*registry.VerifiedPackage{}, pkgs...), mplan.toInstall...)
	if err := checkSharedPicks(checkPkgs, share); err != nil {
		return nil, err
	}
	userMerged := s.mergeBoundVars(ctx, installedPackageID, vars)
	if missing := requiredMissing(checkPkgs, userMerged, share); len(missing) > 0 {
		return nil, ErrMissingVariables{Variables: missing}
	}
	if err := validateEnumValues(checkPkgs, userMerged, share); err != nil {
		return nil, err
	}
	// effectiveVarsFor reads each value under the key install used (bare when shared,
	// package scoped when not) and maps it to the bare name the content substitutes,
	// so a supplied scoped value is applied rather than falling back to the default.
	effective := effectiveVarsFor(pkg, userMerged, share)
	// a bundle's own content was installed with the values of every member merged
	// in, members overriding the bundle, so a reference to a member entity whose
	// name is a variable resolves to the name the member created. Do the same here,
	// reading each member's values from its own install.
	mv, err := s.memberVarsInOrder(ctx, pkg, mplan, userMerged, share, fetch)
	if err != nil {
		return nil, err
	}
	for k, v := range mv {
		effective[k] = v
	}

	docs, derr := contentDocs(pkg)
	if derr != nil {
		return nil, s.contentDocsErr(derr)
	}
	resources, err := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, installedPackageID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	picks, missingPicks := s.oauthPicksForUpdate(ctx, ip, pkg, docs, resources, vars, userMerged, share)
	missingPicks = appendMissing(missingPicks, memberOAuthPicksMissing(mplan.toInstall, vars, share))
	if len(missingPicks) > 0 {
		return nil, ErrMissingVariables{Variables: missingPicks}
	}
	// a package the update adds is checked as a fresh install is, with picks only
	// from the values given now.
	if len(mplan.toInstall) > 0 {
		targets := make([]oauthCheckTarget, 0, len(mplan.toInstall))
		for _, dp := range mplan.toInstall {
			targets = append(targets, oauthCheckTarget{pkg: dp, vars: effectiveVarsFor(dp, userMerged, share)})
		}
		if err := s.checkOAuthBindings(ctx, ip.RegistryID, targets, vars, share); err != nil {
			return nil, err
		}
	}
	boundVars, _ := json.Marshal(userMerged)
	result := &InstallResult{PackageID: ip.PackageID}
	undo := &updateUndo{}

	// one reconcile set per content bearing type. addOrder keeps the install
	// ordering so a referenced entity exists before the one that names it (an oauth
	// provider before an api sender, every entity before the campaign template that
	// binds them). removals run in reverse.
	type reconcileSet struct {
		resourceType string
		addOrder     int
		report       *[]string
		items        []reconcileItem
		currents     map[string]reconcileCurrent
	}
	var sets []reconcileSet
	add := func(
		resourceType string, addOrder int, report *[]string,
		items []reconcileItem, currents map[string]reconcileCurrent,
	) {
		sets = append(sets, reconcileSet{resourceType, addOrder, report, items, currents})
	}

	pItems, pCur := s.buildPageReconcile(ctx, session, pkg, docs, effective, resources, undo)
	add("page", 30, &result.Pages, pItems, pCur)
	eItems, eCur := s.buildEmailReconcile(
		ctx, session, installedPackageID, pkg, docs, effective, resources, string(boundVars), result, undo,
	)
	add("email", 30, &result.Emails, eItems, eCur)
	addSpec := func(resourceType string, addOrder int) {
		items, currents := s.specReconcile(ctx, session, resourceType, pkg, docs, effective, resources, undo)
		add(resourceType, addOrder, &result.Others, items, currents)
	}
	addSpec("script", 30)
	rtItems, rtCur := s.buildReportTemplateReconcile(ctx, session, pkg, docs, effective, resources)
	add("reporttemplate", 30, &result.Others, rtItems, rtCur)
	addSpec("proxy", 30)
	addSpec("remotebrowser", 30)
	addSpec("domain", 30)
	addSpec("allowdeny", 30)
	smHeaders := installedSMTPHeaders(ip, fetch, func(*registry.VerifiedPackage) map[string]string {
		return effective
	})
	smItems, smCur := s.buildSMTPReconcile(ctx, session, docs, effective, resources, undo, smHeaders)
	add("smtp", 30, &result.Others, smItems, smCur)
	addSpec("webhook", 30)
	opItems, opCur := s.specReconcile(ctx, session, "oauthprovider", pkg, docs, effective, resources, undo)
	s.markOAuthURLChanges(ctx, docs, effective, resources, opItems)
	add("oauthprovider", 20, &result.Others, opItems, opCur)
	binding := &oauthBinding{registryID: ip.RegistryID, manifest: pkg.Manifest, supplied: picks, share: share}
	asItems, asCur := s.buildAPISenderReconcile(ctx, session, docs, effective, resources, undo, binding)
	add("apisender", 40, &result.Others, asItems, asCur)
	ctItems, ctCur := s.buildCampaignTemplateReconcile(ctx, session, docs, effective, resources)
	add("campaigntemplate", 50, &result.Others, ctItems, ctCur)

	// a row whose entity could not be read fails the update now. A row whose
	// entity was deleted out of band is set aside and dropped in the write phase,
	// so the declaration recreates it.
	var staleRows []*database.InstalledResource
	for _, set := range sets {
		if err := takeStale(set.currents, &staleRows); err != nil {
			return nil, err
		}
	}

	// conflict pass across every type: collect all conflicts before any write so
	// an unresolved one stops the update with nothing changed.
	var conflicts []UpdateConflict
	for _, set := range sets {
		conflicts = append(conflicts, reconcileConflicts(set.resourceType, set.items, set.currents)...)
	}
	// a dropped member whose content the user edited or uses outside the bundle
	// is kept or removed only as the user chooses.
	memberConflicts, err := s.droppedMemberConflicts(ctx, ip, mplan.dropped, fetch)
	if err != nil {
		return nil, err
	}
	conflicts = append(conflicts, memberConflicts...)
	if unresolved := unresolvedConflicts(conflicts, resolutions); len(unresolved) > 0 {
		return nil, ErrUpdateConflicts{Conflicts: unresolved}
	}
	keepMember := func(packageID string) bool { return resolutions[memberKey(packageID)] == resolveKeep }
	// a report template the install does not track yet overwrites the global one of
	// its kind, which the user must confirm before anything is written.
	if err := s.reportTemplateDeclsValid(checkPkgs); err != nil {
		return nil, err
	}
	rtDecls, err := s.reportTemplateDeclsOf(mplan.toInstall)
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		for _, rt := range doc.dy.ReportTemplates {
			name := strings.TrimSpace(rt.Name)
			if _, ok := pkg.Files[doc.prefix+rt.File]; !ok {
				continue
			}
			if _, tracked := rtCur[name]; tracked {
				continue
			}
			rtDecls = append(rtDecls, reportTemplateDecl{
				sourceName: name, isTraining: rt.IsTraining, packageID: ip.PackageID,
			})
		}
	}
	overwrites, err := s.planReportTemplateOverwrites(ctx, rtDecls, confirmReportTemplates)
	if err != nil {
		return nil, err
	}
	if len(overwrites) > 0 {
		ae.Details["reportTemplateOverwrite"] = reportTemplateOverwriteAudit(overwrites)
	}

	// plan: every write about to happen is checked here, against the entities that
	// exist now plus the names this version declares, before anything changes.
	declared := declaredNames{}
	for _, set := range sets {
		for _, it := range set.items {
			if it.hasContent {
				declared.add(set.resourceType, it.match)
			}
		}
	}
	for _, doc := range docs {
		for _, idn := range doc.dy.Identifiers {
			declared.add("identifier", idn.Name)
		}
	}
	for _, dp := range mplan.toInstall {
		if err := s.declareInstallNames(declared, dp, effectiveVarsFor(dp, userMerged, share)); err != nil {
			return nil, err
		}
	}
	for _, set := range sets {
		for _, it := range set.items {
			if it.preflight == nil || !willWrite(set.resourceType, it, set.currents, resolutions) {
				continue
			}
			if perr := it.preflight(declared); perr != nil {
				return nil, perr
			}
		}
	}
	// a campaign template must not keep naming an entity of a member this update
	// removes, or the removal would break the template.
	if len(mplan.dropped) > 0 {
		owned, oerr := s.droppedMemberEntities(ctx, installedPackageID, mplan.dropped, keepMember)
		if oerr != nil {
			return nil, oerr
		}
		if cerr := s.checkDroppedMemberRefs(ctx, docs, effective, owned); cerr != nil {
			return nil, cerr
		}
	}
	// a global asset no installed package owns sits at a path this version ships;
	// report every such path now rather than failing partway through the writes.
	assetConflicts, err := s.assetPathConflicts(ctx, checkPkgs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if len(assetConflicts) > 0 {
		return nil, assetConflictErr(assetConflicts)
	}
	// an email this update creates (new in the version, or the fresh copy of the
	// "both" resolution) creates the attachments it nests that the install does not
	// track yet. Their file names must be free and distinct; report every clash.
	trackedAtt := map[string]bool{}
	for _, r := range resources {
		if r.ResourceType == "attachment" {
			trackedAtt[strings.TrimSpace(r.SourceName)] = true
		}
	}
	var newAtts []attachmentDecl
	ei := 0
	for _, doc := range docs {
		for _, e := range doc.dy.Emails {
			it := eItems[ei]
			ei++
			if !it.hasContent {
				continue
			}
			creates := true
			if cur, matched := eCur[it.match]; matched {
				upstreamChanged := it.upstreamFP != cur.res.InstalledContentHash
				creates = upstreamChanged && needsChoice(it, cur) &&
					resolutionFor(resolutions, "email", it.match) == resolveBoth
			}
			if !creates {
				continue
			}
			for _, at := range e.Attachments {
				if _, ok := pkg.Files[doc.prefix+at.File]; ok {
					newAtts = append(newAtts, attachmentDecl{name: strings.TrimSpace(at.Name), fileName: at.File})
				}
			}
		}
	}
	// a newly referenced package creates every attachment it declares, as install
	// does. A key no declared name holds keeps each one from being linked by name
	// or taken as tracked.
	for i, d := range installAttachmentDecls(mplan.toInstall) {
		d.name = fmt.Sprintf("\x00%d", i)
		newAtts = append(newAtts, d)
	}
	attConflicts, err := s.attachmentNameConflicts(ctx, newAtts, trackedAtt, true)
	if err != nil {
		return nil, err
	}
	if len(attConflicts) > 0 {
		return nil, attachmentNamesConflictErr(attConflicts)
	}

	// rollback reverses every write applied so far and records how it went.
	// the undo steps use the ctx they were built with. Update passes a ctx that
	// the request ending does not cancel, and the timer below cancels it once
	// rollbackTimeout has passed.
	rollback := func(e error) (*InstallResult, error) {
		timer := time.AfterFunc(rollbackTimeout, cancelUndo)
		undo.run()
		timer.Stop()
		if undo.failures > 0 {
			ae.Details["rollback"] = "partial"
			return nil, fmt.Errorf("%w (rollback incomplete: %d changes could not be reversed)", e, undo.failures)
		}
		ae.Details["rollback"] = "complete"
		return nil, e
	}

	// newly referenced packages first, so the bundle's own content can name their
	// entities, then drop the rows of entities deleted out of band.
	if merr := s.installMembersTracked(
		ctx, session, ip, mplan, userMerged, share, string(boundVars), vars, result, undo,
	); merr != nil {
		return rollback(merr)
	}
	for _, r := range staleRows {
		if derr := s.deleteResourceTracked(ctx, r, undo); derr != nil {
			return rollback(errs.Wrap(derr))
		}
	}
	// assets and identifiers reconcile first on their own rules (shared, never a
	// conflict), mirroring install order so a page's asset is on disk and a campaign
	// template's identifier exists before the entities that reference them.
	if aerr := s.reconcileAssets(
		ctx, session, installedPackageID, pkg, resources, string(boundVars), result, undo,
	); aerr != nil {
		return rollback(aerr)
	}
	if ierr := s.reconcileIdentifiers(
		ctx, installedPackageID, docs, effective, resources, string(boundVars), result, undo,
	); ierr != nil {
		return rollback(ierr)
	}
	// add and update in install order.
	sort.SliceStable(sets, func(i, j int) bool { return sets[i].addOrder < sets[j].addOrder })
	for _, set := range sets {
		if aerr := s.reconcileAddUpdate(
			ctx, session, installedPackageID, set.resourceType, set.items, set.currents,
			resolutions, string(boundVars), result, set.report, undo,
		); aerr != nil {
			return rollback(aerr)
		}
	}

	// attachments of an email that already existed are not reconciled in place
	// (their bytes live in a shared folder and nest under emails through a junction
	// with per link flags); an email this update created got its attachments above.
	// Compare against the provenance as it is now, so attachments created by this
	// update count. When the declared set still differs, do not advance the version
	// so the package keeps showing an update is available rather than claiming it
	// is up to date.
	after, aerr := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, installedPackageID)
	if aerr != nil {
		return rollback(errs.Wrap(aerr))
	}
	attachmentsChanged := s.attachmentsChanged(docs, pkg, after)
	if attachmentsChanged {
		result.Skipped = append(result.Skipped,
			"attachments of existing emails changed upstream and were left unchanged "+
				"(attachment reconcile is not supported); update kept available")
	}
	// record the picks the senders are now bound with and advance the version
	// through a transaction bound registry repository, the only registry writes
	// left once every entity and provenance change applied.
	if verr := s.RegistryRepository.DB.Transaction(func(tx *gorm.DB) error {
		txReg := &repository.Registry{DB: tx}
		err := txReg.SetInstalledOAuthPicks(ctx, installedPackageID, suppliedOAuthPicks(pkg.Manifest, picks, share))
		if err != nil || attachmentsChanged {
			return err
		}
		return txReg.UpdateInstalledPackageVersion(
			ctx, installedPackageID, pkg.Manifest.Version, pkg.Manifest.ContentHash,
		)
	}); verr != nil {
		return rollback(errs.Wrap(verr))
	}

	// removals in reverse install order so a referencing entity is removed before the
	// one it references. They come last because a delete cascades into other tables
	// and cannot be reversed.
	for i := len(sets) - 1; i >= 0; i-- {
		s.reconcileRemove(ctx, installedPackageID, sets[i].resourceType, sets[i].currents, result)
	}
	// packages the bundle no longer references go last, once its own content no
	// longer names their entities.
	if kept := s.unlinkDroppedMembers(ctx, session, ip.ID, mplan.dropped, keepMember, result); len(kept) > 0 {
		ae.Details["membersKept"] = kept
	}

	ae.Details["version"] = pkg.Manifest.Version
	s.AuditLogAuthorized(ae)
	return result, nil
}
