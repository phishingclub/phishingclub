package service

import (
	"context"
	"net"
	"strings"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
	"gorm.io/gorm"
)

// AllowListing builds the allow listing information of templates, campaigns
// and programs. It only reads configuration, never per person data.
type AllowListing struct {
	Common
	CampaignTemplateRepository *repository.CampaignTemplate
	CampaignRepository         *repository.Campaign
	ProgramRepository          *repository.Program
	DomainRepository           *repository.Domain
}

// authorize checks the session may read allow listing information and writes
// an audit event only when it may not
func (a *AllowListing) authorize(session *model.Session, action string, id *uuid.UUID) error {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent(action, session)
		ae.Details["id"] = id.String()
		a.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	return nil
}

// inCompany reports whether an item with the given owner company is shown
// in the company in view. A nil company in view is the global view. A global
// item, with no owner, is shown in every view, the same as the program,
// campaign and template pages show it. An item a company owns is shown only
// in the view of that company.
func inCompany(companyID *uuid.UUID, owner nullable.Nullable[uuid.UUID]) bool {
	ownerID, err := owner.Get()
	if err != nil {
		return true
	}
	return companyID != nil && *companyID == ownerID
}

// GetByTemplateID returns the allow listing information of one template. The
// template must belong to the company in view or be global. The template is
// read once, with what the allow listing needs.
func (a *AllowListing) GetByTemplateID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	companyID *uuid.UUID,
) (*model.AllowListing, error) {
	if err := a.authorize(session, "AllowListing.GetByTemplateId", id); err != nil {
		return nil, err
	}
	ids := []uuid.UUID{*id}
	templates, err := a.readTemplates(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(templates) == 0 {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}
	tmpl := templates[0]
	if !inCompany(companyID, tmpl.CompanyID) {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}
	return a.combine(ctx, ids, templates)
}

// GetByCampaignID returns the allow listing information of every template in
// the template pool of a campaign. A campaign a program owns is not found, as
// it is read through its program.
func (a *AllowListing) GetByCampaignID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	companyID *uuid.UUID,
) (*model.AllowListing, error) {
	if err := a.authorize(session, "AllowListing.GetByCampaignId", id); err != nil {
		return nil, err
	}
	campaign, err := a.CampaignRepository.GetByID(ctx, id, &repository.CampaignOption{})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if campaign.IsProgramManaged() || !inCompany(companyID, campaign.CompanyID) {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}
	ids, err := a.CampaignRepository.GetTemplatePoolIDs(ctx, id)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// a campaign saved before template pools has only its template id
	if len(ids) == 0 {
		if tid, err := campaign.TemplateID.Get(); err == nil {
			ids = []uuid.UUID{tid}
		}
	}
	return a.build(ctx, ids)
}

// GetByProgramID returns the allow listing information of every template the
// steps of a program use
func (a *AllowListing) GetByProgramID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	companyID *uuid.UUID,
) (*model.AllowListing, error) {
	if err := a.authorize(session, "AllowListing.GetByProgramId", id); err != nil {
		return nil, err
	}
	program, err := a.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{WithSteps: true})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if !inCompany(companyID, program.CompanyID) {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}
	ids := make([]uuid.UUID, 0, len(program.Steps))
	for _, step := range program.Steps {
		if step == nil {
			continue
		}
		if tid, err := step.CampaignTemplateID.Get(); err == nil {
			ids = append(ids, tid)
		}
	}
	return a.build(ctx, ids)
}

// build reads the templates and combines their values. Templates are listed
// in the order of ids, each once.
func (a *AllowListing) build(ctx context.Context, ids []uuid.UUID) (*model.AllowListing, error) {
	unique := make([]uuid.UUID, 0, len(ids))
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	templates, err := a.readTemplates(ctx, unique)
	if err != nil {
		return nil, err
	}
	return a.combine(ctx, unique, templates)
}

// readTemplates reads the templates with their domain, email and sender in
// one batched read. A template that does not exist is left out.
func (a *AllowListing) readTemplates(ctx context.Context, ids []uuid.UUID) ([]*model.CampaignTemplate, error) {
	templates, err := a.CampaignTemplateRepository.GetByIDs(ctx, ids, &repository.CampaignTemplateOption{
		WithDomain:            true,
		WithEmail:             true,
		WithSMTPConfiguration: true,
		WithAPISender:         true,
	})
	if err != nil {
		a.Logger.Errorw("failed to get templates for allow listing", "error", err)
		return nil, errs.Wrap(err)
	}
	return templates, nil
}

