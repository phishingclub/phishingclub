package service

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// unlinkedSend adds a send of a campaign that has only its pseudonym left,
// as after the identity retention of an anonymous program, with a lure code
func (p *programTest) unlinkedSend(campaignID uuid.UUID, sentAt time.Time, code string) (uuid.UUID, uuid.UUID) {
	p.t.Helper()
	id, pseudonym := uuid.New(), uuid.New()
	row := &database.CampaignRecipient{
		ID: &id, CreatedAt: &sentAt, UpdatedAt: &sentAt, SentAt: &sentAt,
		CampaignID: &campaignID, AnonymizedID: &pseudonym,
	}
	if code != "" {
		row.LureCode = &code
	}
	p.create(row)
	p.enrollPseudonym(campaignID, pseudonym)
	return id, pseudonym
}

// enrollPseudonym adds a recipient enrolled with the pseudonym in the program
// of the campaign, as the person behind an unlinked send, and returns the
// recipient. A campaign of no program gets none.
func (p *programTest) enrollPseudonym(campaignID, pseudonym uuid.UUID) uuid.UUID {
	p.t.Helper()
	var campaign database.Campaign
	if err := p.db.Where("id = ?", campaignID).First(&campaign).Error; err != nil {
		p.t.Fatalf("load campaign: %v", err)
	}
	if campaign.ProgramID == nil {
		return uuid.UUID{}
	}
	rid := p.recipient(pseudonym.String() + "@example.com")
	e := p.enroll(*campaign.ProgramID, rid, 0)
	if err := p.db.Model(&database.ProgramEnrollment{}).Where("id = ?", e.ID).
		Update("anonymized_id", pseudonym).Error; err != nil {
		p.t.Fatalf("enrollment pseudonym: %v", err)
	}
	return rid
}

// recipientOfPseudonym returns the recipient enrolled with the pseudonym
func (p *programTest) recipientOfPseudonym(pseudonym uuid.UUID) uuid.UUID {
	p.t.Helper()
	var e database.ProgramEnrollment
	if err := p.db.Where("anonymized_id = ?", pseudonym).First(&e).Error; err != nil {
		p.t.Fatalf("load enrollment: %v", err)
	}
	return *e.RecipientID
}

// anonymousStepCampaign returns an anonymous program and the hidden campaign
// of its first step, both marked anonymous
func (p *programTest) anonymousStepCampaign() (uuid.UUID, uuid.UUID) {
	p.t.Helper()
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	if err := p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("is_anonymous", true).Error; err != nil {
		p.t.Fatalf("mark anonymous: %v", err)
	}
	return programID, campaignID
}

// TestUnlinkedAnonymousProgramSendRule checks only a sent row of an open
// anonymous program without a person link takes the pseudonym path
func TestUnlinkedAnonymousProgramSendRule(t *testing.T) {
	p := newProgramServiceTest(t)
	programID, campaignID := p.anonymousStepCampaign()
	sentAt := time.Now().UTC().Add(-20 * 24 * time.Hour)
	sendID, pseudonym := p.unlinkedSend(campaignID, sentAt, "")
	load := func() (uuid.UUID, bool) {
		campaign, err := p.svc.CampaignRepository.GetByID(p.ctx, &campaignID, &repository.CampaignOption{})
		if err != nil {
			t.Fatalf("campaign: %v", err)
		}
		cr, err := p.svc.CampaignRecipientRepository.GetByCampaignRecipientID(p.ctx, &sendID)
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		return UnlinkedAnonymousProgramSend(p.ctx, p.svc.ProgramRepository, campaign, cr)
	}
	if got, ok := load(); !ok || got != pseudonym {
		t.Fatalf("an unlinked anonymous program send must give its pseudonym, got %v %v", got, ok)
	}
	// a linked send keeps the normal path
	rid := p.recipient("anna@example.com")
	if err := p.db.Model(&database.CampaignRecipient{}).Where("id = ?", sendID).Update("recipient_id", rid).Error; err != nil {
		t.Fatalf("link: %v", err)
	}
	if _, ok := load(); ok {
		t.Fatal("a linked send must not take the pseudonym path")
	}
	if err := p.db.Model(&database.CampaignRecipient{}).Where("id = ?", sendID).Update("recipient_id", nil).Error; err != nil {
		t.Fatalf("unlink: %v", err)
	}
	// a named program does not
	p.setProgram(programID, map[string]any{"is_anonymous": false})
	if _, ok := load(); ok {
		t.Fatal("a named program send must not take the pseudonym path")
	}
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	// a closed program does not
	p.setProgram(programID, map[string]any{"closed_at": time.Now().UTC()})
	if _, ok := load(); ok {
		t.Fatal("a closed program send must not take the pseudonym path")
	}
	p.setProgram(programID, map[string]any{"closed_at": nil})
	// a campaign of no program does not
	if err := p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("program_id", nil).Error; err != nil {
		t.Fatalf("unhide: %v", err)
	}
	if _, ok := load(); ok {
		t.Fatal("a normal anonymous campaign send must not take the pseudonym path")
	}
}

