package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	neturl "net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/version"
	"golang.org/x/mod/semver"
	"gorm.io/gorm"
)

// MissingVariable is a required value the user must supply, with what the UI
// needs to render a field for it.
type MissingVariable struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Options     []string `json:"options,omitempty"`
	// Pick, when set, names an entity kind the installer chooses from their own
	// existing entities, so the UI shows a picker and the chosen entity name is the
	// value.
	Pick string `json:"pick,omitempty"`
	// Scope names the referenced package a variable belongs to when a bundle does
	// not share variables by name; empty when the value is shared. Key is the exact
	// map key the caller must send back for this value (the name when shared, a
	// package scoped key when not), so the UI never has to reconstruct it.
	Scope string `json:"scope,omitempty"`
	Key   string `json:"key"`
}

// varScopeSep joins a package id and a variable name into one map key when a
// bundle does not share variables across its referenced packages.
const varScopeSep = "\x1f"

// varKey is the key in the vars map for one package's variable, honoring the share mode:
// the bare name when shared, a package scoped key when each package keeps its own.
func varKey(pkgID, name string, share bool) string {
	if share {
		return name
	}
	return pkgID + varScopeSep + name
}

// varScopeKey returns the Scope and Key the UI is given for one package's
// variable: no scope when shared, the package id when not.
func varScopeKey(pkgID, name string, share bool) (scope, key string) {
	if !share {
		scope = pkgID
	}
	return scope, varKey(pkgID, name, share)
}

// missingVar is the prompt for one package variable that still needs a value.
func missingVar(pkgID string, v registry.Variable, share bool) MissingVariable {
	scope, key := varScopeKey(pkgID, v.Name, share)
	return MissingVariable{
		Name: v.Name, Type: v.Type, Description: v.Description,
		Options: v.Options, Pick: v.Pick, Scope: scope, Key: key,
	}
}

// installedShare is the variable share mode an installed package was installed
// with: off when the install stored its values under package scoped keys, else
// the mode its manifest declares.
func installedShare(ip *database.InstalledPackage, m *registry.Manifest) bool {
	if ip != nil && ip.VariablesScoped {
		return false
	}
	return sharesVariables(m)
}

// sharesVariables reports whether a package fills a variable name once for
// itself and every package it installs with it. Only a bundle can turn that
// off; for any other package shareVariables has no meaning and is ignored.
func sharesVariables(m *registry.Manifest) bool {
	return m == nil || m.Type != "bundle" || m.ShareVariables == nil || *m.ShareVariables
}

// ErrMissingVariables is returned by Install and Update so the UI can prompt for
// the missing values.
type ErrMissingVariables struct {
	Variables []MissingVariable
}

func (e ErrMissingVariables) Error() string {
	names := make([]string, 0, len(e.Variables))
	for _, v := range e.Variables {
		names = append(names, v.Name)
	}
	return "missing required variables: " + strings.Join(names, ", ")
}

// UpdateConflict is one resource the new package version changes that waits for
// the operator's choice: its local content differs from what was imported, or the
// change moves an OAuth binding or where a token or secret is sent. SourceName is
// the package declared name used to match the resource; CurrentName is the entity
// name now (differs when renamed).
type UpdateConflict struct {
	ResourceType string `json:"resourceType"`
	SourceName   string `json:"sourceName"`
	CurrentName  string `json:"currentName"`
	// Note is extra text shown with the choice, such as what overwrite replaces
	// on an asset.
	Note string `json:"note,omitempty"`
	// FilterLoss is set on an asset conflict when applying the update would
	// take allow/deny lists off the asset.
	FilterLoss *AssetFilterLoss `json:"filterLoss,omitempty"`
	// Reason says why the item needs a choice when it is not a local edit, such as
	// an API sender the update would switch to another OAuth provider.
	Reason string `json:"reason,omitempty"`
	// KeepOnly is set when only keep can be applied, such as an API sender the
	// update would bind to an OAuth provider it may not use.
	KeepOnly bool `json:"keepOnly,omitempty"`
	// Member is set on a conflict for a referenced package the new bundle
	// version drops (ResourceType "member", SourceName its package id).
	Member *DroppedMemberImpact `json:"member,omitempty"`
}

