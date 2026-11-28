package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func newLanguageRouteDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "routes.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Campaign{},
		&database.CampaignTemplatePool{},
		&database.CampaignLanguageRoute{},
		&database.CampaignRecipientGroup{},
		&database.CampaignAllowDeny{},
		&database.CampaignEvent{},
		&database.CampaignStats{},
		&database.CampaignRecipient{},
		&database.Program{},
		&database.ProgramStep{},
		&database.ProgramRecipientGroup{},
		&database.ProgramEnrollment{},
		&database.ProgramCampaign{},
		&database.ProgramAllowDeny{},
		&database.ProgramWebhook{},
		&database.ProgramLanguageRoute{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func routedCampaign(name string, routes map[string]string) *model.Campaign {
	group := uuid.New()
	c := &model.Campaign{
		Name:              nullable.NewNullableWithValue(*vo.NewString64Must(name)),
		SortField:         nullable.NewNullableWithValue(*vo.NewCampaignSortFieldMust("email")),
		SortOrder:         nullable.NewNullableWithValue(*vo.NewCampaignSendingOrderMust("asc")),
		RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{&group}),
		LanguageRouteBy:   nullable.NewNullableWithValue("country"),
		LanguageDefault:   nullable.NewNullableWithValue("en"),
	}
	if routes != nil {
		c.LanguageRoutes = nullable.NewNullableWithValue(routes)
	}
	return c
}

func countRoutes(t *testing.T, db *gorm.DB, model any, column string, id *uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := db.Model(model).Where(clause.Eq{Column: clause.Column{Name: column}, Value: id.String()}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestCampaignLanguageRoutesStored checks the campaign language fields and
// routes are written with the campaign, replaced as a whole, kept when left
// out and deleted with the campaign
func TestCampaignLanguageRoutesStored(t *testing.T) {
	db := newLanguageRouteDB(t)
	ctx := context.Background()
	repo := &Campaign{DB: db}

	id, err := repo.Insert(ctx, routedCampaign("routed", map[string]string{"denmark": "da", "germany": "de"}))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := repo.GetByID(ctx, id, &CampaignOption{WithLanguageRoutes: true})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LanguageRouteField() != "country" || got.LanguageDefault.MustGet() != "en" {
		t.Fatalf("fields: %q %q", got.LanguageRouteField(), got.LanguageDefault.MustGet())
	}
	routes := got.LanguageRoutes.MustGet()
	if len(routes) != 2 || routes["denmark"] != "da" || routes["germany"] != "de" {
		t.Fatalf("routes: %v", routes)
	}
	// without the option the routes are not read, so a write back keeps them
	plain, err := repo.GetByID(ctx, id, &CampaignOption{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.LanguageRoutes.IsSpecified() {
		t.Fatal("routes read without the option")
	}
	if err := repo.UpdateByID(ctx, id, plain); err != nil {
		t.Fatalf("update: %v", err)
	}
	if n := countRoutes(t, db, &database.CampaignLanguageRoute{}, "campaign_id", id); n != 2 {
		t.Fatalf("routes after an update without them: %d", n)
	}
	// given routes replace the stored ones
	plain.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{"france": "fr"})
	if err := repo.UpdateByID(ctx, id, plain); err != nil {
		t.Fatalf("update routes: %v", err)
	}
	stored, _ := repo.GetLanguageRoutesByCampaignID(ctx, id)
	if len(stored) != 1 || stored["france"] != "fr" {
		t.Fatalf("replaced routes: %v", stored)
	}
	// a second campaign keeps its own routes
	other, err := repo.Insert(ctx, routedCampaign("other", map[string]string{"spain": "es"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteByID(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := countRoutes(t, db, &database.CampaignLanguageRoute{}, "campaign_id", id); n != 0 {
		t.Fatalf("routes left after delete: %d", n)
	}
	if n := countRoutes(t, db, &database.CampaignLanguageRoute{}, "campaign_id", other); n != 1 {
		t.Fatalf("other campaign lost its routes: %d", n)
	}
}

// TestCampaignLanguageRoutesBatches checks 500 routes are written, more than
// one insert batch
func TestCampaignLanguageRoutesBatches(t *testing.T) {
	db := newLanguageRouteDB(t)
	ctx := context.Background()
	repo := &Campaign{DB: db}
	routes := map[string]string{}
	for i := 0; i < model.MaxLanguageRoutes; i++ {
		routes[uuid.NewString()] = "de"
	}
	id, err := repo.Insert(ctx, routedCampaign("many", routes))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if n := countRoutes(t, db, &database.CampaignLanguageRoute{}, "campaign_id", id); n != int64(model.MaxLanguageRoutes) {
		t.Fatalf("stored %d routes", n)
	}
}

// TestProgramLanguageRoutesStored checks the program language fields and
// routes are written in the program transaction and deleted with it
func TestProgramLanguageRoutesStored(t *testing.T) {
	db := newLanguageRouteDB(t)
	ctx := context.Background()
	repo := &Program{DB: db}
	p := &model.Program{
		Name:            nullable.NewNullableWithValue(*vo.NewString64Must("routed program")),
		LanguageRouteBy: nullable.NewNullableWithValue("department"),
		LanguageDefault: nullable.NewNullableWithValue("de"),
	}
	id, err := repo.Insert(ctx, p, &ProgramChildren{LanguageRoutes: map[string]string{"sales": "fr"}})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := repo.GetByID(ctx, id, &ProgramOption{WithLanguageRoutes: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.LanguageRouteField() != "department" || got.LanguageDefault.MustGet() != "de" || got.LanguageRoutes.MustGet()["sales"] != "fr" {
		t.Fatalf("program routing: %q %q %v", got.LanguageRouteField(), got.LanguageDefault.MustGet(), got.LanguageRoutes.MustGet())
	}
	// nil keeps, an empty map removes
	if err := repo.UpdateByID(ctx, id, &model.Program{}, &ProgramChildren{}, nil); err != nil {
		t.Fatal(err)
	}
	if n := countRoutes(t, db, &database.ProgramLanguageRoute{}, "program_id", id); n != 1 {
		t.Fatalf("routes after update without them: %d", n)
	}
	if err := repo.UpdateByID(ctx, id, &model.Program{}, &ProgramChildren{LanguageRoutes: map[string]string{}}, nil); err != nil {
		t.Fatal(err)
	}
	if n := countRoutes(t, db, &database.ProgramLanguageRoute{}, "program_id", id); n != 0 {
		t.Fatalf("routes after clearing: %d", n)
	}
	if err := repo.UpdateByID(ctx, id, &model.Program{}, &ProgramChildren{LanguageRoutes: map[string]string{"it": "it"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteByID(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := countRoutes(t, db, &database.ProgramLanguageRoute{}, "program_id", id); n != 0 {
		t.Fatalf("routes left after delete: %d", n)
	}
}
