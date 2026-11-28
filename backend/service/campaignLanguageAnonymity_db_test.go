package service

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// stampRecipient sets the language stamp of a person's row in a campaign
func (f *poolFixture) stampRecipient(campaignID, recipientID uuid.UUID, code string) {
	f.t.Helper()
	if err := f.db.Model(&database.CampaignRecipient{}).
		Where("campaign_id = ? AND recipient_id = ?", campaignID.String(), recipientID.String()).
		Update("language", code).Error; err != nil {
		f.t.Fatalf("stamp: %v", err)
	}
}

func (f *poolFixture) campaignRowIDs(campaignID uuid.UUID) []uuid.UUID {
	f.t.Helper()
	var ids []string
	if err := f.db.Model(&database.CampaignRecipient{}).
		Where("campaign_id = ?", campaignID.String()).
		Pluck("id", &ids).Error; err != nil {
		f.t.Fatalf("read rows: %v", err)
	}
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		out = append(out, uuid.MustParse(id))
	}
	return out
}

// TestAnonymousLanguageGates checks that on an anonymous campaign with one
// template and two languages the per person language and the sent indicator
// are hidden and the email body is refused, while the template gate of the
// lure URL, which is the same in every language, stays open. With one
// language the views stay.
func TestAnonymousLanguageGates(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "de")

	mixed := f.campaign([]uuid.UUID{tmpl}, false, true)
	english := f.seedCampaignRecipient(mixed, &tmpl, true, "x")
	german := f.seedCampaignRecipient(mixed, &tmpl, true, "x")
	f.stampRecipient(mixed, english, "en")
	f.stampRecipient(mixed, german, "de")
	campaign := &model.Campaign{ID: nullable.NewNullableWithValue(mixed), IsAnonymous: nullable.NewNullableWithValue(true)}

	if hidden, err := f.svc.recipientTemplateHidden(f.ctx, campaign); err != nil || hidden {
		t.Fatalf("one template must not hide the template views: %v %v", hidden, err)
	}
	if hidden, err := f.svc.recipientContentHidden(f.ctx, campaign); err != nil || !hidden {
		t.Fatalf("two languages must hide the content views: %v %v", hidden, err)
	}
	result, err := f.svc.GetRecipientsByCampaignID(f.ctx, f.session, &mixed, &repository.CampaignRecipientOption{QueryArgs: &vo.QueryArgs{}})
	if err != nil || len(result.Rows) != 2 {
		t.Fatalf("list recipients: %v", err)
	}
	for _, row := range result.Rows {
		if !row.Language.IsNull() {
			t.Fatal("an anonymous campaign must not show a person's language")
		}
		if row.Sent {
			t.Fatal("an anonymous campaign with two languages must withhold the sent indicator")
		}
	}
	for _, crID := range f.campaignRowIDs(mixed) {
		crID := crID
		if _, err := f.svc.GetCampaignEmailBody(f.ctx, f.session, &crID); !errors.Is(err, errRecipientContentHidden) {
			t.Fatalf("email body: want hidden error, got %v", err)
		}
	}

	// one language keeps the sent indicator and the content views
	single := f.campaign([]uuid.UUID{tmpl}, false, true)
	one := f.seedCampaignRecipient(single, &tmpl, true, "x")
	f.stampRecipient(single, one, "de")
	// a row with no stamp counts as the template language
	f.seedCampaignRecipient(single, &tmpl, true, "x")
	if many, err := f.svc.CampaignRepository.HasLanguageGroupDataByCampaignID(f.ctx, &single); err != nil || !many {
		t.Fatalf("a row with no stamp is in the template language: %v %v", many, err)
	}
	if err := f.db.Model(&database.CampaignRecipient{}).
		Where("campaign_id = ? AND language IS NULL", single.String()).
		Update("language", "de").Error; err != nil {
		t.Fatal(err)
	}
	singleCampaign := &model.Campaign{ID: nullable.NewNullableWithValue(single), IsAnonymous: nullable.NewNullableWithValue(true)}
	if hidden, err := f.svc.recipientContentHidden(f.ctx, singleCampaign); err != nil || hidden {
		t.Fatalf("one language must not hide the content views: %v %v", hidden, err)
	}
	result, err = f.svc.GetRecipientsByCampaignID(f.ctx, f.session, &single, &repository.CampaignRecipientOption{QueryArgs: &vo.QueryArgs{}})
	if err != nil || len(result.Rows) != 2 || !result.Rows[0].Sent {
		t.Fatalf("one language anonymous campaign lost the sent indicator: %v", err)
	}
}

