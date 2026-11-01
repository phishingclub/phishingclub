package model

import (
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/vo"
)

type filterSpec struct {
	allowed bool
	cidrs   []string
	country string
	asns    string
	ja4     string
	headers string
}

func makeFilter(t *testing.T, s filterSpec) *AllowDeny {
	t.Helper()
	r := &AllowDeny{
		Name:    nullable.NewNullableWithValue(*vo.NewString127Must("test")),
		Allowed: nullable.NewNullableWithValue(s.allowed),
	}
	if len(s.cidrs) > 0 {
		nets := vo.IPNetSlice{}
		for _, c := range s.cidrs {
			nets = append(nets, *vo.NewIPNetMust(c))
		}
		r.Cidrs = nullable.NewNullableWithValue(nets)
	}
	if s.country != "" {
		r.CountryCodes = nullable.NewNullableWithValue(s.country)
	}
	if s.asns != "" {
		r.Asns = nullable.NewNullableWithValue(s.asns)
	}
	if s.ja4 != "" {
		r.JA4Fingerprints = nullable.NewNullableWithValue(s.ja4)
	}
	if s.headers != "" {
		r.Headers = nullable.NewNullableWithValue(s.headers)
	}
	return r
}

const (
	dkIP      = "80.62.0.1"
	inRangeIP = "10.1.2.3"
	ja4Chrome = "t13d1516h2_8daaf6152771_b186095e22b6"
)

