package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// anonymizeRowsReference is the row by row anonymize that loads every row of
// the campaign, kept here to prove the paced set writes end in the same state
func anonymizeRowsReference(db *gorm.DB, campaignID uuid.UUID) error {
	var rows []database.CampaignRecipient
	if err := db.Where("campaign_id = ?", campaignID).Find(&rows).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, cr := range rows {
		if cr.RecipientID == nil {
			if cr.LureCode != nil || cr.MessageID != nil {
				if err := db.Model(&database.CampaignRecipient{}).Where("id = ?", cr.ID).
					Updates(map[string]any{"lure_code": nil, "message_id": nil, "updated_at": now}).Error; err != nil {
					return err
				}
			}
			continue
		}
		pseudonym := uuid.New()
		if cr.AnonymizedID != nil {
			pseudonym = *cr.AnonymizedID
		}
		if err := db.Model(&database.CampaignRecipient{}).Where("id = ?", cr.ID).
			Updates(map[string]any{"lure_code": nil, "message_id": nil, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := db.Model(&database.CampaignRecipient{}).Where("id = ? AND anonymized_id IS NULL", cr.ID).
			Updates(map[string]any{"anonymized_id": pseudonym.String(), "updated_at": now}).Error; err != nil {
			return err
		}
		if cr.AnonymizedID != nil {
			continue
		}
		if err := db.Model(&database.CampaignEvent{}).Where("campaign_id = ? AND recipient_id = ?", campaignID, cr.RecipientID).
			Updates(map[string]any{
				"recipient_id": nil, "anonymized_id": pseudonym.String(),
				"user_agent": "anonymized", "ip_address": nil, "data": "anonymized", "updated_at": now,
			}).Error; err != nil {
			return err
		}
	}
	return db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignID).
		Updates(map[string]any{"recipient_id": nil, "updated_at": now}).Error
}

// anonymizeSeed is one row kind of the seeded campaign
type anonymizeSeed struct {
	linked, pseudonym, events bool
	lure                      *string
	message                   *string
}

// seedAnonymizeCampaign writes the same rows and events into a database, so two
// databases seeded with the same ids can be compared after each path ran
func seedAnonymizeCampaign(t *testing.T, db *gorm.DB, campaignID uuid.UUID, ids []uuid.UUID, kinds []anonymizeSeed) {
	t.Helper()
	now := time.Now().UTC()
	db.Create(&database.Campaign{ID: &campaignID, CreatedAt: &now, UpdatedAt: &now, Name: "c"})
	rows := []*database.CampaignRecipient{}
	events := []*database.CampaignEvent{}
	eventID := uuid.MustParse("00000000-0000-0000-0000-0000000000e1")
	for i, id := range ids {
		k := kinds[i%len(kinds)]
		row := &database.CampaignRecipient{ID: &ids[i], CreatedAt: &now, UpdatedAt: &now, CampaignID: &campaignID, LureCode: k.lure, MessageID: k.message}
		if k.lure != nil && *k.lure != "" {
			code := fmt.Sprintf("%s%06d", *k.lure, i)
			row.LureCode = &code
		}
		// the person and pseudonym ids follow from the row id, so both
		// databases hold the same values
		recipient := uuid.NewSHA1(id, []byte("recipient"))
		pseudonym := uuid.NewSHA1(id, []byte("pseudonym"))
		if k.linked {
			row.RecipientID = &recipient
		}
		if k.pseudonym {
			row.AnonymizedID = &pseudonym
		}
		rows = append(rows, row)
		if k.events {
			for j := 0; j < 2; j++ {
				eid := uuid.NewSHA1(id, []byte{byte(j)})
				e := &database.CampaignEvent{ID: &eid, CreatedAt: &now, UpdatedAt: &now, CampaignID: &campaignID, EventID: &eventID, Data: "secret", UserAgent: "ua", IPAddress: "192.0.2.1"}
				if k.pseudonym {
					e.AnonymizedID = &pseudonym
				} else {
					e.RecipientID = &recipient
				}
				events = append(events, e)
			}
		}
	}
	if err := db.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatalf("seed rows: %v", err)
	}
	if err := db.CreateInBatches(events, 200).Error; err != nil {
		t.Fatalf("seed events: %v", err)
	}
}

