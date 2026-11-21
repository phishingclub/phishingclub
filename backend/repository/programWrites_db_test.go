package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// stepRows returns the step rows of a program by position
func stepRows(t *testing.T, db *gorm.DB, programID *uuid.UUID) []database.ProgramStep {
	t.Helper()
	rows := []database.ProgramStep{}
	if err := db.Where("program_id = ?", programID).Order("position").Find(&rows).Error; err != nil {
		t.Fatalf("steps: %v", err)
	}
	return rows
}

// TestProgramReplaceSteps checks a step keeps its id while it keeps its
// template, steps may swap places under the unique position index, a new
// template is a new step and a step left out is removed
func TestProgramReplaceSteps(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "replace", nil)
	tA, tB, tC := ptrUUID(), ptrUUID(), ptrUUID()
	stepA := createStep(t, db, programID, tA, 0)
	stepB := createStep(t, db, programID, tB, 1)
	keep := func(id, template *uuid.UUID, position int) *model.ProgramStep {
		s := &model.ProgramStep{
			CampaignTemplateID: nullable.NewNullableWithValue(*template),
			Position:           nullable.NewNullableWithValue(position),
		}
		if id != nil {
			s.ID = nullable.NewNullableWithValue(*id)
		}
		return s
	}
	// swap A and B
	if err := r.ReplaceSteps(ctx, programID, []*model.ProgramStep{keep(stepB, tB, 0), keep(stepA, tA, 1)}); err != nil {
		t.Fatalf("swap: %v", err)
	}
	rows := stepRows(t, db, programID)
	if len(rows) != 2 || *rows[0].ID != *stepB || *rows[1].ID != *stepA {
		t.Fatalf("swap kept ids in new order: %+v", rows)
	}
	// keep B, drop A, add C, and give B's id with A's template, which is a new step
	if err := r.ReplaceSteps(ctx, programID, []*model.ProgramStep{
		keep(stepB, tB, 0),
		keep(nil, tC, 1),
		keep(stepB, tA, 2),
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	rows = stepRows(t, db, programID)
	if len(rows) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(rows))
	}
	if *rows[0].ID != *stepB || *rows[1].CampaignTemplateID != *tC || *rows[2].CampaignTemplateID != *tA {
		t.Fatalf("unexpected steps: %+v", rows)
	}
	if *rows[2].ID == *stepA || *rows[2].ID == *stepB {
		t.Fatal("a step id given with another template must be a new step")
	}
	for _, row := range rows {
		if row.Position < 0 {
			t.Fatalf("a position was left out of the way: %+v", row)
		}
	}
	// an empty list removes every step
	if err := r.ReplaceSteps(ctx, programID, []*model.ProgramStep{}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if rows := stepRows(t, db, programID); len(rows) != 0 {
		t.Fatalf("steps left: %d", len(rows))
	}
}