func TestEvaluateFilters(t *testing.T) {
	uaRule := `[{"keyRegex":"(?i)^user-agent$","valueRegex":"curl"}]`
	cases := []struct {
		name    string
		filters []filterSpec
		visitor FilterVisitor
		want    bool
	}{
		// allow filters: a visitor passes when it matches any listed rule
		{"allow cidr+country, country matches outside cidr",
			[]filterSpec{{allowed: true, cidrs: []string{"10.0.0.0/8"}, country: "DK"}},
			FilterVisitor{IP: dkIP, Country: "DK"}, true},
		{"allow cidr+country, cidr matches other country",
			[]filterSpec{{allowed: true, cidrs: []string{"10.0.0.0/8"}, country: "DK"}},
			FilterVisitor{IP: inRangeIP, Country: "US"}, true},
		{"allow cidr+country, neither matches",
			[]filterSpec{{allowed: true, cidrs: []string{"10.0.0.0/8"}, country: "DK"}},
			FilterVisitor{IP: dkIP, Country: "SE"}, false},
		{"allow country+asn, asn matches",
			[]filterSpec{{allowed: true, country: "DK", asns: "12076"}},
			FilterVisitor{IP: dkIP, Country: "US", ASNs: []uint32{12076}}, true},
		{"allow ja4 only, ja4 matches",
			[]filterSpec{{allowed: true, ja4: "t13d1516h2_*"}},
			FilterVisitor{IP: dkIP, JA4: ja4Chrome}, true},
		{"allow headers only, header matches",
			[]filterSpec{{allowed: true, headers: uaRule}},
			FilterVisitor{IP: dkIP, Headers: map[string][]string{"User-Agent": {"curl/8"}}}, true},

		// empty fields must not count as a match
		{"allow country only, empty ja4 and headers do not match",
			[]filterSpec{{allowed: true, country: "DK"}},
			FilterVisitor{IP: dkIP, Country: "SE", Headers: map[string][]string{"User-Agent": {"x"}}}, false},

		// unknown visitor values never match
		{"allow country, unknown country denied",
			[]filterSpec{{allowed: true, country: "DK"}},
			FilterVisitor{IP: dkIP}, false},
		{"allow ja4, missing ja4 denied",
			[]filterSpec{{allowed: true, ja4: "t13d*"}},
			FilterVisitor{IP: dkIP}, false},
		{"allow asn, unknown asn denied",
			[]filterSpec{{allowed: true, asns: "12076"}},
			FilterVisitor{IP: dkIP, Country: "DK"}, false},
		{"deny country, unknown country allowed",
			[]filterSpec{{allowed: false, country: "RU"}},
			FilterVisitor{IP: dkIP}, true},

		// several allow filters: any one of them is enough
		{"two allow filters, second matches",
			[]filterSpec{{allowed: true, country: "US"}, {allowed: true, country: "DK"}},
			FilterVisitor{IP: dkIP, Country: "DK"}, true},
		{"two allow filters, none match",
			[]filterSpec{{allowed: true, country: "US"}, {allowed: true, asns: "1"}},
			FilterVisitor{IP: dkIP, Country: "DK", ASNs: []uint32{3292}}, false},

		// deny filters: a visitor is blocked when it matches any listed rule
		{"deny cidr+country, country matches",
			[]filterSpec{{allowed: false, cidrs: []string{"10.0.0.0/8"}, country: "DK"}},
			FilterVisitor{IP: dkIP, Country: "DK"}, false},
		{"deny cidr+country, cidr matches",
			[]filterSpec{{allowed: false, cidrs: []string{"10.0.0.0/8"}, country: "DK"}},
			FilterVisitor{IP: inRangeIP, Country: "US"}, false},
		{"deny cidr+country, neither matches",
			[]filterSpec{{allowed: false, cidrs: []string{"10.0.0.0/8"}, country: "DK"}},
			FilterVisitor{IP: dkIP, Country: "SE"}, true},
		{"deny ja4 only, missing ja4 allowed",
			[]filterSpec{{allowed: false, ja4: "*"}},
			FilterVisitor{IP: dkIP}, true},

		// mixed types: a deny match wins, otherwise an allow match is needed
		{"mixed, deny match wins over allow match",
			[]filterSpec{{allowed: true, country: "DK"}, {allowed: false, asns: "3292"}},
			FilterVisitor{IP: dkIP, Country: "DK", ASNs: []uint32{3292}}, false},
		{"mixed, allow match and no deny match",
			[]filterSpec{{allowed: false, asns: "1"}, {allowed: true, country: "DK"}},
			FilterVisitor{IP: dkIP, Country: "DK", ASNs: []uint32{3292}}, true},

		// stored lists may use commas
		{"comma separated countries",
			[]filterSpec{{allowed: true, country: "US,DK"}},
			FilterVisitor{IP: dkIP, Country: "DK"}, true},
		{"case insensitive country",
			[]filterSpec{{allowed: true, country: "dk"}},
			FilterVisitor{IP: dkIP, Country: "DK"}, true},
		{"AS prefix accepted",
			[]filterSpec{{allowed: true, asns: "AS12076"}},
			FilterVisitor{IP: dkIP, ASNs: []uint32{12076}}, true},
		{"any announcing asn matches",
			[]filterSpec{{allowed: true, asns: "13335"}},
			FilterVisitor{IP: dkIP, ASNs: []uint32{15169, 13335}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			filters := []*AllowDeny{}
			for _, s := range c.filters {
				filters = append(filters, makeFilter(t, s))
			}
			got, _, err := EvaluateFilters(filters, c.visitor)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestEvaluateFiltersNoFiltersAllows(t *testing.T) {
	got, _, err := EvaluateFilters(nil, FilterVisitor{IP: dkIP})
	if err != nil || !got {
		t.Fatalf("got %v, %v want true", got, err)
	}
}

func TestEvaluateFiltersBrokenHeadersBlocks(t *testing.T) {
	// a deny filter with broken header rules must not let visitors through
	f := makeFilter(t, filterSpec{allowed: false, headers: "not json"})
	got, _, err := EvaluateFilters([]*AllowDeny{f}, FilterVisitor{
		IP:      dkIP,
		Headers: map[string][]string{"User-Agent": {"x"}},
	})
	if err == nil || got {
		t.Fatalf("got %v, %v want blocked with error", got, err)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		spec    filterSpec
		wantErr bool
	}{
		{"valid asns", filterSpec{allowed: true, asns: "15169\nAS13335"}, false},
		{"bad asn", filterSpec{allowed: true, asns: "15169\nnotanumber"}, true},
		{"valid countries", filterSpec{allowed: true, country: "DK\nus"}, false},
		{"comma countries", filterSpec{allowed: true, country: "US,DK"}, false},
		{"bad country", filterSpec{allowed: true, country: "DNK"}, true},
		{"valid headers", filterSpec{allowed: true, headers: `[{"keyRegex":"a","valueRegex":"b"}]`}, false},
		{"bad header json", filterSpec{allowed: true, headers: "nope"}, true},
		{"bad header regex", filterSpec{allowed: true, headers: `[{"keyRegex":"(","valueRegex":"b"}]`}, true},
		{"empty header rules only", filterSpec{allowed: true, headers: "[]"}, true},
		{"nothing set", filterSpec{allowed: true}, true},
		{"cidr only", filterSpec{allowed: true, cidrs: []string{"10.0.0.0/8"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := makeFilter(t, c.spec).Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("got err %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}
