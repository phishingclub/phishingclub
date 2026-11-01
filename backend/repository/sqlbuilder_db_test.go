package repository

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type capturedQuery struct {
	sql  string
	vars []interface{}
}

// newBuilderDB opens a temp SQLite db and records the SQL and vars of every query
func newBuilderDB(t *testing.T, models ...interface{}) (*gorm.DB, *[]capturedQuery) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "builder.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	captured := &[]capturedQuery{}
	err = db.Callback().Query().After("gorm:query").Register("test:capture", func(tx *gorm.DB) {
		*captured = append(*captured, capturedQuery{
			sql:  tx.Statement.SQL.String(),
			vars: append([]interface{}{}, tx.Statement.Vars...),
		})
	})
	if err != nil {
		t.Fatalf("register callback: %v", err)
	}
	return db, captured
}

func lastQuery(t *testing.T, captured *[]capturedQuery) capturedQuery {
	t.Helper()
	if len(*captured) == 0 {
		t.Fatal("no query captured")
	}
	return (*captured)[len(*captured)-1]
}

// checkPlaceholders fails when the number of ? differs from the bound vars
func checkPlaceholders(t *testing.T, q capturedQuery) {
	t.Helper()
	if n := strings.Count(q.sql, "?"); n != len(q.vars) {
		t.Fatalf("placeholders %d != vars %d in %q (%v)", n, len(q.vars), q.sql, q.vars)
	}
}

func insertScript(t *testing.T, db *gorm.DB, name, body string, companyID *uuid.UUID) {
	t.Helper()
	id := uuid.New()
	now := time.Now()
	row := database.Script{ID: &id, CreatedAt: &now, UpdatedAt: &now, CompanyID: companyID, Name: name, Script: body}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("insert script %s: %v", name, err)
	}
}

// TestCompanyHelpersSQL checks the SQL shape of the company scope helpers
func TestCompanyHelpersSQL(t *testing.T) {
	db, _ := newBuilderDB(t, &database.Script{})
	companyID := uuid.New()
	dry := db.Session(&gorm.Session{DryRun: true})
	build := func(fn func(*gorm.DB) *gorm.DB) capturedQuery {
		var rows []database.Script
		stmt := fn(dry.Model(&database.Script{})).Find(&rows).Statement
		return capturedQuery{sql: stmt.SQL.String(), vars: stmt.Vars}
	}
	cases := []struct {
		name string
		fn   func(*gorm.DB) *gorm.DB
		want string
	}{
		{"including null with company", func(d *gorm.DB) *gorm.DB {
			return withCompanyIncludingNullContext(d, &companyID, database.SCRIPT_TABLE)
		}, "WHERE (`scripts`.`company_id` = ? OR `scripts`.`company_id` IS NULL)"},
		{"including null without company", func(d *gorm.DB) *gorm.DB {
			return withCompanyIncludingNullContext(d, nil, database.SCRIPT_TABLE)
		}, "WHERE `scripts`.`company_id` IS NULL"},
		{"where company", func(d *gorm.DB) *gorm.DB {
			return whereCompany(d, database.SCRIPT_TABLE, &companyID)
		}, "WHERE `scripts`.`company_id` = ?"},
		{"where company nil", func(d *gorm.DB) *gorm.DB {
			return whereCompany(d, database.SCRIPT_TABLE, nil)
		}, "WHERE `scripts`.`company_id` IS NULL"},
		{"where company is null", func(d *gorm.DB) *gorm.DB {
			return whereCompanyIsNull(d, database.SCRIPT_TABLE)
		}, "WHERE `scripts`.`company_id` IS NULL"},
		{"including null joined with and", func(d *gorm.DB) *gorm.DB {
			return withCompanyIncludingNullContext(d.Where("1 = 1"), &companyID, database.SCRIPT_TABLE)
		}, "WHERE 1 = 1 AND (`scripts`.`company_id` = ? OR `scripts`.`company_id` IS NULL)"},
	}
	for _, c := range cases {
		q := build(c.fn)
		if !strings.Contains(q.sql, c.want) {
			t.Fatalf("%s: want %q in %q", c.name, c.want, q.sql)
		}
		checkPlaceholders(t, q)
	}
}

