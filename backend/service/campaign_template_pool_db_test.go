package service

import (
	"context"
	"errors"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type poolFixture struct {
	t       *testing.T
	ctx     context.Context
	db      *gorm.DB
	svc     *Campaign
	session *model.Session
	now     time.Time
}

func newPoolFixture(t *testing.T) *poolFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "pool.db") + "?_foreign_keys=OFF"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Company{},
		&database.Domain{},
		&database.Identifier{},
		&database.CampaignTemplate{},
		&database.Campaign{},
		&database.CampaignTemplatePool{},
		&database.CampaignRecipient{},
		&database.Recipient{},
		&database.RecipientGroup{},
		&database.RecipientGroupRecipient{},
		&database.CampaignRecipientGroup{},
		&database.CampaignEvent{},
		&database.Event{},
		&database.AllowDeny{},
		&database.CampaignAllowDeny{},
		&database.CampaignWebhook{},
		&database.CampaignScript{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, name := range []string{
		data.EVENT_CAMPAIGN_SCHEDULED,
		data.EVENT_CAMPAIGN_SELF_MANAGED,
		data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED,
		data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED,
		data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED,
		data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED,
		data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA,
		data.EVENT_CAMPAIGN_RECIPIENT_REPORTED,
		data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED,
		data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED,
	} {
		if cache.EventIDByName[name] == nil {
			id := uuid.New()
			cache.EventIDByName[name] = &id
			cache.EventNameByID[id.String()] = name
		}
	}
	common := Common{Logger: zap.NewNop().Sugar()}
	campaignRepo := &repository.Campaign{DB: db}
	svc := &Campaign{
		Common:                      common,
		CampaignRepository:          campaignRepo,
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: db},
		RecipientGroupRepository:    &repository.RecipientGroup{DB: db},
		AllowDenyRepository:         &repository.AllowDeny{DB: db},
		CampaignTemplateService: &CampaignTemplate{
			Common:                     common,
			CampaignTemplateRepository: &repository.CampaignTemplate{DB: db},
			CampaignRepository:         campaignRepo,
		},
		TemplateRand: rand.New(rand.NewPCG(11, 12)),
	}
	return &poolFixture{
		t:       t,
		ctx:     context.Background(),
		db:      db,
		svc:     svc,
		session: authorizedSession(t),
		now:     time.Now().UTC(),
	}
}

type templateOpts struct {
	unusable   bool
	mode       string
	length     int
	training   bool
	domainID   *uuid.UUID
	companyID  *uuid.UUID
	nameSuffix string
}

func (f *poolFixture) template(o templateOpts) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	urlIdentifier, stateIdentifier := uuid.New(), uuid.New()
	mode := o.mode
	if mode == "" {
		mode = data.LureURLModeQuery
	}
	length := o.length
	if length == 0 {
		length = 12
	}
	row := &database.CampaignTemplate{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now,
		Name:              "template " + id.String()[:8] + o.nameSuffix,
		URLIdentifierID:   &urlIdentifier,
		StateIdentifierID: &stateIdentifier,
		LureURLMode:       mode,
		LureCodeAlgo:      "crockford32",
		LureCodeLength:    length,
		IsUsable:          !o.unusable,
		IsTraining:        o.training,
		DomainID:          o.domainID,
		CompanyID:         o.companyID,
	}
	if err := f.db.Create(row).Error; err != nil {
		f.t.Fatalf("insert template: %v", err)
	}
	return id
}

// campaign stores a campaign with the given pool. nil sendStart makes it self
// managed.
func (f *poolFixture) campaign(pool []uuid.UUID, selfManaged bool, anonymous bool) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	row := map[string]any{
		"id":                   id,
		"created_at":           utils.NowRFC3339UTC(),
		"updated_at":           utils.NowRFC3339UTC(),
		"name":                 "pool campaign " + id.String(),
		"sort_field":           "email",
		"sort_order":           "asc",
		"campaign_template_id": pool[0].String(),
		"is_anonymous":         anonymous,
	}
	if !selfManaged {
		row["send_start_at"] = utils.RFC3339UTC(f.now.Add(time.Hour))
		row["send_end_at"] = utils.RFC3339UTC(f.now.Add(5 * time.Hour))
	}
	if err := f.db.Model(&database.Campaign{}).Create(row).Error; err != nil {
		f.t.Fatalf("insert campaign: %v", err)
	}
	if err := f.svc.CampaignRepository.SetTemplatePool(f.ctx, &id, pool); err != nil {
		f.t.Fatalf("set pool: %v", err)
	}
	return id
}

// group stores a recipient group with one recipient per department entry
func (f *poolFixture) group(departments []string) uuid.UUID {
	f.t.Helper()
	groupID := uuid.New()
	if err := f.db.Create(&database.RecipientGroup{
		ID: &groupID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "group " + groupID.String(),
	}).Error; err != nil {
		f.t.Fatalf("insert group: %v", err)
	}
	f.addToGroup(groupID, departments)
	return groupID
}

func (f *poolFixture) addToGroup(groupID uuid.UUID, departments []string) []uuid.UUID {
	f.t.Helper()
	ids := []uuid.UUID{}
	for _, d := range departments {
		rid := uuid.New()
		email := rid.String() + "@pool.test"
		if err := f.db.Create(&database.Recipient{
			ID: &rid, CreatedAt: &f.now, UpdatedAt: &f.now, Email: &email, Department: d,
		}).Error; err != nil {
			f.t.Fatalf("insert recipient: %v", err)
		}
		if err := f.db.Create(&database.RecipientGroupRecipient{
			RecipientID: &rid, RecipientGroupID: &groupID,
		}).Error; err != nil {
			f.t.Fatalf("insert group member: %v", err)
		}
		ids = append(ids, rid)
	}
	return ids
}

