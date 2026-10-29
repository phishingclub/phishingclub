package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// entityService holds the create, update and delete of a type's entity service,
// so every write goes through the same permission checks and audit events the
// admin UI uses.
type entityService[T any] struct {
	create func(ctx context.Context, session *model.Session, m *T) (*uuid.UUID, error)
	update func(ctx context.Context, session *model.Session, id *uuid.UUID, m *T) error
	delete func(ctx context.Context, session *model.Session, id *uuid.UUID) error
}

// entitySpec describes how the registry consumer reads, writes and tracks one
// installed resource type, so the steps every type shares are written once.
// Assets, attachments and identifiers are global shared files or values with
// rules of their own and have no spec.
type entitySpec[T any] struct {
	resourceType string
	// label names the type in install results and skip notes, planLabel in the
	// errors the update plan reports.
	label     string
	planLabel string

	svc  func(s *ContentRegistry) entityService[T]
	load func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*T, error)
	// fingerprint is nil for a type that is never checked for local edits.
	fingerprint func(m *T) string
	// name is nil for a type whose entities carry no name.
	name func(m *T) string
	// resolve finds an entity by name, for the types a campaign template names.
	resolve func(s *ContentRegistry, ctx context.Context, name string) (*uuid.UUID, error)
	// templates lists the campaign templates using an entity, for the types whose
	// delete closes the campaigns of those templates.
	templates func(
		repo *repository.CampaignTemplate, ctx context.Context, id *uuid.UUID,
		opt *repository.CampaignTemplateOption,
	) ([]*model.CampaignTemplate, error)

	// snapshot is what a failed update writes back. nil keeps a copy of the whole
	// entity.
	snapshot func(cur *T) *T
	// rename returns the model that renames the local copy for the "both"
	// resolution, built from the snapshot. nil when the type cannot keep both.
	rename func(snap *T, name string) (*T, error)
	// localName names the renamed local copy "<name> (local)" with a free numeric
	// suffix instead of a random one.
	localName bool
	// driftless types are rewired by an update and never raise a local edit
	// conflict.
	driftless bool

	// decls lists what one content folder declares of the type, for the install
	// and update paths those types share. nil for a type with its own paths.
	decls func(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		pkg *registry.VerifiedPackage, doc contentDoc, vars map[string]string,
	) []entityDecl[T]
}

// entityDecl is one declaration of a package, with the values substituted.
type entityDecl[T any] struct {
	// name is the declared name as install records it.
	name string
	// missing is set when the declared file is not in the package.
	missing bool
	// skip, when set, is why install skips the declaration. An update ignores it.
	skip string
	// check is a plan error found before the model is built.
	check error
	build func() (*T, error)
	// fingerprint, create and update replace the defaults when set. The defaults
	// build the model and hand it to the spec.
	fingerprint func(m *T) string
	create      func() (*uuid.UUID, error)
	update      func(id *uuid.UUID) error
}

// resourceSpec is an entitySpec without its type parameter, for the steps that
// look a type up by its provenance name.
type resourceSpec interface {
	storedHash(s *ContentRegistry, ctx context.Context, id *uuid.UUID) string
	currentName(s *ContentRegistry, ctx context.Context, id *uuid.UUID) string
	deleteByID(s *ContentRegistry, ctx context.Context, session *model.Session, id *uuid.UUID) error
	resolveName(s *ContentRegistry, ctx context.Context, name string) (*uuid.UUID, error)
	templatesUsing(
		repo *repository.CampaignTemplate, ctx context.Context, id *uuid.UUID,
	) ([]*model.CampaignTemplate, bool, error)
	currents(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		resources []*database.InstalledResource, undo *updateUndo,
	) map[string]reconcileCurrent
	items(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		pkg *registry.VerifiedPackage, docs []contentDoc, vars map[string]string,
	) []reconcileItem
	install(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		pkg *registry.VerifiedPackage, doc contentDoc, vars map[string]string,
		ipID *uuid.UUID, boundVars string, result *InstallResult,
	) error
}