// TestProgramInsertWithChildren checks a program is stored with its steps,
// groups and settings, and nothing is stored when a child write fails
func TestProgramInsertWithChildren(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	template, group, filter, hook := ptrUUID(), ptrUUID(), ptrUUID(), uuid.New()
	program := &model.Program{Name: nullable.NewNullableWithValue(*vo.NewString64Must("with children"))}
	children := &ProgramChildren{
		Steps: []*model.ProgramStep{{
			CampaignTemplateID: nullable.NewNullableWithValue(*template),
		}},
		RecipientGroupIDs: []*uuid.UUID{group, group},
		AllowDenyIDs:      []*uuid.UUID{filter},
		Webhooks:          []*model.CampaignWebhook{{WebhookID: nullable.NewNullableWithValue(hook)}},
	}
	id, err := r.Insert(ctx, program, children)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	groups, _ := r.GetRecipientGroupIDs(ctx, id)
	filters, _ := r.GetAllowDenyIDs(ctx, id)
	hooks, _ := r.GetWebhooks(ctx, id)
	if len(stepRows(t, db, id)) != 1 || len(groups) != 1 || len(filters) != 1 || len(hooks) != 1 {
		t.Fatalf("children: groups %d filters %d hooks %d", len(groups), len(filters), len(hooks))
	}
	// a failing child write rolls the program row back
	if err := db.Migrator().DropTable(&database.ProgramWebhook{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	program.Name = nullable.NewNullableWithValue(*vo.NewString64Must("rolled back"))
	if _, err := r.Insert(ctx, program, children); err == nil {
		t.Fatal("expected the insert to fail")
	}
	var count int64
	db.Model(&database.Program{}).Where("name = ?", "rolled back").Count(&count)
	if count != 0 {
		t.Fatal("a program must not be stored without its children")
	}
	var stepCount int64
	db.Model(&database.ProgramStep{}).Count(&stepCount)
	if stepCount != 1 {
		t.Fatalf("steps of the failed insert must be rolled back, got %d", stepCount)
	}
}

// TestProgramCancelUnsent checks both cancels touch only rows that are not
// sent, set updated_at, and the recipient cancel works over more recipients
// than one batch holds
func TestProgramCancelUnsent(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "cancel", nil)
	campaignID := ptrUUID()
	createHiddenCampaign(t, db, campaignID, programID)
	old := time.Now().Add(-24 * time.Hour)
	recipients := []*uuid.UUID{}
	rows := []*uuid.UUID{}
	for i := 0; i < programEnrollmentBatch+5; i++ {
		rid := ptrUUID()
		recipients = append(recipients, rid)
		rows = append(rows, addCampaignRecipient(t, db, campaignID, rid, nil, i == 0))
	}
	db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignID).Update("updated_at", old)
	if err := r.CancelUnsentForProgramRecipients(ctx, programID, recipients); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	var cancelled int64
	db.Model(&database.CampaignRecipient{}).Where("cancelled_at IS NOT NULL").Count(&cancelled)
	if cancelled != int64(len(recipients)-1) {
		t.Fatalf("cancelled %d, want %d", cancelled, len(recipients)-1)
	}
	row := database.CampaignRecipient{}
	db.Where("id = ?", rows[1]).First(&row)
	if row.UpdatedAt == nil || !row.UpdatedAt.After(old.Add(time.Hour)) {
		t.Fatalf("updated_at must move on a cancel: %v", row.UpdatedAt)
	}
	row = database.CampaignRecipient{}
	db.Where("id = ?", rows[0]).First(&row)
	if row.CancelledAt != nil {
		t.Fatal("a sent row must not be cancelled")
	}

	// a send planned at or after the end is cancelled, one before it is kept
	other := ptrUUID()
	createHiddenCampaign(t, db, other, programID)
	end := time.Now().Add(48 * time.Hour)
	before := addCampaignRecipient(t, db, other, ptrUUID(), nil, false)
	after := addCampaignRecipient(t, db, other, ptrUUID(), nil, false)
	db.Model(&database.CampaignRecipient{}).Where("id = ?", before).Update("send_at", utils.RFC3339UTC(end.Add(-time.Hour)))
	db.Model(&database.CampaignRecipient{}).Where("id = ?", after).Update("send_at", utils.RFC3339UTC(end))
	if err := r.CancelUnsentFrom(ctx, programID, end); err != nil {
		t.Fatalf("cancel from: %v", err)
	}
	row = database.CampaignRecipient{}
	db.Where("id = ?", before).First(&row)
	if row.CancelledAt != nil {
		t.Fatal("a send before the end must stay")
	}
	row = database.CampaignRecipient{}
	db.Where("id = ?", after).First(&row)
	if row.CancelledAt == nil {
		t.Fatal("a send at the end must be cancelled")
	}
}

