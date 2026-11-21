package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// rbUnlinkedFixture serves the public victim endpoints on a temp SQLite database
type rbUnlinkedFixture struct {
	t          *testing.T
	db         *gorm.DB
	controller *RemoteBrowserController
	now        time.Time
	rbID       uuid.UUID
}

func newRBUnlinkedFixture(t *testing.T) *rbUnlinkedFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := filepath.Join(t.TempDir(), "rb.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Campaign{},
		&database.CampaignRecipient{},
		&database.CampaignEvent{},
		&database.Recipient{},
		&database.Program{},
		&database.ProgramEnrollment{},
		&database.RemoteBrowser{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := zap.NewNop().Sugar()
	programRepo := &repository.Program{DB: db}
	campaignService := &service.Campaign{
		Common: service.Common{Logger: log},
		ProgramService: &service.Program{
			Common:            service.Common{Logger: log},
			ProgramRepository: programRepo,
		},
	}
	f := &rbUnlinkedFixture{t: t, db: db, now: time.Now().UTC()}
	rbRepo := &repository.RemoteBrowser{DB: db}
	f.controller = &RemoteBrowserController{
		Common:                      Common{Logger: log},
		RemoteBrowserService:        &service.RemoteBrowser{Common: service.Common{Logger: log}, RemoteBrowserRepository: rbRepo},
		RemoteBrowserRepository:     rbRepo,
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: db},
		CampaignRepository:          &repository.Campaign{DB: db},
		CampaignService:             campaignService,
		Enabled:                     true,
	}
	rbID := uuid.New()
	f.create(&database.RemoteBrowser{
		ID: &rbID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "script", Script: "// noop", Config: "{}",
	})
	f.rbID = rbID
	return f
}

func (f *rbUnlinkedFixture) create(row any) {
	f.t.Helper()
	if err := f.db.Create(row).Error; err != nil {
		f.t.Fatalf("create %T: %v", row, err)
	}
}

// program creates a started program, anonymous and closed as asked
func (f *rbUnlinkedFixture) program(anonymous, closed bool) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	started := f.now.Add(-60 * 24 * time.Hour)
	row := &database.Program{
		ID: &id, CreatedAt: &started, UpdatedAt: &started, Name: "program " + id.String(),
		StartAt: &started, StartedAt: &started, IsAnonymous: anonymous,
	}
	if closed {
		row.ClosedAt = &f.now
	}
	f.create(row)
	return id
}

// campaign creates an open campaign, hidden in programID when set
func (f *rbUnlinkedFixture) campaign(programID *uuid.UUID, anonymous bool) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.create(&database.Campaign{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "campaign " + id.String(),
		ProgramID: programID, IsAnonymous: anonymous,
	})
	return id
}

// send creates a send sent 20 days ago. linked keeps the person link, else only
// the pseudonym is left, and the person stays enrolled with that pseudonym.
func (f *rbUnlinkedFixture) send(campaignID uuid.UUID, linked bool) uuid.UUID {
	f.t.Helper()
	recipientID, pseudonym, sendID := uuid.New(), uuid.New(), uuid.New()
	email := "alice." + recipientID.String()[:8] + "@example.com"
	f.create(&database.Recipient{ID: &recipientID, CreatedAt: &f.now, UpdatedAt: &f.now, Email: &email})
	sentAt := f.now.Add(-20 * 24 * time.Hour)
	row := &database.CampaignRecipient{
		ID: &sendID, CreatedAt: &sentAt, UpdatedAt: &sentAt, SentAt: &sentAt,
		CampaignID: &campaignID, AnonymizedID: &pseudonym,
	}
	if linked {
		row.RecipientID = &recipientID
	}
	f.create(row)
	var campaign database.Campaign
	if err := f.db.Where("id = ?", campaignID).First(&campaign).Error; err != nil {
		f.t.Fatalf("load campaign: %v", err)
	}
	if campaign.ProgramID != nil {
		enrollmentID := uuid.New()
		f.create(&database.ProgramEnrollment{
			ID: &enrollmentID, CreatedAt: &f.now, UpdatedAt: &f.now,
			ProgramID: campaign.ProgramID, RecipientID: &recipientID,
			Status: "active", AnonymizedID: &pseudonym,
		})
	}
	return sendID
}