// storedHash reads the entity back and fingerprints it. The entity services
// normalize what they store (vo strings are trimmed, an email gets its tracking
// pixel), so hashing what was submitted would not match a later read and would
// flag an unedited resource as edited. Empty when the entity cannot be read or
// the type has no fingerprint.
func (sp *entitySpec[T]) storedHash(s *ContentRegistry, ctx context.Context, id *uuid.UUID) string {
	if sp.fingerprint == nil {
		return ""
	}
	cur, err := sp.load(s, ctx, id)
	if err != nil {
		return ""
	}
	return sp.fingerprint(cur)
}

// currentName is the entity's name now, empty when it has none or cannot be
// read.
func (sp *entitySpec[T]) currentName(s *ContentRegistry, ctx context.Context, id *uuid.UUID) string {
	if sp.name == nil {
		return ""
	}
	cur, err := sp.load(s, ctx, id)
	if err != nil {
		return ""
	}
	return sp.name(cur)
}

func (sp *entitySpec[T]) deleteByID(
	s *ContentRegistry, ctx context.Context, session *model.Session, id *uuid.UUID,
) error {
	return sp.svc(s).delete(ctx, session, id)
}

func (sp *entitySpec[T]) resolveName(s *ContentRegistry, ctx context.Context, name string) (*uuid.UUID, error) {
	if sp.resolve == nil {
		return nil, fmt.Errorf("unknown reference type %q", sp.resourceType)
	}
	return sp.resolve(s, ctx, name)
}

// templatesUsing reports false when deleting the type closes no campaigns.
func (sp *entitySpec[T]) templatesUsing(
	repo *repository.CampaignTemplate, ctx context.Context, id *uuid.UUID,
) ([]*model.CampaignTemplate, bool, error) {
	if sp.templates == nil {
		return nil, false, nil
	}
	t, err := sp.templates(repo, ctx, id, &repository.CampaignTemplateOption{})
	return t, true, err
}

// currents loads the entities this install owns of the type. Each one carries
// a restore that writes the loaded fields back, for a failed update.
func (sp *entitySpec[T]) currents(
	s *ContentRegistry,
	ctx context.Context,
	session *model.Session,
	resources []*database.InstalledResource,
	undo *updateUndo,
) map[string]reconcileCurrent {
	load := func(r *database.InstalledResource) (*loadedEntity, error) {
		id := r.ResourceID
		cur, err := sp.load(s, ctx, id)
		if err != nil {
			return nil, err
		}
		svc := sp.svc(s)
		snap := cur
		if sp.snapshot != nil {
			snap = sp.snapshot(cur)
		} else {
			c := *cur
			snap = &c
		}
		le := &loadedEntity{
			deleteEntity: func() error { return svc.delete(ctx, session, id) },
			restore:      func() error { m := *snap; return svc.update(ctx, session, id, &m) },
		}
		if sp.fingerprint != nil {
			le.fp = sp.fingerprint(cur)
		}
		if sp.name != nil {
			le.name = sp.name(cur)
		}
		if sp.rename != nil {
			oldName := le.name
			rename := func(nm string) error {
				m, err := sp.rename(snap, nm)
				if err != nil {
					return err
				}
				return svc.update(ctx, session, id, m)
			}
			newName := func() string {
				return capRunes(oldName, 40) + " (local " + uuid.New().String()[:8] + ")"
			}
			if sp.localName {
				newName = func() string {
					return s.freeName(ctx, sp.resourceType, capRunes(oldName, 48)+" (local)")
				}
			}
			le.detach = detachNamed(undo, oldName, newName, rename)
		}
		return le, nil
	}
	return s.collectCurrents(ctx, resources, sp.resourceType, sp.driftless, load)
}

func (sp *entitySpec[T]) createFor(
	s *ContentRegistry, ctx context.Context, session *model.Session, d entityDecl[T],
) func() (*uuid.UUID, error) {
	if d.create != nil {
		return d.create
	}
	return func() (*uuid.UUID, error) {
		m, err := d.build()
		if err != nil {
			return nil, err
		}
		return sp.svc(s).create(ctx, session, m)
	}
}

func (sp *entitySpec[T]) updateFor(
	s *ContentRegistry, ctx context.Context, session *model.Session, d entityDecl[T],
) func(id *uuid.UUID) error {
	if d.update != nil {
		return d.update
	}
	return func(id *uuid.UUID) error {
		m, err := d.build()
		if err != nil {
			return err
		}
		return sp.svc(s).update(ctx, session, id, m)
	}
}

