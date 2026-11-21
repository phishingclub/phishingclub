package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// the bounds of the schedule timeline in days of the program zone. A range
// may start at most programTimelineMaxBack days before today and end at most
// programTimelineMaxAhead days after it. One day more than the 90 the page
// offers each way lets a browser whose clock is already on the next or the
// last day ask for the range it shows.
const (
	programTimelineMaxBack  = 91
	programTimelineMaxAhead = 91
	// programTimelineWeekAfter is the longest range counted per day, a longer
	// range is counted per week
	programTimelineWeekAfter = 62
	// programTimelineDefaultDays is how far back and ahead the timeline reads
	// when no range is given
	programTimelineDefaultDays = 30
)

// the bucket sizes of the timeline
const (
	ProgramTimelineBucketDay  = "day"
	ProgramTimelineBucketWeek = "week"
)

// ProgramTimeline is the schedule of a program per day or per week in the
// program zone: what was sent and done on the days before now and what is
// planned to be sent from now on. An anonymous program has no timeline, only
// the Anonymous flag is set, see GetTimeline.
type ProgramTimeline struct {
	Anonymous bool   `json:"anonymous"`
	TimeZone  string `json:"timeZone"`
	// From, To and Today are YYYY-MM-DD dates in the program zone, From and To
	// both included
	From   string `json:"from"`
	To     string `json:"to"`
	Today  string `json:"today"`
	Bucket string `json:"bucket"`
	// Buckets cover the range in order, each its first day and how many days
	// of the range it holds
	Buckets []*ProgramTimelineBucket `json:"buckets"`
	// StartAt is when the program started, or is set to start
	StartAt *time.Time `json:"startAt"`
	// EndAt is the end date of the program, nil when it runs until closed
	EndAt *time.Time `json:"endAt"`
	// ClosedAt is when the program was closed
	ClosedAt *time.Time `json:"closedAt"`
	// PausedAt is set while the program is paused. Earlier pauses are not
	// stored, so the timeline shows the current one only.
	PausedAt *time.Time `json:"pausedAt"`
	// SkipDays are the runs of skip dates within the range
	SkipDays []ProgramTimelineDays `json:"skipDays"`
}

// ProgramTimelineDays is a run of days, both ends included
type ProgramTimelineDays struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ProgramTimelineBucket holds the counts of one day or one week. Sent and
// Planned count sends, phishing and training, and TrainingSent is the
// training part of Sent. The outcome counts are people, each counted once per
// kind in the bucket, by when the event happened. Read, Clicked, Submitted
// and Reported count phishing sends only, TrainingStarted and
// TrainingCompleted training sends only, a completion counting as a start.
// The outcome counts and the sent counts are nil on an anonymous program and
// are left out of the response.
type ProgramTimelineBucket struct {
	Day               string `json:"day"`
	Days              int    `json:"days"`
	Sent              *int   `json:"sent,omitempty"`
	TrainingSent      *int   `json:"trainingSent,omitempty"`
	Read              *int   `json:"read,omitempty"`
	Clicked           *int   `json:"clicked,omitempty"`
	Submitted         *int   `json:"submitted,omitempty"`
	Reported          *int   `json:"reported,omitempty"`
	TrainingStarted   *int   `json:"trainingStarted,omitempty"`
	TrainingCompleted *int   `json:"trainingCompleted,omitempty"`
	Planned           int    `json:"planned"`
}

// programTimelineRange is a range of whole days in a zone
type programTimelineRange struct {
	loc            *time.Location
	from, to       time.Time
	today          time.Time
	days           []time.Time
	start, end     time.Time
	fromKey, toKey string
}

