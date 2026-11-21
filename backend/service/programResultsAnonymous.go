package service

import (
	"sort"
	"time"
)

// An anonymous program counts whole calendar months only, in the program time
// zone. A month is counted once it has ended and settled, programOpenAge
// later, and an outcome of a send counts when it happened before the month
// settled. Each month is then fixed: reading it later gives the same counts,
// so results read on different days can not be compared to learn when one
// person acted.
//
// Every view an operator can read, the program page time ranges and the report
// periods, is a run of whole settled months, and its outcome counts are the
// sums of the month counts, each send counted once per outcome. An outcome is
// shown for a view only when every month in it would show the outcome on its
// own: at least programMinGroup people sent to, withholdCount does not hide
// the people behind it, a zero included, and withholdGap does not hide it
// next to a related outcome. Clicked and submitted are related, as every
// submit is a click, and so are training started and completed: when they
// differ by fewer than programMinGroup people in a month, both are hidden.
// Two views then differ by whole months that each pass that rule, so
// subtracting one shown count from another gives the count of such months and
// never singles out fewer than programMinGroup people.
//
// Repeat offenders count people across months, a count that is not a sum of
// month counts, so an anonymous program never shows it.

// programAnonymousRangeMonths is how many whole months each time range of the
// result tiles covers in an anonymous program, 0 for since the start
var programAnonymousRangeMonths = map[string]int{
	ProgramTimeRange30Days:   1,
	ProgramTimeRange90Days:   3,
	ProgramTimeRange12Months: 12,
	ProgramTimeRangeAll:      0,
}

// the person flag bits in the order the month counts hold them
const (
	anonSent = iota
	anonClicked
	anonSubmitted
	anonReported
	anonTrainingSent
	anonTrainingStarted
	anonTrainingCompleted
	anonFlags
)

// programAnonymousCountedTo returns the first instant an anonymous program
// does not count at now: the start of the month that holds the settle cutoff.
// Every month before it has ended and settled.
func programAnonymousCountedTo(now time.Time, loc *time.Location) time.Time {
	cutoff := programSettleCutoff(now, loc).In(loc)
	return time.Date(cutoff.Year(), cutoff.Month(), 1, 0, 0, 0, 0, loc)
}

// programMonthSettle returns when the month ending at end settles: an outcome
// of a send in the month counts when it happened before this time
func programMonthSettle(end time.Time, loc *time.Location) time.Time {
	return startOfDay(end, loc).AddDate(0, 0, int(programOpenAge/(24*time.Hour)))
}

// programAnonymousFixedFrom returns the first instant at which an outcome no
// longer changes any month an anonymous program shows at now: when the last
// shown month settled. It is never after now. An outcome recorded with an
// earlier time, such as a report read late from a report inbox, could change
// a month already shown, so it is recorded at this time instead.
func programAnonymousFixedFrom(now time.Time, loc *time.Location) time.Time {
	return programMonthSettle(programAnonymousCountedTo(now, loc), loc)
}

// programAnonMonth counts one whole settled month of an anonymous program
type programAnonMonth struct {
	start, end time.Time
	// sends counts the sends per flag, persons the people per flag
	sends   [anonFlags]int
	persons [anonFlags]int
}

// programAnonymousFacts are the settled months of an anonymous program at now
type programAnonymousFacts struct {
	countedTo time.Time
	// months are oldest first
	months []*programAnonMonth
	// monthOf finds the month of a send, -1 for a send not counted
	monthOf func(sentAt int64) int
	order   []int
}

