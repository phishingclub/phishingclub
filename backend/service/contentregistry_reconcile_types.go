package service

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// capRunes shortens a string to at most max runes, leaving room to append a
// suffix without exceeding the name length limit.
func capRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// freeName returns base, or base with a numeric suffix, that no entity of the
// type currently uses, global or company, so a detached local copy can be renamed
// without colliding.
func (s *ContentRegistry) freeName(ctx context.Context, resourceType, base string) string {
	cand := base
	for i := 1; i < 1000; i++ {
		if !s.nameTaken(ctx, resourceType, cand) {
			return cand
		}
		cand = fmt.Sprintf("%s (%d)", base, i)
	}
	return fmt.Sprintf("%s (%s)", base, uuid.New().String()[:8])
}

// nameTaken reports whether any entity of the type uses the name. A global
// entity must not share its name with any row, company rows included, so pages
// and emails are checked across all rows. A failed check and any other type
// count as taken.
func (s *ContentRegistry) nameTaken(ctx context.Context, resourceType, name string) bool {
	var db *gorm.DB
	var table string
	switch resourceType {
	case "page":
		db, table = s.PageRepository.DB, database.PAGE_TABLE
	case "email":
		db, table = s.EmailRepository.DB, database.EMAIL_TABLE
	default:
		return true
	}
	free, err := repository.CheckNameIsUnique(ctx, db.WithContext(ctx), table, name, nil, nil)
	return err != nil || !free
}

// The build*Reconcile methods each turn one content type's package declarations
// and installed resources into the generic reconcile inputs, for the types whose
// declarations do not fit the shared path of their entitySpec. Every item's
// upstream fingerprint is computed the same way its stored fingerprint is read
// back, so an unchanged resource reads as unchanged.

func (s *ContentRegistry) buildPageReconcile(
	ctx context.Context,
	session *model.Session,
	pkg *registry.VerifiedPackage,
	docs []contentDoc,
	effective map[string]string,
	resources []*database.InstalledResource,
	undo *updateUndo,
) ([]reconcileItem, map[string]reconcileCurrent) {
	var items []reconcileItem
	for _, doc := range docs {
		for _, p := range doc.dy.Pages {
			name := strings.TrimSpace(p.Name)
			body, ok := pkg.Files[doc.prefix+p.File]
			fp := ""
			var create func() (*uuid.UUID, error)
			var update func(id *uuid.UUID) error
			var check error
			if ok {
				content := substitute(string(body), effective)
				fp = upstreamPageFP(name, content)
				if _, cerr := vo.NewOptionalString1MB(content); cerr != nil {
					check = cerr
				} else if s.Page != nil && s.Page.TemplateService != nil {
					check = s.Page.TemplateService.ValidatePageTemplate(content)
				}
				create = func() (*uuid.UUID, error) { return s.createPage(ctx, session, name, content) }
				update = func(id *uuid.UUID) error {
					pm := &model.Page{}
					c, cerr := vo.NewOptionalString1MB(content)
					if cerr != nil {
						return errs.NewValidationError(cerr)
					}
					pm.Content = nn(*c)
					return s.Page.UpdateByID(ctx, session, id, pm)
				}
			}
			it := simpleItem("page", name, ok, fp, create, update, func(id *uuid.UUID) string {
				return s.storedPageHash(ctx, id)
			})
			it.preflight = checkBuild("page", name, check)
			items = append(items, it)
		}
	}
	return items, pageSpec.currents(s, ctx, session, resources, undo)
}

