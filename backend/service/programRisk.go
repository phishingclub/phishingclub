package service

import (
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// risk and results tuning for programs
const (
	// programRiskWindow is how far back a send counts toward a person's risk
	programRiskWindow = 365 * 24 * time.Hour
	// programRiskMinSends is how many sends a person needs before a risk is shown
	programRiskMinSends = 3
	// programOpenAge is how long a send is too young to judge. Sends younger than
	// this are counted as open in period comparisons.
	programOpenAge = 3 * 24 * time.Hour
	// programCoverageWindow is how recent a send must be to count as coverage
	programCoverageWindow = 90 * 24 * time.Hour
	// programRepeatLastSends is how many of a person's last sends are checked for
	// repeat clicking
	programRepeatLastSends = 6
	// programMinGroup is the smallest group an anonymous program shows counts for
	programMinGroup = 5
	// programDifficultyMinSent is how many sends a template needs for a label
	programDifficultyMinSent = 30
	// programExposureMaxRows caps the exposure rows, later sends join the last row
	programExposureMaxRows = 12
	// programRiskMediumFrom and programRiskHighFrom are the scores where a
	// person's risk becomes medium and high
	programRiskMediumFrom = 30.0
	programRiskHighFrom   = 60.0
	// programDifficultyHardRate and programDifficultyMediumRate are the click
	// rates where a template is labelled hard and medium
	programDifficultyHardRate   = 0.20
	programDifficultyMediumRate = 0.08
)

// risk levels of a person
const (
	ProgramRiskLow    = "low"
	ProgramRiskMedium = "medium"
	ProgramRiskHigh   = "high"
)

// send scores, higher is riskier
const (
	programScoreSubmitted       = 100.0
	programScoreClicked         = 70.0
	programScoreClickedReported = 50.0
	programScoreIgnored         = 20.0
	programScoreReported        = 0.0
	programScoreTrainingCredit  = 10.0
)

// programSend is one sent message of a program with what the person did. Each
// outcome holds the first time it happened, so page reloads count once.
type programSend struct {
	Campaign    uuid.UUID
	Person      uuid.UUID
	RecipientID *uuid.UUID
	Template    string
	SentAt      time.Time
	Training    bool
	ReadAt      *time.Time
	ClickedAt   *time.Time
	SubmittedAt *time.Time
	ReportedAt  *time.Time
	// TrainingStartedAt and TrainingCompletedAt are set on training sends
	TrainingStartedAt   *time.Time
	TrainingCompletedAt *time.Time
}

// Clicked reports whether the person clicked, a submit counts as a click
func (s *programSend) Clicked() bool {
	return s.ClickedAt != nil || s.SubmittedAt != nil
}

// Kind returns whether the send was phishing or training
func (s *programSend) Kind() string {
	if s.Training {
		return ProgramSendKindTraining
	}
	return ProgramSendKindPhishing
}

// programSendScore scores one phishing send by its outcome
func programSendScore(s *programSend) float64 {
	switch {
	case s.SubmittedAt != nil:
		return programScoreSubmitted
	case s.ClickedAt != nil && s.ReportedAt != nil:
		return programScoreClickedReported
	case s.ClickedAt != nil:
		return programScoreClicked
	case s.ReportedAt != nil:
		return programScoreReported
	}
	return programScoreIgnored
}

// programRiskLevel names the level of a risk score
func programRiskLevel(score *float64) string {
	if score == nil {
		return ""
	}
	switch {
	case *score < programRiskMediumFrom:
		return ProgramRiskLow
	case *score < programRiskHighFrom:
		return ProgramRiskMedium
	}
	return ProgramRiskHigh
}

// programPersonRisk returns one person's risk from their phishing sends and the
// times they completed training. Only sends of the last year count. Each
// completed training lowers the latest failed send before it that has not had
// a training yet. Fewer than programRiskMinSends sends give nil.
func programPersonRisk(sends []*programSend, trainingDone []time.Time, now time.Time) *float64 {
	recent := make([]*programSend, 0, len(sends))
	for _, s := range sends {
		if s.Training || now.Sub(s.SentAt) > programRiskWindow {
			continue
		}
		recent = append(recent, s)
	}
	if len(recent) < programRiskMinSends {
		return nil
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].SentAt.Before(recent[j].SentAt) })
	scores := make([]float64, len(recent))
	for i, s := range recent {
		scores[i] = programSendScore(s)
	}
	done := append([]time.Time(nil), trainingDone...)
	sort.Slice(done, func(i, j int) bool { return done[i].Before(done[j]) })
	credited := make([]bool, len(recent))
	for _, at := range done {
		for i := len(recent) - 1; i >= 0; i-- {
			if recent[i].SentAt.After(at) || credited[i] || !recent[i].Clicked() {
				continue
			}
			credited[i] = true
			scores[i] -= programScoreTrainingCredit
			if scores[i] < 0 {
				scores[i] = 0
			}
			break
		}
	}
	total := 0.0
	for _, v := range scores {
		total += v
	}
	mean := total / float64(len(scores))
	return &mean
}

