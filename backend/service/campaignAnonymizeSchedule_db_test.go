package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
)

// anonCampaign creates a campaign and sets fields on it. Times are written as
// the server writes them, RFC3339 in UTC.
func anonCampaign(p *programTest, fields map[string]any) uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	p.create(&database.Campaign{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "c-" + id.String()[:8]})
	if len(fields) > 0 {
		if err := p.db.Model(&database.Campaign{}).Where("id = ?", id).Updates(fields).Error; err != nil {
			p.t.Fatalf("set campaign fields: %v", err)
		}
	}
	return id
}

// ago is a time before now as the server stores it
func ago(d time.Duration) string {
	return utils.RFC3339UTC(time.Now().Add(-d))
}

// storedCampaign reads a campaign row
func storedCampaign(p *programTest, id uuid.UUID) database.Campaign {
	p.t.Helper()
	var row database.Campaign
	if err := p.db.Where("id = ?", id).First(&row).Error; err != nil {
		p.t.Fatalf("read campaign: %v", err)
	}
	return row
}

// TestCampaignDataAnonymizeAfterClose checks Anonymize Data can run again on
// a closed campaign whose last run was before the close, and is refused once
// it ran after the close, also when a later data anonymize date is set
func TestCampaignDataAnonymizeAfterClose(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	svc := p.svc.CampaignService

	ranBeforeClose := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour), "data_anonymized_at": ago(2 * time.Hour)})
	ranAfterClose := anonCampaign(p, map[string]any{"closed_at": ago(2 * time.Hour), "data_anonymized_at": ago(time.Hour)})
	ranAfterCloseDue := anonCampaign(p, map[string]any{
		"closed_at": ago(3 * time.Hour), "data_anonymized_at": ago(2 * time.Hour), "data_anonymize_at": ago(time.Hour),
	})
	open := anonCampaign(p, map[string]any{"data_anonymized_at": ago(time.Hour)})

	if err := svc.AnonymizeDataByID(p.ctx, session, &ranBeforeClose); err != nil {
		t.Fatalf("a run before the close must allow another run: %v", err)
	}
	if err := svc.AnonymizeDataByID(p.ctx, session, &ranAfterClose); !errors.Is(err, errs.ErrCampaignDataAlreadyAnonymized) {
		t.Fatalf("a run after the close must be refused, got %v", err)
	}
	if err := svc.AnonymizeDataByID(p.ctx, session, &ranAfterCloseDue); !errors.Is(err, errs.ErrCampaignDataAlreadyAnonymized) {
		t.Fatalf("a run after the close must be refused also with a date set, got %v", err)
	}
	if err := svc.AnonymizeDataByID(p.ctx, session, &open); err != nil {
		t.Fatalf("an open campaign must allow another run: %v", err)
	}
}

// TestGetReadyToDataAnonymizeAfterEarlierRun checks the data task picks a
// campaign whose data anonymize date passed after an earlier run before the
// close, and leaves out a date that already ran, a run after the close, a
// future date and an anonymized campaign
func TestGetReadyToDataAnonymizeAfterEarlierRun(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	ranBeforeDate := anonCampaign(p, map[string]any{
		"closed_at": ago(2 * time.Hour), "data_anonymized_at": ago(3 * time.Hour), "data_anonymize_at": ago(time.Hour),
	})
	ranAfterClose := anonCampaign(p, map[string]any{
		"closed_at": ago(3 * time.Hour), "data_anonymized_at": ago(2 * time.Hour), "data_anonymize_at": ago(time.Hour),
	})
	neverRan := anonCampaign(p, map[string]any{"closed_at": ago(4 * time.Hour), "data_anonymize_at": ago(time.Hour)})
	ranAfterDate := anonCampaign(p, map[string]any{
		"closed_at": ago(4 * time.Hour), "data_anonymize_at": ago(2 * time.Hour), "data_anonymized_at": ago(time.Hour),
	})
	future := anonCampaign(p, map[string]any{
		"closed_at": ago(4 * time.Hour), "data_anonymize_at": utils.RFC3339UTC(time.Now().Add(time.Hour)),
	})
	anonymized := anonCampaign(p, map[string]any{
		"closed_at": ago(4 * time.Hour), "data_anonymize_at": ago(time.Hour), "anonymized_at": ago(30 * time.Minute),
	})

	ready := func() map[uuid.UUID]bool {
		t.Helper()
		res, err := p.svc.CampaignRepository.GetReadyToDataAnonymize(p.ctx, &repository.CampaignOption{})
		if err != nil {
			t.Fatalf("ready to data anonymize: %v", err)
		}
		out := map[uuid.UUID]bool{}
		for _, c := range res.Rows {
			out[c.ID.MustGet()] = true
		}
		return out
	}
	got := ready()
	for id, want := range map[uuid.UUID]bool{
		ranBeforeDate: true, neverRan: true, ranAfterDate: false, ranAfterClose: false, future: false, anonymized: false,
	} {
		if got[id] != want {
			t.Fatalf("campaign %s: ready %v, want %v", id, got[id], want)
		}
	}
	// the task runs both and finds neither on the next run
	if err := p.svc.CampaignService.HandleDataAnonymizeCampaigns(p.ctx, session); err != nil {
		t.Fatalf("data anonymize task: %v", err)
	}
	got = ready()
	if got[ranBeforeDate] || got[neverRan] {
		t.Fatalf("campaigns that ran must not be ready again: %v", got)
	}
}

