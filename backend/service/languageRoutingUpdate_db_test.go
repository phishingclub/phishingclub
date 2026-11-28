package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
)

// TestSelfManagedPoolAndRoutingChangeTogether checks one update that turns
// routing on and moves the pool to a template that has only the new routed
// language: the rows not sent yet are judged by the language the new routing
// gives them, so the change goes through, while a row a sender has claimed
// keeps its old language and still refuses a move to a template without it
func TestSelfManagedPoolAndRoutingChangeTogether(t *testing.T) {
	update := func(f *poolFixture, campaignID uuid.UUID, groupID uuid.UUID, pool uuid.UUID) error {
		incoming := selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
		incoming.TemplateIDs = nullable.NewNullableWithValue([]*uuid.UUID{&pool})
		incoming.LanguageRouteBy = nullable.NewNullableWithValue(model.LanguageRouteCountry)
		incoming.LanguageDefault = nullable.NewNullableWithValue("de")
		incoming.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{})
		return f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming)
	}

	f := newLanguagePoolFixture(t)
	english := f.languageTemplate("en")
	german := f.languageTemplate("de")
	groupID, recipients := f.countryGroup("Germany", "Germany")
	campaignID, _ := f.selfManagedScheduled([]uuid.UUID{english}, groupID, false, "", nil)
	if err := update(f, campaignID, groupID, german); err != nil {
		t.Fatalf("pool and routing in one update: %v", err)
	}
	stamps := f.stamps(campaignID)
	templates := f.templatesByRecipient(campaignID)
	for _, rid := range recipients {
		if stamps[rid] != "de" || templates[rid] != german {
			t.Fatalf("want every row on German in German, got stamps %v templates %v", stamps, templates)
		}
	}

	// a row claimed by a sender keeps English, the German template can not
	// send it
	f = newLanguagePoolFixture(t)
	english = f.languageTemplate("en")
	german = f.languageTemplate("de")
	groupID, recipients = f.countryGroup("Germany", "Germany")
	campaignID, _ = f.selfManagedScheduled([]uuid.UUID{english}, groupID, false, "", nil)
	if err := f.db.Model(&database.CampaignRecipient{}).Where("recipient_id = ?", recipients[0].String()).
		Update("last_attempt_at", utils.NowRFC3339UTC()).Error; err != nil {
		t.Fatal(err)
	}
	err := update(f, campaignID, groupID, german)
	if !isValidationError(err) || !strings.Contains(err.Error(), "can not move") {
		t.Fatalf("claimed English row moved to German: want a refusal, got %v", err)
	}
}

// TestCampaignRouteFieldChangeClearsRoutes checks a campaign update that
// changes Language by without sending routes clears the routes of the old
// field, and one that keeps the field keeps them
func TestCampaignRouteFieldChangeClearsRoutes(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "da")
	groupID, _ := f.countryGroup("Denmark")
	campaignID, _ := f.selfManagedScheduled([]uuid.UUID{tmpl}, groupID, false,
		model.LanguageRouteCountry, map[string]string{"denmark": "da"})
	routes := func() map[string]string {
		t.Helper()
		got, err := f.svc.CampaignRepository.GetLanguageRoutesByCampaignID(f.ctx, &campaignID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	// the default alone keeps the routes
	incoming := selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
	incoming.LanguageDefault = nullable.NewNullableWithValue("da")
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); err != nil {
		t.Fatalf("default change: %v", err)
	}
	if got := routes(); got["denmark"] != "da" {
		t.Fatalf("a default change dropped the routes: %v", got)
	}
	incoming = selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
	incoming.LanguageRouteBy = nullable.NewNullableWithValue(model.LanguageRouteCity)
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); err != nil {
		t.Fatalf("field change: %v", err)
	}
	if got := routes(); len(got) != 0 {
		t.Fatalf("routes of the old field kept: %v", got)
	}
	stored := f.loadCampaign(campaignID, groupID)
	if stored.LanguageRouteField() != model.LanguageRouteCity || stored.LanguageDefault.MustGet() != "da" {
		t.Fatalf("field or default not stored: %q %v", stored.LanguageRouteField(), stored.LanguageDefault)
	}
}

