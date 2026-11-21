package repository

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
)

// storedEnrollment is an enrollment row as sqlite stores it, as text
type storedEnrollment struct {
	RoundStartAt *string
	NextDueAt    *string
	Status       string
	ExitedAt     *string
	MissingSince *string
	CompletedAt  *string
}

func readStoredEnrollment(t *testing.T, db *gorm.DB, id *uuid.UUID) storedEnrollment {
	t.Helper()
	row := storedEnrollment{}
	if err := db.Raw(
		"SELECT CAST(round_start_at AS TEXT) AS round_start_at, CAST(next_due_at AS TEXT) AS next_due_at, status, "+
			"CAST(exited_at AS TEXT) AS exited_at, CAST(missing_since AS TEXT) AS missing_since, "+
			"CAST(completed_at AS TEXT) AS completed_at FROM program_enrollments WHERE id = ?", id,
	).Scan(&row).Error; err != nil {
		t.Fatalf("read enrollment: %v", err)
	}
	return row
}

func (e storedEnrollment) text() string {
	return str(e.RoundStartAt) + "|" + str(e.NextDueAt) + "|" + e.Status + "|" + str(e.ExitedAt) + "|" +
		str(e.MissingSince) + "|" + str(e.CompletedAt)
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// TestPlanEnrollmentsMatchesRowUpdates checks the batched plan write stores the
// same values as one update per person, over more than one batch
func TestPlanEnrollmentsMatchesRowUpdates(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "plan", nil)
	const n = 2*programEnrollmentBatch + 50
	base := time.Date(2026, 3, 29, 0, 30, 0, 0, time.FixedZone("x", 2*3600))
	exited := time.Now().Add(-time.Hour)
	batched := []*database.ProgramEnrollment{}
	single := []*database.ProgramEnrollment{}
	for i := 0; i < 2*n+1; i++ {
		rid := ptrUUID()
		e := &database.ProgramEnrollment{
			ProgramID: programID, RecipientID: rid,
			Status: model.ProgramEnrollmentExited, ExitedAt: &exited, MissingSince: &exited,
		}
		switch {
		case i < n:
			batched = append(batched, e)
		case i < 2*n:
			single = append(single, e)
		}
		if err := r.InsertEnrollments(ctx, []*database.ProgramEnrollment{e}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	untouchedBefore := readStoredEnrollment(t, db, func() *uuid.UUID {
		var e database.ProgramEnrollment
		db.Order("created_at desc").First(&e)
		return e.ID
	}())
	plans := []EnrollmentPlan{}
	for i, e := range batched {
		round := base.Add(time.Duration(i) * 37 * time.Minute)
		due := round.Add(time.Duration(i)*time.Second + 123456789*time.Nanosecond)
		plans = append(plans, EnrollmentPlan{ID: e.ID, RoundStartAt: round, NextDueAt: due})
		if err := r.UpdateEnrollmentFields(ctx, single[i].ID, map[string]any{
			"status":         model.ProgramEnrollmentActive,
			"exited_at":      nil,
			"missing_since":  nil,
			"round_start_at": round,
			"next_due_at":    due,
		}); err != nil {
			t.Fatalf("row update: %v", err)
		}
	}
	if err := r.PlanEnrollments(ctx, plans, map[string]any{
		"status":        model.ProgramEnrollmentActive,
		"exited_at":     nil,
		"missing_since": nil,
	}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	for i := range batched {
		got := readStoredEnrollment(t, db, batched[i].ID).text()
		want := readStoredEnrollment(t, db, single[i].ID).text()
		if got != want {
			t.Fatalf("row %d: batched %s, per row %s", i, got, want)
		}
	}
	// a row not in the plan is not changed
	var last database.ProgramEnrollment
	db.Order("created_at desc").First(&last)
	if after := readStoredEnrollment(t, db, last.ID).text(); after != untouchedBefore.text() {
		t.Fatalf("a row outside the plan changed: %s", after)
	}
	// no shared fields only moves the times
	if err := r.PlanEnrollments(ctx, plans[:1], nil); err != nil {
		t.Fatalf("plan without shared fields: %v", err)
	}
	if err := r.PlanEnrollments(ctx, nil, nil); err != nil {
		t.Fatalf("empty plan: %v", err)
	}
}

// TestMoveSendsMatchesSetSendAt checks the batched send move stores the same
// text as the move of one send time, and skips sent rows
func TestMoveSendsMatchesSetSendAt(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "move", nil)
	campaignID := ptrUUID()
	createHiddenCampaign(t, db, campaignID, programID)
	sendAt := func(id *uuid.UUID) string {
		var v *string
		db.Raw("SELECT CAST(send_at AS TEXT) FROM campaign_recipients WHERE id = ?", id).Scan(&v)
		return str(v)
	}
	moves := []SendMove{}
	twins := []*uuid.UUID{}
	for i := 0; i < programEnrollmentBatch+20; i++ {
		a := addCampaignRecipient(t, db, campaignID, ptrUUID(), nil, false)
		b := addCampaignRecipient(t, db, campaignID, ptrUUID(), nil, false)
		at := time.Now().Add(time.Duration(i)*time.Hour + 5*time.Millisecond)
		moves = append(moves, SendMove{ID: *a, SendAt: at})
		twins = append(twins, b)
		if err := r.SetSendAt(ctx, []*uuid.UUID{b}, at); err != nil {
			t.Fatalf("set send at: %v", err)
		}
	}
	sent := addCampaignRecipient(t, db, campaignID, ptrUUID(), nil, true)
	moves = append(moves, SendMove{ID: *sent, SendAt: time.Now()})
	if err := r.MoveSends(ctx, moves); err != nil {
		t.Fatalf("move sends: %v", err)
	}
	for i, b := range twins {
		got, want := sendAt(&moves[i].ID), sendAt(b)
		if got != want || got == "<nil>" {
			t.Fatalf("row %d: moved %s, set %s", i, got, want)
		}
	}
	if v := sendAt(sent); v != "<nil>" {
		t.Fatalf("a sent row must not be moved, got %s", v)
	}
}

// deliveryKeys turns deliveries into sorted comparable keys
func deliveryKeys(ds []ProgramDelivery) []string {
	out := []string{}
	for _, d := range ds {
		k := d.RecipientID.String() + "|" + d.StepID.String() + "|" + string(rune('0'+d.LoopIndex)) + "|"
		if d.TemplateID != nil {
			k += d.TemplateID.String()
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestPeopleSendsMatchesPersonReads checks the sends read for a batch answer
// the same as the reads made for each person
func TestPeopleSendsMatchesPersonReads(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "people", nil)
	otherProgram := createProgram(t, db, "other", nil)
	t1, t2 := ptrUUID(), ptrUUID()
	s1 := createStep(t, db, programID, t1, 0)
	s2 := createStep(t, db, programID, t2, 1)
	removedStep := ptrUUID()
	mkCampaign := func(step *uuid.UUID, loop int, template *uuid.UUID, closed bool) *uuid.UUID {
		id := linkCampaign(t, db, programID, step, loop)
		createHiddenCampaign(t, db, id, programID)
		db.Model(&database.Campaign{}).Where("id = ?", id).Update("campaign_template_id", template)
		if closed {
			db.Model(&database.Campaign{}).Where("id = ?", id).Update("closed_at", time.Now())
		}
		return id
	}
	c1 := mkCampaign(s1, 0, t1, false)
	c2 := mkCampaign(s2, 0, t2, true)
	c3 := mkCampaign(s1, 1, t1, false)
	c4 := mkCampaign(removedStep, 0, t2, false)
	// a campaign of another program is never read
	foreign := ptrUUID()
	createHiddenCampaign(t, db, foreign, otherProgram)

	named, anonRecipient, stripped, nobody := ptrUUID(), ptrUUID(), ptrUUID(), ptrUUID()
	anon, strippedAnon := ptrUUID(), ptrUUID()
	setSendAt := func(id *uuid.UUID, at time.Time) {
		db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("send_at", utils.RFC3339UTC(at))
	}
	cancel := func(id *uuid.UUID) {
		db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("cancelled_at", utils.NowRFC3339UTC())
	}
	now := time.Now()
	// named: sent in c1, waiting in closed c2, waiting in removed step c4
	setSendAt(addCampaignRecipient(t, db, c1, named, nil, true), now.Add(-48*time.Hour))
	setSendAt(addCampaignRecipient(t, db, c2, named, nil, false), now.Add(-24*time.Hour))
	setSendAt(addCampaignRecipient(t, db, c4, named, nil, false), now.Add(-30*time.Hour))
	setSendAt(addCampaignRecipient(t, db, foreign, named, nil, false), now.Add(time.Hour))
	// anonymous with link: sent in c1, cancelled newer one in c3
	setSendAt(addCampaignRecipient(t, db, c1, anonRecipient, anon, true), now.Add(-72*time.Hour))
	late := addCampaignRecipient(t, db, c3, anonRecipient, anon, false)
	setSendAt(late, now.Add(time.Hour))
	cancel(late)
	// anonymous with the link removed: only the pseudonym is left
	setSendAt(addCampaignRecipient(t, db, c1, nil, strippedAnon, true), now.Add(-96*time.Hour))
	setSendAt(addCampaignRecipient(t, db, c3, nil, strippedAnon, false), now.Add(-10*time.Hour))

	people := []ProgramPerson{
		{RecipientID: named},
		{RecipientID: anonRecipient, AnonymizedID: anon},
		{RecipientID: stripped, AnonymizedID: strippedAnon},
		{RecipientID: nobody},
		{AnonymizedID: strippedAnon},
	}
	batch, err := r.GetPeopleSends(ctx, programID, people)
	if err != nil {
		t.Fatalf("people sends: %v", err)
	}
	for i, p := range people {
		for loop := 0; loop < 2; loop++ {
			want, err := r.GetLoopDeliveries(ctx, programID, p.RecipientID, p.AnonymizedID, loop)
			if err != nil {
				t.Fatalf("loop deliveries: %v", err)
			}
			got := batch.LoopDeliveries(p, loop)
			gk, wk := deliveryKeys(got), deliveryKeys(want)
			if len(gk) != len(wk) {
				t.Fatalf("person %d loop %d: batch %v, read %v", i, loop, gk, wk)
			}
			for j := range gk {
				if gk[j] != wk[j] {
					t.Fatalf("person %d loop %d: batch %v, read %v", i, loop, gk, wk)
				}
			}
		}
		for _, c := range []*uuid.UUID{c1, c2, c3, c4, foreign} {
			recipientID := p.RecipientID
			want, err := r.GetPersonSend(ctx, c, recipientID, p.AnonymizedID)
			if err != nil {
				t.Fatalf("person send: %v", err)
			}
			got := batch.PersonSend(*c, p)
			if *c == *foreign {
				// the batch only knows the program's own campaigns, which is all
				// the scheduler asks for
				continue
			}
			if (got == nil) != (want == nil) || (got != nil && (got.ID != want.ID ||
				(got.SentAt == nil) != (want.SentAt == nil) || (got.CancelledAt == nil) != (want.CancelledAt == nil))) {
				t.Fatalf("person %d campaign %s: batch %+v, read %+v", i, c, got, want)
			}
		}
		want, err := r.GetNewestScheduledSend(ctx, programID, p.RecipientID, p.AnonymizedID)
		if err != nil {
			t.Fatalf("newest: %v", err)
		}
		got := batch.NewestScheduledSend(p)
		if (got == nil) != (want == nil) || (got != nil && (!got.SendAt.Equal(want.SendAt) || got.StepID != want.StepID)) {
			t.Fatalf("person %d newest: batch %+v, read %+v", i, got, want)
		}
	}
	// nobody to read still answers
	empty, err := r.GetPeopleSends(ctx, programID, nil)
	if err != nil || empty.NewestScheduledSend(ProgramPerson{RecipientID: named}) != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

// TestProgramWritesCommitPerChunk checks the enrollment insert and plan write
// commit every ProgramWriteChunk rows on their own: all rows over a chunk
// boundary are stored, and a failure in a later chunk keeps the earlier ones
func TestProgramWritesCommitPerChunk(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "chunks", nil)
	const n = ProgramWriteChunk + 1
	// the first row of the second chunk is refused
	if err := db.Exec(`CREATE TRIGGER refuse_insert BEFORE INSERT ON program_enrollments
		WHEN NEW.status = 'refused' BEGIN SELECT RAISE(ABORT, 'insert refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	rows := make([]*database.ProgramEnrollment, 0, n)
	for i := 0; i < n; i++ {
		status := model.ProgramEnrollmentActive
		if i == ProgramWriteChunk {
			status = "refused"
		}
		rows = append(rows, &database.ProgramEnrollment{ProgramID: programID, RecipientID: ptrUUID(), Status: status})
	}
	if err := r.InsertEnrollments(ctx, rows); err == nil {
		t.Fatalf("expected the second chunk to fail")
	}
	count, err := r.CountEnrollments(ctx, programID)
	if err != nil || count != ProgramWriteChunk {
		t.Fatalf("the first chunk must stay stored: %d %v", count, err)
	}
	if err := db.Exec(`DROP TRIGGER refuse_insert`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	// a retry of the rest stores it
	rows[ProgramWriteChunk].Status = model.ProgramEnrollmentActive
	if err := r.InsertEnrollments(ctx, rows[ProgramWriteChunk:]); err != nil {
		t.Fatalf("insert the rest: %v", err)
	}
	if count, err = r.CountEnrollments(ctx, programID); err != nil || count != n {
		t.Fatalf("expected every row stored: %d %v", count, err)
	}

	// the plan write over the boundary: the last row is refused, the first
	// chunk keeps its new plan and the last row its old one
	if err := db.Exec(`CREATE TRIGGER refuse_plan BEFORE UPDATE ON program_enrollments
		WHEN OLD.position = 7 BEGIN SELECT RAISE(ABORT, 'plan refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	// the trigger refuses updates of the row only once it holds 7
	if err := r.UpdateEnrollmentFields(ctx, rows[n-1].ID, map[string]any{"position": 7}); err != nil {
		t.Fatalf("set position: %v", err)
	}
	base := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	plans := make([]EnrollmentPlan, 0, n)
	for _, e := range rows {
		plans = append(plans, EnrollmentPlan{ID: e.ID, RoundStartAt: base, NextDueAt: base})
	}
	if err := r.PlanEnrollments(ctx, plans, nil); err == nil {
		t.Fatalf("expected the second plan chunk to fail")
	}
	want := readStoredEnrollment(t, db, rows[0].ID).NextDueAt
	if want == nil {
		t.Fatalf("the first chunk must keep its plan")
	}
	if got := readStoredEnrollment(t, db, rows[ProgramWriteChunk-1].ID).NextDueAt; got == nil || *got != *want {
		t.Fatalf("the last row of the first chunk must keep its plan: %v", got)
	}
	if got := readStoredEnrollment(t, db, rows[n-1].ID).NextDueAt; got != nil {
		t.Fatalf("the refused row must keep its old plan, got %s", *got)
	}
}