func (f *poolFixture) loadCampaign(id uuid.UUID, groupID uuid.UUID) *model.Campaign {
	f.t.Helper()
	c, err := f.svc.CampaignRepository.GetByID(f.ctx, &id, &repository.CampaignOption{})
	if err != nil {
		f.t.Fatalf("load campaign: %v", err)
	}
	c.RecipientGroupIDs = nullable.NewNullableWithValue([]*uuid.UUID{&groupID})
	return c
}

type crRow struct {
	ID                 uuid.UUID
	RecipientID        *uuid.UUID
	CampaignTemplateID *uuid.UUID
	Department         string
	SentAt             *time.Time
}

func (f *poolFixture) recipientsOf(campaignID uuid.UUID) []crRow {
	f.t.Helper()
	var rows []crRow
	if err := f.db.Table("campaign_recipients AS cr").
		Select("cr.id, cr.recipient_id, cr.campaign_template_id, r.department, cr.sent_at").
		Joins("LEFT JOIN recipients r ON r.id = cr.recipient_id").
		Where("cr.campaign_id = ?", campaignID.String()).
		Scan(&rows).Error; err != nil {
		f.t.Fatalf("read campaign recipients: %v", err)
	}
	return rows
}

// seedCampaignRecipient writes a campaign recipient row directly
func (f *poolFixture) seedCampaignRecipient(campaignID uuid.UUID, templateID *uuid.UUID, sent bool, department string) uuid.UUID {
	f.t.Helper()
	rid := uuid.New()
	email := rid.String() + "@seed.test"
	if err := f.db.Create(&database.Recipient{
		ID: &rid, CreatedAt: &f.now, UpdatedAt: &f.now, Email: &email, Department: department,
	}).Error; err != nil {
		f.t.Fatalf("insert recipient: %v", err)
	}
	id := uuid.New()
	row := map[string]any{
		"id":           id,
		"created_at":   utils.NowRFC3339UTC(),
		"updated_at":   utils.NowRFC3339UTC(),
		"campaign_id":  campaignID.String(),
		"recipient_id": rid.String(),
		"self_managed": false,
	}
	if templateID != nil {
		row["campaign_template_id"] = templateID.String()
	}
	if sent {
		row["sent_at"] = utils.NowRFC3339UTC()
	}
	if err := f.db.Model(&database.CampaignRecipient{}).Create(row).Error; err != nil {
		f.t.Fatalf("insert campaign recipient: %v", err)
	}
	return rid
}

func (f *poolFixture) event(campaignID uuid.UUID, recipientID uuid.UUID, eventName string) {
	f.t.Helper()
	id := uuid.New()
	if err := f.db.Create(&database.CampaignEvent{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now,
		CampaignID: &campaignID, RecipientID: &recipientID,
		EventID: cache.EventIDByName[eventName],
	}).Error; err != nil {
		f.t.Fatalf("insert event: %v", err)
	}
}

func campaignWithPool(ids ...uuid.UUID) *model.Campaign {
	refs := make([]*uuid.UUID, len(ids))
	for i := range ids {
		refs[i] = &ids[i]
	}
	c := &model.Campaign{TemplateIDs: nullable.NewNullableWithValue(refs)}
	c.NormalizeTemplatePool()
	return c
}

// TestValidateTemplatePoolRules checks missing, unusable and mismatched pool
// templates are refused with a validation error
func TestValidateTemplatePoolRules(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})

	if _, err := f.svc.validateTemplatePool(f.ctx, campaignWithPool(a, b)); err != nil {
		t.Fatalf("matching pool refused: %v", err)
	}
	cases := map[string]uuid.UUID{
		"missing":   uuid.New(),
		"unusable":  f.template(templateOpts{unusable: true}),
		"path mode": f.template(templateOpts{mode: data.LureURLModePath}),
		"length":    f.template(templateOpts{length: 16}),
		"training":  f.template(templateOpts{training: true}),
	}
	for name, other := range cases {
		if _, err := f.svc.validateTemplatePool(f.ctx, campaignWithPool(a, other)); !isValidationError(err) {
			t.Errorf("%s: want validation error, got %v", name, err)
		}
	}
	if _, err := f.svc.validateTemplatePool(f.ctx, campaignWithPool(a, b, a)); !isValidationError(err) {
		t.Errorf("duplicate: want validation error, got %v", err)
	}
}

// TestCampaignCompanyRefsIncludePool checks every pool template passes the
// company scope check, not only the first
func TestCampaignCompanyRefsIncludePool(t *testing.T) {
	f := newPoolFixture(t)
	c1, c2 := uuid.New(), uuid.New()
	for _, c := range []uuid.UUID{c1, c2} {
		if err := f.db.Create(&database.Company{ID: c, CreatedAt: &f.now, UpdatedAt: &f.now, Name: c.String(), AssetsKey: c.String()[:8]}).Error; err != nil {
			t.Fatalf("insert company: %v", err)
		}
	}
	own := f.template(templateOpts{companyID: &c1})
	foreign := f.template(templateOpts{companyID: &c2})
	global := f.template(templateOpts{})

	ok := campaignWithPool(own, global)
	if err := ValidateCompanyRefs(f.ctx, f.db, &c1, campaignCompanyRefs(ok)); err != nil {
		t.Fatalf("own and global templates refused: %v", err)
	}
	bad := campaignWithPool(own, global, foreign)
	if err := ValidateCompanyRefs(f.ctx, f.db, &c1, campaignCompanyRefs(bad)); !isValidationError(err) {
		t.Fatalf("foreign template in the pool accepted: %v", err)
	}
}

