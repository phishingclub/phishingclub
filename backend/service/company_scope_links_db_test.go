package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
)

// isScopeError reports whether err is the company scope validation error for field
func isScopeError(err error, field string) bool {
	if !isValidationError(err) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, field) &&
		(strings.Contains(msg, "belongs to") || strings.Contains(msg, "not found"))
}

func (f *companyScopeFixture) companyOf(table string, id uuid.UUID) *uuid.UUID {
	f.t.Helper()
	got, err := repository.GetCompanyIDsByIDs(f.ctx, f.db, table, []uuid.UUID{id})
	if err != nil {
		f.t.Fatalf("load company of %s: %v", table, err)
	}
	owner, ok := got[id]
	if !ok {
		f.t.Fatalf("%s %s not found", table, id)
	}
	return owner
}

func (f *companyScopeFixture) common() Common {
	return Common{Logger: zap.NewNop().Sugar()}
}

func TestNewCompanyRefsKeepsHeldLinks(t *testing.T) {
	held := uuid.New()
	added := uuid.New()
	incoming := []CompanyRef{
		{Table: database.PAGE_TABLE, Field: "a", ID: held},
		{Table: database.PAGE_TABLE, Field: "b", ID: added},
		{Table: database.PROXY_TABLE, Field: "c", ID: held},
	}
	stored := []CompanyRef{{Table: database.PAGE_TABLE, Field: "a", ID: held}}
	got := newCompanyRefs(incoming, stored)
	if len(got) != 2 || got[0].ID != added || got[1].Table != database.PROXY_TABLE {
		t.Fatalf("got %+v, want the added page and the proxy", got)
	}
}

// TestUpdatesKeepStoredCompany checks a companyID in an update request never
// moves a record to another company
func TestUpdatesKeepStoredCompany(t *testing.T) {
	f := newCompanyScopeFixture(t)
	other := nullable.NewNullableWithValue(f.c2)

	t.Run("webhook", func(t *testing.T) {
		id := f.row(database.WEBHOOK_TABLE, &f.c1)
		svc := &Webhook{Common: f.common(), WebhookRepository: &repository.Webhook{DB: f.db}}
		err := svc.Update(f.ctx, f.session, &id, &model.Webhook{
			Name:      nullable.NewNullableWithValue(*vo.NewString127Must("renamed webhook")),
			CompanyID: other,
		})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if got := f.companyOf(database.WEBHOOK_TABLE, id); got == nil || *got != f.c1 {
			t.Fatalf("company moved to %v", got)
		}
	})
	t.Run("script", func(t *testing.T) {
		id := f.row(database.SCRIPT_TABLE, &f.c1)
		svc := &Script{Common: f.common(), ScriptRepository: &repository.Script{DB: f.db}}
		err := svc.Update(f.ctx, f.session, &id, &model.Script{
			Name:      nullable.NewNullableWithValue(*vo.NewString127Must("renamed script")),
			CompanyID: other,
		})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if got := f.companyOf(database.SCRIPT_TABLE, id); got == nil || *got != f.c1 {
			t.Fatalf("company moved to %v", got)
		}
	})
	t.Run("report template", func(t *testing.T) {
		id := uuid.New()
		f.create(&database.ReportTemplate{ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Content: "x", CompanyID: &f.c1})
		svc := &ReportTemplate{Common: f.common(), ReportTemplateRepository: &repository.ReportTemplate{DB: f.db}}
		if err := svc.UpdateByID(f.ctx, f.session, &id, &model.ReportTemplate{CompanyID: other}); err != nil {
			t.Fatalf("update: %v", err)
		}
		row := database.ReportTemplate{}
		f.db.Where("id = ?", id).First(&row)
		if row.CompanyID == nil || *row.CompanyID != f.c1 {
			t.Fatalf("company moved to %v", row.CompanyID)
		}
	})
	t.Run("remote browser", func(t *testing.T) {
		id := uuid.New()
		f.create(&database.RemoteBrowser{ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "rb", Script: "x", Config: "{}", CompanyID: &f.c1})
		svc := &RemoteBrowser{Common: f.common(), RemoteBrowserRepository: &repository.RemoteBrowser{DB: f.db}}
		if err := svc.UpdateByID(f.ctx, f.session, &id, &model.RemoteBrowser{CompanyID: other}); err != nil {
			t.Fatalf("update: %v", err)
		}
		row := database.RemoteBrowser{}
		f.db.Where("id = ?", id).First(&row)
		if row.CompanyID == nil || *row.CompanyID != f.c1 {
			t.Fatalf("company moved to %v", row.CompanyID)
		}
	})
	t.Run("email", func(t *testing.T) {
		id := f.row(database.EMAIL_TABLE, &f.c1)
		f.db.Model(&database.Email{}).Where("id = ?", id).Updates(map[string]any{
			"content": "hi", "mail_from": "a@b.test", "subject": "s", "from": "a@b.test",
		})
		svc := &Email{Common: f.common(), EmailRepository: &repository.Email{DB: f.db}}
		err := svc.UpdateByID(f.ctx, f.session, &id, &model.Email{
			Name:      nullable.NewNullableWithValue(*vo.NewString64Must("renamed email")),
			CompanyID: other,
		})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if got := f.companyOf(database.EMAIL_TABLE, id); got == nil || *got != f.c1 {
			t.Fatalf("company moved to %v", got)
		}
	})
	t.Run("api sender", func(t *testing.T) {
		id := f.row(database.API_SENDER_TABLE, &f.c1)
		svc := &APISender{Common: f.common(), APISenderRepository: &repository.APISender{DB: f.db}}
		// the stored fixture is not a complete sender, so the update may fail
		// validation later. the company must not move either way.
		_ = svc.UpdateByID(f.ctx, f.session, &id, &model.APISender{CompanyID: other})
		if got := f.companyOf(database.API_SENDER_TABLE, id); got == nil || *got != f.c1 {
			t.Fatalf("company moved to %v", got)
		}
	})
}

