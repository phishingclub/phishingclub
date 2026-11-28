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

// recipientService returns a recipient service on the fixture database
func (f *poolFixture) recipientService() *Recipient {
	return &Recipient{
		Common:              f.svc.Common,
		RecipientRepository: &repository.Recipient{DB: f.db},
	}
}

// storedLanguage returns the language column of a recipient, "<nil>" for null
func (f *poolFixture) storedLanguage(id uuid.UUID) string {
	f.t.Helper()
	var row database.Recipient
	if err := f.db.Where("id = ?", id).First(&row).Error; err != nil {
		f.t.Fatalf("read recipient: %v", err)
	}
	if row.Language == nil {
		return "<nil>"
	}
	return *row.Language
}

// languageRecipient stores a recipient in groupID with a language, nil for
// none, and returns its id
func (f *poolFixture) languageRecipient(groupID uuid.UUID, language *string) uuid.UUID {
	f.t.Helper()
	rid := uuid.New()
	email := rid.String() + "@language.test"
	if err := f.db.Create(&database.Recipient{
		ID: &rid, CreatedAt: &f.now, UpdatedAt: &f.now, Email: &email, Language: language,
	}).Error; err != nil {
		f.t.Fatalf("insert recipient: %v", err)
	}
	if err := f.db.Create(&database.RecipientGroupRecipient{RecipientID: &rid, RecipientGroupID: &groupID}).Error; err != nil {
		f.t.Fatalf("insert group member: %v", err)
	}
	return rid
}

func strPtr(v string) *string { return &v }

func recipientWithLanguage(email string, language string) *model.Recipient {
	return &model.Recipient{
		Email:    nullable.NewNullableWithValue(*vo.NewEmailMust(email)),
		Language: nullable.NewNullableWithValue(language),
	}
}