// TestProgramPartialRoutingUpdate checks a program update can send one
// language field, the others are taken from the stored program, a new field
// without routes clears the routes of the old one, and the merged routing is
// still checked against the step templates
func TestProgramPartialRoutingUpdate(t *testing.T) {
	p := newProgramLanguageTest(t)
	session := authorizedSession(t)
	full := p.languageTemplate("full lure", "en", "de")
	req := newProgramRequest(t, "partial", full, p.group("partial group"))
	req.LanguageRouteBy = nullable.NewNullableWithValue(model.LanguageRouteCountry)
	req.LanguageDefault = nullable.NewNullableWithValue("en")
	req.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{"Secretland": "de"})
	id, err := p.svc.Create(p.ctx, session, req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	name := p.loadProgram(*id).Name
	routes := func() map[string]string {
		t.Helper()
		got, err := p.svc.ProgramRepository.GetLanguageRoutesByProgramID(p.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if err := p.svc.UpdateByID(p.ctx, session, id, &model.Program{
		Name: name, LanguageDefault: nullable.NewNullableWithValue("de"),
	}); err != nil {
		t.Fatalf("default only: %v", err)
	}
	stored := p.loadProgram(*id)
	if stored.LanguageRouteField() != model.LanguageRouteCountry || stored.LanguageDefault.MustGet() != "de" || routes()["secretland"] != "de" {
		t.Fatalf("default only: got %q %v %v", stored.LanguageRouteField(), stored.LanguageDefault, routes())
	}
	err = p.svc.UpdateByID(p.ctx, session, id, &model.Program{
		Name: name, LanguageDefault: nullable.NewNullableWithValue("fr"),
	})
	if err == nil || !strings.Contains(err.Error(), "French") {
		t.Fatalf("a merged default the template lacks: want a refusal naming French, got %v", err)
	}
	if err := p.svc.UpdateByID(p.ctx, session, id, &model.Program{
		Name: name, LanguageRouteBy: nullable.NewNullableWithValue(model.LanguageRouteCity),
	}); err != nil {
		t.Fatalf("field only: %v", err)
	}
	stored = p.loadProgram(*id)
	if stored.LanguageRouteField() != model.LanguageRouteCity || stored.LanguageDefault.MustGet() != "de" || len(routes()) != 0 {
		t.Fatalf("field only: got %q %v %v", stored.LanguageRouteField(), stored.LanguageDefault, routes())
	}
	if err := p.svc.UpdateByID(p.ctx, session, id, &model.Program{
		Name: name, LanguageRouteBy: nullable.NewNullableWithValue(""),
	}); err != nil {
		t.Fatalf("routing off: %v", err)
	}
	// with routing off a default alone is cleared, as on a campaign
	if err := p.svc.UpdateByID(p.ctx, session, id, &model.Program{
		Name: name, LanguageDefault: nullable.NewNullableWithValue("de"),
	}); err != nil {
		t.Fatalf("a default with routing off: %v", err)
	}
	stored = p.loadProgram(*id)
	if stored.LanguageRouteField() != "" || stored.LanguageDefault.MustGet() != "" {
		t.Fatalf("routing off: got %q %v", stored.LanguageRouteField(), stored.LanguageDefault)
	}
}

// TestProgramResultsHaveLanguageGroupData checks the program results say
// when the sends hold more than one language, also after the program stops
// picking languages, and that an anonymous program counts only the sends its
// sizes only language rows count
func TestProgramResultsHaveLanguageGroupData(t *testing.T) {
	p := newProgramLanguageTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	templateID := p.languageTemplate("lure", "en", "de")
	stepID := p.templateStep(programID, templateID, 0)
	campaignID := p.stepCampaign(programID, stepID, 0)
	p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("campaign_template_id", templateID)
	hasLanguages := func(when string, want bool) {
		t.Helper()
		p.svc.invalidateResults(&programID)
		results, err := p.svc.GetResults(p.ctx, session, &programID, "")
		if err != nil {
			t.Fatalf("%s: results: %v", when, err)
		}
		if results.HasLanguageGroupData != want {
			t.Fatalf("%s: hasLanguageGroupData %v, want %v", when, results.HasLanguageGroupData, want)
		}
	}
	hasLanguages("no sends", false)
	p.routeProgram(programID, model.LanguageRouteCountry, "en", map[string]string{"Germany": "de"})
	for _, country := range []string{"Germany", "France"} {
		p.enroll(programID, p.countryRecipient(uuid.NewString()+"@example.com", country), 0)
	}
	if _, err := p.svc.advanceDueBatch(p.ctx, session, time.Now()); err != nil {
		t.Fatalf("advance batch: %v", err)
	}
	p.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignID).Update("sent_at", time.Now().UTC())
	p.svc.resultsCache.invalidate(programID)
	hasLanguages("two languages", true)
	p.routeProgram(programID, "", "", map[string]string{})
	hasLanguages("routing off", true)

	// an anonymous program counts whole settled months only, the sends of
	// this month are not counted yet
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	p.svc.resultsCache.invalidate(programID)
	hasLanguages("anonymous this month", false)
}

// TestCampaignUpdateWithoutRoutingAuditsNothing checks that an update that
// sends Language by off for a campaign without routing adds no language
// fields to the audit and does not write the routes
func TestCampaignUpdateWithoutRoutingAuditsNothing(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "da")
	groupID, _ := f.countryGroup("Denmark")
	campaignID, _ := f.selfManagedScheduled([]uuid.UUID{tmpl}, groupID, false, "", nil)
	current := f.loadCampaign(campaignID, groupID)
	incoming := selfManagedUpdate(current, groupID)
	incoming.LanguageRouteBy = nullable.NewNullableWithValue("")
	ae := NewAuditEvent("Campaign.UpdateByID", f.session)
	changed, err := mergeLanguageRouting(f.ctx, f.svc.CampaignRepository, &campaignID, current, incoming, ae)
	if err != nil {
		t.Fatal(err)
	}
	if changed || current.LanguageRoutes.IsSpecified() {
		t.Fatalf("nothing changed: changed %v, routes written %v", changed, current.LanguageRoutes.IsSpecified())
	}
	for key := range ae.Details {
		if strings.Contains(strings.ToLower(key), "language") {
			t.Fatalf("audit has %s although routing did not change", key)
		}
	}
	// a route change is still audited
	current = f.loadCampaign(campaignID, groupID)
	incoming = selfManagedUpdate(current, groupID)
	incoming.LanguageRouteBy = nullable.NewNullableWithValue(model.LanguageRouteCountry)
	incoming.LanguageDefault = nullable.NewNullableWithValue("en")
	incoming.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{"Denmark": "da"})
	ae = NewAuditEvent("Campaign.UpdateByID", f.session)
	if changed, err = mergeLanguageRouting(f.ctx, f.svc.CampaignRepository, &campaignID, current, incoming, ae); err != nil || !changed {
		t.Fatalf("routing on: changed %v, %v", changed, err)
	}
	if ae.Details["languageRouteCount"] != 1 {
		t.Fatalf("routing on not audited: %v", ae.Details)
	}
}

