package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// newProgramLanguageTest is a program sender test where the program service
// reads templates, emails and language rows the way the server wires it
func newProgramLanguageTest(t *testing.T) *programTest {
	t.Helper()
	p := newProgramSenderTest(t)
	withProgramLanguageServices(p)
	return p
}

// withProgramLanguageServices gives the program service what it reads to
// check templates in a language
func withProgramLanguageServices(p *programTest) {
	cs := p.svc.CampaignService
	p.svc.CampaignTemplateService = cs.CampaignTemplateService
	if cs.MailService == nil {
		cs.MailService = &Email{Common: p.svc.Common, EmailRepository: &repository.Email{DB: p.db}}
	}
	if cs.TemplateService == nil {
		cs.TemplateService = &Template{Common: p.svc.Common}
	}
}

// languageEmail stores an email that parses, with a marker in its body
func (p *programTest) languageEmail(marker string) uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	p.create(&database.Email{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "email " + marker + " " + id.String()[:8],
		Content: "body-" + marker + " {{.FirstName}}", MailFrom: "bounce@" + marker + ".example.com",
		From: "Sender <sender@" + marker + ".example.com>", Subject: "subject " + marker})
	return id
}

// languagePage stores a page
func (p *programTest) languagePage() uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	p.create(&database.Page{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "page " + id.String()[:8], Content: "<p>page</p>"})
	return id
}

// languageTemplate stores a usable template named name written in language,
// with an email and a landing page, and one usable language row per code
func (p *programTest) languageTemplate(name string, language string, codes ...string) uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	id, urlID := uuid.New(), uuid.New()
	emailID, pageID := p.languageEmail(language), p.languagePage()
	p.create(&database.CampaignTemplate{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: name, URLIdentifierID: &urlID,
		EmailID: &emailID, LandingPageID: &pageID, IsUsable: true, Language: language,
	})
	for _, code := range codes {
		p.languageTemplateRow(id, code, p.languageEmail(code), true)
	}
	return id
}

// languageTemplateRow stores a language row of a template
func (p *programTest) languageTemplateRow(templateID uuid.UUID, code string, emailID uuid.UUID, usable bool) {
	p.t.Helper()
	now := time.Now().UTC()
	rowID, pageID := uuid.New(), p.languagePage()
	p.create(&database.CampaignTemplateLanguage{
		ID: &rowID, CreatedAt: &now, UpdatedAt: &now, CampaignTemplateID: &templateID,
		Language: code, EmailID: &emailID, LandingPageID: &pageID, IsUsable: usable,
	})
}

// templateStep adds a step of the template to the program
func (p *programTest) templateStep(programID uuid.UUID, templateID uuid.UUID, position int) uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	p.create(&database.ProgramStep{ID: &id, CreatedAt: &now, UpdatedAt: &now,
		ProgramID: &programID, CampaignTemplateID: &templateID, Position: position})
	return id
}

// routeProgram stores the language routing of a program, with keys as an
// operator types them
func (p *programTest) routeProgram(programID uuid.UUID, routeBy string, fallback string, routes map[string]string) {
	p.t.Helper()
	p.setProgram(programID, map[string]any{"language_route_by": routeBy, "language_default": fallback,
		"updated_at": time.Now().UTC()})
	p.db.Where("program_id = ?", programID).Delete(&database.ProgramLanguageRoute{})
	for raw, code := range routes {
		id := programID
		p.create(&database.ProgramLanguageRoute{ProgramID: &id, MatchKey: model.NormalizeLanguageKey(raw), Language: code})
	}
}

// countryRecipient stores a person with a country
func (p *programTest) countryRecipient(email string, country string) uuid.UUID {
	p.t.Helper()
	id := p.recipient(email)
	p.db.Model(&database.Recipient{}).Where("id = ?", id).Update("country", country)
	return id
}

// stampOf returns the language of the one send of a person in a campaign,
// "-" for no stamp
func (p *programTest) stampOf(campaignID, recipientID uuid.UUID) string {
	p.t.Helper()
	rows := p.sendsOf(campaignID, recipientID)
	if len(rows) != 1 {
		p.t.Fatalf("want one send, got %d", len(rows))
	}
	if rows[0].Language == nil {
		return "-"
	}
	return *rows[0].Language
}

// advanceAtDue advances a person at the next due time stored for them
func (p *programTest) advanceAtDue(programID uuid.UUID, enrollmentID *uuid.UUID) {
	p.t.Helper()
	row := p.enrollment(enrollmentID)
	if row.NextDueAt == nil {
		p.t.Fatal("the person has no next send")
	}
	if err := p.svc.advanceEnrollment(p.ctx, nil, p.loadProgram(programID), &row, *row.NextDueAt, nil, nil); err != nil {
		p.t.Fatalf("advance: %v", err)
	}
}

