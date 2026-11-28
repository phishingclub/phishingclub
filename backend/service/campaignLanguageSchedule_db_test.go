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
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
)

// newLanguagePoolFixture is the pool fixture with the email table, so the
// emails of language rows can be checked
func newLanguagePoolFixture(t *testing.T) *poolFixture {
	t.Helper()
	f := newPoolFixture(t)
	if err := f.db.AutoMigrate(&database.Email{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return f
}

// languageEmail stores an email with a marker in its body and subject
func (f *poolFixture) languageEmail(marker string, subject string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if subject == "" {
		subject = "subject " + marker
	}
	if err := f.db.Create(&database.Email{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "email " + marker + " " + id.String()[:8],
		Content: "body " + marker, MailFrom: "bounce@" + marker + ".test", From: "a@" + marker + ".test", Subject: subject,
	}).Error; err != nil {
		f.t.Fatalf("insert email: %v", err)
	}
	return id
}

// languageTemplate stores a pool template written in language with one usable
// language row for each code
func (f *poolFixture) languageTemplate(language string, codes ...string) uuid.UUID {
	f.t.Helper()
	id := f.template(templateOpts{})
	if err := f.db.Model(&database.CampaignTemplate{}).Where("id = ?", id).Update("language", language).Error; err != nil {
		f.t.Fatalf("set template language: %v", err)
	}
	for _, code := range codes {
		f.languageRow(id, code, f.languageEmail(code, ""), true)
	}
	return id
}

func (f *poolFixture) languageRow(templateID uuid.UUID, code string, emailID uuid.UUID, usable bool) {
	f.t.Helper()
	rowID := uuid.New()
	if err := f.db.Create(&database.CampaignTemplateLanguage{
		ID: &rowID, CreatedAt: &f.now, UpdatedAt: &f.now, CampaignTemplateID: &templateID,
		Language: code, EmailID: &emailID, IsUsable: usable,
	}).Error; err != nil {
		f.t.Fatalf("insert language row: %v", err)
	}
}

// countryGroup stores a group with one recipient per country entry and
// returns the recipient ids in the same order
func (f *poolFixture) countryGroup(countries ...string) (uuid.UUID, []uuid.UUID) {
	f.t.Helper()
	groupID := f.group(nil)
	return groupID, f.addCountries(groupID, countries...)
}

func (f *poolFixture) addCountries(groupID uuid.UUID, countries ...string) []uuid.UUID {
	f.t.Helper()
	ids := []uuid.UUID{}
	for _, c := range countries {
		rid := uuid.New()
		email := rid.String() + "@country.test"
		if err := f.db.Create(&database.Recipient{
			ID: &rid, CreatedAt: &f.now, UpdatedAt: &f.now, Email: &email, Country: c,
		}).Error; err != nil {
			f.t.Fatalf("insert recipient: %v", err)
		}
		if err := f.db.Create(&database.RecipientGroupRecipient{RecipientID: &rid, RecipientGroupID: &groupID}).Error; err != nil {
			f.t.Fatalf("insert group member: %v", err)
		}
		ids = append(ids, rid)
	}
	return ids
}

// route turns language routing on for a stored campaign
func (f *poolFixture) route(campaignID uuid.UUID, routeBy string, fallback string, routes map[string]string) {
	f.t.Helper()
	normalized, err := model.NormalizeLanguageRoutes(routes)
	if err != nil {
		f.t.Fatalf("routes: %v", err)
	}
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Updates(map[string]any{
		"language_route_by": routeBy, "language_default": fallback,
	}).Error; err != nil {
		f.t.Fatalf("set routing: %v", err)
	}
	if err := f.db.Where("campaign_id = ?", campaignID).Delete(&database.CampaignLanguageRoute{}).Error; err != nil {
		f.t.Fatalf("clear routes: %v", err)
	}
	for key, code := range normalized {
		if err := f.db.Create(&database.CampaignLanguageRoute{CampaignID: &campaignID, MatchKey: key, Language: code}).Error; err != nil {
			f.t.Fatalf("insert route: %v", err)
		}
	}
}

// stamps returns the stamp of every row of a campaign by recipient, "<nil>"
// for a row with none
func (f *poolFixture) stamps(campaignID uuid.UUID) map[uuid.UUID]string {
	f.t.Helper()
	var rows []database.CampaignRecipient
	if err := f.db.Where("campaign_id = ?", campaignID).Find(&rows).Error; err != nil {
		f.t.Fatalf("read rows: %v", err)
	}
	out := map[uuid.UUID]string{}
	for _, r := range rows {
		v := "<nil>"
		if r.Language != nil {
			v = *r.Language
		}
		out[*r.RecipientID] = v
	}
	return out
}

var testCountryRoutes = map[string]string{
	"Denmark": "da",
	"germany": "de",
	// written in NFC, matched by a recipient written in NFD
	"Åland": "da",
}

// TestScheduleStampsLanguageOnEveryPath checks every insert path of a
// scheduled campaign stamps the routed language: the value is normalized
// (spaces, case, NFD input), and a value with no route or no value gets the
// default
func TestScheduleStampsLanguageOnEveryPath(t *testing.T) {
	countries := []string{"Denmark", " denmark ", "DENMARK", "Germany", "France", "", "Åland"}
	want := []string{"da", "da", "da", "de", "en", "en", "da"}
	for _, path := range []string{"spread", "daily slots", "single"} {
		t.Run(path, func(t *testing.T) {
			f := newLanguagePoolFixture(t)
			tmpl := f.languageTemplate("en", "da", "de")
			list, expect := countries, want
			if path == "single" {
				list, expect = []string{" GERMANY"}, []string{"de"}
			}
			groupID, recipients := f.countryGroup(list...)
			campaignID := f.campaign([]uuid.UUID{tmpl}, false, false)
			f.route(campaignID, model.LanguageRouteCountry, "en", testCountryRoutes)
			campaign := f.loadCampaign(campaignID, groupID)
			if path == "daily slots" {
				campaign.ConstraintWeekDays = nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(127))
				campaign.ConstraintStartTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("00:00"))
				campaign.ConstraintEndTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("23:00"))
				campaign.SendEndAt = nullable.NewNullableWithValue(f.now.Add(72 * time.Hour))
			}
			if err := f.svc.schedule(f.ctx, f.session, campaign); err != nil {
				t.Fatalf("schedule: %v", err)
			}
			got := f.stamps(campaignID)
			if len(got) != len(recipients) {
				t.Fatalf("got %d rows, want %d", len(got), len(recipients))
			}
			for i, rid := range recipients {
				if got[rid] != expect[i] {
					t.Errorf("%q: stamped %s, want %s", list[i], got[rid], expect[i])
				}
			}
		})
	}
}

