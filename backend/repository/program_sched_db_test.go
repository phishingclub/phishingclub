package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
)

func enroll(t *testing.T, db *gorm.DB, programID *uuid.UUID, due time.Time, missingSince *time.Time) *uuid.UUID {
	t.Helper()
	recipientID := ptrUUID()
	d := due.UTC()
	e := &database.ProgramEnrollment{
		ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
		ProgramID: programID, RecipientID: recipientID, NextDueAt: &d, Status: "active",
		MissingSince: missingSince,
	}
	if err := db.Create(e).Error; err != nil {
		t.Fatalf("enroll: %v", err)
	}
	return recipientID
}

// TestGetDueEnrollmentsSkipsDraftScheduledAndMissing checks that drafts,
// programs with a start in the future and people missing from the groups are
// not advanced
func TestGetDueEnrollmentsSkipsDraftScheduledAndMissing(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	running := createProgram(t, db, "running", nil)
	draft := createProgram(t, db, "draft", nil)
	scheduled := createProgram(t, db, "scheduled", nil)
	now := time.Now()
	db.Model(&database.Program{}).Where("id = ?", draft).Update("start_at", nil)
	db.Model(&database.Program{}).Where("id = ?", scheduled).Update("start_at", now.Add(time.Hour).UTC())
	past := now.Add(-time.Hour)
	wanted := enroll(t, db, running, past, nil)
	missing := now.Add(-time.Minute).UTC()
	enroll(t, db, running, past, &missing)
	enroll(t, db, draft, past, nil)
	enroll(t, db, scheduled, past, nil)

	due, err := r.GetDueEnrollments(ctx, now, 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 || *due[0].RecipientID != *wanted {
		t.Fatalf("due rows = %d, want only the present person of the running program", len(due))
	}
	active, err := r.GetActivePrograms(ctx, now)
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if len(active) != 1 || active[0].ID.MustGet() != *running {
		t.Fatalf("active programs = %d, want only the running one", len(active))
	}
}

// TestDeliveredIgnoresCancelledAndClosedUnsent checks that a cancelled send and
// a send that never went out from a closed campaign do not use up a step, while
// a sent one and one still waiting in an open campaign do
func TestDeliveredIgnoresCancelledAndClosedUnsent(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "p", nil)
	recipientID := ptrUUID()
	steps := map[string]*uuid.UUID{}
	for i, name := range []string{"sent", "waiting", "cancelled", "closedUnsent", "closedSent"} {
		stepID := createStep(t, db, programID, ptrUUID(), i)
		steps[name] = stepID
		campaignID := linkCampaign(t, db, programID, stepID, 0)
		createHiddenCampaign(t, db, campaignID, programID)
		closed := name == "closedUnsent" || name == "closedSent"
		if closed {
			db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("closed_at", time.Now().Add(-time.Minute).UTC())
		}
		sent := name == "sent" || name == "closedSent"
		crID := addCampaignRecipient(t, db, campaignID, recipientID, nil, sent)
		if name == "cancelled" {
			db.Model(&database.CampaignRecipient{}).Where("id = ?", crID).Update("cancelled_at", utils.NowRFC3339UTC())
		}
	}
	got, err := r.GetDeliveredStepIDs(ctx, programID, recipientID, nil, 0)
	if err != nil {
		t.Fatalf("delivered: %v", err)
	}
	for name, stepID := range steps {
		want := name == "sent" || name == "waiting" || name == "closedSent"
		if got[*stepID] != want {
			t.Errorf("step %s delivered = %v, want %v", name, got[*stepID], want)
		}
	}
	deliveries, err := r.GetDeliveries(ctx, programID, nil)
	if err != nil {
		t.Fatalf("deliveries: %v", err)
	}
	if len(deliveries) != 3 {
		t.Errorf("deliveries = %d, want 3", len(deliveries))
	}
}

