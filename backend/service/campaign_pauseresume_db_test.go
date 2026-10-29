package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newPauseResumeService builds a Campaign service with only the dependencies the
// pause and resume paths touch, on a temp database with the tables they read and
// write. It also registers the two lifecycle events in the id cache so the
// timeline event write finds an id.
func newPauseResumeService(t *testing.T) (*Campaign, *gorm.DB) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "pauseresume.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Campaign{},
		&database.CampaignRecipient{},
		&database.Recipient{},
		&database.CampaignEvent{},
		&database.Event{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pausedID := uuid.New()
	resumedID := uuid.New()
	cache.EventIDByName[data.EVENT_CAMPAIGN_PAUSED] = &pausedID
	cache.EventIDByName[data.EVENT_CAMPAIGN_RESUMED] = &resumedID

	svc := &Campaign{
		Common:                      Common{Logger: zap.NewNop().Sugar()},
		CampaignRepository:          &repository.Campaign{DB: db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: db},
	}
	return svc, db
}

func authorizedSession(t *testing.T) *model.Session {
	t.Helper()
	uid := uuid.New()
	return &model.Session{
		IP: "127.0.0.1",
		User: &model.User{
			ID:   nullable.NewNullableWithValue(uid),
			Role: &model.Role{ID: uuid.New(), Name: data.RoleSuperAdministrator},
		},
	}
}

func unauthorizedSession(t *testing.T) *model.Session {
	t.Helper()
	uid := uuid.New()
	return &model.Session{
		IP: "127.0.0.1",
		User: &model.User{
			ID:   nullable.NewNullableWithValue(uid),
			Role: &model.Role{ID: uuid.New(), Name: data.RoleCompanyUser},
		},
	}
}

type seededCampaign struct {
	id          uuid.UUID
	recipientID []uuid.UUID
}

// seedCampaign writes a campaign row. sendStart/sendEnd nil makes it self
// managed. closeAt/anonymizeAt set the corresponding schedule dates.
func seedCampaign(t *testing.T, db *gorm.DB, sendStart, sendEnd, closeAt, anonymizeAt *time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := utils.NowRFC3339UTC()
	row := map[string]any{
		"id":         id,
		"created_at": now,
		"updated_at": now,
		"name":       "pause resume " + id.String(),
	}
	if sendStart != nil {
		row["send_start_at"] = utils.RFC3339UTC(*sendStart)
	}
	if sendEnd != nil {
		row["send_end_at"] = utils.RFC3339UTC(*sendEnd)
	}
	if closeAt != nil {
		row["close_at"] = utils.RFC3339UTC(*closeAt)
	}
	if anonymizeAt != nil {
		row["anonymize_at"] = utils.RFC3339UTC(*anonymizeAt)
	}
	if err := db.Model(&database.Campaign{}).Create(row).Error; err != nil {
		t.Fatalf("seed campaign: %v", err)
	}
	return id
}

// seedRecipient writes one campaign recipient. attempted marks it as already
// attempted (last_attempt_at set), which takes it out of the sendable set.
func seedRecipient(t *testing.T, db *gorm.DB, campaignID uuid.UUID, sendAt time.Time, attempted bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := utils.NowRFC3339UTC()
	row := map[string]any{
		"id":           id,
		"created_at":   now,
		"updated_at":   now,
		"campaign_id":  campaignID,
		"recipient_id": uuid.New(),
		"send_at":      utils.RFC3339UTC(sendAt),
		"self_managed": false,
	}
	if attempted {
		row["last_attempt_at"] = now
	}
	if err := db.Model(&database.CampaignRecipient{}).Create(row).Error; err != nil {
		t.Fatalf("seed recipient: %v", err)
	}
	return id
}

// markClosed sets closed_at so the guard that rejects acting on a closed
// campaign can be exercised.
func markClosed(t *testing.T, db *gorm.DB, campaignID uuid.UUID) {
	t.Helper()
	if err := db.Model(&database.Campaign{}).
		Where("id = ?", campaignID).
		Update("closed_at", utils.NowRFC3339UTC()).Error; err != nil {
		t.Fatalf("mark closed: %v", err)
	}
}

func countCampaignEvents(t *testing.T, db *gorm.DB, campaignID uuid.UUID, eventID *uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&database.CampaignEvent{}).
		Where("campaign_id = ? AND event_id = ?", campaignID, eventID).
		Count(&n).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

func pausedAtOf(t *testing.T, db *gorm.DB, campaignID uuid.UUID) *time.Time {
	t.Helper()
	var c database.Campaign
	if err := db.Where("id = ?", campaignID).First(&c).Error; err != nil {
		t.Fatalf("read campaign: %v", err)
	}
	return c.PausedAt
}