// TestScheduleRoutingOffStampsTemplateLanguage checks a campaign without
// Language by stamps each row with its template language, and leaves rows of
// a template with no language set without a stamp
func TestScheduleRoutingOffStampsTemplateLanguage(t *testing.T) {
	f := newLanguagePoolFixture(t)
	german := f.languageTemplate("de")
	plain := f.template(templateOpts{})
	groupID := f.group([]string{"a", "a", "b", "b", "c", "c"})
	campaignID := f.campaign([]uuid.UUID{german, plain}, false, false)
	if err := f.svc.schedule(f.ctx, f.session, f.loadCampaign(campaignID, groupID)); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	byTemplate := f.templatesByRecipient(campaignID)
	stamps := f.stamps(campaignID)
	for rid, tid := range byTemplate {
		want := "<nil>"
		if tid == german {
			want = "de"
		}
		if stamps[rid] != want {
			t.Fatalf("row on %v stamped %s, want %s", tid, stamps[rid], want)
		}
	}
}

// TestScheduleRefusesLanguageAPoolTemplateLacks checks schedule refuses
// routing that picks a language one pool template can not send, before any
// row is written
func TestScheduleRefusesLanguageAPoolTemplateLacks(t *testing.T) {
	cases := map[string]struct {
		fallback string
		routes   map[string]string
		setup    func(f *poolFixture, other uuid.UUID)
		field    string
	}{
		"route code missing": {"en", map[string]string{"germany": "fr"}, nil, "languageRoutes"},
		"default missing":    {"fr", map[string]string{"germany": "de"}, nil, "languageDefault"},
		"row not usable":     {"en", map[string]string{"germany": "sv"}, func(f *poolFixture, other uuid.UUID) { f.languageRow(other, "sv", f.languageEmail("sv", ""), false) }, "languageRoutes"},
		"subject breaks": {"en", map[string]string{"germany": "fi"}, func(f *poolFixture, other uuid.UUID) {
			f.languageRow(other, "fi", f.languageEmail("fi", "{{.FirstName"), true)
		}, "languageRoutes"},
		"no template language": {"en", map[string]string{}, func(f *poolFixture, other uuid.UUID) {
			f.db.Model(&database.CampaignTemplate{}).Where("id = ?", other).Update("language", "")
		}, "languageRouteBy"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newLanguagePoolFixture(t)
			full := f.languageTemplate("en", "de", "fr", "sv", "fi")
			other := f.languageTemplate("en", "de")
			if tc.setup != nil {
				tc.setup(f, other)
			}
			groupID, _ := f.countryGroup("Germany", "Denmark", "France")
			campaignID := f.campaign([]uuid.UUID{full, other}, false, false)
			f.route(campaignID, model.LanguageRouteCountry, tc.fallback, tc.routes)
			err := f.svc.schedule(f.ctx, f.session, f.loadCampaign(campaignID, groupID))
			if !isValidationError(err) || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("want a validation error on %s, got %v", tc.field, err)
			}
			if n := len(f.recipientsOf(campaignID)); n != 0 {
				t.Fatalf("a refused schedule wrote %d rows", n)
			}
		})
	}
}

