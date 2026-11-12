package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/utils"
)

func TestParseReportedDate_Formats(t *testing.T) {
	utc := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatalf("bad want %q: %v", s, err)
		}
		return v
	}
	cases := map[string]time.Time{
		// iso 8601, with and without fractional seconds and zone
		"2024-03-05T10:20:30.1234567Z":   utc("2024-03-05T10:20:30.1234567Z"),
		"2024-03-05T10:20:30.5+05:00":    utc("2024-03-05T05:20:30.5Z"),
		"2024-03-05T10:20:30.999-07:00":  utc("2024-03-05T17:20:30.999Z"),
		"2024-03-05T10:20:30Z":           utc("2024-03-05T10:20:30Z"),
		"2024-03-05T10:20:30+02:00":      utc("2024-03-05T08:20:30Z"),
		"2024-03-05T10:20:30+01:00":      utc("2024-03-05T09:20:30Z"),
		"2024-03-05T10:20:30-07:00":      utc("2024-03-05T17:20:30Z"),
		"2024-03-05T10:20:30":            utc("2024-03-05T10:20:30Z"),
		"2024-03-05T10:20:30.123":        utc("2024-03-05T10:20:30.123Z"),
		"2024-03-05 10:20:30":            utc("2024-03-05T10:20:30Z"),
		"2024-03-05 10:20":               utc("2024-03-05T10:20:00Z"),
		"2024-03-05":                     utc("2024-03-05T00:00:00Z"),
		// month first
		"03/05/2024 10:20:30": utc("2024-03-05T10:20:30Z"),
		"03/05/2024":          utc("2024-03-05T00:00:00Z"),
		"3/5/2024 9:05":       utc("2024-03-05T09:05:00Z"),
		"3/5/2024":            utc("2024-03-05T00:00:00Z"),
		// day first
		"05-03-2024 10:20:30": utc("2024-03-05T10:20:30Z"),
		"05-03-2024":          utc("2024-03-05T00:00:00Z"),
		"5-3-2024 10:20":      utc("2024-03-05T10:20:00Z"),
		"2024/03/05 10:20:30": utc("2024-03-05T10:20:30Z"),
		"2024/03/05 10:20":    utc("2024-03-05T10:20:00Z"),
		"2024/03/05":          utc("2024-03-05T00:00:00Z"),
	}
	for in, want := range cases {
		got, err := parseReportedDate(in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", in, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("%q: got %s want %s", in, got.UTC().Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
		}
	}
	for _, in := range []string{
		"", "yesterday", "13/02/2024", "2024-03-05T10:20:30+0200", "05.03.2024", "2024-13-01",
	} {
		if _, err := parseReportedDate(in); err == nil {
			t.Errorf("%q: expected an error", in)
		}
	}
}

func TestProcessReportedCSV_ReportsFailedRows(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(&f.c1, false)
	_, alice := f.recipient(camp, "alice@c1.example", "")
	_, bob := f.recipient(camp, "Bob@c1.example", "")
	// a recipient of the same company outside this campaign
	other := f.campaign(&f.c1, false)
	f.recipient(other, "carol@c1.example", "")
	session := sessionWithRole(data.RoleSuperAdministrator)

	header := []string{"email", "date"}
	rows := []ReportedCSVRow{
		{Line: 2, Fields: []string{"alice@c1.example", "2024-03-05T10:20:30Z"}},
		{Line: 3, Fields: []string{"alice@c1.example", "not a date"}},
		{Line: 5, Fields: []string{"bob@c1.example", "3/5/2024 9:05"}},
		{Line: 6, Fields: []string{"carol@c1.example", "2024-03-05"}},
		{Line: 7, Fields: []string{"", "2024-03-05"}},
		{Line: 8, Fields: []string{"alice@c1.example"}},
		{Line: 9, Fields: []string{"no-at-sign", "2024-03-05"}},
		{Line: 10, Fields: []string{"alice@c1.example", strings.Repeat("x", 200)}},
	}
	res, err := f.svc.CampaignService.ProcessReportedCSV(f.ctx, session, &camp, header, rows, 0, 1)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Processed != 2 || res.Failed != 6 || res.AlreadyReported != 0 || res.Skipped != 6 {
		t.Fatalf("counts: %+v", res)
	}
	want := []ReportedCSVRowError{
		{Line: 3, Reason: `Invalid date "not a date"`},
		{Line: 6, Reason: `"carol@c1.example" is not a recipient of this campaign`},
		{Line: 7, Reason: "Email is empty"},
		{Line: 8, Reason: "Missing email or date column"},
		{Line: 9, Reason: `Invalid email "no-at-sign"`},
	}
	if len(res.Errors) != len(want) {
		t.Fatalf("errors: %+v", res.Errors)
	}
	for i := range want {
		if res.Errors[i] != want[i] {
			t.Errorf("error %d: got %+v want %+v", i, res.Errors[i], want[i])
		}
	}

	// the event keeps the date from the file, case is ignored for a single match
	events := f.reportedEvents(camp, alice)
	if len(events) != 1 || !events[0].CreatedAt.Equal(time.Date(2024, 3, 5, 10, 20, 30, 0, time.UTC)) {
		t.Fatalf("alice events: %+v", events)
	}
	if got := f.reportedEvents(camp, bob); len(got) != 1 {
		t.Fatalf("bob events: %d", len(got))
	}

	// importing the same rows again records nothing new and lists no duplicates
	res, err = f.svc.CampaignService.ProcessReportedCSV(f.ctx, session, &camp, header, rows[:3], 0, 1)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if res.Processed != 0 || res.AlreadyReported != 2 || res.Failed != 1 || res.Skipped != 3 || len(res.Errors) != 1 {
		t.Fatalf("second import counts: %+v", res)
	}
	if got := f.reportedEvents(camp, alice); len(got) != 1 {
		t.Fatalf("alice reported twice: %d", len(got))
	}
	if got := f.reportedEvents(camp, bob); len(got) != 1 {
		t.Fatalf("bob reported twice: %d", len(got))
	}
}