// TestScheduleSpreadsPoolTemplates checks the basic schedule gives every
// recipient a pool template, balanced and mixed within each department
func TestScheduleSpreadsPoolTemplates(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	departments := []string{}
	for i := 0; i < 6; i++ {
		departments = append(departments, "sales", "it")
	}
	departments = append(departments, "")
	groupID := f.group(departments)
	campaignID := f.campaign([]uuid.UUID{a, b}, false, false)

	if err := f.svc.schedule(f.ctx, f.session, f.loadCampaign(campaignID, groupID)); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	rows := f.recipientsOf(campaignID)
	if len(rows) != len(departments) {
		t.Fatalf("got %d campaign recipients, want %d", len(rows), len(departments))
	}
	totals := map[uuid.UUID]int{}
	perDept := map[string]map[uuid.UUID]bool{}
	for _, r := range rows {
		if r.CampaignTemplateID == nil {
			t.Fatal("recipient scheduled without a template")
		}
		totals[*r.CampaignTemplateID]++
		if perDept[r.Department] == nil {
			perDept[r.Department] = map[uuid.UUID]bool{}
		}
		perDept[r.Department][*r.CampaignTemplateID] = true
	}
	if d := totals[a] - totals[b]; d > 1 || d < -1 {
		t.Fatalf("templates not balanced: %v", totals)
	}
	for _, d := range []string{"sales", "it"} {
		if len(perDept[d]) != 2 {
			t.Fatalf("department %s did not get both templates", d)
		}
	}
}

// TestScheduleSelfManagedPicksLeastUsed checks recipients added to a self
// managed campaign get the least used template and existing ones keep theirs
func TestScheduleSelfManagedPicksLeastUsed(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	groupID := f.group([]string{"x", "x", "y"})
	campaignID := f.campaign([]uuid.UUID{a, b}, true, false)
	campaign := f.loadCampaign(campaignID, groupID)
	campaign.SendStartAt.SetNull()
	campaign.SendEndAt.SetNull()

	if err := f.svc.schedule(f.ctx, f.session, campaign); err != nil {
		t.Fatalf("first schedule: %v", err)
	}
	before := map[uuid.UUID]uuid.UUID{}
	for _, r := range f.recipientsOf(campaignID) {
		before[*r.RecipientID] = *r.CampaignTemplateID
	}
	// five more join the group, totals must end balanced
	f.addToGroup(groupID, []string{"x", "y", "y", "z", "z"})
	if err := f.svc.schedule(f.ctx, f.session, campaign); err != nil {
		t.Fatalf("second schedule: %v", err)
	}
	rows := f.recipientsOf(campaignID)
	if len(rows) != 8 {
		t.Fatalf("got %d recipients, want 8", len(rows))
	}
	totals := map[uuid.UUID]int{}
	for _, r := range rows {
		totals[*r.CampaignTemplateID]++
		if prev, ok := before[*r.RecipientID]; ok && prev != *r.CampaignTemplateID {
			t.Fatal("existing recipient changed template on reschedule")
		}
	}
	if totals[a] != 4 || totals[b] != 4 {
		t.Fatalf("not balanced after additions: %v", totals)
	}
}

