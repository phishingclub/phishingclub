package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// TestProgramCloseAnswersBeforeTheWork checks a close request marks the
// program as closing and stops all sending, and the background close
// finishes it
func TestProgramCloseAnswersBeforeTheWork(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	alice := p.recipient("alice@example.com")
	p.enroll(programID, alice, 0)
	held := p.sendAt(campaignID, alice, time.Now().Add(time.Hour), false)

	if err := p.svc.Close(p.ctx, session, &programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	program := p.loadProgram(programID)
	if program.ClosingAt == nil && !program.IsClosed() {
		t.Fatalf("the request must mark the close")
	}
	if !program.IsPaused() && !program.IsClosed() {
		t.Fatalf("the request must stop all sending")
	}
	mustSettle(t, p.svc)
	program = p.loadProgram(programID)
	if !program.IsClosed() || program.ClosingAt != nil {
		t.Fatalf("the background close must finish, got %s", program.Status)
	}
	if p.campaignRecipient(held).CancelledAt == nil {
		t.Fatalf("the send not yet sent must be cancelled")
	}
	if p.svc.isStoppingNow(programID) {
		t.Fatalf("the close must release its claim")
	}
}

// TestProgramResumeFailureIsShown checks a resume that fails in the
// background leaves the program paused and shows the failure, and that a new
// resume clears it
func TestProgramResumeFailureIsShown(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	if err := p.svc.Pause(p.ctx, session, &programID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := p.db.Exec(`CREATE TRIGGER fail_resume BEFORE UPDATE OF paused_at ON programs
		WHEN NEW.paused_at IS NULL BEGIN SELECT RAISE(ABORT, 'resume refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := p.svc.Resume(p.ctx, session, &programID); err != nil {
		t.Fatalf("the request must answer before the work: %v", err)
	}
	mustSettle(t, p.svc)
	program := p.loadProgram(programID)
	if !program.IsPaused() {
		t.Fatalf("a failed resume must leave the program paused")
	}
	p.svc.applyRunState(program)
	if program.Resuming {
		t.Fatalf("the resume has ended")
	}
	if program.ActionFailure == nil || program.ActionFailure.Action != programActionResume || program.ActionFailure.Reason == "" {
		t.Fatalf("the failure must be shown, got %+v", program.ActionFailure)
	}
	if err := p.db.Exec(`DROP TRIGGER fail_resume`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if err := p.svc.resumeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("second resume: %v", err)
	}
	program = p.loadProgram(programID)
	if program.IsPaused() {
		t.Fatalf("the second resume must clear the pause")
	}
	p.svc.applyRunState(program)
	if program.ActionFailure != nil {
		t.Fatalf("a new resume must clear the failure")
	}
}

// TestProgramResumeRefusedWhileResuming checks a second resume of a program
// that is being resumed is refused, and a pause stops the resume without a
// failure
func TestProgramResumeRefusedWhileResuming(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	if err := p.svc.Pause(p.ctx, session, &programID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	token := p.svc.startResume(programID)
	var v errs.ValidationError
	if err := p.svc.Resume(p.ctx, session, &programID); !errors.As(err, &v) || v.Err != errProgramResuming {
		t.Fatalf("expected a refused resume, got %v", err)
	}
	program := p.loadProgram(programID)
	p.svc.applyRunState(program)
	if !program.Resuming {
		t.Fatalf("the read must show the resume")
	}
	p.svc.endResume(programID, token)
	// a resume stopped by a pause is not a failure
	p.svc.finishAction(programID, programActionResume, session, "Program.Resume",
		errs.NewValidationError(errProgramPausedOnResume), false)
	if p.svc.lastActionErr(programID) != nil {
		t.Fatalf("a resume stopped by a pause is not a failure")
	}
}

// TestProgramAnonymizeDataInBackground checks Anonymize Data answers before
// the work, holds the program while it runs, shows a failure and is cleared
// by a new run
func TestProgramAnonymizeDataInBackground(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	// a run that holds the program refuses a second one
	if !f.svc.claimAnonymizing(f.programID) {
		t.Fatal("claim")
	}
	var v errs.ValidationError
	if err := f.svc.AnonymizeDataByID(f.ctx, session, &f.programID); !errors.As(err, &v) || v.Err != errProgramAnonymizing {
		t.Fatalf("expected a refused run, got %v", err)
	}
	f.svc.releaseAnonymizing(f.programID)

	if err := f.db.Exec(`CREATE TRIGGER fail_scrub BEFORE UPDATE ON campaign_events
		BEGIN SELECT RAISE(ABORT, 'scrub refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := f.svc.AnonymizeDataByID(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("the request must answer before the work: %v", err)
	}
	mustSettle(t, f.svc)
	if f.svc.isAnonymizingNow(f.programID) {
		t.Fatal("a failed run must release the program")
	}
	program := f.loadProgram(f.programID)
	f.svc.applyRunState(program)
	if program.ActionFailure == nil || program.ActionFailure.Action != programActionAnonymizeData {
		t.Fatalf("the failure must be shown, got %+v", program.ActionFailure)
	}
	if program.DataAnonymizedAt != nil {
		t.Fatal("a failed run must not mark the data as anonymized")
	}
	// the run stays marked so the background pass finishes it, also after a
	// restart
	if program.DataAnonymizingAt == nil {
		t.Fatal("a failed run must stay marked")
	}
	ids, err := f.svc.ProgramRepository.GetIDsToDataAnonymize(f.ctx, time.Now(), nil)
	if err != nil || len(ids) != 1 || *ids[0] != f.programID {
		t.Fatalf("the background pass must find the run: %v %v", ids, err)
	}
	if err := f.db.Exec(`DROP TRIGGER fail_scrub`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if err := f.svc.anonymizeDataWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("second run: %v", err)
	}
	program = f.loadProgram(f.programID)
	f.svc.applyRunState(program)
	if program.DataAnonymizedAt == nil || program.ActionFailure != nil || program.DataAnonymizingAt != nil {
		t.Fatalf("the second run must finish, clear the failure and the mark")
	}
}

// deleteFixture is a company and recipient group service on the program test
// database
type deleteFixture struct {
	*programTest
	groups    *RecipientGroup
	companies *Company
}

func newDeleteFixture(t *testing.T) *deleteFixture {
	t.Helper()
	p := newProgramLifecycleTest(t)
	if err := p.db.AutoMigrate(
		&database.Company{},
		&database.Domain{},
		&database.Page{},
		&database.Email{},
		&database.Attachment{},
		&database.EmailAttachment{},
		&database.APISender{},
		&database.APISenderHeader{},
		&database.Webhook{},
		&database.Script{},
		&database.AllowDeny{},
		&database.Asset{},
		&database.CompanyReportConfig{},
		&database.ReportSendLog{},
		&database.CompanyScimConfig{},
		&database.Role{},
		&database.User{},
		&database.SMTPConfiguration{},
		&database.Proxy{},
		&database.OAuthProvider{},
		&database.RemoteBrowser{},
		&database.ReportTemplate{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := p.svc.Logger
	common := Common{Logger: log}
	groupRepo := &repository.RecipientGroup{DB: p.db}
	recipientRepo := &repository.Recipient{DB: p.db}
	campaignRepo := &repository.Campaign{DB: p.db}
	programRepo := p.svc.ProgramRepository
	recipients := &Recipient{
		Common:                      common,
		RecipientRepository:         recipientRepo,
		RecipientGroupRepository:    groupRepo,
		CampaignRepository:          campaignRepo,
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: p.db},
		ProgramRepository:           programRepo,
		ProgramService:              p.svc,
	}
	groups := &RecipientGroup{
		Common:                      common,
		CampaignRepository:          campaignRepo,
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: p.db},
		RecipientGroupRepository:    groupRepo,
		RecipientRepository:         recipientRepo,
		RecipientService:            recipients,
		ProgramRepository:           programRepo,
		DB:                          p.db,
	}
	templates := &CampaignTemplate{
		Common:                     common,
		CampaignTemplateRepository: &repository.CampaignTemplate{DB: p.db},
		CampaignRepository:         campaignRepo,
		IdentifierRepository:       &repository.Identifier{DB: p.db},
		ProgramRepository:          programRepo,
		ProgramService:             p.svc,
	}
	p.svc.CampaignTemplateService = templates
	p.svc.CampaignService.CampaignTemplateService = templates
	companyRepo := &repository.Company{DB: p.db}
	p.svc.CompanyRepository = companyRepo
	groups.CompanyRepository = companyRepo
	domainRepo := &repository.Domain{DB: p.db}
	companies := &Company{
		Common: common,
		DomainService: &Domain{
			Common: common, DomainRepository: domainRepo, CompanyRepository: companyRepo,
			CampaignTemplateService: templates,
		},
		PageService: &Page{
			Common: common, PageRepository: &repository.Page{DB: p.db}, CampaignRepository: campaignRepo,
			CampaignTemplateService: templates, DomainRepository: domainRepo, ProgramRepository: programRepo,
		},
		EmailService: &Email{
			Common: common, EmailRepository: &repository.Email{DB: p.db}, RecipientService: recipients,
			CampaignTemplateService: templates,
		},
		APISenderService: &APISender{
			Common: common, CampaignTemplateService: templates, APISenderRepository: &repository.APISender{DB: p.db},
		},
		RecipientService:      recipients,
		RecipientGroupService: groups,
		CampaignService:       p.svc.CampaignService,
		CampaignTemplate:      templates,
		ProgramService:        p.svc,
		AllowDenyService: &AllowDeny{
			Common: common, AllowDenyRepository: &repository.AllowDeny{DB: p.db}, CampaignRepository: campaignRepo,
			ProgramRepository: programRepo, AssetRepository: &repository.Asset{DB: p.db},
		},
		WebhookService: &Webhook{
			Common: common, CampaignRepository: campaignRepo, WebhookRepository: &repository.Webhook{DB: p.db},
			ProgramRepository: programRepo,
		},
		ScriptService: &Script{
			Common: common, CampaignRepository: campaignRepo, ScriptRepository: &repository.Script{DB: p.db},
		},
		AssetService: &Asset{
			Common: common, RootFolder: t.TempDir(), AssetRepository: &repository.Asset{DB: p.db},
			DomainRepository: domainRepo, CompanyRepository: companyRepo,
			AllowDenyRepository: &repository.AllowDeny{DB: p.db},
		},
		CompanyRepository:             companyRepo,
		CompanyReportConfigRepository: &repository.CompanyReportConfig{DB: p.db},
		ReportSendLogRepository:       &repository.ReportSendLog{DB: p.db},
		CompanyScimConfigRepository:   &repository.CompanyScimConfig{DB: p.db},
	}
	companies.AttachmentService = &Attachment{
		Common: common, RootFolder: t.TempDir(), FileService: &File{Common: common},
		AttachmentRepository: &repository.Attachment{DB: p.db}, EmailRepository: &repository.Email{DB: p.db},
		CompanyRepository: companyRepo,
	}
	// every service that writes rows of a company refuses a deleting one
	companies.PageService.CompanyRepository = companyRepo
	companies.EmailService.CompanyRepository = companyRepo
	companies.APISenderService.CompanyRepository = companyRepo
	companies.AllowDenyService.CompanyRepository = companyRepo
	companies.WebhookService.CompanyRepository = companyRepo
	companies.ScriptService.CompanyRepository = companyRepo
	templates.CompanyRepository = companyRepo
	recipients.CompanyRepository = companyRepo
	p.svc.CampaignService.CompanyRepository = companyRepo
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = groups.deletes.wait(ctx)
		_ = companies.deletes.wait(ctx)
	})
	return &deleteFixture{programTest: p, groups: groups, companies: companies}
}

func (f *deleteFixture) company(name string) uuid.UUID {
	f.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	f.create(&database.Company{ID: id, CreatedAt: &now, UpdatedAt: &now, Name: name, AssetsKey: id.String()})
	return id
}

func (f *deleteFixture) group(companyID *uuid.UUID, members ...uuid.UUID) uuid.UUID {
	f.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	f.create(&database.RecipientGroup{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "group " + id.String(), CompanyID: companyID})
	for _, m := range members {
		m := m
		f.create(&database.RecipientGroupRecipient{RecipientGroupID: &id, RecipientID: &m})
	}
	return id
}

func (f *deleteFixture) companyRecipient(companyID uuid.UUID, email string) uuid.UUID {
	f.t.Helper()
	id := f.recipient(email)
	if err := f.db.Model(&database.Recipient{}).Where("id = ?", id).Update("company_id", companyID).Error; err != nil {
		f.t.Fatalf("set company: %v", err)
	}
	return id
}

func (f *deleteFixture) exists(row any, id uuid.UUID) bool {
	f.t.Helper()
	var n int64
	if err := f.db.Model(row).Where("id = ?", id).Count(&n).Error; err != nil {
		f.t.Fatalf("count: %v", err)
	}
	return n > 0
}

func (f *deleteFixture) waitDeletes() {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		if err := f.groups.deletes.wait(ctx); err != nil {
			f.t.Fatalf("group deletes: %v", err)
		}
		if err := f.companies.deletes.wait(ctx); err != nil {
			f.t.Fatalf("company deletes: %v", err)
		}
		mustSettle(f.t, f.svc)
	}
}

