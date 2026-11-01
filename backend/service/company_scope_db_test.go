package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type companyScopeFixture struct {
	t       *testing.T
	ctx     context.Context
	db      *gorm.DB
	now     time.Time
	session *model.Session
	c1      uuid.UUID
	c2      uuid.UUID
}

func newCompanyScopeFixture(t *testing.T) *companyScopeFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "scope.db") + "?_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// rows reference each other only by id here, the scope check is what is tested
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Company{},
		&database.Identifier{},
		&database.Domain{},
		&database.Page{},
		&database.Proxy{},
		&database.Email{},
		&database.SMTPConfiguration{},
		&database.APISender{},
		&database.Webhook{},
		&database.Script{},
		&database.RecipientGroup{},
		&database.AllowDeny{},
		&database.CampaignTemplate{},
		&database.Campaign{},
		&database.CampaignRecipientGroup{},
		&database.CampaignAllowDeny{},
		&database.CampaignWebhook{},
		&database.CampaignScript{},
		&database.Attachment{},
		&database.EmailAttachment{},
		&database.OAuthProvider{},
		&database.CompanyReportConfig{},
		&database.RemoteBrowser{},
		&database.ReportTemplate{},
		&database.Recipient{},
		&database.RecipientGroupRecipient{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	f := &companyScopeFixture{
		t:       t,
		ctx:     context.Background(),
		db:      db,
		now:     time.Now(),
		session: sessionWithRole(data.RoleSuperAdministrator),
	}
	f.c1 = f.company("c1")
	f.c2 = f.company("c2")
	return f
}

func (f *companyScopeFixture) company(key string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.create(&database.Company{ID: id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "company " + key, AssetsKey: key})
	return id
}

func (f *companyScopeFixture) create(row any) {
	f.t.Helper()
	if err := f.db.Create(row).Error; err != nil {
		f.t.Fatalf("insert %T: %v", row, err)
	}
}

// row inserts one record into table owned by companyID, nil is global
func (f *companyScopeFixture) row(table string, companyID *uuid.UUID) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	name := table + " " + id.String()
	ts := &f.now
	switch table {
	case database.DOMAIN_TABLE:
		f.create(&database.Domain{ID: id, CreatedAt: ts, UpdatedAt: ts, Name: id.String() + ".test", CompanyID: companyID})
	case database.PAGE_TABLE:
		f.create(&database.Page{ID: &id, CreatedAt: ts, UpdatedAt: ts, Name: name, CompanyID: companyID})
	case database.PROXY_TABLE:
		f.create(&database.Proxy{ID: &id, CreatedAt: ts, UpdatedAt: ts, Name: name, CompanyID: companyID})
	case database.EMAIL_TABLE:
		f.create(&database.Email{ID: &id, CreatedAt: ts, UpdatedAt: ts, Name: name[:40], Content: "hi", MailFrom: "a@b.test", Subject: "s", From: "a@b.test", CompanyID: companyID})
	case database.SMTP_CONFIGURATION_TABLE:
		f.create(&database.SMTPConfiguration{ID: id, CreatedAt: ts, UpdatedAt: ts, Name: name, CompanyID: companyID})
	case database.API_SENDER_TABLE:
		f.create(&database.APISender{ID: &id, CreatedAt: ts, UpdatedAt: ts, Name: name[:40], RequestMethod: "POST", RequestURL: "https://send.test", CompanyID: companyID})
	case database.WEBHOOK_TABLE:
		f.create(&database.Webhook{ID: &id, CreatedAt: ts, UpdatedAt: ts, Name: name, URL: "https://hook.test", CompanyID: companyID})
	case database.SCRIPT_TABLE:
		f.create(&database.Script{ID: &id, CreatedAt: ts, UpdatedAt: ts, Name: name, Script: "1", CompanyID: companyID})
	case database.RECIPIENT_GROUP_TABLE:
		f.create(&database.RecipientGroup{ID: &id, CreatedAt: ts, UpdatedAt: ts, Name: name, CompanyID: companyID})
	case database.ALLOW_DENY_TABLE:
		f.create(&database.AllowDeny{ID: &id, CreatedAt: ts, UpdatedAt: ts, Name: name, Cidrs: "10.0.0.0/8", CompanyID: companyID})
	case database.CAMPAIGN_TEMPLATE_TABLE:
		urlIdentifier, stateIdentifier := uuid.New(), uuid.New()
		f.create(&database.CampaignTemplate{
			ID: &id, CreatedAt: ts, UpdatedAt: ts, Name: name, CompanyID: companyID,
			URLIdentifierID: &urlIdentifier, StateIdentifierID: &stateIdentifier,
		})
	case database.ATTACHMENT_TABLE:
		f.create(&database.Attachment{ID: &id, CreatedAt: ts, UpdatedAt: ts, Name: name, Filename: id.String() + ".txt", CompanyID: companyID})
	case database.OAUTH_PROVIDER_TABLE:
		f.create(&database.OAuthProvider{ID: id, CreatedAt: ts, UpdatedAt: ts, Name: name, CompanyID: companyID})
	default:
		f.t.Fatalf("no fixture for table %s", table)
	}
	return id
}