// TestGroupedStatsByTemplate checks results group by each recipient's template,
// that rows without one count under the first template, and that an anonymous
// campaign shows group sizes only
func TestGroupedStatsByTemplate(t *testing.T) {
	for _, anonymous := range []bool{false, true} {
		f := newPoolFixture(t)
		a := f.template(templateOpts{nameSuffix: " a"})
		b := f.template(templateOpts{nameSuffix: " b"})
		campaignID := f.campaign([]uuid.UUID{a, b}, false, anonymous)
		onA := []uuid.UUID{}
		for i := 0; i < 4; i++ {
			onA = append(onA, f.seedCampaignRecipient(campaignID, &a, true, ""))
		}
		// a row from before pools counts under the first template
		onA = append(onA, f.seedCampaignRecipient(campaignID, nil, true, ""))
		for i := 0; i < 3; i++ {
			f.seedCampaignRecipient(campaignID, &b, true, "")
		}
		f.event(campaignID, onA[0], data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
		f.event(campaignID, onA[4], data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)

		stats, err := f.svc.GetGroupedResultStats(f.ctx, f.session, &campaignID, repository.GroupedStatByTemplate)
		if err != nil {
			t.Fatalf("grouped stats: %v", err)
		}
		byID := map[string]model.CampaignGroupStat{}
		for _, s := range stats {
			byID[s.GroupID] = s
		}
		ga, gb := byID[a.String()], byID[b.String()]
		if ga.Total != 5 || gb.Total != 3 {
			t.Fatalf("anonymous=%v: totals a=%d b=%d, want 5 and 3", anonymous, ga.Total, gb.Total)
		}
		if ga.Group == "" || ga.Group == gb.Group {
			t.Fatalf("group labels should be template names, got %q and %q", ga.Group, gb.Group)
		}
		if anonymous {
			if ga.Clicked != -1 || gb.Clicked != -1 {
				t.Fatalf("anonymous campaign shows outcomes: a=%d b=%d", ga.Clicked, gb.Clicked)
			}
		} else if ga.Clicked != 2 || gb.Clicked != 0 {
			t.Fatalf("clicked a=%d b=%d, want 2 and 0", ga.Clicked, gb.Clicked)
		}
		has, err := f.svc.HasTemplateGroupData(f.ctx, f.session, &campaignID)
		if err != nil || !has {
			t.Fatalf("template grouping should be available: %v %v", has, err)
		}
	}
}

// TestRemoveTemplateFromPools checks deleting a template moves the campaign to
// the next pool template and leaves recipients with the id they were given
func TestRemoveTemplateFromPools(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	campaignID := f.campaign([]uuid.UUID{a, b}, false, false)
	rid := f.seedCampaignRecipient(campaignID, &a, true, "")

	used, err := f.svc.CampaignRepository.GetByTemplateIDs(f.ctx, []*uuid.UUID{&b})
	if err != nil || len(used) != 1 {
		t.Fatalf("pool template not found in use: %d %v", len(used), err)
	}
	if err := f.svc.CampaignRepository.RemoveTemplateFromPools(f.ctx, &a); err != nil {
		t.Fatalf("remove: %v", err)
	}
	pool, err := f.svc.CampaignRepository.GetTemplatePoolIDs(f.ctx, &campaignID)
	if err != nil || len(pool) != 1 || pool[0] != b {
		t.Fatalf("pool after remove: %v %v", pool, err)
	}
	c, err := f.svc.CampaignRepository.GetByID(f.ctx, &campaignID, &repository.CampaignOption{})
	if err != nil || c.TemplateID.MustGet() != b {
		t.Fatalf("campaign template not moved to the next one: %v", err)
	}
	// a template kept only by a recipient already sent to no longer counts as
	// used, so deleting it does not close a campaign that no longer sends it
	used, err = f.svc.CampaignRepository.GetByTemplateIDs(f.ctx, []*uuid.UUID{&a})
	if err != nil || len(used) != 0 {
		t.Fatalf("template only on a sent recipient counted as used: %d %v", len(used), err)
	}
	for _, r := range f.recipientsOf(campaignID) {
		if *r.RecipientID == rid && *r.CampaignTemplateID != a {
			t.Fatal("recipient template changed")
		}
	}
}

// TestTemplatePoolBackfill checks the migration gives a campaign without pool
// rows a pool of its template, once
func TestTemplatePoolBackfill(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	id := uuid.New()
	if err := f.db.Model(&database.Campaign{}).Create(map[string]any{
		"id": id, "created_at": utils.NowRFC3339UTC(), "updated_at": utils.NowRFC3339UTC(),
		"name": "legacy", "sort_field": "email", "sort_order": "asc",
		"campaign_template_id": a.String(),
	}).Error; err != nil {
		t.Fatalf("insert campaign: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := (database.CampaignTemplatePool{}).Migrate(f.db); err != nil {
			t.Fatalf("backfill: %v", err)
		}
	}
	pool, err := f.svc.CampaignRepository.GetTemplatePoolIDs(f.ctx, &id)
	if err != nil || len(pool) != 1 || pool[0] != a {
		t.Fatalf("backfilled pool: %v %v", pool, err)
	}
}

// TestLureCodeResolvesOnRecipientTemplateDomain checks a lure code matches only
// on the domain of the recipient's own template
func TestLureCodeResolvesOnRecipientTemplateDomain(t *testing.T) {
	f := newPoolFixture(t)
	d1, d2 := uuid.New(), uuid.New()
	for _, d := range []uuid.UUID{d1, d2} {
		if err := f.db.Create(&database.Domain{ID: d, CreatedAt: &f.now, UpdatedAt: &f.now, Name: d.String() + ".test"}).Error; err != nil {
			t.Fatalf("insert domain: %v", err)
		}
	}
	a := f.template(templateOpts{domainID: &d1})
	b := f.template(templateOpts{domainID: &d2})
	campaignID := f.campaign([]uuid.UUID{a, b}, false, false)
	rid := f.seedCampaignRecipient(campaignID, &b, false, "")
	if err := f.db.Model(&database.CampaignRecipient{}).
		Where("recipient_id = ?", rid.String()).
		Update("lure_code", "POOLCODE1234").Error; err != nil {
		t.Fatalf("set code: %v", err)
	}
	repo := f.svc.CampaignRecipientRepository
	if _, err := repo.GetByLureCodeOnDomain(f.ctx, "POOLCODE1234", &database.Domain{ID: d2}); err != nil {
		t.Fatalf("code not found on its template domain: %v", err)
	}
	if _, err := repo.GetByLureCodeOnDomain(f.ctx, "POOLCODE1234", &database.Domain{ID: d1}); err == nil {
		t.Fatal("code matched on the first template's domain")
	}
}

// TestHasTemplateGroupDataSingleTemplate checks a pool of one gives false for
// recipients on that template and for old rows without one, and true once a
// recipient uses another template
func TestHasTemplateGroupDataSingleTemplate(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	campaignID := f.campaign([]uuid.UUID{a}, false, false)
	for i := 0; i < 3; i++ {
		f.seedCampaignRecipient(campaignID, &a, false, "")
	}
	for i := 0; i < 3; i++ {
		f.seedCampaignRecipient(campaignID, nil, false, "")
	}
	has, err := f.svc.HasTemplateGroupData(f.ctx, f.session, &campaignID)
	if err != nil || has {
		t.Fatalf("pool of one: got %v %v, want false", has, err)
	}
	legacyOnly := f.campaign([]uuid.UUID{a}, false, false)
	for i := 0; i < 4; i++ {
		f.seedCampaignRecipient(legacyOnly, nil, true, "")
	}
	has, err = f.svc.HasTemplateGroupData(f.ctx, f.session, &legacyOnly)
	if err != nil || has {
		t.Fatalf("old rows only: got %v %v, want false", has, err)
	}
	// a recipient kept on a template removed from the pool counts
	f.seedCampaignRecipient(campaignID, &b, true, "")
	has, err = f.svc.HasTemplateGroupData(f.ctx, f.session, &campaignID)
	if err != nil || !has {
		t.Fatalf("two templates used: got %v %v, want true", has, err)
	}
}

// selfManagedWithRecipients builds a scheduled self managed campaign on the pool
func (f *poolFixture) selfManagedWithRecipients(pool []uuid.UUID) (uuid.UUID, *model.Campaign, uuid.UUID) {
	f.t.Helper()
	return f.selfManagedWithDepartments(pool, []string{"x", "y"})
}

func (f *poolFixture) selfManagedWithDepartments(pool []uuid.UUID, departments []string) (uuid.UUID, *model.Campaign, uuid.UUID) {
	f.t.Helper()
	groupID := f.group(departments)
	campaignID := f.campaign(pool, true, false)
	campaign := f.loadCampaign(campaignID, groupID)
	campaign.SendStartAt.SetNull()
	campaign.SendEndAt.SetNull()
	if err := f.svc.schedule(f.ctx, f.session, campaign); err != nil {
		f.t.Fatalf("schedule: %v", err)
	}
	return campaignID, campaign, groupID
}

func selfManagedUpdate(campaign *model.Campaign, groupID uuid.UUID) *model.Campaign {
	incoming := &model.Campaign{
		Name:              campaign.Name,
		SortField:         campaign.SortField,
		SortOrder:         campaign.SortOrder,
		RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{&groupID}),
	}
	incoming.SendStartAt.SetNull()
	incoming.SendEndAt.SetNull()
	return incoming
}

