package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/repository"
)

// withPrograms wires the program tables and repository into a registry fixture
func withPrograms(t *testing.T, h *ownershipFixture) {
	t.Helper()
	if err := h.db.AutoMigrate(
		&database.InstalledPackageMember{},
		&database.Program{},
		&database.ProgramLanguageRoute{},
		&database.ProgramStep{},
		&database.ProgramAllowDeny{},
		&database.ProgramWebhook{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h.s.CampaignTemplateRepository = &repository.CampaignTemplate{DB: h.db}
	h.s.CampaignTemplate = h.s.Page.CampaignTemplateService
	h.s.CampaignTemplate.ProgramRepository = &repository.Program{DB: h.db}
	h.s.Page.ProgramRepository = h.s.CampaignTemplate.ProgramRepository
}

// insertProgram adds a program with one step on the template and its hidden
// campaign, and returns the program id
func insertProgram(t *testing.T, h *ownershipFixture, name string, closed bool, templateID, denyPageID *uuid.UUID) uuid.UUID {
	t.Helper()
	now := time.Now()
	id, stepID, campaignID := uuid.New(), uuid.New(), uuid.New()
	row := &database.Program{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: name, DenyPageID: denyPageID}
	if closed {
		row.ClosedAt = &now
	}
	if err := h.db.Create(row).Error; err != nil {
		t.Fatalf("insert program: %v", err)
	}
	if templateID != nil {
		if err := h.db.Create(&database.ProgramStep{
			ID: &stepID, CreatedAt: &now, UpdatedAt: &now, ProgramID: &id, CampaignTemplateID: templateID,
		}).Error; err != nil {
			t.Fatalf("insert step: %v", err)
		}
	}
	if err := h.db.Create(&database.Campaign{
		ID: &campaignID, CreatedAt: &now, UpdatedAt: &now,
		Name:               "__program:" + stepID.String() + ":0:abcd1234",
		CampaignTemplateID: templateID, DenyPageID: denyPageID, ProgramID: &id,
	}).Error; err != nil {
		t.Fatalf("insert hidden campaign: %v", err)
	}
	return id
}

// TestUninstallBlockedByOpenProgramTemplate checks the preview names the open
// program that uses a template the uninstall deletes, never its hidden
// campaign, that the uninstall is refused up front, and that a closed program
// does not block
func TestUninstallBlockedByOpenProgramTemplate(t *testing.T) {
	h := newOwnershipFixture(t)
	withPrograms(t, h)
	now := time.Now()
	tplID, identID := uuid.New(), uuid.New()
	if err := h.db.Create(&database.CampaignTemplate{
		ID: &tplID, CreatedAt: &now, UpdatedAt: &now, Name: "Pkg tpl",
		URLIdentifierID: &identID, StateIdentifierID: &identID,
	}).Error; err != nil {
		t.Fatalf("insert template: %v", err)
	}
	ipID := h.newInstall("acme/tpl")
	if err := h.s.recordResource(h.ctx, ipID, "campaigntemplate", "Pkg tpl", &tplID, "x", "{}"); err != nil {
		t.Fatalf("record resource: %v", err)
	}
	openID := insertProgram(t, h, "Quarterly", false, &tplID, nil)
	insertProgram(t, h, "Archived", true, &tplID, nil)

	preview, err := h.s.UninstallPreview(h.ctx, h.session, ipID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	for _, c := range preview.Campaigns {
		if strings.HasPrefix(c.Name, "__program:") {
			t.Fatalf("a hidden program campaign was listed: %+v", c)
		}
	}
	if len(preview.Programs) != 1 || preview.Programs[0].Name != "Quarterly" || !preview.Programs[0].Blocks ||
		preview.Programs[0].ID != openID.String() || preview.ProgramCount != 1 || !preview.ProgramsBlock {
		t.Fatalf("expected only the open program, blocking, got %+v", preview.Programs)
	}

	err = h.s.Remove(h.ctx, h.session, ipID)
	var validation errs.ValidationError
	if !errors.As(err, &validation) || !strings.Contains(err.Error(), "Quarterly") {
		t.Fatalf("expected a refusal naming the program, got %v", err)
	}
	var count int64
	h.db.Model(&database.CampaignTemplate{}).Where("id = ?", tplID.String()).Count(&count)
	if count != 1 {
		t.Fatal("a refused uninstall must delete nothing")
	}

	// once the program is closed the uninstall goes through
	if err := h.db.Model(&database.Program{}).Where("id = ?", openID.String()).Update("closed_at", now).Error; err != nil {
		t.Fatalf("close program: %v", err)
	}
	if err := h.s.Remove(h.ctx, h.session, ipID); err != nil {
		t.Fatalf("remove after close: %v", err)
	}
}

// TestUninstallPreviewNamesProgramUsingPage checks a page an open program uses
// as deny page is listed as blocked under the program, never its hidden
// campaign, and that remove keeps it. Once the program is closed the page is
// removed.
func TestUninstallPreviewNamesProgramUsingPage(t *testing.T) {
	h := newOwnershipFixture(t)
	withPrograms(t, h)
	ip, pageID := installLoginPage(t, h)
	programID := insertProgram(t, h, "Always on", false, nil, pageID)

	preview, err := h.s.UninstallPreview(h.ctx, h.session, ip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.ChangedCount != 0 || preview.ProgramCount != 0 {
		t.Fatalf("a page that stays changes nothing, got changed %+v programs %+v", preview.Changed, preview.Programs)
	}
	if len(preview.Blocked) != 1 || strings.Join(preview.Blocked[0].UsedBy, ",") != "program Always on" {
		t.Fatalf("expected the page blocked by program Always on, got %+v", preview.Blocked)
	}
	if err := h.s.Remove(h.ctx, h.session, ip.ID); err == nil || !strings.Contains(err.Error(), "program Always on") {
		t.Fatalf("expected remove to keep the page used by the program, got %v", err)
	}
	if _, ok := h.pageIDByName("Login"); !ok {
		t.Fatalf("the Login page should stay installed")
	}

	if err := h.db.Model(&database.Program{}).Where("id = ?", programID.String()).
		Update("closed_at", time.Now()).Error; err != nil {
		t.Fatalf("close program: %v", err)
	}
	preview, err = h.s.UninstallPreview(h.ctx, h.session, ip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview.Blocked) != 0 || preview.ProgramCount != 0 {
		t.Fatalf("a closed program neither holds the page nor is listed, got %+v %+v", preview.Blocked, preview.Programs)
	}
	if err := h.s.Remove(h.ctx, h.session, ip.ID); err != nil {
		t.Fatalf("remove after close: %v", err)
	}
	if _, ok := h.pageIDByName("Login"); ok {
		t.Fatalf("the Login page should be removed")
	}
}
