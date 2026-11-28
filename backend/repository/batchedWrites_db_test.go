package repository

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// addEvents stores n events of one type for a campaign, each for a new person
func addEvents(t *testing.T, db *gorm.DB, campaignID, eventID *uuid.UUID, n int) {
	t.Helper()
	rows := make([]*database.CampaignEvent, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, &database.CampaignEvent{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			CampaignID: campaignID, RecipientID: ptrUUID(), EventID: eventID,
		})
	}
	if err := db.CreateInBatches(rows, 500).Error; err != nil {
		t.Fatalf("add events: %v", err)
	}
}

// addSends stores n sends not yet sent for a campaign and returns their ids
func addSends(t *testing.T, db *gorm.DB, campaignID *uuid.UUID, n int) []*uuid.UUID {
	t.Helper()
	rows := make([]*database.CampaignRecipient, 0, n)
	ids := make([]*uuid.UUID, 0, n)
	for i := 0; i < n; i++ {
		id := ptrUUID()
		ids = append(ids, id)
		rows = append(rows, &database.CampaignRecipient{
			ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			CampaignID: campaignID, RecipientID: ptrUUID(),
		})
	}
	if err := db.CreateInBatches(rows, 500).Error; err != nil {
		t.Fatalf("add sends: %v", err)
	}
	return ids
}