// TestUpdateRejectsOversizedPoolBeforeQueries checks a pool over the limit or
// with duplicates is refused as a validation error, not a database error
func TestUpdateRejectsOversizedPoolBeforeQueries(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	campaignID, campaign, groupID := f.selfManagedWithRecipients([]uuid.UUID{a})

	big := make([]*uuid.UUID, 5000)
	for i := range big {
		id := uuid.New()
		big[i] = &id
	}
	incoming := selfManagedUpdate(campaign, groupID)
	incoming.TemplateIDs = nullable.NewNullableWithValue(big)
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); !isValidationError(err) {
		t.Fatalf("oversized pool: want validation error, got %v", err)
	}
	incoming = selfManagedUpdate(campaign, groupID)
	incoming.TemplateIDs = nullable.NewNullableWithValue([]*uuid.UUID{&a, &a})
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); !isValidationError(err) {
		t.Fatalf("duplicate pool: want validation error, got %v", err)
	}
}

// TestUpdateWithOnlyTemplateIDKeepsPool checks an old client sending the stored
// first template keeps the pool, and a different template replaces it
func TestUpdateWithOnlyTemplateIDKeepsPool(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	other := f.template(templateOpts{})
	campaignID, campaign, groupID := f.selfManagedWithRecipients([]uuid.UUID{a, b})

	incoming := selfManagedUpdate(campaign, groupID)
	incoming.TemplateID = nullable.NewNullableWithValue(a)
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); err != nil {
		t.Fatalf("update with the same templateID: %v", err)
	}
	pool, err := f.svc.CampaignRepository.GetTemplatePoolIDs(f.ctx, &campaignID)
	if err != nil || len(pool) != 2 || pool[0] != a || pool[1] != b {
		t.Fatalf("pool dropped by an old client: %v %v", pool, err)
	}

	incoming = selfManagedUpdate(campaign, groupID)
	incoming.TemplateID = nullable.NewNullableWithValue(other)
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); err != nil {
		t.Fatalf("update with another templateID: %v", err)
	}
	pool, err = f.svc.CampaignRepository.GetTemplatePoolIDs(f.ctx, &campaignID)
	if err != nil || len(pool) != 1 || pool[0] != other {
		t.Fatalf("pool after a new templateID: %v %v", pool, err)
	}
	// a and b were removed, so every recipient moves to the new template
	for _, r := range f.recipientsOf(campaignID) {
		if r.CampaignTemplateID == nil || *r.CampaignTemplateID != other {
			t.Fatal("recipient left on a removed template")
		}
	}
}

func (f *poolFixture) templatesByRecipient(campaignID uuid.UUID) map[uuid.UUID]uuid.UUID {
	f.t.Helper()
	out := map[uuid.UUID]uuid.UUID{}
	for _, r := range f.recipientsOf(campaignID) {
		if r.CampaignTemplateID == nil {
			f.t.Fatal("recipient without a template")
		}
		out[*r.RecipientID] = *r.CampaignTemplateID
	}
	return out
}

func (f *poolFixture) updatePool(campaignID uuid.UUID, campaign *model.Campaign, groupID uuid.UUID, pool ...uuid.UUID) error {
	incoming := selfManagedUpdate(campaign, groupID)
	refs := make([]*uuid.UUID, len(pool))
	for i := range pool {
		refs[i] = &pool[i]
	}
	incoming.TemplateIDs = nullable.NewNullableWithValue(refs)
	return f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming)
}

// TestSelfManagedSwitchMovesEveryone checks switching a self managed campaign
// from A to B puts every recipient on B, including one stored without a template
func TestSelfManagedSwitchMovesEveryone(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	campaignID, campaign, groupID := f.selfManagedWithDepartments([]uuid.UUID{a}, []string{"x", "x", "y", "y"})
	rows := f.recipientsOf(campaignID)
	if err := f.db.Model(&database.CampaignRecipient{}).
		Where("recipient_id = ?", rows[0].RecipientID.String()).
		Update("campaign_template_id", nil).Error; err != nil {
		t.Fatalf("clear template: %v", err)
	}
	if err := f.updatePool(campaignID, campaign, groupID, b); err != nil {
		t.Fatalf("update: %v", err)
	}
	for _, tid := range f.templatesByRecipient(campaignID) {
		if tid != b {
			t.Fatal("recipient not moved to the new template")
		}
	}
}

