package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
)

// timelineEventID returns the id of an event type, made up when the cache has
// none yet
func timelineEventID(name string) uuid.UUID {
	if cache.EventIDByName[name] == nil {
		id := uuid.New()
		cache.EventIDByName[name] = &id
	}
	return *cache.EventIDByName[name]
}

// timelineProgram creates a started program in a company and zone with one
// open hidden campaign and returns both ids
func (p *programTest) timelineProgram(companyID *uuid.UUID, zone string, anonymous bool) (uuid.UUID, uuid.UUID) {
	p.t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	started := now.AddDate(0, 0, -60)
	p.create(&database.Program{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "timeline " + id.String(),
		CompanyID: companyID, LoopMode: "complete", OrderMode: "sequential",
		StartAt: &started, StartedAt: &started, TimeZone: zone, IsAnonymous: anonymous,
	})
	step := p.step(id, 0, true)
	return id, p.stepCampaign(id, step, 0)
}

// timelineCampaign adds another step with its open hidden campaign, as a
// person has one row per campaign
func (p *programTest) timelineCampaign(programID uuid.UUID, position int) uuid.UUID {
	p.t.Helper()
	return p.stepCampaign(programID, p.step(programID, position, true), 0)
}

// timelineSent adds a send sent at a time
func (p *programTest) timelineSent(campaignID, recipientID uuid.UUID, at time.Time) {
	p.t.Helper()
	id := p.send(campaignID, recipientID, false)
	if err := p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Updates(map[string]any{
		"send_at": utils.RFC3339UTC(at), "sent_at": utils.RFC3339UTC(at),
	}).Error; err != nil {
		p.t.Fatalf("set sent_at: %v", err)
	}
}

// timelineEvent adds an event of a recipient at a time, stored as RFC3339 text
// the same as the event path
func (p *programTest) timelineEvent(campaignID, recipientID uuid.UUID, name string, at time.Time) {
	p.t.Helper()
	err := p.db.Table(database.CAMPAIGN_EVENT_TABLE).Create(map[string]any{
		"id": uuid.NewString(), "created_at": utils.RFC3339UTC(at), "updated_at": utils.RFC3339UTC(at),
		"data": "", "metadata": "", "campaign_id": campaignID.String(),
		"event_id": timelineEventID(name).String(), "recipient_id": recipientID.String(),
	}).Error
	if err != nil {
		p.t.Fatalf("create event: %v", err)
	}
}

// at returns a wall clock time on a day relative to today in loc
func timelineAt(loc *time.Location, days, hour, minute int) time.Time {
	now := time.Now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day()+days, hour, minute, 0, 0, loc)
}

func timelineDay(loc *time.Location, days int) string {
	return timelineAt(loc, days, 12, 0).Format("2006-01-02")
}

// TestProgramTimelineAccess checks the session, the company in view and that
// a program of another company is not found
func TestProgramTimelineAccess(t *testing.T) {
	p := newProgramServiceTest(t)
	companyA, companyB := uuid.New(), uuid.New()
	ownID, _ := p.timelineProgram(&companyA, "UTC", false)
	sharedID, _ := p.timelineProgram(nil, "UTC", false)

	if _, err := p.svc.GetTimeline(p.ctx, unauthorizedSession(t), &ownID, nil, "", ""); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("an unauthorized session must be refused, got %v", err)
	}
	session := authorizedSession(t)
	if _, err := p.svc.GetTimeline(p.ctx, session, &ownID, &companyB, "", ""); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("a program of another company must not be found, got %v", err)
	}
	if _, err := p.svc.GetTimeline(p.ctx, session, &ownID, &companyA, "", ""); err != nil {
		t.Fatalf("own company: %v", err)
	}
	if _, err := p.svc.GetTimeline(p.ctx, session, &ownID, nil, "", ""); err != nil {
		t.Fatalf("global view: %v", err)
	}
	// a global program shows in every company view
	if _, err := p.svc.GetTimeline(p.ctx, session, &sharedID, &companyB, "", ""); err != nil {
		t.Fatalf("shared program: %v", err)
	}
	missing := uuid.New()
	if _, err := p.svc.GetTimeline(p.ctx, session, &missing, nil, "", ""); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("an unknown program must not be found, got %v", err)
	}
}

