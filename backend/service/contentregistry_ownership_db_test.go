package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// ownershipFixture extends the reconcile fixture with real Asset and Attachment
// services over temp folders, so asset and attachment ownership runs against
// stored rows and files on disk.
type ownershipFixture struct {
	*regFixture
	assetRoot      string
	attachmentRoot string
}

func newOwnershipFixture(t *testing.T) *ownershipFixture {
	t.Helper()
	h := newRegFixture(t)
	if err := h.db.AutoMigrate(
		&database.Domain{},
		&database.Asset{},
		&database.Email{},
		&database.Attachment{},
		&database.EmailAttachment{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	common := h.s.Common
	fileSvc := &File{Common: common}
	assetRoot := filepath.Join(t.TempDir(), "assets")
	attachmentRoot := filepath.Join(t.TempDir(), "attachments")
	emailRepo := &repository.Email{DB: h.db}
	h.s.File = fileSvc
	h.s.EmailRepository = emailRepo
	h.s.Asset = &Asset{
		Common:           common,
		RootFolder:       assetRoot,
		FileService:      fileSvc,
		AssetRepository:  &repository.Asset{DB: h.db},
		DomainRepository: &repository.Domain{DB: h.db},
	}
	h.s.Attachment = &Attachment{
		Common:               common,
		RootFolder:           attachmentRoot,
		FileService:          fileSvc,
		AttachmentRepository: &repository.Attachment{DB: h.db},
		EmailRepository:      emailRepo,
	}
	return &ownershipFixture{regFixture: h, assetRoot: assetRoot, attachmentRoot: attachmentRoot}
}

// newInstall inserts an installed package row and returns its id.
func (h *ownershipFixture) newInstall(pkgID string) *uuid.UUID {
	h.t.Helper()
	regID := uuid.New()
	id, err := h.s.RegistryRepository.InsertInstalledPackage(h.ctx, &database.InstalledPackage{
		RegistryID: &regID, PackageID: pkgID, PackageType: "page", Version: "1.0.0",
	})
	if err != nil {
		h.t.Fatalf("insert installed package: %v", err)
	}
	return id
}

// userAsset stores a global asset the way a user upload does: a row plus the
// file in the shared folder, with no provenance.
func (h *ownershipFixture) userAsset(rel, content string) *uuid.UUID {
	h.t.Helper()
	a := &model.Asset{}
	p := vo.NewRelativeFilePathMust(rel)
	a.Path = nullable.NewNullableWithValue(*p)
	id, err := h.s.Asset.AssetRepository.Insert(h.ctx, a)
	if err != nil {
		h.t.Fatalf("insert user asset: %v", err)
	}
	if err := h.s.writeAssetFile(rel, []byte(content), false); err != nil {
		h.t.Fatalf("write user asset: %v", err)
	}
	return id
}

func (h *ownershipFixture) assetFile(rel string) string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.assetRoot, data.ASSET_GLOBAL_FOLDER, rel))
	if err != nil {
		h.t.Fatalf("read asset file %s: %v", rel, err)
	}
	return string(b)
}

func (h *ownershipFixture) resourcesOf(ipID *uuid.UUID, resourceType string) []*database.InstalledResource {
	h.t.Helper()
	all, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, ipID)
	if err != nil {
		h.t.Fatalf("list resources: %v", err)
	}
	var out []*database.InstalledResource
	for _, r := range all {
		if r.ResourceType == resourceType {
			out = append(out, r)
		}
	}
	return out
}

func assetPkg(rel, content string) *registry.VerifiedPackage {
	return &registry.VerifiedPackage{Files: map[string][]byte{"assets/" + rel: []byte(content)}}
}

