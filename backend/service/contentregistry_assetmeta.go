package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// installAssetYAML is one entry of the assets block of a data.yaml. It sets
// how a file the package ships in its assets folder is served. Path is
// relative to that folder and is never substituted. Every other field is.
type installAssetYAML struct {
	Path        string              `yaml:"path"`
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	ContentType string              `yaml:"contentType"`
	Headers     []installHeaderYAML `yaml:"headers"`
	AllowDeny   []string            `yaml:"allowDeny"`
}

// assetHeaderKV is one response header of an asset, as stored.
type assetHeaderKV struct {
	key   string
	value string
}

// assetSettings is how an asset is served: its content type, its response
// headers in order and the names of its allow deny lists, each once.
type assetSettings struct {
	contentType string
	headers     []assetHeaderKV
	allowDeny   []string
}

// hash fingerprints the settings. Headers keep their order, since they are
// sent in that order. List names are sorted, since their order has no effect.
func (st assetSettings) hash() string {
	hs := make([]string, 0, len(st.headers))
	for _, h := range st.headers {
		hs = append(hs, h.key+"\x01"+h.value)
	}
	names := append([]string{}, st.allowDeny...)
	sort.Strings(names)
	return hashParts(st.contentType, strings.Join(hs, "\x02"), strings.Join(names, "\x02"))
}

// headerModels returns the headers as models for the repository.
func (st assetSettings) headerModels() []*model.AssetHeader {
	out := make([]*model.AssetHeader, 0, len(st.headers))
	for _, h := range st.headers {
		out = append(out, &model.AssetHeader{
			Key:   nullable.NewNullableWithValue(*vo.NewString127Must(h.key)),
			Value: nullable.NewNullableWithValue(*vo.NewString2048Must(h.value)),
		})
	}
	return out
}

// assetFPSep joins the two parts of an asset fingerprint.
const assetFPSep = "."

// assetFingerprint is the recorded baseline of a package asset: the hash of its
// bytes and the hash of its settings, joined. It changes when either changes,
// and an update can still tell a change of the bytes from a change of the
// settings, since the bytes follow their own rule.
func assetFingerprint(bytesHash string, st assetSettings) string {
	return bytesHash + assetFPSep + st.hash()
}

// splitAssetFingerprint returns the bytes part and the settings part of an
// asset fingerprint. A value without the separator has an empty settings part.
func splitAssetFingerprint(fp string) (string, string) {
	b, st, _ := strings.Cut(fp, assetFPSep)
	return b, st
}

// assetDecl is what a package sets for one asset path, with the values
// substituted and checked.
type assetDecl struct {
	path        string
	name        string
	description string
	settings    assetSettings
	// picked is set when the one list comes from an installer pick, which is a
	// global list found by name.
	picked bool
	// scope resolves the list names, see listScope. It is nil for a pick.
	scope *listScope
}

// scopeDecls sets the list scope of every declaration that names lists from
// list packages.
func scopeDecls(decls map[string]*assetDecl, sc *listScope) {
	for _, d := range decls {
		if !d.picked {
			d.scope = sc
		}
	}
}

// defaultAssetDecl is the asset a package ships without an entry in an assets
// block: named after its file, served with the type found from the file
// extension, no headers and no lists.
func defaultAssetDecl(rel string) *assetDecl {
	return &assetDecl{path: rel, name: filepath.Base(rel)}
}

// assetDeclFor returns the declaration of rel, or the default one.
func assetDeclFor(decls map[string]*assetDecl, rel string) *assetDecl {
	if d, ok := decls[rel]; ok {
		return d
	}
	return defaultAssetDecl(rel)
}

// packageLabel names a package in an error message.
func packageLabel(pkg *registry.VerifiedPackage) string {
	if pkg != nil && pkg.Manifest != nil && pkg.Manifest.ID != "" {
		return "package " + pkg.Manifest.ID
	}
	return "the package"
}

// packageAssetDecls reads every assets block of the package, substitutes the
// values and checks each entry: the path must be a file in the assets folder
// of the same package and appear once across all blocks, and the name,
// description, content type and headers must be values an asset can store.
func packageAssetDecls(pkg *registry.VerifiedPackage, vars map[string]string) (map[string]*assetDecl, error) {
	docs, err := contentDocs(pkg)
	if err != nil {
		return nil, err
	}
	out := map[string]*assetDecl{}
	for _, doc := range docs {
		for _, a := range doc.dy.Assets {
			d, err := buildAssetDecl(pkg, a, vars)
			if err != nil {
				return nil, err
			}
			if _, dup := out[d.path]; dup {
				return nil, errs.NewValidationError(fmt.Errorf(
					"%s sets the asset %q more than once", packageLabel(pkg), d.path,
				))
			}
			out[d.path] = d
		}
	}
	return out, nil
}