func (s *ContentRegistry) buildEmailReconcile(
	ctx context.Context,
	session *model.Session,
	ipID *uuid.UUID,
	pkg *registry.VerifiedPackage,
	docs []contentDoc,
	effective map[string]string,
	resources []*database.InstalledResource,
	boundVars string,
	result *InstallResult,
	undo *updateUndo,
) ([]reconcileItem, map[string]reconcileCurrent) {
	// attachments this install already tracks, by declared name, so an email the
	// update creates links them instead of creating a second copy of the file.
	trackedAttachments := map[string]*database.InstalledResource{}
	for _, r := range resources {
		if r.ResourceType == "attachment" {
			trackedAttachments[strings.TrimSpace(r.SourceName)] = r
		}
	}
	var items []reconcileItem
	// what each item declares, by item index, for the plan check of a new email.
	type emailDecl struct {
		ec emailContent
	}
	var decls []emailDecl
	for _, doc := range docs {
		for _, e := range doc.dy.Emails {
			name := strings.TrimSpace(e.Name)
			body, ok := pkg.Files[doc.prefix+e.File]
			ec := emailContent{
				content:      substitute(string(body), effective),
				subject:      substitute(e.Subject, effective),
				from:         substitute(e.From, effective),
				envelopeFrom: substitute(e.EnvelopeFrom, effective),
			}
			decls = append(decls, emailDecl{ec})
			fp := ""
			var create func() (*uuid.UUID, error)
			var update func(id *uuid.UUID) error
			var check error
			if ok {
				fp = s.upstreamEmailFP(name, ec)
				if _, nerr := vo.NewString64(name); nerr != nil {
					check = nerr
				} else if _, cerr := vo.NewOptionalString1MB(ec.content); cerr != nil {
					check = cerr
				} else if s.Email != nil && s.Email.TemplateService != nil {
					check = s.Email.TemplateService.ValidateEmailTemplate(ec.content)
				}
				prefix, atts := doc.prefix, e.Attachments
				create = func() (*uuid.UUID, error) {
					id, cerr := s.createEmail(ctx, session, name, ec.content, ec.subject, ec.from, ec.envelopeFrom)
					if cerr != nil {
						return nil, cerr
					}
					aerr := s.attachEmailAttachments(
						ctx, session, ipID, pkg, prefix, id, atts, effective,
						boundVars, trackedAttachments, result, undo,
					)
					if aerr != nil {
						s.cleanupUntracked(ctx, session, "email", id)
						return nil, aerr
					}
					return id, nil
				}
				update = func(id *uuid.UUID) error {
					em := &model.Email{}
					n, nerr := vo.NewString64(name)
					if nerr != nil {
						return errs.NewValidationError(nerr)
					}
					em.Name = nn(*n)
					c, cerr := vo.NewOptionalString1MB(ec.content)
					if cerr != nil {
						return errs.NewValidationError(cerr)
					}
					em.Content = nn(*c)
					setEmailMeta(em, ec)
					em.AddTrackingPixel = nn(true)
					return s.Email.UpdateByID(ctx, session, id, em)
				}
			}
			it := simpleItem("email", name, ok, fp, create, update, func(id *uuid.UUID) string {
				return s.storedHash(ctx, "email", id)
			})
			it.preflight = checkBuild("email", name, check)
			items = append(items, it)
		}
	}
	currents := emailSpec.currents(s, ctx, session, resources, undo)
	// an email new in this version is created whole, so the full model must pass
	// the Email service validation. Check it in the plan phase so an invalid email
	// fails the update before anything is written.
	for i := range items {
		if _, matched := currents[items[i].match]; matched || !items[i].hasContent {
			continue
		}
		name, decl, base := items[i].match, decls[i], items[i].preflight
		items[i].preflight = func(declared declaredNames) error {
			if base != nil {
				if err := base(declared); err != nil {
					return err
				}
			}
			m, berr := buildEmailModel(name, decl.ec)
			if berr == nil {
				berr = m.Validate()
			}
			if berr != nil {
				return checkBuild("email", name, berr)(declared)
			}
			return nil
		}
	}
	return items, currents
}

// detachNamed renames the local copy to newName through the supplied rename
// closure and pushes the rename back onto undo, for the "both" resolution on a
// named entity.
func detachNamed(undo *updateUndo, oldName string, newName func() string, rename func(name string) error) func() error {
	return func() error {
		if err := rename(newName()); err != nil {
			return err
		}
		undo.pushErr(func() error { return rename(oldName) })
		return nil
	}
}

