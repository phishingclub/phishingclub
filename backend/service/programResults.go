package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// ProgramResultRow counts the phishing sends of one period or one template and
// how many of those sends were clicked, had data submitted or were reported.
// Rates are per send, so a month reads the same however long the program ran.
// For an anonymous program a row with fewer than programMinGroup people has its
// counts withheld and Suppressed set. In a row that is shown, an outcome count
// is null when it is 1 to programMinGroup - 1 or when it leaves fewer than
// programMinGroup out, counted in sends and in people.
type ProgramResultRow struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Sent      *int   `json:"sent"`
	Clicked   *int   `json:"clicked"`
	Submitted *int   `json:"submitted"`
	Reported  *int   `json:"reported"`
	// Open counts sends too young to judge, left out of the counts. It is null
	// on an anonymous program when it is 1 to programMinGroup - 1.
	Open *int `json:"open,omitempty"`
	// Difficulty labels a template from its click rate once sent often enough
	Difficulty string `json:"difficulty,omitempty"`
	Suppressed bool   `json:"suppressed"`
}

// ProgramResults is the program results breakdown
type ProgramResults struct {
	Anonymous bool `json:"anonymous"`
	// Closed is set for a closed program. Its Coverage is null, as nobody is
	// active in a closed program.
	Closed    bool                `json:"closed"`
	Months    []*ProgramResultRow `json:"months"`
	Templates []*ProgramResultRow `json:"templates"`
	// Last30 and Previous30 are the sends of the last 30 days and the 30 before.
	// Sends younger than three days are counted as open on Last30 instead. An
	// anonymous program counts no young sends at all, so it has no open count.
	Last30         *ProgramResultRow      `json:"last30"`
	Previous30     *ProgramResultRow      `json:"previous30"`
	LastSentAt     *time.Time             `json:"lastSentAt"`
	Coverage       *ProgramCoverage       `json:"coverage"`
	PhishProne     *ProgramPhishProne     `json:"phishProne"`
	RepeatClickers *ProgramRepeatClickers `json:"repeatClickers"`
	Exposure       []*ProgramExposureRow  `json:"exposure"`
	Resilience     *ProgramResilience     `json:"resilience"`
}

// resultCounts adds up sends and the people behind them before they become a
// result row
type resultCounts struct {
	sent, opened, clicked, submitted, reported programTally
}

func (c *resultCounts) add(s *programSend) {
	c.sent.add(s.Person)
	if s.ReadAt != nil {
		c.opened.add(s.Person)
	}
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

// outcome returns the send count of an outcome, nil when an anonymous program
// withholds it
func (c *resultCounts) outcome(t *programTally, anonymous bool) *int {
	if anonymous && t.withheldIn(&c.sent) {
		return nil
	}
	return intPtr(t.sends)
}

// row turns the counts into a result row, withholding small groups and small
// or nearly total outcomes of an anonymous program
func (c *resultCounts) row(key, name string, anonymous bool) *ProgramResultRow {
	r := &ProgramResultRow{Key: key, Name: name}
	if c.tooFew(anonymous) {
		r.Suppressed = true
		return r
	}
	r.Sent = intPtr(c.sent.sends)
	r.Clicked = c.outcome(&c.clicked, anonymous)
	r.Submitted = c.outcome(&c.submitted, anonymous)
	r.Reported = c.outcome(&c.reported, anonymous)
	return r
}

// programFacts is everything sent by a program with the outcomes
type programFacts struct {
	anonymous bool
	sends     []*programSend
	// templateNames maps a template id to its name
	templateNames map[string]string
}

// phishing returns the phishing sends
func (f *programFacts) phishing() []*programSend {
	out := make([]*programSend, 0, len(f.sends))
	for _, s := range f.sends {
		if !s.Training {
			out = append(out, s)
		}
	}
	return out
}

// trainingDone returns when each person completed a training
func (f *programFacts) trainingDone() map[uuid.UUID][]time.Time {
	out := map[uuid.UUID][]time.Time{}
	for _, s := range f.sends {
		if s.Training && s.TrainingCompletedAt != nil {
			out[s.Person] = append(out[s.Person], *s.TrainingCompletedAt)
		}
	}
	return out
}

// risks returns the risk of every person with phishing sends
func (f *programFacts) risks(now time.Time) map[uuid.UUID]*float64 {
	done := f.trainingDone()
	out := map[uuid.UUID]*float64{}
	for person, list := range programByPerson(f.sends) {
		out[person] = programPersonRisk(list, done[person], now)
	}
	return out
}

// programPerson names the person of a row. An anonymous program names people
// by pseudonym, as its rows may lose the recipient link, any other program by
// recipient id.
func programPerson(anonymous bool, recipientID, anonymizedID *uuid.UUID) (uuid.UUID, bool) {
	first, second := recipientID, anonymizedID
	if anonymous {
		first, second = anonymizedID, recipientID
	}
	if first != nil {
		return *first, true
	}
	if second != nil {
		return *second, true
	}
	return uuid.UUID{}, false
}

// programEventIDs returns the ids of the events the results read
func programEventIDs() []*uuid.UUID {
	return []*uuid.UUID{
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED],
	}
}

