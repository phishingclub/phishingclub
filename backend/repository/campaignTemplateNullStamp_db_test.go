package repository

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
)

// TestNullStampByCampaign checks the null stamp of a template language change
// reaches every row that gets its content from the template, across more
// campaigns than one batch holds: a row holding the template in a campaign
// whose pool no longer has it, a row with no template of its own in a
// campaign on the template, and leaves rows of other templates, rows that
// already have a language and rows with no template in other campaigns. Both
// statements read the campaign and template index instead of the table.
func TestNullStampByCampaign(t *testing.T) {
	db := newLanguageRouteDB(t)
	if err := (database.CampaignRecipient{}).Migrate(db); err != nil {
		t.Fatalf("migrate indexes: %v", err)
	}
	tmpl, other := uuid.New(), uuid.New()
	now := utils.NowRFC3339UTC()
	campaigns := []map[string]any{}
	ids := []uuid.UUID{}
	for i := 0; i < 2*nullStampBatch+7; i++ {
		id := uuid.New()
		ids = append(ids, id)
		templateID := other
		if i%3 == 0 {
			templateID = tmpl
		}
		campaigns = append(campaigns, map[string]any{
			"id": id.String(), "created_at": now, "updated_at": now,
			"name": "c " + id.String(), "sort_field": "email", "sort_order": "asc",
			"campaign_template_id": templateID.String(),
		})
	}
	if err := db.Model(&database.Campaign{}).CreateInBatches(campaigns, 100).Error; err != nil {
		t.Fatalf("insert campaigns: %v", err)
	}
	type want struct {
		id   uuid.UUID
		code string
	}
	checks := []want{}
	row := func(campaignID uuid.UUID, templateID *uuid.UUID, language *string, code string) {
		t.Helper()
		id := uuid.New()
		r := map[string]any{"id": id.String(), "created_at": now, "updated_at": now, "campaign_id": campaignID.String()}
		if templateID != nil {
			r["campaign_template_id"] = templateID.String()
		}
		if language != nil {
			r["language"] = *language
		}
		if err := db.Model(&database.CampaignRecipient{}).Create(r).Error; err != nil {
			t.Fatalf("insert row: %v", err)
		}
		checks = append(checks, want{id, code})
	}
	da := "da"
	onOther := ids[len(ids)-1] // campaign template is other
	onTemplate := ids[0]       // i%3 == 0, campaign template is tmpl
	// a row that holds the template in a campaign on another template
	row(onOther, &tmpl, nil, "de")
	// a row of another template and a row with no template there stay
	row(onOther, &other, nil, "")
	row(onOther, nil, nil, "")
	// a row with no template of its own in a campaign on the template
	row(onTemplate, nil, nil, "de")
	// a row that has a language keeps it
	row(onTemplate, nil, &da, "da")
	row(onTemplate, &tmpl, &da, "da")
	// a row on the template in the last campaign of a later batch
	row(ids[2*nullStampBatch+3], &tmpl, nil, "de")

	var stamped int64
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		stamped, err = stampTemplateNullLanguagesTx(tx, &tmpl, "de")
		return err
	}); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if stamped != 3 {
		t.Fatalf("stamped %d rows, want 3", stamped)
	}
	for _, c := range checks {
		var got database.CampaignRecipient
		if err := db.Where("id = ?", c.id.String()).First(&got).Error; err != nil {
			t.Fatal(err)
		}
		have := ""
		if got.Language != nil {
			have = *got.Language
		}
		if have != c.code {
			t.Fatalf("row stamped %q, want %q", have, c.code)
		}
	}

	plan := func(stmt string, args ...any) string {
		t.Helper()
		var rows []struct {
			ID, Parent, Notused int
			Detail              string
		}
		if err := db.Raw("EXPLAIN QUERY PLAN "+stmt, args...).Scan(&rows).Error; err != nil {
			t.Fatalf("plan: %v", err)
		}
		details := []string{}
		for _, r := range rows {
			details = append(details, r.Detail)
		}
		return strings.Join(details, " | ")
	}
	batch := []string{ids[0].String(), ids[1].String()}
	for _, p := range []string{
		plan(`UPDATE campaign_recipients SET language = ?, updated_at = ?
			WHERE campaign_id IN ? AND campaign_template_id = ? AND language IS NULL`, "de", now, batch, tmpl.String()),
		plan(`UPDATE campaign_recipients SET language = ?, updated_at = ?
			WHERE campaign_id IN ? AND campaign_template_id IS NULL AND language IS NULL`, "de", now, batch),
	} {
		if !strings.Contains(p, "idx_campaign_recipients_campaign_template") {
			t.Fatalf("the null stamp must read the campaign and template index, plan: %s", p)
		}
	}
}
