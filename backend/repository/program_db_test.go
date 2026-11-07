package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newProgramTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "program.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Campaign{},
		&database.CampaignRecipient{},
		&database.CampaignEvent{},
		&database.Program{},
		&database.ProgramStep{},
		&database.ProgramCampaign{},
		&database.ProgramRecipientGroup{},
		&database.ProgramEnrollment{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func ptrUUID() *uuid.UUID {
	id := uuid.New()
	return &id
}

func ptrNow() *time.Time {
	now := time.Now()
	return &now
}

func createProgram(t *testing.T, db *gorm.DB, name string, companyID *uuid.UUID) *uuid.UUID {
	t.Helper()
	id := ptrUUID()
	started := time.Now().Add(-time.Hour).UTC()
	row := &database.Program{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: name, CompanyID: companyID, StartAt: &started}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create program: %v", err)
	}
	return id
}

func createStep(t *testing.T, db *gorm.DB, programID, templateID *uuid.UUID, position int) *uuid.UUID {
	t.Helper()
	id := ptrUUID()
	row := &database.ProgramStep{
		ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
		ProgramID: programID, CampaignTemplateID: templateID, Position: position,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create step: %v", err)
	}
	return id
}

func linkCampaign(t *testing.T, db *gorm.DB, programID, stepID *uuid.UUID, loop int) *uuid.UUID {
	t.Helper()
	campaignID := ptrUUID()
	row := &database.ProgramCampaign{
		ID: ptrUUID(), CreatedAt: ptrNow(),
		ProgramID: programID, ProgramStepID: stepID, LoopIndex: loop, CampaignID: campaignID,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("link campaign: %v", err)
	}
	return campaignID
}

func createHiddenCampaign(t *testing.T, db *gorm.DB, id, programID *uuid.UUID) {
	t.Helper()
	row := &database.Campaign{
		ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
		Name: "campaign " + id.String(), ProgramID: programID,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create campaign: %v", err)
	}
}

func addCampaignRecipient(t *testing.T, db *gorm.DB, campaignID, recipientID, anonymizedID *uuid.UUID, sent bool) *uuid.UUID {
	t.Helper()
	id := ptrUUID()
	row := &database.CampaignRecipient{
		ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
		CampaignID: campaignID, RecipientID: recipientID, AnonymizedID: anonymizedID,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("add campaign recipient: %v", err)
	}
	// the app stores sent_at as RFC 3339 UTC text, so write it the same way
	if sent {
		if err := db.Model(&database.CampaignRecipient{}).Where("id = ?", id).
			Update("sent_at", utils.NowRFC3339UTC()).Error; err != nil {
			t.Fatalf("set sent_at: %v", err)
		}
	}
	return id
}

func addEvent(t *testing.T, db *gorm.DB, campaignID, recipientID, anonymizedID, eventID *uuid.UUID) {
	t.Helper()
	row := &database.CampaignEvent{
		ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
		CampaignID: campaignID, RecipientID: recipientID, AnonymizedID: anonymizedID, EventID: eventID,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("add event: %v", err)
	}
}

