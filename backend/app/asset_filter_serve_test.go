package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/ipdata"
	"github.com/phishingclub/phishingclub/middleware"
	"go.uber.org/zap"
)

const (
	// visitor addresses used by the tests
	ipPlain   = "192.0.2.1"    // no country, no ASN
	ipOffice  = "10.1.2.3"     // inside 10.0.0.0/8
	ipGermany = "198.51.100.7" // country DE in the test geo data
	ipASN     = "203.0.113.9"  // AS64500 in the test ASN data
)

// filterList is the content of an allow deny list in a test
type filterList struct {
	cidrs, ja4, countries, asns, headers string
}

func (f *assetServeFixture) list(name string, allowed bool, companyID *uuid.UUID, l filterList) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if err := f.db.Create(&database.AllowDeny{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, CompanyID: companyID, Name: name,
		Cidrs: l.cidrs, JA4Fingerprints: l.ja4, CountryCodes: l.countries, Asns: l.asns,
		Headers: l.headers, Allowed: allowed,
	}).Error; err != nil {
		f.t.Fatalf("insert list: %v", err)
	}
	return id
}

func (f *assetServeFixture) link(assetID uuid.UUID, listIDs ...uuid.UUID) {
	f.t.Helper()
	for i := range listIDs {
		if err := f.db.Create(&database.AssetAllowDeny{AssetID: &assetID, AllowDenyID: &listIDs[i]}).Error; err != nil {
			f.t.Fatalf("link: %v", err)
		}
	}
}

// visit runs the asset handler as a visitor from ip with an optional JA4
// fingerprint and request headers
func (f *assetServeFixture) visit(
	method, host string,
	domainID *uuid.UUID,
	path, ip, ja4 string,
	headers http.Header,
) (*httpRecorder, assetServeResult) {
	f.t.Helper()
	w, res := f.request(method, host, domainID, path, nil, func(c *gin.Context) {
		c.Request.RemoteAddr = ip + ":40000"
		for k, vs := range headers {
			for _, v := range vs {
				c.Request.Header.Add(k, v)
			}
		}
		if ja4 != "" {
			c.Set(middleware.ContextKeyJA4, ja4)
		}
	})
	return &httpRecorder{w.Code, w.Header(), w.Body.String()}, res
}

type httpRecorder struct {
	code   int
	header http.Header
	body   string
}

// writeIPPackage writes a downloaded ip data package of the kind
func writeIPPackage(t *testing.T, root, kind string, entries []byte) {
	t.Helper()
	dir := filepath.Join(root, "ipdata", kind)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(entries)
	info, _ := json.Marshal(map[string]any{
		"format": 1, "name": kind, "version": "test", "created": "2026-01-01T00:00:00Z",
		"source": "test", "license": "CC0-1.0", "entries": 1,
		"content_hash": hex.EncodeToString(sum[:]),
	})
	if err := os.WriteFile(filepath.Join(dir, "package.json"), info, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "entries.json"), entries, 0o640); err != nil {
		t.Fatal(err)
	}
}

// useTestIPData loads small country and ASN data sets into the ip data store
func useTestIPData(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	writeIPPackage(t, root, ipdata.KindGeoIP, []byte(
		`[{"code":"DE","name":"Germany","ipv4":["198.51.100.0/24"],"ipv6":[]}]`,
	))
	writeIPPackage(t, root, ipdata.KindASN, []byte(
		`[{"asn":64500,"handle":"TEST","name":"Test","country":"US","ipv4":["203.0.113.0/24"],"ipv6":[]}]`,
	))
	ipdata.Init(root, zap.NewNop().Sugar())
	if c, _ := ipdata.Get().LookupCountry(ipGermany); c != "DE" {
		t.Fatalf("test geo data not loaded: %q", c)
	}
}

func assertServedFiltered(t *testing.T, name string, rec *httpRecorder, res assetServeResult, body string) {
	t.Helper()
	if res != assetServed || rec.code != http.StatusOK || rec.body != body {
		t.Fatalf("%s: want served, got %v %d %q", name, res, rec.code, rec.body)
	}
	if got := rec.header.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("%s: cache control = %q", name, got)
	}
}