// TestAnonymousRoutingLockedOnceRowsExist checks a self managed anonymous
// campaign refuses a change of Language by, the default or the routes once it
// has recipient rows, and its rows keep their language. A scheduled anonymous
// campaign rebuilds its rows on update, so it allows the change until a row
// was sent or an event was saved. Before any row exists the change is
// allowed.
func TestAnonymousRoutingLockedOnceRowsExist(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "da", "de")
	groupID, r := f.countryGroup("Denmark", "Germany")
	// a self managed anonymous campaign can pick only one language
	campaignID, _ := f.selfManagedScheduled([]uuid.UUID{tmpl}, groupID, true,
		model.LanguageRouteCountry, map[string]string{"denmark": "en"})
	before := f.stamps(campaignID)
	if before[r[0]] != "en" || before[r[1]] != "en" {
		t.Fatalf("scheduled stamps: %v", before)
	}
	refused := func(id uuid.UUID, incoming *model.Campaign) {
		t.Helper()
		err := f.svc.UpdateByID(f.ctx, f.session, &id, incoming)
		if err == nil || !strings.Contains(err.Error(), anonymousRoutingLockedText) {
			t.Fatalf("want the routing lock refusal, got %v", err)
		}
	}
	// routes
	incoming := selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
	incoming.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{"denmark": "de", "germany": "de"})
	refused(campaignID, incoming)
	// default
	incoming = selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
	incoming.LanguageDefault = nullable.NewNullableWithValue("de")
	refused(campaignID, incoming)
	// field, and routing off
	incoming = selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
	incoming.LanguageRouteBy = nullable.NewNullableWithValue(model.LanguageRouteCity)
	refused(campaignID, incoming)
	incoming = selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
	incoming.LanguageRouteBy = nullable.NewNullableWithValue("")
	refused(campaignID, incoming)
	after := f.stamps(campaignID)
	if after[r[0]] != "en" || after[r[1]] != "en" {
		t.Fatalf("a refused update changed stamps: %v", after)
	}
	// an update that leaves the routing as it is still goes through
	incoming = selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
	incoming.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{"denmark": "en"})
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); err != nil {
		t.Fatalf("update with the same routing: %v", err)
	}
	// the restamp never rewrites an anonymous campaign's rows
	f.route(campaignID, model.LanguageRouteCountry, "de", map[string]string{"denmark": "de"})
	stored := f.loadCampaign(campaignID, groupID)
	if n, err := f.svc.updateRestampingUnsentLanguages(f.ctx, &campaignID, stored, []uuid.UUID{tmpl}); err != nil || n != 0 {
		t.Fatalf("restamp of an anonymous campaign: %d %v", n, err)
	}
	if got := f.stamps(campaignID); got[r[0]] != "en" || got[r[1]] != "en" {
		t.Fatalf("restamp changed an anonymous campaign: %v", got)
	}

	// a scheduled anonymous campaign that has not started rebuilds its rows
	// on update, so its routing can change
	scheduled := f.campaign([]uuid.UUID{tmpl}, false, true)
	f.seedCampaignRecipient(scheduled, &tmpl, false, "x")
	scheduledUpdate := func(defaultCode string) *model.Campaign {
		loaded := f.loadCampaign(scheduled, groupID)
		return &model.Campaign{
			Name:              loaded.Name,
			SortField:         loaded.SortField,
			SortOrder:         loaded.SortOrder,
			SendStartAt:       loaded.SendStartAt,
			SendEndAt:         loaded.SendEndAt,
			RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{&groupID}),
			LanguageRouteBy:   nullable.NewNullableWithValue(model.LanguageRouteCountry),
			LanguageDefault:   nullable.NewNullableWithValue(defaultCode),
			LanguageRoutes:    nullable.NewNullableWithValue(map[string]string{"denmark": "da"}),
		}
	}
	if err := f.svc.UpdateByID(f.ctx, f.session, &scheduled, scheduledUpdate("en")); err != nil {
		t.Fatalf("a scheduled anonymous campaign that has not started must allow routing changes: %v", err)
	}
	if got := f.stamps(scheduled); got[r[0]] != "da" || got[r[1]] != "en" {
		t.Fatalf("the rebuilt rows are stamped by the new routing: %v", got)
	}

	// once a row of a scheduled anonymous campaign was sent it is locked
	if err := f.db.Model(&database.CampaignRecipient{}).
		Where("campaign_id = ? AND recipient_id = ?", scheduled.String(), r[0].String()).
		Update("sent_at", time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	refused(scheduled, scheduledUpdate("de"))

	// and once an event was saved, even with no row sent
	withEvent := f.campaign([]uuid.UUID{tmpl}, false, true)
	f.seedCampaignRecipient(withEvent, &tmpl, false, "x")
	pseudonym := uuid.New()
	eventID := uuid.New()
	if err := f.db.Create(&database.CampaignEvent{
		ID: &eventID, CreatedAt: &f.now, UpdatedAt: &f.now,
		CampaignID: &withEvent, AnonymizedID: &pseudonym,
		EventID: cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.svc.refuseAnonymousRoutingChange(f.ctx, &withEvent, f.loadCampaign(withEvent, groupID)); err == nil ||
		!strings.Contains(err.Error(), anonymousRoutingLockedText) {
		t.Fatalf("a scheduled anonymous campaign with an event is locked: %v", err)
	}

	// before any row exists the routing can change
	empty := f.campaign([]uuid.UUID{tmpl}, true, true)
	if err := f.svc.refuseAnonymousRoutingChange(f.ctx, &empty, f.loadCampaign(empty, groupID)); err != nil {
		t.Fatalf("an anonymous campaign with no rows must allow routing changes: %v", err)
	}
}

// TestSelfManagedRestampFailureKeepsRouting checks that when the restamp of a
// self managed campaign fails, the new routing is not saved either, so saving
// the same values again restamps the rows
func TestSelfManagedRestampFailureKeepsRouting(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "da", "de")
	groupID, r := f.countryGroup("Denmark", "Germany")
	campaignID, _ := f.selfManagedScheduled([]uuid.UUID{tmpl}, groupID, false,
		model.LanguageRouteCountry, map[string]string{"denmark": "da"})
	if got := f.stamps(campaignID); got[r[0]] != "da" || got[r[1]] != "en" {
		t.Fatalf("scheduled stamps: %v", got)
	}
	update := func() error {
		incoming := selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
		incoming.LanguageRouteBy = nullable.NewNullableWithValue(model.LanguageRouteCountry)
		incoming.LanguageDefault = nullable.NewNullableWithValue("de")
		incoming.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{"denmark": "de"})
		return f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming)
	}
	// a stamp write fails, as a busy database would
	if err := f.db.Exec(`CREATE TRIGGER fail_restamp BEFORE UPDATE OF language ON campaign_recipients
		BEGIN SELECT RAISE(ABORT, 'restamp failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := update(); err == nil {
		t.Fatal("a failed restamp must fail the update")
	}
	stored := f.loadCampaign(campaignID, groupID)
	if v, _ := stored.LanguageDefault.Get(); v != "en" {
		t.Fatalf("a failed restamp must not save the new default, got %q", v)
	}
	routes, err := f.svc.CampaignRepository.GetLanguageRoutesByCampaignID(f.ctx, &campaignID)
	if err != nil || routes["denmark"] != "da" {
		t.Fatalf("a failed restamp must not save the new routes, got %v %v", routes, err)
	}
	if got := f.stamps(campaignID); got[r[0]] != "da" || got[r[1]] != "en" {
		t.Fatalf("a failed update changed stamps: %v", got)
	}
	// saving the same values again restamps the rows
	if err := f.db.Exec(`DROP TRIGGER fail_restamp`).Error; err != nil {
		t.Fatal(err)
	}
	if err := update(); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := f.stamps(campaignID); got[r[0]] != "de" || got[r[1]] != "de" {
		t.Fatalf("the retry restamps the rows: %v", got)
	}
}

// TestSelfManagedPoolAndRoutingFailureKeepsBoth checks one update of a self
// managed campaign that changes both the template pool and the routing: when
// the restamp fails, the pool, the moves and the routing are all left as they
// were, and saving the same values again writes all of them
func TestSelfManagedPoolAndRoutingFailureKeepsBoth(t *testing.T) {
	f := newLanguagePoolFixture(t)
	first := f.languageTemplate("en", "da", "de")
	second := f.languageTemplate("en", "da", "de")
	groupID, r := f.countryGroup("Denmark", "Germany")
	campaignID, _ := f.selfManagedScheduled([]uuid.UUID{first}, groupID, false,
		model.LanguageRouteCountry, map[string]string{"denmark": "da"})
	if got := f.stamps(campaignID); got[r[0]] != "da" || got[r[1]] != "en" {
		t.Fatalf("scheduled stamps: %v", got)
	}
	update := func() error {
		incoming := selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
		incoming.TemplateIDs = nullable.NewNullableWithValue([]*uuid.UUID{&second})
		incoming.LanguageRouteBy = nullable.NewNullableWithValue(model.LanguageRouteCountry)
		incoming.LanguageDefault = nullable.NewNullableWithValue("de")
		incoming.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{"denmark": "de"})
		return f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming)
	}
	pool := func() []uuid.UUID {
		t.Helper()
		ids, err := f.svc.CampaignRepository.GetTemplatePoolIDs(f.ctx, &campaignID)
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}
	// a stamp write fails, as a busy database would
	if err := f.db.Exec(`CREATE TRIGGER fail_restamp BEFORE UPDATE OF language ON campaign_recipients
		BEGIN SELECT RAISE(ABORT, 'restamp failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := update(); err == nil {
		t.Fatal("a failed restamp must fail the update")
	}
	if got := pool(); len(got) != 1 || got[0] != first {
		t.Fatalf("a failed restamp must not save the new pool, got %v", got)
	}
	stored := f.loadCampaign(campaignID, groupID)
	if v, _ := stored.TemplateID.Get(); v != first {
		t.Fatalf("a failed restamp must not change the first template, got %v", v)
	}
	if v, _ := stored.LanguageDefault.Get(); v != "en" {
		t.Fatalf("a failed restamp must not save the new default, got %q", v)
	}
	routes, err := f.svc.CampaignRepository.GetLanguageRoutesByCampaignID(f.ctx, &campaignID)
	if err != nil || routes["denmark"] != "da" {
		t.Fatalf("a failed restamp must not save the new routes, got %v %v", routes, err)
	}
	for _, rid := range r {
		if got := f.templatesByRecipient(campaignID)[rid]; got != first {
			t.Fatalf("a failed update moved a row: %v", f.templatesByRecipient(campaignID))
		}
	}
	if got := f.stamps(campaignID); got[r[0]] != "da" || got[r[1]] != "en" {
		t.Fatalf("a failed update changed stamps: %v", got)
	}
	// saving the same values again writes the pool, the moves and the stamps
	if err := f.db.Exec(`DROP TRIGGER fail_restamp`).Error; err != nil {
		t.Fatal(err)
	}
	if err := update(); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := pool(); len(got) != 1 || got[0] != second {
		t.Fatalf("the retry saves the pool, got %v", got)
	}
	templates := f.templatesByRecipient(campaignID)
	stamps := f.stamps(campaignID)
	for _, rid := range r {
		if templates[rid] != second || stamps[rid] != "de" {
			t.Fatalf("the retry moves and restamps every row: templates %v stamps %v", templates, stamps)
		}
	}
}

