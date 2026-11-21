package service

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
)

// TestResolveMembershipLargeGroup checks a group with more members than sqlite
// takes as bound parameters in one query is resolved and enrolled, and the
// SCIM disabled, deleted and other company rules of static and dynamic groups
// hold
func TestResolveMembershipLargeGroup(t *testing.T) {
	p := newProgramLifecycleTest(t)
	now := time.Now().UTC()
	const members = 33000
	staticID, dynamicID := uuid.New(), uuid.New()
	p.create(&database.RecipientGroup{ID: &staticID, CreatedAt: &now, UpdatedAt: &now, Name: "static"})
	p.create(&database.RecipientGroup{
		ID: &dynamicID, CreatedAt: &now, UpdatedAt: &now, Name: "dynamic",
		IsDynamic: true, FilterField: "department", FilterValue: "sales",
	})
	recipients := make([]*database.Recipient, 0, members)
	links := make([]*database.RecipientGroupRecipient, 0, members)
	for i := 0; i < members; i++ {
		id := uuid.New()
		email := fmt.Sprintf("m%d@example.com", i)
		r := &database.Recipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, Email: &email}
		// the first ten are also in the dynamic group, so they are counted once
		if i < 10 {
			r.Department = "sales"
		}
		// two static members are SCIM disabled
		if i == 20 || i == 21 {
			r.ScimSoftDeletedAt = &now
		}
		recipients = append(recipients, r)
		links = append(links, &database.RecipientGroupRecipient{RecipientID: &id, RecipientGroupID: &staticID})
	}
	if err := p.db.CreateInBatches(recipients, 500).Error; err != nil {
		t.Fatalf("recipients: %v", err)
	}
	if err := p.db.CreateInBatches(links, 500).Error; err != nil {
		t.Fatalf("group links: %v", err)
	}
	// dynamic only members: one counted, one deleted, one SCIM disabled and one
	// of another company are left out
	extra := func(email string, deleted, scim bool, company *uuid.UUID) uuid.UUID {
		id := uuid.New()
		r := &database.Recipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, Email: &email, Department: "sales", CompanyID: company}
		if deleted {
			r.DeletedAt = &now
		}
		if scim {
			r.ScimSoftDeletedAt = &now
		}
		p.create(r)
		return id
	}
	kept := extra("kept@example.com", false, false, nil)
	extra("deleted@example.com", true, false, nil)
	extra("scim@example.com", false, true, nil)
	other := uuid.New()
	extra("other@example.com", false, false, &other)

	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	program := p.loadProgram(programID)
	program.RecipientGroupIDs = nullable.NewNullableWithValue([]*uuid.UUID{&staticID, &dynamicID})
	p.assignGroup(programID, staticID)
	p.assignGroup(programID, dynamicID)
	got, err := p.svc.resolveMembership(p.ctx, program)
	if err != nil {
		t.Fatalf("resolve membership: %v", err)
	}
	want := members - 2 + 1
	if len(got) != want {
		t.Fatalf("expected %d members, got %d", want, len(got))
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range got {
		if seen[*id] {
			t.Fatalf("member %s returned twice", id)
		}
		seen[*id] = true
	}
	if !seen[kept] || seen[*recipients[20].ID] {
		t.Fatal("the dynamic member must be in and the SCIM disabled member out")
	}

	// the whole group is enrolled through the batched writes
	if err := p.svc.resolveListedProgramWith(p.ctx, program, got); err != nil {
		t.Fatalf("apply membership: %v", err)
	}
	if n, err := p.svc.ProgramRepository.CountActiveEnrollments(p.ctx, &programID); err != nil || n != int64(want) {
		t.Fatalf("expected %d enrolled, got %d %v", want, n, err)
	}
	// a second pass changes nothing, and a member who left is marked missing
	got = got[1:]
	if err := p.svc.resolveListedProgramWith(p.ctx, program, got); err != nil {
		t.Fatalf("apply membership again: %v", err)
	}
	var missing int64
	p.db.Model(&database.ProgramEnrollment{}).Where("program_id = ? AND missing_since IS NOT NULL", programID).Count(&missing)
	if n, _ := p.svc.ProgramRepository.CountEnrollments(p.ctx, &programID); n != int64(want) || missing != 1 {
		t.Fatalf("expected %d enrollments with one missing, got %d and %d", want, n, missing)
	}
}

// batchOutcome is what an advance leaves for one person, without the random
// times and ids
type batchOutcome struct {
	status   string
	position int
	loop     int
	failures int
	lastStep int
	sends    string
}