// assertDenied checks the request was denied and nothing was written, so the
// handler can continue as if there was no file
func assertDenied(t *testing.T, name string, rec *httpRecorder, res assetServeResult) {
	t.Helper()
	if res != assetDenied {
		t.Fatalf("%s: want denied, got %v", name, res)
	}
	if rec.body != "" || len(rec.header) != 0 {
		t.Fatalf("%s: denied request wrote a response: %v %q", name, rec.header, rec.body)
	}
}

// TestServeFilteredAssetMatrix checks each kind of rule in allow and deny
// lists against a matching and a not matching visitor
func TestServeFilteredAssetMatrix(t *testing.T) {
	useTestIPData(t)
	headerRule := `[{"keyRegex":"(?i)^user-agent$","valueRegex":"(?i)scanbot"}]`
	botUA := http.Header{"User-Agent": {"ScanBot/1.0"}}
	browserUA := http.Header{"User-Agent": {"Mozilla/5.0"}}
	ja4 := "t13d1516h2_8daaf6152771_02713d6af862"

	type visitor struct {
		ip      string
		ja4     string
		headers http.Header
	}
	tests := []struct {
		name     string
		list     filterList
		matching visitor
		other    visitor
	}{
		{"cidr", filterList{cidrs: "10.0.0.0/8"}, visitor{ip: ipOffice}, visitor{ip: ipPlain}},
		{"country", filterList{countries: "DE"}, visitor{ip: ipGermany}, visitor{ip: ipPlain}},
		{"asn", filterList{asns: "AS64500"}, visitor{ip: ipASN}, visitor{ip: ipPlain}},
		{"ja4", filterList{ja4: "t13d*"}, visitor{ip: ipPlain, ja4: ja4}, visitor{ip: ipPlain}},
		{"header", filterList{headers: headerRule}, visitor{ip: ipPlain, headers: botUA}, visitor{ip: ipPlain, headers: browserUA}},
	}
	for _, tt := range tests {
		for _, allowed := range []bool{true, false} {
			f := newAssetServeFixture(t)
			domainID := uuid.New()
			f.file("a.test/f.txt", "file")
			assetID := f.asset(&domainID, nil, "f.txt", "")
			f.link(assetID, f.list(tt.name, allowed, nil, tt.list))

			name := tt.name + " deny"
			if allowed {
				name = tt.name + " allow"
			}
			m, o := tt.matching, tt.other
			recM, resM := f.visit(http.MethodGet, "a.test", &domainID, "/f.txt", m.ip, m.ja4, m.headers)
			recO, resO := f.visit(http.MethodGet, "a.test", &domainID, "/f.txt", o.ip, o.ja4, o.headers)
			if allowed {
				assertServedFiltered(t, name+" matching", recM, resM, "file")
				assertDenied(t, name+" other", recO, resO)
			} else {
				assertDenied(t, name+" matching", recM, resM)
				assertServedFiltered(t, name+" other", recO, resO, "file")
			}
		}
	}
}

// TestServeFilteredAssetSeveralLists checks several lists on one asset: a
// visitor is denied by any deny list, and passes when it matches any allow list
func TestServeFilteredAssetSeveralLists(t *testing.T) {
	useTestIPData(t)
	f := newAssetServeFixture(t)
	domainID := uuid.New()
	f.file("a.test/deny.txt", "d")
	f.file("a.test/allow.txt", "a")
	deny := f.asset(&domainID, nil, "deny.txt", "")
	f.link(deny,
		f.list("d1", false, nil, filterList{cidrs: "10.0.0.0/8"}),
		f.list("d2", false, nil, filterList{countries: "DE"}),
	)
	allow := f.asset(&domainID, nil, "allow.txt", "")
	f.link(allow,
		f.list("a1", true, nil, filterList{cidrs: "10.0.0.0/8"}),
		f.list("a2", true, nil, filterList{asns: "64500"}),
	)
	for _, ip := range []string{ipOffice, ipGermany} {
		rec, res := f.visit(http.MethodGet, "a.test", &domainID, "/deny.txt", ip, "", nil)
		assertDenied(t, "deny "+ip, rec, res)
	}
	rec, res := f.visit(http.MethodGet, "a.test", &domainID, "/deny.txt", ipASN, "", nil)
	assertServedFiltered(t, "deny other", rec, res, "d")
	for _, ip := range []string{ipOffice, ipASN} {
		rec, res := f.visit(http.MethodGet, "a.test", &domainID, "/allow.txt", ip, "", nil)
		assertServedFiltered(t, "allow "+ip, rec, res, "a")
	}
	rec, res = f.visit(http.MethodGet, "a.test", &domainID, "/allow.txt", ipGermany, "", nil)
	assertDenied(t, "allow other", rec, res)
}