// TestProgramStampsRoutedLanguage checks the batch and the single advance
// stamp every send with the person's language: a routed value after
// normalizing, the default for an unmatched or empty value
func TestProgramStampsRoutedLanguage(t *testing.T) {
	p := newProgramLanguageTest(t)
	programID := p.program(model.ProgramLoopComplete)
	templateID := p.languageTemplate("lure", "en", "da", "de")
	stepID := p.templateStep(programID, templateID, 0)
	campaignID := p.stepCampaign(programID, stepID, 0)
	p.routeProgram(programID, model.LanguageRouteCountry, "en", map[string]string{"Denmark": "da", "Germany": "de"})

	people := map[string]uuid.UUID{
		"da": p.countryRecipient("dane@example.com", "  DENMARK "),
		"de": p.countryRecipient("german@example.com", "Germany"),
		"en": p.countryRecipient("french@example.com", "France"),
		"":   p.countryRecipient("nobody@example.com", ""),
	}
	for _, id := range people {
		p.enroll(programID, id, 0)
	}
	if _, err := p.svc.advanceDueBatch(p.ctx, authorizedSession(t), time.Now()); err != nil {
		t.Fatalf("advance batch: %v", err)
	}
	for want, id := range people {
		if want == "" {
			want = "en"
		}
		if got := p.stampOf(campaignID, id); got != want {
			t.Errorf("batch: want %s, got %s", want, got)
		}
	}

	// the single advance reads the person on its own
	single := p.countryRecipient("single@example.com", "germany")
	e := p.enroll(programID, single, 0)
	p.advance(programID, e)
	if got := p.stampOf(campaignID, single); got != "de" {
		t.Fatalf("single advance: want de, got %s", got)
	}
}

// TestProgramRoutingOffStampsTemplateLanguage checks a program that picks no
// language stamps the template language, and no stamp when the template has
// none, like a campaign
func TestProgramRoutingOffStampsTemplateLanguage(t *testing.T) {
	p := newProgramLanguageTest(t)
	programID := p.program(model.ProgramLoopComplete)
	german := p.languageTemplate("german lure", "de", "en")
	stepGerman := p.templateStep(programID, german, 0)
	stepPlain := p.step(programID, 1, true)
	campaignGerman := p.stepCampaign(programID, stepGerman, 0)
	campaignPlain := p.stepCampaign(programID, stepPlain, 0)
	alice := p.countryRecipient("alice@example.com", "Denmark")
	e := p.enroll(programID, alice, 0)
	p.advance(programID, e)
	if got := p.stampOf(campaignGerman, alice); got != "de" {
		t.Fatalf("want the template language, got %s", got)
	}
	p.advanceAtDue(programID, e.ID)
	if got := p.stampOf(campaignPlain, alice); got != "-" {
		t.Fatalf("a template with no language gives no stamp, got %s", got)
	}
}

// TestProgramLanguageChangesBetweenSteps checks each send gets the language
// decided when it is planned: a stamp never changes once planned, and a
// person whose field changed gets the new language on their next step
func TestProgramLanguageChangesBetweenSteps(t *testing.T) {
	p := newProgramLanguageTest(t)
	programID := p.program(model.ProgramLoopComplete)
	first := p.languageTemplate("first", "en", "da", "de")
	second := p.languageTemplate("second", "de", "da", "en")
	stepFirst := p.templateStep(programID, first, 0)
	stepSecond := p.templateStep(programID, second, 1)
	campaignFirst := p.stepCampaign(programID, stepFirst, 0)
	campaignSecond := p.stepCampaign(programID, stepSecond, 0)
	p.routeProgram(programID, model.LanguageRouteCountry, "en", map[string]string{"Denmark": "da", "Germany": "de"})
	alice := p.countryRecipient("alice@example.com", "Germany")
	e := p.enroll(programID, alice, 0)
	p.advance(programID, e)
	if got := p.stampOf(campaignFirst, alice); got != "de" {
		t.Fatalf("first step: want de, got %s", got)
	}
	p.db.Model(&database.Recipient{}).Where("id = ?", alice).Update("country", "Denmark")
	p.advanceAtDue(programID, e.ID)
	if got := p.stampOf(campaignSecond, alice); got != "da" {
		t.Fatalf("second step: want da, got %s", got)
	}
	if got := p.stampOf(campaignFirst, alice); got != "de" {
		t.Fatalf("a planned send keeps its stamp, got %s", got)
	}
}