// scopeTables are all tables a campaign or campaign template references
var scopeTables = []string{
	database.DOMAIN_TABLE,
	database.PAGE_TABLE,
	database.PROXY_TABLE,
	database.EMAIL_TABLE,
	database.SMTP_CONFIGURATION_TABLE,
	database.API_SENDER_TABLE,
	database.WEBHOOK_TABLE,
	database.SCRIPT_TABLE,
	database.RECIPIENT_GROUP_TABLE,
	database.ALLOW_DENY_TABLE,
	database.CAMPAIGN_TEMPLATE_TABLE,
	database.ATTACHMENT_TABLE,
	database.OAUTH_PROVIDER_TABLE,
}

func (f *companyScopeFixture) check(companyID *uuid.UUID, table string, id uuid.UUID) error {
	return ValidateCompanyRefs(f.ctx, f.db, companyID, []CompanyRef{{Table: table, Field: "ref", ID: id}})
}

// TestValidateCompanyRefsRule checks the rule for every referenced table:
// company context takes own and global records, global context takes only
// global records
func TestValidateCompanyRefsRule(t *testing.T) {
	f := newCompanyScopeFixture(t)
	for _, table := range scopeTables {
		global := f.row(table, nil)
		own := f.row(table, &f.c1)
		other := f.row(table, &f.c2)

		if err := f.check(&f.c1, table, global); err != nil {
			t.Errorf("%s: global record from company refused: %v", table, err)
		}
		if err := f.check(&f.c1, table, own); err != nil {
			t.Errorf("%s: own company record refused: %v", table, err)
		}
		if err := f.check(&f.c1, table, other); !isValidationError(err) {
			t.Errorf("%s: other company record accepted, err %v", table, err)
		}
		if err := f.check(nil, table, global); err != nil {
			t.Errorf("%s: global record from global refused: %v", table, err)
		}
		if err := f.check(nil, table, own); !isValidationError(err) {
			t.Errorf("%s: company record from global accepted, err %v", table, err)
		}
		if err := f.check(&f.c1, table, uuid.New()); !isValidationError(err) {
			t.Errorf("%s: missing record accepted, err %v", table, err)
		}
	}
}

// TestValidateCompanyRefsNamesField checks the error names the failing field
func TestValidateCompanyRefsNamesField(t *testing.T) {
	f := newCompanyScopeFixture(t)
	ok := f.row(database.PAGE_TABLE, nil)
	bad := f.row(database.PAGE_TABLE, &f.c2)
	err := ValidateCompanyRefs(f.ctx, f.db, &f.c1, []CompanyRef{
		{Table: database.PAGE_TABLE, Field: "denyPageID", ID: ok},
		{Table: database.PAGE_TABLE, Field: "evasionPageID", ID: bad},
	})
	if !isValidationError(err) || !strings.Contains(err.Error(), "evasionPageID") {
		t.Fatalf("want validation error on evasionPageID, got %v", err)
	}
}

