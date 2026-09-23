package service

import (
	"strings"

	"github.com/google/uuid"
	"github.com/wneessen/go-mail"
)

// domainFromEmailAddress returns the domain part of an email address.
// it returns an empty string when the address has no domain or the domain
// does not look like a real domain (no dot).
func domainFromEmailAddress(address string) string {
	at := strings.LastIndex(address, "@")
	if at < 0 || at == len(address)-1 {
		return ""
	}
	domain := strings.TrimSpace(address[at+1:])
	// a right side without a dot is not a valid domain and would look wrong
	// to a receiving mail server, so treat it as unusable
	if !strings.Contains(domain, ".") {
		return ""
	}
	return domain
}

// setMessageIDFromAddress sets the message "Message-ID" header as uuid@domain,
// using the sending domain as the right side, so outgoing mail does not carry
// the machine hostname that go-mail would otherwise use by default.
// when the address has no usable domain the message keeps whatever Message-ID
// go-mail assigns.
func setMessageIDFromAddress(m *mail.Msg, address string) {
	domain := domainFromEmailAddress(address)
	if domain == "" {
		return
	}
	m.SetMessageIDWithValue(uuid.NewString() + "@" + domain)
}
