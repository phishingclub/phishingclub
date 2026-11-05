package model

import (
	"errors"
	"net/mail"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

const (
	// ReportInboxAuthApp reads the mailbox with an Entra app using client credentials
	ReportInboxAuthApp = "app"
	// ReportInboxAuthDelegated reads the mailbox with the token of an authorized
	// oauth provider, signed in as an account that can only open that mailbox
	ReportInboxAuthDelegated = "delegated"

	reportInboxMaxSecret = 1024
)

// tenantIDPattern allows a directory id (GUID) or a verified domain such as
// contoso.onmicrosoft.com. It goes into the token url path, so nothing else is allowed.
var tenantIDPattern = regexp.MustCompile(`^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+)$`)

// ReportInbox is a mailbox that users report phishing to, read on a schedule to
// record reported events. A nil CompanyID covers global campaigns only.
type ReportInbox struct {
	ID        nullable.Nullable[uuid.UUID] `json:"id"`
	CreatedAt *time.Time                   `json:"createdAt"`
	UpdatedAt *time.Time                   `json:"updatedAt"`
	CompanyID nullable.Nullable[uuid.UUID] `json:"companyID"`

	Enabled  bool   `json:"enabled"`
	AuthMode string `json:"authMode"`
	Mailbox  string `json:"mailbox"`

	TenantID string `json:"tenantID"`
	ClientID string `json:"clientID"`
	// ClientSecret is never sent to the client, HasClientSecret tells if one is set
	ClientSecret    string `json:"-"`
	HasClientSecret bool   `json:"hasClientSecret"`

	OAuthProviderID nullable.Nullable[uuid.UUID] `json:"oauthProviderID"`

	ReadSince      *time.Time `json:"readSince"`
	Watermark      *time.Time `json:"-"`
	ResumeAt       *time.Time `json:"-"`
	NextPollAt     *time.Time `json:"nextPollAt"`
	LastPolledAt   *time.Time `json:"lastPolledAt"`
	LastSuccessAt  *time.Time `json:"lastSuccessAt"`
	LastError      string     `json:"lastError"`
	FailureCount   int        `json:"failureCount"`
	ProcessedCount int        `json:"processedCount"`
	MatchedCount   int        `json:"matchedCount"`
	ConfigVersion  int        `json:"-"`
	// ClearClientSecret removes the stored secret, set when another Entra app is
	// saved without a new secret, as the old one belongs to the old app
	ClearClientSecret bool `json:"-"`
}

// Validate checks the config. Field formats are always checked, while the fields
// needed to read the mailbox are only required when the inbox is enabled, so a
// config can be saved while it is still being set up.
func (r *ReportInbox) Validate() error {
	if r.AuthMode != ReportInboxAuthApp && r.AuthMode != ReportInboxAuthDelegated {
		return validate.WrapErrorWithField(errors.New("must be app or delegated"), "authMode")
	}
	if r.Mailbox != "" {
		// only a bare address, a display name form would end up in the Graph path
		addr, err := mail.ParseAddress(r.Mailbox)
		if err != nil || addr.Address != r.Mailbox || addr.Name != "" {
			return validate.WrapErrorWithField(errors.New("enter only the email address"), "mailbox")
		}
		if _, err := vo.NewEmail(r.Mailbox); err != nil {
			return validate.WrapErrorWithField(errors.New("invalid email address"), "mailbox")
		}
	}
	if r.TenantID != "" && !tenantIDPattern.MatchString(r.TenantID) {
		return validate.WrapErrorWithField(errors.New("must be a directory id or a domain"), "tenantID")
	}
	if r.ClientID != "" {
		if _, err := uuid.Parse(r.ClientID); err != nil {
			return validate.WrapErrorWithField(errors.New("must be an application (client) id"), "clientID")
		}
	}
	if len(r.ClientSecret) > reportInboxMaxSecret {
		return validate.WrapErrorWithField(errors.New("too long"), "clientSecret")
	}
	if !r.Enabled {
		return nil
	}
	if r.Mailbox == "" {
		return validate.WrapErrorWithField(errors.New("required"), "mailbox")
	}
	switch r.AuthMode {
	case ReportInboxAuthApp:
		if r.TenantID == "" {
			return validate.WrapErrorWithField(errors.New("required"), "tenantID")
		}
		if r.ClientID == "" {
			return validate.WrapErrorWithField(errors.New("required"), "clientID")
		}
		if r.ClientSecret == "" && !r.HasClientSecret {
			return validate.WrapErrorWithField(errors.New("required"), "clientSecret")
		}
	case ReportInboxAuthDelegated:
		if err := validate.NullableFieldRequired("oauthProviderID", r.OAuthProviderID); err != nil {
			return err
		}
	}
	return nil
}

// ToDBMap converts the user editable fields to a map for persistence. An empty
// client secret keeps the stored one, so the UI never needs to know the secret.
func (r *ReportInbox) ToDBMap() map[string]any {
	m := map[string]any{
		"enabled":   r.Enabled,
		"auth_mode": r.AuthMode,
		"mailbox":   r.Mailbox,
		"tenant_id": r.TenantID,
		"client_id": r.ClientID,
	}
	if r.ClientSecret != "" {
		m["client_secret"] = r.ClientSecret
	}
	// a secret is only kept for app mode and for the same app
	if r.AuthMode != ReportInboxAuthApp || r.ClearClientSecret {
		m["client_secret"] = ""
	}
	m["o_auth_provider_id"] = nil
	if v, err := r.OAuthProviderID.Get(); err == nil {
		m["o_auth_provider_id"] = v
	}
	return m
}
