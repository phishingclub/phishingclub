package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/vo"
)

func validSMTPConfiguration(ips string) *SMTPConfiguration {
	return &SMTPConfiguration{
		Name:             nullable.NewNullableWithValue(*vo.NewString127Must("smtp")),
		Host:             nullable.NewNullableWithValue(*vo.NewString255Must("smtp.example.test")),
		Port:             nullable.NewNullableWithValue(*vo.NewPortMust(587)),
		Username:         nullable.NewNullableWithValue(*vo.NewOptionalString255Must("")),
		Password:         nullable.NewNullableWithValue(*vo.NewOptionalString255Must("")),
		IgnoreCertErrors: nullable.NewNullableWithValue(false),
		PublicSendingIPs: nullable.NewNullableWithValue(ips),
	}
}

func TestParsePublicSendingIPsValid(t *testing.T) {
	cases := map[string][]string{
		"":                                {},
		"203.0.113.10":                    {"203.0.113.10"},
		"2001:db8::1":                     {"2001:db8::1"},
		"203.0.113.0/24":                  {"203.0.113.0/24"},
		"2001:db8::/32":                   {"2001:db8::/32"},
		" 203.0.113.10 \n\n2001:DB8::1 ":  {"203.0.113.10", "2001:db8::1"},
		"203.0.113.10, 198.51.100.7":      {"203.0.113.10", "198.51.100.7"},
		"203.0.113.10\n203.0.113.10":      {"203.0.113.10"},
		"203.0.113.77/24\n203.0.113.0/24": {"203.0.113.0/24"},
	}
	for input, want := range cases {
		got, err := ParsePublicSendingIPs(input)
		if err != nil {
			t.Fatalf("%q: unexpected error %v", input, err)
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("%q: got %v, want %v", input, got, want)
		}
		if err := validSMTPConfiguration(input).Validate(); err != nil {
			t.Fatalf("%q: Validate refused a valid list: %v", input, err)
		}
	}
}

func TestParsePublicSendingIPsInvalid(t *testing.T) {
	many := make([]string, 0, MaxPublicSendingIPs+1)
	for i := 0; i <= MaxPublicSendingIPs; i++ {
		many = append(many, fmt.Sprintf("203.0.113.%d", i+1))
	}
	cases := []string{
		"mail.example.com",
		"localhost",
		"not an ip",
		"203.0.113.300",
		"203.0.113.10/33",
		"fe80::1%eth0",
		"203.0.113.10; rm -rf /",
		"http://203.0.113.10",
		strings.Join(many, "\n"),
		strings.Repeat("1", 2000),
	}
	for _, input := range cases {
		if _, err := ParsePublicSendingIPs(input); err == nil {
			t.Fatalf("%q: expected an error", input)
		}
		if err := validSMTPConfiguration(input).Validate(); err == nil {
			t.Fatalf("%q: Validate accepted an invalid list", input)
		}
	}
}

func TestSMTPConfigurationToDBMapNormalizesPublicSendingIPs(t *testing.T) {
	m := validSMTPConfiguration(" 203.0.113.10 , 2001:DB8::1\n203.0.113.10").ToDBMap()
	if got := m["public_sending_ips"]; got != "203.0.113.10\n2001:db8::1" {
		t.Fatalf("got %q", got)
	}
	// an unset field is not written, so an update without it keeps the value
	unset := validSMTPConfiguration("")
	unset.PublicSendingIPs = nullable.Nullable[string]{}
	if _, ok := unset.ToDBMap()["public_sending_ips"]; ok {
		t.Fatal("an unset field must not be written")
	}
}

// TestParsePublicSendingIPsAcceptsAnyAddress checks the list only checks the
// form of an entry: the list fills in allow listing text, so loopback,
// unspecified, link local, multicast, private and wide ranges are kept as
// written, a range as its network address
func TestParsePublicSendingIPsAcceptsAnyAddress(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1":       "127.0.0.1",
		"::1":             "::1",
		"0.0.0.0":         "0.0.0.0",
		"0.0.0.0/0":       "0.0.0.0/0",
		"169.254.10.1":    "169.254.10.1",
		"fe80::/10":       "fe80::/10",
		"239.1.2.3":       "239.1.2.3",
		"10.0.0.0/8":      "10.0.0.0/8",
		"2000::/3":        "2000::/3",
		"1.2.3.4/16":      "1.2.0.0/16",
		"240.0.0.1":       "240.0.0.1",
		"255.255.255.255": "255.255.255.255",
	}
	for input, want := range cases {
		got, err := ParsePublicSendingIPs(input)
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("%q: got %v %v, want %s", input, got, err, want)
		}
		if err := validSMTPConfiguration(input).Validate(); err != nil {
			t.Fatalf("%q: Validate refused it: %v", input, err)
		}
	}
}