// TestProgramUsabilityKeyedByLanguage checks a template is usable per
// language: its own content, a usable row, and not a row that is not usable,
// a missing row or a row whose email does not parse, each kept apart in the
// batch cache
func TestProgramUsabilityKeyedByLanguage(t *testing.T) {
	p := newProgramLanguageTest(t)
	templateID := p.languageTemplate("lure", "en", "de")
	p.languageTemplateRow(templateID, "fr", p.languageEmail("fr"), false)
	brokenEmail := p.languageEmail("da")
	p.db.Model(&database.Email{}).Where("id = ?", brokenEmail).Update("content", "Hej {{.FirstName")
	p.languageTemplateRow(templateID, "da", brokenEmail, true)

	cache := map[programUsableKey]bool{}
	for code, want := range map[string]bool{"": true, "de": true, "fr": false, "es": false, "da": false} {
		got, err := p.svc.templateUsable(p.ctx, programUsableKey{templateID: templateID, language: code}, nil, cache)
		if err != nil {
			t.Fatalf("usable %q: %v", code, err)
		}
		if got != want {
			t.Errorf("language %q: want %v, got %v", code, want, got)
		}
	}
	if len(cache) != 5 || !cache[programUsableKey{templateID: templateID}] || cache[programUsableKey{templateID: templateID, language: "fr"}] {
		t.Fatalf("the cache must hold each language on its own, got %v", cache)
	}
	// a program of a company parses with that company's template functions,
	// so its answer is kept apart from the global one
	companyKey := programUsableKey{templateID: templateID, company: uuid.New()}
	if got, err := p.svc.templateUsable(p.ctx, companyKey, nil, cache); err != nil || !got {
		t.Fatalf("usable for a company: %v %v", got, err)
	}
	if len(cache) != 6 || !cache[companyKey] {
		t.Fatalf("the cache must keep the company answer on its own, got %v", cache)
	}
}

// TestProgramBrokenLanguagePicksAnotherStep checks a person routed to a
// language one template can not send is given another step, while people of
// the other languages still get that template
func TestProgramBrokenLanguagePicksAnotherStep(t *testing.T) {
	p := newProgramLanguageTest(t)
	programID := p.program(model.ProgramLoopComplete)
	first := p.languageTemplate("first", "en")
	p.languageTemplateRow(first, "de", p.languageEmail("de"), false)
	second := p.languageTemplate("second", "en", "de")
	campaignFirst := p.stepCampaign(programID, p.templateStep(programID, first, 0), 0)
	campaignSecond := p.stepCampaign(programID, p.templateStep(programID, second, 1), 0)
	p.routeProgram(programID, model.LanguageRouteCountry, "en", map[string]string{"Germany": "de"})
	alice := p.countryRecipient("alice@example.com", "")
	bob := p.countryRecipient("bob@example.com", "Germany")
	p.enroll(programID, alice, 0)
	p.enroll(programID, bob, 0)
	if _, err := p.svc.advanceDueBatch(p.ctx, authorizedSession(t), time.Now()); err != nil {
		t.Fatalf("advance batch: %v", err)
	}
	if got := p.stampOf(campaignFirst, alice); got != "en" {
		t.Fatalf("alice must get the first template in English, got %s", got)
	}
	if len(p.sendsOf(campaignFirst, bob)) != 0 {
		t.Fatal("bob must not be planned onto a language the template can not send")
	}
	if got := p.stampOf(campaignSecond, bob); got != "de" {
		t.Fatalf("bob must get the second template in German, got %s", got)
	}
}