// advanceTwin builds the same people in a fresh database and returns them by
// name with the outcome reader
func advanceTwin(t *testing.T) (*programTest, uuid.UUID, map[string]*database.ProgramEnrollment, func(string) batchOutcome) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	steps := []uuid.UUID{p.step(programID, 0, true), p.step(programID, 1, true), p.step(programID, 2, true)}
	campaigns := []uuid.UUID{}
	for _, st := range steps {
		campaigns = append(campaigns, p.stepCampaign(programID, st, 0))
	}
	people := map[string]*database.ProgramEnrollment{}
	recipients := map[string]uuid.UUID{}
	for _, name := range []string{"fresh", "second", "revive", "failed", "last", "done"} {
		rid := p.recipient(name + "@example.com")
		recipients[name] = rid
		people[name] = p.enroll(programID, rid, 0)
	}
	// second had step one
	p.send(campaigns[0], recipients["second"], true)
	// revive had step one and a cancelled step two, which is put back in the queue
	p.send(campaigns[0], recipients["revive"], true)
	cancelled := p.send(campaigns[1], recipients["revive"], false)
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", cancelled).Update("cancelled_at", time.Now().UTC())
	// failed failed twice before
	p.setEnrollment(people["failed"].ID, map[string]any{"advance_failures": 2})
	// last had two steps, the third completes them
	p.send(campaigns[0], recipients["last"], true)
	p.send(campaigns[1], recipients["last"], true)
	// done had every step, so they are completed without a send
	for _, c := range campaigns {
		p.send(c, recipients["done"], true)
	}
	for name, e := range people {
		row := p.enrollment(e.ID)
		people[name] = &row
	}
	stepIndex := map[uuid.UUID]int{}
	for i, st := range steps {
		stepIndex[st] = i
	}
	outcome := func(name string) batchOutcome {
		row := p.enrollment(people[name].ID)
		o := batchOutcome{status: row.Status, position: row.Position, loop: row.LoopCount, failures: row.AdvanceFailures, lastStep: -1}
		if row.LastStepID != nil {
			o.lastStep = stepIndex[*row.LastStepID]
		}
		parts := []string{}
		for i, c := range campaigns {
			for _, cr := range p.sendsIn(c) {
				if cr.RecipientID == nil || *cr.RecipientID != recipients[name] {
					continue
				}
				parts = append(parts, fmt.Sprintf("%d:sent=%v:cancelled=%v:queued=%v", i, cr.SentAt != nil, cr.CancelledAt != nil, cr.SendAt != nil))
			}
		}
		sort.Strings(parts)
		o.sends = fmt.Sprint(parts)
		return o
	}
	return p, programID, people, outcome
}

// TestAdvanceBatchMatchesPerPerson checks a batch with its sends read ahead and
// its writes stored in one transaction leaves every person as advancing them
// one by one does
func TestAdvanceBatchMatchesPerPerson(t *testing.T) {
	names := []string{"fresh", "second", "revive", "failed", "last", "done"}

	batch, _, _, batchOut := advanceTwin(t)
	if n, err := batch.svc.advanceDueBatch(batch.ctx, nil, time.Now()); err != nil || n != len(names) {
		t.Fatalf("advance batch: %d %v", n, err)
	}

	single, programID, people, singleOut := advanceTwin(t)
	for _, name := range names {
		e := people[name]
		err := single.svc.advanceEnrollment(single.ctx, nil, single.loadProgram(programID), e, time.Now(), nil, nil)
		if err != nil {
			t.Fatalf("advance %s: %v", name, err)
		}
		// as the batch does after an advance that works
		if e.AdvanceFailures > 0 {
			single.setEnrollment(e.ID, map[string]any{"advance_failures": 0})
		}
	}
	for _, name := range names {
		got, want := batchOut(name), singleOut(name)
		if got != want {
			t.Fatalf("%s: batch %+v, one by one %+v", name, got, want)
		}
	}
	// spot checks that the outcome is the expected one, not only the same
	if o := batchOut("revive"); o.position != 2 || o.sends != "[0:sent=true:cancelled=false:queued=false 1:sent=false:cancelled=false:queued=true]" {
		t.Fatalf("revive: %+v", o)
	}
	if o := batchOut("failed"); o.failures != 0 || o.position != 1 {
		t.Fatalf("failed: %+v", o)
	}
	if o := batchOut("last"); o.status != model.ProgramEnrollmentCompleted || o.lastStep != 2 {
		t.Fatalf("last: %+v", o)
	}
	if o := batchOut("done"); o.status != model.ProgramEnrollmentCompleted {
		t.Fatalf("done: %+v", o)
	}
}
