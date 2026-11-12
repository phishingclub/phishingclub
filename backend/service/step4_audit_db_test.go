package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap/zapcore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func step4Events(t *testing.T, out string, name string) []map[string]any {
	t.Helper()
	var ev []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["name"] == name {
			ev = append(ev, m)
		}
	}
	return ev
}

func TestCampaignExportEventsAudit(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "export.db") + "?_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.CampaignEvent{}, &database.Recipient{}); err != nil {
		t.Fatal(err)
	}
	auditLogger, buf := bufferLogger(zapcore.InfoLevel)
	c := &Campaign{
		Common:             Common{Logger: auditLogger},
		CampaignRepository: &repository.Campaign{DB: db},
	}
	ctx := context.Background()
	campaignID := uuid.New()
	q := &vo.QueryArgs{}

	if _, err := c.ExportEventsByCampaignID(ctx, adminSession(uuid.New(), data.RoleCompanyUser), &campaignID, q, nil, "submissions"); err == nil {
		t.Fatal("expected a role without permissions to be refused")
	}
	res, err := c.ExportEventsByCampaignID(ctx, adminSession(uuid.New(), data.RoleSuperAdministrator), &campaignID, q, nil, "events")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	ev := step4Events(t, buf.String(), "Campaign.ExportEvents")
	if len(ev) != 2 {
		t.Fatalf("expected 2 export events, got %d: %v", len(ev), ev)
	}
	d0, _ := ev[0]["details"].(map[string]any)
	d1, _ := ev[1]["details"].(map[string]any)
	if ev[0]["result"] != "denied" || d0["kind"] != "submissions" || d0["campaignId"] != campaignID.String() {
		t.Errorf("denied export: %v", ev[0])
	}
	if ev[1]["result"] != "success" || d1["kind"] != "events" || d1["rows"] != float64(len(res.Rows)) {
		t.Errorf("export: %v", ev[1])
	}
	// the read underneath stays unaudited on success
	if got := step4Events(t, buf.String(), "Campaign.GetEventsByCampaignID"); len(got) != 0 {
		t.Fatalf("the plain read should write nothing on success: %v", got)
	}
}

func TestAuthorizeExportAudit(t *testing.T) {
	auditLogger, buf := bufferLogger(zapcore.InfoLevel)
	common := Common{Logger: auditLogger}
	company := &Company{Common: common}
	recipient := &Recipient{Common: common}
	admin := adminSession(uuid.New(), data.RoleSuperAdministrator)
	other := adminSession(uuid.New(), data.RoleCompanyUser)
	companyID := uuid.New()
	recipientID := uuid.New()

	if err := company.AuthorizeExport(admin, &companyID); err != nil {
		t.Fatal(err)
	}
	if err := company.AuthorizeExport(admin, nil); err != nil {
		t.Fatal(err)
	}
	if err := recipient.AuthorizeExport(other, &recipientID); err == nil {
		t.Fatal("expected a role without permissions to be refused")
	}

	ce := step4Events(t, buf.String(), "Company.Export")
	if len(ce) != 2 || ce[0]["result"] != "success" || ce[1]["result"] != "success" {
		t.Fatalf("company export events: %v", ce)
	}
	if d, _ := ce[0]["details"].(map[string]any); d["companyId"] != companyID.String() {
		t.Errorf("company export should name the company: %v", ce[0])
	}
	if d, _ := ce[1]["details"].(map[string]any); d["shared"] != true {
		t.Errorf("shared export should say so: %v", ce[1])
	}
	re := step4Events(t, buf.String(), "Recipient.Export")
	if len(re) != 1 || re[0]["result"] != "denied" {
		t.Fatalf("recipient export events: %v", re)
	}
}

func TestLiveSessionAudit(t *testing.T) {
	auditLogger, buf := bufferLogger(zapcore.InfoLevel)
	rb := &RemoteBrowser{Common: Common{Logger: auditLogger}}
	admin := adminSession(uuid.New(), data.RoleSuperAdministrator)
	other := adminSession(uuid.New(), data.RoleCompanyUser)

	// an allowed list is polled by the page and writes nothing
	if err := rb.AuthorizeLiveSession(admin, "ListLiveSessions"); err != nil {
		t.Fatal(err)
	}
	if err := rb.AuthorizeLiveSession(other, "WatchLiveSession"); err == nil {
		t.Fatal("expected a role without permissions to be refused")
	}
	rb.AuditLiveSession(admin, "WatchLiveSession", "cr-1", "camp-1", "control")
	rb.AuditLiveSession(admin, "CloseLiveSession", "cr-1", "", "")

	if got := step4Events(t, buf.String(), "RemoteBrowser.ListLiveSessions"); len(got) != 0 {
		t.Fatalf("an allowed list must write nothing: %v", got)
	}
	watch := step4Events(t, buf.String(), "RemoteBrowser.WatchLiveSession")
	if len(watch) != 2 || watch[0]["result"] != "denied" || watch[1]["result"] != "success" {
		t.Fatalf("watch events: %v", watch)
	}
	d, _ := watch[1]["details"].(map[string]any)
	if d["campaignRecipientId"] != "cr-1" || d["campaignId"] != "camp-1" || d["mode"] != "control" {
		t.Fatalf("watch details: %v", d)
	}
	cl := step4Events(t, buf.String(), "RemoteBrowser.CloseLiveSession")
	if len(cl) != 1 || cl[0]["result"] != "success" {
		t.Fatalf("close events: %v", cl)
	}
}