// TestAPISenderOAuthProviderScope checks the OAuth provider of a sender
func TestAPISenderOAuthProviderScope(t *testing.T) {
	f := newCompanyScopeFixture(t)
	svc := &APISender{Common: f.common(), APISenderRepository: &repository.APISender{DB: f.db}}
	update := func(sender, provider uuid.UUID) error {
		return svc.UpdateByID(f.ctx, f.session, &sender, &model.APISender{
			OAuthProviderID: nullable.NewNullableWithValue(provider),
		})
	}
	c1Sender := f.row(database.API_SENDER_TABLE, &f.c1)
	globalSender := f.row(database.API_SENDER_TABLE, nil)

	if err := update(c1Sender, f.row(database.OAUTH_PROVIDER_TABLE, &f.c2)); !isScopeError(err, "oauthProviderID") {
		t.Fatalf("company sender with other company provider: want scope error, got %v", err)
	}
	if err := update(globalSender, f.row(database.OAUTH_PROVIDER_TABLE, &f.c1)); !isScopeError(err, "oauthProviderID") {
		t.Fatalf("global sender with company provider: want scope error, got %v", err)
	}
	if err := update(c1Sender, f.row(database.OAUTH_PROVIDER_TABLE, nil)); isScopeError(err, "oauthProviderID") {
		t.Fatalf("company sender with global provider refused: %v", err)
	}
	if err := update(c1Sender, f.row(database.OAUTH_PROVIDER_TABLE, &f.c1)); isScopeError(err, "oauthProviderID") {
		t.Fatalf("company sender with own provider refused: %v", err)
	}
	// a link stored before the check existed does not block edits
	legacy := f.row(database.OAUTH_PROVIDER_TABLE, &f.c2)
	res := f.db.Model(&database.APISender{}).Where("id = ?", c1Sender).Update("o_auth_provider_id", legacy)
	if res.Error != nil || res.RowsAffected != 1 {
		t.Fatalf("store legacy link: %v, rows %d", res.Error, res.RowsAffected)
	}
	if err := update(c1Sender, legacy); isScopeError(err, "oauthProviderID") {
		t.Fatalf("stored link refused on edit: %v", err)
	}

	create := &model.APISender{
		CompanyID:       nullable.NewNullableWithValue(f.c1),
		OAuthProviderID: nullable.NewNullableWithValue(f.row(database.OAUTH_PROVIDER_TABLE, &f.c2)),
	}
	// Create validates the sender first, so only check the scope when that passes
	if _, err := svc.Create(f.ctx, f.session, create); err == nil {
		t.Fatalf("company sender created with other company provider")
	}
}

// TestCompanyReportConfigScope checks the delivery group and SMTP configuration
func TestCompanyReportConfigScope(t *testing.T) {
	f := newCompanyScopeFixture(t)
	svc := &CompanyReportConfig{
		Common:                        f.common(),
		CompanyReportConfigRepository: &repository.CompanyReportConfig{DB: f.db},
	}
	upsert := func(companyID *uuid.UUID, groupOwner, smtpOwner *uuid.UUID) error {
		_, err := svc.Upsert(f.ctx, f.session, companyID, &model.CompanyReportConfig{
			RecipientGroupID:    nullable.NewNullableWithValue(f.row(database.RECIPIENT_GROUP_TABLE, groupOwner)),
			SMTPConfigurationID: nullable.NewNullableWithValue(f.row(database.SMTP_CONFIGURATION_TABLE, smtpOwner)),
		})
		return err
	}
	if err := upsert(&f.c1, &f.c2, nil); !isScopeError(err, "recipientGroupID") {
		t.Fatalf("other company group: want scope error, got %v", err)
	}
	if err := upsert(&f.c1, nil, &f.c2); !isScopeError(err, "smtpConfigurationID") {
		t.Fatalf("other company smtp: want scope error, got %v", err)
	}
	if err := upsert(nil, &f.c1, nil); !isScopeError(err, "recipientGroupID") {
		t.Fatalf("global default with company group: want scope error, got %v", err)
	}
	if err := upsert(&f.c1, nil, nil); err != nil {
		t.Fatalf("company with global group and smtp refused: %v", err)
	}
	if err := upsert(&f.c1, &f.c1, &f.c1); err != nil {
		t.Fatalf("company with own group and smtp refused: %v", err)
	}
	if err := upsert(nil, nil, nil); err != nil {
		t.Fatalf("global default with global group and smtp refused: %v", err)
	}
}