func eventIs(id uuid.UUID, name string) bool {
	e := cache.EventIDByName[name]
	return e != nil && *e == id
}

// earliest keeps the first time an outcome happened
func earliest(cur **time.Time, at time.Time) {
	if *cur == nil || at.Before(**cur) {
		t := at
		*cur = &t
	}
}

// loadProgramFacts loads every sent message of a program with its outcomes.
// When recipientID is set only that recipient's messages are loaded.
func (s *Program) loadProgramFacts(
	ctx context.Context,
	session *model.Session,
	program *model.Program,
	recipientIDs []*uuid.UUID,
) (*programFacts, error) {
	programID := program.ID.MustGet()
	anonymous := program.IsAnonymousOn()
	infos, err := s.ProgramRepository.GetCampaignInfos(ctx, &programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	infoOf := map[uuid.UUID]repository.ProgramCampaignInfo{}
	for _, i := range infos {
		infoOf[i.ID] = i
	}
	rows, err := s.ProgramRepository.GetSentRows(ctx, &programID, recipientIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	events, err := s.ProgramRepository.GetEventRows(ctx, &programID, recipientIDs, programEventIDs())
	if err != nil {
		return nil, errs.Wrap(err)
	}
	facts := &programFacts{anonymous: anonymous, templateNames: map[string]string{}}
	type rowKey struct {
		campaign uuid.UUID
		id       uuid.UUID
	}
	// a send is found by its recipient id and by its pseudonym, so an event
	// matches whichever of the two it carries
	index := map[rowKey]*programSend{}
	for _, r := range rows {
		person, ok := programPerson(anonymous, r.RecipientID, r.AnonymizedID)
		if !ok {
			continue
		}
		info, ok := infoOf[r.CampaignID]
		if !ok {
			continue
		}
		templateID := ""
		if info.CampaignTemplateID != nil {
			templateID = info.CampaignTemplateID.String()
		}
		sd := &programSend{
			Campaign:    r.CampaignID,
			Person:      person,
			RecipientID: r.RecipientID,
			Template:    templateID,
			SentAt:      r.SentAt,
			Training:    info.IsTraining,
		}
		facts.sends = append(facts.sends, sd)
		if r.RecipientID != nil {
			index[rowKey{r.CampaignID, *r.RecipientID}] = sd
		}
		if r.AnonymizedID != nil {
			index[rowKey{r.CampaignID, *r.AnonymizedID}] = sd
		}
	}
	for _, e := range events {
		var sd *programSend
		if e.RecipientID != nil {
			sd = index[rowKey{e.CampaignID, *e.RecipientID}]
		}
		if sd == nil && e.AnonymizedID != nil {
			sd = index[rowKey{e.CampaignID, *e.AnonymizedID}]
		}
		if sd == nil {
			continue
		}
		at := e.CreatedAt
		if sd.Training {
			// opening a training page is not a click on a phish
			switch {
			case eventIs(e.EventID, data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ):
				earliest(&sd.ReadAt, at)
			case eventIs(e.EventID, data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED):
				earliest(&sd.TrainingStartedAt, at)
			case eventIs(e.EventID, data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED):
				earliest(&sd.TrainingCompletedAt, at)
			}
			continue
		}
		switch {
		case eventIs(e.EventID, data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ):
			earliest(&sd.ReadAt, at)
		case eventIs(e.EventID, data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED),
			eventIs(e.EventID, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED),
			eventIs(e.EventID, data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED):
			earliest(&sd.ClickedAt, at)
		case eventIs(e.EventID, data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA):
			earliest(&sd.SubmittedAt, at)
		case eventIs(e.EventID, data.EVENT_CAMPAIGN_RECIPIENT_REPORTED):
			earliest(&sd.ReportedAt, at)
		}
	}
	for _, sd := range facts.sends {
		if _, ok := facts.templateNames[sd.Template]; ok {
			continue
		}
		facts.templateNames[sd.Template] = s.templateName(ctx, session, sd.Template)
	}
	return facts, nil
}

// programDeletedTemplateName names a template that no longer exists
const programDeletedTemplateName = "Deleted template"

// programLastPeriod is the length of the last period and of the one before it
// in the period comparison
const programLastPeriod = 30 * 24 * time.Hour

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

// templateName returns the name of a template, or a placeholder when it is gone
func (s *Program) templateName(ctx context.Context, session *model.Session, templateID string) string {
	name := programDeletedTemplateName
	if templateID == "" {
		return name
	}
	tid, err := uuid.Parse(templateID)
	if err != nil {
		return name
	}
	t, err := s.CampaignTemplateService.GetByID(ctx, session, &tid, &repository.CampaignTemplateOption{})
	if err != nil {
		return name
	}
	if v, err := t.Name.Get(); err == nil {
		return v.String()
	}
	return name
}

// activeEnrollmentPeople returns the person key of every active enrollment, by
// pseudonym for an anonymous program. An anonymous person who has not been sent
// anything yet has no pseudonym and gets a key that matches no send.
func (s *Program) activeEnrollmentPeople(ctx context.Context, programID *uuid.UUID, anonymous bool) ([]uuid.UUID, error) {
	enrollments, err := s.ProgramRepository.GetEnrollmentsByProgramID(ctx, programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	out := []uuid.UUID{}
	for _, e := range enrollments {
		if e.Status != model.ProgramEnrollmentActive {
			continue
		}
		var person uuid.UUID
		switch {
		case anonymous && e.AnonymizedID != nil:
			person = *e.AnonymizedID
		case !anonymous && e.RecipientID != nil:
			person = *e.RecipientID
		default:
			person = uuid.New()
		}
		out = append(out, person)
	}
	return out, nil
}

// programSettleCutoff returns the time before which an anonymous program counts
// sends and outcomes: the start of today in loc, less programOpenAge in whole
// days. The counts then change at most once a day and never right after a send
// or an action, so reading them again and again can not tell when one person
// acted.
func programSettleCutoff(now time.Time, loc *time.Location) time.Time {
	return startOfDay(now, loc).AddDate(0, 0, -int(programOpenAge/(24*time.Hour)))
}

// beforeCutoff returns t when it is before cutoff, nil otherwise
func beforeCutoff(t *time.Time, cutoff time.Time) *time.Time {
	if t == nil || !t.Before(cutoff) {
		return nil
	}
	return t
}

// settled returns the facts an anonymous program may count: the sends sent
// before cutoff, each with only the outcomes that happened before cutoff
func (f *programFacts) settled(cutoff time.Time) *programFacts {
	out := &programFacts{anonymous: f.anonymous, templateNames: f.templateNames}
	for _, sd := range f.sends {
		if !sd.SentAt.Before(cutoff) {
			continue
		}
		c := *sd
		c.ReadAt = beforeCutoff(sd.ReadAt, cutoff)
		c.ClickedAt = beforeCutoff(sd.ClickedAt, cutoff)
		c.SubmittedAt = beforeCutoff(sd.SubmittedAt, cutoff)
		c.ReportedAt = beforeCutoff(sd.ReportedAt, cutoff)
		c.TrainingStartedAt = beforeCutoff(sd.TrainingStartedAt, cutoff)
		c.TrainingCompletedAt = beforeCutoff(sd.TrainingCompletedAt, cutoff)
		out.sends = append(out.sends, &c)
	}
	return out
}

// countable returns the facts that may be counted at now: every fact of a
// named program, only the settled ones of an anonymous program
func (f *programFacts) countable(now time.Time, loc *time.Location) *programFacts {
	if !f.anonymous {
		return f
	}
	return f.settled(programSettleCutoff(now, loc))
}

// startOfDay returns the first instant of the calendar day of t in loc
func startOfDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// buildProgramResults computes the results from the facts. Only phishing sends
// in [from, to) count when a bound is set. Month names use loc. An anonymous
// program counts only settled sends and outcomes, see programSettleCutoff, and
// reads now as the start of the day in loc, so the rolling windows move a whole
// day at a time and can not single out one send.
func buildProgramResults(
	facts *programFacts,
	activePeople []uuid.UUID,
	loc *time.Location,
	now time.Time,
	from, to *time.Time,
) *ProgramResults {
	anonymous := facts.anonymous
	facts = facts.countable(now, loc)
	if anonymous {
		now = startOfDay(now, loc)
	}
	sends := []*programSend{}
	for _, sd := range facts.phishing() {
		if !inPeriod(sd, from, to) {
			continue
		}
		sends = append(sends, sd)
	}
	months := map[string]*resultCounts{}
	monthNames := map[string]string{}
	templates := map[string]*resultCounts{}
	last30, previous30 := &resultCounts{}, &resultCounts{}
	open := &programTally{}
	out := &ProgramResults{Anonymous: anonymous, Months: []*ProgramResultRow{}, Templates: []*ProgramResultRow{}}
	for _, sd := range sends {
		month := sd.SentAt.In(loc).Format("2006-01")
		if months[month] == nil {
			months[month] = &resultCounts{}
			monthNames[month] = sd.SentAt.In(loc).Format("Jan 2006")
		}
		months[month].add(sd)
		if templates[sd.Template] == nil {
			templates[sd.Template] = &resultCounts{}
		}
		templates[sd.Template].add(sd)
		age := now.Sub(sd.SentAt)
		switch {
		case age < programOpenAge:
			open.add(sd.Person)
		case age < programLastPeriod:
			last30.add(sd)
		case age < 2*programLastPeriod:
			previous30.add(sd)
		}
		if out.LastSentAt == nil || sd.SentAt.After(*out.LastSentAt) {
			t := sd.SentAt
			out.LastSentAt = &t
		}
	}
	for key, c := range months {
		out.Months = append(out.Months, c.row(key, monthNames[key], anonymous))
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
		r := c.row(key, name, anonymous)
		// the label tells the click rate band, so it goes with the clicks
		if !r.Suppressed && r.Clicked != nil {
			r.Difficulty = programDifficulty(c.sent.sends, c.clicked.sends)
		}
		trows = append(trows, templateRow{r, c.sent.sends})
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
	out.Last30 = last30.row("last30", "Last 30 days", anonymous)
	if !anonymous {
		out.Last30.Open = intPtr(open.sends)
	}
	out.Previous30 = previous30.row("previous30", "Previous 30 days", anonymous)
	// an anonymous program shows the day of the last send, not the time
	if anonymous && out.LastSentAt != nil {
		day := startOfDay(*out.LastSentAt, loc)
		out.LastSentAt = &day
	}

	byPerson := programByPerson(sends)
	out.Coverage = programCoverage(activePeople, programByPerson(facts.phishing()), now)
	out.PhishProne = programPhishProne(byPerson, now, anonymous)
	out.RepeatClickers = programRepeatClickers(byPerson, now, anonymous)
	out.Exposure = programExposure(byPerson, now, anonymous)
	out.Resilience = programResilience(sends, anonymous)
	return out
}

// authorizedProgram checks the session and loads a program for a read. The
// audit event is logged when the session is not authorized.
func (s *Program) authorizedProgram(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	ae *AuditEvent,
	option *repository.ProgramOption,
) (*model.Program, error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	if option == nil {
		option = &repository.ProgramOption{}
	}
	program, err := s.ProgramRepository.GetByID(ctx, id, option)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return program, nil
}

// GetResults returns the phishing results of a program by month, in the
// program time zone, and by template, with the people based measures.
func (s *Program) GetResults(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*ProgramResults, error) {
	ae := NewAuditEvent("Program.GetResults", session)
	program, err := s.authorizedProgram(ctx, session, id, ae, nil)
	if err != nil {
		return nil, err
	}
	return s.programResults(ctx, session, program, time.Now(), nil, nil)
}

func (s *Program) programResults(
	ctx context.Context,
	session *model.Session,
	program *model.Program,
	now time.Time,
	from, to *time.Time,
) (*ProgramResults, error) {
	facts, err := s.loadProgramFacts(ctx, session, program, nil)
	if err != nil {
		return nil, err
	}
	return s.resultsFromFacts(ctx, program, facts, now, from, to)
}

// resultsFromFacts computes the results of loaded facts
func (s *Program) resultsFromFacts(
	ctx context.Context,
	program *model.Program,
	facts *programFacts,
	now time.Time,
	from, to *time.Time,
) (*ProgramResults, error) {
	programID := program.ID.MustGet()
	active, err := s.activeEnrollmentPeople(ctx, &programID, facts.anonymous)
	if err != nil {
		return nil, err
	}
	results := buildProgramResults(facts, active, program.Location(), now, from, to)
	// nobody is active in a closed program, so coverage is not shown for it
	if program.IsClosed() {
		results.Closed = true
		results.Coverage = nil
	}
	return results, nil
}