func (s *ContentRegistry) buildReportTemplateReconcile(
	ctx context.Context,
	session *model.Session,
	pkg *registry.VerifiedPackage,
	docs []contentDoc,
	effective map[string]string,
	resources []*database.InstalledResource,
) ([]reconcileItem, map[string]reconcileCurrent) {
	var items []reconcileItem
	declErr := s.reportTemplateDeclsValid([]*registry.VerifiedPackage{pkg})
	for _, doc := range docs {
		for _, rt := range doc.dy.ReportTemplates {
			name := strings.TrimSpace(rt.Name)
			body, ok := pkg.Files[doc.prefix+rt.File]
			fp := ""
			var create func() (*uuid.UUID, error)
			var update func(id *uuid.UUID) error
			var check error
			// replaced is the content create overwrote in the global template, kept so
			// provenance and undo can write it back.
			var replaced *string
			if ok {
				content := substitute(string(body), effective)
				training := rt.IsTraining
				if m, berr := buildReportTemplateModel(content, training); berr == nil {
					fp = reportTemplateFingerprint(m)
				} else {
					check = berr
				}
				// applyUpdate has the overwrite of an existing global template confirmed
				// before any write, so create takes over the slot of its kind.
				create = func() (*uuid.UUID, error) {
					id, prev, cerr := s.installReportTemplate(ctx, session, content, training)
					replaced = prev
					return id, cerr
				}
				update = func(id *uuid.UUID) error {
					m, berr := buildReportTemplateModel(content, training)
					if berr != nil {
						return berr
					}
					if uerr := s.ReportTemplate.UpdateByID(ctx, session, id, m); uerr != nil {
						if isUniqueViolation(uerr) {
							// the new version moved the template to a kind whose slot is taken.
							return reportTemplateSlotErr(training)
						}
						return uerr
					}
					return nil
				}
			}
			it := simpleItem("reporttemplate", name, ok, fp, create, update, func(id *uuid.UUID) string {
				return s.storedReportTemplateHash(ctx, id)
			})
			it.replaced = func() *string { return replaced }
			buildCheck := checkBuild("report template", name, check)
			it.preflight = func(d declaredNames) error {
				if declErr != nil {
					return declErr
				}
				if buildCheck != nil {
					return buildCheck(d)
				}
				return nil
			}
			items = append(items, it)
		}
	}
	// report templates carry no name and the "both" resolution is refused for them
	// (noBothTypes), so there is nothing to rename (detach nil). Removing one the
	// install overwrote writes its previous content back instead of deleting it.
	load := func(r *database.InstalledResource) (*loadedEntity, error) {
		id := r.ResourceID
		cur, gerr := s.ReportTemplate.ReportTemplateRepository.GetByID(ctx, id, &repository.ReportTemplateOption{})
		if gerr != nil {
			return nil, gerr
		}
		snap := *cur
		replacedContent := r.ReplacedContent
		return &loadedEntity{
			fp: reportTemplateFingerprint(cur),
			deleteEntity: func() error {
				return s.releaseResourceEntity(ctx, session, "reporttemplate", id, replacedContent)
			},
			restore: func() error { m := snap; return s.ReportTemplate.UpdateByID(ctx, session, id, &m) },
		}, nil
	}
	currents := s.collectCurrents(ctx, resources, "reporttemplate", false, load)
	return items, currents
}

// buildDomainReconcile reconciles domains. A domain update does not change its
// name, so the "both" resolution is not offered (detach nil, and
// validateResolutions rejects it).
func (s *ContentRegistry) buildDomainReconcile(
	ctx context.Context,
	session *model.Session,
	pkg *registry.VerifiedPackage,
	docs []contentDoc,
	effective map[string]string,
	resources []*database.InstalledResource,
) ([]reconcileItem, map[string]reconcileCurrent) {
	return s.specReconcile(ctx, session, "domain", pkg, docs, effective, resources, nil)
}

// installedSMTPHeaders returns a lookup of the headers the installed version of a
// package declared for an smtp configuration, by source name, with variables
// substituted. The installed version is fetched once, on first use. The lookup
// reports false when that version or the configuration cannot be read, or when
// the fetched version does not carry the content hash recorded at install.
func installedSMTPHeaders(
	ip *database.InstalledPackage,
	fetch func(id string) (*registry.VerifiedPackage, error),
	effective func(old *registry.VerifiedPackage) map[string]string,
) func(source string) ([]*model.SMTPHeader, bool) {
	var (
		loaded bool
		byName map[string][]*model.SMTPHeader
	)
	return func(source string) ([]*model.SMTPHeader, bool) {
		if !loaded {
			loaded = true
			if fetch == nil || ip == nil || ip.Version == "" {
				return nil, false
			}
			old, err := fetch(ip.PackageID + "@" + ip.Version)
			if err != nil || old == nil {
				return nil, false
			}
			if ip.ContentHash == "" ||
				!strings.EqualFold(strings.TrimSpace(old.Manifest.ContentHash), strings.TrimSpace(ip.ContentHash)) {
				return nil, false
			}
			docs, err := contentDocs(old)
			if err != nil {
				return nil, false
			}
			byName = map[string][]*model.SMTPHeader{}
			vars := effective(old)
			for _, doc := range docs {
				for _, sm := range doc.dy.SMTPConfigurations {
					hs, herr := smtpHeaderModels(sm.Headers, vars)
					if herr != nil {
						continue
					}
					byName[strings.TrimSpace(sm.Name)] = hs
				}
			}
		}
		hs, ok := byName[strings.TrimSpace(source)]
		return hs, ok
	}
}

// smtpHeaderSet returns the headers as sorted key and value pairs, for an order
// free compare.
func smtpHeaderSet(headers []*model.SMTPHeader) []string {
	out := make([]string, 0, len(headers))
	for _, h := range headers {
		if h == nil {
			continue
		}
		out = append(out, optStr(h.Key)+"\x01"+optStr(h.Value))
	}
	sort.Strings(out)
	return out
}

func sameSMTPHeaders(a, b []*model.SMTPHeader) bool {
	return slices.Equal(smtpHeaderSet(a), smtpHeaderSet(b))
}