// TestGetProgramResultStatsCountsUniquePeople checks that a person reached by
// several steps of a program is counted once and that other programs and plain
// campaigns are left out
func TestGetProgramResultStatsCountsUniquePeople(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	sentID, clickID := ptrUUID(), ptrUUID()
	prevSent := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT]
	prevClick := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED]
	cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT] = sentID
	cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED] = clickID
	t.Cleanup(func() {
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT] = prevSent
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED] = prevClick
	})

	programID := createProgram(t, db, "p1", nil)
	stepA := createStep(t, db, programID, ptrUUID(), 0)
	stepB := createStep(t, db, programID, ptrUUID(), 1)
	campA := linkCampaign(t, db, programID, stepA, 0)
	campB := linkCampaign(t, db, programID, stepB, 0)
	createHiddenCampaign(t, db, campA, programID)
	createHiddenCampaign(t, db, campB, programID)
	// a training campaign of the program: opening its page is not a phish click
	trainingCamp := ptrUUID()
	if err := db.Create(&database.Campaign{
		ID: trainingCamp, CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
		Name: "training", ProgramID: programID, IsTraining: true,
	}).Error; err != nil {
		t.Fatalf("create training campaign: %v", err)
	}

	alice, bob := ptrUUID(), ptrUUID()
	// alice gets both steps, bob only the first
	addCampaignRecipient(t, db, campA, alice, nil, true)
	addCampaignRecipient(t, db, campB, alice, nil, true)
	addCampaignRecipient(t, db, campA, bob, nil, true)
	addEvent(t, db, campA, alice, nil, sentID)
	addEvent(t, db, campB, alice, nil, sentID)
	addEvent(t, db, campA, bob, nil, sentID)
	addEvent(t, db, campB, alice, nil, clickID)
	// a pseudonymous event counts as its own person
	addEvent(t, db, campA, nil, ptrUUID(), clickID)
	// an anonymous person keeps one pseudonym across steps, and a live row
	// carrying both ids is still one person
	carol, carolPseudonym := ptrUUID(), ptrUUID()
	addCampaignRecipient(t, db, campA, carol, carolPseudonym, true)
	addCampaignRecipient(t, db, campB, carol, carolPseudonym, true)
	addEvent(t, db, campA, nil, carolPseudonym, sentID)
	addEvent(t, db, campB, nil, carolPseudonym, sentID)

	// noise outside the program
	otherProgram := createProgram(t, db, "p2", nil)
	otherCamp := linkCampaign(t, db, otherProgram, createStep(t, db, otherProgram, ptrUUID(), 0), 0)
	createHiddenCampaign(t, db, otherCamp, otherProgram)
	addCampaignRecipient(t, db, otherCamp, ptrUUID(), nil, true)
	addEvent(t, db, otherCamp, ptrUUID(), nil, sentID)
	plainCamp := ptrUUID()
	addCampaignRecipient(t, db, plainCamp, alice, nil, true)
	addEvent(t, db, plainCamp, alice, nil, clickID)

	addCampaignRecipient(t, db, trainingCamp, bob, nil, true)
	addEvent(t, db, trainingCamp, bob, nil, clickID)

	r := &Campaign{DB: db}
	stats, err := r.GetProgramResultStats(ctx, programID)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Recipients != 3 {
		t.Errorf("recipients = %d, want 3", stats.Recipients)
	}
	if stats.EmailsSent != 3 {
		t.Errorf("emails sent = %d, want 3", stats.EmailsSent)
	}
	if stats.WebsiteLoaded != 2 {
		t.Errorf("website loaded = %d, want 2", stats.WebsiteLoaded)
	}
}