// TestProgramStopEnrollments checks active people stop and every next send is
// cleared, other people keep their status
func TestProgramStopEnrollments(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "stop", nil)
	due := time.Now().Add(time.Hour)
	ids := map[string]*uuid.UUID{}
	for _, status := range []string{model.ProgramEnrollmentActive, model.ProgramEnrollmentExited} {
		id := ptrUUID()
		ids[status] = id
		row := &database.ProgramEnrollment{
			ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			ProgramID: programID, RecipientID: ptrUUID(), Status: status, NextDueAt: &due,
		}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("enroll: %v", err)
		}
	}
	if err := r.StopEnrollments(ctx, programID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	var active, exited database.ProgramEnrollment
	db.Where("id = ?", ids[model.ProgramEnrollmentActive]).First(&active)
	db.Where("id = ?", ids[model.ProgramEnrollmentExited]).First(&exited)
	if active.Status != model.ProgramEnrollmentStopped || active.NextDueAt != nil {
		t.Fatalf("active person: %+v", active)
	}
	if exited.Status != model.ProgramEnrollmentExited || exited.NextDueAt != nil {
		t.Fatalf("exited person: %+v", exited)
	}
}

// TestProgramClearEnrollmentPseudonymsChunked checks the pseudonyms of a
// program are cleared in writes of at most ProgramWriteChunk rows, and the
// pseudonyms of another program stay
func TestProgramClearEnrollmentPseudonymsChunked(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "clear", nil)
	otherID := createProgram(t, db, "keep", nil)
	const n = 2*ProgramWriteChunk + 7
	rows := []*database.ProgramEnrollment{}
	for i := 0; i < n+1; i++ {
		pid := programID
		if i == n {
			pid = otherID
		}
		pseudonym := uuid.New()
		rows = append(rows, &database.ProgramEnrollment{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			ProgramID: pid, RecipientID: ptrUUID(), Status: model.ProgramEnrollmentStopped,
			AnonymizedID: &pseudonym,
		})
	}
	if err := db.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatalf("enroll: %v", err)
	}
	writes := []int64{}
	if err := db.Callback().Update().After("gorm:update").Register("test:count_clear", func(tx *gorm.DB) {
		if tx.Statement.Table == database.PROGRAM_ENROLLMENT_TABLE {
			writes = append(writes, tx.Statement.RowsAffected)
		}
	}); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if err := r.ClearEnrollmentPseudonyms(ctx, programID, false); err != nil {
		t.Fatalf("clear: %v", err)
	}
	_ = db.Callback().Update().Remove("test:count_clear")
	if len(writes) != 3 {
		t.Fatalf("writes = %v, want 3 chunks", writes)
	}
	for _, w := range writes {
		if w > ProgramWriteChunk {
			t.Fatalf("a write changed %d rows, more than %d", w, ProgramWriteChunk)
		}
	}
	var left, kept int64
	db.Model(&database.ProgramEnrollment{}).Where("program_id = ? AND anonymized_id IS NOT NULL", programID).Count(&left)
	db.Model(&database.ProgramEnrollment{}).Where("program_id = ? AND anonymized_id IS NOT NULL", otherID).Count(&kept)
	if left != 0 || kept != 1 {
		t.Fatalf("left %d pseudonyms, kept %d of the other program", left, kept)
	}
	// a second run finds nothing and is fine
	if err := r.ClearEnrollmentPseudonyms(ctx, programID, false); err != nil {
		t.Fatalf("clear again: %v", err)
	}
}