// TestCreateAndUpdateRefuseLanguageAPoolTemplateLacks checks the campaign
// create and update refuse routing that picks a language a pool template
// can not send, naming the template
func TestCreateAndUpdateRefuseLanguageAPoolTemplateLacks(t *testing.T) {
	f := newLanguagePoolFixture(t)
	full := f.languageTemplate("en", "de")
	partial := f.languageTemplate("en")
	groupID, _ := f.countryGroup("Germany")
	create := &model.Campaign{
		Name:              nullable.NewNullableWithValue(*vo.NewString64Must("language campaign")),
		SortField:         nullable.NewNullableWithValue(*vo.NewCampaignSortFieldMust("email")),
		SortOrder:         nullable.NewNullableWithValue(*vo.NewCampaignSendingOrderMust("asc")),
		SendStartAt:       nullable.NewNullableWithValue(f.now.Add(2 * time.Hour)),
		SendEndAt:         nullable.NewNullableWithValue(f.now.Add(4 * time.Hour)),
		TemplateIDs:       nullable.NewNullableWithValue([]*uuid.UUID{&full, &partial}),
		RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{&groupID}),
		LanguageRouteBy:   nullable.NewNullableWithValue(model.LanguageRouteCountry),
		LanguageDefault:   nullable.NewNullableWithValue("en"),
		LanguageRoutes:    nullable.NewNullableWithValue(map[string]string{"Germany": "de"}),
	}
	_, err := f.svc.Create(f.ctx, f.session, create)
	if !isValidationError(err) || !strings.Contains(err.Error(), "German") {
		t.Fatalf("create: want a refusal naming German, got %v", err)
	}
	var count int64
	f.db.Model(&database.Campaign{}).Count(&count)
	if count != 0 {
		t.Fatalf("a refused create stored %d campaigns", count)
	}

	// a self managed campaign on the full template takes the routes, a pool
	// change that adds the partial template is refused
	campaignID, campaign, groupID := f.selfManagedWithDepartments([]uuid.UUID{full}, []string{"x"})
	incoming := selfManagedUpdate(campaign, groupID)
	incoming.LanguageRouteBy = nullable.NewNullableWithValue(model.LanguageRouteCountry)
	incoming.LanguageDefault = nullable.NewNullableWithValue("en")
	incoming.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{"germany": "de"})
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); err != nil {
		t.Fatalf("update with routes the pool has: %v", err)
	}
	if err := f.updatePool(campaignID, campaign, groupID, full, partial); !isValidationError(err) || !strings.Contains(err.Error(), "German") {
		t.Fatalf("pool change: want a refusal naming German, got %v", err)
	}
	pool, err := f.svc.CampaignRepository.GetTemplatePoolIDs(f.ctx, &campaignID)
	if err != nil || len(pool) != 1 || pool[0] != full {
		t.Fatalf("a refused update changed the pool: %v %v", pool, err)
	}
}