// TestUpdateByIDWithFailureKeepsAllowDenyLists checks that a campaign update
// whose transaction fails leaves the campaign's allow and deny lists as they
// were
func TestUpdateByIDWithFailureKeepsAllowDenyLists(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en")
	campaignID := f.campaign([]uuid.UUID{tmpl}, true, false)
	listID := uuid.New()
	now := time.Now().UTC()
	if err := f.db.Create(&database.AllowDeny{ID: &listID, CreatedAt: &now, UpdatedAt: &now, Name: "kept list", Allowed: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&database.CampaignAllowDeny{CampaignID: &campaignID, AllowDenyID: &listID}).Error; err != nil {
		t.Fatal(err)
	}
	links := func() int64 {
		t.Helper()
		var n int64
		if err := f.db.Model(&database.CampaignAllowDeny{}).Where("campaign_id = ?", campaignID.String()).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	update := &model.Campaign{}
	update.AllowDenyIDs = nullable.NewNullableWithValue([]*uuid.UUID{})
	failed := errors.New("write failed")
	err := f.svc.CampaignRepository.UpdateByIDWith(f.ctx, &campaignID, update, func(tx *gorm.DB) error {
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatalf("want the write error, got %v", err)
	}
	if n := links(); n != 1 {
		t.Fatalf("a failed update changed the allow and deny lists: %d links", n)
	}
	// without a failure the lists are replaced
	if err := f.svc.CampaignRepository.UpdateByIDWith(f.ctx, &campaignID, update, nil); err != nil {
		t.Fatal(err)
	}
	if n := links(); n != 0 {
		t.Fatalf("the lists were not replaced: %d links", n)
	}
}

// TestAnonymousSelfManagedRefusesSeveralLanguages checks an anonymous self
// managed campaign refuses Language by that can pick more than one language
// on create and update, as each email is delivered by hand there. One
// language and scheduled anonymous campaigns are allowed.
func TestAnonymousSelfManagedRefusesSeveralLanguages(t *testing.T) {
	routed := func(c *model.Campaign, fallback string, routes map[string]string) *model.Campaign {
		c.LanguageRouteBy = nullable.NewNullableWithValue(model.LanguageRouteCountry)
		c.LanguageDefault = nullable.NewNullableWithValue(fallback)
		c.LanguageRoutes = nullable.NewNullableWithValue(routes)
		return c
	}
	wantRefused := func(err error) {
		t.Helper()
		if !isValidationError(err) || !strings.Contains(err.Error(), "can use only one language") {
			t.Fatalf("want the one language refusal, got %v", err)
		}
	}

	// create, checked by the model
	create := routed(&model.Campaign{IsAnonymous: nullable.NewNullableWithValue(true)}, "en",
		map[string]string{"Denmark": "da"})
	create.SendStartAt.SetNull()
	create.SendEndAt.SetNull()
	wantRefused(create.ValidateAnonymousSelfManagedLanguages())
	create.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{"Denmark": "en"})
	if err := create.ValidateAnonymousSelfManagedLanguages(); err != nil {
		t.Fatalf("one language refused: %v", err)
	}
	create.LanguageDefault = nullable.NewNullableWithValue("de")
	wantRefused(create.ValidateAnonymousSelfManagedLanguages())
	scheduled := routed(&model.Campaign{IsAnonymous: nullable.NewNullableWithValue(true)}, "en",
		map[string]string{"Denmark": "da"})
	scheduled.SendStartAt = nullable.NewNullableWithValue(time.Now().Add(time.Hour))
	scheduled.SendEndAt = nullable.NewNullableWithValue(time.Now().Add(2 * time.Hour))
	if err := scheduled.ValidateAnonymousSelfManagedLanguages(); err != nil {
		t.Fatalf("scheduled anonymous campaign refused: %v", err)
	}
	named := routed(&model.Campaign{IsAnonymous: nullable.NewNullableWithValue(false)}, "en",
		map[string]string{"Denmark": "da"})
	named.SendStartAt.SetNull()
	named.SendEndAt.SetNull()
	if err := named.ValidateAnonymousSelfManagedLanguages(); err != nil {
		t.Fatalf("named self managed campaign refused: %v", err)
	}

	// update, checked on the merged campaign with the stored routes
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "da")
	groupID, _ := f.countryGroup("Denmark", "Germany")
	campaignID := f.campaign([]uuid.UUID{tmpl}, true, true)
	incoming := routed(selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID), "en",
		map[string]string{"denmark": "da"})
	wantRefused(f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming))
	// routes stored before, only the default sent
	f.route(campaignID, model.LanguageRouteCountry, "en", map[string]string{"denmark": "en"})
	incoming = selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
	incoming.LanguageDefault = nullable.NewNullableWithValue("da")
	wantRefused(f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming))
	incoming = routed(selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID), "da",
		map[string]string{"denmark": "da"})
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); err != nil {
		t.Fatalf("one language on an anonymous self managed campaign: %v", err)
	}
}
