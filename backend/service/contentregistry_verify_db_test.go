package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/vo"
)

// withEmailService wires the real Email service onto an ownership fixture that
// already has the update dependencies, so an update can create emails and their
// nested attachments.
func (h *ownershipFixture) withEmailService() {
	h.t.Helper()
	h.s.Email = &Email{
		Common:                  h.s.Common,
		EmailRepository:         h.s.EmailRepository,
		TemplateService:         &Template{Common: h.s.Common},
		AttachmentService:       h.s.Attachment,
		CampaignTemplateService: h.s.CampaignTemplate,
	}
}

func (h *ownershipFixture) emailExists(name string) bool {
	h.t.Helper()
	_, err := h.s.resolveEmailID(h.ctx, name)
	return err == nil
}

const invoiceMailYAML = `
emails:
  - name: Invoice mail
    file: mail.html
    subject: Your invoice
    from: billing@example.test
    envelope from: billing@example.test
    attachments:
      - name: Invoice
        file: invoice.pdf
`

// TestUpdateNewEmailAttachmentNameTaken updates an install to a version that adds
// an email whose attachment file name a user attachment already uses. The update
// must fail in the plan phase naming the file, write nothing and leave the user
// file alone; once the name is free the same update applies.
func TestUpdateNewEmailAttachmentNameTaken(t *testing.T) {
	h := newOwnershipFixture(t)
	ip := h.withUpdateDeps("ns/mail")
	h.withEmailService()

	shared := filepath.Join(h.attachmentRoot, data.ASSET_GLOBAL_FOLDER)
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	userFile := filepath.Join(shared, "invoice.pdf")
	if err := os.WriteFile(userFile, []byte("user pdf"), 0o644); err != nil {
		t.Fatalf("write user attachment: %v", err)
	}
	um := &model.Attachment{}
	um.FileName = nn(*vo.NewFileNameMust("invoice.pdf"))
	um.EmbeddedContent = nn(false)
	um.SendAsCalendar = nn(false)
	userAttID, err := h.s.Attachment.AttachmentRepository.Insert(h.ctx, um)
	if err != nil {
		t.Fatalf("insert user attachment: %v", err)
	}

	// a new email the Email service would reject (no envelope from) fails in the
	// plan phase too, not partway through the writes.
	noEnvelope := yamlPackage(
		"ns/mail", "1.1.0",
		strings.Replace(invoiceMailYAML, "    envelope from: billing@example.test\n", "", 1),
		map[string]string{
			"mail.html":   "<html><body>hi</body></html>",
			"invoice.pdf": "package pdf",
		},
	)
	if _, ae, err := h.apply(ip, noEnvelope, nil); err == nil {
		t.Fatal("update with an invalid new email succeeded")
	} else if _, wrote := ae.Details["rollback"]; wrote {
		t.Fatalf("invalid new email should fail before any write: %v", err)
	}

	pkg := yamlPackage("ns/mail", "1.1.0", invoiceMailYAML, map[string]string{
		"mail.html":   "<html><body>hi</body></html>",
		"invoice.pdf": "package pdf",
	})
	_, ae, err := h.apply(ip, pkg, nil)
	if err == nil {
		t.Fatal("update over a taken attachment name succeeded")
	}
	if !strings.Contains(err.Error(), "invoice.pdf") {
		t.Fatalf("error does not name the file: %v", err)
	}
	if _, wrote := ae.Details["rollback"]; wrote {
		t.Fatalf("taken attachment name should fail before any write, rollback=%v", ae.Details["rollback"])
	}
	if h.emailExists("Invoice mail") {
		t.Fatal("email created despite the failed update")
	}
	if rs, _ := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, ip.ID); len(rs) != 0 {
		t.Fatalf("provenance rows left behind: %d", len(rs))
	}
	if b, _ := os.ReadFile(userFile); string(b) != "user pdf" {
		t.Fatalf("user attachment file changed: %q", b)
	}

	// free the name; the same update now creates the email and its attachment.
	if err := h.s.Attachment.AttachmentRepository.DeleteByID(h.ctx, userAttID); err != nil {
		t.Fatalf("delete user attachment row: %v", err)
	}
	if err := os.Remove(userFile); err != nil {
		t.Fatalf("remove user attachment file: %v", err)
	}
	if _, _, err := h.apply(ip, pkg, nil); err != nil {
		t.Fatalf("update with a free attachment name: %v", err)
	}
	if !h.emailExists("Invoice mail") {
		t.Fatal("email not created")
	}
	if rs := h.resourcesOf(ip.ID, "attachment"); len(rs) != 1 {
		t.Fatalf("attachment provenance rows = %d, want 1", len(rs))
	}
	if b, _ := os.ReadFile(userFile); string(b) != "package pdf" {
		t.Fatalf("package attachment not written: %q", b)
	}
	got, err := h.s.RegistryRepository.GetInstalledPackageByID(h.ctx, ip.ID)
	if err != nil || got.Version != "1.1.0" {
		t.Fatalf("version not advanced: %v %v", got, err)
	}
}