func countWhere(t *testing.T, db *gorm.DB, table any, column string, value any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(table).Where(clause.Eq{Column: clause.Column{Name: column}, Value: value}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestCampaignDeletesInBatches checks the batched deletes of events, sends
// and enrollments remove every row of the campaign or program over several
// batches and leave the rows of others alone
func TestCampaignDeletesInBatches(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	camp, other, eventID := ptrUUID(), ptrUUID(), ptrUUID()
	n := deleteBatch*2 + 7
	addEvents(t, db, camp, eventID, n)
	addEvents(t, db, other, eventID, 3)
	addSends(t, db, camp, n)
	addSends(t, db, other, 3)

	campaigns := &Campaign{DB: db}
	if err := campaigns.DeleteEventsByCampaignID(ctx, camp); err != nil {
		t.Fatalf("delete events: %v", err)
	}
	if err := (&CampaignRecipient{DB: db}).DeleteByCampaigID(ctx, camp); err != nil {
		t.Fatalf("delete sends: %v", err)
	}
	if got := countWhere(t, db, &database.CampaignEvent{}, "campaign_id", camp); got != 0 {
		t.Fatalf("%d events left", got)
	}
	if got := countWhere(t, db, &database.CampaignRecipient{}, "campaign_id", camp); got != 0 {
		t.Fatalf("%d sends left", got)
	}
	if countWhere(t, db, &database.CampaignEvent{}, "campaign_id", other) != 3 ||
		countWhere(t, db, &database.CampaignRecipient{}, "campaign_id", other) != 3 {
		t.Fatal("the rows of another campaign must stay")
	}

	// a program with more enrollments than one batch
	r := &Program{DB: db}
	programID := createProgram(t, db, "p", nil)
	keep := createProgram(t, db, "keep", nil)
	enrollments := []*database.ProgramEnrollment{}
	for i := 0; i < deleteBatch+5; i++ {
		enrollments = append(enrollments, &database.ProgramEnrollment{ProgramID: programID, RecipientID: ptrUUID(), Status: model.ProgramEnrollmentActive})
	}
	enrollments = append(enrollments, &database.ProgramEnrollment{ProgramID: keep, RecipientID: ptrUUID(), Status: model.ProgramEnrollmentActive})
	if err := r.InsertEnrollments(ctx, enrollments); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if err := r.DeleteByID(ctx, programID); err != nil {
		t.Fatalf("delete program: %v", err)
	}
	if got := countWhere(t, db, &database.ProgramEnrollment{}, "program_id", programID); got != 0 {
		t.Fatalf("%d enrollments left", got)
	}
	if countWhere(t, db, &database.ProgramEnrollment{}, "program_id", keep) != 1 {
		t.Fatal("the enrollment of another program must stay")
	}
}

// TestCampaignDeleteAfterLateEvent checks a campaign delete goes through when
// a tracking event was saved after its events were deleted, with foreign keys
// enforced as the app runs
func TestCampaignDeleteAfterLateEvent(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "fk.db") + "?_foreign_keys=ON&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&database.Campaign{}, &database.CampaignEvent{}, &database.CampaignRecipient{},
		&database.CampaignTemplatePool{}, &database.CampaignLanguageRoute{}, &database.CampaignStats{}, &database.Recipient{}, &database.Event{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	eventID := ptrUUID()
	if err := db.Create(&database.Event{ID: eventID, CreatedAt: ptrNow(), Name: "clicked"}).Error; err != nil {
		t.Fatalf("event type: %v", err)
	}
	camp := ptrUUID()
	if err := db.Create(&database.Campaign{ID: camp, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "c"}).Error; err != nil {
		t.Fatalf("campaign: %v", err)
	}
	add := func() {
		if err := db.Create(&database.CampaignEvent{ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(), CampaignID: camp, EventID: eventID}).Error; err != nil {
			t.Fatalf("add event: %v", err)
		}
	}
	add()
	r := &Campaign{DB: db}
	if err := r.DeleteEventsByCampaignID(ctx, camp); err != nil {
		t.Fatalf("delete events: %v", err)
	}
	// the late event
	add()
	if err := db.Where("id = ?", camp).Delete(&database.Campaign{}).Error; err == nil {
		t.Fatal("foreign keys must be enforced for this test")
	}
	if err := r.DeleteByID(ctx, camp); err != nil {
		t.Fatalf("delete campaign: %v", err)
	}
	if countWhere(t, db, &database.Campaign{}, "id", camp) != 0 || countWhere(t, db, &database.CampaignEvent{}, "campaign_id", camp) != 0 {
		t.Fatal("the campaign and its events must be gone")
	}
}

// TestCancelOverBindLimit checks Cancel takes more ids than sqlite binds in
// one statement and cancels every row, setting the cancelled notable event
// on the rows not sent
func TestCancelOverBindLimit(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	cancelledID, scheduledID := ptrUUID(), ptrUUID()
	prevCancelled := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_CANCELLED]
	prevScheduled := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED]
	cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_CANCELLED] = cancelledID
	cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED] = scheduledID
	t.Cleanup(func() {
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_CANCELLED] = prevCancelled
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED] = prevScheduled
	})
	camp := ptrUUID()
	ids := addSends(t, db, camp, 32766+50)
	// one row sent, one with a notable event that is kept
	sent, clicked := ids[0], ids[1]
	clickID := ptrUUID()
	db.Model(&database.CampaignRecipient{}).Where("id = ?", sent).Update("sent_at", utils.NowRFC3339UTC())
	db.Model(&database.CampaignRecipient{}).Where("id = ?", clicked).Update("notable_event_id", clickID)
	db.Model(&database.CampaignRecipient{}).Where("id = ?", ids[2]).Update("notable_event_id", scheduledID)

	if err := (&CampaignRecipient{DB: db}).Cancel(ctx, ids); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	var notCancelled, notable int64
	db.Model(&database.CampaignRecipient{}).Where("campaign_id = ? AND cancelled_at IS NULL", camp).Count(&notCancelled)
	db.Model(&database.CampaignRecipient{}).Where("campaign_id = ? AND notable_event_id = ?", camp, cancelledID).Count(&notable)
	if notCancelled != 0 {
		t.Fatalf("%d rows not cancelled", notCancelled)
	}
	if notable != int64(len(ids)-2) {
		t.Fatalf("cancelled notable event on %d rows, want %d", notable, len(ids)-2)
	}
	var kept database.CampaignRecipient
	db.Where("id = ?", clicked).First(&kept)
	if kept.NotableEventID == nil || *kept.NotableEventID != *clickID {
		t.Fatal("a notable event other than scheduled must be kept")
	}
}