// anonymousFacts counts every whole settled month of an anonymous program
func (st *programFactsStore) anonymousFacts(now time.Time, loc *time.Location) *programAnonymousFacts {
	countedTo := programAnonymousCountedTo(now, loc)
	limit := unixNanos(countedTo)
	months := newProgramMonths(loc)
	counts := []*programAnonMonth{}
	people := []map[int32]uint8{}
	settle := []int64{}
	for i := range st.sends {
		sd := &st.sends[i]
		if sd.sentAt >= limit {
			continue
		}
		m := months.of(sd.sentAt)
		for len(counts) <= m {
			mo := months.months[len(counts)]
			counts = append(counts, &programAnonMonth{start: mo.start, end: mo.end})
			people = append(people, map[int32]uint8{})
			settle = append(settle, unixNanos(programMonthSettle(mo.end, loc)))
		}
		until := settle[m]
		f := personFlags(sd, func(at int64) bool { return at < until })
		for bit := 0; bit < anonFlags; bit++ {
			if f&(1<<bit) != 0 {
				counts[m].sends[bit]++
			}
		}
		people[m][sd.person] |= f
	}
	for m, byPerson := range people {
		for _, f := range byPerson {
			for bit := 0; bit < anonFlags; bit++ {
				if f&(1<<bit) != 0 {
					counts[m].persons[bit]++
				}
			}
		}
	}
	order := make([]int, len(counts))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return counts[order[i]].start.Before(counts[order[j]].start) })
	sorted := make([]*programAnonMonth, 0, len(counts))
	for _, i := range order {
		sorted = append(sorted, counts[i])
	}
	out := &programAnonymousFacts{countedTo: countedTo, months: sorted}
	out.monthOf = func(sentAt int64) int {
		if sentAt >= limit {
			return -1
		}
		return months.of(sentAt)
	}
	out.order = order
	return out
}

// inView reports whether a month is in the view [from, to), a nil bound is
// open. A view holds whole months.
func (m *programAnonMonth) inView(from, to *time.Time) bool {
	return (from == nil || !m.start.Before(*from)) && (to == nil || m.start.Before(*to))
}

// programAnonTotals adds up the months of a view
type programAnonTotals struct {
	sends [anonFlags]int
	// withheld is set for an outcome a month of the view withholds on its own
	withheld [anonFlags]bool
	// sentWithheld is set for a send count group, anonSent or anonTrainingSent,
	// when a month of the view sent to 1 to programMinGroup minus 1 people. The
	// send total of a view that holds such a month would single that month out,
	// for example a one month range or report, so the send total is withheld
	sentWithheld [anonFlags]bool
}

func anonTotalsOf(months []*programAnonMonth) programAnonTotals {
	t := programAnonTotals{}
	for _, m := range months {
		for bit := 0; bit < anonFlags; bit++ {
			t.sends[bit] += m.sends[bit]
		}
		for _, group := range []int{anonSent, anonTrainingSent} {
			if n := m.persons[group]; n > 0 && n < programMinGroup {
				t.sentWithheld[group] = true
			}
		}
		for _, check := range []struct {
			group int
			bits  []int
			// related are the pairs of outcomes checked by withholdGap
			related [][2]int
		}{
			{anonSent, []int{anonClicked, anonSubmitted, anonReported}, [][2]int{{anonClicked, anonSubmitted}}},
			{anonTrainingSent, []int{anonTrainingStarted, anonTrainingCompleted}, [][2]int{{anonTrainingStarted, anonTrainingCompleted}}},
		} {
			group := m.persons[check.group]
			if group == 0 {
				continue
			}
			for _, bit := range check.bits {
				if group < programMinGroup || withholdCount(group, m.persons[bit]) {
					t.withheld[bit] = true
				}
			}
			for _, pair := range check.related {
				if withholdGap(m.persons[pair[0]], m.persons[pair[1]]) {
					t.withheld[pair[0]] = true
					t.withheld[pair[1]] = true
				}
			}
		}
	}
	return t
}

// sent returns the send total of a group, anonSent or anonTrainingSent, nil
// when a month of the view sent to fewer than programMinGroup people and the
// total would single that month out
func (t *programAnonTotals) sent(group int) *int {
	if t.sentWithheld[group] {
		return nil
	}
	return intPtr(t.sends[group])
}

// count returns the sends of an outcome, nil when the view has no sends of
// its kind or a month withholds it
func (t *programAnonTotals) count(bit int) *int {
	group := anonSent
	if bit >= anonTrainingSent {
		group = anonTrainingSent
	}
	if t.sends[group] == 0 || t.withheld[bit] {
		return nil
	}
	return intPtr(t.sends[bit])
}

// view returns the months of [from, to)
func (a *programAnonymousFacts) view(from, to *time.Time) []*programAnonMonth {
	out := []*programAnonMonth{}
	for _, m := range a.months {
		if m.inView(from, to) {
			out = append(out, m)
		}
	}
	return out
}

