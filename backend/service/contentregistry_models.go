package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/embedded"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// setSMTPHeaders replaces the custom headers on an smtp configuration with want.
func (s *ContentRegistry) setSMTPHeaders(
	ctx context.Context, session *model.Session, id *uuid.UUID, want []*model.SMTPHeader,
) error {
	cur, err := s.SMTPConfigurationRepository.GetByID(ctx, id, &repository.SMTPConfigurationOption{WithHeaders: true})
	if err != nil {
		return errs.Wrap(err)
	}
	for _, h := range cur.Headers {
		if h == nil {
			continue
		}
		hid := h.ID
		if err := s.SMTPConfiguration.RemoveHeader(ctx, session, id, &hid); err != nil {
			return errs.Wrap(err)
		}
	}
	for _, h := range want {
		nh := &model.SMTPHeader{Key: h.Key, Value: h.Value}
		if _, err := s.SMTPConfiguration.AddHeader(ctx, session, id, nh); err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

// createSMTPWithHeaders creates an smtp configuration and adds its headers,
// deleting the configuration again when a header cannot be added.
func (s *ContentRegistry) createSMTPWithHeaders(
	ctx context.Context,
	session *model.Session,
	name, host string,
	port int,
	username, password string,
	ignoreCertErrors bool,
	helo string,
	headers []*model.SMTPHeader,
) (*uuid.UUID, error) {
	id, err := s.createSMTP(ctx, session, name, host, port, username, password, ignoreCertErrors, helo)
	if err != nil {
		return nil, err
	}
	for _, h := range headers {
		nh := &model.SMTPHeader{Key: h.Key, Value: h.Value}
		if _, herr := s.SMTPConfiguration.AddHeader(ctx, session, id, nh); herr != nil {
			_ = s.SMTPConfiguration.DeleteByID(ctx, session, id)
			return nil, errs.Wrap(herr)
		}
	}
	return id, nil
}

func (s *ContentRegistry) createPage(
	ctx context.Context, session *model.Session, name, content string,
) (*uuid.UUID, error) {
	p := &model.Page{}
	n, err := vo.NewString64(name)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	p.Name = nullable.NewNullableWithValue(*n)
	c, err := vo.NewOptionalString1MB(content)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	p.Content = nullable.NewNullableWithValue(*c)
	return s.Page.Create(ctx, session, p)
}

func (s *ContentRegistry) createEmail(
	ctx context.Context, session *model.Session, name, content, subject, from, envelopeFrom string,
) (*uuid.UUID, error) {
	e, err := buildEmailModel(name, emailContent{
		content: content, subject: subject, from: from, envelopeFrom: envelopeFrom,
	})
	if err != nil {
		return nil, err
	}
	return s.Email.Create(ctx, session, e)
}

// buildEmailModel constructs the email model createEmail sends to the Email
// service, with the tracking pixel requested.
func buildEmailModel(name string, ec emailContent) (*model.Email, error) {
	e := &model.Email{}
	n, err := vo.NewString64(name)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	e.Name = nullable.NewNullableWithValue(*n)
	c, err := vo.NewOptionalString1MB(ec.content)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	e.Content = nullable.NewNullableWithValue(*c)
	setEmailMeta(e, ec)
	e.AddTrackingPixel = nullable.NewNullableWithValue(true)
	return e, nil
}

func nn[T any](v T) nullable.Nullable[T] { return nullable.NewNullableWithValue(v) }

// buildScriptModel constructs a script model from the installed values. create
// and the update reconcile share it so an install and a later update build the
// entity the same way.
func buildScriptModel(name, body string) (*model.Script, error) {
	m := &model.Script{}
	n, err := vo.NewString127(name)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Name = nn(*n)
	b, err := vo.NewString1MB(body)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Script = nn(*b)
	return m, nil
}

func buildReportTemplateModel(content string, isTraining bool) (*model.ReportTemplate, error) {
	m := &model.ReportTemplate{}
	c, err := vo.NewOptionalString1MB(content)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Content = nn(*c)
	m.IsTraining = nn(isTraining)
	return m, nil
}

func (s *ContentRegistry) createReportTemplate(
	ctx context.Context, session *model.Session, content string, isTraining bool,
) (*uuid.UUID, error) {
	m, err := buildReportTemplateModel(content, isTraining)
	if err != nil {
		return nil, err
	}
	id, err := s.ReportTemplate.Create(ctx, session, m)
	if err != nil && isUniqueViolation(err) {
		// the slot was taken after the preflight check ran.
		return nil, reportTemplateSlotErr(isTraining)
	}
	return id, err
}

// isUniqueViolation reports whether err is a database unique constraint error.
func isUniqueViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate key")
}

// reportTemplateKind names a report template kind for messages and for the
// overwrite confirmation sent back by the caller.
func reportTemplateKind(isTraining bool) string {
	if isTraining {
		return "training"
	}
	return "simulation"
}

// reportTemplateSlotErr explains that a write would make a second global report
// template of a kind. Phishing Club holds exactly one per kind (simulation and
// training), so a package template moved to a kind whose slot is held by
// another template is refused.
func reportTemplateSlotErr(isTraining bool) error {
	kind := reportTemplateKind(isTraining)
	return errs.NewValidationError(fmt.Errorf(
		"Phishing Club keeps one global %s report template and it already exists, "+
			"so this package cannot add another one",
		kind,
	))
}

// reportTemplateSlot returns the global report template of a kind, or nil when
// none exists.
func (s *ContentRegistry) reportTemplateSlot(ctx context.Context, isTraining bool) (*model.ReportTemplate, error) {
	cur, err := s.ReportTemplate.ReportTemplateRepository.GetForCampaign(ctx, nil, isTraining)
	if err == nil {
		return cur, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return nil, errs.Wrap(err)
}

// reportTemplateOwnerErr refuses to overwrite a global report template that an
// installed package already tracks: two packages would then restore over each
// other on uninstall. The user uninstalls or updates that package instead.
func (s *ContentRegistry) reportTemplateOwnerErr(ctx context.Context, isTraining bool, id *uuid.UUID) error {
	rows, err := s.RegistryRepository.GetInstalledResourcesByResource(ctx, "reporttemplate", id)
	if err != nil {
		return errs.Wrap(err)
	}
	if len(rows) == 0 {
		return nil
	}
	owner := "another installed package"
	if ip, gerr := s.RegistryRepository.GetInstalledPackageByID(ctx, rows[0].InstalledPackageID); gerr == nil {
		owner = "the installed package " + ip.PackageID
	}
	return errs.NewValidationError(fmt.Errorf(
		"the %s report template is managed by %s; uninstall that package first, then install again",
		reportTemplateKind(isTraining), owner,
	))
}

// reportTemplateName describes a global report template for the overwrite
// confirmation, since report templates carry no name of their own.
func reportTemplateName(m *model.ReportTemplate, isTraining bool) (string, bool) {
	def := embedded.DefaultReportHTML
	if isTraining {
		def = embedded.DefaultTrainingReportHTML
	}
	customized := strings.TrimSpace(optStr(m.Content)) != strings.TrimSpace(def)
	label := "Default"
	if customized {
		label = "Custom"
	}
	return label + " " + reportTemplateKind(isTraining) + " report template", customized
}

// installReportTemplate writes package content into the global report template
// of a kind. An existing template is overwritten in place, keeping its id and
// kind so reports keep using it, and its previous content is returned so it can
// be written back on uninstall or a failed install. When the slot is empty the
// template is created and replaced is nil.
func (s *ContentRegistry) installReportTemplate(
	ctx context.Context, session *model.Session, content string, isTraining bool,
) (_ *uuid.UUID, replaced *string, _ error) {
	cur, err := s.reportTemplateSlot(ctx, isTraining)
	if err != nil {
		return nil, nil, err
	}
	if cur == nil {
		id, cerr := s.createReportTemplate(ctx, session, content, isTraining)
		return id, nil, cerr
	}
	id := cur.ID.MustGet()
	if oerr := s.reportTemplateOwnerErr(ctx, isTraining, &id); oerr != nil {
		return nil, nil, oerr
	}
	c, err := vo.NewOptionalString1MB(content)
	if err != nil {
		return nil, nil, errs.NewValidationError(err)
	}
	prev := optStr(cur.Content)
	if uerr := s.ReportTemplate.UpdateByID(ctx, session, &id, &model.ReportTemplate{Content: nn(*c)}); uerr != nil {
		return nil, nil, uerr
	}
	return &id, &prev, nil
}

// restoreReportTemplate writes previous content back onto a global report
// template. A template that no longer exists counts as restored.
func (s *ContentRegistry) restoreReportTemplate(
	ctx context.Context, session *model.Session, id *uuid.UUID, content string,
) error {
	c, err := vo.NewOptionalString1MB(content)
	if err != nil {
		return errs.NewValidationError(err)
	}
	_, gerr := s.ReportTemplate.ReportTemplateRepository.GetByID(ctx, id, &repository.ReportTemplateOption{})
	if errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil
	}
	return s.ReportTemplate.UpdateByID(ctx, session, id, &model.ReportTemplate{Content: nn(*c)})
}

// reportTemplateDeclsValid refuses a set of packages that declares more than one
// report template of the same kind, since there is only one slot per kind.
func (s *ContentRegistry) reportTemplateDeclsValid(pkgs []*registry.VerifiedPackage) error {
	seen := map[bool]bool{}
	for _, pkg := range pkgs {
		docs, err := contentDocs(pkg)
		if err != nil {
			return s.contentDocsErr(err)
		}
		for _, doc := range docs {
			for _, rt := range doc.dy.ReportTemplates {
				if seen[rt.IsTraining] {
					return errs.NewValidationError(fmt.Errorf(
						"more than one %s report template is declared; Phishing Club keeps one per kind",
						reportTemplateKind(rt.IsTraining),
					))
				}
				seen[rt.IsTraining] = true
			}
		}
	}
	return nil
}

// reportTemplateDecl is one report template a write would put in a global slot.
type reportTemplateDecl struct {
	sourceName string
	isTraining bool
	packageID  string
}

// planReportTemplateOverwrites checks, before anything is written, each global
// report template the declarations would overwrite. A template another install
// already tracks is refused. When a kind is not in confirm, it returns
// ErrReportTemplateOverwrite listing every template that would be overwritten,
// so the user confirms them together. It returns the planned overwrites for the
// audit log.
func (s *ContentRegistry) planReportTemplateOverwrites(
	ctx context.Context, decls []reportTemplateDecl, confirm []string,
) ([]ReportTemplateOverwrite, error) {
	confirmed := map[string]bool{}
	for _, k := range confirm {
		confirmed[strings.ToLower(strings.TrimSpace(k))] = true
	}
	var planned []ReportTemplateOverwrite
	missing := false
	for _, d := range decls {
		cur, err := s.reportTemplateSlot(ctx, d.isTraining)
		if err != nil {
			return nil, err
		}
		if cur == nil {
			continue
		}
		id := cur.ID.MustGet()
		if oerr := s.reportTemplateOwnerErr(ctx, d.isTraining, &id); oerr != nil {
			return nil, oerr
		}
		kind := reportTemplateKind(d.isTraining)
		name, customized := reportTemplateName(cur, d.isTraining)
		planned = append(planned, ReportTemplateOverwrite{
			Kind:       kind,
			TemplateID: id.String(),
			Name:       name,
			Customized: customized,
			SourceName: d.sourceName,
			PackageID:  d.packageID,
		})
		if !confirmed[kind] {
			missing = true
		}
	}
	if missing {
		return nil, ErrReportTemplateOverwrite{Templates: planned}
	}
	return planned, nil
}

// reportTemplateOverwriteAudit is the audit detail for planned overwrites: the
// template id, its kind and the package that replaces it.
func reportTemplateOverwriteAudit(planned []ReportTemplateOverwrite) []map[string]string {
	out := make([]map[string]string, 0, len(planned))
	for _, p := range planned {
		out = append(out, map[string]string{"templateId": p.TemplateID, "kind": p.Kind, "package": p.PackageID})
	}
	return out
}

// reportTemplateInstallCheck runs before an install writes anything. It refuses
// two templates of one kind and plans the overwrite of each global report
// template the install replaces, which needs the kind in confirm.
func (s *ContentRegistry) reportTemplateInstallCheck(
	ctx context.Context, pkgs []*registry.VerifiedPackage, confirm []string,
) ([]ReportTemplateOverwrite, error) {
	if err := s.reportTemplateDeclsValid(pkgs); err != nil {
		return nil, err
	}
	decls, err := s.reportTemplateDeclsOf(pkgs)
	if err != nil {
		return nil, err
	}
	return s.planReportTemplateOverwrites(ctx, decls, confirm)
}

// reportTemplateDeclsOf lists the report templates a fresh install of pkgs
// writes, skipping a declaration whose file is missing.
func (s *ContentRegistry) reportTemplateDeclsOf(pkgs []*registry.VerifiedPackage) ([]reportTemplateDecl, error) {
	var decls []reportTemplateDecl
	for _, pkg := range pkgs {
		docs, err := contentDocs(pkg)
		if err != nil {
			return nil, s.contentDocsErr(err)
		}
		pkgID := ""
		if pkg.Manifest != nil {
			pkgID = pkg.Manifest.ID
		}
		for _, doc := range docs {
			for _, rt := range doc.dy.ReportTemplates {
				if _, ok := pkg.Files[doc.prefix+rt.File]; !ok {
					continue
				}
				decls = append(decls, reportTemplateDecl{
					sourceName: strings.TrimSpace(rt.Name), isTraining: rt.IsTraining, packageID: pkgID,
				})
			}
		}
	}
	return decls, nil
}

func buildProxyModel(name, description, startURL, config string) (*model.Proxy, error) {
	m := &model.Proxy{}
	n, err := vo.NewString64(name)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Name = nn(*n)
	if description != "" {
		if d, derr := vo.NewOptionalString1024(description); derr == nil {
			m.Description = nn(*d)
		}
	}
	su, err := vo.NewString1024(startURL)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.StartURL = nn(*su)
	cfg, err := vo.NewString1MB(config)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.ProxyConfig = nn(*cfg)
	return m, nil
}

func buildRemoteBrowserModel(
	name, description, scriptBody, mode, remote, proxy string,
	headless bool,
	timeout int,
	lang string,
	extraFlags []string,
) (*model.RemoteBrowser, error) {
	m := &model.RemoteBrowser{}
	n, err := vo.NewString64(name)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Name = nn(*n)
	if description != "" {
		if d, derr := vo.NewOptionalString1024(description); derr == nil {
			m.Description = nn(*d)
		}
	}
	b, err := vo.NewString1MB(scriptBody)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Script = nn(*b)
	m.Config = nn(model.RemoteBrowserConfig{
		Mode:       mode,
		Remote:     remote,
		Proxy:      proxy,
		Headless:   headless,
		Timeout:    timeout,
		Lang:       lang,
		ExtraFlags: extraFlags,
	})
	return m, nil
}

func buildWebhookModel(name, url, secret string) (*model.Webhook, error) {
	m := &model.Webhook{}
	n, err := vo.NewString127(name)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Name = nn(*n)
	u, err := vo.NewString1024(url)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.URL = nn(*u)
	if secret != "" {
		if sec, serr := vo.NewOptionalString1024(secret); serr == nil {
			m.Secret = nn(*sec)
		}
	}
	return m, nil
}

// createIdentifier inserts a global identifier if one of that name does not exist.
// Returns the id and whether it was newly created.
func (s *ContentRegistry) createIdentifier(ctx context.Context, name string) (*uuid.UUID, bool, error) {
	existing, err := s.IdentifierRepository.GetByName(ctx, name)
	if err == nil && existing != nil && existing.ID.IsSpecified() {
		id := existing.ID.MustGet()
		return id, false, nil
	}
	m := &model.Identifier{}
	m.Name = nn(name)
	id, err := s.IdentifierRepository.Insert(ctx, m)
	if err != nil {
		return nil, false, errs.Wrap(err)
	}
	return id, true, nil
}

// buildDomainModel builds a domain from a package declaration. The name goes
// through the same check Domain create and update use: a plain lowercase
// hostname that is not one of reservedHosts (the admin and tls hosts). The
// redirect url must be empty or an absolute http or https url.
func buildDomainModel(
	name string,
	hostWebsite, managedTLS, selfSignedTLS, ownManagedTLS bool,
	ownKey, ownPem, redirectURL, content, notFound string,
	reservedHosts ...string,
) (*model.Domain, error) {
	if err := model.ValidateDomainName(name, reservedHosts...); err != nil {
		return nil, err
	}
	if err := model.ValidateDomainRedirectURL(redirectURL); err != nil {
		return nil, err
	}
	m := &model.Domain{}
	n, err := vo.NewString255(name)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Name = nn(*n)
	t, _ := vo.NewString32("regular")
	m.Type = nn(*t)
	m.HostWebsite = nn(hostWebsite)
	m.ManagedTLS = nn(managedTLS)
	m.SelfSignedTLS = nn(selfSignedTLS)
	m.OwnManagedTLS = nn(ownManagedTLS)
	m.OwnManagedTLSKey = nn(ownKey)
	m.OwnManagedTLSPem = nn(ownPem)
	if pc, e := vo.NewOptionalString1MB(content); e == nil {
		m.PageContent = nn(*pc)
	}
	if nf, e := vo.NewOptionalString1MB(notFound); e == nil {
		m.PageNotFoundContent = nn(*nf)
	}
	ru, err := vo.NewOptionalString1024(redirectURL)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.RedirectURL = nn(*ru)
	if ptd, e := vo.NewOptionalString255(""); e == nil {
		m.ProxyTargetDomain = nn(*ptd)
	}
	return m, nil
}

// installDomainName is the form a package domain name is stored in: trimmed,
// lower case and without one trailing dot, since the domain validator refuses
// upper case and a variable such as @@DOMAIN@@ may be filled in with any case.
func installDomainName(name string) string {
	return model.NormalizeDomainName(name)
}

// domainReservedHosts returns the hosts the admin server answers on, the same
// list the Domain service refuses as a domain name.
func (s *ContentRegistry) domainReservedHosts() []string {
	if s.Domain == nil || s.Domain.AdminHosts == nil {
		return nil
	}
	return s.Domain.AdminHosts()
}

func (s *ContentRegistry) createDomain(
	ctx context.Context,
	session *model.Session,
	name string,
	hostWebsite, managedTLS, selfSignedTLS, ownManagedTLS bool,
	ownKey, ownPem, redirectURL, content, notFound string,
) (*uuid.UUID, error) {
	m, err := buildDomainModel(
		installDomainName(name), hostWebsite, managedTLS, selfSignedTLS, ownManagedTLS,
		ownKey, ownPem, redirectURL, content, notFound, s.domainReservedHosts()...,
	)
	if err != nil {
		return nil, err
	}
	return s.Domain.Create(ctx, session, m)
}

func buildAllowDenyModel(
	name string, allowed bool, cidrs, countryCodes, asns, ja4, headersJSON string,
) (*model.AllowDeny, error) {
	m := &model.AllowDeny{}
	n, err := vo.NewString127(name)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Name = nn(*n)
	m.Allowed = nn(allowed)
	// blank lines are skipped like the admin form does. Any other line must be a
	// valid CIDR, so a bad value or a token left without a value fails the install
	// instead of being dropped from the list.
	var nets vo.IPNetSlice
	for _, line := range strings.Split(cidrs, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ipn, e := vo.NewIPNet(line)
		if e != nil {
			return nil, errs.NewValidationError(fmt.Errorf("allow/deny list %q has an invalid CIDR: %q", name, line))
		}
		nets = append(nets, *ipn)
	}
	if len(nets) > 0 {
		m.Cidrs = nn(nets)
	}
	if strings.TrimSpace(countryCodes) != "" {
		m.CountryCodes = nn(countryCodes)
	}
	if strings.TrimSpace(asns) != "" {
		m.Asns = nn(asns)
	}
	if strings.TrimSpace(ja4) != "" {
		m.JA4Fingerprints = nn(ja4)
	}
	if headersJSON != "" {
		m.Headers = nn(headersJSON)
	}
	return m, nil
}

func buildSMTPModel(
	name, host string, port int, username, password string, ignoreCertErrors bool, helo string,
) (*model.SMTPConfiguration, error) {
	m := &model.SMTPConfiguration{}
	n, err := vo.NewString127(name)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Name = nn(*n)
	h, err := vo.NewString255(host)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Host = nn(*h)
	p, err := vo.NewPort(uint16(port))
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Port = nn(*p)
	if u, e := vo.NewOptionalString255(username); e == nil {
		m.Username = nn(*u)
	}
	if pw, e := vo.NewOptionalString255(password); e == nil {
		m.Password = nn(*pw)
	}
	m.IgnoreCertErrors = nn(ignoreCertErrors)
	if hl, e := vo.NewOptionalString255(helo); e == nil {
		m.Helo = nn(*hl)
	}
	return m, nil
}

func (s *ContentRegistry) createSMTP(
	ctx context.Context,
	session *model.Session,
	name, host string,
	port int,
	username, password string,
	ignoreCertErrors bool,
	helo string,
) (*uuid.UUID, error) {
	m, err := buildSMTPModel(name, host, port, username, password, ignoreCertErrors, helo)
	if err != nil {
		return nil, err
	}
	return s.SMTPConfiguration.Create(ctx, session, m)
}

// buildOAuthProviderModel never carries tokens: OAuthProvider.Create forces
// IsAuthorized false and the operator authorizes afterwards.
func buildOAuthProviderModel(
	name, authURL, tokenURL, scopes, clientID, clientSecret string,
) (*model.OAuthProvider, error) {
	m := &model.OAuthProvider{}
	n, err := vo.NewString127(name)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Name = nn(*n)
	au, err := vo.NewString512(authURL)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.AuthURL = nn(*au)
	tu, err := vo.NewString512(tokenURL)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.TokenURL = nn(*tu)
	sc, err := vo.NewString2048(scopes)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Scopes = nn(*sc)
	ci, err := vo.NewString255(clientID)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.ClientID = nn(*ci)
	cs, err := vo.NewOptionalString255(clientSecret)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.ClientSecret = nn(*cs)
	return m, nil
}

func buildAPISenderModel(
	name, method, requestURL string,
	requestHeaders []model.HTTPHeader,
	requestBody string,
	expectedStatus int,
	responseHeaders []model.HTTPHeader,
	expectedBody, apiKey string,
	oauthProviderID *uuid.UUID,
) (*model.APISender, error) {
	m := &model.APISender{}
	n, err := vo.NewString64(name)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.Name = nn(*n)
	meth, err := vo.NewHTTPMethod(method)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.RequestMethod = nn(*meth)
	ru, err := vo.NewString255(requestURL)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.RequestURL = nn(*ru)
	reqHdr := model.APISenderHeaders{}
	for i := range requestHeaders {
		h := requestHeaders[i]
		reqHdr.Headers = append(reqHdr.Headers, &h)
	}
	m.RequestHeaders = nn(reqHdr)
	respHdr := model.APISenderHeaders{}
	for i := range responseHeaders {
		h := responseHeaders[i]
		respHdr.Headers = append(respHdr.Headers, &h)
	}
	m.ExpectedResponseHeaders = nn(respHdr)
	if rb, e := vo.NewOptionalString1MB(requestBody); e == nil {
		m.RequestBody = nn(*rb)
	}
	if eb, e := vo.NewOptionalString1MB(expectedBody); e == nil {
		m.ExpectedResponseBody = nn(*eb)
	}
	if expectedStatus > 0 {
		m.ExpectedResponseStatusCode = nn(expectedStatus)
	}
	if apiKey != "" {
		if ak, e := vo.NewOptionalString255(apiKey); e == nil {
			m.APIKey = nn(*ak)
		}
	}
	if oauthProviderID != nil {
		m.OAuthProviderID = nn(*oauthProviderID)
	}
	return m, nil
}

// apiSenderModel builds the api sender a declaration describes, with the values
// substituted and oauthProviderID bound. Install and update share it.
func apiSenderModel(
	as installAPISenderYAML, vars map[string]string, oauthProviderID *uuid.UUID,
) (*model.APISender, error) {
	headers := func(hs []installHeaderYAML) []model.HTTPHeader {
		out := make([]model.HTTPHeader, 0, len(hs))
		for _, h := range hs {
			out = append(out, model.HTTPHeader{Key: substitute(h.Key, vars), Value: substitute(h.Value, vars)})
		}
		return out
	}
	return buildAPISenderModel(
		as.Name, as.RequestMethod, substitute(as.RequestURL, vars),
		headers(as.RequestHeaders), substitute(as.RequestBody, vars),
		as.ExpectedResponseStatusCode, headers(as.ExpectedResponseHeaders),
		substitute(as.ExpectedResponseBody, vars), substitute(as.APIKey, vars),
		oauthProviderID,
	)
}

func refErr(kind, name string) error {
	return errs.NewValidationError(
		fmt.Errorf("referenced %s %q is not available (install its package first)", kind, name),
	)
}

// collisionErr turns the generic uniqueness error from a create into a clear
// message naming the resource, since an install or update aborts when a name
// already exists in Phishing Club rather than overwriting it.
func collisionErr(kind, name string, err error) error {
	if err != nil && strings.Contains(err.Error(), "not unique") {
		return errs.NewValidationError(
			fmt.Errorf("a %s named %q already exists; rename or remove it, then try again", kind, name),
		)
	}
	return err
}

// errCompanyDomain marks a domain name that exists but belongs to a company.
// Package content is global, so it never binds to a company's domain.
var errCompanyDomain = errors.New("domain belongs to a company")

// refResolveErr is the install error for a reference that did not resolve.
func refResolveErr(kind, name string, err error) error {
	if errors.Is(err, errCompanyDomain) {
		return errs.NewValidationError(fmt.Errorf(
			"%s %q belongs to a company; a package can only use a global domain", kind, name,
		))
	}
	return refErr(kind, name)
}

func (s *ContentRegistry) resolveDomainID(ctx context.Context, name string) (*uuid.UUID, error) {
	nv, err := vo.NewString255(installDomainName(name))
	if err != nil {
		return nil, err
	}
	// domain names are unique across companies, so the lookup is by name and a
	// company row is refused rather than skipped.
	d, err := s.DomainRepository.GetByName(ctx, nv, &repository.DomainOption{})
	if err != nil {
		return nil, err
	}
	if cid, e := d.CompanyID.Get(); e == nil && cid != uuid.Nil {
		return nil, errCompanyDomain
	}
	id := d.ID.MustGet()
	return &id, nil
}

func (s *ContentRegistry) resolveEmailID(ctx context.Context, name string) (*uuid.UUID, error) {
	nv, err := vo.NewString64(name)
	if err != nil {
		return nil, err
	}
	e, err := s.EmailRepository.GetByNameAndCompanyID(ctx, nv, nil, &repository.EmailOption{})
	if err != nil {
		return nil, err
	}
	id := e.ID.MustGet()
	return &id, nil
}

func (s *ContentRegistry) resolvePageID(ctx context.Context, name string) (*uuid.UUID, error) {
	nv, err := vo.NewString64(name)
	if err != nil {
		return nil, err
	}
	p, err := s.PageRepository.GetByNameAndCompanyID(ctx, nv, nil, &repository.PageOption{})
	if err != nil {
		return nil, err
	}
	id := p.ID.MustGet()
	return &id, nil
}

func (s *ContentRegistry) resolveProxyID(ctx context.Context, name string) (*uuid.UUID, error) {
	nv, err := vo.NewString64(name)
	if err != nil {
		return nil, err
	}
	p, err := s.ProxyRepository.GetByNameAndCompanyID(ctx, nv, nil, &repository.ProxyOption{})
	if err != nil {
		return nil, err
	}
	id := p.ID.MustGet()
	return &id, nil
}

func (s *ContentRegistry) resolveSMTPID(ctx context.Context, name string) (*uuid.UUID, error) {
	nv, err := vo.NewString127(name)
	if err != nil {
		return nil, err
	}
	sm, err := s.SMTPConfigurationRepository.GetByNameAndCompanyID(ctx, nv, nil, &repository.SMTPConfigurationOption{})
	if err != nil {
		return nil, err
	}
	id := sm.ID.MustGet()
	return &id, nil
}

func (s *ContentRegistry) resolveAPISenderID(ctx context.Context, name string) (*uuid.UUID, error) {
	nv, err := vo.NewString64(name)
	if err != nil {
		return nil, err
	}
	a, err := s.APISenderRepository.GetByName(ctx, nv, nil, &repository.APISenderOption{})
	if err != nil {
		return nil, err
	}
	id := a.ID.MustGet()
	return &id, nil
}

// pickOAuthProvider is the pick kind of a variable the operator fills by choosing
// one of their own OAuth providers in the install dialog.
const pickOAuthProvider = "oauthProvider"

// oauthBinding is what decides which OAuth provider a package may bind an API
// sender to: a provider installed from the same namespace on the same Bait Box,
// or one the operator chose through a pick variable of the package.
type oauthBinding struct {
	registryID *uuid.UUID
	manifest   *registry.Manifest
	// supplied holds the values the operator gave, keyed as varKey stores them.
	// A pick only counts when its value is in here, never from a default.
	supplied map[string]string
	share    bool
}

// oauthBindingFor reads the binding rules for content installed under ipID.
// picks holds the values the operator supplied.
func (s *ContentRegistry) oauthBindingFor(
	ctx context.Context, ipID *uuid.UUID, manifest *registry.Manifest, picks map[string]string,
) (*oauthBinding, error) {
	ip, err := s.RegistryRepository.GetInstalledPackageByID(ctx, ipID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &oauthBinding{
		registryID: ip.RegistryID, manifest: manifest, supplied: picks, share: !ip.VariablesScoped,
	}, nil
}

// picked returns the provider name the operator chose when raw is exactly one
// @@NAME@@ token for an OAuth provider pick variable of the package and the
// operator supplied a value for it. The name comes only from that value, never
// from merged values, where a bundle member could override it.
func (b *oauthBinding) picked(raw string) (string, bool) {
	name, ok := singleToken(raw)
	if !ok {
		return "", false
	}
	for _, v := range b.manifest.Variables {
		if v.Name != name || v.Pick != pickOAuthProvider {
			continue
		}
		val := strings.TrimSpace(b.supplied[varKey(b.manifest.ID, name, b.share)])
		return val, val != ""
	}
	return "", false
}

// singleToken returns NAME when raw is exactly one @@NAME@@ token.
func singleToken(raw string) (string, bool) {
	name, ok := strings.CutPrefix(strings.TrimSpace(raw), "@@")
	if !ok {
		return "", false
	}
	name, ok = strings.CutSuffix(name, "@@")
	if !ok || !registry.ValidVariableName(name) {
		return "", false
	}
	return name, true
}

// oauthRef is the OAuth provider name an API sender in the package points at:
// the operator's pick when raw is a pick token, else raw with vars substituted.
func (b *oauthBinding) oauthRef(raw string, vars map[string]string) string {
	if v, ok := b.picked(raw); ok {
		return v
	}
	return strings.TrimSpace(substitute(raw, vars))
}

// namespace is the namespace of the package the binding is for.
func (b *oauthBinding) namespace() string {
	return b.manifest.Namespace()
}

// oauthProviderFromNamespace reports whether the provider was installed by a
// package from registryID in namespace. Package ids are lower case by the
// manifest rules, so the namespaces compare exactly.
func (s *ContentRegistry) oauthProviderFromNamespace(
	ctx context.Context, providerID, registryID *uuid.UUID, namespace string,
) (bool, error) {
	rows, err := s.RegistryRepository.GetInstalledResourcesByResource(ctx, "oauthprovider", providerID)
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		ip, gerr := s.RegistryRepository.GetInstalledPackageByID(ctx, r.InstalledPackageID)
		if gerr != nil {
			if errors.Is(gerr, gorm.ErrRecordNotFound) {
				continue
			}
			return false, gerr
		}
		if *ip.RegistryID == *registryID && registry.IDNamespace(ip.PackageID) == namespace {
			return true, nil
		}
	}
	return false, nil
}

// foreignOAuthProviderErr refuses binding a package to an OAuth provider that did
// not come from its namespace on its Bait Box, since the sender could hand its
// token to any URL.
func foreignOAuthProviderErr(sender, provider string) error {
	return errs.NewValidationError(fmt.Errorf(
		"API sender %q names the OAuth provider %q, which was not installed from this namespace on this Bait Box. "+
			"Choose the provider through the package's OAuth provider variable in the install dialog. "+
			"If the package has no such variable, its author must add one",
		sender, provider,
	))
}

// packageOAuthProvider is what the OAuth provider reference of an API sender in a
// package points at. id is nil when no provider has the name ref. foreign is set
// when the provider exists but the package may not bind it.
type packageOAuthProvider struct {
	ref     string
	id      *uuid.UUID
	foreign bool
}

// lookupPackageOAuthProvider finds the provider an API sender in a package names.
// A package binds only a provider installed from its own namespace on the same
// Bait Box or one the operator picked; any other provider with a matching name
// is foreign.
func (s *ContentRegistry) lookupPackageOAuthProvider(
	ctx context.Context, b *oauthBinding, raw string, vars map[string]string,
) (packageOAuthProvider, error) {
	out := packageOAuthProvider{ref: b.oauthRef(raw, vars)}
	if out.ref == "" {
		return out, nil
	}
	p, err := s.OAuthProviderRepository.GetByNameAndCompanyID(ctx, out.ref, nil)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, nil
	}
	if err != nil {
		return out, errs.Wrap(err)
	}
	id := p.ID.MustGet()
	if _, ok := b.picked(raw); ok {
		out.id = &id
		return out, nil
	}
	ok, err := s.oauthProviderFromNamespace(ctx, &id, b.registryID, b.namespace())
	if err != nil {
		return out, errs.Wrap(err)
	}
	out.id = &id
	out.foreign = !ok
	return out, nil
}

