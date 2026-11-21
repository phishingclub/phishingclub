package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InsertStepCampaign inserts a hidden, program managed campaign row from the
// model. It sets no recipient groups, unlike the campaign repository Insert.
func (r *Program) InsertStepCampaign(
	ctx context.Context,
	campaign *model.Campaign,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := campaign.ToDBMap()
	row["id"] = id
	AddTimestamps(row)
	res := r.DB.WithContext(ctx).Model(&database.Campaign{}).Create(row)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return &id, nil
}

// GetProgramCampaigns returns the link rows for a step and loop iteration,
// newest first. There is more than one when a closed step campaign was
// followed by a new one.
func (r *Program) GetProgramCampaigns(
	ctx context.Context,
	programID *uuid.UUID,
	stepID *uuid.UUID,
	loopIndex int,
) ([]*database.ProgramCampaign, error) {
	rows := []*database.ProgramCampaign{}
	res := r.DB.WithContext(ctx).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Where(clause.Eq{Column: clause.Column{Name: "program_step_id"}, Value: stepID}).
		Where(clause.Eq{Column: clause.Column{Name: "loop_index"}, Value: loopIndex}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "created_at"}, Desc: true}).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// ProgramPersonSend is the state of one person's send in a hidden campaign
type ProgramPersonSend struct {
	ID          uuid.UUID
	SentAt      *time.Time
	CancelledAt *time.Time
	// LastAttemptAt is set once the sender took the row, the email may have
	// gone out although sent_at was not written
	LastAttemptAt *time.Time
}

// GetPersonSend returns a person's send in a campaign, matched by recipient id
// or pseudonym, or nil when there is none
func (r *Program) GetPersonSend(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
) (*ProgramPersonSend, error) {
	rows := []ProgramPersonSend{}
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("id", "sent_at", "cancelled_at", "last_attempt_at").
		Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}).
		Where(r.personMatch(recipientID, anonymizedID)).
		Limit(1).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// ReviveCancelledSend puts a cancelled, never sent and never tried row back in
// the send queue at a new time. A row the sender took may have gone out before
// a crash kept sent_at from being written, so it is never put back. The
// unique campaign and recipient index allows one row per person per campaign,
// so a returning person reuses it, and so does a send the sender held as its
// template could not be sent. A notable event of cancelled
// goes back to scheduled, as it would outrank the sent event.
func (r *Program) ReviveCancelledSend(
	ctx context.Context,
	campaignRecipientID *uuid.UUID,
	sendAt time.Time,
) error {
	return errs.Wrap(r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		byID := clause.Eq{Column: clause.Column{Name: "id"}, Value: campaignRecipientID}
		unsent := clause.And(
			clause.Eq{Column: clause.Column{Name: "sent_at"}, Value: nil},
			clause.Eq{Column: clause.Column{Name: "last_attempt_at"}, Value: nil},
		)
		res := tx.
			Model(&database.CampaignRecipient{}).
			Where(byID).
			Where(unsent).
			Updates(map[string]any{
				"cancelled_at":    nil,
				"send_at":         utils.RFC3339UTC(sendAt),
				"last_attempt_at": nil,
				"updated_at":      utils.NowRFC3339UTC(),
			})
		if res.Error != nil {
			return res.Error
		}
		cancelledEvent := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_CANCELLED]
		scheduledEvent := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED]
		if cancelledEvent == nil || scheduledEvent == nil {
			return nil
		}
		return tx.
			Model(&database.CampaignRecipient{}).
			Where(byID).
			Where(unsent).
			Where(clause.Eq{Column: clause.Column{Name: "notable_event_id"}, Value: cancelledEvent}).
			Update("notable_event_id", scheduledEvent).Error
	}))
}

// ProgramNewestSend is the newest planned send of a person's scheduled steps
type ProgramNewestSend struct {
	SendAt time.Time
	StepID uuid.UUID
}

// GetNewestScheduledSend returns the latest planned send time of a person over
// the program's steps, nil when there is none. Cancelled sends do not count. A
// send of a step removed since then still counts.
func (r *Program) GetNewestScheduledSend(
	ctx context.Context,
	programID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
) (*ProgramNewestSend, error) {
	rows := []struct {
		CampaignID uuid.UUID
		SendAt     *time.Time
	}{}
	if err := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("campaign_id", "send_at").
		Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
		// the rows of one person are found through the person indexes, not
		// through the cancelled_at or send_at index that holds every campaign
		Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "cancelled_at"})}}).
		Where(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{notIndexed(clause.Column{Name: "send_at"})}}).
		Where(r.personMatch(recipientID, anonymizedID)).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "send_at"}, Desc: true}).
		Limit(1).
		Scan(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	if len(rows) == 0 || rows[0].SendAt == nil {
		return nil, nil
	}
	newest := &ProgramNewestSend{SendAt: *rows[0].SendAt}
	newestCampaign := rows[0].CampaignID
	link := database.ProgramCampaign{}
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: newestCampaign}).
		Limit(1).
		Find(&link)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	if res.RowsAffected > 0 && link.ProgramStepID != nil {
		newest.StepID = *link.ProgramStepID
	}
	return newest, nil
}