// TestUpdateSharedAssetChangedBytes checks that an update shipping new bytes for
// an asset another install shares is refused, naming the path and the owner, and
// does not advance the version; that it overwrites the asset once this install
// is the only owner; and that it writes the old bytes back when a later write in
// the same update fails.
func TestUpdateSharedAssetChangedBytes(t *testing.T) {
	h := newOwnershipFixture(t)
	ip := h.withUpdateDeps("ns/a")
	const rel = "css/site.css"

	other := h.newInstall("ns/b")
	if err := h.s.installEntities(
		h.ctx, h.session, assetPkg(rel, "v1"), map[string]string{}, other, "{}", nil, &InstallResult{},
	); err != nil {
		t.Fatalf("install other: %v", err)
	}
	if err := h.s.installEntities(
		h.ctx, h.session, assetPkg(rel, "v1"), map[string]string{}, ip.ID, "{}", nil, &InstallResult{},
	); err != nil {
		t.Fatalf("install a: %v", err)
	}
	baseline := h.resourcesOf(ip.ID, "asset")[0].InstalledContentHash

	withAsset := func(version, content string, files map[string]string, dataYAML string) *registry.VerifiedPackage {
		p := yamlPackage("ns/a", version, dataYAML, files)
		p.Files["assets/"+rel] = []byte(content)
		return p
	}

	// shared: the changed bytes refuse the update and nothing advances.
	before, err := h.s.RegistryRepository.GetInstalledPackageByID(h.ctx, ip.ID)
	if err != nil {
		t.Fatalf("installed package: %v", err)
	}
	_, _, err = h.apply(ip, withAsset("1.1.0", "v2", nil, ""), nil)
	if err == nil {
		t.Fatal("update with new bytes for a shared asset succeeded")
	}
	for _, want := range []string{rel, "ns/b", "asset path conflict"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error does not name %q: %v", want, err)
		}
	}
	after, err := h.s.RegistryRepository.GetInstalledPackageByID(h.ctx, ip.ID)
	if err != nil || after.Version != before.Version {
		t.Fatalf("version advanced on a refused update: %v %v", after, err)
	}
	if got := h.assetFile(rel); got != "v1" {
		t.Fatalf("shared asset overwritten: %q", got)
	}
	if got := h.resourcesOf(ip.ID, "asset")[0].InstalledContentHash; got != baseline {
		t.Fatal("shared asset baseline moved although the bytes were not written")
	}

	// the other install goes away: this install now owns the asset alone.
	if !h.s.rollbackInstall(h.ctx, h.session, other, false) {
		t.Fatal("remove other install reported leftovers")
	}
	if _, _, err := h.apply(ip, withAsset("1.2.0", "v2", nil, ""), nil); err != nil {
		t.Fatalf("update with a sole owned asset: %v", err)
	}
	if got := h.assetFile(rel); got != "v2" {
		t.Fatalf("sole owned asset not overwritten: %q", got)
	}

	// a later write fails (a page name a user page already uses): the asset bytes
	// and baseline go back to what they were.
	if _, err := h.s.createPage(h.ctx, h.session, "Taken", "<p>user</p>"); err != nil {
		t.Fatalf("user page: %v", err)
	}
	v2Hash := h.resourcesOf(ip.ID, "asset")[0].InstalledContentHash
	failing := withAsset("1.3.0", "v3", map[string]string{"taken.html": "<p>pkg</p>"}, `
pages:
  - name: Taken
    file: taken.html
`)
	_, ae, err := h.apply(ip, failing, nil)
	if err == nil {
		t.Fatal("update over a taken page name succeeded")
	}
	if ae.Details["rollback"] != "complete" {
		t.Fatalf("rollback = %v, want complete", ae.Details["rollback"])
	}
	if got := h.assetFile(rel); got != "v2" {
		t.Fatalf("asset bytes not restored after rollback: %q", got)
	}
	if got := h.resourcesOf(ip.ID, "asset")[0].InstalledContentHash; got != v2Hash {
		t.Fatal("asset baseline not restored after rollback")
	}
}