// TestInstallDoesNotAdoptUserAsset checks that a package shipping an asset at a
// path where the user already has a global asset fails with a conflict naming
// the path, records no provenance for it, and leaves the user file untouched, so
// a later uninstall can never delete it.
func TestInstallDoesNotAdoptUserAsset(t *testing.T) {
	h := newOwnershipFixture(t)
	userID := h.userAsset("img/logo.png", "user bytes")
	ip := h.newInstall("acme/kit")

	pkg := assetPkg("img/logo.png", "package bytes")
	conflicts, err := h.s.assetPathConflicts(h.ctx, []*registry.VerifiedPackage{pkg})
	if err != nil {
		t.Fatalf("conflict pre pass: %v", err)
	}
	if len(conflicts) != 1 || conflicts[0] != "img/logo.png" {
		t.Fatalf("pre pass conflicts = %v, want [img/logo.png]", conflicts)
	}

	err = h.s.installEntities(h.ctx, h.session, pkg, map[string]string{}, ip, "{}", nil, &InstallResult{})
	if err == nil {
		t.Fatalf("install over a user asset succeeded, want a conflict error")
	}
	if !strings.Contains(err.Error(), "img/logo.png") {
		t.Fatalf("conflict error does not name the path: %v", err)
	}
	if rs := h.resourcesOf(ip, "asset"); len(rs) != 0 {
		t.Fatalf("user asset was adopted: %d provenance rows", len(rs))
	}
	if got := h.assetFile("img/logo.png"); got != "user bytes" {
		t.Fatalf("user asset file overwritten: %q", got)
	}

	// rolling back and removing the install must keep the user asset.
	h.s.rollbackInstall(h.ctx, h.session, ip, false)
	if a, _ := h.s.Asset.AssetRepository.GetGlobalByPath(h.ctx, "img/logo.png"); a == nil || a.ID.MustGet() != *userID {
		t.Fatalf("user asset row gone after rollback")
	}
	if got := h.assetFile("img/logo.png"); got != "user bytes" {
		t.Fatalf("user asset file changed after rollback: %q", got)
	}
}

// TestRollbackKeepsSharedAsset checks that rolling back a failed install keeps an
// asset another install still references, and that the last owner's rollback
// then deletes it.
func TestRollbackKeepsSharedAsset(t *testing.T) {
	h := newOwnershipFixture(t)
	pkg := assetPkg("css/site.css", "body{}")

	first := h.newInstall("acme/first")
	res1 := &InstallResult{}
	if err := h.s.installEntities(h.ctx, h.session, pkg, map[string]string{}, first, "{}", nil, res1); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if len(res1.Assets) != 1 {
		t.Fatalf("first install should create the asset, got %v", res1.Assets)
	}

	// a second package ships the same bytes at the same path: it shares the asset,
	// does not create it.
	second := h.newInstall("acme/second")
	res2 := &InstallResult{}
	if err := h.s.installEntities(
		h.ctx, h.session, assetPkg("css/site.css", "body{}"), map[string]string{}, second, "{}", nil, res2,
	); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if len(res2.Assets) != 0 {
		t.Fatalf("second install should share, not create: %v", res2.Assets)
	}
	if rs := h.resourcesOf(second, "asset"); len(rs) != 1 {
		t.Fatalf("shared asset reference not recorded: %d rows", len(rs))
	}
	if got := h.assetFile("css/site.css"); got != "body{}" {
		t.Fatalf("shared asset overwritten by second install: %q", got)
	}

	// the second install fails later and rolls back: the shared asset stays.
	if !h.s.rollbackInstall(h.ctx, h.session, second, false) {
		t.Fatalf("rollback of second install reported leftovers")
	}
	a, err := h.s.Asset.AssetRepository.GetGlobalByPath(h.ctx, "css/site.css")
	if err != nil || a == nil {
		t.Fatalf("shared asset row deleted by rollback: %v", err)
	}
	if got := h.assetFile("css/site.css"); got != "body{}" {
		t.Fatalf("shared asset file changed by rollback: %q", got)
	}
	if rs := h.resourcesOf(first, "asset"); len(rs) != 1 {
		t.Fatalf("first install lost its asset provenance")
	}

	// the first install is now the only owner, so its rollback deletes the asset.
	if !h.s.rollbackInstall(h.ctx, h.session, first, false) {
		t.Fatalf("rollback of first install reported leftovers")
	}
	if a, _ := h.s.Asset.AssetRepository.GetGlobalByPath(h.ctx, "css/site.css"); a != nil {
		t.Fatalf("owned asset row kept after last owner rolled back")
	}
	if _, err := os.Stat(filepath.Join(h.assetRoot, data.ASSET_GLOBAL_FOLDER, "css/site.css")); !os.IsNotExist(err) {
		t.Fatalf("owned asset file kept after last owner rolled back: %v", err)
	}
}