// programByPerson groups the phishing sends by person, oldest first
func programByPerson(sends []*programSend) map[uuid.UUID][]*programSend {
	out := map[uuid.UUID][]*programSend{}
	for _, s := range sends {
		if s.Training {
			continue
		}
		out[s.Person] = append(out[s.Person], s)
	}
	for _, list := range out {
		sort.Slice(list, func(i, j int) bool { return list[i].SentAt.Before(list[j].SentAt) })
	}
	return out
}

// ProgramRepeatClickers counts people who clicked on several of their last
// sends of the last year. TwoPlus is out of the people with 2 or more sends,
// ThreePlus out of the people with 3 or more.
type ProgramRepeatClickers struct {
	Eligible2  *int `json:"eligible2"`
	TwoPlus    *int `json:"twoPlus"`
	Eligible3  *int `json:"eligible3"`
	ThreePlus  *int `json:"threePlus"`
	Suppressed bool `json:"suppressed"`
}

// programRepeatClickers counts repeat clickers. For an anonymous program each
// count is only shown when its group has programMinGroup people or more.
func programRepeatClickers(byPerson map[uuid.UUID][]*programSend, now time.Time, anonymous bool) *ProgramRepeatClickers {
	eligible2, two, eligible3, three := 0, 0, 0, 0
	for _, list := range byPerson {
		recent := []*programSend{}
		for _, s := range list {
			if now.Sub(s.SentAt) <= programRiskWindow {
				recent = append(recent, s)
			}
		}
		if len(recent) > programRepeatLastSends {
			recent = recent[len(recent)-programRepeatLastSends:]
		}
		clicks := 0
		for _, s := range recent {
			if s.Clicked() {
				clicks++
			}
		}
		if len(recent) >= 2 {
			eligible2++
			if clicks >= 2 {
				two++
			}
		}
		if len(recent) >= 3 {
			eligible3++
			if clicks >= 3 {
				three++
			}
		}
	}
	out := &ProgramRepeatClickers{}
	// an anonymous program shows a count only when its group is big enough and
	// the count itself is not a handful of people that could be pointed out
	show := func(eligible, count int) bool {
		return !anonymous || (eligible >= programMinGroup && !withholdCount(eligible, count))
	}
	if show(eligible2, two) {
		out.Eligible2, out.TwoPlus = intPtr(eligible2), intPtr(two)
	} else {
		out.Suppressed = true
	}
	if show(eligible3, three) {
		out.Eligible3, out.ThreePlus = intPtr(eligible3), intPtr(three)
	} else {
		out.Suppressed = true
	}
	return out
}