// TestUpdateUserAssetConflictBeforeWrites checks that an update shipping an
// asset at a path a user asset holds fails in the plan phase, listing the path,
// without touching the content this install already owns.
func TestUpdateUserAssetConflictBeforeWrites(t *testing.T) {
	h := newOwnershipFixture(t)
	ip := h.withUpdateDeps("ns/a")
	h.userAsset("img/user.png", "user")
	pkg := yamlPackage("ns/a", "1.1.0", "", nil)
	pkg.Files["assets/img/user.png"] = []byte("pkg")
	pkg.Files["assets/img/new.png"] = []byte("new")
	_, ae, err := h.apply(ip, pkg, nil)
	if err == nil || !strings.Contains(err.Error(), "img/user.png") {
		t.Fatalf("expected an asset conflict naming the path, got %v", err)
	}
	if _, wrote := ae.Details["rollback"]; wrote {
		t.Fatal("asset conflict should fail before any write")
	}
	if a, _ := h.s.Asset.AssetRepository.GetGlobalByPath(h.ctx, "img/new.png"); a != nil {
		t.Fatal("an asset was written before the conflict was reported")
	}
	if got := h.assetFile("img/user.png"); got != "user" {
		t.Fatalf("user asset changed: %q", got)
	}
}

// TestRemoveBundleRetryAfterMemberFailure removes a bundle whose member's page
// cannot be deleted. The first uninstall reports the failure and keeps the
// bundle, the member and the link; once the block is gone a retry removes
// everything.
func TestRemoveBundleRetryAfterMemberFailure(t *testing.T) {
	h := newRegFixture(t)
	bundle := h.withUpdateDeps("ns/bundle")

	memberID := uuid.New()
	member := h.insertPackage(&memberID, "ns/member")
	if err := h.db.Model(&database.InstalledPackage{}).
		Where("id = ?", member.ID).
		Update("installed_as_dependency", true).Error; err != nil {
		t.Fatalf("mark dependency: %v", err)
	}
	if err := h.s.RegistryRepository.InsertPackageMember(h.ctx, bundle.ID, member.ID); err != nil {
		t.Fatalf("link member: %v", err)
	}
	install := func(ipID *uuid.UUID, name string) {
		pkg, docs := pageDocs(name, name+".html", "<p>"+name+"</p>")
		if err := h.s.installDocEntities(
			h.ctx, h.session, pkg, "", docs[0].dy, map[string]string{}, ipID, "{}", nil, &InstallResult{},
		); err != nil {
			t.Fatalf("install %s: %v", name, err)
		}
	}
	install(bundle.ID, "BundlePage")
	install(member.ID, "MemberPage")

	if err := h.db.Exec(`CREATE TRIGGER block_member_page BEFORE DELETE ON pages
		WHEN old.name = 'MemberPage'
		BEGIN SELECT RAISE(ABORT, 'blocked'); END`).Error; err != nil {
		t.Fatalf("trigger: %v", err)
	}
	err := h.s.Remove(h.ctx, h.session, bundle.ID)
	if err == nil || !strings.Contains(err.Error(), "uninstall incomplete") {
		t.Fatalf("expected an incomplete uninstall, got %v", err)
	}
	if _, ok := h.pageIDByName("BundlePage"); ok {
		t.Fatal("bundle page not removed on the first pass")
	}
	if _, ok := h.pageIDByName("MemberPage"); !ok {
		t.Fatal("blocked member page was removed")
	}
	if _, gerr := h.s.RegistryRepository.GetInstalledPackageByID(h.ctx, bundle.ID); gerr != nil {
		t.Fatalf("bundle row removed although a member failed: %v", gerr)
	}
	if _, gerr := h.s.RegistryRepository.GetInstalledPackageByID(h.ctx, member.ID); gerr != nil {
		t.Fatalf("member row removed although its page failed: %v", gerr)
	}
	if ms, _ := h.s.RegistryRepository.GetPackageMembers(h.ctx, bundle.ID); len(ms) != 1 {
		t.Fatalf("member link rows = %d, want 1", len(ms))
	}
	if rs, _ := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, member.ID); len(rs) != 1 {
		t.Fatalf("member provenance rows = %d, want 1", len(rs))
	}

	if err := h.db.Exec(`DROP TRIGGER block_member_page`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if err := h.s.Remove(h.ctx, h.session, bundle.ID); err != nil {
		t.Fatalf("retry uninstall: %v", err)
	}
	if _, ok := h.pageIDByName("MemberPage"); ok {
		t.Fatal("member page left after retry")
	}
	for _, id := range []*uuid.UUID{bundle.ID, member.ID} {
		if _, gerr := h.s.RegistryRepository.GetInstalledPackageByID(h.ctx, id); gerr == nil {
			t.Fatalf("package %s still installed after retry", id)
		}
	}
	if ms, _ := h.s.RegistryRepository.GetPackageMembers(h.ctx, bundle.ID); len(ms) != 0 {
		t.Fatalf("member links left after retry: %d", len(ms))
	}
}