// combine combines the values of the templates read for unique, listed in
// the order of unique. An id with no template counts as deleted.
func (a *AllowListing) combine(
	ctx context.Context,
	unique []uuid.UUID,
	templates []*model.CampaignTemplate,
) (*model.AllowListing, error) {
	result := &model.AllowListing{
		MailFromDomains:      []string{},
		SendingIPs:           []string{},
		SimulationURLs:       []string{},
		SimulationDomainURLs: []string{},
		TooLongURLs:          []string{},
		Templates:            []*model.AllowListingTemplate{},
	}
	byID := make(map[uuid.UUID]*model.CampaignTemplate, len(templates))
	proxyIDs := []uuid.UUID{}
	for _, t := range templates {
		tid, err := t.ID.Get()
		if err != nil {
			continue
		}
		byID[tid] = t
		proxyIDs = append(proxyIDs, templateProxyIDs(t)...)
	}
	proxyDomains, err := a.DomainRepository.GetNamesByProxyIDs(ctx, proxyIDs)
	if err != nil {
		a.Logger.Errorw("failed to get proxy domains for allow listing", "error", err)
		return nil, errs.Wrap(err)
	}

	mailFrom := newOrderedSet()
	sendingIPs := newOrderedSet()
	hosts := newOrderedSet()
	for _, id := range unique {
		t, ok := byID[id]
		if !ok {
			result.DeletedTemplates++
			continue
		}
		entry := allowListingTemplate(id, t, proxyDomains)
		mailFrom.add(entry.MailFromDomain)
		for _, ip := range entry.SendingIPs {
			sendingIPs.add(ip)
		}
		hosts.add(entry.Domain)
		for _, d := range entry.ProxyDomains {
			hosts.add(d)
		}
		result.Templates = append(result.Templates, entry)
	}
	result.MailFromDomains = mailFrom.values
	result.SendingIPs = sendingIPs.values
	setSimulationURLs(result, hosts.values)
	for _, ip := range result.SendingIPs {
		if strings.Contains(ip, ":") {
			result.HasIPv6SendingIP = true
			break
		}
	}
	result.OverLimit = model.NewAllowListingOverLimit(result)
	return result, nil
}

// allowListingTemplate reads the allow listing values of one template and
// lists what it is missing
func allowListingTemplate(
	id uuid.UUID,
	t *model.CampaignTemplate,
	proxyDomains map[uuid.UUID][]string,
) *model.AllowListingTemplate {
	entry := &model.AllowListingTemplate{
		ID:           id,
		ProxyDomains: []string{},
		SendingIPs:   []string{},
		Missing:      []string{},
	}
	if name, err := t.Name.Get(); err == nil {
		entry.Name = name.String()
	}
	if t.Domain != nil {
		if name, err := t.Domain.Name.Get(); err == nil {
			entry.Domain = normalizeHost(name.String())
		}
	}
	domains := newOrderedSet()
	for _, proxyID := range templateProxyIDs(t) {
		for _, name := range proxyDomains[proxyID] {
			if d := normalizeHost(name); d != "" && d != entry.Domain {
				domains.add(d)
			}
		}
	}
	entry.ProxyDomains = domains.values
	if entry.Domain == "" && len(entry.ProxyDomains) == 0 {
		entry.Missing = append(entry.Missing, model.AllowListingMissingDomain)
	}

	if t.Email != nil {
		if emailID, err := t.Email.ID.Get(); err == nil {
			entry.EmailID = &emailID
		}
		if from, err := t.Email.MailEnvelopeFrom.Get(); err == nil {
			entry.MailFromDomain = envelopeDomain(from.String())
		}
		if entry.MailFromDomain == "" {
			entry.Missing = append(entry.Missing, model.AllowListingMissingMailFrom)
		}
	} else {
		entry.Missing = append(entry.Missing, model.AllowListingMissingEmail)
	}

	switch {
	case t.SMTPConfiguration != nil:
		entry.SenderType = model.AllowListingSenderSMTP
		if sid, err := t.SMTPConfiguration.ID.Get(); err == nil {
			entry.SenderID = &sid
		}
		if name, err := t.SMTPConfiguration.Name.Get(); err == nil {
			entry.SenderName = name.String()
		}
		entry.SendingIPs = t.SMTPConfiguration.PublicSendingIPList()
		if len(entry.SendingIPs) == 0 {
			entry.Missing = append(entry.Missing, model.AllowListingMissingSendingIP)
		}
	case t.APISender != nil:
		// an API sender has no sending IP field, the provider holds them
		entry.SenderType = model.AllowListingSenderAPISender
		if sid, err := t.APISender.ID.Get(); err == nil {
			entry.SenderID = &sid
		}
		if name, err := t.APISender.Name.Get(); err == nil {
			entry.SenderName = name.String()
		}
	default:
		entry.Missing = append(entry.Missing, model.AllowListingMissingSender)
	}
	return entry
}