func TestPauseCampaignByID_Guards(t *testing.T) {
	ctx := context.Background()
	past := time.Now().Add(-2 * time.Hour)
	future := time.Now().Add(2 * time.Hour)

	t.Run("unauthorized is rejected", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		id := seedCampaign(t, db, &past, &future, nil, nil)
		seedRecipient(t, db, id, future, false)
		err := svc.PauseCampaignByID(ctx, unauthorizedSession(t), &id)
		if !errors.Is(err, errs.ErrAuthorizationFailed) {
			t.Fatalf("want ErrAuthorizationFailed, got %v", err)
		}
	})

	t.Run("self managed is rejected", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		id := seedCampaign(t, db, nil, nil, nil, nil) // no window = self managed
		seedRecipient(t, db, id, future, false)
		err := svc.PauseCampaignByID(ctx, authorizedSession(t), &id)
		if !errors.Is(err, errs.ErrCampaignCannotPause) {
			t.Fatalf("want ErrCampaignCannotPause, got %v", err)
		}
	})

	t.Run("not started is rejected", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		start := time.Now().Add(1 * time.Hour) // starts in the future
		id := seedCampaign(t, db, &start, &future, nil, nil)
		seedRecipient(t, db, id, future, false)
		err := svc.PauseCampaignByID(ctx, authorizedSession(t), &id)
		if !errors.Is(err, errs.ErrCampaignCannotPause) {
			t.Fatalf("want ErrCampaignCannotPause, got %v", err)
		}
	})

	t.Run("no remaining recipients is rejected", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		id := seedCampaign(t, db, &past, &future, nil, nil)
		seedRecipient(t, db, id, past, true) // already attempted
		err := svc.PauseCampaignByID(ctx, authorizedSession(t), &id)
		if !errors.Is(err, errs.ErrCampaignNothingToSend) {
			t.Fatalf("want ErrCampaignNothingToSend, got %v", err)
		}
	})

	t.Run("closed campaign is rejected", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		id := seedCampaign(t, db, &past, &future, nil, nil)
		seedRecipient(t, db, id, future, false)
		markClosed(t, db, id)
		err := svc.PauseCampaignByID(ctx, authorizedSession(t), &id)
		if !errors.Is(err, errs.ErrCampaignAlreadyClosed) {
			t.Fatalf("want ErrCampaignAlreadyClosed, got %v", err)
		}
	})
}

func TestPauseCampaignByID_Success(t *testing.T) {
	ctx := context.Background()
	svc, db := newPauseResumeService(t)
	past := time.Now().Add(-2 * time.Hour)
	future := time.Now().Add(2 * time.Hour)
	id := seedCampaign(t, db, &past, &future, nil, nil)
	seedRecipient(t, db, id, future, false)
	session := authorizedSession(t)

	if err := svc.PauseCampaignByID(ctx, session, &id); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if pausedAtOf(t, db, id) == nil {
		t.Fatalf("paused_at was not set")
	}
	if n := countCampaignEvents(t, db, id, cache.EventIDByName[data.EVENT_CAMPAIGN_PAUSED]); n != 1 {
		t.Fatalf("want 1 paused event, got %d", n)
	}
	// pausing again is rejected
	if err := svc.PauseCampaignByID(ctx, session, &id); !errors.Is(err, errs.ErrCampaignAlreadyPaused) {
		t.Fatalf("double pause: want ErrCampaignAlreadyPaused, got %v", err)
	}
}