// ProgramHeldSend is a send of a program that is not sent and not cancelled.
type ProgramHeldSend struct {
	ID          uuid.UUID
	CampaignID  uuid.UUID
	RecipientID *uuid.UUID
	SendAt      *time.Time
}

// GetHeldSends returns the sends of a program's open hidden campaigns that are
// neither sent nor cancelled
func (r *Program) GetHeldSends(
	ctx context.Context,
	programID *uuid.UUID,
) ([]ProgramHeldSend, error) {
	openCampaigns := r.programCampaignIDs(programID).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil})
	rows := []ProgramHeldSend{}
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("id", "campaign_id", "recipient_id", "send_at").
		Where("campaign_id IN (?)", openCampaigns).
		Where(untakenSends()).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// SetSendAt moves the given sends that are not sent yet to a new send time, in
// batches. The rows are found by id, sent_at is not read through its index
func (r *Program) SetSendAt(
	ctx context.Context,
	campaignRecipientIDs []*uuid.UUID,
	sendAt time.Time,
) error {
	for start := 0; start < len(campaignRecipientIDs); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(campaignRecipientIDs))
		res := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(campaignRecipientIDs[start:end])}).
			Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "sent_at"})}}).
			Updates(map[string]any{
				"send_at":    utils.RFC3339UTC(sendAt),
				"updated_at": utils.NowRFC3339UTC(),
			})
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
	}
	return nil
}

// SendMove is a new send time for one not yet sent send
type SendMove struct {
	ID     uuid.UUID
	SendAt time.Time
}

// moveSendsBatch is how many sends one MoveSends transaction moves. Each move
// changes several indexes of the send, so a transaction is kept smaller than
// ProgramWriteChunk.
const moveSendsBatch = 100

// MoveSends gives each of the not yet sent sends its own new send time, found
// by id with sent_at not read through its index. Each moveSendsBatch rows are
// written in one transaction that only writes, one update per row. A failure
// keeps the batches before it, the sends after it keep their old time and the
// caller may move them again. There is no pause between the batches, as the
// caller holds the lifecycle lock.
func (r *Program) MoveSends(ctx context.Context, moves []SendMove) error {
	now := utils.NowRFC3339UTC()
	for start := 0; start < len(moves); start += moveSendsBatch {
		if err := r.moveSendsTx(ctx, moves[start:min(start+moveSendsBatch, len(moves))], now); err != nil {
			return err
		}
	}
	return nil
}

// moveSendsTx writes one chunk of MoveSends in one transaction
func (r *Program) moveSendsTx(ctx context.Context, moves []SendMove, now string) error {
	return errs.Wrap(r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, m := range moves {
			// by table name, so gorm does not copy each value back into a model
			res := tx.Table(database.CAMPAIGN_RECIPIENT_TABLE_NAME).
				Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: m.ID}).
				Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "sent_at"})}}).
				Updates(map[string]any{
					"send_at":    utils.RFC3339UTC(m.SendAt),
					"updated_at": now,
				})
			if res.Error != nil {
				return res.Error
			}
		}
		return nil
	}))
}

// GetAnonymousProgramIDs returns the ids of every anonymous program that is
// not closed. The close of a program removes every identity link of its
// sends, so a closed program has nothing left for the identity removal.
func (r *Program) GetAnonymousProgramIDs(ctx context.Context) ([]*uuid.UUID, error) {
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: clause.Column{Name: "is_anonymous"}, Value: true}).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil}), "id")
}

// GetAnonymousAmong returns which of the given programs are anonymous, open
// or closed
func (r *Program) GetAnonymousAmong(ctx context.Context, ids []*uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	for start := 0; start < len(ids); start += ProgramWriteChunk {
		end := min(start+ProgramWriteChunk, len(ids))
		found, err := pluckUUIDs(r.DB.WithContext(ctx).
			Model(&database.Program{}).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(ids[start:end])}).
			Where(clause.Eq{Column: clause.Column{Name: "is_anonymous"}, Value: true}), "id")
		if err != nil {
			return nil, err
		}
		for _, id := range found {
			out[*id] = true
		}
	}
	return out, nil
}

// programIdentityBatch is how many rows one identity removal write changes.
// Each changed row is written to several indexes, so the write is kept short
// and other writers get a turn between batches.
const programIdentityBatch = 500