// TestServeFilteredAssetHeaders checks the cache header of a filtered asset,
// that a custom Cache-Control replaces it, and that unfiltered assets get no
// cache header
func TestServeFilteredAssetHeaders(t *testing.T) {
	f := newAssetServeFixture(t)
	domainID := uuid.New()
	f.file("a.test/custom.txt", "c")
	f.file("a.test/plain.txt", "p")
	custom := f.asset(&domainID, nil, "custom.txt", "text/x-custom")
	f.header(custom, "Cache-Control", "public, max-age=60")
	f.header(custom, "X-Extra", "e")
	f.link(custom, f.list("deny office", false, nil, filterList{cidrs: "10.0.0.0/8"}))
	f.asset(&domainID, nil, "plain.txt", "")

	rec, res := f.visit(http.MethodGet, "a.test", &domainID, "/custom.txt", ipPlain, "", nil)
	if res != assetServed || rec.header.Values("Cache-Control")[0] != "public, max-age=60" ||
		len(rec.header.Values("Cache-Control")) != 1 ||
		rec.header.Get("Content-Type") != "text/x-custom" || rec.header.Get("X-Extra") != "e" {
		t.Fatalf("custom served with %v %v", res, rec.header)
	}
	rec, res = f.visit(http.MethodGet, "a.test", &domainID, "/custom.txt", ipOffice, "", nil)
	assertDenied(t, "custom denied", rec, res)

	rec, res = f.visit(http.MethodGet, "a.test", &domainID, "/plain.txt", ipOffice, "", nil)
	if res != assetServed || rec.header.Get("Cache-Control") != "" {
		t.Fatalf("unfiltered asset served with %v %v", res, rec.header)
	}
}

// TestServeDeniedDomainAssetDoesNotUseShared checks a denied domain asset does
// not fall back to a shared file at the same path
func TestServeDeniedDomainAssetDoesNotUseShared(t *testing.T) {
	f := newAssetServeFixture(t)
	domainID := uuid.New()
	f.file("a.test/x.txt", "domain")
	f.file("shared/x.txt", "shared")
	id := f.asset(&domainID, nil, "x.txt", "")
	f.asset(nil, nil, "x.txt", "")
	f.link(id, f.list("deny office", false, nil, filterList{cidrs: "10.0.0.0/8"}))

	rec, res := f.visit(http.MethodGet, "a.test", &domainID, "/x.txt", ipOffice, "", nil)
	assertDenied(t, "domain denied", rec, res)
	rec, res = f.visit(http.MethodGet, "a.test", &domainID, "/x.txt", ipPlain, "", nil)
	assertServedFiltered(t, "domain allowed", rec, res, "domain")
	// another domain without the file gets the shared file
	other := uuid.New()
	rec, res = f.visit(http.MethodGet, "b.test", &other, "/x.txt", ipOffice, "", nil)
	if res != assetServed || rec.body != "shared" {
		t.Fatalf("shared file on other domain: %v %q", res, rec.body)
	}
}