// TestSelfManagedAddingTemplateMovesNone checks adding a template keeps every
// existing recipient and gives only new recipients the added one
func TestSelfManagedAddingTemplateMovesNone(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	c := f.template(templateOpts{})
	campaignID, campaign, groupID := f.selfManagedWithDepartments([]uuid.UUID{a, b}, []string{"x", "x", "y", "y"})
	before := f.templatesByRecipient(campaignID)
	f.addToGroup(groupID, []string{"x", "y"})
	if err := f.updatePool(campaignID, campaign, groupID, a, b, c); err != nil {
		t.Fatalf("update: %v", err)
	}
	after := f.templatesByRecipient(campaignID)
	newOnC := 0
	for rid, tid := range after {
		if prev, ok := before[rid]; ok {
			if prev != tid {
				t.Fatal("existing recipient moved when a template was added")
			}
			continue
		}
		if tid == c {
			newOnC++
		}
	}
	if len(after) != 6 || newOnC != 2 {
		t.Fatalf("want 6 recipients and both new ones on the added template, got %d and %d", len(after), newOnC)
	}
}

// TestSelfManagedRemovingTemplateMovesOnlyItsRecipients checks removing B from
// A, B, C moves only B's recipients, spread so totals stay balanced
func TestSelfManagedRemovingTemplateMovesOnlyItsRecipients(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	c := f.template(templateOpts{})
	campaignID, campaign, groupID := f.selfManagedWithDepartments(
		[]uuid.UUID{a, b, c},
		[]string{"x", "x", "x", "y", "y", "y"},
	)
	before := f.templatesByRecipient(campaignID)
	if err := f.updatePool(campaignID, campaign, groupID, a, c); err != nil {
		t.Fatalf("update: %v", err)
	}
	after := f.templatesByRecipient(campaignID)
	totals := map[uuid.UUID]int{}
	for rid, tid := range after {
		totals[tid]++
		if before[rid] != b && before[rid] != tid {
			t.Fatal("recipient on a kept template was moved")
		}
		if tid == b {
			t.Fatal("recipient left on the removed template")
		}
	}
	if totals[a] != 3 || totals[c] != 3 {
		t.Fatalf("moved recipients not balanced: %v", totals)
	}
}

// TestSelfManagedPoolChangeAllowsNewLureSettings checks a self managed campaign
// with recipients may move to templates with other lure settings, keeping the
// settings it copied
func TestSelfManagedPoolChangeAllowsNewLureSettings(t *testing.T) {
	f := newPoolFixture(t)
	old := f.template(templateOpts{})
	pathA := f.template(templateOpts{mode: data.LureURLModePath})
	pathB := f.template(templateOpts{mode: data.LureURLModePath})
	campaignID, campaign, groupID := f.selfManagedWithRecipients([]uuid.UUID{old})
	if err := f.updatePool(campaignID, campaign, groupID, pathA, pathB); err != nil {
		t.Fatalf("update: %v", err)
	}
	stored, err := f.svc.CampaignRepository.GetByID(f.ctx, &campaignID, &repository.CampaignOption{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if mode, _, _ := stored.LureSettings(); mode != data.LureURLModeQuery {
		t.Fatalf("campaign lure mode changed to %s", mode)
	}
	// pool members must still agree with each other
	if err := f.updatePool(campaignID, campaign, groupID, pathA, old); !isValidationError(err) {
		t.Fatalf("mixed pool: want validation error, got %v", err)
	}
}

// TestAnonymousCampaignPool checks an anonymous scheduled campaign can use a
// pool, an anonymous self managed one can not, and that no per recipient view of
// an anonymous pooled campaign shows which template a person got
func TestAnonymousCampaignPool(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})

	create := campaignWithPool(a, b)
	create.Name = nullable.NewNullableWithValue(*vo.NewString64Must("anon pool"))
	create.SortField = nullable.NewNullableWithValue(*vo.NewCampaignSortFieldMust("email"))
	create.SortOrder = nullable.NewNullableWithValue(*vo.NewCampaignSendingOrderMust("asc"))
	create.IsAnonymous = nullable.NewNullableWithValue(true)
	groupID := f.group([]string{"x"})
	create.RecipientGroupIDs = nullable.NewNullableWithValue([]*uuid.UUID{&groupID})
	if err := create.ValidateAnonymousSelfManagedPool(); err != nil {
		t.Fatalf("anonymous scheduled pool refused: %v", err)
	}
	create.SendStartAt.SetNull()
	create.SendEndAt.SetNull()
	if err := create.ValidateAnonymousSelfManagedPool(); !isValidationError(err) {
		t.Fatalf("anonymous self managed pool on create: want validation error, got %v", err)
	}

	// an anonymous self managed campaign can not move to a pool
	anonID, anon, anonGroup := f.selfManagedWithRecipients([]uuid.UUID{a})
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", anonID.String()).
		Update("is_anonymous", true).Error; err != nil {
		t.Fatalf("set anonymous: %v", err)
	}
	if err := f.updatePool(anonID, anon, anonGroup, a, b); !isValidationError(err) {
		t.Fatalf("anonymous self managed pool on update: want validation error, got %v", err)
	}

	// an anonymous scheduled campaign with two templates
	pooledAnon := f.campaign([]uuid.UUID{a, b}, false, true)
	f.seedCampaignRecipient(pooledAnon, &a, true, "x")
	f.seedCampaignRecipient(pooledAnon, &b, true, "x")
	result, err := f.svc.GetRecipientsByCampaignID(f.ctx, f.session, &pooledAnon, &repository.CampaignRecipientOption{QueryArgs: &vo.QueryArgs{}})
	if err != nil || len(result.Rows) != 2 {
		t.Fatalf("list recipients: %v", err)
	}
	for _, row := range result.Rows {
		if !row.TemplateID.IsNull() || row.Template != nil {
			t.Fatal("anonymous campaign recipient shows its template")
		}
		if row.Sent {
			t.Fatal("anonymous pooled campaign shows the sent indicator")
		}
		crID := row.ID.MustGet()
		if _, err := f.svc.GetCampaignEmailBody(f.ctx, f.session, &crID); !errors.Is(err, errRecipientTemplateHidden) {
			t.Fatalf("email preview: want hidden error, got %v", err)
		}
		if _, err := f.svc.GetLandingPageURLByCampaignRecipientID(f.ctx, f.session, &crID); !errors.Is(err, errRecipientTemplateHidden) {
			t.Fatalf("lure URL: want hidden error, got %v", err)
		}
	}

	// an anonymous campaign with one template keeps the sent indicator and the
	// per recipient views
	singleAnon := f.campaign([]uuid.UUID{a}, false, true)
	f.seedCampaignRecipient(singleAnon, &a, true, "x")
	result, err = f.svc.GetRecipientsByCampaignID(f.ctx, f.session, &singleAnon, &repository.CampaignRecipientOption{QueryArgs: &vo.QueryArgs{}})
	if err != nil || len(result.Rows) != 1 || !result.Rows[0].Sent {
		t.Fatalf("single template anonymous campaign lost the sent indicator: %v", err)
	}
	single := &model.Campaign{
		ID:          nullable.NewNullableWithValue(singleAnon),
		IsAnonymous: nullable.NewNullableWithValue(true),
	}
	if hidden, err := f.svc.recipientTemplateHidden(f.ctx, single); err != nil || hidden {
		t.Fatalf("single template anonymous campaign hides per recipient views: %v %v", hidden, err)
	}

	// a normal pooled campaign shows templates
	poolID, _, _ := f.selfManagedWithRecipients([]uuid.UUID{a, b})
	result, err = f.svc.GetRecipientsByCampaignID(f.ctx, f.session, &poolID, &repository.CampaignRecipientOption{QueryArgs: &vo.QueryArgs{}})
	if err != nil || len(result.Rows) == 0 || result.Rows[0].Template == nil {
		t.Fatalf("normal campaign recipient template missing: %v", err)
	}
}