// campaignWithRefs builds a campaign that sets every reference, each owned by
// owner (nil is global)
func (f *companyScopeFixture) campaignWithRefs(companyID, owner *uuid.UUID) *model.Campaign {
	group := f.row(database.RECIPIENT_GROUP_TABLE, owner)
	list := f.row(database.ALLOW_DENY_TABLE, owner)
	c := &model.Campaign{
		Name:              nullable.NewNullableWithValue(*vo.NewString64Must("scope campaign " + uuid.NewString()[:8])),
		SortField:         nullable.NewNullableWithValue(*vo.NewCampaignSortFieldMust("email")),
		SortOrder:         nullable.NewNullableWithValue(*vo.NewCampaignSendingOrderMust("asc")),
		TemplateID:        nullable.NewNullableWithValue(f.row(database.CAMPAIGN_TEMPLATE_TABLE, owner)),
		RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{&group}),
		AllowDenyIDs:      nullable.NewNullableWithValue([]*uuid.UUID{&list}),
		DenyPageID:        nullable.NewNullableWithValue(f.row(database.PAGE_TABLE, owner)),
		EvasionPageID:     nullable.NewNullableWithValue(f.row(database.PAGE_TABLE, owner)),
		Webhooks: nullable.NewNullableWithValue([]*model.CampaignWebhook{{
			WebhookID:          nullable.NewNullableWithValue(f.row(database.WEBHOOK_TABLE, owner)),
			WebhookIncludeData: nullable.NewNullableWithValue(model.WebhookDataLevelFull),
			WebhookEvents:      nullable.NewNullableWithValue(0),
		}}),
		Scripts: nullable.NewNullableWithValue([]*model.CampaignScript{{
			ScriptID:          nullable.NewNullableWithValue(f.row(database.SCRIPT_TABLE, owner)),
			ScriptIncludeData: nullable.NewNullableWithValue(model.WebhookDataLevelFull),
			ScriptEvents:      nullable.NewNullableWithValue(0),
		}}),
	}
	if companyID != nil {
		c.CompanyID = nullable.NewNullableWithValue(*companyID)
	}
	return c
}

// templateWithRefs builds a template that sets every reference, each owned by owner
func (f *companyScopeFixture) templateWithRefs(owner *uuid.UUID) *model.CampaignTemplate {
	return &model.CampaignTemplate{
		DomainID:             nullable.NewNullableWithValue(f.row(database.DOMAIN_TABLE, owner)),
		EmailID:              nullable.NewNullableWithValue(f.row(database.EMAIL_TABLE, owner)),
		SMTPConfigurationID:  nullable.NewNullableWithValue(f.row(database.SMTP_CONFIGURATION_TABLE, owner)),
		APISenderID:          nullable.NewNullableWithValue(f.row(database.API_SENDER_TABLE, owner)),
		BeforeLandingPageID:  nullable.NewNullableWithValue(f.row(database.PAGE_TABLE, owner)),
		LandingPageID:        nullable.NewNullableWithValue(f.row(database.PAGE_TABLE, owner)),
		AfterLandingPageID:   nullable.NewNullableWithValue(f.row(database.PAGE_TABLE, owner)),
		BeforeLandingProxyID: nullable.NewNullableWithValue(f.row(database.PROXY_TABLE, owner)),
		LandingProxyID:       nullable.NewNullableWithValue(f.row(database.PROXY_TABLE, owner)),
		AfterLandingProxyID:  nullable.NewNullableWithValue(f.row(database.PROXY_TABLE, owner)),
	}
}