// buildSMTPReconcile reconciles smtp configurations together with their custom
// headers. The items go through smtpSpec; the installed entities load here,
// since their drift read and restore cover the headers too.
func (s *ContentRegistry) buildSMTPReconcile(
	ctx context.Context,
	session *model.Session,
	docs []contentDoc,
	effective map[string]string,
	resources []*database.InstalledResource,
	undo *updateUndo,
	installedHeaders func(source string) ([]*model.SMTPHeader, bool),
) ([]reconcileItem, map[string]reconcileCurrent) {
	items := smtpSpec.items(s, ctx, session, nil, docs, effective)
	load := func(r *database.InstalledResource) (*loadedEntity, error) {
		id := r.ResourceID
		cur, gerr := s.SMTPConfigurationRepository.GetByID(
			ctx, id, &repository.SMTPConfigurationOption{WithHeaders: true},
		)
		if gerr != nil {
			return nil, gerr
		}
		name := optStr(cur.Name)
		fp := smtpFingerprint(cur)
		// a baseline recorded before headers were part of the fingerprint covers the
		// fields alone, so it says nothing about the headers. When the fields match it,
		// the headers count as unedited only if they equal what the installed version
		// declared. When that version cannot be read, or they differ, fp stays the
		// full fingerprint, which differs from the baseline, so the update raises a
		// conflict instead of replacing the headers.
		if len(cur.Headers) > 0 && r.InstalledContentHash == smtpFieldsFingerprint(cur) && installedHeaders != nil {
			if declared, ok := installedHeaders(r.SourceName); ok && sameSMTPHeaders(declared, cur.Headers) {
				fp = r.InstalledContentHash
			}
		}
		oldHeaders := cur.Headers
		snap := *cur
		snap.Headers = nil
		// SMTPConfiguration.UpdateByID validates the incoming model, so a rename sends
		// the current (fully populated) entity with a new name.
		rename := func(nm string) error {
			m := snap
			if nv, e := vo.NewString127(nm); e == nil {
				m.Name = nn(*nv)
			}
			return s.SMTPConfiguration.UpdateByID(ctx, session, id, &m)
		}
		newName := func() string { return capRunes(name, 40) + " (local " + uuid.New().String()[:8] + ")" }
		return &loadedEntity{
			fp:           fp,
			name:         name,
			detach:       detachNamed(undo, name, newName, rename),
			deleteEntity: func() error { return s.SMTPConfiguration.DeleteByID(ctx, session, id) },
			restore: func() error {
				m := snap
				if err := s.SMTPConfiguration.UpdateByID(ctx, session, id, &m); err != nil {
					return err
				}
				return s.setSMTPHeaders(ctx, session, id, oldHeaders)
			},
		}, nil
	}
	currents := s.collectCurrents(ctx, resources, "smtp", false, load)
	return items, currents
}

func (s *ContentRegistry) buildAPISenderReconcile(
	ctx context.Context,
	session *model.Session,
	docs []contentDoc,
	effective map[string]string,
	resources []*database.InstalledResource,
	undo *updateUndo,
	binding *oauthBinding,
) ([]reconcileItem, map[string]reconcileCurrent) {
	var items []reconcileItem
	for _, doc := range docs {
		for _, as := range doc.dy.APISenders {
			name := strings.TrimSpace(as.Name)
			oauthRef := binding.oauthRef(as.OAuthProvider, effective)
			buildWith := func(oauthID *uuid.UUID) (*model.APISender, error) {
				return apiSenderModel(as, effective, oauthID)
			}
			lookupOAuth := func() (packageOAuthProvider, error) {
				p, err := s.lookupPackageOAuthProvider(ctx, binding, as.OAuthProvider, effective)
				if err == nil && p.ref != "" && p.id == nil {
					p.id = s.trackedOAuthProvider(ctx, resources, p.ref)
				}
				return p, err
			}
			resolveOAuth := func() (*uuid.UUID, error) {
				p, err := lookupOAuth()
				if err != nil {
					return nil, err
				}
				return p.bind(as.Name)
			}
			build := func() (*model.APISender, error) {
				oauthID, e := resolveOAuth()
				if e != nil {
					return nil, e
				}
				return buildWith(oauthID)
			}
			fp := ""
			if m, berr := build(); berr == nil {
				fp = apiSenderFingerprint(m)
			}
			create := func() (*uuid.UUID, error) {
				m, berr := build()
				if berr != nil {
					return nil, berr
				}
				return s.APISender.Create(ctx, session, m)
			}
			update := func(id *uuid.UUID) error {
				m, berr := build()
				if berr != nil {
					return berr
				}
				return s.APISender.UpdateByID(ctx, session, id, m)
			}
			it := simpleItem("apisender", name, true, fp, create, update, func(id *uuid.UUID) string {
				return s.storedHash(ctx, "apisender", id)
			})
			buildUnbound := func() (*model.APISender, error) { return buildWith(nil) }
			it.rebindReason, it.keptNote, it.keepOnly = s.apiSenderRebindReason(
				ctx, resources, name, oauthRef, lookupOAuth, buildUnbound,
			)
			// the provider may be one this version adds, so a name it declares counts as
			// present; the rest of the model is checked with no provider bound.
			it.preflight = func(declared declaredNames) error {
				if oauthRef != "" && !declared.has("oauthprovider", oauthRef) {
					if _, e := resolveOAuth(); e != nil {
						return e
					}
				}
				if _, berr := buildWith(nil); berr != nil {
					return checkBuild("api sender", name, berr)(declared)
				}
				return nil
			}
			items = append(items, it)
		}
	}
	return items, apiSenderSpec.currents(s, ctx, session, resources, undo)
}