// TestRepeatOffenderWindowBoundary checks the repeat offender window starts at
// the given time, not at the start of its day. created_at is RFC3339 text, an
// event an hour before the window on the same day is outside it.
func TestRepeatOffenderWindowBoundary(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	clickID := ptrUUID()
	prev := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED]
	cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED] = clickID
	t.Cleanup(func() { cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED] = prev })
	since := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	inside, outside := ptrUUID(), ptrUUID()
	email := func(s string) *string { return &s }
	for id, address := range map[*uuid.UUID]string{inside: "in@example.com", outside: "out@example.com"} {
		if err := db.Create(&database.Recipient{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Email: email(address)}).Error; err != nil {
			t.Fatalf("recipient: %v", err)
		}
	}
	click := func(person *uuid.UUID, at time.Time) {
		camp := ptrUUID()
		if err := db.Create(&database.Campaign{ID: camp, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "c " + camp.String()}).Error; err != nil {
			t.Fatalf("campaign: %v", err)
		}
		id := ptrUUID()
		if err := db.Create(&database.CampaignEvent{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), CampaignID: camp, RecipientID: person, EventID: clickID}).Error; err != nil {
			t.Fatalf("event: %v", err)
		}
		// stored the way the app stores it
		db.Model(&database.CampaignEvent{}).Where("id = ?", id).Update("created_at", utils.RFC3339UTC(at))
	}
	click(inside, since.Add(time.Hour))
	click(inside, since.Add(2*time.Hour))
	click(outside, since.Add(time.Hour))
	click(outside, since.Add(-time.Hour))

	r := &Recipient{DB: db}
	var ids []string
	if err := r.repeatOffenderIDs(since).WithContext(ctx).Scan(&ids).Error; err != nil {
		t.Fatalf("repeat offenders: %v", err)
	}
	if len(ids) != 1 || ids[0] != inside.String() {
		t.Fatalf("repeat offenders %v, want only %s", ids, inside)
	}
	count := func(person *uuid.UUID) int64 {
		var n int64
		if err := db.Table(database.RECIPIENT_TABLE).
			Select("(?)", r.repeatOffenderCampaignCount(since)).
			Where("id = ?", person).Scan(&n).Error; err != nil {
			t.Fatalf("campaign count: %v", err)
		}
		return n
	}
	if got := count(inside); got != 2 {
		t.Fatalf("inside: %d campaigns, want 2", got)
	}
	if got := count(outside); got != 1 {
		t.Fatalf("outside: %d campaigns, want 1", got)
	}
}

// TestPauseBetweenWritesRespectsCancel checks the pause between chunked writes
// waits its time and ends early with the context error once the context ends
func TestPauseBetweenWritesRespectsCancel(t *testing.T) {
	start := time.Now()
	if err := PauseBetweenWrites(context.Background()); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if took := time.Since(start); took < WriteChunkPause {
		t.Fatalf("pause took %v, want at least %v", took, WriteChunkPause)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	if err := PauseBetweenWrites(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pause on an ended context: %v", err)
	}
	if took := time.Since(start); took >= WriteChunkPause {
		t.Fatalf("pause on an ended context took %v", took)
	}
	// a batched delete stops at the first pause of an ended context and keeps
	// the rows it did not reach
	db := newProgramTestDB(t)
	camp, eventID := ptrUUID(), ptrUUID()
	addEvents(t, db, camp, eventID, deleteBatch*3)
	ctx, cancel = context.WithCancel(context.Background())
	db.Callback().Delete().After("gorm:delete").Register("zz_cancel_after_delete", func(*gorm.DB) { cancel() })
	where := clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: camp}
	err := deleteInBatches(ctx, db, &database.CampaignEvent{}, where, deleteBatch)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("delete on a cancelled context: %v", err)
	}
	if got := countWhere(t, db, &database.CampaignEvent{}, "campaign_id", camp); got != int64(deleteBatch*2) {
		t.Fatalf("%d events left, want %d", got, deleteBatch*2)
	}
	// the delete of a campaign finishes also when its request ends, so the
	// campaign is never left half deleted
	if err := (&Campaign{DB: db}).DeleteEventsByCampaignID(ctx, camp); err != nil {
		t.Fatalf("campaign delete on a cancelled context: %v", err)
	}
	if got := countWhere(t, db, &database.CampaignEvent{}, "campaign_id", camp); got != 0 {
		t.Fatalf("%d events left, want none", got)
	}
}