// TestUnlinkedAnonymousProgramSendNeedsLivePerson checks an unlinked send
// only takes the pseudonym path while its person is still enrolled with that
// pseudonym, exists and is not disabled by SCIM
func TestUnlinkedAnonymousProgramSendNeedsLivePerson(t *testing.T) {
	p := newProgramServiceTest(t)
	_, campaignID := p.anonymousStepCampaign()
	sentAt := time.Now().UTC().Add(-20 * 24 * time.Hour)
	campaign, err := p.svc.CampaignRepository.GetByID(p.ctx, &campaignID, &repository.CampaignOption{})
	if err != nil {
		t.Fatalf("campaign: %v", err)
	}
	check := func(sendID uuid.UUID) bool {
		t.Helper()
		cr, err := p.svc.CampaignRecipientRepository.GetByCampaignRecipientID(p.ctx, &sendID)
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		_, ok := UnlinkedAnonymousProgramSend(p.ctx, p.svc.ProgramRepository, campaign, cr)
		return ok
	}
	// a person still enrolled is served
	enrolled, _ := p.unlinkedSend(campaignID, sentAt, "")
	if !check(enrolled) {
		t.Fatal("the send of an enrolled person must take the pseudonym path")
	}
	// a deleted recipient loses its enrollments and its row, even when sent
	// yesterday
	deleted, deletedPseudonym := p.unlinkedSend(campaignID, time.Now().UTC().Add(-24*time.Hour), "")
	deletedRecipient := p.recipientOfPseudonym(deletedPseudonym)
	if err := p.svc.ProgramRepository.UnlinkEnrollmentsOfRecipient(p.ctx, &deletedRecipient); err != nil {
		t.Fatalf("remove enrollments: %v", err)
	}
	if err := p.svc.RecipientRepository.DeleteByID(p.ctx, &deletedRecipient); err != nil {
		t.Fatalf("delete recipient: %v", err)
	}
	if check(deleted) {
		t.Fatal("the send of a deleted recipient must not take the pseudonym path")
	}
	// a SCIM disabled recipient keeps its enrollment
	disabled, disabledPseudonym := p.unlinkedSend(campaignID, sentAt, "")
	disabledRecipient := p.recipientOfPseudonym(disabledPseudonym)
	if err := p.db.Model(&database.Recipient{}).Where("id = ?", disabledRecipient).
		Update("scim_soft_deleted_at", time.Now().UTC()).Error; err != nil {
		t.Fatalf("disable: %v", err)
	}
	if check(disabled) {
		t.Fatal("the send of a SCIM disabled recipient must not take the pseudonym path")
	}
	// an enrollment in another program does not count
	other, otherPseudonym := p.unlinkedSend(campaignID, sentAt, "")
	otherProgram := p.program(model.ProgramLoopComplete)
	if err := p.db.Model(&database.ProgramEnrollment{}).Where("anonymized_id = ?", otherPseudonym).
		Update("program_id", otherProgram).Error; err != nil {
		t.Fatalf("move enrollment: %v", err)
	}
	if check(other) {
		t.Fatal("an enrollment of another program must not count")
	}
}

