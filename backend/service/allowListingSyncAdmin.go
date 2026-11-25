package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// allowListingScope is the scope label for audit logging
func allowListingScope(companyID *uuid.UUID) string {
	if companyID == nil {
		return "global"
	}
	return companyID.String()
}

func allowListingAudit(name string, session *model.Session, companyID *uuid.UUID) *AuditEvent {
	ae := NewAuditEvent(name, session)
	ae.Details["scope"] = allowListingScope(companyID)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	return ae
}

func (s *AllowListingSync) authorize(session *model.Session, ae *AuditEvent) error {
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

// lockScope serializes the saves of the target of one scope
func (s *AllowListingSync) lockScope(companyID *uuid.UUID) func() {
	key := "global"
	if companyID != nil {
		key = companyID.String()
	}
	v, _ := s.targetLocks.LoadOrStore("scope:"+key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// target reads the target of a scope
func (s *AllowListingSync) target(ctx context.Context, companyID *uuid.UUID) (*model.AllowListingTarget, error) {
	target, _, err := s.Repository.GetTargetByCompanyID(ctx, companyID)
	if err != nil {
		return nil, err
	}
	return target, nil
}

// targetView reads the target of a scope for an admin, with the issue of
// its runner domain
func (s *AllowListingSync) targetView(ctx context.Context, companyID *uuid.UUID) (*model.AllowListingTarget, error) {
	target, err := s.target(ctx, companyID)
	if err != nil {
		return nil, err
	}
	return s.withRunnerDomainIssue(ctx, target)
}

// GetByCompanyID returns the target of a scope, a nil company is the global
// target
func (s *AllowListingSync) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*model.AllowListingTarget, error) {
	ae := allowListingAudit("AllowListingSync.GetByCompanyID", session, companyID)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	return s.targetView(ctx, companyID)
}

// Upsert creates the target of a scope with a new token, or saves the
// settings of the existing one. The token is returned once on create.
func (s *AllowListingSync) Upsert(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	settings *model.AllowListingTargetSettings,
) (_ *model.AllowListingTarget, err error) {
	ae := allowListingAudit("AllowListingSync.Upsert", session, companyID)
	defer s.auditDone(ae, &err)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	// one save per scope at a time, so two creates do not both insert
	defer s.lockScope(companyID)()
	release, err := refuseDeletingCompany(ctx, s.CompanyRepository, companyID)
	if err != nil {
		return nil, err
	}
	defer release()
	if companyID != nil {
		if _, err := s.CompanyRepository.GetByID(ctx, companyID); err != nil {
			return nil, errs.Wrap(err)
		}
	}
	ae.Details["enabled"] = settings.Enabled
	ae.Details["leadMinutes"] = settings.LeadMinutes
	ae.Details["graceMinutes"] = settings.GraceMinutes
	ae.Details["intervalMinutes"] = settings.IntervalMinutes
	existing, err := s.target(ctx, companyID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(err)
	}
	if existing != nil {
		if err := s.Repository.UpdateTargetSettings(ctx, &existing.ID, settings); err != nil {
			s.Logger.Errorw("failed to update allow listing target", "error", err)
			return nil, errs.Wrap(err)
		}
		ae.Details["targetId"] = existing.ID.String()
		ae.Details["action"] = "update"
		s.AuditLogAuthorized(ae)
		return s.targetView(ctx, companyID)
	}
	plain, prefix, err := generateScimToken()
	if err != nil {
		return nil, errs.Wrap(err)
	}
	hash, err := allowListingTokenHash(plain)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	id, err := s.Repository.InsertTarget(ctx, companyID, settings, hash, prefix)
	if err != nil {
		s.Logger.Errorw("failed to insert allow listing target", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["targetId"] = id.String()
	ae.Details["action"] = "insert"
	s.AuditLogAuthorized(ae)
	created, _, err := s.Repository.GetTargetByID(ctx, id)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	created.Token = plain
	return created, nil
}

// RotateToken gives the target of a scope a new token, returned once. The
// old token stops working at once.
func (s *AllowListingSync) RotateToken(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (_ *model.AllowListingTarget, err error) {
	ae := allowListingAudit("AllowListingSync.RotateToken", session, companyID)
	defer s.auditDone(ae, &err)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	target, err := s.target(ctx, companyID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	plain, prefix, err := generateScimToken()
	if err != nil {
		return nil, errs.Wrap(err)
	}
	hash, err := allowListingTokenHash(plain)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if err := s.Repository.UpdateTargetToken(ctx, &target.ID, hash, prefix); err != nil {
		return nil, errs.Wrap(err)
	}
	ae.Details["targetId"] = target.ID.String()
	s.AuditLogAuthorized(ae)
	updated, err := s.targetView(ctx, companyID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	updated.Token = plain
	return updated, nil
}

// CancelDelete stops a delete that waits for the owned values to be
// removed. The next plans add the values that are needed again.
func (s *AllowListingSync) CancelDelete(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (_ *model.AllowListingTarget, err error) {
	ae := allowListingAudit("AllowListingSync.CancelDelete", session, companyID)
	defer s.auditDone(ae, &err)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	target, err := s.target(ctx, companyID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	defer s.lockTarget(target.ID)()
	if err := s.Repository.SetTargetDeleteRequested(ctx, &target.ID, nil); err != nil {
		return nil, errs.Wrap(err)
	}
	ae.Details["targetId"] = target.ID.String()
	s.AuditLogAuthorized(ae)
	return s.targetView(ctx, companyID)
}

// DeleteByCompanyID removes the target of a scope with its ledger and pins.
// With removeValues and owned values in the tenant, the target is kept until
// the runner has removed them and is then deleted by the report that shows
// it. The returned target is nil when it was deleted now.
func (s *AllowListingSync) DeleteByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	removeValues bool,
) (_ *model.AllowListingTarget, err error) {
	ae := allowListingAudit("AllowListingSync.DeleteByCompanyID", session, companyID)
	defer s.auditDone(ae, &err)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	target, err := s.target(ctx, companyID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	defer s.lockTarget(target.ID)()
	ae.Details["targetId"] = target.ID.String()
	ae.Details["removeValues"] = removeValues
	if removeValues {
		owned, err := s.ownedCount(ctx, &target.ID)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		if owned > 0 {
			if !target.IsDeleting() {
				if err := s.Repository.SetTargetDeleteRequested(ctx, &target.ID, ptrTime(s.now())); err != nil {
					return nil, errs.Wrap(err)
				}
			}
			ae.Details["waitsForRemoval"] = owned
			s.AuditLogAuthorized(ae)
			return s.targetView(ctx, companyID)
		}
	}
	if err := s.Repository.DeleteTarget(ctx, &target.ID); err != nil {
		return nil, errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil, nil
}

// ownedCount is the number of values Phishing Club added that are in the
// tenant
func (s *AllowListingSync) ownedCount(ctx context.Context, id *uuid.UUID) (int, error) {
	rows, err := s.Repository.ListEntries(ctx, id)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	n := 0
	for _, r := range rows {
		if r.State == model.AllowListingStateOwned {
			n++
		}
	}
	return n, nil
}

// DeleteForCompany removes the target of a company that is being deleted.
// It is called by the company delete, which checks the session itself.
func (s *AllowListingSync) DeleteForCompany(ctx context.Context, companyID *uuid.UUID) error {
	if companyID == nil {
		return nil
	}
	target, err := s.target(ctx, companyID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return errs.Wrap(err)
	}
	if err := s.Repository.DeleteTarget(ctx, &target.ID); err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// Entries returns the ledger of the target of a scope, with which values are
// needed and when an owned value nothing needs is removed
func (s *AllowListingSync) Entries(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) ([]*model.AllowListingEntry, error) {
	ae := allowListingAudit("AllowListingSync.Entries", session, companyID)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	target, err := s.target(ctx, companyID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	now := s.now()
	st, err := s.state(ctx, target, now)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// owned values that are due but stay, as the rule can not be left
	// without a domain or a sending IP
	kept := map[string]bool{}
	_, removing := st.diff(now)
	removingSet := map[string]bool{}
	for _, v := range removing {
		removingSet[v.Key()] = true
	}
	for key, row := range st.ledger {
		if _, needed := st.desired[key]; row.State == model.AllowListingStateOwned && !needed &&
			st.removalDue(row, now) && !removingSet[key] {
			kept[key] = true
		}
	}
	out := []*model.AllowListingEntry{}
	for key, row := range st.ledger {
		entry := &model.AllowListingEntry{
			Kind: row.Kind, Value: row.Value, State: row.State,
			AddedAt: row.AddedAt, LastSeenAt: row.LastSeenAt, UndesiredSince: row.UndesiredSince,
		}
		if row.ID != nil {
			entry.ID = *row.ID
		}
		_, entry.Needed = st.desired[key]
		if row.State == model.AllowListingStateOwned && !entry.Needed && kept[key] {
			// the rule needs one domain and one sending IP, this one stays
			entry.Kept = true
		} else if row.State == model.AllowListingStateOwned && !entry.Needed {
			at := now
			if row.UndesiredSince != nil {
				at = *row.UndesiredSince
			}
			if !target.IsDeleting() {
				at = at.Add(target.Grace())
			}
			entry.RemoveAfter = &at
		}
		out = append(out, entry)
	}
	sortEntries(out)
	return out, nil
}

func sortEntries(entries []*model.AllowListingEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].Value < entries[j].Value
	})
}

// allowListingPreviewMaxSources is the most sources a preview lists
const allowListingPreviewMaxSources = 200

// Preview returns what the next plan of the target of a scope would add and
// remove, and the state of every campaign, program and pin that counts. It
// writes nothing.
func (s *AllowListingSync) Preview(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*model.AllowListingSyncPreview, error) {
	ae := allowListingAudit("AllowListingSync.Preview", session, companyID)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	target, err := s.target(ctx, companyID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	now := s.now()
	st, err := s.state(ctx, target, now)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	add, remove := st.diff(now)
	// the same rules as the plan: the first run only reads, and a target
	// that is turned off changes nothing unless it is being taken down
	if target.LastReportAt == nil || (!target.Enabled && !target.IsDeleting()) {
		add, remove = []model.AllowListingOp{}, []model.AllowListingOp{}
	}
	out := &model.AllowListingSyncPreview{
		Add: add, Remove: remove, Sources: []model.AllowListingSyncSource{}, SourcesTotal: len(st.sources),
	}
	for i, src := range st.sources {
		// the list grows with the campaigns, only the first are shown
		if i == allowListingPreviewMaxSources {
			break
		}
		values := append([]model.AllowListingOp{}, src.values...)
		sortOps(values)
		out.Sources = append(out.Sources, model.AllowListingSyncSource{
			Type: src.typ, ID: src.id, Name: src.name,
			Status: st.sourceStatus(src, now), Values: values, Incomplete: src.incomplete,
		})
	}
	return out, nil
}

// ListPins returns the pins of the target of a scope
func (s *AllowListingSync) ListPins(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) ([]*model.AllowListingPin, error) {
	ae := allowListingAudit("AllowListingSync.ListPins", session, companyID)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	target, err := s.target(ctx, companyID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	rows, err := s.Repository.ListPins(ctx, &target.ID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	out := make([]*model.AllowListingPin, 0, len(rows))
	for _, row := range rows {
		pin := &model.AllowListingPin{
			CreatedAt: row.CreatedAt, CampaignTemplateID: row.CampaignTemplateID,
			CampaignTemplateName: row.CampaignTemplateName, Kind: row.Kind, Value: row.Value,
			Note: row.Note, ExpiresAt: row.ExpiresAt,
		}
		if row.ID != nil {
			pin.ID = *row.ID
		}
		out = append(out, pin)
	}
	return out, nil
}

var errAllowListingPinLimit = errors.New("a target holds at most 50 pins")

// CreatePin pins a template or a value on the target of a scope. A template
// must be global or belong to the company of the target, the global target
// pins global templates only.
func (s *AllowListingSync) CreatePin(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	input *model.AllowListingPinInput,
) (err error) {
	ae := allowListingAudit("AllowListingSync.CreatePin", session, companyID)
	defer s.auditDone(ae, &err)
	if err := s.authorize(session, ae); err != nil {
		return err
	}
	now := s.now()
	if err := input.Validate(now); err != nil {
		return err
	}
	release, err := refuseDeletingCompany(ctx, s.CompanyRepository, companyID)
	if err != nil {
		return err
	}
	defer release()
	target, err := s.target(ctx, companyID)
	if err != nil {
		return errs.Wrap(err)
	}
	// the count and the insert run under the target lock, so the limit holds
	defer s.lockTarget(target.ID)()
	// expired pins do no more, they are removed so they do not hold room
	if err := s.Repository.DeleteExpiredPins(ctx, &target.ID, now); err != nil {
		return errs.Wrap(err)
	}
	count, err := s.Repository.CountPins(ctx, &target.ID)
	if err != nil {
		return errs.Wrap(err)
	}
	if count >= model.AllowListingSyncMaxPins {
		return validate.WrapErrorWithField(errAllowListingPinLimit, "pin")
	}
	row := &database.AllowListingPin{
		TargetID:  &target.ID,
		Note:      input.Note,
		ExpiresAt: input.ExpiresAt,
	}
	if input.CampaignTemplateID != nil {
		tmpl, err := s.CampaignTemplateRepository.GetByID(ctx, input.CampaignTemplateID, &repository.CampaignTemplateOption{})
		if err != nil {
			return errs.Wrap(err)
		}
		if owner, err := tmpl.CompanyID.Get(); err == nil && (companyID == nil || owner != *companyID) {
			return errs.Wrap(gorm.ErrRecordNotFound)
		}
		row.CampaignTemplateID = input.CampaignTemplateID
		if name, err := tmpl.Name.Get(); err == nil {
			row.CampaignTemplateName = name.String()
		}
		ae.Details["campaignTemplateId"] = input.CampaignTemplateID.String()
	} else {
		row.Kind = input.Kind
		row.Value = input.Value
		ae.Details["value"] = input.Kind + ":" + input.Value
	}
	id, err := s.Repository.InsertPin(ctx, row)
	if err != nil {
		return errs.Wrap(err)
	}
	ae.Details["targetId"] = target.ID.String()
	ae.Details["pinId"] = id.String()
	s.AuditLogAuthorized(ae)
	return nil
}

// DeletePin removes a pin of the target of a scope
func (s *AllowListingSync) DeletePin(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	pinID *uuid.UUID,
) (err error) {
	ae := allowListingAudit("AllowListingSync.DeletePin", session, companyID)
	defer s.auditDone(ae, &err)
	ae.Details["pinId"] = pinID.String()
	if err := s.authorize(session, ae); err != nil {
		return err
	}
	target, err := s.target(ctx, companyID)
	if err != nil {
		return errs.Wrap(err)
	}
	if err := s.Repository.DeletePin(ctx, &target.ID, pinID); err != nil {
		return errs.Wrap(err)
	}
	ae.Details["targetId"] = target.ID.String()
	s.AuditLogAuthorized(ae)
	return nil
}

// GetCampaignStatus returns the sync status of a campaign. The campaign must be
// shown in the company in view. A campaign of a program is read through its
// program.
func (s *AllowListingSync) GetCampaignStatus(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	viewCompanyID *uuid.UUID,
) (*model.AllowListingSyncStatus, error) {
	ae := NewAuditEvent("AllowListingSync.CampaignStatus", session)
	ae.Details["campaignId"] = id.String()
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	campaign, err := s.CampaignRepository.GetByID(ctx, id, &repository.CampaignOption{})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if campaign.IsProgramManaged() || !inCompany(viewCompanyID, campaign.CompanyID) {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}
	var companyID *uuid.UUID
	if owner, err := campaign.CompanyID.Get(); err == nil {
		companyID = &owner
	}
	return s.statusOf(ctx, companyID, allowListingSourceCampaign, *id)
}

// GetProgramStatus returns the sync status of a program shown in the company in
// view
func (s *AllowListingSync) GetProgramStatus(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	viewCompanyID *uuid.UUID,
) (*model.AllowListingSyncStatus, error) {
	ae := NewAuditEvent("AllowListingSync.ProgramStatus", session)
	ae.Details["programId"] = id.String()
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if !inCompany(viewCompanyID, program.CompanyID) {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}
	var companyID *uuid.UUID
	if owner, err := program.CompanyID.Get(); err == nil {
		companyID = &owner
	}
	return s.statusOf(ctx, companyID, allowListingSourceProgram, *id)
}

// allowListingTokenHashPrefix marks a SHA-256 runner token hash
const allowListingTokenHashPrefix = "sha256:"

// allowListingTokenHash hashes a runner token. The token is 32 random
// bytes, so a fast hash is enough: there is nothing to guess, and a slow hash
// would only let the time of a request tell that the runner path exists.
func allowListingTokenHash(plain string) (string, error) {
	sum := sha256.Sum256([]byte(plain))
	return allowListingTokenHashPrefix + hex.EncodeToString(sum[:]), nil
}

// allowListingTokenMatches compares a token with a stored hash in constant
// time
func allowListingTokenMatches(stored string, token string) bool {
	want, _ := allowListingTokenHash(token)
	return subtle.ConstantTimeCompare([]byte(stored), []byte(want)) == 1
}

// allowListingAuthFailedThrottle limits the audit events of wrong runner
// tokens, keyed by target id, which is a bounded set as only existing targets
// are logged
var allowListingAuthFailedThrottle = newAuditThrottle(time.Minute)

func (s *AllowListingSync) auditAuthFailed(target *model.AllowListingTarget, ip string, reason string) {
	key := target.ID.String()
	write, suppressed, lastIP := allowListingAuthFailedThrottle.allow(key, ip)
	if !write {
		return
	}
	ae := NewAuditEvent("AllowListingSync.AuthFailed", nil)
	ae.IP = ip
	ae.Details["actor"] = "api"
	ae.Details["targetId"] = key
	ae.Details["reason"] = reason
	addThrottleDetails(ae, suppressed, lastIP)
	s.AuditLogNotAuthorized(ae)
}

// Authenticate checks a runner request. It returns false for an unknown
// target, a company being deleted or a wrong token, all alike, so the caller can answer them the same. Only failures against an
// existing target are logged, so the log can not be filled with made up ids.
func (s *AllowListingSync) Authenticate(
	ctx context.Context,
	targetID string,
	token string,
	ip string,
) (*model.AllowListingTarget, bool) {
	if token == "" || len(token) > 256 {
		return nil, false
	}
	id, err := uuid.Parse(targetID)
	if err != nil {
		return nil, false
	}
	target, hash, err := s.Repository.GetTargetByID(ctx, &id)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			s.Logger.Errorw("failed to read allow listing target", "error", err)
		}
		return nil, false
	}
	if !allowListingTokenMatches(hash, token) {
		s.auditAuthFailed(target, ip, "wrongToken")
		return nil, false
	}
	if target.CompanyID != nil && s.CompanyRepository != nil {
		deleting, err := s.CompanyRepository.IsDeleting(ctx, target.CompanyID)
		if err != nil || deleting {
			s.auditAuthFailed(target, ip, "companyDeleting")
			return nil, false
		}
	}
	return target, true
}