// items turns the declarations of the new version into reconcile items. The
// upstream fingerprint is built from the same model install writes, so an
// unchanged resource reads as unchanged.
func (sp *entitySpec[T]) items(
	s *ContentRegistry,
	ctx context.Context,
	session *model.Session,
	pkg *registry.VerifiedPackage,
	docs []contentDoc,
	vars map[string]string,
) []reconcileItem {
	var items []reconcileItem
	for _, doc := range docs {
		for _, d := range sp.decls(s, ctx, session, pkg, doc, vars) {
			if d.skip != "" {
				// install never created it, so there is nothing to reconcile.
				continue
			}
			name := strings.TrimSpace(d.name)
			fingerprint := sp.fingerprint
			if d.fingerprint != nil {
				fingerprint = d.fingerprint
			}
			fp, check := "", d.check
			var create func() (*uuid.UUID, error)
			var update func(id *uuid.UUID) error
			if !d.missing {
				if m, berr := d.build(); berr != nil {
					check = berr
				} else if check == nil {
					fp = fingerprint(m)
				}
				create = sp.createFor(s, ctx, session, d)
				update = sp.updateFor(s, ctx, session, d)
			}
			it := simpleItem(sp.resourceType, name, !d.missing, fp, create, update, func(id *uuid.UUID) string {
				return sp.storedHash(s, ctx, id)
			})
			it.preflight = checkBuild(sp.planLabel, name, check)
			items = append(items, it)
		}
	}
	return items
}

// install creates the entities one content folder declares of the type and
// records their provenance. An entity whose provenance cannot be recorded is
// deleted again, since rollback only sees recorded entities.
func (sp *entitySpec[T]) install(
	s *ContentRegistry,
	ctx context.Context,
	session *model.Session,
	pkg *registry.VerifiedPackage,
	doc contentDoc,
	vars map[string]string,
	ipID *uuid.UUID,
	boundVars string,
	result *InstallResult,
) error {
	for _, d := range sp.decls(s, ctx, session, pkg, doc, vars) {
		if d.skip != "" {
			result.Skipped = append(result.Skipped, d.skip)
			continue
		}
		if d.missing {
			result.Skipped = append(result.Skipped, sp.label+" "+d.name+": file missing")
			continue
		}
		id, err := sp.createFor(s, ctx, session, d)()
		if err != nil {
			return err
		}
		err = s.recordResource(ctx, ipID, sp.resourceType, d.name, id, sp.storedHash(s, ctx, id), boundVars)
		if err != nil {
			s.cleanupUntracked(ctx, session, sp.resourceType, id)
			return err
		}
		result.Others = append(result.Others, sp.label+" "+d.name)
	}
	return nil
}

// resourceSpecs maps a provenance resource type to its spec.
var resourceSpecs = map[string]resourceSpec{
	"page":             pageSpec,
	"email":            emailSpec,
	"script":           scriptSpec,
	"reporttemplate":   reportTemplateSpec,
	"proxy":            proxySpec,
	"remotebrowser":    remoteBrowserSpec,
	"domain":           domainSpec,
	"allowdeny":        allowDenySpec,
	"smtp":             smtpSpec,
	"webhook":          webhookSpec,
	"oauthprovider":    oauthProviderSpec,
	"apisender":        apiSenderSpec,
	"campaigntemplate": campaignTemplateSpec,
}

// storedHash is the drift fingerprint of an installed entity, empty when the
// type has none or the entity cannot be read.
func (s *ContentRegistry) storedHash(ctx context.Context, resourceType string, id *uuid.UUID) string {
	if sp, ok := resourceSpecs[resourceType]; ok {
		return sp.storedHash(s, ctx, id)
	}
	return ""
}

func (s *ContentRegistry) storedPageHash(ctx context.Context, id *uuid.UUID) string {
	return s.storedHash(ctx, "page", id)
}

func (s *ContentRegistry) storedReportTemplateHash(ctx context.Context, id *uuid.UUID) string {
	return s.storedHash(ctx, "reporttemplate", id)
}

