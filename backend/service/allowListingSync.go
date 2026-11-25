package service

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// AllowListingSync keeps the Microsoft 365 Advanced Delivery lists of a
// tenant in step with the campaigns and programs that need them. A runner in
// the tenant pulls a plan, applies it and reports the lists it reads back.
// The ledger only changes from what the runner reads back.
//
// Every target stands alone. The global target counts global campaigns and
// programs only, a company target only those of its company.
type AllowListingSync struct {
	Common
	Repository                 *repository.AllowListingSync
	AllowListing               *AllowListing
	CampaignTemplateRepository *repository.CampaignTemplate
	CampaignRepository         *repository.Campaign
	ProgramRepository          *repository.Program
	CompanyRepository          *repository.Company
	DomainRepository           *repository.Domain
	OptionRepository           *repository.Option
	// Now is the clock, nil is time.Now
	Now func() time.Time

	// targetLocks serializes plans and reports of one target
	targetLocks sync.Map
}

// allowListingPlanMaxAge is how long a plan may be applied. A report for an
// older plan is used for what it read back only.
const allowListingPlanMaxAge = 2 * time.Hour

func (s *AllowListingSync) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *AllowListingSync) lockTarget(id uuid.UUID) func() {
	v, _ := s.targetLocks.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// Microsoft limits per kind
func allowListingCap(kind string) int {
	switch kind {
	case model.AllowListingKindDomain:
		return model.AllowListingMaxDomains
	case model.AllowListingKindIP:
		return model.AllowListingMaxSendingIPs
	}
	return model.AllowListingMaxSimulationURLs
}

// source types
const (
	allowListingSourceCampaign = "campaign"
	allowListingSourceProgram  = "program"
	allowListingSourcePin      = "pin"
)

// allowListingSource is one campaign, program or pin that needs values
type allowListingSource struct {
	typ        string
	id         uuid.UUID
	name       string
	start      time.Time
	values     []model.AllowListingOp
	incomplete []string
	overLimit  bool
}

// allowListingState is the desired set of a target next to its ledger
type allowListingState struct {
	target  *model.AllowListingTarget
	sources []*allowListingSource
	// desired are the values the sources that fit need
	desired map[string]model.AllowListingOp
	ledger  map[string]*database.AllowListingEntry
	// pendingRemove are values a plan handed out after the last report
	// removes. They may be gone already, so they count as missing until a
	// report shows them.
	pendingRemove map[string]bool
}

// present reports if a value is known to be in the tenant
func (st *allowListingState) present(key string) bool {
	_, ok := st.ledger[key]
	return ok && !st.pendingRemove[key]
}

func ledgerKey(row *database.AllowListingEntry) string {
	return model.AllowListingOp{Kind: row.Kind, Value: row.Value}.Key()
}

// campaignCounts reports if a campaign needs allow listing at now. A closed
// campaign no longer counts, its values are kept for the grace time by the
// removal of values nothing needs. An open one counts from the lead time
// before its start. A self managed campaign has no start
// and counts from when it was made.
func campaignCounts(c *database.Campaign, now time.Time, lead time.Duration) (bool, time.Time) {
	start := time.Time{}
	switch {
	case c.SendStartAt != nil:
		start = *c.SendStartAt
	case c.ScheduleAt != nil:
		start = *c.ScheduleAt
	case c.CreatedAt != nil:
		start = *c.CreatedAt
	}
	if c.ClosedAt != nil && !c.ClosedAt.After(now) {
		return false, start
	}
	return !start.Add(-lead).After(now), start
}

// programCounts reports if a program needs allow listing at now. A draft has
// no start and never counts, a closed program counts no longer.
func programCounts(p *database.Program, now time.Time, lead time.Duration) (bool, time.Time) {
	if p.StartAt == nil {
		return false, time.Time{}
	}
	if p.ClosedAt != nil && !p.ClosedAt.After(now) {
		return false, *p.StartAt
	}
	return !p.StartAt.Add(-lead).After(now), *p.StartAt
}

// templateValues builds the values of templates for a target. Only templates
// the scope of the target may use are read, a global template or one of the
// company of the target. Any other is left out as if it was deleted.
func (s *AllowListingSync) templateValues(
	ctx context.Context,
	companyID *uuid.UUID,
	ids []uuid.UUID,
) ([]model.AllowListingOp, []string, error) {
	unique := make([]uuid.UUID, 0, len(ids))
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return []model.AllowListingOp{}, []string{}, nil
	}
	templates, err := s.AllowListing.readTemplates(ctx, unique)
	if err != nil {
		return nil, nil, err
	}
	allowed := make([]*model.CampaignTemplate, 0, len(templates))
	for _, t := range templates {
		owner, err := t.CompanyID.Get()
		if err != nil {
			allowed = append(allowed, t)
			continue
		}
		if companyID != nil && owner == *companyID {
			allowed = append(allowed, t)
		}
	}
	listing, err := s.AllowListing.combine(ctx, unique, allowed)
	if err != nil {
		return nil, nil, err
	}
	incomplete := []string{}
	// the envelope sender domain of a template is listed only when it sends
	// from a known IP. Mail through an API sender leaves from the provider,
	// which has no IP to list, so only its simulation URLs are listed. Its
	// domain alone would let mail from any other listed IP skip filtering,
	// as Microsoft does not tie a domain to an IP.
	mailFrom := []string{}
	seenMailFrom := map[string]bool{}
	for _, t := range listing.Templates {
		if len(t.Missing) > 0 {
			incomplete = append(incomplete, t.Name)
		}
		if t.MailFromDomain != "" && len(t.SendingIPs) > 0 && !seenMailFrom[t.MailFromDomain] {
			seenMailFrom[t.MailFromDomain] = true
			mailFrom = append(mailFrom, t.MailFromDomain)
		}
	}
	if len(listing.TooLongURLs) > 0 {
		incomplete = append(incomplete, "URL longer than Microsoft allows")
	}
	// a template that is deleted or outside the scope adds nothing, so the
	// source can not be confirmed
	if listing.DeletedTemplates > 0 {
		incomplete = append(incomplete, "template not available")
	}
	values := []model.AllowListingOp{}
	add := func(kind string, list []string) {
		for _, v := range list {
			value, err := model.NormalizeAllowListingValue(kind, v, true)
			if err != nil {
				// a value that can not be listed leaves the source incomplete,
				// so the status never reads it as confirmed
				incomplete = append(incomplete, "value can not be listed: "+v)
				continue
			}
			values = append(values, model.AllowListingOp{Kind: kind, Value: value})
		}
	}
	add(model.AllowListingKindDomain, mailFrom)
	add(model.AllowListingKindIP, listing.SendingIPs)
	add(model.AllowListingKindURL, listing.SimulationURLs)
	return values, incomplete, nil
}