// buildAssetDecl checks one assets block entry.
func buildAssetDecl(pkg *registry.VerifiedPackage, a installAssetYAML, vars map[string]string) (*assetDecl, error) {
	rel := a.Path
	fail := func(err error) (*assetDecl, error) {
		return nil, errs.NewValidationError(fmt.Errorf("asset %q in %s: %w", rel, packageLabel(pkg), err))
	}
	if rel == "" {
		return nil, errs.NewValidationError(fmt.Errorf("an asset entry in %s has no path", packageLabel(pkg)))
	}
	if err := checkAssetRel(rel); err != nil {
		return fail(err)
	}
	if _, ok := pkg.Files["assets/"+rel]; !ok {
		return fail(errors.New("the file is not in the assets folder of the package"))
	}
	d := &assetDecl{path: rel}
	name := strings.TrimSpace(substitute(a.Name, vars))
	if name == "" {
		name = filepath.Base(rel)
	}
	n, err := vo.NewOptionalString127(name)
	if err != nil {
		return fail(fmt.Errorf("invalid name: %w", err))
	}
	d.name = n.String()
	desc, err := vo.NewOptionalString255(substitute(a.Description, vars))
	if err != nil {
		return fail(fmt.Errorf("invalid description: %w", err))
	}
	d.description = desc.String()
	ct, err := vo.NewOptionalString255(substitute(a.ContentType, vars))
	if err != nil {
		return fail(fmt.Errorf("invalid content type: %w", err))
	}
	if err := model.ValidateAssetContentType(ct.String()); err != nil {
		return fail(err)
	}
	d.settings.contentType = ct.String()
	if len(a.Headers) > model.MaxAssetHeaders {
		return fail(fmt.Errorf("an asset can have at most %d response headers", model.MaxAssetHeaders))
	}
	for _, h := range a.Headers {
		k, kerr := vo.NewString127(substitute(h.Key, vars))
		if kerr != nil {
			return fail(fmt.Errorf("invalid header name %q: %w", h.Key, kerr))
		}
		v, verr := vo.NewString2048(substitute(h.Value, vars))
		if verr != nil {
			return fail(fmt.Errorf("invalid value for header %q: %w", h.Key, verr))
		}
		if herr := model.ValidateAssetHeader(k.String(), v.String()); herr != nil {
			return fail(fmt.Errorf("header %q: %w", k.String(), herr))
		}
		d.settings.headers = append(d.settings.headers, assetHeaderKV{key: k.String(), value: v.String()})
	}
	picked, err := checkListPickUse(pkg, a.AllowDeny)
	if err != nil {
		return fail(err)
	}
	d.picked = picked
	seen := map[string]bool{}
	for _, raw := range a.AllowDeny {
		// an entry that names no list is refused, never dropped, so an asset is
		// never served with fewer lists than the package meant.
		listName := strings.TrimSpace(substitute(raw, vars))
		if listName == "" {
			return fail(errors.New("an allow/deny list entry is empty"))
		}
		if seen[listName] {
			continue
		}
		if _, nerr := vo.NewString127(listName); nerr != nil {
			return fail(fmt.Errorf("invalid allow/deny list name %q: %w", listName, nerr))
		}
		seen[listName] = true
		d.settings.allowDeny = append(d.settings.allowDeny, listName)
	}
	// a package must not weaken the cache rule a filtered asset is sent with.
	if len(d.settings.allowDeny) > 0 {
		for _, h := range d.settings.headers {
			if http.CanonicalHeaderKey(h.key) != "Cache-Control" {
				continue
			}
			if cerr := model.ValidateFilteredCacheControl(h.value); cerr != nil {
				return fail(cerr)
			}
		}
	}
	return d, nil
}

