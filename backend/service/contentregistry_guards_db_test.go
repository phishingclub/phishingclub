package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// withSMTP wires the smtp configuration service and its tables into the fixture.
func (h *regFixture) withSMTP() {
	h.t.Helper()
	if err := h.db.AutoMigrate(&database.SMTPConfiguration{}, &database.SMTPHeader{}); err != nil {
		h.t.Fatalf("migrate: %v", err)
	}
	repo := &repository.SMTPConfiguration{DB: h.db}
	h.s.SMTPConfigurationRepository = repo
	h.s.SMTPConfiguration = &SMTPConfiguration{Common: h.s.Common, SMTPConfigurationRepository: repo}
}

// smtpYAML is one smtp configuration named Relay with the given host and headers.
func smtpYAML(host string, headers ...[2]string) string {
	y := "smtpConfigurations:\n  - name: Relay\n    host: " + host + "\n    port: 25\n"
	if len(headers) > 0 {
		y += "    headers:\n"
		for _, hd := range headers {
			y += "      - key: " + hd[0] + "\n        value: \"" + hd[1] + "\"\n"
		}
	}
	return y
}

// reconcileSMTP runs the smtp reconcile the way applyUpdate does: build, conflict
// pass, then add and update.
func (h *regFixture) reconcileSMTP(
	dataYAML string,
	installed func(string) ([]*model.SMTPHeader, bool),
	resolutions map[string]string,
) (*InstallResult, error) {
	h.t.Helper()
	docs, err := contentDocs(yamlPackage("ns/smtp", "1.0.0", dataYAML, nil))
	if err != nil {
		h.t.Fatalf("docs: %v", err)
	}
	resources, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, h.ipID)
	if err != nil {
		h.t.Fatalf("resources: %v", err)
	}
	undo := &updateUndo{}
	items, currents := h.s.buildSMTPReconcile(h.ctx, h.session, docs, map[string]string{}, resources, undo, installed)
	conflicts := reconcileConflicts("smtp", items, currents)
	if unresolved := unresolvedConflicts(conflicts, resolutions); len(unresolved) > 0 {
		return nil, ErrUpdateConflicts{Conflicts: unresolved}
	}
	result := &InstallResult{}
	if aerr := h.s.reconcileAddUpdate(
		h.ctx, h.session, h.ipID, "smtp", items, currents, resolutions, "{}", result, &result.Others, undo,
	); aerr != nil {
		undo.run()
		return nil, aerr
	}
	return result, nil
}

func (h *regFixture) smtpRow() (*uuid.UUID, *database.InstalledResource) {
	h.t.Helper()
	rows, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, h.ipID)
	if err != nil || len(rows) != 1 {
		h.t.Fatalf("expected one smtp resource, got %d (%v)", len(rows), err)
	}
	return rows[0].ResourceID, rows[0]
}

func (h *regFixture) smtpHeaders(id *uuid.UUID) map[string]string {
	h.t.Helper()
	cur, err := h.s.SMTPConfigurationRepository.GetByID(
		h.ctx, id, &repository.SMTPConfigurationOption{WithHeaders: true},
	)
	if err != nil {
		h.t.Fatalf("get smtp: %v", err)
	}
	out := map[string]string{}
	for _, hd := range cur.Headers {
		out[optStr(hd.Key)] = optStr(hd.Value)
	}
	return out
}

// editSMTPHeader replaces the one header on the configuration, like a user edit.
func (h *regFixture) editSMTPHeader(id *uuid.UUID, key, value string) {
	h.t.Helper()
	k, _ := vo.NewString127(key)
	v, _ := vo.NewString255(value)
	if err := h.s.setSMTPHeaders(h.ctx, h.session, id, []*model.SMTPHeader{{Key: nn(*k), Value: nn(*v)}}); err != nil {
		h.t.Fatalf("edit header: %v", err)
	}
}

func isConflict(err error) bool {
	var uc ErrUpdateConflicts
	return errors.As(err, &uc)
}

