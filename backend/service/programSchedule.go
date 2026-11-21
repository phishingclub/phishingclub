package service

import (
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/model"
)

// lockedSource is a random source that is safe to share between goroutines
type lockedSource struct {
	mu  sync.Mutex
	src rand.Source64
}

// Int63 returns a non negative random number
func (l *lockedSource) Int63() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.src.Int63()
}

// Uint64 returns a random number
func (l *lockedSource) Uint64() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.src.Uint64()
}

// Seed seeds the source
func (l *lockedSource) Seed(seed int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.src.Seed(seed)
}

// programRandSource is the one random source of the program scheduling
var programRandSource = &lockedSource{
	src: rand.NewSource(time.Now().UnixNano()).(rand.Source64),
}

// programSlotSearchDays is how far ahead the next window opening is looked for
// first. Long blackout ranges are searched further, up to programSlotMaxDays.
const programSlotSearchDays = 15

// programSlotMaxDays is the longest look ahead for a window opening in one
// search
const programSlotMaxDays = 400

// programSlotMaxSearches is how many searches of programSlotMaxDays follow one
// another before the look ahead ends. A one off skip range is at most a year
// and a program has at most model.ProgramBlackoutMax of them, so the ranges
// put end to end are covered.
const programSlotMaxSearches = model.ProgramBlackoutMax + 5

// programPostponeDelay is how long a person waits when none of their next steps
// can be sent, for example because every template left is broken
const programPostponeDelay = 24 * time.Hour

// timeSpan is a half open span of time [start, end)
type timeSpan struct {
	start time.Time
	end   time.Time
}

// programWindowBound returns the instant of a window bound, minutes after
// midnight, on a date in loc. Minute 0 and the end of the day are the first
// instant of the day and of the next day, see model.ProgramWallTime. A bound
// in a clock change gap or overlap is refused when the window is saved, see
// model validateWindowClock. A program saved before that rule falls back to
// the normalized wall time.
func programWindowBound(y int, mo time.Month, d int, minutes int, loc *time.Location) time.Time {
	if t, ok := model.ProgramWallTime(y, mo, d, minutes, loc); ok {
		return t
	}
	return time.Date(y, mo, d, minutes/60, minutes%60, 0, 0, loc)
}

// programWindowSpans returns the parts of [from, to) that fall inside the
// program's delivery window and outside its blackout days, read in the program
// time zone. Without a window and blackout days the whole span is allowed. An
// end of 23:59 runs to midnight.
func programWindowSpans(program *model.Program, from, to time.Time) []timeSpan {
	if !to.After(from) {
		return nil
	}
	hasWindow := program.HasWindow()
	hasBlackouts := program.HasBlackouts()
	if !hasWindow && !hasBlackouts {
		return []timeSpan{{start: from, end: to}}
	}
	loc := program.Location()
	allowed := map[time.Weekday]bool{}
	// without a window every day is allowed from midnight to midnight
	startMinutes := 0
	endMinutes := 24 * 60
	if hasWindow {
		weekDays := program.ConstraintWeekDays.MustGet()
		for _, d := range weekDays.AsSlice() {
			allowed[time.Weekday(d)] = true
		}
		startMinutes, endMinutes = program.WindowMinutes()
	} else {
		for d := time.Sunday; d <= time.Saturday; d++ {
			allowed[d] = true
		}
	}

	spans := []timeSpan{}
	// dates are walked in UTC, which has no clock changes, and each window is
	// built from the wall clock in the zone, so a clock change keeps the hours
	fy, fmo, fd := from.In(loc).Date()
	ty, tmo, td := to.In(loc).Date()
	last := time.Date(ty, tmo, td, 0, 0, 0, 0, time.UTC)
	for day := time.Date(fy, fmo, fd, 0, 0, 0, 0, time.UTC); !day.After(last); day = day.AddDate(0, 0, 1) {
		if !allowed[day.Weekday()] || (hasBlackouts && program.IsBlackoutDay(day.Format(model.ProgramDateLayout))) {
			continue
		}
		y, mo, d := day.Date()
		s := programWindowBound(y, mo, d, startMinutes, loc)
		e := programWindowBound(y, mo, d, endMinutes, loc)
		if from.After(s) {
			s = from
		}
		if to.Before(e) {
			e = to
		}
		if e.After(s) {
			spans = append(spans, timeSpan{start: s, end: e})
		}
	}
	return spans
}

