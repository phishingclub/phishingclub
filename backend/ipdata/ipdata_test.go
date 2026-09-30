package ipdata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"go.uber.org/zap"
)

func TestLongestPrefixAndMultiOrigin(t *testing.T) {
	// two countries, one nested prefix to prove longest match wins
	geo := buildGeo([]geoEntry{
		{Code: "US", Name: "United States", IPv4: []string{"8.0.0.0/8"}},
		{Code: "DE", Name: "Germany", IPv4: []string{"8.8.0.0/16"}, IPv6: []string{"2a01:4f8::/32"}},
	})
	s := &Store{geo: geo}

	if code, ok := s.LookupCountry("8.8.8.8"); !ok || code != "DE" {
		t.Fatalf("8.8.8.8 got %q %v, want DE", code, ok)
	}
	if code, ok := s.LookupCountry("8.9.0.1"); !ok || code != "US" {
		t.Fatalf("8.9.0.1 got %q %v, want US", code, ok)
	}
	if code, ok := s.LookupCountry("2a01:4f8::1"); !ok || code != "DE" {
		t.Fatalf("ipv6 got %q %v, want DE", code, ok)
	}
	if _, ok := s.LookupCountry("1.2.3.4"); ok {
		t.Fatal("1.2.3.4 should not match")
	}

	// same prefix announced by two ASNs must return both
	asn := buildASN([]asnEntry{
		{ASN: 64500, Handle: "A", Name: "Alpha", IPv4: []string{"203.0.113.0/24"}},
		{ASN: 64501, Handle: "B", Name: "Beta", IPv4: []string{"203.0.113.0/24"}},
		{ASN: 15169, Handle: "GOOGLE", Name: "Google LLC", IPv4: []string{"8.8.8.0/24"}},
	})
	s.asn = asn

	got := s.LookupASNs("203.0.113.9")
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if !reflect.DeepEqual(got, []uint32{64500, 64501}) {
		t.Fatalf("multi origin got %v, want [64500 64501]", got)
	}
	if got := s.LookupASNs("8.8.8.8"); len(got) != 1 || got[0] != 15169 {
		t.Fatalf("8.8.8.8 asn got %v, want [15169]", got)
	}
	if !s.HasASN(15169) || s.HasASN(1) {
		t.Fatal("HasASN wrong")
	}

	// search and resolve
	if res := s.SearchASN("goog", 10); len(res) != 1 || res[0].ASN != 15169 {
		t.Fatalf("search got %v", res)
	}
	if res := s.ResolveASNs([]uint32{15169, 99999}); len(res) != 1 || res[0].ASN != 15169 {
		t.Fatalf("resolve got %v, want only 15169", res)
	}
}

func TestNilStoreSafe(t *testing.T) {
	var s *Store
	if _, ok := s.LookupCountry("8.8.8.8"); ok {
		t.Fatal("nil store should not match")
	}
	if s.LookupASNs("8.8.8.8") != nil {
		t.Fatal("nil store asn should be nil")
	}
	if s.HasASN(1) {
		t.Fatal("nil store HasASN should be false")
	}
}

func TestLoadPackageAndSearchRanking(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ipdata", "asn")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries := []byte(`[
{"asn":9009,"handle":"M247","name":"M247 Europe SRL","country":"RO","ipv4":["5.62.0.0/16"],"ipv6":[]},
{"asn":329035,"handle":"M247AI-AS-US","name":"M247 LLC","country":"US","ipv4":["23.19.0.0/16"],"ipv6":[]},
{"asn":15169,"handle":"GOOGLE","name":"Google LLC","country":"US","ipv4":["8.8.8.0/24"],"ipv6":[]}
]`)
	sum := sha256.Sum256(entries)
	info := map[string]any{
		"format": 1, "name": "asn", "version": "test", "created": "2026-01-01T00:00:00Z",
		"source": "test", "license": "CC0-1.0", "entries": 3,
		"ipv4_prefixes": 3, "ipv6_prefixes": 0, "content_hash": hex.EncodeToString(sum[:]),
	}
	infoBytes, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), infoBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "entries.json"), entries, 0o644); err != nil {
		t.Fatal(err)
	}

	s := Init(root+"/", zap.NewNop().Sugar())

	// a name search returns the matching systems, exact handle match first
	res := s.SearchASN("M247", 10)
	if len(res) != 2 {
		t.Fatalf("search M247 got %d results, want 2: %+v", len(res), res)
	}
	if res[0].ASN != 9009 {
		t.Fatalf("expected handle-exact AS9009 first, got %d", res[0].ASN)
	}

	// number search
	if r := s.SearchASN("15169", 10); len(r) != 1 || r[0].Handle != "GOOGLE" {
		t.Fatalf("number search got %+v", r)
	}

	// ip to asn still works from the loaded package
	if got := s.LookupASNDetails("5.62.0.1"); len(got) != 1 || got[0].ASN != 9009 {
		t.Fatalf("ip lookup got %+v", got)
	}
}