// TestProgramTimelineAnonymous checks an anonymous program gives no timeline:
// only the flag, with no bucket, no planned send and no count of any kind
func TestProgramTimelineAnonymous(t *testing.T) {
	p := newProgramServiceTest(t)
	loc := time.UTC
	programID, campaignID := p.timelineProgram(nil, "UTC", true)
	next := p.timelineCampaign(programID, 1)
	for i := 0; i < 8; i++ {
		person := p.recipient(uuid.NewString() + "@example.com")
		sentAt := timelineAt(loc, -5, 10, i)
		p.timelineSent(campaignID, person, sentAt)
		p.timelineEvent(campaignID, person, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, sentAt.Add(time.Minute))
		p.timelineEvent(campaignID, person, data.EVENT_CAMPAIGN_RECIPIENT_REPORTED, sentAt.Add(time.Minute))
		p.sendAt(next, person, timelineAt(loc, 3, 10, i), false)
	}
	// a person whose next send is due in four days
	due := timelineAt(loc, 4, 9, 0)
	e := p.enroll(programID, p.recipient("due@example.com"), 0)
	p.setEnrollment(e.ID, map[string]any{"next_due_at": due})
	timeline, err := p.svc.GetTimeline(p.ctx, authorizedSession(t), &programID, nil, timelineDay(loc, -7), timelineDay(loc, 7))
	if err != nil {
		t.Fatal(err)
	}
	if !timeline.Anonymous {
		t.Fatal("the timeline must say the program is anonymous")
	}
	if len(timeline.Buckets) != 0 || len(timeline.SkipDays) != 0 {
		t.Fatalf("an anonymous program must give no buckets: %+v", timeline)
	}
	if timeline.StartAt != nil || timeline.EndAt != nil || timeline.PausedAt != nil || timeline.ClosedAt != nil {
		t.Fatalf("an anonymous program must give only the flag: %+v", timeline)
	}
	body, err := json.Marshal(timeline)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"planned"`, `"sent"`, `"read"`, `"clicked"`, `"submitted"`, `"reported"`} {
		if strings.Contains(string(body), key) {
			t.Fatalf("the response of an anonymous program holds %s: %s", key, body)
		}
	}
}

// TestProgramTimelineDays checks sends, outcomes and planned sends are counted
// on the day of the program zone, a send counts once per kind and day, and
// skip dates and the pause are marked
func TestProgramTimelineDays(t *testing.T) {
	p := newProgramServiceTest(t)
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no zone data")
	}
	programID, campaignID := p.timelineProgram(nil, "America/New_York", false)
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	// 23:30 in New York is the next day in UTC, it counts on the New York day
	late := timelineAt(loc, -3, 23, 30)
	p.timelineSent(campaignID, alice, late)
	p.timelineEvent(campaignID, alice, data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ, late.Add(time.Minute))
	// two pages of one send count as one click
	p.timelineEvent(campaignID, alice, data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED, late.Add(2*time.Minute))
	p.timelineEvent(campaignID, alice, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, late.Add(3*time.Minute))
	// the submit lands after midnight in New York, on the next day
	p.timelineEvent(campaignID, alice, data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA, timelineAt(loc, -2, 0, 15))
	p.timelineSent(campaignID, bob, timelineAt(loc, -2, 9, 0))
	p.timelineEvent(campaignID, bob, data.EVENT_CAMPAIGN_RECIPIENT_REPORTED, timelineAt(loc, -2, 9, 5))
	// planned sends, one late in the evening
	next := p.timelineCampaign(programID, 1)
	p.sendAt(next, bob, timelineAt(loc, 2, 23, 45), false)
	p.sendAt(next, alice, timelineAt(loc, 2, 8, 0), false)
	// a cancelled send is not planned
	cancelled := p.sendAt(p.timelineCampaign(programID, 2), alice, timelineAt(loc, 3, 8, 0), false)
	if err := p.db.Model(&database.CampaignRecipient{}).Where("id = ?", cancelled).
		Update("cancelled_at", utils.RFC3339UTC(time.Now())).Error; err != nil {
		t.Fatal(err)
	}
	// next sends of active people, one late in the evening in New York, one
	// of a person missing from the groups and one of a person who left
	dueLate := p.enroll(programID, p.recipient("carol@example.com"), 0)
	p.setEnrollment(dueLate.ID, map[string]any{"next_due_at": timelineAt(loc, 1, 23, 50)})
	missing := p.enroll(programID, p.recipient("dan@example.com"), 0)
	p.setEnrollment(missing.ID, map[string]any{"next_due_at": timelineAt(loc, 1, 9, 0), "missing_since": time.Now()})
	left := p.enroll(programID, p.recipient("erin@example.com"), 0)
	p.setEnrollment(left.ID, map[string]any{"next_due_at": timelineAt(loc, 1, 9, 0), "status": "exited"})
	skip := timelineDay(loc, 4)
	skipEnd := timelineDay(loc, 5)
	p.setProgram(programID, map[string]any{
		"blackout_dates": `[{"from":"` + skip + `","to":"` + skipEnd + `"}]`,
		"paused_at":      utils.RFC3339UTC(time.Now().Add(-time.Hour)),
	})

	timeline, err := p.svc.GetTimeline(p.ctx, authorizedSession(t), &programID, nil, timelineDay(loc, -7), timelineDay(loc, 7))
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Bucket != ProgramTimelineBucketDay || len(timeline.Buckets) != 15 {
		t.Fatalf("want 15 days, got %s %d", timeline.Bucket, len(timeline.Buckets))
	}
	byDay := map[string]*ProgramTimelineBucket{}
	for _, b := range timeline.Buckets {
		byDay[b.Day] = b
	}
	d3, d2, a2 := byDay[timelineDay(loc, -3)], byDay[timelineDay(loc, -2)], byDay[timelineDay(loc, 2)]
	if *d3.Sent != 1 || *d3.Read != 1 || *d3.Clicked != 1 || *d3.Submitted != 0 {
		t.Fatalf("day -3: %+v sent %d read %d clicked %d submitted %d", d3, *d3.Sent, *d3.Read, *d3.Clicked, *d3.Submitted)
	}
	if *d2.Sent != 1 || *d2.Submitted != 1 || *d2.Reported != 1 || *d2.Clicked != 0 {
		t.Fatalf("day -2: sent %d submitted %d reported %d clicked %d", *d2.Sent, *d2.Submitted, *d2.Reported, *d2.Clicked)
	}
	if a2.Planned != 2 {
		t.Fatalf("day +2 must plan 2 sends, got %d", a2.Planned)
	}
	if byDay[timelineDay(loc, 1)].Planned != 1 {
		t.Fatalf("day +1 must plan the one active person due, got %d", byDay[timelineDay(loc, 1)].Planned)
	}
	if byDay[timelineDay(loc, 3)].Planned != 0 {
		t.Fatal("a cancelled send must not be planned")
	}
	if len(timeline.SkipDays) != 1 || timeline.SkipDays[0].From != skip || timeline.SkipDays[0].To != skipEnd {
		t.Fatalf("skip days: %+v", timeline.SkipDays)
	}
	if timeline.PausedAt == nil {
		t.Fatal("the pause must be marked")
	}
	if timeline.TimeZone != "America/New_York" {
		t.Fatalf("zone: %s", timeline.TimeZone)
	}
}

// TestProgramTimelineRange checks the range is read strictly and bounded
func TestProgramTimelineRange(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, loc)
	r, err := parseProgramTimelineRange("", "", now, loc)
	if err != nil || r.fromKey != "2026-10-16" || r.toKey != "2026-12-15" || len(r.days) != 61 {
		t.Fatalf("default range: %+v %v", r, err)
	}
	if _, err := parseProgramTimelineRange("2026-08-16", "2027-02-13", now, loc); err != nil {
		t.Fatalf("91 days each way must be allowed: %v", err)
	}
	bad := [][2]string{
		{"2026-08-15", "2026-11-15"}, // too far back
		{"2026-11-15", "2027-02-15"}, // too far ahead
		{"2026-11-20", "2026-11-10"}, // backwards
		{"2026-11-1", "2026-11-20"},  // not a date
		{"2026-11-10", ""},           // one end only
		{"", "2026-11-10"},           // one end only
		{"2026-11-10T00:00", "2026-11-20"},
		{"2026-02-30", "2026-11-20"},
	}
	for _, b := range bad {
		if _, err := parseProgramTimelineRange(b[0], b[1], now, loc); err == nil {
			t.Fatalf("%v must be refused", b)
		} else if !errors.As(err, new(errs.ValidationError)) {
			t.Fatalf("%v must be a validation error, got %T", b, err)
		}
	}
}

// TestProgramTimelineWeeks checks a long range is counted per week from
// Monday, the first and last week holding only the days of the range
func TestProgramTimelineWeeks(t *testing.T) {
	p := newProgramServiceTest(t)
	programID, campaignID := p.timelineProgram(nil, "UTC", false)
	loc := time.UTC
	p.timelineSent(campaignID, p.recipient("a@example.com"), timelineAt(loc, -1, 8, 0))
	p.timelineSent(campaignID, p.recipient("b@example.com"), timelineAt(loc, -2, 8, 0))
	timeline, err := p.svc.GetTimeline(p.ctx, authorizedSession(t), &programID, nil, timelineDay(loc, -90), timelineDay(loc, 90))
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Bucket != ProgramTimelineBucketWeek {
		t.Fatalf("want weeks, got %s", timeline.Bucket)
	}
	days, sent := 0, 0
	for i, b := range timeline.Buckets {
		days += b.Days
		sent += *b.Sent
		start, _ := time.Parse("2006-01-02", b.Day)
		if i > 0 && start.Weekday() != time.Monday {
			t.Fatalf("week %s does not start on Monday", b.Day)
		}
		if b.Days > 7 || b.Days < 1 {
			t.Fatalf("week %s holds %d days", b.Day, b.Days)
		}
	}
	if days != 181 || sent != 2 {
		t.Fatalf("weeks hold %d days and %d sends", days, sent)
	}
}

// TestProgramTimelineSpans checks a range over a daylight saving change is
// split where the offset changes
func TestProgramTimelineSpans(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no zone data")
	}
	start := time.Date(2026, 3, 27, 0, 0, 0, 0, loc)
	end := time.Date(2026, 3, 31, 0, 0, 0, 0, loc)
	spans := programTimelineSpans(start, end, loc)
	if len(spans) != 2 {
		t.Fatalf("want 2 spans, got %+v", spans)
	}
	change := time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC)
	if spans[0].Offset != 3600 || spans[1].Offset != 7200 || !spans[0].To.Equal(change) || !spans[1].From.Equal(change) {
		t.Fatalf("spans: %+v", spans)
	}
	if !spans[0].From.Equal(start) || !spans[1].To.Equal(end) {
		t.Fatalf("spans do not cover the range: %+v", spans)
	}
	if got := programTimelineSpans(end, start, loc); len(got) != 0 {
		t.Fatalf("an empty range has no spans: %+v", got)
	}
}

// TestProgramTimelineWeekPeople checks a week counts distinct people per
// kind: a person active on two days or on two sends of a week counts once,
// and the week starts on Monday in the program zone
func TestProgramTimelineWeekPeople(t *testing.T) {
	p := newProgramServiceTest(t)
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no zone data")
	}
	programID, campaignID := p.timelineProgram(nil, "Europe/Copenhagen", false)
	second := p.timelineCampaign(programID, 1)
	// the Monday two weeks back, as days from today
	today := timelineAt(loc, 0, 12, 0)
	monday := -14 - (int(today.Weekday())+6)%7
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	carol := p.recipient("carol@example.com")
	p.timelineSent(campaignID, alice, timelineAt(loc, monday, 9, 0))
	p.timelineSent(second, alice, timelineAt(loc, monday+2, 9, 0))
	// alice clicks on Monday and on Wednesday, on two sends, and reads twice
	p.timelineEvent(campaignID, alice, data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ, timelineAt(loc, monday, 9, 30))
	p.timelineEvent(campaignID, alice, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, timelineAt(loc, monday, 9, 31))
	p.timelineEvent(second, alice, data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ, timelineAt(loc, monday+2, 9, 30))
	p.timelineEvent(second, alice, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, timelineAt(loc, monday+2, 9, 31))
	// 00:30 on Monday in Copenhagen is Sunday in UTC, it counts in this week
	p.timelineEvent(campaignID, bob, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, timelineAt(loc, monday, 0, 30))
	// 23:30 on the Sunday before is the week before
	p.timelineEvent(campaignID, carol, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, timelineAt(loc, monday-1, 23, 30))
	// reported on Sunday at the end of the week
	p.timelineEvent(campaignID, bob, data.EVENT_CAMPAIGN_RECIPIENT_REPORTED, timelineAt(loc, monday+6, 22, 0))

	timeline, err := p.svc.GetTimeline(p.ctx, authorizedSession(t), &programID, nil, timelineDay(loc, -90), timelineDay(loc, 90))
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Bucket != ProgramTimelineBucketWeek {
		t.Fatalf("want weeks, got %s", timeline.Bucket)
	}
	byDay := map[string]*ProgramTimelineBucket{}
	for _, b := range timeline.Buckets {
		byDay[b.Day] = b
	}
	week := byDay[timelineDay(loc, monday)]
	before := byDay[timelineDay(loc, monday-7)]
	if week == nil || before == nil {
		t.Fatalf("no week starts on %s: %+v", timelineDay(loc, monday), timeline.Buckets)
	}
	if *week.Sent != 2 {
		t.Fatalf("the week must count 2 sends, got %d", *week.Sent)
	}
	if *week.Read != 1 || *week.Clicked != 2 || *week.Reported != 1 || *week.Submitted != 0 {
		t.Fatalf("the week must count people once: read %d clicked %d reported %d submitted %d",
			*week.Read, *week.Clicked, *week.Reported, *week.Submitted)
	}
	if *before.Clicked != 1 {
		t.Fatalf("the week before must count 1 click, got %d", *before.Clicked)
	}
	clicked := 0
	for _, b := range timeline.Buckets {
		clicked += *b.Clicked
	}
	if clicked != 3 {
		t.Fatalf("want 3 clicks over the weeks, got %d", clicked)
	}
}

// TestProgramTimelineDayOverClockChange checks a person active on both sides
// of a daylight saving change counts once on that day. On 2026-10-25 the
// clock in Copenhagen goes back from 03:00 to 02:00, so 02:30 happens twice.
func TestProgramTimelineDayOverClockChange(t *testing.T) {
	p := newProgramServiceTest(t)
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no zone data")
	}
	programID, campaignID := p.timelineProgram(nil, "Europe/Copenhagen", false)
	alice := p.recipient("alice@example.com")
	// 02:30 summer time and 02:30 winter time
	summer := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	winter := time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)
	p.timelineSent(campaignID, alice, summer.Add(-time.Minute))
	p.timelineEvent(campaignID, alice, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, summer)
	p.timelineEvent(campaignID, alice, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, winter)
	p.timelineEvent(campaignID, alice, data.EVENT_CAMPAIGN_RECIPIENT_REPORTED, winter)
	program, err := p.svc.ProgramRepository.GetByID(p.ctx, &programID, &repository.ProgramOption{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, loc)
	timeline, err := p.svc.programTimeline(p.ctx, program, "2026-10-24", "2026-10-26", now)
	if err != nil {
		t.Fatal(err)
	}
	var day *ProgramTimelineBucket
	for _, b := range timeline.Buckets {
		if b.Day == "2026-10-25" {
			day = b
		}
	}
	if day == nil {
		t.Fatalf("no bucket for the day: %+v", timeline.Buckets)
	}
	if *day.Clicked != 1 || *day.Reported != 1 || *day.Sent != 1 {
		t.Fatalf("the day must count alice once: sent %d clicked %d reported %d", *day.Sent, *day.Clicked, *day.Reported)
	}
}

// TestProgramTimelineTraining checks a training send counts in Sent and in
// TrainingSent, opening a training page or sending its form is no read, click
// or submit, and a started or completed training counts once per person in
// its own rows, a completion counting as a start
func TestProgramTimelineTraining(t *testing.T) {
	p := newProgramServiceTest(t)
	loc := time.UTC
	programID, campaignID := p.timelineProgram(nil, "UTC", false)
	training := p.timelineCampaign(programID, 1)
	if err := p.db.Model(&database.Campaign{}).Where("id = ?", training).Update("is_training", true).Error; err != nil {
		t.Fatal(err)
	}
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	carol := p.recipient("carol@example.com")
	at := timelineAt(loc, -2, 9, 0)
	p.timelineSent(campaignID, alice, at)
	p.timelineEvent(campaignID, alice, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, at.Add(time.Minute))
	// bob only gets training, opens it, sends its form, starts and completes it
	p.timelineSent(training, bob, at)
	p.timelineEvent(training, bob, data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ, at.Add(time.Minute))
	p.timelineEvent(training, bob, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, at.Add(2*time.Minute))
	p.timelineEvent(training, bob, data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA, at.Add(3*time.Minute))
	p.timelineEvent(training, bob, data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED, at.Add(4*time.Minute))
	p.timelineEvent(training, bob, data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED, at.Add(5*time.Minute))
	// carol completes the next day with no start event
	p.timelineSent(training, carol, at)
	p.timelineEvent(training, carol, data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED, at.Add(24*time.Hour))
	// a training event on the phishing campaign is not a training
	p.timelineEvent(campaignID, alice, data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED, at.Add(time.Minute))
	for _, r := range [][2]string{{timelineDay(loc, -7), timelineDay(loc, 7)}, {timelineDay(loc, -90), timelineDay(loc, 90)}} {
		timeline, err := p.svc.GetTimeline(p.ctx, authorizedSession(t), &programID, nil, r[0], r[1])
		if err != nil {
			t.Fatal(err)
		}
		sent, trainingSent, read, clicked, submitted, started, completed := 0, 0, 0, 0, 0, 0, 0
		for _, b := range timeline.Buckets {
			sent += *b.Sent
			trainingSent += *b.TrainingSent
			read += *b.Read
			clicked += *b.Clicked
			submitted += *b.Submitted
			started += *b.TrainingStarted
			completed += *b.TrainingCompleted
		}
		if sent != 3 || trainingSent != 2 {
			t.Fatalf("%s: sent %d training sent %d, want 3 and 2", timeline.Bucket, sent, trainingSent)
		}
		if read != 0 || clicked != 1 || submitted != 0 {
			t.Fatalf("%s: training must not count as phishing: read %d clicked %d submitted %d",
				timeline.Bucket, read, clicked, submitted)
		}
		if started != 2 || completed != 2 {
			t.Fatalf("%s: started %d completed %d, want 2 and 2", timeline.Bucket, started, completed)
		}
	}
}