// programNextWindowSlot returns the first moment at or after t that is inside
// the delivery window and not on a blackout day. A send is held until the
// blackout ranges in its way have ended, however long they run. Only when no
// day is free in the whole look ahead, which the saved ranges leave no room
// for, it returns the end of the look ahead, a time far out that is checked
// again then.
func programNextWindowSlot(program *model.Program, t time.Time) time.Time {
	spans := programWindowSpans(program, t, t.Add(programSlotSearchDays*24*time.Hour))
	if len(spans) > 0 {
		return spans[0].start
	}
	if !program.HasBlackouts() {
		return t
	}
	from := t
	for i := 0; i < programSlotMaxSearches; i++ {
		end := from.Add(programSlotMaxDays * 24 * time.Hour)
		if spans := programWindowSpans(program, from, end); len(spans) > 0 {
			return spans[0].start
		}
		from = end
	}
	return from
}

// programPickSendTime draws a send time for a round that starts at roundStart.
// The time is uniform over the window minutes in [roundStart, roundStart+spread)
// that are not in the past, so recipients of the same round are spread out
// rather than sent together. A round that holds less sending time than a whole
// round is made longer, see programRoundEnd. With no spread or no window time
// left in the round, it is the next window opening.
func programPickSendTime(program *model.Program, roundStart, now time.Time, rng *rand.Rand) time.Time {
	return programPickSendTimeAfter(program, roundStart, now, time.Time{}, rng)
}

// programPickSendTimeAfter is programPickSendTime with a lower bound: the time
// is never before notBefore. A zero notBefore sets no bound. The length of the
// round is decided from the later of roundStart and now, the bound of the
// person only moves the start of the draw.
func programPickSendTimeAfter(program *model.Program, roundStart, now, notBefore time.Time, rng *rand.Rand) time.Time {
	base := roundStart
	if now.After(base) {
		base = now
	}
	spread := program.SpreadOrDefault()
	// a spread of whole days is counted on the calendar like the interval, so
	// a clock change does not add or take an hour of sending time
	to := programAddInterval(program, roundStart, spread)
	if spread > 0 {
		to = programRoundEnd(program, base, to, spread)
	}
	from := base
	if notBefore.After(from) {
		from = notBefore
	}
	spans := programWindowSpans(program, from, to)
	total := programSpansLength(spans)
	if total <= 0 {
		return programNextWindowSlot(program, from)
	}
	offset := time.Duration(rng.Int63n(int64(total)))
	for _, sp := range spans {
		length := sp.end.Sub(sp.start)
		if offset < length {
			return sp.start.Add(offset).Truncate(time.Second)
		}
		offset -= length
	}
	return spans[len(spans)-1].start
}

// programRoundEnd returns the end of a round that is read from base and would
// end at to. A whole round holds the sending time of the spread measured from
// the opening of the sending hours base falls in, or of the next ones. When
// [base, to) holds less, because the round starts late in the sending hours,
// outside them or partly in the past, the round ends later, once it holds that
// much. So a start or a resume on a Friday evening spreads the sends over the
// next sending hours instead of sending everyone at the next opening.
func programRoundEnd(program *model.Program, base, to time.Time, spread time.Duration) time.Time {
	opening := programWindowOpening(program, base)
	full := programSpansLength(programWindowSpans(program, opening, programAddInterval(program, opening, spread)))
	if full <= 0 || programSpansLength(programWindowSpans(program, base, to)) >= full {
		return to
	}
	return programWindowEndAfter(program, base, full, base.Add(spread+programSlotSearchDays*24*time.Hour))
}

// programWindowOpening returns the start of the sending hours t falls in, or of
// the next ones when t is outside them. Without a window and blackout days
// every time is sending time and it is t.
func programWindowOpening(program *model.Program, t time.Time) time.Time {
	if !program.HasWindow() && !program.HasBlackouts() {
		return t
	}
	loc := program.Location()
	y, mo, d := t.In(loc).Date()
	dayStart, _ := model.ProgramWallTime(y, mo, d, 0, loc)
	dayEnd, _ := model.ProgramWallTime(y, mo, d, 24*60, loc)
	if spans := programWindowSpans(program, dayStart, dayEnd); len(spans) > 0 && t.Before(spans[0].end) {
		return spans[0].start
	}
	return programNextWindowSlot(program, t)
}

