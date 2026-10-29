package repository

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newAllowDenyRepo(t *testing.T) *AllowDeny {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "allowdeny.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.AllowDeny{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &AllowDeny{DB: db}
}

func insertAllowDeny(t *testing.T, r *AllowDeny, name, cidr, countries string) {
	t.Helper()
	m := &model.AllowDeny{}
	m.Name = nullable.NewNullableWithValue(*vo.NewString127Must(name))
	m.Allowed = nullable.NewNullableWithValue(true)
	m.Cidrs = nullable.NewNullableWithValue(vo.IPNetSlice{*vo.NewIPNetMust(cidr)})
	m.CountryCodes = nullable.NewNullableWithValue(countries)
	if _, err := r.Insert(context.Background(), m); err != nil {
		t.Fatalf("insert %s: %v", name, err)
	}
}

// TestAllowDenyAllowColumnsExist checks that every allowlisted column is a real
// column, so a search or an order by any of them runs without a SQL error.
func TestAllowDenyAllowColumnsExist(t *testing.T) {
	r := newAllowDenyRepo(t)
	insertAllowDeny(t, r, "EU visitors", "10.0.0.0/8", "DK\nDE")
	insertAllowDeny(t, r, "Office", "192.168.0.0/16", "US")

	got, err := r.GetAll(context.Background(), nil, &AllowDenyOption{QueryArgs: &vo.QueryArgs{Search: "EU"}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got.Rows) != 1 || got.Rows[0].Name.MustGet().String() != "EU visitors" {
		t.Fatalf("search EU: want only EU visitors, got %d rows", len(got.Rows))
	}

	got, err = r.GetAll(context.Background(), nil, &AllowDenyOption{QueryArgs: &vo.QueryArgs{Search: "192.168"}})
	if err != nil {
		t.Fatalf("search cidr: %v", err)
	}
	if len(got.Rows) != 1 || got.Rows[0].Name.MustGet().String() != "Office" {
		t.Fatalf("search 192.168: want only Office, got %d rows", len(got.Rows))
	}

	for _, col := range allowDenyAllowColumns {
		// the allowlist holds `table`.`column`; a request sends the bare column.
		bare := strings.Trim(col[strings.LastIndex(col, ".")+1:], "`")
		if _, err := r.GetAll(context.Background(), nil, &AllowDenyOption{QueryArgs: &vo.QueryArgs{OrderBy: bare, Search: "x"}}); err != nil {
			t.Fatalf("order by %s: %v", bare, err)
		}
	}

	if _, err := r.GetAll(context.Background(), nil, &AllowDenyOption{QueryArgs: &vo.QueryArgs{OrderBy: "headers"}}); err == nil {
		t.Fatal("order by a column outside the allowlist must fail")
	}
}