// TestServeDeniedSharedAsset checks filters on global and company folder
// assets, and that the lists of every row stored for a file apply
func TestServeDeniedSharedAsset(t *testing.T) {
	f := newAssetServeFixture(t)
	domainID := uuid.New()
	companyID := uuid.New()
	if err := f.db.Create(&database.Company{
		ID: companyID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "c", AssetsKey: "ckey",
	}).Error; err != nil {
		t.Fatalf("insert company: %v", err)
	}
	f.file("shared/g.txt", "global")
	f.file("shared/ckey/doc.txt", "company")
	global := f.asset(nil, nil, "g.txt", "")
	f.link(global, f.list("deny office", false, nil, filterList{cidrs: "10.0.0.0/8"}))

	rec, res := f.visit(http.MethodGet, "a.test", &domainID, "/g.txt", ipOffice, "", nil)
	assertDenied(t, "global denied", rec, res)
	rec, res = f.visit(http.MethodGet, "a.test", &domainID, "/g.txt", ipPlain, "", nil)
	assertServedFiltered(t, "global allowed", rec, res, "global")

	// company row without lists, global row for the same file with a deny list
	company := f.asset(nil, &companyID, "doc.txt", "text/x-company")
	f.header(company, "X-Company", "c")
	collide := f.asset(nil, nil, "ckey/doc.txt", "text/x-global")
	f.link(collide, f.list("deny germany", false, nil, filterList{cidrs: "198.51.100.0/24"}))
	rec, res = f.visit(http.MethodGet, "a.test", &domainID, "/ckey/doc.txt", ipGermany, "", nil)
	assertDenied(t, "collision denied by global row", rec, res)
	rec, res = f.visit(http.MethodGet, "a.test", &domainID, "/ckey/doc.txt", ipPlain, "", nil)
	assertServedFiltered(t, "collision allowed", rec, res, "company")
	if rec.header.Get("Content-Type") != "text/x-company" || rec.header.Get("X-Company") != "c" {
		t.Fatalf("collision did not use company settings: %v", rec.header)
	}
	// a list on the company row applies too
	f.link(company, f.list("company deny", false, &companyID, filterList{cidrs: "192.0.2.0/24"}))
	rec, res = f.visit(http.MethodGet, "a.test", &domainID, "/ckey/doc.txt", ipPlain, "", nil)
	assertDenied(t, "collision denied by company row", rec, res)
	rec, res = f.visit(http.MethodGet, "a.test", &domainID, "/ckey/doc.txt", ipOffice, "", nil)
	assertServedFiltered(t, "collision allowed by both", rec, res, "company")
}

// TestServeFilteredAssetAllMethods checks every method is filtered
func TestServeFilteredAssetAllMethods(t *testing.T) {
	f := newAssetServeFixture(t)
	domainID := uuid.New()
	f.file("a.test/m.txt", "m")
	f.file("shared/s.txt", "s")
	id := f.asset(&domainID, nil, "m.txt", "")
	f.link(id, f.list("deny office", false, nil, filterList{cidrs: "10.0.0.0/8"}))
	shared := f.asset(nil, nil, "s.txt", "")
	f.link(shared, f.list("deny office shared", false, nil, filterList{cidrs: "10.0.0.0/8"}))
	methods := []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions, "PROPFIND",
	}
	for _, method := range methods {
		for _, p := range []string{"/m.txt", "/s.txt"} {
			rec, res := f.visit(method, "a.test", &domainID, p, ipOffice, "", nil)
			assertDenied(t, method+" "+p, rec, res)
			_, res = f.visit(method, "a.test", &domainID, p, ipPlain, "", nil)
			if res != assetServed {
				t.Fatalf("%s %s allowed visitor not served: %v", method, p, res)
			}
		}
	}
}

// TestServeFilteredAssetPathVariants checks other spellings of a filtered
// path are denied or not found, never served
func TestServeFilteredAssetPathVariants(t *testing.T) {
	f := newAssetServeFixture(t)
	domainID := uuid.New()
	f.file("a.test/dir/x.txt", "domain")
	f.file("shared/dir/x.txt", "shared")
	f.file("shared/g/y.txt", "global")
	id := f.asset(&domainID, nil, "dir/x.txt", "")
	f.link(id, f.list("deny all", false, nil, filterList{cidrs: "0.0.0.0/0"}))
	g := f.asset(nil, nil, "g/y.txt", "")
	f.link(g, f.list("deny all shared", false, nil, filterList{cidrs: "0.0.0.0/0"}))

	paths := []string{
		"/dir/x.txt", "//dir/x.txt", "/./dir/x.txt", "/dir/./x.txt", "/dir//x.txt",
		"/dir/../dir/x.txt", "/x/../dir/x.txt", "/%2e/dir/x.txt", "/dir/%2e%2e/dir/x.txt",
		"/%2E/dir/x.txt", "/dir/x.txt/", "/DIR/x.txt", "/dir/X.TXT", "/dir%2fx.txt",
		"/g/y.txt", "//g/y.txt", "/g/./y.txt", "/%2e/g/y.txt", "/G/y.txt", "/g/../g/y.txt",
	}
	for _, p := range paths {
		rec, res := f.visit(http.MethodGet, "a.test", &domainID, p, ipPlain, "", nil)
		if res == assetServed || rec.body != "" {
			t.Fatalf("%s was served: %v %q", p, res, rec.body)
		}
	}
}

