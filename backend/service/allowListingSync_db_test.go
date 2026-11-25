package service

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

type alsFixture struct {
	*allowListingFixture
	sync  *AllowListingSync
	clock time.Time
}

func newALSFixture(t *testing.T) *alsFixture {
	t.Helper()
	f := newAllowListingFixture(t)
	if err := f.db.AutoMigrate(
		&database.AllowListingTarget{},
		&database.AllowListingEntry{},
		&database.AllowListingPin{},
		&database.Option{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	fx := &alsFixture{allowListingFixture: f, clock: time.Now().UTC()}
	fx.sync = &AllowListingSync{
		Common:                     f.svc.Common,
		Repository:                 &repository.AllowListingSync{DB: f.db},
		AllowListing:               f.svc,
		CampaignTemplateRepository: f.svc.CampaignTemplateRepository,
		CampaignRepository:         f.svc.CampaignRepository,
		ProgramRepository:          f.svc.ProgramRepository,
		CompanyRepository:          &repository.Company{DB: f.db},
		DomainRepository:           f.svc.DomainRepository,
		OptionRepository:           &repository.Option{DB: f.db},
		Now:                        func() time.Time { return fx.clock },
	}
	return fx
}

func (f *alsFixture) advance(d time.Duration) {
	f.clock = f.clock.Add(d)
}

// realCompany adds a company row, a company target needs one
func (f *alsFixture) realCompany(name string) uuid.UUID {
	id := uuid.New()
	f.create(&database.Company{ID: id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: name, AssetsKey: id.String()})
	return id
}

// tmpl adds a template that sends from mailFrom over ip with a landing host
func (f *alsFixture) tmpl(name, mailFrom, ip, host string, companyID *uuid.UUID) uuid.UUID {
	domainID := f.domain(host, nil)
	emailID := f.email("bounce@" + mailFrom)
	smtpID := f.smtpConfig(ip)
	return f.template(allowListingTemplateOpts{
		name: name, domainID: &domainID, emailID: &emailID, smtpID: &smtpID, companyID: companyID,
	})
}

func (f *alsFixture) target(companyID *uuid.UUID, settings model.AllowListingTargetSettings) *model.AllowListingTarget {
	f.t.Helper()
	target, err := f.sync.Upsert(f.ctx, f.session, companyID, &settings)
	if err != nil {
		f.t.Fatalf("upsert target: %v", err)
	}
	if target.Token != "" {
		// the first run of a runner only reads an empty tenant
		f.sendReport(target, &model.AllowListingReport{Before: &model.AllowListingObserved{}})
	}
	return target
}

func defaultSettings() model.AllowListingTargetSettings {
	return model.AllowListingTargetSettings{
		Enabled:     true,
		LeadMinutes: 60, GraceMinutes: 60, IntervalMinutes: 60,
	}
}

func (f *alsFixture) plan(target *model.AllowListingTarget) *model.AllowListingPlan {
	f.t.Helper()
	plan, err := f.sync.Plan(f.ctx, target)
	if err != nil {
		f.t.Fatalf("plan: %v", err)
	}
	return plan
}

// tenant is what the fake tenant holds, applied like the runner applies a plan
type tenant map[string]model.AllowListingOp

func (tn tenant) apply(plan *model.AllowListingPlan) {
	for _, v := range plan.Add {
		tn[v.Key()] = v
	}
	for _, v := range plan.Remove {
		delete(tn, v.Key())
	}
}

func (tn tenant) observed() model.AllowListingObserved {
	r := &model.AllowListingReport{}
	for _, v := range tn {
		switch v.Kind {
		case model.AllowListingKindDomain:
			r.Observed.Domains = append(r.Observed.Domains, v.Value)
		case model.AllowListingKindIP:
			r.Observed.IPs = append(r.Observed.IPs, v.Value)
		case model.AllowListingKindURL:
			r.Observed.URLs = append(r.Observed.URLs, v.Value)
		}
	}
	return r.Observed
}

func (tn tenant) report(planID string) *model.AllowListingReport {
	return &model.AllowListingReport{PlanID: planID, Observed: tn.observed()}
}

func (f *alsFixture) sendReport(target *model.AllowListingTarget, r *model.AllowListingReport) {
	f.t.Helper()
	if err := f.sync.Report(f.ctx, target, r, "198.51.100.7"); err != nil {
		f.t.Fatalf("report: %v", err)
	}
}

// syncOnce runs one plan, apply and report round against the fake tenant
func (f *alsFixture) syncOnce(target *model.AllowListingTarget, tn tenant) *model.AllowListingPlan {
	f.t.Helper()
	before := tn.observed()
	had := map[string]bool{}
	for k := range tn {
		had[k] = true
	}
	plan := f.plan(target)
	tn.apply(plan)
	r := tn.report(plan.PlanID.String())
	r.Before = &before
	for _, v := range plan.Add {
		if !had[v.Key()] {
			r.AddedNow = append(r.AddedNow, v.Kind+":"+v.Value)
		}
	}
	f.sendReport(target, r)
	return plan
}

func (f *alsFixture) ledger(target *model.AllowListingTarget) map[string]string {
	f.t.Helper()
	rows, err := f.sync.Repository.ListEntries(f.ctx, &target.ID)
	if err != nil {
		f.t.Fatalf("ledger: %v", err)
	}
	out := map[string]string{}
	for _, row := range rows {
		out[row.Kind+":"+row.Value] = row.State
	}
	return out
}

func (f *alsFixture) closeCampaign(id uuid.UUID, at time.Time) {
	f.t.Helper()
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", id.String()).
		Update("closed_at", at.UTC()).Error; err != nil {
		f.t.Fatalf("close: %v", err)
	}
}

func opKeys(ops []model.AllowListingOp) string {
	out := []string{}
	for _, v := range ops {
		out = append(out, v.Kind+":"+v.Value)
	}
	return strings.Join(out, "|")
}

// closing one campaign keeps the values another campaign still uses, and the
// values nothing uses go only after the grace time
func TestAllowListingSyncOverlapKeepsSharedValues(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	t2 := f.tmpl("two", "mail-b.example.test", "203.0.113.20", "phish-b.example.test", nil)
	a := f.campaign([]uuid.UUID{t1}, nil, nil)
	f.campaign([]uuid.UUID{t1, t2}, nil, nil)
	target := f.target(nil, defaultSettings())
	tn := tenant{}

	plan := f.syncOnce(target, tn)
	want := "domain:mail-a.example.test|domain:mail-b.example.test|ip:203.0.113.10|ip:203.0.113.20|url:phish-a.example.test/*|url:phish-b.example.test/*"
	if opKeys(plan.Add) != want {
		t.Fatalf("first plan adds: got %s", opKeys(plan.Add))
	}
	for key, state := range f.ledger(target) {
		if state != model.AllowListingStateOwned {
			t.Fatalf("%s should be owned, is %s", key, state)
		}
	}

	f.closeCampaign(a, f.clock)
	f.advance(2 * time.Hour)
	plan = f.syncOnce(target, tn)
	if len(plan.Add) != 0 || len(plan.Remove) != 0 {
		t.Fatalf("closing one of two campaigns that share values changed the plan: add %s remove %s",
			opKeys(plan.Add), opKeys(plan.Remove))
	}
}

// values nothing needs any more are removed after the grace time, except the
// last domain and sending IP, which the Microsoft rule needs
func TestAllowListingSyncRemovesAfterGraceWholeRule(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	a := f.campaign([]uuid.UUID{t1}, nil, nil)
	target := f.target(nil, defaultSettings())
	tn := tenant{}
	f.syncOnce(target, tn)

	f.closeCampaign(a, f.clock)
	f.advance(61 * time.Minute)
	// first plan after the campaign stops counting only marks the values
	plan := f.syncOnce(target, tn)
	if len(plan.Remove) != 0 {
		t.Fatalf("values were removed before the grace time: %s", opKeys(plan.Remove))
	}
	f.advance(61 * time.Minute)
	plan = f.syncOnce(target, tn)
	// nothing needs the rule and everything in it was added by Phishing
	// Club, so all of it goes with the rule
	if !plan.RemoveRule || len(plan.Remove) != 3 {
		t.Fatalf("after the grace time every value and the rule should go: rule %v, got %s", plan.RemoveRule, opKeys(plan.Remove))
	}
	if ledger := f.ledger(target); len(ledger) != 0 {
		t.Fatalf("removed values are still in the ledger: %v", ledger)
	}
	// a new campaign adds the values again, the runner makes the rule again
	f.campaign([]uuid.UUID{t1}, nil, nil)
	plan = f.syncOnce(target, tn)
	if len(plan.Add) != 3 || plan.RemoveRule {
		t.Fatalf("a new campaign should add the values again: %s", opKeys(plan.Add))
	}
}

// values the tenant held before are recorded as the tenant's and are never
// removed, even when Phishing Club no longer needs them
func TestAllowListingSyncTenantValuesNeverRemoved(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	a := f.campaign([]uuid.UUID{t1}, nil, nil)
	settings := defaultSettings()
	// made without the fixture, so the runner has not read anything yet
	target, err := f.sync.Upsert(f.ctx, f.session, nil, &settings)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	tn := tenant{}
	for _, v := range []model.AllowListingOp{
		{Kind: model.AllowListingKindDomain, Value: "mail-a.example.test"},
		{Kind: model.AllowListingKindDomain, Value: "other-vendor.example.test"},
		{Kind: model.AllowListingKindIP, Value: "192.0.2.1"},
		{Kind: model.AllowListingKindURL, Value: "phish-a.example.test/*"},
	} {
		tn[v.Key()] = v
	}
	// the first run only reads
	if plan := f.syncOnce(target, tn); len(plan.Add) != 0 {
		t.Fatalf("a first plan must only read: %s", opKeys(plan.Add))
	}
	for key, state := range f.ledger(target) {
		if state != model.AllowListingStateTenant {
			t.Fatalf("%s was there first and must be the tenant's, is %s", key, state)
		}
	}
	plan := f.syncOnce(target, tn)
	if opKeys(plan.Add) != "ip:203.0.113.10" {
		t.Fatalf("only the missing IP should be added: %s", opKeys(plan.Add))
	}
	f.closeCampaign(a, f.clock)
	f.advance(3 * time.Hour)
	f.syncOnce(target, tn)
	f.advance(3 * time.Hour)
	plan = f.syncOnce(target, tn)
	if opKeys(plan.Remove) != "ip:203.0.113.10" {
		t.Fatalf("only the owned IP may be removed: %s", opKeys(plan.Remove))
	}
	ledger := f.ledger(target)
	for _, key := range []string{"domain:mail-a.example.test", "domain:other-vendor.example.test", "ip:192.0.2.1", "url:phish-a.example.test/*"} {
		if ledger[key] != model.AllowListingStateTenant {
			t.Fatalf("tenant value %s was lost: %v", key, ledger)
		}
	}
}

// an owned value that disappears is counted as drift and added again, and a
// report for an old plan never claims values
func TestAllowListingSyncDriftAndOldPlan(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	f.campaign([]uuid.UUID{t1}, nil, nil)
	target := f.target(nil, defaultSettings())
	tn := tenant{}
	f.syncOnce(target, tn)

	delete(tn, model.AllowListingOp{Kind: model.AllowListingKindURL, Value: "phish-a.example.test/*"}.Key())
	f.sendReport(target, tn.report(""))
	current, _, _ := f.sync.Repository.GetTargetByID(f.ctx, &target.ID)
	if current.DriftCount != 1 {
		t.Fatalf("drift: got %d", current.DriftCount)
	}
	old := f.plan(target)
	if opKeys(old.Add) != "url:phish-a.example.test/*" {
		t.Fatalf("a drifted value should be added again: %s", opKeys(old.Add))
	}
	// a newer plan is handed out, then the runner reports for the first one,
	// which is still recent, so the value it added is owned
	before := tn.observed()
	f.plan(target)
	tn.apply(old)
	r := tn.report(old.PlanID.String())
	r.Before = &before
	r.AddedNow = []string{"url:phish-a.example.test/*"}
	f.sendReport(target, r)
	if state := f.ledger(target)["url:phish-a.example.test/*"]; state != model.AllowListingStateOwned {
		t.Fatalf("a late report of a recent plan lost a value it added: %s", state)
	}
	// a report for a plan that is not known can not claim a value no plan
	// asked for, whatever it says it added
	vendor := model.AllowListingOp{Kind: model.AllowListingKindDomain, Value: "vendor.example.test"}
	f.plan(target)
	tn[vendor.Key()] = vendor
	r = tn.report(uuid.New().String())
	r.Before = &model.AllowListingObserved{}
	r.AddedNow = []string{"domain:vendor.example.test"}
	f.sendReport(target, r)
	if state := f.ledger(target)["domain:vendor.example.test"]; state != model.AllowListingStateTenant {
		t.Fatalf("a report for an unknown plan claimed a value: %s", state)
	}
}

// every target sees only its own scope: the global target only global
// campaigns, a company target only that company
func TestAllowListingSyncTargetsAreIsolated(t *testing.T) {
	f := newALSFixture(t)
	companyA := f.realCompany("a")
	companyB := f.realCompany("b")
	tGlobal := f.tmpl("global", "global.example.test", "203.0.113.1", "g.example.test", nil)
	tA := f.tmpl("a", "a.example.test", "203.0.113.2", "a-phish.example.test", &companyA)
	tB := f.tmpl("b", "b.example.test", "203.0.113.3", "b-phish.example.test", &companyB)
	f.campaign([]uuid.UUID{tGlobal}, nil, nil)
	f.campaign([]uuid.UUID{tA}, &companyA, nil)
	// a company campaign may use a global template
	f.campaign([]uuid.UUID{tGlobal}, &companyB, nil)
	f.campaign([]uuid.UUID{tB}, &companyB, nil)
	// a campaign of company A that names a template of company B gets nothing
	// from it
	f.campaign([]uuid.UUID{tB}, &companyA, nil)

	global := f.target(nil, defaultSettings())
	a := f.target(&companyA, defaultSettings())
	b := f.target(&companyB, defaultSettings())
	if got := opKeys(f.plan(global).Add); got != "domain:global.example.test|ip:203.0.113.1|url:g.example.test/*" {
		t.Fatalf("global target: %s", got)
	}
	if got := opKeys(f.plan(a).Add); got != "domain:a.example.test|ip:203.0.113.2|url:a-phish.example.test/*" {
		t.Fatalf("company A target: %s", got)
	}
	want := "domain:b.example.test|domain:global.example.test|ip:203.0.113.1|ip:203.0.113.3|url:b-phish.example.test/*|url:g.example.test/*"
	if got := opKeys(f.plan(b).Add); got != want {
		t.Fatalf("company B target: %s", got)
	}
	// reports of one target never touch another
	tn := tenant{}
	f.syncOnce(a, tn)
	if len(f.ledger(global)) != 0 || len(f.ledger(b)) != 0 {
		t.Fatalf("a report changed the ledger of another target")
	}
}

// a campaign scheduled beyond the lead time does not count yet
func TestAllowListingSyncLeadTime(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	c := f.campaign([]uuid.UUID{t1}, nil, nil)
	start := f.clock.Add(3 * time.Hour)
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", c.String()).
		Updates(map[string]any{"send_start_at": start, "send_end_at": start.Add(time.Hour)}).Error; err != nil {
		t.Fatalf("schedule: %v", err)
	}
	target := f.target(nil, defaultSettings())
	if got := f.plan(target).Add; len(got) != 0 {
		t.Fatalf("a campaign 3 hours out with a 1 hour lead counted: %s", opKeys(got))
	}
	f.advance(2*time.Hour + time.Minute)
	if got := f.plan(target).Add; len(got) != 3 {
		t.Fatalf("a campaign inside the lead time did not count: %s", opKeys(got))
	}
}

// a campaign waits until its values are in the tenant, is confirmed after a
// report and goes stale when the last report is older than the max age
func TestAllowListingSyncCampaignStatus(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	c := f.campaign([]uuid.UUID{t1}, nil, nil)
	target := f.target(nil, defaultSettings())
	status := func() string {
		st, err := f.sync.GetCampaignStatus(f.ctx, f.session, &c, nil)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		return st.Status
	}
	if got := status(); got != model.AllowListingStatusWaiting {
		t.Fatalf("a campaign not in the tenant should wait: %s", got)
	}
	f.syncOnce(target, tenant{})
	if got := status(); got != model.AllowListingStatusConfirmed {
		t.Fatalf("a synced campaign should be confirmed: %s", got)
	}
	f.advance(4 * time.Hour)
	if got := status(); got != model.AllowListingStatusStale {
		t.Fatalf("a report older than the max age should be stale: %s", got)
	}
}

// a template sent through an API sender lists only its simulation URL and
// is not incomplete
func TestAllowListingSyncAPISenderListsURLOnly(t *testing.T) {
	f := newALSFixture(t)
	domainID := f.domain("phish-api.example.test", nil)
	emailID := f.email("bounce@api.example.test")
	api := f.apiSender()
	tmpl := f.template(allowListingTemplateOpts{name: "api", domainID: &domainID, emailID: &emailID, apiSenderID: &api})
	c := f.campaign([]uuid.UUID{tmpl}, nil, nil)
	target := f.target(nil, defaultSettings())
	plan := f.syncOnce(target, tenant{})
	// mail through an API sender has no IP to list, so only the simulation
	// URL is listed and its domain is left out
	if opKeys(plan.Add) != "url:phish-api.example.test/*" {
		t.Fatalf("an API sender template should only list its URL: %s", opKeys(plan.Add))
	}
	status, err := f.sync.GetCampaignStatus(f.ctx, f.session, &c, nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Status == model.AllowListingStatusIncomplete {
		t.Fatalf("an API sender template is not incomplete: %+v", status)
	}
}

// sources get room in order of start, one that does not fit adds nothing
func TestAllowListingSyncOverLimit(t *testing.T) {
	f := newALSFixture(t)
	target := f.target(nil, defaultSettings())
	first := []uuid.UUID{}
	for i := 0; i < model.AllowListingMaxSendingIPs; i++ {
		n := strconv.Itoa(i + 1)
		first = append(first, f.tmpl("t"+n, "mail.example.test", "203.0.113."+n, "phish"+n+".example.test", nil))
	}
	early := f.campaign(first, nil, nil)
	late := f.tmpl("late", "late.example.test", "198.51.100.99", "late.example.test", nil)
	lateCampaign := f.campaign([]uuid.UUID{late}, nil, nil)
	for id, at := range map[uuid.UUID]time.Time{early: f.clock.Add(-time.Hour), lateCampaign: f.clock} {
		if err := f.db.Model(&database.Campaign{}).Where("id = ?", id.String()).
			Update("created_at", at).Error; err != nil {
			t.Fatalf("update: %v", err)
		}
	}
	plan := f.plan(target)
	for _, v := range plan.Add {
		if strings.Contains(v.Value, "late.example.test") || v.Value == "198.51.100.99" {
			t.Fatalf("the campaign over the limit was partly listed: %s", opKeys(plan.Add))
		}
	}
	status, err := f.sync.GetCampaignStatus(f.ctx, f.session, &lateCampaign, nil)
	if err != nil || status.Status != model.AllowListingStatusOverLimit {
		t.Fatalf("status: %+v %v", status, err)
	}
}

// a delete that removes the values first plans every owned value away at
// once, can be cancelled, and deletes the target once a report shows they are
// gone. Pins count like campaigns.
func TestAllowListingSyncDeleteRemovesValuesAndPins(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	target := f.target(nil, defaultSettings())
	if err := f.sync.CreatePin(f.ctx, f.session, nil, &model.AllowListingPinInput{CampaignTemplateID: &t1}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if err := f.sync.CreatePin(f.ctx, f.session, nil, &model.AllowListingPinInput{
		Kind: model.AllowListingKindURL, Value: "Extra.Example.Test/*",
	}); err != nil {
		t.Fatalf("pin value: %v", err)
	}
	expired := f.clock.Add(time.Minute)
	if err := f.sync.CreatePin(f.ctx, f.session, nil, &model.AllowListingPinInput{
		Kind: model.AllowListingKindIP, Value: "192.0.2.50", ExpiresAt: &expired,
	}); err != nil {
		t.Fatalf("pin expiring: %v", err)
	}
	f.advance(2 * time.Minute)
	tn := tenant{}
	plan := f.syncOnce(target, tn)
	want := "domain:mail-a.example.test|ip:203.0.113.10|url:extra.example.test/*|url:phish-a.example.test/*"
	if opKeys(plan.Add) != want {
		t.Fatalf("pins: got %s", opKeys(plan.Add))
	}
	kept, err := f.sync.DeleteByCompanyID(f.ctx, f.session, nil, true)
	if err != nil || kept == nil || !kept.IsDeleting() {
		t.Fatalf("delete with removal should keep the target until the values are gone: %+v %v", kept, err)
	}
	if _, err := f.sync.CancelDelete(f.ctx, f.session, nil); err != nil {
		t.Fatalf("cancel delete: %v", err)
	}
	target, _, _ = f.sync.Repository.GetTargetByID(f.ctx, &target.ID)
	if target.IsDeleting() || len(f.plan(target).Remove) != 0 {
		t.Fatalf("a cancelled delete still removes values")
	}
	if _, err := f.sync.DeleteByCompanyID(f.ctx, f.session, nil, true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	target, _, _ = f.sync.Repository.GetTargetByID(f.ctx, &target.ID)
	plan = f.syncOnce(target, tn)
	want = "domain:mail-a.example.test|ip:203.0.113.10|url:extra.example.test/*|url:phish-a.example.test/*"
	if !plan.Deleting || !plan.RemoveRule || opKeys(plan.Remove) != want {
		t.Fatalf("a delete should remove every owned value and the rule: %s %v", opKeys(plan.Remove), plan.RemoveRule)
	}
	if _, _, err := f.sync.Repository.GetTargetByID(f.ctx, &target.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("the target was not deleted after its values were removed: %v", err)
	}
}

// a delete with removal and nothing owned, or without removal, is done at once
func TestAllowListingSyncDeleteAtOnce(t *testing.T) {
	f := newALSFixture(t)
	target := f.target(nil, defaultSettings())
	kept, err := f.sync.DeleteByCompanyID(f.ctx, f.session, nil, true)
	if err != nil || kept != nil {
		t.Fatalf("a target with nothing owned should be deleted at once: %+v %v", kept, err)
	}
	f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	target = f.target(nil, defaultSettings())
	if err := f.sync.CreatePin(f.ctx, f.session, nil, &model.AllowListingPinInput{
		Kind: model.AllowListingKindURL, Value: "extra.example.test/*",
	}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	f.syncOnce(target, tenant{})
	if kept, err := f.sync.DeleteByCompanyID(f.ctx, f.session, nil, false); err != nil || kept != nil {
		t.Fatalf("a delete without removal should be done at once: %+v %v", kept, err)
	}
}

// a company target pins only global templates or its own
func TestAllowListingSyncPinScope(t *testing.T) {
	f := newALSFixture(t)
	companyA := f.realCompany("a")
	companyB := f.realCompany("b")
	tB := f.tmpl("b", "b.example.test", "203.0.113.3", "b-phish.example.test", &companyB)
	f.target(&companyA, defaultSettings())
	f.target(nil, defaultSettings())
	if err := f.sync.CreatePin(f.ctx, f.session, &companyA, &model.AllowListingPinInput{CampaignTemplateID: &tB}); !isNotFound(err) {
		t.Fatalf("a company pinned another company's template: %v", err)
	}
	if err := f.sync.CreatePin(f.ctx, f.session, nil, &model.AllowListingPinInput{CampaignTemplateID: &tB}); !isNotFound(err) {
		t.Fatalf("the global target pinned a company template: %v", err)
	}
}

// the runner token, a disabled target and made up ids all fail alike
func TestAllowListingSyncAuthenticate(t *testing.T) {
	f := newALSFixture(t)
	target := f.target(nil, defaultSettings())
	if target.Token == "" {
		t.Fatalf("a new target has no token")
	}
	if _, ok := f.sync.Authenticate(f.ctx, target.ID.String(), target.Token, "198.51.100.7"); !ok {
		t.Fatalf("the right token was refused")
	}
	if _, ok := f.sync.Authenticate(f.ctx, target.ID.String(), "wrong", "198.51.100.7"); ok {
		t.Fatalf("a wrong token was accepted")
	}
	if _, ok := f.sync.Authenticate(f.ctx, uuid.New().String(), target.Token, "198.51.100.7"); ok {
		t.Fatalf("a token was accepted for another target id")
	}
	if _, ok := f.sync.Authenticate(f.ctx, "not-a-uuid", target.Token, "198.51.100.7"); ok {
		t.Fatalf("a bad id was accepted")
	}
	rotated, err := f.sync.RotateToken(f.ctx, f.session, nil)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, ok := f.sync.Authenticate(f.ctx, target.ID.String(), target.Token, "198.51.100.7"); ok {
		t.Fatalf("the old token works after a rotate")
	}
	// a disabled target still talks to its runner, it gets an empty plan
	settings := defaultSettings()
	settings.Enabled = false
	f.target(nil, settings)
	disabled, ok := f.sync.Authenticate(f.ctx, target.ID.String(), rotated.Token, "198.51.100.7")
	if !ok {
		t.Fatalf("a disabled target must still get plans")
	}
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	f.campaign([]uuid.UUID{t1}, nil, nil)
	if plan := f.plan(disabled); len(plan.Add) != 0 || len(plan.Remove) != 0 {
		t.Fatalf("a disabled target got changes: %s", opKeys(plan.Add))
	}
}

// a value whose report was lost is still owned when a plan handed out after
// the last report asked for it
func TestAllowListingSyncLostReportKeepsOwnership(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	f.campaign([]uuid.UUID{t1}, nil, nil)
	target := f.target(nil, defaultSettings())
	tn := tenant{}
	lost := f.plan(target)
	tn.apply(lost)
	// the report of that run never arrives, the next run sees the values
	// already there
	before := tn.observed()
	next := f.plan(target)
	r := tn.report(next.PlanID.String())
	r.Before = &before
	f.sendReport(target, r)
	for key, state := range f.ledger(target) {
		if state != model.AllowListingStateOwned {
			t.Fatalf("%s was added by Phishing Club but stored as %s", key, state)
		}
	}
	// a value no plan asked for stays the tenant's
	op := model.AllowListingOp{Kind: model.AllowListingKindDomain, Value: "vendor.example.test"}
	tn[op.Key()] = op
	before = tn.observed()
	last := f.plan(target)
	r = tn.report(last.PlanID.String())
	r.Before = &before
	f.sendReport(target, r)
	if state := f.ledger(target)["domain:vendor.example.test"]; state != model.AllowListingStateTenant {
		t.Fatalf("a value no plan added was claimed: %s", state)
	}
}

// one read that misses an owned value does not lose it: when the read
// before the change saw it and only the read after missed it, the value
// stays owned. When both reads miss it, the ledger lets it go.
func TestAllowListingSyncMissedReadKeepsOwned(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	f.campaign([]uuid.UUID{t1}, nil, nil)
	target := f.target(nil, defaultSettings())
	tn := tenant{}
	f.syncOnce(target, tn)
	f.syncOnce(target, tn)
	urlKey := ""
	for key, state := range f.ledger(target) {
		if state != model.AllowListingStateOwned {
			t.Fatalf("%s not owned", key)
		}
		if strings.HasPrefix(key, "url:") {
			urlKey = key
		}
	}
	if urlKey == "" {
		t.Fatalf("no URL owned")
	}
	missed := tenant{}
	for k, v := range tn {
		if v.Kind != model.AllowListingKindURL {
			missed[k] = v
		}
	}
	// the read after the change misses the URL once
	f.advance(61 * time.Minute)
	plan := f.plan(target)
	before := tn.observed()
	r := missed.report(plan.PlanID.String())
	r.Before = &before
	f.sendReport(target, r)
	if state := f.ledger(target)[urlKey]; state != model.AllowListingStateOwned {
		t.Fatalf("one missed read changed %s to %q", urlKey, state)
	}
	// hours later the read is right again, no recent plan added the URL
	f.advance(6 * time.Hour)
	f.syncOnce(target, tn)
	for key, state := range f.ledger(target) {
		if state != model.AllowListingStateOwned {
			t.Fatalf("%s was owned but is now %s after one missed read", key, state)
		}
	}
	// both reads miss it, as when the tenant removed it by hand
	f.advance(61 * time.Minute)
	plan = f.plan(target)
	before = missed.observed()
	r = missed.report(plan.PlanID.String())
	r.Before = &before
	f.sendReport(target, r)
	if state, ok := f.ledger(target)[urlKey]; ok {
		t.Fatalf("%s missed by both reads is still in the ledger as %s", urlKey, state)
	}
}

// a run that failed reports only why: the error shows and the ledger is
// left alone until a run reports without an error
func TestAllowListingSyncFailedRunReported(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	f.campaign([]uuid.UUID{t1}, nil, nil)
	target := f.target(nil, defaultSettings())
	tn := tenant{}
	f.syncOnce(target, tn)
	f.syncOnce(target, tn)
	ledgerBefore := f.ledger(target)
	f.sendReport(target, &model.AllowListingReport{Failed: true, Errors: []string{"Connect-ExchangeOnline failed"}})
	got, _, err := f.sync.Repository.GetTargetByID(f.ctx, &target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.LastError, "Connect-ExchangeOnline failed") || got.FailureCount == 0 {
		t.Fatalf("the failure was not recorded: %q %d", got.LastError, got.FailureCount)
	}
	if after := f.ledger(target); len(after) != len(ledgerBefore) {
		t.Fatalf("a failed run changed the ledger: %v", after)
	}
	f.advance(61 * time.Minute)
	f.syncOnce(target, tn)
	got, _, _ = f.sync.Repository.GetTargetByID(f.ctx, &target.ID)
	if got.LastError != "" || got.FailureCount != 0 {
		t.Fatalf("a run without errors did not clear the failure: %q %d", got.LastError, got.FailureCount)
	}
}

// a target that is turned off adds nothing but still removes what nothing
// needs, so no value stays with no end
func TestAllowListingSyncTurnedOffStillRemoves(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	c := f.campaign([]uuid.UUID{t1}, nil, nil)
	target := f.target(nil, defaultSettings())
	tn := tenant{}
	f.syncOnce(target, tn)
	f.syncOnce(target, tn)
	if len(tn) == 0 {
		t.Fatalf("nothing was added")
	}
	off := defaultSettings()
	off.Enabled = false
	target = f.target(nil, off)
	t2 := f.tmpl("two", "mail-b.example.test", "203.0.113.20", "phish-b.example.test", nil)
	f.campaign([]uuid.UUID{t2}, nil, nil)
	f.closeCampaign(c, f.clock)
	f.advance(61 * time.Minute)
	if plan := f.syncOnce(target, tn); len(plan.Add) != 0 {
		t.Fatalf("a turned off target added: %s", opKeys(plan.Add))
	}
	f.advance(61 * time.Minute)
	plan := f.syncOnce(target, tn)
	if len(plan.Add) != 0 || len(plan.Remove) == 0 {
		t.Fatalf("a turned off target should only remove: add %s remove %s", opKeys(plan.Add), opKeys(plan.Remove))
	}
}

// keep after close is counted once: the values go one grace time after the
// first run that sees the campaign closed, not after a second grace time
func TestAllowListingSyncGraceCountedOnce(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	c := f.campaign([]uuid.UUID{t1}, nil, nil)
	settings := defaultSettings()
	settings.GraceMinutes = 180
	target := f.target(nil, settings)
	tn := tenant{}
	f.syncOnce(target, tn)
	f.syncOnce(target, tn)
	if len(tn) == 0 {
		t.Fatalf("nothing was added")
	}
	f.closeCampaign(c, f.clock)
	f.advance(61 * time.Minute)
	if plan := f.syncOnce(target, tn); len(plan.Remove) != 0 {
		t.Fatalf("removed before the grace time: %s", opKeys(plan.Remove))
	}
	f.advance(target.Grace() + time.Minute)
	plan := f.syncOnce(target, tn)
	if len(plan.Remove) == 0 {
		t.Fatalf("not removed one grace time after the close")
	}
}

// on a long interval the next run comes hours after a lost report, the
// adds of the lost plan still count, so the values stay owned and are
// removed once nothing needs them
func TestAllowListingSyncLostReportLongInterval(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	c := f.campaign([]uuid.UUID{t1}, nil, nil)
	settings := defaultSettings()
	settings.IntervalMinutes = 360
	settings.LeadMinutes = 360
	settings.GraceMinutes = 360
	target := f.target(nil, settings)
	tn := tenant{}
	lost := f.plan(target)
	if len(lost.Add) == 0 {
		t.Fatalf("nothing to add")
	}
	tn.apply(lost)
	// the report is lost, the campaign closes and the next run is 6 hours
	// later, after the grace time, so its plan no longer adds the values
	f.closeCampaign(c, f.clock)
	f.advance(6*time.Hour + 30*time.Minute)
	before := tn.observed()
	next := f.plan(target)
	if len(next.Add) != 0 {
		t.Fatalf("the closed campaign still adds: %s", opKeys(next.Add))
	}
	r := tn.report(next.PlanID.String())
	r.Before = &before
	f.sendReport(target, r)
	for key, state := range f.ledger(target) {
		if state != model.AllowListingStateOwned {
			t.Fatalf("%s was added by Phishing Club but stored as %s", key, state)
		}
	}
}

// a tenant value in a form Phishing Club does not write is kept as the
// tenant's, a value with control characters or too long is left out, and
// only a report over the limits is refused
func TestAllowListingSyncReportValidation(t *testing.T) {
	f := newALSFixture(t)
	target := f.target(nil, defaultSettings())
	r := &model.AllowListingReport{Before: &model.AllowListingObserved{}, Observed: model.AllowListingObserved{
		Domains: []string{"Contoso.Example.", "bad\x00domain.example.test", strings.Repeat("a", 400)},
		IPs:     []string{"192.168.0.1-192.168.0.254", "10.0.0.0/8"},
		URLs:    []string{"*.Contoso.Example/path*", "b\u00fccher.example/*"},
	}}
	if err := f.sync.Report(f.ctx, target, r, "198.51.100.7"); err != nil {
		t.Fatalf("a report with tenant forms was refused: %v", err)
	}
	ledger := f.ledger(target)
	for _, key := range []string{
		"domain:contoso.example", "ip:192.168.0.1-192.168.0.254", "ip:10.0.0.0/8",
		"url:*.contoso.example/path*", "url:b\u00fccher.example/*",
	} {
		if ledger[key] != model.AllowListingStateTenant {
			t.Fatalf("%s should be kept as the tenant's: %v", key, ledger)
		}
	}
	if len(ledger) != 5 {
		t.Fatalf("bad values were stored: %v", ledger)
	}
	current, _, _ := f.sync.Repository.GetTargetByID(f.ctx, &target.ID)
	if !strings.Contains(current.LastNote, "2 values") || current.LastError != "" || current.FailureCount != 0 {
		t.Fatalf("left out values should be a note, not an error: note %q error %q", current.LastNote, current.LastError)
	}
	tooMany := make([]string, model.AllowListingSyncMaxObserved+1)
	for i := range tooMany {
		tooMany[i] = "192.0.2." + strconv.Itoa(i%100)
	}
	r = &model.AllowListingReport{Observed: model.AllowListingObserved{IPs: tooMany}}
	if err := f.sync.Report(f.ctx, target, r, "198.51.100.7"); err == nil {
		t.Fatalf("a report over the limit was accepted")
	}
	// a pin can not list a wide range or a wildcard
	for _, in := range []model.AllowListingPinInput{
		{Kind: model.AllowListingKindIP, Value: "0.0.0.0/0"},
		{Kind: model.AllowListingKindIP, Value: "2001:db8::/32"},
		{Kind: model.AllowListingKindURL, Value: "*.co.uk/*"},
		{Kind: model.AllowListingKindIP, Value: "::ffff:10.0.0.0/120"},
		{Kind: model.AllowListingKindIP, Value: "2001:db8:0::1"},
	} {
		in := in
		if err := f.sync.CreatePin(f.ctx, f.session, nil, &in); err == nil {
			t.Fatalf("pin %s was accepted", in.Value)
		}
	}
}

// a value the tenant had when Phishing Club first wanted it stays the
// tenant's, even when the plan asked to add it
func TestAllowListingSyncPlanAddOfPresentValueStaysTenant(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	f.campaign([]uuid.UUID{t1}, nil, nil)
	target := f.target(nil, defaultSettings())
	// the tenant adds the domain between the first read and the next plan
	tn := tenant{}
	op := model.AllowListingOp{Kind: model.AllowListingKindDomain, Value: "mail-a.example.test"}
	tn[op.Key()] = op
	f.syncOnce(target, tn)
	if state := f.ledger(target)["domain:mail-a.example.test"]; state != model.AllowListingStateTenant {
		t.Fatalf("a value in the tenant before the change was claimed: %s", state)
	}
}

// a campaign that starts earlier never takes listed values away from a
// campaign that already has them
func TestAllowListingSyncFitKeepsListedValues(t *testing.T) {
	f := newALSFixture(t)
	target := f.target(nil, defaultSettings())
	poolA := []uuid.UUID{}
	for i := 0; i < 6; i++ {
		n := strconv.Itoa(i + 1)
		poolA = append(poolA, f.tmpl("a"+n, "mail.example.test", "203.0.113."+n, "pa"+n+".example.test", nil))
	}
	a := f.campaign(poolA, nil, nil)
	tn := tenant{}
	f.syncOnce(target, tn)
	poolB := []uuid.UUID{}
	for i := 0; i < 5; i++ {
		n := strconv.Itoa(i + 50)
		poolB = append(poolB, f.tmpl("b"+n, "mail.example.test", "198.51.100."+n, "pb"+n+".example.test", nil))
	}
	b := f.campaign(poolB, nil, nil)
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", b.String()).
		Update("created_at", f.clock.Add(-48*time.Hour)).Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	plan := f.syncOnce(target, tn)
	if len(plan.Remove) != 0 || len(plan.Add) != 0 {
		t.Fatalf("the earlier campaign over the limit changed the lists: add %s remove %s", opKeys(plan.Add), opKeys(plan.Remove))
	}
	f.advance(3 * time.Hour)
	plan = f.syncOnce(target, tn)
	if len(plan.Remove) != 0 {
		t.Fatalf("values of a counting campaign were removed: %s", opKeys(plan.Remove))
	}
	for id, want := range map[uuid.UUID]string{a: model.AllowListingStatusStale, b: model.AllowListingStatusOverLimit} {
		status, err := f.sync.GetCampaignStatus(f.ctx, f.session, &id, nil)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if want == model.AllowListingStatusStale && status.Status != model.AllowListingStatusConfirmed {
			t.Fatalf("the listed campaign is not confirmed: %+v", status)
		}
		if want == model.AllowListingStatusOverLimit && status.Status != want {
			t.Fatalf("the new campaign should be over the limit: %+v", status)
		}
	}
}

// the runbook only names a domain the scope may use and the seeded path
func TestAllowListingSyncRunbook(t *testing.T) {
	f := newALSFixture(t)
	companyA := f.realCompany("a")
	companyB := f.realCompany("b")
	f.domain("runner.example.test", nil)
	ownB := uuid.New()
	f.create(&database.Domain{ID: ownB, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "b-only.example.test", CompanyID: &companyB})
	optID := uuid.New()
	path := strings.Repeat("a1", 16)
	f.create(&database.Option{ID: &optID, Key: data.OptionKeyAllowListingSyncPath, Value: path})
	target := f.target(&companyA, defaultSettings())

	rb, err := f.sync.Runbook(f.ctx, f.session, &companyA, "Runner.Example.Test")
	if err != nil {
		t.Fatalf("runbook: %v", err)
	}
	want := "https://runner.example.test/" + path + "/" + target.ID.String()
	if rb.RunnerURL != want || !strings.Contains(rb.Runbook, "'"+want+"'") {
		t.Fatalf("runbook URL: got %s", rb.RunnerURL)
	}
	if strings.Contains(rb.Runbook, target.Token) {
		t.Fatalf("the runbook holds the token")
	}
	if rb.Schedule != "every hour" || rb.ModuleVersion != allowListingExchangeModuleVersion {
		t.Fatalf("the setup does not name the hourly schedule")
	}
	settings := defaultSettings()
	settings.IntervalMinutes = 360
	settings.LeadMinutes = 1440
	settings.GraceMinutes = 1440
	f.target(&companyA, settings)
	rb, err = f.sync.Runbook(f.ctx, f.session, &companyA, "runner.example.test")
	if err != nil || rb.Schedule != "every 6 hours" {
		t.Fatalf("the setup does not name the 6 hour schedule: %v", err)
	}
	for _, bad := range []string{"b-only.example.test", "missing.example.test", "x'; Remove-Item", ""} {
		if _, err := f.sync.Runbook(f.ctx, f.session, &companyA, bad); err == nil {
			t.Fatalf("runbook accepted domain %q", bad)
		}
	}
}

// the runner domain is stored on the target, only a domain of the scope
// that is not a proxy is taken, and a stored domain that is later deleted
// or made a proxy is reported
func TestAllowListingSyncRunnerDomain(t *testing.T) {
	f := newALSFixture(t)
	companyA := f.realCompany("a")
	companyB := f.realCompany("b")
	runner := f.domain("runner.example.test", nil)
	proxyID := f.proxy()
	f.domain("proxied.example.test", &proxyID)
	ownB := uuid.New()
	f.create(&database.Domain{ID: ownB, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "b-only.example.test", CompanyID: &companyB})
	f.target(&companyA, defaultSettings())

	got, err := f.sync.SetRunnerDomain(f.ctx, f.session, &companyA, "Runner.Example.Test.")
	if err != nil || got.RunnerDomain != "runner.example.test" || got.RunnerDomainIssue != "" {
		t.Fatalf("set runner domain: %v %+v", err, got)
	}
	for _, bad := range []string{"b-only.example.test", "proxied.example.test", "missing.example.test", "x'; Remove-Item", ""} {
		if _, err := f.sync.SetRunnerDomain(f.ctx, f.session, &companyA, bad); err == nil {
			t.Fatalf("runner domain %q accepted", bad)
		}
	}
	got, err = f.sync.GetByCompanyID(f.ctx, f.session, &companyA)
	if err != nil || got.RunnerDomain != "runner.example.test" || got.RunnerDomainIssue != "" {
		t.Fatalf("a refused domain changed the stored one: %v %+v", err, got)
	}

	if err := f.db.Model(&database.Domain{}).Where("id = ?", runner).Update("proxy_id", f.proxy()).Error; err != nil {
		t.Fatal(err)
	}
	got, err = f.sync.GetByCompanyID(f.ctx, f.session, &companyA)
	if err != nil || got.RunnerDomainIssue != AllowListingRunnerDomainProxy {
		t.Fatalf("proxy runner domain not reported: %v %+v", err, got)
	}
	if err := f.db.Where("id = ?", runner).Delete(&database.Domain{}).Error; err != nil {
		t.Fatal(err)
	}
	got, err = f.sync.GetByCompanyID(f.ctx, f.session, &companyA)
	if err != nil || got.RunnerDomain != "runner.example.test" || got.RunnerDomainIssue != AllowListingRunnerDomainDeleted {
		t.Fatalf("deleted runner domain not reported: %v %+v", err, got)
	}
}

// a value a plan removed counts as missing until a report shows it, so a
// campaign that needs it again is not confirmed and the value is added back
func TestAllowListingSyncPendingRemoveIsMissing(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	t2 := f.tmpl("two", "mail-b.example.test", "203.0.113.20", "phish-b.example.test", nil)
	a := f.campaign([]uuid.UUID{t1, t2}, nil, nil)
	target := f.target(nil, defaultSettings())
	tn := tenant{}
	f.syncOnce(target, tn)
	f.closeCampaign(a, f.clock)
	f.advance(61 * time.Minute)
	f.syncOnce(target, tn)
	f.advance(61 * time.Minute)
	removing := f.plan(target)
	if len(removing.Remove) == 0 {
		t.Fatalf("nothing to remove")
	}
	// before the report of that plan arrives a campaign needs t2 again
	b := f.campaign([]uuid.UUID{t2}, nil, nil)
	target, _, _ = f.sync.Repository.GetTargetByID(f.ctx, &target.ID)
	status, err := f.sync.GetCampaignStatus(f.ctx, f.session, &b, nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Status == model.AllowListingStatusConfirmed {
		t.Fatalf("a value a plan removes read as confirmed")
	}
	next := f.plan(target)
	if !strings.Contains(opKeys(next.Add), "url:phish-b.example.test/*") {
		t.Fatalf("a removed value that is needed again is not added back: %s", opKeys(next.Add))
	}
}

// an owned value that stays because the rule needs it is shown as kept
func TestAllowListingSyncEntriesShowKept(t *testing.T) {
	f := newALSFixture(t)
	t1 := f.tmpl("one", "mail-a.example.test", "203.0.113.10", "phish-a.example.test", nil)
	a := f.campaign([]uuid.UUID{t1}, nil, nil)
	target := f.target(nil, defaultSettings())
	// the tenant has its own sending IP in the rule, so the rule stays and
	// needs a domain: the domain Phishing Club added is kept
	own := model.AllowListingOp{Kind: model.AllowListingKindIP, Value: "198.51.100.7"}
	tn := tenant{own.Key(): own}
	f.syncOnce(target, tn)
	f.closeCampaign(a, f.clock)
	f.advance(61 * time.Minute)
	f.syncOnce(target, tn)
	f.advance(61 * time.Minute)
	f.syncOnce(target, tn)
	entries, err := f.sync.Entries(f.ctx, f.session, nil)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	kept := 0
	for _, e := range entries {
		if e.Kept {
			kept++
		}
	}
	if kept != 1 {
		t.Fatalf("the last domain should be shown as kept: %+v", entries)
	}
	if _, ok := tn[own.Key()]; !ok {
		t.Fatalf("the tenant IP was removed")
	}
}

// the interval decides how old a report may be, the lead and keep times
// can not be shorter than it, and the time between reports is stored
func TestAllowListingSyncInterval(t *testing.T) {
	f := newALSFixture(t)
	for _, bad := range []model.AllowListingTargetSettings{
		{Enabled: true, IntervalMinutes: 30, LeadMinutes: 1440, GraceMinutes: 1440},
		{Enabled: true, IntervalMinutes: 120, LeadMinutes: 1440, GraceMinutes: 1440},
		{Enabled: true, IntervalMinutes: 360, LeadMinutes: 60, GraceMinutes: 1440},
		{Enabled: true, IntervalMinutes: 360, LeadMinutes: 1440, GraceMinutes: 60},
	} {
		bad := bad
		if _, err := f.sync.Upsert(f.ctx, f.session, nil, &bad); err == nil {
			t.Fatalf("settings accepted: %+v", bad)
		}
	}
	settings := defaultSettings()
	settings.IntervalMinutes = 360
	settings.LeadMinutes = 720
	settings.GraceMinutes = 720
	target := f.target(nil, settings)
	if target.MaxReportAgeMinutes != 2*360+60 {
		t.Fatalf("max report age: %d", target.MaxReportAgeMinutes)
	}
	f.advance(6 * time.Hour)
	f.syncOnce(target, tenant{})
	got, _, err := f.sync.Repository.GetTargetByID(f.ctx, &target.ID)
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	if got.ReportGapMinutes != 360 {
		t.Fatalf("report gap: %d", got.ReportGapMinutes)
	}
	// a report 12 hours old is still fresh on a 6 hour interval
	f.advance(12 * time.Hour)
	if !reportFresh(got, f.clock) {
		t.Fatalf("a report within two runs of a 6 hour interval is stale")
	}
	f.advance(2 * time.Hour)
	if reportFresh(got, f.clock) {
		t.Fatalf("a report older than two runs and an hour is fresh")
	}
}
