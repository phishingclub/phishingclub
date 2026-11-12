package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

const (
	reportInboxPollInterval = 2 * time.Minute
	reportInboxMaxBackoff   = time.Hour
	reportInboxPollTimeout  = 3 * time.Minute
	// reportInboxReadBudget is how long a poll starts new messages. After it the
	// poll stops and the next one continues right away, so a large backlog is
	// read over several polls instead of running into the poll timeout.
	reportInboxReadBudget = 90 * time.Second
	// reportInboxTestTimeout bounds the connection test, below the 15 second
	// write timeout of the admin server
	reportInboxTestTimeout = 12 * time.Second
	// reportInboxAccessSamples is how many other mailboxes the test tries to open
	reportInboxAccessSamples = 3
	// reportInboxMessageTries is how many polls must fail on the same message
	// before it is passed over, together with reportInboxGiveUpAfter
	reportInboxMessageTries = 8
	// reportInboxMaxRetries is how many pending messages a poll reads again
	reportInboxMaxRetries = 10
	// reportInboxWorkers is how many inboxes are polled at the same time. A slow
	// tenant only holds its own slot.
	reportInboxWorkers = 6
	// reportInboxJitter spreads polls out so inboxes saved together do not stay in step
	reportInboxJitter        = 20 * time.Second
	reportInboxPageSize      = 25
	reportInboxMaxPages      = 8
	reportInboxMaxAttachment = 3
	// reportInboxMaxFetches caps the attachment requests made for one message
	reportInboxMaxFetches = 10
	// reportInboxOverlap reads a little before the watermark on every poll, so a
	// message that shows up late in the mailbox is not missed. Already processed
	// messages in the overlap are skipped by id.
	reportInboxOverlap = 10 * time.Minute
)

// ReportInbox reads report mailboxes and records reported events for campaign
// recipients found in reported emails
type ReportInbox struct {
	Common
	ReportInboxRepository       *repository.ReportInbox
	CompanyRepository           *repository.Company
	CampaignRecipientRepository *repository.CampaignRecipient
	CampaignService             *Campaign
	OAuthProviderService        *OAuthProvider

	graphOnce sync.Once
	graph     *graphMailClient

	// tokens caches app tokens by a hash of the credentials, so changed
	// credentials never reuse a token of the old ones
	tokenMu sync.Mutex
	tokens  map[string]cachedToken

	// inflight holds the inboxes being polled right now
	inflightMu sync.Mutex
	inflight   map[uuid.UUID]bool
	// cancels stops a running poll, used when its inbox is changed or deleted
	cancels map[uuid.UUID]context.CancelFunc
	polls   sync.WaitGroup

	// pollAgain holds inboxes that were asked to poll now while a poll was
	// running. That poll saves its own next poll time, so it polls again after.
	pollAgain map[uuid.UUID]bool
	// stopped is set when the server shuts down, no poll starts after it
	stopped bool
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

// ReportInboxTestStep is one check of a connection test
type ReportInboxTestStep struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// ReportInboxTestResult is the outcome of a connection test
type ReportInboxTestResult struct {
	OK       bool                  `json:"ok"`
	Steps    []ReportInboxTestStep `json:"steps"`
	Warnings []string              `json:"warnings"`
}

func (s *ReportInbox) client() *graphMailClient {
	s.graphOnce.Do(func() {
		if s.graph == nil {
			s.graph = newGraphMailClient()
		}
	})
	return s.graph
}

func reportInboxScope(companyID *uuid.UUID) string {
	if companyID == nil {
		return "global"
	}
	return companyID.String()
}

// authorize checks the session may manage report inboxes
func (s *ReportInbox) authorize(session *model.Session, ae *AuditEvent) error {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	return nil
}

// GetByCompanyID returns the report inbox of a company, or the global one when nil
func (s *ReportInbox) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*model.ReportInbox, error) {
	ae := NewAuditEvent("ReportInbox.GetByCompanyID", session)
	ae.Details["scope"] = reportInboxScope(companyID)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	inbox, err := s.ReportInboxRepository.GetByCompanyID(ctx, companyID)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			s.Logger.Errorw("failed to get reporting inbox", "error", err)
		}
		return nil, errs.Wrap(err)
	}
	return inbox, nil
}

// prepare validates incoming settings for a scope. It checks the company exists
// and that a delegated oauth provider is usable in the scope. existing may be nil.
func (s *ReportInbox) prepare(
	ctx context.Context,
	companyID *uuid.UUID,
	incoming *model.ReportInbox,
	existing *model.ReportInbox,
) error {
	if companyID != nil {
		if _, err := s.CompanyRepository.GetByID(ctx, companyID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return validate.WrapErrorWithField(errors.New("not found"), "companyID")
			}
			return errs.Wrap(err)
		}
		incoming.CompanyID = nullable.NewNullableWithValue(*companyID)
	} else {
		incoming.CompanyID = nullable.NewNullNullable[uuid.UUID]()
	}
	// a stored secret stays when the request leaves the secret empty, but only
	// for the same app. Another app needs its own secret.
	if existing != nil && incoming.ClientSecret == "" && existing.HasClientSecret {
		if sameApp(existing, incoming) {
			incoming.HasClientSecret = true
		} else {
			incoming.ClearClientSecret = true
		}
	}
	// app mode never uses an oauth provider and delegated mode never uses app credentials
	if incoming.AuthMode == model.ReportInboxAuthDelegated {
		incoming.TenantID = ""
		incoming.ClientID = ""
	} else {
		incoming.OAuthProviderID = nullable.NewNullNullable[uuid.UUID]()
	}
	if err := incoming.Validate(); err != nil {
		return err
	}
	// the oauth provider must be global or owned by the company, the global
	// inbox can only use a global one
	refs := companyRefs{}
	refs.add(database.OAUTH_PROVIDER_TABLE, "oauthProviderID", incoming.OAuthProviderID)
	held := companyRefs{}
	if existing != nil {
		held.add(database.OAUTH_PROVIDER_TABLE, "oauthProviderID", existing.OAuthProviderID)
	}
	if err := ValidateCompanyRefs(ctx, s.ReportInboxRepository.DB, companyID, newCompanyRefs(refs, held)); err != nil {
		return err
	}
	if providerID, err := incoming.OAuthProviderID.Get(); err == nil {
		if err := s.checkMicrosoftProvider(ctx, providerID); err != nil {
			return validate.WrapErrorWithField(err, "oauthProviderID")
		}
	}
	return nil
}

