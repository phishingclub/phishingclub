package service

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
)

// mixProgram makes a started program in the given order that mixes by
// department, with steps and their open hidden campaigns
func (p *programTest) mixProgram(order string, mixBy string, steps int) (uuid.UUID, []uuid.UUID, []uuid.UUID) {
	p.t.Helper()
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"order_mode": order, "template_mix_by": mixBy})
	stepIDs, campaignIDs := []uuid.UUID{}, []uuid.UUID{}
	for i := 0; i < steps; i++ {
		stepID := p.step(programID, i, true)
		stepIDs = append(stepIDs, stepID)
		campaignIDs = append(campaignIDs, p.stepCampaign(programID, stepID, 0))
	}
	return programID, stepIDs, campaignIDs
}

// mixPeople enrolls n people of a department, due now
func (p *programTest) mixPeople(programID uuid.UUID, department string, n int) map[uuid.UUID]string {
	p.t.Helper()
	out := map[uuid.UUID]string{}
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		id := uuid.New()
		email := id.String() + "@example.com"
		p.create(&database.Recipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, Email: &email, Department: department})
		p.enroll(programID, id, 0)
		out[id] = department
	}
	return out
}

// sendsPerDepartment counts the not cancelled sends of each campaign by the
// department of the person
func (p *programTest) sendsPerDepartment(campaignIDs []uuid.UUID, departments map[uuid.UUID]string) map[string][]int {
	p.t.Helper()
	out := map[string][]int{}
	for i, c := range campaignIDs {
		for _, cr := range p.sendsIn(c) {
			if cr.RecipientID == nil || cr.CancelledAt != nil {
				continue
			}
			d, ok := departments[*cr.RecipientID]
			if !ok {
				continue
			}
			if out[d] == nil {
				out[d] = make([]int, len(campaignIDs))
			}
			out[d][i]++
		}
	}
	return out
}

// TestProgramMixBalancesDepartment checks that in random order with mixing
// by department, a department of ten with four templates gets each template
// two or three times in the same round, over many runs
func TestProgramMixBalancesDepartment(t *testing.T) {
	for run := 0; run < 8; run++ {
		p := newProgramServiceTest(t)
		programID, _, campaigns := p.mixProgram(model.ProgramOrderShuffle, model.TemplateMixDepartment, 4)
		departments := p.mixPeople(programID, "sales", 10)
		for id, d := range p.mixPeople(programID, "it", 10) {
			departments[id] = d
		}
		if n, err := p.svc.advanceDueBatch(p.ctx, nil, time.Now()); err != nil || n != 20 {
			t.Fatalf("advance: %d %v", n, err)
		}
		for d, counts := range p.sendsPerDepartment(campaigns, departments) {
			total := 0
			for _, n := range counts {
				total += n
				if n < 2 || n > 3 {
					t.Fatalf("run %d: %s is not balanced: %v", run, d, counts)
				}
			}
			if total != 10 {
				t.Fatalf("run %d: %s expected ten sends, got %v", run, d, counts)
			}
		}
	}
}

// TestProgramMixSequentialUnchanged checks a program in the same order sends
// everyone the first template even with a mix field stored
func TestProgramMixSequentialUnchanged(t *testing.T) {
	p := newProgramServiceTest(t)
	programID, _, campaigns := p.mixProgram(model.ProgramOrderSequential, model.TemplateMixDepartment, 4)
	departments := p.mixPeople(programID, "sales", 10)
	if _, err := p.svc.advanceDueBatch(p.ctx, nil, time.Now()); err != nil {
		t.Fatalf("advance: %v", err)
	}
	counts := p.sendsPerDepartment(campaigns, departments)["sales"]
	if fmt.Sprint(counts) != "[10 0 0 0]" {
		t.Fatalf("same order must send the first template to all, got %v", counts)
	}
}

// TestProgramMixAnonymous checks an anonymous program mixes too, and that an
// old send whose person link was removed still counts by the department kept
// on the send
func TestProgramMixAnonymous(t *testing.T) {
	p := newProgramServiceTest(t)
	programID, steps, campaigns := p.mixProgram(model.ProgramOrderShuffle, model.TemplateMixDepartment, 4)
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	for _, c := range campaigns {
		p.db.Model(&database.Campaign{}).Where("id = ?", c).Update("is_anonymous", true)
	}
	// three sends of the first template an hour ago, known by pseudonym only
	sentAt := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 3; i++ {
		id, pseudonym := uuid.New(), uuid.New()
		p.create(&database.CampaignRecipient{
			ID: &id, CreatedAt: &sentAt, UpdatedAt: &sentAt, CampaignID: &campaigns[0],
			SentAt: &sentAt, AnonymizedID: &pseudonym, Department: "sales",
		})
		// stored as RFC3339 text, the same as the campaign send path
		p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("send_at", utils.RFC3339UTC(sentAt))
	}
	counts, err := p.svc.ProgramRepository.GetMixCounts(p.ctx, &programID, model.TemplateMixDepartment, sentAt.Add(-time.Minute), time.Now())
	if err != nil || counts["sales"][steps[0]] != 3 {
		t.Fatalf("expected three sales sends of the first template, got %v %v", counts, err)
	}
	departments := p.mixPeople(programID, "sales", 4)
	rows := []database.ProgramEnrollment{}
	p.db.Where("program_id = ?", programID).Find(&rows)
	for _, e := range rows {
		p.setEnrollment(e.ID, map[string]any{"anonymized_id": uuid.New()})
	}
	if n, err := p.svc.advanceDueBatch(p.ctx, nil, time.Now()); err != nil || n != 4 {
		t.Fatalf("advance: %d %v", n, err)
	}
	got := p.sendsPerDepartment(campaigns, departments)["sales"]
	if fmt.Sprint(got) == "" || got[0] != 0 || got[1]+got[2]+got[3] != 4 {
		t.Fatalf("the first template was sent to sales an hour ago and must be avoided, got %v", got)
	}
	for _, c := range campaigns[1:] {
		for _, cr := range p.sendsIn(c) {
			if cr.AnonymizedID == nil {
				t.Fatal("an anonymous send must carry a pseudonym")
			}
		}
	}
}