// RemoveIdentityOfSendsBefore removes the recipient link of the sent rows of a
// program's hidden campaigns sent before the given time. The lure code and
// the message id are kept, so the link in the email works and the report
// inbox can match a reported email until the program closes, and the close
// releases both. Only rows that carry a pseudonym are changed, so the
// results stay countable by it, and rows not yet sent are never touched. It
// writes in batches with a pause between them, see PauseAfterWrite, so no
// single write holds the database for long and other writers get a turn. The
// caller holds no lock.
func (r *Program) RemoveIdentityOfSendsBefore(
	ctx context.Context,
	programID *uuid.UUID,
	before time.Time,
) (int64, error) {
	var total int64
	for {
		// both IS NOT NULL predicates are written as the partial index
		// idx_campaign_recipients_identity_sent states them, and the campaign
		// is not read through an index, so sqlite reads that partial index by
		// sent_at. It holds only the sent rows that still name a person, so
		// a batch does not walk the rows changed before. Through the campaign
		// and recipient index it would walk every row of the program again.
		batch := r.DB.
			Model(&database.CampaignRecipient{}).
			Select("id").
			Where(r.inProgramCampaigns(programID, false)).
			Where(clause.Neq{Column: clause.Column{Name: "recipient_id"}, Value: nil}).
			Where(clause.Neq{Column: clause.Column{Name: "anonymized_id"}, Value: nil}).
			Where(clause.Neq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
			// sent_at is stored as RFC3339 UTC text, so it is compared as such
			Where(clause.Lt{Column: clause.Column{Name: "sent_at"}, Value: utils.RFC3339UTC(before)}).
			Limit(programIdentityBatch)
		start := time.Now()
		res := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Where("id IN (?)", batch).
			Updates(map[string]any{
				"recipient_id": nil,
				"updated_at":   utils.NowRFC3339UTC(),
			})
		if res.Error != nil {
			return total, errs.Wrap(res.Error)
		}
		total += res.RowsAffected
		if res.RowsAffected < programIdentityBatch {
			return total, nil
		}
		if err := PauseAfterWrite(ctx, time.Since(start)); err != nil {
			return total, err
		}
	}
}

// DeleteUnlinkedDeviceCodes deletes the device code entries of a program's
// hidden campaigns that name a person whose send no longer names them, as
// after RemoveIdentityOfSendsBefore. Such an entry would keep the person and
// what was captured linked to the campaign. An entry of a send that still
// names the person is kept. The entries are deleted in chunks with a pause
// between them, see PauseAfterWrite. The caller holds no lock.
func (r *Program) DeleteUnlinkedDeviceCodes(ctx context.Context, programID *uuid.UUID) error {
	codeCampaign := clause.Column{Table: database.MICROSOFT_DEVICE_CODE_TABLE, Name: "campaign_id"}
	codeRecipient := clause.Column{Table: database.MICROSOFT_DEVICE_CODE_TABLE, Name: "recipient_id"}
	// the send of the entry, found through the campaign and recipient index
	send := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.CampaignRecipient{}).
		Select("id").
		Where(clause.Expr{SQL: "? = ?", Vars: []any{clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "campaign_id"}, codeCampaign}}).
		Where(clause.Expr{SQL: "? = ?", Vars: []any{clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "recipient_id"}, codeRecipient}})
	for {
		batch := r.DB.Session(&gorm.Session{NewDB: true}).
			Model(&database.MicrosoftDeviceCode{}).
			Select("id").
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{codeCampaign, r.programCampaignIDs(programID)}}).
			Where(clause.Neq{Column: codeRecipient, Value: nil}).
			Where(clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{send}}).
			Limit(programIdentityBatch)
		start := time.Now()
		res := r.DB.WithContext(ctx).
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Name: "id"}, batch}}).
			Delete(&database.MicrosoftDeviceCode{})
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if res.RowsAffected < programIdentityBatch {
			return nil
		}
		if err := PauseAfterWrite(ctx, time.Since(start)); err != nil {
			return err
		}
	}
}

// InsertProgramCampaign creates the link between a step iteration and a hidden
// campaign.
func (r *Program) InsertProgramCampaign(
	ctx context.Context,
	programID *uuid.UUID,
	stepID *uuid.UUID,
	loopIndex int,
	campaignID *uuid.UUID,
) error {
	now := time.Now()
	id := uuid.New()
	row := database.ProgramCampaign{
		ID:            &id,
		CreatedAt:     &now,
		ProgramID:     programID,
		ProgramStepID: stepID,
		LoopIndex:     loopIndex,
		CampaignID:    campaignID,
	}
	res := r.DB.WithContext(ctx).Create(&row)
	return errs.Wrap(res.Error)
}

// GetCampaignIDs returns the ids of the hidden campaigns a program owns. It reads
// campaigns.program_id rather than the links, so it still finds every campaign
// after a delete that stopped half way.
func (r *Program) GetCampaignIDs(
	ctx context.Context,
	programID *uuid.UUID,
) ([]*uuid.UUID, error) {
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)), "id")
}

// DeleteProgramCampaignByCampaignID removes the link between a hidden campaign
// and its program step
func (r *Program) DeleteProgramCampaignByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}).
		Delete(&database.ProgramCampaign{})
	return errs.Wrap(res.Error)
}

// ProgramDelivery is one step a recipient has been given in a loop, with the
// template the hidden campaign of the step sends
type ProgramDelivery struct {
	RecipientID uuid.UUID
	StepID      uuid.UUID
	LoopIndex   int
	TemplateID  *uuid.UUID
}

