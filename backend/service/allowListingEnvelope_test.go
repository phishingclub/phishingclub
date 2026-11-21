package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// TestEnvelopeDomainNormalized checks the mail from domain is written the
// same way as the URL hosts: lowercase, without a trailing dot, in punycode
func TestEnvelopeDomainNormalized(t *testing.T) {
	cases := map[string]string{
		"bounce@Mail.Example.COM":      "mail.example.com",
		"bounce@mail.example.com.":     "mail.example.com",
		"Name <bounce@BÜCHER.example>": "xn--bcher-kva.example",
		"bounce@bücher.example.":       "xn--bcher-kva.example",
		"no address":                   "",
		"bounce@":                      "",
	}
	for in, want := range cases {
		if got := envelopeDomain(in); got != want {
			t.Fatalf("%q: got %q, want %q", in, got, want)
		}
	}
}

// TestAllowListingTemplateHostsNormalized checks the domain and the proxy
// domains of a template are written the same way as the URL hosts, and a
// proxy domain equal to the template domain after that is not listed twice
func TestAllowListingTemplateHostsNormalized(t *testing.T) {
	proxyID := uuid.New()
	tmpl := &model.CampaignTemplate{
		Domain:         &model.Domain{Name: nullable.NewNullableWithValue(*vo.NewString255Must("Login.BÜCHER.example."))},
		LandingProxyID: nullable.NewNullableWithValue(proxyID),
	}
	entry := allowListingTemplate(uuid.New(), tmpl, map[uuid.UUID][]string{
		proxyID: {"login.xn--bcher-kva.example", "Proxy.Example.COM.", " "},
	})
	if entry.Domain != "login.xn--bcher-kva.example" {
		t.Fatalf("domain: got %q", entry.Domain)
	}
	if len(entry.ProxyDomains) != 1 || entry.ProxyDomains[0] != "proxy.example.com" {
		t.Fatalf("proxy domains: got %q", entry.ProxyDomains)
	}
}
