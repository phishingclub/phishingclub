package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// programUsableKey is a template and the language it is sent in. An empty
// language is the template's own content.
type programUsableKey struct {
	templateID uuid.UUID
	language   string
	// company is the company of the program, uuid.Nil for a global one. The
	// email is parsed with the template functions of that company, the same
	// as the sender does.
	company uuid.UUID
}

// programLanguages is the language routing of one program and the language
// sets of its step templates, read once per tick, see campaignLanguages.
// read holds the templates already looked up, also the ones that no longer
// exist, so a missing template is not read again.
type programLanguages struct {
	*campaignLanguages
	read      map[uuid.UUID]bool
	updatedAt time.Time
}

// templateRepository returns the template repository of the program service
func (s *Program) templateRepository() *repository.CampaignTemplate {
	if s.CampaignTemplateService != nil && s.CampaignTemplateService.CampaignTemplateRepository != nil {
		return s.CampaignTemplateService.CampaignTemplateRepository
	}
	return &repository.CampaignTemplate{DB: s.ProgramRepository.DB}
}

// stepTemplateIDs returns the template of every step, each once
func stepTemplateIDs(steps []*model.ProgramStep) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	for _, st := range steps {
		if st == nil {
			continue
		}
		id, err := st.CampaignTemplateID.Get()
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// programRouting returns the route field, the default and the routes of a
// program. Routes that are not set on the program are read from the
// database. With routing off nothing is read.
func (s *Program) programRouting(
	ctx context.Context,
	program *model.Program,
) (routeBy string, fallback string, routes map[string]string, err error) {
	routeBy = program.LanguageRouteField()
	if routeBy == "" {
		return "", "", map[string]string{}, nil
	}
	if v, err := program.LanguageDefault.Get(); err == nil {
		fallback = v
	}
	if v, err := program.LanguageRoutes.Get(); err == nil && v != nil {
		return routeBy, fallback, v, nil
	}
	routes = map[string]string{}
	if id, err := program.ID.Get(); err == nil {
		routes, err = s.ProgramRepository.GetLanguageRoutesByProgramID(ctx, &id)
		if err != nil {
			return "", "", nil, errs.Wrap(err)
		}
	}
	return routeBy, fallback, routes, nil
}

// programLanguagesFor reads the routing of a program and the language rows
// of its step templates: one read for the routes and one for the templates
// and their rows
func (s *Program) programLanguagesFor(ctx context.Context, program *model.Program) (*programLanguages, error) {
	routeBy, fallback, routes, err := s.programRouting(ctx, program)
	if err != nil {
		return nil, err
	}
	l := &programLanguages{
		campaignLanguages: &campaignLanguages{
			routeBy:  routeBy,
			fallback: fallback,
			routes:   routes,
			sets:     map[uuid.UUID]*repository.TemplateLanguageSet{},
			memo:     map[string]string{},
		},
		read: map[uuid.UUID]bool{},
	}
	if program.UpdatedAt != nil {
		l.updatedAt = *program.UpdatedAt
	}
	if err := s.fillLanguageSets(ctx, l, program.Steps); err != nil {
		return nil, err
	}
	return l, nil
}

// fillLanguageSets reads the language sets of the step templates not read
// yet, in one query for the templates and one for their rows
func (s *Program) fillLanguageSets(ctx context.Context, l *programLanguages, steps []*model.ProgramStep) error {
	missing := []uuid.UUID{}
	for _, id := range stepTemplateIDs(steps) {
		if !l.read[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sets, err := s.templateRepository().GetLanguageSetsByIDs(ctx, missing)
	if err != nil {
		return errs.Wrap(err)
	}
	for _, id := range missing {
		l.read[id] = true
		if set, ok := sets[id]; ok {
			l.sets[id] = set
		}
	}
	// routing by the Language field looks at every step template, so the
	// languages picked before a template was added are picked again
	if l.routeBy == model.LanguageRouteLanguage {
		l.memo = map[string]string{}
	}
	return nil
}

// tickLanguages returns the language state of a program for this tick,
// reading it on first use. A program changed since it was read is read
// again, and step templates added since are read on their own.
func (s *Program) tickLanguages(
	ctx context.Context,
	languages map[uuid.UUID]*programLanguages,
	program *model.Program,
) (*programLanguages, error) {
	id := program.ID.MustGet()
	l, ok := languages[id]
	if ok && program.UpdatedAt != nil && !program.UpdatedAt.Equal(l.updatedAt) {
		ok = false
	}
	if !ok {
		var err error
		if l, err = s.programLanguagesFor(ctx, program); err != nil {
			return nil, err
		}
		languages[id] = l
		return l, nil
	}
	if err := s.fillLanguageSets(ctx, l, program.Steps); err != nil {
		return nil, err
	}
	return l, nil
}

// routing reports whether the program picks languages by a field
func (l *programLanguages) routing() bool {
	return l != nil && l.routeBy != ""
}

// programPersonLanguage is a person being advanced and their language. With
// routing on the language is decided from the person before any step is
// picked, so every step is checked in the language the person gets.
type programPersonLanguage struct {
	langs     *programLanguages
	recipient *model.Recipient
}

// stamp returns the language a new send of templateID is stamped with: the
// person's language with routing on, the template language with it off, and
// no stamp when the template has no language
func (p *programPersonLanguage) stamp(templateID uuid.UUID) nullable.Nullable[string] {
	if p == nil || p.langs == nil {
		return nullable.Nullable[string]{}
	}
	return p.langs.languageFor(p.recipient, templateID)
}

// stampPtr is stamp as a pointer, nil for no stamp
func (p *programPersonLanguage) stampPtr(templateID uuid.UUID) *string {
	if v, err := p.stamp(templateID).Get(); err == nil && v != "" {
		return &v
	}
	return nil
}

// usableKey returns the key a send of templateID is checked by and the
// language set of the template. A language that is the template language is
// the template's own content and has the empty language.
func (p *programPersonLanguage) usableKey(templateID uuid.UUID) (programUsableKey, *repository.TemplateLanguageSet) {
	key := programUsableKey{templateID: templateID}
	if p == nil || p.langs == nil {
		return key, nil
	}
	set := p.langs.sets[templateID]
	code, err := p.stamp(templateID).Get()
	if err != nil || code == "" || (set != nil && code == set.Language) {
		return key, set
	}
	key.language = code
	return key, set
}

// programPersonFor decides the language state of the person of an
// enrollment. With routing on the person is taken from the batch read ahead,
// or read on their own when the batch read failed. A person that no longer
// exists gets the default language, the advance then lets them leave.
func (s *Program) programPersonFor(
	ctx context.Context,
	langs *programLanguages,
	e *database.ProgramEnrollment,
	w *programWrites,
) (*programPersonLanguage, error) {
	person := &programPersonLanguage{langs: langs}
	if !langs.routing() || e.RecipientID == nil {
		return person, nil
	}
	if w != nil {
		if r := w.recipients[*e.RecipientID]; r != nil {
			person.recipient = r
			return person, nil
		}
	}
	r, err := s.RecipientRepository.GetByID(ctx, e.RecipientID, &repository.RecipientOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return person, nil
	}
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if w != nil {
		w.recipients[*e.RecipientID] = r
	}
	person.recipient = r
	return person, nil
}

// languageChecker returns a campaign service that can check language routing
// against templates, see Campaign.checkPoolLanguages
func (s *Program) languageChecker() *Campaign {
	c := s.CampaignService
	if c != nil && c.CampaignTemplateService != nil && c.CampaignTemplateService.CampaignTemplateRepository != nil &&
		c.CampaignRepository != nil {
		return c
	}
	checker := &Campaign{
		CampaignRepository:      &repository.Campaign{DB: s.ProgramRepository.DB},
		CampaignTemplateService: &CampaignTemplate{CampaignTemplateRepository: s.templateRepository()},
	}
	if c != nil {
		checker.Common = c.Common
		checker.TemplateService = c.TemplateService
	}
	return checker
}

// checkProgramLanguages refuses language routing that can pick a language
// one of the step templates can not send, the same check a campaign makes on
// its pool, see Campaign.checkPoolLanguages. With the Language field the
// languages the members of groupIDs have count. The error names the template
// and the language, never a route value.
func (s *Program) checkProgramLanguages(
	ctx context.Context,
	companyID *uuid.UUID,
	routeBy string,
	fallback string,
	routes map[string]string,
	steps []*model.ProgramStep,
	groupIDs []*uuid.UUID,
) error {
	if routeBy == "" {
		return nil
	}
	var reachable []string
	if routeBy == model.LanguageRouteLanguage {
		var err error
		if reachable, err = groupLanguageCodes(ctx, s.RecipientGroupRepository, groupIDs); err != nil {
			return errs.Wrap(err)
		}
	}
	_, err := s.languageChecker().checkPoolLanguages(ctx, companyID, routeBy, fallback, routes, stepTemplateIDs(steps), reachable)
	return err
}

// mergeProgramRouting fills the language fields an update leaves out with
// the stored ones when it sets any of them, and checks the result, so a
// request can change one field. Routes keyed on a field that the update
// replaces are cleared when the request sends no routes. Only the number of
// cleared routes is logged, the keys are recipient values.
func (s *Program) mergeProgramRouting(
	ctx context.Context,
	existing *model.Program,
	incoming *model.Program,
	ae *AuditEvent,
) error {
	if !incoming.LanguageRouteBy.IsSpecified() && !incoming.LanguageDefault.IsSpecified() && !incoming.LanguageRoutes.IsSpecified() {
		return nil
	}
	if !incoming.LanguageRouteBy.IsSpecified() {
		incoming.LanguageRouteBy = nullable.NewNullableWithValue(existing.LanguageRouteField())
	}
	if !incoming.LanguageDefault.IsSpecified() {
		fallback := ""
		if v, err := existing.LanguageDefault.Get(); err == nil {
			fallback = v
		}
		incoming.LanguageDefault = nullable.NewNullableWithValue(fallback)
	}
	if !incoming.LanguageRoutes.IsSpecified() && incoming.LanguageRouteField() != existing.LanguageRouteField() {
		id := existing.ID.MustGet()
		stored, err := s.ProgramRepository.GetLanguageRoutesByProgramID(ctx, &id)
		if err != nil {
			return errs.Wrap(err)
		}
		incoming.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{})
		if len(stored) > 0 {
			ae.Details["languageRoutesCleared"] = len(stored)
		}
	}
	return incoming.ValidateLanguageRouting()
}

// programUpdateRouting returns the routing a program has after an update:
// the fields of the request, the stored ones for a field it leaves out. It
// reports whether the request changes the routing.
func (s *Program) programUpdateRouting(
	ctx context.Context,
	existing *model.Program,
	incoming *model.Program,
) (routeBy string, fallback string, routes map[string]string, specified bool, err error) {
	specified = incoming.LanguageRouteBy.IsSpecified() || incoming.LanguageDefault.IsSpecified() ||
		incoming.LanguageRoutes.IsSpecified()
	routeBy = existing.LanguageRouteField()
	if incoming.LanguageRouteBy.IsSpecified() {
		routeBy = incoming.LanguageRouteField()
	}
	if routeBy == "" {
		return "", "", map[string]string{}, specified, nil
	}
	if v, err := existing.LanguageDefault.Get(); err == nil {
		fallback = v
	}
	if incoming.LanguageDefault.IsSpecified() {
		fallback = ""
		if v, err := incoming.LanguageDefault.Get(); err == nil {
			fallback = v
		}
	}
	if incoming.LanguageRoutes.IsSpecified() {
		routes = map[string]string{}
		if v, err := incoming.LanguageRoutes.Get(); err == nil && v != nil {
			routes = v
		}
		return routeBy, fallback, routes, specified, nil
	}
	id := existing.ID.MustGet()
	routes, err = s.ProgramRepository.GetLanguageRoutesByProgramID(ctx, &id)
	if err != nil {
		return "", "", nil, specified, errs.Wrap(err)
	}
	return routeBy, fallback, routes, specified, nil
}

// auditLanguageRouting adds the language routing of a program to an audit
// event: the field, the default and how many values are routed, never the
// values, which are recipient data
func auditLanguageRouting(ae *AuditEvent, routeBy string, fallback string, routes map[string]string) {
	ae.Details["languageRouteBy"] = routeBy
	ae.Details["languageDefault"] = fallback
	ae.Details["languageRouteCount"] = len(routes)
}

// programLanguageCheck is what the program page needs to warn about the
// languages a program picks that a step template can not send
type programLanguageCheck struct {
	codes []string
	sets  map[uuid.UUID]*repository.TemplateLanguageSet
}

// programLanguageCheckOrNil reads the languages a program picks and the
// language rows of its step templates. It is nil when the program picks no
// language by a field or the read failed.
func (s *Program) programLanguageCheckOrNil(ctx context.Context, program *model.Program) *programLanguageCheck {
	if program.LanguageRouteField() == "" {
		return nil
	}
	routeBy, fallback, routes, err := s.programRouting(ctx, program)
	if err != nil {
		s.Logger.Warnw("failed to read the program language routes", "error", err)
		return nil
	}
	if routeBy == model.LanguageRouteLanguage {
		// only the default must be sent by every template, other languages
		// fall back to it per person
		routes = map[string]string{}
	}
	sets, err := s.templateRepository().GetLanguageSetsByIDs(ctx, stepTemplateIDs(program.Steps))
	if err != nil {
		s.Logger.Warnw("failed to read the languages of the program templates", "error", err)
		return nil
	}
	return &programLanguageCheck{codes: routedCodes(fallback, routes), sets: sets}
}

// languageWarningsFrom lists one warning per language a step template can
// not send while the program picks it. A template that is gone or can not be
// sent at all is warned about on its own and left out here.
func languageWarningsFrom(
	program *model.Program,
	templates map[uuid.UUID]repository.ProgramTemplateInfo,
	languages *programLanguageCheck,
) []string {
	warnings := []string{}
	if languages == nil || len(languages.codes) == 0 {
		return warnings
	}
	codes := append([]string{}, languages.codes...)
	sort.Slice(codes, func(i, j int) bool {
		return data.LanguageName(codes[i]) < data.LanguageName(codes[j])
	})
	for _, templateID := range stepTemplateIDs(program.Steps) {
		t, ok := templates[templateID]
		if !ok || !t.IsUsable {
			continue
		}
		set, ok := languages.sets[templateID]
		if !ok {
			continue
		}
		for _, code := range codes {
			if set.CanSend(code) {
				continue
			}
			warnings = append(warnings, data.LanguageName(code)+" of template '"+t.Name+
				"' can not be sent, people routed to it wait")
		}
	}
	return warnings
}
