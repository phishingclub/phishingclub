package service

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"go.uber.org/zap/zapcore"
)

// programBackgroundMutators are the program methods the scheduler and the
// report sender run on their own that change data or send. Each must create
// an audit event, as no request is there to audit them.
var programBackgroundMutators = map[string][]string{
	"programScheduler.go": {
		"Tick",
		"endProgramDue",
		"removeOldIdentityLinks",
	},
	"program.go": {
		"finishDeleteOf",
	},
	"programAnonymize.go": {
		"HandleAnonymizePrograms",
		"HandleDataAnonymizePrograms",
	},
	"programReportSchedule.go": {
		"SendDueReports",
		"sendProgramReport",
	},
}

// TestProgramBackgroundMutatorsAudit checks every listed background mutator
// creates an audit event
func TestProgramBackgroundMutatorsAudit(t *testing.T) {
	for path, names := range programBackgroundMutators {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		bodies := map[string]string{}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv == nil || receiverType(fn) != "Program" {
				continue
			}
			bodies[fn.Name.Name] = string(src[fset.Position(fn.Body.Pos()).Offset:fset.Position(fn.Body.End()).Offset])
		}
		for _, name := range names {
			body, ok := bodies[name]
			if !ok {
				t.Errorf("%s: Program.%s is gone, update the list", path, name)
				continue
			}
			if !strings.Contains(body, "NewAuditEvent(") {
				t.Errorf("%s: Program.%s changes data in the background but writes no audit event", path, name)
			}
		}
	}
}

// withAuditBuffer sends the audit events of the program service to a buffer
func (p *programTest) withAuditBuffer() *bytes.Buffer {
	logger, buf := bufferLogger(zapcore.InfoLevel)
	p.svc.AuditLogger = logger
	p.svc.CampaignService.AuditLogger = logger
	return buf
}

func eventDetails(t *testing.T, ev map[string]any) map[string]any {
	t.Helper()
	d, _ := ev["details"].(map[string]any)
	if d == nil {
		t.Fatalf("event has no details: %v", ev)
	}
	return d
}

// TestTickAuditsAdvance checks a tick that enrolls, creates a hidden campaign
// and schedules a send writes one system style event with the counts, and a
// tick that changes nothing writes none. The advance of a tick plans from the
// time after its membership pass, so a person enrolled as due is advanced in
// the same tick.
func TestTickAuditsAdvance(t *testing.T) {
	p := newProgramDataSavingTest(t)
	buf := p.withAuditBuffer()
	session := authorizedSession(t)
	groupID := p.group("staff")
	alice := p.recipient("alice@example.com")
	p.create(&database.RecipientGroupRecipient{RecipientID: &alice, RecipientGroupID: &groupID})
	programID := p.program(model.ProgramLoopComplete)
	p.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID})
	p.step(programID, 0, true)

	// the membership pass enrolls alice and the advance plans her send
	p.svc.lastRetentionAt = time.Now()
	if err := p.svc.Tick(p.ctx, session); err != nil {
		t.Fatalf("tick: %v", err)
	}
	enrollments, err := p.svc.ProgramRepository.GetEnrollmentPlansByProgramID(p.ctx, &programID)
	if err != nil || len(enrollments) != 1 {
		t.Fatalf("expected alice enrolled: %d %v", len(enrollments), err)
	}
	// nothing is due, nothing is written
	p.svc.lastMembershipAt = time.Now()
	if err := p.svc.Tick(p.ctx, session); err != nil {
		t.Fatalf("tick: %v", err)
	}

	ev := step4Events(t, buf.String(), "Program.Advance")
	if len(ev) != 1 {
		t.Fatalf("expected one event per tick that changed something, got %d: %v", len(ev), ev)
	}
	if ev[0]["result"] != "success" {
		t.Fatalf("expected success: %v", ev[0])
	}
	details := eventDetails(t, ev[0])
	ids, _ := details["programIds"].([]any)
	if len(ids) != 1 || ids[0] != programID.String() {
		t.Fatalf("tick program ids: %v", details)
	}
	if details["enrolled"] != float64(1) || details["campaignsCreated"] != float64(1) ||
		details["sendsScheduled"] != float64(1) || details["exited"] != float64(0) {
		t.Fatalf("tick details: %v", details)
	}
	if strings.Contains(buf.String(), "alice@example.com") || strings.Contains(buf.String(), alice.String()) {
		t.Fatal("the tick event must not name a person")
	}
}