// deliveredSends selects the campaign recipient rows that count as a delivery:
// not cancelled, and either sent or still waiting in a campaign that is open.
// A send that will never go out does not use up its step. A row the sender
// took counts also when it was cancelled later, it may have gone out before a
// crash kept sent_at from being written.
func (r *Program) deliveredSends() *gorm.DB {
	openCampaigns := r.DB.Model(&database.Campaign{}).
		Select("id").
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil})
	// the caller limits the rows by campaign or by person, so the cancelled_at
	// and sent_at predicates are not read through their indexes, which hold
	// every campaign
	waiting := r.DB.
		Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "cancelled_at"})}}).
		Where(r.DB.
			Where(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{notIndexed(clause.Column{Name: "sent_at"})}}).
			Or(clause.Expr{SQL: "? IN (?)", Vars: []any{notIndexed(clause.Column{Name: "campaign_id"}), openCampaigns}}))
	return r.DB.Model(&database.CampaignRecipient{}).
		Where(r.DB.
			Where(waiting).
			Or(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{notIndexed(clause.Column{Name: "last_attempt_at"})}}))
}

// GetLoopDeliveries returns the steps a recipient has been given in a loop,
// each with the template its hidden campaign sends. A cancelled send is not a
// delivery, so the step is offered again. The pseudonym, when given, also
// matches sends whose recipient link was removed.
func (r *Program) GetLoopDeliveries(
	ctx context.Context,
	programID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
	loopIndex int,
) ([]ProgramDelivery, error) {
	sentTo := r.deliveredSends().
		Select("campaign_id").
		Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
		Where(r.personMatch(recipientID, anonymizedID))
	links := []database.ProgramCampaign{}
	res := r.DB.WithContext(ctx).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Where(clause.Eq{Column: clause.Column{Name: "loop_index"}, Value: loopIndex}).
		Where("campaign_id IN (?)", sentTo).
		Find(&links)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	templates, err := r.campaignTemplates(ctx, links)
	if err != nil {
		return nil, err
	}
	out := make([]ProgramDelivery, 0, len(links))
	for _, l := range links {
		d := ProgramDelivery{StepID: *l.ProgramStepID, LoopIndex: l.LoopIndex}
		if recipientID != nil {
			d.RecipientID = *recipientID
		}
		if t, ok := templates[*l.CampaignID]; ok {
			d.TemplateID = &t
		}
		out = append(out, d)
	}
	return out, nil
}

// ProgramPerson names a person of a program by recipient id, pseudonym or both.
// Loop, when set, is the loop the person is in.
type ProgramPerson struct {
	RecipientID  *uuid.UUID
	AnonymizedID *uuid.UUID
	Loop         *int
}

// programPersonRow is one send of a program's hidden campaign to a person
type programPersonRow struct {
	ID            uuid.UUID
	CampaignID    uuid.UUID
	RecipientID   *uuid.UUID
	AnonymizedID  *uuid.UUID
	SendAt        *time.Time
	SentAt        *time.Time
	CancelledAt   *time.Time
	LastAttemptAt *time.Time
}

// matches reports whether the row is a send of the person, by recipient id or
// by pseudonym, the same way personMatch does
func (row *programPersonRow) matches(p ProgramPerson) bool {
	if p.RecipientID != nil && row.RecipientID != nil && *row.RecipientID == *p.RecipientID {
		return true
	}
	return p.AnonymizedID != nil && row.AnonymizedID != nil && *row.AnonymizedID == *p.AnonymizedID
}

// ProgramPeopleSends holds the sends of a group of people in one program, read
// in a few queries, so the scheduler does not read them again for each person.
// It answers the same as GetLoopDeliveries, GetPersonSend and
// GetNewestScheduledSend at the time it was read.
type ProgramPeopleSends struct {
	links     []database.ProgramCampaign
	stepOf    map[uuid.UUID]*uuid.UUID
	open      map[uuid.UUID]bool
	templates map[uuid.UUID]uuid.UUID
	byRecip   map[uuid.UUID][]*programPersonRow
	byAnon    map[uuid.UUID][]*programPersonRow
}