// specReconcile is the reconcile input of a type that goes through its spec.
func (s *ContentRegistry) specReconcile(
	ctx context.Context,
	session *model.Session,
	resourceType string,
	pkg *registry.VerifiedPackage,
	docs []contentDoc,
	effective map[string]string,
	resources []*database.InstalledResource,
	undo *updateUndo,
) ([]reconcileItem, map[string]reconcileCurrent) {
	sp := resourceSpecs[resourceType]
	return sp.items(s, ctx, session, pkg, docs, effective), sp.currents(s, ctx, session, resources, undo)
}

// fileDecl starts the declaration of a type that keeps its content in a file of
// the package, and returns the file content with the values substituted.
func fileDecl[T any](
	pkg *registry.VerifiedPackage, doc contentDoc, name, file string, vars map[string]string,
) (entityDecl[T], string) {
	body, ok := pkg.Files[doc.prefix+file]
	return entityDecl[T]{name: name, missing: !ok}, substitute(string(body), vars)
}

// renameOnly renames through an empty model that carries only the new name,
// left unset when it is not valid.
func renameOnly[T any, N any](newName func(string) (*N, error), set func(m *T, n N)) func(*T, string) (*T, error) {
	return func(_ *T, name string) (*T, error) {
		m := new(T)
		if nv, e := newName(name); e == nil {
			set(m, *nv)
		}
		return m, nil
	}
}

// renameCopy renames through a copy of the snapshot, for an update that needs
// the whole entity.
func renameCopy[T any, N any](newName func(string) (*N, error), set func(m *T, n N)) func(*T, string) (*T, error) {
	return func(snap *T, name string) (*T, error) {
		m := *snap
		if nv, e := newName(name); e == nil {
			set(&m, *nv)
		}
		return &m, nil
	}
}

var pageSpec = &entitySpec[model.Page]{
	resourceType: "page",
	svc: func(s *ContentRegistry) entityService[model.Page] {
		return entityService[model.Page]{s.Page.Create, s.Page.UpdateByID, s.Page.DeleteByID}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.Page, error) {
		return s.PageRepository.GetByID(ctx, id, &repository.PageOption{})
	},
	fingerprint: pageFingerprint,
	name:        func(m *model.Page) string { return optStr(m.Name) },
	resolve:     (*ContentRegistry).resolvePageID,
	templates:   (*repository.CampaignTemplate).GetByPageID,
	snapshot:    func(cur *model.Page) *model.Page { return &model.Page{Name: cur.Name, Content: cur.Content} },
	rename: func(_ *model.Page, name string) (*model.Page, error) {
		nv, err := vo.NewString64(name)
		if err != nil {
			return nil, err
		}
		return &model.Page{Name: nn(*nv)}, nil
	},
	localName: true,
}

var emailSpec = &entitySpec[model.Email]{
	resourceType: "email",
	svc: func(s *ContentRegistry) entityService[model.Email] {
		return entityService[model.Email]{s.Email.Create, s.Email.UpdateByID, s.Email.DeleteByID}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.Email, error) {
		return s.EmailRepository.GetByID(ctx, id, &repository.EmailOption{})
	},
	fingerprint: emailFingerprint,
	name:        func(m *model.Email) string { return optStr(m.Name) },
	resolve:     (*ContentRegistry).resolveEmailID,
	templates:   (*repository.CampaignTemplate).GetByEmailID,
	// the stored content already carries the tracking pixel, so the restore leaves
	// AddTrackingPixel unset and the content is written back as it was.
	snapshot: func(cur *model.Email) *model.Email {
		return &model.Email{
			Name:              cur.Name,
			Content:           cur.Content,
			MailHeaderSubject: cur.MailHeaderSubject,
			MailHeaderFrom:    cur.MailHeaderFrom,
			MailEnvelopeFrom:  cur.MailEnvelopeFrom,
		}
	},
	rename: func(_ *model.Email, name string) (*model.Email, error) {
		nv, err := vo.NewString64(name)
		if err != nil {
			return nil, err
		}
		return &model.Email{Name: nn(*nv)}, nil
	},
	localName: true,
}