// TestProgramHeldSendsAndSetSendAt checks the held sends are the open, unsent
// and not cancelled ones, and a move of the send time skips sent rows
func TestProgramHeldSendsAndSetSendAt(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "held", nil)
	open, closed := ptrUUID(), ptrUUID()
	createHiddenCampaign(t, db, open, programID)
	createHiddenCampaign(t, db, closed, programID)
	db.Model(&database.Campaign{}).Where("id = ?", closed).Update("closed_at", time.Now())
	held := addCampaignRecipient(t, db, open, ptrUUID(), nil, false)
	sent := addCampaignRecipient(t, db, open, ptrUUID(), nil, true)
	cancelled := addCampaignRecipient(t, db, open, ptrUUID(), nil, false)
	db.Model(&database.CampaignRecipient{}).Where("id = ?", cancelled).Update("cancelled_at", utils.NowRFC3339UTC())
	addCampaignRecipient(t, db, closed, ptrUUID(), nil, false)
	got, err := r.GetHeldSends(ctx, programID)
	if err != nil || len(got) != 1 || got[0].ID != *held {
		t.Fatalf("held sends: %v %+v", err, got)
	}
	at := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	if err := r.SetSendAt(ctx, []*uuid.UUID{held, sent, nil}, at); err != nil {
		t.Fatalf("set send at: %v", err)
	}
	row := database.CampaignRecipient{}
	db.Where("id = ?", held).First(&row)
	if row.SendAt == nil || !row.SendAt.Equal(at) {
		t.Fatalf("held send moved to %v, want %v", row.SendAt, at)
	}
	row = database.CampaignRecipient{}
	db.Where("id = ?", sent).First(&row)
	if row.SendAt != nil {
		t.Fatal("a sent row must not be moved")
	}
}

// statusProgram stores a program with the given state
func statusProgram(t *testing.T, db *gorm.DB, name string, startAt, startedAt, pausedAt, closedAt *time.Time) *uuid.UUID {
	t.Helper()
	id := ptrUUID()
	row := &database.Program{
		ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: name,
		StartAt: startAt, StartedAt: startedAt, PausedAt: pausedAt, ClosedAt: closedAt,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create program: %v", err)
	}
	return id
}

// TestProgramStatusFilterAndCounts checks the list status filter and the
// dashboard counts use the same rules: a paused program that has not started
// is scheduled, a running program with a broken template or nobody active
// needs attention
func TestProgramStatusFilterAndCounts(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	now := time.Now().UTC()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	usable, broken := ptrUUID(), ptrUUID()
	for _, tpl := range []struct {
		id     *uuid.UUID
		usable bool
	}{{usable, true}, {broken, false}} {
		row := &database.CampaignTemplate{ID: tpl.id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "t " + tpl.id.String(), URLIdentifierID: ptrUUID(), IsUsable: tpl.usable}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("template: %v", err)
		}
	}
	want := map[string]*uuid.UUID{
		model.ProgramStatusDraft:          statusProgram(t, db, "draft", nil, nil, nil, nil),
		model.ProgramStatusScheduled:      statusProgram(t, db, "scheduled", &future, nil, &now, nil),
		model.ProgramStatusPaused:         statusProgram(t, db, "paused", &past, &past, &now, nil),
		model.ProgramStatusActive:         statusProgram(t, db, "active", &past, &past, nil, nil),
		model.ProgramStatusNeedsAttention: statusProgram(t, db, "attention", &past, &past, nil, nil),
		model.ProgramStatusClosed:         statusProgram(t, db, "closed", &past, &past, nil, &now),
	}
	createStep(t, db, want[model.ProgramStatusActive], usable, 0)
	createStep(t, db, want[model.ProgramStatusNeedsAttention], broken, 0)
	enrollment := &database.ProgramEnrollment{
		ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
		ProgramID: want[model.ProgramStatusActive], RecipientID: ptrUUID(), Status: model.ProgramEnrollmentActive,
		NextDueAt: &future,
	}
	if err := db.Create(enrollment).Error; err != nil {
		t.Fatalf("enroll: %v", err)
	}
	for status, id := range want {
		got, err := r.GetAll(ctx, nil, &ProgramOption{QueryArgs: &vo.QueryArgs{}, Status: status}, now)
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if len(got.Rows) != 1 || got.Rows[0].ID.MustGet() != *id {
			t.Fatalf("%s: got %d rows", status, len(got.Rows))
		}
	}
	if _, err := r.GetAll(ctx, nil, &ProgramOption{QueryArgs: &vo.QueryArgs{}, Status: "running"}, now); err == nil {
		t.Fatal("an unknown status must be refused")
	}
	counts, err := r.CountByStatus(ctx, nil, now)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts.Total != 6 || counts.Scheduled != 1 || counts.Paused != 1 || counts.Active != 1 || counts.NeedsAttention != 1 {
		t.Fatalf("counts: %+v", counts)
	}
	// the model status agrees with the filter
	scheduled, _ := r.GetByID(ctx, want[model.ProgramStatusScheduled], &ProgramOption{})
	if scheduled.StatusAt(now) != model.ProgramStatusScheduled {
		t.Fatalf("a paused program before its start reads %s", scheduled.StatusAt(now))
	}
}