// GetPeopleSends reads the sends of the given people in a program's hidden
// campaigns, with the campaign links and the state of each campaign. When every
// person has a loop, only the campaigns of the loop before, the loop itself and
// the loop after are read, which hold every send the scheduler asks about: the
// deliveries of the loop, the send in the campaign of this or the next loop,
// and the newest send, which is in the loop of the last send or the one after
// it. Without a loop for every person all campaigns are read.
func (r *Program) GetPeopleSends(
	ctx context.Context,
	programID *uuid.UUID,
	people []ProgramPerson,
) (*ProgramPeopleSends, error) {
	out := &ProgramPeopleSends{
		stepOf:    map[uuid.UUID]*uuid.UUID{},
		open:      map[uuid.UUID]bool{},
		templates: map[uuid.UUID]uuid.UUID{},
		byRecip:   map[uuid.UUID][]*programPersonRow{},
		byAnon:    map[uuid.UUID][]*programPersonRow{},
	}
	if err := r.DB.WithContext(ctx).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Find(&out.links).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	for i := range out.links {
		l := out.links[i]
		// the first link of a campaign names its step, as a read with a limit
		// of one does
		if l.CampaignID != nil {
			if _, ok := out.stepOf[*l.CampaignID]; !ok {
				out.stepOf[*l.CampaignID] = l.ProgramStepID
			}
		}
	}
	campaigns := []struct {
		ID                 uuid.UUID
		ClosedAt           *time.Time
		CampaignTemplateID *uuid.UUID
	}{}
	if err := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Select("id", "closed_at", "campaign_template_id").
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Scan(&campaigns).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	for _, c := range campaigns {
		out.open[c.ID] = c.ClosedAt == nil
		if c.CampaignTemplateID != nil {
			out.templates[c.ID] = *c.CampaignTemplateID
		}
	}
	recipientIDs := []any{}
	anonymizedIDs := []any{}
	for _, p := range people {
		if p.RecipientID != nil {
			recipientIDs = append(recipientIDs, *p.RecipientID)
		}
		if p.AnonymizedID != nil {
			anonymizedIDs = append(anonymizedIDs, *p.AnonymizedID)
		}
	}
	if len(campaigns) == 0 || len(recipientIDs)+len(anonymizedIDs) == 0 {
		return out, nil
	}
	campaignColumn := notIndexed(clause.Column{Name: "campaign_id"})
	inCampaigns := clause.Expr{SQL: "? IN (?)", Vars: []any{campaignColumn, r.programCampaignIDs(programID)}}
	if loops := peopleLoops(people); loops != nil {
		ids := []any{}
		seen := map[uuid.UUID]bool{}
		for _, l := range out.links {
			if l.CampaignID == nil || !loops[l.LoopIndex] || seen[*l.CampaignID] {
				continue
			}
			seen[*l.CampaignID] = true
			ids = append(ids, *l.CampaignID)
		}
		if len(ids) == 0 {
			return out, nil
		}
		inCampaigns = clause.Expr{SQL: "? IN ?", Vars: []any{campaignColumn, ids}}
	}
	rows := []*programPersonRow{}
	// the people are read a few at a time through the person indexes, the
	// campaign predicate is not read through an index. Without statistics
	// sqlite reads a long list of people through a campaign index or the whole
	// table instead
	for start := 0; start < len(recipientIDs) || start < len(anonymizedIDs); start += programPeopleReadChunk {
		who := []clause.Expression{}
		if start < len(recipientIDs) {
			who = append(who, clause.IN{Column: clause.Column{Name: "recipient_id"}, Values: recipientIDs[start:min(start+programPeopleReadChunk, len(recipientIDs))]})
		}
		if start < len(anonymizedIDs) {
			who = append(who, clause.IN{Column: clause.Column{Name: "anonymized_id"}, Values: anonymizedIDs[start:min(start+programPeopleReadChunk, len(anonymizedIDs))]})
		}
		part := []*programPersonRow{}
		if err := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Select("id", "campaign_id", "recipient_id", "anonymized_id", "send_at", "sent_at", "cancelled_at", "last_attempt_at").
			Where(inCampaigns).
			Where(clause.Or(who...)).
			Scan(&part).Error; err != nil {
			return nil, errs.Wrap(err)
		}
		rows = append(rows, part...)
	}
	for _, row := range rows {
		if row.RecipientID != nil {
			out.byRecip[*row.RecipientID] = append(out.byRecip[*row.RecipientID], row)
		}
		if row.AnonymizedID != nil {
			out.byAnon[*row.AnonymizedID] = append(out.byAnon[*row.AnonymizedID], row)
		}
	}
	return out, nil
}

// programPeopleReadChunk is how many people one read of GetPeopleSends names.
// A longer list makes sqlite read the sends by campaign or the whole table.
const programPeopleReadChunk = 25

// peopleLoops returns the loops whose campaigns hold the sends of the people,
// see GetPeopleSends, or nil when a person has no loop
func peopleLoops(people []ProgramPerson) map[int]bool {
	out := map[int]bool{}
	for _, p := range people {
		if p.Loop == nil {
			return nil
		}
		for l := *p.Loop - 1; l <= *p.Loop+1; l++ {
			out[l] = true
		}
	}
	return out
}

// rowsOf returns the sends of a person, each once
func (s *ProgramPeopleSends) rowsOf(p ProgramPerson) []*programPersonRow {
	out := []*programPersonRow{}
	seen := map[uuid.UUID]bool{}
	add := func(rows []*programPersonRow) {
		for _, row := range rows {
			if !seen[row.ID] && row.matches(p) {
				seen[row.ID] = true
				out = append(out, row)
			}
		}
	}
	if p.RecipientID != nil {
		add(s.byRecip[*p.RecipientID])
	}
	if p.AnonymizedID != nil {
		add(s.byAnon[*p.AnonymizedID])
	}
	return out
}