var scriptSpec = &entitySpec[model.Script]{
	resourceType: "script",
	label:        "script",
	planLabel:    "script",
	svc: func(s *ContentRegistry) entityService[model.Script] {
		return entityService[model.Script]{s.Script.Create, s.Script.Update, s.Script.DeleteByID}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.Script, error) {
		return s.Script.ScriptRepository.GetByID(ctx, id)
	},
	fingerprint: scriptFingerprint,
	name:        func(m *model.Script) string { return optStr(m.Name) },
	rename:      renameCopy(vo.NewString127, func(m *model.Script, n vo.String127) { m.Name = nn(n) }),
	decls: func(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		pkg *registry.VerifiedPackage, doc contentDoc, vars map[string]string,
	) []entityDecl[model.Script] {
		var out []entityDecl[model.Script]
		for _, sc := range doc.dy.Scripts {
			d, content := fileDecl[model.Script](pkg, doc, sc.Name, sc.File, vars)
			d.build = func() (*model.Script, error) { return buildScriptModel(sc.Name, content) }
			out = append(out, d)
		}
		return out
	},
}

// reportTemplateSpec has no name and no decls: a report template overwrites the
// global template of its kind, so install and update handle it on their own.
var reportTemplateSpec = &entitySpec[model.ReportTemplate]{
	resourceType: "reporttemplate",
	svc: func(s *ContentRegistry) entityService[model.ReportTemplate] {
		return entityService[model.ReportTemplate]{
			s.ReportTemplate.Create, s.ReportTemplate.UpdateByID, s.ReportTemplate.DeleteByID,
		}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.ReportTemplate, error) {
		return s.ReportTemplate.ReportTemplateRepository.GetByID(ctx, id, &repository.ReportTemplateOption{})
	},
	fingerprint: reportTemplateFingerprint,
}

var proxySpec = &entitySpec[model.Proxy]{
	resourceType: "proxy",
	label:        "proxy",
	planLabel:    "proxy",
	svc: func(s *ContentRegistry) entityService[model.Proxy] {
		return entityService[model.Proxy]{s.Proxy.Create, s.Proxy.UpdateByID, s.Proxy.DeleteByID}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.Proxy, error) {
		return s.ProxyRepository.GetByID(ctx, id, &repository.ProxyOption{})
	},
	fingerprint: proxyFingerprint,
	name:        func(m *model.Proxy) string { return optStr(m.Name) },
	resolve:     (*ContentRegistry).resolveProxyID,
	templates:   (*repository.CampaignTemplate).GetByProxyID,
	rename:      renameOnly(vo.NewString64, func(m *model.Proxy, n vo.String64) { m.Name = nn(n) }),
	decls: func(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		pkg *registry.VerifiedPackage, doc contentDoc, vars map[string]string,
	) []entityDecl[model.Proxy] {
		var out []entityDecl[model.Proxy]
		for _, px := range doc.dy.Proxies {
			d, cfg := fileDecl[model.Proxy](pkg, doc, px.Name, px.File, vars)
			desc, startURL := substitute(px.Description, vars), substitute(px.StartURL, vars)
			d.build = func() (*model.Proxy, error) { return buildProxyModel(px.Name, desc, startURL, cfg) }
			out = append(out, d)
		}
		return out
	},
}

var remoteBrowserSpec = &entitySpec[model.RemoteBrowser]{
	resourceType: "remotebrowser",
	label:        "remote browser",
	planLabel:    "remote browser",
	svc: func(s *ContentRegistry) entityService[model.RemoteBrowser] {
		return entityService[model.RemoteBrowser]{
			s.RemoteBrowser.Create, s.RemoteBrowser.UpdateByID, s.RemoteBrowser.DeleteByID,
		}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.RemoteBrowser, error) {
		return s.RemoteBrowser.RemoteBrowserRepository.GetByID(ctx, id, &repository.RemoteBrowserOption{})
	},
	fingerprint: remoteBrowserFingerprint,
	name:        func(m *model.RemoteBrowser) string { return optStr(m.Name) },
	rename:      renameCopy(vo.NewString64, func(m *model.RemoteBrowser, n vo.String64) { m.Name = nn(n) }),
	decls: func(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		pkg *registry.VerifiedPackage, doc contentDoc, vars map[string]string,
	) []entityDecl[model.RemoteBrowser] {
		var out []entityDecl[model.RemoteBrowser]
		for _, rb := range doc.dy.RemoteBrowsers {
			d, scriptBody := fileDecl[model.RemoteBrowser](pkg, doc, rb.Name, rb.File, vars)
			desc := substitute(rb.Description, vars)
			remote, proxy := substitute(rb.Remote, vars), substitute(rb.Proxy, vars)
			d.build = func() (*model.RemoteBrowser, error) {
				return buildRemoteBrowserModel(
					rb.Name, desc, scriptBody, rb.Mode, remote, proxy,
					rb.Headless, rb.Timeout, rb.Lang, rb.ExtraFlags,
				)
			}
			out = append(out, d)
		}
		return out
	},
}

