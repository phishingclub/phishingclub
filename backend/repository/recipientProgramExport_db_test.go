package repository

import (
	"context"
	"testing"

	"github.com/phishingclub/phishingclub/database"
)

// TestProgramExportReads checks the export reads name a program send by the
// program and the template, leave out anonymous programs and plain campaigns,
// and list the programs of exactly one company
func TestProgramExportReads(t *testing.T) {
	db := newProgramTestDB(t)
	if err := db.AutoMigrate(&database.CampaignTemplate{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	companyID := ptrUUID()
	named := createProgram(t, db, "Named", companyID)
	anon := createProgram(t, db, "Hidden", companyID)
	createProgram(t, db, "Global", nil)
	if err := db.Model(&database.Program{}).Where("id = ?", anon).Update("is_anonymous", true).Error; err != nil {
		t.Fatalf("set anonymous: %v", err)
	}
	templateID := ptrUUID()
	if err := db.Create(&database.CampaignTemplate{
		ID: templateID, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "Invoice lure",
		URLIdentifierID: ptrUUID(), StateIdentifierID: ptrUUID(),
	}).Error; err != nil {
		t.Fatalf("create template: %v", err)
	}
	alice, clickID := ptrUUID(), ptrUUID()
	namedCampaign, anonCampaign, plainCampaign := ptrUUID(), ptrUUID(), ptrUUID()
	createHiddenCampaign(t, db, namedCampaign, named)
	createHiddenCampaign(t, db, anonCampaign, anon)
	if err := db.Model(&database.Campaign{}).Where("id IN ?", []string{namedCampaign.String(), anonCampaign.String()}).
		Update("campaign_template_id", templateID).Error; err != nil {
		t.Fatalf("set template: %v", err)
	}
	if err := db.Create(&database.Campaign{ID: plainCampaign, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "plain"}).Error; err != nil {
		t.Fatalf("create plain: %v", err)
	}
	addCampaignRecipient(t, db, namedCampaign, alice, nil, true)
	addCampaignRecipient(t, db, anonCampaign, alice, nil, true)
	addCampaignRecipient(t, db, plainCampaign, alice, nil, true)
	addEvent(t, db, namedCampaign, alice, nil, clickID)
	addEvent(t, db, anonCampaign, alice, nil, clickID)
	addEvent(t, db, plainCampaign, alice, nil, clickID)

	r := &Recipient{DB: db}
	events, err := r.GetProgramEventsForExport(ctx, alice)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 1 || events[0].ProgramName != "Named" || events[0].TemplateName != "Invoice lure" ||
		*events[0].ProgramID != *named || *events[0].ProgramCompanyID != *companyID || *events[0].EventID != *clickID {
		t.Fatalf("events = %+v", events)
	}
	sends, err := r.GetProgramSendsForExport(ctx, alice)
	if err != nil {
		t.Fatalf("sends: %v", err)
	}
	if len(sends) != 1 || sends[0].ProgramName != "Named" || sends[0].TemplateName != "Invoice lure" || sends[0].SentAt == nil {
		t.Fatalf("sends = %+v", sends)
	}
	// a send with no stamp is in the template language, a stamped one in its
	// stamp
	if err := db.Model(&database.CampaignTemplate{}).Where("id = ?", templateID).Update("language", "en").Error; err != nil {
		t.Fatal(err)
	}
	sends, err = r.GetProgramSendsForExport(ctx, alice)
	if err != nil || len(sends) != 1 || sends[0].EffectiveLanguage() != "en" {
		t.Fatalf("template language of a send = %+v %v", sends, err)
	}
	if err := db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", namedCampaign).Update("language", "de").Error; err != nil {
		t.Fatal(err)
	}
	sends, err = r.GetProgramSendsForExport(ctx, alice)
	if err != nil || len(sends) != 1 || sends[0].EffectiveLanguage() != "de" {
		t.Fatalf("stamped language of a send = %+v %v", sends, err)
	}
	// a send with no stamp on its own template is in the language of that
	// template, not of the hidden campaign's template
	ownTemplate := ptrUUID()
	if err := db.Create(&database.CampaignTemplate{
		ID: ownTemplate, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "Parcel lure", Language: "da",
		URLIdentifierID: ptrUUID(), StateIdentifierID: ptrUUID(),
	}).Error; err != nil {
		t.Fatalf("create template: %v", err)
	}
	if err := db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", namedCampaign).
		Updates(map[string]any{"language": nil, "campaign_template_id": ownTemplate.String()}).Error; err != nil {
		t.Fatal(err)
	}
	sends, err = r.GetProgramSendsForExport(ctx, alice)
	if err != nil || len(sends) != 1 || sends[0].EffectiveLanguage() != "da" || sends[0].TemplateName != "Parcel lure" {
		t.Fatalf("own template of a send = %+v %v", sends, err)
	}
	if err := db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", namedCampaign).
		Updates(map[string]any{"language": "de", "campaign_template_id": nil}).Error; err != nil {
		t.Fatal(err)
	}

	c := &Campaign{DB: db}
	programs, err := c.GetProgramsForExport(ctx, companyID)
	if err != nil {
		t.Fatalf("programs: %v", err)
	}
	if len(programs) != 2 || programs[0].Name != "Hidden" || !programs[0].IsAnonymous || programs[1].Name != "Named" {
		t.Fatalf("programs = %+v", programs)
	}
	global, err := c.GetProgramsForExport(ctx, nil)
	if err != nil || len(global) != 1 || global[0].Name != "Global" {
		t.Fatalf("global programs = %+v %v", global, err)
	}
	names, err := c.GetProgramTemplateNames(ctx, named)
	if err != nil || names[*namedCampaign] != "Invoice lure" || len(names) != 1 {
		t.Fatalf("template names = %v %v", names, err)
	}
	display, err := c.GetDisplayNameByID(ctx, namedCampaign)
	if err != nil || display.Name != "Named" || display.ProgramName != "Named" || *display.ProgramID != *named {
		t.Fatalf("display name = %+v %v", display, err)
	}
	plain, err := c.GetDisplayNameByID(ctx, plainCampaign)
	if err != nil || plain.Name != "plain" || plain.ProgramID != nil {
		t.Fatalf("plain display name = %+v %v", plain, err)
	}
}