// checkMicrosoftProvider makes sure an oauth provider signs in with Microsoft,
// as its token is sent to Microsoft Graph
func (s *ReportInbox) checkMicrosoftProvider(ctx context.Context, providerID uuid.UUID) error {
	if s.OAuthProviderService == nil || s.OAuthProviderService.OAuthProviderRepository == nil {
		return errors.New("oauth providers are not available")
	}
	provider, err := s.OAuthProviderService.OAuthProviderRepository.GetByID(ctx, providerID)
	if err != nil {
		return errs.Wrap(err)
	}
	tokenURL, err := provider.TokenURL.Get()
	if err != nil {
		return errors.New("the oauth provider has no token url")
	}
	u, err := url.Parse(tokenURL.String())
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return errors.New("the oauth provider token url must be an https url")
	}
	switch strings.ToLower(u.Hostname()) {
	case "login.microsoftonline.com", "login.microsoft.com", "login.windows.net":
		return nil
	}
	return errors.New("the oauth provider must sign in with Microsoft (login.microsoftonline.com)")
}

// sameApp tells if two configs use the same Entra app, so a stored secret
// belongs to it and can be kept
func sameApp(a, b *model.ReportInbox) bool {
	return strings.EqualFold(a.TenantID, b.TenantID) && strings.EqualFold(a.ClientID, b.ClientID)
}

// connectionChanged tells if the change means the mailbox must be read again
// from the start
func connectionChanged(a, b *model.ReportInbox) bool {
	if a.AuthMode != b.AuthMode || !strings.EqualFold(a.Mailbox, b.Mailbox) ||
		!strings.EqualFold(a.TenantID, b.TenantID) || !strings.EqualFold(a.ClientID, b.ClientID) ||
		b.ClientSecret != "" {
		return true
	}
	av, aErr := a.OAuthProviderID.Get()
	bv, bErr := b.OAuthProviderID.Get()
	return (aErr == nil) != (bErr == nil) || av != bv
}