// TestProgramLookups checks the id and name lookups used by program, company
// and template deletion
func TestProgramLookups(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}

	companyID := ptrUUID()
	templateID := ptrUUID()
	p1 := createProgram(t, db, "beta", companyID)
	p2 := createProgram(t, db, "alpha", companyID)
	p3 := createProgram(t, db, "global", nil)
	s1 := createStep(t, db, p1, templateID, 0)
	createStep(t, db, p1, templateID, 1)
	createStep(t, db, p2, templateID, 0)
	createStep(t, db, p3, ptrUUID(), 0)
	c1 := linkCampaign(t, db, p1, s1, 0)
	c2 := linkCampaign(t, db, p1, s1, 1)
	createHiddenCampaign(t, db, c1, p1)
	createHiddenCampaign(t, db, c2, p1)
	// a plain campaign and another program's campaign are not returned
	createHiddenCampaign(t, db, ptrUUID(), nil)
	createHiddenCampaign(t, db, ptrUUID(), p3)

	names, err := r.GetNamesByCampaignTemplateID(ctx, templateID)
	if err != nil {
		t.Fatalf("names: %v", err)
	}
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("names = %v, want [alpha beta]", names)
	}
	unused, err := r.GetNamesByCampaignTemplateID(ctx, ptrUUID())
	if err != nil || len(unused) != 0 {
		t.Errorf("unused template names = %v, err %v, want none", unused, err)
	}

	ids, err := r.GetIDsByCompanyID(ctx, companyID)
	if err != nil {
		t.Fatalf("company ids: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("company program ids = %d, want 2", len(ids))
	}

	campaignIDs, err := r.GetCampaignIDs(ctx, p1)
	if err != nil {
		t.Fatalf("campaign ids: %v", err)
	}
	got := map[uuid.UUID]bool{}
	for _, id := range campaignIDs {
		got[*id] = true
	}
	if len(got) != 2 || !got[*c1] || !got[*c2] {
		t.Errorf("campaign ids = %v, want %v and %v", campaignIDs, c1, c2)
	}

	if err := r.DeleteProgramCampaignByCampaignID(ctx, c1); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	var remaining int64
	db.Model(&database.ProgramCampaign{}).Count(&remaining)
	if remaining != 1 {
		t.Errorf("program campaign links after unlink = %d, want 1", remaining)
	}

	groupID := ptrUUID()
	link := &database.ProgramRecipientGroup{ProgramID: p2, RecipientGroupID: groupID}
	if err := db.Create(link).Error; err != nil {
		t.Fatalf("assign group: %v", err)
	}
	groupNames, err := r.GetNamesByRecipientGroupID(ctx, groupID)
	if err != nil || len(groupNames) != 1 || groupNames[0] != "alpha" {
		t.Errorf("group names = %v, err %v, want [alpha]", groupNames, err)
	}

	recipientID := ptrUUID()
	enrollment := &database.ProgramEnrollment{ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(), ProgramID: p2, RecipientID: recipientID}
	if err := db.Create(enrollment).Error; err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if err := r.DeleteEnrollmentsByRecipientID(ctx, recipientID); err != nil {
		t.Fatalf("delete enrollments: %v", err)
	}
	var enrollments int64
	db.Model(&database.ProgramEnrollment{}).Count(&enrollments)
	if enrollments != 0 {
		t.Errorf("enrollments after delete = %d, want 0", enrollments)
	}

	// delete removes the campaign links with the program
	if err := r.DeleteByID(ctx, p1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var links int64
	db.Model(&database.ProgramCampaign{}).Count(&links)
	if links != 0 {
		t.Errorf("program campaign links after delete = %d, want 0", links)
	}
}

// TestGetDueEnrollmentsSkipsStoppedPrograms checks that due rows of paused and
// closed programs do not fill the batch
func TestGetDueEnrollmentsSkipsStoppedPrograms(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	running := createProgram(t, db, "running", nil)
	paused := createProgram(t, db, "paused", nil)
	closed := createProgram(t, db, "closed", nil)
	now := time.Now()
	db.Model(&database.Program{}).Where("id = ?", paused).Update("paused_at", now)
	db.Model(&database.Program{}).Where("id = ?", closed).Update("closed_at", now)
	past := now.Add(-time.Hour)
	for _, p := range []*uuid.UUID{running, paused, closed} {
		e := &database.ProgramEnrollment{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			ProgramID: p, RecipientID: ptrUUID(), NextDueAt: &past, Status: "active",
		}
		if err := db.Create(e).Error; err != nil {
			t.Fatalf("enroll: %v", err)
		}
	}
	due, err := r.GetDueEnrollments(ctx, now, 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 || *due[0].ProgramID != *running {
		t.Fatalf("due rows = %d, want only the running program", len(due))
	}
}

func TestGetListSummaries(t *testing.T) {
	db := newProgramTestDB(t)
	if err := db.AutoMigrate(&database.CampaignTemplate{}); err != nil {
		t.Fatalf("migrate templates: %v", err)
	}
	ctx := context.Background()
	r := &Program{DB: db}
	a := createProgram(t, db, "a", nil)
	b := createProgram(t, db, "b", nil)
	empty := createProgram(t, db, "empty", nil)
	good, broken, gone := ptrUUID(), ptrUUID(), ptrUUID()
	for _, tpl := range []struct {
		id     *uuid.UUID
		usable bool
	}{{good, true}, {broken, false}} {
		row := &database.CampaignTemplate{ID: tpl.id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: tpl.id.String(), URLIdentifierID: ptrUUID(), IsUsable: tpl.usable}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create template: %v", err)
		}
	}
	createStep(t, db, a, good, 0)
	createStep(t, db, a, broken, 1)
	createStep(t, db, b, gone, 0)

	soon := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	later := soon.Add(24 * time.Hour)
	enroll := func(p *uuid.UUID, due *time.Time, status string) {
		e := &database.ProgramEnrollment{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			ProgramID: p, RecipientID: ptrUUID(), NextDueAt: due, Status: status,
		}
		if err := db.Create(e).Error; err != nil {
			t.Fatalf("enroll: %v", err)
		}
	}
	enroll(a, &later, "active")
	enroll(a, &soon, "active")
	enroll(a, nil, "active")
	enroll(a, &soon, "exited")
	enroll(b, &later, "active")

	got, err := r.GetListSummaries(ctx, []*uuid.UUID{a, b, empty})
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if got[*a].Enrolled != 3 || got[*b].Enrolled != 1 || got[*empty].Enrolled != 0 {
		t.Fatalf("enrolled a=%d b=%d empty=%d", got[*a].Enrolled, got[*b].Enrolled, got[*empty].Enrolled)
	}
	if got[*a].NextDueAt == nil || !got[*a].NextDueAt.Equal(soon) {
		t.Fatalf("next due a = %v, want %v", got[*a].NextDueAt, soon)
	}
	if got[*empty].NextDueAt != nil {
		t.Fatalf("empty program should have no next due")
	}
	if len(got[*a].StepTemplateIDs) != 2 || len(got[*b].StepTemplateIDs) != 1 {
		t.Fatalf("step templates a=%d b=%d", len(got[*a].StepTemplateIDs), len(got[*b].StepTemplateIDs))
	}
	usable, err := r.GetTemplateUsability(ctx, []uuid.UUID{*good, *broken, *gone})
	if err != nil {
		t.Fatalf("usability: %v", err)
	}
	if !usable[*good] || usable[*broken] {
		t.Fatalf("usability = %v", usable)
	}
	if _, ok := usable[*gone]; ok {
		t.Fatalf("a deleted template must be missing from the result")
	}
}