// TestScheduleAnonymize checks the anonymize date of a closed campaign can be
// set and cleared, and every refusal: not authorized, open, hidden program
// campaign, anonymous, anonymized, past, too far ahead, and data that was
// anonymized after the close
func TestScheduleAnonymize(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	svc := p.svc.CampaignService
	in := func(d time.Duration) *time.Time {
		v := time.Now().Add(d)
		return &v
	}

	// closed by hand before its planned close, the date is not held to the
	// later close date
	closed := anonCampaign(p, map[string]any{
		"closed_at": ago(time.Hour), "close_at": utils.RFC3339UTC(time.Now().Add(60 * 24 * time.Hour)),
	})
	if err := svc.ScheduleAnonymizeByID(p.ctx, unauthorizedSession(t), &closed, in(time.Hour)); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("an unauthorized session must be refused, got %v", err)
	}
	at := in(14 * 24 * time.Hour)
	if err := svc.ScheduleAnonymizeByID(p.ctx, session, &closed, at); err != nil {
		t.Fatalf("schedule anonymize: %v", err)
	}
	row := storedCampaign(p, closed)
	if row.AnonymizeAt == nil || !row.AnonymizeAt.Equal(at.UTC().Truncate(time.Minute)) {
		t.Fatalf("anonymize at = %v, want %v", row.AnonymizeAt, at.UTC().Truncate(time.Minute))
	}
	if row.AnonymizedAt != nil || row.DataAnonymizeAt != nil {
		t.Fatalf("only the anonymize date may be written: %+v", row)
	}
	// a future date is not picked by the anonymize task
	res, err := p.svc.CampaignRepository.GetReadyToAnonymize(p.ctx, &repository.CampaignOption{})
	if err != nil {
		t.Fatalf("ready to anonymize: %v", err)
	}
	for _, c := range res.Rows {
		if c.ID.MustGet() == closed {
			t.Fatal("a future anonymize date must not be ready")
		}
	}
	if err := svc.ScheduleAnonymizeByID(p.ctx, session, &closed, nil); err != nil {
		t.Fatalf("clear anonymize date: %v", err)
	}
	if row := storedCampaign(p, closed); row.AnonymizeAt != nil {
		t.Fatalf("the anonymize date must be cleared: %v", row.AnonymizeAt)
	}
	if err := svc.ScheduleAnonymizeByID(p.ctx, session, &closed, in(-time.Minute)); !isValidationError(err) {
		t.Fatalf("a past date must be refused, got %v", err)
	}
	if err := svc.ScheduleAnonymizeByID(p.ctx, session, &closed, in(6*365*24*time.Hour)); !isValidationError(err) {
		t.Fatalf("a date past 5 years must be refused, got %v", err)
	}

	open := anonCampaign(p, nil)
	if err := svc.ScheduleAnonymizeByID(p.ctx, session, &open, in(time.Hour)); !isValidationError(err) {
		t.Fatalf("an open campaign must be refused, got %v", err)
	}
	if row := storedCampaign(p, open); row.AnonymizeAt != nil {
		t.Fatal("an open campaign must not get a date")
	}

	programID := uuid.New()
	hidden := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour), "program_id": programID})
	if err := svc.ScheduleAnonymizeByID(p.ctx, session, &hidden, in(time.Hour)); err == nil {
		t.Fatal("a hidden program campaign must be refused")
	}
	if row := storedCampaign(p, hidden); row.AnonymizeAt != nil {
		t.Fatal("a hidden program campaign must not get a date")
	}

	anonymous := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour), "is_anonymous": true})
	if err := svc.ScheduleAnonymizeByID(p.ctx, session, &anonymous, in(time.Hour)); !isValidationError(err) {
		t.Fatalf("an anonymous campaign must be refused, got %v", err)
	}

	anonymized := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour), "anonymized_at": ago(time.Minute)})
	for _, onlyData := range []bool{false, true} {
		err := svc.scheduleAnonymize(p.ctx, session, &anonymized, in(time.Hour), onlyData)
		if !isValidationError(err) {
			t.Fatalf("an anonymized campaign must be refused (data %v), got %v", onlyData, err)
		}
	}

	// data: a run after the close leaves nothing to scrub, clearing works
	ranAfterClose := anonCampaign(p, map[string]any{
		"closed_at": ago(2 * time.Hour), "data_anonymized_at": ago(time.Hour), "data_anonymize_at": ago(3 * time.Hour),
	})
	if err := svc.ScheduleDataAnonymizeByID(p.ctx, session, &ranAfterClose, in(time.Hour)); !isValidationError(err) {
		t.Fatalf("data anonymized after the close must be refused, got %v", err)
	}
	if err := svc.ScheduleDataAnonymizeByID(p.ctx, session, &ranAfterClose, nil); err != nil {
		t.Fatalf("clearing the data date must be allowed: %v", err)
	}
	// data: a run before the close can be followed by a scheduled run, which
	// the data task picks once it is due
	ranBeforeClose := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour), "data_anonymized_at": ago(2 * time.Hour)})
	if err := svc.ScheduleDataAnonymizeByID(p.ctx, session, &ranBeforeClose, in(time.Hour)); err != nil {
		t.Fatalf("schedule data anonymize: %v", err)
	}
	if row := storedCampaign(p, ranBeforeClose); row.DataAnonymizeAt == nil || row.AnonymizeAt != nil {
		t.Fatalf("only the data date may be written: %+v", row)
	}
	// an anonymous campaign can still have its data date set
	if err := svc.ScheduleDataAnonymizeByID(p.ctx, session, &anonymous, in(time.Hour)); err != nil {
		t.Fatalf("schedule data anonymize on an anonymous campaign: %v", err)
	}
}