// LoopDeliveries answers as GetLoopDeliveries does
func (s *ProgramPeopleSends) LoopDeliveries(p ProgramPerson, loopIndex int) []ProgramDelivery {
	delivered := map[uuid.UUID]bool{}
	for _, row := range s.rowsOf(p) {
		// a delivery is not cancelled, and sent or waiting in an open campaign,
		// or was taken by the sender
		if (row.CancelledAt == nil && (row.SentAt != nil || s.open[row.CampaignID])) || row.LastAttemptAt != nil {
			delivered[row.CampaignID] = true
		}
	}
	out := []ProgramDelivery{}
	for _, l := range s.links {
		if l.LoopIndex != loopIndex || l.CampaignID == nil || !delivered[*l.CampaignID] {
			continue
		}
		d := ProgramDelivery{StepID: *l.ProgramStepID, LoopIndex: l.LoopIndex}
		if p.RecipientID != nil {
			d.RecipientID = *p.RecipientID
		}
		if t, ok := s.templates[*l.CampaignID]; ok {
			d.TemplateID = &t
		}
		out = append(out, d)
	}
	return out
}

// PersonSend answers as GetPersonSend does
func (s *ProgramPeopleSends) PersonSend(campaignID uuid.UUID, p ProgramPerson) *ProgramPersonSend {
	for _, row := range s.rowsOf(p) {
		if row.CampaignID == campaignID {
			return &ProgramPersonSend{ID: row.ID, SentAt: row.SentAt, CancelledAt: row.CancelledAt, LastAttemptAt: row.LastAttemptAt}
		}
	}
	return nil
}

// NewestScheduledSend answers as GetNewestScheduledSend does
func (s *ProgramPeopleSends) NewestScheduledSend(p ProgramPerson) *ProgramNewestSend {
	var newest *programPersonRow
	for _, row := range s.rowsOf(p) {
		if row.CancelledAt != nil || row.SendAt == nil {
			continue
		}
		if newest == nil || row.SendAt.After(*newest.SendAt) {
			newest = row
		}
	}
	if newest == nil {
		return nil
	}
	out := &ProgramNewestSend{SendAt: *newest.SendAt}
	if step := s.stepOf[newest.CampaignID]; step != nil {
		out.StepID = *step
	}
	return out
}

// campaignTemplates returns the template of each linked hidden campaign that
// has one
func (r *Program) campaignTemplates(
	ctx context.Context,
	links []database.ProgramCampaign,
) (map[uuid.UUID]uuid.UUID, error) {
	out := map[uuid.UUID]uuid.UUID{}
	if len(links) == 0 {
		return out, nil
	}
	ids := make([]*uuid.UUID, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.CampaignID)
	}
	for start := 0; start < len(ids); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(ids))
		rows := []struct {
			ID                 uuid.UUID
			CampaignTemplateID *uuid.UUID
		}{}
		if err := r.DB.WithContext(ctx).
			Model(&database.Campaign{}).
			Select("id", "campaign_template_id").
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(ids[start:end])}).
			Scan(&rows).Error; err != nil {
			return nil, errs.Wrap(err)
		}
		for _, row := range rows {
			if row.CampaignTemplateID != nil {
				out[row.ID] = *row.CampaignTemplateID
			}
		}
	}
	return out, nil
}

// personMatch matches campaign recipient rows of a person by recipient id or
// by pseudonym, by whichever is given. With neither it matches no row.
func (r *Program) personMatch(recipientID *uuid.UUID, anonymizedID *uuid.UUID) clause.Expression {
	who := []clause.Expression{}
	if recipientID != nil {
		who = append(who, clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID})
	}
	if anonymizedID != nil {
		who = append(who, clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: anonymizedID})
	}
	if len(who) == 0 {
		return clause.IN{Column: clause.Column{Name: "id"}, Values: []any{}}
	}
	return clause.Or(who...)
}

// GetDeliveries returns every step given to the recipients of a program. When
// recipientIDs is not nil only those recipients' deliveries are returned.
func (r *Program) GetDeliveries(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
) ([]ProgramDelivery, error) {
	if recipientIDs != nil && len(recipientIDs) == 0 {
		return []ProgramDelivery{}, nil
	}
	links := []database.ProgramCampaign{}
	if err := r.DB.WithContext(ctx).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Find(&links).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	type sent struct {
		CampaignID  uuid.UUID
		RecipientID uuid.UUID
	}
	sends := []sent{}
	if recipientIDs == nil {
		if err := r.deliveredSends().
			WithContext(ctx).
			Select("campaign_id", "recipient_id").
			Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
			Where(clause.Neq{Column: clause.Column{Name: "recipient_id"}, Value: nil}).
			Scan(&sends).Error; err != nil {
			return nil, errs.Wrap(err)
		}
	}
	// the people are read a few at a time through the recipient index, see
	// programPeopleReadChunk
	for start := 0; recipientIDs != nil && start < len(recipientIDs); start += programPeopleReadChunk {
		part := []sent{}
		if err := r.deliveredSends().
			WithContext(ctx).
			Select("campaign_id", "recipient_id").
			Where(r.inProgramCampaigns(programID, false)).
			Where(clause.IN{Column: clause.Column{Name: "recipient_id"}, Values: uuidValues(recipientIDs[start:min(start+programPeopleReadChunk, len(recipientIDs))])}).
			Scan(&part).Error; err != nil {
			return nil, errs.Wrap(err)
		}
		sends = append(sends, part...)
	}
	templates, err := r.campaignTemplates(ctx, links)
	if err != nil {
		return nil, err
	}
	byCampaign := map[uuid.UUID]database.ProgramCampaign{}
	for _, l := range links {
		byCampaign[*l.CampaignID] = l
	}
	out := make([]ProgramDelivery, 0, len(sends))
	for _, sd := range sends {
		l, ok := byCampaign[sd.CampaignID]
		if !ok {
			continue
		}
		d := ProgramDelivery{RecipientID: sd.RecipientID, StepID: *l.ProgramStepID, LoopIndex: l.LoopIndex}
		if t, ok := templates[sd.CampaignID]; ok {
			d.TemplateID = &t
		}
		out = append(out, d)
	}
	return out, nil
}