// anonymousResults computes the results of an anonymous program for the
// period [from, to) of the input, see the rules at the top of this file. The
// summary counts sends, the rows show sizes only.
func (st *programFactsStore) anonymousResults(a *programAnonymousFacts, in programResultsInput) *ProgramResults {
	loc := in.loc
	out := &ProgramResults{Months: []*ProgramResultRow{}, Templates: []*ProgramResultRow{}}
	countedTo := a.countedTo
	if in.to != nil && in.to.Before(countedTo) {
		countedTo = *in.to
	}
	out.CountedTo = &countedTo
	months := a.view(in.from, in.to)
	t := anonTotalsOf(months)
	// the templates and the last send read the sends of the view's months
	inView := map[int]bool{}
	for i, m := range a.months {
		if m.inView(in.from, in.to) {
			inView[a.order[i]] = true
		}
	}
	templateCounts := make([]programRowSends, len(st.templates))
	templatePeople := make([]map[int32]struct{}, len(st.templates))
	// the people sent each template per month: a template row adds up its
	// months, so a month that sent it to too few people would show as the
	// difference between views, the same as a month row
	templateMonthPeople := make([]map[int]map[int32]struct{}, len(st.templates))
	last := programFactNone
	for i := range st.sends {
		sd := &st.sends[i]
		m := a.monthOf(sd.sentAt)
		if m < 0 || !inView[m] {
			continue
		}
		if sd.sentAt > last {
			last = sd.sentAt
		}
		if sd.training {
			continue
		}
		templateCounts[sd.template].used = true
		templateCounts[sd.template].sent++
		if templatePeople[sd.template] == nil {
			templatePeople[sd.template] = map[int32]struct{}{}
		}
		templatePeople[sd.template][sd.person] = struct{}{}
		if templateMonthPeople[sd.template] == nil {
			templateMonthPeople[sd.template] = map[int]map[int32]struct{}{}
		}
		if templateMonthPeople[sd.template][m] == nil {
			templateMonthPeople[sd.template][m] = map[int32]struct{}{}
		}
		templateMonthPeople[sd.template][m][sd.person] = struct{}{}
	}
	// a month in which a template went to too few people shows no send count,
	// and neither does the summary of a view that holds such a month, or the
	// count minus the shown template rows would give the withheld template
	templateWithheld := withheldTemplateMonths(templateMonthPeople)
	repeatMonths := in.repeatMonths
	if repeatMonths <= 0 {
		repeatMonths = programRepeatOffenderMonthsDefault
	}
	sum := &ProgramSummary{
		Recipients:           in.recipients,
		RepeatOffenderMonths: repeatMonths,
		HasTraining:          in.hasTraining || t.sends[anonTrainingSent] > 0,
		HasPhishing:          in.hasPhishing || t.sends[anonSent] > 0,
		SentTotal:            t.sends[anonSent] + t.sends[anonTrainingSent],
		Sent:                 t.sent(anonSent),
		// repeat offenders count people across months, see the top of the file
		Suppressed: true,
	}
	sum.ClickedLink = t.count(anonClicked)
	sum.SubmittedData = t.count(anonSubmitted)
	sum.Reported = t.count(anonReported)
	if sum.HasTraining {
		sum.TrainingSent = t.sent(anonTrainingSent)
		sum.TrainingStarted = t.count(anonTrainingStarted)
		sum.TrainingCompleted = t.count(anonTrainingCompleted)
	}
	sum.HasSent = sum.SentTotal > 0
	if len(templateWithheld) > 0 {
		sum.Sent = nil
	}
	// the total adds phishing and training sends, so it is withheld when
	// either group is, or subtracting Sent would give the withheld one
	if t.sentWithheld[anonSent] || t.sentWithheld[anonTrainingSent] || len(templateWithheld) > 0 {
		sum.SentTotal = 0
		sum.SentTotalHidden = true
	}
	out.Summary = sum
	// the rows show the size only, withheld for fewer than programMinGroup
	// people
	row := func(key, name string, sent, persons int) *ProgramResultRow {
		r := &ProgramResultRow{Key: key, Name: name, OutcomesHidden: true}
		if persons < programMinGroup {
			r.Suppressed = true
			return r
		}
		r.Sent = intPtr(sent)
		return r
	}
	for i, m := range a.months {
		if !m.inView(in.from, in.to) || m.sends[anonSent] == 0 {
			continue
		}
		r := row(m.start.Format("2006-01"), m.start.Format("Jan 2006"), m.sends[anonSent], m.persons[anonSent])
		if templateWithheld[a.order[i]] {
			r.Sent = nil
			r.Suppressed = true
		}
		r.Partial = partialMonth(in, a.countedTo, m.start, m.end)
		out.Months = append(out.Months, r)
	}
	out.Templates = st.sortedTemplateRows(templateCounts, func(i int, c *programRowSends) *ProgramResultRow {
		r := row(st.templates[i], st.names[i], c.sent, len(templatePeople[i]))
		for _, people := range templateMonthPeople[i] {
			if len(people) < programMinGroup {
				r.Sent = nil
				r.Suppressed = true
				break
			}
		}
		return r
	})
	// the month of the last send, a day could tell when one person was sent
	if last != programFactNone {
		at := time.Unix(0, last).In(loc)
		month := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, loc)
		out.LastSentAt = &month
	}
	return out
}