// TestProgramSenderHoldsOnlyBrokenLanguage walks a program send through a
// language that breaks after it was planned: the sender holds only the rows
// of that language and plans their people again, the other languages are
// sent and their people left alone. The held person gets another step, and
// once the language is fixed the held step is given again, its row put back
// with the language decided at that time.
func TestProgramSenderHoldsOnlyBrokenLanguage(t *testing.T) {
	f := newLanguageSendFixture(t)
	p := f.programTest
	withProgramLanguageServices(p)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	stepFirst := p.templateStep(programID, f.template, 0)
	second := p.languageTemplate("second", "en", "de", "fr")
	stepSecond := p.templateStep(programID, second, 1)
	campaignFirst := p.stepCampaign(programID, stepFirst, 0)
	campaignSecond := p.stepCampaign(programID, stepSecond, 0)
	p.routeProgram(programID, model.LanguageRouteCountry, "en", map[string]string{"Germany": "de", "France": "fr"})
	alice := p.countryRecipient("alice@example.com", "")
	bob := p.countryRecipient("bob@example.com", "Germany")
	carol := p.countryRecipient("carol@example.com", "France")
	enrollments := map[uuid.UUID]*database.ProgramEnrollment{}
	for _, id := range []uuid.UUID{alice, bob, carol} {
		enrollments[id] = p.enroll(programID, id, 0)
	}
	if _, err := p.svc.advanceDueBatch(p.ctx, session, time.Now()); err != nil {
		t.Fatalf("advance batch: %v", err)
	}
	for id, want := range map[uuid.UUID]string{alice: "en", bob: "de", carol: "fr"} {
		if got := p.stampOf(campaignFirst, id); got != want {
			t.Fatalf("want %s, got %s", want, got)
		}
	}
	before := map[uuid.UUID]database.ProgramEnrollment{}
	for id, e := range enrollments {
		before[id] = p.enrollment(e.ID)
	}

	// French breaks after it was planned
	f.db.Model(&database.Email{}).Where("id = ?", f.frEmail).Update("content", "Bonjour {{.FirstName")
	f.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignFirst).
		Update("send_at", time.Now().UTC().Add(-time.Minute))
	if err := f.sendDue(campaignFirst); err != nil {
		t.Fatalf("a held language is no error: %v", err)
	}
	if conns, messages := f.smtp.counts(); conns != 1 || len(messages) != 2 {
		t.Fatalf("want English and German sent on one connection, got %d on %d", len(messages), conns)
	}
	held := p.sendsOf(campaignFirst, carol)[0]
	if held.CancelledAt == nil || held.SentAt != nil {
		t.Fatalf("the French send must be held, cancelled %v sent %v", held.CancelledAt, held.SentAt)
	}
	now := time.Now()
	carolRow := p.enrollment(enrollments[carol].ID)
	if carolRow.NextDueAt == nil || carolRow.NextDueAt.Before(now.Add(programHoldRetryDelay-time.Minute)) {
		t.Fatalf("carol must be due again after the hold wait, got %v", carolRow.NextDueAt)
	}
	for _, id := range []uuid.UUID{alice, bob} {
		row := p.enrollment(enrollments[id].ID)
		if !sameTime(row.NextDueAt, before[id].NextDueAt) || row.Position != before[id].Position {
			t.Fatalf("a person of a language that was sent must not be planned again")
		}
	}

	// carol gets the other step in French, the broken one is skipped
	due := *carolRow.NextDueAt
	if err := p.svc.advanceEnrollment(p.ctx, session, p.loadProgram(programID), &carolRow, due, nil, nil); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if got := p.stampOf(campaignSecond, carol); got != "fr" {
		t.Fatalf("carol must get the other step in French, got %s", got)
	}
	if rows := p.sendsOf(campaignFirst, carol); len(rows) != 1 || rows[0].CancelledAt == nil {
		t.Fatal("the held French row must stay held")
	}

	// fixed, and carol moved to Germany: the held step is given again with
	// the language decided now
	f.db.Model(&database.Email{}).Where("id = ?", f.frEmail).Update("content", "Bonjour {{.FirstName}}")
	f.db.Model(&database.Recipient{}).Where("id = ?", carol).Update("country", "Germany")
	carolRow = p.enrollment(enrollments[carol].ID)
	next := *carolRow.NextDueAt
	if err := p.svc.advanceEnrollment(p.ctx, session, p.loadProgram(programID), &carolRow, next, nil, nil); err != nil {
		t.Fatalf("advance: %v", err)
	}
	revived := p.campaignRecipient(*held.ID)
	if revived.CancelledAt != nil || revived.Language == nil || *revived.Language != "de" {
		t.Fatalf("the held row must be back in the queue in German, cancelled %v language %v",
			revived.CancelledAt, revived.Language)
	}
}

// TestProgramBatchReviveStampsLanguage checks the batch write puts a
// cancelled row back with the language decided now
func TestProgramBatchReviveStampsLanguage(t *testing.T) {
	p := newProgramLanguageTest(t)
	programID := p.program(model.ProgramLoopComplete)
	templateID := p.languageTemplate("lure", "en", "de")
	campaignID := p.stepCampaign(programID, p.templateStep(programID, templateID, 0), 0)
	p.routeProgram(programID, model.LanguageRouteCountry, "en", map[string]string{"Germany": "de"})
	alice := p.countryRecipient("alice@example.com", "Germany")
	p.enroll(programID, alice, 0)
	cancelled := p.send(campaignID, alice, false)
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", cancelled).
		Updates(map[string]any{"cancelled_at": time.Now().UTC(), "language": "en"})
	if _, err := p.svc.advanceDueBatch(p.ctx, authorizedSession(t), time.Now()); err != nil {
		t.Fatalf("advance batch: %v", err)
	}
	row := p.campaignRecipient(cancelled)
	if row.CancelledAt != nil || row.Language == nil || *row.Language != "de" {
		t.Fatalf("the revived row must be stamped again, cancelled %v language %v", row.CancelledAt, row.Language)
	}
}

// programLanguageRequest is an update request that sets the routing only,
// with the name every update carries
func (p *programTest) programLanguageRequest(id uuid.UUID, routeBy string, fallback string, routes map[string]string) *model.Program {
	return &model.Program{
		Name:            p.loadProgram(id).Name,
		LanguageRouteBy: nullable.NewNullableWithValue(routeBy),
		LanguageDefault: nullable.NewNullableWithValue(fallback),
		LanguageRoutes:  nullable.NewNullableWithValue(routes),
	}
}

