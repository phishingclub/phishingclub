package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
)

// TestGetGoneWithUnsentSends checks only people who left the program and still
// have an unsent send of it are found, each once, and that the limit holds
func TestGetGoneWithUnsentSends(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "gone", nil)
	otherProgram := createProgram(t, db, "other", nil)
	campaigns := []*uuid.UUID{ptrUUID(), ptrUUID()}
	for _, id := range campaigns {
		createHiddenCampaign(t, db, id, programID)
	}
	otherCampaign := ptrUUID()
	createHiddenCampaign(t, db, otherCampaign, otherProgram)
	now := time.Now()
	cutoff := now.Add(-24 * time.Hour)
	enroll := func(program *uuid.UUID, status string, missingSince *time.Time) *uuid.UUID {
		rid := ptrUUID()
		e := &database.ProgramEnrollment{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			ProgramID: program, RecipientID: rid, Status: status, MissingSince: missingSince,
		}
		if err := db.Create(e).Error; err != nil {
			t.Fatalf("enroll: %v", err)
		}
		return rid
	}
	long, recent := now.Add(-48*time.Hour), now.Add(-time.Hour)
	exited := enroll(programID, model.ProgramEnrollmentExited, nil)
	missing := enroll(programID, model.ProgramEnrollmentCompleted, &long)
	recentlyMissing := enroll(programID, model.ProgramEnrollmentCompleted, &recent)
	completed := enroll(programID, model.ProgramEnrollmentCompleted, nil)
	active := enroll(programID, model.ProgramEnrollmentActive, &long)
	exitedDone := enroll(programID, model.ProgramEnrollmentExited, nil)
	exitedElsewhere := enroll(otherProgram, model.ProgramEnrollmentExited, nil)
	for _, rid := range []*uuid.UUID{exited, missing, recentlyMissing, completed, active, exitedElsewhere} {
		for _, c := range campaigns {
			addCampaignRecipient(t, db, c, rid, nil, false)
		}
	}
	addCampaignRecipient(t, db, otherCampaign, exitedElsewhere, nil, false)
	// a person who left with only a sent and a cancelled send
	addCampaignRecipient(t, db, campaigns[0], exitedDone, nil, true)
	cancelled := addCampaignRecipient(t, db, campaigns[1], exitedDone, nil, false)
	db.Model(&database.CampaignRecipient{}).Where("id = ?", cancelled).Update("cancelled_at", utils.NowRFC3339UTC())

	ids, err := r.GetGoneWithUnsentSends(ctx, programID, cutoff, 10)
	if err != nil {
		t.Fatalf("gone: %v", err)
	}
	got := map[uuid.UUID]int{}
	for _, id := range ids {
		got[*id]++
	}
	if len(ids) != 2 || got[*exited] != 1 || got[*missing] != 1 {
		t.Fatalf("expected the person who exited and the one missing past the grace once each, got %v", ids)
	}
	ids, err = r.GetGoneWithUnsentSends(ctx, programID, cutoff, 1)
	if err != nil || len(ids) != 1 {
		t.Fatalf("limit: %v %v", ids, err)
	}
}

// TestPauseAfterWrite checks the wait after a write follows the time the
// write took, between WriteChunkPause and MaxBusyWait, and that there is no
// wait when WriteChunkPause is zero
func TestPauseAfterWrite(t *testing.T) {
	defer func(v time.Duration) { WriteChunkPause = v }(WriteChunkPause)
	WriteChunkPause = 0
	start := time.Now()
	if err := PauseAfterWrite(context.Background(), time.Hour); err != nil || time.Since(start) > 20*time.Millisecond {
		t.Fatalf("no wait expected without a pause: %v %s", err, time.Since(start))
	}
	WriteChunkPause = 30 * time.Millisecond
	for _, c := range []struct {
		took, min, max time.Duration
	}{
		{0, 30 * time.Millisecond, 80 * time.Millisecond},
		{60 * time.Millisecond, 60 * time.Millisecond, 150 * time.Millisecond},
		{time.Second, MaxBusyWait, MaxBusyWait + 100*time.Millisecond},
	} {
		start := time.Now()
		if err := PauseAfterWrite(context.Background(), c.took); err != nil {
			t.Fatalf("pause: %v", err)
		}
		if d := time.Since(start); d < c.min || d > c.max {
			t.Fatalf("took %s: waited %s, want between %s and %s", c.took, d, c.min, c.max)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := PauseAfterWrite(ctx, time.Second); err == nil {
		t.Fatal("expected the context error")
	}
}