// TestSMTPHeaderEditRaisesConflict: a user edited header is a local edit, so an
// upstream change raises a conflict, keep leaves the edit alone and overwrite
// applies upstream.
func TestSMTPHeaderEditRaisesConflict(t *testing.T) {
	h := newRegFixture(t)
	h.withSMTP()
	if _, err := h.reconcileSMTP(smtpYAML("mail.example.test", [2]string{"X-Tag", "v1"}), nil, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	id, _ := h.smtpRow()
	h.editSMTPHeader(id, "X-Tag", "mine")

	v2 := smtpYAML("mail.example.test", [2]string{"X-Tag", "v2"})
	if _, err := h.reconcileSMTP(v2, nil, nil); !isConflict(err) {
		t.Fatalf("expected a conflict for an edited header, got %v", err)
	}
	if got := h.smtpHeaders(id)["X-Tag"]; got != "mine" {
		t.Fatalf("header changed before a resolution: %q", got)
	}
	res, err := h.reconcileSMTP(v2, nil, map[string]string{"smtp:Relay": resolveKeep})
	if err != nil {
		t.Fatalf("keep: %v", err)
	}
	if got := h.smtpHeaders(id)["X-Tag"]; got != "mine" {
		t.Fatalf("keep replaced the edited header: %q", got)
	}
	if !containsSuffix(res.Skipped, "kept (local edit)") {
		t.Fatalf("keep not reported: %+v", res.Skipped)
	}
	if _, err := h.reconcileSMTP(v2, nil, map[string]string{"smtp:Relay": resolveOverwrite}); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got := h.smtpHeaders(id)["X-Tag"]; got != "v2" {
		t.Fatalf("overwrite did not apply upstream: %q", got)
	}

	// an unedited configuration updates silently.
	if _, err := h.reconcileSMTP(smtpYAML("mail.example.test", [2]string{"X-Tag", "v3"}), nil, nil); err != nil {
		t.Fatalf("silent update: %v", err)
	}
	if got := h.smtpHeaders(id)["X-Tag"]; got != "v3" {
		t.Fatalf("silent update not applied: %q", got)
	}
}

// TestSMTPLegacyBaselineHeaders covers a baseline recorded over the fields only:
// headers equal to what the installed version declared are unedited, while
// edited headers or an unreadable installed version raise a conflict.
func TestSMTPLegacyBaselineHeaders(t *testing.T) {
	h := newRegFixture(t)
	h.withSMTP()
	if _, err := h.reconcileSMTP(smtpYAML("mail.example.test", [2]string{"X-Tag", "v1"}), nil, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	id, row := h.smtpRow()
	cur, err := h.s.SMTPConfigurationRepository.GetByID(
		h.ctx, id, &repository.SMTPConfigurationOption{WithHeaders: true},
	)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	legacy := smtpFieldsFingerprint(cur)
	setBaseline := func() {
		t.Helper()
		if err := h.db.Model(&database.InstalledResource{}).
			Where("id = ?", row.ID).
			Update("installed_content_hash", legacy).Error; err != nil {
			t.Fatalf("set baseline: %v", err)
		}
	}
	setBaseline()

	k, _ := vo.NewString127("X-Tag")
	v1, _ := vo.NewString255("v1")
	declaredV1 := func(string) ([]*model.SMTPHeader, bool) {
		return []*model.SMTPHeader{{Key: nn(*k), Value: nn(*v1)}}, true
	}
	unknown := func(string) ([]*model.SMTPHeader, bool) { return nil, false }
	v2 := smtpYAML("mail.example.test", [2]string{"X-Tag", "v2"})

	// installed version unreadable: the headers cannot be shown unedited.
	if _, err := h.reconcileSMTP(v2, unknown, nil); !isConflict(err) {
		t.Fatalf("expected a conflict when the installed version is unknown, got %v", err)
	}

	// user edited the header: conflict, nothing changed.
	h.editSMTPHeader(id, "X-Tag", "mine")
	if _, err := h.reconcileSMTP(v2, declaredV1, nil); !isConflict(err) {
		t.Fatalf("expected a conflict for an edited header, got %v", err)
	}
	if got := h.smtpHeaders(id)["X-Tag"]; got != "mine" {
		t.Fatalf("edited header replaced: %q", got)
	}

	// headers as the installed version declared them: unedited, applied silently.
	h.editSMTPHeader(id, "X-Tag", "v1")
	setBaseline()
	if _, err := h.reconcileSMTP(v2, declaredV1, nil); err != nil {
		t.Fatalf("unedited legacy update: %v", err)
	}
	if got := h.smtpHeaders(id)["X-Tag"]; got != "v2" {
		t.Fatalf("unedited legacy headers not updated: %q", got)
	}
}

// withReportTemplates wires the report template service and table.
func (h *regFixture) withReportTemplates() *repository.ReportTemplate {
	h.t.Helper()
	if err := h.db.AutoMigrate(&database.ReportTemplate{}); err != nil {
		h.t.Fatalf("migrate: %v", err)
	}
	if err := (&database.ReportTemplate{}).Migrate(h.db); err != nil {
		h.t.Fatalf("migrate indexes: %v", err)
	}
	repo := &repository.ReportTemplate{DB: h.db}
	h.s.ReportTemplate = &ReportTemplate{Common: h.s.Common, ReportTemplateRepository: repo}
	return repo
}

func reportTemplatePkg(training bool) *registry.VerifiedPackage {
	return reportTemplatePkgContent(training, "<html>pkg</html>")
}

func reportTemplatePkgContent(training bool, content string) *registry.VerifiedPackage {
	flag := "false"
	if training {
		flag = "true"
	}
	return yamlPackage(
		"ns/report", "1.0.0",
		"reportTemplates:\n  - name: Report\n    file: report.html\n    isTraining: "+flag+"\n",
		map[string]string{"report.html": content},
	)
}

func isValidation(err error) bool {
	var ve errs.ValidationError
	return errors.As(err, &ve)
}

func isOverwrite(err error) (ErrReportTemplateOverwrite, bool) {
	var ow ErrReportTemplateOverwrite
	ok := errors.As(err, &ow)
	return ow, ok
}

// seedReportTemplates inserts the two global report templates the way startup
// seeding does and returns their ids.
func (h *regFixture) seedReportTemplates(
	repo *repository.ReportTemplate, simulation, training string,
) (*uuid.UUID, *uuid.UUID) {
	h.t.Helper()
	ids := make([]*uuid.UUID, 2)
	for i, c := range []struct {
		content  string
		training bool
	}{{simulation, false}, {training, true}} {
		m, err := buildReportTemplateModel(c.content, c.training)
		if err != nil {
			h.t.Fatal(err)
		}
		id, err := repo.Insert(h.ctx, m)
		if err != nil {
			h.t.Fatalf("seed report template: %v", err)
		}
		ids[i] = id
	}
	return ids[0], ids[1]
}

// reportTemplateState reads a report template's content and kind.
func (h *regFixture) reportTemplateState(repo *repository.ReportTemplate, id *uuid.UUID) (string, bool) {
	h.t.Helper()
	cur, err := repo.GetByID(h.ctx, id, &repository.ReportTemplateOption{})
	if err != nil {
		h.t.Fatalf("get report template: %v", err)
	}
	return optStr(cur.Content), cur.IsTraining.MustGet()
}

// reportTemplateRow returns the single report template provenance row of an install.
func (h *regFixture) reportTemplateRow(ipID *uuid.UUID) *database.InstalledResource {
	h.t.Helper()
	rows, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, ipID)
	if err != nil {
		h.t.Fatalf("resources: %v", err)
	}
	var out *database.InstalledResource
	for _, r := range rows {
		if r.ResourceType == "reporttemplate" {
			if out != nil {
				h.t.Fatalf("more than one report template row")
			}
			out = r
		}
	}
	return out
}

// TestReportTemplateOverwriteNeedsConfirm: installing a report template package
// over the global template of its kind stops with the overwrite error and writes
// nothing until that kind is confirmed; confirmed, it overwrites only that kind
// in place, keeps id and kind, and records the previous content. A second
// package cannot take over a template another install tracks, and uninstall
// writes the previous content back.
func TestReportTemplateOverwriteNeedsConfirm(t *testing.T) {
	h := newRegFixture(t)
	repo := h.withReportTemplates()
	if err := h.db.AutoMigrate(&database.InstalledPackageMember{}); err != nil {
		t.Fatal(err)
	}
	h.insertPackage(h.ipID, "ns/report")
	simID, trainID := h.seedReportTemplates(repo, "<html>sim</html>", "<html>mine</html>")
	pkgs := []*registry.VerifiedPackage{reportTemplatePkg(true)}

	// no confirmation, or only the other kind: 409 naming the training template.
	for _, confirm := range [][]string{nil, {"simulation"}} {
		_, err := h.s.reportTemplateInstallCheck(h.ctx, pkgs, confirm)
		ow, ok := isOverwrite(err)
		if !ok || len(ow.Templates) != 1 {
			t.Fatalf("confirm %v: expected the overwrite error, got %v", confirm, err)
		}
		got := ow.Templates[0]
		if got.Kind != "training" || got.TemplateID != trainID.String() || !got.Customized ||
			got.Name != "Custom training report template" || got.PackageID != "ns/report" {
			t.Fatalf("unexpected overwrite entry %+v", got)
		}
	}
	if c, _ := h.reportTemplateState(repo, trainID); c != "<html>mine</html>" {
		t.Fatalf("template changed without confirmation: %q", c)
	}

	planned, err := h.s.reportTemplateInstallCheck(h.ctx, pkgs, []string{"training"})
	if err != nil || len(planned) != 1 {
		t.Fatalf("confirmed check: %v %+v", err, planned)
	}
	if err := h.s.installEntities(
		h.ctx, h.session, pkgs[0], map[string]string{}, h.ipID, "{}", nil, &InstallResult{},
	); err != nil {
		t.Fatalf("install: %v", err)
	}
	if c, training := h.reportTemplateState(repo, trainID); c != "<html>pkg</html>" || !training {
		t.Fatalf("training template not overwritten in place: %q training=%v", c, training)
	}
	if c, training := h.reportTemplateState(repo, simID); c != "<html>sim</html>" || training {
		t.Fatalf("simulation template touched: %q", c)
	}
	row := h.reportTemplateRow(h.ipID)
	if row == nil || *row.ResourceID != *trainID || row.ReplacedContent == nil ||
		*row.ReplacedContent != "<html>mine</html>" {
		t.Fatalf("provenance does not hold the previous content: %+v", row)
	}
	if row.InstalledContentHash != h.s.storedReportTemplateHash(h.ctx, trainID) {
		t.Fatal("baseline does not match the stored template")
	}

	// a second package cannot take over a template another install tracks.
	if _, err := h.s.reportTemplateInstallCheck(h.ctx, pkgs, []string{"training"}); !isValidation(err) ||
		!strings.Contains(err.Error(), "ns/report") {
		t.Fatalf("expected the managed template to be refused, got %v", err)
	}

	// uninstall writes the previous content back and keeps the template.
	if err := h.s.Remove(h.ctx, h.session, h.ipID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if c, training := h.reportTemplateState(repo, trainID); c != "<html>mine</html>" || !training {
		t.Fatalf("uninstall did not restore the template: %q", c)
	}
	if h.reportTemplateRow(h.ipID) != nil {
		t.Fatal("provenance left after uninstall")
	}
}

// TestReportTemplateFailedInstallRestores: rolling back an install, as a failed
// install does, writes the overwritten content back.
func TestReportTemplateFailedInstallRestores(t *testing.T) {
	h := newRegFixture(t)
	repo := h.withReportTemplates()
	h.insertPackage(h.ipID, "ns/report")
	simID, _ := h.seedReportTemplates(repo, "<html>sim</html>", "<html>train</html>")
	if err := h.s.installEntities(
		h.ctx, h.session, reportTemplatePkg(false), map[string]string{}, h.ipID, "{}", nil, &InstallResult{},
	); err != nil {
		t.Fatalf("install: %v", err)
	}
	if c, _ := h.reportTemplateState(repo, simID); c != "<html>pkg</html>" {
		t.Fatalf("not overwritten: %q", c)
	}
	if !h.s.rollbackInstall(h.ctx, h.session, h.ipID, false) {
		t.Fatal("rollback reported leftovers")
	}
	if c, training := h.reportTemplateState(repo, simID); c != "<html>sim</html>" || training {
		t.Fatalf("rollback did not restore the template: %q", c)
	}
}

// TestReportTemplateBothKinds: one simulation and one training template need both
// kinds confirmed and then overwrite both slots; two of one kind stay refused.
func TestReportTemplateBothKinds(t *testing.T) {
	h := newRegFixture(t)
	repo := h.withReportTemplates()
	h.insertPackage(h.ipID, "ns/report")
	simID, trainID := h.seedReportTemplates(repo, "<html>sim</html>", "<html>train</html>")
	both := yamlPackage(
		"ns/report", "1.0.0",
		"reportTemplates:\n  - name: Sim\n    file: s.html\n  - name: Train\n    file: t.html\n    isTraining: true\n",
		map[string]string{"s.html": "<html>S</html>", "t.html": "<html>T</html>"},
	)
	pkgs := []*registry.VerifiedPackage{both}
	_, err := h.s.reportTemplateInstallCheck(h.ctx, pkgs, []string{"training"})
	if ow, ok := isOverwrite(err); !ok || len(ow.Templates) != 2 {
		t.Fatalf("expected both kinds listed, got %v", err)
	}
	if _, err := h.s.reportTemplateInstallCheck(h.ctx, pkgs, []string{"simulation", "training"}); err != nil {
		t.Fatalf("both confirmed: %v", err)
	}
	if err := h.s.installEntities(
		h.ctx, h.session, both, map[string]string{}, h.ipID, "{}", nil, &InstallResult{},
	); err != nil {
		t.Fatalf("install: %v", err)
	}
	if c, tr := h.reportTemplateState(repo, simID); c != "<html>S</html>" || tr {
		t.Fatalf("simulation slot: %q %v", c, tr)
	}
	if c, tr := h.reportTemplateState(repo, trainID); c != "<html>T</html>" || !tr {
		t.Fatalf("training slot: %q %v", c, tr)
	}

	two := yamlPackage(
		"ns/report", "1.0.0",
		"reportTemplates:\n  - name: A\n    file: a.html\n  - name: B\n    file: b.html\n",
		map[string]string{"a.html": "a", "b.html": "b"},
	)
	if _, err := h.s.reportTemplateInstallCheck(
		h.ctx, []*registry.VerifiedPackage{two}, []string{"simulation", "training"},
	); !isValidation(err) {
		t.Fatalf("expected two of one kind to be refused, got %v", err)
	}
	// keep both would need a second template of the kind.
	if err := validateResolutions(map[string]string{"reporttemplate:Report": resolveBoth}); err == nil {
		t.Fatal("keep both accepted for a report template")
	}
	// the create backstop maps a unique error to a validation error, not a 500.
	if _, err := h.s.createReportTemplate(h.ctx, h.session, "<html>x</html>", true); !isValidation(err) {
		t.Fatalf("expected a validation error from create, got %v", err)
	}
}

// TestReportTemplateUpdate: an update that adds a report template needs the kind
// confirmed and overwrites in place; a later version that changes the template
// after a local edit raises the normal conflict, and a failed update writes the
// previous content back.
func TestReportTemplateUpdate(t *testing.T) {
	h := newRegFixture(t)
	repo := h.withReportTemplates()
	ip := h.withUpdateDeps("ns/report")
	simID, _ := h.seedReportTemplates(repo, "<html>sim</html>", "<html>train</html>")
	apply := func(pkg *registry.VerifiedPackage, resolutions map[string]string, confirm []string) error {
		ae := NewAuditEvent("Registry.PackageUpdate", h.session)
		_, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, pkg, nil, nil, resolutions, confirm)
		return err
	}

	v1 := reportTemplatePkgContent(false, "<html>v1</html>")
	if _, ok := isOverwrite(apply(v1, nil, nil)); !ok {
		t.Fatal("expected the overwrite error for a new report template")
	}
	if c, _ := h.reportTemplateState(repo, simID); c != "<html>sim</html>" {
		t.Fatalf("template changed without confirmation: %q", c)
	}
	if err := apply(v1, nil, []string{"simulation"}); err != nil {
		t.Fatalf("confirmed update: %v", err)
	}
	if c, tr := h.reportTemplateState(repo, simID); c != "<html>v1</html>" || tr {
		t.Fatalf("update did not overwrite in place: %q", c)
	}
	row := h.reportTemplateRow(h.ipID)
	if row == nil || row.ReplacedContent == nil || *row.ReplacedContent != "<html>sim</html>" {
		t.Fatalf("update provenance lacks the previous content: %+v", row)
	}

	// a local edit plus an upstream change is a conflict; nothing changes.
	edited, _ := buildReportTemplateModel("<html>local</html>", false)
	if err := repo.UpdateByID(h.ctx, simID, edited); err != nil {
		t.Fatal(err)
	}
	v2 := reportTemplatePkgContent(false, "<html>v2</html>")
	if err := apply(v2, nil, nil); !isConflict(err) {
		t.Fatalf("expected a conflict for the edited template, got %v", err)
	}
	if c, _ := h.reportTemplateState(repo, simID); c != "<html>local</html>" {
		t.Fatalf("edited template changed before a resolution: %q", c)
	}
	if err := apply(v2, map[string]string{"reporttemplate:Report": resolveOverwrite}, nil); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if c, _ := h.reportTemplateState(repo, simID); c != "<html>v2</html>" {
		t.Fatalf("overwrite not applied: %q", c)
	}
	// the previous content survives the update for uninstall.
	if row := h.reportTemplateRow(h.ipID); row == nil || row.ReplacedContent == nil ||
		*row.ReplacedContent != "<html>sim</html>" {
		t.Fatalf("previous content lost on update: %+v", row)
	}

	// a version that drops the template writes the previous content back.
	if err := apply(yamlPackage("ns/report", "1.0.2", "pages: []\n", nil), nil, nil); err != nil {
		t.Fatalf("drop update: %v", err)
	}
	if c, _ := h.reportTemplateState(repo, simID); c != "<html>sim</html>" {
		t.Fatalf("dropped template not restored: %q", c)
	}
	if h.reportTemplateRow(h.ipID) != nil {
		t.Fatal("provenance left for a dropped template")
	}

	// undo after an overwrite in an update writes the previous content back.
	undo := &updateUndo{}
	pkg := reportTemplatePkgContent(false, "<html>v3</html>")
	docs, err := contentDocs(pkg)
	if err != nil {
		t.Fatal(err)
	}
	items, currents := h.s.buildReportTemplateReconcile(h.ctx, h.session, pkg, docs, map[string]string{}, nil)
	var report []string
	if err := h.s.reconcileAddUpdate(
		h.ctx, h.session, h.ipID, "reporttemplate", items, currents, nil, "{}", &InstallResult{}, &report, undo,
	); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if c, _ := h.reportTemplateState(repo, simID); c != "<html>v3</html>" {
		t.Fatalf("not overwritten: %q", c)
	}
	undo.run()
	if c, _ := h.reportTemplateState(repo, simID); c != "<html>sim</html>" || undo.failures != 0 {
		t.Fatalf("undo did not restore the template: %q (failures %d)", c, undo.failures)
	}
	if h.reportTemplateRow(h.ipID) != nil {
		t.Fatal("undo left provenance")
	}
}

// TestInstallerDomainValidation: the installer refuses a domain name that is not
// a plain hostname or equals an admin host, and a redirect url that is not http
// or https, both at create and in the update plan.
func TestInstallerDomainValidation(t *testing.T) {
	h := newRegFixture(t)
	h.s.Domain = &Domain{AdminHosts: func() []string { return []string{"admin.example.test", "0.0.0.0"} }}

	cases := []struct {
		name, redirect, want string
	}{
		{"admin.example.test", "", "reserved"},
		{"ADMIN.Example.test", "", "reserved"},
		{"bad_name.example.test", "", "letters"},
		{"*.example.test", "", "letters"},
		{"shop.example.test/x", "", "letters"},
		{"shop.example.test", "javascript:alert(1)", "http"},
		{"shop.example.test", "ftp://files.example.test", "http"},
		{"shop.example.test", "//evil.example.test", "redirectURL"},
	}
	for _, c := range cases {
		_, err := h.s.createDomain(h.ctx, h.session, c.name, false, false, false, false, "", "", c.redirect, "", "")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("create %q redirect %q: expected error with %q, got %v", c.name, c.redirect, c.want, err)
		}
		if err != nil && !isValidation(err) {
			t.Errorf("create %q: expected a validation error, got %T", c.name, err)
		}
	}

	// update plan: the reserved name and the bad redirect fail the preflight.
	for _, c := range []struct{ name, redirect string }{
		{"admin.example.test", "https://ok.example.test"},
		{"shop.example.test", "javascript:alert(1)"},
	} {
		dy := "domains:\n  - name: " + c.name + "\n    redirectURL: \"" + c.redirect + "\"\n"
		pkg := yamlPackage("ns/domain", "1.0.0", dy, nil)
		docs, err := contentDocs(pkg)
		if err != nil {
			t.Fatal(err)
		}
		items, _ := h.s.buildDomainReconcile(h.ctx, h.session, pkg, docs, map[string]string{}, nil)
		if len(items) != 1 || items[0].preflight == nil {
			t.Fatalf("%s: expected a failing preflight", c.name)
		}
		if err := items[0].preflight(declaredNames{}); err == nil {
			t.Fatalf("%s: preflight accepted", c.name)
		}
	}

	// a valid name and an https redirect build.
	if _, err := buildDomainModel(
		"shop.example.test", false, false, false, false, "", "",
		"https://ok.example.test/x", "", "", "admin.example.test",
	); err != nil {
		t.Fatalf("valid domain refused: %v", err)
	}
}

// TestAuditFailureRedactsError: the audit event records an error class, never
// the error text that can carry a substituted secret.
func TestAuditFailureRedactsError(t *testing.T) {
	h := newRegFixture(t)
	const secret = "s3cr3t-token-value"
	ae := NewAuditEvent("Registry.Install", h.session)
	err := errs.NewValidationError(errors.New("header Authorization value Bearer " + secret + " is invalid"))
	h.s.auditFailure(ae, &err)
	for k, v := range ae.Details {
		if s, ok := v.(string); ok && strings.Contains(s, secret) {
			t.Fatalf("audit detail %q leaks the secret", k)
		}
	}
	if ae.Details["errorClass"] != "validation" {
		t.Fatalf("expected errorClass validation, got %v", ae.Details["errorClass"])
	}
	if _, ok := ae.Details["error"]; ok {
		t.Fatal("audit detail still holds the error text")
	}
	if got := auditErrorClass(errors.New("boom " + secret)); got != "internal" {
		t.Fatalf("unexpected class %q", got)
	}
}
