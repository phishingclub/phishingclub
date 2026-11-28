//go:build dev

package seed

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phishingclub/phishingclub/app"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestDevSeedFreshDatabase runs the full install and development seed on an
// empty database and checks the multilingual template and the recipient
// countries it adds
func TestDevSeedFreshDatabase(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "seed.db") + "?_foreign_keys=ON"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	repos := app.NewRepositories(db)
	log := zap.NewNop().Sugar()
	if err := initialInstallAndSeed(db, repos, log, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := RunSeedDevelopmentData(repos, db, log); err != nil {
		t.Fatalf("dev seed: %v", err)
	}
	// a second run is a no op
	if err := RunSeedDevelopmentData(repos, db, log); err != nil {
		t.Fatalf("dev seed again: %v", err)
	}

	ctx := context.Background()
	tmpl, err := repos.CampaignTemplate.GetByNameAndCompanyID(ctx, TEST_TEMPLATE_NAME_MULTILINGUAL, nil, &repository.CampaignTemplateOption{})
	if err != nil {
		t.Fatalf("multilingual template: %v", err)
	}
	if tmpl.TemplateLanguage() != "en" {
		t.Fatalf("template language %q, want en", tmpl.TemplateLanguage())
	}
	if usable, _ := tmpl.IsUsable.Get(); !usable {
		t.Fatalf("multilingual template is not usable")
	}
	id := tmpl.ID.MustGet()
	languages, err := repos.CampaignTemplate.GetLanguagesByTemplateID(ctx, &id, true)
	if err != nil {
		t.Fatalf("languages: %v", err)
	}
	if len(languages) != 1 || languages[0].Language != "de" || !languages[0].IsUsable {
		t.Fatalf("want one usable de row, got %+v", languages)
	}
	if languages[0].EmailName != TEST_EMAIL_NAME_1_DE || languages[0].LandingPageName != TEST_PAGE_NAME_1_DE {
		t.Fatalf("de row content %q / %q", languages[0].EmailName, languages[0].LandingPageName)
	}

	var countries []string
	if err := db.Model(&database.Recipient{}).
		Where("email IN ?", []string{TEST_RECIPIENT_EMAIL_1, TEST_RECIPIENT_EMAIL_2, TEST_RECIPIENT_EMAIL_3, TEST_RECIPIENT_EMAIL_4}).
		Order("email").
		Pluck("country", &countries).Error; err != nil {
		t.Fatalf("countries: %v", err)
	}
	want := []string{"Germany", "Germany", "Denmark", "Denmark"} // alice, bob, mallory, vicky
	if len(countries) != len(want) {
		t.Fatalf("countries %v", countries)
	}
	for i := range want {
		if countries[i] != want[i] {
			t.Fatalf("countries %v, want %v", countries, want)
		}
	}

	var languageCodes []string
	if err := db.Model(&database.Recipient{}).
		Where("email IN ?", []string{TEST_RECIPIENT_EMAIL_1, TEST_RECIPIENT_EMAIL_2, TEST_RECIPIENT_EMAIL_3, TEST_RECIPIENT_EMAIL_4}).
		Order("email").
		Pluck("language", &languageCodes).Error; err != nil {
		t.Fatalf("languages: %v", err)
	}
	if strings.Join(languageCodes, ",") != "de,de,da,da" {
		t.Fatalf("languages %v, want de,de,da,da", languageCodes)
	}
}
