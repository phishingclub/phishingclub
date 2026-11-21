package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// reportService returns the campaign service wired for report tests, with the
// program service and the recipient repository set, and the webhook table
// migrated so a linked send can run its webhook step
func (p *programTest) reportService() *Campaign {
	p.t.Helper()
	eventIDFor(data.EVENT_CAMPAIGN_RECIPIENT_REPORTED)
	if err := p.db.AutoMigrate(&database.CampaignWebhook{}); err != nil {
		p.t.Fatalf("migrate webhook: %v", err)
	}
	c := p.svc.CampaignService
	c.ProgramService = p.svc
	c.RecipientRepository = &repository.Recipient{DB: p.db}
	return c
}

func (p *programTest) reportEvents(campaignID uuid.UUID) []database.CampaignEvent {
	p.t.Helper()
	rows := []database.CampaignEvent{}
	reportedID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]
	if err := p.db.Where("campaign_id = ? AND event_id = ?", campaignID, reportedID.String()).Find(&rows).Error; err != nil {
		p.t.Fatalf("report events: %v", err)
	}
	return rows
}

func reportContext() *gin.Context {
	g, _ := gin.CreateTestContext(httptest.NewRecorder())
	g.Request = httptest.NewRequest("GET", "https://phish.test/report", nil)
	g.Request.RemoteAddr = "192.0.2.10:40000"
	return g
}

// TestUnlinkedAnonymousProgramButtonReport checks the report button records a
// report of a send that lost its person link on its pseudonym only, with no
// identity, and does not record it twice
func TestUnlinkedAnonymousProgramButtonReport(t *testing.T) {
	p := newProgramServiceTest(t)
	c := p.reportService()
	_, campaignID := p.anonymousStepCampaign()
	sentAt := time.Now().UTC().Add(-20 * 24 * time.Hour)
	sendID, pseudonym := p.unlinkedSend(campaignID, sentAt, "")

	if err := c.SaveRecipientReported(reportContext(), &sendID); err != nil {
		t.Fatalf("report: %v", err)
	}
	rows := p.reportEvents(campaignID)
	if len(rows) != 1 {
		t.Fatalf("one report event must be recorded, got %d", len(rows))
	}
	r := rows[0]
	if r.RecipientID != nil || r.AnonymizedID == nil || *r.AnonymizedID != pseudonym {
		t.Fatalf("the report must carry the pseudonym only: %+v", r)
	}
	if r.IPAddress != "" || r.UserAgent != "" || r.Data != "" || r.Metadata != "" {
		t.Fatalf("the report must carry no identity or data: %+v", r)
	}
	// a second report of the same send does not add another event
	if err := c.SaveRecipientReported(reportContext(), &sendID); err != nil {
		t.Fatalf("second report: %v", err)
	}
	if n := len(p.reportEvents(campaignID)); n != 1 {
		t.Fatalf("a repeat report must not record again, got %d", n)
	}
}