// TestProgramCalendarUsesStartedAt checks a started program shows from its
// first enrollment, a scheduled one from its planned start
func TestProgramCalendarUsesStartedAt(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	now := time.Now().UTC().Truncate(time.Second)
	planned, started, future := now.Add(-48*time.Hour), now.Add(-47*time.Hour), now.Add(24*time.Hour)
	running := statusProgram(t, db, "running", &planned, &started, nil, nil)
	scheduled := statusProgram(t, db, "scheduled", &future, nil, nil, nil)
	statusProgram(t, db, "draft", nil, nil, nil, nil)
	got, err := r.GetCalendarEntries(ctx, nil, now.Add(-72*time.Hour), now.Add(72*time.Hour), &ProgramOption{QueryArgs: &vo.QueryArgs{}})
	if err != nil {
		t.Fatalf("calendar: %v", err)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("expected two entries, got %d", len(got.Rows))
	}
	for _, e := range got.Rows {
		e.SetStatus(now)
		switch e.ID {
		case *running:
			if !e.StartAt.Equal(started) || e.Status != model.ProgramStatusActive {
				t.Fatalf("running entry: %+v", e)
			}
		case *scheduled:
			if !e.StartAt.Equal(future) || e.Status != model.ProgramStatusScheduled {
				t.Fatalf("scheduled entry: %+v", e)
			}
		}
	}
}

// TestProgramClosedReleasesGroupsAndTemplates checks a closed program does not
// block deleting its group or template, and the removal helpers only touch
// closed programs
func TestProgramClosedReleasesGroupsAndTemplates(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	now := time.Now()
	past := now.Add(-time.Hour)
	open := statusProgram(t, db, "open", &past, &past, nil, nil)
	closed := statusProgram(t, db, "closed", &past, &past, nil, &now)
	group, template := ptrUUID(), ptrUUID()
	for _, id := range []*uuid.UUID{open, closed} {
		createStep(t, db, id, template, 0)
		if err := db.Create(&database.ProgramRecipientGroup{ProgramID: id, RecipientGroupID: group}).Error; err != nil {
			t.Fatalf("group: %v", err)
		}
	}
	names, _ := r.GetNamesByRecipientGroupID(ctx, group)
	if len(names) != 1 || names[0] != "open" {
		t.Fatalf("group users: %v", names)
	}
	names, _ = r.GetNamesByCampaignTemplateID(ctx, template)
	if len(names) != 1 || names[0] != "open" {
		t.Fatalf("template users: %v", names)
	}
	if err := r.RemoveRecipientGroupFromClosedPrograms(ctx, group); err != nil {
		t.Fatalf("remove group: %v", err)
	}
	if err := r.RemoveCampaignTemplateFromClosedPrograms(ctx, template); err != nil {
		t.Fatalf("remove template: %v", err)
	}
	if ids, _ := r.GetRecipientGroupIDs(ctx, closed); len(ids) != 0 {
		t.Fatal("the closed program must lose the group")
	}
	if ids, _ := r.GetRecipientGroupIDs(ctx, open); len(ids) != 1 {
		t.Fatal("the open program must keep the group")
	}
	if len(stepRows(t, db, closed)) != 0 || len(stepRows(t, db, open)) != 1 {
		t.Fatal("only the closed program loses the step")
	}
}