// TestEmailBodyWithDeletedTemplate checks the email preview for a recipient
// whose template was deleted is a validation error, not a crash
func TestEmailBodyWithDeletedTemplate(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	campaignID := f.campaign([]uuid.UUID{a}, true, false)
	gone := uuid.New()
	f.seedCampaignRecipient(campaignID, &gone, false, "")
	var ids []string
	if err := f.db.Model(&database.CampaignRecipient{}).
		Where("campaign_id = ?", campaignID.String()).
		Pluck("id", &ids).Error; err != nil || len(ids) != 1 {
		t.Fatalf("read campaign recipient: %v", err)
	}
	id := uuid.MustParse(ids[0])
	if _, err := f.svc.GetCampaignEmailBody(f.ctx, f.session, &id); !isValidationError(err) {
		t.Fatalf("want validation error, got %v", err)
	}
}

// TestTemplateLureChangeBlockedInSharedPool checks a template's lure settings
// can not change while it shares a pool in an open campaign, and can once the
// campaign is closed or the template is alone in its pool
func TestTemplateLureChangeBlockedInSharedPool(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	solo := f.template(templateOpts{})
	campaignID := f.campaign([]uuid.UUID{a, b}, false, false)
	f.campaign([]uuid.UUID{solo}, false, false)
	tmplSvc := f.svc.CampaignTemplateService

	change := func(id uuid.UUID) error {
		return tmplSvc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
			LureCodeLength: nullable.NewNullableWithValue(16),
		})
	}
	// the fixture templates miss pages, so an allowed change still fails later
	// validation. only the pool error is checked here.
	poolErr := func(err error) bool {
		return err != nil && strings.Contains(err.Error(), "shares a pool")
	}
	err := change(a)
	if !isValidationError(err) || !poolErr(err) || !strings.Contains(err.Error(), "pool campaign") {
		t.Fatalf("shared pool: want pool validation error naming the campaign, got %v", err)
	}
	if err := change(solo); poolErr(err) {
		t.Fatalf("template alone in its pool refused: %v", err)
	}
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", campaignID.String()).
		Update("closed_at", utils.NowRFC3339UTC()).Error; err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := change(a); poolErr(err) {
		t.Fatalf("closed campaign refused: %v", err)
	}
}

