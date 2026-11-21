package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"go.uber.org/zap"
)

func newClearBackupFixture(t *testing.T) (*Backup, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "backups")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"backup-20260101-000000.tar.gz",
		"backup-20260102-000000.tar.gz",
		"notes.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	// a folder with the backup name shape is not a backup file
	if err := os.Mkdir(filepath.Join(dir, "backup-folder.tar.gz"), 0o750); err != nil {
		t.Fatal(err)
	}
	nop := zap.NewNop().Sugar()
	return &Backup{Common: Common{Logger: nop, AuditLogger: nop}, FilePath: root}, dir
}

func TestClearBackupsDeletesOnlyBackupFiles(t *testing.T) {
	svc, dir := newClearBackupFixture(t)
	session := adminSession(uuid.New(), data.RoleSuperAdministrator)

	deleted, err := svc.ClearBackups(context.Background(), session)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	left := map[string]bool{}
	for _, e := range entries {
		left[e.Name()] = true
	}
	if len(left) != 2 || !left["notes.txt"] || !left["backup-folder.tar.gz"] {
		t.Fatalf("unexpected files left: %v", left)
	}
}

func TestClearBackupsWithoutFolder(t *testing.T) {
	nop := zap.NewNop().Sugar()
	svc := &Backup{Common: Common{Logger: nop, AuditLogger: nop}, FilePath: t.TempDir()}
	deleted, err := svc.ClearBackups(context.Background(), adminSession(uuid.New(), data.RoleSuperAdministrator))
	if err != nil || deleted != 0 {
		t.Fatalf("expected nothing deleted without error, got %d %v", deleted, err)
	}
}

func TestClearBackupsRequiresGlobalPermission(t *testing.T) {
	svc, dir := newClearBackupFixture(t)
	session := adminSession(uuid.New(), data.RoleCompanyUser)

	if _, err := svc.ClearBackups(context.Background(), session); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected authorization failure, got %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 4 {
		t.Fatalf("nothing should be deleted, %d entries left", len(entries))
	}
}

func TestClearBackupsRefusedWhileBackupRuns(t *testing.T) {
	svc, dir := newClearBackupFixture(t)
	prev := currentBackupStatus
	currentBackupStatus = &BackupStatus{IsRunning: true}
	t.Cleanup(func() { currentBackupStatus = prev })

	_, err := svc.ClearBackups(context.Background(), adminSession(uuid.New(), data.RoleSuperAdministrator))
	if !errors.As(err, &errs.CustomError{}) {
		t.Fatalf("expected a refusal while a backup runs, got %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 4 {
		t.Fatalf("nothing should be deleted, %d entries left", len(entries))
	}
}