// TestSelfManagedAdditionsKeepExistingStamps checks a reschedule of a self
// managed campaign stamps only the new recipients, with the routes as they
// are now, and never changes a stamp already written
func TestSelfManagedAdditionsKeepExistingStamps(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "da", "de")
	groupID, first := f.countryGroup("Denmark", "Germany")
	campaignID := f.campaign([]uuid.UUID{tmpl}, true, false)
	f.route(campaignID, model.LanguageRouteCountry, "en", map[string]string{"denmark": "da", "germany": "de"})
	campaign := f.loadCampaign(campaignID, groupID)
	campaign.SendStartAt.SetNull()
	campaign.SendEndAt.SetNull()
	if err := f.svc.schedule(f.ctx, f.session, campaign); err != nil {
		t.Fatalf("first schedule: %v", err)
	}
	// Denmark now routes to German, a live change that must not re route the
	// planned rows
	f.route(campaignID, model.LanguageRouteCountry, "en", map[string]string{"denmark": "de", "germany": "de"})
	added := f.addCountries(groupID, "Denmark")
	campaign = f.loadCampaign(campaignID, groupID)
	campaign.SendStartAt.SetNull()
	campaign.SendEndAt.SetNull()
	if err := f.svc.schedule(f.ctx, f.session, campaign); err != nil {
		t.Fatalf("second schedule: %v", err)
	}
	stamps := f.stamps(campaignID)
	if stamps[first[0]] != "da" || stamps[first[1]] != "de" {
		t.Fatalf("existing stamps changed: %v", stamps)
	}
	if stamps[added[0]] != "de" {
		t.Fatalf("new recipient stamped %s, want de", stamps[added[0]])
	}
}

// selfManagedScheduled stores a self managed campaign on pool, sets its
// routing when routeBy is set and schedules it
func (f *poolFixture) selfManagedScheduled(pool []uuid.UUID, groupID uuid.UUID, anonymous bool, routeBy string, routes map[string]string) (uuid.UUID, *model.Campaign) {
	f.t.Helper()
	campaignID := f.campaign(pool, true, anonymous)
	if routeBy != "" {
		f.route(campaignID, routeBy, "en", routes)
	}
	campaign := f.loadCampaign(campaignID, groupID)
	campaign.SendStartAt.SetNull()
	campaign.SendEndAt.SetNull()
	if err := f.svc.schedule(f.ctx, f.session, campaign); err != nil {
		f.t.Fatalf("schedule: %v", err)
	}
	return campaignID, campaign
}

func (f *poolFixture) clearStamp(recipientID uuid.UUID) {
	f.t.Helper()
	if err := f.db.Model(&database.CampaignRecipient{}).
		Where("recipient_id = ?", recipientID.String()).
		Update("language", nil).Error; err != nil {
		f.t.Fatalf("clear stamp: %v", err)
	}
}

// TestPoolMoveKeepsRoutedLanguage checks that with Language by on a self
// managed pool change keeps the language of moved rows, stamps rows with no
// language with the language of the template they had in the same write, and
// refuses a move to a template without a moved row's language
func TestPoolMoveKeepsRoutedLanguage(t *testing.T) {
	f := newLanguagePoolFixture(t)
	english := f.languageTemplate("en", "de", "da")
	german := f.languageTemplate("de", "en")
	groupID, recipients := f.countryGroup("Denmark", "Germany", "Germany")
	campaignID, campaign := f.selfManagedScheduled([]uuid.UUID{english}, groupID, false,
		model.LanguageRouteCountry, map[string]string{"denmark": "da", "germany": "de"})
	// a row planned before languages existed
	f.clearStamp(recipients[2])
	// Danish is no longer routed, so the routes fit German, but the Danish row
	// would move to a template without Danish
	f.route(campaignID, model.LanguageRouteCountry, "en", map[string]string{"germany": "de"})
	campaign = f.loadCampaign(campaignID, groupID)
	err := f.updatePool(campaignID, campaign, groupID, german)
	if !isValidationError(err) || !strings.Contains(err.Error(), "can not move") {
		t.Fatalf("move of a Danish row to German: want a refusal of the move, got %v", err)
	}
	if got := f.stamps(campaignID)[recipients[2]]; got != "<nil>" {
		t.Fatalf("a refused change stamped a row: %s", got)
	}
	// the Danish row was sent to, so it stays and nothing blocks the move
	if err := f.db.Model(&database.CampaignRecipient{}).Where("recipient_id = ?", recipients[0].String()).
		Update("sent_at", utils.NowRFC3339UTC()).Error; err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	if err := f.updatePool(campaignID, campaign, groupID, german); err != nil {
		t.Fatalf("move to German: %v", err)
	}
	templates := f.templatesByRecipient(campaignID)
	if templates[recipients[0]] != english || templates[recipients[1]] != german || templates[recipients[2]] != german {
		t.Fatalf("templates after the move: %v", templates)
	}
	stamps := f.stamps(campaignID)
	want := map[uuid.UUID]string{recipients[0]: "da", recipients[1]: "de", recipients[2]: "en"}
	for rid, code := range want {
		if stamps[rid] != code {
			t.Fatalf("after the move %v is %s, want %s", rid, stamps[rid], code)
		}
	}
}