// domainSpec offers no "both" resolution: a domain update does not change its
// name, so there is nothing to rename.
var domainSpec = &entitySpec[model.Domain]{
	resourceType: "domain",
	label:        "domain",
	planLabel:    "domain",
	svc: func(s *ContentRegistry) entityService[model.Domain] {
		return entityService[model.Domain]{s.Domain.Create, s.Domain.UpdateByID, s.Domain.DeleteByID}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.Domain, error) {
		return s.DomainRepository.GetByID(ctx, id, &repository.DomainOption{})
	},
	fingerprint: domainFingerprint,
	name:        func(m *model.Domain) string { return optStr(m.Name) },
	resolve:     (*ContentRegistry).resolveDomainID,
	templates:   (*repository.CampaignTemplate).GetByDomainID,
	decls: func(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		pkg *registry.VerifiedPackage, doc contentDoc, vars map[string]string,
	) []entityDecl[model.Domain] {
		var out []entityDecl[model.Domain]
		for _, d := range doc.dy.Domains {
			// the name is the real domain the installer supplies through a variable
			// such as @@DOMAIN@@, so it is substituted rather than used as authored.
			domainName := installDomainName(substitute(d.Name, vars))
			if strings.EqualFold(d.Type, "proxy") {
				// a proxy domain needs a resolved proxy reference that the package format
				// does not carry, so it is never created.
				out = append(out, entityDecl[model.Domain]{
					name: domainName,
					skip: "domain " + domainName + ": proxy domains are not installable",
				})
				continue
			}
			content := substitute(string(pkg.Files[doc.prefix+d.Content]), vars)
			notFound := substitute(string(pkg.Files[doc.prefix+d.NotFound]), vars)
			redirect := substitute(d.RedirectURL, vars)
			out = append(out, entityDecl[model.Domain]{
				name: domainName,
				build: func() (*model.Domain, error) {
					return buildDomainModel(
						domainName, d.HostWebsite, d.ManagedTLS, d.SelfSignedTLS, d.OwnManagedTLS,
						d.OwnManagedTLSKey, d.OwnManagedTLSPem, redirect, content, notFound,
						s.domainReservedHosts()...,
					)
				},
				create: func() (*uuid.UUID, error) {
					return s.createDomain(
						ctx, session, domainName, d.HostWebsite, d.ManagedTLS, d.SelfSignedTLS, d.OwnManagedTLS,
						d.OwnManagedTLSKey, d.OwnManagedTLSPem, redirect, content, notFound,
					)
				},
			})
		}
		return out
	},
}

var allowDenySpec = &entitySpec[model.AllowDeny]{
	resourceType: "allowdeny",
	label:        "allow/deny",
	planLabel:    "allow/deny list",
	svc: func(s *ContentRegistry) entityService[model.AllowDeny] {
		return entityService[model.AllowDeny]{s.AllowDeny.Create, s.AllowDeny.Update, s.AllowDeny.DeleteByID}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.AllowDeny, error) {
		return s.AllowDeny.AllowDenyRepository.GetByID(ctx, id, &repository.AllowDenyOption{})
	},
	fingerprint: allowDenyFingerprint,
	name:        func(m *model.AllowDeny) string { return optStr(m.Name) },
	rename:      renameOnly(vo.NewString127, func(m *model.AllowDeny, n vo.String127) { m.Name = nn(n) }),
	decls: func(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		pkg *registry.VerifiedPackage, doc contentDoc, vars map[string]string,
	) []entityDecl[model.AllowDeny] {
		var out []entityDecl[model.AllowDeny]
		for _, ad := range doc.dy.AllowDenies {
			// every field is substituted, the name too, so the planned fingerprint
			// matches what install stored and no false conflict appears.
			name := substitute(ad.Name, vars)
			cidrs, countryCodes := substitute(ad.Cidrs, vars), substitute(ad.CountryCodes, vars)
			asns, ja4 := substitute(ad.Asns, vars), substitute(ad.JA4, vars)
			headersJSON := allowDenyHeadersJSON(ad.Headers, vars)
			out = append(out, entityDecl[model.AllowDeny]{name: name, build: func() (*model.AllowDeny, error) {
				return buildAllowDenyModel(name, ad.Allowed, cidrs, countryCodes, asns, ja4, headersJSON)
			}})
		}
		return out
	},
}

