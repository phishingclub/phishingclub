package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
)

// lateRecipient holds the fields a late schedule test sets on a recipient
type lateRecipient struct {
	department string
	country    string
	language   *string
}

// lateMember stores a recipient with the given fields, in the static group
// when staticGroup is not nil, and returns its id
func (f *poolFixture) lateMember(staticGroup *uuid.UUID, r lateRecipient) uuid.UUID {
	f.t.Helper()
	rid := uuid.New()
	email := rid.String() + "@late.test"
	if err := f.db.Create(&database.Recipient{
		ID: &rid, CreatedAt: &f.now, UpdatedAt: &f.now, Email: &email,
		Department: r.department, Country: r.country, Language: r.language,
	}).Error; err != nil {
		f.t.Fatalf("insert recipient: %v", err)
	}
	if staticGroup != nil {
		if err := f.db.Create(&database.RecipientGroupRecipient{RecipientID: &rid, RecipientGroupID: staticGroup}).Error; err != nil {
			f.t.Fatalf("insert group member: %v", err)
		}
	}
	return rid
}

// dynamicGroup stores a dynamic group that matches field = value
func (f *poolFixture) dynamicGroup(field string, value string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if err := f.db.Create(&database.RecipientGroup{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "dynamic " + id.String(),
		IsDynamic: true, FilterField: field, FilterValue: value,
	}).Error; err != nil {
		f.t.Fatalf("insert dynamic group: %v", err)
	}
	return id
}

// setRecipient changes columns of a stored recipient
func (f *poolFixture) setRecipient(id uuid.UUID, values map[string]any) {
	f.t.Helper()
	if err := f.db.Model(&database.Recipient{}).Where("id = ?", id).Updates(values).Error; err != nil {
		f.t.Fatalf("update recipient: %v", err)
	}
}

// lateCampaign links the campaign to the group and makes it wait for a late
// schedule an hour from now, as a campaign saved with a schedule time does
func (f *poolFixture) lateCampaign(campaignID uuid.UUID, groupID uuid.UUID) {
	f.t.Helper()
	for _, event := range []string{data.EVENT_CAMPAIGN_PENDING_SCHEDULE, data.EVENT_CAMPAIGN_CLOSED} {
		if cache.EventIDByName[event] == nil {
			id := uuid.New()
			cache.EventIDByName[event] = &id
			cache.EventNameByID[id.String()] = event
		}
	}
	if err := f.db.Create(&database.CampaignRecipientGroup{CampaignID: &campaignID, RecipientGroupID: &groupID}).Error; err != nil {
		f.t.Fatalf("link group: %v", err)
	}
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", campaignID.String()).Updates(map[string]any{
		"schedule_at":      utils.RFC3339UTC(f.now.Add(time.Hour)),
		"send_start_at":    utils.RFC3339UTC(f.now.Add(2 * time.Hour)),
		"send_end_at":      utils.RFC3339UTC(f.now.Add(6 * time.Hour)),
		"notable_event_id": cache.EventIDByName[data.EVENT_CAMPAIGN_PENDING_SCHEDULE].String(),
	}).Error; err != nil {
		f.t.Fatalf("set schedule: %v", err)
	}
	// the task before the schedule time writes nothing
	f.runLateSchedule()
	if rows := f.stamps(campaignID); len(rows) != 0 {
		f.t.Fatalf("a campaign before its schedule time got %d rows", len(rows))
	}
}

// makeDue moves the schedule time of the campaign into the past and runs the
// task the task runner calls every tick
func (f *poolFixture) makeDue(campaignID uuid.UUID) {
	f.t.Helper()
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", campaignID.String()).
		Update("schedule_at", utils.RFC3339UTC(f.now.Add(-time.Minute))).Error; err != nil {
		f.t.Fatalf("make due: %v", err)
	}
	f.runLateSchedule()
	var stored database.Campaign
	if err := f.db.Where("id = ?", campaignID.String()).First(&stored).Error; err != nil {
		f.t.Fatalf("read campaign: %v", err)
	}
	if stored.ClosedAt != nil {
		f.t.Fatal("the late scheduled campaign was closed")
	}
	if stored.ScheduleAt != nil {
		f.t.Fatal("the late scheduled campaign still waits for a schedule")
	}
}

func (f *poolFixture) runLateSchedule() {
	f.t.Helper()
	if err := f.svc.SchedulePendingCampaigns(f.ctx, f.session); err != nil {
		f.t.Fatalf("schedule pending: %v", err)
	}
}

// checkStamps compares the stamps of a campaign with want exactly: every
// wanted recipient has a row with the wanted stamp and no other row exists
func checkStamps(t *testing.T, name string, got map[uuid.UUID]string, want map[uuid.UUID]string) {
	t.Helper()
	for rid, code := range want {
		stamp, ok := got[rid]
		if !ok {
			t.Errorf("%s: recipient %s has no row", name, rid)
			continue
		}
		if stamp != code {
			t.Errorf("%s: recipient %s stamped %s, want %s", name, rid, stamp, code)
		}
	}
	for rid, stamp := range got {
		if _, ok := want[rid]; !ok {
			t.Errorf("%s: recipient %s got a row (%s) and should not", name, rid, stamp)
		}
	}
}