// userAttachment stores a global attachment the way a user upload does.
func (h *ownershipFixture) userAttachment(fileName, content string) {
	h.t.Helper()
	shared := filepath.Join(h.attachmentRoot, data.ASSET_GLOBAL_FOLDER)
	if err := os.MkdirAll(shared, 0o755); err != nil {
		h.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shared, fileName), []byte(content), 0o644); err != nil {
		h.t.Fatalf("write user attachment: %v", err)
	}
	um := &model.Attachment{}
	um.FileName = nn(*vo.NewFileNameMust(fileName))
	um.EmbeddedContent = nn(false)
	um.SendAsCalendar = nn(false)
	if _, err := h.s.Attachment.AttachmentRepository.Insert(h.ctx, um); err != nil {
		h.t.Fatalf("insert user attachment: %v", err)
	}
}

// TestInstallAttachmentConflictsListed checks the install pre check lists every
// attachment file name that a user attachment holds or that two declarations in
// the install would both create, across packages.
func TestInstallAttachmentConflictsListed(t *testing.T) {
	h := newOwnershipFixture(t)
	h.userAttachment("invoice.pdf", "user")
	a := yamlPackage("ns/a", "1.0.0", `
emails:
  - name: Mail
    file: mail.html
    attachments:
      - name: Invoice
        file: invoice.pdf
      - name: Memo
        file: memo.pdf
`, map[string]string{"mail.html": "x", "invoice.pdf": "p", "memo.pdf": "m"})
	b := yamlPackage("ns/b", "1.0.0", `
attachments:
  - name: Memo copy
    file: memo.pdf
  - name: Free
    file: free.pdf
`, map[string]string{"memo.pdf": "m", "free.pdf": "f"})
	decls := installAttachmentDecls([]*registry.VerifiedPackage{a, b})
	got, err := h.s.attachmentNameConflicts(h.ctx, decls, nil, false)
	if err != nil {
		t.Fatalf("pre check: %v", err)
	}
	if strings.Join(got, ",") != "invoice.pdf,memo.pdf" {
		t.Fatalf("conflicts = %v, want [invoice.pdf memo.pdf]", got)
	}
}