// TestSelfManagedRemovingTemplateKeepsSentRecipients checks removing a template
// moves only recipients not yet sent to. One already sent and one with an event
// keep the removed template, so their delivered links and results stay with it.
func TestSelfManagedRemovingTemplateKeepsSentRecipients(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	campaignID, campaign, groupID := f.selfManagedWithDepartments([]uuid.UUID{a}, []string{"x", "x", "x"})
	rows := f.recipientsOf(campaignID)
	sentID := *rows[0].RecipientID
	clickedID := *rows[1].RecipientID
	if err := f.db.Model(&database.CampaignRecipient{}).
		Where("campaign_id = ? AND recipient_id = ?", campaignID.String(), sentID.String()).
		Update("sent_at", utils.NowRFC3339UTC()).Error; err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	// a recipient stored without a template that has an event keeps the old one too
	if err := f.db.Model(&database.CampaignRecipient{}).
		Where("campaign_id = ? AND recipient_id = ?", campaignID.String(), clickedID.String()).
		Update("campaign_template_id", nil).Error; err != nil {
		t.Fatalf("clear template: %v", err)
	}
	f.event(campaignID, clickedID, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	if err := f.updatePool(campaignID, campaign, groupID, b); err != nil {
		t.Fatalf("update: %v", err)
	}
	for rid, tid := range f.templatesByRecipient(campaignID) {
		switch rid {
		case sentID, clickedID:
			if tid != a {
				t.Fatal("recipient already sent to was moved off its template")
			}
		default:
			if tid != b {
				t.Fatal("unsent recipient not moved to the new template")
			}
		}
	}
}

// TestSelfManagedPoolChangeKeepsTraining checks a self managed campaign with
// recipients can not switch to templates with another training setting
func TestSelfManagedPoolChangeKeepsTraining(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	training := f.template(templateOpts{training: true})
	campaignID, campaign, groupID := f.selfManagedWithRecipients([]uuid.UUID{a})
	if err := f.updatePool(campaignID, campaign, groupID, training); !isValidationError(err) {
		t.Fatalf("training switch: want validation error, got %v", err)
	}
}

// TestScheduledPoolChangeMayChangeTraining checks a scheduled campaign that has
// not started may switch to a template with another training setting, since its
// recipients are made again from the new template
func TestScheduledPoolChangeMayChangeTraining(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	training := f.template(templateOpts{training: true})
	groupID := f.group([]string{"x", "y"})
	campaignID := f.campaign([]uuid.UUID{a}, false, false)
	campaign := f.loadCampaign(campaignID, groupID)
	if err := f.svc.schedule(f.ctx, f.session, campaign); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(f.recipientsOf(campaignID)) == 0 {
		t.Fatal("scheduled campaign has no recipients")
	}
	incoming := &model.Campaign{
		Name:              campaign.Name,
		SortField:         campaign.SortField,
		SortOrder:         campaign.SortOrder,
		SendStartAt:       campaign.SendStartAt,
		SendEndAt:         campaign.SendEndAt,
		RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{&groupID}),
		TemplateIDs:       nullable.NewNullableWithValue([]*uuid.UUID{&training}),
	}
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); err != nil {
		t.Fatalf("switch a scheduled campaign to a training template: %v", err)
	}
	stored, err := f.svc.CampaignRepository.GetByID(f.ctx, &campaignID, &repository.CampaignOption{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if v, err := stored.IsTraining.Get(); err != nil || !v {
		t.Fatal("training setting not taken from the new template")
	}
}

// TestAnonymousSelfManagedSwitchMovesEveryone checks an anonymous self managed
// campaign, which holds one template, moves every recipient on a switch, also
// ones already sent to, so it never looks like it uses several templates
func TestAnonymousSelfManagedSwitchMovesEveryone(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	groupID := f.group([]string{"x", "y", "z"})
	campaignID := f.campaign([]uuid.UUID{a}, true, true)
	campaign := f.loadCampaign(campaignID, groupID)
	campaign.SendStartAt.SetNull()
	campaign.SendEndAt.SetNull()
	if err := f.svc.schedule(f.ctx, f.session, campaign); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := f.db.Model(&database.CampaignRecipient{}).
		Where("campaign_id = ?", campaignID.String()).
		Update("sent_at", utils.NowRFC3339UTC()).Error; err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	if err := f.updatePool(campaignID, campaign, groupID, b); err != nil {
		t.Fatalf("update: %v", err)
	}
	for _, tid := range f.templatesByRecipient(campaignID) {
		if tid != b {
			t.Fatal("recipient of an anonymous self managed campaign left on the old template")
		}
	}
	several, err := f.svc.CampaignRepository.HasTemplateGroupDataByCampaignID(f.ctx, &campaignID)
	if err != nil || several {
		t.Fatalf("campaign reads as using several templates: %v %v", several, err)
	}
}

// TestSelfManagedNewGroupStaysMixedAfterAdd checks recipients added after a
// template joined the pool get a mix within their group, instead of all going
// to the new template because it is least used in the whole campaign
func TestSelfManagedNewGroupStaysMixedAfterAdd(t *testing.T) {
	f := newPoolFixture(t)
	a := f.template(templateOpts{})
	b := f.template(templateOpts{})
	c := f.template(templateOpts{})
	campaignID, campaign, groupID := f.selfManagedWithDepartments(
		[]uuid.UUID{a, b},
		[]string{"x", "x", "x", "x", "x", "x"},
	)
	before := f.templatesByRecipient(campaignID)
	added := f.addToGroup(groupID, []string{"z", "z", "z"})
	if err := f.updatePool(campaignID, campaign, groupID, a, b, c); err != nil {
		t.Fatalf("update: %v", err)
	}
	after := f.templatesByRecipient(campaignID)
	got := map[uuid.UUID]bool{}
	for _, rid := range added {
		tid, ok := after[rid]
		if !ok {
			t.Fatal("added recipient not scheduled")
		}
		got[tid] = true
	}
	if len(got) != 3 {
		t.Fatalf("the new group should get all three templates, got %d", len(got))
	}
	for rid, tid := range before {
		if after[rid] != tid {
			t.Fatal("an existing recipient moved")
		}
	}
}