// TestProgramRoutesValidatedAgainstSteps checks create and update refuse a
// default or a route to a language a step template lacks or can not send,
// naming the template and the language and never a route value, also when a
// step template is added, and that a route edit changes no planned send
func TestProgramRoutesValidatedAgainstSteps(t *testing.T) {
	p := newProgramLanguageTest(t)
	session := authorizedSession(t)
	full := p.languageTemplate("full lure", "en", "de")
	englishOnly := p.languageTemplate("english lure", "en")
	notUsable := p.languageTemplate("draft lure", "en")
	p.languageTemplateRow(notUsable, "de", p.languageEmail("de"), false)
	noLanguage := p.languageTemplate("plain lure", "")

	refused := func(err error, template string, language string) {
		t.Helper()
		if err == nil {
			t.Fatalf("want a refusal naming %s and %s", template, language)
		}
		msg := err.Error()
		if !strings.Contains(msg, template) || !strings.Contains(msg, language) {
			t.Fatalf("the refusal must name %s and %s, got %v", template, language, err)
		}
		if strings.Contains(strings.ToLower(msg), "secretland") {
			t.Fatalf("a route value must never be named, got %v", err)
		}
	}
	create := func(name string, templates []uuid.UUID, fallback string, routes map[string]string) (*uuid.UUID, error) {
		req := newProgramRequest(t, name, templates[0], p.group(name+" group"))
		req.Steps = []*model.ProgramStep{}
		for _, id := range templates {
			req.Steps = append(req.Steps, &model.ProgramStep{CampaignTemplateID: nullable.NewNullableWithValue(id)})
		}
		req.LanguageRouteBy = nullable.NewNullableWithValue(model.LanguageRouteCountry)
		req.LanguageDefault = nullable.NewNullableWithValue(fallback)
		req.LanguageRoutes = nullable.NewNullableWithValue(routes)
		return p.svc.Create(p.ctx, session, req)
	}
	_, err := create("lacks route", []uuid.UUID{full, englishOnly}, "en", map[string]string{"Secretland": "de"})
	refused(err, "english lure", "German")
	_, err = create("lacks default", []uuid.UUID{full}, "fr", map[string]string{})
	refused(err, "full lure", "French")
	_, err = create("not usable", []uuid.UUID{notUsable}, "en", map[string]string{"Secretland": "de"})
	refused(err, "draft lure", "German")
	_, err = create("no language", []uuid.UUID{noLanguage}, "en", map[string]string{})
	refused(err, "plain lure", "main language")
	id, err := create("works", []uuid.UUID{full}, "en", map[string]string{"Secretland": "de"})
	if err != nil {
		t.Fatalf("a routing every step template can send must be stored: %v", err)
	}

	// adding a step template that lacks a routed language is refused
	addStep := &model.Program{Name: p.loadProgram(*id).Name, Steps: []*model.ProgramStep{
		{CampaignTemplateID: nullable.NewNullableWithValue(full)},
		{CampaignTemplateID: nullable.NewNullableWithValue(englishOnly)},
	}}
	refused(p.svc.UpdateByID(p.ctx, session, id, addStep), "english lure", "German")
	refused(p.svc.UpdateByID(p.ctx, session, id,
		p.programLanguageRequest(*id, model.LanguageRouteCountry, "en", map[string]string{"Secretland": "fr"})), "full lure", "French")

	// a route edit changes only the sends planned from now on
	program := p.loadProgram(*id)
	stepID := program.Steps[0].ID.MustGet()
	campaignID := p.stepCampaign(*id, stepID, 0)
	alice := p.countryRecipient("alice@example.com", "Secretland")
	e := p.enroll(*id, alice, 0)
	queued := p.sendAt(campaignID, alice, time.Now().Add(time.Hour), false)
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", queued).Update("language", "de")
	beforeDue := p.enrollment(e.ID).NextDueAt
	if err := p.svc.UpdateByID(p.ctx, session, id,
		p.programLanguageRequest(*id, model.LanguageRouteCountry, "de", map[string]string{"Secretland": "en"})); err != nil {
		t.Fatalf("a valid route edit must be stored: %v", err)
	}
	if row := p.campaignRecipient(queued); row.Language == nil || *row.Language != "de" || row.CancelledAt != nil {
		t.Fatal("a route edit must not change a planned send")
	}
	if !sameTime(p.enrollment(e.ID).NextDueAt, beforeDue) {
		t.Fatal("a route edit must not plan people again")
	}
	routes, err := p.svc.ProgramRepository.GetLanguageRoutesByProgramID(p.ctx, id)
	if err != nil || routes["secretland"] != "en" {
		t.Fatalf("the new routes must be stored, got %v %v", routes, err)
	}

	// with routing off any step template can be added
	off := p.programLanguageRequest(*id, "", "", map[string]string{})
	off.Steps = addStep.Steps
	if err := p.svc.UpdateByID(p.ctx, session, id, off); err != nil {
		t.Fatalf("routing off must allow any template: %v", err)
	}
}

// TestProgramHiddenCampaignGetsNoRoutes checks the hidden campaign of a
// program that picks languages holds no routing of its own, the program
// stamps every send itself
func TestProgramHiddenCampaignGetsNoRoutes(t *testing.T) {
	p := newProgramLanguageTest(t)
	p.svc.PageRepository = &repository.Page{DB: p.db}
	programID := p.program(model.ProgramLoopComplete)
	templateID := p.languageTemplate("lure", "en", "de")
	p.templateStep(programID, templateID, 0)
	p.routeProgram(programID, model.LanguageRouteCountry, "en", map[string]string{"Germany": "de"})
	campaignID := p.stepCampaignOf(programID, 0)
	row := p.campaignRow(campaignID)
	if row.LanguageRouteBy != "" || row.LanguageDefault != "" {
		t.Fatalf("a hidden campaign must not pick languages, got %q %q", row.LanguageRouteBy, row.LanguageDefault)
	}
	var routes int64
	p.db.Model(&database.CampaignLanguageRoute{}).Where("campaign_id = ?", campaignID).Count(&routes)
	if routes != 0 {
		t.Fatalf("a hidden campaign must have no routes, got %d", routes)
	}
	// a settings copy to the hidden campaigns does not add them either
	if err := p.svc.syncProgramCampaignSettings(p.ctx, &programID); err != nil {
		t.Fatalf("sync settings: %v", err)
	}
	p.db.Model(&database.CampaignLanguageRoute{}).Where("campaign_id = ?", campaignID).Count(&routes)
	if row := p.campaignRow(campaignID); routes != 0 || row.LanguageRouteBy != "" {
		t.Fatal("a settings copy must not give a hidden campaign routes")
	}
}

