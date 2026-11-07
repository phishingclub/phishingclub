package service

import (
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/model"
)

// programSlotSearchDays is how far ahead the next window opening is looked for
// first. Long blackout ranges are searched further, up to programSlotMaxDays.
const programSlotSearchDays = 15

// programSlotMaxDays is the longest look ahead for a window opening
const programSlotMaxDays = 400

// programPostponeDelay is how long a person waits when none of their next steps
// can be sent, for example because every template left is broken
const programPostponeDelay = 24 * time.Hour

// timeSpan is a half open span of time [start, end)
type timeSpan struct {
	start time.Time
	end   time.Time
}

// programWindowSpans returns the parts of [from, to) that fall inside the
// program's delivery window and outside its blackout days, read in the program
// time zone. Without a window and blackout days the whole span is allowed.
func programWindowSpans(program *model.Program, from, to time.Time) []timeSpan {
	if !to.After(from) {
		return nil
	}
	hasWindow := program.ConstraintWeekDays.IsSpecified() && !program.ConstraintWeekDays.IsNull() &&
		program.ConstraintStartTime.IsSpecified() && !program.ConstraintStartTime.IsNull() &&
		program.ConstraintEndTime.IsSpecified() && !program.ConstraintEndTime.IsNull()
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
		startMinutes = int(program.ConstraintStartTime.MustGet().Minutes() / time.Minute)
		endMinutes = int(program.ConstraintEndTime.MustGet().Minutes() / time.Minute)
	} else {
		for d := time.Sunday; d <= time.Saturday; d++ {
			allowed[d] = true
		}
	}

	spans := []timeSpan{}
	local := from.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	for !day.After(to) {
		if allowed[day.Weekday()] && !(hasBlackouts && program.IsBlackoutDay(day.Format(model.ProgramDateLayout))) {
			// built from the wall clock so a daylight saving change keeps the hours.
			// minute 1440 is midnight of the next day
			dayStart := time.Date(day.Year(), day.Month(), day.Day(), startMinutes/60, startMinutes%60, 0, 0, loc)
			dayEnd := time.Date(day.Year(), day.Month(), day.Day(), endMinutes/60, endMinutes%60, 0, 0, loc)
			s := dayStart
			if from.After(s) {
				s = from
			}
			e := dayEnd
			if to.Before(e) {
				e = to
			}
			if e.After(s) {
				spans = append(spans, timeSpan{start: s, end: e})
			}
		}
		day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc)
	}
	return spans
}

// programNextWindowSlot returns the first moment at or after t that is inside
// the delivery window and not on a blackout day. When no opening is found in
// the look ahead it returns the end of the look ahead, so nothing is sent on a
// blocked day. The time is checked again then.
func programNextWindowSlot(program *model.Program, t time.Time) time.Time {
	spans := programWindowSpans(program, t, t.Add(programSlotSearchDays*24*time.Hour))
	if len(spans) > 0 {
		return spans[0].start
	}
	if !program.HasBlackouts() {
		return t
	}
	end := t.Add(programSlotMaxDays * 24 * time.Hour)
	spans = programWindowSpans(program, t, end)
	if len(spans) > 0 {
		return spans[0].start
	}
	return end
}

// programPickSendTime draws a send time for a round that starts at roundStart.
// The time is uniform over the window minutes in [roundStart, roundStart+spread)
// that are not in the past, so recipients of the same round are spread out
// rather than sent together. With no spread or no window time left in the
// round, it is the next window opening.
func programPickSendTime(program *model.Program, roundStart, now time.Time, rng *rand.Rand) time.Time {
	return programPickSendTimeAfter(program, roundStart, now, time.Time{}, rng)
}

// programPickSendTimeAfter is programPickSendTime with a lower bound: the time
// is never before notBefore. A zero notBefore sets no bound.
func programPickSendTimeAfter(program *model.Program, roundStart, now, notBefore time.Time, rng *rand.Rand) time.Time {
	from := roundStart
	if now.After(from) {
		from = now
	}
	if notBefore.After(from) {
		from = notBefore
	}
	to := roundStart.Add(program.SpreadOrDefault())
	spans := programWindowSpans(program, from, to)
	var total time.Duration
	for _, sp := range spans {
		total += sp.end.Sub(sp.start)
	}
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

// programNextRoundStart returns when the round after one that started at
// roundStart begins. A round that would already be over, for example after a
// long pause, starts now instead, so overdue recipients are spread again rather
// than all sent at once.
func programNextRoundStart(program *model.Program, roundStart, now time.Time) time.Time {
	next := roundStart.Add(program.IntervalOrDefault())
	if next.Add(program.SpreadOrDefault()).Before(now) {
		return now
	}
	return next
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