// parseProgramTimelineRange reads from and to, YYYY-MM-DD dates in loc. Both
// empty is the default range around today. A range that is not well formed,
// runs backwards or reaches past the bounds is refused.
func parseProgramTimelineRange(fromText, toText string, now time.Time, loc *time.Location) (*programTimelineRange, error) {
	today := startOfDay(now, loc)
	var from, to time.Time
	switch {
	case fromText == "" && toText == "":
		from = today.AddDate(0, 0, -programTimelineDefaultDays)
		to = today.AddDate(0, 0, programTimelineDefaultDays)
	case fromText == "" || toText == "":
		return nil, errs.NewValidationError(errors.New("from and to must be given together"))
	default:
		var err error
		from, err = time.ParseInLocation(model.ProgramDateLayout, fromText, loc)
		if err != nil {
			return nil, errs.NewValidationError(errors.New("from must be a YYYY-MM-DD date"))
		}
		to, err = time.ParseInLocation(model.ProgramDateLayout, toText, loc)
		if err != nil {
			return nil, errs.NewValidationError(errors.New("to must be a YYYY-MM-DD date"))
		}
	}
	if to.Before(from) {
		return nil, errs.NewValidationError(errors.New("to must not be before from"))
	}
	if from.Before(today.AddDate(0, 0, -programTimelineMaxBack)) {
		return nil, errs.NewValidationError(errors.New("from is too far back"))
	}
	if to.After(today.AddDate(0, 0, programTimelineMaxAhead)) {
		return nil, errs.NewValidationError(errors.New("to is too far ahead"))
	}
	r := &programTimelineRange{loc: loc, from: from, to: to, today: today}
	// days are walked by date, so a day of 23 or 25 hours is still one day
	for d := from; !d.After(to); d = time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc) {
		r.days = append(r.days, d)
	}
	r.start = from
	r.end = time.Date(to.Year(), to.Month(), to.Day()+1, 0, 0, 0, 0, loc)
	r.fromKey = from.Format(model.ProgramDateLayout)
	r.toKey = to.Format(model.ProgramDateLayout)
	return r, nil
}

// programTimelineSpans splits [start, end) where the zone offset changes, so
// each span reads its days with one offset
func programTimelineSpans(start, end time.Time, loc *time.Location) []repository.ProgramTimelineSpan {
	spans := []repository.ProgramTimelineSpan{}
	if !start.Before(end) {
		return spans
	}
	offsetAt := func(t time.Time) int {
		_, offset := t.In(loc).Zone()
		return offset
	}
	cur := start
	offset := offsetAt(cur)
	// no zone changes its offset twice within an hour, so stepping by hours
	// and halving down to the instant finds each change
	for t := cur.Add(time.Hour); t.Before(end); t = t.Add(time.Hour) {
		if offsetAt(t) == offset {
			continue
		}
		lo, hi := t.Add(-time.Hour), t
		for hi.Sub(lo) > time.Nanosecond {
			mid := lo.Add(hi.Sub(lo) / 2)
			if offsetAt(mid) == offset {
				lo = mid
			} else {
				hi = mid
			}
		}
		spans = append(spans, repository.ProgramTimelineSpan{From: cur, To: hi, Offset: offset})
		cur = hi
		offset = offsetAt(cur)
	}
	return append(spans, repository.ProgramTimelineSpan{From: cur, To: end, Offset: offset})
}

// programTimelineEventKinds returns the event ids the timeline counts
func programTimelineEventKinds() repository.ProgramTimelineEventKinds {
	return repository.ProgramTimelineEventKinds{
		Read: cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ],
		Clicked: []*uuid.UUID{
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
		},
		Submitted:         cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
		Reported:          cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED],
		TrainingStarted:   cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED],
		TrainingCompleted: cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED],
	}
}

// GetTimeline returns the schedule timeline of a program for the days from
// and to, YYYY-MM-DD dates in the program zone, or the default range when
// both are empty. companyID, when given, is the company in view, and a
// program of another company is not found.
//
// The days before now count sends, and the people with a read, a click, a
// submit, a report, a started or a completed training by the day or the week
// the event happened. From now on the planned sends are counted by when they
// are planned: the rows waiting to be sent in the open hidden campaigns, and
// the next send of each active person, which the scheduler stores as a row
// only when it is due. An anonymous program has no timeline: its results are
// counted by whole settled month and a count per day, planned sends included,
// could point at one person, so nothing is read for it and only the Anonymous
// flag is given.
func (s *Program) GetTimeline(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	companyID *uuid.UUID,
	fromText, toText string,
) (*ProgramTimeline, error) {
	ae := NewAuditEvent("Program.GetTimeline", session)
	program, err := s.authorizedProgram(ctx, session, id, ae, nil)
	if err != nil {
		return nil, err
	}
	if companyID != nil {
		if owner, err := program.CompanyID.Get(); err == nil && owner != *companyID {
			return nil, errs.Wrap(gorm.ErrRecordNotFound)
		}
	}
	return s.programTimeline(ctx, program, fromText, toText, time.Now())
}