// TestRecipientGroupDeleteInBackground checks a group delete marks the group,
// refuses changes and new use while it is marked, cancels the pending sends
// of its members and removes the group in the background
func TestRecipientGroupDeleteInBackground(t *testing.T) {
	f := newDeleteFixture(t)
	session := authorizedSession(t)
	alice := f.recipient("alice@example.com")
	groupID := f.group(nil, alice)
	now := time.Now().UTC()
	campaignID := uuid.New()
	f.create(&database.Campaign{ID: &campaignID, CreatedAt: &now, UpdatedAt: &now, Name: "campaign " + campaignID.String()})
	f.create(&database.CampaignRecipientGroup{CampaignID: &campaignID, RecipientGroupID: &groupID})
	pending := f.sendAt(campaignID, alice, time.Now().Add(time.Hour), false)

	// the mark alone refuses changes and new use
	if err := f.groups.RecipientGroupRepository.MarkDeleting(f.ctx, &groupID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	name := vo.NewString127Must("renamed")
	if err := f.groups.UpdateByID(f.ctx, session, &groupID, &model.RecipientGroup{Name: nullable.NewNullableWithValue(*name)}); !isErr(err, errGroupDeleting) {
		t.Fatalf("expected a refused update, got %v", err)
	}
	if err := f.groups.AddRecipients(f.ctx, session, &groupID, []*uuid.UUID{&alice}); !isErr(err, errGroupDeleting) {
		t.Fatalf("expected a refused add, got %v", err)
	}
	if err := f.groups.RemoveRecipients(f.ctx, session, &groupID, []*uuid.UUID{&alice}); !isErr(err, errGroupDeleting) {
		t.Fatalf("expected a refused remove, got %v", err)
	}
	if err := f.svc.validateScope(f.ctx, session, nil, nil, []*uuid.UUID{&groupID}); !isErr(err, errGroupDeleting) {
		t.Fatalf("expected a program to refuse the group, got %v", err)
	}
	if deleting, err := f.groups.RecipientGroupRepository.AnyBeingDeleted(f.ctx, []*uuid.UUID{&groupID}); err != nil || !deleting {
		t.Fatalf("expected the group seen as being deleted: %v %v", deleting, err)
	}

	if err := f.groups.DeleteByID(f.ctx, session, &groupID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.RecipientGroup{}, groupID) {
		t.Fatal("the background delete must remove the group")
	}
	if f.campaignRecipient(pending).CancelledAt == nil {
		t.Fatal("the pending send of the member must be cancelled")
	}
	if !f.exists(&database.Recipient{}, alice) {
		t.Fatal("the members are kept")
	}
}

// TestRecipientGroupDeleteResumesAfterRestart checks a group marked as being
// deleted, as one a restart stopped, is finished by the task runner pass
func TestRecipientGroupDeleteResumesAfterRestart(t *testing.T) {
	f := newDeleteFixture(t)
	groupID := f.group(nil, f.recipient("bob@example.com"))
	if err := f.groups.RecipientGroupRepository.MarkDeleting(f.ctx, &groupID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := f.groups.FinishDeletesInBackground(f.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.RecipientGroup{}, groupID) {
		t.Fatal("the pass must finish the delete")
	}
	if err := f.groups.FinishDeletesInBackground(f.ctx, unauthorizedSession(t)); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected a refused pass, got %v", err)
	}
}

// TestRecipientGroupDeleteRefusedForProgram checks a group a program uses is
// refused at once, and a group a program started to use after the mark keeps
// its rows and its mark, shows why it waits and is deleted once the program
// no longer uses it
func TestRecipientGroupDeleteRefusedForProgram(t *testing.T) {
	f := newDeleteFixture(t)
	session := authorizedSession(t)
	groupID := f.group(nil, f.recipient("carol@example.com"))
	programID := f.program(model.ProgramLoopComplete)
	f.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID})
	if err := f.groups.DeleteByID(f.ctx, session, &groupID); !isValidationError(err) {
		t.Fatalf("expected a refused delete, got %v", err)
	}
	if row := f.groupRow(groupID); row.DeletingAt != nil {
		t.Fatal("a refused delete must not mark the group")
	}
	// a program that took the group after the mark stops the delete
	if err := f.groups.RecipientGroupRepository.MarkDeleting(f.ctx, &groupID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := f.groups.FinishDeletesInBackground(f.ctx, session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	if !f.exists(&database.RecipientGroup{}, groupID) {
		t.Fatal("a group a program uses must be kept")
	}
	if row := f.groupRow(groupID); row.DeletingAt == nil {
		t.Fatal("the pass must never remove the mark")
	}
	g, err := f.groups.GetByID(f.ctx, session, &groupID, &repository.RecipientGroupOption{})
	if err != nil || !strings.Contains(g.DeleteFailure, "program") {
		t.Fatalf("the group must show why the delete waits, got %+v %v", g, err)
	}
	if err := f.db.Delete(&database.ProgramRecipientGroup{}, "program_id = ?", programID).Error; err != nil {
		t.Fatalf("remove use: %v", err)
	}
	f.groups.deletes.mu.Lock()
	f.groups.deletes.retryAt = nil
	f.groups.deletes.mu.Unlock()
	if err := f.groups.FinishDeletesInBackground(f.ctx, session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.RecipientGroup{}, groupID) {
		t.Fatal("the delete must go on once the use is removed")
	}
}

func (f *deleteFixture) groupRow(id uuid.UUID) database.RecipientGroup {
	f.t.Helper()
	var row database.RecipientGroup
	if err := f.db.Where("id = ?", id).First(&row).Error; err != nil {
		f.t.Fatalf("load group: %v", err)
	}
	return row
}

// TestCompanyDeleteInBackground checks a company delete marks the company,
// refuses a rename while it is marked, and removes the company with its
// programs, groups and recipients in the background, also after a restart
func TestCompanyDeleteInBackground(t *testing.T) {
	f := newDeleteFixture(t)
	session := authorizedSession(t)
	companyID := f.company("Acme")
	alice := f.companyRecipient(companyID, "alice@acme.example")
	groupID := f.group(&companyID, alice)
	programID := f.program(model.ProgramLoopComplete)
	if err := f.db.Model(&database.Program{}).Where("id = ?", programID).Update("company_id", companyID).Error; err != nil {
		t.Fatalf("program company: %v", err)
	}
	stepID := f.step(programID, 0, true)
	campaignID := f.stepCampaign(programID, stepID, 0)
	f.send(campaignID, alice, true)

	if err := f.companies.CompanyRepository.MarkDeleting(f.ctx, &companyID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	name := vo.NewString64Must("Renamed")
	if err := f.companies.UpdateByID(f.ctx, session, &companyID, &model.Company{Name: nullable.NewNullableWithValue(*name)}); !isErr(err, errCompanyDeleting) {
		t.Fatalf("expected a refused rename, got %v", err)
	}
	// the task runner pass finishes a marked company, as after a restart
	if err := f.companies.FinishDeletesInBackground(f.ctx, session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	for what, gone := range map[string]bool{
		"company":   !f.exists(&database.Company{}, companyID),
		"group":     !f.exists(&database.RecipientGroup{}, groupID),
		"recipient": !f.exists(&database.Recipient{}, alice),
		"program":   !f.exists(&database.Program{}, programID),
		"campaign":  !f.exists(&database.Campaign{}, campaignID),
	} {
		if !gone {
			t.Fatalf("the company delete must remove the %s", what)
		}
	}

	// a delete request marks the company and the pass removes it
	other := f.company("Other")
	if _, err := f.companies.DeleteByID(f.ctx, session, &other); err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.Company{}, other) {
		t.Fatal("the background delete must remove the company")
	}
	if _, err := f.companies.DeleteByID(f.ctx, unauthorizedSession(t), &other); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected a refused delete, got %v", err)
	}
}

// isErr reports whether err is a validation error that wraps target
func isErr(err, target error) bool {
	var v errs.ValidationError
	return errors.As(err, &v) && errors.Is(v.Err, target)
}

// enableForeignKeys turns foreign keys on, as in production. The pool is one
// connection so the setting holds for every query.
func (f *deleteFixture) enableForeignKeys() {
	f.t.Helper()
	sqlDB, err := f.db.DB()
	if err != nil {
		f.t.Fatalf("db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := f.db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		f.t.Fatalf("foreign keys: %v", err)
	}
}

func (f *deleteFixture) reportTemplate(companyID uuid.UUID) uuid.UUID {
	f.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	f.create(&database.ReportTemplate{ID: &id, CreatedAt: &now, UpdatedAt: &now, CompanyID: &companyID, Content: "x"})
	return id
}

// TestCompanyDeleteWithForeignKeys checks with foreign keys on that a
// company holding rows its delete does not remove is refused up front and
// named, that the SCIM config goes with the company, and that a row added
// after the mark makes the delete wait with its mark kept and the reason
// shown, and the delete goes on once the row is removed
func TestCompanyDeleteWithForeignKeys(t *testing.T) {
	f := newDeleteFixture(t)
	f.enableForeignKeys()
	session := authorizedSession(t)
	companyID := f.company("Blocked")
	templateID := f.reportTemplate(companyID)

	_, err := f.companies.DeleteByID(f.ctx, session, &companyID)
	var v errs.ValidationError
	if !errors.As(err, &v) || !strings.Contains(v.Err.Error(), "report templates") {
		t.Fatalf("expected a refusal naming the report templates, got %v", err)
	}
	if row := f.companyRow(companyID); row.DeletingAt != nil {
		t.Fatal("a refused delete must not mark the company")
	}

	// without the template the company and its SCIM config are removed
	if err := f.db.Delete(&database.ReportTemplate{}, "id = ?", templateID).Error; err != nil {
		t.Fatalf("delete template: %v", err)
	}
	now := time.Now().UTC()
	scimID := uuid.New()
	f.create(&database.CompanyScimConfig{ID: &scimID, CreatedAt: &now, UpdatedAt: &now, CompanyID: &companyID, TokenHash: "h", TokenPrefix: "p"})
	if _, err := f.companies.DeleteByID(f.ctx, session, &companyID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.Company{}, companyID) || f.exists(&database.CompanyScimConfig{}, scimID) {
		t.Fatal("the company and its SCIM config must be removed")
	}

	// a blocking row added after the mark makes the delete wait, the mark
	// stays and the reason is shown
	late := f.company("Late")
	if err := f.companies.CompanyRepository.MarkDeleting(f.ctx, &late, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	lateTemplate := f.reportTemplate(late)
	if err := f.companies.FinishDeletesInBackground(f.ctx, session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	if row := f.companyRow(late); row.DeletingAt == nil {
		t.Fatal("the pass must never remove the mark")
	}
	c, err := f.companies.GetByID(f.ctx, session, &late)
	if err != nil || !strings.Contains(c.DeleteFailure, "report templates") {
		t.Fatalf("the company must show why the delete waits, got %+v %v", c, err)
	}
	// new data in a company that is being deleted is refused
	if _, err := refuseDeletingCompany(f.ctx, f.companies.CompanyRepository, &late); !isErr(err, errCompanyDeleting) {
		t.Fatalf("expected new data refused, got %v", err)
	}
	// once the row is removed the delete goes on
	if err := f.db.Delete(&database.ReportTemplate{}, "id = ?", lateTemplate).Error; err != nil {
		t.Fatalf("delete template: %v", err)
	}
	f.companies.deletes.mu.Lock()
	f.companies.deletes.retryAt = nil
	f.companies.deletes.mu.Unlock()
	if err := f.companies.FinishDeletesInBackground(f.ctx, session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.Company{}, late) {
		t.Fatal("the delete must go on once the row is removed")
	}
}

func (f *deleteFixture) companyRow(id uuid.UUID) database.Company {
	f.t.Helper()
	var row database.Company
	if err := f.db.Where("id = ?", id).First(&row).Error; err != nil {
		f.t.Fatalf("load company: %v", err)
	}
	return row
}

// TestRecipientGroupDeleteWithForeignKeys checks with foreign keys on that a
// group a report delivery setting sends to is refused up front, and that one
// such setting added after the mark makes the delete wait with its mark kept
// and the reason shown
func TestRecipientGroupDeleteWithForeignKeys(t *testing.T) {
	f := newDeleteFixture(t)
	f.enableForeignKeys()
	session := authorizedSession(t)
	companyID := f.company("Reports")
	groupID := f.group(&companyID, f.recipient("dave@example.com"))
	now := time.Now().UTC()
	configID := uuid.New()
	f.create(&database.CompanyReportConfig{ID: &configID, CreatedAt: &now, UpdatedAt: &now, CompanyID: &companyID, RecipientGroupID: &groupID})
	if err := f.groups.DeleteByID(f.ctx, session, &groupID); !isValidationError(err) {
		t.Fatalf("expected a refused delete, got %v", err)
	}
	if row := f.groupRow(groupID); row.DeletingAt != nil {
		t.Fatal("a refused delete must not mark the group")
	}
	if err := f.db.Delete(&database.CompanyReportConfig{}, "id = ?", configID).Error; err != nil {
		t.Fatalf("delete config: %v", err)
	}
	if err := f.groups.RecipientGroupRepository.MarkDeleting(f.ctx, &groupID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	f.create(&database.CompanyReportConfig{ID: &configID, CreatedAt: &now, UpdatedAt: &now, CompanyID: &companyID, RecipientGroupID: &groupID})
	if err := f.groups.FinishDeletesInBackground(f.ctx, session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	if !f.exists(&database.RecipientGroup{}, groupID) || f.groupRow(groupID).DeletingAt == nil {
		t.Fatal("the group must stay and keep its mark")
	}
	g, err := f.groups.GetByID(f.ctx, session, &groupID, &repository.RecipientGroupOption{})
	if err != nil || g.DeleteFailure == "" {
		t.Fatalf("the group must show why the delete waits, got %+v %v", g, err)
	}
	// without the setting the group is deleted with foreign keys on
	if err := f.db.Delete(&database.CompanyReportConfig{}, "id = ?", configID).Error; err != nil {
		t.Fatalf("delete config: %v", err)
	}
	f.groups.deletes.mu.Lock()
	f.groups.deletes.retryAt = nil
	f.groups.deletes.mu.Unlock()
	if err := f.groups.deleteWait(f.ctx, session, &groupID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if f.exists(&database.RecipientGroup{}, groupID) {
		t.Fatal("the group must be removed")
	}
}

// TestDeletePassSkipsHeldRows checks a pass whose oldest marks all wait after
// a failure still reaches a newer mark
func TestDeletePassSkipsHeldRows(t *testing.T) {
	f := newDeleteFixture(t)
	var held []uuid.UUID
	for i := 0; i < deletePassScanLimit+2; i++ {
		id := f.group(nil)
		held = append(held, id)
		if err := f.groups.RecipientGroupRepository.MarkDeleting(f.ctx, &id, time.Now().Add(-time.Hour)); err != nil {
			t.Fatalf("mark: %v", err)
		}
		f.groups.deletes.mu.Lock()
		if f.groups.deletes.retryAt == nil {
			f.groups.deletes.retryAt = map[uuid.UUID]time.Time{}
		}
		f.groups.deletes.retryAt[id] = time.Now().Add(time.Hour)
		f.groups.deletes.mu.Unlock()
	}
	newer := f.group(nil)
	if err := f.groups.RecipientGroupRepository.MarkDeleting(f.ctx, &newer, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := f.groups.FinishDeletesInBackground(f.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.RecipientGroup{}, newer) {
		t.Fatal("a newer mark must be reached past the held ones")
	}
	if !f.exists(&database.RecipientGroup{}, held[0]) {
		t.Fatal("a held mark waits")
	}
}

// TestCompanyDeleteNeverUnmarksAfterRemoval checks with foreign keys on that
// a company delete keeps its mark once it removed something: a busy program
// and a failing last write are tried again until the delete finishes
func TestCompanyDeleteNeverUnmarksAfterRemoval(t *testing.T) {
	f := newDeleteFixture(t)
	f.enableForeignKeys()
	session := authorizedSession(t)
	companyID := f.company("Busy")
	programID := f.program(model.ProgramLoopComplete)
	if err := f.db.Model(&database.Program{}).Where("id = ?", programID).Update("company_id", companyID).Error; err != nil {
		t.Fatalf("program company: %v", err)
	}
	now := time.Now().UTC()
	scimID := uuid.New()
	f.create(&database.CompanyScimConfig{ID: &scimID, CreatedAt: &now, UpdatedAt: &now, CompanyID: &companyID, TokenHash: "h", TokenPrefix: "p"})
	// a close of the program runs now, the delete must wait for it
	f.svc.claimStopping(programID)
	if _, err := f.companies.DeleteByID(f.ctx, session, &companyID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.CompanyScimConfig{}, scimID) {
		t.Fatal("the SCIM config goes first")
	}
	if row := f.companyRow(companyID); row.DeletingAt == nil {
		t.Fatal("a delete that removed something must keep its mark")
	}
	f.svc.releaseStopping(programID)

	// the last write fails, the programs are gone and the mark stays
	if err := f.db.Exec(`CREATE TRIGGER fail_company BEFORE DELETE ON companies
		BEGIN SELECT RAISE(ABORT, 'company delete refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	f.companies.deletes.mu.Lock()
	f.companies.deletes.retryAt = nil
	f.companies.deletes.mu.Unlock()
	if err := f.companies.FinishDeletesInBackground(f.ctx, session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.Program{}, programID) {
		t.Fatal("the program must be removed")
	}
	if row := f.companyRow(companyID); row.DeletingAt == nil {
		t.Fatal("a delete that removed something must keep its mark")
	}
	c, err := f.companies.GetByID(f.ctx, session, &companyID)
	if err != nil || c.DeleteFailure == "" {
		t.Fatalf("a delete that is tried again shows why it waits: %+v %v", c, err)
	}
	if err := f.db.Exec(`DROP TRIGGER fail_company`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	f.companies.deletes.mu.Lock()
	f.companies.deletes.retryAt = nil
	f.companies.deletes.mu.Unlock()
	if err := f.companies.FinishDeletesInBackground(f.ctx, session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.Company{}, companyID) {
		t.Fatal("the delete must finish")
	}
}

// TestCompanyDeletingRefusesWrites checks a company marked as being deleted
// takes no new pages, groups or program copies
func TestCompanyDeletingRefusesWrites(t *testing.T) {
	f := newDeleteFixture(t)
	f.enableForeignKeys()
	session := authorizedSession(t)
	companyID := f.company("Inert")
	programID := f.program(model.ProgramLoopComplete)
	if err := f.companies.CompanyRepository.MarkDeleting(f.ctx, &companyID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	pageName := vo.NewString64Must("page")
	_, err := f.companies.PageService.Create(f.ctx, session, &model.Page{
		Name: nullable.NewNullableWithValue(*pageName), CompanyID: nullable.NewNullableWithValue(companyID),
	})
	if !isErr(err, errCompanyDeleting) {
		t.Fatalf("expected a refused page, got %v", err)
	}
	groupName := vo.NewString127Must("group")
	_, err = f.groups.Create(f.ctx, session, &model.RecipientGroup{
		Name: nullable.NewNullableWithValue(*groupName), CompanyID: nullable.NewNullableWithValue(companyID),
	})
	if !isErr(err, errCompanyDeleting) {
		t.Fatalf("expected a refused group, got %v", err)
	}
	_, err = f.svc.Clone(f.ctx, session, &programID, *vo.NewString64Must("copy"), &companyID)
	if !isErr(err, errCompanyDeleting) {
		t.Fatalf("expected a refused copy, got %v", err)
	}
}

// TestReportConfigRefusesDeletingGroup checks the report delivery can not
// be set to send to a group that is being deleted
func TestReportConfigRefusesDeletingGroup(t *testing.T) {
	f := newDeleteFixture(t)
	f.enableForeignKeys()
	session := authorizedSession(t)
	companyID := f.company("Reports")
	groupID := f.group(&companyID)
	if err := f.groups.RecipientGroupRepository.MarkDeleting(f.ctx, &groupID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	svc := &CompanyReportConfig{
		Common:                        Common{Logger: f.svc.Logger},
		CompanyReportConfigRepository: &repository.CompanyReportConfig{DB: f.db},
		RecipientGroupRepository:      f.groups.RecipientGroupRepository,
		CompanyRepository:             f.companies.CompanyRepository,
	}
	_, err := svc.Upsert(f.ctx, session, &companyID, &model.CompanyReportConfig{
		RecipientGroupID: nullable.NewNullableWithValue(groupID),
	})
	if !isErr(err, errGroupDeleting) {
		t.Fatalf("expected a refused group, got %v", err)
	}
}

// TestRecipientGroupDeleteNeverLeftEmptied checks with foreign keys on that a
// group delete that removed something keeps its mark when a later write
// fails, and finishes on the next try
func TestRecipientGroupDeleteNeverLeftEmptied(t *testing.T) {
	f := newDeleteFixture(t)
	f.enableForeignKeys()
	session := authorizedSession(t)
	alice := f.recipient("erin@example.com")
	groupID := f.group(nil, alice)
	now := time.Now().UTC()
	campaignID := uuid.New()
	f.create(&database.Campaign{ID: &campaignID, CreatedAt: &now, UpdatedAt: &now, Name: "campaign " + campaignID.String()})
	f.create(&database.CampaignRecipientGroup{CampaignID: &campaignID, RecipientGroupID: &groupID})
	pending := f.sendAt(campaignID, alice, time.Now().Add(time.Hour), false)
	if err := f.db.Exec(`CREATE TRIGGER fail_group BEFORE DELETE ON recipient_groups
		BEGIN SELECT RAISE(ABORT, 'group delete refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := f.groups.DeleteByID(f.ctx, session, &groupID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.waitDeletes()
	if f.campaignRecipient(pending).CancelledAt == nil {
		t.Fatal("the pending send was cancelled before the failure")
	}
	if row := f.groupRow(groupID); row.DeletingAt == nil {
		t.Fatal("a delete that removed something must keep its mark")
	}
	if err := f.db.Exec(`DROP TRIGGER fail_group`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	f.groups.deletes.mu.Lock()
	f.groups.deletes.retryAt = nil
	f.groups.deletes.mu.Unlock()
	if err := f.groups.FinishDeletesInBackground(f.ctx, session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.RecipientGroup{}, groupID) {
		t.Fatal("the delete must finish")
	}
}

// TestProgramStartFailureClearedByTick checks a failed enrollment after
// Start is no longer shown once the tick enrolled the members
func TestProgramStartFailureClearedByTick(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	p.svc.recordActionFailure(programID, programActionStart, errs.NewValidationError(errors.New("enroll refused")))
	if err := p.svc.resolveEnrollments(p.ctx); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	program := p.loadProgram(programID)
	p.svc.applyRunState(program)
	if program.ActionFailure != nil {
		t.Fatalf("the tick must clear the start failure, got %+v", program.ActionFailure)
	}
}

// TestCloneRefusesDeletingGroup checks a copy that keeps a group that is
// being deleted is refused
func TestCloneRefusesDeletingGroup(t *testing.T) {
	f := newDeleteFixture(t)
	session := authorizedSession(t)
	programID := f.program(model.ProgramLoopComplete)
	f.step(programID, 0, true)
	groupID := f.group(nil, f.recipient("frank@example.com"))
	f.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID})
	// the fixture rows need no checks, the copy runs with foreign keys on
	f.enableForeignKeys()
	if err := f.groups.RecipientGroupRepository.MarkDeleting(f.ctx, &groupID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	_, err := f.svc.Clone(f.ctx, session, &programID, *vo.NewString64Must("copy"), nil)
	if !isErr(err, errGroupDeleting) {
		t.Fatalf("expected a refused copy, got %v", err)
	}
}

// TestDeleteMarkReleasedOnPanic checks a panic while the mark lock is held
// leaves the lock free for the next delete and for saves
func TestDeleteMarkReleasedOnPanic(t *testing.T) {
	var l deleteMarkLock
	id := uuid.New()
	func() {
		defer func() { _ = recover() }()
		unlock, err := l.mark(context.Background(), id)
		if err != nil {
			t.Fatalf("mark: %v", err)
		}
		defer unlock()
		panic("boom")
	}()
	unlock, err := l.mark(context.Background(), id)
	if err != nil {
		t.Fatalf("the lock must be free after a panic: %v", err)
	}
	unlock()
	l.read(&id)()
	if len(l.entries) != 0 {
		t.Fatalf("unused entries must be removed, %d left", len(l.entries))
	}
}

// TestDeleteMarkKeyedPerID checks a save that holds one company does not
// keep the delete of another company waiting
func TestDeleteMarkKeyedPerID(t *testing.T) {
	var l deleteMarkLock
	busy, other := uuid.New(), uuid.New()
	release := l.read(&busy)
	defer release()
	start := time.Now()
	unlock, err := l.mark(context.Background(), other)
	if err != nil {
		t.Fatalf("an unrelated delete must not wait: %v", err)
	}
	unlock()
	if time.Since(start) > time.Second {
		t.Fatal("an unrelated delete waited")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := l.mark(ctx, busy); err == nil {
		t.Fatal("the delete of the held company must wait")
	}
}

// TestDeletingCompanyStopsSending checks a company marked as being deleted
// sends nothing more: its due sends are not read or claimed and its programs
// are not advanced or enrolled
func TestDeletingCompanyStopsSending(t *testing.T) {
	f := newDeleteFixture(t)
	f.enableForeignKeys()
	companyID := f.company("Quiet")
	alice := f.companyRecipient(companyID, "gina@quiet.example")
	now := time.Now().UTC()
	campaignID := uuid.New()
	f.create(&database.Campaign{ID: &campaignID, CreatedAt: &now, UpdatedAt: &now, Name: "campaign " + campaignID.String(), CompanyID: &companyID})
	due := f.sendAt(campaignID, alice, time.Now().Add(-time.Minute), false)
	programID := f.program(model.ProgramLoopComplete)
	if err := f.db.Model(&database.Program{}).Where("id = ?", programID).Update("company_id", companyID).Error; err != nil {
		t.Fatalf("program company: %v", err)
	}
	f.enroll(programID, alice, 0)
	sends := &repository.CampaignRecipient{DB: f.db}
	before, err := sends.GetUnsendRecipientsForSending(f.ctx, 100, &repository.CampaignRecipientOption{})
	if err != nil || len(before) != 1 {
		t.Fatalf("the due send must be read before the mark: %d %v", len(before), err)
	}
	if err := f.companies.CompanyRepository.MarkDeleting(f.ctx, &companyID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	after, err := sends.GetUnsendRecipientsForSending(f.ctx, 100, &repository.CampaignRecipientOption{})
	if err != nil || len(after) != 0 {
		t.Fatalf("a deleting company sends nothing, read %d %v", len(after), err)
	}
	if ok, err := sends.ClaimForSending(f.ctx, &due, time.Now()); err != nil || ok {
		t.Fatalf("a send read before the mark must not be claimed: %v %v", ok, err)
	}
	enrollments, err := f.svc.ProgramRepository.GetDueEnrollments(f.ctx, time.Now().Add(time.Hour), 100)
	if err != nil || len(enrollments) != 0 {
		t.Fatalf("a program of a deleting company is not advanced: %d %v", len(enrollments), err)
	}
	active, err := f.svc.ProgramRepository.GetActivePrograms(f.ctx, time.Now())
	if err != nil || len(active) != 0 {
		t.Fatalf("a program of a deleting company enrolls nobody: %d %v", len(active), err)
	}
}

// TestCompanyDeleteRemovesStatsAndAttachments checks the manual campaign
// stats are refused while the company is being deleted and removed with it,
// and its attachments are removed with their files, shared ones are kept
func TestCompanyDeleteRemovesStatsAndAttachments(t *testing.T) {
	f := newDeleteFixture(t)
	f.enableForeignKeys()
	session := authorizedSession(t)
	companyID := f.company("Files")
	now := time.Now().UTC()
	statsID := uuid.New()
	f.create(&database.CampaignStats{ID: &statsID, CreatedAt: &now, UpdatedAt: &now, CampaignName: "manual", CompanyID: &companyID})
	att := f.companies.AttachmentService
	attachmentID, sharedID := uuid.New(), uuid.New()
	f.create(&database.Attachment{ID: &attachmentID, CreatedAt: &now, UpdatedAt: &now, CompanyID: &companyID, Filename: "a.pdf"})
	f.create(&database.Attachment{ID: &sharedID, CreatedAt: &now, UpdatedAt: &now, Filename: "s.pdf"})
	dir := filepath.Join(att.RootFolder, companyID.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(dir, "a.pdf")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := f.companies.CompanyRepository.MarkDeleting(f.ctx, &companyID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	_, err := f.svc.CampaignService.CreateManualCampaignStats(f.ctx, session, &database.CampaignStats{CampaignName: "late", CompanyID: &companyID})
	if !isErr(err, errCompanyDeleting) {
		t.Fatalf("expected manual stats refused, got %v", err)
	}
	if err := f.companies.FinishDeletesInBackground(f.ctx, session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	f.waitDeletes()
	if f.exists(&database.Company{}, companyID) {
		t.Fatal("the company must be removed")
	}
	if f.exists(&database.CampaignStats{}, statsID) {
		t.Fatal("the manual stats must be removed")
	}
	if f.exists(&database.Attachment{}, attachmentID) {
		t.Fatal("the attachment must be removed")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("the attachment file must be removed: %v", err)
	}
	if !f.exists(&database.Attachment{}, sharedID) {
		t.Fatal("a shared attachment must be kept")
	}
}

// TestGroupOfDeletingCompanyRefusesMembers checks the members of a group of
// a company that is being deleted can not be changed
func TestGroupOfDeletingCompanyRefusesMembers(t *testing.T) {
	f := newDeleteFixture(t)
	f.enableForeignKeys()
	session := authorizedSession(t)
	companyID := f.company("Members")
	alice := f.companyRecipient(companyID, "hank@members.example")
	groupID := f.group(&companyID)
	if err := f.companies.CompanyRepository.MarkDeleting(f.ctx, &companyID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := f.groups.AddRecipients(f.ctx, session, &groupID, []*uuid.UUID{&alice}); !isErr(err, errCompanyDeleting) {
		t.Fatalf("expected a refused add, got %v", err)
	}
	if err := f.groups.RemoveRecipients(f.ctx, session, &groupID, []*uuid.UUID{&alice}); !isErr(err, errCompanyDeleting) {
		t.Fatalf("expected a refused remove, got %v", err)
	}
}