// newInstallFrom inserts a Bait Box row and an installed package row from it.
func (h *ownershipFixture) newInstallFrom(boxName, pkgID string) *uuid.UUID {
	h.t.Helper()
	regID, err := h.s.RegistryRepository.InsertRegistry(h.ctx, &database.Registry{
		Name: boxName, URL: "https://" + strings.ToLower(boxName) + ".example.test",
	})
	if err != nil {
		h.t.Fatalf("insert registry: %v", err)
	}
	id, err := h.s.RegistryRepository.InsertInstalledPackage(h.ctx, &database.InstalledPackage{
		RegistryID: regID, PackageID: pkgID, PackageType: "page", Version: "1.0.0",
	})
	if err != nil {
		h.t.Fatalf("insert installed package: %v", err)
	}
	return id
}

// reconcileAssetPkg runs the asset step of an update for ipID with pkg.
func (h *ownershipFixture) reconcileAssetPkg(ipID *uuid.UUID, pkg *registry.VerifiedPackage) (*updateUndo, error) {
	h.t.Helper()
	resources, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, ipID)
	if err != nil {
		h.t.Fatalf("resources: %v", err)
	}
	undo := &updateUndo{}
	return undo, h.s.reconcileAssets(h.ctx, h.session, ipID, pkg, resources, "{}", &InstallResult{}, undo)
}

// TestSharedAssetSameBytes checks that a second package shipping the same bytes
// at a path another install owns shares the asset and records the hash of those
// bytes.
func TestSharedAssetSameBytes(t *testing.T) {
	h := newOwnershipFixture(t)
	first := h.newInstall("acme/first")
	if err := h.s.installEntities(
		h.ctx, h.session, assetPkg("js/app.js", "ok()"), map[string]string{}, first, "{}", nil, &InstallResult{},
	); err != nil {
		t.Fatalf("first install: %v", err)
	}
	second := h.newInstall("acme/second")
	if err := h.s.installEntities(
		h.ctx, h.session, assetPkg("js/app.js", "ok()"), map[string]string{}, second, "{}", nil, &InstallResult{},
	); err != nil {
		t.Fatalf("second install with the same bytes: %v", err)
	}
	rs := h.resourcesOf(second, "asset")
	if len(rs) != 1 || rs[0].InstalledContentHash != contentHash("ok()") {
		t.Fatalf("shared asset provenance = %+v", rs)
	}
	// an update of the second package that keeps the same bytes still works.
	if _, err := h.reconcileAssetPkg(second, assetPkg("js/app.js", "ok()")); err != nil {
		t.Fatalf("update with the same bytes: %v", err)
	}
}

// TestSharedAssetOtherBytesRefusedOnInstall checks that a package shipping other
// bytes at a path another install owns is refused with an error naming the path
// and the owner, records no provenance, and leaves the file as it was.
func TestSharedAssetOtherBytesRefusedOnInstall(t *testing.T) {
	h := newOwnershipFixture(t)
	first := h.newInstall("acme/first")
	if err := h.s.installEntities(
		h.ctx, h.session, assetPkg("js/jquery.min.js", "jquery()"), map[string]string{}, first, "{}", nil, &InstallResult{},
	); err != nil {
		t.Fatalf("first install: %v", err)
	}
	second := h.newInstall("acme/second")
	err := h.s.installEntities(
		h.ctx, h.session, assetPkg("js/jquery.min.js", "evil()"), map[string]string{}, second, "{}", nil, &InstallResult{},
	)
	if err == nil {
		t.Fatal("install with other bytes at a shared path succeeded")
	}
	if !strings.Contains(err.Error(), "js/jquery.min.js") || !strings.Contains(err.Error(), "acme/first") {
		t.Fatalf("error does not name the path and owner: %v", err)
	}
	if rs := h.resourcesOf(second, "asset"); len(rs) != 0 {
		t.Fatalf("provenance recorded for bytes the package did not write: %d rows", len(rs))
	}
	if got := h.assetFile("js/jquery.min.js"); got != "jquery()" {
		t.Fatalf("shared asset file changed: %q", got)
	}
	// the refused install rolls back without touching the owner's asset.
	h.s.rollbackInstall(h.ctx, h.session, second, false)
	if got := h.assetFile("js/jquery.min.js"); got != "jquery()" {
		t.Fatalf("shared asset file changed by rollback: %q", got)
	}
	if rs := h.resourcesOf(first, "asset"); len(rs) != 1 {
		t.Fatalf("owner lost its asset provenance")
	}
}