// state builds the desired set of a target and reads its ledger
func (s *AllowListingSync) state(ctx context.Context, target *model.AllowListingTarget, now time.Time) (*allowListingState, error) {
	st := &allowListingState{
		target:        target,
		sources:       []*allowListingSource{},
		desired:       map[string]model.AllowListingOp{},
		ledger:        map[string]*database.AllowListingEntry{},
		pendingRemove: map[string]bool{},
	}
	rows, err := s.Repository.ListEntries(ctx, &target.ID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		st.ledger[ledgerKey(row)] = row
	}
	st.pendingRemove = map[string]bool{}
	for _, p := range recentPlans(target.RecentPlansJSON, now, recentPlanKeep(target)) {
		if target.LastReportAt != nil && !p.IssuedAt.After(*target.LastReportAt) {
			continue
		}
		for _, k := range p.Remove {
			st.pendingRemove[k] = true
		}
	}
	if target.IsDeleting() {
		return st, nil
	}
	lead := target.Lead()
	// only campaigns and programs still open count
	closedAfter := now

	campaigns, err := s.Repository.CountingCampaigns(ctx, target.CompanyID, closedAfter)
	if err != nil {
		return nil, err
	}
	counting := []*database.Campaign{}
	starts := map[uuid.UUID]time.Time{}
	ids := []uuid.UUID{}
	for _, c := range campaigns {
		if c.ID == nil {
			continue
		}
		ok, start := campaignCounts(c, now, lead)
		if !ok {
			continue
		}
		counting = append(counting, c)
		starts[*c.ID] = start
		ids = append(ids, *c.ID)
	}
	pools, err := s.Repository.TemplatePools(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, c := range counting {
		templateIDs := pools[*c.ID]
		// a campaign saved before template pools has only its template id
		if len(templateIDs) == 0 && c.CampaignTemplateID != nil {
			templateIDs = []uuid.UUID{*c.CampaignTemplateID}
		}
		values, incomplete, err := s.templateValues(ctx, target.CompanyID, templateIDs)
		if err != nil {
			return nil, err
		}
		if len(templateIDs) == 0 {
			incomplete = append(incomplete, "no template")
		}
		st.sources = append(st.sources, &allowListingSource{
			typ: allowListingSourceCampaign, id: *c.ID, name: c.Name,
			start: starts[*c.ID], values: values, incomplete: incomplete,
		})
	}

	programs, err := s.Repository.CountingPrograms(ctx, target.CompanyID, closedAfter)
	if err != nil {
		return nil, err
	}
	programIDs := []uuid.UUID{}
	programStarts := map[uuid.UUID]time.Time{}
	countingPrograms := []*database.Program{}
	for _, p := range programs {
		if p.ID == nil {
			continue
		}
		ok, start := programCounts(p, now, lead)
		if !ok {
			continue
		}
		countingPrograms = append(countingPrograms, p)
		programIDs = append(programIDs, *p.ID)
		programStarts[*p.ID] = start
	}
	steps, err := s.Repository.ProgramStepTemplates(ctx, programIDs)
	if err != nil {
		return nil, err
	}
	for _, p := range countingPrograms {
		values, incomplete, err := s.templateValues(ctx, target.CompanyID, steps[*p.ID])
		if err != nil {
			return nil, err
		}
		st.sources = append(st.sources, &allowListingSource{
			typ: allowListingSourceProgram, id: *p.ID, name: p.Name,
			start: programStarts[*p.ID], values: values, incomplete: incomplete,
		})
	}

	pins, err := s.Repository.ListPins(ctx, &target.ID)
	if err != nil {
		return nil, err
	}
	for _, pin := range pins {
		if pin.ID == nil || (pin.ExpiresAt != nil && !pin.ExpiresAt.After(now)) {
			continue
		}
		src := &allowListingSource{typ: allowListingSourcePin, id: *pin.ID, incomplete: []string{}}
		if pin.CreatedAt != nil {
			src.start = *pin.CreatedAt
		}
		if pin.CampaignTemplateID != nil {
			src.name = pin.CampaignTemplateName
			values, incomplete, err := s.templateValues(ctx, target.CompanyID, []uuid.UUID{*pin.CampaignTemplateID})
			if err != nil {
				return nil, err
			}
			src.values, src.incomplete = values, incomplete
		} else {
			src.name = pin.Value
			src.values = []model.AllowListingOp{{Kind: pin.Kind, Value: pin.Value}}
		}
		st.sources = append(st.sources, src)
	}

	st.fit()
	return st, nil
}

// fit gives the sources room under the Microsoft limits in order of their
// start. Every value already in the tenant uses room, and an owned value a
// source needs stays desired, so a value already listed is never taken away
// from a source that still counts. Only values not in the tenant yet compete
// for the free room. A source whose new values do not fit adds none of them,
// so it is never half listed.
func (st *allowListingState) fit() {
	sort.SliceStable(st.sources, func(i, j int) bool {
		return st.sources[i].start.Before(st.sources[j].start)
	})
	used := map[string]int{}
	for _, row := range st.ledger {
		used[row.Kind]++
	}
	for _, src := range st.sources {
		for _, v := range src.values {
			if _, ok := st.ledger[v.Key()]; ok {
				st.desired[v.Key()] = v
			}
		}
	}
	for _, src := range st.sources {
		extra := map[string]int{}
		fresh := []model.AllowListingOp{}
		seen := map[string]bool{}
		for _, v := range src.values {
			key := v.Key()
			if seen[key] {
				continue
			}
			seen[key] = true
			if _, ok := st.desired[key]; ok {
				continue
			}
			extra[v.Kind]++
			fresh = append(fresh, v)
		}
		fits := true
		for kind, n := range extra {
			if used[kind]+n > allowListingCap(kind) {
				fits = false
			}
		}
		if !fits {
			src.overLimit = true
			continue
		}
		for kind, n := range extra {
			used[kind] += n
		}
		for _, v := range fresh {
			st.desired[v.Key()] = v
		}
	}
}

// missing returns the values of a source that are not in the tenant
func (st *allowListingState) missing(src *allowListingSource) []model.AllowListingOp {
	out := []model.AllowListingOp{}
	for _, v := range src.values {
		if !st.present(v.Key()) {
			out = append(out, v)
		}
	}
	return out
}

// reportGap is the whole minutes between the last report and this one, 0
// for the first report
func reportGap(last *time.Time, now time.Time) int {
	if last == nil || !now.After(*last) {
		return 0
	}
	return int(now.Sub(*last) / time.Minute)
}

// reportFresh reports if the last report is recent enough to trust
func reportFresh(target *model.AllowListingTarget, now time.Time) bool {
	return target.LastReportAt != nil && now.Sub(*target.LastReportAt) <= target.MaxReportAge()
}

// sourceStatus is the sync status of one source
func (st *allowListingState) sourceStatus(src *allowListingSource, now time.Time) string {
	switch {
	case len(src.incomplete) > 0:
		return model.AllowListingStatusIncomplete
	case src.overLimit:
		return model.AllowListingStatusOverLimit
	case len(st.missing(src)) > 0:
		return model.AllowListingStatusWaiting
	case !reportFresh(st.target, now):
		return model.AllowListingStatusStale
	}
	return model.AllowListingStatusConfirmed
}

// removalDue reports if an owned value nothing needs may be removed at now
func (st *allowListingState) removalDue(row *database.AllowListingEntry, now time.Time) bool {
	if st.target.IsDeleting() {
		return true
	}
	if row.UndesiredSince == nil {
		return st.target.Grace() <= 0
	}
	return !row.UndesiredSince.Add(st.target.Grace()).After(now)
}

// diff returns the values to add and the owned values that are due to be
// removed. When every domain and sending IP of the rule goes, the whole rule
// goes. Otherwise the last domain and the last sending IP are never removed,
// the Microsoft rule needs one of each.
func (st *allowListingState) diff(now time.Time) ([]model.AllowListingOp, []model.AllowListingOp) {
	add := []model.AllowListingOp{}
	for key, v := range st.desired {
		if !st.present(key) {
			add = append(add, v)
		}
	}
	remove := []model.AllowListingOp{}
	for key, row := range st.ledger {
		if row.State != model.AllowListingStateOwned {
			continue
		}
		if _, ok := st.desired[key]; ok {
			continue
		}
		if st.removalDue(row, now) {
			remove = append(remove, model.AllowListingOp{Kind: row.Kind, Value: row.Value})
		}
	}
	sortOps(add)
	sortOps(remove)
	return add, st.limitRemoves(add, remove)
}

// limitRemoves keeps a domain and a sending IP in the rule unless the whole
// rule goes
func (st *allowListingState) limitRemoves(add, remove []model.AllowListingOp) []model.AllowListingOp {
	if st.removeRule(add, remove) {
		return remove
	}
	remove = st.keepLast(add, remove, model.AllowListingKindDomain)
	remove = st.keepLast(add, remove, model.AllowListingKindIP)
	return remove
}

// removeRule reports if the whole rule goes: every domain and sending IP in
// it is owned and due to be removed, and none is added. Nothing is then left
// that needs the rule, and a later add creates it again.
func (st *allowListingState) removeRule(add, remove []model.AllowListingOp) bool {
	for _, v := range add {
		if v.Kind == model.AllowListingKindDomain || v.Kind == model.AllowListingKindIP {
			return false
		}
	}
	removing := map[string]bool{}
	for _, v := range remove {
		removing[v.Key()] = true
	}
	any := false
	for key, row := range st.ledger {
		if row.Kind != model.AllowListingKindDomain && row.Kind != model.AllowListingKindIP {
			continue
		}
		if row.State != model.AllowListingStateOwned || !removing[key] {
			return false
		}
		any = true
	}
	return any
}

// keepLast drops the removal of one value of a kind when the removals would
// leave the rule without a value of that kind. A value a recent plan already
// removed may be gone, so it does not count as present and is not the one
// kept.
func (st *allowListingState) keepLast(add, remove []model.AllowListingOp, kind string) []model.AllowListingOp {
	present := 0
	for key, row := range st.ledger {
		if row.Kind == kind && st.present(key) {
			present++
		}
	}
	for _, v := range add {
		if v.Kind == kind {
			present++
		}
	}
	removing := 0
	for _, v := range remove {
		if v.Kind == kind && st.present(v.Key()) {
			removing++
		}
	}
	if removing == 0 || present-removing > 0 {
		return remove
	}
	out := make([]model.AllowListingOp, 0, len(remove))
	kept := false
	for _, v := range remove {
		if v.Kind == kind && !kept && st.present(v.Key()) {
			kept = true
			continue
		}
		out = append(out, v)
	}
	return out
}

func sortOps(ops []model.AllowListingOp) {
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Kind != ops[j].Kind {
			return ops[i].Kind < ops[j].Kind
		}
		return ops[i].Value < ops[j].Value
	})
}