// TestProgramBrokenLanguageWarnings checks the program page warns once per
// language a step template can not send, and the list counts the program as
// needing attention, by the reason and by the status filter
func TestProgramBrokenLanguageWarnings(t *testing.T) {
	p := newProgramLanguageTest(t)
	broken := p.program(model.ProgramLoopComplete)
	fine := p.program(model.ProgramLoopComplete)
	lure := p.languageTemplate("Invoice", "en", "da")
	p.languageTemplateRow(lure, "de", p.languageEmail("de"), false)
	p.templateStep(broken, lure, 0)
	p.templateStep(fine, lure, 0)
	p.routeProgram(broken, model.LanguageRouteCountry, "en", map[string]string{"Germany": "de", "Denmark": "da", "France": "fr"})
	p.routeProgram(fine, model.LanguageRouteCountry, "en", map[string]string{"Denmark": "da"})
	p.enroll(broken, p.recipient("a@example.com"), 0)
	p.enroll(fine, p.recipient("b@example.com"), 0)

	warnings := p.svc.templateWarnings(p.ctx, nil, p.loadProgram(broken))
	want := []string{
		"French of template 'Invoice' can not be sent, people routed to it wait",
		"German of template 'Invoice' can not be sent, people routed to it wait",
	}
	if strings.Join(warnings, "|") != strings.Join(want, "|") {
		t.Fatalf("want %v, got %v", want, warnings)
	}
	if got := p.svc.templateWarnings(p.ctx, nil, p.loadProgram(fine)); len(got) != 0 {
		t.Fatalf("a program whose languages all send has no warning, got %v", got)
	}

	now := time.Now()
	sums, err := p.svc.ProgramRepository.GetListSummaries(p.ctx, []*uuid.UUID{&broken, &fine})
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	usable := map[uuid.UUID]bool{lure: true}
	if reason := programAttentionReason(p.loadProgram(broken), sums[broken], usable, now); !strings.Contains(reason, "language") {
		t.Fatalf("a broken language needs attention, got %q", reason)
	}
	if reason := programAttentionReason(p.loadProgram(fine), sums[fine], usable, now); reason != "" {
		t.Fatalf("a program whose languages send needs no attention, got %q", reason)
	}
	listed := func(status string) map[uuid.UUID]bool {
		list, err := p.svc.ProgramRepository.GetAll(p.ctx, nil, &repository.ProgramOption{
			QueryArgs: &vo.QueryArgs{}, Status: status,
		}, now)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		out := map[uuid.UUID]bool{}
		for _, row := range list.Rows {
			out[row.ID.MustGet()] = true
		}
		return out
	}
	attention, active := listed(model.ProgramStatusNeedsAttention), listed(model.ProgramStatusActive)
	if !attention[broken] || active[broken] || attention[fine] || !active[fine] {
		t.Fatalf("the status filter must match the reason, attention %v active %v", attention, active)
	}

	// routing off: the language rows do not matter
	p.routeProgram(broken, "", "", nil)
	if got := p.svc.templateWarnings(p.ctx, nil, p.loadProgram(broken)); len(got) != 0 {
		t.Fatalf("routing off has no language warning, got %v", got)
	}
	if listed(model.ProgramStatusNeedsAttention)[broken] {
		t.Fatal("routing off must not need attention for a language")
	}
}

// TestProgramResultsSeeSchedulerStamps checks results by language count the
// sends by the stamp the scheduler wrote
func TestProgramResultsSeeSchedulerStamps(t *testing.T) {
	p := newProgramLanguageTest(t)
	programID := p.program(model.ProgramLoopComplete)
	templateID := p.languageTemplate("lure", "en", "de")
	stepID := p.templateStep(programID, templateID, 0)
	campaignID := p.stepCampaign(programID, stepID, 0)
	p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("campaign_template_id", templateID)
	p.routeProgram(programID, model.LanguageRouteCountry, "en", map[string]string{"Germany": "de"})
	for _, country := range []string{"Germany", "Germany", "France"} {
		p.enroll(programID, p.countryRecipient(uuid.NewString()+"@example.com", country), 0)
	}
	if _, err := p.svc.advanceDueBatch(p.ctx, authorizedSession(t), time.Now()); err != nil {
		t.Fatalf("advance batch: %v", err)
	}
	now := time.Now().UTC()
	p.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignID).Update("sent_at", now)
	g, err := p.svc.programLanguageGroups(p.ctx, p.loadProgram(programID), now)
	if err != nil {
		t.Fatalf("language groups: %v", err)
	}
	german, english := rowByName(g.Rows, "German"), rowByName(g.Rows, "English")
	if german == nil || english == nil || *german.Sent != 2 || *english.Sent != 1 {
		t.Fatalf("want 2 German and 1 English sends, got %+v", g.Rows)
	}
}