// Upsert creates or updates the report inbox for a scope
func (s *ReportInbox) Upsert(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	incoming *model.ReportInbox,
) (_ *model.ReportInbox, err error) {
	ae := NewAuditEvent("ReportInbox.Upsert", session)
	defer s.auditDone(ae, &err)
	ae.Details["scope"] = reportInboxScope(companyID)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	existing, err := s.ReportInboxRepository.GetByCompanyID(ctx, companyID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.Logger.Errorw("failed to look up reporting inbox", "error", err)
		return nil, errs.Wrap(err)
	}
	if err := s.prepare(ctx, companyID, incoming, existing); err != nil {
		return nil, err
	}
	ae.Details["enabled"] = incoming.Enabled
	ae.Details["authMode"] = incoming.AuthMode
	ae.Details["mailbox"] = incoming.Mailbox

	if existing != nil {
		id := existing.ID.MustGet()
		// another mailbox is read from scratch. New credentials for the same
		// mailbox, such as a rotated secret, keep the reading position so the
		// reports that arrived while the old ones failed are still read.
		mailboxChanged := !strings.EqualFold(existing.Mailbox, incoming.Mailbox)
		update := repository.ReportInboxUpdate{
			ResetPoll: mailboxChanged,
			// turning the inbox back on also starts a fresh check, without the
			// backoff and error of before
			RetryNow: connectionChanged(existing, incoming) || (incoming.Enabled && !existing.Enabled),
		}
		// reading starts when the inbox is turned on, or again from now when it
		// reads another mailbox
		if incoming.Enabled && (mailboxChanged || !existing.Enabled || existing.ReadSince == nil) {
			now := time.Now()
			update.ReadSince = &now
		}
		if err := s.ReportInboxRepository.UpdateByID(ctx, &id, incoming, update); err != nil {
			s.Logger.Errorw("failed to update reporting inbox", "error", err)
			return nil, errs.Wrap(err)
		}
		// a running poll uses the old settings, it stops so the new ones apply
		s.CancelPoll(id)
		s.forgetToken(existing)
		ae.Details["id"] = id.String()
		ae.Details["action"] = "update"
		s.AuditLogAuthorized(ae)
		return s.ReportInboxRepository.GetByID(ctx, &id)
	}
	var readSince *time.Time
	if incoming.Enabled {
		now := time.Now()
		readSince = &now
	}
	id, err := s.ReportInboxRepository.Insert(ctx, incoming, readSince)
	if err != nil {
		s.Logger.Errorw("failed to insert reporting inbox", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	ae.Details["action"] = "insert"
	s.AuditLogAuthorized(ae)
	return s.ReportInboxRepository.GetByID(ctx, id)
}

// DeleteByCompanyID removes the report inbox of a scope
func (s *ReportInbox) DeleteByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (err error) {
	ae := NewAuditEvent("ReportInbox.DeleteByCompanyID", session)
	defer s.auditDone(ae, &err)
	ae.Details["scope"] = reportInboxScope(companyID)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	if err := s.authorize(session, ae); err != nil {
		return err
	}
	existing, err := s.ReportInboxRepository.GetByCompanyID(ctx, companyID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.AuditLogAuthorized(ae)
			return nil
		}
		return errs.Wrap(err)
	}
	id := existing.ID.MustGet()
	if err := s.ReportInboxRepository.DeleteByID(ctx, &id); err != nil {
		s.Logger.Errorw("failed to delete reporting inbox", "error", err)
		return errs.Wrap(err)
	}
	// a running poll must not keep reading a mailbox that was removed. Stopped
	// after the delete, so a poll planned meanwhile finds the inbox gone.
	s.CancelPoll(id)
	s.forgetToken(existing)
	ae.Details["id"] = id.String()
	s.AuditLogAuthorized(ae)
	return nil
}

// PollNow makes the inbox of a scope poll on the next round
func (s *ReportInbox) PollNow(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (err error) {
	ae := NewAuditEvent("ReportInbox.PollNow", session)
	defer s.auditDone(ae, &err)
	ae.Details["scope"] = reportInboxScope(companyID)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	if err := s.authorize(session, ae); err != nil {
		return err
	}
	existing, err := s.ReportInboxRepository.GetByCompanyID(ctx, companyID)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			s.Logger.Errorw("failed to get reporting inbox", "error", err)
		}
		return errs.Wrap(err)
	}
	id := existing.ID.MustGet()
	// marked before the write, so a running poll that saves its own next poll
	// time in between still sees the request when it finishes
	s.inflightMu.Lock()
	if s.inflight[id] {
		if s.pollAgain == nil {
			s.pollAgain = map[uuid.UUID]bool{}
		}
		s.pollAgain[id] = true
	}
	s.inflightMu.Unlock()
	if err := s.ReportInboxRepository.PollNow(ctx, &id); err != nil {
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// Test checks the given settings against Microsoft Graph without saving them.
// An empty client secret uses the stored one.
func (s *ReportInbox) Test(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	incoming *model.ReportInbox,
) (_ *ReportInboxTestResult, err error) {
	ae := NewAuditEvent("ReportInbox.Test", session)
	defer s.auditDone(ae, &err)
	ae.Details["scope"] = reportInboxScope(companyID)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	existing, err := s.ReportInboxRepository.GetByCompanyID(ctx, companyID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(err)
	}
	// every field the test needs is required, as if the inbox was enabled
	incoming.Enabled = true
	if err := s.prepare(ctx, companyID, incoming, existing); err != nil {
		return nil, err
	}
	if incoming.ClientSecret == "" && existing != nil && sameApp(existing, incoming) {
		incoming.ClientSecret = existing.ClientSecret
	}
	ae.Details["mailbox"] = incoming.Mailbox
	ae.Details["authMode"] = incoming.AuthMode

	// an address in the same scope and domain that the app should not be able to open
	others, err := s.ReportInboxRepository.SampleRecipientEmails(ctx, companyID, incoming.Mailbox, reportInboxAccessSamples)
	if err != nil {
		s.Logger.Warnw("failed to pick an address for the access check", "error", err)
	}

	// stays under the admin server write timeout, so the steps always come back
	tctx, cancel := context.WithTimeout(ctx, reportInboxTestTimeout)
	defer cancel()
	// a token refresh of an oauth provider does not stop on the context, so the
	// test runs on its own and the answer is given when the time is up
	done := make(chan *ReportInboxTestResult, 1)
	// tracked like a poll, so shutdown waits for it before the database closes
	s.inflightMu.Lock()
	if s.stopped {
		s.inflightMu.Unlock()
		return nil, errors.New("the server is shutting down")
	}
	s.polls.Add(1)
	s.inflightMu.Unlock()
	go func() {
		defer s.polls.Done()
		defer func() {
			if r := recover(); r != nil {
				s.Logger.Errorw("reporting inbox test panicked", "error", r)
				done <- &ReportInboxTestResult{
					Steps:    []ReportInboxTestStep{{Name: "Test", OK: false, Message: "the test failed unexpectedly"}},
					Warnings: []string{},
				}
			}
		}()
		done <- s.runTest(tctx, incoming, others)
	}()
	var result *ReportInboxTestResult
	select {
	case result = <-done:
	case <-tctx.Done():
		result = &ReportInboxTestResult{
			Steps:    []ReportInboxTestStep{{Name: "Test", OK: false, Message: "Microsoft 365 did not answer in time"}},
			Warnings: []string{},
		}
	}
	ae.Details["ok"] = result.OK
	s.AuditLogAuthorized(ae)
	return result, nil
}

// runTest signs in, reads the report mailbox and checks that other mailboxes in
// the tenant can not be opened. others are recipient addresses on the same
// domain, empty when none is known. For them only the inbox folder is requested,
// so no message of those people is read even when access is not limited.
func (s *ReportInbox) runTest(ctx context.Context, inbox *model.ReportInbox, others []string) *ReportInboxTestResult {
	res := &ReportInboxTestResult{Steps: []ReportInboxTestStep{}, Warnings: []string{}}
	// a refreshed oauth token is stored after the request, so the sign in does
	// not stop when the test answers on its timeout
	tokenCtx, cancelToken := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelToken()
	token, err := s.fetchToken(tokenCtx, inbox)
	if err != nil {
		res.Steps = append(res.Steps, ReportInboxTestStep{Name: "Sign in", OK: false, Message: err.Error()})
		return res
	}
	res.Steps = append(res.Steps, ReportInboxTestStep{Name: "Sign in", OK: true, Message: "Got an access token"})

	// a delegated account can always open its own mailbox, so it is not one of
	// the mailboxes the access check tries
	if inbox.AuthMode == model.ReportInboxAuthDelegated {
		if self := signedInAddress(token); self != "" {
			kept := others[:0:0]
			for _, o := range others {
				if !strings.EqualFold(o, self) {
					kept = append(kept, o)
				}
			}
			others = kept
		}
	}
	roles, scopes := tokenClaims(token)
	if inbox.AuthMode == model.ReportInboxAuthApp {
		if broad := broadMailRoles(roles); len(broad) > 0 {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"The app has the tenant wide permission %s in Entra, so it can read every mailbox. Remove it from the app's API permissions and keep only the Exchange RBAC role assignment.",
				strings.Join(broad, ", "),
			))
		}
	} else if len(scopes) > 0 {
		res.Steps = append(res.Steps, ReportInboxTestStep{Name: "Permissions", OK: true, Message: "Delegated scopes: " + strings.Join(scopes, " ")})
	}

	if err := s.client().canRead(ctx, token, inbox.Mailbox); err != nil {
		msg := err.Error()
		if isAccessDenied(err) {
			msg = "Access to " + inbox.Mailbox + " was denied. Role assignments in Exchange can take up to 2 hours to apply. " + msg
		}
		res.Steps = append(res.Steps, ReportInboxTestStep{Name: "Read report mailbox", OK: false, Message: msg})
		return res
	}
	res.Steps = append(res.Steps, ReportInboxTestStep{Name: "Read report mailbox", OK: true, Message: "Can read " + inbox.Mailbox})

	res.Steps = append(res.Steps, s.accessLimitedStep(ctx, token, others, inbox.AuthMode == model.ReportInboxAuthDelegated, res))

	res.OK = true
	for _, step := range res.Steps {
		if !step.OK {
			res.OK = false
		}
	}
	return res
}

// fetchToken gets a fresh token for the inbox settings
func (s *ReportInbox) fetchToken(ctx context.Context, inbox *model.ReportInbox) (string, error) {
	if inbox.AuthMode == model.ReportInboxAuthDelegated {
		providerID, err := inbox.OAuthProviderID.Get()
		if err != nil {
			return "", errors.New("no oauth provider selected")
		}
		// the token is sent to Microsoft Graph, so it must come from Microsoft
		if err := s.checkMicrosoftProvider(ctx, providerID); err != nil {
			return "", err
		}
		return s.OAuthProviderService.GetValidAccessToken(ctx, providerID)
	}
	token, _, err := s.client().appToken(ctx, inbox.TenantID, inbox.ClientID, inbox.ClientSecret)
	return token, err
}

// credentialKey identifies the app credentials of an inbox in the token cache
func credentialKey(inbox *model.ReportInbox) string {
	sum := sha256.Sum256([]byte(inbox.TenantID + "\x00" + inbox.ClientID + "\x00" + inbox.ClientSecret))
	return hex.EncodeToString(sum[:])
}