// TestLanguageGroupDataFromRoutes checks routes that pick more than one
// language count before any row exists, and one language does not
func TestLanguageGroupDataFromRoutes(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "de")
	campaignID := f.campaign([]uuid.UUID{tmpl}, false, true)
	has := func() bool {
		t.Helper()
		many, err := f.svc.CampaignRepository.HasLanguageGroupDataByCampaignID(f.ctx, &campaignID)
		if err != nil {
			t.Fatal(err)
		}
		return many
	}
	f.route(campaignID, "country", "en", map[string]string{"england": "en"})
	if has() {
		t.Fatal("routes that pick one language are one language")
	}
	f.route(campaignID, "country", "en", map[string]string{"germany": "de"})
	if !has() {
		t.Fatal("routes that pick two languages must count")
	}
}

// TestLanguageGateFailsClosed checks the language gate hides the views when
// the language use can not be read
func TestLanguageGateFailsClosed(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en")
	campaignID := f.campaign([]uuid.UUID{tmpl}, false, true)
	if err := f.db.Migrator().DropTable(&database.CampaignLanguageRoute{}); err != nil {
		t.Fatal(err)
	}
	campaign := &model.Campaign{ID: nullable.NewNullableWithValue(campaignID), IsAnonymous: nullable.NewNullableWithValue(true)}
	if hidden, err := f.svc.recipientContentHidden(f.ctx, campaign); err == nil || !hidden {
		t.Fatalf("a failed read must hide the views: %v %v", hidden, err)
	}
}

// TestAnonymizeKeepsLanguageStamp guards the language stamp through
// anonymization, like department and position: results by language stay
// after a campaign or a person is anonymized. A change here must be
// deliberate.
func TestAnonymizeKeepsLanguageStamp(t *testing.T) {
	p := newProgramServiceTest(t)
	now := time.Now().UTC()
	campaignID := uuid.New()
	p.create(&database.Campaign{ID: &campaignID, CreatedAt: &now, UpdatedAt: &now, Name: "c"})
	rows := map[uuid.UUID]string{}
	people := []uuid.UUID{}
	for _, code := range []string{"de", "da", "en"} {
		code := code
		id, person := uuid.New(), uuid.New()
		p.create(&database.CampaignRecipient{
			ID: &id, CreatedAt: &now, UpdatedAt: &now, CampaignID: &campaignID,
			RecipientID: &person, Language: &code, SentAt: &now,
		})
		rows[id] = code
		people = append(people, person)
	}
	pseudonym := uuid.New()
	if err := p.svc.CampaignRecipientRepository.Anonymize(p.ctx, &campaignID, &people[0], &pseudonym); err != nil {
		t.Fatalf("anonymize person: %v", err)
	}
	if err := p.svc.CampaignRecipientRepository.AnonymizeRowsOfCampaign(p.ctx, &campaignID); err != nil {
		t.Fatalf("anonymize campaign: %v", err)
	}
	for id, code := range rows {
		var row database.CampaignRecipient
		if err := p.db.Where("id = ?", id).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.RecipientID != nil {
			t.Fatal("the person link must be removed")
		}
		if row.Language == nil || *row.Language != code {
			t.Fatalf("anonymization must keep the language stamp %s", code)
		}
	}
}