// resolvePackageOAuthProviderID resolves the OAuth provider an API sender in a
// package names, refusing a missing or foreign provider.
func (s *ContentRegistry) resolvePackageOAuthProviderID(
	ctx context.Context, b *oauthBinding, sender, raw string, vars map[string]string,
) (*uuid.UUID, error) {
	p, err := s.lookupPackageOAuthProvider(ctx, b, raw, vars)
	if err != nil {
		return nil, err
	}
	return p.bind(sender)
}

// bind returns the provider id to bind, refusing a missing or foreign provider.
func (p packageOAuthProvider) bind(sender string) (*uuid.UUID, error) {
	switch {
	case p.ref == "":
		return nil, nil
	case p.id == nil:
		return nil, refErr("oauth provider", p.ref)
	case p.foreign:
		return nil, foreignOAuthProviderErr(sender, p.ref)
	}
	return p.id, nil
}

func (s *ContentRegistry) resolveIdentifierID(ctx context.Context, name string) (*uuid.UUID, error) {
	i, err := s.IdentifierRepository.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	return i.ID.MustGet(), nil
}

// setEmailMeta leaves fields the package does not set untouched.
func setEmailMeta(e *model.Email, ec emailContent) {
	if ec.from != "" {
		if f, err := vo.NewEmail(ec.from); err == nil {
			e.MailHeaderFrom = nullable.NewNullableWithValue(*f)
		}
	}
	if ec.envelopeFrom != "" {
		if ef, err := vo.NewMailEnvelopeFrom(ec.envelopeFrom); err == nil {
			e.MailEnvelopeFrom = nullable.NewNullableWithValue(*ef)
		}
	}
	if sub, err := vo.NewOptionalString255(ec.subject); err == nil {
		e.MailHeaderSubject = nullable.NewNullableWithValue(*sub)
	}
}

type emailContent struct {
	content      string
	subject      string
	from         string
	envelopeFrom string
}

// allowDenyHeadersJSON encodes the package header rules with variables
// substituted. Install and update both use it so the fingerprint and the stored
// entity match.
func allowDenyHeadersJSON(headers []struct {
	KeyRegex   string `yaml:"keyRegex"`
	ValueRegex string `yaml:"valueRegex"`
}, vars map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	rules := make([]map[string]string, 0, len(headers))
	for _, h := range headers {
		rules = append(rules, map[string]string{
			"keyRegex":   substitute(h.KeyRegex, vars),
			"valueRegex": substitute(h.ValueRegex, vars),
		})
	}
	if b, merr := json.Marshal(rules); merr == nil {
		return string(b)
	}
	return ""
}
