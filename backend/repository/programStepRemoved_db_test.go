package repository

import (
	"context"
	"testing"

	"github.com/phishingclub/phishingclub/database"
)

// TestIsCampaignStepRemoved checks a hidden campaign whose step is still in its
// program is not reported, and one whose step was removed is
func TestIsCampaignStepRemoved(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "steps", nil)
	stepID := createStep(t, db, programID, ptrUUID(), 0)
	campaignID := linkCampaign(t, db, programID, stepID, 0)
	if removed, err := r.IsCampaignStepRemoved(ctx, campaignID); err != nil || removed {
		t.Fatalf("a step still in the program: %v %v", removed, err)
	}
	if err := db.Where("id = ?", stepID).Delete(&database.ProgramStep{}).Error; err != nil {
		t.Fatalf("remove step: %v", err)
	}
	if removed, err := r.IsCampaignStepRemoved(ctx, campaignID); err != nil || !removed {
		t.Fatalf("a removed step: %v %v", removed, err)
	}
	if removed, err := r.IsCampaignStepRemoved(ctx, ptrUUID()); err != nil || removed {
		t.Fatalf("a campaign of no program: %v %v", removed, err)
	}
}