// TestPoolMoveRoutingOffFollowsTemplate checks that with Language by off a
// pool whose templates are in different languages can still lose a template:
// the moved rows get the language of their new template, also in an
// anonymous self managed campaign where everyone moves and in a scheduled
// campaign that is made again
func TestPoolMoveRoutingOffFollowsTemplate(t *testing.T) {
	t.Run("self managed", func(t *testing.T) {
		f := newLanguagePoolFixture(t)
		english := f.languageTemplate("en")
		danish := f.languageTemplate("da")
		groupID := f.group([]string{"x", "x", "y", "y"})
		campaignID, campaign := f.selfManagedScheduled([]uuid.UUID{english, danish}, groupID, false, "", nil)
		before := f.templatesByRecipient(campaignID)
		for rid, tid := range before {
			if want := map[uuid.UUID]string{english: "en", danish: "da"}[tid]; f.stamps(campaignID)[rid] != want {
				t.Fatalf("row on %v not stamped %s", tid, want)
			}
		}
		// a row planned before languages existed on the Danish template
		for rid, tid := range before {
			if tid == danish {
				f.clearStamp(rid)
				break
			}
		}
		if err := f.updatePool(campaignID, campaign, groupID, english); err != nil {
			t.Fatalf("remove the Danish template: %v", err)
		}
		stamps := f.stamps(campaignID)
		for rid, tid := range f.templatesByRecipient(campaignID) {
			if tid != english || stamps[rid] != "en" {
				t.Fatalf("row %v on %v stamped %s, want English", rid, tid, stamps[rid])
			}
		}
	})
	t.Run("anonymous self managed", func(t *testing.T) {
		f := newLanguagePoolFixture(t)
		english := f.languageTemplate("en")
		danish := f.languageTemplate("da")
		groupID, recipients := f.countryGroup("Denmark", "Denmark")
		campaignID, campaign := f.selfManagedScheduled([]uuid.UUID{danish}, groupID, true, "", nil)
		// a sent row moves too in an anonymous campaign
		f.db.Model(&database.CampaignRecipient{}).Where("recipient_id = ?", recipients[0].String()).
			Update("sent_at", utils.NowRFC3339UTC())
		if err := f.updatePool(campaignID, campaign, groupID, english); err != nil {
			t.Fatalf("switch template: %v", err)
		}
		stamps := f.stamps(campaignID)
		for rid, tid := range f.templatesByRecipient(campaignID) {
			if tid != english || stamps[rid] != "en" {
				t.Fatalf("row %v on %v stamped %s, want English", rid, tid, stamps[rid])
			}
		}
	})
	t.Run("scheduled rebuild", func(t *testing.T) {
		f := newLanguagePoolFixture(t)
		english := f.languageTemplate("en")
		danish := f.languageTemplate("da")
		groupID := f.group([]string{"x", "x", "y", "y"})
		campaignID := f.campaign([]uuid.UUID{english, danish}, false, false)
		campaign := f.loadCampaign(campaignID, groupID)
		if err := f.svc.schedule(f.ctx, f.session, campaign); err != nil {
			t.Fatalf("schedule: %v", err)
		}
		incoming := &model.Campaign{
			Name:              campaign.Name,
			SortField:         campaign.SortField,
			SortOrder:         campaign.SortOrder,
			SendStartAt:       campaign.SendStartAt,
			SendEndAt:         campaign.SendEndAt,
			RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{&groupID}),
			TemplateIDs:       nullable.NewNullableWithValue([]*uuid.UUID{&english}),
		}
		if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); err != nil {
			t.Fatalf("remove the Danish template: %v", err)
		}
		stamps := f.stamps(campaignID)
		if len(stamps) != 4 {
			t.Fatalf("rebuilt %d rows, want 4", len(stamps))
		}
		for rid, tid := range f.templatesByRecipient(campaignID) {
			if tid != english || stamps[rid] != "en" {
				t.Fatalf("row %v on %v stamped %s, want English", rid, tid, stamps[rid])
			}
		}
	})
}

