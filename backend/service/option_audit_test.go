package service

import (
	"bytes"
	"strings"
	"testing"

	"github.com/phishingclub/phishingclub/data"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

// bufferLogger returns a logger at level that writes JSON lines to the buffer
func bufferLogger(level zapcore.Level) (*zap.SugaredLogger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(buf),
		level,
	)
	return zap.New(core).Sugar(), buf
}

func TestCommonAuditLogUsesAuditLogger(t *testing.T) {
	mainLogger, mainBuf := bufferLogger(zapcore.ErrorLevel)
	auditLogger, auditBuf := bufferLogger(zapcore.InfoLevel)
	c := Common{Logger: mainLogger, AuditLogger: auditLogger}
	c.AuditLogAuthorized(NewAuditEvent("Test.Event", nil))
	if !strings.Contains(auditBuf.String(), `"name":"Test.Event"`) {
		t.Fatalf("expected the event in the audit logger, got %q", auditBuf.String())
	}
	if mainBuf.Len() != 0 {
		t.Fatalf("audit should not go through the main logger when an audit logger is set")
	}
}

func TestCommonAuditLogFallsBackToLogger(t *testing.T) {
	mainLogger, mainBuf := bufferLogger(zapcore.InfoLevel)
	c := Common{Logger: mainLogger}
	c.AuditLogNotAuthorized(NewAuditEvent("Test.Event", nil))
	if !strings.Contains(mainBuf.String(), `"name":"Test.Event"`) {
		t.Fatalf("expected fallback to main logger, got %q", mainBuf.String())
	}
}
