package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
)

// TestRecipientGroupDeleteBeyondBindLimit checks a group with more members
// than SQLite binds in one statement is deleted, and that only the unsent and
// not cancelled sends of its members in active campaigns are cancelled. Sent,
// cancelled, closed campaign and non member rows stay as they are. A dynamic
// group cancels the sends of the people its filter matches.
func TestRecipientGroupDeleteBeyondBindLimit(t *testing.T) {
	p := newProgramServiceTest(t)
	if err := p.db.AutoMigrate(&database.RecipientGroup{}, &database.RecipientGroupRecipient{}, &database.CampaignRecipientGroup{}, &database.CompanyReportConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := (database.CampaignRecipient{}).Migrate(p.db); err != nil {
		t.Fatalf("indexes: %v", err)
	}
	svc := &RecipientGroup{
		Common:                      Common{Logger: p.svc.Logger},
		CampaignRepository:          &repository.Campaign{DB: p.db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: p.db},
		RecipientGroupRepository:    &repository.RecipientGroup{DB: p.db},
		ProgramRepository:           &repository.Program{DB: p.db},
	}
	session := authorizedSession(t)
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	active, closed := uuid.New(), uuid.New()
	p.create(&database.Campaign{ID: &active, CreatedAt: &now, UpdatedAt: &now, Name: "active", SendStartAt: &past})
	p.create(&database.Campaign{ID: &closed, CreatedAt: &now, UpdatedAt: &now, Name: "closed", SendStartAt: &past, ClosedAt: &now})

	groupID := uuid.New()
	p.create(&database.RecipientGroup{ID: &groupID, CreatedAt: &now, UpdatedAt: &now, Name: "big"})
	// the group is used by both campaigns, the closed one is never cancelled
	p.create(&database.CampaignRecipientGroup{CampaignID: &active, RecipientGroupID: &groupID})
	p.create(&database.CampaignRecipientGroup{CampaignID: &closed, RecipientGroupID: &groupID})
	const members = 33000
	recipients := make([]*database.Recipient, 0, members+1)
	links := make([]*database.RecipientGroupRecipient, 0, members)
	sends := make([]*database.CampaignRecipient, 0, members+4)
	send := func(campaignID, recipientID uuid.UUID) *database.CampaignRecipient {
		id := uuid.New()
		row := &database.CampaignRecipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, CampaignID: &campaignID, RecipientID: &recipientID, SendAt: &now}
		sends = append(sends, row)
		return row
	}
	ids := make([]uuid.UUID, members)
	for i := range ids {
		ids[i] = uuid.New()
		email := ids[i].String() + "@example.com"
		recipients = append(recipients, &database.Recipient{ID: &ids[i], CreatedAt: &now, UpdatedAt: &now, Email: &email})
		links = append(links, &database.RecipientGroupRecipient{RecipientID: &ids[i], RecipientGroupID: &groupID})
		send(active, ids[i])
	}
	// one member was sent, one already cancelled, one is in a closed campaign
	sent := send(closed, ids[0])
	sends[1].SentAt = &now
	sends[2].CancelledAt = &past
	outsider := uuid.New()
	outsiderEmail := "outsider@example.com"
	recipients = append(recipients, &database.Recipient{ID: &outsider, CreatedAt: &now, UpdatedAt: &now, Email: &outsiderEmail})
	outsiderSend := send(active, outsider)
	for _, batch := range []any{recipients, links, sends} {
		if err := p.db.CreateInBatches(batch, 500).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := svc.deleteWait(p.ctx, session, &groupID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var cancelled int64
	p.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ? AND cancelled_at IS NOT NULL", active).Count(&cancelled)
	// every member row of the active campaign but the sent one, the one already
	// cancelled counts too
	if cancelled != members-1 {
		t.Fatalf("cancelled %d, want %d", cancelled, members-1)
	}
	if row := p.campaignRecipient(*sends[1].ID); row.CancelledAt != nil {
		t.Fatal("a sent row must be kept")
	}
	if row := p.campaignRecipient(*sends[2].ID); row.CancelledAt == nil || !row.CancelledAt.Equal(past) {
		t.Fatalf("an already cancelled row must keep its time, got %v", row.CancelledAt)
	}
	if row := p.campaignRecipient(*sent.ID); row.CancelledAt != nil {
		t.Fatal("a row of a closed campaign must be kept")
	}
	if row := p.campaignRecipient(*outsiderSend.ID); row.CancelledAt != nil {
		t.Fatal("a row of a person outside the group must be kept")
	}
	var left int64
	p.db.Model(&database.RecipientGroupRecipient{}).Where("recipient_group_id = ?", groupID).Count(&left)
	if left != 0 {
		t.Fatalf("%d member links left", left)
	}

	// a dynamic group cancels the people its filter matches, not deleted ones
	dynamicID := uuid.New()
	p.create(&database.RecipientGroup{ID: &dynamicID, CreatedAt: &now, UpdatedAt: &now, Name: "sales", IsDynamic: true, FilterField: "department", FilterValue: "sales"})
	p.create(&database.CampaignRecipientGroup{CampaignID: &active, RecipientGroupID: &dynamicID})
	inSales, deleted := uuid.New(), uuid.New()
	for _, r := range []struct {
		id      uuid.UUID
		deleted *time.Time
	}{{inSales, nil}, {deleted, &now}} {
		email := r.id.String() + "@example.com"
		p.create(&database.Recipient{ID: &r.id, CreatedAt: &now, UpdatedAt: &now, Email: &email, Department: "sales", DeletedAt: r.deleted})
	}
	sends = sends[:0]
	salesSend, deletedSend := send(active, inSales), send(active, deleted)
	if err := p.db.Create(sends).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.deleteWait(p.ctx, session, &dynamicID); err != nil {
		t.Fatalf("delete dynamic: %v", err)
	}
	if row := p.campaignRecipient(*salesSend.ID); row.CancelledAt == nil {
		t.Fatal("a member of the dynamic group must be cancelled")
	}
	if row := p.campaignRecipient(*deletedSend.ID); row.CancelledAt != nil {
		t.Fatal("a deleted recipient is no member of the dynamic group")
	}
}

// TestRecipientGroupDeleteCancelsOnlyItsCampaigns checks a group delete
// cancels the sends of its members only in the campaigns that use it, also a
// scheduled one, keeps the send of a member still covered by another group
// of the campaign, and never touches a campaign that does not use the group
// or a hidden program campaign
func TestRecipientGroupDeleteCancelsOnlyItsCampaigns(t *testing.T) {
	p := newProgramServiceTest(t)
	if err := p.db.AutoMigrate(&database.RecipientGroup{}, &database.RecipientGroupRecipient{}, &database.CampaignRecipientGroup{}, &database.CompanyReportConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := &RecipientGroup{
		Common:                      Common{Logger: p.svc.Logger},
		CampaignRepository:          &repository.Campaign{DB: p.db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: p.db},
		RecipientGroupRepository:    &repository.RecipientGroup{DB: p.db},
		ProgramRepository:           &repository.Program{DB: p.db},
	}
	session := authorizedSession(t)
	now := time.Now().UTC()
	past, future := now.Add(-time.Hour), now.Add(48*time.Hour)
	g, h := uuid.New(), uuid.New()
	p.create(&database.RecipientGroup{ID: &g, CreatedAt: &now, UpdatedAt: &now, Name: "G"})
	p.create(&database.RecipientGroup{ID: &h, CreatedAt: &now, UpdatedAt: &now, Name: "H"})
	// both is in G and H, onlyG is in G only
	both := p.recipient("both@example.com")
	onlyG := p.recipient("onlyg@example.com")
	for _, link := range []struct{ r, g uuid.UUID }{{both, g}, {both, h}, {onlyG, g}} {
		p.create(&database.RecipientGroupRecipient{RecipientID: &link.r, RecipientGroupID: &link.g})
	}
	campaign := func(name string, start time.Time, groups ...uuid.UUID) uuid.UUID {
		id := uuid.New()
		p.create(&database.Campaign{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: name, SendStartAt: &start})
		for _, gid := range groups {
			p.create(&database.CampaignRecipientGroup{CampaignID: &id, RecipientGroupID: &gid})
		}
		return id
	}
	gh := campaign("G and H", past, g, h)
	onlyH := campaign("H only", past, h)
	scheduled := campaign("G scheduled", future, g)
	// a hidden program campaign with both people. It is linked to G here so
	// the test shows it is skipped even then.
	programID := p.program("complete")
	hidden := p.stepCampaign(programID, p.step(programID, 0, true), 0)
	p.create(&database.CampaignRecipientGroup{CampaignID: &hidden, RecipientGroupID: &g})
	send := func(campaignID, recipientID uuid.UUID) uuid.UUID {
		return p.send(campaignID, recipientID, false)
	}
	ghBoth, ghOnlyG := send(gh, both), send(gh, onlyG)
	hBoth := send(onlyH, both)
	schedBoth, schedOnlyG := send(scheduled, both), send(scheduled, onlyG)
	hiddenBoth, hiddenOnlyG := send(hidden, both), send(hidden, onlyG)

	if err := svc.deleteWait(p.ctx, session, &g); err != nil {
		t.Fatalf("delete: %v", err)
	}
	cancelled := func(id uuid.UUID) bool { return p.campaignRecipient(id).CancelledAt != nil }
	if cancelled(ghBoth) {
		t.Fatal("a member still in another group of the campaign must keep the send")
	}
	if !cancelled(ghOnlyG) {
		t.Fatal("a member covered only by the deleted group must be cancelled")
	}
	if cancelled(hBoth) {
		t.Fatal("a campaign that does not use the group must be left as it is")
	}
	if !cancelled(schedBoth) || !cancelled(schedOnlyG) {
		t.Fatal("a scheduled campaign of the group must be cancelled")
	}
	if cancelled(hiddenBoth) || cancelled(hiddenOnlyG) {
		t.Fatal("a hidden program campaign must never be cancelled by a group delete")
	}
}

// TestRecipientGroupRemoveKeepsOtherGroupSends checks removing a person from a
// group cancels their unsent sends only in the open campaigns that use the
// group, keeps the send of a campaign where another group still covers them,
// and keeps a queued program send of a program on another group of theirs
func TestRecipientGroupRemoveKeepsOtherGroupSends(t *testing.T) {
	p := newProgramServiceTest(t)
	if err := p.db.AutoMigrate(&database.RecipientGroup{}, &database.RecipientGroupRecipient{}, &database.CampaignRecipientGroup{}, &database.CompanyReportConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := &RecipientGroup{
		Common:                      Common{Logger: p.svc.Logger},
		CampaignRepository:          &repository.Campaign{DB: p.db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: p.db},
		RecipientGroupRepository:    &repository.RecipientGroup{DB: p.db},
		ProgramRepository:           &repository.Program{DB: p.db},
	}
	session := authorizedSession(t)
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	a, b := uuid.New(), uuid.New()
	p.create(&database.RecipientGroup{ID: &a, CreatedAt: &now, UpdatedAt: &now, Name: "A"})
	p.create(&database.RecipientGroup{ID: &b, CreatedAt: &now, UpdatedAt: &now, Name: "B"})
	person := p.recipient("person@example.com")
	other := p.recipient("other@example.com")
	for _, link := range []struct{ r, g uuid.UUID }{{person, a}, {person, b}, {other, b}} {
		p.create(&database.RecipientGroupRecipient{RecipientID: &link.r, RecipientGroupID: &link.g})
	}
	campaign := func(name string, groups ...uuid.UUID) uuid.UUID {
		id := uuid.New()
		p.create(&database.Campaign{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: name, SendStartAt: &past})
		for _, gid := range groups {
			p.create(&database.CampaignRecipientGroup{CampaignID: &id, RecipientGroupID: &gid})
		}
		return id
	}
	onlyB := campaign("B only", b)
	ab := campaign("A and B", a, b)
	onlyA := campaign("A only", a)
	// a program on A with a queued send
	programID := p.program("complete")
	p.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &a})
	hidden := p.stepCampaign(programID, p.step(programID, 0, true), 0)
	p.enroll(programID, person, 0)
	programSend := p.send(hidden, person, false)
	bSend, abSend, aSend := p.send(onlyB, person, false), p.send(ab, person, false), p.send(onlyA, person, false)
	otherSend := p.send(onlyB, other, false)

	if err := svc.RemoveRecipients(p.ctx, session, &b, []*uuid.UUID{&person}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cancelled := func(id uuid.UUID) bool { return p.campaignRecipient(id).CancelledAt != nil }
	if cancelled(programSend) {
		t.Fatal("the queued program send of a program on another group must be kept")
	}
	if !cancelled(bSend) {
		t.Fatal("the send of a campaign on the group only must be cancelled")
	}
	if cancelled(abSend) {
		t.Fatal("a campaign where another group still covers the person must keep the send")
	}
	if cancelled(aSend) {
		t.Fatal("a campaign that does not use the group must be left as it is")
	}
	if cancelled(otherSend) {
		t.Fatal("the send of a person who was not removed must be kept")
	}
}