// programTimeline computes the timeline of a loaded program at now
func (s *Program) programTimeline(
	ctx context.Context,
	program *model.Program,
	fromText, toText string,
	now time.Time,
) (*ProgramTimeline, error) {
	loc := program.Location()
	// an anonymous program has no timeline, not even its planned sends, as a
	// count per day can point at one person. Only the flag is given.
	if program.IsAnonymousOn() {
		return &ProgramTimeline{
			Anonymous: true,
			TimeZone:  loc.String(),
			Buckets:   []*ProgramTimelineBucket{},
			SkipDays:  []ProgramTimelineDays{},
		}, nil
	}
	r, err := parseProgramTimelineRange(fromText, toText, now, loc)
	if err != nil {
		return nil, err
	}
	programID := program.ID.MustGet()
	anonymous := false
	out := &ProgramTimeline{
		Anonymous: anonymous,
		TimeZone:  loc.String(),
		From:      r.fromKey,
		To:        r.toKey,
		Today:     r.today.Format(model.ProgramDateLayout),
		Bucket:    ProgramTimelineBucketDay,
		StartAt:   program.StartedAt,
		EndAt:     program.EndAtTime(),
		SkipDays:  []ProgramTimelineDays{},
	}
	if out.StartAt == nil {
		out.StartAt = program.StartAtTime()
	}
	if closedAt, err := program.ClosedAt.Get(); err == nil {
		out.ClosedAt = &closedAt
	}
	if pausedAt, err := program.PausedAt.Get(); err == nil && !program.IsClosed() {
		out.PausedAt = &pausedAt
	}

	// counts per day, in the order of the days
	index := make(map[string]int, len(r.days))
	daily := make([]*ProgramTimelineBucket, len(r.days))
	for i, d := range r.days {
		key := d.Format(model.ProgramDateLayout)
		index[key] = i
		daily[i] = &ProgramTimelineBucket{Day: key, Days: 1}
		if program.IsBlackoutDay(key) {
			n := len(out.SkipDays)
			if n > 0 && out.SkipDays[n-1].To == daily[i-1].Day {
				out.SkipDays[n-1].To = key
			} else {
				out.SkipDays = append(out.SkipDays, ProgramTimelineDays{From: key, To: key})
			}
		}
	}

	// the past reaches up to now, the planned sends start at now
	pastEnd := minTime(now, r.end)
	planStart := maxTime(now, r.start)
	weekly := len(r.days) > programTimelineWeekAfter
	var outcomes map[string]programTimelineOutcomes
	if !anonymous {
		zero := func() *int { v := 0; return &v }
		for _, b := range daily {
			b.Sent, b.TrainingSent, b.Read, b.Clicked, b.Submitted, b.Reported = zero(), zero(), zero(), zero(), zero(), zero()
			b.TrainingStarted, b.TrainingCompleted = zero(), zero()
		}
		// the whole days before today, or the whole weeks before this week,
		// are kept for a while and the rest is read every time. A week is
		// split at its Monday only, so each person counts once per week.
		keptEnd := r.today
		if weekly {
			keptEnd = programTimelineMonday(r.today)
		}
		keptEnd = minTime(keptEnd, pastEnd)
		kept, err := s.timelineKeptCounts(ctx, &programID, r.start, keptEnd, loc, now, weekly)
		if err != nil {
			return nil, err
		}
		live, err := s.timelineCounts(ctx, &programID, maxTime(keptEnd, r.start), pastEnd, loc, weekly)
		if err != nil {
			return nil, err
		}
		outcomes = map[string]programTimelineOutcomes{}
		for _, counts := range []*programTimelineCounts{kept, live} {
			for day, n := range counts.sent {
				if i, ok := index[day]; ok {
					*daily[i].Sent += n
				}
			}
			for day, n := range counts.trainingSent {
				if i, ok := index[day]; ok {
					*daily[i].TrainingSent += n
				}
			}
			for key, c := range counts.outcomes {
				outcomes[key] = outcomes[key].add(c)
			}
		}
	}
	if planSpans := programTimelineSpans(planStart, r.end, loc); len(planSpans) > 0 && !program.IsClosed() {
		planned, err := s.ProgramRepository.GetTimelinePlanned(ctx, &programID, planSpans)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		due, err := s.ProgramRepository.GetTimelineDue(ctx, &programID, planSpans, program.EndAtTime())
		if err != nil {
			return nil, errs.Wrap(err)
		}
		for _, row := range append(planned, due...) {
			if i, ok := index[row.Day]; ok {
				daily[i].Planned += row.Count
			}
		}
	}

	buckets := daily
	if weekly {
		out.Bucket = ProgramTimelineBucketWeek
		buckets = programTimelineWeeks(daily, r.days)
	}
	// the outcomes are keyed by the first day of a day bucket, or by the
	// Monday of a week bucket, which is before the range for the first week
	for _, b := range buckets {
		if b.Read == nil {
			continue
		}
		key := b.Day
		if weekly {
			day, err := time.ParseInLocation(model.ProgramDateLayout, b.Day, loc)
			if err != nil {
				return nil, errs.Wrap(err)
			}
			key = programTimelineMonday(day).Format(model.ProgramDateLayout)
		}
		c := outcomes[key]
		*b.Read, *b.Clicked, *b.Submitted, *b.Reported = c.read, c.clicked, c.submitted, c.reported
		*b.TrainingStarted, *b.TrainingCompleted = c.trainingStarted, c.trainingCompleted
	}
	out.Buckets = buckets
	return out, nil
}