// victimContext builds a gin context for the public victim url of a send
func (f *rbUnlinkedFixture) victimContext(sendID uuid.UUID) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	g, _ := gin.CreateTestContext(w)
	g.Request = httptest.NewRequest(http.MethodGet, "https://phish.test/rb/"+sendID.String()+"/"+f.rbID.String(), nil)
	g.Request.RemoteAddr = "192.0.2.10:40000"
	g.Params = gin.Params{
		{Key: "crID", Value: sendID.String()},
		{Key: "rbID", Value: f.rbID.String()},
	}
	return g, w
}

// TestRemoteBrowserUnlinkedServesExpired checks the public victim endpoint
// serves a clean 410 expired page for a send that lost its person link, over
// both transports, and stores nothing
func TestRemoteBrowserUnlinkedServesExpired(t *testing.T) {
	f := newRBUnlinkedFixture(t)
	programID := f.program(true, false)
	campaignID := f.campaign(&programID, true)
	sendID := f.send(campaignID, false)

	g, w := f.victimContext(sendID)
	f.controller.ServeVictim(g)
	if w.Code != http.StatusGone {
		t.Fatalf("ServeVictim must serve the expired page, got %d", w.Code)
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "expired") {
		t.Fatalf("expired page body, got %q", w.Body.String())
	}

	g, w = f.victimContext(sendID)
	pt, ok := f.controller.pollSession(g, true)
	if ok || pt != nil {
		t.Fatalf("pollSession must not start a session for an unlinked send, got %v", ok)
	}
	if w.Code != http.StatusGone {
		t.Fatalf("pollSession must serve the expired page, got %d", w.Code)
	}

	var events int64
	f.db.Model(&database.CampaignEvent{}).Where("campaign_id = ?", campaignID).Count(&events)
	if events != 0 {
		t.Fatalf("the victim endpoint must store nothing, got %d events", events)
	}
}

// TestRemoteBrowserLinkedNotExpired checks a send that keeps its person link is
// not served the expired page, and every other unlinked send stops as before
func TestRemoteBrowserLinkedNotExpired(t *testing.T) {
	f := newRBUnlinkedFixture(t)
	programID := f.program(true, false)
	campaignID := f.campaign(&programID, true)

	cr, err := f.controller.CampaignRecipientRepository.GetByCampaignRecipientID(
		context.Background(), ptr(f.send(campaignID, true)))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	g, _ := gin.CreateTestContext(httptest.NewRecorder())
	g.Request = httptest.NewRequest(http.MethodGet, "https://phish.test/", nil)
	if f.controller.serveExpiredIfUnlinked(g, cr) {
		t.Fatal("a linked send must not serve the expired page")
	}

	// a named program and a closed anonymous program never take the expired path
	named := f.campaign(ptr(f.program(false, false)), false)
	closed := f.campaign(ptr(f.program(true, true)), true)
	for name, campaignID := range map[string]uuid.UUID{"named": named, "closed anonymous": closed} {
		cr, err := f.controller.CampaignRecipientRepository.GetByCampaignRecipientID(
			context.Background(), ptr(f.send(campaignID, false)))
		if err != nil {
			t.Fatalf("%s send: %v", name, err)
		}
		g, _ := gin.CreateTestContext(httptest.NewRecorder())
		g.Request = httptest.NewRequest(http.MethodGet, "https://phish.test/", nil)
		if f.controller.serveExpiredIfUnlinked(g, cr) {
			t.Fatalf("%s: must not serve the expired page", name)
		}
	}
}

func ptr[T any](v T) *T { return &v }
