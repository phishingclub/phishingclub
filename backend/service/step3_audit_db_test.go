package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOAuthExchangeAudit(t *testing.T) {
	f := newDomainFixture(t)
	logger, buf := bufferLogger(zapcore.InfoLevel)
	f.oauth.AuditLogger = logger
	if err := f.db.AutoMigrate(&database.OAuthProvider{}); err != nil {
		t.Fatal(err)
	}
	f.oauth.OAuthProviderRepository = &repository.OAuthProvider{DB: f.db}
	repo := f.oauth.OAuthStateRepository
	providerID := uuid.New()
	insert := func(token string, expires time.Time) uuid.UUID {
		id, err := repo.Insert(f.ctx, &model.OAuthState{
			StateToken:      nullable.NewNullableWithValue(*vo.NewString255Must(token)),
			OAuthProviderID: nullable.NewNullableWithValue(providerID),
			ExpiresAt:       &expires,
		})
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		return *id
	}
	ip := "203.0.113.31"

	// unknown state, no event
	if _, err := f.oauth.ExchangeCodeForTokens(f.ctx, nil, "no-such-state", "secret-code-1", "https://cb", ip); err == nil {
		t.Fatal("expected unknown state to fail")
	}
	// used state
	used := insert("used-state", time.Now().Add(time.Minute))
	if err := repo.MarkAsUsed(f.ctx, used); err != nil {
		t.Fatal(err)
	}
	_, _ = f.oauth.ExchangeCodeForTokens(f.ctx, nil, "used-state", "secret-code-2", "https://cb", ip)
	// expired state
	insert("expired-state", time.Now().Add(-time.Minute))
	_, _ = f.oauth.ExchangeCodeForTokens(f.ctx, nil, "expired-state", "secret-code-3", "https://cb", ip)
	// valid state, the provider is not stored so the exchange fails
	insert("good-state", time.Now().Add(time.Minute))
	if _, err := f.oauth.ExchangeCodeForTokens(f.ctx, nil, "good-state", "secret-code-4", "https://cb", ip); err == nil {
		t.Fatal("expected the exchange to fail without a provider")
	}

	var ev []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["name"] == "OAuthProvider.ExchangeCodeForTokens" {
			ev = append(ev, m)
		}
	}
	if len(ev) != 3 {
		t.Fatalf("expected 3 events (unknown state writes none), got %d: %v", len(ev), ev)
	}
	want := []struct{ result, reason string }{
		{"denied", "stateUsed"},
		{"denied", "stateExpired"},
		{"failed", ""},
	}
	for i, w := range want {
		d, _ := ev[i]["details"].(map[string]any)
		if ev[i]["result"] != w.result || ev[i]["ip"] != ip || d["id"] != providerID.String() ||
			(w.reason != "" && d["reason"] != w.reason) {
			t.Errorf("event %d: %v", i, ev[i])
		}
	}
	if strings.Contains(buf.String(), "secret-code") || strings.Contains(buf.String(), "good-state") {
		t.Fatal("an authorization code or state token reached the audit log")
	}
}

func TestManualCampaignStatsAudit(t *testing.T) {
	logger, buf := bufferLogger(zapcore.InfoLevel)
	c := &Campaign{Common: Common{Logger: logger}}
	other := adminSession(uuid.New(), data.RoleCompanyUser)
	id := uuid.New()
	if _, err := c.CreateManualCampaignStats(context.Background(), other, nil); err == nil {
		t.Fatal("expected a role without permissions to be refused")
	}
	if _, err := c.UpdateManualCampaignStats(context.Background(), other, &database.CampaignStats{ID: &id}); err == nil {
		t.Fatal("expected a role without permissions to be refused")
	}
	if err := c.DeleteManualCampaignStats(context.Background(), other, &id); err == nil {
		t.Fatal("expected a role without permissions to be refused")
	}
	out := buf.String()
	for _, name := range []string{"Campaign.CreateManualCampaignStats", "Campaign.UpdateManualCampaignStats", "Campaign.DeleteManualCampaignStats"} {
		if strings.Count(out, `"name":"`+name+`"`) != 1 {
			t.Errorf("%s: expected one denied event in %q", name, out)
		}
	}
	if strings.Count(out, `"result":"denied"`) != 3 {
		t.Fatalf("all three should be denied: %q", out)
	}
	if !strings.Contains(out, `"id":"`+id.String()+`"`) {
		t.Fatalf("update and delete should name the stats id: %q", out)
	}
}

func TestCampaignTemplateRemoveDomainAudit(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "tpl.db") + "?_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.CampaignTemplate{}, &database.Campaign{}, &database.CampaignTemplatePool{}); err != nil {
		t.Fatal(err)
	}
	auditLogger, buf := bufferLogger(zapcore.InfoLevel)
	svc := &CampaignTemplate{
		Common:                     Common{Logger: zap.NewNop().Sugar(), AuditLogger: auditLogger},
		CampaignTemplateRepository: &repository.CampaignTemplate{DB: db},
		CampaignRepository:         &repository.Campaign{DB: db},
	}
	session := adminSession(uuid.New(), data.RoleSuperAdministrator)
	domainID := uuid.New()
	if err := svc.RemoveDomainByDomainID(context.Background(), session, &domainID); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()
	if err := svc.RemoveDomainByDomainID(context.Background(), session, &domainID); err == nil {
		t.Fatal("expected a closed database to fail")
	}
	out := buf.String()
	if strings.Count(out, `"name":"CampaignTemplate.RemoveDomainByDomainID"`) != 2 ||
		!strings.Contains(out, `"result":"success"`) || !strings.Contains(out, `"result":"failed"`) {
		t.Fatalf("expected one success and one failed event: %q", out)
	}
}