// TestSharedAssetOtherBytesRefusedOnUpdate checks that an update which starts to
// ship an asset at a path another install owns with other bytes is refused, and
// that the same bytes there are shared.
func TestSharedAssetOtherBytesRefusedOnUpdate(t *testing.T) {
	h := newOwnershipFixture(t)
	first := h.newInstall("acme/first")
	if err := h.s.installEntities(
		h.ctx, h.session, assetPkg("css/a.css", "a{}"), map[string]string{}, first, "{}", nil, &InstallResult{},
	); err != nil {
		t.Fatalf("first install: %v", err)
	}
	second := h.newInstall("acme/second")
	undo, err := h.reconcileAssetPkg(second, assetPkg("css/a.css", "b{}"))
	if err == nil {
		t.Fatal("update with other bytes at a shared path succeeded")
	}
	undo.run()
	if !strings.Contains(err.Error(), "css/a.css") || !strings.Contains(err.Error(), "acme/first") {
		t.Fatalf("error does not name the path and owner: %v", err)
	}
	if rs := h.resourcesOf(second, "asset"); len(rs) != 0 {
		t.Fatalf("provenance recorded for bytes the package did not write: %d rows", len(rs))
	}
	if got := h.assetFile("css/a.css"); got != "a{}" {
		t.Fatalf("shared asset file changed: %q", got)
	}
	if _, err := h.reconcileAssetPkg(second, assetPkg("css/a.css", "a{}")); err != nil {
		t.Fatalf("update with the same bytes: %v", err)
	}
	if rs := h.resourcesOf(second, "asset"); len(rs) != 1 || rs[0].InstalledContentHash != contentHash("a{}") {
		t.Fatalf("shared asset provenance = %+v", rs)
	}
	// uninstall order: the first owner leaves, the second keeps the asset, then
	// the last owner removes it.
	if !h.s.rollbackInstall(h.ctx, h.session, first, false) {
		t.Fatal("rollback of first install reported leftovers")
	}
	if got := h.assetFile("css/a.css"); got != "a{}" {
		t.Fatalf("shared asset removed while still referenced: %q", got)
	}
	if !h.s.rollbackInstall(h.ctx, h.session, second, false) {
		t.Fatal("rollback of second install reported leftovers")
	}
	if a, _ := h.s.Asset.AssetRepository.GetGlobalByPath(h.ctx, "css/a.css"); a != nil {
		t.Fatal("asset kept after the last owner left")
	}
}