// TestRecipientLanguageCreateAndUpdate checks the form and API path: a code,
// tag or name is stored as the code, an unknown value is refused on the
// language field, an update without the field keeps it and an empty value
// clears it
func TestRecipientLanguageCreateAndUpdate(t *testing.T) {
	f := newPoolFixture(t)
	svc := f.recipientService()
	id, err := svc.Create(f.ctx, f.session, recipientWithLanguage("dane@language.test", " Danish "))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := f.storedLanguage(*id); got != "da" {
		t.Fatalf("create stored %s, want da", got)
	}
	_, err = svc.Create(f.ctx, f.session, recipientWithLanguage("klingon@language.test", "Klingon"))
	if !isValidationError(err) || !strings.Contains(err.Error(), "language") || !strings.Contains(err.Error(), "Klingon") {
		t.Fatalf("unknown language: want a validation error naming the value, got %v", err)
	}
	if _, err := svc.Create(f.ctx, f.session, recipientWithLanguage("none@language.test", "")); err != nil {
		t.Fatalf("create without a language: %v", err)
	}

	update := func(r *model.Recipient) error {
		return svc.UpdateByID(f.ctx, f.session, id, r)
	}
	if err := update(&model.Recipient{Language: nullable.NewNullableWithValue("de-AT")}); err != nil {
		t.Fatalf("update with a tag: %v", err)
	}
	if got := f.storedLanguage(*id); got != "de" {
		t.Fatalf("update stored %s, want de", got)
	}
	firstName := nullable.NewNullableWithValue(*vo.NewOptionalString127Must("Dane"))
	if err := update(&model.Recipient{FirstName: firstName}); err != nil {
		t.Fatalf("update without language: %v", err)
	}
	if got := f.storedLanguage(*id); got != "de" {
		t.Fatalf("an update without the field changed it to %s", got)
	}
	if err := update(&model.Recipient{Language: nullable.NewNullableWithValue("xx")}); !isValidationError(err) {
		t.Fatalf("update with an unknown language: want a validation error, got %v", err)
	}
	if got := f.storedLanguage(*id); got != "de" {
		t.Fatalf("a refused update changed the language to %s", got)
	}
	if err := update(&model.Recipient{Language: nullable.NewNullableWithValue("")}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := f.storedLanguage(*id); got != "<nil>" {
		t.Fatalf("an empty value stored %s, want null", got)
	}
	got, err := svc.GetByID(f.ctx, f.session, id, &repository.RecipientOption{})
	if err != nil || got.Language.MustGet() != "" {
		t.Fatalf("read back %v %v, want an empty language", got.Language, err)
	}
}

// TestRecipientLanguageImport checks the CSV import path: each row is read
// on its own, an unknown language fails only its row with the reason, and an
// empty value keeps or clears the stored language as the overwrite option says
func TestRecipientLanguageImport(t *testing.T) {
	f := newPoolFixture(t)
	svc := f.recipientService()
	rows := []*model.Recipient{
		recipientWithLanguage("a@import.test", "da-DK"),
		recipientWithLanguage("b@import.test", "Elvish"),
		recipientWithLanguage("c@import.test", "german"),
	}
	result, err := svc.Import(f.ctx, f.session, rows, false, nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Summary.Created != 2 || result.Summary.Failed != 1 || len(result.Failures) != 1 {
		t.Fatalf("summary %+v, want 2 created and 1 failed", result.Summary)
	}
	failure := result.Failures[0]
	if failure.Index != 1 || !strings.Contains(failure.Reason, "unknown language") || !strings.Contains(failure.Reason, "Elvish") {
		t.Fatalf("failure %+v, want row 1 with the unknown language", failure)
	}
	byEmail := func(email string) uuid.UUID {
		var row database.Recipient
		if err := f.db.Where("email = ?", email).First(&row).Error; err != nil {
			t.Fatalf("read %s: %v", email, err)
		}
		return *row.ID
	}
	a, c := byEmail("a@import.test"), byEmail("c@import.test")
	if f.storedLanguage(a) != "da" || f.storedLanguage(c) != "de" {
		t.Fatalf("stored %s and %s, want da and de", f.storedLanguage(a), f.storedLanguage(c))
	}

	// an empty language on an update keeps the stored one when empty fields
	// are ignored, else it clears it, the same as the other fields
	if _, err := svc.Import(f.ctx, f.session, []*model.Recipient{recipientWithLanguage("a@import.test", "")}, true, nil); err != nil {
		t.Fatalf("import keeping empty fields: %v", err)
	}
	if got := f.storedLanguage(a); got != "da" {
		t.Fatalf("ignored empty value changed the language to %s", got)
	}
	if _, err := svc.Import(f.ctx, f.session, []*model.Recipient{recipientWithLanguage("a@import.test", " ")}, false, nil); err != nil {
		t.Fatalf("import overwriting empty fields: %v", err)
	}
	if got := f.storedLanguage(a); got != "<nil>" {
		t.Fatalf("overwritten empty value stored %s, want null", got)
	}
	// a new value on an update replaces the stored one
	if _, err := svc.Import(f.ctx, f.session, []*model.Recipient{recipientWithLanguage("c@import.test", "Swedish")}, false, nil); err != nil {
		t.Fatalf("import update: %v", err)
	}
	if got := f.storedLanguage(c); got != "sv" {
		t.Fatalf("update stored %s, want sv", got)
	}
}

// TestScimRecipientLanguage checks SCIM maps preferredLanguage, falls back to
// locale when preferredLanguage is absent or not known, leaves unknown values empty
// without failing, clears the field on a full replace without it, and that
// PATCH paths and value maps follow the same rules
func TestScimRecipientLanguage(t *testing.T) {
	for name, tc := range map[string]struct {
		user ScimUser
		want string
	}{
		"preferred tag":         {ScimUser{PreferredLanguage: "da-DK"}, "da"},
		"preferred over locale": {ScimUser{PreferredLanguage: "de", Locale: "fr-FR"}, "de"},
		"locale when absent":    {ScimUser{Locale: "sv_SE"}, "sv"},
		"accept language form":  {ScimUser{PreferredLanguage: "fr-CH;q=0.9, en;q=0.8"}, "fr"},
		"unknown preferred":     {ScimUser{PreferredLanguage: "tlh", Locale: "de-DE"}, "de"},
		"both unknown":          {ScimUser{PreferredLanguage: "tlh", Locale: "xx-YY"}, ""},
		"unknown locale":        {ScimUser{Locale: "xx-YY"}, ""},
		"neither":               {ScimUser{}, ""},
	} {
		if got := scimLanguageFrom(&tc.user); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}

	f := newPoolFixture(t)
	companyID := uuid.New()
	svc := &Scim{Common: f.svc.Common, RecipientRepository: &repository.Recipient{DB: f.db}}
	created, err := svc.CreateUser(f.ctx, &companyID, nil, &ScimUser{
		UserName: "anna@scim.test", Active: true, PreferredLanguage: "nb-NO",
		Emails: []ScimEmail{{Value: "anna@scim.test", Primary: true}},
	}, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.PreferredLanguage != "no" {
		t.Fatalf("create returned %q, want no", created.PreferredLanguage)
	}
	id := uuid.MustParse(created.ID)
	if got := f.storedLanguage(id); got != "no" {
		t.Fatalf("create stored %s, want no", got)
	}
	// an unknown value never fails the sync
	if _, err := svc.CreateUser(f.ctx, &companyID, nil, &ScimUser{
		UserName: "bob@scim.test", Active: true, PreferredLanguage: "Klingon",
		Emails: []ScimEmail{{Value: "bob@scim.test", Primary: true}},
	}, ""); err != nil {
		t.Fatalf("create with an unknown language failed the sync: %v", err)
	}

	patch := func(ops ...ScimPatchOpItem) {
		t.Helper()
		if _, err := svc.PatchUser(f.ctx, &companyID, nil, &id, &ScimPatchOp{Operations: ops}, ""); err != nil {
			t.Fatalf("patch: %v", err)
		}
	}
	patch(ScimPatchOpItem{Op: "replace", Path: "preferredLanguage", Value: "de-AT"})
	if got := f.storedLanguage(id); got != "de" {
		t.Fatalf("patch path stored %s, want de", got)
	}
	patch(ScimPatchOpItem{Op: "replace", Path: "locale", Value: "fr-FR"},
		ScimPatchOpItem{Op: "replace", Path: "preferredLanguage", Value: "it"})
	if got := f.storedLanguage(id); got != "it" {
		t.Fatalf("locale beside preferredLanguage stored %s, want it", got)
	}
	patch(ScimPatchOpItem{Op: "replace", Path: "locale", Value: "fr-FR"})
	if got := f.storedLanguage(id); got != "fr" {
		t.Fatalf("locale alone stored %s, want fr", got)
	}
	patch(ScimPatchOpItem{Op: "replace", Value: map[string]any{"preferredLanguage": "es-MX", "locale": "pt-BR"}})
	if got := f.storedLanguage(id); got != "es" {
		t.Fatalf("value map stored %s, want es", got)
	}
	// an unknown preferredLanguage falls back to a known locale of the request
	patch(ScimPatchOpItem{Op: "replace", Path: "preferredLanguage", Value: "tlh"},
		ScimPatchOpItem{Op: "replace", Path: "locale", Value: "sv-SE"})
	if got := f.storedLanguage(id); got != "sv" {
		t.Fatalf("unknown preferredLanguage beside locale stored %s, want sv", got)
	}
	patch(ScimPatchOpItem{Op: "replace", Value: map[string]any{"preferredLanguage": "tlh", "locale": "nl-NL"}})
	if got := f.storedLanguage(id); got != "nl" {
		t.Fatalf("value map with unknown preferredLanguage stored %s, want nl", got)
	}
	// a remove operation is ignored like for other attributes, so the locale
	// of the same request is used
	patch(ScimPatchOpItem{Op: "remove", Path: "preferredLanguage"},
		ScimPatchOpItem{Op: "replace", Path: "locale", Value: "da-DK"})
	if got := f.storedLanguage(id); got != "da" {
		t.Fatalf("remove preferredLanguage beside locale stored %s, want da", got)
	}
	patch(ScimPatchOpItem{Op: "remove", Path: "preferredLanguage"})
	if got := f.storedLanguage(id); got != "da" {
		t.Fatalf("remove preferredLanguage stored %s, want da kept", got)
	}
	patch(ScimPatchOpItem{Op: "replace", Path: "preferredLanguage", Value: "zz"})
	if got := f.storedLanguage(id); got != "<nil>" {
		t.Fatalf("unknown patch value stored %s, want null", got)
	}

	// PUT is a full replace: a value is set, and leaving it out clears it
	replace := func(u ScimUser) {
		t.Helper()
		u.UserName, u.Active = "anna@scim.test", true
		u.Emails = []ScimEmail{{Value: "anna@scim.test", Primary: true}}
		if _, err := svc.ReplaceUser(f.ctx, &companyID, nil, &id, &u, ""); err != nil {
			t.Fatalf("replace: %v", err)
		}
	}
	replace(ScimUser{Locale: "pl-PL"})
	if got := f.storedLanguage(id); got != "pl" {
		t.Fatalf("replace with locale stored %s, want pl", got)
	}
	replace(ScimUser{PreferredLanguage: "Klingon", Locale: "fi-FI"})
	if got := f.storedLanguage(id); got != "fi" {
		t.Fatalf("replace with unknown preferredLanguage stored %s, want fi", got)
	}
	replace(ScimUser{})
	if got := f.storedLanguage(id); got != "<nil>" {
		t.Fatalf("replace without a language stored %s, want null", got)
	}
}

// TestDynamicGroupByLanguage checks a dynamic group can filter on the
// language field: the value is stored as the code and an unknown value is
// refused
func TestDynamicGroupByLanguage(t *testing.T) {
	g := &model.RecipientGroup{
		Name:        nullable.NewNullableWithValue(*vo.NewString127Must("danes")),
		IsDynamic:   nullable.NewNullableWithValue(true),
		FilterField: nullable.NewNullableWithValue("language"),
		FilterValue: nullable.NewNullableWithValue(" Danish "),
	}
	if err := g.ValidateDynamic(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if v := g.FilterValue.MustGet(); v != "da" {
		t.Fatalf("filter value %q, want da", v)
	}
	g.FilterValue = nullable.NewNullableWithValue("Elvish")
	if err := g.ValidateDynamic(); !isValidationError(err) {
		t.Fatalf("unknown language: want a validation error, got %v", err)
	}

	f := newPoolFixture(t)
	groupID := uuid.New()
	if err := f.db.Create(&database.RecipientGroup{
		ID: &groupID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "danes",
		IsDynamic: true, FilterField: "language", FilterValue: "da",
	}).Error; err != nil {
		t.Fatalf("insert group: %v", err)
	}
	other := f.group(nil)
	dane := f.languageRecipient(other, strPtr("da"))
	f.languageRecipient(other, strPtr("de"))
	f.languageRecipient(other, nil)
	repo := &repository.RecipientGroup{DB: f.db}
	result, err := repo.GetRecipientsByGroupID(f.ctx, &groupID, &repository.RecipientOption{})
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0].ID.MustGet() != dane {
		t.Fatalf("members %d, want only the Danish recipient", len(result.Rows))
	}
	// a recipient in no static group is not orphaned while the dynamic group
	// matches their language
	f.db.Where("recipient_id = ?", dane).Delete(&database.RecipientGroupRecipient{})
	loner := f.languageRecipient(other, strPtr("de"))
	f.db.Where("recipient_id = ?", loner).Delete(&database.RecipientGroupRecipient{})
	orphans, err := (&repository.Recipient{DB: f.db}).GetOrphaned(f.ctx, nil, &repository.RecipientOption{})
	if err != nil {
		t.Fatalf("orphans: %v", err)
	}
	if len(orphans.Rows) != 1 || orphans.Rows[0].ID.MustGet() != loner {
		t.Fatalf("orphans %d, want only the German recipient outside every group", len(orphans.Rows))
	}
}

// TestFieldValuesByLanguage checks the field values endpoint reads the
// language field: codes counted, no language counted as empty
func TestFieldValuesByLanguage(t *testing.T) {
	f := newPoolFixture(t)
	groupID := f.group(nil)
	for _, l := range []*string{strPtr("da"), strPtr("da"), strPtr("de"), nil} {
		f.languageRecipient(groupID, l)
	}
	svc := &RecipientGroup{Common: f.svc.Common, RecipientGroupRepository: &repository.RecipientGroup{DB: f.db}}
	got, err := svc.GetFieldValues(f.ctx, f.session, nil, model.LanguageRouteLanguage, []*uuid.UUID{&groupID})
	if err != nil {
		t.Fatalf("field values: %v", err)
	}
	if len(got.Values) != 2 || got.Values[0].Key != "da" || got.Values[0].Count != 2 ||
		got.Values[1].Key != "de" || got.Values[1].Count != 1 || got.EmptyCount != 1 {
		t.Fatalf("values %+v empty %d", got.Values, got.EmptyCount)
	}
}

// TestScheduleRoutesByLanguageField checks Language by Language: a recipient
// gets their own language when every pool template can send it, else the
// default; no language or a language one template lacks gets the default
func TestScheduleRoutesByLanguageField(t *testing.T) {
	f := newLanguagePoolFixture(t)
	full := f.languageTemplate("en", "da", "de", "fr")
	partial := f.languageTemplate("en", "da", "de")
	broken := f.languageTemplate("en", "da", "de", "fr")
	// German of the third template is not usable, so German falls back too
	f.db.Model(&database.CampaignTemplateLanguage{}).
		Where("campaign_template_id = ? AND language = ?", broken, "de").Update("is_usable", false)
	groupID := f.group(nil)
	people := map[uuid.UUID]string{
		f.languageRecipient(groupID, strPtr("da")): "da",
		f.languageRecipient(groupID, strPtr("de")): "en",
		f.languageRecipient(groupID, strPtr("fr")): "en",
		f.languageRecipient(groupID, strPtr("en")): "en",
		f.languageRecipient(groupID, nil):          "en",
	}
	campaignID := f.campaign([]uuid.UUID{full, partial, broken}, false, false)
	f.route(campaignID, model.LanguageRouteLanguage, "en", nil)
	if err := f.svc.schedule(f.ctx, f.session, f.loadCampaign(campaignID, groupID)); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	stamps := f.stamps(campaignID)
	for rid, want := range people {
		if stamps[rid] != want {
			t.Errorf("stamped %s, want %s", stamps[rid], want)
		}
	}

	// one template: each recipient gets their own language
	f2 := newLanguagePoolFixture(t)
	only := f2.languageTemplate("en", "da", "de", "fr")
	group2 := f2.group(nil)
	dane, german := f2.languageRecipient(group2, strPtr("da")), f2.languageRecipient(group2, strPtr("de"))
	italian := f2.languageRecipient(group2, strPtr("it"))
	campaign2 := f2.campaign([]uuid.UUID{only}, false, false)
	f2.route(campaign2, model.LanguageRouteLanguage, "fr", nil)
	if err := f2.svc.schedule(f2.ctx, f2.session, f2.loadCampaign(campaign2, group2)); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	stamps = f2.stamps(campaign2)
	if stamps[dane] != "da" || stamps[german] != "de" || stamps[italian] != "fr" {
		t.Fatalf("stamps %s %s %s, want da de fr", stamps[dane], stamps[german], stamps[italian])
	}
}

// TestLanguageFieldRoutingChecks checks the rules of Language by Language:
// only the default must be on every template, routes are refused, an email
// of a language it can pick must parse, and an anonymous self managed
// campaign can not use it
func TestLanguageFieldRoutingChecks(t *testing.T) {
	f := newLanguagePoolFixture(t)
	full := f.languageTemplate("en", "da", "de")
	partial := f.languageTemplate("en")
	if _, err := f.svc.checkPoolLanguages(f.ctx, nil, model.LanguageRouteLanguage, "en", nil, []uuid.UUID{full, partial}, nil); err != nil {
		t.Fatalf("default on every template refused: %v", err)
	}
	_, err := f.svc.checkPoolLanguages(f.ctx, nil, model.LanguageRouteLanguage, "de", nil, []uuid.UUID{full, partial}, nil)
	if !isValidationError(err) || !strings.Contains(err.Error(), "languageDefault") || !strings.Contains(err.Error(), "German") {
		t.Fatalf("default missing on a template: want a refusal naming German, got %v", err)
	}
	// a broken Danish email is checked only when a recipient has Danish
	broken := f.languageTemplate("en")
	f.languageRow(broken, "da", f.languageEmail("da", "{{.FirstName"), true)
	if _, err := f.svc.checkPoolLanguages(f.ctx, nil, model.LanguageRouteLanguage, "en", nil, []uuid.UUID{broken}, []string{"de", "en"}); err != nil {
		t.Fatalf("broken email of a language no recipient has refused: %v", err)
	}
	_, err = f.svc.checkPoolLanguages(f.ctx, nil, model.LanguageRouteLanguage, "en", nil, []uuid.UUID{broken}, []string{"da"})
	if !isValidationError(err) || !strings.Contains(err.Error(), "Danish") {
		t.Fatalf("broken email of a language a recipient has: want a refusal naming Danish, got %v", err)
	}

	routing := func(routeBy string, routes map[string]string) error {
		c := &model.Campaign{
			LanguageRouteBy: nullable.NewNullableWithValue(routeBy),
			LanguageDefault: nullable.NewNullableWithValue("en"),
			LanguageRoutes:  nullable.NewNullableWithValue(routes),
		}
		return c.ValidateLanguageRouting()
	}
	if err := routing(model.LanguageRouteLanguage, map[string]string{"da": "da"}); !isValidationError(err) || !strings.Contains(err.Error(), "languageRoutes") {
		t.Fatalf("routes with the Language field: want a refusal, got %v", err)
	}
	if err := routing(model.LanguageRouteLanguage, map[string]string{}); err != nil {
		t.Fatalf("Language field without routes: %v", err)
	}
	if err := model.AnonymousSelfManagedLanguagesError(model.LanguageRouteLanguage, "en", nil); !isValidationError(err) {
		t.Fatalf("anonymous self managed by the Language field: want a refusal, got %v", err)
	}
}

// TestLanguageFieldRoutingCountsAsSeveralLanguages checks the anonymity gate
// treats Language by Language as more than one language before any row exists
func TestLanguageFieldRoutingCountsAsSeveralLanguages(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "da")
	campaignID := f.campaign([]uuid.UUID{tmpl}, false, false)
	f.route(campaignID, model.LanguageRouteLanguage, "en", nil)
	has, err := f.svc.CampaignRepository.HasLanguageGroupDataByCampaignID(f.ctx, &campaignID)
	if err != nil || !has {
		t.Fatalf("has language group data %v %v, want true", has, err)
	}
}

// TestProgramRoutesByLanguageField checks a program with Language by Language
// stamps each person's own language when every step template has it, else
// the default, and that a step added later can change that for new sends
func TestProgramRoutesByLanguageField(t *testing.T) {
	p := newProgramLanguageTest(t)
	programID := p.program(model.ProgramLoopComplete)
	first := p.languageTemplate("first", "en", "da", "de")
	second := p.languageTemplate("second", "en", "da")
	stepFirst := p.templateStep(programID, first, 0)
	p.templateStep(programID, second, 1)
	campaignID := p.stepCampaign(programID, stepFirst, 0)
	p.routeProgram(programID, model.LanguageRouteLanguage, "en", nil)

	person := func(email string, language string) uuid.UUID {
		id := p.recipient(email)
		if language != "" {
			p.db.Model(&database.Recipient{}).Where("id = ?", id).Update("language", language)
		}
		return id
	}
	people := map[uuid.UUID]string{
		person("dane@example.com", "da"):   "da",
		person("german@example.com", "de"): "en",
		person("none@example.com", ""):     "en",
	}
	for id := range people {
		p.enroll(programID, id, 0)
	}
	if _, err := p.svc.advanceDueBatch(p.ctx, authorizedSession(t), time.Now()); err != nil {
		t.Fatalf("advance batch: %v", err)
	}
	for id, want := range people {
		if got := p.stampOf(campaignID, id); got != want {
			t.Errorf("want %s, got %s", want, got)
		}
	}
	// the program create and update need only the default on every step
	if err := p.svc.checkProgramLanguages(p.ctx, nil, model.LanguageRouteLanguage, "en", nil, p.loadProgram(programID).Steps, nil); err != nil {
		t.Fatalf("default on every step refused: %v", err)
	}
	if err := p.svc.checkProgramLanguages(p.ctx, nil, model.LanguageRouteLanguage, "de", nil, p.loadProgram(programID).Steps, nil); !isValidationError(err) {
		t.Fatalf("default missing on a step: want a refusal, got %v", err)
	}
}