// TestCompanyHelpersRows checks the company helpers return a company's rows plus
// the global rows, only the global rows for no company, and only the rows with
// no company for the is null helper
func TestCompanyHelpersRows(t *testing.T) {
	db, _ := newBuilderDB(t, &database.Script{})
	a, b := uuid.New(), uuid.New()
	insertScript(t, db, "global", "g", nil)
	insertScript(t, db, "a1", "a", &a)
	insertScript(t, db, "b1", "b", &b)

	count := func(d *gorm.DB) int64 {
		var n int64
		if err := d.Model(&database.Script{}).Count(&n).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if n := count(withCompanyIncludingNullContext(db, &a, database.SCRIPT_TABLE)); n != 2 {
		t.Fatalf("company a plus global: want 2, got %d", n)
	}
	if n := count(withCompanyIncludingNullContext(db, nil, database.SCRIPT_TABLE)); n != 1 {
		t.Fatalf("global only: want 1, got %d", n)
	}
	if n := count(whereCompany(db, database.SCRIPT_TABLE, &b)); n != 1 {
		t.Fatalf("company b: want 1, got %d", n)
	}
	if n := count(whereCompany(db, database.SCRIPT_TABLE, nil)); n != 1 {
		t.Fatalf("company nil: want 1, got %d", n)
	}
	if n := count(whereCompanyIsNull(db, database.SCRIPT_TABLE)); n != 1 {
		t.Fatalf("company is null: want 1, got %d", n)
	}
}

// TestScriptGetByNameScoped checks the company script wins over a global one
func TestScriptGetByNameScoped(t *testing.T) {
	db, captured := newBuilderDB(t, &database.Script{})
	r := &Script{DB: db}
	a := uuid.New()
	insertScript(t, db, "shared", "global body", nil)
	insertScript(t, db, "shared", "company body", &a)
	insertScript(t, db, "only global", "only global body", nil)

	got, err := r.GetByNameScoped(context.Background(), "shared", &a)
	if err != nil {
		t.Fatalf("company lookup: %v", err)
	}
	if got.CompanyID.IsNull() {
		t.Fatal("company lookup: want the company script, got the global one")
	}
	q := lastQuery(t, captured)
	checkPlaceholders(t, q)
	if !strings.Contains(q.sql, "ORDER BY `scripts`.`company_id` IS NULL, `scripts`.`id` LIMIT 1") {
		t.Fatalf("missing company order in %q", q.sql)
	}

	got, err = r.GetByNameScoped(context.Background(), "shared", nil)
	if err != nil {
		t.Fatalf("global lookup: %v", err)
	}
	if !got.CompanyID.IsNull() {
		t.Fatal("global lookup: want the global script")
	}
	if _, err := r.GetByNameScoped(context.Background(), "only global", &a); err != nil {
		t.Fatalf("global script must be visible to a company: %v", err)
	}
	b := uuid.New()
	if _, err := r.GetByNameScoped(context.Background(), "shared", &b); err != nil {
		t.Fatalf("other company sees the global script: %v", err)
	}
}

// TestNameLookupPlaceholders checks every name lookup binds one var per placeholder
// and is scoped to the right company, with and without a company id
func TestNameLookupPlaceholders(t *testing.T) {
	db, captured := newBuilderDB(t,
		&database.SMTPConfiguration{},
		&database.RecipientGroup{},
		&database.Email{},
		&database.Page{},
		&database.Proxy{},
		&database.APISender{},
		&database.RemoteBrowser{},
		&database.Domain{},
		&database.Identifier{},
		&database.Webhook{},
	)
	ctx := context.Background()
	companyID := uuid.New()
	n64 := vo.NewString64Must("missing")
	n127 := vo.NewString127Must("missing")
	n255 := vo.NewString255Must("missing")

	type lookup struct {
		name     string
		run      func(c *uuid.UUID) error
		scoped   bool
		wantWith string
		wantNil  string
	}
	lookups := []lookup{
		{"smtp", func(c *uuid.UUID) error {
			_, err := (&SMTPConfiguration{DB: db}).GetByNameAndCompanyID(ctx, n127, c, &SMTPConfigurationOption{})
			return err
		}, true, "`smtp_configurations`.`company_id` = ?", "`smtp_configurations`.`company_id` IS NULL"},
		{"recipient group", func(c *uuid.UUID) error {
			_, err := (&RecipientGroup{DB: db}).GetByNameAndCompanyID(ctx, "missing", c, &RecipientGroupOption{})
			return err
		}, true, "`recipient_groups`.`company_id` = ?", "`recipient_groups`.`company_id` IS NULL"},
		{"email", func(c *uuid.UUID) error {
			_, err := (&Email{DB: db}).GetByNameAndCompanyID(ctx, n64, c, &EmailOption{})
			return err
		}, true, "`emails`.`company_id` = ?", "`emails`.`company_id` IS NULL"},
		{"page", func(c *uuid.UUID) error {
			_, err := (&Page{DB: db}).GetByNameAndCompanyID(ctx, n64, c, &PageOption{})
			return err
		}, true, "(`pages`.`company_id` = ? OR `pages`.`company_id` IS NULL)", "`pages`.`company_id` IS NULL"},
		{"proxy", func(c *uuid.UUID) error {
			_, err := (&Proxy{DB: db}).GetByNameAndCompanyID(ctx, n64, c, &ProxyOption{})
			return err
		}, true, "(`proxies`.`company_id` = ? OR `proxies`.`company_id` IS NULL)", "`proxies`.`company_id` IS NULL"},
		{"api sender", func(c *uuid.UUID) error {
			_, err := (&APISender{DB: db}).GetByName(ctx, n64, c, &APISenderOption{})
			return err
		}, true,
			"(`api_senders`.`company_id` = ? OR `api_senders`.`company_id` IS NULL)",
			"`api_senders`.`company_id` IS NULL",
		},
		{"remote browser", func(c *uuid.UUID) error {
			_, err := (&RemoteBrowser{DB: db}).GetByNameAndCompanyID(ctx, n64, c, &RemoteBrowserOption{})
			return err
		}, true,
			"(`remote_browsers`.`company_id` = ? OR `remote_browsers`.`company_id` IS NULL)",
			"`remote_browsers`.`company_id` IS NULL",
		},
		{"domain", func(c *uuid.UUID) error {
			_, err := (&Domain{DB: db}).GetByName(ctx, n255, &DomainOption{})
			return err
		}, false, "`domains`.`name` = ?", "`domains`.`name` = ?"},
		{"identifier", func(c *uuid.UUID) error {
			_, err := (&Identifier{DB: db}).GetByName(ctx, "missing")
			return err
		}, false, "`identifiers`.`name` = ?", "`identifiers`.`name` = ?"},
		{"webhook", func(c *uuid.UUID) error {
			_, err := (&Webhook{DB: db}).GetByName(ctx, n127)
			return err
		}, false, "`webhooks`.`name` = ?", "`webhooks`.`name` = ?"},
	}
	for _, l := range lookups {
		for _, c := range []*uuid.UUID{&companyID, nil} {
			if err := l.run(c); err != gorm.ErrRecordNotFound {
				t.Fatalf("%s: want record not found, got %v", l.name, err)
			}
			q := lastQuery(t, captured)
			checkPlaceholders(t, q)
			want := l.wantWith
			if c == nil {
				want = l.wantNil
			}
			if !strings.Contains(q.sql, want) {
				t.Fatalf("%s: want %q in %q", l.name, want, q.sql)
			}
			if !strings.Contains(q.sql, ".`name` = ?") {
				t.Fatalf("%s: missing name filter in %q", l.name, q.sql)
			}
			if !l.scoped {
				break
			}
		}
	}
}

// TestCheckColumnIsUnique checks the unique check sees global and company rows
func TestCheckColumnIsUnique(t *testing.T) {
	db, captured := newBuilderDB(t, &database.Script{})
	a, b := uuid.New(), uuid.New()
	insertScript(t, db, "global", "g", nil)
	insertScript(t, db, "a1", "a", &a)
	ctx := context.Background()

	cases := []struct {
		name      string
		companyID *uuid.UUID
		want      bool
	}{
		{"global", &b, false},
		{"a1", &a, false},
		{"a1", &b, true},
		{"a1", nil, false},
		{"fresh", &a, true},
		{"fresh", nil, true},
	}
	for _, c := range cases {
		ok, err := CheckNameIsUnique(ctx, db, database.SCRIPT_TABLE, c.name, c.companyID, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if ok != c.want {
			t.Fatalf("%s company %v: want unique %v, got %v", c.name, c.companyID, c.want, ok)
		}
		checkPlaceholders(t, lastQuery(t, captured))
	}
	var row database.Script
	if err := db.Where("name = ?", "a1").First(&row).Error; err != nil {
		t.Fatalf("load a1: %v", err)
	}
	ok, err := CheckNameIsUnique(ctx, db, database.SCRIPT_TABLE, "a1", &a, row.ID)
	if err != nil || !ok {
		t.Fatalf("own row must not clash: ok %v err %v", ok, err)
	}
}
