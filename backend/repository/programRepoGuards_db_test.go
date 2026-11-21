package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
)

// TestCancelInActiveCampaignsKeepsSentRows checks a disabled person loses only
// the sends not yet made in a plain campaign, and that the sent rows, a closed
// campaign and the hidden campaigns of a program are left as they are. The
// program cancels its own sends once the person is gone past the grace period
func TestCancelInActiveCampaignsKeepsSentRows(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	recipientID := ptrUUID()
	programID := createProgram(t, db, "program", nil)
	hidden := ptrUUID()
	createHiddenCampaign(t, db, hidden, programID)
	plain := ptrUUID()
	closed := ptrUUID()
	for _, id := range []*uuid.UUID{plain, closed} {
		row := &database.Campaign{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "campaign " + id.String()}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create campaign: %v", err)
		}
	}
	db.Model(&database.Campaign{}).Where("id = ?", closed).Update("closed_at", time.Now())

	hiddenSent := addCampaignRecipient(t, db, hidden, recipientID, ptrUUID(), true)
	plainSent := addCampaignRecipient(t, db, plain, recipientID, nil, true)
	// a later loop of the program and a second plain campaign hold the unsent rows
	hiddenLater := ptrUUID()
	createHiddenCampaign(t, db, hiddenLater, programID)
	hiddenUnsent := addCampaignRecipient(t, db, hiddenLater, recipientID, ptrUUID(), false)
	plainLater := ptrUUID()
	if err := db.Create(&database.Campaign{ID: plainLater, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "later"}).Error; err != nil {
		t.Fatalf("create campaign: %v", err)
	}
	plainUnsent := addCampaignRecipient(t, db, plainLater, recipientID, nil, false)
	closedUnsent := addCampaignRecipient(t, db, closed, recipientID, nil, false)
	other := addCampaignRecipient(t, db, plainLater, ptrUUID(), nil, false)

	cr := &CampaignRecipient{DB: db}
	if err := cr.CancelInActiveCampaigns(ctx, recipientID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	cancelled := func(id *uuid.UUID) bool {
		var row database.CampaignRecipient
		if err := db.Where("id = ?", id).First(&row).Error; err != nil {
			t.Fatalf("read row: %v", err)
		}
		return row.CancelledAt != nil
	}
	for name, id := range map[string]*uuid.UUID{"program sent": hiddenSent, "program unsent": hiddenUnsent, "plain sent": plainSent, "closed campaign": closedUnsent, "other person": other} {
		if cancelled(id) {
			t.Errorf("%s row was cancelled", name)
		}
	}
	for name, id := range map[string]*uuid.UUID{"plain unsent": plainUnsent} {
		if !cancelled(id) {
			t.Errorf("%s row was not cancelled", name)
		}
	}
}

// TestGetDueEnrollmentsSkipsEndedPrograms checks people of a program past its
// end date are not read, so they do not take the places of other programs
func TestGetDueEnrollmentsSkipsEndedPrograms(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	now := time.Now()
	ended := createProgram(t, db, "ended", nil)
	open := createProgram(t, db, "open", nil)
	later := createProgram(t, db, "later", nil)
	db.Model(&database.Program{}).Where("id = ?", ended).Update("end_at", now.Add(-time.Minute).UTC())
	db.Model(&database.Program{}).Where("id = ?", later).Update("end_at", now.Add(time.Hour).UTC())
	past := now.Add(-time.Hour)
	for _, p := range []*uuid.UUID{ended, open, later} {
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
	got := map[uuid.UUID]bool{}
	for _, e := range due {
		got[*e.ProgramID] = true
	}
	if len(due) != 2 || !got[*open] || !got[*later] {
		t.Fatalf("due programs = %v, want the open and the later ending program", got)
	}
}

// TestGetAnonymousProgramIDsSkipsClosed checks the identity removal does not
// visit a closed program or a program that is not anonymous
func TestGetAnonymousProgramIDsSkipsClosed(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	open := createProgram(t, db, "open", nil)
	closed := createProgram(t, db, "closed", nil)
	createProgram(t, db, "named", nil)
	db.Model(&database.Program{}).Where("id IN ?", []string{open.String(), closed.String()}).Update("is_anonymous", true)
	db.Model(&database.Program{}).Where("id = ?", closed).Update("closed_at", time.Now())
	ids, err := r.GetAnonymousProgramIDs(ctx)
	if err != nil {
		t.Fatalf("ids: %v", err)
	}
	if len(ids) != 1 || *ids[0] != *open {
		t.Fatalf("ids = %v, want only the open anonymous program", ids)
	}
}

// TestRemoveIdentityOverBatches checks the identity removal changes every old
// sent row of the program over several batches and leaves other programs
func TestRemoveIdentityOverBatches(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "anon", nil)
	otherID := createProgram(t, db, "other", nil)
	campaign := ptrUUID()
	createHiddenCampaign(t, db, campaign, programID)
	otherCampaign := ptrUUID()
	createHiddenCampaign(t, db, otherCampaign, otherID)
	n := programIdentityBatch*2 + 3
	old := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	rows := make([]*database.CampaignRecipient, 0, n+1)
	for i := 0; i < n+1; i++ {
		c := campaign
		if i == n {
			c = otherCampaign
		}
		rows = append(rows, &database.CampaignRecipient{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			CampaignID: c, RecipientID: ptrUUID(), AnonymizedID: ptrUUID(),
		})
	}
	if err := db.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatalf("add sends: %v", err)
	}
	db.Model(&database.CampaignRecipient{}).Where("1 = 1").Update("sent_at", old)
	prev := WriteChunkPause
	WriteChunkPause = 0
	defer func() { WriteChunkPause = prev }()
	changed, err := r.RemoveIdentityOfSendsBefore(ctx, programID, time.Now().Add(-14*24*time.Hour))
	if err != nil {
		t.Fatalf("remove identity: %v", err)
	}
	if changed != int64(n) {
		t.Fatalf("rows changed = %d, want %d", changed, n)
	}
	var left int64
	db.Model(&database.CampaignRecipient{}).Where("recipient_id IS NOT NULL").Count(&left)
	if left != 1 {
		t.Fatalf("rows with a person = %d, want only the other program row", left)
	}
}