// trackedOAuthProvider returns the provider this install tracks under the source
// name ref, or nil. It finds the package's own provider after its name changed,
// which a lookup by name misses.
func (s *ContentRegistry) trackedOAuthProvider(
	ctx context.Context, resources []*database.InstalledResource, ref string,
) *uuid.UUID {
	r := trackedResource(resources, "oauthprovider", ref)
	if r == nil {
		return nil
	}
	if _, err := s.OAuthProviderRepository.GetByID(ctx, *r.ResourceID); err != nil {
		return nil
	}
	return r.ResourceID
}

// apiSenderRebindReason returns a sentence for the conflict list, and the note
// for keeping the sender, when the update would point an installed API sender at
// another OAuth provider than it uses now, or would change where a sender bound to
// a provider sends its requests, which can carry the OAuth token. It returns ""
// when neither happens. A provider the new version has yet to create counts as a
// change. keepOnly is set when the sender may not bind the provider, so keeping
// it is the only choice that applies.
func (s *ContentRegistry) apiSenderRebindReason(
	ctx context.Context,
	resources []*database.InstalledResource,
	name, oauthRef string,
	lookup func() (packageOAuthProvider, error),
	next func() (*model.APISender, error),
) (reason, keptNote string, keepOnly bool) {
	row := trackedResource(resources, "apisender", name)
	if row == nil {
		return "", "", false
	}
	// a sender that is gone or unreadable is handled by the currents load.
	cur, err := s.APISenderRepository.GetByID(ctx, row.ResourceID, &repository.APISenderOption{})
	if err != nil {
		return "", "", false
	}
	var curID *uuid.UUID
	if cur.OAuthProviderID.IsSpecified() && !cur.OAuthProviderID.IsNull() {
		v := cur.OAuthProviderID.MustGet()
		curID = &v
	}
	var reasons []string
	note := keptCurrentRequest
	if oauthRef != "" {
		p, lerr := lookup()
		if lerr == nil && p.foreign {
			return fmt.Sprintf(
				"The OAuth provider %q was not installed from this namespace on this Bait Box, so only Keep mine can be applied.",
				oauthRef,
			), keptCurrentOAuth, true
		}
		if lerr != nil || p.id == nil || curID == nil || *p.id != *curID {
			from := "no OAuth provider"
			if curID != nil {
				from = "an OAuth provider that no longer exists"
				if p, perr := s.OAuthProviderRepository.GetByID(ctx, *curID); perr == nil {
					from = fmt.Sprintf("the OAuth provider %q", optStr(p.Name))
				}
			}
			reasons = append(reasons, fmt.Sprintf(
				"The new version would switch this API sender from %s to the OAuth provider %q.", from, oauthRef,
			))
			note = keptCurrentOAuth
		}
	}
	if curID != nil || oauthRef != "" {
		if m, berr := next(); berr == nil && apiSenderRequestChanged(cur, m) {
			reasons = append(reasons, fmt.Sprintf(
				"The new version changes where the API sender %q sends requests that can carry the OAuth token.", name,
			))
		}
	}
	if len(reasons) == 0 {
		return "", "", false
	}
	return strings.Join(reasons, " "), note, false
}

// apiSenderRequestChanged reports whether next sends to another URL, or with other
// headers or another body, than cur. Values compare in the form they are stored in.
func apiSenderRequestChanged(cur, next *model.APISender) bool {
	return optStr(cur.RequestURL) != optStr(next.RequestURL) ||
		storedHeaders(cur.RequestHeaders) != storedHeaders(next.RequestHeaders) ||
		optStr(cur.RequestBody) != optStr(next.RequestBody)
}

// storedHeaders is the header text as a stored sender reads it back, with each key
// and value trimmed.
func storedHeaders(n nullable.Nullable[model.APISenderHeaders]) string {
	raw := optHeaders(n)
	h, err := model.NewAPISenderHeader(raw)
	if err != nil {
		return raw
	}
	return h.String()
}