// Key is the resolution map key for this conflict: "type:sourceName".
func (c UpdateConflict) Key() string { return c.ResourceType + ":" + c.SourceName }

// ErrUpdateConflicts is returned when an update has conflicts the caller has not
// said how to resolve. The caller shows a per resource choice (keep, overwrite,
// both) and retries with resolutions.
type ErrUpdateConflicts struct {
	Conflicts []UpdateConflict
}

func (e ErrUpdateConflicts) Error() string {
	return fmt.Sprintf("update has %d conflicts that need a choice", len(e.Conflicts))
}

// ReportTemplateOverwrite is one global report template an install or update
// would overwrite. Name describes the current template, since report templates
// have no name of their own.
type ReportTemplateOverwrite struct {
	Kind       string `json:"kind"`
	TemplateID string `json:"templateId"`
	Name       string `json:"name"`
	Customized bool   `json:"customized"`
	SourceName string `json:"sourceName"`
	PackageID  string `json:"packageId"`
}

// ErrReportTemplateOverwrite is returned by Install and Update when the package
// would overwrite a global report template whose kind the caller has not
// confirmed. Nothing has been written. The caller asks the user and retries with
// every listed kind confirmed.
type ErrReportTemplateOverwrite struct {
	Templates []ReportTemplateOverwrite
}

func (e ErrReportTemplateOverwrite) Error() string {
	kinds := make([]string, 0, len(e.Templates))
	for _, t := range e.Templates {
		kinds = append(kinds, t.Kind)
	}
	return "installing overwrites the current report template: " + strings.Join(kinds, ", ")
}

// resolution choices for a conflicted resource.
const (
	resolveKeep      = "keep"      // leave the local edit, do not take upstream
	resolveOverwrite = "overwrite" // replace the local edit with the new version
	resolveBoth      = "both"      // keep the local copy detached, install new fresh
)

// resolutionFor returns the chosen resolution for a resource, defaulting to keep
// so an unspecified conflict never silently destroys a local edit.
func resolutionFor(resolutions map[string]string, resourceType, sourceName string) string {
	if r, ok := resolutions[resourceType+":"+sourceName]; ok {
		switch r {
		case resolveKeep, resolveOverwrite, resolveBoth:
			return r
		}
	}
	return resolveKeep
}

// ContentRegistry is the consumer side of the content registry (baitbox).
type ContentRegistry struct {
	Common
	RegistryRepository          *repository.Registry
	Page                        *Page
	Email                       *Email
	Asset                       *Asset
	File                        *File
	Domain                      *Domain
	Proxy                       *Proxy
	Script                      *Script
	AllowDeny                   *AllowDeny
	RemoteBrowser               *RemoteBrowser
	ReportTemplate              *ReportTemplate
	SMTPConfiguration           *SMTPConfiguration
	APISender                   *APISender
	OAuthProvider               *OAuthProvider
	Webhook                     *Webhook
	Attachment                  *Attachment
	CampaignTemplate            *CampaignTemplate
	PageRepository              *repository.Page
	EmailRepository             *repository.Email
	DomainRepository            *repository.Domain
	ProxyRepository             *repository.Proxy
	SMTPConfigurationRepository *repository.SMTPConfiguration
	APISenderRepository         *repository.APISender
	OAuthProviderRepository     *repository.OAuthProvider
	CampaignTemplateRepository  *repository.CampaignTemplate
	IdentifierRepository        *repository.Identifier

	// opSem runs install, update and uninstall one at a time. Two of them on the
	// same package, or two bundles that share a referenced package, would
	// otherwise read the same state and then both write it. It is a channel of
	// size one so waiting for it can stop when the request ends. Only the
	// exported entry points take it, and only after the registry downloads are
	// done; the internal functions they call never do. Reads do not take it.
	opSem     chan struct{}
	opSemOnce sync.Once
}

