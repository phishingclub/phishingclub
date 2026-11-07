package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
)

// TestProgramResultReads checks the result reads stay inside one program, find
// the latest send by recipient or pseudonym and page the people table
func TestProgramResultReads(t *testing.T) {
	db := newProgramTestDB(t)
	if err := db.AutoMigrate(&database.Recipient{}); err != nil {
		t.Fatalf("migrate recipients: %v", err)
	}
	ctx := context.Background()
	repo := &Program{DB: db}

	programID := createProgram(t, db, "p", nil)
	otherID := createProgram(t, db, "other", nil)
	c1, c2, other := ptrUUID(), ptrUUID(), ptrUUID()
	createHiddenCampaign(t, db, c1, programID)
	createHiddenCampaign(t, db, c2, programID)
	createHiddenCampaign(t, db, other, otherID)

	alice, bob := ptrUUID(), ptrUUID()
	for i, r := range []struct {
		id    *uuid.UUID
		email string
		first string
	}{{alice, "alice@example.com", "Alice"}, {bob, "bob@example.com", "Bob"}} {
		email := r.email
		row := &database.Recipient{ID: r.id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Email: &email, FirstName: r.first}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create recipient %d: %v", i, err)
		}
		due := time.Now().Add(time.Duration(i+1) * time.Hour)
		e := &database.ProgramEnrollment{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			ProgramID: programID, RecipientID: r.id, Status: model.ProgramEnrollmentActive, NextDueAt: &due,
		}
		if err := db.Create(e).Error; err != nil {
			t.Fatalf("create enrollment: %v", err)
		}
	}

	pseudonym := ptrUUID()
	first := addCampaignRecipient(t, db, c1, alice, nil, true)
	second := addCampaignRecipient(t, db, c2, nil, pseudonym, true)
	addCampaignRecipient(t, db, c1, bob, nil, false)
	addCampaignRecipient(t, db, other, alice, nil, true)
	old := time.Now().Add(-48 * time.Hour).UTC()
	if err := db.Model(&database.CampaignRecipient{}).Where("id = ?", first).Update("sent_at", utils.RFC3339UTC(old)).Error; err != nil {
		t.Fatalf("set sent_at: %v", err)
	}
	clickID := ptrUUID()
	addEvent(t, db, c1, alice, nil, clickID)
	addEvent(t, db, other, alice, nil, clickID)

	sent, err := repo.GetSentRows(ctx, programID, nil)
	if err != nil || len(sent) != 2 {
		t.Fatalf("expected 2 sent rows of the program, got %d (%v)", len(sent), err)
	}
	sent, err = repo.GetSentRows(ctx, programID, []*uuid.UUID{alice})
	if err != nil || len(sent) != 1 || sent[0].ID != *first {
		t.Fatalf("expected alice's one sent row, got %v (%v)", sent, err)
	}
	events, err := repo.GetEventRows(ctx, programID, nil, []*uuid.UUID{clickID})
	if err != nil || len(events) != 1 || events[0].CampaignID != *c1 {
		t.Fatalf("expected one event of the program, got %v (%v)", events, err)
	}

	// latest send of the person matched by recipient id or pseudonym
	latest, err := repo.GetLatestPhishingSend(ctx, programID, alice, pseudonym, nil)
	if err != nil || latest.ID.MustGet() != *second {
		t.Fatalf("expected the newest send, got %v", err)
	}
	before := time.Now().Add(-24 * time.Hour)
	latest, err = repo.GetLatestPhishingSend(ctx, programID, alice, pseudonym, &before)
	if err != nil || latest.ID.MustGet() != *first {
		t.Fatalf("expected the send before the report time, got %v", err)
	}
	tooEarly := time.Now().Add(-72 * time.Hour)
	if _, err := repo.GetLatestPhishingSend(ctx, programID, alice, nil, &tooEarly); err == nil {
		t.Fatalf("expected no send before the first one")
	}

	// people page: search, sort and limit
	rows, count, err := repo.GetEnrollmentPage(ctx, programID, &vo.QueryArgs{
		OrderBy: ProgramEnrollmentSortColumns["email"], Desc: true, Limit: 1,
	})
	if err != nil || count != 2 || len(rows) != 1 || rows[0].Recipient == nil || *rows[0].Recipient.Email != "bob@example.com" {
		t.Fatalf("expected bob first of 2, got %d rows count %d (%v)", len(rows), count, err)
	}
	rows, count, err = repo.GetEnrollmentPage(ctx, programID, &vo.QueryArgs{Search: "ali"})
	if err != nil || count != 1 || len(rows) != 1 || *rows[0].RecipientID != *alice {
		t.Fatalf("expected search to find alice, got %d (%v)", count, err)
	}
	// risk_score is a real column in older databases but is not sortable, so it
	// stands in for a name outside the allowlist
	if _, _, err := repo.GetEnrollmentPage(ctx, programID, &vo.QueryArgs{OrderBy: "risk_score"}); err == nil {
		t.Fatalf("expected a column outside the allowlist to be rejected")
	}
	counts, err := repo.GetEnrollmentStatusCounts(ctx, programID)
	if err != nil || len(counts) != 1 || counts[0].Count != 2 {
		t.Fatalf("expected 2 active, got %v (%v)", counts, err)
	}
	enrollments, err := repo.GetEnrollmentsByRecipientID(ctx, alice)
	if err != nil || len(enrollments) != 1 {
		t.Fatalf("expected one enrollment for alice, got %d (%v)", len(enrollments), err)
	}
}