// programWindowEndAfter returns the earliest end at which [from, end) holds
// amount of sending time. The time up to horizon is searched first, then up to
// programSlotMaxDays more. When that holds less, it returns the end of the
// search.
func programWindowEndAfter(program *model.Program, from time.Time, amount time.Duration, horizon time.Time) time.Time {
	last := horizon.Add(programSlotMaxDays * 24 * time.Hour)
	for _, limit := range []time.Time{horizon, last} {
		var held time.Duration
		for _, sp := range programWindowSpans(program, from, limit) {
			length := sp.end.Sub(sp.start)
			if held+length >= amount {
				return sp.start.Add(amount - held)
			}
			held += length
		}
	}
	return last
}

// programSpansLength returns the time the spans hold together
func programSpansLength(spans []timeSpan) time.Duration {
	var total time.Duration
	for _, sp := range spans {
		total += sp.end.Sub(sp.start)
	}
	return total
}

// programNextRoundStart returns when the round after one that started at
// roundStart begins. A round that would already be over, for example after a
// long pause, starts now instead, so overdue recipients are spread again rather
// than all sent at once.
func programNextRoundStart(program *model.Program, roundStart, now time.Time) time.Time {
	next := programAddInterval(program, roundStart, program.IntervalOrDefault())
	if programAddInterval(program, next, program.SpreadOrDefault()).Before(now) {
		return now
	}
	return next
}

// programAddInterval returns t plus the interval. An interval of whole days is
// added as calendar days in the program time zone, so a round keeps its wall
// clock time over a clock change and stays inside a narrow window. A negative
// interval moves back the same way.
func programAddInterval(program *model.Program, t time.Time, interval time.Duration) time.Time {
	const day = 24 * time.Hour
	if interval == 0 || interval%day != 0 {
		return t.Add(interval)
	}
	return t.In(program.Location()).AddDate(0, 0, int(interval/day)).In(t.Location())
}

// programMinNextSend returns the earliest time a person may be sent again after
// a send at lastSend: half the step interval later. Zero when there was no send.
func programMinNextSend(program *model.Program, lastSend *time.Time) time.Time {
	if lastSend == nil {
		return time.Time{}
	}
	return lastSend.Add(program.IntervalOrDefault() / 2)
}

// programLoopOpeners splits the candidates of a new loop in shuffle mode into
// the preferred ones and the step sent last, so the new loop does not open with
// the template the person just had. With one step, sequential order or no
// earlier send everything is preferred.
func programLoopOpeners(
	program *model.Program,
	candidates []*model.ProgramStep,
	lastStepID *uuid.UUID,
) (preferred []*model.ProgramStep, fallback []*model.ProgramStep) {
	if lastStepID == nil || len(candidates) < 2 || program.OrderModeOrDefault() != model.ProgramOrderShuffle {
		return candidates, nil
	}
	for _, c := range candidates {
		if c.ID.IsSpecified() && !c.ID.IsNull() && c.ID.MustGet() == *lastStepID {
			return removeStep(candidates, c), []*model.ProgramStep{c}
		}
	}
	return candidates, nil
}

// programPickStep chooses the next step for a recipient among the scheduled
// steps they have not had in the current loop. Shuffle picks at random,
// sequential takes the first by position. It returns nil when none are left.
func programPickStep(program *model.Program, candidates []*model.ProgramStep, rng *rand.Rand) *model.ProgramStep {
	if len(candidates) == 0 {
		return nil
	}
	if program.OrderModeOrDefault() == model.ProgramOrderShuffle {
		return candidates[rng.Intn(len(candidates))]
	}
	first := candidates[0]
	for _, c := range candidates[1:] {
		if stepPosition(c) < stepPosition(first) {
			first = c
		}
	}
	return first
}

func stepPosition(s *model.ProgramStep) int {
	if s.Position.IsSpecified() && !s.Position.IsNull() {
		return s.Position.MustGet()
	}
	return 0
}