// programCampaignIDs selects the ids of the hidden campaigns a program owns
func (r *Program) programCampaignIDs(programID *uuid.UUID) *gorm.DB {
	return r.DB.Model(&database.Campaign{}).
		Select("id").
		Where(programIDEq(clause.Column{Name: "program_id"}, programID))
}

// unsentSends matches the rows that are neither sent nor cancelled. Both
// predicates are written as the partial index
// idx_campaign_recipients_unsent_campaign states them, so a read of the unsent
// rows of some campaigns uses that index and not the sent_at or cancelled_at
// index, which hold every row of every campaign.
func unsentSends() clause.Expression {
	return clause.And(
		clause.Eq{Column: clause.Column{Name: "sent_at"}, Value: nil},
		clause.Eq{Column: clause.Column{Name: "cancelled_at"}, Value: nil},
	)
}

// untakenSends matches the unsent rows the sender has not taken. A row it took
// may have gone out before sent_at was written, so it is never cancelled,
// moved or deleted. Every read that finds rows to change and the write that
// changes them use it, so a taken row is never read again and again.
func untakenSends() clause.Expression {
	return clause.And(
		unsentSends(),
		clause.Eq{Column: clause.Column{Name: "last_attempt_at"}, Value: nil},
	)
}

// CancelUnsentForProgramRecipients marks the not yet sent step sends of the
// given recipients as cancelled, so program leavers receive no further mail.
// The recipient ids are read in batches to stay under the bind parameter
// limit.
func (r *Program) CancelUnsentForProgramRecipients(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
) error {
	now := utils.NowRFC3339UTC()
	for start := 0; start < len(recipientIDs); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(recipientIDs))
		res := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			// read through the unsent campaign index by campaign and person
			Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
			Where(clause.IN{Column: clause.Column{Name: "recipient_id"}, Values: uuidValues(recipientIDs[start:end])}).
			Where(untakenSends()).
			Updates(map[string]any{
				"cancelled_at": now,
				"updated_at":   now,
			})
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
	}
	return nil
}

// ProgramStepUnsentSend is a send of a program step that is neither sent nor
// cancelled
type ProgramStepUnsentSend struct {
	ID           uuid.UUID
	RecipientID  *uuid.UUID
	AnonymizedID *uuid.UUID
	SendAt       *time.Time
}

// GetUnsentSendsOfSteps returns the sends of the hidden campaigns of the given
// steps, in every loop, that are neither sent nor cancelled. The step ids are
// read in batches to stay under the bind parameter limit.
func (r *Program) GetUnsentSendsOfSteps(
	ctx context.Context,
	programID *uuid.UUID,
	stepIDs []*uuid.UUID,
) ([]ProgramStepUnsentSend, error) {
	out := []ProgramStepUnsentSend{}
	for start := 0; start < len(stepIDs); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(stepIDs))
		stepCampaigns := r.DB.Model(&database.ProgramCampaign{}).
			Select("campaign_id").
			Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
			Where(clause.IN{Column: clause.Column{Name: "program_step_id"}, Values: uuidValues(stepIDs[start:end])})
		rows := []ProgramStepUnsentSend{}
		if err := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Select("id", "recipient_id", "anonymized_id", "send_at").
			Where("campaign_id IN (?)", stepCampaigns).
			Where(untakenSends()).
			Scan(&rows).Error; err != nil {
			return nil, errs.Wrap(err)
		}
		out = append(out, rows...)
	}
	return out, nil
}