// smtpSpec covers the custom headers too: the fingerprint includes them, an in
// place update replaces them and a create adds them. Its update reconcile loads
// the installed entities on its own, see buildSMTPReconcile.
var smtpSpec = &entitySpec[model.SMTPConfiguration]{
	resourceType: "smtp",
	label:        "smtp",
	planLabel:    "smtp configuration",
	svc: func(s *ContentRegistry) entityService[model.SMTPConfiguration] {
		return entityService[model.SMTPConfiguration]{
			s.SMTPConfiguration.Create, s.SMTPConfiguration.UpdateByID, s.SMTPConfiguration.DeleteByID,
		}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.SMTPConfiguration, error) {
		return s.SMTPConfigurationRepository.GetByID(ctx, id, &repository.SMTPConfigurationOption{WithHeaders: true})
	},
	fingerprint: smtpFingerprint,
	name:        func(m *model.SMTPConfiguration) string { return optStr(m.Name) },
	resolve:     (*ContentRegistry).resolveSMTPID,
	templates:   (*repository.CampaignTemplate).GetBySmtpID,
	decls: func(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		pkg *registry.VerifiedPackage, doc contentDoc, vars map[string]string,
	) []entityDecl[model.SMTPConfiguration] {
		var out []entityDecl[model.SMTPConfiguration]
		for _, sm := range doc.dy.SMTPConfigurations {
			host, username := substitute(sm.Host, vars), substitute(sm.Username, vars)
			password, helo := substitute(sm.Password, vars), substitute(sm.Helo, vars)
			headers, herr := smtpHeaderModels(sm.Headers, vars)
			build := func() (*model.SMTPConfiguration, error) {
				return buildSMTPModel(sm.Name, host, sm.Port, username, password, sm.IgnoreCertErrors, helo)
			}
			out = append(out, entityDecl[model.SMTPConfiguration]{
				name:  sm.Name,
				check: herr,
				build: build,
				fingerprint: func(m *model.SMTPConfiguration) string {
					m.Headers = headers
					return smtpFingerprint(m)
				},
				create: func() (*uuid.UUID, error) {
					if herr != nil {
						return nil, herr
					}
					return s.createSMTPWithHeaders(
						ctx, session, sm.Name, host, sm.Port, username, password,
						sm.IgnoreCertErrors, helo, headers,
					)
				},
				update: func(id *uuid.UUID) error {
					m, berr := build()
					if berr != nil {
						return berr
					}
					if err := s.SMTPConfiguration.UpdateByID(ctx, session, id, m); err != nil {
						return err
					}
					return s.setSMTPHeaders(ctx, session, id, headers)
				},
			})
		}
		return out
	},
}

var webhookSpec = &entitySpec[model.Webhook]{
	resourceType: "webhook",
	label:        "webhook",
	planLabel:    "webhook",
	svc: func(s *ContentRegistry) entityService[model.Webhook] {
		return entityService[model.Webhook]{s.Webhook.Create, s.Webhook.Update, s.Webhook.DeleteByID}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.Webhook, error) {
		return s.Webhook.WebhookRepository.GetByID(ctx, id)
	},
	fingerprint: webhookFingerprint,
	name:        func(m *model.Webhook) string { return optStr(m.Name) },
	rename:      renameOnly(vo.NewString127, func(m *model.Webhook, n vo.String127) { m.Name = nn(n) }),
	decls: func(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		pkg *registry.VerifiedPackage, doc contentDoc, vars map[string]string,
	) []entityDecl[model.Webhook] {
		var out []entityDecl[model.Webhook]
		for _, wh := range doc.dy.Webhooks {
			url, secret := substitute(wh.URL, vars), substitute(wh.Secret, vars)
			out = append(out, entityDecl[model.Webhook]{name: wh.Name, build: func() (*model.Webhook, error) {
				return buildWebhookModel(wh.Name, url, secret)
			}})
		}
		return out
	},
}