// TestLateDynamicScheduleLanguageByCountry checks a late scheduled campaign on
// a dynamic group with Language by Country takes the country of each member
// and the members of the group as they are at schedule time
func TestLateDynamicScheduleLanguageByCountry(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "da", "de")
	groupID := f.dynamicGroup("department", "Sales")
	moves := f.lateMember(nil, lateRecipient{department: "Sales", country: "Denmark"})
	stays := f.lateMember(nil, lateRecipient{department: "Sales", country: "Germany"})
	leaves := f.lateMember(nil, lateRecipient{department: "Sales", country: "Denmark"})
	joins := f.lateMember(nil, lateRecipient{department: "IT", country: "Germany"})
	unrouted := f.lateMember(nil, lateRecipient{department: "Sales", country: "Germany"})
	campaignID := f.campaign([]uuid.UUID{tmpl}, false, false)
	f.route(campaignID, model.LanguageRouteCountry, "en", map[string]string{"denmark": "da", "germany": "de"})
	f.lateCampaign(campaignID, groupID)

	f.setRecipient(moves, map[string]any{"country": "Germany"})
	f.setRecipient(leaves, map[string]any{"department": "IT"})
	f.setRecipient(joins, map[string]any{"department": "Sales"})
	f.setRecipient(unrouted, map[string]any{"country": "France"})
	added := f.lateMember(nil, lateRecipient{department: "Sales", country: "Denmark"})
	f.makeDue(campaignID)

	checkStamps(t, "dynamic by country", f.stamps(campaignID), map[uuid.UUID]string{
		moves: "de", stays: "de", joins: "de", unrouted: "en", added: "da",
	})
}

// TestLateDynamicScheduleLanguageByLanguage checks a late scheduled campaign
// on a dynamic group with Language by Language: the language of each member at
// schedule time is stamped, a language the templates lack gets the default,
// and members who join or leave the group in between are counted as they
// are at schedule time. The group filters on language itself, so a language
// change also moves a member in or out.
func TestLateDynamicScheduleLanguageByLanguage(t *testing.T) {
	f := newLanguagePoolFixture(t)
	full := f.languageTemplate("en", "da", "de", "fr")
	partial := f.languageTemplate("en", "da", "de")

	// a group on department: changes of language keep the member
	byDepartment := f.dynamicGroup("department", "Sales")
	toGerman := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("da")})
	toFrench := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("da")})
	toItalian := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("de")})
	cleared := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("de")})
	gained := f.lateMember(nil, lateRecipient{department: "Sales"})
	leaves := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("da")})
	joins := f.lateMember(nil, lateRecipient{department: "IT", language: strPtr("de")})
	campaignID := f.campaign([]uuid.UUID{full, partial}, false, false)
	f.route(campaignID, model.LanguageRouteLanguage, "en", nil)
	f.lateCampaign(campaignID, byDepartment)

	f.setRecipient(toGerman, map[string]any{"language": "de"})
	// French is on one template only, so it falls back to the default
	f.setRecipient(toFrench, map[string]any{"language": "fr"})
	// no template has Italian
	f.setRecipient(toItalian, map[string]any{"language": "it"})
	f.setRecipient(cleared, map[string]any{"language": nil})
	f.setRecipient(gained, map[string]any{"language": "da"})
	f.setRecipient(leaves, map[string]any{"department": "IT"})
	f.setRecipient(joins, map[string]any{"department": "Sales"})
	added := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("de")})
	f.makeDue(campaignID)
	checkStamps(t, "dynamic on department", f.stamps(campaignID), map[uuid.UUID]string{
		toGerman: "de", toFrench: "en", toItalian: "en", cleared: "en",
		gained: "da", joins: "de", added: "de",
	})

	// a group on language: a member who changes language leaves, and one who
	// takes the language joins
	g := newLanguagePoolFixture(t)
	tmpl := g.languageTemplate("en", "da", "de")
	danes := g.dynamicGroup("language", "da")
	stays := g.lateMember(nil, lateRecipient{language: strPtr("da")})
	becomesGerman := g.lateMember(nil, lateRecipient{language: strPtr("da")})
	becomesDane := g.lateMember(nil, lateRecipient{language: strPtr("de")})
	noneToDane := g.lateMember(nil, lateRecipient{})
	campaign2 := g.campaign([]uuid.UUID{tmpl}, false, false)
	g.route(campaign2, model.LanguageRouteLanguage, "en", nil)
	g.lateCampaign(campaign2, danes)

	g.setRecipient(becomesGerman, map[string]any{"language": "de"})
	g.setRecipient(becomesDane, map[string]any{"language": "da"})
	g.setRecipient(noneToDane, map[string]any{"language": "da"})
	g.makeDue(campaign2)
	checkStamps(t, "dynamic on language", g.stamps(campaign2), map[uuid.UUID]string{
		stays: "da", becomesDane: "da", noneToDane: "da",
	})
}

