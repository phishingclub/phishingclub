package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// TestTemplateTrainingChangeAllowedInProgram checks a template may become
// training while it is the only template of an open program that is not
// training, since a program may hold only training templates, and that the
// kind is kept while the program has started and is not closed
func TestTemplateTrainingChangeAllowedInProgram(t *testing.T) {
	f := newPoolFixture(t)
	if err := f.db.AutoMigrate(&database.Program{}, &database.ProgramStep{}, &database.Page{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tmplSvc := f.svc.CampaignTemplateService
	tmplSvc.ProgramRepository = &repository.Program{DB: f.db}

	id := uuid.New()
	if err := f.db.Create(&database.Program{ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "Lonely program"}).Error; err != nil {
		t.Fatalf("create program: %v", err)
	}
	only := f.template(templateOpts{})
	training := f.template(templateOpts{training: true})
	for i, templateID := range []uuid.UUID{only, training} {
		stepID, tid := uuid.New(), templateID
		if err := f.db.Create(&database.ProgramStep{
			ID: &stepID, CreatedAt: &f.now, UpdatedAt: &f.now,
			ProgramID: &id, CampaignTemplateID: &tid, Position: i,
		}).Error; err != nil {
			t.Fatalf("create step: %v", err)
		}
	}
	// a training template needs a landing page and an after landing page,
	// so the template gets both and the update goes through
	pages := map[string]uuid.UUID{}
	for _, column := range []string{"landing_page_id", "after_landing_page_id"} {
		pageID := uuid.New()
		if err := f.db.Create(&database.Page{ID: &pageID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: column, Content: "page"}).Error; err != nil {
			t.Fatalf("create page: %v", err)
		}
		if err := f.db.Model(&database.CampaignTemplate{}).Where("id = ?", only).Update(column, pageID).Error; err != nil {
			t.Fatalf("set %s: %v", column, err)
		}
		pages[column] = pageID
	}
	err := tmplSvc.UpdateByID(f.ctx, f.session, &only, &model.CampaignTemplate{
		IsTraining: nullable.NewNullableWithValue(true),
	})
	if err != nil {
		t.Fatalf("a template of an open program must be allowed to become training: %v", err)
	}
	var stored database.CampaignTemplate
	if err := f.db.Where("id = ?", only).First(&stored).Error; err != nil {
		t.Fatalf("load template: %v", err)
	}
	if !stored.IsTraining {
		t.Fatal("the template must now be training")
	}
	if stored.LandingPageID == nil || *stored.LandingPageID != pages["landing_page_id"] ||
		stored.AfterLandingPageID == nil || *stored.AfterLandingPageID != pages["after_landing_page_id"] {
		t.Fatal("the update must keep the pages of the template")
	}

	// once the program has started its campaigns hold the settings they were
	// made with, so the template kind can not change until it is closed
	if err := f.db.Model(&database.Program{}).Where("id = ?", id).Update("started_at", f.now).Error; err != nil {
		t.Fatalf("start program: %v", err)
	}
	err = tmplSvc.UpdateByID(f.ctx, f.session, &only, &model.CampaignTemplate{
		IsTraining: nullable.NewNullableWithValue(false),
	})
	if err == nil {
		t.Fatal("the kind of a template of a started program must not change")
	}
	if err := f.db.Model(&database.Program{}).Where("id = ?", id).Update("closed_at", f.now).Error; err != nil {
		t.Fatalf("close program: %v", err)
	}
	err = tmplSvc.UpdateByID(f.ctx, f.session, &only, &model.CampaignTemplate{
		IsTraining: nullable.NewNullableWithValue(false),
	})
	if err != nil {
		t.Fatalf("the kind of a template of a closed program may change: %v", err)
	}
}
