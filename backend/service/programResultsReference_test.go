package service

// The computation of named program results as it was before the facts store,
// kept as a reference: TestProgramResultsMatchReference checks the store gives
// the same results for a named program.

import (
	"sort"
	"time"

	"github.com/google/uuid"
)

// offendedSince reports whether the person clicked or submitted data on the
// send at or after since, the repeat offender rule of the recipient pages
func (s *programSend) offendedSince(since time.Time) bool {
	return (s.ClickedAt != nil && !s.ClickedAt.Before(since)) ||
		(s.SubmittedAt != nil && !s.SubmittedAt.Before(since))
}

// programTally counts sends and the people behind them
type programTally struct {
	sends  int
	people map[uuid.UUID]struct{}
}

func (t *programTally) add(person uuid.UUID) {
	t.sends++
	if t.people == nil {
		t.people = map[uuid.UUID]struct{}{}
	}
	t.people[person] = struct{}{}
}

// persons returns how many people the tally holds
func (t *programTally) persons() int {
	return len(t.people)
}

// resultCounts adds up sends and the people behind them before they become a
// result row
type resultCounts struct {
	sent, clicked, submitted, reported               programTally
	trainingSent, trainingStarted, trainingCompleted programTally
}

func (c *resultCounts) add(s *programSend) {
	if s.Training {
		c.trainingSent.add(s.Person)
		// a completed training counts as started
		if s.TrainingStartedAt != nil || s.TrainingCompletedAt != nil {
			c.trainingStarted.add(s.Person)
		}
		if s.TrainingCompletedAt != nil {
			c.trainingCompleted.add(s.Person)
		}
		return
	}
	c.sent.add(s.Person)
	if s.Clicked() {
		c.clicked.add(s.Person)
	}
	if s.SubmittedAt != nil {
		c.submitted.add(s.Person)
	}
	if s.ReportedAt != nil {
		c.reported.add(s.Person)
	}
}

// tooFew reports whether an anonymous program must withhold the whole group
func (c *resultCounts) tooFew(anonymous bool) bool {
	return anonymous && c.sent.persons() < programMinGroup
}

// row turns the counts into a result row. An anonymous program shows the
// sends only, and withholds them too for a small group. A named program shows
// the phishing counts when phishing is set and the training counts when
// training is set.
func (c *resultCounts) row(key, name string, anonymous, phishing, training bool) *ProgramResultRow {
	r := &ProgramResultRow{Key: key, Name: name, OutcomesHidden: anonymous}
	if c.tooFew(anonymous) {
		r.Suppressed = true
		return r
	}
	if anonymous {
		r.Sent = intPtr(c.sent.sends)
		return r
	}
	if phishing {
		r.Sent = intPtr(c.sent.sends)
		r.Clicked = intPtr(c.clicked.sends)
		r.Submitted = intPtr(c.submitted.sends)
		r.Reported = intPtr(c.reported.sends)
	}
	if training {
		r.TrainingSent = intPtr(c.trainingSent.sends)
		r.TrainingStarted = intPtr(c.trainingStarted.sends)
		r.TrainingCompleted = intPtr(c.trainingCompleted.sends)
	}
	return r
}

// inPeriod reports whether a send went out in the period from, inclusive, to
// to, exclusive. A nil bound is open.
func inPeriod(sd *programSend, from, to *time.Time) bool {
	if from != nil && sd.SentAt.Before(*from) {
		return false
	}
	if to != nil && !sd.SentAt.Before(*to) {
		return false
	}
	return true
}

