package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
)

func newTestAttachment(name string) *model.Attachment {
	return &model.Attachment{
		CompanyID: nullable.NewNullNullable[uuid.UUID](),
		FileName:  nullable.NewNullableWithValue(*vo.NewFileNameMust(name)),
	}
}

func writeAttachmentFile(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, data.ASSET_GLOBAL_FOLDER)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("content"), 0o640); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestAttachmentGetPathRootWithSpecialCharacters(t *testing.T) {
	root := filepath.Join(t.TempDir(), "user@host+files~")
	writeAttachmentFile(t, root, "report.pdf")
	svc := &Attachment{Common: Common{Logger: zap.NewNop().Sugar()}, RootFolder: root}

	p, err := svc.GetPath(newTestAttachment("report.pdf"))
	if err != nil {
		t.Fatalf("get path: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "content" {
		t.Fatalf("read %q: %v", p, err)
	}
}

func TestAttachmentGetPathRelativeRootWithParent(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeAttachmentFile(t, filepath.Join(base, "data@1"), "report.pdf")
	t.Chdir(work)
	svc := &Attachment{Common: Common{Logger: zap.NewNop().Sugar()}, RootFolder: "../data@1"}

	p, err := svc.GetPath(newTestAttachment("report.pdf"))
	if err != nil {
		t.Fatalf("get path: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "content" {
		t.Fatalf("read %q: %v", p, err)
	}
}

func TestAttachmentGetPathMissingFile(t *testing.T) {
	root := t.TempDir()
	writeAttachmentFile(t, root, "other.pdf")
	svc := &Attachment{Common: Common{Logger: zap.NewNop().Sugar()}, RootFolder: root}
	if _, err := svc.GetPath(newTestAttachment("report.pdf")); err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}