// TestLateDynamicScheduleSoftDeleted checks members deleted or disabled by
// SCIM between save and schedule time are left out of a dynamic group
func TestLateDynamicScheduleSoftDeleted(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "da")
	groupID := f.dynamicGroup("department", "Sales")
	kept := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("da")})
	deleted := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("da")})
	disabled := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("da")})
	campaignID := f.campaign([]uuid.UUID{tmpl}, false, false)
	f.route(campaignID, model.LanguageRouteLanguage, "en", nil)
	f.lateCampaign(campaignID, groupID)

	f.setRecipient(deleted, map[string]any{"deleted_at": f.now})
	f.setRecipient(disabled, map[string]any{"scim_soft_deleted_at": f.now})
	f.makeDue(campaignID)
	checkStamps(t, "soft deleted", f.stamps(campaignID), map[uuid.UUID]string{kept: "da"})
}

// TestLateStaticScheduleLanguage is the same for a static group: values at
// schedule time are used, a member taken out is left out and a member added
// is included, for Language by Language and Language by Country
func TestLateStaticScheduleLanguage(t *testing.T) {
	for _, routeBy := range []string{model.LanguageRouteLanguage, model.LanguageRouteCountry} {
		f := newLanguagePoolFixture(t)
		tmpl := f.languageTemplate("en", "da", "de")
		groupID := f.group(nil)
		changes := f.lateMember(&groupID, lateRecipient{country: "Denmark", language: strPtr("da")})
		toUnknown := f.lateMember(&groupID, lateRecipient{country: "Denmark", language: strPtr("da")})
		removed := f.lateMember(&groupID, lateRecipient{country: "Denmark", language: strPtr("da")})
		campaignID := f.campaign([]uuid.UUID{tmpl}, false, false)
		routes := map[string]string{"denmark": "da", "germany": "de"}
		if routeBy == model.LanguageRouteLanguage {
			routes = nil
		}
		f.route(campaignID, routeBy, "en", routes)
		f.lateCampaign(campaignID, groupID)

		f.setRecipient(changes, map[string]any{"country": "Germany", "language": "de"})
		// a language and a country the campaign has no route or template for
		f.setRecipient(toUnknown, map[string]any{"country": "Spain", "language": "es"})
		if err := f.db.Where("recipient_id = ? AND recipient_group_id = ?", removed, groupID).
			Delete(&database.RecipientGroupRecipient{}).Error; err != nil {
			t.Fatalf("remove member: %v", err)
		}
		added := f.lateMember(&groupID, lateRecipient{country: "Germany", language: strPtr("de")})
		f.makeDue(campaignID)
		checkStamps(t, "static by "+routeBy, f.stamps(campaignID), map[uuid.UUID]string{
			changes: "de", toUnknown: "en", added: "de",
		})
	}
}

// TestLateDynamicScheduleWithoutLanguages checks a campaign without language
// routing on a late dynamic schedule stamps the template language, or nothing
// when the template has none, whatever the recipients' language and country
func TestLateDynamicScheduleWithoutLanguages(t *testing.T) {
	for name, tc := range map[string]struct {
		templateLanguage string
		want             string
	}{
		"template with a language":    {"en", "en"},
		"template without a language": {"", "<nil>"},
	} {
		f := newLanguagePoolFixture(t)
		var tmpl uuid.UUID
		if tc.templateLanguage == "" {
			tmpl = f.template(templateOpts{})
		} else {
			tmpl = f.languageTemplate(tc.templateLanguage, "da", "de")
		}
		groupID := f.dynamicGroup("department", "Sales")
		changes := f.lateMember(nil, lateRecipient{department: "Sales", country: "Denmark", language: strPtr("da")})
		leaves := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("de")})
		joins := f.lateMember(nil, lateRecipient{department: "IT", country: "Germany"})
		campaignID := f.campaign([]uuid.UUID{tmpl}, false, false)
		f.lateCampaign(campaignID, groupID)

		f.setRecipient(changes, map[string]any{"country": "Germany", "language": "de"})
		f.setRecipient(leaves, map[string]any{"department": "IT"})
		f.setRecipient(joins, map[string]any{"department": "Sales", "language": "da"})
		f.makeDue(campaignID)
		checkStamps(t, name, f.stamps(campaignID), map[uuid.UUID]string{changes: tc.want, joins: tc.want})
		for _, row := range f.recipientsOf(campaignID) {
			if row.CampaignTemplateID == nil || *row.CampaignTemplateID != tmpl {
				t.Errorf("%s: row got template %v, want %s", name, row.CampaignTemplateID, tmpl)
			}
		}
	}
}