// anonymousTiles counts the result tiles of an anonymous program: the last
// whole settled months of the time range, see the rules at the top of the
// file
func (st *programFactsStore) anonymousTiles(a *programAnonymousFacts, period programTilesPeriod, out *ProgramTiles) {
	to := a.countedTo
	var from *time.Time
	if n := programAnonymousRangeMonths[period.key]; n > 0 {
		f := to.AddDate(0, -n, 0)
		from = &f
	}
	months := a.view(from, &to)
	if from == nil && len(months) > 0 {
		from = &months[0].start
	}
	out.RangeFrom = from
	out.RangeTo = &to
	if from != nil {
		first := from.Format("Jan 2006")
		last := to.AddDate(0, -1, 0).Format("Jan 2006")
		out.RangeText = first
		if last != first {
			out.RangeText = first + " to " + last
		}
	}
	t := anonTotalsOf(months)
	out.Sent = t.sent(anonSent)
	// the send count of a range equals the summary of the same view, which
	// is withheld when a template of one of its months is
	if out.Sent != nil {
		inView := map[int]bool{}
		for i, m := range a.months {
			if m.inView(from, &to) {
				inView[a.order[i]] = true
			}
		}
		if len(withheldTemplateMonths(st.anonTemplateMonthPeople(a, inView))) > 0 {
			out.Sent = nil
		}
	}
	out.ClickedLink = t.count(anonClicked)
	out.SubmittedData = t.count(anonSubmitted)
	out.Reported = t.count(anonReported)
	if out.ClickedLink == nil || out.SubmittedData == nil || out.Reported == nil {
		out.Suppressed = true
	}
	if out.HasTraining {
		out.TrainingSent = t.sent(anonTrainingSent)
		out.TrainingStarted = t.count(anonTrainingStarted)
		out.TrainingCompleted = t.count(anonTrainingCompleted)
		if out.TrainingStarted == nil || out.TrainingCompleted == nil {
			out.Suppressed = true
		}
	}
}

// anonTemplateMonthPeople returns, for each template, the people its
// phishing sends went to in each month of inView, keyed by the month index
// of monthOf
func (st *programFactsStore) anonTemplateMonthPeople(a *programAnonymousFacts, inView map[int]bool) []map[int]map[int32]struct{} {
	out := make([]map[int]map[int32]struct{}, len(st.templates))
	for i := range st.sends {
		sd := &st.sends[i]
		if sd.training {
			continue
		}
		m := a.monthOf(sd.sentAt)
		if m < 0 || !inView[m] {
			continue
		}
		if out[sd.template] == nil {
			out[sd.template] = map[int]map[int32]struct{}{}
		}
		if out[sd.template][m] == nil {
			out[sd.template][m] = map[int32]struct{}{}
		}
		out[sd.template][m][sd.person] = struct{}{}
	}
	return out
}

// withheldTemplateMonths returns the months, by the month index of monthOf,
// in which some template went to fewer than programMinGroup people. The row
// of such a template is withheld, so the send count of that month and of any
// view that holds it is withheld too, or subtracting the shown template rows
// would give it.
func withheldTemplateMonths(templateMonthPeople []map[int]map[int32]struct{}) map[int]bool {
	out := map[int]bool{}
	for _, byMonth := range templateMonthPeople {
		for m, people := range byMonth {
			if len(people) < programMinGroup {
				out[m] = true
			}
		}
	}
	return out
}