// markOAuthURLChanges makes an update that changes a URL of an authorized OAuth
// provider wait for the operator's choice, since the next authorization would
// send the authorization code and client secret to the new URL.
func (s *ContentRegistry) markOAuthURLChanges(
	ctx context.Context,
	docs []contentDoc,
	effective map[string]string,
	resources []*database.InstalledResource,
	items []reconcileItem,
) {
	type urls struct{ auth, token string }
	declared := map[string]urls{}
	for _, doc := range docs {
		for _, op := range doc.dy.OAuthProviders {
			declared[strings.TrimSpace(op.Name)] = urls{
				strings.TrimSpace(substitute(op.AuthURL, effective)),
				strings.TrimSpace(substitute(op.TokenURL, effective)),
			}
		}
	}
	for i := range items {
		it := &items[i]
		want, ok := declared[it.match]
		if !ok {
			continue
		}
		row := trackedResource(resources, "oauthprovider", it.match)
		if row == nil {
			continue
		}
		// a provider that is gone or unreadable is handled by the currents load.
		cur, err := s.OAuthProviderRepository.GetByID(ctx, *row.ResourceID)
		if err != nil || !oauthProviderAuthorized(cur) {
			continue
		}
		var changed []string
		if optStr(cur.AuthURL) != want.auth {
			changed = append(changed, "authorization URL")
		}
		if optStr(cur.TokenURL) != want.token {
			changed = append(changed, "token URL")
		}
		if len(changed) == 0 {
			continue
		}
		it.rebindReason = fmt.Sprintf("The new version changes the %s of the authorized OAuth provider %q.",
			strings.Join(changed, " and "), optStr(cur.Name))
		it.keptNote = keptCurrentURLs
	}
}

// oauthProviderAuthorized reports whether the provider holds tokens from an
// authorization or an import.
func oauthProviderAuthorized(p *model.OAuthProvider) bool {
	if p.IsAuthorized.IsSpecified() && !p.IsAuthorized.IsNull() && p.IsAuthorized.MustGet() {
		return true
	}
	return optStr(p.AccessToken) != "" || optStr(p.RefreshToken) != ""
}

// campaignTemplateRef is one entity a campaign template declaration names: the
// label used in errors, the provenance resource type, the substituted name and
// the field of the model it fills.
type campaignTemplateRef struct {
	label        string
	resourceType string
	name         string
	set          func(m *model.CampaignTemplate, id *uuid.UUID)
}

// campaignTemplateRefs lists the references a campaign template declaration
// names, with their precedence (a landing page over a landing proxy, an api
// sender over an smtp configuration), so the build and the plan phase check
// resolve exactly the same names.
func campaignTemplateRefs(ct installCampaignTemplateYAML, vars map[string]string) []campaignTemplateRef {
	var out []campaignTemplateRef
	add := func(label, resourceType, raw string, set func(m *model.CampaignTemplate, id *uuid.UUID)) {
		if raw != "" {
			out = append(out, campaignTemplateRef{label, resourceType, substitute(raw, vars), set})
		}
	}
	add("domain", "domain", ct.Domain, func(m *model.CampaignTemplate, id *uuid.UUID) { m.DomainID = nn(*id) })
	add("email", "email", ct.Email, func(m *model.CampaignTemplate, id *uuid.UUID) { m.EmailID = nn(*id) })
	if ct.LandingPage != "" {
		add("landing page", "page", ct.LandingPage, func(m *model.CampaignTemplate, id *uuid.UUID) {
			m.LandingPageID = nn(*id)
		})
	} else {
		add("landing proxy", "proxy", ct.LandingProxy, func(m *model.CampaignTemplate, id *uuid.UUID) {
			m.LandingProxyID = nn(*id)
		})
	}
	add("before landing page", "page", ct.BeforeLandingPage, func(m *model.CampaignTemplate, id *uuid.UUID) {
		m.BeforeLandingPageID = nn(*id)
	})
	add("after landing page", "page", ct.AfterLandingPage, func(m *model.CampaignTemplate, id *uuid.UUID) {
		m.AfterLandingPageID = nn(*id)
	})
	// a campaign template sends through exactly one of an smtp configuration or an
	// api sender; the model rejects both set. api sender wins if a package somehow
	// declares both, matching the authoring UI's single sender slot.
	if ct.APISender != "" {
		add("api sender", "apisender", ct.APISender, func(m *model.CampaignTemplate, id *uuid.UUID) {
			m.APISenderID = nn(*id)
		})
	} else {
		add("smtp configuration", "smtp", ct.SMTPConfiguration, func(m *model.CampaignTemplate, id *uuid.UUID) {
			m.SMTPConfigurationID = nn(*id)
		})
	}
	add("url identifier", "identifier", ct.URLIdentifier, func(m *model.CampaignTemplate, id *uuid.UUID) {
		m.URLIdentifierID = nn(id)
	})
	add("state identifier", "identifier", ct.StateIdentifier, func(m *model.CampaignTemplate, id *uuid.UUID) {
		m.StateIdentifierID = nn(id)
	})
	return out
}