func TestResumeCampaignByID_Guards(t *testing.T) {
	ctx := context.Background()
	past := time.Now().Add(-2 * time.Hour)
	future := time.Now().Add(2 * time.Hour)
	session := authorizedSession(t)

	windowReq := func(start, end time.Time) *model.Campaign {
		return &model.Campaign{
			SendStartAt: nullable.NewNullableWithValue(start),
			SendEndAt:   nullable.NewNullableWithValue(end),
		}
	}

	t.Run("not paused is rejected", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		id := seedCampaign(t, db, &past, &future, nil, nil)
		seedRecipient(t, db, id, future, false)
		err := svc.ResumeCampaignByID(ctx, session, &id, windowReq(time.Now().Add(time.Hour), time.Now().Add(3*time.Hour)))
		if !errors.Is(err, errs.ErrCampaignNotPaused) {
			t.Fatalf("want ErrCampaignNotPaused, got %v", err)
		}
	})

	t.Run("finish before start is rejected", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		id := seedCampaign(t, db, &past, &future, nil, nil)
		seedRecipient(t, db, id, future, false)
		mustPause(t, svc, db, session, id)
		start := time.Now().Add(3 * time.Hour)
		end := time.Now().Add(1 * time.Hour)
		if err := svc.ResumeCampaignByID(ctx, session, &id, windowReq(start, end)); err == nil {
			t.Fatalf("want validation error for finish before start, got nil")
		}
	})

	t.Run("window past close date is rejected", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		closeAt := time.Now().Add(2 * time.Hour)
		id := seedCampaign(t, db, &past, &future, &closeAt, nil)
		seedRecipient(t, db, id, future, false)
		mustPause(t, svc, db, session, id)
		// window ends after the close date
		err := svc.ResumeCampaignByID(ctx, session, &id, windowReq(time.Now().Add(30*time.Minute), time.Now().Add(3*time.Hour)))
		if err == nil || !strings.Contains(err.Error(), "close date") {
			t.Fatalf("want a close date validation error, got %v", err)
		}
	})

	t.Run("window past anonymize date is rejected", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		anonAt := time.Now().Add(2 * time.Hour)
		id := seedCampaign(t, db, &past, &future, nil, &anonAt)
		seedRecipient(t, db, id, future, false)
		mustPause(t, svc, db, session, id)
		err := svc.ResumeCampaignByID(ctx, session, &id, windowReq(time.Now().Add(30*time.Minute), time.Now().Add(3*time.Hour)))
		if err == nil || !strings.Contains(err.Error(), "anonymize date") {
			t.Fatalf("want an anonymize date validation error, got %v", err)
		}
	})

	t.Run("finish before start names the end field", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		id := seedCampaign(t, db, &past, &future, nil, nil)
		seedRecipient(t, db, id, future, false)
		mustPause(t, svc, db, session, id)
		err := svc.ResumeCampaignByID(ctx, session, &id, windowReq(time.Now().Add(3*time.Hour), time.Now().Add(1*time.Hour)))
		if err == nil || !strings.Contains(err.Error(), "after start") {
			t.Fatalf("want an end after start validation error, got %v", err)
		}
	})

	t.Run("closed campaign is rejected", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		id := seedCampaign(t, db, &past, &future, nil, nil)
		seedRecipient(t, db, id, future, false)
		markClosed(t, db, id)
		err := svc.ResumeCampaignByID(ctx, session, &id, windowReq(time.Now().Add(time.Hour), time.Now().Add(3*time.Hour)))
		if !errors.Is(err, errs.ErrCampaignAlreadyClosed) {
			t.Fatalf("want ErrCampaignAlreadyClosed, got %v", err)
		}
	})

	t.Run("paused with nothing to send is rejected", func(t *testing.T) {
		svc, db := newPauseResumeService(t)
		id := seedCampaign(t, db, &past, &future, nil, nil)
		seedRecipient(t, db, id, past, true) // only an attempted recipient remains
		// pause directly, since the pause guard would itself reject a campaign with
		// nothing to send, and we need a paused campaign to reach the resume guard
		if err := (&repository.Campaign{DB: db}).SetPausedByID(ctx, &id); err != nil {
			t.Fatalf("set paused: %v", err)
		}
		err := svc.ResumeCampaignByID(ctx, session, &id, windowReq(time.Now().Add(time.Hour), time.Now().Add(3*time.Hour)))
		if !errors.Is(err, errs.ErrCampaignNothingToSend) {
			t.Fatalf("want ErrCampaignNothingToSend, got %v", err)
		}
	})
}