// oauthProviderSpec offers no "both" resolution: a provider never returns its
// client secret, so a rename cannot send the full model its update checks. The
// repository row does carry the secret, so a restore can, and sending a secret
// marks the provider unauthorized the same as the forward update did.
var oauthProviderSpec = &entitySpec[model.OAuthProvider]{
	resourceType: "oauthprovider",
	label:        "oauth provider",
	planLabel:    "oauth provider",
	svc: func(s *ContentRegistry) entityService[model.OAuthProvider] {
		return entityService[model.OAuthProvider]{
			s.OAuthProvider.Create, s.OAuthProvider.UpdateByID, s.OAuthProvider.DeleteByID,
		}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.OAuthProvider, error) {
		return s.OAuthProviderRepository.GetByID(ctx, *id)
	},
	fingerprint: oauthProviderFingerprint,
	name:        func(m *model.OAuthProvider) string { return optStr(m.Name) },
	decls: func(
		s *ContentRegistry, ctx context.Context, session *model.Session,
		pkg *registry.VerifiedPackage, doc contentDoc, vars map[string]string,
	) []entityDecl[model.OAuthProvider] {
		var out []entityDecl[model.OAuthProvider]
		for _, op := range doc.dy.OAuthProviders {
			authURL, tokenURL := substitute(op.AuthURL, vars), substitute(op.TokenURL, vars)
			scopes := substitute(op.Scopes, vars)
			clientID, clientSecret := substitute(op.ClientID, vars), substitute(op.ClientSecret, vars)
			build := func() (*model.OAuthProvider, error) {
				return buildOAuthProviderModel(op.Name, authURL, tokenURL, scopes, clientID, clientSecret)
			}
			out = append(out, entityDecl[model.OAuthProvider]{name: op.Name, build: build})
		}
		return out
	},
}

// apiSenderSpec has no decls: a sender names its oauth provider, which install
// and update resolve on their own.
var apiSenderSpec = &entitySpec[model.APISender]{
	resourceType: "apisender",
	svc: func(s *ContentRegistry) entityService[model.APISender] {
		return entityService[model.APISender]{s.APISender.Create, s.APISender.UpdateByID, s.APISender.DeleteByID}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.APISender, error) {
		return s.APISenderRepository.GetByID(ctx, id, &repository.APISenderOption{
			WithRequestHeaders: true, WithResponseHeaders: true,
		})
	},
	fingerprint: apiSenderFingerprint,
	name:        func(m *model.APISender) string { return optStr(m.Name) },
	resolve:     (*ContentRegistry).resolveAPISenderID,
	templates:   (*repository.CampaignTemplate).GetByAPISenderID,
	rename:      renameOnly(vo.NewString64, func(m *model.APISender, n vo.String64) { m.Name = nn(n) }),
}

// campaignTemplateSpec is pure wiring: its drift is read from the declaration,
// so it has no stored fingerprint and an update never raises a conflict for it.
var campaignTemplateSpec = &entitySpec[model.CampaignTemplate]{
	resourceType: "campaigntemplate",
	svc: func(s *ContentRegistry) entityService[model.CampaignTemplate] {
		return entityService[model.CampaignTemplate]{
			s.CampaignTemplate.Create, s.CampaignTemplate.UpdateByID, s.CampaignTemplate.DeleteByID,
		}
	},
	load: func(s *ContentRegistry, ctx context.Context, id *uuid.UUID) (*model.CampaignTemplate, error) {
		return s.CampaignTemplateRepository.GetByID(ctx, id, &repository.CampaignTemplateOption{})
	},
	name:      func(m *model.CampaignTemplate) string { return optStr(m.Name) },
	driftless: true,
}