// resolveReferenceID looks up the id of an entity of the provenance resource type
// by name, for the types a campaign template can name.
func (s *ContentRegistry) resolveReferenceID(ctx context.Context, resourceType, name string) (*uuid.UUID, error) {
	if resourceType == "identifier" {
		return s.resolveIdentifierID(ctx, name)
	}
	if sp, ok := resourceSpecs[resourceType]; ok {
		return sp.resolveName(s, ctx, name)
	}
	return nil, fmt.Errorf("unknown reference type %q", resourceType)
}

// checkCampaignTemplateRefs checks, before any write, that every entity a
// campaign template names exists or is declared by the new version.
func (s *ContentRegistry) checkCampaignTemplateRefs(
	ctx context.Context, ct installCampaignTemplateYAML, vars map[string]string, declared declaredNames,
) error {
	if _, err := vo.NewString64(substitute(ct.Name, vars)); err != nil {
		return errs.NewValidationError(
			fmt.Errorf("campaign template %q in the new version is invalid: %w", ct.Name, err),
		)
	}
	for _, r := range campaignTemplateRefs(ct, vars) {
		if declared.has(r.resourceType, r.name) {
			continue
		}
		if _, err := s.resolveReferenceID(ctx, r.resourceType, r.name); err != nil {
			return refResolveErr(r.label, r.name, err)
		}
	}
	return nil
}

// buildCampaignTemplateReconcile reconciles campaign templates as pure wiring:
// the drift fingerprint is forced to the recorded baseline (driftless), so a
// local edit never raises a conflict and the template is simply rewired to the
// new version when its declaration changed, the same way an asset is overwritten.
func (s *ContentRegistry) buildCampaignTemplateReconcile(
	ctx context.Context,
	session *model.Session,
	docs []contentDoc,
	effective map[string]string,
	resources []*database.InstalledResource,
) ([]reconcileItem, map[string]reconcileCurrent) {
	var items []reconcileItem
	for _, doc := range docs {
		for _, ct := range doc.dy.CampaignTemplates {
			name := strings.TrimSpace(ct.Name)
			cc := ct
			fp := campaignTemplateDeclFingerprint(cc, effective)
			create := func() (*uuid.UUID, error) {
				m, berr := s.buildCampaignTemplateModel(ctx, cc, effective)
				if berr != nil {
					return nil, berr
				}
				return s.CampaignTemplate.Create(ctx, session, m)
			}
			update := func(id *uuid.UUID) error {
				m, berr := s.buildCampaignTemplateModel(ctx, cc, effective)
				if berr != nil {
					return berr
				}
				return s.CampaignTemplate.UpdateByID(ctx, session, id, m)
			}
			items = append(items, reconcileItem{
				match:      name,
				hasContent: true,
				upstreamFP: fp,
				create: func() (*uuid.UUID, string, error) {
					id, err := create()
					if err != nil {
						return nil, "", collisionErr("campaign template", name, err)
					}
					return id, campaignTemplateDeclFingerprint(cc, effective), nil
				},
				update: func(id *uuid.UUID) (string, error) {
					if err := update(id); err != nil {
						return "", errs.Wrap(err)
					}
					return campaignTemplateDeclFingerprint(cc, effective), nil
				},
				preflight: func(declared declaredNames) error {
					return s.checkCampaignTemplateRefs(ctx, cc, effective, declared)
				},
			})
		}
	}
	return items, campaignTemplateSpec.currents(s, ctx, session, resources, nil)
}

