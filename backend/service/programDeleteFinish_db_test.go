package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// programGone reports whether a program and all its hidden campaigns are
// deleted
func (p *programTest) programGone(programID uuid.UUID) bool {
	p.t.Helper()
	var programs, campaigns int64
	p.db.Model(&database.Program{}).Where("id = ?", programID).Count(&programs)
	p.db.Model(&database.Campaign{}).Where("program_id = ?", programID).Count(&campaigns)
	return programs == 0 && campaigns == 0
}

// waitDeletes waits for the background delete pass to end
func (p *programTest) waitDeletes() {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := p.svc.waitForDeletes(ctx); err != nil {
		p.t.Fatal("the delete pass did not end")
	}
}

// TestProgramDeleteOutlivesRequest checks a delete whose request ends half
// way still removes every hidden campaign and the program
func TestProgramDeleteOutlivesRequest(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	p.manyCampaigns(programID, 4)
	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	// the request ends when the first campaign is being removed
	p.probe("delete", database.CAMPAIGN_EVENT_TABLE, cancel)
	if err := p.svc.DeleteByID(ctx, session, &programID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	p.waitDeletes()
	if !p.programGone(programID) {
		t.Fatal("expected the program and its campaigns deleted after the request ended")
	}
	if !lockIsFree(p.svc) || p.svc.isStoppingNow(programID) {
		t.Fatal("the delete must release the lock and its claim")
	}
}

// TestSchedulerFinishesHalfDelete checks the scheduler finishes a delete that
// stopped half way, waits before it tries a failed one again, audits a
// failure once and the finished delete as a success, and leaves a delete
// that a request runs now alone
func TestSchedulerFinishesHalfDelete(t *testing.T) {
	p := newProgramLifecycleTest(t)
	buf := p.withAuditBuffer()
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	campaigns, _ := p.manyCampaigns(programID, 3)
	other := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"deleting_at": time.Now().UTC(), "paused_at": time.Now().UTC()})

	// a delete a request runs now is left to it
	p.svc.claimStopping(programID)
	if p.svc.finishDelete(p.ctx, session, time.Now()) {
		t.Fatal("a delete under way in a request must be left alone")
	}
	p.svc.releaseStopping(programID)

	// the second campaign can not be deleted, the pass fails
	p.db.Model(&database.Campaign{}).Where("id = ?", campaigns[1]).Update("name", "blocked")
	if err := p.db.Exec(`CREATE TRIGGER fail_delete BEFORE DELETE ON campaigns
		WHEN OLD.name = 'blocked' BEGIN SELECT RAISE(ABORT, 'delete refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	now := time.Now()
	if p.svc.finishDelete(p.ctx, session, now) {
		t.Fatal("expected the pass to fail")
	}
	if p.programGone(programID) || p.svc.isStoppingNow(programID) {
		t.Fatal("a failed pass keeps the program and releases its claim")
	}
	if err := p.db.Exec(`DROP TRIGGER fail_delete`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	// a failed program waits before it is tried again
	if p.svc.finishDelete(p.ctx, session, now.Add(time.Minute)) {
		t.Fatal("a failed delete must wait before it is tried again")
	}
	if !p.svc.finishDelete(p.ctx, session, now.Add(programDeleteRetryEvery)) {
		t.Fatal("expected the delete finished")
	}
	if !p.programGone(programID) {
		t.Fatal("expected the program and its campaigns deleted")
	}
	if _, err := p.svc.ProgramRepository.GetByID(p.ctx, &other, nil); err != nil {
		t.Fatalf("the other program must stay: %v", err)
	}
	if !lockIsFree(p.svc) {
		t.Fatal("the pass must release the lock")
	}
	ev := step4Events(t, buf.String(), "Program.DeleteById")
	if len(ev) != 2 || ev[0]["result"] != "failed" || ev[1]["result"] != "success" {
		t.Fatalf("expected a failed and a success event, got %v", ev)
	}
	if d := eventDetails(t, ev[1]); d["reason"] != "delete finished" || d["id"] != programID.String() {
		t.Fatalf("delete event details: %v", d)
	}
	if strings.Count(buf.String(), `"Program.DeleteById"`) != 2 {
		t.Fatalf("expected two delete events: %s", buf.String())
	}
}

// TestTickStartsDeleteFinish checks the membership pass of a tick finishes a
// delete that stopped half way, on its own goroutine
func TestTickStartsDeleteFinish(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.manyCampaigns(programID, 2)
	p.setProgram(programID, map[string]any{"deleting_at": time.Now().UTC(), "paused_at": time.Now().UTC()})
	p.svc.lastRetentionAt = time.Now()
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	p.waitForCloses(t)
	p.waitDeletes()
	if !p.programGone(programID) {
		t.Fatal("expected the tick to finish the delete")
	}
}

// TestCampaignDeleteClosesFirst checks a campaign delete that stops half way
// leaves the campaign closed, so it does not send or serve pages without its
// allow and deny lists, and a campaign already closed keeps its close time
func TestCampaignDeleteClosesFirst(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	campaigns, _ := p.manyCampaigns(programID, 2)
	closed := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	p.db.Model(&database.Campaign{}).Where("id = ?", campaigns[1]).Update("closed_at", closed)
	p.db.Model(&database.Campaign{}).Where("id IN ?", campaigns).Update("name", "blocked")
	if err := p.db.Exec(`CREATE TRIGGER fail_delete BEFORE DELETE ON campaigns
		WHEN OLD.name = 'blocked' BEGIN SELECT RAISE(ABORT, 'delete refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	for _, id := range campaigns {
		if err := p.svc.CampaignService.deleteByID(p.ctx, session, &id, true); err == nil {
			t.Fatal("expected the delete to fail")
		}
	}
	if row := p.campaignRow(campaigns[0]); row.ClosedAt == nil {
		t.Fatal("a delete that stopped half way must leave the campaign closed")
	}
	if row := p.campaignRow(campaigns[1]); row.ClosedAt == nil || !row.ClosedAt.Equal(closed) {
		t.Fatalf("a closed campaign keeps its close time, got %v", row.ClosedAt)
	}
	var sends int64
	p.db.Model(&database.CampaignRecipient{}).Where("campaign_id IN ?", campaigns).Count(&sends)
	if sends != 0 {
		t.Fatalf("expected the rows removed before the campaign, %d left", sends)
	}
	if err := p.db.Exec(`DROP TRIGGER fail_delete`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if err := p.svc.CampaignService.deleteByID(p.ctx, session, &campaigns[0], true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := p.svc.CampaignService.CampaignRepository.GetByID(p.ctx, &campaigns[0], &repository.CampaignOption{}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected the campaign gone, got %v", err)
	}
}

// TestRemovedStepBacklogSkipsLinksWithNothingLeft checks the read of the
// sends of removed steps finds a new send after more open links of removed
// steps than one read looks at, all with nothing left to cancel, and skips a
// program being closed
func TestRemovedStepBacklogSkipsLinksWithNothingLeft(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	removedStep := p.step(programID, 0, true)
	now := time.Now().UTC()
	n := 1001
	campaigns := make([]*database.Campaign, 0, n)
	links := make([]*database.ProgramCampaign, 0, n)
	for i := 0; i < n; i++ {
		cid, lid := uuid.New(), uuid.New()
		campaigns = append(campaigns, &database.Campaign{ID: &cid, CreatedAt: &now, UpdatedAt: &now, Name: "campaign " + cid.String(), ProgramID: &programID})
		links = append(links, &database.ProgramCampaign{ID: &lid, CreatedAt: &now, ProgramID: &programID, ProgramStepID: &removedStep, LoopIndex: i + 1, CampaignID: &cid})
	}
	for _, rows := range []any{campaigns, links} {
		if err := p.db.CreateInBatches(rows, 200).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	// the new work sorts after every link with nothing left
	newCampaign := uuid.New()
	p.create(&database.Campaign{ID: &newCampaign, CreatedAt: &now, UpdatedAt: &now, Name: "new", ProgramID: &programID})
	last := uuid.MustParse("ffffffff-ffff-4fff-bfff-ffffffffffff")
	p.create(&database.ProgramCampaign{ID: &last, CreatedAt: &now, ProgramID: &programID, ProgramStepID: &removedStep, LoopIndex: 0, CampaignID: &newCampaign})
	alice := p.recipient("alice@example.com")
	send := p.sendAt(newCampaign, alice, now.Add(time.Hour), false)
	if err := p.db.Where("id = ?", removedStep).Delete(&database.ProgramStep{}).Error; err != nil {
		t.Fatalf("remove step: %v", err)
	}
	found, err := p.svc.ProgramRepository.GetUnsentSendsOfRemovedSteps(p.ctx, 10)
	if err != nil || len(found) != 1 || found[0].ID != send {
		t.Fatalf("expected the new send found, got %v %v", found, err)
	}
	p.setProgram(programID, map[string]any{"closing_at": now})
	found, err = p.svc.ProgramRepository.GetUnsentSendsOfRemovedSteps(p.ctx, 10)
	if err != nil || len(found) != 0 {
		t.Fatalf("a program being closed cancels its own sends, got %v %v", found, err)
	}
}

// TestTickRepairsStaleCampaignSettings checks the settings of a program are
// copied to an open hidden campaign whose lists, webhooks, pages or data
// saving differ, for example after the copy of an edit failed, and that a
// campaign that matches, a closed campaign and a program being closed are
// left alone
func TestTickRepairsStaleCampaignSettings(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	stale := p.stepCampaign(programID, stepID, 0)
	closedCampaign := p.stepCampaign(programID, stepID, 1)
	p.db.Model(&database.Campaign{}).Where("id = ?", closedCampaign).Update("closed_at", time.Now().UTC())
	list, oldList, hook, page := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	p.create(&database.ProgramAllowDeny{ProgramID: &programID, AllowDenyID: &list})
	p.create(&database.ProgramWebhook{ProgramID: &programID, WebhookID: &hook, WebhookIncludeData: "full", WebhookEvents: 3})
	p.setProgram(programID, map[string]any{"deny_page_id": page})
	// the campaign kept the list of before the edit
	p.create(&database.CampaignAllowDeny{CampaignID: &stale, AllowDenyID: &oldList})
	p.create(&database.CampaignAllowDeny{CampaignID: &closedCampaign, AllowDenyID: &oldList})

	// a matching program is not listed
	other := p.program(model.ProgramLoopComplete)
	otherStep := p.step(other, 0, true)
	p.stepCampaign(other, otherStep, 0)

	found, err := p.svc.ProgramRepository.GetProgramsWithStaleCampaignSettings(p.ctx, 10)
	if err != nil || len(found) != 1 || *found[0] != programID {
		t.Fatalf("expected only the stale program listed, got %v %v", found, err)
	}
	// a program being closed is left to its close
	p.setProgram(programID, map[string]any{"closing_at": time.Now().UTC()})
	if found, err := p.svc.ProgramRepository.GetProgramsWithStaleCampaignSettings(p.ctx, 10); err != nil || len(found) != 0 {
		t.Fatalf("a program being closed must not be listed, got %v %v", found, err)
	}
	p.setProgram(programID, map[string]any{"closing_at": nil})

	p.svc.lastRetentionAt = time.Now()
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	p.waitForCloses(t)
	var lists []database.CampaignAllowDeny
	p.db.Where("campaign_id = ?", stale).Find(&lists)
	if len(lists) != 1 || *lists[0].AllowDenyID != list {
		t.Fatalf("expected the program list on the campaign, got %v", lists)
	}
	var hooks []database.CampaignWebhook
	p.db.Where("campaign_id = ?", stale).Find(&hooks)
	if len(hooks) != 1 || *hooks[0].WebhookID != hook || hooks[0].WebhookEvents != 3 {
		t.Fatalf("expected the program webhook on the campaign, got %v", hooks)
	}
	if row := p.campaignRow(stale); row.DenyPageID == nil || *row.DenyPageID != page {
		t.Fatalf("expected the deny page copied, got %v", row.DenyPageID)
	}
	var closedLists []database.CampaignAllowDeny
	p.db.Where("campaign_id = ?", closedCampaign).Find(&closedLists)
	if len(closedLists) != 1 || *closedLists[0].AllowDenyID != oldList {
		t.Fatalf("a closed campaign keeps its settings, got %v", closedLists)
	}
	if found, err := p.svc.ProgramRepository.GetProgramsWithStaleCampaignSettings(p.ctx, 10); err != nil || len(found) != 0 {
		t.Fatalf("expected nothing left to repair, got %v %v", found, err)
	}
	if !lockIsFree(p.svc) {
		t.Fatal("the repair must release the lock")
	}
}

// TestCloseReleasesLureCodeOfRowUnlinkedMeanwhile checks the anonymize of a
// close releases the lure code of a row whose person link is cleared between
// the read and the write, as the identity retention of a program does
func TestCloseReleasesLureCodeOfRowUnlinkedMeanwhile(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	alice := p.recipient("alice@example.com")
	sendID := p.sendAt(campaignID, alice, time.Now().Add(-time.Hour), true)
	pseudonym := uuid.New()
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", sendID).
		Updates(map[string]any{"lure_code": "ABCD1234", "anonymized_id": pseudonym})
	// the person link is cleared right before the first write of the anonymize
	p.probe("update", database.CAMPAIGN_RECIPIENT_TABLE_NAME, func() {
		p.db.Model(&database.CampaignRecipient{}).Where("id = ?", sendID).Update("recipient_id", nil)
	})
	if err := p.svc.CampaignService.anonymizeCampaignRecipients(p.ctx, &campaignID); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	row := p.campaignRecipient(sendID)
	if row.LureCode != nil {
		t.Fatalf("expected the lure code released, got %v", *row.LureCode)
	}
	if row.AnonymizedID == nil || *row.AnonymizedID != pseudonym {
		t.Fatalf("expected the pseudonym kept, got %v", row.AnonymizedID)
	}
}

// TestProgramDeleteAnswersAtOnce checks a delete request returns once the
// program is marked, while the hidden campaigns are still being removed in
// the background, and that the request audits the start and the background
// part audits the finish as the system
func TestProgramDeleteAnswersAtOnce(t *testing.T) {
	p := newProgramLifecycleTest(t)
	buf := p.withAuditBuffer()
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	p.manyCampaigns(programID, 3)
	release := make(chan struct{})
	reached := make(chan struct{})
	// the background part waits while it removes the first campaign
	p.probe("delete", database.CAMPAIGN_EVENT_TABLE, func() {
		close(reached)
		<-release
	})
	if err := p.svc.DeleteByID(p.ctx, session, &programID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("the background delete did not start")
	}
	program := p.loadProgram(programID)
	if program.DeletingAt == nil || !program.IsPaused() {
		close(release)
		t.Fatal("the request must leave the program marked and paused")
	}
	if !lockIsFree(p.svc) {
		close(release)
		t.Fatal("the background delete must not hold the lifecycle lock")
	}
	// a second request while the first runs is asked to wait
	if err := p.svc.DeleteByID(p.ctx, session, &programID); !isProgramBusy(err) {
		close(release)
		t.Fatalf("a second delete must wait for the first, got %v", err)
	}
	close(release)
	p.waitDeletes()
	if !p.programGone(programID) {
		t.Fatal("expected the program and its campaigns deleted")
	}
	ev := step4Events(t, buf.String(), "Program.DeleteById")
	if len(ev) != 3 {
		t.Fatalf("expected a start, a refused second request and a finish, got %v", ev)
	}
	if d := eventDetails(t, ev[0]); ev[0]["result"] != "success" || d["reason"] != "delete started" || d["actor"] == "system" {
		t.Fatalf("start event: %v", ev[0])
	}
	if d := eventDetails(t, ev[2]); ev[2]["result"] != "success" || d["reason"] != "delete finished" || d["actor"] != "system" {
		t.Fatalf("finish event: %v", ev[2])
	}
}
