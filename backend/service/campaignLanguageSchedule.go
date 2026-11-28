package service

import (
	"context"
	"fmt"
	"sort"
	"text/template"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// campaignLanguages picks the language each new recipient of a campaign is
// stamped with. With routing on, the value of the route field is normalized
// and looked up in the routes, and a value with no route gets the default.
// Routing by the Language field takes the recipient's own language when every
// template in sets can send it, else the default. With routing off, a
// recipient gets the language of its template, or no stamp when the template
// has no language set.
type campaignLanguages struct {
	// companyID and pool are what the emails are checked against, see
	// checkStampEmails
	companyID *uuid.UUID
	pool      []uuid.UUID
	routeBy   string
	fallback  string
	routes    map[string]string
	sets      map[uuid.UUID]*repository.TemplateLanguageSet
	// memo holds the language of each raw value seen, so a value is
	// normalized once per schedule and not once per recipient
	memo map[string]string
}

// languageFor returns the stamp of a new row for recipient on templateID.
// A stamp that is not set is not written.
func (l *campaignLanguages) languageFor(recipient *model.Recipient, templateID uuid.UUID) nullable.Nullable[string] {
	if l == nil {
		return nullable.Nullable[string]{}
	}
	return l.languageForValue(recipientRouteValue(recipient, l.routeBy), templateID)
}

// recipientRouteValue returns the recipient's value of a language route field,
// or empty
func recipientRouteValue(recipient *model.Recipient, routeBy string) string {
	if routeBy != model.LanguageRouteLanguage {
		return recipientMixValue(recipient, routeBy)
	}
	if recipient == nil {
		return ""
	}
	if v, err := recipient.Language.Get(); err == nil {
		return v
	}
	return ""
}

// assignmentRouteValue returns the value of a language route field from a
// stored assignment
func assignmentRouteValue(row repository.TemplateAssignment, routeBy string) string {
	if routeBy == model.LanguageRouteLanguage {
		return row.RecipientLanguage
	}
	return assignmentMixValue(row, routeBy)
}

// directLanguage returns the language a recipient whose Language field holds
// raw gets: raw when every template in sets can send it, else the default
func (l *campaignLanguages) directLanguage(raw string) string {
	code, ok := data.NormalizeLanguage(raw)
	if !ok || code == "" {
		return l.fallback
	}
	for _, set := range l.sets {
		if !set.CanSend(code) {
			return l.fallback
		}
	}
	return code
}

// languageForValue returns the stamp of a row on templateID whose route
// field holds raw, see languageFor
func (l *campaignLanguages) languageForValue(raw string, templateID uuid.UUID) nullable.Nullable[string] {
	if l == nil {
		return nullable.Nullable[string]{}
	}
	if l.routeBy == "" {
		if set, ok := l.sets[templateID]; ok && set.Language != "" {
			return nullable.NewNullableWithValue(set.Language)
		}
		return nullable.Nullable[string]{}
	}
	code, ok := l.memo[raw]
	if !ok {
		code = l.fallback
		if l.routeBy == model.LanguageRouteLanguage {
			code = l.directLanguage(raw)
		} else if key := model.NormalizeLanguageKey(raw); key != "" {
			if v, found := l.routes[key]; found {
				code = v
			}
		}
		l.memo[raw] = code
	}
	return nullable.NewNullableWithValue(code)
}

// languageRoutingOf returns the route field, the default and the routes of a
// campaign. Routes that are not set on the campaign are read from the
// database.
func (c *Campaign) languageRoutingOf(
	ctx context.Context,
	campaign *model.Campaign,
) (routeBy string, fallback string, routes map[string]string, err error) {
	routeBy = campaign.LanguageRouteField()
	if routeBy == "" {
		return "", "", map[string]string{}, nil
	}
	if v, err := campaign.LanguageDefault.Get(); err == nil {
		fallback = v
	}
	if v, err := campaign.LanguageRoutes.Get(); err == nil {
		return routeBy, fallback, v, nil
	}
	routes = map[string]string{}
	if id, err := campaign.ID.Get(); err == nil {
		routes, err = c.CampaignRepository.GetLanguageRoutesByCampaignID(ctx, &id)
		if err != nil {
			return "", "", nil, err
		}
	}
	return routeBy, fallback, routes, nil
}

// campaignLanguagesFor reads the language routing of a campaign, checks it
// against every pool template, see checkPoolLanguages, and returns what
// schedule stamps new rows with. With routing by the Language field only the
// default is checked here, the caller checks the languages the rows get with
// checkStampEmails before it writes them.
func (c *Campaign) campaignLanguagesFor(
	ctx context.Context,
	campaign *model.Campaign,
	pool []uuid.UUID,
) (*campaignLanguages, error) {
	routeBy, fallback, routes, err := c.languageRoutingOf(ctx, campaign)
	if err != nil {
		return nil, err
	}
	sets, err := c.checkPoolLanguages(ctx, companyIDPtr(campaign.CompanyID), routeBy, fallback, routes, pool, nil)
	if err != nil {
		return nil, err
	}
	return &campaignLanguages{
		companyID: companyIDPtr(campaign.CompanyID),
		pool:      pool,
		routeBy:   routeBy,
		fallback:  fallback,
		routes:    routes,
		sets:      sets,
		memo:      map[string]string{},
	}, nil
}

// checkStampEmails checks the emails of the languages that recipients whose
// Language field holds one of raws get, see directLanguage. It is only needed
// for routing by the Language field, the other routings check every language
// they can pick in campaignLanguagesFor. The default is checked there too.
func (c *Campaign) checkStampEmails(ctx context.Context, l *campaignLanguages, raws []string) error {
	if l == nil || l.routeBy != model.LanguageRouteLanguage {
		return nil
	}
	codes := []string{}
	seen := map[string]bool{l.fallback: true}
	for _, raw := range raws {
		code := l.directLanguage(raw)
		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		return nil
	}
	sort.Strings(codes)
	return c.checkLanguageEmails(ctx, l.companyID, l.fallback, l.sets, l.pool, codes)
}

// recipientLanguages returns the Language field of each recipient
func recipientLanguages(recipients []*model.Recipient) []string {
	out := make([]string, 0, len(recipients))
	for _, r := range recipients {
		out = append(out, recipientRouteValue(r, model.LanguageRouteLanguage))
	}
	return out
}

// reachableCodes returns the codes routing by the Language field gives
// recipients with the languages in reachable on the templates in sets: the
// default and each of reachable that all of them can send, sorted and each
// once. Any other language gets the default.
func reachableCodes(fallback string, sets map[uuid.UUID]*repository.TemplateLanguageSet, reachable []string) []string {
	out := []string{}
	seen := map[string]bool{}
	if fallback != "" {
		out = append(out, fallback)
		seen[fallback] = true
	}
	for _, code := range reachable {
		if seen[code] || len(sets) == 0 {
			continue
		}
		seen[code] = true
		all := true
		for _, set := range sets {
			if !set.CanSend(code) {
				all = false
				break
			}
		}
		if all {
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out
}

// groupLanguageCodes returns the language codes the members of the groups
// have. Every code of the list is returned when the values can not all be
// read, so a check that uses them never checks less than it should.
func groupLanguageCodes(
	ctx context.Context,
	groups *repository.RecipientGroup,
	groupIDs []*uuid.UUID,
) ([]string, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	if groups == nil {
		return allLanguageCodes(), nil
	}
	counts, err := groups.GetFieldValueCounts(ctx, model.LanguageRouteLanguage, groupIDs)
	if err != nil {
		return nil, err
	}
	if counts.More {
		return allLanguageCodes(), nil
	}
	out := []string{}
	for _, row := range counts.Rows {
		if code, ok := data.NormalizeLanguage(row.Value); ok && code != "" {
			out = append(out, code)
		}
	}
	return out, nil
}

// reachableLanguages returns the language codes of the members of the
// campaign groups when routeBy is the Language field, else nothing
func (c *Campaign) reachableLanguages(
	ctx context.Context,
	routeBy string,
	campaign *model.Campaign,
) ([]string, error) {
	if routeBy != model.LanguageRouteLanguage {
		return nil, nil
	}
	ids, err := campaign.RecipientGroupIDs.Get()
	if err != nil {
		// the groups are not known, so every language may be reached
		return allLanguageCodes(), nil
	}
	return groupLanguageCodes(ctx, c.RecipientGroupRepository, ids)
}

// allLanguageCodes returns every code of the language list
func allLanguageCodes() []string {
	out := make([]string, 0, len(data.Languages))
	for _, l := range data.Languages {
		out = append(out, l.Code)
	}
	return out
}

// routedCodes returns the codes the routing can pick, the default and every
// route code, sorted and each once
func routedCodes(fallback string, routes map[string]string) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(code string) {
		if code != "" && !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	add(fallback)
	for _, code := range routes {
		add(code)
	}
	sort.Strings(out)
	return out
}

// languageContentError is a refusal of language routing because the content
// of a language can not be sent: the language is incomplete, or its email is
// gone or does not parse. It is still a validation error. A late schedule
// closes the campaign on it, as the sender does for broken content.
type languageContentError struct {
	err error
	// reason is the refusal without the field name, for the close reason
	reason string
}

func (e languageContentError) Error() string { return e.err.Error() }
func (e languageContentError) Unwrap() error { return e.err }

// checkPoolLanguages refuses language routing that can pick a language one of
// the pool templates can not send: every pool template must have a template
// language, and the default and every route code must be the template
// language or a usable language row of it. The email of every such row must
// exist and its body and subject must parse. Routing by the Language field
// needs only the default on every template, other languages fall back to the
// default per recipient; the emails of the default and of the languages in
// reachable that it gives are checked, so a broken email in a language no
// recipient has does not refuse the campaign. reachable is not used by the
// other routings. Returns the language set of each pool template that exists.
// A template that no longer exists is left to the pool checks and the sender.
func (c *Campaign) checkPoolLanguages(
	ctx context.Context,
	companyID *uuid.UUID,
	routeBy string,
	fallback string,
	routes map[string]string,
	pool []uuid.UUID,
	reachable []string,
) (map[uuid.UUID]*repository.TemplateLanguageSet, error) {
	sets, err := c.CampaignTemplateService.CampaignTemplateRepository.GetLanguageSetsByIDs(ctx, pool)
	if err != nil {
		return nil, err
	}
	if routeBy == "" {
		return sets, nil
	}
	codes := routedCodes(fallback, routes)
	if routeBy == model.LanguageRouteLanguage {
		codes = []string{fallback}
	}
	for _, id := range pool {
		set, ok := sets[id]
		if !ok {
			continue
		}
		if set.Language == "" {
			return nil, validate.WrapErrorWithField(
				fmt.Errorf("template '%s' has no main language, it is needed to pick languages by a field", set.Name),
				"languageRouteBy",
			)
		}
		for _, code := range codes {
			if set.CanSend(code) {
				continue
			}
			field := "languageRoutes"
			if code == fallback {
				field = "languageDefault"
			}
			reason := fmt.Errorf("template '%s' has no %s", set.Name, data.LanguageName(code))
			if set.Row(code) != nil {
				reason = fmt.Errorf("the %s of template '%s' is incomplete", data.LanguageName(code), set.Name)
			}
			return nil, languageContentError{err: validate.WrapErrorWithField(reason, field), reason: reason.Error()}
		}
	}
	emailCodes := codes
	if routeBy == model.LanguageRouteLanguage {
		emailCodes = reachableCodes(fallback, poolSets(sets, pool), reachable)
	}
	if err := c.checkLanguageEmails(ctx, companyID, fallback, sets, pool, emailCodes); err != nil {
		return nil, err
	}
	return sets, nil
}

// checkLanguageEmails refuses when the email of one of codes on a pool
// template is gone, has no content or subject, or does not parse. A code a
// template has no row for is left out, checkPoolLanguages refuses it.
func (c *Campaign) checkLanguageEmails(
	ctx context.Context,
	companyID *uuid.UUID,
	fallback string,
	sets map[uuid.UUID]*repository.TemplateLanguageSet,
	pool []uuid.UUID,
	emailCodes []string,
) error {
	for _, id := range pool {
		set, ok := sets[id]
		if !ok {
			continue
		}
		for _, code := range emailCodes {
			row := set.Row(code)
			if row == nil {
				continue
			}
			emailID, err := row.EmailID.Get()
			if err != nil {
				continue
			}
			problem, err := c.languageEmailProblem(ctx, companyID, &emailID)
			if err != nil {
				return err
			}
			if problem != "" {
				field := "languageRoutes"
				if code == fallback {
					field = "languageDefault"
				}
				reason := fmt.Errorf("the %s email of template '%s' %s", data.LanguageName(code), set.Name, problem)
				return languageContentError{err: validate.WrapErrorWithField(reason, field), reason: reason.Error()}
			}
		}
	}
	return nil
}

// poolSets returns the sets of the pool templates that exist
func poolSets(sets map[uuid.UUID]*repository.TemplateLanguageSet, pool []uuid.UUID) map[uuid.UUID]*repository.TemplateLanguageSet {
	out := make(map[uuid.UUID]*repository.TemplateLanguageSet, len(pool))
	for _, id := range pool {
		if set, ok := sets[id]; ok {
			out[id] = set
		}
	}
	return out
}

// languageEmailProblem tells what keeps an email from being sent, empty when
// nothing does: it is gone, has no content or subject, or its body or subject
// does not parse. A failed read is returned as an error.
func (c *Campaign) languageEmailProblem(
	ctx context.Context,
	companyID *uuid.UUID,
	emailID *uuid.UUID,
) (string, error) {
	emails := &repository.Email{DB: c.CampaignRepository.DB}
	email, err := emails.GetByID(ctx, emailID, &repository.EmailOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "was not found", nil
	}
	if err != nil {
		return "", err
	}
	content, err := email.Content.Get()
	if err != nil {
		return "has no content", nil
	}
	funcs := TemplateFuncs()
	if c.TemplateService != nil {
		funcs = c.TemplateService.TemplateFuncsWithCompany(ctx, companyID)
	}
	if _, err := template.New("email").Funcs(funcs).Parse(content.String()); err != nil {
		return "does not parse", nil
	}
	subject, err := email.MailHeaderSubject.Get()
	if err != nil {
		return "has no subject", nil
	}
	if _, err := template.New("subject").Funcs(funcs).Parse(subject.String()); err != nil {
		return "has a subject that does not parse", nil
	}
	return "", nil
}

// moverLanguage is the language of a recipient a pool change moves, and the
// template it was given until now
type moverLanguage struct {
	stamp      string
	from       *uuid.UUID
	assignment repository.TemplateAssignment
}

// checkMoverLanguages refuses a pool change that moves a recipient to a
// template with no usable content in the recipient's language. A recipient
// with no stamp has the language of the template it was given until now.
func (c *Campaign) checkMoverLanguages(
	ctx context.Context,
	newPool []uuid.UUID,
	movers map[uuid.UUID]moverLanguage,
	targets ...map[uuid.UUID][]uuid.UUID,
) error {
	if len(movers) == 0 {
		return nil
	}
	ids := append([]uuid.UUID{}, newPool...)
	for _, m := range movers {
		if m.from != nil {
			ids = append(ids, *m.from)
		}
	}
	sets, err := c.CampaignTemplateService.CampaignTemplateRepository.GetLanguageSetsByIDs(ctx, ids)
	if err != nil {
		return err
	}
	for _, byTarget := range targets {
		for target, crIDs := range byTarget {
			set, ok := sets[target]
			if !ok {
				continue
			}
			for _, crID := range crIDs {
				m, ok := movers[crID]
				if !ok {
					continue
				}
				code := m.stamp
				if code == "" && m.from != nil {
					if from, ok := sets[*m.from]; ok {
						code = from.Language
					}
				}
				if code == "" || set.CanSend(code) {
					continue
				}
				return validate.WrapErrorWithField(
					fmt.Errorf(
						"recipients in %s can not move to template '%s', it has no usable %s",
						data.LanguageName(code), set.Name, data.LanguageName(code),
					),
					"templateIDs",
				)
			}
		}
	}
	return nil
}
