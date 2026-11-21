package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
)

// TestProgramAttentionCompletedPeople checks a running program whose people all
// completed is active, not in need of attention, while a started program with
// nobody enrolled or only people who left needs attention. The list filter,
// the dashboard counts and the list summary agree.
func TestProgramAttentionCompletedPeople(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	template := ptrUUID()
	if err := db.Create(&database.CampaignTemplate{ID: template, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "t", URLIdentifierID: ptrUUID(), IsUsable: true}).Error; err != nil {
		t.Fatalf("template: %v", err)
	}
	completed := statusProgram(t, db, "completed", &past, &past, nil, nil)
	empty := statusProgram(t, db, "empty", &past, &past, nil, nil)
	left := statusProgram(t, db, "left", &past, &past, nil, nil)
	for _, id := range []*uuid.UUID{completed, empty, left} {
		createStep(t, db, id, template, 0)
	}
	enroll := func(programID *uuid.UUID, status string) {
		e := &database.ProgramEnrollment{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			ProgramID: programID, RecipientID: ptrUUID(), Status: status,
		}
		if err := db.Create(e).Error; err != nil {
			t.Fatalf("enroll: %v", err)
		}
	}
	enroll(completed, model.ProgramEnrollmentCompleted)
	enroll(completed, model.ProgramEnrollmentCompleted)
	enroll(completed, model.ProgramEnrollmentExited)
	enroll(left, model.ProgramEnrollmentExited)

	ids := func(status string) map[uuid.UUID]bool {
		got, err := r.GetAll(ctx, nil, &ProgramOption{QueryArgs: &vo.QueryArgs{}, Status: status}, now)
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		out := map[uuid.UUID]bool{}
		for _, p := range got.Rows {
			out[p.ID.MustGet()] = true
		}
		return out
	}
	active, attention := ids(model.ProgramStatusActive), ids(model.ProgramStatusNeedsAttention)
	if !active[*completed] || attention[*completed] {
		t.Fatal("a program whose people all completed must be active")
	}
	if active[*empty] || !attention[*empty] || active[*left] || !attention[*left] {
		t.Fatal("a started program with nobody enrolled or only leavers must need attention")
	}
	counts, err := r.CountByStatus(ctx, nil, now)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts.Active != 1 || counts.NeedsAttention != 2 {
		t.Fatalf("counts: %+v", counts)
	}
	sums, err := r.GetListSummaries(ctx, []*uuid.UUID{completed, empty, left})
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if sums[*completed].Enrolled != 0 || sums[*completed].People != 2 || sums[*empty].People != 0 || sums[*left].People != 0 {
		t.Fatalf("summaries: completed %+v empty %+v left %+v", sums[*completed], sums[*empty], sums[*left])
	}
}

// TestPeopleSendsLoopWindow checks a read limited to the loops around each
// person answers the same as a read of every loop, also over more people than
// one read names
func TestPeopleSendsLoopWindow(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "loops", nil)
	step := createStep(t, db, programID, ptrUUID(), 0)
	campaigns := []*uuid.UUID{}
	for loop := 0; loop < 6; loop++ {
		id := linkCampaign(t, db, programID, step, loop)
		createHiddenCampaign(t, db, id, programID)
		campaigns = append(campaigns, id)
	}
	now := time.Now()
	people := []ProgramPerson{}
	all := []ProgramPerson{}
	for i := 0; i < programPeopleReadChunk*2+3; i++ {
		rid := ptrUUID()
		// sent in loops 0 to 4, the send of loop 4 is the newest
		for loop := 0; loop <= 4; loop++ {
			id := addCampaignRecipient(t, db, campaigns[loop], rid, nil, loop < 4)
			db.Model(&database.CampaignRecipient{}).Where("id = ?", id).
				Update("send_at", utils.RFC3339UTC(now.Add(time.Duration(loop-5)*time.Hour)))
		}
		loop := 4
		people = append(people, ProgramPerson{RecipientID: rid, Loop: &loop})
		all = append(all, ProgramPerson{RecipientID: rid})
	}
	window, err := r.GetPeopleSends(ctx, programID, people)
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	full, err := r.GetPeopleSends(ctx, programID, all)
	if err != nil {
		t.Fatalf("full: %v", err)
	}
	for i, p := range people {
		if len(window.LoopDeliveries(p, 4)) != 1 || len(full.LoopDeliveries(all[i], 4)) != 1 {
			t.Fatalf("person %d: loop 4 deliveries differ", i)
		}
		for _, loop := range []int{4, 5} {
			got, want := window.PersonSend(*campaigns[loop], p), full.PersonSend(*campaigns[loop], all[i])
			if (got == nil) != (want == nil) || (got != nil && got.ID != want.ID) {
				t.Fatalf("person %d loop %d: window %+v, full %+v", i, loop, got, want)
			}
		}
		got, want := window.NewestScheduledSend(p), full.NewestScheduledSend(all[i])
		if got == nil || want == nil || !got.SendAt.Equal(want.SendAt) || got.StepID != want.StepID {
			t.Fatalf("person %d newest: window %+v, full %+v", i, got, want)
		}
	}
	// the window leaves out the old loops
	if n := len(window.rowsOf(people[0])); n != 2 {
		t.Fatalf("expected the sends of loops 3 and 4, got %d", n)
	}
}