// TestLiveEnrollmentLookupUsesIndex checks the enrollment lookup of an
// unlinked send reads the program and pseudonym index
func TestLiveEnrollmentLookupUsesIndex(t *testing.T) {
	p := newProgramServiceTest(t)
	if err := (database.ProgramEnrollment{}).Migrate(p.db); err != nil {
		t.Fatalf("migrate index: %v", err)
	}
	programID, pseudonym := uuid.New(), uuid.New()
	var query string
	var vars []any
	if err := p.db.Callback().Row().After("gorm:row").Register("test:capture", func(db *gorm.DB) {
		if query == "" {
			query, vars = db.Statement.SQL.String(), db.Statement.Vars
		}
	}); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if _, err := p.svc.ProgramRepository.HasLiveEnrollmentByPseudonym(p.ctx, &programID, &pseudonym); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	type planRow struct {
		ID      int
		Parent  int
		Notused int
		Detail  string
	}
	rows := []planRow{}
	if err := p.db.Raw("EXPLAIN QUERY PLAN "+query, vars...).Scan(&rows).Error; err != nil {
		t.Fatalf("plan: %v", err)
	}
	found := false
	for _, r := range rows {
		if strings.Contains(r.Detail, "idx_program_enrollments_program_pseudonym") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the lookup must use the program and pseudonym index: %+v", rows)
	}
}

// TestUnlinkedAnonymousProgramEventsCounted checks clicks recorded on the
// pseudonym of sends that lost their person link count in the results
func TestUnlinkedAnonymousProgramEventsCounted(t *testing.T) {
	p := newProgramServiceTest(t)
	programID, campaignID := p.anonymousStepCampaign()
	now := time.Now().UTC()
	// a month that has ended and settled
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -2, 0)
	sentAt := month.Add(5 * 24 * time.Hour)
	visit := eventIDFor(data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	for i := 0; i < 10; i++ {
		_, pseudonym := p.unlinkedSend(campaignID, sentAt, "")
		if i < 5 {
			at, id := sentAt.Add(time.Hour), uuid.New()
			p.create(&database.CampaignEvent{
				ID: &id, CreatedAt: &at, UpdatedAt: &at,
				CampaignID: &campaignID, AnonymizedID: &pseudonym, EventID: &visit,
			})
		}
	}
	r, err := p.svc.programResults(p.ctx, nil, p.loadProgram(programID), now, nil, nil)
	if err != nil {
		t.Fatalf("results: %v", err)
	}
	if r.Summary.Sent == nil || *r.Summary.Sent != 10 || r.Summary.ClickedLink == nil || *r.Summary.ClickedLink != 5 {
		t.Fatalf("clicks on the pseudonym must count: %+v", r.Summary)
	}
}

// TestUnlinkedAnonymousProgramTrackingPixel checks the opened event of an
// unlinked anonymous program send is recorded on its pseudonym, and that of a
// normal anonymous campaign is not
func TestUnlinkedAnonymousProgramTrackingPixel(t *testing.T) {
	p := newProgramServiceTest(t)
	eventIDFor(data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ)
	_, campaignID := p.anonymousStepCampaign()
	sentAt := time.Now().UTC().Add(-20 * 24 * time.Hour)
	sendID, pseudonym := p.unlinkedSend(campaignID, sentAt, "")
	c := p.svc.CampaignService
	c.ProgramService = p.svc
	open := func(id uuid.UUID) {
		t.Helper()
		g, _ := gin.CreateTestContext(httptest.NewRecorder())
		g.Request = httptest.NewRequest("GET", "https://phish.test/pixel", nil)
		g.Request.RemoteAddr = "192.0.2.10:40000"
		if err := c.SaveTrackingPixelLoaded(g, &id); err != nil {
			t.Fatalf("pixel: %v", err)
		}
	}
	open(sendID)
	rows := []database.CampaignEvent{}
	p.db.Where("campaign_id = ?", campaignID).Find(&rows)
	if len(rows) != 1 || rows[0].RecipientID != nil || rows[0].AnonymizedID == nil || *rows[0].AnonymizedID != pseudonym ||
		rows[0].IPAddress != "" || rows[0].UserAgent != "" {
		t.Fatalf("the open must be recorded on the pseudonym only: %+v", rows)
	}
	// a normal anonymous campaign whose send has no person records nothing
	normal := uuid.New()
	created := time.Now().UTC()
	p.create(&database.Campaign{ID: &normal, CreatedAt: &created, UpdatedAt: &created, Name: "normal", IsAnonymous: true})
	normalSend, _ := p.unlinkedSend(normal, sentAt, "")
	open(normalSend)
	var count int64
	p.db.Model(&database.CampaignEvent{}).Where("campaign_id = ?", normal).Count(&count)
	if count != 0 {
		t.Fatalf("a normal anonymous campaign must record nothing, got %d", count)
	}
}

