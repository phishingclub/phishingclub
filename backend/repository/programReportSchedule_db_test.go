package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

func TestProgramReportScheduleStore(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	repo := &Program{DB: db}
	id := createProgram(t, db, "reports", nil)
	createProgram(t, db, "no reports", nil)

	// the settings and the baseline are written through the update map
	baseline := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	update := &model.Program{
		Name:           nullable.NewNullableWithValue(*vo.NewString64Must("reports")),
		ReportEnabled:  nullable.NewNullableWithValue(true),
		ReportInterval: nullable.NewNullableWithValue(model.ProgramReportQuarterly),
		ReportPeriod:   nullable.NewNullableWithValue(model.ProgramReportPeriodStart),
		ReportBaseline: &baseline,
	}
	if err := repo.UpdateByID(ctx, id, update, nil, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	p, err := repo.GetByID(ctx, id, &ProgramOption{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !p.ReportEnabledOn() || p.ReportIntervalOrDefault() != model.ProgramReportQuarterly ||
		p.ReportPeriodOrDefault() != model.ProgramReportPeriodStart ||
		p.ReportLastPeriodEnd == nil || !p.ReportLastPeriodEnd.Equal(baseline) {
		t.Fatalf("stored settings: %+v", p)
	}

	// only started programs with reports on are listed
	if ids, err := repo.GetReportEnabledIDs(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("a program that has not started is not listed: %v %v", ids, err)
	}
	if err := db.Model(&database.Program{}).Where("id = ?", id).Update("started_at", time.Now().UTC()).Error; err != nil {
		t.Fatalf("start: %v", err)
	}
	ids, err := repo.GetReportEnabledIDs(ctx)
	if err != nil || len(ids) != 1 || *ids[0] != *id {
		t.Fatalf("enabled ids: %v %v", ids, err)
	}

	// a claim moves the end once, a second claim from the same value loses
	end := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	ok, err := repo.ClaimReportPeriod(ctx, id, p.ReportLastPeriodEnd, &end)
	if err != nil || !ok {
		t.Fatalf("first claim: %v %v", ok, err)
	}
	ok, err = repo.ClaimReportPeriod(ctx, id, p.ReportLastPeriodEnd, &end)
	if err != nil || ok {
		t.Fatalf("second claim must lose: %v %v", ok, err)
	}
	// the claim can be given back to the value read before, also to none
	p2, _ := repo.GetByID(ctx, id, &ProgramOption{})
	ok, err = repo.ClaimReportPeriod(ctx, id, p2.ReportLastPeriodEnd, nil)
	if err != nil || !ok {
		t.Fatalf("give back to none: %v %v", ok, err)
	}
	ok, err = repo.ClaimReportPeriod(ctx, id, nil, &end)
	if err != nil || !ok {
		t.Fatalf("claim from none: %v %v", ok, err)
	}
	p3, _ := repo.GetByID(ctx, id, &ProgramOption{})
	if p3.ReportLastPeriodEnd == nil || !p3.ReportLastPeriodEnd.Equal(end) {
		t.Fatalf("stored end: %v", p3.ReportLastPeriodEnd)
	}

	// a closed program stays listed until the period it closed in is sent,
	// the close is stored in another zone than the end to check the times
	// are compared as times
	cph := time.FixedZone("plus2", 2*3600)
	closeAt := func(at time.Time) {
		t.Helper()
		if err := db.Model(&database.Program{}).Where("id = ?", id).Update("closed_at", at).Error; err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	closeAt(end.Add(time.Hour).In(cph))
	if ids, err := repo.GetReportEnabledIDs(ctx); err != nil || len(ids) != 1 {
		t.Fatalf("a closed program with its last report due is listed: %v %v", ids, err)
	}
	closeAt(end.Add(-time.Hour).In(cph))
	if ids, err := repo.GetReportEnabledIDs(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("a closed program with its last report sent is not listed: %v %v", ids, err)
	}
	closeAt(end.In(cph))
	if ids, err := repo.GetReportEnabledIDs(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("a program closed at the end of the last period sent is not listed: %v %v", ids, err)
	}
}

// TestProgramReportSkipsDeletingAndClosing checks a program that is being
// deleted or closed is not listed for reports and its period cannot be
// claimed, while a give back still works during a close, and a closed program
// is listed again for its last report
func TestProgramReportSkipsDeletingAndClosing(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	repo := &Program{DB: db}
	id := createProgram(t, db, "reports", nil)
	now := time.Now().UTC()
	if err := db.Model(&database.Program{}).Where("id = ?", id).
		Updates(map[string]any{"report_enabled": true, "started_at": now}).Error; err != nil {
		t.Fatalf("start: %v", err)
	}
	listed := func() bool {
		t.Helper()
		ids, err := repo.GetReportEnabledIDs(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return len(ids) == 1 && *ids[0] == *id
	}
	if !listed() {
		t.Fatal("a running program is listed")
	}
	end := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, col := range []string{"deleting_at", "closing_at"} {
		if err := db.Model(&database.Program{}).Where("id = ?", id).Update(col, now).Error; err != nil {
			t.Fatalf("mark %s: %v", col, err)
		}
		if listed() {
			t.Fatalf("a program with %s is not listed", col)
		}
		ok, err := repo.ClaimReportPeriod(ctx, id, nil, &end)
		if err != nil || ok {
			t.Fatalf("a claim with %s must be refused: %v %v", col, ok, err)
		}
		if err := db.Model(&database.Program{}).Where("id = ?", id).Update(col, nil).Error; err != nil {
			t.Fatalf("clear %s: %v", col, err)
		}
	}
	// a period claimed before a close started can be given back during it
	ok, err := repo.ClaimReportPeriod(ctx, id, nil, &end)
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	db.Model(&database.Program{}).Where("id = ?", id).Update("closing_at", now)
	ok, err = repo.GiveBackReportPeriod(ctx, id, &end, nil)
	if err != nil || !ok {
		t.Fatalf("give back during a close: %v %v", ok, err)
	}
	// a finished close lists the program again so its last period is sent
	db.Model(&database.Program{}).Where("id = ?", id).
		Updates(map[string]any{"closing_at": nil, "closed_at": now})
	if !listed() {
		t.Fatal("a closed program is listed for its last report")
	}
}

// TestProgramReportStaleClaimGivenBack checks a claimed period that was never
// marked as sent is given back once it is older than the cut off, a fresh
// claim and a period marked as sent are kept, and a claim from no end goes
// back to no end
func TestProgramReportStaleClaimGivenBack(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	repo := &Program{DB: db}
	stale := createProgram(t, db, "stale", nil)
	fresh := createProgram(t, db, "fresh", nil)
	sent := createProgram(t, db, "sent", nil)
	none := createProgram(t, db, "from none", nil)
	prev := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	for _, id := range []*uuid.UUID{stale, fresh, sent} {
		if err := db.Model(&database.Program{}).Where("id = ?", id).Update("report_last_period_end", prev).Error; err != nil {
			t.Fatalf("baseline: %v", err)
		}
		if ok, err := repo.ClaimReportPeriod(ctx, id, &prev, &end); err != nil || !ok {
			t.Fatalf("claim: %v %v", ok, err)
		}
	}
	if ok, err := repo.ClaimReportPeriod(ctx, none, nil, &end); err != nil || !ok {
		t.Fatalf("claim from none: %v %v", ok, err)
	}
	if err := repo.FinishReportClaim(ctx, sent, &end); err != nil {
		t.Fatalf("finish: %v", err)
	}
	old := time.Now().UTC().Add(-2 * time.Hour)
	for _, id := range []*uuid.UUID{stale, sent, none} {
		if err := db.Model(&database.Program{}).Where("id = ?", id).
			Where("report_claimed_at IS NOT NULL").Update("report_claimed_at", old).Error; err != nil {
			t.Fatalf("age claim: %v", err)
		}
	}
	n, err := repo.GiveBackStaleReportClaims(ctx, time.Now().Add(-time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("expected the stale claim and the claim from none given back: %d %v", n, err)
	}
	endOf := func(id *uuid.UUID) *time.Time {
		var row database.Program
		if err := db.Where("id = ?", id).First(&row).Error; err != nil {
			t.Fatalf("read: %v", err)
		}
		if row.ReportClaimedAt != nil && *id != *fresh {
			t.Fatalf("a given back or sent period keeps no claim: %v", row.ReportClaimedAt)
		}
		return row.ReportLastPeriodEnd
	}
	if got := endOf(stale); got == nil || !got.Equal(prev) {
		t.Fatalf("a stale claim must go back to the end before it, got %v", got)
	}
	if got := endOf(none); got != nil {
		t.Fatalf("a stale claim from no end must go back to no end, got %v", got)
	}
	for _, id := range []*uuid.UUID{fresh, sent} {
		if got := endOf(id); got == nil || !got.Equal(end) {
			t.Fatalf("a fresh or sent claim is kept, got %v", got)
		}
	}
	// the given back period can be claimed again
	if ok, err := repo.ClaimReportPeriod(ctx, stale, &prev, &end); err != nil || !ok {
		t.Fatalf("claim again: %v %v", ok, err)
	}
}