// TestCancelUnsentOverChunks checks the program wide cancels reach every
// unsent row over more than one write and leave sent rows and rows before the
// given time alone
func TestCancelUnsentOverChunks(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "cancel", nil)
	campaign := ptrUUID()
	createHiddenCampaign(t, db, campaign, programID)
	now := time.Now()
	n := ProgramWriteChunk + 7
	early := []*uuid.UUID{}
	for i := 0; i < n; i++ {
		id := addCampaignRecipient(t, db, campaign, ptrUUID(), nil, false)
		at := now.Add(time.Hour)
		if i%2 == 0 {
			at = now.Add(-time.Hour)
			early = append(early, id)
		}
		db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("send_at", utils.RFC3339UTC(at))
	}
	sent := addCampaignRecipient(t, db, campaign, ptrUUID(), nil, true)
	count := func() int64 {
		var c int64
		db.Model(&database.CampaignRecipient{}).Where("campaign_id = ? AND cancelled_at IS NOT NULL", campaign).Count(&c)
		return c
	}
	if err := r.CancelUnsentFrom(ctx, programID, now); err != nil {
		t.Fatalf("cancel from: %v", err)
	}
	if got := count(); got != int64(n-len(early)) {
		t.Fatalf("expected %d cancelled from now, got %d", n-len(early), got)
	}
	if err := r.CancelAllUnsent(ctx, programID); err != nil {
		t.Fatalf("cancel all: %v", err)
	}
	if got := count(); got != int64(n) {
		t.Fatalf("expected %d cancelled, got %d", n, got)
	}
	var row database.CampaignRecipient
	db.Where("id = ?", sent).First(&row)
	if row.CancelledAt != nil {
		t.Fatal("a sent row must not be cancelled")
	}
}

// TestInsertEnrollmentsSkipsEnrolled checks a list planned from an older read
// stores the new people and skips a person enrolled in between
func TestInsertEnrollmentsSkipsEnrolled(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "insert", nil)
	enrolled, joiner := ptrUUID(), ptrUUID()
	first := &database.ProgramEnrollment{ProgramID: programID, RecipientID: enrolled, Status: model.ProgramEnrollmentCompleted}
	if err := r.InsertEnrollments(ctx, []*database.ProgramEnrollment{first}); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	stale := []*database.ProgramEnrollment{
		{ProgramID: programID, RecipientID: enrolled, Status: model.ProgramEnrollmentActive},
		{ProgramID: programID, RecipientID: joiner, Status: model.ProgramEnrollmentActive},
	}
	if err := r.InsertEnrollments(ctx, stale); err != nil {
		t.Fatalf("stale insert: %v", err)
	}
	rows, err := r.GetEnrollmentPlansByProgramID(ctx, programID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("expected two enrollments, got %d %v", len(rows), err)
	}
	for _, e := range rows {
		if *e.RecipientID == *enrolled && e.Status != model.ProgramEnrollmentCompleted {
			t.Fatal("the enrolled person must keep their enrollment")
		}
	}
}

// TestEnrollmentStatusGuards checks the guarded writes change a person only in
// the status they were planned from
func TestEnrollmentStatusGuards(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "guards", nil)
	active := &database.ProgramEnrollment{ProgramID: programID, RecipientID: ptrUUID(), Status: model.ProgramEnrollmentActive}
	done := &database.ProgramEnrollment{ProgramID: programID, RecipientID: ptrUUID(), Status: model.ProgramEnrollmentCompleted}
	if err := r.InsertEnrollments(ctx, []*database.ProgramEnrollment{active, done}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := r.UpdateEnrollmentsFields(ctx, []*uuid.UUID{active.ID, done.ID}, map[string]any{
		"status": model.ProgramEnrollmentExited,
	}, model.ProgramEnrollmentActive); err != nil {
		t.Fatalf("update: %v", err)
	}
	exited, err := r.GetRecipientsWithStatus(ctx, programID, []*uuid.UUID{active.RecipientID, done.RecipientID}, model.ProgramEnrollmentExited)
	if err != nil || len(exited) != 1 || *exited[0] != *active.RecipientID {
		t.Fatalf("expected only the active person exited, got %v %v", exited, err)
	}
	if err := r.PlanEnrollments(ctx, []EnrollmentPlan{{ID: done.ID, RoundStartAt: time.Now(), NextDueAt: time.Now()}},
		map[string]any{"status": model.ProgramEnrollmentActive}, model.ProgramEnrollmentExited); err != nil {
		t.Fatalf("plan: %v", err)
	}
	var row database.ProgramEnrollment
	db.Where("id = ?", done.ID).First(&row)
	if row.Status != model.ProgramEnrollmentCompleted || row.NextDueAt != nil {
		t.Fatalf("a completed person must not be planned as a returning one, got %s %v", row.Status, row.NextDueAt)
	}
}

// TestProgramEnrollmentMigrateCreatesIndexes checks migrate creates the
// enrollment indexes and the unsent indexes of the program sends
func TestProgramEnrollmentMigrateCreatesIndexes(t *testing.T) {
	db := newProgramTestDB(t)
	if err := (database.ProgramEnrollment{}).Migrate(db); err != nil {
		t.Fatalf("migrate enrollments: %v", err)
	}
	if err := (database.CampaignRecipient{}).Migrate(db); err != nil {
		t.Fatalf("migrate sends: %v", err)
	}
	has := func(name string) bool {
		var n int64
		db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&n)
		return n > 0
	}
	for _, name := range []string{
		"idx_program_enrollments_status_due",
		"idx_program_enrollments_program_status_due",
		"idx_program_enrollments_program_recipient",
		"idx_campaign_recipients_unsent_campaign",
		"idx_campaign_recipients_campaign_send_at",
	} {
		if !has(name) {
			t.Fatalf("%s must exist", name)
		}
	}
	// a second run changes nothing
	if err := (database.ProgramEnrollment{}).Migrate(db); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
}
