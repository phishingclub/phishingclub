package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/phishingclub/phishingclub/database"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestParseStoredTime checks the stored time forms read directly give the
// same instant as the driver forms, and that other text falls back to them
func TestParseStoredTime(t *testing.T) {
	cases := map[string]time.Time{
		"2026-11-13 10:43:50.023172473+00:00": time.Date(2026, 11, 13, 10, 43, 50, 23172473, time.UTC),
		"2026-11-13T10:43:50.023172473Z":      time.Date(2026, 11, 13, 10, 43, 50, 23172473, time.UTC),
		"2026-11-13T10:43:50Z":                time.Date(2026, 11, 13, 10, 43, 50, 0, time.UTC),
		"2026-11-13 10:43:50+00:00":           time.Date(2026, 11, 13, 10, 43, 50, 0, time.UTC),
		"2026-11-13 10:43:50.5+02:00":         time.Date(2026, 11, 13, 8, 43, 50, 500000000, time.UTC),
		"2026-11-13 10:43:50.5-01:30":         time.Date(2026, 11, 13, 12, 13, 50, 500000000, time.UTC),
		"2026-11-13 10:43:50":                 time.Date(2026, 11, 13, 10, 43, 50, 0, time.UTC),
		"2026-11-13T10:43:50.123":             time.Date(2026, 11, 13, 10, 43, 50, 123000000, time.UTC),
		// not read directly, the driver forms read them
		"2026-11-13 10:43":    time.Date(2026, 11, 13, 10, 43, 0, 0, time.UTC),
		"2026-11-13":          time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC),
		"":                    {},
		"not a time at all!!": {},
		"2026-02-30 10:00:00": {},
	}
	for in, want := range cases {
		if got := parseStoredTime([]byte(in)); !got.Equal(want) {
			t.Fatalf("parseStoredTime(%q) = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"2026-11-13 10:43:50.", "2026-11-13 10:43:50.1234567890Z", "2026-11-13 10:43:50+0200", "2026-13-13 10:43:50"} {
		if _, ok := parseStoredTimeFast([]byte(bad)); ok {
			t.Fatalf("parseStoredTimeFast(%q) must refuse it", bad)
		}
	}
}

// TestProgramSentRowsReadStoredTime checks the sent and event times read as
// text give the stored instant in both stored forms
func TestProgramSentRowsReadStoredTime(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	repo := &Program{DB: db}
	programID := createProgram(t, db, "p", nil)
	campaign := ptrUUID()
	createHiddenCampaign(t, db, campaign, programID)
	at := time.Date(2026, 3, 4, 5, 6, 7, 891000000, time.UTC)
	a := addCampaignRecipient(t, db, campaign, ptrUUID(), nil, true)
	b := addCampaignRecipient(t, db, campaign, ptrUUID(), nil, true)
	// one row in the form of the driver, one as RFC 3339 text
	db.Model(&database.CampaignRecipient{}).Where("id = ?", a).Update("sent_at", at)
	db.Model(&database.CampaignRecipient{}).Where("id = ?", b).Update("sent_at", at.Format(time.RFC3339Nano))
	rows, err := repo.GetSentRows(ctx, programID, nil)
	if err != nil {
		t.Fatalf("sent rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows %d, want 2", len(rows))
	}
	for _, r := range rows {
		if !r.SentAt.Equal(at) {
			t.Fatalf("sent at %v, want %v", r.SentAt, at)
		}
	}
}

// planLogger keeps the last statement run
type planLogger struct {
	logger.Interface
	sql *string
}

func (l planLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	*l.sql, _ = fc()
}

// TestCountSentRowsCoveringIndex checks the sent row count of a program reads
// the partial index. Its predicates are implied by the index, so SQLite does
// not test them and never seeks the table rows.
func TestCountSentRowsCoveringIndex(t *testing.T) {
	db := newProgramTestDB(t)
	if err := (database.CampaignRecipient{}).Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	programID := createProgram(t, db, "p", nil)
	campaign := ptrUUID()
	createHiddenCampaign(t, db, campaign, programID)
	addCampaignRecipient(t, db, campaign, ptrUUID(), nil, true)
	addCampaignRecipient(t, db, campaign, nil, ptrUUID(), true)
	addCampaignRecipient(t, db, campaign, ptrUUID(), nil, false)
	var stmt string
	traced := db.Session(&gorm.Session{Logger: planLogger{Interface: logger.Default.LogMode(logger.Silent), sql: &stmt}})
	count, err := (&Program{DB: traced}).CountSentRows(ctx, programID)
	if err != nil || count != 2 {
		t.Fatalf("count %d %v, want 2", count, err)
	}
	var plan []struct {
		ID, Parent, Notused int
		Detail              string
	}
	if err := db.Raw("EXPLAIN QUERY PLAN " + stmt).Scan(&plan).Error; err != nil {
		t.Fatalf("plan: %v", err)
	}
	details := []string{}
	for _, p := range plan {
		details = append(details, p.Detail)
	}
	joined := strings.Join(details, " | ")
	if !strings.Contains(joined, "campaign_recipients USING INDEX idx_campaign_recipients_campaign_sent_at") && !strings.Contains(joined, "campaign_recipients USING COVERING INDEX idx_campaign_recipients_campaign_sent_at") {
		t.Fatalf("the count must read the partial index, plan: %s", joined)
	}
}

// explainPlan returns the plan details of a statement joined in one line
func explainPlan(t *testing.T, db *gorm.DB, stmt string) string {
	t.Helper()
	var plan []struct {
		ID, Parent, Notused int
		Detail              string
	}
	if err := db.Raw("EXPLAIN QUERY PLAN " + stmt).Scan(&plan).Error; err != nil {
		t.Fatalf("plan: %v", err)
	}
	details := []string{}
	for _, p := range plan {
		details = append(details, p.Detail)
	}
	return strings.Join(details, " | ")
}

// TestReleaseUnlinkedSendsOfRecipientPlan checks the release of the sends of
// a removed person finds the rows through the pseudonym index. The recipient
// index holds every unlinked row under the null value, so a read through it
// walks all of them inside the write.
func TestReleaseUnlinkedSendsOfRecipientPlan(t *testing.T) {
	db := newProgramTestDB(t)
	if err := (database.CampaignRecipient{}).Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	programID := createProgram(t, db, "p", nil)
	campaign := ptrUUID()
	createHiddenCampaign(t, db, campaign, programID)
	recipient := ptrUUID()
	pseudonym := ptrUUID()
	if err := db.Create(&database.ProgramEnrollment{
		ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
		ProgramID: programID, RecipientID: recipient, AnonymizedID: pseudonym,
	}).Error; err != nil {
		t.Fatalf("enroll: %v", err)
	}
	row := addCampaignRecipient(t, db, campaign, nil, pseudonym, true)
	if err := db.Model(&database.CampaignRecipient{}).Where("id = ?", row).
		Update("message_id", "m1").Error; err != nil {
		t.Fatalf("message id: %v", err)
	}
	var stmt string
	traced := db.Session(&gorm.Session{Logger: planLogger{Interface: logger.Default.LogMode(logger.Silent), sql: &stmt}})
	if err := (&Program{DB: traced}).ReleaseUnlinkedSendsOfRecipient(ctx, recipient); err != nil {
		t.Fatalf("release: %v", err)
	}
	var left int64
	db.Model(&database.CampaignRecipient{}).Where("id = ? AND message_id IS NOT NULL", row).Count(&left)
	if left != 0 {
		t.Fatalf("the message id of the unlinked send must be released")
	}
	plan := explainPlan(t, db, stmt)
	if strings.Contains(plan, "idx_campaign_recipients_recipient_id") {
		t.Fatalf("the release must not read the recipient index, plan: %s", plan)
	}
	if !strings.Contains(plan, "campaign_recipients USING INDEX idx_campaign_recipients_anonymized_id") &&
		!strings.Contains(plan, "campaign_recipients USING COVERING INDEX idx_campaign_recipients_anonymized_id") {
		t.Fatalf("the release must find the rows through the pseudonym index, plan: %s", plan)
	}
}

// TestTimelineSentCoveringIndex checks the timeline sent count reads each
// program campaign within the range of the campaign and sent time index, and
// not the sent_at index of every campaign. A send that names nobody is not
// counted, as in the facts of the program.
func TestTimelineSentCoveringIndex(t *testing.T) {
	db := newProgramTestDB(t)
	if err := (database.CampaignRecipient{}).Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	programID := createProgram(t, db, "p", nil)
	campaign := ptrUUID()
	createHiddenCampaign(t, db, campaign, programID)
	other := ptrUUID()
	createHiddenCampaign(t, db, other, nil)
	addCampaignRecipient(t, db, campaign, ptrUUID(), nil, true)
	addCampaignRecipient(t, db, campaign, nil, ptrUUID(), true)
	addCampaignRecipient(t, db, campaign, nil, nil, true)
	addCampaignRecipient(t, db, campaign, ptrUUID(), nil, false)
	addCampaignRecipient(t, db, other, ptrUUID(), nil, true)
	now := time.Now().UTC()
	spans := []ProgramTimelineSpan{{From: now.Add(-24 * time.Hour), To: now.Add(time.Hour)}}
	var stmt string
	traced := db.Session(&gorm.Session{Logger: planLogger{Interface: logger.Default.LogMode(logger.Silent), sql: &stmt}})
	rows, err := (&Program{DB: traced}).GetTimelineSent(ctx, programID, spans)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	total := 0
	for _, row := range rows {
		total += row.Count
	}
	if total != 2 {
		t.Fatalf("sent %d, want 2", total)
	}
	plan := explainPlan(t, db, stmt)
	if !strings.Contains(plan, "campaign_recipients USING INDEX idx_campaign_recipients_campaign_sent_at (campaign_id=? AND sent_at>? AND sent_at<?)") &&
		!strings.Contains(plan, "campaign_recipients USING COVERING INDEX idx_campaign_recipients_campaign_sent_at (campaign_id=? AND sent_at>? AND sent_at<?)") {
		t.Fatalf("the count must read the campaign and sent time index within the range, plan: %s", plan)
	}
}

// TestCampaignSentIndexReplaced checks the migration drops the campaign only
// sent index and creates the campaign and sent time index
func TestCampaignSentIndexReplaced(t *testing.T) {
	db := newProgramTestDB(t)
	if err := db.Exec(`CREATE INDEX idx_campaign_recipients_campaign_sent ON campaign_recipients (campaign_id) WHERE sent_at IS NOT NULL`).Error; err != nil {
		t.Fatalf("old index: %v", err)
	}
	if err := (database.CampaignRecipient{}).Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var names []string
	db.Raw(`SELECT name FROM sqlite_master WHERE type = 'index' AND name LIKE 'idx_campaign_recipients_campaign_sent%'`).Scan(&names)
	if len(names) != 1 || names[0] != "idx_campaign_recipients_campaign_sent_at" {
		t.Fatalf("indexes %v, want only idx_campaign_recipients_campaign_sent_at", names)
	}
}