// TestAnonymizeRowsMatchesRowByRow checks the paced anonymize of a campaign
// ends in the state the row by row anonymize left, for every kind of row, over
// more rows than one write chunk holds
func TestAnonymizeRowsMatchesRowByRow(t *testing.T) {
	code, empty, msg := "LC", "", "msg@example.com"
	kinds := []anonymizeSeed{
		{linked: true, events: true, lure: &code, message: &msg},                  // normal campaign row
		{linked: true, pseudonym: true, events: true, lure: &code, message: &msg}, // anonymous campaign row
		{linked: true}, // never sent
		{pseudonym: true, events: true, lure: &code, message: &msg}, // unlinked, code kept
		{pseudonym: true, lure: &empty, message: &msg},              // unlinked, empty code
		{pseudonym: true, message: &msg},                            // unlinked, no code
	}
	n := 3*repository.ProgramWriteChunk + 7
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}
	campaignID := uuid.New()
	ref, got := newProgramServiceTest(t), newProgramServiceTest(t)
	seedAnonymizeCampaign(t, ref.db, campaignID, ids, kinds)
	seedAnonymizeCampaign(t, got.db, campaignID, ids, kinds)
	if err := anonymizeRowsReference(ref.db, campaignID); err != nil {
		t.Fatalf("reference: %v", err)
	}
	if err := got.svc.CampaignRecipientRepository.AnonymizeRowsOfCampaign(got.ctx, &campaignID); err != nil {
		t.Fatalf("paced: %v", err)
	}
	load := func(db *gorm.DB) (map[uuid.UUID]database.CampaignRecipient, map[uuid.UUID]database.CampaignEvent) {
		var rows []database.CampaignRecipient
		var events []database.CampaignEvent
		db.Where("campaign_id = ?", campaignID).Find(&rows)
		db.Where("campaign_id = ?", campaignID).Find(&events)
		rm, em := map[uuid.UUID]database.CampaignRecipient{}, map[uuid.UUID]database.CampaignEvent{}
		for _, r := range rows {
			rm[*r.ID] = r
		}
		for _, e := range events {
			em[*e.ID] = e
		}
		return rm, em
	}
	refRows, refEvents := load(ref.db)
	gotRows, gotEvents := load(got.db)
	str := func(s *string) string {
		if s == nil {
			return "<nil>"
		}
		return *s
	}
	// the pseudonym each row ends with, a fresh one is random in both paths
	refPseudonym, gotPseudonym := map[uuid.UUID]uuid.UUID{}, map[uuid.UUID]uuid.UUID{}
	for i, id := range ids {
		k := kinds[i%len(kinds)]
		r, g := refRows[id], gotRows[id]
		if r.RecipientID != nil || g.RecipientID != nil {
			t.Fatalf("row %d: person link kept, ref %v got %v", i, r.RecipientID, g.RecipientID)
		}
		if g.LureCode != nil || g.MessageID != nil {
			t.Fatalf("row %d: lure code %s or message id %s kept", i, str(g.LureCode), str(g.MessageID))
		}
		if str(r.LureCode) != str(g.LureCode) || str(r.MessageID) != str(g.MessageID) {
			t.Fatalf("row %d: lure %s/%s message %s/%s", i, str(r.LureCode), str(g.LureCode), str(r.MessageID), str(g.MessageID))
		}
		if (r.AnonymizedID == nil) != (g.AnonymizedID == nil) {
			t.Fatalf("row %d: pseudonym presence differs", i)
		}
		if k.pseudonym && *r.AnonymizedID != *g.AnonymizedID {
			t.Fatalf("row %d: an existing pseudonym must be kept", i)
		}
		if r.AnonymizedID != nil {
			refPseudonym[id], gotPseudonym[id] = *r.AnonymizedID, *g.AnonymizedID
		}
	}
	if len(refEvents) != len(gotEvents) {
		t.Fatalf("events: %d against %d", len(refEvents), len(gotEvents))
	}
	for i, id := range ids {
		if !kinds[i%len(kinds)].events {
			continue
		}
		for j := 0; j < 2; j++ {
			eid := uuid.NewSHA1(id, []byte{byte(j)})
			r, g := refEvents[eid], gotEvents[eid]
			if r.RecipientID != nil || g.RecipientID != nil {
				t.Fatalf("event of row %d keeps the person", i)
			}
			if r.AnonymizedID == nil || g.AnonymizedID == nil ||
				*r.AnonymizedID != refPseudonym[id] || *g.AnonymizedID != gotPseudonym[id] {
				t.Fatalf("event of row %d must carry the pseudonym of its row", i)
			}
			if r.Data != g.Data || r.UserAgent != g.UserAgent || r.IPAddress != g.IPAddress {
				t.Fatalf("event of row %d: data %q/%q ua %q/%q ip %q/%q", i, r.Data, g.Data, r.UserAgent, g.UserAgent, r.IPAddress, g.IPAddress)
			}
		}
	}
	// a second run finds nothing left and changes nothing
	if err := got.svc.CampaignRecipientRepository.AnonymizeRowsOfCampaign(got.ctx, &campaignID); err != nil {
		t.Fatalf("second run: %v", err)
	}
	again, _ := load(got.db)
	for id, r := range again {
		if r.AnonymizedID == nil || (gotRows[id].AnonymizedID != nil && *r.AnonymizedID != *gotRows[id].AnonymizedID) {
			t.Fatalf("a second run must keep every pseudonym")
		}
	}
}