// TestAnonymizedCampaignHidesLanguage checks that a named campaign with two
// languages lists each row's language and template until it is anonymized,
// and then lists no language, no template and no exact timing. A row whose
// recipient was removed from a named campaign shows no language but keeps
// its template.
func TestAnonymizedCampaignHidesLanguage(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "de")
	campaignID := f.campaign([]uuid.UUID{tmpl}, false, false)
	english := f.seedCampaignRecipient(campaignID, &tmpl, true, "x")
	german := f.seedCampaignRecipient(campaignID, &tmpl, true, "x")
	f.stampRecipient(campaignID, english, "en")
	f.stampRecipient(campaignID, german, "de")
	list := func() []*model.CampaignRecipient {
		t.Helper()
		result, err := f.svc.GetRecipientsByCampaignID(f.ctx, f.session, &campaignID, &repository.CampaignRecipientOption{
			QueryArgs: &vo.QueryArgs{OrderBy: "campaign_recipients.sent_at"},
		})
		if err != nil || len(result.Rows) != 2 {
			t.Fatalf("list recipients: %v", err)
		}
		return result.Rows
	}
	for _, row := range list() {
		if row.Language.IsNull() || row.TemplateID.IsNull() {
			t.Fatal("a named campaign shows each row's language and template")
		}
	}

	// the recipient of the German row is removed
	if err := f.svc.CampaignRecipientRepository.RemoveRecipientIDByRecipientID(f.ctx, &german); err != nil {
		t.Fatal(err)
	}
	for _, row := range list() {
		if rowHasRecipient(row) {
			if v, err := row.Language.Get(); err != nil || v != "en" {
				t.Fatal("a linked row keeps its language")
			}
			continue
		}
		if !row.Language.IsNull() {
			t.Fatal("a row with no recipient must not show its language")
		}
		if row.TemplateID.IsNull() {
			t.Fatal("a row with no recipient on a named campaign keeps its template")
		}
		if row.SentAt.IsNull() {
			t.Fatal("a row with no recipient on a named campaign keeps its send time")
		}
	}

	// the whole campaign is anonymized
	if err := f.svc.CampaignRecipientRepository.AnonymizeRowsOfCampaign(f.ctx, &campaignID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", campaignID.String()).
		Update("anonymized_at", time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range list() {
		if !row.Language.IsNull() {
			t.Fatal("an anonymized campaign must not show a row's language")
		}
		if !row.TemplateID.IsNull() || row.Template != nil {
			t.Fatal("an anonymized campaign with two languages must not show a row's template")
		}
		if !row.SentAt.IsNull() || !row.SendAt.IsNull() {
			t.Fatal("an anonymized campaign with two languages must not show exact send times")
		}
	}
}

// TestAnonymizedSingleLanguageHidesSendTimes checks that an anonymized
// campaign with one template and no languages lists no exact timing and
// orders its rows by the random row id, whatever order is asked for, so a
// row's send time and position can not be matched to the person it had
// before. A named campaign keeps the times and the asked order.
func TestAnonymizedSingleLanguageHidesSendTimes(t *testing.T) {
	f := newPoolFixture(t)
	tmpl := f.template(templateOpts{})
	campaignID := f.campaign([]uuid.UUID{tmpl}, false, false)
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 6; i++ {
		rid := f.seedCampaignRecipient(campaignID, &tmpl, true, "x")
		// each row gets its own send time, in the order the rows were made
		at := base.Add(time.Duration(i) * time.Minute)
		if err := f.db.Model(&database.CampaignRecipient{}).
			Where("campaign_id = ? AND recipient_id = ?", campaignID.String(), rid.String()).
			Updates(map[string]any{"sent_at": at, "send_at": at}).Error; err != nil {
			t.Fatal(err)
		}
	}
	list := func(orderBy string) []*model.CampaignRecipient {
		t.Helper()
		result, err := f.svc.GetRecipientsByCampaignID(f.ctx, f.session, &campaignID, &repository.CampaignRecipientOption{
			QueryArgs: &vo.QueryArgs{OrderBy: orderBy},
		})
		if err != nil || len(result.Rows) != 6 {
			t.Fatalf("list recipients: %v", err)
		}
		return result.Rows
	}
	named := list("campaign_recipients.sent_at")
	for i, row := range named {
		at, err := row.SentAt.Get()
		if err != nil {
			t.Fatal("a named campaign shows each row's send time")
		}
		if i > 0 {
			prev, _ := named[i-1].SentAt.Get()
			if at.Before(prev) {
				t.Fatal("a named campaign keeps the asked order")
			}
		}
	}

	if err := f.svc.CampaignRecipientRepository.AnonymizeRowsOfCampaign(f.ctx, &campaignID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", campaignID.String()).
		Update("anonymized_at", time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	ids := f.campaignRowIDs(campaignID)
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	for _, orderBy := range []string{"campaign_recipients.sent_at", "campaign_recipients.send_at", ""} {
		rows := list(orderBy)
		for i, row := range rows {
			if !row.SentAt.IsNull() || !row.SendAt.IsNull() || !row.LastAttemptAt.IsNull() || !row.CancelledAt.IsNull() {
				t.Fatal("an anonymized campaign must not show exact send times")
			}
			if !row.Language.IsNull() {
				t.Fatal("an anonymized campaign must not show a row's language")
			}
			if row.TemplateID.IsNull() {
				t.Fatal("an anonymized campaign with one template keeps the template")
			}
			id, err := row.ID.Get()
			if err != nil || id != ids[i] {
				t.Fatalf("an anonymized campaign orders rows by row id (order %q)", orderBy)
			}
		}
	}
}
