package model

import "github.com/google/uuid"

// allow listing sender types
const (
	AllowListingSenderSMTP      = "smtp"
	AllowListingSenderAPISender = "apiSender"
)

// allow listing values a template is missing
const (
	AllowListingMissingDomain    = "domain"
	AllowListingMissingEmail     = "email"
	AllowListingMissingMailFrom  = "mailFrom"
	AllowListingMissingSender    = "sender"
	AllowListingMissingSendingIP = "sendingIP"
)

// Microsoft Defender Advanced Delivery limits on the phishing simulation
// fields. The portal allows 50 domains and PowerShell 20, so 20 is used.
const (
	AllowListingMaxDomains        = 20
	AllowListingMaxSendingIPs     = 10
	AllowListingMaxSimulationURLs = 30
	// AllowListingMaxURLLength is the longest one simulation URL entry may be
	AllowListingMaxURLLength = 250
)

// AllowListing is the information an operator enters in a mail filter, such
// as the Microsoft Defender Advanced Delivery policy, so the simulation mail of
// one or more templates is delivered. Values are deduplicated over all
// templates.
type AllowListing struct {
	// MailFromDomains are the envelope sender (MAIL FROM) domains
	MailFromDomains []string `json:"mailFromDomains"`
	// SendingIPs are the public sending IPs set on the SMTP configurations
	SendingIPs []string `json:"sendingIPs"`
	// SimulationURLs are the exact phishing hosts as host/*
	SimulationURLs []string `json:"simulationURLs"`
	// SimulationDomainURLs are the registered domains of the phishing hosts,
	// each as domain/* and *.domain/*. A host that is a public suffix or an
	// IP address is listed as host/*.
	SimulationDomainURLs []string `json:"simulationDomainURLs"`
	// TooLongURLs are entries left out of the lists above because they are
	// longer than AllowListingMaxURLLength
	TooLongURLs []string `json:"tooLongURLs"`
	// OverLimit flags the lists with more entries than Microsoft allows
	OverLimit AllowListingOverLimit `json:"overLimit"`
	// HasIPv6SendingIP is set when a sending IP is an IPv6 value, which can be
	// added only with PowerShell
	HasIPv6SendingIP bool `json:"hasIPv6SendingIP"`
	// Templates is the breakdown per template, so a missing value can be traced
	Templates []*AllowListingTemplate `json:"templates"`
	// DeletedTemplates counts templates that are referenced but no longer exist
	DeletedTemplates int `json:"deletedTemplates"`
}

// AllowListingTemplate is the allow listing information of one template
type AllowListingTemplate struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// EmailID is the email of the template, nil when it has none
	EmailID *uuid.UUID `json:"emailID"`
	// Domain is the template domain, empty when it has none
	Domain string `json:"domain"`
	// ProxyDomains are the domains of the proxies the template uses
	ProxyDomains []string `json:"proxyDomains"`
	// MailFromDomain is the envelope sender domain of the template email
	MailFromDomain string `json:"mailFromDomain"`
	// SenderType is smtp, apiSender or empty when the template has no sender
	SenderType string     `json:"senderType"`
	SenderID   *uuid.UUID `json:"senderID"`
	SenderName string     `json:"senderName"`
	// SendingIPs are the public sending IPs of the SMTP configuration
	SendingIPs []string `json:"sendingIPs"`
	// Missing lists the values the template lacks
	Missing []string `json:"missing"`
	// Language is the template language, empty when not set
	Language string `json:"language"`
	// Languages are the other languages of the template, each with the
	// envelope sender of its email. The sender and the domains are shared.
	Languages []*AllowListingTemplateLanguage `json:"languages"`
}

// AllowListingTemplateLanguage is the allow listing information of one more
// language of a template
type AllowListingTemplateLanguage struct {
	// Language is the language code
	Language string `json:"language"`
	// EmailID is the email of the language, nil when it has none
	EmailID *uuid.UUID `json:"emailID"`
	// MailFromDomain is the envelope sender domain of the language email
	MailFromDomain string `json:"mailFromDomain"`
	// Missing lists the values the language lacks, email or mailFrom
	Missing []string `json:"missing"`
}

// AllowListingOverLimit flags each list that has more entries than Microsoft
// allows in one phishing simulation policy
type AllowListingOverLimit struct {
	MailFromDomains      bool `json:"mailFromDomains"`
	SendingIPs           bool `json:"sendingIPs"`
	SimulationURLs       bool `json:"simulationURLs"`
	SimulationDomainURLs bool `json:"simulationDomainURLs"`
}

// NewAllowListingOverLimit flags the lists of an allow listing that pass the
// Microsoft limits
func NewAllowListingOverLimit(a *AllowListing) AllowListingOverLimit {
	return AllowListingOverLimit{
		MailFromDomains:      len(a.MailFromDomains) > AllowListingMaxDomains,
		SendingIPs:           len(a.SendingIPs) > AllowListingMaxSendingIPs,
		SimulationURLs:       len(a.SimulationURLs) > AllowListingMaxSimulationURLs,
		SimulationDomainURLs: len(a.SimulationDomainURLs) > AllowListingMaxSimulationURLs,
	}
}