// TestStopEnrollmentsInChunks checks every active person of a program is
// stopped and every pending send time cleared over more than one chunk, and
// other programs are left alone
func TestStopEnrollmentsInChunks(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "p", nil)
	other := createProgram(t, db, "other", nil)
	due := time.Now().Add(time.Hour)
	rows := []*database.ProgramEnrollment{}
	for i := 0; i < ProgramWriteChunk*2+3; i++ {
		rows = append(rows, &database.ProgramEnrollment{ProgramID: programID, RecipientID: ptrUUID(), Status: model.ProgramEnrollmentActive, NextDueAt: &due})
	}
	// a person who left but still has a due time
	rows = append(rows, &database.ProgramEnrollment{ProgramID: programID, RecipientID: ptrUUID(), Status: model.ProgramEnrollmentExited, NextDueAt: &due})
	rows = append(rows, &database.ProgramEnrollment{ProgramID: other, RecipientID: ptrUUID(), Status: model.ProgramEnrollmentActive, NextDueAt: &due})
	if err := r.InsertEnrollments(ctx, rows); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if err := r.StopEnrollments(ctx, programID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	var active, pending, stopped int64
	db.Model(&database.ProgramEnrollment{}).Where("program_id = ? AND status = ?", programID, model.ProgramEnrollmentActive).Count(&active)
	db.Model(&database.ProgramEnrollment{}).Where("program_id = ? AND next_due_at IS NOT NULL", programID).Count(&pending)
	db.Model(&database.ProgramEnrollment{}).Where("program_id = ? AND status = ?", programID, model.ProgramEnrollmentStopped).Count(&stopped)
	if active != 0 || pending != 0 || stopped != int64(ProgramWriteChunk*2+3) {
		t.Fatalf("active %d pending %d stopped %d", active, pending, stopped)
	}
	var untouched int64
	db.Model(&database.ProgramEnrollment{}).Where("program_id = ? AND status = ? AND next_due_at IS NOT NULL", other, model.ProgramEnrollmentActive).Count(&untouched)
	if untouched != 1 {
		t.Fatal("another program must be left alone")
	}
}

// TestDeleteBatchSizes checks a batched delete of sends starts with
// deleteBatchSends rows per write and a batched delete of events with
// deleteBatch rows, that a later write removes no fewer rows than the first
// and no more than deleteBatchMax, and that every row is deleted
func TestDeleteBatchSizes(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	camp, eventID := ptrUUID(), ptrUUID()
	addSends(t, db, camp, deleteBatchSends*2+3)
	addEvents(t, db, camp, eventID, deleteBatch+3)
	writes := map[string][]int64{}
	db.Callback().Delete().After("gorm:delete").Register("zz_count_rows", func(tx *gorm.DB) {
		writes[tx.Statement.Table] = append(writes[tx.Statement.Table], tx.RowsAffected)
	})
	if err := (&CampaignRecipient{DB: db}).DeleteByCampaigID(ctx, camp); err != nil {
		t.Fatalf("delete sends: %v", err)
	}
	if err := (&Campaign{DB: db}).DeleteEventsByCampaignID(ctx, camp); err != nil {
		t.Fatalf("delete events: %v", err)
	}
	for table, want := range map[string]struct{ first, total int64 }{
		database.CAMPAIGN_RECIPIENT_TABLE_NAME: {deleteBatchSends, deleteBatchSends*2 + 3},
		database.CAMPAIGN_EVENT_TABLE:          {deleteBatch, deleteBatch + 3},
	} {
		got := writes[table]
		if len(got) < 2 || got[0] != want.first {
			t.Fatalf("%s writes %v, want a first write of %d", table, got, want.first)
		}
		total := int64(0)
		for i, n := range got {
			if n > deleteBatchMax || (i > 0 && i < len(got)-1 && n < want.first) {
				t.Fatalf("%s writes %v out of bounds", table, got)
			}
			total += n
		}
		if total != want.total {
			t.Fatalf("%s writes %v, want %d rows in all", table, got, want.total)
		}
	}
}