// refBuildProgramResults computes the results from the facts. Only sends in
// [from, to) count when a bound is set. Month names use loc. An anonymous
// program counts only settled sends and outcomes, see programSettleCutoff, and
// reads now as the start of the day in loc.
func refBuildProgramResults(facts *programFacts, in programResultsInput) *ProgramResults {
	anonymous := facts.anonymous
	loc, now := in.loc, in.now
	if anonymous {
		now = startOfDay(now, loc)
	}
	out := &ProgramResults{Anonymous: anonymous, Months: []*ProgramResultRow{}, Templates: []*ProgramResultRow{}}
	sends := []*programSend{}
	for _, sd := range facts.sends {
		if inPeriod(sd, in.from, in.to) {
			sends = append(sends, sd)
		}
	}
	out.Summary = refProgramSummary(sends, in, now, anonymous)
	months := map[string]*resultCounts{}
	monthStarts := map[string]time.Time{}
	templates := map[string]*resultCounts{}
	for _, sd := range sends {
		if out.LastSentAt == nil || sd.SentAt.After(*out.LastSentAt) {
			t := sd.SentAt
			out.LastSentAt = &t
		}
		if sd.Training && anonymous {
			continue
		}
		local := sd.SentAt.In(loc)
		month := local.Format("2006-01")
		if months[month] == nil {
			months[month] = &resultCounts{}
			monthStarts[month] = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
		}
		months[month].add(sd)
		if templates[sd.Template] == nil {
			templates[sd.Template] = &resultCounts{}
		}
		templates[sd.Template].add(sd)
	}
	// the months the program or the period covers only in part
	runFrom := in.startedAt
	if in.from != nil && (runFrom == nil || in.from.After(*runFrom)) {
		runFrom = in.from
	}
	runTo := now
	if in.closedAt != nil && in.closedAt.Before(runTo) {
		runTo = *in.closedAt
	}
	if in.to != nil && in.to.Before(runTo) {
		runTo = *in.to
	}
	for key, c := range months {
		r := c.row(key, monthStarts[key].Format("Jan 2006"), anonymous, out.Summary.HasPhishing, out.Summary.HasTraining)
		start := monthStarts[key]
		end := start.AddDate(0, 1, 0)
		r.Partial = (runFrom != nil && runFrom.After(start)) || runTo.Before(end)
		out.Months = append(out.Months, r)
	}
	sort.Slice(out.Months, func(i, j int) bool { return out.Months[i].Key < out.Months[j].Key })
	type templateRow struct {
		row  *ProgramResultRow
		sent int
	}
	trows := []templateRow{}
	for key, c := range templates {
		name, ok := facts.templateNames[key]
		if !ok {
			name = programDeletedTemplateName
		}
		r := c.row(key, name, anonymous, c.sent.sends > 0, out.Summary.HasTraining && c.trainingSent.sends > 0)
		trows = append(trows, templateRow{r, c.sent.sends + c.trainingSent.sends})
	}
	sort.Slice(trows, func(i, j int) bool {
		if trows[i].sent != trows[j].sent {
			return trows[i].sent > trows[j].sent
		}
		return trows[i].row.Name < trows[j].row.Name
	})
	for _, t := range trows {
		out.Templates = append(out.Templates, t.row)
	}
	// an anonymous program shows the day of the last send, not the time
	if anonymous && out.LastSentAt != nil {
		day := startOfDay(*out.LastSentAt, loc)
		out.LastSentAt = &day
	}
	return out
}

