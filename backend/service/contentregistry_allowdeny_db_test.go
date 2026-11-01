package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
)

// withAllowDenyService wires the real AllowDeny service onto the fixture.
func (h *regFixture) withAllowDenyService() {
	h.t.Helper()
	if err := h.db.AutoMigrate(&database.AllowDeny{}, &database.AssetAllowDeny{}); err != nil {
		h.t.Fatalf("migrate: %v", err)
	}
	h.s.AllowDeny = &AllowDeny{
		Common:              h.s.Common,
		AllowDenyRepository: &repository.AllowDeny{DB: h.db},
		CampaignRepository:  &repository.Campaign{DB: h.db},
		AssetRepository:     &repository.Asset{DB: h.db},
	}
}

func (h *regFixture) allowDenyByName(name string) *model.AllowDeny {
	h.t.Helper()
	var row database.AllowDeny
	if err := h.db.Where("name = ?", name).First(&row).Error; err != nil {
		h.t.Fatalf("allow/deny %q not found: %v", name, err)
	}
	m, err := h.s.AllowDeny.AllowDenyRepository.GetByID(h.ctx, row.ID, &repository.AllowDenyOption{})
	if err != nil {
		h.t.Fatalf("get allow/deny %q: %v", name, err)
	}
	return m
}

func cidrStrings(m *model.AllowDeny) []string {
	var out []string
	if nets, err := m.Cidrs.Get(); err == nil {
		for _, n := range nets {
			out = append(out, n.String())
		}
	}
	return out
}

const allowDenyVarYAML = `
allowDenies:
  - name: Office @@SITE@@
    allowed: true
    cidrs: |
      10.0.0.0/8
      @@EXTRA_CIDR@@
    countryCodes: "@@COUNTRY@@"
    headers:
      - keyRegex: "@@HEADER@@"
        valueRegex: ".*"
`

func allowDenyVarPackage(version, dataYAML string) *registry.VerifiedPackage {
	pkg := yamlPackage("ns/filter", version, dataYAML, nil)
	pkg.Manifest.Variables = []registry.Variable{
		{Name: "SITE", Type: "text", Required: true},
		{Name: "EXTRA_CIDR", Type: "text", Required: true},
		{Name: "COUNTRY", Type: "text", Required: true},
		{Name: "HEADER", Type: "text", Required: true},
	}
	return pkg
}

// TestInstallAllowDenySubstitutesVariables installs an allow/deny list whose
// fields hold variable tokens. Every token is replaced with the supplied value,
// so the list stores both CIDRs, the country and the header rule. An update to a
// newer version with the same values raises no false local edit conflict.
func TestInstallAllowDenySubstitutesVariables(t *testing.T) {
	h := newRegFixture(t)
	ip := h.withUpdateDeps("ns/filter")
	h.withAllowDenyService()

	vars := map[string]string{
		"SITE":       "HQ",
		"EXTRA_CIDR": "192.168.0.0/16",
		"COUNTRY":    "DK",
		"HEADER":     "X-Office",
	}
	bound, _ := json.Marshal(vars)
	err := h.s.installEntities(
		h.ctx, h.session, allowDenyVarPackage("1.0.0", allowDenyVarYAML), vars, ip.ID, string(bound), nil, &InstallResult{},
	)
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	m := h.allowDenyByName("Office HQ")
	if got := strings.Join(cidrStrings(m), ","); got != "10.0.0.0/8,192.168.0.0/16" {
		t.Fatalf("cidrs = %q, want both networks", got)
	}
	if got := m.CountryCodes.MustGet(); got != "DK" {
		t.Fatalf("country codes = %q, want DK", got)
	}
	if got := m.Headers.MustGet(); !strings.Contains(got, `"keyRegex":"X-Office"`) {
		t.Fatalf("headers = %q, want substituted key regex", got)
	}

	// same values, one more CIDR: the stored list matches its baseline, so the
	// update applies in place without a conflict.
	v2 := strings.Replace(allowDenyVarYAML, "      @@EXTRA_CIDR@@\n", "      @@EXTRA_CIDR@@\n      172.16.0.0/12\n", 1)
	if _, _, err := h.apply(ip, allowDenyVarPackage("1.1.0", v2), nil); err != nil {
		var uc ErrUpdateConflicts
		if errors.As(err, &uc) {
			t.Fatalf("update raised a false local edit conflict: %+v", uc.Conflicts)
		}
		t.Fatalf("update: %v", err)
	}
	m = h.allowDenyByName("Office HQ")
	if got := strings.Join(cidrStrings(m), ","); got != "10.0.0.0/8,192.168.0.0/16,172.16.0.0/12" {
		t.Fatalf("cidrs after update = %q", got)
	}
}

// TestInstallAllowDenyInvalidCIDR checks that an entry which is not a CIDR fails
// the install with a validation error naming the value instead of being dropped.
func TestInstallAllowDenyInvalidCIDR(t *testing.T) {
	h := newRegFixture(t)
	ip := h.withUpdateDeps("ns/filter")
	h.withAllowDenyService()

	vars := map[string]string{
		"SITE":       "HQ",
		"EXTRA_CIDR": "not-a-cidr",
		"COUNTRY":    "DK",
		"HEADER":     "X-Office",
	}
	err := h.s.installEntities(
		h.ctx, h.session, allowDenyVarPackage("1.0.0", allowDenyVarYAML), vars, ip.ID, "{}", nil, &InstallResult{},
	)
	if err == nil {
		t.Fatal("install with an invalid CIDR succeeded")
	}
	var ve errs.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want a validation error, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "not-a-cidr") {
		t.Fatalf("error does not name the bad value: %v", err)
	}
	var count int64
	h.db.Model(&database.AllowDeny{}).Count(&count)
	if count != 0 {
		t.Fatalf("an allow/deny list was stored: %d", count)
	}
}

// TestBuildAllowDenyModelSkipsBlankLines keeps blank lines out of the list.
func TestBuildAllowDenyModelSkipsBlankLines(t *testing.T) {
	m, err := buildAllowDenyModel("List", false, "\n10.0.0.0/8\n\n  \n", "", "", "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := strings.Join(cidrStrings(m), ","); got != "10.0.0.0/8" {
		t.Fatalf("cidrs = %q", got)
	}
}