// TestPacedUpdateReadsThenWrites checks a paced update reads the rowids of a
// chunk before it writes them, leaves a row another writer changed between
// the read and the write, and walks the unlinked rows of an anonymized
// campaign by rowid over more than one chunk
func TestPacedUpdateReadsThenWrites(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	repo := &CampaignRecipient{DB: db}

	// a row sent between the read and the write is not cancelled
	camp := ptrUUID()
	addSends(t, db, camp, ProgramWriteChunk+5)
	var sentMeanwhile int64
	once := false
	if err := db.Callback().Query().After("gorm:query").Register("zz_send_meanwhile", func(tx *gorm.DB) {
		rowids, ok := tx.Statement.Dest.(*[]int64)
		if once || !ok || len(*rowids) == 0 {
			return
		}
		once = true
		sentMeanwhile = (*rowids)[0]
		res := db.Session(&gorm.Session{NewDB: true}).Model(&database.CampaignRecipient{}).
			Where("rowid = ?", sentMeanwhile).Update("sent_at", utils.NowRFC3339UTC())
		if res.Error != nil || res.RowsAffected != 1 {
			t.Errorf("send meanwhile: %v %d", res.Error, res.RowsAffected)
		}
	}); err != nil {
		t.Fatalf("callback: %v", err)
	}
	members := db.Session(&gorm.Session{NewDB: true}).Model(&database.CampaignRecipient{}).
		Select("recipient_id").Where("campaign_id = ?", camp)
	if err := repo.CancelUnsentOfMembersInCampaign(ctx, camp, members, nil); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := db.Callback().Query().Remove("zz_send_meanwhile"); err != nil {
		t.Fatalf("remove callback: %v", err)
	}
	if !once {
		t.Fatal("the rows must be read before the write")
	}
	var sent database.CampaignRecipient
	db.Where("rowid = ?", sentMeanwhile).First(&sent)
	if sent.CancelledAt != nil {
		t.Fatalf("a row sent between the read and the write must not be cancelled: sent %v cancelled %v", sent.SentAt, sent.CancelledAt)
	}
	var open int64
	db.Model(&database.CampaignRecipient{}).Where("campaign_id = ? AND sent_at IS NULL AND cancelled_at IS NULL", camp).Count(&open)
	if open != 0 {
		t.Fatalf("%d rows left uncancelled", open)
	}

	// the unlinked rows of an anonymous campaign keep their place in the
	// campaign and recipient index after the write, every one is released
	anon := ptrUUID()
	unlinked := addSends(t, db, anon, 2*ProgramWriteChunk+3)
	if err := db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", anon).
		Updates(map[string]any{"recipient_id": nil, "anonymized_id": gorm.Expr("id"), "message_id": gorm.Expr("id")}).Error; err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if err := repo.AnonymizeRowsOfCampaign(ctx, anon); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	var kept int64
	db.Model(&database.CampaignRecipient{}).Where("campaign_id = ? AND message_id IS NOT NULL", anon).Count(&kept)
	if kept != 0 || len(unlinked) == 0 {
		t.Fatalf("%d unlinked rows kept their message id", kept)
	}
}

// TestWritePacer checks the pacer lets short runs of writes go on without a
// pause and pauses once the writes since the last pause took half of
// MaxBusyWait
func TestWritePacer(t *testing.T) {
	defer func(v time.Duration) { WriteChunkPause = v }(WriteChunkPause)
	WriteChunkPause = 30 * time.Millisecond
	ctx := context.Background()
	var p WritePacer
	start := time.Now()
	for i := 0; i < 5; i++ {
		if err := p.Pause(ctx); err != nil {
			t.Fatalf("pause: %v", err)
		}
	}
	if took := time.Since(start); took >= WriteChunkPause {
		t.Fatalf("a short run paused for %v", took)
	}
	time.Sleep(MaxBusyWait / 2)
	start = time.Now()
	if err := p.Pause(ctx); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if took := time.Since(start); took < WriteChunkPause {
		t.Fatalf("a long run paused for %v, want at least %v", took, WriteChunkPause)
	}
	// the run starts again after a pause
	start = time.Now()
	if err := p.Pause(ctx); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if took := time.Since(start); took >= WriteChunkPause {
		t.Fatalf("the run after a pause paused for %v", took)
	}
	// an ended context ends a pause that is due
	time.Sleep(MaxBusyWait / 2)
	ended, cancel := context.WithCancel(ctx)
	cancel()
	if err := p.Pause(ended); !errors.Is(err, context.Canceled) {
		t.Fatalf("pause on an ended context: %v", err)
	}
}