// markUndesired keeps undesired_since of the owned values in step with the
// desired set
func (s *AllowListingSync) markUndesired(ctx context.Context, st *allowListingState, now time.Time) error {
	set, clearIDs := []uuid.UUID{}, []uuid.UUID{}
	for key, row := range st.ledger {
		if row.State != model.AllowListingStateOwned || row.ID == nil {
			continue
		}
		_, wanted := st.desired[key]
		switch {
		case !wanted && row.UndesiredSince == nil:
			set = append(set, *row.ID)
			at := now
			row.UndesiredSince = &at
		case wanted && row.UndesiredSince != nil:
			clearIDs = append(clearIDs, *row.ID)
			row.UndesiredSince = nil
		}
	}
	return s.Repository.MarkUndesired(ctx, &st.target.ID, set, clearIDs, now)
}

// allowListingMaxRecentPlans is the most plans kept for reports that come
// late or from runs that overlap
const allowListingMaxRecentPlans = 20

// allowListingRecentPlan is a plan handed out within allowListingPlanMaxAge
type allowListingRecentPlan struct {
	ID       uuid.UUID `json:"id"`
	IssuedAt time.Time `json:"issuedAt"`
	Add      []string  `json:"add"`
	Remove   []string  `json:"remove"`
}

// recentPlanKeep is how long a plan is kept with the recent plans. A
// report lost on a long interval is only noticed by the next run, so the
// removes of a plan count as pending as long as a report may be missed, and
// never less than the time a plan may be applied.
func recentPlanKeep(target *model.AllowListingTarget) time.Duration {
	if keep := target.MaxReportAge(); keep > allowListingPlanMaxAge {
		return keep
	}
	return allowListingPlanMaxAge
}