// TestDeliveredMatchesPseudonymAfterIdentityRemoved checks that old sends of an
// anonymous program lose the person link but still count by pseudonym, and that
// unsent and recent rows keep it
func TestDeliveredMatchesPseudonymAfterIdentityRemoved(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "anon", nil)
	recipientID := ptrUUID()
	pseudonym := ptrUUID()
	oldStep := createStep(t, db, programID, ptrUUID(), 0)
	oldCampaign := linkCampaign(t, db, programID, oldStep, 0)
	createHiddenCampaign(t, db, oldCampaign, programID)
	oldRow := addCampaignRecipient(t, db, oldCampaign, recipientID, pseudonym, true)
	db.Model(&database.CampaignRecipient{}).Where("id = ?", oldRow).Updates(map[string]any{
		"sent_at":   utils.RFC3339UTC(time.Now().Add(-20 * 24 * time.Hour)),
		"lure_code": "OLDCODE1",
	})
	newStep := createStep(t, db, programID, ptrUUID(), 1)
	newCampaign := linkCampaign(t, db, programID, newStep, 0)
	createHiddenCampaign(t, db, newCampaign, programID)
	newRow := addCampaignRecipient(t, db, newCampaign, recipientID, pseudonym, true)
	unsentStep := createStep(t, db, programID, ptrUUID(), 2)
	unsentCampaign := linkCampaign(t, db, programID, unsentStep, 0)
	createHiddenCampaign(t, db, unsentCampaign, programID)
	unsentRow := addCampaignRecipient(t, db, unsentCampaign, recipientID, pseudonym, false)

	n, err := r.RemoveIdentityOfSendsBefore(ctx, programID, time.Now().Add(-14*24*time.Hour))
	if err != nil {
		t.Fatalf("remove identity: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows changed = %d, want 1", n)
	}
	var old database.CampaignRecipient
	db.Where("id = ?", oldRow).First(&old)
	if old.RecipientID != nil || old.LureCode != nil || old.AnonymizedID == nil {
		t.Fatal("old row must keep only the pseudonym")
	}
	for _, id := range []*uuid.UUID{newRow, unsentRow} {
		var row database.CampaignRecipient
		db.Where("id = ?", id).First(&row)
		if row.RecipientID == nil {
			t.Fatal("recent and unsent rows must keep the person link")
		}
	}
	// without the pseudonym the old step looks undelivered, with it delivered
	got, err := r.GetDeliveredStepIDs(ctx, programID, recipientID, nil, 0)
	if err != nil {
		t.Fatalf("delivered: %v", err)
	}
	if got[*oldStep] {
		t.Fatal("old step found by recipient id after the link was removed")
	}
	got, err = r.GetDeliveredStepIDs(ctx, programID, recipientID, pseudonym, 0)
	if err != nil {
		t.Fatalf("delivered: %v", err)
	}
	if !got[*oldStep] || !got[*newStep] || !got[*unsentStep] {
		t.Fatalf("pseudonym match missed a step: %v", got)
	}
	send, err := r.GetPersonSend(ctx, oldCampaign, recipientID, pseudonym)
	if err != nil || send == nil || send.ID != *oldRow {
		t.Fatalf("person send by pseudonym = %v, %v", send, err)
	}
}

// TestStepCampaignCanBeReplaced checks that a second campaign can be linked to
// the same step and loop, and that the newest link comes first
func TestStepCampaignCanBeReplaced(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "p", nil)
	stepID := createStep(t, db, programID, ptrUUID(), 0)
	first := linkCampaign(t, db, programID, stepID, 0)
	time.Sleep(5 * time.Millisecond)
	second := ptrUUID()
	if err := r.InsertProgramCampaign(ctx, programID, stepID, 0, second); err != nil {
		t.Fatalf("second link for the same step and loop: %v", err)
	}
	links, err := r.GetProgramCampaigns(ctx, programID, stepID, 0)
	if err != nil {
		t.Fatalf("links: %v", err)
	}
	if len(links) != 2 || *links[0].CampaignID != *second || *links[1].CampaignID != *first {
		t.Fatalf("links not newest first")
	}
	other, err := r.GetProgramCampaigns(ctx, programID, stepID, 1)
	if err != nil || len(other) != 0 {
		t.Fatalf("another loop must have no links, got %d", len(other))
	}
}

// TestReviveCancelledSend checks that a cancelled unsent row goes back in the
// queue and a sent row is never changed
func TestReviveCancelledSend(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "p", nil)
	campaignID := ptrUUID()
	createHiddenCampaign(t, db, campaignID, programID)
	cancelled := addCampaignRecipient(t, db, campaignID, ptrUUID(), nil, false)
	sent := addCampaignRecipient(t, db, campaignID, ptrUUID(), nil, true)
	stamp := utils.NowRFC3339UTC()
	for _, id := range []*uuid.UUID{cancelled, sent} {
		db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("cancelled_at", stamp)
	}
	sendAt := time.Now().Add(time.Hour)
	for _, id := range []*uuid.UUID{cancelled, sent} {
		if err := r.ReviveCancelledSend(ctx, id, sendAt); err != nil {
			t.Fatalf("revive: %v", err)
		}
	}
	var row database.CampaignRecipient
	db.Where("id = ?", cancelled).First(&row)
	if row.CancelledAt != nil || row.SendAt == nil {
		t.Fatal("cancelled unsent row was not revived")
	}
	var sentRow database.CampaignRecipient
	db.Where("id = ?", sent).First(&sentRow)
	if sentRow.CancelledAt == nil {
		t.Fatal("a sent row must not be changed")
	}
}