// TestTickAuditLeavesOutAnonymousPrograms checks the tick event never names
// an anonymous program, gives its counts or says one changed, while a named
// program changed in the same tick keeps its id and counts, and a tick that
// changed only an anonymous program writes no event
func TestTickAuditLeavesOutAnonymousPrograms(t *testing.T) {
	p := newProgramDataSavingTest(t)
	buf := p.withAuditBuffer()
	session := authorizedSession(t)
	groupID := p.group("staff")
	alice := p.recipient("alice@example.com")
	p.create(&database.RecipientGroupRecipient{RecipientID: &alice, RecipientGroupID: &groupID})
	anonymousID := p.program(model.ProgramLoopComplete)
	p.setProgram(anonymousID, map[string]any{"is_anonymous": true})
	p.create(&database.ProgramRecipientGroup{ProgramID: &anonymousID, RecipientGroupID: &groupID})
	p.step(anonymousID, 0, true)
	namedID := p.program(model.ProgramLoopComplete)
	p.create(&database.ProgramRecipientGroup{ProgramID: &namedID, RecipientGroupID: &groupID})
	p.step(namedID, 0, true)

	p.svc.lastRetentionAt = time.Now()
	if err := p.svc.Tick(p.ctx, session); err != nil {
		t.Fatalf("tick: %v", err)
	}
	ev := step4Events(t, buf.String(), "Program.Advance")
	if len(ev) != 1 || ev[0]["result"] != "success" {
		t.Fatalf("expected one success event, got %v", ev)
	}
	details := eventDetails(t, ev[0])
	ids, _ := details["programIds"].([]any)
	if len(ids) != 1 || ids[0] != namedID.String() {
		t.Fatalf("only the named program may be named: %v", details)
	}
	if details["enrolled"] != float64(1) || details["sendsScheduled"] != float64(1) {
		t.Fatalf("the counts must be those of the named program only: %v", details)
	}
	if _, ok := details["anonymousProgramsChanged"]; ok {
		t.Fatalf("the event must not say an anonymous program changed: %v", details)
	}
	if strings.Contains(buf.String(), anonymousID.String()) {
		t.Fatal("the tick event must not name the anonymous program")
	}

	// a tick that changes only the anonymous program writes no event
	buf.Reset()
	p.setProgram(namedID, map[string]any{"closed_at": time.Now().UTC()})
	bob := p.recipient("bob@example.com")
	p.create(&database.RecipientGroupRecipient{RecipientID: &bob, RecipientGroupID: &groupID})
	p.svc.lastMembershipAt = time.Time{}
	if err := p.svc.Tick(p.ctx, session); err != nil {
		t.Fatalf("tick: %v", err)
	}
	enrolled, err := p.svc.ProgramRepository.GetEnrollmentPlansByProgramID(p.ctx, &anonymousID)
	if err != nil || len(enrolled) != 2 {
		t.Fatalf("expected bob enrolled in the anonymous program: %d %v", len(enrolled), err)
	}
	if ev := step4Events(t, buf.String(), "Program.Advance"); len(ev) != 0 {
		t.Fatalf("an anonymous only tick must write no event, got %v", ev)
	}
}