// TestCampaignCompanyRefsCollectsEveryReference checks the campaign and
// template reference lists cover every field, so a field left out of the list
// shows up here as an accepted foreign record
func TestCampaignCompanyRefsCollectsEveryReference(t *testing.T) {
	f := newCompanyScopeFixture(t)

	campaignRefs := campaignCompanyRefs(f.campaignWithRefs(&f.c1, &f.c2))
	if len(campaignRefs) != 7 {
		t.Fatalf("campaign refs: got %d, want 7", len(campaignRefs))
	}
	for _, ref := range campaignRefs {
		if err := ValidateCompanyRefs(f.ctx, f.db, &f.c1, []CompanyRef{ref}); !isValidationError(err) {
			t.Errorf("campaign %s from another company accepted, err %v", ref.Field, err)
		}
	}
	templateRefs := campaignTemplateCompanyRefs(f.templateWithRefs(&f.c2))
	if len(templateRefs) != 10 {
		t.Fatalf("template refs: got %d, want 10", len(templateRefs))
	}
	for _, ref := range templateRefs {
		if err := ValidateCompanyRefs(f.ctx, f.db, &f.c1, []CompanyRef{ref}); !isValidationError(err) {
			t.Errorf("template %s from another company accepted, err %v", ref.Field, err)
		}
	}
	// global records are fine for a company and for the global context
	for _, ctxCompany := range []*uuid.UUID{&f.c1, nil} {
		if err := ValidateCompanyRefs(f.ctx, f.db, ctxCompany, campaignCompanyRefs(f.campaignWithRefs(ctxCompany, nil))); err != nil {
			t.Errorf("campaign with global refs refused: %v", err)
		}
		if err := ValidateCompanyRefs(f.ctx, f.db, ctxCompany, campaignTemplateCompanyRefs(f.templateWithRefs(nil))); err != nil {
			t.Errorf("template with global refs refused: %v", err)
		}
	}
}

func (f *companyScopeFixture) campaignService() *Campaign {
	common := Common{Logger: zap.NewNop().Sugar()}
	return &Campaign{
		Common:              common,
		CampaignRepository:  &repository.Campaign{DB: f.db},
		AllowDenyRepository: &repository.AllowDeny{DB: f.db},
	}
}

func (f *companyScopeFixture) templateService() *CampaignTemplate {
	common := Common{Logger: zap.NewNop().Sugar()}
	return &CampaignTemplate{
		Common:                     common,
		CampaignTemplateRepository: &repository.CampaignTemplate{DB: f.db},
		CampaignRepository:         &repository.Campaign{DB: f.db},
		IdentifierRepository:       &repository.Identifier{DB: f.db},
	}
}

// TestCampaignCreateRejectsForeignRefs checks the service refuses a campaign
// that references another company, or a company record from the global context
func TestCampaignCreateRejectsForeignRefs(t *testing.T) {
	f := newCompanyScopeFixture(t)
	svc := f.campaignService()

	if _, err := svc.Create(f.ctx, f.session, f.campaignWithRefs(&f.c1, &f.c2)); !isValidationError(err) {
		t.Fatalf("company campaign with other company refs: want validation error, got %v", err)
	}
	if _, err := svc.Create(f.ctx, f.session, f.campaignWithRefs(nil, &f.c1)); !isValidationError(err) {
		t.Fatalf("global campaign with company refs: want validation error, got %v", err)
	}
	var count int64
	f.db.Table(database.CAMPAIGN_TABLE).Count(&count)
	if count != 0 {
		t.Fatalf("refused campaign was stored, %d rows", count)
	}
}

// TestCampaignUpdateUsesStoredCompany checks an update cannot reference
// another company, and that a companyID in the request does not change which
// company is checked
func TestCampaignUpdateUsesStoredCompany(t *testing.T) {
	f := newCompanyScopeFixture(t)
	svc := f.campaignService()
	id := uuid.New()
	templateID := f.row(database.CAMPAIGN_TEMPLATE_TABLE, &f.c1)
	f.create(&database.Campaign{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "stored", CompanyID: &f.c1,
		CampaignTemplateID: &templateID, SortField: "email", SortOrder: "asc",
	})
	// the request claims company c2 to try to pass c2 records
	incoming := f.campaignWithRefs(&f.c2, &f.c2)
	if err := svc.UpdateByID(f.ctx, f.session, &id, incoming); !isValidationError(err) {
		t.Fatalf("update with other company refs: want validation error, got %v", err)
	}
}