// programTimelineMonday returns the start of the Monday on or before a day
func programTimelineMonday(day time.Time) time.Time {
	back := (int(day.Weekday()) + 6) % 7
	return time.Date(day.Year(), day.Month(), day.Day()-back, 0, 0, 0, 0, day.Location())
}

// programTimelineOutcomes are the people with each outcome in a day or a week
type programTimelineOutcomes struct {
	read, clicked, submitted, reported int
	trainingStarted, trainingCompleted int
}

func (c programTimelineOutcomes) add(o programTimelineOutcomes) programTimelineOutcomes {
	return programTimelineOutcomes{
		read:              c.read + o.read,
		clicked:           c.clicked + o.clicked,
		submitted:         c.submitted + o.submitted,
		reported:          c.reported + o.reported,
		trainingStarted:   c.trainingStarted + o.trainingStarted,
		trainingCompleted: c.trainingCompleted + o.trainingCompleted,
	}
}

// programTimelineCounts are the counts of a named program over a part of the
// range: the sends per day, the training part of them, and the outcomes per
// day, or per week by the Monday it starts on
type programTimelineCounts struct {
	sent         map[string]int
	trainingSent map[string]int
	outcomes     map[string]programTimelineOutcomes
}

// timelineCounts counts the sends and outcomes of a named program in
// [start, end) in loc, the outcomes per week when weekly
func (s *Program) timelineCounts(
	ctx context.Context,
	programID *uuid.UUID,
	start, end time.Time,
	loc *time.Location,
	weekly bool,
) (*programTimelineCounts, error) {
	out := &programTimelineCounts{
		sent:         map[string]int{},
		trainingSent: map[string]int{},
		outcomes:     map[string]programTimelineOutcomes{},
	}
	spans := programTimelineSpans(start, end, loc)
	if len(spans) == 0 {
		return out, nil
	}
	sent, err := s.ProgramRepository.GetTimelineSent(ctx, programID, spans)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, row := range sent {
		out.sent[row.Day] += row.Count
		if row.Kind == repository.ProgramTimelineSentTraining {
			out.trainingSent[row.Day] += row.Count
		}
	}
	kinds := programTimelineEventKinds()
	var rows []repository.ProgramTimelineCount
	if weekly {
		rows, err = s.ProgramRepository.GetTimelineOutcomesByWeek(ctx, programID, kinds, spans)
	} else {
		rows, err = s.ProgramRepository.GetTimelineOutcomes(ctx, programID, kinds, spans)
	}
	if err != nil {
		return nil, errs.Wrap(err)
	}
	training, err := s.ProgramRepository.GetTimelineTrainingOutcomes(ctx, programID, kinds, spans, weekly)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, row := range append(rows, training...) {
		c := out.outcomes[row.Day]
		switch row.Kind {
		case repository.ProgramTimelineKindRead:
			c.read += row.Count
		case repository.ProgramTimelineKindClicked:
			c.clicked += row.Count
		case repository.ProgramTimelineKindSubmitted:
			c.submitted += row.Count
		case repository.ProgramTimelineKindReported:
			c.reported += row.Count
		case repository.ProgramTimelineKindTrainingStarted:
			c.trainingStarted += row.Count
		case repository.ProgramTimelineKindTrainingCompleted:
			c.trainingCompleted += row.Count
		}
		out.outcomes[row.Day] = c
	}
	return out, nil
}