// TestUpdateAttachmentClashesBeforeWrites checks the update plan phase refuses
// two new emails whose different attachments share a file name, and a "both"
// resolved email whose fresh copy adds an attachment over a user file name.
func TestUpdateAttachmentClashesBeforeWrites(t *testing.T) {
	h := newOwnershipFixture(t)
	ip := h.withUpdateDeps("ns/mail")
	h.withEmailService()

	twoEmails := yamlPackage("ns/mail", "1.1.0", `
emails:
  - name: First
    file: mail.html
    subject: s
    from: a@example.test
    envelope from: a@example.test
    attachments:
      - name: Report A
        file: report.pdf
  - name: Second
    file: mail.html
    subject: s
    from: a@example.test
    envelope from: a@example.test
    attachments:
      - name: Report B
        file: report.pdf
`, map[string]string{"mail.html": "<p>hi</p>", "report.pdf": "r"})
	_, ae, err := h.apply(ip, twoEmails, nil)
	if err == nil || !strings.Contains(err.Error(), "report.pdf") {
		t.Fatalf("expected a clash naming report.pdf, got %v", err)
	}
	if _, wrote := ae.Details["rollback"]; wrote {
		t.Fatal("clash should fail before any write")
	}
	if h.emailExists("First") || h.emailExists("Second") {
		t.Fatal("an email was created despite the clash")
	}

	// install an email with no attachments, then edit it locally.
	v1 := yamlPackage("ns/mail", "1.1.0", `
emails:
  - name: Notice
    file: mail.html
    subject: s
    from: a@example.test
    envelope from: a@example.test
`, map[string]string{"mail.html": "<p>v1</p>"})
	if _, _, err := h.apply(ip, v1, nil); err != nil {
		t.Fatalf("install notice: %v", err)
	}
	id, err := h.s.resolveEmailID(h.ctx, "Notice")
	if err != nil {
		t.Fatalf("notice missing: %v", err)
	}
	em := &model.Email{}
	em.Name = nn(*vo.NewString64Must("Notice"))
	em.Content = nn(mustOptional1MB(t, "<p>local</p>"))
	if err := h.s.Email.UpdateByID(h.ctx, h.session, id, em); err != nil {
		t.Fatalf("local edit: %v", err)
	}
	h.userAttachment("terms.pdf", "user")
	v2 := yamlPackage("ns/mail", "1.2.0", `
emails:
  - name: Notice
    file: mail.html
    subject: s
    from: a@example.test
    envelope from: a@example.test
    attachments:
      - name: Terms
        file: terms.pdf
`, map[string]string{"mail.html": "<p>v2</p>", "terms.pdf": "pkg"})
	ae = NewAuditEvent("Registry.PackageUpdate", h.session)
	res := map[string]string{"email:Notice": resolveBoth}
	_, err = h.s.applyUpdate(h.ctx, h.session, ae, ip, v2, nil, nil, res, nil)
	if err == nil || !strings.Contains(err.Error(), "terms.pdf") {
		t.Fatalf("expected a clash naming terms.pdf, got %v", err)
	}
	if _, wrote := ae.Details["rollback"]; wrote {
		t.Fatal("both clash should fail before any write")
	}
	if !h.emailExists("Notice") {
		t.Fatal("local email renamed despite the failed update")
	}
}

// TestOrderMembersAndMemoFetch checks members sort into manifest order with
// unlisted ones last by package id, and a memoized fetch downloads each id once.
func TestOrderMembersAndMemoFetch(t *testing.T) {
	h := newRegFixture(t)
	h.withUpdateDeps("ns/bundle")
	ids := map[string]*uuid.UUID{}
	for _, n := range []string{"ns/a", "ns/b", "ns/c", "ns/z"} {
		id := uuid.New()
		h.insertPackage(&id, n)
		ids[n] = &id
	}
	deps := []registry.Dependency{{ID: "ns/c"}, {ID: "ns/a"}}
	got := h.s.orderMembers(h.ctx, deps, []*uuid.UUID{ids["ns/z"], ids["ns/a"], ids["ns/b"], ids["ns/c"]})
	want := []*uuid.UUID{ids["ns/c"], ids["ns/a"], ids["ns/b"], ids["ns/z"]}
	for i := range want {
		if *got[i] != *want[i] {
			t.Fatalf("position %d = %s, want %s", i, got[i], want[i])
		}
	}

	calls := 0
	f := memoFetch(func(id string) (*registry.VerifiedPackage, error) {
		calls++
		return &registry.VerifiedPackage{}, nil
	})
	for i := 0; i < 3; i++ {
		_, _ = f("ns/a")
	}
	_, _ = f("ns/b")
	if calls != 2 {
		t.Fatalf("fetch calls = %d, want 2", calls)
	}
}
