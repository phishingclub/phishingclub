package repository

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestPagingPeek checks that a list read with one extra row gives the right
// page and the right next page flag on a real database
func TestPagingPeek(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "peek.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Company{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	const total = 7
	now := time.Now().UTC()
	for i := 0; i < total; i++ {
		row := database.Company{
			ID:        uuid.New(),
			CreatedAt: &now,
			UpdatedAt: &now,
			Name:      fmt.Sprintf("company-%02d", i),
			AssetsKey: uuid.NewString(),
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	repo := &Company{DB: db}

	cases := []struct {
		offset, limit int
		wantRows      int
		wantNext      bool
		wantFirst     string
	}{
		{offset: 0, limit: 3, wantRows: 3, wantNext: true, wantFirst: "company-00"},
		{offset: 3, limit: 3, wantRows: 3, wantNext: true, wantFirst: "company-03"},
		// the page ends exactly at the last row
		{offset: total - 3, limit: 3, wantRows: 3, wantNext: false, wantFirst: "company-04"},
		{offset: total - 1, limit: 3, wantRows: 1, wantNext: false, wantFirst: "company-06"},
		{offset: total, limit: 3, wantRows: 0, wantNext: false},
		{offset: total + 5, limit: 3, wantRows: 0, wantNext: false},
		// a page that holds everything
		{offset: 0, limit: total, wantRows: total, wantNext: false, wantFirst: "company-00"},
		{offset: 0, limit: total - 1, wantRows: total - 1, wantNext: true, wantFirst: "company-00"},
		// no limit reads all rows
		{offset: 0, limit: 0, wantRows: total, wantNext: false, wantFirst: "company-00"},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("offset%d_limit%d", c.offset, c.limit), func(t *testing.T) {
			q := &vo.QueryArgs{Offset: c.offset, Limit: c.limit, OrderBy: "name"}
			res, err := repo.GetAll(context.Background(), q)
			if err != nil {
				t.Fatalf("get all: %v", err)
			}
			if len(res.Rows) != c.wantRows {
				t.Fatalf("rows = %d, want %d", len(res.Rows), c.wantRows)
			}
			if res.HasNextPage != c.wantNext {
				t.Fatalf("has next page = %v, want %v", res.HasNextPage, c.wantNext)
			}
			if c.wantFirst != "" {
				got := res.Rows[0].Name.MustGet().String()
				if got != c.wantFirst {
					t.Fatalf("first row = %s, want %s", got, c.wantFirst)
				}
			}
		})
	}

	// nil query args reads everything with no next page
	res, err := repo.GetAll(context.Background(), nil)
	if err != nil {
		t.Fatalf("get all nil: %v", err)
	}
	if len(res.Rows) != total || res.HasNextPage {
		t.Fatalf("nil args: rows = %d next = %v", len(res.Rows), res.HasNextPage)
	}
}