// reconcileAssets uploads a new asset, overwrites an asset only this install owns
// whose bytes changed, and references an asset another installed package owns
// with the same bytes without changing it. Other bytes at a path another package
// owns fail the update, also when this install owns it too. A global asset the
// user added at a package path is never adopted or overwritten; it fails the
// update with a conflict error. Assets are global shared files, so there is no
// conflict prompt and none are removed.
func (s *ContentRegistry) reconcileAssets(
	ctx context.Context,
	session *model.Session,
	ipID *uuid.UUID,
	pkg *registry.VerifiedPackage,
	resources []*database.InstalledResource,
	boundVars string,
	result *InstallResult,
	undo *updateUndo,
) error {
	assetRes := map[string]*database.InstalledResource{}
	for _, r := range resources {
		if r.ResourceType == "asset" {
			assetRes[r.ResourceID.String()] = r
		}
	}
	for path, b := range pkg.Files {
		if !strings.HasPrefix(path, "assets/") {
			continue
		}
		rel := strings.TrimPrefix(path, "assets/")
		if rel == "" {
			continue
		}
		if perr := checkAssetRel(rel); perr != nil {
			return perr
		}
		newH := contentHash(string(b))
		existing, gerr := s.Asset.AssetRepository.GetGlobalByPath(ctx, rel)
		if gerr != nil {
			return errs.Wrap(gerr)
		}
		if existing != nil && existing.ID.IsSpecified() && !existing.ID.IsNull() {
			aid := existing.ID.MustGet()
			shared, cerr := s.assetOwnedByOtherInstall(ctx, &aid, ipID)
			if cerr != nil {
				return errs.Wrap(cerr)
			}
			if r, owned := assetRes[aid.String()]; owned {
				if r.InstalledContentHash == newH {
					continue
				}
				// another package uses the same file, so changing it would change
				// that package's content too. Refuse the update so the version
				// does not advance on stale bytes.
				if shared {
					return s.sharedAssetChangeErr(ctx, &aid, ipID, rel)
				}
				// keep the bytes on disk so a failed update can write them back.
				old, rerr := s.readAssetFile(rel)
				if rerr != nil {
					return errs.Wrap(rerr)
				}
				if werr := s.writeAssetFile(rel, b, true); werr != nil {
					return errs.Wrap(werr)
				}
				undo.pushErr(func() error { return s.writeAssetFile(rel, old, true) })
				if rerr := s.rebaselineResourceTracked(ctx, r, newH, boundVars, undo); rerr != nil {
					return errs.Wrap(rerr)
				}
				result.Assets = append(result.Assets, rel)
				continue
			}
			if !shared {
				return assetConflictErr([]string{rel})
			}
			if berr := s.checkSharedAssetBytes(ctx, &aid, ipID, rel, b); berr != nil {
				return berr
			}
			// another installed package owns it with the same bytes: reference it so
			// uninstall ref counts it.
			if rerr := s.recordResourceTracked(ctx, ipID, "asset", rel, &aid, newH, boundVars, undo, nil); rerr != nil {
				return errs.Wrap(rerr)
			}
			continue
		}
		id, created, aerr := s.installAsset(ctx, session, ipID, rel, b)
		if aerr != nil {
			return aerr
		}
		if id == nil {
			continue
		}
		if created {
			rid := id
			undo.pushErr(func() error { return s.deleteResourceEntity(ctx, session, "asset", rid) })
			result.Assets = append(result.Assets, rel)
		}
		if rerr := s.recordResourceTracked(ctx, ipID, "asset", rel, id, newH, boundVars, undo, nil); rerr != nil {
			return errs.Wrap(rerr)
		}
	}
	return nil
}

// reconcileIdentifiers adds any identifier the new version declares that is not
// already tracked. Identifiers are global shared values, so none are updated or
// removed.
func (s *ContentRegistry) reconcileIdentifiers(
	ctx context.Context,
	ipID *uuid.UUID,
	docs []contentDoc,
	effective map[string]string,
	resources []*database.InstalledResource,
	boundVars string,
	result *InstallResult,
	undo *updateUndo,
) error {
	owned := map[string]bool{}
	for _, r := range resources {
		if r.ResourceType == "identifier" {
			owned[strings.TrimSpace(r.SourceName)] = true
		}
	}
	for _, doc := range docs {
		for _, idn := range doc.dy.Identifiers {
			name := strings.TrimSpace(idn.Name)
			if owned[name] {
				continue
			}
			owned[name] = true
			id, created, cerr := s.createIdentifier(ctx, idn.Name)
			if cerr != nil {
				return cerr
			}
			if created && id != nil {
				rerr := s.recordResourceTracked(
					ctx, ipID, "identifier", idn.Name, id, contentHash(idn.Name), boundVars, undo, nil,
				)
				if rerr != nil {
					return errs.Wrap(rerr)
				}
				result.Others = append(result.Others, "identifier "+idn.Name)
			}
		}
	}
	return nil
}

// attachmentsChanged reports whether the new version's attachment set (names and
// file bytes) differs from what is installed. Attachments are not reconciled in
// place, so a change here keeps the update marked available rather than advancing
// the version and claiming the package is up to date.
func (s *ContentRegistry) attachmentsChanged(
	docs []contentDoc, pkg *registry.VerifiedPackage, resources []*database.InstalledResource,
) bool {
	installed := map[string]string{}
	for _, r := range resources {
		if r.ResourceType == "attachment" {
			installed[strings.TrimSpace(r.SourceName)] = r.InstalledContentHash
		}
	}
	declared := map[string]string{}
	addDecl := func(a installAttachmentYAML, prefix string) {
		if a.File == "" {
			return
		}
		body, ok := pkg.Files[prefix+a.File]
		if !ok {
			return
		}
		declared[strings.TrimSpace(a.Name)] = contentHash(string(body))
	}
	for _, doc := range docs {
		for _, e := range doc.dy.Emails {
			for _, a := range e.Attachments {
				addDecl(a, doc.prefix)
			}
		}
		for _, a := range doc.dy.Attachments {
			addDecl(a, doc.prefix)
		}
	}
	if len(installed) != len(declared) {
		return true
	}
	for k, v := range declared {
		if installed[k] != v {
			return true
		}
	}
	return false
}
