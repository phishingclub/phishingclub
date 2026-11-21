package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
)

// TestPageDeleteRefusedWhileOpenUses checks a page delete is refused while a
// campaign or program that is not closed uses it as deny or evasion page, and
// the refusal names a few of them with the count of the rest, and that the
// filters of the open users are left as they were
func TestPageDeleteRefusedWhileOpenUses(t *testing.T) {
	p := newProgramServiceTest(t)
	pages := &Page{
		Common:             Common{Logger: p.svc.Logger},
		CampaignRepository: &repository.Campaign{DB: p.db},
		ProgramRepository:  p.svc.ProgramRepository,
	}
	pageID := uuid.New()
	now := time.Now().UTC()
	for i := 0; i < 4; i++ {
		id := uuid.New()
		p.create(&database.Campaign{
			ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: fmt.Sprintf("campaign %d", i),
			DenyPageID: &pageID,
		})
	}
	for i := 0; i < 3; i++ {
		id := uuid.New()
		p.create(&database.Program{
			ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: fmt.Sprintf("program %d", i),
			EvasionPageID: &pageID,
		})
	}
	err := pages.DeleteByID(p.ctx, authorizedSession(t), &pageID)
	if !isValidationError(err) {
		t.Fatalf("expected a validation error, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"campaign campaign 0", "campaign campaign 3", "program program 0", "and 2 more"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the refusal must name %q: %s", want, msg)
		}
	}
	var kept int64
	p.db.Model(&database.Campaign{}).Where("deny_page_id = ?", pageID).Count(&kept)
	if kept != 4 {
		t.Fatalf("the open campaigns must keep the page, %d do", kept)
	}
	p.db.Model(&database.Program{}).Where("evasion_page_id = ?", pageID).Count(&kept)
	if kept != 3 {
		t.Fatalf("the open programs must keep the page, %d do", kept)
	}

	// closed users do not refuse the delete
	p.db.Model(&database.Campaign{}).Where("deny_page_id = ?", pageID).Update("closed_at", now)
	p.db.Model(&database.Program{}).Where("evasion_page_id = ?", pageID).Update("closed_at", now)
	if err := pages.checkPageNotUsedByOpen(p.ctx, &pageID); err != nil {
		t.Fatalf("closed users must not refuse the delete: %v", err)
	}
}