// token returns a cached token for a stored inbox, fetching a new one when needed
func (s *ReportInbox) token(ctx context.Context, inbox *model.ReportInbox) (string, error) {
	if inbox.AuthMode == model.ReportInboxAuthDelegated {
		// the oauth provider service caches and refreshes on its own
		return s.fetchToken(ctx, inbox)
	}
	key := credentialKey(inbox)
	s.tokenMu.Lock()
	cached, ok := s.tokens[key]
	s.tokenMu.Unlock()
	if ok && time.Now().Add(5*time.Minute).Before(cached.expiresAt) {
		return cached.token, nil
	}
	token, expiresAt, err := s.client().appToken(ctx, inbox.TenantID, inbox.ClientID, inbox.ClientSecret)
	if err != nil {
		return "", err
	}
	now := time.Now()
	s.tokenMu.Lock()
	if s.tokens == nil {
		s.tokens = map[string]cachedToken{}
	}
	// drop expired tokens so old credentials do not pile up
	for k, v := range s.tokens {
		if now.After(v.expiresAt) {
			delete(s.tokens, k)
		}
	}
	s.tokens[key] = cachedToken{token: token, expiresAt: expiresAt}
	s.tokenMu.Unlock()
	return token, nil
}

// forgetToken drops the cached token of the inbox credentials
func (s *ReportInbox) forgetToken(inbox *model.ReportInbox) {
	if inbox == nil || inbox.AuthMode != model.ReportInboxAuthApp {
		return
	}
	key := credentialKey(inbox)
	s.tokenMu.Lock()
	delete(s.tokens, key)
	s.tokenMu.Unlock()
}

// PollDue starts a poll for due inboxes, up to reportInboxWorkers at the same
// time, and returns without waiting for them. It runs from the background task
// runner every few seconds, so a free slot is filled on the next round. An inbox
// that is still being polled is never started twice.
func (s *ReportInbox) PollDue(ctx context.Context) error {
	s.inflightMu.Lock()
	if s.inflight == nil {
		s.inflight = map[uuid.UUID]bool{}
	}
	busy := len(s.inflight)
	s.inflightMu.Unlock()
	free := reportInboxWorkers - busy
	if free <= 0 {
		return nil
	}
	// fetch extra rows, the ones already being polled are skipped
	inboxes, err := s.ReportInboxRepository.ListDue(ctx, time.Now(), free+busy)
	if err != nil {
		return errs.Wrap(err)
	}
	for _, inbox := range inboxes {
		if free == 0 || ctx.Err() != nil {
			break
		}
		id := inbox.ID.MustGet()
		s.inflightMu.Lock()
		if s.stopped {
			s.inflightMu.Unlock()
			break
		}
		if s.inflight[id] {
			s.inflightMu.Unlock()
			continue
		}
		s.inflight[id] = true
		// added under the lock, so WaitPolls never runs while a poll is added
		s.polls.Add(1)
		// the cancel is known from the moment the poll is planned, so a change
		// or delete of the inbox right after stops it
		pctx, cancel := context.WithTimeout(ctx, reportInboxPollTimeout)
		if s.cancels == nil {
			s.cancels = map[uuid.UUID]context.CancelFunc{}
		}
		s.cancels[id] = cancel
		s.inflightMu.Unlock()
		free--
		go func(inbox *model.ReportInbox) {
			defer func() {
				if r := recover(); r != nil {
					s.Logger.Errorw("reporting inbox poll panicked",
						"reportInboxID", id.String(),
						"error", r,
						"stack", string(debug.Stack()),
					)
					s.savePanicState(ctx, inbox)
				}
				s.inflightMu.Lock()
				delete(s.inflight, id)
				// a request after the poll read its flag is already in the
				// database, the flag is not needed any more
				delete(s.pollAgain, id)
				s.inflightMu.Unlock()
				s.polls.Done()
			}()
			defer cancel()
			defer func() {
				s.inflightMu.Lock()
				delete(s.cancels, id)
				s.inflightMu.Unlock()
			}()
			s.poll(pctx, inbox)
		}(inbox)
	}
	return nil
}

// savePanicState records a panicked poll as a failure with backoff, so the inbox
// is not polled again on the next round. It recovers on its own so a second
// panic can not escape the poll goroutine.
func (s *ReportInbox) savePanicState(ctx context.Context, inbox *model.ReportInbox) {
	defer func() {
		if r := recover(); r != nil {
			s.Logger.Errorw("failed to record reporting inbox panic", "error", r)
		}
	}()
	id := inbox.ID.MustGet()
	now := time.Now()
	failures := inbox.FailureCount + 1
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, err := s.ReportInboxRepository.SavePollState(sctx, &id, inbox.ConfigVersion, &repository.ReportInboxPollState{
		PolledAt:     now,
		NextPollAt:   now.Add(reportInboxBackoff(failures, nil) + jitter()),
		LastError:    "internal error while checking the mailbox, see the server log",
		FailureCount: failures,
	}); err != nil {
		s.Logger.Errorw("failed to save reporting inbox panic state", "reportInboxID", id.String(), "error", err)
	}
}

// WaitPolls blocks until the polls started so far are done
func (s *ReportInbox) WaitPolls() {
	s.polls.Wait()
}

// Stop keeps new polls from starting and waits for the running ones, used on
// shutdown before the database closes
func (s *ReportInbox) Stop() {
	s.inflightMu.Lock()
	s.stopped = true
	s.inflightMu.Unlock()
	s.polls.Wait()
}

// jitter returns a random delay up to reportInboxJitter
func jitter() time.Duration {
	return time.Duration(rand.Int64N(int64(reportInboxJitter)))
}

// pollOutcome is what one poll read from the mailbox
type pollOutcome struct {
	newest    *time.Time
	processed int
	// listed counts the messages handled from the listing, without retries
	listed int
	// keepResume keeps the stored resume point when a poll made no progress
	keepResume bool
	matched    int
	more       bool
}

