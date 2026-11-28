package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"go.uber.org/zap/zapcore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// closeReportSkipFixture is a Campaign service with only what the report
// delivery path reads before it builds a report, on a temp database
type closeReportSkipFixture struct {
	t         *testing.T
	ctx       context.Context
	db        *gorm.DB
	svc       *Campaign
	audit     interface{ String() string }
	companyID uuid.UUID
	groupID   uuid.UUID
	infoID    uuid.UUID
}

func newCloseReportSkipFixture(t *testing.T) *closeReportSkipFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "reportskip.db") + "?_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Company{},
		&database.Option{},
		&database.Campaign{},
		&database.CampaignLanguageRoute{},
		&database.CampaignEvent{},
		&database.Event{},
		&database.Recipient{},
		&database.RecipientGroup{},
		&database.RecipientGroupRecipient{},
		&database.CompanyReportConfig{},
		&database.ReportSendLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := cache.EventIDByName[data.EVENT_CAMPAIGN_INFO]
	t.Cleanup(func() { cache.EventIDByName[data.EVENT_CAMPAIGN_INFO] = prev })
	infoID := uuid.New()
	cache.EventIDByName[data.EVENT_CAMPAIGN_INFO] = &infoID

	auditLogger, buf := bufferLogger(zapcore.InfoLevel)
	f := &closeReportSkipFixture{
		t:      t,
		ctx:    context.Background(),
		db:     db,
		audit:  buf,
		infoID: infoID,
		svc: &Campaign{
			Common:                        Common{Logger: auditLogger, AuditLogger: auditLogger},
			CampaignRepository:            &repository.Campaign{DB: db},
			CompanyReportConfigRepository: &repository.CompanyReportConfig{DB: db},
			ReportSendLogRepository:       &repository.ReportSendLog{DB: db},
			RecipientGroupRepository:      &repository.RecipientGroup{DB: db},
			OptionService: &Option{
				Common:           Common{Logger: auditLogger},
				OptionRepository: &repository.Option{DB: db},
			},
		},
	}
	now := time.Now()
	optionID := uuid.New()
	f.create(&database.Option{ID: &optionID, Key: data.OptionKeyReportPDFEnabled, Value: "true"})
	f.companyID = uuid.New()
	f.create(&database.Company{ID: f.companyID, CreatedAt: &now, UpdatedAt: &now, Name: "reports", AssetsKey: "reports"})
	f.groupID = uuid.New()
	f.create(&database.RecipientGroup{ID: &f.groupID, CreatedAt: &now, UpdatedAt: &now, Name: "report readers", CompanyID: &f.companyID})
	return f
}

func (f *closeReportSkipFixture) create(row any) {
	f.t.Helper()
	if err := f.db.Create(row).Error; err != nil {
		f.t.Fatalf("insert %T: %v", row, err)
	}
}

// config writes the company report config. complete sets every field a
// delivery needs, otherwise the sender is left out
func (f *closeReportSkipFixture) config(sendOnFinish, complete bool) {
	f.t.Helper()
	now := time.Now()
	id := uuid.New()
	smtpID := uuid.New()
	row := &database.CompanyReportConfig{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, CompanyID: &f.companyID,
		Enabled: true, SendOnFinish: sendOnFinish,
		RecipientGroupID: &f.groupID, SMTPConfigurationID: &smtpID,
	}
	if complete {
		row.SenderEmail = "reports@example.com"
	}
	f.create(row)
}

func (f *closeReportSkipFixture) campaign(isTest bool) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	now := utils.NowRFC3339UTC()
	if err := f.db.Model(&database.Campaign{}).Create(map[string]any{
		"id": id, "created_at": now, "updated_at": now, "closed_at": now,
		"name": "campaign " + id.String(), "company_id": f.companyID, "is_test": isTest,
	}).Error; err != nil {
		f.t.Fatalf("seed campaign: %v", err)
	}
	return id
}

func (f *closeReportSkipFixture) logs() []database.ReportSendLog {
	f.t.Helper()
	var rows []database.ReportSendLog
	if err := f.db.Find(&rows).Error; err != nil {
		f.t.Fatalf("read logs: %v", err)
	}
	return rows
}

func (f *closeReportSkipFixture) infoEvents(campaignID uuid.UUID) []database.CampaignEvent {
	f.t.Helper()
	var rows []database.CampaignEvent
	if err := f.db.Where("campaign_id = ? AND event_id = ?", campaignID, f.infoID).Find(&rows).Error; err != nil {
		f.t.Fatalf("read events: %v", err)
	}
	return rows
}

func (f *closeReportSkipFixture) lastSentAt() *time.Time {
	f.t.Helper()
	var row database.CompanyReportConfig
	if err := f.db.Where("company_id = ?", f.companyID).First(&row).Error; err != nil {
		f.t.Fatalf("read config: %v", err)
	}
	return row.LastSentAt
}

