package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wneessen/go-mail"
)

// TestSMTPUnreachableIsNotDelivered checks the mail client leaves no send
// error on a message when the server can not be reached, so the sender must
// treat a message that was not delivered as failed and never as sent
func TestSMTPUnreachableIsNotDelivered(t *testing.T) {
	m := mail.NewMsg()
	if err := m.From("sender@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := m.To("alice@example.com"); err != nil {
		t.Fatal(err)
	}
	m.Subject("test")
	m.SetBodyString(mail.TypeTextPlain, "test")
	c, err := mail.NewClient("127.0.0.1", mail.WithPort(1), mail.WithTimeout(2*time.Second), mail.WithTLSPolicy(mail.NoTLS))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sendErr := c.DialAndSendWithContext(ctx, m)
	if sendErr == nil {
		t.Fatal("expected the dial to fail")
	}
	if m.HasSendError() || m.IsDelivered() {
		t.Fatalf("expected no message error and not delivered, got sendError=%v delivered=%v", m.HasSendError(), m.IsDelivered())
	}
	reason := smtpNotDeliveredError(sendErr)
	if reason == nil || !strings.Contains(reason.Error(), "not delivered") || !errors.Is(reason, sendErr) {
		t.Fatalf("unexpected reason: %v", reason)
	}
	if smtpNotDeliveredError(nil) == nil {
		t.Fatal("a message not delivered must fail also without a send error")
	}
}