func (s *ReportInbox) poll(ctx context.Context, inbox *model.ReportInbox) {
	id := inbox.ID.MustGet()
	start := time.Now()
	// the inbox may have been changed, turned off or deleted after it was
	// listed as due. Such a poll does not read the mailbox at all.
	current, err := s.ReportInboxRepository.GetByID(ctx, &id)
	if err != nil || !current.Enabled || current.ConfigVersion != inbox.ConfigVersion {
		return
	}
	out, err := s.readMailbox(ctx, inbox)
	if err != nil && errors.Is(err, context.Canceled) {
		// the server is shutting down. The poll state is left as it was, so the
		// next start reads from the same place and no failure is recorded. The
		// messages handled so far are kept and still counted.
		if !s.discardIfMailboxChanged(inbox, start) {
			s.addCounts(inbox, out)
		}
		s.cleanUpIfDeleted(inbox)
		return
	}
	// the watermark only moves forward. A poll that read the overlap again can
	// end before it, and failed messages are kept as pending, not behind it.
	watermark := out.newest
	if inbox.Watermark != nil && (watermark == nil || inbox.Watermark.After(*watermark)) {
		kept := *inbox.Watermark
		watermark = &kept
	}
	state := &repository.ReportInboxPollState{
		Watermark:     watermark,
		PolledAt:      start,
		ProcessedDiff: out.processed,
		MatchedDiff:   out.matched,
	}
	if err != nil {
		state.FailureCount = inbox.FailureCount + 1
		state.LastError = err.Error()
		if len(state.LastError) > 1000 {
			state.LastError = state.LastError[:1000]
		}
		state.NextPollAt = start.Add(reportInboxBackoff(state.FailureCount, err) + jitter())
		var ge *graphError
		if errors.As(err, &ge) && ge.Status == http.StatusUnauthorized {
			s.forgetToken(inbox)
		}
		s.Logger.Warnw("reporting inbox poll failed",
			"reportInboxID", id.String(),
			"failures", state.FailureCount,
			"error", err,
		)
	} else {
		state.Success = true
		state.NextPollAt = start.Add(reportInboxPollInterval + jitter())
		if out.more {
			// more messages are waiting, read them on the next round and continue
			// after the newest one seen. Going back to the watermark would read
			// the same handled messages again and never get past them.
			state.NextPollAt = start
			state.ResumeAt = out.newest
		} else if out.keepResume {
			state.ResumeAt = inbox.ResumeAt
		}
	}
	// the poll context may have run out, the state is still worth saving
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	saved, saveErr := s.ReportInboxRepository.SavePollState(sctx, &id, inbox.ConfigVersion, state)
	if saveErr != nil {
		s.Logger.Errorw("failed to save reporting inbox poll state", "reportInboxID", id.String(), "error", saveErr)
		if !s.discardIfMailboxChanged(inbox, start) {
			s.addCounts(inbox, out)
		}
		s.cleanUpIfDeleted(inbox)
		return
	}
	if !saved {
		if s.discardIfMailboxChanged(inbox, start) {
			return
		}
		// the messages handled in this poll are recorded and will not be read
		// again, so they are still counted
		s.addCounts(inbox, out)
		// the inbox was changed or deleted while polling. A changed inbox starts
		// over from its own state, a deleted one must not keep message ids around.
		s.cleanUpIfDeleted(inbox)
		return
	}
	if watermark != nil {
		if pruneErr := s.ReportInboxRepository.PruneMessages(sctx, &id, watermark.Add(-reportInboxOverlap-24*time.Hour)); pruneErr != nil {
			s.Logger.Warnw("failed to prune reporting inbox messages", "reportInboxID", id.String(), "error", pruneErr)
		}
	}
	// a poll now request that came in while this poll ran
	s.inflightMu.Lock()
	again := s.pollAgain[id]
	delete(s.pollAgain, id)
	s.inflightMu.Unlock()
	if again {
		if err := s.ReportInboxRepository.PollNow(sctx, &id); err != nil {
			s.Logger.Warnw("failed to schedule a requested reporting inbox poll", "reportInboxID", id.String(), "error", err)
		}
	}
}

// reportInboxBackoff doubles the wait for every failure in a row, honoring a
// Retry-After from Graph when it asks for longer
func reportInboxBackoff(failures int, err error) time.Duration {
	if failures > 5 {
		failures = 5
	}
	wait := reportInboxPollInterval * time.Duration(1<<failures)
	if wait > reportInboxMaxBackoff {
		wait = reportInboxMaxBackoff
	}
	var ge *graphError
	if errors.As(err, &ge) && ge.RetryAfter > wait {
		wait = ge.RetryAfter
		if wait > reportInboxMaxBackoff {
			wait = reportInboxMaxBackoff
		}
	}
	return wait
}

// readMailbox processes new messages, oldest first. It stops at the first error
// without remembering the failing message, so that message is read again on the
// next poll.
func (s *ReportInbox) readMailbox(ctx context.Context, inbox *model.ReportInbox) (pollOutcome, error) {
	out := pollOutcome{}
	id := inbox.ID.MustGet()
	token, err := s.token(ctx, inbox)
	if err != nil {
		return out, err
	}
	senderDomains := s.senderDomainsLoader(inbox)
	started := time.Now()

	// read from a little before the newest processed message, but never mail
	// from before the inbox was turned on
	var since time.Time
	switch {
	case inbox.Watermark != nil:
		since = inbox.Watermark.Add(-reportInboxOverlap)
		if inbox.ReadSince != nil && inbox.ReadSince.After(since) {
			since = *inbox.ReadSince
		}
	case inbox.ReadSince != nil:
		since = *inbox.ReadSince
	default:
		since = time.Now()
	}
	if inbox.ResumeAt != nil && inbox.ResumeAt.After(since) {
		since = *inbox.ResumeAt
	}
	path := messagesFirstPage(inbox.Mailbox, since, reportInboxPageSize)
	pageSince := since
	for page := 0; page < reportInboxMaxPages && path != ""; page++ {
		messages, next, err := s.client().listMessages(ctx, token, path)
		if err != nil {
			return out, err
		}
		// one query per page for the messages already processed or pending
		pageIDs := make([]string, 0, len(messages))
		for i := range messages {
			pageIDs = append(pageIDs, messages[i].ID)
		}
		seenIDs, err := s.ReportInboxRepository.SeenMessages(ctx, &id, pageIDs)
		if err != nil {
			return out, err
		}
		for i := range messages {
			m := &messages[i]
			if time.Since(started) > reportInboxReadBudget {
				// continue after the newest message read so far on the next poll.
				// When nothing new was read, the second may not be fully listed,
				// so it is not skipped and the next poll comes at the normal time.
				out.more = out.listed > 0 || (out.newest != nil && out.newest.After(since.Truncate(time.Second)))
				// the backlog is not done, the next poll goes on from the same place
				out.keepResume = !out.more
				return out, nil
			}
			if !seenIDs[m.ID] {
				// a message started late in the poll that runs into the poll's
				// time limit is not to blame for it
				startedEarly := time.Since(started) < reportInboxReadBudget/3
				matched, err := s.processMessage(ctx, token, inbox, m, senderDomains)
				if err != nil {
					progressed := out.listed > 0 || (out.newest != nil && out.newest.After(since.Truncate(time.Second)))
					if isTimeout(err) && errors.Is(ctx.Err(), context.DeadlineExceeded) && !startedEarly && progressed {
						// the poll ran out of time on a message it started late, the
						// next poll continues from here and nothing failed
						out.more = true
						return out, nil
					}
					if messageNotToBlame(ctx, err) {
						// the whole mailbox is affected, the poll fails and backs off
						return out, err
					}
					// one message failing does not hold up the ones after it. It is
					// kept as pending and read again by its id on the next polls.
					if err := s.recordFailure(ctx, inbox, m.ID, m.ReceivedDateTime, err, &out); err != nil {
						return out, err
					}
				} else if err := s.resolveMessage(ctx, inbox, m.ID, m.ReceivedDateTime, matched, &out); err != nil {
					return out, err
				}
				out.listed++
			}
			received := m.ReceivedDateTime
			if out.newest == nil || received.After(*out.newest) {
				out.newest = &received
			}
			if ctx.Err() != nil {
				// no time left in this poll for the next messages
				out.more = true
				return out, nil
			}
		}
		// the next page starts at the arrival time of the last message, not at an
		// offset, so a message deleted meanwhile can not shift a later one out of
		// view. Messages on the boundary come again and are skipped as seen. Only
		// a page where every message has the same time uses the Graph next link.
		switch {
		case next == "" || len(messages) < reportInboxPageSize:
			path = ""
		case messages[len(messages)-1].ReceivedDateTime.After(pageSince):
			pageSince = messages[len(messages)-1].ReceivedDateTime
			path = messagesFirstPage(inbox.Mailbox, pageSince, reportInboxPageSize)
		default:
			path = next
		}
	}
	out.more = path != ""
	if out.more {
		noProgressGuard(&out, since)
		return out, nil
	}
	// messages that failed before are read again by their id once the new mail
	// is read, with the time that is left. The mailbox answered the listing, so
	// a missing message really is gone.
	if err := s.retryPendingMessages(ctx, token, inbox, senderDomains, started, &out); err != nil {
		return out, err
	}
	return out, nil
}

