package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestWebhookNamesProgramSends checks that a webhook for a program send names
// the program and never the hidden campaign, and that a plain campaign is sent
// as before
func TestWebhookNamesProgramSends(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "webhook.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Campaign{}, &database.CampaignWebhook{}, &database.Webhook{}, &database.Program{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	received := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		payload := map[string]any{}
		_ = json.Unmarshal(body, &payload)
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	now := time.Now().UTC()
	webhookID := uuid.New()
	if err := db.Create(&database.Webhook{ID: &webhookID, CreatedAt: &now, UpdatedAt: &now, Name: "hook", URL: srv.URL}).Error; err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	programID := uuid.New()
	if err := db.Create(&database.Program{ID: &programID, CreatedAt: &now, UpdatedAt: &now, Name: "Quarterly program"}).Error; err != nil {
		t.Fatalf("create program: %v", err)
	}
	hiddenID, plainID := uuid.New(), uuid.New()
	for _, c := range []database.Campaign{
		{ID: &hiddenID, CreatedAt: &now, UpdatedAt: &now, Name: "__program:" + uuid.NewString() + ":0:abcd1234", ProgramID: &programID},
		{ID: &plainID, CreatedAt: &now, UpdatedAt: &now, Name: "Plain campaign"},
	} {
		row := c
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("create campaign: %v", err)
		}
		if err := db.Create(&database.CampaignWebhook{CampaignID: row.ID, WebhookID: &webhookID, WebhookIncludeData: "basic"}).Error; err != nil {
			t.Fatalf("link webhook: %v", err)
		}
	}

	log := zap.NewNop().Sugar()
	svc := &Campaign{
		Common:             Common{Logger: log},
		CampaignRepository: &repository.Campaign{DB: db},
		WebhookRepository:  &repository.Webhook{DB: db},
		WebhookService:     &Webhook{Common: Common{Logger: log}},
	}
	wait := func() map[string]any {
		t.Helper()
		select {
		case p := <-received:
			return p
		case <-time.After(5 * time.Second):
			t.Fatal("no webhook received")
		}
		return nil
	}

	if err := svc.HandleWebhooks(context.Background(), &hiddenID, nil, data.EVENT_CAMPAIGN_CLOSED, nil); err != nil {
		t.Fatalf("program webhook: %v", err)
	}
	p := wait()
	if p["campaignName"] != "Quarterly program" || p["programName"] != "Quarterly program" || p["programId"] != programID.String() {
		t.Fatalf("program payload = %v", p)
	}

	if err := svc.HandleWebhooks(context.Background(), &plainID, nil, data.EVENT_CAMPAIGN_CLOSED, nil); err != nil {
		t.Fatalf("plain webhook: %v", err)
	}
	p = wait()
	if p["campaignName"] != "Plain campaign" {
		t.Fatalf("plain payload = %v", p)
	}
	if _, has := p["programName"]; has {
		t.Fatalf("plain payload carries a program name: %v", p)
	}
	if _, has := p["programId"]; has {
		t.Fatalf("plain payload carries a program id: %v", p)
	}
}