// TestProgramAllowedColumnsExist checks every allowlisted sort column of the
// program list and the people table exists, and a column outside the list is
// refused
func TestProgramAllowedColumnsExist(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "columns", nil)
	for _, col := range allowedProgramColumns {
		bare := strings.Trim(col[strings.LastIndex(col, ".")+1:], "`")
		if _, err := r.GetAll(ctx, nil, &ProgramOption{QueryArgs: &vo.QueryArgs{OrderBy: bare, Search: "x"}}, time.Now()); err != nil {
			t.Fatalf("order by %s: %v", bare, err)
		}
	}
	if _, err := r.GetAll(ctx, nil, &ProgramOption{QueryArgs: &vo.QueryArgs{OrderBy: "is_anonymous"}}, time.Now()); err == nil {
		t.Fatal("order by a column outside the allowlist must fail")
	}
	if err := db.AutoMigrate(&database.Recipient{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, col := range ProgramEnrollmentSortColumns {
		if _, _, err := r.GetEnrollmentPage(ctx, programID, &vo.QueryArgs{OrderBy: col, Search: "x"}); err != nil {
			t.Fatalf("people order by %s: %v", col, err)
		}
	}
	if _, _, err := r.GetEnrollmentPage(ctx, programID, &vo.QueryArgs{OrderBy: "`program_enrollments`.`anonymized_id`"}); err == nil {
		t.Fatal("people order by a column outside the allowlist must fail")
	}
}

// TestProgramListAllCompanies checks no company lists only the programs
// without a company, unless AllCompanies is set, and that WithCompany loads
// the company of each program
func TestProgramListAllCompanies(t *testing.T) {
	db := newProgramTestDB(t)
	if err := db.AutoMigrate(&database.Company{}); err != nil {
		t.Fatalf("migrate company: %v", err)
	}
	ctx := context.Background()
	r := &Program{DB: db}
	company := &database.Company{ID: uuid.New(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "acme", AssetsKey: "acme"}
	if err := db.Create(company).Error; err != nil {
		t.Fatalf("company: %v", err)
	}
	statusProgram(t, db, "shared", nil, nil, nil, nil)
	owned := statusProgram(t, db, "owned", nil, nil, nil, nil)
	if err := db.Model(&database.Program{}).Where("id = ?", owned).Update("company_id", company.ID).Error; err != nil {
		t.Fatalf("set company: %v", err)
	}
	now := time.Now()
	got, err := r.GetAll(ctx, nil, &ProgramOption{QueryArgs: &vo.QueryArgs{}}, now)
	if err != nil || len(got.Rows) != 1 || got.Rows[0].Name.MustGet().String() != "shared" {
		t.Fatalf("no company: %v %d", err, len(got.Rows))
	}
	got, err = r.GetAll(ctx, nil, &ProgramOption{QueryArgs: &vo.QueryArgs{}, AllCompanies: true, WithCompany: true}, now)
	if err != nil || len(got.Rows) != 2 {
		t.Fatalf("all companies: %v", err)
	}
	for _, p := range got.Rows {
		if p.ID.MustGet() == *owned && (p.Company == nil || p.Company.Name.MustGet().String() != "acme") {
			t.Fatalf("company not loaded")
		}
	}
	// a given company is never widened by AllCompanies
	got, err = r.GetAll(ctx, &company.ID, &ProgramOption{QueryArgs: &vo.QueryArgs{}, AllCompanies: true}, now)
	if err != nil || len(got.Rows) != 1 || got.Rows[0].ID.MustGet() != *owned {
		t.Fatalf("one company: %v", err)
	}
}