// TestServeFilteredAssetFailsClosed checks lookup and evaluation errors deny
// the visitor when the file may be filtered
func TestServeFilteredAssetFailsClosed(t *testing.T) {
	t.Run("filter lookup fails", func(t *testing.T) {
		f := newAssetServeFixture(t)
		domainID := uuid.New()
		f.file("a.test/x.txt", "x")
		f.file("shared/s.txt", "s")
		f.asset(&domainID, nil, "x.txt", "")
		if err := f.db.Migrator().DropTable(&database.AssetAllowDeny{}); err != nil {
			t.Fatalf("drop: %v", err)
		}
		// it can not be told if the files are filtered
		for _, p := range []string{"/x.txt", "/s.txt"} {
			rec, res := f.visit(http.MethodGet, "a.test", &domainID, p, ipPlain, "", nil)
			assertDenied(t, p, rec, res)
		}
	})
	t.Run("list load fails", func(t *testing.T) {
		f := newAssetServeFixture(t)
		domainID := uuid.New()
		f.file("a.test/x.txt", "x")
		f.file("a.test/plain.txt", "p")
		id := f.asset(&domainID, nil, "x.txt", "")
		f.asset(&domainID, nil, "plain.txt", "")
		f.link(id, f.list("deny office", false, nil, filterList{cidrs: "10.0.0.0/8"}))
		if err := f.db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
			t.Fatalf("pragma: %v", err)
		}
		if err := f.db.Migrator().DropTable(&database.AllowDeny{}); err != nil {
			t.Fatalf("drop: %v", err)
		}
		rec, res := f.visit(http.MethodGet, "a.test", &domainID, "/x.txt", ipPlain, "", nil)
		assertDenied(t, "filtered", rec, res)
		// a file known to have no lists is still served
		rec, res = f.visit(http.MethodGet, "a.test", &domainID, "/plain.txt", ipPlain, "", nil)
		if res != assetServed || rec.body != "p" {
			t.Fatalf("unfiltered not served: %v %q", res, rec.body)
		}
	})
	t.Run("broken stored rule", func(t *testing.T) {
		f := newAssetServeFixture(t)
		domainID := uuid.New()
		f.file("a.test/x.txt", "x")
		id := f.asset(&domainID, nil, "x.txt", "")
		f.link(id, f.list("broken", false, nil, filterList{headers: "not json"}))
		rec, res := f.visit(http.MethodGet, "a.test", &domainID, "/x.txt", ipPlain, "", http.Header{"X-A": {"1"}})
		assertDenied(t, "broken rule", rec, res)
	})
	t.Run("settings lookup fails", func(t *testing.T) {
		f := newAssetServeFixture(t)
		domainID := uuid.New()
		f.file("a.test/x.txt", "x")
		id := f.asset(&domainID, nil, "x.txt", "")
		f.link(id, f.list("deny office", false, nil, filterList{cidrs: "10.0.0.0/8"}))
		if err := f.db.Migrator().DropTable(&database.AssetHeader{}); err != nil {
			t.Fatalf("drop: %v", err)
		}
		// the filters are still known and applied, the file is served with
		// the default settings
		rec, res := f.visit(http.MethodGet, "a.test", &domainID, "/x.txt", ipOffice, "", nil)
		assertDenied(t, "denied visitor", rec, res)
		rec, res = f.visit(http.MethodGet, "a.test", &domainID, "/x.txt", ipPlain, "", nil)
		assertServedFiltered(t, "allowed visitor", rec, res, "x")
	})
}
