package service

import (
	"archive/zip"
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/registry"
)

// TestInstallAssetLoosePathRefused checks that a package asset path with a
// trailing space is refused by the conflict pre pass and by install, so it can
// never look up one row while writing or deleting another file, and that the
// user asset at the trimmed path stays after a rollback.
func TestInstallAssetLoosePathRefused(t *testing.T) {
	h := newOwnershipFixture(t)
	userID := h.userAsset("logo.png", "user bytes")
	ip := h.newInstall("acme/kit")

	pkg := assetPkg("logo.png ", "package bytes")
	if _, err := h.s.assetPathConflicts(h.ctx, []*registry.VerifiedPackage{pkg}); err == nil {
		t.Fatalf("conflict pre pass accepted a path with a trailing space")
	}
	if err := h.s.installEntities(h.ctx, h.session, pkg, map[string]string{}, ip, "{}", nil, &InstallResult{}); err == nil {
		t.Fatalf("install accepted a path with a trailing space")
	}
	if _, _, err := h.s.installAsset(h.ctx, h.session, ip, "logo.png ", []byte("package bytes")); err == nil {
		t.Fatalf("installAsset accepted a path with a trailing space")
	}
	if rs := h.resourcesOf(ip, "asset"); len(rs) != 0 {
		t.Fatalf("provenance recorded for a refused path: %d rows", len(rs))
	}

	// removing the install must not touch the user asset.
	h.s.rollbackInstall(h.ctx, h.session, ip, false)
	a, _ := h.s.Asset.AssetRepository.GetGlobalByPath(h.ctx, "logo.png")
	if a == nil || a.ID.MustGet() != *userID {
		t.Fatalf("user asset row gone after rollback")
	}
	if got := h.assetFile("logo.png"); got != "user bytes" {
		t.Fatalf("user asset file changed: %q", got)
	}
}

func zipWith(t *testing.T, name, body string) *zip.File {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip read: %v", err)
	}
	return r.File[0]
}

// TestZipImportAssetOverwrite checks that an unclean zip asset path is refused
// without touching the existing file, that a path owned by an asset row is
// skipped, and that a file on disk with no asset row is overwritten and gets
// a row so it shows in the asset list.
func TestZipImportAssetOverwrite(t *testing.T) {
	h := newOwnershipFixture(t)
	h.userAsset("a/logo.png", "user bytes")
	im := &Import{Common: h.s.Common, File: h.s.File, Asset: h.s.Asset}
	gin.SetMode(gin.TestMode)
	g, _ := gin.CreateTestContext(httptest.NewRecorder())
	g.Request = httptest.NewRequest("POST", "/", nil)

	created, err := im.createAssetFromZipFile(g, h.session, zipWith(t, "assets/a//logo.png", "zip bytes"), "a//logo.png")
	if err == nil || created {
		t.Fatalf("unclean path accepted: created=%v err=%v", created, err)
	}
	if got := h.assetFile("a/logo.png"); got != "user bytes" {
		t.Fatalf("existing file overwritten: %q", got)
	}

	// a path owned by a row is skipped and the file kept
	created, err = im.createAssetFromZipFile(g, h.session, zipWith(t, "assets/a/logo.png", "zip bytes"), "a/logo.png")
	if err != nil || created {
		t.Fatalf("owned path should be skipped: created=%v err=%v", created, err)
	}
	if got := h.assetFile("a/logo.png"); got != "user bytes" {
		t.Fatalf("owned file overwritten: %q", got)
	}

	// a file on disk without a row is overwritten and the new row kept
	orphan := filepath.Join(h.assetRoot, data.ASSET_GLOBAL_FOLDER, "b", "logo.png")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(orphan, []byte("disk bytes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	created, err = im.createAssetFromZipFile(g, h.session, zipWith(t, "assets/b/logo.png", "zip bytes"), "b/logo.png")
	if err != nil || !created {
		t.Fatalf("file without a row should be imported: created=%v err=%v", created, err)
	}
	if got := h.assetFile("b/logo.png"); got != "zip bytes" {
		t.Fatalf("file not overwritten: %q", got)
	}
	if a, _ := h.s.Asset.AssetRepository.GetGlobalByPath(h.ctx, "b/logo.png"); a == nil {
		t.Fatalf("no row kept for the imported file")
	}
}

// TestRemoveEmptyFolderStaysInRoot checks that empty folders are removed up to
// the root, and that a symlinked folder pointing outside the root is neither
// followed nor removed.
func TestRemoveEmptyFolderStaysInRoot(t *testing.T) {
	h := newOwnershipFixture(t)
	f := h.s.File
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rootDir := filepath.Join(base, "root")
	if err := os.MkdirAll(filepath.Join(rootDir, "a", "b"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(rootDir, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer root.Close()

	if err := f.RemoveEmptyFolderRecursively(root, "link/sub"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := f.RemoveEmptyFolderRecursively(root, "link"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "sub")); err != nil {
		t.Fatalf("folder outside the root was removed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(rootDir, "link")); err != nil {
		t.Fatalf("symlink was removed: %v", err)
	}

	if err := f.RemoveEmptyFolderRecursively(root, "a/b"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "a")); !os.IsNotExist(err) {
		t.Fatalf("empty folders kept: %v", err)
	}
	if _, err := os.Stat(rootDir); err != nil {
		t.Fatalf("root removed: %v", err)
	}

	// a delete through the root cannot reach outside it.
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Delete(root, "link/keep.txt"); err == nil {
		t.Fatalf("delete through a symlink out of the root succeeded")
	}
	if err := f.Delete(root, "../outside/keep.txt"); err == nil {
		t.Fatalf("delete with a parent segment succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("file outside the root was removed: %v", err)
	}
}

// TestUploadFileExclusive checks that a write without overwrite never replaces
// an existing file and that overwrite does.
func TestUploadFileExclusive(t *testing.T) {
	h := newOwnershipFixture(t)
	f := h.s.File
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer root.Close()
	read := func() string {
		b, err := os.ReadFile(filepath.Join(dir, "x", "y.txt"))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return string(b)
	}
	if err := f.UploadFile(root, "x/y.txt", bytes.NewBufferString("first"), false); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := f.UploadFile(root, "x/y.txt", bytes.NewBufferString("second"), false); err == nil {
		t.Fatalf("write without overwrite replaced an existing file")
	}
	if got := read(); got != "first" {
		t.Fatalf("file changed: %q", got)
	}
	if err := f.UploadFile(root, "x/y.txt", bytes.NewBufferString("third"), true); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got := read(); got != "third" {
		t.Fatalf("overwrite did not apply: %q", got)
	}
	if err := f.UploadFile(root, "../escape.txt", bytes.NewBufferString("x"), false); err == nil {
		t.Fatalf("write outside the root succeeded")
	}
}