// refProgramSummary counts the recipients of the sends, each once. Repeat
// offenders are counted back from the end of the period, or now. An anonymous
// program withholds every outcome when fewer than programMinGroup people were
// sent to, and any count withholdCount hides out of the people sent to.
func refProgramSummary(sends []*programSend, in programResultsInput, now time.Time, anonymous bool) *ProgramSummary {
	months := in.repeatMonths
	if months <= 0 {
		months = programRepeatOffenderMonthsDefault
	}
	out := &ProgramSummary{
		Recipients:           in.recipients,
		RepeatOffenderMonths: months,
		HasTraining:          in.hasTraining,
		HasPhishing:          in.hasPhishing,
	}
	end := now
	if in.to != nil && in.to.Before(end) {
		end = *in.to
	}
	since := end.AddDate(0, -months, 0)
	var sent, clicked, submitted, reported, trainingSent, trainingStarted, trainingCompleted programTally
	offences := map[uuid.UUID]int{}
	for _, sd := range sends {
		out.SentTotal++
		out.HasSent = true
		if sd.Training {
			out.HasTraining = true
			trainingSent.add(sd.Person)
			// a completed training counts as started
			if sd.TrainingStartedAt != nil || sd.TrainingCompletedAt != nil {
				trainingStarted.add(sd.Person)
			}
			if sd.TrainingCompletedAt != nil {
				trainingCompleted.add(sd.Person)
			}
			continue
		}
		out.HasPhishing = true
		sent.add(sd.Person)
		if sd.Clicked() {
			clicked.add(sd.Person)
		}
		if sd.SubmittedAt != nil {
			submitted.add(sd.Person)
		}
		if sd.ReportedAt != nil {
			reported.add(sd.Person)
		}
		if sd.offendedSince(since) {
			offences[sd.Person]++
		}
	}
	offenders := 0
	for _, n := range offences {
		if n >= 2 {
			offenders++
		}
	}
	// count returns n, or nil when an anonymous program withholds it out of group
	count := func(n, group int) *int {
		if anonymous && (group < programMinGroup || withholdCount(group, n)) {
			out.Suppressed = true
			return nil
		}
		return intPtr(n)
	}
	out.Sent = intPtr(sent.persons())
	out.ClickedLink = count(clicked.persons(), sent.persons())
	out.SubmittedData = count(submitted.persons(), sent.persons())
	out.Reported = count(reported.persons(), sent.persons())
	out.RepeatOffenders = count(offenders, sent.persons())
	// repeat offenders clicked or submitted, so the count would give away a
	// hidden clicked or submitted count
	if anonymous && (out.ClickedLink == nil || out.SubmittedData == nil) {
		out.RepeatOffenders = nil
		out.Suppressed = true
	}
	if out.HasTraining {
		out.TrainingSent = intPtr(trainingSent.persons())
		out.TrainingStarted = count(trainingStarted.persons(), trainingSent.persons())
		out.TrainingCompleted = count(trainingCompleted.persons(), trainingSent.persons())
	}
	return out
}

// refBuildProgramTiles counts the tiles of a named program the way the
// program page did before the facts store
func refBuildProgramTiles(
	facts *programFacts,
	in programResultsInput,
	period programTilesPeriod,
	closed bool,
	all *ProgramSummary,
) *ProgramTiles {
	out := &ProgramTiles{
		Range:       period.key,
		Recipients:  in.enrollment.active,
		Completed:   in.enrollment.completed,
		Removed:     in.enrollment.exited,
		HasTraining: in.hasTraining,
		HasPhishing: in.hasPhishing,
	}
	if closed {
		out.Recipients = in.enrollment.stopped
	}
	var sent, clicked, submitted, reported, trainingSent, trainingStarted, trainingCompleted programTally
	for _, sd := range facts.sends {
		if !inPeriod(sd, period.from, nil) {
			continue
		}
		if sd.Training {
			trainingSent.add(sd.Person)
			// a completed training counts as started
			if sd.TrainingStartedAt != nil || sd.TrainingCompletedAt != nil {
				trainingStarted.add(sd.Person)
			}
			if sd.TrainingCompletedAt != nil {
				trainingCompleted.add(sd.Person)
			}
			continue
		}
		sent.add(sd.Person)
		if sd.Clicked() {
			clicked.add(sd.Person)
		}
		if sd.SubmittedAt != nil {
			submitted.add(sd.Person)
		}
		if sd.ReportedAt != nil {
			reported.add(sd.Person)
		}
	}
	out.Sent = intPtr(sent.sends)
	out.ClickedLink = intPtr(clicked.sends)
	out.SubmittedData = intPtr(submitted.sends)
	out.Reported = intPtr(reported.sends)
	if out.HasTraining {
		out.TrainingSent = intPtr(trainingSent.sends)
		out.TrainingStarted = intPtr(trainingStarted.sends)
		out.TrainingCompleted = intPtr(trainingCompleted.sends)
	}
	if all != nil {
		out.RepeatOffenders = all.RepeatOffenders
		out.RepeatOffenderMonths = all.RepeatOffenderMonths
		if all.RepeatOffenders == nil {
			out.Suppressed = true
		}
	}
	return out
}