// TestCloseReleasesKeptLureCode checks the lure code an unlinked anonymous
// program send keeps is released when its campaign is anonymized at close,
// so the code can be given out again
func TestCloseReleasesKeptLureCode(t *testing.T) {
	p := newProgramServiceTest(t)
	if err := p.db.AutoMigrate(&database.RecipientGroup{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, campaignID := p.anonymousStepCampaign()
	sentAt := time.Now().UTC().Add(-20 * 24 * time.Hour)
	sendID, _ := p.unlinkedSend(campaignID, sentAt, "KEPTCODE12")
	// a send with only a message id left is cleared too
	msgOnlyID, _ := p.unlinkedSend(campaignID, sentAt, "")
	p.db.Model(&database.CampaignRecipient{}).Where("id IN ?", []string{sendID.String(), msgOnlyID.String()}).
		Update("message_id", gorm.Expr("id"))
	if err := p.svc.CampaignService.anonymizeCampaignRecipients(p.ctx, &campaignID); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	for _, id := range []uuid.UUID{sendID, msgOnlyID} {
		var row database.CampaignRecipient
		if err := p.db.Where("id = ?", id).First(&row).Error; err != nil {
			t.Fatalf("load: %v", err)
		}
		if row.LureCode != nil || row.MessageID != nil || row.AnonymizedID == nil {
			t.Fatalf("the close must release the kept lure code and message id and keep the pseudonym: %+v", row)
		}
	}
}

// TestRecipientDeleteReleasesUnlinkedProgramSends checks that deleting a
// recipient clears the lure code and message id of the program sends that
// already lost their recipient link, and leaves the sends of other people
func TestRecipientDeleteReleasesUnlinkedProgramSends(t *testing.T) {
	p := newProgramServiceTest(t)
	if err := p.db.AutoMigrate(&database.RecipientGroup{}, &database.RecipientGroupRecipient{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, campaignID := p.anonymousStepCampaign()
	sentAt := time.Now().UTC().Add(-20 * 24 * time.Hour)
	mineID, pseudonym := p.unlinkedSend(campaignID, sentAt, "MINECODE12")
	otherID, _ := p.unlinkedSend(campaignID, sentAt, "OTHERCODE1")
	p.db.Model(&database.CampaignRecipient{}).Where("id IN ?", []string{mineID.String(), otherID.String()}).
		Update("message_id", gorm.Expr("id"))
	recipientID := p.recipientOfPseudonym(pseudonym)
	svc := &Recipient{
		Common:                      Common{Logger: p.svc.Logger},
		RecipientRepository:         &repository.Recipient{DB: p.db},
		RecipientGroupRepository:    &repository.RecipientGroup{DB: p.db},
		CampaignRepository:          &repository.Campaign{DB: p.db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: p.db},
		ProgramRepository:           &repository.Program{DB: p.db},
	}
	if err := svc.deleteRecipientByID(p.ctx, &recipientID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var mine, other database.CampaignRecipient
	p.db.Where("id = ?", mineID).First(&mine)
	p.db.Where("id = ?", otherID).First(&other)
	if mine.LureCode != nil || mine.MessageID != nil || mine.AnonymizedID == nil {
		t.Fatalf("the deleted person's send must lose its code and message id and keep its pseudonym: %+v", mine)
	}
	if other.LureCode == nil || other.MessageID == nil {
		t.Fatalf("another person's send must be kept: %+v", other)
	}
	var left int64
	p.db.Model(&database.ProgramEnrollment{}).Where("recipient_id = ?", recipientID).Count(&left)
	if left != 0 {
		t.Fatalf("enrollments left = %d", left)
	}
}