// TestTickAuditsFailureThrottled checks a failing tick writes a failed event
// once and holds back the same failure on the next ticks
func TestTickAuditsFailureThrottled(t *testing.T) {
	p := newProgramServiceTest(t)
	buf := p.withAuditBuffer()
	p.svc.lastMembershipAt = time.Now()
	p.svc.lastRetentionAt = time.Now()
	if err := p.db.Migrator().DropTable(&database.ProgramEnrollment{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := p.svc.Tick(p.ctx, authorizedSession(t)); err == nil {
			t.Fatal("expected the tick to fail")
		}
	}
	ev := step4Events(t, buf.String(), "Program.Advance")
	if len(ev) != 1 || ev[0]["result"] != "failed" {
		t.Fatalf("expected one failed event, got %v", ev)
	}
}

// TestProgramTaskDeniedAudit checks the scheduler and the report sender write
// a denied event for a session without the permission
func TestProgramTaskDeniedAudit(t *testing.T) {
	p := newProgramServiceTest(t)
	buf := p.withAuditBuffer()
	if err := p.svc.Tick(p.ctx, unauthorizedSession(t)); err == nil {
		t.Fatal("expected the tick to be refused")
	}
	if err := p.svc.SendDueReports(p.ctx, unauthorizedSession(t)); err == nil {
		t.Fatal("expected the report pass to be refused")
	}
	for _, name := range []string{"Program.Advance", "Program.SendDueReports"} {
		ev := step4Events(t, buf.String(), name)
		if len(ev) != 1 || ev[0]["result"] != "denied" {
			t.Fatalf("%s: expected one denied event, got %v", name, ev)
		}
	}
}

// TestRemoveIdentityLinksAudit checks the identity removal writes one event
// per day with the total row count of the day, not the anonymous programs,
// once a pass runs on a later day, none for a day without changes, no debug
// line with a program, and a throttled failed event when it fails
func TestRemoveIdentityLinksAudit(t *testing.T) {
	p := newProgramServiceTest(t)
	buf := p.withAuditBuffer()
	logs, logBuf := bufferLogger(zapcore.DebugLevel)
	p.svc.Logger = logs
	session := authorizedSession(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	alice := p.recipient("alice@example.com")
	sendID := p.send(campaignID, alice, true)
	pseudonym := uuid.New()
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", sendID).Updates(map[string]any{
		"anonymized_id": pseudonym,
		"sent_at":       utils.RFC3339UTC(now.Add(-20 * 24 * time.Hour)),
	})

	p.svc.removeOldIdentityLinks(p.ctx, session, now)
	if row := p.campaignRecipient(sendID); row.RecipientID != nil {
		t.Fatal("expected the person link removed")
	}
	// more passes the same month write nothing yet, not even on a later day
	p.svc.removeOldIdentityLinks(p.ctx, session, now.Add(time.Hour))
	p.svc.removeOldIdentityLinks(p.ctx, session, now.Add(24*time.Hour))
	if ev := step4Events(t, buf.String(), "Program.RemoveIdentityLinks"); len(ev) != 0 {
		t.Fatalf("no event may be written before the month passed, got %v", ev)
	}
	if strings.Contains(logBuf.String(), programID.String()) {
		t.Fatal("no log line may name the anonymous program")
	}
	// the first pass of the next month writes the total of the month before
	nextMonth := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	p.svc.removeOldIdentityLinks(p.ctx, session, nextMonth)
	ev := step4Events(t, buf.String(), "Program.RemoveIdentityLinks")
	if len(ev) != 1 || ev[0]["result"] != "success" {
		t.Fatalf("expected one success event, got %v", ev)
	}
	d := eventDetails(t, ev[0])
	if _, named := d["programIds"]; named || d["rows"] != float64(1) || d["month"] != "2026-03" {
		t.Fatalf("the event must give the total of the month only, not the anonymous programs: %v", d)
	}
	if _, day := d["day"]; day {
		t.Fatalf("the event must not give a day: %v", d)
	}
	// a month without changes writes nothing
	p.svc.removeOldIdentityLinks(p.ctx, session, nextMonth.AddDate(0, 1, 0))
	if ev := step4Events(t, buf.String(), "Program.RemoveIdentityLinks"); len(ev) != 1 {
		t.Fatalf("a month without changes must write nothing, got %v", ev)
	}

	if err := p.db.Migrator().DropTable(&database.CampaignRecipient{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	p.svc.removeOldIdentityLinks(p.ctx, session, now)
	p.svc.removeOldIdentityLinks(p.ctx, session, now)
	ev = step4Events(t, buf.String(), "Program.RemoveIdentityLinks")
	if len(ev) != 2 || ev[1]["result"] != "failed" {
		t.Fatalf("expected one failed event after the success, got %v", ev)
	}
	failed, _ := eventDetails(t, ev[1])["failedProgramIds"].([]any)
	if len(failed) != 1 || failed[0] != programID.String() {
		t.Fatalf("the failed event must name the program: %v", ev[1])
	}
}

// TestSendProgramReportFailuresAudited checks a report that is not set up and
// an anonymous report that is not whole months each write a failed event,
// once per period
func TestSendProgramReportFailuresAudited(t *testing.T) {
	p := newProgramServiceTest(t)
	buf := p.withAuditBuffer()
	session := authorizedSession(t)
	now := time.Now()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	notSetUp := p.loadProgram(p.program(model.ProgramLoopComplete))
	for i := 0; i < 2; i++ {
		if err := p.svc.sendProgramReport(p.ctx, session, notSetUp, from, to, now); err == nil {
			t.Fatal("expected the report to fail without delivery set up")
		}
	}
	anonID := p.program(model.ProgramLoopComplete)
	p.setProgram(anonID, map[string]any{"is_anonymous": true})
	anon := p.loadProgram(anonID)
	if err := p.svc.sendProgramReport(p.ctx, session, anon, from.Add(24*time.Hour), to, now); err == nil {
		t.Fatal("expected a part month anonymous report to be refused")
	}

	ev := step4Events(t, buf.String(), "Program.SendReport")
	if len(ev) != 2 {
		t.Fatalf("expected one failed event per program and period, got %d: %v", len(ev), ev)
	}
	first := eventDetails(t, ev[0])
	if ev[0]["result"] != "failed" || first["id"] != notSetUp.ID.MustGet().String() || first["reason"] != "notSetUp" {
		t.Fatalf("not set up event: %v", ev[0])
	}
	second := eventDetails(t, ev[1])
	if ev[1]["result"] != "failed" || second["id"] != anonID.String() || second["errorClass"] != "validation" {
		t.Fatalf("whole month event: %v", ev[1])
	}
}

// TestProgramEventsCarryID checks denied and failed events of a program
// change or read name the program
func TestProgramEventsCarryID(t *testing.T) {
	p := newProgramLifecycleTest(t)
	buf := p.withAuditBuffer()
	id := uuid.New()
	other := unauthorizedSession(t)
	rename := &model.Program{Name: nullable.NewNullableWithValue(*mustString64(t, "renamed"))}
	_ = p.svc.UpdateByID(p.ctx, other, &id, rename)
	_ = p.svc.DeleteByID(p.ctx, other, &id)
	_, _ = p.svc.GetByID(p.ctx, other, &id)
	_, _ = p.svc.GetResults(p.ctx, other, &id, "")
	// an update of a program that does not exist fails after the check
	_ = p.svc.UpdateByID(p.ctx, authorizedSession(t), &id, rename)
	for _, c := range []struct{ name, result string }{
		{"Program.UpdateById", "denied"},
		{"Program.DeleteById", "denied"},
		{"Program.GetById", "denied"},
		{"Program.GetResults", "denied"},
		{"Program.UpdateById", "failed"},
	} {
		found := false
		for _, ev := range step4Events(t, buf.String(), c.name) {
			if ev["result"] == c.result {
				found = true
				if eventDetails(t, ev)["id"] != id.String() {
					t.Errorf("%s %s has no program id: %v", c.name, c.result, ev)
				}
			}
		}
		if !found {
			t.Errorf("no %s %s event", c.name, c.result)
		}
	}
}

// TestProgramCreateUnknownCompany checks a create for a company that does not
// exist is a validation error and stores nothing
func TestProgramCreateUnknownCompany(t *testing.T) {
	p := newProgramWriteTest(t)
	if err := p.db.AutoMigrate(&database.Company{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	p.svc.CompanyRepository = &repository.Company{DB: p.db}
	templateID := p.stepTemplate(p.step(p.program(model.ProgramLoopComplete), 0, true))
	req := newProgramRequest(t, "unknown company", templateID, p.group("staff"))
	req.CompanyID = nullable.NewNullableWithValue(uuid.New())
	if _, err := p.svc.Create(p.ctx, authorizedSession(t), req); err == nil || !isValidation(err) {
		t.Fatalf("expected a validation error, got %v", err)
	}
	var count int64
	p.db.Model(&database.Program{}).Where("name = ?", "unknown company").Count(&count)
	if count != 0 {
		t.Fatal("no program may be stored for an unknown company")
	}
}

// TestHasTemplateGroupDataGuards checks the template group check refuses a
// hidden program campaign and audits a denied session
func TestHasTemplateGroupDataGuards(t *testing.T) {
	p := newProgramServiceTest(t)
	buf := p.withAuditBuffer()
	programID := p.program(model.ProgramLoopComplete)
	campaignID := p.stepCampaign(programID, p.step(programID, 0, true), 0)
	if _, err := p.svc.CampaignService.HasTemplateGroupData(p.ctx, authorizedSession(t), &campaignID); err == nil {
		t.Fatal("a hidden program campaign must not be read")
	}
	if _, err := p.svc.CampaignService.HasTemplateGroupData(p.ctx, unauthorizedSession(t), &campaignID); err == nil {
		t.Fatal("expected the session to be refused")
	}
	ev := step4Events(t, buf.String(), "Campaign.HasTemplateGroupData")
	if len(ev) != 1 || ev[0]["result"] != "denied" || eventDetails(t, ev[0])["campaignId"] != campaignID.String() {
		t.Fatalf("expected one denied event with the campaign, got %v", ev)
	}
}

// TestSharedExportListsGlobalPrograms checks the shared export lists the
// global programs, a named one with its events and an anonymous one without,
// and its audit event counts them
func TestSharedExportListsGlobalPrograms(t *testing.T) {
	p := newProgramServiceTest(t)
	buf := p.withAuditBuffer()
	now := time.Now().UTC()
	alice := p.recipient("alice@example.com")
	companyID := uuid.New()
	mk := func(name string, company *uuid.UUID, anonymous bool) {
		programID := p.program(model.ProgramLoopComplete)
		p.db.Model(&database.Program{}).Where("id = ?", programID).Updates(map[string]any{
			"name": name, "company_id": company, "is_anonymous": anonymous,
		})
		campaignID := p.stepCampaign(programID, p.step(programID, 0, true), 0)
		p.send(campaignID, alice, true)
		eventID := uuid.New()
		p.create(&database.CampaignEvent{
			ID: &eventID, CreatedAt: &now, UpdatedAt: &now,
			CampaignID: &campaignID, RecipientID: &alice, EventID: &eventID,
		})
	}
	mk("Global named", nil, false)
	mk("Global anonymous", nil, true)
	mk("Company named", &companyID, false)

	sections, err := p.svc.CampaignService.GetProgramEventsExport(p.ctx, authorizedSession(t), nil)
	if err != nil {
		t.Fatalf("shared export: %v", err)
	}
	if len(sections) != 2 {
		t.Fatalf("expected the two global programs, got %d", len(sections))
	}
	for _, s := range sections {
		switch s.ProgramName {
		case "Global named":
			if s.Anonymous || len(exportEvents(t, p.ctx, s)) != 1 {
				t.Fatalf("named global program: %+v", s)
			}
		case "Global anonymous":
			if !s.Anonymous || len(exportEvents(t, p.ctx, s)) != 0 {
				t.Fatalf("an anonymous program must have no events: %+v", s)
			}
		default:
			t.Fatalf("unexpected program %q", s.ProgramName)
		}
	}

	company := &Company{Common: p.svc.Common, CampaignService: p.svc.CampaignService}
	for _, id := range []*uuid.UUID{nil, &companyID} {
		done, err := company.AuthorizeExport(p.ctx, authorizedSession(t), id)
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		done(true)
	}
	ev := step4Events(t, buf.String(), "Company.Export")
	if len(ev) != 2 {
		t.Fatalf("expected two export events, got %v", ev)
	}
	shared, own := eventDetails(t, ev[0]), eventDetails(t, ev[1])
	if shared["programs"] != float64(2) || shared["programsWithEvents"] != float64(1) {
		t.Fatalf("shared export details: %v", shared)
	}
	if own["programs"] != float64(1) || own["programsWithEvents"] != float64(1) {
		t.Fatalf("company export details: %v", own)
	}
}