// retryPendingMessages reads the messages that failed before again, by their id
func (s *ReportInbox) retryPendingMessages(
	ctx context.Context,
	token string,
	inbox *model.ReportInbox,
	senderDomains sendersFunc,
	started time.Time,
	out *pollOutcome,
) error {
	id := inbox.ID.MustGet()
	// the ones that failed longest ago first, so the same few do not take every turn
	// a message that failed in this poll's listing waits for the next poll
	pending, err := s.ReportInboxRepository.PendingMessages(ctx, &id, reportInboxMaxRetries, started)
	if err != nil {
		return err
	}
	for _, p := range pending {
		if ctx.Err() != nil || time.Since(started) > reportInboxReadBudget {
			return nil
		}
		m, err := s.client().getMessage(ctx, token, inbox.Mailbox, p.MessageID)
		var ge *graphError
		if errors.As(err, &ge) && ge.Status == http.StatusNotFound && ge.Code == "ErrorItemNotFound" {
			// the message was deleted, there is nothing left to read
			if err := s.resolveMessage(ctx, inbox, p.MessageID, p.ReceivedAt, false, out); err != nil {
				return err
			}
			continue
		}
		matched := false
		if err == nil {
			matched, err = s.processMessage(ctx, token, inbox, m, senderDomains)
		}
		if err != nil {
			if messageNotToBlame(ctx, err) {
				return err
			}
			// counted also when the poll ran out of time on it, as a message that
			// always takes that long would otherwise stay first in line for good.
			// The 4 hour span keeps an outage from passing it over.
			if err := s.recordFailure(ctx, inbox, p.MessageID, p.ReceivedAt, err, out); err != nil {
				return err
			}
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		if err := s.resolveMessage(ctx, inbox, p.MessageID, p.ReceivedAt, matched, out); err != nil {
			return err
		}
	}
	return nil
}

// resolveMessage records a message as handled and counts it
func (s *ReportInbox) resolveMessage(ctx context.Context, inbox *model.ReportInbox, messageID string, receivedAt time.Time, matched bool, out *pollOutcome) error {
	id := inbox.ID.MustGet()
	// the poll context may have run out on this message, the record is still
	// written so the message is not read again
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.ReportInboxRepository.ResolveMessage(wctx, &id, messageID, receivedAt, matched); err != nil {
		return errs.Wrap(err)
	}
	out.processed++
	if matched {
		out.matched++
	}
	return nil
}

// recordFailure keeps a failed message as pending, or passes it over once it
// has failed often enough over a long enough time
func (s *ReportInbox) recordFailure(ctx context.Context, inbox *model.ReportInbox, messageID string, receivedAt time.Time, cause error, out *pollOutcome) error {
	id := inbox.ID.MustGet()
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	failures, first, err := s.ReportInboxRepository.MarkMessageFailed(wctx, &id, messageID, receivedAt)
	if err != nil {
		return errs.Wrap(err)
	}
	// both a number of tries and a time span must pass, so an outage at
	// Microsoft does not drop reports however often the inbox is polled
	if failures >= reportInboxMessageTries && time.Since(first) >= reportInboxGiveUpAfter {
		s.Logger.Warnw("passing over a reported message that keeps failing",
			"reportInboxID", id.String(),
			"error", cause,
		)
		return s.resolveMessage(ctx, inbox, messageID, receivedAt, false, out)
	}
	s.Logger.Warnw("reading a reported message failed, trying again on the next poll",
		"reportInboxID", id.String(),
		"error", cause,
	)
	return nil
}

// noProgressGuard moves a poll that read only already seen messages of one
// second past that second. More than a full poll of messages share it, so
// continuing at it would read the same ones again forever. Graph filters on
// whole seconds, so the comparison does too.
func noProgressGuard(out *pollOutcome, since time.Time) {
	sinceSecond := since.Truncate(time.Second)
	if out.listed == 0 && out.newest != nil && !out.newest.After(sinceSecond) {
		next := sinceSecond.Add(time.Second)
		out.newest = &next
	}
}

// isTimeout tells if an error is a deadline or a timed out request
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ue *url.Error
	return errors.As(err, &ue) && ue.Timeout()
}

// skippable tells if a Graph error only affects this one item, so the message can
// be passed over instead of failing the poll. Any client error is about the item,
// for example a removed attachment, except a failed sign in, missing access, a
// timeout and throttling, which can affect the whole mailbox and are retried.
func skippable(err error) bool {
	var ge *graphError
	if !errors.As(err, &ge) {
		return false
	}
	switch ge.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return false
	}
	return ge.Status >= 400 && ge.Status < 500
}

// isEMLAttachment tells if a file attachment is an email saved as a file
func isEMLAttachment(a *graphAttachment) bool {
	if a.ODataType != "#microsoft.graph.fileAttachment" {
		return false
	}
	ct := strings.ToLower(a.ContentType)
	return ct == "message/rfc822" || strings.HasSuffix(strings.ToLower(a.Name), ".eml")
}