// TestAnonymousProgramStampsLanguage checks an anonymous program that picks
// languages stamps every send like a named one, and keeps the department of
// the anonymous snapshot read from the same batch read
func TestAnonymousProgramStampsLanguage(t *testing.T) {
	p := newProgramLanguageTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	templateID := p.languageTemplate("lure", "en", "de")
	campaignID := p.stepCampaign(programID, p.templateStep(programID, templateID, 0), 0)
	p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("is_anonymous", true)
	p.routeProgram(programID, model.LanguageRouteCountry, "en", map[string]string{"Germany": "de"})
	alice := p.countryRecipient("alice@example.com", "Germany")
	p.db.Model(&database.Recipient{}).Where("id = ?", alice).Update("department", "Sales")
	p.enroll(programID, alice, 0)
	if _, err := p.svc.advanceDueBatch(p.ctx, authorizedSession(t), time.Now()); err != nil {
		t.Fatalf("advance batch: %v", err)
	}
	rows := p.sendsOf(campaignID, alice)
	if len(rows) != 1 || rows[0].Language == nil || *rows[0].Language != "de" {
		t.Fatalf("want one send stamped de, got %+v", rows)
	}
	if rows[0].AnonymizedID == nil || rows[0].Department != "Sales" {
		t.Fatalf("an anonymous send keeps a pseudonym and the department, got %v %q", rows[0].AnonymizedID, rows[0].Department)
	}
}

// peopleLanguagesOf returns the language of each person on the people page,
// "-" when none
func (p *programTest) peopleLanguagesOf(programID uuid.UUID) (*ProgramRecipientsView, map[string]string) {
	p.t.Helper()
	view, err := p.svc.GetRecipientsView(p.ctx, authorizedSession(p.t), &programID, &vo.QueryArgs{Limit: 50})
	if err != nil {
		p.t.Fatalf("recipients view: %v", err)
	}
	out := map[string]string{}
	for _, row := range view.Rows {
		out[row.RecipientID] = "-"
		if row.Language != nil {
			out[row.RecipientID] = *row.Language
		}
	}
	return view, out
}

// TestProgramPeopleShowLanguage checks the people rows of a named program with
// Language by give the language of the queued send, then of the newest sent
// one, a send with no stamp in its template language, and no language with
// routing off
func TestProgramPeopleShowLanguage(t *testing.T) {
	p := newProgramLanguageTest(t)
	programID := p.program(model.ProgramLoopComplete)
	templateID := p.languageTemplate("lure", "en", "de")
	campaignID := p.stepCampaign(programID, p.templateStep(programID, templateID, 0), 0)
	p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("campaign_template_id", templateID)
	p.routeProgram(programID, model.LanguageRouteCountry, "en", map[string]string{"Germany": "de"})
	german := p.countryRecipient("german@example.com", "Germany")
	french := p.countryRecipient("french@example.com", "France")
	waiting := p.countryRecipient("waiting@example.com", "Germany")
	for _, id := range []uuid.UUID{german, french} {
		p.enroll(programID, id, 0)
	}
	// a person not planned yet has no send to read a language from
	e := p.enroll(programID, waiting, 0)
	later := time.Now().Add(24 * time.Hour)
	p.setEnrollment(e.ID, map[string]any{"next_due_at": later})
	if _, err := p.svc.advanceDueBatch(p.ctx, authorizedSession(t), time.Now()); err != nil {
		t.Fatalf("advance batch: %v", err)
	}

	// the queued sends
	_, got := p.peopleLanguagesOf(programID)
	if got[german.String()] != "de" || got[french.String()] != "en" || got[waiting.String()] != "-" {
		t.Fatalf("queued: want de, en and none, got %v", got)
	}

	// the sent ones, a send with no stamp reads its template language
	now := time.Now().UTC()
	p.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignID).Update("sent_at", now)
	p.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ? AND recipient_id = ?", campaignID, french).
		Update("language", nil)
	_, got = p.peopleLanguagesOf(programID)
	if got[german.String()] != "de" || got[french.String()] != "en" {
		t.Fatalf("sent: want de and en, got %v", got)
	}

	// routing off shows no language
	p.setProgram(programID, map[string]any{"language_route_by": "", "language_default": ""})
	_, got = p.peopleLanguagesOf(programID)
	for person, code := range got {
		if code != "-" {
			t.Fatalf("routing off gives no language, got %s for %s", code, person)
		}
	}
}

