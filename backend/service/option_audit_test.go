package service

import (
	"strings"
	"testing"

	"github.com/phishingclub/phishingclub/data"
)

func TestAuditOptionValueRedactsSSOSecret(t *testing.T) {
	secret := "super-secret-value-123"
	value := `{"enabled":true,"providerType":"entraid","clientID":"cid","tenantID":"tid","clientSecret":"` + secret + `","redirectURL":"https://example.test/cb"}`

	got := auditOptionValue(data.OptionKeyAdminSSOLogin, value)
	if strings.Contains(got, secret) {
		t.Fatalf("audit value contains the client secret: %s", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("audit value should mark the secret as redacted: %s", got)
	}
	if !strings.Contains(got, `"clientID":"cid"`) {
		t.Fatalf("audit value should keep non secret fields: %s", got)
	}
}

func TestAuditOptionValueUnparsableSSOIsWithheld(t *testing.T) {
	got := auditOptionValue(data.OptionKeyAdminSSOLogin, `{"clientSecret":"abc"`)
	if got != "[redacted]" {
		t.Fatalf("expected whole value withheld, got %s", got)
	}
}

func TestAuditOptionValueOtherKeysUnchanged(t *testing.T) {
	got := auditOptionValue(data.OptionKeyDisplayMode, data.OptionValueDisplayModeWhitebox)
	if got != data.OptionValueDisplayModeWhitebox {
		t.Fatalf("expected value unchanged, got %s", got)
	}
}