// ErrRegistryBusy is returned when the request ends while waiting for another
// install, update or uninstall to finish.
var ErrRegistryBusy = errs.NewValidationError(errors.New(
	"another install, update or uninstall is still running, try again when it has finished",
))

// rollbackTimeout bounds how long undoing a failed install or update may take.
const rollbackTimeout = 2 * time.Minute

// lockOps waits until no other install, update or uninstall runs and returns the
// function that releases the lock. The wait stops when ctx ends.
func (s *ContentRegistry) lockOps(ctx context.Context) (func(), error) {
	s.opSemOnce.Do(func() {
		if s.opSem == nil {
			s.opSem = make(chan struct{}, 1)
		}
	})
	select {
	case s.opSem <- struct{}{}:
		return func() { <-s.opSem }, nil
	case <-ctx.Done():
		return nil, ErrRegistryBusy
	}
}

// InstallResult reports what an install or update did, per content type.
type InstallResult struct {
	PackageID string   `json:"packageId"`
	Pages     []string `json:"pages"`
	Emails    []string `json:"emails"`
	Assets    []string `json:"assets"`
	Others    []string `json:"others"`
	Skipped   []string `json:"skipped"`
	// pendingLinks are created assets whose allow deny lists are set once every
	// list of the install or update exists. Their files are written only then.
	pendingLinks []pendingAssetLink
	// createdAssets are the assets this install or update created, by path, so
	// a second package of the same operation that ships the path is compared
	// with what was created and not with the stored asset, whose lists and file
	// may still be pending.
	createdAssets map[string]createdAsset
	// listScopes resolve the list names of the assets of each package of the
	// install or update, by package id, see listScope.
	listScopes map[string]*listScope
}

// listScope returns the list scope of the package id, nil when it has none.
func (r *InstallResult) listScope(packageID string) *listScope {
	if r == nil {
		return nil
	}
	return r.listScopes[packageID]
}

// RegistryView is a registry connection for listing (token never serialized).
type RegistryView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	InsecureTLS bool   `json:"insecureTLS"`
	HasToken    bool   `json:"hasToken"`
}

// RegistryStatusView reports whether one registry answered its info endpoint.
// ConnectError carries a short reason when it did not.
type RegistryStatusView struct {
	ID           string `json:"id"`
	Connected    bool   `json:"connected"`
	ConnectError string `json:"connectError,omitempty"`
	// TokenRefused is set when the registry answered 401 or 403.
	TokenRefused bool `json:"tokenRefused,omitempty"`
	// Incompatible holds the reason this instance cannot use the registry, set
	// when the registry requires a newer Phishing Club version.
	Incompatible string `json:"incompatible,omitempty"`
}