// CancelPoll stops a running poll of an inbox, so a deleted or changed inbox is
// not read any further. The poll then saves nothing.
func (s *ReportInbox) CancelPoll(id uuid.UUID) {
	s.inflightMu.Lock()
	cancel := s.cancels[id]
	s.inflightMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// discardIfMailboxChanged removes the rows a poll wrote when the mailbox was
// changed while it ran. They belong to the old mailbox and must not be retried
// against the new one. It tells if that was the case.
func (s *ReportInbox) discardIfMailboxChanged(inbox *model.ReportInbox, since time.Time) bool {
	id := inbox.ID.MustGet()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	current, err := s.ReportInboxRepository.GetByID(ctx, &id)
	if err != nil || strings.EqualFold(current.Mailbox, inbox.Mailbox) {
		return false
	}
	if delErr := s.ReportInboxRepository.DeleteMessagesSince(ctx, &id, since); delErr != nil {
		s.Logger.Warnw("failed to remove messages of a changed mailbox", "reportInboxID", id.String(), "error", delErr)
	}
	return true
}

// cleanUpIfDeleted removes the message ids a poll stored for an inbox that was
// deleted while it ran
func (s *ReportInbox) cleanUpIfDeleted(inbox *model.ReportInbox) {
	id := inbox.ID.MustGet()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.ReportInboxRepository.GetByID(ctx, &id); errors.Is(err, gorm.ErrRecordNotFound) {
		if delErr := s.ReportInboxRepository.DeleteByID(ctx, &id); delErr != nil {
			s.Logger.Warnw("failed to clean up a deleted reporting inbox", "reportInboxID", id.String(), "error", delErr)
		}
	}
}

// accessLimitedStep tries to open the inbox of each address. One that opens means
// access is not limited. Several addresses are tried, as an address without a
// mailbox, such as a group or an alias, can not show it.
func (s *ReportInbox) accessLimitedStep(ctx context.Context, token string, others []string, delegated bool, res *ReportInboxTestResult) ReportInboxTestStep {
	const name = "Access is limited"
	if len(others) == 0 {
		return ReportInboxTestStep{Name: name, OK: true, Message: "Skipped, no other recipient on the same domain to check against"}
	}
	var denied []string
	var lastErr error
	for _, other := range others {
		err := s.client().canOpenInbox(ctx, token, other)
		switch {
		case err == nil:
			res.Warnings = append(res.Warnings, "Access is not limited to the report mailbox. Limit the app with Exchange RBAC for Applications before enabling.")
			return ReportInboxTestStep{Name: name, OK: false, Message: "Could also open the mailbox of " + other}
		case isAccessRefused(err, delegated):
			denied = append(denied, other)
		default:
			// try the next address. Not found means the address has no mailbox,
			// which says nothing about access, like any other error.
			lastErr = err
		}
	}
	if len(denied) == 0 {
		return ReportInboxTestStep{Name: name, OK: false, Message: "Could not verify, no other mailbox could be checked: " + lastErr.Error()}
	}
	return ReportInboxTestStep{Name: name, OK: true, Message: "Could not open the mailbox of " + strings.Join(denied, ", ")}
}

// addCounts adds the processed and matched counts of a poll whose state was not
// saved
func (s *ReportInbox) addCounts(inbox *model.ReportInbox, out pollOutcome) {
	if out.processed == 0 && out.matched == 0 {
		return
	}
	id := inbox.ID.MustGet()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.ReportInboxRepository.AddCounts(ctx, &id, out.processed, out.matched); err != nil {
		s.Logger.Warnw("failed to add reporting inbox counts", "reportInboxID", id.String(), "error", err)
	}
}

// messageNotToBlame tells if an error affects the whole mailbox rather than the
// message being read: a shutdown, a failed sign in, throttling or a connection
// that could not be made
func messageNotToBlame(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return true
	}
	var ge *graphError
	if errors.As(err, &ge) {
		switch ge.Status {
		case http.StatusUnauthorized, http.StatusTooManyRequests:
			return true
		}
	}
	var ue *url.Error
	return errors.As(err, &ue) && !ue.Timeout()
}

// reportInboxGiveUpAfter is how long a message must keep failing before it is
// passed over, so an outage at Microsoft does not drop reports. A variable so
// tests can shorten it.
var reportInboxGiveUpAfter = 4 * time.Hour

// sendersFunc returns the sender addresses and domains of an inbox scope
type sendersFunc func(context.Context) (*repository.Senders, error)

// senderDomainsLoader loads the senders of the inbox scope the first time a
// message needs them, so a poll without new reports does not query them
func (s *ReportInbox) senderDomainsLoader(inbox *model.ReportInbox) sendersFunc {
	var senders *repository.Senders
	return func(ctx context.Context) (*repository.Senders, error) {
		if senders != nil {
			return senders, nil
		}
		v, err := s.ReportInboxRepository.SenderInfo(ctx, companyIDPtr(inbox.CompanyID))
		if err != nil {
			return nil, err
		}
		senders = v
		return senders, nil
	}
}

// errNotAnEmail is returned for an attached item that is not an email
var errNotAnEmail = errors.New("attached item is not an email")

// processMessage matches one mailbox message.
//
// An email attached as an Outlook item, which is what the Outlook Report button
// sends, is checked in steps so that mail Phishing Club did not send is not read:
// first only its Message-ID and sender are fetched. A known Message-ID is a match.
// A sender domain that none of the scope's emails use means it is not ours and
// nothing more is fetched. Only when the sender domain is ours are the headers
// and body fetched to look for a recipient id in the links. The attached email's
// own files are never downloaded.
//
// An email attached as an .eml file can only be read by downloading it. A
// message without an attached email is a plain forward, for it the headers and
// body of the message itself are read.
func (s *ReportInbox) processMessage(
	ctx context.Context,
	token string,
	inbox *model.ReportInbox,
	m *graphMessage,
	senderDomains sendersFunc,
) (bool, error) {
	reporter := m.From.EmailAddress.Address
	// drafts and mail sent from the mailbox itself, such as an analyst forwarding
	// a report from it, are not reports
	if m.IsDraft || equalFoldEmail(reporter, inbox.Mailbox) {
		return false, nil
	}
	if m.HasAttachments {
		// without the attachment list the reported email is unknown, and the
		// user's own message around it must not be read instead, so retry later
		attachments, err := s.client().listAttachments(ctx, token, inbox.Mailbox, m.ID)
		if err != nil {
			return false, err
		}
		tried := 0
		// fetched caps the attachment requests of one message, also for items
		// that turn out not to be emails, so a message with many attached items
		// can not hold up the poll
		fetched := 0
		capped := false
		var lastErr error
		failed := 0
		for i := range attachments {
			a := &attachments[i]
			isItem := a.ODataType == "#microsoft.graph.itemAttachment"
			if !isItem && !isEMLAttachment(a) {
				continue
			}
			if tried >= reportInboxMaxAttachment {
				break
			}
			if fetched >= reportInboxMaxFetches {
				capped = true
				break
			}
			tried++
			fetched++
			var candidates *reportCandidates
			if isItem {
				candidates, err = s.itemCandidates(ctx, token, inbox, m.ID, a.ID, senderDomains)
			} else {
				candidates, err = s.emlCandidates(ctx, token, inbox, m.ID, a, senderDomains)
			}
			if errors.Is(err, errNotAnEmail) {
				// not the reported email, such as an attached meeting invite, so
				// the message is still read as a forward when nothing else is
				tried--
				continue
			}
			if err != nil {
				if skippable(err) {
					lastErr = err
					failed++
					continue
				}
				return false, err
			}
			if candidates == nil {
				continue
			}
			matched, err := s.matchCandidates(ctx, inbox, candidates, m.ReceivedDateTime, reporter)
			if err != nil || matched {
				return matched, err
			}
		}
		// the cap stopped the loop before an email was found, the reported email
		// may be further down, so the user's own message is not read instead
		if capped && tried == 0 {
			return false, nil
		}
		// an attached email that could not be read may be the reported one, so
		// retry later. After a few polls the message is passed over.
		if failed > 0 {
			return false, lastErr
		}
		// the attached email is the reported one, the message around it is the
		// user's report and is not read
		if tried > 0 {
			return false, nil
		}
	}
	// a plain forward has no attached email, the reported email is the message
	// itself. Only its headers and text body are fetched, never attached files.
	// an error is retried, after a few polls the message is passed over
	content, err := s.client().messageContent(ctx, token, inbox.Mailbox, m.ID)
	if err != nil {
		return false, err
	}
	if content == nil {
		return false, nil
	}
	// the delivered simulation email itself can land in the mailbox, copied or
	// redirected by a rule. That is not a report. A forward always gets a new
	// Message-ID and comes from the person reporting, while the copy keeps a
	// Message-ID Phishing Club sent with, or comes from a simulation sender.
	used, err := s.ReportInboxRepository.MessageIDUsed(ctx, normalizeMessageID(content.InternetMessageID))
	if err != nil {
		return false, err
	}
	if used {
		return false, nil
	}
	senders, err := senderDomains(ctx)
	if err != nil {
		return false, err
	}
	if senders.Addresses[repository.BareAddress(reporter)] {
		return false, nil
	}
	return s.matchCandidates(ctx, inbox, contentCandidates(content, senders.Served), m.ReceivedDateTime, reporter)
}