// TestDomainCreateProxyScope checks a proxy linked on a directly created domain
func TestDomainCreateProxyScope(t *testing.T) {
	f := newCompanyScopeFixture(t)
	svc := &Domain{Common: f.common(), DomainRepository: &repository.Domain{DB: f.db}}
	create := func(companyID *uuid.UUID, proxyOwner *uuid.UUID) error {
		d := &model.Domain{
			Name:    nullable.NewNullableWithValue(*vo.NewString255Must("scope.test")),
			ProxyID: nullable.NewNullableWithValue(f.row(database.PROXY_TABLE, proxyOwner)),
		}
		if companyID != nil {
			d.CompanyID = nullable.NewNullableWithValue(*companyID)
		}
		_, err := svc.Create(f.ctx, f.session, d)
		return err
	}
	if err := create(&f.c1, &f.c2); !isScopeError(err, "proxyID") {
		t.Fatalf("company domain with other company proxy: want scope error, got %v", err)
	}
	if err := create(nil, &f.c1); !isScopeError(err, "proxyID") {
		t.Fatalf("global domain with company proxy: want scope error, got %v", err)
	}
}

// TestEmailAddAttachmentsScope checks attachments linked to emails. A shared
// email may carry attachments of several companies by design.
func TestEmailAddAttachmentsScope(t *testing.T) {
	f := newCompanyScopeFixture(t)
	emailRepo := &repository.Email{DB: f.db}
	svc := &Email{
		Common:          f.common(),
		EmailRepository: emailRepo,
		AttachmentService: &Attachment{
			Common:               f.common(),
			RootFolder:           filepath.Join(t.TempDir(), "attachments"),
			FileService:          &File{Common: f.common()},
			AttachmentRepository: &repository.Attachment{DB: f.db},
			EmailRepository:      emailRepo,
		},
	}
	add := func(emailOwner, attachmentOwner *uuid.UUID) error {
		emailID := f.row(database.EMAIL_TABLE, emailOwner)
		attachmentID := f.row(database.ATTACHMENT_TABLE, attachmentOwner)
		return svc.AddAttachments(f.ctx, f.session, &emailID, []AttachmentWithInline{{ID: &attachmentID}})
	}
	if err := add(&f.c1, &f.c2); !isScopeError(err, "attachments") {
		t.Fatalf("company email with other company attachment: want scope error, got %v", err)
	}
	if err := add(&f.c1, nil); isScopeError(err, "attachments") {
		t.Fatalf("company email with global attachment refused: %v", err)
	}
	if err := add(&f.c1, &f.c1); isScopeError(err, "attachments") {
		t.Fatalf("company email with own attachment refused: %v", err)
	}
	if err := add(nil, &f.c1); isScopeError(err, "attachments") {
		t.Fatalf("shared email with company attachment refused: %v", err)
	}
}

// TestRecipientGroupImportCompanyMismatch checks an import for another company
// is refused before any recipient is saved
func TestRecipientGroupImportCompanyMismatch(t *testing.T) {
	f := newCompanyScopeFixture(t)
	svc := &RecipientGroup{
		Common:                   f.common(),
		RecipientGroupRepository: &repository.RecipientGroup{DB: f.db},
		RecipientRepository:      &repository.Recipient{DB: f.db},
	}
	recipients := []*model.Recipient{{
		Email: nullable.NewNullableWithValue(*vo.NewEmailMust("someone@scope.test")),
	}}
	c1Group := f.row(database.RECIPIENT_GROUP_TABLE, &f.c1)
	if _, err := svc.Import(f.ctx, f.session, recipients, false, &c1Group, &f.c2); !isValidationError(err) {
		t.Fatalf("import into company group as other company: want validation error, got %v", err)
	}
	globalGroup := f.row(database.RECIPIENT_GROUP_TABLE, nil)
	if _, err := svc.Import(f.ctx, f.session, recipients, false, &globalGroup, &f.c1); !isValidationError(err) {
		t.Fatalf("import company recipients into global group: want validation error, got %v", err)
	}
	var count int64
	f.db.Model(&database.Recipient{}).Count(&count)
	if count != 0 {
		t.Fatalf("refused import saved %d recipients", count)
	}
}
