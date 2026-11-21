package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
)

// TestToReportSendLogSource checks a program report row is labelled as a
// program with its program id, and a campaign row as a campaign
func TestToReportSendLogSource(t *testing.T) {
	id, campaignID, programID := uuid.New(), uuid.New(), uuid.New()
	campaignRow := ToReportSendLog(&database.ReportSendLog{ID: &id, CampaignID: &campaignID, CampaignName: "Q1"})
	if campaignRow.Source != model.ReportSendLogSourceCampaign || campaignRow.SourceID != campaignID.String() || campaignRow.SourceName != "Q1" {
		t.Fatalf("campaign row = %+v", campaignRow)
	}
	programRow := ToReportSendLog(&database.ReportSendLog{ID: &id, ProgramID: &programID, CampaignName: "Program A"})
	if programRow.Source != model.ReportSendLogSourceProgram || programRow.SourceID != programID.String() || programRow.SourceName != "Program A" {
		t.Fatalf("program row = %+v", programRow)
	}
}
