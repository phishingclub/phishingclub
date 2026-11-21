package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// busySnapshotError makes a real SQLITE_BUSY_SNAPSHOT: a transaction reads,
// another connection writes and commits, and the first transaction then
// tries to write
func busySnapshotError(t *testing.T) error {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "busy.db") + "?_journal_mode=WAL&_busy_timeout=5000"
	a, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer a.Close()
	b, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer b.Close()
	if _, err := a.Exec("CREATE TABLE t (v INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	ctx := context.Background()
	tx, err := a.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRow("SELECT COUNT(*) FROM t").Scan(&n); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := b.Exec("INSERT INTO t (v) VALUES (1)"); err != nil {
		t.Fatalf("other write: %v", err)
	}
	_, err = tx.Exec("INSERT INTO t (v) VALUES (2)")
	if err == nil {
		t.Fatal("a write after a stale read must fail")
	}
	return err
}

// TestRetryBusyTriesAgain checks a chunk write that fails with a busy error,
// as a stale read before the first write gives, is tried again, and any
// other error or a busy error on every try ends the write
func TestRetryBusyTriesAgain(t *testing.T) {
	busy := busySnapshotError(t)
	if !isBusy(busy) {
		t.Fatalf("a busy snapshot error must count as busy: %v", busy)
	}
	ctx := context.Background()
	calls := 0
	if err := retryBusy(ctx, func() error {
		calls++
		if calls < 3 {
			return busy
		}
		return nil
	}); err != nil || calls != 3 {
		t.Fatalf("expected success on the third try, got %v after %d", err, calls)
	}
	calls = 0
	if err := retryBusy(ctx, func() error {
		calls++
		return busy
	}); err == nil || calls != closeWriteTries {
		t.Fatalf("expected the busy error after %d tries, got %v after %d", closeWriteTries, err, calls)
	}
	calls = 0
	other := context.Canceled
	if err := retryBusy(ctx, func() error {
		calls++
		return other
	}); err != other || calls != 1 {
		t.Fatalf("another error must not be tried again, got %v after %d", err, calls)
	}
}