// assetFilePaths returns the paths, relative to the assets folder, of every
// asset file the package ships, sorted.
func assetFilePaths(pkg *registry.VerifiedPackage) []string {
	var out []string
	for p := range pkg.Files {
		rel, ok := strings.CutPrefix(p, "assets/")
		if !ok || rel == "" {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// storedAssetSettings reads how an asset is served now.
func (s *ContentRegistry) storedAssetSettings(ctx context.Context, id *uuid.UUID) (assetSettings, *model.Asset, error) {
	repo := s.Asset.AssetRepository
	a, err := repo.GetByID(ctx, id)
	if err != nil {
		return assetSettings{}, nil, err
	}
	st := assetSettings{contentType: optStr(a.ContentType)}
	headers, err := repo.GetHeadersByAssetID(ctx, id)
	if err != nil {
		return assetSettings{}, nil, err
	}
	for _, h := range headers {
		st.headers = append(st.headers, assetHeaderKV{key: optStr(h.Key), value: optStr(h.Value)})
	}
	lists, err := repo.GetAllowDenyByAssetID(ctx, id)
	if err != nil {
		return assetSettings{}, nil, err
	}
	for _, l := range lists {
		st.allowDeny = append(st.allowDeny, s.packageListName(ctx, l))
	}
	return st, a, nil
}

// packageListName is the name a package knows a list by: the name its list
// package declared, read from the installed resource that tracks it, so a list
// renamed in Phishing Club reads as unchanged. A list no package tracks has its
// name now.
func (s *ContentRegistry) packageListName(ctx context.Context, l *model.AllowDeny) string {
	name := optStr(l.Name)
	id, err := l.ID.Get()
	if err != nil {
		return name
	}
	rows, err := s.RegistryRepository.GetInstalledResourcesByResource(ctx, "allowdeny", &id)
	if err != nil {
		return name
	}
	for _, r := range rows {
		if n := strings.TrimSpace(r.SourceName); n != "" {
			return n
		}
	}
	return name
}

// storedAssetHash is the fingerprint of a package asset as it is stored now:
// the bytes of its file and its settings, computed the same way the baseline
// is. A file that cannot be read gives an empty bytes part, so it reads as
// changed. Empty when the asset cannot be read.
func (s *ContentRegistry) storedAssetHash(ctx context.Context, id *uuid.UUID) string {
	if s.Asset == nil || s.Asset.AssetRepository == nil {
		return ""
	}
	st, a, err := s.storedAssetSettings(ctx, id)
	if err != nil {
		return ""
	}
	bytesHash := ""
	if rel := optStr(a.Path); rel != "" {
		if b, rerr := s.readAssetFile(rel); rerr == nil {
			bytesHash = contentHash(string(b))
		}
	}
	return assetFingerprint(bytesHash, st)
}

// checkSharedAssetSettings refuses to share the asset at rel unless it is
// served the way decl sets it, as for the bytes in checkSharedAssetBytes.
func (s *ContentRegistry) checkSharedAssetSettings(
	ctx context.Context, assetID, ipID *uuid.UUID, rel string, decl *assetDecl,
) error {
	cur, _, err := s.storedAssetSettings(ctx, assetID)
	if err != nil {
		return errs.Wrap(err)
	}
	if cur.hash() == decl.settings.hash() {
		return nil
	}
	owner, err := s.assetOwnerLabel(ctx, assetID, ipID)
	if err != nil {
		return errs.Wrap(err)
	}
	return errs.NewValidationError(fmt.Errorf(
		"asset path conflict: %q is already installed by %s with a different content type, headers "+
			"or allow/deny lists. Uninstall that package, then try again",
		rel, owner,
	))
}

// globalAllowDeny finds the global allow deny list with the exact name.
func (s *ContentRegistry) globalAllowDeny(ctx context.Context, name string) (*model.AllowDeny, error) {
	if s.AllowDeny == nil || s.AllowDeny.AllowDenyRepository == nil {
		return nil, errors.New("allow/deny lists are not available")
	}
	return s.AllowDeny.AllowDenyRepository.GetByName(ctx, name, nil)
}

// resolveAllowDenyID finds the global allow deny list by name.
func (s *ContentRegistry) resolveAllowDenyID(ctx context.Context, name string) (*uuid.UUID, error) {
	l, err := s.globalAllowDeny(ctx, name)
	if err != nil {
		return nil, err
	}
	id := l.ID.MustGet()
	return &id, nil
}

// resolveAssetList finds the list an asset names, once every list of the
// install or update exists: through sc for a name from a list package, else
// the global list with that name.
func (s *ContentRegistry) resolveAssetList(
	ctx context.Context, rel, name string, sc *listScope,
) (*model.AllowDeny, error) {
	if ref, ok := sc.ref(name); ok {
		return s.scopedList(ctx, sc, ref, name)
	}
	l, err := s.globalAllowDeny(ctx, name)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, assetListErr(rel, name)
	}
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if !model.AllowDenyUsableInCompany(l, nil) {
		return nil, assetListErr(rel, name)
	}
	return l, nil
}

// assetListKind returns whether the list an asset names is an allow list, for
// a check before any write. A list of a package the operation installs does
// not exist yet and has the kind its package declares.
func (s *ContentRegistry) assetListKind(ctx context.Context, rel, name string, sc *listScope) (bool, error) {
	if ref, ok := sc.ref(name); ok && ref.planned {
		return ref.allowed, nil
	}
	l, err := s.resolveAssetList(ctx, rel, name, sc)
	if err != nil {
		return false, err
	}
	allowed, _ := l.Allowed.Get()
	return allowed, nil
}

// assetListErr is the error for an asset list name that does not resolve.
func assetListErr(rel, name string) error {
	return errs.NewValidationError(fmt.Errorf(
		"asset %q uses the allow/deny list %q, which no list package it depends on declares "+
			"and is not an existing global list",
		rel, name,
	))
}

// mixedListsErr is the error for an asset that names both kinds of list.
func mixedListsErr(rel string) error {
	return errs.NewValidationError(fmt.Errorf(
		"asset %q uses both allow lists and block lists; an asset can only use one kind", rel,
	))
}

// assetTarget is a package whose assets an install or update writes, with the
// values its content is substituted with.
type assetTarget struct {
	pkg  *registry.VerifiedPackage
	vars map[string]string
}

// declaredAllowDenies maps the substituted name of every allow deny list the
// targets declare to whether it is an allow list.
func declaredAllowDenies(targets []assetTarget) (map[string]bool, error) {
	out := map[string]bool{}
	for _, t := range targets {
		docs, err := contentDocs(t.pkg)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			for _, ad := range doc.dy.AllowDenies {
				out[strings.TrimSpace(substitute(ad.Name, t.vars))] = ad.Allowed
			}
		}
	}
	return out, nil
}

// checkAssetLists checks, before any write, that every list name resolves
// through sc (see listScope) and that all are of one kind. A company list
// never counts, since a package asset is global.
func (s *ContentRegistry) checkAssetLists(
	ctx context.Context, rel string, names []string, sc *listScope,
) error {
	var kinds []bool
	for _, n := range names {
		allowed, err := s.assetListKind(ctx, rel, n, sc)
		if err != nil {
			return err
		}
		kinds = append(kinds, allowed)
	}
	for _, k := range kinds {
		if k != kinds[0] {
			return mixedListsErr(rel)
		}
	}
	return nil
}

// assetSeen is an asset path one package of an install or update writes, so a
// second package shipping the same path can be compared with it.
type assetSeen struct {
	bytesHash    string
	settingsHash string
	label        string
	// planned is the update plan item of the path when the package being
	// updated ships it.
	planned *assetUpdateItem
}

// checkAssetDecls checks the assets of the targets before any write: every
// assets block is valid, two targets that ship the same path ship the same
// bytes and settings, an asset another installed package owns is shared only
// when it has the same bytes and settings, and every list name resolves. seen
// holds the paths written by packages checked before, and gets the paths of
// the targets added. An asset at a path no installed package owns is left to
// assetPathConflicts.
func (s *ContentRegistry) checkAssetDecls(
	ctx context.Context, targets []assetTarget, scopes map[string]*listScope, seen map[string]assetSeen,
) error {
	for _, t := range targets {
		decls, err := packageAssetDecls(t.pkg, t.vars)
		if err != nil {
			return s.assetDeclsErr(err)
		}
		scopeDecls(decls, scopes[t.pkg.Manifest.ID])
		for _, rel := range assetFilePaths(t.pkg) {
			if err := checkAssetRel(rel); err != nil {
				return err
			}
			decl := assetDeclFor(decls, rel)
			body := t.pkg.Files["assets/"+rel]
			cur := assetSeen{
				bytesHash: contentHash(string(body)), settingsHash: decl.settings.hash(), label: packageLabel(t.pkg),
			}
			if prev, ok := seen[rel]; ok {
				if prev.bytesHash != cur.bytesHash || prev.settingsHash != cur.settingsHash {
					return errs.NewValidationError(fmt.Errorf(
						"asset path conflict: %s and %s both ship %q with different content or settings",
						prev.label, cur.label, rel,
					))
				}
				if err := s.checkSharesPlannedAsset(ctx, prev, cur, rel, body, decl); err != nil {
					return err
				}
				continue
			}
			seen[rel] = cur
			existing, err := s.Asset.AssetRepository.GetGlobalByPath(ctx, rel)
			if err != nil {
				return errs.Wrap(err)
			}
			if existing != nil && existing.ID.IsSpecified() && !existing.ID.IsNull() {
				id := existing.ID.MustGet()
				shared, serr := s.assetOwnedByOtherInstall(ctx, &id, nil)
				if serr != nil {
					return errs.Wrap(serr)
				}
				if !shared {
					continue
				}
				if berr := s.checkSharedAssetBytes(ctx, &id, nil, rel, body); berr != nil {
					return berr
				}
				if serr := s.checkSharedAssetSettings(ctx, &id, nil, rel, decl); serr != nil {
					return serr
				}
				continue
			}
			if lerr := s.checkAssetLists(ctx, rel, decl.settings.allowDeny, decl.scope); lerr != nil {
				return lerr
			}
		}
	}
	return nil
}

// checkSharesPlannedAsset checks a package an update adds that ships a path
// the package being updated already has installed. The added package shares
// that asset, so the asset must not change: an update that changes its bytes
// or settings is refused, since a shared asset cannot change, and the stored
// asset must hold what the added package ships, as for any shared asset.
func (s *ContentRegistry) checkSharesPlannedAsset(
	ctx context.Context, prev, cur assetSeen, rel string, body []byte, decl *assetDecl,
) error {
	it := prev.planned
	if it == nil || it.assetID == nil || it.res == nil {
		return nil
	}
	if it.changes() {
		return errs.NewValidationError(fmt.Errorf(
			"asset path conflict: %q changes in this version of %s and %s, which this version adds, "+
				"ships it too, so the asset would be shared. A shared asset cannot change. "+
				"Ship the asset in one of the packages only",
			rel, prev.label, cur.label,
		))
	}
	if err := s.checkSharedAssetBytes(ctx, it.assetID, nil, rel, body); err != nil {
		return err
	}
	return s.checkSharedAssetSettings(ctx, it.assetID, nil, rel, decl)
}

// assetDeclsErr turns a packageAssetDecls failure into the error for the user.
func (s *ContentRegistry) assetDeclsErr(err error) error {
	var de *dataYAMLError
	if errors.As(err, &de) {
		return s.contentDocsErr(err)
	}
	return err
}

// pendingAssetLink is an asset an install or update created whose allow deny
// lists are set once every list of the install or update exists. body is the
// file, written only after the lists are linked.
type pendingAssetLink struct {
	assetID *uuid.UUID
	path    string
	names   []string
	scope   *listScope
	body    []byte
}

// createdAsset is an asset an install or update created, with the bytes and
// settings it was created with.
type createdAsset struct {
	assetSeen
	id *uuid.UUID
}

// createdAsset returns the asset this install or update created at rel.
func (r *InstallResult) createdAsset(rel string) (createdAsset, bool) {
	if r == nil {
		return createdAsset{}, false
	}
	c, ok := r.createdAssets[rel]
	return c, ok
}

// linkAssetLists sets the lists named on an asset, each resolved through sc,
// with the checks the asset service makes: each list is a global list and all
// are of one kind.
func (s *ContentRegistry) linkAssetLists(
	ctx context.Context, assetID *uuid.UUID, rel string, names []string, sc *listScope,
) error {
	lists := make([]*model.AllowDeny, 0, len(names))
	ids := make([]*uuid.UUID, 0, len(names))
	for _, n := range names {
		l, err := s.resolveAssetList(ctx, rel, n, sc)
		if err != nil {
			return err
		}
		id := l.ID.MustGet()
		lists = append(lists, l)
		ids = append(ids, &id)
	}
	if err := model.ValidateAllowDenySameType(lists); err != nil {
		return mixedListsErr(rel)
	}
	if err := s.Asset.AssetRepository.ReplaceAllowDeny(ctx, assetID, ids); err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// linkPendingAssets sets the lists of every asset the install or update
// created that names lists, and then writes its file, so the file is never
// served without its lists. It runs after every list exists. With undo set,
// each linked asset pushes its delete, which runs before the lists the
// operation created are deleted, so a rollback never leaves the asset without
// its lists. Install deletes the assets it created first on rollback instead.
func (s *ContentRegistry) linkPendingAssets(
	ctx context.Context, session *model.Session, result *InstallResult, undo *updateUndo,
) error {
	pending := result.pendingLinks
	result.pendingLinks = nil
	for _, p := range pending {
		if err := s.linkAssetLists(ctx, p.assetID, p.path, p.names, p.scope); err != nil {
			return err
		}
		if undo != nil {
			id := p.assetID
			undo.pushErr(func() error { return s.deleteResourceEntity(ctx, session, "asset", id) })
		}
		// never write over a file already on disk; it belongs to someone else.
		if err := s.writeAssetFile(p.path, p.body, false); err != nil {
			return errs.NewValidationError(fmt.Errorf("asset %q: %w", p.path, err))
		}
	}
	return nil
}

// deleteCreatedAssets deletes every asset the install created, before the
// lists it created are removed, so a failed delete leaves the asset with its
// lists rather than open. It returns false when any could not be deleted.
func (s *ContentRegistry) deleteCreatedAssets(ctx context.Context, session *model.Session, result *InstallResult) bool {
	clean := true
	rels := make([]string, 0, len(result.createdAssets))
	for rel := range result.createdAssets {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		if err := s.deleteResourceEntity(ctx, session, "asset", result.createdAssets[rel].id); err != nil {
			s.Logger.Errorw("install rollback: delete asset", "path", rel, "error", err)
			clean = false
		}
	}
	return clean
}

// assetUpdateItem is one asset path the new version of a package ships, with
// what the update does with it.
type assetUpdateItem struct {
	rel     string
	body    []byte
	decl    *assetDecl
	bytesH  string
	fp      string
	assetID *uuid.UUID
	// res is the provenance row of this install, nil when it does not own the
	// asset yet.
	res *database.InstalledResource
	// writeBytes is set when the bytes changed upstream.
	writeBytes bool
	// settingsChanged is set when the settings changed upstream, localChanged
	// when the stored settings differ from the baseline too.
	settingsChanged bool
	localChanged    bool
	// filterLoss is set when applying the new settings takes lists off the
	// asset in a way that needs a choice.
	filterLoss *AssetFilterLoss
}

// AssetFilterLoss describes the allow/deny lists an asset loses when an update
// writes its new settings onto it. RemovedLists names each list the asset uses
// now that the new version does not name. OpenToEveryone is set when the asset
// has an allow list now and would have none. Unfiltered is set when the asset
// has lists now and would have none.
type AssetFilterLoss struct {
	RemovedLists   []string `json:"removedLists"`
	OpenToEveryone bool     `json:"openToEveryone"`
	Unfiltered     bool     `json:"unfiltered"`
}

// assetOverwriteNote is shown on every asset conflict.
const assetOverwriteNote = "Overwrite replaces the content type, response headers and allow/deny lists " +
	"of the asset with the ones the new version sets, including any you added."

// needsChoice reports whether the new settings are written only when the
// operator chooses overwrite.
func (it *assetUpdateItem) needsChoice() bool {
	return it.localChanged || it.filterLoss != nil
}

// applySettings reports whether the update writes the new settings onto an
// asset this install owns: the settings changed upstream and either nothing
// needs a choice or the operator chose to overwrite.
func (it *assetUpdateItem) applySettings(resolutions map[string]string) bool {
	if it.res == nil || !it.settingsChanged {
		return false
	}
	if it.needsChoice() {
		return resolutionFor(resolutions, "asset", it.rel) == resolveOverwrite
	}
	return true
}

// changes reports whether the update changes the bytes or the settings of an
// asset this install owns, or would if the operator overwrites.
func (it *assetUpdateItem) changes() bool {
	return it.res != nil && it.fp != it.res.InstalledContentHash
}

// finalFP is the baseline an asset this install owns has after the update.
func (it *assetUpdateItem) finalFP(resolutions map[string]string) string {
	_, baseSettings := splitAssetFingerprint(it.res.InstalledContentHash)
	if it.applySettings(resolutions) {
		baseSettings = it.decl.settings.hash()
	}
	return it.bytesH + assetFPSep + baseSettings
}

// assetUpdatePlan is how an update changes the assets of a package.
type assetUpdatePlan struct {
	items     []*assetUpdateItem
	conflicts []UpdateConflict
}

// item returns the plan item for rel, or nil.
func (p *assetUpdatePlan) item(rel string) *assetUpdateItem {
	if p == nil {
		return nil
	}
	for _, it := range p.items {
		if it.rel == rel {
			return it
		}
	}
	return nil
}

// plannedAssetLists is the lists an update sets on an asset: the ids of the
// ones that exist already, and the kind of each one the update creates.
type plannedAssetLists struct {
	ids     map[uuid.UUID]bool
	created []bool
}

// plannedLists maps each asset this install owns whose lists the update
// replaces to its new lists. A name that does not resolve is left out; the
// list checks refuse it before any write.
func (s *ContentRegistry) plannedLists(
	ctx context.Context, p *assetUpdatePlan, resolutions map[string]string,
) map[uuid.UUID]*plannedAssetLists {
	out := map[uuid.UUID]*plannedAssetLists{}
	if p == nil {
		return out
	}
	for _, it := range p.items {
		if it.assetID == nil || !it.applySettings(resolutions) {
			continue
		}
		pl := &plannedAssetLists{ids: map[uuid.UUID]bool{}}
		for _, n := range it.decl.settings.allowDeny {
			if ref, ok := it.decl.scope.ref(n); ok && ref.planned {
				pl.created = append(pl.created, ref.allowed)
				continue
			}
			l, err := s.resolveAssetList(ctx, it.rel, n, it.decl.scope)
			if err != nil {
				continue
			}
			if id, ierr := l.ID.Get(); ierr == nil {
				pl.ids[id] = true
			}
		}
		out[*it.assetID] = pl
	}
	return out
}

// planAssetUpdate reads, without writing, what the update does with each asset
// the new version ships. An asset shared with another installed package that
// would change, or one at a path no installed package owns, fails the update.
// Settings changed both upstream and locally raise a conflict, and so do new
// settings that take lists off the asset, see assetFilterLoss. sc resolves the
// list names of the package.
func (s *ContentRegistry) planAssetUpdate(
	ctx context.Context,
	ipID *uuid.UUID,
	pkg *registry.VerifiedPackage,
	vars map[string]string,
	resources []*database.InstalledResource,
	sc *listScope,
) (*assetUpdatePlan, error) {
	decls, err := packageAssetDecls(pkg, vars)
	if err != nil {
		return nil, s.assetDeclsErr(err)
	}
	scopeDecls(decls, sc)
	owned := map[uuid.UUID]*database.InstalledResource{}
	for _, r := range resources {
		if r.ResourceType == "asset" && r.ResourceID != nil {
			owned[*r.ResourceID] = r
		}
	}
	plan := &assetUpdatePlan{}
	for _, rel := range assetFilePaths(pkg) {
		if err := checkAssetRel(rel); err != nil {
			return nil, err
		}
		body := pkg.Files["assets/"+rel]
		decl := assetDeclFor(decls, rel)
		it := &assetUpdateItem{rel: rel, body: body, decl: decl, bytesH: contentHash(string(body))}
		it.fp = assetFingerprint(it.bytesH, decl.settings)
		plan.items = append(plan.items, it)
		existing, gerr := s.Asset.AssetRepository.GetGlobalByPath(ctx, rel)
		if gerr != nil {
			return nil, errs.Wrap(gerr)
		}
		if existing == nil || !existing.ID.IsSpecified() || existing.ID.IsNull() {
			continue
		}
		aid := existing.ID.MustGet()
		it.assetID = &aid
		shared, cerr := s.assetOwnedByOtherInstall(ctx, &aid, ipID)
		if cerr != nil {
			return nil, errs.Wrap(cerr)
		}
		r, isOwned := owned[aid]
		if !isOwned {
			if !shared {
				return nil, assetConflictErr([]string{rel})
			}
			if berr := s.checkSharedAssetBytes(ctx, &aid, ipID, rel, body); berr != nil {
				return nil, berr
			}
			if serr := s.checkSharedAssetSettings(ctx, &aid, ipID, rel, decl); serr != nil {
				return nil, serr
			}
			continue
		}
		it.res = r
		if r.InstalledContentHash == it.fp {
			continue
		}
		// another package uses the same asset, so changing it would change that
		// package's content too.
		if shared {
			return nil, s.sharedAssetChangeErr(ctx, &aid, ipID, rel)
		}
		baseBytes, baseSettings := splitAssetFingerprint(r.InstalledContentHash)
		it.writeBytes = it.bytesH != baseBytes
		it.settingsChanged = decl.settings.hash() != baseSettings
		if !it.settingsChanged {
			continue
		}
		cur, _, serr := s.storedAssetSettings(ctx, &aid)
		if serr != nil {
			return nil, errs.Wrap(fmt.Errorf("read installed asset %q: %w", rel, serr))
		}
		it.localChanged = cur.hash() != baseSettings
		loss, lerr := s.assetFilterLoss(ctx, &aid, decl, it.localChanged)
		if lerr != nil {
			return nil, lerr
		}
		it.filterLoss = loss
		if it.needsChoice() {
			plan.conflicts = append(plan.conflicts, UpdateConflict{
				ResourceType: "asset", SourceName: rel, CurrentName: optStr(existing.Name),
				Note: assetOverwriteNote, FilterLoss: loss,
			})
		}
	}
	return plan, nil
}

// assetFilterLoss works out the lists an asset loses when decl is written onto
// it. It returns nil unless that needs a choice: the asset would have no lists
// left, would lose its last allow list, or, when the operator changed the
// asset locally, would lose any list it has now, since the operator may have
// added it. A list that does not resolve yet counts as neither kind; the list
// checks refuse it before any write.
func (s *ContentRegistry) assetFilterLoss(
	ctx context.Context, assetID *uuid.UUID, decl *assetDecl, localChanged bool,
) (*AssetFilterLoss, error) {
	current, err := s.Asset.AssetRepository.GetAllowDenyByAssetID(ctx, assetID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if len(current) == 0 {
		return nil, nil
	}
	next := map[string]bool{}
	for _, n := range decl.settings.allowDeny {
		next[n] = true
	}
	loss := &AssetFilterLoss{RemovedLists: []string{}}
	hadAllow := false
	for _, l := range current {
		if allowed, _ := l.Allowed.Get(); allowed {
			hadAllow = true
		}
		if !next[s.packageListName(ctx, l)] {
			loss.RemovedLists = append(loss.RemovedLists, optStr(l.Name))
		}
	}
	keepsAllow := false
	for _, n := range decl.settings.allowDeny {
		if allowed, kerr := s.assetListKind(ctx, decl.path, n, decl.scope); kerr == nil && allowed {
			keepsAllow = true
		}
	}
	loss.Unfiltered = len(decl.settings.allowDeny) == 0
	loss.OpenToEveryone = hadAllow && !keepsAllow
	if loss.Unfiltered || loss.OpenToEveryone || (localChanged && len(loss.RemovedLists) > 0) {
		return loss, nil
	}
	return nil, nil
}

// checkAssetUpdate checks, before any write, the list names of every asset the
// update creates or writes new settings onto, and records the paths the new
// version writes in seen so the packages the update adds are compared with
// them.
func (s *ContentRegistry) checkAssetUpdate(
	ctx context.Context,
	plan *assetUpdatePlan,
	resolutions map[string]string,
	seen map[string]assetSeen,
	label string,
) error {
	for _, it := range plan.items {
		seen[it.rel] = assetSeen{
			bytesHash: it.bytesH, settingsHash: it.decl.settings.hash(), label: label, planned: it,
		}
		if it.assetID != nil && !it.applySettings(resolutions) {
			continue
		}
		if err := s.checkAssetLists(ctx, it.rel, it.decl.settings.allowDeny, it.decl.scope); err != nil {
			return err
		}
	}
	return nil
}

// applyAssetSettings writes what planAssetUpdate decided onto the assets this
// install owns, once every list of the update exists: first the new settings
// and lists, then new bytes, so new bytes are never served with the lists of
// before. It then links the lists of the assets the update created and writes
// their files. Each write pushes its reverse onto undo.
func (s *ContentRegistry) applyAssetSettings(
	ctx context.Context,
	session *model.Session,
	plan *assetUpdatePlan,
	resolutions map[string]string,
	result *InstallResult,
	undo *updateUndo,
) error {
	for _, it := range plan.items {
		if it.res == nil {
			continue
		}
		if it.applySettings(resolutions) {
			if err := s.writeAssetSettings(ctx, it, undo); err != nil {
				return err
			}
			if !containsString(result.Assets, it.rel) {
				result.Assets = append(result.Assets, it.rel)
			}
		}
		if it.writeBytes {
			rel, b := it.rel, it.body
			// keep the bytes on disk so a failed update can write them back.
			old, rerr := s.readAssetFile(rel)
			if rerr != nil {
				return errs.Wrap(rerr)
			}
			if werr := s.writeAssetFile(rel, b, true); werr != nil {
				return errs.Wrap(werr)
			}
			undo.pushErr(func() error { return s.writeAssetFile(rel, old, true) })
			if !containsString(result.Assets, rel) {
				result.Assets = append(result.Assets, rel)
			}
		}
	}
	return s.linkPendingAssets(ctx, session, result, undo)
}

// writeAssetSettings writes the new settings of it onto the asset and pushes
// the write back of the old ones onto undo.
func (s *ContentRegistry) writeAssetSettings(ctx context.Context, it *assetUpdateItem, undo *updateUndo) error {
	repo := s.Asset.AssetRepository
	id := it.assetID
	old, _, err := s.storedAssetSettings(ctx, id)
	if err != nil {
		return errs.Wrap(err)
	}
	oldLists, err := repo.GetAllowDenyByAssetID(ctx, id)
	if err != nil {
		return errs.Wrap(err)
	}
	oldIDs := make([]*uuid.UUID, 0, len(oldLists))
	for _, l := range oldLists {
		lid := l.ID.MustGet()
		oldIDs = append(oldIDs, &lid)
	}
	undo.pushErr(func() error {
		if err := repo.UpdateByID(ctx, id, assetContentTypeModel(old.contentType)); err != nil {
			return err
		}
		if err := repo.ReplaceHeaders(ctx, id, old.headerModels()); err != nil {
			return err
		}
		return repo.ReplaceAllowDeny(ctx, id, oldIDs)
	})
	if err := repo.UpdateByID(ctx, id, assetContentTypeModel(it.decl.settings.contentType)); err != nil {
		return errs.Wrap(err)
	}
	if err := repo.ReplaceHeaders(ctx, id, it.decl.settings.headerModels()); err != nil {
		return errs.Wrap(err)
	}
	return s.linkAssetLists(ctx, id, it.rel, it.decl.settings.allowDeny, it.decl.scope)
}

// assetContentTypeModel is an asset model that only sets the content type.
func assetContentTypeModel(contentType string) *model.Asset {
	m := &model.Asset{}
	m.ContentType = nullable.NewNullableWithValue(*vo.NewOptionalString255Must(contentType))
	return m
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// checkListPickUse refuses an asset allowDeny entry list that mixes an
// installer list pick with list names, uses more than one list pick, or names
// a whole @@NAME@@ variable that is not a list pick, whose value could be
// anything or empty. An asset uses either names of lists or exactly one pick.
// It reports whether the asset uses a pick.
func checkListPickUse(pkg *registry.VerifiedPackage, entries []string) (bool, error) {
	picks := map[string]bool{}
	if pkg.Manifest != nil {
		for _, v := range pkg.Manifest.Variables {
			if v.Pick == pickAllowDeny {
				picks["@@"+v.Name+"@@"] = true
			}
		}
	}
	nPicks, nNames := 0, 0
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		switch {
		case picks[e]:
			nPicks++
		case len(e) > 4 && strings.HasPrefix(e, "@@") && strings.HasSuffix(e, "@@"):
			return false, fmt.Errorf("%s is not a variable that lets the installer pick an allow/deny list", e)
		default:
			nNames++
		}
	}
	if nPicks > 1 {
		return false, errors.New("an asset can use only one list the installer picks")
	}
	if nPicks == 1 && nNames > 0 {
		return false, errors.New("an asset uses either list names or one list the installer picks, not both")
	}
	return nPicks == 1, nil
}