// TestCampaignTemplateCreateAndUpdateRejectForeignRefs checks the template
// service refuses references across companies on create and update
func TestCampaignTemplateCreateAndUpdateRejectForeignRefs(t *testing.T) {
	f := newCompanyScopeFixture(t)
	svc := f.templateService()
	for _, name := range []string{"id", "p"} {
		identifier := uuid.New()
		f.create(&database.Identifier{ID: &identifier, Name: name})
	}

	create := func(companyID, owner *uuid.UUID) error {
		tmpl := f.templateWithRefs(owner)
		// a stage takes a page or a proxy, keep the pages only
		tmpl.BeforeLandingProxyID.SetNull()
		tmpl.LandingProxyID.SetNull()
		tmpl.AfterLandingProxyID.SetNull()
		tmpl.SMTPConfigurationID.SetNull()
		tmpl.Name = nullable.NewNullableWithValue(*vo.NewString64Must("tmpl " + uuid.NewString()[:8]))
		if companyID != nil {
			tmpl.CompanyID = nullable.NewNullableWithValue(*companyID)
		}
		_, err := svc.Create(f.ctx, f.session, tmpl)
		return err
	}
	if err := create(&f.c1, &f.c2); !isValidationError(err) {
		t.Fatalf("company template with other company refs: want validation error, got %v", err)
	}
	if err := create(nil, &f.c1); !isValidationError(err) {
		t.Fatalf("global template with company refs: want validation error, got %v", err)
	}
	if err := create(&f.c1, nil); err != nil {
		t.Fatalf("company template with global refs refused: %v", err)
	}
	if err := create(&f.c1, &f.c1); err != nil {
		t.Fatalf("company template with own refs refused: %v", err)
	}
	if err := create(nil, nil); err != nil {
		t.Fatalf("global template with global refs refused: %v", err)
	}

	// update checks against the stored company, not the request company
	stored := f.row(database.CAMPAIGN_TEMPLATE_TABLE, &f.c1)
	landingPage := f.row(database.PAGE_TABLE, &f.c1)
	if err := f.db.Model(&database.CampaignTemplate{}).
		Where("id = ?", stored).
		Update("landing_page_id", landingPage).Error; err != nil {
		t.Fatalf("set landing page: %v", err)
	}
	update := &model.CampaignTemplate{
		CompanyID: nullable.NewNullableWithValue(f.c2),
		EmailID:   nullable.NewNullableWithValue(f.row(database.EMAIL_TABLE, &f.c2)),
	}
	if err := svc.UpdateByID(f.ctx, f.session, &stored, update); !isValidationError(err) {
		t.Fatalf("template update with other company email: want validation error, got %v", err)
	}
	update = &model.CampaignTemplate{
		EmailID: nullable.NewNullableWithValue(f.row(database.EMAIL_TABLE, nil)),
	}
	if err := svc.UpdateByID(f.ctx, f.session, &stored, update); err != nil {
		t.Fatalf("template update with global email refused: %v", err)
	}

	// a link stored before the check existed does not block edits that send it again
	legacyEmail := f.row(database.EMAIL_TABLE, &f.c2)
	if err := f.db.Model(&database.CampaignTemplate{}).
		Where("id = ?", stored).
		Update("email_id", legacyEmail).Error; err != nil {
		t.Fatalf("store legacy email: %v", err)
	}
	update = &model.CampaignTemplate{
		Name:    nullable.NewNullableWithValue(*vo.NewString64Must("renamed template")),
		EmailID: nullable.NewNullableWithValue(legacyEmail),
	}
	if err := svc.UpdateByID(f.ctx, f.session, &stored, update); err != nil {
		t.Fatalf("template edit with stored legacy email refused: %v", err)
	}
}