// TestRecipientFieldValues checks the field values of groups are merged by
// normalized key, counted once per recipient, sorted by count, capped and
// read only from groups the company may use
func TestRecipientFieldValues(t *testing.T) {
	f := newLanguagePoolFixture(t)
	if err := f.db.AutoMigrate(&database.Company{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := &RecipientGroup{Common: f.svc.Common, RecipientGroupRepository: f.svc.RecipientGroupRepository}
	static, ids := f.countryGroup("Denmark", " denmark ", "Germany", "Åland", "", "Denmark")
	// a dynamic group of every German recipient, one of them also in the
	// static group
	dynamicID := uuid.New()
	if err := f.db.Create(&database.RecipientGroup{
		ID: &dynamicID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "dynamic", IsDynamic: true,
		FilterField: "country", FilterValue: "Germany",
	}).Error; err != nil {
		t.Fatalf("insert dynamic group: %v", err)
	}
	other := f.group(nil)
	f.addCountries(other, "Germany")
	// a deleted recipient is left out
	f.db.Model(&database.Recipient{}).Where("id = ?", ids[5]).Update("deleted_at", utils.NowRFC3339UTC())

	got, err := svc.GetFieldValues(f.ctx, f.session, nil, model.LanguageRouteCountry, []*uuid.UUID{&static, &dynamicID})
	if err != nil {
		t.Fatalf("field values: %v", err)
	}
	if got.EmptyCount != 1 || got.Total != 3 || got.Truncated || got.UnlistedCount != 0 {
		t.Fatalf("totals: %+v", got)
	}
	if got.Values[0].Key != "denmark" || got.Values[0].Count != 2 || len(got.Values[0].Values) != 2 {
		t.Fatalf("first value: %+v", got.Values[0])
	}
	if got.Values[1].Key != "germany" || got.Values[1].Count != 2 {
		t.Fatalf("Germany counts each recipient once: %+v", got.Values[1])
	}
	if got.Values[2].Key != "åland" || got.Values[2].Count != 1 {
		t.Fatalf("NFD value not merged: %+v", got.Values[2])
	}

	if _, err := svc.GetFieldValues(f.ctx, f.session, nil, "email", []*uuid.UUID{&static}); !isValidationError(err) {
		t.Fatalf("a field that is not a route field: want validation error, got %v", err)
	}
	// a group of a company is refused in the global view and in another
	// company
	companyID, otherCompany := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{companyID, otherCompany} {
		id := id
		f.db.Create(&database.Company{ID: id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "c " + id.String()[:8], AssetsKey: id.String()[:8]})
	}
	f.db.Model(&database.RecipientGroup{}).Where("id = ?", other).Update("company_id", companyID)
	if _, err := svc.GetFieldValues(f.ctx, f.session, nil, model.LanguageRouteCountry, []*uuid.UUID{&other}); !isValidationError(err) {
		t.Fatalf("company group in the global view: want validation error, got %v", err)
	}
	if _, err := svc.GetFieldValues(f.ctx, f.session, &otherCompany, model.LanguageRouteCountry, []*uuid.UUID{&other}); !isValidationError(err) {
		t.Fatalf("company group in another company: want validation error, got %v", err)
	}
	if got, err := svc.GetFieldValues(f.ctx, f.session, &companyID, model.LanguageRouteCountry, []*uuid.UUID{&other}); err != nil || got.Total != 1 {
		t.Fatalf("company group in its company: %+v %v", got, err)
	}
	if _, err := svc.GetFieldValues(f.ctx, unauthorizedSession(t), nil, model.LanguageRouteCountry, []*uuid.UUID{&static}); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("a company user: want authorization failure, got %v", err)
	}
}

// TestMergeFieldValuesCap checks more than the listed number of values is
// cut and flagged, with the total of all values and the members of the values
// that are not listed
func TestMergeFieldValuesCap(t *testing.T) {
	rows := []repository.FieldValueCount{}
	for i := 0; i < model.MaxRecipientFieldValues+20; i++ {
		rows = append(rows, repository.FieldValueCount{Value: "v" + uuid.NewString(), Count: 1})
	}
	rows = append(rows, repository.FieldValueCount{Value: "", Count: 3})
	got := mergeFieldValues(model.LanguageRouteCity, &repository.FieldValueCounts{
		Rows: rows, Members: model.MaxRecipientFieldValues + 23, Distinct: len(rows),
	})
	if len(got.Values) != model.MaxRecipientFieldValues || !got.Truncated || got.Total != model.MaxRecipientFieldValues+20 {
		t.Fatalf("cap: %d values, truncated %v, total %d", len(got.Values), got.Truncated, got.Total)
	}
	if got.EmptyCount != 3 || got.UnlistedCount != 20 {
		t.Fatalf("empty %d and unlisted %d, want 3 and 20", got.EmptyCount, got.UnlistedCount)
	}
}

// TestFieldValuesRawCutMergesToFewValues checks the raw values are cut at
// MaxFieldValueRows also when they merge into fewer listed values than the
// cap: truncated stays set, the total is more than the listed values and the
// members of the values that were not read are counted as unlisted
func TestFieldValuesRawCutMergesToFewValues(t *testing.T) {
	f := newLanguagePoolFixture(t)
	svc := &RecipientGroup{Common: f.svc.Common, RecipientGroupRepository: f.svc.RecipientGroupRepository}
	groupID := f.group(nil)
	// every value is "x" with other spaces around it, so they all merge
	values := []string{}
	for i := 0; len(values) < repository.MaxFieldValueRows+5; i++ {
		for j := 0; j <= i && len(values) < repository.MaxFieldValueRows+5; j++ {
			values = append(values, strings.Repeat(" ", j)+"x"+strings.Repeat(" ", i-j))
		}
	}
	recipients := make([]database.Recipient, 0, len(values))
	members := make([]database.RecipientGroupRecipient, 0, len(values))
	for _, v := range values {
		rid := uuid.New()
		email := rid.String() + "@cut.test"
		recipients = append(recipients, database.Recipient{ID: &rid, CreatedAt: &f.now, UpdatedAt: &f.now, Email: &email, City: v})
		members = append(members, database.RecipientGroupRecipient{RecipientID: &rid, RecipientGroupID: &groupID})
	}
	if err := f.db.CreateInBatches(recipients, 200).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.CreateInBatches(members, 200).Error; err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetFieldValues(f.ctx, f.session, nil, model.LanguageRouteCity, []*uuid.UUID{&groupID})
	if err != nil {
		t.Fatalf("field values: %v", err)
	}
	if len(got.Values) != 1 || got.Values[0].Key != "x" || got.Values[0].Count != repository.MaxFieldValueRows {
		t.Fatalf("values: %+v", got.Values)
	}
	if !got.Truncated || got.Total <= len(got.Values) {
		t.Fatalf("a cut must stay visible: truncated %v, total %d", got.Truncated, got.Total)
	}
	if got.UnlistedCount != 5 || got.EmptyCount != 0 {
		t.Fatalf("unlisted %d and empty %d, want 5 and 0", got.UnlistedCount, got.EmptyCount)
	}
}

// TestSelfManagedRoutingChangeRestampsUnsent checks an update of a self
// managed campaign that changes Language by, the default or the routes gives
// the rows not sent yet the new language, and leaves rows that are sent, have
// an event or are claimed by a sender with the language they had. An update
// that does not change the routing changes no stamp.
func TestSelfManagedRoutingChangeRestampsUnsent(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "da", "de")
	groupID, r := f.countryGroup("Denmark", "Denmark", "Denmark", "Denmark", "Germany")
	campaignID, campaign := f.selfManagedScheduled([]uuid.UUID{tmpl}, groupID, false,
		model.LanguageRouteCountry, map[string]string{"denmark": "da"})
	set := func(recipientID uuid.UUID, column string, v any) {
		t.Helper()
		if err := f.db.Model(&database.CampaignRecipient{}).Where("recipient_id = ?", recipientID.String()).
			Update(column, v).Error; err != nil {
			t.Fatal(err)
		}
	}
	set(r[0], "sent_at", utils.NowRFC3339UTC())
	f.event(campaignID, r[1], data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	set(r[2], "last_attempt_at", utils.NowRFC3339UTC())
	check := func(when string, want ...string) {
		t.Helper()
		stamps := f.stamps(campaignID)
		for i, code := range want {
			if stamps[r[i]] != code {
				t.Fatalf("%s: recipient %d is %s, want %s (all %v)", when, i, stamps[r[i]], code, stamps)
			}
		}
	}
	check("scheduled", "da", "da", "da", "da", "en")

	update := func(routeBy string, fallback string, routes map[string]string) {
		t.Helper()
		incoming := selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID)
		incoming.LanguageRouteBy = nullable.NewNullableWithValue(routeBy)
		incoming.LanguageDefault = nullable.NewNullableWithValue(fallback)
		incoming.LanguageRoutes = nullable.NewNullableWithValue(routes)
		if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); err != nil {
			t.Fatalf("update routing: %v", err)
		}
	}
	update(model.LanguageRouteCountry, "en", map[string]string{"denmark": "de", "germany": "de"})
	check("routes changed", "da", "da", "da", "de", "de")
	update(model.LanguageRouteCountry, "da", map[string]string{"denmark": "de"})
	check("default changed", "da", "da", "da", "de", "da")
	// routing off: the rows get the template language
	update("", "", map[string]string{})
	check("routing off", "da", "da", "da", "en", "en")

	// an update that leaves the routing as it is changes no stamp
	set(r[3], "language", "de")
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, selfManagedUpdate(campaign, groupID)); err != nil {
		t.Fatalf("update without routing: %v", err)
	}
	update("", "", map[string]string{})
	check("routing unchanged", "da", "da", "da", "de", "en")
}

