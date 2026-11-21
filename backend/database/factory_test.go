package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/file"
)

// TestSqlitePragmasOnEveryConnection opens the database the way the app does
// and checks that every pooled connection has the DSN settings and the
// journal size limit set by the connect hook.
func TestSqlitePragmasOnEveryConnection(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "data", "db.sqlite3")
	conf, err := config.NewConfig(
		"", "localhost", false, "", "",
		"127.0.0.1:0", "127.0.0.1:0", "127.0.0.1:0",
		config.Database{Engine: config.DefaultAdministrationUseSqlite, DSN: dsn},
		&file.FileWriter{}, "", "", config.IPSecurityConfig{},
	)
	if err != nil {
		t.Fatal(err)
	}
	db, err := FromConfig(*conf)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	ctx := context.Background()
	// hold several connections at once so each one is a separate sqlite
	// connection from the pool
	conns := []*sql.Conn{}
	for i := 0; i < sqliteMaxConns; i++ {
		conn, err := sqlDB.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
	}
	want := map[string]string{
		"journal_mode":       "wal",
		"busy_timeout":       "5000",
		"synchronous":        "1",
		"foreign_keys":       "1",
		"journal_size_limit": "67108864",
	}
	for i, conn := range conns {
		for pragma, value := range want {
			var got string
			// fixed names from the map above
			if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatalf("connection %d PRAGMA %s: %v", i, pragma, err)
			}
			if strings.ToLower(got) != value {
				t.Fatalf("connection %d %s = %s, want %s", i, pragma, got, value)
			}
		}
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
}
