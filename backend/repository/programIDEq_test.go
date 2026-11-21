package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"gorm.io/gorm/clause"
)

// TestProgramIDEqNilMatchesNothing checks a nil program id matches no row, so
// it can never select the campaigns that belong to no program
func TestProgramIDEqNilMatchesNothing(t *testing.T) {
	db := newProgramTestDB(t)
	now := time.Now().UTC()
	id := uuid.New()
	if err := db.Create(&database.Campaign{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "normal"}).Error; err != nil {
		t.Fatalf("create campaign: %v", err)
	}
	var n int64
	if err := db.Model(&database.Campaign{}).
		Where(programIDEq(clause.Column{Name: "program_id"}, nil)).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("a nil program id matched %d campaigns", n)
	}
	ids, err := (&Program{DB: db}).GetCampaignIDs(t.Context(), nil)
	if err != nil || len(ids) != 0 {
		t.Fatalf("campaigns of a nil program: %v %v", ids, err)
	}
}
