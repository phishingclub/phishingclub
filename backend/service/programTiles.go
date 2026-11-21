package service

import "time"

// time ranges of the result tiles on the program page
const (
	ProgramTimeRange30Days   = "30d"
	ProgramTimeRange90Days   = "90d"
	ProgramTimeRange12Months = "12m"
	ProgramTimeRangeAll      = "all"
)

// IsValidProgramTimeRange reports whether v is a time range key of the result
// tiles
func IsValidProgramTimeRange(v string) bool {
	switch v {
	case ProgramTimeRange30Days, ProgramTimeRange90Days, ProgramTimeRange12Months, ProgramTimeRangeAll:
		return true
	}
	return false
}

// defaultProgramTimeRange is the time range the tiles show when none is
// chosen. An anonymous program counts settled days only and hides small
// groups, so it starts with a longer range.
func defaultProgramTimeRange(anonymous bool) string {
	if anonymous {
		return ProgramTimeRange90Days
	}
	return ProgramTimeRange30Days
}

// programTimeRangeFrom returns the first instant a time range counts at now:
// the start of the day in loc, today included, nil for since the start. The
// start moves once a day, so the result of a range can be kept for the day.
func programTimeRangeFrom(key string, now time.Time, loc *time.Location) *time.Time {
	today := startOfDay(now, loc)
	var from time.Time
	switch key {
	case ProgramTimeRange30Days:
		from = today.AddDate(0, 0, -29)
	case ProgramTimeRange90Days:
		from = today.AddDate(0, 0, -89)
	case ProgramTimeRange12Months:
		from = today.AddDate(-1, 0, 1)
	default:
		return nil
	}
	return &from
}

// programTilesPeriod is the time range the tiles count
type programTilesPeriod struct {
	key string
	// from is the first instant counted, nil for since the start
	from *time.Time
}

// ProgramTiles are the result tiles of the program page. Sent, Clicked link,
// Data submitted and Reported count phishing sends sent in the time range, a
// send counts once per outcome. The training counts read training sends the
// same way. Recipients is who is enrolled now, Repeat offenders keeps its own
// window. On a closed program Recipients is who was still active at the
// close, and the page shows Recipients, Completed and Removed together as
// everyone the program had. An anonymous program counts the last whole settled months of the
// range, named by RangeFrom and RangeTo, and withholds an outcome, null with
// Suppressed set, when a month of the range withholds it on its own, see
// programResultsAnonymous.go.
type ProgramTiles struct {
	// Range is the time range key the tiles count
	Range string `json:"range"`
	// RangeFrom and RangeTo are set on an anonymous program: the months
	// counted, [RangeFrom, RangeTo). RangeFrom is nil when no month is counted.
	RangeFrom *time.Time `json:"rangeFrom,omitempty"`
	RangeTo   *time.Time `json:"rangeTo,omitempty"`
	// RangeText names the months counted in the program time zone, such as
	// "Aug 2026 to Oct 2026", empty when no month is counted
	RangeText string `json:"rangeText,omitempty"`
	// Recipients is the people active now, or still active when the program
	// closed. With Completed and Removed it adds up to everyone the program
	// had, which a closed program shows.
	Recipients int `json:"recipients"`
	Completed  int `json:"completed"`
	Removed    int `json:"removed"`
	// Sent is the phishing sends in the range, null when an anonymous program
	// withholds the send total because a month of the range is too small
	Sent          *int `json:"sent"`
	ClickedLink   *int `json:"clickedLink"`
	SubmittedData *int `json:"submittedData"`
	Reported      *int `json:"reported"`
	// HasPhishing is set when the program has or had a phishing template
	HasPhishing bool `json:"hasPhishing"`
	// HasTraining is set when the program has or had a training template
	HasTraining       bool `json:"hasTraining"`
	TrainingSent      *int `json:"trainingSent"`
	TrainingStarted   *int `json:"trainingStarted"`
	TrainingCompleted *int `json:"trainingCompleted"`
	// RepeatOffenders is the people who clicked or submitted data on 2 or
	// more sends within RepeatOffenderMonths back from now, whatever the range
	RepeatOffenders      *int `json:"repeatOffenders"`
	RepeatOffenderMonths int  `json:"repeatOffenderMonths"`
	Suppressed           bool `json:"suppressed"`
}

// buildProgramTiles computes the tiles of loaded facts for a time range. all
// is the summary since the start, the repeat offenders come from it.
func buildProgramTiles(
	facts *programFacts,
	in programResultsInput,
	period programTilesPeriod,
	closed bool,
	all *ProgramSummary,
) *ProgramTiles {
	st := programFactsStoreOf(facts)
	var anon *programAnonymousFacts
	if st.anonymous {
		anon = st.anonymousFacts(in.now, in.loc)
	}
	return st.programTiles(anon, in, period, closed, all)
}

// programTiles counts the tiles of a time range. anon holds the settled
// months of an anonymous program. The caller holds the read lock.
func (st *programFactsStore) programTiles(
	anon *programAnonymousFacts,
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
	// an anonymous program counts the people who had every template with the
	// people still in it, as the recipient page does, since when a person
	// finished would point at the time of their sends
	if st.anonymous {
		out.Recipients += out.Completed
		out.Completed = 0
	}
	if anon != nil {
		st.anonymousTiles(anon, period, out)
	} else {
		st.namedTiles(period, out)
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