// TestCampaignUpdateRefusesClosed checks a closed campaign is not edited.
// A self managed campaign has no start time, so the start check does not
// stop it.
func TestCampaignUpdateRefusesClosed(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	closed := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour)})
	group := uuid.New()
	incoming := model.Campaign{}
	incoming.RecipientGroupIDs = nullable.NewNullableWithValue([]*uuid.UUID{&group})
	err := p.svc.CampaignService.UpdateByID(p.ctx, session, &closed, &incoming)
	if !isValidationError(err) || !strings.Contains(err.Error(), "a closed campaign can not be edited") {
		t.Fatalf("a closed campaign must be refused as closed, got %v", err)
	}
}

// TestAnonymizeTasksSkipCampaignNoLongerDue checks the anonymize tasks read
// the campaign again and leave it when its date was cleared or moved after
// the task listed it
func TestAnonymizeTasksSkipCampaignNoLongerDue(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	svc := p.svc.CampaignService
	future := utils.RFC3339UTC(time.Now().Add(time.Hour))

	moved := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour), "anonymize_at": future})
	if err := svc.anonymizeByID(p.ctx, session, &moved, true); !errors.Is(err, errAnonymizeNotDue) {
		t.Fatalf("a moved anonymize date must be skipped, got %v", err)
	}
	if row := storedCampaign(p, moved); row.AnonymizedAt != nil {
		t.Fatal("a campaign no longer due must not be anonymized")
	}
	cleared := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour)})
	if err := svc.anonymizeByID(p.ctx, session, &cleared, true); !errors.Is(err, errAnonymizeNotDue) {
		t.Fatalf("a cleared anonymize date must be skipped, got %v", err)
	}
	// still due runs, and an anonymous closed campaign is always due
	due := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour), "anonymize_at": ago(time.Minute)})
	if err := svc.anonymizeByID(p.ctx, session, &due, true); err != nil {
		t.Fatalf("a due campaign must be anonymized: %v", err)
	}
	if row := storedCampaign(p, due); row.AnonymizedAt == nil {
		t.Fatal("a due campaign must be anonymized")
	}
	anonymous := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour), "is_anonymous": true})
	if err := svc.anonymizeByID(p.ctx, session, &anonymous, true); err != nil {
		t.Fatalf("a closed anonymous campaign must be anonymized: %v", err)
	}
	// by hand the date does not matter
	byHand := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour)})
	if err := svc.AnonymizeByID(p.ctx, session, &byHand); err != nil {
		t.Fatalf("anonymize by hand: %v", err)
	}

	dataMoved := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour), "data_anonymize_at": future})
	if err := svc.anonymizeDataByID(p.ctx, session, &dataMoved, true); !errors.Is(err, errAnonymizeNotDue) {
		t.Fatalf("a moved data date must be skipped, got %v", err)
	}
	if row := storedCampaign(p, dataMoved); row.DataAnonymizedAt != nil {
		t.Fatal("data no longer due must not be anonymized")
	}
	dataDue := anonCampaign(p, map[string]any{"closed_at": ago(time.Hour), "data_anonymize_at": ago(time.Minute)})
	if err := svc.anonymizeDataByID(p.ctx, session, &dataDue, true); err != nil {
		t.Fatalf("due data must be anonymized: %v", err)
	}
}