// InstalledView is one installed package for the Updates/Installed listing.
type InstalledView struct {
	ID              string `json:"id"`
	RegistryID      string `json:"registryId"`
	PackageID       string `json:"packageId"`
	PackageType     string `json:"packageType"`
	Version         string `json:"version"`
	CurrentVersion  string `json:"currentVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	// RegistryName is empty and RegistryRemoved set when the registry this was
	// installed from no longer exists.
	RegistryName          string `json:"registryName"`
	RegistryRemoved       bool   `json:"registryRemoved"`
	InstalledAsDependency bool   `json:"installedAsDependency"`
	// Status is one of the installedStatus values.
	Status string `json:"status"`
	// StatusMessage is the token refused message on a tokenRefused row.
	StatusMessage string `json:"statusMessage,omitempty"`
}

// installedStatus values for InstalledView.Status. Unknown means the registry did
// not answer for the package (unreachable, or the package is not there).
// TokenRefused means the registry answered 401 or 403.
const (
	installedStatusUpdate       = "updateAvailable"
	installedStatusCurrent      = "upToDate"
	installedStatusUnknown      = "unknown"
	installedStatusNoSource     = "registryRemoved"
	installedStatusTokenRefused = "tokenRefused"
)

// authorize runs the standard permission check and returns the audit event.
func (s *ContentRegistry) authorize(session *model.Session, action string) (*AuditEvent, error) {
	ae := NewAuditEvent(action, session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return ae, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return ae, errs.ErrAuthorizationFailed
	}
	return ae, nil
}

// auditFailure records a mutation that passed authorization and then failed, so
// the audit log shows the attempt with its error. Missing variables and update
// conflicts ask the user for more input and are not failures, so they are skipped.
// Call it deferred, after authorize succeeds, with a pointer to the named error.
func (s *ContentRegistry) auditFailure(ae *AuditEvent, errp *error) {
	err := *errp
	if err == nil || errors.Is(err, errs.ErrAuthorizationFailed) {
		return
	}
	var mv ErrMissingVariables
	var uc ErrUpdateConflicts
	var ro ErrReportTemplateOverwrite
	if errors.As(err, &mv) || errors.As(err, &uc) || errors.As(err, &ro) {
		return
	}
	ae.Details["result"] = "failed"
	ae.Details["errorClass"] = auditErrorClass(err)
	s.AuditLogAuthorized(ae)
}

// auditErrorClass names the kind of a failure for the audit log. The error text
// is not logged: it can carry values the user supplied, such as a substituted
// secret variable inside a header, a url or a template, and those must not end
// up in the audit log. The full error still goes back to the caller.
func auditErrorClass(err error) string {
	var ve errs.ValidationError
	var se errs.SyntaxError
	var ce errs.ConflictError
	var oe errs.OperationalError
	var cu errs.CustomError
	switch {
	case errors.As(err, &ve):
		return "validation"
	case errors.As(err, &se):
		return "syntax"
	case errors.As(err, &ce):
		return "conflict"
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "not found"
	case errors.As(err, &oe):
		return "operational"
	case errors.As(err, &cu):
		return "custom"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	default:
		return "internal"
	}
}

func (s *ContentRegistry) clientFor(reg *database.Registry) *registry.Client {
	return registry.NewClient(reg.URL, reg.Token, reg.InsecureTLS)
}

// registryProbeTimeout bounds a connection test so adding or listing a registry
// never hangs on an unreachable host.
const registryProbeTimeout = 8 * time.Second

// testConnection fetches the registry's info endpoint with the read token within
// a short timeout to check it is reachable and accepts the token. The info it
// returns is reused for the version check, so one probe reads the endpoint once.
func (s *ContentRegistry) testConnection(ctx context.Context, client *registry.Client) (*registry.RegistryInfo, error) {
	tctx, cancel := context.WithTimeout(ctx, registryProbeTimeout)
	defer cancel()
	return client.Info(tctx)
}

// checkRegistryCompatible refuses to use a registry that requires a newer
// Phishing Club version than this instance runs. A registry that reports no
// minimum, one this instance meets, missing info (a transient
// error), or a non semver local version (a dev build) is allowed.
func (s *ContentRegistry) checkRegistryCompatible(info *registry.RegistryInfo) error {
	if info == nil {
		return nil
	}
	min := strings.TrimSpace(info.MinPhishingClubVersion)
	if min == "" || min == "0.0.0" {
		return nil
	}
	cur := version.GetSemver()
	mv := ensureVPrefix(min)
	cv := ensureVPrefix(cur)
	if !semver.IsValid(mv) || !semver.IsValid(cv) {
		return nil
	}
	if semver.Compare(cv, mv) < 0 {
		return errs.NewValidationError(fmt.Errorf(
			"this Bait Box requires Phishing Club %s or newer; this instance is %s, "+
				"update Phishing Club to connect",
			min, cur,
		))
	}
	return nil
}

func (s *ContentRegistry) AddRegistry(
	ctx context.Context, session *model.Session, name, url, token string, insecure bool,
) (_ *uuid.UUID, err error) {
	ae, err := s.authorize(session, "Registry.Add")
	if err != nil {
		return nil, err
	}
	defer s.auditFailure(ae, &err)
	name = strings.TrimSpace(name)
	url = strings.TrimSpace(url)
	ae.Details["url"] = url
	token = strings.TrimSpace(token)
	if name == "" || url == "" {
		return nil, errs.NewValidationError(fmt.Errorf("name and URL are required"))
	}
	if err := checkRegistryURL(url); err != nil {
		return nil, err
	}
	// an empty token is allowed for a Bait Box in public mode. a private one
	// refuses it, and the probe below reports that as a refused token.
	client := registry.NewClient(url, token, insecure)
	defer client.CloseIdle()
	// refuse a registry that cannot be reached rather than saving a dead connection.
	info, err := s.testConnection(ctx, client)
	if err != nil {
		return nil, connectError(url, err)
	}
	if err := s.checkRegistryCompatible(info); err != nil {
		return nil, err
	}
	row := &database.Registry{
		Name:        name,
		URL:         strings.TrimRight(url, "/"),
		Token:       token,
		InsecureTLS: insecure,
	}
	id, err := s.RegistryRepository.InsertRegistry(ctx, row)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	ae.Details["registryId"] = id.String()
	s.AuditLogAuthorized(ae)
	return id, nil
}

// UpdateRegistryInput is an edit of a stored registry. An empty URL keeps the
// stored one. An empty Token keeps the stored token unless ClearToken is set.
type UpdateRegistryInput struct {
	Name       string
	URL        string
	Token      string
	ClearToken bool
	Insecure   bool
}

// UpdateRegistry edits a stored registry, see UpdateRegistryInput.
func (s *ContentRegistry) UpdateRegistry(
	ctx context.Context, session *model.Session, id *uuid.UUID, in UpdateRegistryInput,
) (err error) {
	ae, err := s.authorize(session, "Registry.Update")
	if err != nil {
		return err
	}
	defer s.auditFailure(ae, &err)
	ae.Details["registryId"] = id.String()
	if strings.TrimSpace(in.Token) != "" {
		ae.Details["tokenChanged"] = true
	}
	if in.ClearToken {
		ae.Details["tokenRemoved"] = true
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return errs.NewValidationError(fmt.Errorf("name is required"))
	}
	reg, err := s.RegistryRepository.GetRegistryByID(ctx, id)
	if err != nil {
		return errs.Wrap(err)
	}
	url := strings.TrimSpace(in.URL)
	if url == "" {
		url = reg.URL
	}
	if err := checkRegistryURL(url); err != nil {
		return err
	}
	url = strings.TrimRight(url, "/")
	ae.Details["url"] = url
	token, err := effectiveToken(reg, in)
	if err != nil {
		return err
	}
	// test again with the effective url, token and tls setting so an edit that breaks the
	// connection is refused rather than stored.
	client := registry.NewClient(url, token, in.Insecure)
	defer client.CloseIdle()
	if _, err := s.testConnection(ctx, client); err != nil {
		return connectError(url, err)
	}
	if err := s.RegistryRepository.UpdateRegistry(ctx, id, name, url, token, in.Insecure); err != nil {
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// effectiveToken picks the token an edit stores.
func effectiveToken(reg *database.Registry, in UpdateRegistryInput) (string, error) {
	token := strings.TrimSpace(in.Token)
	if in.ClearToken {
		if token != "" {
			return "", errs.NewValidationError(fmt.Errorf("enter a new token or remove the token, not both"))
		}
		return "", nil
	}
	if token != "" {
		return token, nil
	}
	return reg.Token, nil
}

// checkRegistryURL checks a registry url. The registry is always served over
// TLS and the read token travels on every request, so a plain http url is
// refused. A self signed certificate is allowed through the insecure flag.
func checkRegistryURL(raw string) error {
	u, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return errs.NewValidationError(fmt.Errorf("the Bait Box URL is not valid"))
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return errs.NewValidationError(fmt.Errorf("the Bait Box URL must use https"))
	}
	return nil
}

// ListRegistries returns the configured registries without their tokens. It does
// not contact any registry, so one that is down never slows the list. The UI
// reads each registry's reachability through RegistryStatus.
func (s *ContentRegistry) ListRegistries(ctx context.Context, session *model.Session) ([]RegistryView, error) {
	if _, err := s.authorize(session, "Registry.List"); err != nil {
		return nil, err
	}
	rows, err := s.RegistryRepository.GetAllRegistries(ctx)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	out := make([]RegistryView, len(rows))
	for i, r := range rows {
		out[i] = RegistryView{
			ID:          r.ID.String(),
			Name:        r.Name,
			URL:         r.URL,
			InsecureTLS: r.InsecureTLS,
			HasToken:    r.Token != "",
		}
	}
	return out, nil
}

// RegistryStatus probes one registry's info endpoint under registryProbeTimeout
// and reports whether it answered. An unreachable registry is a normal result,
// not an error; ConnectError then carries a short reason, and TokenRefused is
// set when the registry refused the token.
func (s *ContentRegistry) RegistryStatus(
	ctx context.Context, session *model.Session, id *uuid.UUID,
) (*RegistryStatusView, error) {
	if _, err := s.authorize(session, "Registry.Status"); err != nil {
		return nil, err
	}
	reg, err := s.RegistryRepository.GetRegistryByID(ctx, id)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	out := &RegistryStatusView{ID: reg.ID.String()}
	client := s.clientFor(reg)
	defer client.CloseIdle()
	info, perr := s.testConnection(ctx, client)
	if perr != nil {
		out.ConnectError = connErrReason(perr)
		out.TokenRefused = errors.Is(perr, registry.ErrUnauthorized)
		return out, nil
	}
	out.Connected = true
	// the same answer carries the version check, so the page does not need a
	// second probe to learn a registry is too new for this instance.
	if cerr := s.checkRegistryCompatible(info); cerr != nil {
		out.Incompatible = cerr.Error()
	}
	return out, nil
}

// tokenRefusedMessage is the one message for a 401 or 403 from a registry.
const tokenRefusedMessage = "the Bait Box refused the token: it may be missing, revoked or " +
	"expired, not allowed from this IP, or not scoped to this namespace"

// connErrReason maps a probe error to a short reason. A transport error is
// unwrapped first so the request url never reaches the user. An error the
// registry client wrote itself is kept, any other text is replaced by a generic
// reason.
func connErrReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, registry.ErrUnauthorized) {
		return tokenRefusedMessage
	}
	msg := err.Error()
	var ue *neturl.Error
	if errors.As(err, &ue) && ue.Err != nil {
		msg = ue.Err.Error()
	}
	switch {
	case strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "Timeout"),
		strings.Contains(msg, "timeout"):
		return "timed out"
	case strings.Contains(msg, "connection refused"):
		return "connection refused"
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "server misbehaving"):
		return "host not found"
	case strings.Contains(msg, "certificate"), strings.Contains(msg, "x509"):
		return "TLS certificate not trusted (turn on Allow self signed TLS for a self signed Bait Box)"
	case strings.Contains(msg, "tls:"):
		return "TLS handshake failed"
	}
	var se *registry.StatusError
	if errors.As(err, &se) || errors.Is(err, registry.ErrUnexpectedAnswer) {
		return err.Error()
	}
	return "the connection failed"
}

// connectError is the error for a failed connection test on add or edit.
func connectError(url string, err error) error {
	if errors.Is(err, registry.ErrUnauthorized) {
		return errs.NewValidationError(errors.New(tokenRefusedMessage))
	}
	return errs.NewValidationError(fmt.Errorf(
		"could not connect to the Bait Box at %s: %s", url, connErrReason(err),
	))
}

// registryError wraps a failed Bait Box call for the user. A refused token reads
// the same everywhere, and a transport error, including a body read that timed
// out or ended early, is reduced to its short reason.
func registryError(what string, err error) error {
	if errors.Is(err, registry.ErrUnauthorized) {
		return errs.NewValidationError(errors.New(tokenRefusedMessage))
	}
	if isTransportError(err) {
		return errs.NewValidationError(fmt.Errorf("%s: %s", what, connErrReason(err)))
	}
	return errs.NewValidationError(fmt.Errorf("%s: %w", what, err))
}

// DeleteRegistry removes a registry connection. Installed content is left intact.
func (s *ContentRegistry) DeleteRegistry(ctx context.Context, session *model.Session, id *uuid.UUID) (err error) {
	ae, err := s.authorize(session, "Registry.Delete")
	if err != nil {
		return err
	}
	defer s.auditFailure(ae, &err)
	ae.Details["registryId"] = id.String()
	if err := s.RegistryRepository.DeleteRegistry(ctx, id); err != nil {
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// fetchPackage fetches and verifies one package at the Bait Box current version.
func (s *ContentRegistry) fetchPackage(
	ctx context.Context, client *registry.Client, id string,
) (*registry.VerifiedPackage, error) {
	ns, name := splitPackageID(id)
	if ns == "" || name == "" {
		return nil, fmt.Errorf("invalid package id %q", id)
	}
	pkg, _, err := client.FetchVerified(ctx, ns, name)
	if err != nil {
		return nil, err
	}
	return pkg, nil
}

// registryLookupConcurrency is how many version lookups run at once against one
// registry when listing installed packages or checking bundle references.
const registryLookupConcurrency = 4

// versionLookup is the result of one version lookup: the version, or the error
// that lookup returned. Both are nil for a lookup that was skipped.
type versionLookup struct {
	vr  *registry.VersionResponse
	err error
}

// resolveVersions looks up the current version of each package id on one
// registry, a few at a time, each under registryProbeTimeout. Every id gets its
// own result, so one refused or missing package never decides the status of
// another. After a transport error (the registry is unreachable or timed out)
// lookups not yet started are skipped, since they would only fail the same way.
func (s *ContentRegistry) resolveVersions(
	ctx context.Context, client *registry.Client, ids []string,
) map[string]versionLookup {
	out := make(map[string]versionLookup, len(ids))
	var mu sync.Mutex
	var unreachable atomic.Bool
	sem := make(chan struct{}, registryLookupConcurrency)
	var wg sync.WaitGroup
	for _, id := range ids {
		ns, nm := splitPackageID(id)
		if ns == "" || nm == "" {
			mu.Lock()
			out[id] = versionLookup{err: fmt.Errorf("invalid package id %q", id)}
			mu.Unlock()
			continue
		}
		mu.Lock()
		out[id] = versionLookup{}
		mu.Unlock()
		wg.Add(1)
		go func(id, ns, nm string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if unreachable.Load() {
				return
			}
			cctx, cancel := context.WithTimeout(ctx, registryProbeTimeout)
			defer cancel()
			vr, err := client.GetVersion(cctx, ns, nm)
			if isTransportError(err) {
				unreachable.Store(true)
			}
			mu.Lock()
			out[id] = versionLookup{vr: vr, err: err}
			mu.Unlock()
		}(id, ns, nm)
	}
	wg.Wait()
	return out
}

// isTransportError reports whether err means the request never got an answer
// from the registry, as opposed to an answer that refused or missed the package.
func isTransportError(err error) bool {
	if err == nil {
		return false
	}
	var ue *neturl.Error
	var ne net.Error
	// a body read that times out or ends early is a net.Error or an unexpected
	// EOF rather than a url error.
	return errors.As(err, &ue) ||
		errors.As(err, &ne) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, io.ErrUnexpectedEOF)
}

// isDowngrade reports whether latest is a lower semver than installed. A version
// that does not parse is not treated as a downgrade.
func isDowngrade(installed, latest string) bool {
	iv, lv := ensureVPrefix(installed), ensureVPrefix(latest)
	if !semver.IsValid(iv) || !semver.IsValid(lv) {
		return false
	}
	return semver.Compare(lv, iv) < 0
}

// ListInstalled returns installed packages with whether a newer version exists on
// their registry (the per item update signal). Each registry is queried in
// parallel under a short timeout per lookup. Each package status comes from its
// own lookup: a refused token marks that package token refused, any other failure
// leaves it unknown. Once a registry cannot be reached, its remaining packages
// are not looked up and stay unknown.
func (s *ContentRegistry) ListInstalled(ctx context.Context, session *model.Session) ([]InstalledView, error) {
	if _, err := s.authorize(session, "Registry.ListInstalled"); err != nil {
		return nil, err
	}
	rows, err := s.RegistryRepository.GetAllInstalledPackages(ctx)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// group the package ids by registry so each registry is resolved once.
	byRegistry := map[uuid.UUID][]string{}
	for _, ip := range rows {
		byRegistry[*ip.RegistryID] = append(byRegistry[*ip.RegistryID], ip.PackageID)
	}
	versions := map[uuid.UUID]map[string]versionLookup{}
	regNames := map[uuid.UUID]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for rid, ids := range byRegistry {
		reg, rerr := s.RegistryRepository.GetRegistryByID(ctx, &rid)
		if errors.Is(rerr, gorm.ErrRecordNotFound) {
			continue
		}
		if rerr != nil {
			return nil, errs.Wrap(rerr)
		}
		regNames[rid] = reg.Name
		wg.Add(1)
		go func(rid uuid.UUID, client *registry.Client, ids []string) {
			defer wg.Done()
			defer client.CloseIdle()
			res := s.resolveVersions(ctx, client, ids)
			mu.Lock()
			versions[rid] = res
			mu.Unlock()
		}(rid, s.clientFor(reg), ids)
	}
	wg.Wait()

	out := make([]InstalledView, 0, len(rows))
	for _, ip := range rows {
		view := InstalledView{
			ID:          ip.ID.String(),
			RegistryID:  ip.RegistryID.String(),
			PackageID:   ip.PackageID,
			PackageType: ip.PackageType,
			Version:     ip.Version,

			InstalledAsDependency: ip.InstalledAsDependency,
			Status:                installedStatusUnknown,
		}
		name, known := regNames[*ip.RegistryID]
		if !known {
			view.RegistryRemoved = true
			view.Status = installedStatusNoSource
		} else {
			view.RegistryName = name
			lk := versions[*ip.RegistryID][ip.PackageID]
			if vr := lk.vr; vr != nil {
				view.CurrentVersion = vr.Version
				// a registry whose latest is lower than what is installed (a yanked or
				// rolled back release) is not offered as an update.
				view.UpdateAvailable = !strings.EqualFold(vr.ContentHash, ip.ContentHash) &&
					!isDowngrade(ip.Version, vr.Version)
				view.Status = installedStatusCurrent
				if view.UpdateAvailable {
					view.Status = installedStatusUpdate
				}
			} else if errors.Is(lk.err, registry.ErrUnauthorized) {
				view.Status = installedStatusTokenRefused
				view.StatusMessage = tokenRefusedMessage
			}
		}
		out = append(out, view)
	}
	return out, nil
}

// InstalledResourceView tells the UI that one entity came from a package, so an
// editor can warn that local edits may conflict on the next update.
type InstalledResourceView struct {
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	PackageID    string `json:"packageId"`
	RegistryID   string `json:"registryId"`
	SourceName   string `json:"sourceName"`
}

// ListInstalledResources lets the UI mark a resource as package managed wherever
// it is edited or listed.
func (s *ContentRegistry) ListInstalledResources(
	ctx context.Context, session *model.Session,
) ([]InstalledResourceView, error) {
	if _, err := s.authorize(session, "Registry.ListInstalled"); err != nil {
		return nil, err
	}
	pkgs, err := s.RegistryRepository.GetAllInstalledPackages(ctx)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	type pkgMeta struct{ packageID, registryID string }
	byID := map[string]pkgMeta{}
	for _, ip := range pkgs {
		byID[ip.ID.String()] = pkgMeta{packageID: ip.PackageID, registryID: ip.RegistryID.String()}
	}
	resources, err := s.RegistryRepository.GetAllInstalledResources(ctx)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	out := make([]InstalledResourceView, 0, len(resources))
	for _, r := range resources {
		if r.ResourceID == nil || r.InstalledPackageID == nil {
			continue
		}
		meta := byID[r.InstalledPackageID.String()]
		out = append(out, InstalledResourceView{
			ResourceType: r.ResourceType,
			ResourceID:   r.ResourceID.String(),
			PackageID:    meta.packageID,
			RegistryID:   meta.registryID,
			SourceName:   r.SourceName,
		})
	}
	return out, nil
}
