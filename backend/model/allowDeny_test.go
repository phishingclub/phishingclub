package model

import (
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/vo"
)

func filter(allowed bool, country, asns string) *AllowDeny {
	r := &AllowDeny{Allowed: nullable.NewNullableWithValue(allowed)}
	if country != "" {
		r.CountryCodes = nullable.NewNullableWithValue(country)
	}
	if asns != "" {
		r.Asns = nullable.NewNullableWithValue(asns)
	}
	return r
}

func TestIsCountryAllowed(t *testing.T) {
	cases := []struct {
		name    string
		allowed bool
		country string
		visitor string
		want    bool
	}{
		{"no filter configured passes", true, "", "DK", true},
		{"allow list match", true, "DK\nUS", "DK", true},
		{"allow list no match denies", true, "DK\nUS", "FR", false},
		{"deny list match denies", false, "RU", "RU", false},
		{"deny list no match allows", false, "RU", "DK", true},
		// the fail open fix: an unknown visitor country must be denied by an
		// allow list and allowed by a deny list
		{"allow list unknown visitor denied", true, "DK", "", false},
		{"deny list unknown visitor allowed", false, "RU", "", true},
		{"case insensitive", true, "dk", "DK", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := filter(c.allowed, c.country, "").IsCountryAllowed(c.visitor)
			if got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestIsASNAllowed(t *testing.T) {
	cases := []struct {
		name    string
		allowed bool
		asns    string
		visitor []uint32
		want    bool
	}{
		{"no filter configured passes", true, "", []uint32{15169}, true},
		{"allow list match", true, "15169\n13335", []uint32{15169}, true},
		{"allow list no match denies", true, "15169", []uint32{13335}, false},
		{"allow list matches any announcing asn", true, "13335", []uint32{15169, 13335}, true},
		{"deny list match denies", false, "13335", []uint32{13335}, false},
		{"deny list no match allows", false, "13335", []uint32{15169}, true},
		{"AS prefix accepted", true, "AS15169", []uint32{15169}, true},
		// unknown visitor asn: allow list denies, deny list allows
		{"allow list unknown visitor denied", true, "15169", nil, false},
		{"deny list unknown visitor allowed", false, "15169", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := filter(c.allowed, "", c.asns).IsASNAllowed(c.visitor)
			if got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestValidateRejectsBadASN(t *testing.T) {
	r := &AllowDeny{
		Name:    nullable.NewNullableWithValue(*vo.NewString127Must("test")),
		Allowed: nullable.NewNullableWithValue(true),
		Asns:    nullable.NewNullableWithValue("15169\nnotanumber"),
	}
	if err := r.Validate(); err == nil {
		t.Fatal("expected validation error for bad ASN")
	}
	r.Asns = nullable.NewNullableWithValue("15169\nAS13335")
	if err := r.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