func TestResumeCampaignByID_Reschedules(t *testing.T) {
	ctx := context.Background()
	svc, db := newPauseResumeService(t)
	session := authorizedSession(t)
	past := time.Now().Add(-2 * time.Hour)
	future := time.Now().Add(2 * time.Hour)
	id := seedCampaign(t, db, &past, &future, nil, nil)

	// three sendable recipients, all with send times in the past
	crIDs := []uuid.UUID{
		seedRecipient(t, db, id, past, false),
		seedRecipient(t, db, id, past.Add(time.Minute), false),
		seedRecipient(t, db, id, past.Add(2*time.Minute), false),
	}
	// one attempted recipient that must not be rescheduled
	attempted := seedRecipient(t, db, id, past, true)

	mustPause(t, svc, db, session, id)

	start := time.Now().Add(1 * time.Hour).Truncate(time.Minute)
	end := time.Now().Add(5 * time.Hour).Truncate(time.Minute)
	req := &model.Campaign{
		SendStartAt: nullable.NewNullableWithValue(start),
		SendEndAt:   nullable.NewNullableWithValue(end),
	}
	if err := svc.ResumeCampaignByID(ctx, session, &id, req); err != nil {
		t.Fatalf("resume: %v", err)
	}

	// paused flag cleared
	if pausedAtOf(t, db, id) != nil {
		t.Fatalf("paused_at should be cleared after resume")
	}
	// resumed event written
	if n := countCampaignEvents(t, db, id, cache.EventIDByName[data.EVENT_CAMPAIGN_RESUMED]); n != 1 {
		t.Fatalf("want 1 resumed event, got %d", n)
	}
	// every sendable recipient now sits inside the new window, and each gets its own
	// distinct time (a spread, not all clamped to one) which also guards the batch
	// loop against a shared-pointer capture bug writing the same id repeatedly
	seen := map[string]bool{}
	for _, crID := range crIDs {
		var row database.CampaignRecipient
		if err := db.Where("id = ?", crID).First(&row).Error; err != nil {
			t.Fatalf("read recipient: %v", err)
		}
		if row.SendAt == nil {
			t.Fatalf("recipient %s has no send time", crID)
		}
		got := row.SendAt.UTC()
		if got.Before(start.UTC().Add(-time.Second)) || got.After(end.UTC().Add(time.Second)) {
			t.Fatalf("recipient %s send time %s outside window [%s, %s]", crID, got, start.UTC(), end.UTC())
		}
		seen[got.Format(time.RFC3339)] = true
	}
	if len(seen) != len(crIDs) {
		t.Fatalf("want %d distinct send times across recipients, got %d (batch may not be assigning per recipient)", len(crIDs), len(seen))
	}
	// the attempted recipient keeps its old (past) send time
	var att database.CampaignRecipient
	if err := db.Where("id = ?", attempted).First(&att).Error; err != nil {
		t.Fatalf("read attempted: %v", err)
	}
	if att.SendAt == nil || !att.SendAt.UTC().Before(start.UTC()) {
		t.Fatalf("attempted recipient should keep its old send time, got %v", att.SendAt)
	}
}

// mustPause pauses a campaign and fails the test on error.
func mustPause(t *testing.T, svc *Campaign, db *gorm.DB, session *model.Session, id uuid.UUID) {
	t.Helper()
	if err := svc.PauseCampaignByID(context.Background(), session, &id); err != nil {
		t.Fatalf("pause setup: %v", err)
	}
}

// Resuming in Daily Slots mode must place every remaining recipient on a
// selected week day inside the daily time window, honoring the same scheduling
// options as creating a campaign.
func TestResumeCampaignByID_DailySlots(t *testing.T) {
	ctx := context.Background()
	svc, db := newPauseResumeService(t)
	session := authorizedSession(t)
	past := time.Now().Add(-2 * time.Hour)
	future := time.Now().Add(2 * time.Hour)
	id := seedCampaign(t, db, &past, &future, nil, nil)
	crIDs := []uuid.UUID{
		seedRecipient(t, db, id, past, false),
		seedRecipient(t, db, id, past.Add(time.Minute), false),
		seedRecipient(t, db, id, past.Add(2*time.Minute), false),
		seedRecipient(t, db, id, past.Add(3*time.Minute), false),
	}
	mustPause(t, svc, db, session, id)

	// a two week window starting tomorrow, Mon..Fri, 09:00-17:00 UTC
	start := time.Now().Add(24 * time.Hour).Truncate(24 * time.Hour)
	end := start.Add(14 * 24 * time.Hour).Add(-time.Minute)
	req := &model.Campaign{
		SendStartAt:         nullable.NewNullableWithValue(start),
		SendEndAt:           nullable.NewNullableWithValue(end),
		ConstraintWeekDays:  nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(62)),
		ConstraintStartTime: nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("09:00")),
		ConstraintEndTime:   nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("17:00")),
	}
	if err := svc.ResumeCampaignByID(ctx, session, &id, req); err != nil {
		t.Fatalf("resume daily slots: %v", err)
	}
	if pausedAtOf(t, db, id) != nil {
		t.Fatalf("paused_at should be cleared after resume")
	}
	for _, crID := range crIDs {
		var row database.CampaignRecipient
		if err := db.Where("id = ?", crID).First(&row).Error; err != nil {
			t.Fatalf("read recipient: %v", err)
		}
		if row.SendAt == nil {
			t.Fatalf("recipient %s has no send time", crID)
		}
		ts := row.SendAt.UTC()
		if wd := int(ts.Weekday()); wd < 1 || wd > 5 {
			t.Fatalf("recipient %s scheduled on a weekend (%s)", crID, ts)
		}
		mins := ts.Hour()*60 + ts.Minute()
		if mins < 9*60 || mins > 17*60 {
			t.Fatalf("recipient %s scheduled outside 09:00-17:00 (%s)", crID, ts)
		}
	}
}