// TestProgramMixCountsWindow checks the counts read only sends in the window,
// leave out cancelled sends, and are kept per value and step
func TestProgramMixCountsWindow(t *testing.T) {
	p := newProgramServiceTest(t)
	programID, steps, campaigns := p.mixProgram(model.ProgramOrderShuffle, model.TemplateMixCity, 2)
	now := time.Now().UTC()
	person := func(city string) uuid.UUID {
		id := uuid.New()
		email := id.String() + "@example.com"
		p.create(&database.Recipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, Email: &email, City: city})
		return id
	}
	inside := now.Add(-time.Hour)
	p.sendAt(campaigns[0], person("Aarhus"), inside, true)
	p.sendAt(campaigns[0], person("Aarhus"), inside, false)
	p.sendAt(campaigns[1], person("Odense"), inside, false)
	// before the window
	p.sendAt(campaigns[1], person("Aarhus"), now.Add(-48*time.Hour), true)
	// cancelled
	cancelled := p.sendAt(campaigns[1], person("Aarhus"), inside, false)
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", cancelled).Update("cancelled_at", now)

	counts, err := p.svc.ProgramRepository.GetMixCounts(p.ctx, &programID, model.TemplateMixCity, now.Add(-24*time.Hour), now)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts["Aarhus"][steps[0]] != 2 || counts["Aarhus"][steps[1]] != 0 || counts["Odense"][steps[1]] != 1 {
		t.Fatalf("unexpected counts %v", counts)
	}
	// a field outside the allowlist reads nothing
	if counts, err := p.svc.ProgramRepository.GetMixCounts(p.ctx, &programID, "email", now.Add(-24*time.Hour), now); err != nil || len(counts) != 0 {
		t.Fatalf("an unknown field must read nothing, got %v %v", counts, err)
	}
	if values, err := p.svc.ProgramRepository.GetRecipientMixValues(p.ctx, []*uuid.UUID{&programID}, "email"); err != nil || len(values) != 0 {
		t.Fatalf("an unknown field must read nothing, got %v %v", values, err)
	}
}

// TestProgramMixBatchCounts checks picks made earlier in a batch count for
// the later people of the same value, and other values stay apart
func TestProgramMixBatchCounts(t *testing.T) {
	program := &model.Program{OrderMode: nullable.NewNullableWithValue(model.ProgramOrderShuffle)}
	steps := []*model.ProgramStep{}
	for i := 0; i < 4; i++ {
		steps = append(steps, &model.ProgramStep{ID: nullable.NewNullableWithValue(uuid.New()), Position: nullable.NewNullableWithValue(i)})
	}
	mix := &programMix{values: map[uuid.UUID]string{}, counts: repository.ProgramMixCounts{}}
	people := []uuid.UUID{}
	for i := 0; i < 8; i++ {
		id := uuid.New()
		mix.values[id] = "sales"
		people = append(people, id)
	}
	other := uuid.New()
	mix.values[other] = "it"
	rng := rand.New(rand.NewSource(1))
	picked := map[uuid.UUID]int{}
	for _, id := range people {
		pick := programPickStepMixed(program, steps, rng, mix.countsFor(&id))
		mix.add(&id, pick.ID.MustGet())
		picked[pick.ID.MustGet()]++
	}
	for _, st := range steps {
		if picked[st.ID.MustGet()] != 2 {
			t.Fatalf("expected each template twice, got %v", picked)
		}
	}
	if len(mix.countsFor(&other)) != 0 {
		t.Fatal("another department must not see the sales picks")
	}
	// nil counts and same order pick as before
	if programPickStepMixed(program, steps, rng, nil) == nil {
		t.Fatal("expected a pick without counts")
	}
	seq := &model.Program{OrderMode: nullable.NewNullableWithValue(model.ProgramOrderSequential)}
	if programPickStepMixed(seq, steps, rng, map[uuid.UUID]int{steps[0].ID.MustGet(): 9}) != steps[0] {
		t.Fatal("same order takes the first step whatever the counts")
	}
	var none *programMix
	if none.countsFor(&other) != nil {
		t.Fatal("a nil mix has no counts")
	}
	none.add(&other, steps[0].ID.MustGet())
}