// TestCloseReportSkipsTestCampaign checks the automatic report of a closed
// test campaign is not sent, and the skip is written to the delivery log, the
// campaign events and the audit log
func TestCloseReportSkipsTestCampaign(t *testing.T) {
	f := newCloseReportSkipFixture(t)
	f.config(true, true)
	campaignID := f.campaign(true)

	if err := f.svc.SendCampaignReport(f.ctx, authorizedSession(t), &campaignID, false); err != nil {
		t.Fatalf("send: %v", err)
	}
	logs := f.logs()
	if len(logs) != 1 {
		t.Fatalf("expected one delivery log row, got %d", len(logs))
	}
	row := logs[0]
	if row.Status != "skipped" || row.Trigger != "on_finish" || row.ErrorMessage != reportSkippedTestCampaign {
		t.Fatalf("unexpected row: status %q trigger %q message %q", row.Status, row.Trigger, row.ErrorMessage)
	}
	if row.GroupName != "report readers" || row.SenderEmail != "reports@example.com" || row.RecipientCount != 0 {
		t.Fatalf("unexpected row: group %q sender %q count %d", row.GroupName, row.SenderEmail, row.RecipientCount)
	}
	if row.CampaignID == nil || *row.CampaignID != campaignID || row.CompanyID == nil || *row.CompanyID != f.companyID {
		t.Fatal("the row must name the campaign and its company")
	}
	events := f.infoEvents(campaignID)
	if len(events) != 1 || events[0].Data != "Automatic report skipped: "+reportSkippedTestCampaign {
		t.Fatalf("expected one info event with the reason, got %+v", events)
	}
	if f.lastSentAt() != nil {
		t.Fatal("a skipped report must not set last sent")
	}
	ev := step4Events(t, f.audit.String(), "Campaign.SendCampaignReport")
	if len(ev) != 1 || ev[0]["result"] != "success" {
		t.Fatalf("expected one success audit event, got %v", ev)
	}
	if d := eventDetails(t, ev[0]); d["outcome"] != "skipped" || d["onDemand"] != false {
		t.Fatalf("audit details: %v", d)
	}
}

// TestCloseReportSkipsTestCampaignBeforeConfigCheck checks a test campaign is
// logged as skipped, not failed, when the delivery config is incomplete, as
// nothing would be sent for it either way
func TestCloseReportSkipsTestCampaignBeforeConfigCheck(t *testing.T) {
	f := newCloseReportSkipFixture(t)
	f.config(true, false)
	campaignID := f.campaign(true)

	if err := f.svc.SendCampaignReport(f.ctx, authorizedSession(t), &campaignID, false); err != nil {
		t.Fatalf("send: %v", err)
	}
	logs := f.logs()
	if len(logs) != 1 || logs[0].Status != "skipped" || logs[0].GroupName != "report readers" || logs[0].SenderEmail != "" {
		t.Fatalf("expected one skipped row without a sender, got %+v", logs)
	}
	if len(f.infoEvents(campaignID)) != 1 {
		t.Fatal("expected one info event")
	}
}

// TestCloseReportTestCampaignWithoutSendOnFinish checks nothing is written for
// a test campaign when the company has not opted into sending on close
func TestCloseReportTestCampaignWithoutSendOnFinish(t *testing.T) {
	f := newCloseReportSkipFixture(t)
	f.config(false, true)
	campaignID := f.campaign(true)

	if err := f.svc.SendCampaignReport(f.ctx, authorizedSession(t), &campaignID, false); err != nil {
		t.Fatalf("send: %v", err)
	}
	if n := len(f.logs()); n != 0 {
		t.Fatalf("expected no delivery log row, got %d", n)
	}
	if n := len(f.infoEvents(campaignID)); n != 0 {
		t.Fatalf("expected no info event, got %d", n)
	}
}

// TestCloseReportOnDemandTestCampaignNotSkipped checks Send Report is not
// skipped for a test campaign. An incomplete config stops it at the config
// check, which only a delivery that is attempted reaches
func TestCloseReportOnDemandTestCampaignNotSkipped(t *testing.T) {
	f := newCloseReportSkipFixture(t)
	f.config(true, false)
	campaignID := f.campaign(true)

	if err := f.svc.SendCampaignReport(f.ctx, authorizedSession(t), &campaignID, true); err == nil {
		t.Fatal("expected the incomplete config to be refused")
	}
	logs := f.logs()
	if len(logs) != 1 || logs[0].Status != "failed" || logs[0].Trigger != "on_demand" {
		t.Fatalf("expected one failed on demand row, got %+v", logs)
	}
	if n := len(f.infoEvents(campaignID)); n != 0 {
		t.Fatalf("an on demand send must not add a skip event, got %d", n)
	}
}

// TestCloseReportReadyIgnoresTestCampaign checks a closing test campaign does
// not wait for a report browser, while a production one does
func TestCloseReportReadyIgnoresTestCampaign(t *testing.T) {
	f := newCloseReportSkipFixture(t)
	f.config(true, true)
	if f.svc.closeReportReady(f.campaign(true)) {
		t.Fatal("a test campaign needs no report browser")
	}
	if !f.svc.closeReportReady(f.campaign(false)) {
		t.Fatal("a production campaign with a complete config needs a report browser")
	}
}