// the counts of whole past days or weeks are kept for programTimelineKeepFor,
// at most programTimelineKeepMax ranges. A send or an event is stored at the
// time it happens, so a whole day before today rarely changes. A report
// recorded later with the time it was made can show up to
// programTimelineKeepFor late.
const (
	programTimelineKeepFor = 5 * time.Minute
	programTimelineKeepMax = 64
)

type programTimelineKept struct {
	at     time.Time
	counts *programTimelineCounts
}

var programTimelineStore = struct {
	sync.Mutex
	ranges map[string]programTimelineKept
}{ranges: map[string]programTimelineKept{}}

// timelineKeptCounts counts [start, end) like timelineCounts and keeps the
// result, see programTimelineKeepFor
func (s *Program) timelineKeptCounts(
	ctx context.Context,
	programID *uuid.UUID,
	start, end time.Time,
	loc *time.Location,
	now time.Time,
	weekly bool,
) (*programTimelineCounts, error) {
	if !start.Before(end) {
		return &programTimelineCounts{}, nil
	}
	bucket := ProgramTimelineBucketDay
	if weekly {
		bucket = ProgramTimelineBucketWeek
	}
	key := programID.String() + "|" + loc.String() + "|" + bucket + "|" + start.UTC().Format(time.RFC3339) + "|" + end.UTC().Format(time.RFC3339)
	programTimelineStore.Lock()
	kept, ok := programTimelineStore.ranges[key]
	programTimelineStore.Unlock()
	if ok && now.Sub(kept.at) < programTimelineKeepFor && !now.Before(kept.at) {
		return kept.counts, nil
	}
	counts, err := s.timelineCounts(ctx, programID, start, end, loc, weekly)
	if err != nil {
		return nil, err
	}
	programTimelineStore.Lock()
	defer programTimelineStore.Unlock()
	if len(programTimelineStore.ranges) >= programTimelineKeepMax {
		// drop what is too old, then the oldest when still full. The first
		// entry is always a candidate, so one is dropped also when every
		// entry was kept at the same time.
		oldestKey, oldestAt, found := "", time.Time{}, false
		for k, v := range programTimelineStore.ranges {
			if now.Sub(v.at) >= programTimelineKeepFor {
				delete(programTimelineStore.ranges, k)
				continue
			}
			if !found || v.at.Before(oldestAt) {
				oldestKey, oldestAt, found = k, v.at, true
			}
		}
		if found && len(programTimelineStore.ranges) >= programTimelineKeepMax {
			delete(programTimelineStore.ranges, oldestKey)
		}
	}
	programTimelineStore.ranges[key] = programTimelineKept{at: now, counts: counts}
	return counts, nil
}

// programTimelineWeeks adds the sends of the days up into weeks that start on
// Monday. The first and the last week hold only the days of the range. The
// outcome counts of a week are not sums of its days, as a person active on
// two days counts once in the week, so they start at zero and are set from
// the counts per week.
func programTimelineWeeks(daily []*ProgramTimelineBucket, days []time.Time) []*ProgramTimelineBucket {
	zero := func(a *int) *int {
		if a == nil {
			return nil
		}
		v := 0
		return &v
	}
	weeks := []*ProgramTimelineBucket{}
	var week *ProgramTimelineBucket
	for i, b := range daily {
		if week == nil || days[i].Weekday() == time.Monday {
			week = &ProgramTimelineBucket{
				Day: b.Day, Sent: zero(b.Sent), TrainingSent: zero(b.TrainingSent),
				Read: zero(b.Read), Clicked: zero(b.Clicked), Submitted: zero(b.Submitted), Reported: zero(b.Reported),
				TrainingStarted: zero(b.TrainingStarted), TrainingCompleted: zero(b.TrainingCompleted),
			}
			weeks = append(weeks, week)
		}
		week.Days++
		if week.Sent != nil && b.Sent != nil {
			*week.Sent += *b.Sent
		}
		if week.TrainingSent != nil && b.TrainingSent != nil {
			*week.TrainingSent += *b.TrainingSent
		}
		week.Planned += b.Planned
	}
	return weeks
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