// TestAnonymousProgramPeopleHideLanguage checks an anonymous program with
// Language by never ties a language to a person: the view has no rows, the
// loaded sends keep no stamp and the language lookup gives nothing
func TestAnonymousProgramPeopleHideLanguage(t *testing.T) {
	p := newProgramLanguageTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	templateID := p.languageTemplate("lure", "en", "de")
	campaignID := p.stepCampaign(programID, p.templateStep(programID, templateID, 0), 0)
	p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).
		Updates(map[string]any{"campaign_template_id": templateID, "is_anonymous": true})
	p.routeProgram(programID, model.LanguageRouteCountry, "en", map[string]string{"Germany": "de"})
	alice := p.countryRecipient("alice@example.com", "Germany")
	p.enroll(programID, alice, 0)
	if _, err := p.svc.advanceDueBatch(p.ctx, authorizedSession(t), time.Now()); err != nil {
		t.Fatalf("advance batch: %v", err)
	}
	if got := p.stampOf(campaignID, alice); got != "de" {
		t.Fatalf("the fixture must stamp de, got %s", got)
	}
	queued, err := p.svc.ProgramRepository.GetQueuedSends(p.ctx, &programID, nil)
	if err != nil {
		t.Fatalf("queued sends: %v", err)
	}
	p.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignID).Update("sent_at", time.Now().UTC())

	view, rows := p.peopleLanguagesOf(programID)
	if !view.Anonymous || len(rows) != 0 {
		t.Fatalf("an anonymous program gives no rows, got %+v", view.Rows)
	}
	program := p.loadProgram(programID)
	facts, err := p.svc.loadProgramFacts(p.ctx, program, nil)
	if err != nil {
		t.Fatalf("facts: %v", err)
	}
	if len(facts.sends) != 1 {
		t.Fatalf("want one send, got %d", len(facts.sends))
	}
	if facts.sends[0].Language != "" {
		t.Fatalf("an anonymous send keeps no language, got %q", facts.sends[0].Language)
	}
	// even with a stamp at hand the lookup gives nothing for an anonymous program
	last := map[uuid.UUID]*programSend{alice: {Person: alice, Language: "de", Template: templateID.String()}}
	queuedOf := map[uuid.UUID]repository.ProgramQueuedSend{}
	for _, q := range queued {
		queuedOf[q.RecipientID] = q
	}
	languages, err := p.svc.peopleLanguages(p.ctx, program, last, queuedOf)
	if err != nil || len(languages) != 0 {
		t.Fatalf("an anonymous program gives no language, got %v %v", languages, err)
	}
}

// TestProgramResultsLanguageAfterMainSwap checks that the program results by
// language follow the stamps an update of the template writes on sends with
// no language, also when the program results were read before and their
// facts are kept
func TestProgramResultsLanguageAfterMainSwap(t *testing.T) {
	p := newProgramLanguageTest(t)
	templates := p.svc.CampaignService.CampaignTemplateService
	templates.ProgramService = p.svc
	programID := p.program(model.ProgramLoopComplete)
	templateID := p.languageTemplate("lure", "en")
	p.db.Model(&database.CampaignTemplate{}).Where("id = ?", templateID).Updates(map[string]any{"language": "", "state_identifier_id": uuid.New()})
	stepID := p.templateStep(programID, templateID, 0)
	campaignID := p.stepCampaign(programID, stepID, 0)
	p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("campaign_template_id", templateID)
	for range 2 {
		p.enroll(programID, p.countryRecipient(uuid.NewString()+"@example.com", "Denmark"), 0)
	}
	if _, err := p.svc.advanceDueBatch(p.ctx, authorizedSession(t), time.Now()); err != nil {
		t.Fatalf("advance batch: %v", err)
	}
	now := time.Now().UTC()
	p.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignID).
		Updates(map[string]any{"sent_at": now, "language": nil})
	// the results are read, so the facts are kept
	if _, err := p.svc.programLanguageGroups(p.ctx, p.loadProgram(programID), now); err != nil {
		t.Fatalf("language groups: %v", err)
	}

	// Multiple languages with a new Danish main and the old content as English
	var stored database.CampaignTemplate
	if err := p.db.Where("id = ?", templateID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if err := templates.UpdateByID(p.ctx, authorizedSession(t), &templateID, &model.CampaignTemplate{
		Language:      nullable.NewNullableWithValue("da"),
		EmailID:       nullable.NewNullableWithValue(p.languageEmail("da")),
		LandingPageID: nullable.NewNullableWithValue(p.languagePage()),
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{{
			Language:      "en",
			EmailID:       nullable.NewNullableWithValue(*stored.EmailID),
			LandingPageID: nullable.NewNullableWithValue(*stored.LandingPageID),
		}}),
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	g, err := p.svc.programLanguageGroups(p.ctx, p.loadProgram(programID), now)
	if err != nil {
		t.Fatalf("language groups: %v", err)
	}
	english := rowByName(g.Rows, "English")
	if english == nil || *english.Sent != 2 || rowByName(g.Rows, "Danish") != nil {
		t.Fatalf("want the 2 sends as English, got %+v", g.Rows)
	}
}