// only the campaigns the removal actually closed are named in the event
func TestCampaignTemplateRemoveDomainNamesClosedCampaigns(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "tplc.db") + "?_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.CampaignTemplate{}, &database.Campaign{}, &database.CampaignTemplatePool{}); err != nil {
		t.Fatal(err)
	}
	auditLogger, buf := bufferLogger(zapcore.InfoLevel)
	svc := &CampaignTemplate{
		Common:                     Common{Logger: zap.NewNop().Sugar(), AuditLogger: auditLogger},
		CampaignTemplateRepository: &repository.CampaignTemplate{DB: db},
		CampaignRepository:         &repository.Campaign{DB: db},
	}
	ctx := context.Background()
	domainID := uuid.New()
	templateID := uuid.New()
	now := time.Now().UTC()
	if err := db.Model(&database.CampaignTemplate{}).Create(map[string]any{
		"id":                templateID,
		"created_at":        now,
		"updated_at":        now,
		"name":              "tpl",
		"domain_id":         domainID,
		"url_identifier_id": uuid.New(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	addCampaign := func(closedAt *time.Time) uuid.UUID {
		id := uuid.New()
		row := map[string]any{
			"id":                   id,
			"created_at":           now,
			"updated_at":           now,
			"name":                 "c " + id.String(),
			"sort_field":           "email",
			"sort_order":           "asc",
			"campaign_template_id": templateID.String(),
		}
		if closedAt != nil {
			row["closed_at"] = closedAt.Format(time.RFC3339)
		}
		if err := db.Model(&database.Campaign{}).Create(row).Error; err != nil {
			t.Fatal(err)
		}
		if err := svc.CampaignRepository.SetTemplatePool(ctx, &id, []uuid.UUID{templateID}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	active := addCampaign(nil)
	past := now.Add(-time.Hour)
	closed := addCampaign(&past)

	session := adminSession(uuid.New(), data.RoleSuperAdministrator)
	if err := svc.RemoveDomainByDomainID(ctx, session, &domainID); err != nil {
		t.Fatal(err)
	}
	var ev map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["name"] == "CampaignTemplate.RemoveDomainByDomainID" {
			ev = m
		}
	}
	d, _ := ev["details"].(map[string]any)
	ids, _ := d["closedCampaignIds"].([]any)
	if len(ids) != 1 || ids[0] != active.String() {
		t.Fatalf("expected only the active campaign %s, got %v (closed one was %s)", active, d["closedCampaignIds"], closed)
	}
	if _, has := d["closeFailedCampaignIds"]; has {
		t.Fatalf("no close failed: %v", d)
	}
}

func TestImportAudit(t *testing.T) {
	logger, buf := bufferLogger(zapcore.InfoLevel)
	im := &Import{Common: Common{Logger: logger}}
	g, _ := gin.CreateTestContext(httptest.NewRecorder())
	g.Request = httptest.NewRequest("POST", "/", nil)

	var zipBuf bytes.Buffer
	if err := zip.NewWriter(&zipBuf).Close(); err != nil {
		t.Fatal(err)
	}
	companyID := uuid.New()

	denied := adminSession(uuid.New(), data.RoleCompanyUser)
	if _, err := im.ImportFromBytes(g, denied, zipBuf.Bytes(), false, nil); err == nil {
		t.Fatal("expected a role without permissions to be refused")
	}
	admin := adminSession(uuid.New(), data.RoleSuperAdministrator)
	if _, err := im.ImportFromBytes(g, admin, zipBuf.Bytes(), true, &companyID); err != nil {
		t.Fatalf("expected an empty archive to import: %v", err)
	}
	if _, err := im.ImportFromBytes(g, admin, []byte("not a zip"), false, nil); err == nil {
		t.Fatal("expected a broken archive to fail")
	}

	var ev []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["name"] == "Import.Import" {
			ev = append(ev, m)
		}
	}
	if len(ev) != 3 {
		t.Fatalf("expected 3 import events, got %d: %v", len(ev), ev)
	}
	results := []string{"denied", "success", "failed"}
	for i, r := range results {
		d, _ := ev[i]["details"].(map[string]any)
		if ev[i]["result"] != r || d["source"] != "templates" {
			t.Errorf("event %d: %v", i, ev[i])
		}
	}
	d, _ := ev[1]["details"].(map[string]any)
	if d["companyId"] != companyID.String() || d["created"] != float64(0) || d["errors"] != float64(0) {
		t.Fatalf("success event should hold company and counts: %v", d)
	}
}
