package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
)

// newSendRaceTest is a test with a campaign and due rows, each for a real
// recipient, so the due read of the sender loads them with their recipient
func newSendRaceTest(t *testing.T, rows int) (*programTest, uuid.UUID, []uuid.UUID) {
	t.Helper()
	p := newProgramLifecycleTest(t)
	for _, name := range []string{
		data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT,
		data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_FAILED,
		data.EVENT_CAMPAIGN_RECIPIENT_CANCELLED,
	} {
		if cache.EventIDByName[name] == nil {
			id := uuid.New()
			cache.EventIDByName[name] = &id
		}
		cache.EventNameByID[cache.EventIDByName[name].String()] = name
	}
	campaignID := seedCampaign(t, p.db, nil, nil, nil, nil)
	ids := []uuid.UUID{}
	for i := 0; i < rows; i++ {
		recipientID := p.recipient(uuid.New().String() + "@example.com")
		id := uuid.New()
		now := utils.NowRFC3339UTC()
		if err := p.db.Model(&database.CampaignRecipient{}).Create(map[string]any{
			"id":           id,
			"created_at":   now,
			"updated_at":   now,
			"campaign_id":  campaignID,
			"recipient_id": recipientID,
			"send_at":      utils.RFC3339UTC(time.Now().Add(-time.Minute)),
			"self_managed": false,
		}).Error; err != nil {
			t.Fatalf("seed row: %v", err)
		}
		ids = append(ids, id)
	}
	return p, campaignID, ids
}

// readBatch reads the due rows the way SendNextBatch does
func readBatch(p *programTest) map[uuid.UUID]*model.CampaignRecipient {
	p.t.Helper()
	rows, err := p.svc.CampaignRecipientRepository.GetUnsendRecipientsForSending(
		p.ctx, 1000, &repository.CampaignRecipientOption{WithRecipient: true},
	)
	if err != nil {
		p.t.Fatalf("read batch: %v", err)
	}
	out := map[uuid.UUID]*model.CampaignRecipient{}
	for _, r := range rows {
		out[r.ID.MustGet()] = r
	}
	return out
}