// TestUnlinkedAnonymousProgramInboxReport checks a report found in a report
// inbox is recorded on the pseudonym of an unlinked send, matched by the
// Message-ID still kept on the row
func TestUnlinkedAnonymousProgramInboxReport(t *testing.T) {
	p := newProgramServiceTest(t)
	c := p.reportService()
	_, campaignID := p.anonymousStepCampaign()
	sentAt := time.Now().UTC().Add(-20 * 24 * time.Hour)
	sendID, pseudonym := p.unlinkedSend(campaignID, sentAt, "")
	const messageID = "kept-id@send.example"
	if err := p.db.Model(&database.CampaignRecipient{}).Where("id = ?", sendID).Update("message_id", messageID).Error; err != nil {
		t.Fatalf("set message id: %v", err)
	}
	// the Message-ID still finds the send after the person link is gone
	cr, err := c.CampaignRecipientRepository.GetByMessageID(p.ctx, messageID)
	if err != nil {
		t.Fatalf("message id lookup: %v", err)
	}
	if cr.ID.MustGet() != sendID {
		t.Fatalf("the Message-ID must find the unlinked send")
	}
	res, err := c.RecordInboxReport(p.ctx, cr, nil, sentAt.Add(48*time.Hour), "someone@elsewhere.example", "soc@phish.test")
	if err != nil {
		t.Fatalf("inbox report: %v", err)
	}
	if res != InboxReportAnonymous {
		t.Fatalf("the inbox report must be handled as anonymous, got %v", res)
	}
	rows := p.reportEvents(campaignID)
	if len(rows) != 1 {
		t.Fatalf("one report event must be recorded, got %d", len(rows))
	}
	r := rows[0]
	if r.RecipientID != nil || r.AnonymizedID == nil || *r.AnonymizedID != pseudonym {
		t.Fatalf("the inbox report must carry the pseudonym only: %+v", r)
	}
	// the anonymization strips every field, so no identity, data or metadata is
	// kept, not even the inbox source marker
	if r.IPAddress != "" || r.UserAgent != "" || r.Data != "" || r.Metadata != "" {
		t.Fatalf("the inbox report must carry no identity or data: %+v", r)
	}
}

// TestReportLinkedNamedProgramUnchanged checks a send that keeps its person
// link on a named program records the report on the recipient, as before
func TestReportLinkedNamedProgramUnchanged(t *testing.T) {
	p := newProgramServiceTest(t)
	c := p.reportService()
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	recipientID := p.recipient("bob@example.com")
	sentAt := time.Now().UTC().Add(-time.Hour)
	sendID := uuid.New()
	p.create(&database.CampaignRecipient{
		ID: &sendID, CreatedAt: &sentAt, UpdatedAt: &sentAt, SentAt: &sentAt,
		CampaignID: &campaignID, RecipientID: &recipientID,
	})

	if err := c.SaveRecipientReported(reportContext(), &sendID); err != nil {
		t.Fatalf("report: %v", err)
	}
	rows := p.reportEvents(campaignID)
	if len(rows) != 1 {
		t.Fatalf("one report event must be recorded, got %d", len(rows))
	}
	r := rows[0]
	if r.RecipientID == nil || *r.RecipientID != recipientID || r.AnonymizedID != nil {
		t.Fatalf("a named report must carry the recipient: %+v", r)
	}
}

// TestReportUnlinkedOutsideAnonymousProgramSkipped checks every other send
// without a person link records no report: a normal anonymous campaign and a
// closed anonymous program
func TestReportUnlinkedOutsideAnonymousProgramSkipped(t *testing.T) {
	p := newProgramServiceTest(t)
	c := p.reportService()

	// a normal anonymous campaign of no program
	normalID := uuid.New()
	created := time.Now().UTC()
	p.create(&database.Campaign{ID: &normalID, CreatedAt: &created, UpdatedAt: &created, Name: "normal", IsAnonymous: true})
	sentAt := time.Now().UTC().Add(-20 * 24 * time.Hour)
	normalSend, _ := p.unlinkedSend(normalID, sentAt, "")

	// a send of an anonymous program that has closed
	programID, closedCampaignID := p.anonymousStepCampaign()
	p.setProgram(programID, map[string]any{"closed_at": time.Now().UTC()})
	closedSend, _ := p.unlinkedSend(closedCampaignID, sentAt, "")

	for name, sendID := range map[string]uuid.UUID{
		"normal anonymous campaign": normalSend,
		"closed anonymous program":  closedSend,
	} {
		if err := c.SaveRecipientReported(reportContext(), &sendID); err != nil {
			t.Fatalf("%s: report: %v", name, err)
		}
	}
	if n := len(p.reportEvents(normalID)); n != 0 {
		t.Fatalf("a normal anonymous campaign must record no report, got %d", n)
	}
	if n := len(p.reportEvents(closedCampaignID)); n != 0 {
		t.Fatalf("a closed anonymous program must record no report, got %d", n)
	}
}