// withholdCount reports whether an anonymous program must hide count out of
// group. A count of 1 to programMinGroup - 1 is hidden, and so is a count that
// leaves fewer than programMinGroup of the group out, as that tells nearly
// everyone did it. A count of zero is shown.
func withholdCount(group, count int) bool {
	if count <= 0 {
		return false
	}
	return count < programMinGroup || group-count < programMinGroup
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

// withheldIn reports whether an anonymous program must hide the outcome tally
// t of group g. Both the sends and the people are checked, so one person with
// many sends can not stand out.
func (t *programTally) withheldIn(g *programTally) bool {
	return withholdCount(g.sends, t.sends) || withholdCount(g.persons(), t.persons())
}

// ProgramPhishProne is the share of people who clicked at least once in the
// last year, out of the people sent a simulation in that year
type ProgramPhishProne struct {
	People     *int     `json:"people"`
	Prone      *int     `json:"prone"`
	Rate       *float64 `json:"rate"`
	Suppressed bool     `json:"suppressed"`
}

func programPhishProne(byPerson map[uuid.UUID][]*programSend, now time.Time, anonymous bool) *ProgramPhishProne {
	people, prone := 0, 0
	for _, list := range byPerson {
		sent, clicked := false, false
		for _, s := range list {
			if now.Sub(s.SentAt) > programRiskWindow {
				continue
			}
			sent = true
			if s.Clicked() {
				clicked = true
			}
		}
		if !sent {
			continue
		}
		people++
		if clicked {
			prone++
		}
	}
	if anonymous && (people < programMinGroup || withholdCount(people, prone)) {
		return &ProgramPhishProne{Suppressed: true}
	}
	out := &ProgramPhishProne{People: intPtr(people), Prone: intPtr(prone)}
	if people > 0 {
		r := float64(prone) / float64(people)
		out.Rate = &r
	}
	return out
}

// ProgramExposureRow holds the outcome of every person's nth phishing send, so
// the same people can be compared across rounds
type ProgramExposureRow struct {
	N          int      `json:"n"`
	Label      string   `json:"label"`
	Sent       *int     `json:"sent"`
	Clicked    *int     `json:"clicked"`
	Reported   *int     `json:"reported"`
	ClickRate  *float64 `json:"clickRate"`
	ReportRate *float64 `json:"reportRate"`
	Suppressed bool     `json:"suppressed"`
}

// programExposure builds the exposure rows. The nth send counts every send of
// the person, but only sends older than programOpenAge are judged. For an
// anonymous program a row with fewer than programMinGroup people is withheld,
// and a click or report count that is small or nearly everyone is withheld
// with its rate.
func programExposure(byPerson map[uuid.UUID][]*programSend, now time.Time, anonymous bool) []*ProgramExposureRow {
	type acc struct{ sent, clicked, reported programTally }
	rows := map[int]*acc{}
	maxN := 0
	for person, list := range byPerson {
		for i, s := range list {
			n := i + 1
			if n > programExposureMaxRows {
				n = programExposureMaxRows
			}
			if now.Sub(s.SentAt) < programOpenAge {
				continue
			}
			if rows[n] == nil {
				rows[n] = &acc{}
			}
			rows[n].sent.add(person)
			if s.Clicked() {
				rows[n].clicked.add(person)
			}
			if s.ReportedAt != nil {
				rows[n].reported.add(person)
			}
			if n > maxN {
				maxN = n
			}
		}
	}
	out := []*ProgramExposureRow{}
	for n := 1; n <= maxN; n++ {
		a := rows[n]
		if a == nil {
			a = &acc{}
		}
		label := ordinal(n)
		if n == programExposureMaxRows {
			label += " or later"
		}
		row := &ProgramExposureRow{N: n, Label: label}
		if anonymous && a.sent.persons() < programMinGroup {
			row.Suppressed = true
			out = append(out, row)
			continue
		}
		row.Sent = intPtr(a.sent.sends)
		count := func(t *programTally) (*int, *float64) {
			if anonymous && t.withheldIn(&a.sent) {
				return nil, nil
			}
			if a.sent.sends == 0 {
				return intPtr(t.sends), nil
			}
			r := float64(t.sends) / float64(a.sent.sends)
			return intPtr(t.sends), &r
		}
		row.Clicked, row.ClickRate = count(&a.clicked)
		row.Reported, row.ReportRate = count(&a.reported)
		out = append(out, row)
	}
	return out
}

// ProgramResilience shows how well people report phishing. For an anonymous
// program a value that would point at a few people is null and its json name
// is listed in Withheld, so a null that means hidden can be told from a null
// that means none.
type ProgramResilience struct {
	Sent                     *int     `json:"sent"`
	ReportRate               *float64 `json:"reportRate"`
	ReportedWithoutClickRate *float64 `json:"reportedWithoutClickRate"`
	// ResilienceRatio is reports per click, nil when nobody clicked
	ResilienceRatio *float64 `json:"resilienceRatio"`
	// MedianMinutesToReport is the median time from send to report
	MedianMinutesToReport *float64 `json:"medianMinutesToReport"`
	Suppressed            bool     `json:"suppressed"`
	Withheld              []string `json:"withheld,omitempty"`
}

func programResilience(sends []*programSend, anonymous bool) *ProgramResilience {
	var sent, reported, reportedNoClick, clickedReported, clicked programTally
	minutes := []float64{}
	for _, s := range sends {
		if s.Training {
			continue
		}
		sent.add(s.Person)
		if s.Clicked() {
			clicked.add(s.Person)
		}
		if s.ReportedAt != nil {
			reported.add(s.Person)
			if s.Clicked() {
				clickedReported.add(s.Person)
			} else {
				reportedNoClick.add(s.Person)
			}
			d := s.ReportedAt.Sub(s.SentAt).Minutes()
			if d < 0 {
				d = 0
			}
			minutes = append(minutes, d)
		}
	}
	if anonymous && sent.persons() < programMinGroup {
		return &ProgramResilience{Suppressed: true}
	}
	out := &ProgramResilience{Sent: intPtr(sent.sends)}
	hide := func(name string, tallies ...*programTally) bool {
		if !anonymous {
			return false
		}
		for _, t := range tallies {
			if t.withheldIn(&sent) {
				out.Withheld = append(out.Withheld, name)
				return true
			}
		}
		return false
	}
	if sent.sends > 0 {
		if !hide("reportRate", &reported) {
			rr := float64(reported.sends) / float64(sent.sends)
			out.ReportRate = &rr
		}
		// the reports after a click are the difference of the two rates, so
		// they are checked too
		if !hide("reportedWithoutClickRate", &reportedNoClick, &clickedReported) {
			rn := float64(reportedNoClick.sends) / float64(sent.sends)
			out.ReportedWithoutClickRate = &rn
		}
	}
	if !hide("resilienceRatio", &reported, &clicked) && clicked.sends > 0 {
		ratio := float64(reported.sends) / float64(clicked.sends)
		out.ResilienceRatio = &ratio
	}
	// an anonymous median needs programMinGroup reports or more, a median of a
	// few reports is close to one person's time
	medianHidden := anonymous && len(minutes) > 0 && (len(minutes) < programMinGroup || reported.withheldIn(&sent))
	if medianHidden {
		out.Withheld = append(out.Withheld, "medianMinutesToReport")
	} else if len(minutes) > 0 {
		sort.Float64s(minutes)
		var m float64
		mid := len(minutes) / 2
		if len(minutes)%2 == 1 {
			m = minutes[mid]
		} else {
			m = (minutes[mid-1] + minutes[mid]) / 2
		}
		out.MedianMinutesToReport = &m
	}
	return out
}

// ProgramCoverage is the share of active people sent a phishing message in the
// last 90 days
type ProgramCoverage struct {
	Active  int      `json:"active"`
	Covered int      `json:"covered"`
	Rate    *float64 `json:"rate"`
}

func programCoverage(active []uuid.UUID, byPerson map[uuid.UUID][]*programSend, now time.Time) *ProgramCoverage {
	out := &ProgramCoverage{Active: len(active)}
	for _, p := range active {
		for _, s := range byPerson[p] {
			if now.Sub(s.SentAt) <= programCoverageWindow {
				out.Covered++
				break
			}
		}
	}
	if out.Active > 0 {
		r := float64(out.Covered) / float64(out.Active)
		out.Rate = &r
	}
	return out
}

// template difficulty labels, from the click rate of a template
const (
	ProgramDifficultyHard   = "Hard to spot"
	ProgramDifficultyMedium = "Medium"
	ProgramDifficultyEasy   = "Easy"
)

// programDifficulty labels a template by its click rate once it has been sent
// programDifficultyMinSent times
func programDifficulty(sent, clicked int) string {
	if sent < programDifficultyMinSent {
		return ""
	}
	rate := float64(clicked) / float64(sent)
	switch {
	case rate >= programDifficultyHardRate:
		return ProgramDifficultyHard
	case rate >= programDifficultyMediumRate:
		return ProgramDifficultyMedium
	}
	return ProgramDifficultyEasy
}

func intPtr(v int) *int {
	return &v
}

// ordinal names a position as 1st, 2nd, 3rd and so on
func ordinal(n int) string {
	suffix := "th"
	switch n % 100 {
	case 11, 12, 13:
	default:
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}