// CancelSends marks the given sends as cancelled when they are still neither
// sent nor cancelled and returns how many it cancelled. At most
// ProgramWriteChunk sends are written in one transaction that only writes.
func (r *Program) CancelSends(
	ctx context.Context,
	campaignRecipientIDs []*uuid.UUID,
) (int, error) {
	cancelled := 0
	now := utils.NowRFC3339UTC()
	for chunk := 0; chunk < len(campaignRecipientIDs); chunk += ProgramWriteChunk {
		ids := campaignRecipientIDs[chunk:min(chunk+ProgramWriteChunk, len(campaignRecipientIDs))]
		n := 0
		err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for start := 0; start < len(ids); start += programEnrollmentBatch {
				end := min(start+programEnrollmentBatch, len(ids))
				// found by id: the sent_at and cancelled_at predicates are not
				// read through an index, or sqlite reads every unsent row
				res := tx.Model(&database.CampaignRecipient{}).
					Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(ids[start:end])}).
					Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "sent_at"})}}).
					Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "cancelled_at"})}}).
					// a row the sender took may have gone out already
					Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "last_attempt_at"})}}).
					Updates(map[string]any{
						"cancelled_at": now,
						"updated_at":   now,
					})
				if res.Error != nil {
					return res.Error
				}
				n += int(res.RowsAffected)
			}
			return nil
		})
		if err != nil {
			return cancelled, errs.Wrap(err)
		}
		cancelled += n
	}
	return cancelled, nil
}

// GetUnsentSendIDsFrom returns the ids of the not yet sent sends of a program
// that are planned at or after the given time. The rows are found in one read
// through the unsent campaign index, with send_at checked on its rows.
func (r *Program) GetUnsentSendIDsFrom(
	ctx context.Context,
	programID *uuid.UUID,
	at time.Time,
) ([]*uuid.UUID, error) {
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
		Where(untakenSends()).
		Where(clause.Expr{
			SQL:  "? >= ?",
			Vars: []any{notIndexed(clause.Column{Name: "send_at"}), utils.RFC3339UTC(at)},
		}), "id")
}

// CancelUnsentFrom marks the not yet sent sends of a program that are planned
// at or after the given time as cancelled, see GetUnsentSendIDsFrom. The rows
// are cancelled by id in chunks, back to back. The program service reads the
// ids and cancels them in chunks with a pause between them instead.
func (r *Program) CancelUnsentFrom(
	ctx context.Context,
	programID *uuid.UUID,
	at time.Time,
) error {
	ids, err := r.GetUnsentSendIDsFrom(ctx, programID, at)
	if err != nil {
		return err
	}
	_, err = r.CancelSends(ctx, ids)
	return err
}

// ProgramQueuedSend is a send of a program that waits in an open hidden
// campaign, with the template that campaign sends
type ProgramQueuedSend struct {
	RecipientID uuid.UUID
	CampaignID  uuid.UUID
	SendAt      *time.Time
	TemplateID  *uuid.UUID
}

// GetQueuedSends returns the sends of a program that are not sent or cancelled
// yet, in campaigns that are open. When recipientIDs is not nil only those
// recipients' sends are returned.
func (r *Program) GetQueuedSends(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
) ([]ProgramQueuedSend, error) {
	if recipientIDs != nil && len(recipientIDs) == 0 {
		return []ProgramQueuedSend{}, nil
	}
	openCampaigns := r.DB.Model(&database.Campaign{}).
		Select("id").
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil})
	rows := []ProgramQueuedSend{}
	db := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("recipient_id", "campaign_id", "send_at").
		Where("campaign_id IN (?)", openCampaigns)
	if recipientIDs == nil {
		db = db.
			Where(untakenSends()).
			Where(clause.Neq{Column: clause.Column{Name: "recipient_id"}, Value: nil})
	} else {
		// a few people are found through the campaign and recipient index, not
		// through the sent_at or cancelled_at index that holds every campaign
		db = db.
			Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "sent_at"})}}).
			Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "cancelled_at"})}}).
			Where(clause.IN{Column: clause.Column{Name: "recipient_id"}, Values: uuidValues(recipientIDs)})
	}
	if err := db.Scan(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	if len(rows) == 0 {
		return rows, nil
	}
	links := make([]database.ProgramCampaign, 0, len(rows))
	seen := map[uuid.UUID]bool{}
	for _, row := range rows {
		if seen[row.CampaignID] {
			continue
		}
		seen[row.CampaignID] = true
		id := row.CampaignID
		links = append(links, database.ProgramCampaign{CampaignID: &id})
	}
	templates, err := r.campaignTemplates(ctx, links)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if t, ok := templates[rows[i].CampaignID]; ok {
			tid := t
			rows[i].TemplateID = &tid
		}
	}
	return rows, nil
}

// GetCampaignLinks returns the link rows of every hidden campaign of a program,
// with the step and loop each one sends
func (r *Program) GetCampaignLinks(ctx context.Context, programID *uuid.UUID) ([]database.ProgramCampaign, error) {
	links := []database.ProgramCampaign{}
	if err := r.DB.WithContext(ctx).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Find(&links).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	return links, nil
}

// GetTemplateNames returns the names of the given templates in one read per
// batch. A template that does not exist is left out.
func (r *Program) GetTemplateNames(ctx context.Context, ids []*uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	for start := 0; start < len(ids); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(ids))
		rows := []struct {
			ID   uuid.UUID
			Name string
		}{}
		if err := r.DB.WithContext(ctx).
			Model(&database.CampaignTemplate{}).
			Select("id", "name").
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(ids[start:end])}).
			Scan(&rows).Error; err != nil {
			return nil, errs.Wrap(err)
		}
		for _, row := range rows {
			out[row.ID] = row.Name
		}
	}
	return out, nil
}