// TestSenderSkipsRowCancelledAfterBatchRead checks a row cancelled, paused or
// sent after the batch was read is not claimed for sending and keeps what the
// other writer set, while a row still due is claimed once
func TestSenderSkipsRowCancelledAfterBatchRead(t *testing.T) {
	p, _, ids := newSendRaceTest(t, 3)
	c := p.svc.CampaignService
	batch := readBatch(p)
	if len(batch) != 3 {
		t.Fatalf("all rows must be due, got %d", len(batch))
	}
	cancelled, due, sent := ids[0], ids[1], ids[2]

	// other writers change rows while the earlier campaigns of the batch send
	if err := c.CampaignRecipientRepository.Cancel(p.ctx, []*uuid.UUID{&cancelled}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := c.CampaignRecipientRepository.SetSentAtByID(p.ctx, &sent, time.Now()); err != nil {
		t.Fatalf("set sent: %v", err)
	}

	claimed, err := c.claimForSending(p.ctx, batch[cancelled])
	if err != nil || claimed {
		t.Fatalf("a cancelled row must not be claimed: %v %v", claimed, err)
	}
	row := p.campaignRecipient(cancelled)
	if row.CancelledAt == nil || row.LastAttemptAt != nil || row.SentAt != nil {
		t.Fatalf("a cancelled row must stay cancelled and untried: %+v", row)
	}
	claimed, err = c.claimForSending(p.ctx, batch[sent])
	if err != nil || claimed {
		t.Fatalf("a sent row must not be claimed again: %v %v", claimed, err)
	}
	claimed, err = c.claimForSending(p.ctx, batch[due])
	if err != nil || !claimed {
		t.Fatalf("a due row must be claimed: %v %v", claimed, err)
	}
	if p.campaignRecipient(due).LastAttemptAt == nil {
		t.Fatal("a claimed row must have its last attempt time set")
	}
	claimed, err = c.claimForSending(p.ctx, batch[due])
	if err != nil || claimed {
		t.Fatalf("a row must only be claimed once: %v %v", claimed, err)
	}

	// a pause after the read holds the rows not yet claimed
	p2, campaign2, ids2 := newSendRaceTest(t, 1)
	batch2 := readBatch(p2)
	if err := p2.db.Model(&database.Campaign{}).Where("id = ?", campaign2).
		Update("paused_at", utils.NowRFC3339UTC()).Error; err != nil {
		t.Fatalf("pause: %v", err)
	}
	claimed, err = p2.svc.CampaignService.claimForSending(p2.ctx, batch2[ids2[0]])
	if err != nil || claimed {
		t.Fatalf("a row of a paused campaign must not be claimed: %v %v", claimed, err)
	}
	if row := p2.campaignRecipient(ids2[0]); row.LastAttemptAt != nil || row.CancelledAt != nil {
		t.Fatalf("a held row must stay due for the resume: %+v", row)
	}

	// a row moved later after the read, as a resume spreads the sends again,
	// waits for its new time
	p4, _, ids4 := newSendRaceTest(t, 2)
	batch4 := readBatch(p4)
	moved, disabled := ids4[0], ids4[1]
	if err := p4.db.Model(&database.CampaignRecipient{}).Where("id = ?", moved).
		Update("send_at", utils.RFC3339UTC(time.Now().Add(time.Hour))).Error; err != nil {
		t.Fatalf("move: %v", err)
	}
	claimed, err = p4.svc.CampaignService.claimForSending(p4.ctx, batch4[moved])
	if err != nil || claimed {
		t.Fatalf("a row moved later must not be claimed: %v %v", claimed, err)
	}
	// a person disabled by SCIM after the read gets nothing
	disabledRecipient := batch4[disabled].RecipientID.MustGet()
	if err := p4.db.Model(&database.Recipient{}).Where("id = ?", disabledRecipient).
		Update("scim_soft_deleted_at", utils.NowRFC3339UTC()).Error; err != nil {
		t.Fatalf("disable: %v", err)
	}
	claimed, err = p4.svc.CampaignService.claimForSending(p4.ctx, batch4[disabled])
	if err != nil || claimed {
		t.Fatalf("a row of a disabled person must not be claimed: %v %v", claimed, err)
	}

	// a row whose recipient was deleted after the read is not sent
	p3, _, ids3 := newSendRaceTest(t, 1)
	batch3 := readBatch(p3)
	recipientID := batch3[ids3[0]].RecipientID.MustGet()
	if err := p3.svc.CampaignRecipientRepository.RemoveRecipientIDByRecipientID(p3.ctx, &recipientID); err != nil {
		t.Fatalf("remove recipient: %v", err)
	}
	claimed, err = p3.svc.CampaignService.claimForSending(p3.ctx, batch3[ids3[0]])
	if err != nil || claimed {
		t.Fatalf("a row with its recipient removed must not be claimed: %v %v", claimed, err)
	}
}

// TestSaveSendingResultKeepsOtherWriters checks the result of a send writes
// only the sent time, so what another writer changed during the send is kept
func TestSaveSendingResultKeepsOtherWriters(t *testing.T) {
	p, _, ids := newSendRaceTest(t, 2)
	c := p.svc.CampaignService
	batch := readBatch(p)
	for _, id := range ids {
		claimed, err := c.claimForSending(p.ctx, batch[id])
		if err != nil || !claimed {
			t.Fatalf("claim: %v %v", claimed, err)
		}
	}
	okID, failedID := ids[0], ids[1]

	// during the send the recipient is deleted, the row gets a pseudonym and is
	// cancelled by a close
	recipientID := batch[okID].RecipientID.MustGet()
	if err := c.CampaignRecipientRepository.RemoveRecipientIDByRecipientID(p.ctx, &recipientID); err != nil {
		t.Fatalf("remove recipient: %v", err)
	}
	pseudonym := uuid.New()
	if err := p.db.Model(&database.CampaignRecipient{}).Where("id IN ?", ids).
		Update("anonymized_id", pseudonym).Error; err != nil {
		t.Fatalf("set pseudonym: %v", err)
	}
	if err := c.CampaignRecipientRepository.Cancel(p.ctx, []*uuid.UUID{&failedID}); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if err := c.saveSendingResult(p.ctx, batch[okID], nil); err != nil {
		t.Fatalf("save sent: %v", err)
	}
	if err := c.saveSendingResult(p.ctx, batch[failedID], errors.New("refused")); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	row := p.campaignRecipient(okID)
	if row.SentAt == nil || row.LastAttemptAt == nil {
		t.Fatalf("a sent row must have its sent time: %+v", row)
	}
	if row.RecipientID != nil {
		t.Fatal("the result must not restore a removed recipient")
	}
	if row.AnonymizedID == nil || *row.AnonymizedID != pseudonym {
		t.Fatal("the result must keep the pseudonym set during the send")
	}
	failed := p.campaignRecipient(failedID)
	if failed.CancelledAt == nil || failed.SentAt != nil {
		t.Fatalf("a failed send must keep a cancel made during the send: %+v", failed)
	}
	if failed.AnonymizedID == nil || *failed.AnonymizedID != pseudonym {
		t.Fatal("a failed send must keep the pseudonym set during the send")
	}
	if failed.RecipientID == nil {
		t.Fatal("a failed send must keep its recipient")
	}
}

// TestSendNextBatchRestsFailingCampaign checks a campaign whose send stops
// with an error and leaves its rows due is left out of the next batches for
// a while, so its rows can not fill every batch
func TestSendNextBatchRestsFailingCampaign(t *testing.T) {
	p, campaignID, ids := newSendRaceTest(t, 2)
	c := p.svc.CampaignService
	// the test campaign has no template, so its send stops before any row
	// is touched
	if err := c.SendNextBatch(p.ctx, authorizedSession(t)); err == nil {
		t.Fatal("a campaign with no template must fail the send")
	}
	for _, id := range ids {
		if row := p.campaignRecipient(id); row.LastAttemptAt != nil || row.CancelledAt != nil {
			t.Fatalf("the rows stay due: %+v", row)
		}
	}
	now := time.Now()
	resting := c.restingCampaigns(now)
	if len(resting) != 1 || *resting[0] != campaignID {
		t.Fatalf("the failing campaign must rest, got %v", resting)
	}
	// another campaign due at the same time is still read
	other := seedRecipient(t, p.db, seedCampaign(t, p.db, nil, nil, nil, nil), now.Add(-time.Minute), false)
	rows, err := c.CampaignRecipientRepository.GetUnsendRecipientsForSending(
		p.ctx, 1000, &repository.CampaignRecipientOption{ExcludeCampaignIDs: resting},
	)
	if err != nil {
		t.Fatalf("read batch: %v", err)
	}
	if len(rows) != 1 || rows[0].ID.MustGet() != other {
		t.Fatalf("only the other campaign must be read while the first rests, got %d rows", len(rows))
	}
	// the rest ends
	if got := c.restingCampaigns(now.Add(campaignSendRest)); len(got) != 0 {
		t.Fatalf("the rest must end, got %v", got)
	}
}