// itemCandidates returns match candidates for an attached Outlook item, or nil
// when it was not sent by Phishing Club
func (s *ReportInbox) itemCandidates(
	ctx context.Context,
	token string,
	inbox *model.ReportInbox,
	messageID string,
	attachmentID string,
	senderDomains sendersFunc,
) (*reportCandidates, error) {
	// an item that is not an email comes back with its type only. An email whose
	// fields are refused comes back as an error, so it is kept and retried.
	item, err := s.client().itemAttachment(ctx, token, inbox.Mailbox, messageID, attachmentID, "internetMessageId,from")
	if err != nil || item == nil {
		return nil, err
	}
	// an attached item that is not an email, such as a calendar event or a
	// contact, is not a report of a simulation email. A meeting request is an
	// email and its type names a message.
	if item.ODataType != "" && !strings.Contains(strings.ToLower(item.ODataType), "message") {
		return nil, errNotAnEmail
	}
	// a known Message-ID decides it without reading anything else
	if id := normalizeMessageID(item.InternetMessageID); id != "" {
		if _, lookupErr := s.CampaignRecipientRepository.GetByMessageID(ctx, id); lookupErr == nil {
			return &reportCandidates{MessageIDs: []string{id}}, nil
		} else if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return nil, errs.Wrap(lookupErr)
		}
	}
	senders, err := senderDomains(ctx)
	if err != nil {
		return nil, err
	}
	if !senders.Domains[repository.AddressDomain(item.From.EmailAddress.Address)] {
		return nil, nil
	}
	// sent from one of our domains but no known Message-ID, for example through
	// an API sender, so look for the recipient id in the headers and links
	if item.Body == nil {
		item, err = s.client().itemAttachment(ctx, token, inbox.Mailbox, messageID, attachmentID, "internetMessageId,from,internetMessageHeaders,body")
		if err != nil || item == nil {
			return nil, err
		}
	}
	return itemCandidates(item, senders.Served), nil
}

// emlCandidates downloads an email attached as a file and returns its candidates
func (s *ReportInbox) emlCandidates(
	ctx context.Context,
	token string,
	inbox *model.ReportInbox,
	messageID string,
	a *graphAttachment,
	senderDomains sendersFunc,
) (*reportCandidates, error) {
	if a.Size > reportMaxMIMEBytes {
		return nil, nil
	}
	raw, tooLarge, err := s.client().attachmentMIME(ctx, token, inbox.Mailbox, messageID, a.ID)
	if err != nil || tooLarge {
		return nil, err
	}
	senders, err := senderDomains(ctx)
	if err != nil {
		return nil, err
	}
	return extractReportCandidates(raw, senders.Served), nil
}

// matchCandidates tries the candidates in order and records the first campaign
// recipient that is in the inbox scope. Candidates outside the scope are ignored.
func (s *ReportInbox) matchCandidates(
	ctx context.Context,
	inbox *model.ReportInbox,
	c *reportCandidates,
	reportedAt time.Time,
	reporter string,
) (bool, error) {
	scope := companyIDPtr(inbox.CompanyID)
	// found stops the search at the first recipient in scope. Only a recorded
	// event counts as matched, a skipped one (closed campaign, already reported)
	// does not.
	try := func(cr *model.CampaignRecipient) (found bool, recorded bool, err error) {
		res, err := s.CampaignService.RecordInboxReport(ctx, cr, scope, reportedAt, reporter, inbox.Mailbox)
		if err != nil {
			return false, false, err
		}
		return res != InboxReportOutOfScope, res == InboxReportRecorded, nil
	}
	for _, messageID := range c.MessageIDs {
		cr, err := s.CampaignRecipientRepository.GetByMessageID(ctx, messageID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return false, errs.Wrap(err)
		}
		if found, recorded, err := try(cr); err != nil || found {
			return recorded, err
		}
	}
	for i := range c.RecipientIDs {
		cr, err := s.CampaignRecipientRepository.GetByCampaignRecipientID(ctx, &c.RecipientIDs[i])
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return false, errs.Wrap(err)
		}
		if found, recorded, err := try(cr); err != nil || found {
			return recorded, err
		}
	}
	// a lure code in a link, for a campaign that carries a code in its links
	// instead of the recipient id. The code is only looked up on the domain of
	// the link, the same way a click is, and a code set by hand is not used as
	// it could be guessed.
	for _, link := range c.LureLinks {
		cr, err := s.CampaignRecipientRepository.GetByLureCodeOnDomain(ctx, link.Code, link.Domain)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return false, errs.Wrap(err)
		}
		if custom, cErr := cr.LureCodeCustom.Get(); cErr == nil && custom {
			continue
		}
		if found, recorded, err := try(cr); err != nil || found {
			return recorded, err
		}
	}
	return false, nil
}