// TestLateScheduleBrokenLanguageClosesCampaign checks a late scheduled
// campaign whose routed language email no longer parses is closed with the
// reason when its schedule time comes, not left pending. An anonymous
// campaign's reason does not name the language.
func TestLateScheduleBrokenLanguageClosesCampaign(t *testing.T) {
	for _, event := range []string{data.EVENT_CAMPAIGN_PENDING_SCHEDULE, data.EVENT_CAMPAIGN_CLOSED} {
		if cache.EventIDByName[event] == nil {
			id := uuid.New()
			cache.EventIDByName[event] = &id
			cache.EventNameByID[id.String()] = event
		}
	}
	run := func(anonymous bool) string {
		f := newLanguagePoolFixture(t)
		tmpl := f.languageTemplate("en", "de")
		var row database.CampaignTemplateLanguage
		if err := f.db.Where("campaign_template_id = ? AND language = ?", tmpl.String(), "de").First(&row).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.db.Model(&database.Email{}).Where("id = ?", row.EmailID.String()).
			Update("subject", "Hallo {{.FirstName").Error; err != nil {
			t.Fatal(err)
		}
		groupID, _ := f.countryGroup("Germany", "Denmark")
		campaignID := f.campaign([]uuid.UUID{tmpl}, false, anonymous)
		f.route(campaignID, model.LanguageRouteCountry, "en", map[string]string{"germany": "de"})
		if err := f.db.Create(&database.CampaignRecipientGroup{CampaignID: &campaignID, RecipientGroupID: &groupID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.db.Model(&database.Campaign{}).Where("id = ?", campaignID.String()).Updates(map[string]any{
			"schedule_at":      utils.RFC3339UTC(f.now.Add(-time.Minute)),
			"notable_event_id": cache.EventIDByName[data.EVENT_CAMPAIGN_PENDING_SCHEDULE].String(),
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.svc.SchedulePendingCampaigns(f.ctx, f.session); err != nil {
			t.Fatalf("schedule pending: %v", err)
		}
		var stored database.Campaign
		if err := f.db.Where("id = ?", campaignID.String()).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if stored.ClosedAt == nil {
			t.Fatal("a late scheduled campaign with a broken language must be closed")
		}
		var rows int64
		f.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignID.String()).Count(&rows)
		if rows != 0 {
			t.Fatalf("no row may be scheduled, got %d", rows)
		}
		var event database.CampaignEvent
		if err := f.db.Where("campaign_id = ? AND event_id = ?", campaignID.String(),
			cache.EventIDByName[data.EVENT_CAMPAIGN_CLOSED].String()).First(&event).Error; err != nil {
			t.Fatalf("close event: %v", err)
		}
		return event.Data
	}
	if reason := run(false); !strings.Contains(reason, "could not be scheduled") || !strings.Contains(reason, "German") {
		t.Fatalf("named close reason: %q", reason)
	}
	if reason := run(true); !strings.Contains(reason, "could not be scheduled") || strings.Contains(reason, "German") {
		t.Fatalf("anonymous close reason must not name the language: %q", reason)
	}
}