// recentPlans reads the stored recent plans and drops the ones older than
// keep
func recentPlans(raw string, now time.Time, keep time.Duration) []allowListingRecentPlan {
	plans := []allowListingRecentPlan{}
	if raw == "" {
		return plans
	}
	stored := []allowListingRecentPlan{}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return plans
	}
	for _, p := range stored {
		if now.Sub(p.IssuedAt) <= keep {
			plans = append(plans, p)
		}
	}
	return plans
}

func opKeySet(ops []model.AllowListingOp) []string {
	out := make([]string, 0, len(ops))
	for _, v := range ops {
		out = append(out, v.Key())
	}
	return out
}

// Plan builds the next plan of a target for its runner. The plan is kept
// with the recent plans, so a report can tell which values Phishing Club
// added. The first plan of a target only reads, so every value already in
// the tenant is recorded as the tenant's before anything is added. The
// target is authenticated by the caller.
func (s *AllowListingSync) Plan(ctx context.Context, target *model.AllowListingTarget) (*model.AllowListingPlan, error) {
	defer s.lockTarget(target.ID)()
	now := s.now()
	current, _, err := s.Repository.GetTargetByID(ctx, &target.ID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	st, err := s.state(ctx, current, now)
	if err != nil {
		s.Logger.Errorw("failed to build allow listing plan", "error", err)
		return nil, errs.Wrap(err)
	}
	if err := s.markUndesired(ctx, st, now); err != nil {
		s.Logger.Errorw("failed to mark undesired allow listing values", "error", err)
		return nil, errs.Wrap(err)
	}
	add, remove := st.diff(now)
	firstRun := current.LastReportAt == nil
	switch {
	case firstRun:
		add, remove = []model.AllowListingOp{}, []model.AllowListingOp{}
	case !current.Enabled && !current.IsDeleting():
		// a target that is turned off adds nothing, values nothing needs are
		// still removed after the grace time so none stays with no end
		add = []model.AllowListingOp{}
		remove = st.limitRemoves(add, remove)
	}
	plan := &model.AllowListingPlan{
		PlanID:     uuid.New(),
		IssuedAt:   now,
		Add:        add,
		Remove:     remove,
		Deleting:   current.IsDeleting(),
		RemoveRule: !firstRun && st.removeRule(add, remove),
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	recent := append(recentPlans(current.RecentPlansJSON, now, recentPlanKeep(current)), allowListingRecentPlan{
		ID: plan.PlanID, IssuedAt: now, Add: opKeySet(add), Remove: opKeySet(remove),
	})
	if len(recent) > allowListingMaxRecentPlans {
		recent = recent[len(recent)-allowListingMaxRecentPlans:]
	}
	recentRaw, err := json.Marshal(recent)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if err := s.Repository.SetTargetPlan(ctx, &target.ID, plan.PlanID, string(raw), string(recentRaw), now); err != nil {
		s.Logger.Errorw("failed to store allow listing plan", "error", err)
		return nil, errs.Wrap(err)
	}
	return plan, nil
}

// allowListingReportResult is what a report changed, for the audit log
type allowListingReportResult struct {
	Added   []model.AllowListingOp
	Removed []model.AllowListingOp
	Drift   int
}

// Report applies what the runner read back from the tenant to the ledger. A
// value that is not in the ledger is owned only when a recent plan of this
// report added it and the runner did not see it before it changed anything.
// Any other new value is the tenant's. A value that is gone is removed from
// the ledger, and counted as drift when it was owned and the plan did not
// remove it.
func (s *AllowListingSync) Report(
	ctx context.Context,
	target *model.AllowListingTarget,
	report *model.AllowListingReport,
	ip string,
) (err error) {
	ae := NewAuditEvent("AllowListingSync.Report", nil)
	ae.IP = ip
	ae.Details["actor"] = "api"
	ae.Details["targetId"] = target.ID.String()
	defer s.auditDone(ae, &err)

	// a run that failed only reports why, the error shows until a run
	// reports without one
	if report.Failed {
		message := strings.Join(report.CleanErrors(), "; ")
		if message == "" {
			message = "the run failed without an error message"
		}
		ae.Details["failed"] = true
		return errs.Wrap(s.Repository.RecordTargetFailure(ctx, &target.ID, model.CutUTF8("the run failed: "+message, 1000)))
	}
	if len(report.AddedNow) > 3*model.AllowListingSyncMaxObserved {
		return validate.WrapErrorWithField(errors.New("too many values"), "addedNow")
	}
	observed, dropped, err := report.Observed.Ops()
	var before map[string]bool
	if err == nil && report.Before != nil {
		var beforeOps []model.AllowListingOp
		beforeOps, _, err = report.Before.Ops()
		before = map[string]bool{}
		for _, v := range beforeOps {
			before[v.Key()] = true
		}
	}
	if err != nil {
		if recErr := s.Repository.RecordTargetFailure(ctx, &target.ID, "report refused: "+err.Error()); recErr != nil {
			s.Logger.Errorw("failed to record allow listing failure", "error", recErr)
		}
		return err
	}
	changed, err := s.applyReport(ctx, ae, target, report, observed, dropped, before)
	if err != nil {
		return err
	}
	// a report that changed nothing is not written, the runner reports every
	// hour
	if !changed {
		ae.skip()
		return nil
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// applyReport writes a checked report. A nil before claims no values.
func (s *AllowListingSync) applyReport(
	ctx context.Context,
	ae *AuditEvent,
	target *model.AllowListingTarget,
	report *model.AllowListingReport,
	observed []model.AllowListingOp,
	dropped int,
	before map[string]bool,
) (bool, error) {
	defer s.lockTarget(target.ID)()
	now := s.now()
	current, _, err := s.Repository.GetTargetByID(ctx, &target.ID)
	if err != nil {
		return false, errs.Wrap(err)
	}
	planAdds, planRemoves := map[string]bool{}, map[string]bool{}
	planFound := false
	addedNow := map[string]bool{}
	for _, v := range report.AddedNow {
		if op, ok := model.ParseAllowListingKey(v); ok {
			addedNow[op.Key()] = true
		}
	}
	recent := recentPlans(current.RecentPlansJSON, now, recentPlanKeep(current))
	// lostAdds are the adds of plans handed out since the last report that
	// arrived, other than the plan of this report. Their reports were lost or
	// are still to come, so a value one of them added shows up here as
	// already in the tenant and still counts as added by Phishing Club.
	reportPlan, _ := uuid.Parse(report.PlanID)
	lostAdds := map[string]bool{}
	for _, p := range recent {
		if p.ID == reportPlan || (current.LastReportAt != nil && p.IssuedAt.Before(*current.LastReportAt)) {
			continue
		}
		for _, k := range p.Add {
			lostAdds[k] = true
		}
	}
	if id, perr := uuid.Parse(report.PlanID); perr == nil {
		for _, p := range recent {
			// a plan older than it may be applied is used for what the
			// report read back only
			if p.ID != id || now.Sub(p.IssuedAt) > allowListingPlanMaxAge {
				continue
			}
			planFound = true
			for _, k := range p.Add {
				planAdds[k] = true
			}
			for _, k := range p.Remove {
				planRemoves[k] = true
			}
		}
	}

	rows, err := s.Repository.ListEntries(ctx, &target.ID)
	if err != nil {
		return false, errs.Wrap(err)
	}
	ledger := map[string]*database.AllowListingEntry{}
	for _, row := range rows {
		ledger[ledgerKey(row)] = row
	}
	changes := &repository.AllowListingEntryChanges{}
	result := &allowListingReportResult{Added: []model.AllowListingOp{}, Removed: []model.AllowListingOp{}}
	seen := map[string]bool{}
	for _, v := range observed {
		key := v.Key()
		seen[key] = true
		if row, ok := ledger[key]; ok {
			if row.ID != nil {
				changes.Seen = append(changes.Seen, *row.ID)
			}
			continue
		}
		state := model.AllowListingStateTenant
		// added by this run for this plan, with this read not seeing it
		// before, or added by a run whose report was lost
		if (planAdds[key] && addedNow[key] && before != nil && !before[key]) || lostAdds[key] {
			state = model.AllowListingStateOwned
			result.Added = append(result.Added, v)
		}
		changes.Insert = append(changes.Insert, &database.AllowListingEntry{
			Kind: v.Kind, Value: v.Value, State: state,
		})
	}
	for key, row := range ledger {
		if seen[key] || row.ID == nil {
			continue
		}
		// an owned value the read before the change saw but the read after
		// missed, without the plan removing it, was missed by one read. It
		// stays owned, so it is not taken for a tenant value when it shows
		// again. When both reads miss it, it is gone.
		if row.State == model.AllowListingStateOwned && before != nil && before[key] && !planRemoves[key] {
			continue
		}
		changes.Delete = append(changes.Delete, *row.ID)
		if row.State != model.AllowListingStateOwned {
			continue
		}
		if planRemoves[key] {
			result.Removed = append(result.Removed, model.AllowListingOp{Kind: row.Kind, Value: row.Value})
		} else {
			result.Drift++
		}
	}
	runnerErrors := report.CleanErrors()
	// notes say how the report was read, they are not failures of the run
	notes := []string{}
	if report.PlanID != "" && !planFound {
		notes = append(notes, "the report was for an unknown or old plan, only the lists it read were used")
	}
	if before == nil && len(planAdds) > 0 {
		notes = append(notes, "the runner sent no lists from before the change, no value was recorded as added")
	}
	if dropped > 0 {
		notes = append(notes, strconv.Itoa(dropped)+" values in the tenant could not be read and were left out")
	}
	lastError := model.CutUTF8(strings.Join(runnerErrors, "; "), 1000)
	lastNote := model.CutUTF8(strings.Join(notes, "; "), 1000)
	if err := s.Repository.ApplyReport(ctx, &target.ID, changes, now, lastError, lastNote, result.Drift, reportGap(current.LastReportAt, now)); err != nil {
		s.Logger.Errorw("failed to apply allow listing report", "error", err)
		return false, errs.Wrap(err)
	}
	sortOps(result.Added)
	sortOps(result.Removed)
	ae.Details["added"] = opStrings(result.Added)
	ae.Details["removed"] = opStrings(result.Removed)
	ae.Details["drift"] = result.Drift
	ae.Details["planMatched"] = planFound
	if len(runnerErrors) > 0 {
		ae.Details["runnerErrors"] = len(runnerErrors)
	}
	// the same error every hour is written once
	changed := len(result.Added) > 0 || len(result.Removed) > 0 || result.Drift > 0 ||
		(lastError != "" && lastError != current.LastError)
	if current.IsDeleting() {
		done, err := s.finishDelete(ctx, &target.ID, now)
		if err != nil {
			s.Logger.Errorw("failed to finish the allow listing sync delete", "error", err)
		} else if done {
			ae.Details["deleted"] = true
			changed = true
		}
	}
	return changed, nil
}

// finishDelete deletes a target that removes its values before it is
// deleted, once a report shows nothing is left to remove. A domain or
// sending IP the rule needs because the tenant has its own values in it
// stays in the tenant. The caller holds the target lock.
func (s *AllowListingSync) finishDelete(ctx context.Context, id *uuid.UUID, now time.Time) (bool, error) {
	target, _, err := s.Repository.GetTargetByID(ctx, id)
	if err != nil {
		return false, errs.Wrap(err)
	}
	if !target.IsDeleting() {
		return false, nil
	}
	st, err := s.state(ctx, target, now)
	if err != nil {
		return false, errs.Wrap(err)
	}
	if _, remove := st.diff(now); len(remove) > 0 {
		return false, nil
	}
	if err := s.Repository.DeleteTarget(ctx, id); err != nil {
		return false, errs.Wrap(err)
	}
	return true, nil
}

func opStrings(ops []model.AllowListingOp) []string {
	out := make([]string, 0, len(ops))
	for _, v := range ops {
		out = append(out, v.Kind+":"+v.Value)
	}
	return out
}

// statusOf returns the sync status of one campaign or program of a scope
func (s *AllowListingSync) statusOf(
	ctx context.Context,
	companyID *uuid.UUID,
	typ string,
	id uuid.UUID,
) (*model.AllowListingSyncStatus, error) {
	out := &model.AllowListingSyncStatus{
		Status:     model.AllowListingStatusNoTarget,
		Missing:    []model.AllowListingOp{},
		Incomplete: []string{},
	}
	target, _, err := s.Repository.GetTargetByCompanyID(ctx, companyID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, errs.Wrap(err)
	}
	out.TargetID = &target.ID
	out.LastReportAt = target.LastReportAt
	if !target.Enabled {
		out.Status = model.AllowListingStatusDisabled
		return out, nil
	}
	now := s.now()
	st, err := s.state(ctx, target, now)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	out.Status = model.AllowListingStatusNotCounting
	for _, src := range st.sources {
		if src.typ != typ || src.id != id {
			continue
		}
		out.Status = st.sourceStatus(src, now)
		out.Missing = st.missing(src)
		out.Incomplete = src.incomplete
	}
	return out, nil
}