func TestProcessReportedCSV_LongValueIsCut(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(&f.c1, false)
	f.recipient(camp, "alice@c1.example", "")
	rows := []ReportedCSVRow{{Line: 2, Fields: []string{"alice@c1.example", strings.Repeat("x", 200)}}}
	res, err := f.svc.CampaignService.ProcessReportedCSV(f.ctx, sessionWithRole(data.RoleSuperAdministrator), &camp, []string{"e", "d"}, rows, 0, 1)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.Errors) != 1 || res.Errors[0].Reason != `Invalid date "`+strings.Repeat("x", reportedCSVMaxValueRunes)+`..."` {
		t.Fatalf("errors: %+v", res.Errors)
	}
}

func TestProcessReportedCSV_ListsOnlyFirstFiveErrors(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(&f.c1, false)
	rows := []ReportedCSVRow{}
	for i := 0; i < 12; i++ {
		rows = append(rows, ReportedCSVRow{Line: i + 2, Fields: []string{"x@c1.example", "bad"}})
	}
	res, err := f.svc.CampaignService.ProcessReportedCSV(f.ctx, sessionWithRole(data.RoleSuperAdministrator), &camp, []string{"e", "d"}, rows, 0, 1)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Failed != 12 || len(res.Errors) != reportedCSVMaxErrors || res.Errors[4].Line != 6 {
		t.Fatalf("result: %+v", res)
	}
}

func TestProcessReportedCSV_AmbiguousCaseIsNotGuessed(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(&f.c1, false)
	f.recipient(camp, "Dave@c1.example", "")
	f.recipient(camp, "DAVE@c1.example", "")
	rows := []ReportedCSVRow{{Line: 2, Fields: []string{"dave@c1.example", "2024-03-05"}}}
	res, err := f.svc.CampaignService.ProcessReportedCSV(f.ctx, sessionWithRole(data.RoleSuperAdministrator), &camp, []string{"e", "d"}, rows, 0, 1)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Processed != 0 || res.Failed != 1 ||
		res.Errors[0].Reason != `"dave@c1.example" matches more than one recipient, use the exact case` {
		t.Fatalf("result: %+v", res)
	}
	// an exact match still wins over the other case
	rows[0].Fields[0] = "DAVE@c1.example"
	res, err = f.svc.CampaignService.ProcessReportedCSV(f.ctx, sessionWithRole(data.RoleSuperAdministrator), &camp, []string{"e", "d"}, rows, 0, 1)
	if err != nil || res.Processed != 1 {
		t.Fatalf("exact match: %+v %v", res, err)
	}
}

func TestProcessReportedCSV_AnonymousHidesWhoReported(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(&f.c1, false)
	crid, _ := f.recipient(camp, "alice@c1.example", "")
	aid := uuid.New()
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", camp).Update("is_anonymous", true).Error; err != nil {
		t.Fatalf("anon: %v", err)
	}
	if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", crid).Update("anonymized_id", aid).Error; err != nil {
		t.Fatalf("anon id: %v", err)
	}
	session := sessionWithRole(data.RoleSuperAdministrator)
	rows := []ReportedCSVRow{{Line: 2, Fields: []string{"alice@c1.example", "2024-03-05"}}}
	for i := 0; i < 2; i++ {
		res, err := f.svc.CampaignService.ProcessReportedCSV(f.ctx, session, &camp, []string{"e", "d"}, rows, 0, 1)
		if err != nil {
			t.Fatalf("import %d: %v", i, err)
		}
		// the second import looks the same as the first
		if res.Processed != 1 || res.AlreadyReported != 0 || res.Skipped != 0 {
			t.Fatalf("import %d: %+v", i, res)
		}
	}
	var count int64
	f.db.Model(&database.CampaignEvent{}).Where("campaign_id = ? AND anonymized_id = ?", camp, aid).Count(&count)
	if count != 1 {
		t.Fatalf("anonymous events: %d", count)
	}
	var withRecipient int64
	f.db.Model(&database.CampaignEvent{}).Where("campaign_id = ? AND recipient_id IS NOT NULL", camp).Count(&withRecipient)
	if withRecipient != 0 {
		t.Fatalf("anonymous event carries a recipient id")
	}
}

func TestProcessReportedCSV_RejectsAnonymizedAndUnauthorized(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(&f.c1, false)
	rows := []ReportedCSVRow{{Line: 2, Fields: []string{"alice@c1.example", "2024-03-05"}}}
	if _, err := f.svc.CampaignService.ProcessReportedCSV(f.ctx, sessionWithRole(data.RoleCompanyUser), &camp, []string{"e", "d"}, rows, 0, 1); err == nil {
		t.Fatalf("company user was allowed")
	}
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", camp).Update("anonymized_at", utils.NowRFC3339UTC()).Error; err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	_, err := f.svc.CampaignService.ProcessReportedCSV(f.ctx, sessionWithRole(data.RoleSuperAdministrator), &camp, []string{"e", "d"}, rows, 0, 1)
	if !isValidationError(err) {
		t.Fatalf("anonymized campaign: %v", err)
	}
}