// templateProxyIDs returns the proxies a template uses before, as and after
// the landing page
func templateProxyIDs(t *model.CampaignTemplate) []uuid.UUID {
	out := []uuid.UUID{}
	for _, v := range []nullable.Nullable[uuid.UUID]{
		t.BeforeLandingProxyID, t.LandingProxyID, t.AfterLandingProxyID,
	} {
		if id, err := v.Get(); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// envelopeDomain returns the domain of an envelope sender address in the form
// normalizeHost gives, empty when it has none
func envelopeDomain(from string) string {
	at := strings.LastIndex(from, "@")
	if at == -1 {
		return ""
	}
	domain := from[at+1:]
	if end := strings.IndexAny(domain, "> \t"); end != -1 {
		domain = domain[:end]
	}
	// written the same way as the hosts of the URLs: lowercase, without a
	// trailing dot and in punycode
	return normalizeHost(domain)
}

// setSimulationURLs sets the exact and the whole domain simulation URL lists
// of the hosts. An entry longer than Microsoft allows is left out and listed
// in TooLongURLs.
func setSimulationURLs(result *model.AllowListing, hosts []string) {
	exact := newOrderedSet()
	whole := newOrderedSet()
	tooLong := newOrderedSet()
	add := func(set *orderedSet, entry string) {
		if len(entry) > model.AllowListingMaxURLLength {
			tooLong.add(entry)
			return
		}
		set.add(entry)
	}
	for _, h := range hosts {
		host := normalizeHost(h)
		if host == "" {
			continue
		}
		add(exact, simulationURL(host))
		domain := registeredDomain(host)
		if domain == "" {
			add(whole, simulationURL(host))
			continue
		}
		// domain/* matches only the domain itself and *.domain/* only its
		// subdomains, so both are needed
		add(whole, simulationURL(domain))
		add(whole, simulationURL("*."+domain))
	}
	result.SimulationURLs = exact.values
	result.SimulationDomainURLs = whole.values
	result.TooLongURLs = tooLong.values
}

// normalizeHost returns the host in lower case, with an international name
// in punycode. A name that can not be converted is kept in lower case.
func normalizeHost(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return ""
	}
	if ascii, err := idna.Lookup.ToASCII(host); err == nil && ascii != "" {
		return ascii
	}
	return host
}

// registeredDomain returns the domain a host is registered under, such as
// example.co.uk for a.b.example.co.uk. It is empty for an IP address and for
// a host that is a public suffix itself, as those are listed exactly.
func registeredDomain(host string) string {
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return ""
	}
	if suffix, _ := publicsuffix.PublicSuffix(host); suffix == host {
		return ""
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}
	return domain
}

// simulationURL returns the URL pattern that allows every path on a host,
// empty for an empty host
func simulationURL(host string) string {
	if host == "" {
		return ""
	}
	return host + "/*"
}

// orderedSet keeps the first time each non empty value was added
type orderedSet struct {
	seen   map[string]bool
	values []string
}

func newOrderedSet() *orderedSet {
	return &orderedSet{seen: map[string]bool{}, values: []string{}}
}

func (s *orderedSet) add(v string) {
	if v == "" || s.seen[v] {
		return
	}
	s.seen[v] = true
	s.values = append(s.values, v)
}