// TestSharedAssetCrossBaitBox checks that a package from one Bait Box cannot
// plant bytes that a package from another Bait Box then loads: the later install
// and its updates are refused, naming the owner package and its Bait Box.
func TestSharedAssetCrossBaitBox(t *testing.T) {
	h := newOwnershipFixture(t)
	hostile := h.newInstallFrom("Hostile", "evil/kit")
	if err := h.s.installEntities(
		h.ctx, h.session, assetPkg("js/jquery.min.js", "evil()"), map[string]string{}, hostile, "{}", nil, &InstallResult{},
	); err != nil {
		t.Fatalf("hostile install: %v", err)
	}
	trusted := h.newInstallFrom("Trusted", "good/kit")
	err := h.s.installEntities(
		h.ctx, h.session, assetPkg("js/jquery.min.js", "jquery()"),
		map[string]string{}, trusted, "{}", nil, &InstallResult{},
	)
	if err == nil {
		t.Fatal("trusted install reused the hostile bytes")
	}
	for _, want := range []string{"js/jquery.min.js", "evil/kit", "Hostile"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error does not name %q: %v", want, err)
		}
	}
	h.s.rollbackInstall(h.ctx, h.session, trusted, false)
	undo, err := h.reconcileAssetPkg(trusted, assetPkg("js/jquery.min.js", "jquery()"))
	undo.run()
	if err == nil || !strings.Contains(err.Error(), "Hostile") {
		t.Fatalf("trusted update over hostile bytes not refused: %v", err)
	}
	if rs := h.resourcesOf(trusted, "asset"); len(rs) != 0 {
		t.Fatalf("trusted package tracks the hostile asset: %d rows", len(rs))
	}
}

// TestAttachmentNameCollisionDoesNotOverwrite checks that a package attachment
// whose file name is already used by an existing attachment fails clearly and
// leaves the existing file and row untouched, and that a fresh name installs.
func TestAttachmentNameCollisionDoesNotOverwrite(t *testing.T) {
	h := newOwnershipFixture(t)
	shared := filepath.Join(h.attachmentRoot, data.ASSET_GLOBAL_FOLDER)
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shared, "invoice.pdf"), []byte("user pdf"), 0o644); err != nil {
		t.Fatalf("write user attachment: %v", err)
	}
	um := &model.Attachment{}
	um.FileName = nn(*vo.NewFileNameMust("invoice.pdf"))
	um.EmbeddedContent = nn(false)
	um.SendAsCalendar = nn(false)
	if _, err := h.s.Attachment.AttachmentRepository.Insert(h.ctx, um); err != nil {
		t.Fatalf("insert user attachment: %v", err)
	}

	_, err := h.s.createAttachment(h.ctx, h.session, "Invoice", "invoice.pdf", "", false, false, []byte("package pdf"))
	if err == nil {
		t.Fatalf("attachment over an existing file name succeeded, want a conflict error")
	}
	if !strings.Contains(err.Error(), "invoice.pdf") {
		t.Fatalf("conflict error does not name the file: %v", err)
	}
	b, rerr := os.ReadFile(filepath.Join(shared, "invoice.pdf"))
	if rerr != nil || string(b) != "user pdf" {
		t.Fatalf("existing attachment file changed: %q %v", b, rerr)
	}
	if n, _ := h.s.Attachment.AttachmentRepository.CountGlobalByFileName(h.ctx, "invoice.pdf"); n != 1 {
		t.Fatalf("attachment rows for invoice.pdf = %d, want 1", n)
	}

	// a file left on disk without any row is not overwritten either.
	if err := os.WriteFile(filepath.Join(shared, "stray.txt"), []byte("stray"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	_, err = h.s.createAttachment(h.ctx, h.session, "Stray", "stray.txt", "", false, false, []byte("package"))
	if err == nil {
		t.Fatalf("attachment over a stray file succeeded")
	}
	if b, _ := os.ReadFile(filepath.Join(shared, "stray.txt")); string(b) != "stray" {
		t.Fatalf("stray file overwritten: %q", b)
	}

	// a free name installs and its removal deletes only its own file.
	id, err := h.s.createAttachment(h.ctx, h.session, "Memo", "memo.pdf", "", false, false, []byte("memo"))
	if err != nil {
		t.Fatalf("attachment with a free name: %v", err)
	}
	if err := h.s.deleteResourceEntity(h.ctx, h.session, "attachment", id); err != nil {
		t.Fatalf("delete own attachment: %v", err)
	}
	if _, err := os.Stat(filepath.Join(shared, "memo.pdf")); !os.IsNotExist(err) {
		t.Fatalf("own attachment file kept: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(shared, "invoice.pdf")); string(b) != "user pdf" {
		t.Fatalf("user attachment changed by removing another: %q", b)
	}
}
