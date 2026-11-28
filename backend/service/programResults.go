package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// results tuning for programs
const (
	// programOpenAge is how long an anonymous program waits before it counts a
	// send or an outcome, see programSettleCutoff
	programOpenAge = model.ProgramAnonymousSettleDays * 24 * time.Hour
	// programMinGroup is the smallest group an anonymous program shows counts for
	programMinGroup = 5
	// programRepeatOffenderMonthsDefault is the repeat offender window used when
	// the repeat_offender_months option can not be read
	programRepeatOffenderMonthsDefault = 12
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
	ClickedAt   *time.Time
	SubmittedAt *time.Time
	ReportedAt  *time.Time
	// TrainingStartedAt and TrainingCompletedAt are set on training sends
	TrainingStartedAt   *time.Time
	TrainingCompletedAt *time.Time
	// NotableEventID is the most notable event of the send, as on a campaign
	NotableEventID *uuid.UUID
	// Language is the language stamp of the send, empty when it has none. An
	// anonymous program does not keep it, so no language is tied to a person.
	Language string
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

// withholdCount reports whether an anonymous program must hide count out of
// group. A count below programMinGroup is hidden, zero too, as a zero tells
// what everyone did just as a full count does. So is a count that leaves fewer
// than programMinGroup of the group out, as that tells nearly everyone did it.
func withholdCount(group, count int) bool {
	return count < programMinGroup || group-count < programMinGroup
}

// withholdGap reports whether an anonymous program must hide two related
// counts, such as clicked and submitted, whose difference is fewer than
// programMinGroup people. Subtracting one from the other would otherwise name
// the few who did one and not the other.
func withholdGap(a, b int) bool {
	gap := a - b
	if gap < 0 {
		gap = -gap
	}
	return gap < programMinGroup
}

func intPtr(v int) *int {
	return &v
}

// ProgramResultRow counts the phishing sends of one month or one template and
// how many of those sends were clicked, had data submitted or were reported.
// When the program has training it also counts the training sends and how
// many of those were started or completed, a completed training counts as
// started. The phishing counts of a template that sent no phishing are null,
// as are the phishing counts of a month of a program without phishing, and
// the training counts are null on a template that sent no training and on
// every row of a program without training.
// An anonymous program shows the size of a row only, the sends, and every
// outcome is null with OutcomesHidden set: a month and a template row share
// sends, so outcomes shown per row could be subtracted from each other or from
// the summary to learn what a hidden row holds. The outcomes are in the
// summary only. A row with fewer than programMinGroup people also has its
// sends withheld and Suppressed set.
type ProgramResultRow struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Sent      *int   `json:"sent"`
	Clicked   *int   `json:"clicked"`
	Submitted *int   `json:"submitted"`
	Reported  *int   `json:"reported"`
	// TrainingSent, TrainingStarted and TrainingCompleted count the training
	// sends of the row. An anonymous program holds them null, the training
	// outcomes are in the summary only.
	TrainingSent      *int `json:"trainingSent"`
	TrainingStarted   *int `json:"trainingStarted"`
	TrainingCompleted *int `json:"trainingCompleted"`
	// Partial is set on a month the program or the period covers only in part
	Partial    bool `json:"partial,omitempty"`
	Suppressed bool `json:"suppressed"`
	// OutcomesHidden is set on every row of an anonymous program
	OutcomesHidden bool `json:"outcomesHidden,omitempty"`
}

// ProgramSummary counts recipients, each once, over the whole program or the
// period of a report: the result tiles of the PDF report and the CSV export.
// The program page tiles are ProgramTiles. Sent, Clicked link,
// Data submitted, Reported and Repeat offenders read phishing sends, the
// training counts read training sends. An anonymous program counts sends
// instead of recipients, in whole settled months, and never shows repeat
// offenders, see programResultsAnonymous.go. A count an anonymous program
// withholds is null and Suppressed is set.
type ProgramSummary struct {
	// Recipients is everyone ever enrolled in the program
	Recipients    int  `json:"recipients"`
	Sent          *int `json:"sent"`
	ClickedLink   *int `json:"clickedLink"`
	SubmittedData *int `json:"submittedData"`
	Reported      *int `json:"reported"`
	// RepeatOffenders clicked or submitted data on 2 or more sends within
	// RepeatOffenderMonths, the repeat offender setting
	RepeatOffenders      *int `json:"repeatOffenders"`
	RepeatOffenderMonths int  `json:"repeatOffenderMonths"`
	// HasPhishing is set when the program has or had a phishing template
	HasPhishing bool `json:"hasPhishing"`
	// HasTraining is set when the program has or had a training template
	HasTraining bool `json:"hasTraining"`
	// TrainingSent counts the people sent a training template, the base of
	// Training started
	TrainingSent      *int `json:"trainingSent"`
	TrainingStarted   *int `json:"trainingStarted"`
	TrainingCompleted *int `json:"trainingCompleted"`
	// SentTotal counts every template sent, phishing and training. An
	// anonymous program gives 0 with SentTotalHidden set when it withholds Sent.
	SentTotal       int  `json:"sentTotal"`
	SentTotalHidden bool `json:"sentTotalHidden"`
	// HasSent is set when any template was sent in the view, also when the
	// counts are withheld
	HasSent    bool `json:"hasSent"`
	Suppressed bool `json:"suppressed"`
}

// ProgramResults is the program results breakdown
type ProgramResults struct {
	Anonymous bool `json:"anonymous"`
	// Closed is set for a closed program
	Closed     bool                `json:"closed"`
	Summary    *ProgramSummary     `json:"summary"`
	Months     []*ProgramResultRow `json:"months"`
	Templates  []*ProgramResultRow `json:"templates"`
	LastSentAt *time.Time          `json:"lastSentAt"`
	// Tiles is set on the results of the program page
	Tiles *ProgramTiles `json:"tiles,omitempty"`
	// CountedTo is set on an anonymous program: the first instant not
	// counted, the end of the last whole settled month in the period
	CountedTo *time.Time `json:"countedTo,omitempty"`
	// HasLanguageGroupData is true when the results grouped by language have
	// more than one row, also after the program stops picking languages. For
	// a program with sizes only it counts the rows that grouping gives.
	HasLanguageGroupData bool `json:"hasLanguageGroupData"`
}

// programFacts is everything sent by a program with the outcomes
type programFacts struct {
	anonymous bool
	sends     []*programSend
	// templateNames maps a template id to its name
	templateNames map[string]string
}

// programPerson names the person of a row, by pseudonym first when
// byPseudonym is set, else by recipient id first. An anonymous program names
// people by pseudonym, as its rows may lose the recipient link.
func programPerson(byPseudonym bool, recipientID, anonymizedID *uuid.UUID) (uuid.UUID, bool) {
	first, second := recipientID, anonymizedID
	if byPseudonym {
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

// programPersonByPseudonym reports whether the results of a named program
// name people by pseudonym first. Anonymize All gives every send of the
// program its pseudonym before it marks the program and before any send
// loses its person, so from the mark on every send has one, and the sends
// that lost their person are counted with the rest.
func programPersonByPseudonym(program *model.Program) bool {
	return program.AnonymizeBegun()
}

// earliest keeps the first time an outcome happened
func earliest(cur **time.Time, at time.Time) {
	if *cur == nil || at.Before(**cur) {
		t := at
		*cur = &t
	}
}

// loadProgramFacts loads every sent message of a program with its outcomes.
// When recipientIDs is not nil only those recipients' messages are loaded.
// The rows are read one at a time, so only the sends are held in memory.
func (s *Program) loadProgramFacts(
	ctx context.Context,
	program *model.Program,
	recipientIDs []*uuid.UUID,
) (*programFacts, error) {
	programID := program.ID.MustGet()
	anonymous := program.IsAnonymousOn()
	infos, err := s.ProgramRepository.GetCampaignInfos(ctx, &programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// a campaign is named by its place in infos, so the send index key is small
	infoOf := make(map[uuid.UUID]int32, len(infos))
	templateOf := make([]string, len(infos))
	templateIDs := make([]*uuid.UUID, 0, len(infos))
	for i, info := range infos {
		infoOf[info.ID] = int32(i)
		if info.CampaignTemplateID != nil {
			templateOf[i] = info.CampaignTemplateID.String()
			templateIDs = append(templateIDs, info.CampaignTemplateID)
		}
	}
	facts := &programFacts{anonymous: anonymous, templateNames: map[string]string{}}
	type rowKey struct {
		campaign int32
		id       uuid.UUID
	}
	// a send is found by its recipient id and by its pseudonym, so an event
	// matches whichever of the two it carries
	index := map[rowKey]*programSend{}
	err = s.ProgramRepository.EachSentRow(ctx, &programID, recipientIDs, func(r *repository.ProgramSendRow) error {
		person, ok := programPerson(anonymous, r.RecipientID, r.AnonymizedID)
		if !ok {
			return nil
		}
		ci, ok := infoOf[r.CampaignID]
		if !ok {
			return nil
		}
		info := infos[ci]
		sd := &programSend{
			Campaign:    r.CampaignID,
			Person:      person,
			RecipientID: r.RecipientID,
			Template:    templateOf[ci],
			SentAt:      r.SentAt,
			Training:    info.IsTraining,
			// the notable event names the person's own outcome, so an
			// anonymous program does not keep it
			NotableEventID: notableOf(anonymous, r.NotableEventID),
		}
		if !anonymous {
			sd.Language = r.Language
		}
		facts.sends = append(facts.sends, sd)
		if r.RecipientID != nil {
			index[rowKey{ci, *r.RecipientID}] = sd
		}
		if r.AnonymizedID != nil {
			index[rowKey{ci, *r.AnonymizedID}] = sd
		}
		return nil
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	ids := programResultEventIDs()
	err = s.ProgramRepository.EachEventRow(ctx, &programID, recipientIDs, ids.all(), func(e *repository.ProgramEventRow) error {
		ci, ok := infoOf[e.CampaignID]
		if !ok {
			return nil
		}
		var sd *programSend
		if e.RecipientID != nil {
			sd = index[rowKey{ci, *e.RecipientID}]
		}
		if sd == nil && e.AnonymizedID != nil {
			sd = index[rowKey{ci, *e.AnonymizedID}]
		}
		if sd == nil {
			return nil
		}
		ids.apply(sd, e.EventID, e.CreatedAt)
		return nil
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	names, err := s.programTemplateNames(ctx, templateIDs)
	if err != nil {
		return nil, err
	}
	for _, sd := range facts.sends {
		if _, ok := facts.templateNames[sd.Template]; ok {
			continue
		}
		name := programDeletedTemplateName
		if tid, err := uuid.Parse(sd.Template); err == nil {
			if n, ok := names[tid]; ok {
				name = n
			}
		}
		facts.templateNames[sd.Template] = name
	}
	return facts, nil
}

// programResultEvents holds the ids of the events the results read, looked up
// once per load instead of once per event
type programResultEvents struct {
	beforeVisit, visit, afterVisit, submitted, reported, trainingStarted, trainingCompleted *uuid.UUID
}

func programResultEventIDs() programResultEvents {
	return programResultEvents{
		beforeVisit:       cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
		visit:             cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
		afterVisit:        cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
		submitted:         cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
		reported:          cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED],
		trainingStarted:   cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED],
		trainingCompleted: cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED],
	}
}

// all returns the ids of the events the results read
func (p programResultEvents) all() []*uuid.UUID {
	return []*uuid.UUID{
		p.beforeVisit, p.visit, p.afterVisit, p.submitted, p.reported, p.trainingStarted, p.trainingCompleted,
	}
}

// apply records an event on its send, keeping the first time of each outcome
func (p programResultEvents) apply(sd *programSend, eventID uuid.UUID, at time.Time) {
	is := func(id *uuid.UUID) bool { return id != nil && *id == eventID }
	if sd.Training {
		// opening a training page is not a click on a phish
		switch {
		case is(p.trainingStarted):
			earliest(&sd.TrainingStartedAt, at)
		case is(p.trainingCompleted):
			earliest(&sd.TrainingCompletedAt, at)
		}
		return
	}
	switch {
	case is(p.beforeVisit), is(p.visit), is(p.afterVisit):
		earliest(&sd.ClickedAt, at)
	case is(p.submitted):
		earliest(&sd.SubmittedAt, at)
	case is(p.reported):
		earliest(&sd.ReportedAt, at)
	}
}

// programTemplateNames reads the names of templates in one query. A template
// that no longer exists is left out, so the caller names it as deleted.
func (s *Program) programTemplateNames(ctx context.Context, ids []*uuid.UUID) (map[uuid.UUID]string, error) {
	seen := map[uuid.UUID]bool{}
	unique := make([]*uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == nil || seen[*id] {
			continue
		}
		seen[*id] = true
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return map[uuid.UUID]string{}, nil
	}
	names, err := s.ProgramRepository.GetTemplateNames(ctx, unique)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return names, nil
}

// programDeletedTemplateName names a template that no longer exists
const programDeletedTemplateName = "Deleted template"

// notableOf returns the notable event of a send row, none for an anonymous
// program
func notableOf(anonymous bool, id *uuid.UUID) *uuid.UUID {
	if anonymous {
		return nil
	}
	return id
}

// programEnrollmentCounts counts the people of a program by enrollment status
type programEnrollmentCounts struct {
	total, active, completed, exited, stopped int
}

// enrollmentCounts counts the people ever enrolled in a program by status
func (s *Program) enrollmentCounts(ctx context.Context, programID *uuid.UUID) (programEnrollmentCounts, error) {
	counts, err := s.ProgramRepository.GetEnrollmentStatusCounts(ctx, programID)
	if err != nil {
		return programEnrollmentCounts{}, errs.Wrap(err)
	}
	out := programEnrollmentCounts{}
	for _, c := range counts {
		out.total += c.Count
		switch c.Status {
		case model.ProgramEnrollmentActive:
			out.active += c.Count
		case model.ProgramEnrollmentCompleted:
			out.completed += c.Count
		case model.ProgramEnrollmentExited:
			out.exited += c.Count
		case model.ProgramEnrollmentStopped:
			out.stopped += c.Count
		}
	}
	return out, nil
}

// repeatOffenderMonths reads the repeat offender window the way the recipient
// pages do, the default when the option can not be read
func (s *Program) repeatOffenderMonths(ctx context.Context) int {
	if s.RecipientRepository == nil || s.RecipientRepository.OptionRepository == nil {
		return programRepeatOffenderMonthsDefault
	}
	months, err := s.RecipientRepository.RepeatOffenderMonths(ctx)
	if err != nil || months <= 0 {
		if err != nil {
			s.Logger.Warnw("failed to read the repeat offender months, using the default", "error", err)
		}
		return programRepeatOffenderMonthsDefault
	}
	return months
}

// programStepKinds tells which kinds of template the steps of a program send
type programStepKinds struct {
	training bool
	phishing bool
}

// stepKinds reports whether a step of the program sends a training template
// and whether a step sends a phishing template
// template, read for every step in one query
func (s *Program) stepKinds(ctx context.Context, program *model.Program) (programStepKinds, error) {
	kinds := programStepKinds{}
	infos, err := s.stepTemplateInfos(ctx, program)
	if err != nil {
		return kinds, err
	}
	for _, info := range infos {
		if info.IsTraining {
			kinds.training = true
		} else {
			kinds.phishing = true
		}
	}
	return kinds, nil
}

// stepTemplateInfos reads the templates of the steps of a program in one
// query. A template that no longer exists is left out.
func (s *Program) stepTemplateInfos(ctx context.Context, program *model.Program) (map[uuid.UUID]repository.ProgramTemplateInfo, error) {
	ids := make([]*uuid.UUID, 0, len(program.Steps))
	for _, st := range program.Steps {
		if templateID, err := st.CampaignTemplateID.Get(); err == nil {
			ids = append(ids, &templateID)
		}
	}
	infos, err := s.ProgramRepository.GetTemplateInfos(ctx, ids)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return infos, nil
}

// programSettleCutoff returns the time before which an anonymous program counts
// sends and outcomes: the start of today in loc, less programOpenAge in whole
// days. The counts then change at most once a day and never right after a send
// or an action, so reading them again and again can not tell when one person
// acted.
func programSettleCutoff(now time.Time, loc *time.Location) time.Time {
	return model.ProgramAnonymousSettleCutoff(now, loc)
}

// startOfDay returns the first instant of the calendar day of t in loc
func startOfDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// programResultsInput is what the results are computed with besides the facts
type programResultsInput struct {
	// loc names the months
	loc *time.Location
	now time.Time
	// from and to bound the period [from, to), nil is open
	from, to *time.Time
	// recipients is how many people were ever enrolled
	recipients int
	// enrollment counts the people by enrollment status now
	enrollment programEnrollmentCounts
	// hasTraining is set when the program has or had a training template
	hasTraining bool
	// hasPhishing is set when the program has or had a phishing template
	hasPhishing bool
	// repeatMonths is the repeat offender window in months
	repeatMonths int
	// startedAt and closedAt bound the time the program ran, to tell partial
	// months
	startedAt, closedAt *time.Time
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
	ae.Details["id"] = id.String()
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
	program.SetReadState(time.Now())
	return program, nil
}

// GetResults returns the program results: the result tiles for a time range,
// see ProgramTiles, and the phishing results by month, in the program time
// zone, and by template since the start. An empty time range is the default
// of the program, any other must be one of the time range keys.
func (s *Program) GetResults(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	timeRange string,
) (*ProgramResults, error) {
	if timeRange != "" && !IsValidProgramTimeRange(timeRange) {
		return nil, errs.NewValidationError(errors.New("unknown time range"))
	}
	ae := NewAuditEvent("Program.GetResults", session)
	program, err := s.authorizedProgram(ctx, session, id, ae, &repository.ProgramOption{WithSteps: true})
	if err != nil {
		return nil, err
	}
	if timeRange == "" {
		timeRange = defaultProgramTimeRange(program.IsAnonymousOn())
	}
	now := time.Now()
	tiles := &programTilesPeriod{key: timeRange, from: programTimeRangeFrom(timeRange, now, program.Location())}
	return s.cachedProgramResults(ctx, program, now, nil, nil, tiles)
}

// programResults returns the results of a program at now for the period
// [from, to), nil bounds are open. A result is cached until something it is
// computed from changes, see programResultsStamp. An anonymous program counts
// only settled months, so its result holds until the day moves. A named program
// is computed again after a new event or a new send on its campaigns, and
// always after programResultsNamedMaxAge. When computing it was slow it is
// kept at least programResultsNamedMinAge.
func (s *Program) programResults(
	ctx context.Context,
	_ *model.Session,
	program *model.Program,
	now time.Time,
	from, to *time.Time,
) (*ProgramResults, error) {
	return s.cachedProgramResults(ctx, program, now, from, to, nil)
}

// cachedProgramResults returns the results of a program at now for the period
// [from, to), with the result tiles of the program page when tiles is set. The
// results are computed in memory from the facts of the program, see
// programFactsFor, which are read once and then kept up to date.
func (s *Program) cachedProgramResults(
	ctx context.Context,
	program *model.Program,
	now time.Time,
	from, to *time.Time,
	tiles *programTilesPeriod,
) (*ProgramResults, error) {
	programID := program.ID.MustGet()
	in, kinds, err := s.programResultsInputs(ctx, program, now, from, to)
	if err != nil {
		return nil, err
	}
	stamp := programResultsStamp{
		closed:       program.IsClosed(),
		anonymous:    program.IsAnonymousOn(),
		byPseudonym:  programPersonByPseudonym(program),
		timeZone:     in.loc.String(),
		recipients:   in.recipients,
		enrollment:   in.enrollment,
		repeatMonths: in.repeatMonths,
		stepTraining: kinds.training,
		stepPhishing: kinds.phishing,
	}
	if program.UpdatedAt != nil {
		stamp.updatedAt = program.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if in.startedAt != nil {
		stamp.updatedAt += "|" + in.startedAt.UTC().Format(time.RFC3339Nano)
	}
	if in.closedAt != nil {
		stamp.updatedAt += "|" + in.closedAt.UTC().Format(time.RFC3339Nano)
	}
	expires := now.Add(programResultsNamedMaxAge)
	if stamp.anonymous {
		day := startOfDay(now, in.loc)
		stamp.day = day.Format(time.RFC3339)
		expires = day.AddDate(0, 0, 1)
	}
	key := newProgramResultsKey(programID, from, to)
	if tiles != nil {
		key = key.withTiles(tiles)
	}
	fresh := func(e *programResultsEntry) (bool, error) {
		if stamp.anonymous {
			// an anonymous program counts settled months only, they do not
			// change during the day
			return true, nil
		}
		if e.took > programResultsSlowCompute && now.Sub(e.computedAt) < programResultsNamedMinAge {
			return true, nil
		}
		facts, err := s.programFactsFor(ctx, program, now)
		if err != nil {
			return false, err
		}
		return facts.load == e.factsLoad && facts.currentVersion() == e.factsVersion, nil
	}
	compute := func() (*programResultsEntry, error) {
		started := time.Now()
		facts, err := s.programFactsFor(ctx, program, now)
		if err != nil {
			return nil, err
		}
		facts.mu.RLock()
		keys := make([]string, len(facts.templates))
		copy(keys, facts.templates)
		facts.mu.RUnlock()
		templateLanguages, err := s.factTemplateLanguages(ctx, keys)
		if err != nil {
			return nil, err
		}
		sizesOnly := program.IsAnonymousOn() || programPersonByPseudonym(program)
		facts.mu.RLock()
		in.hasTraining = kinds.training || facts.hasTraining
		in.hasPhishing = kinds.phishing || facts.hasPhishing
		version := facts.version
		results := facts.programResults(in, tiles, program.IsClosed())
		results.HasLanguageGroupData = facts.languageGroupCount(templateLanguages, sizesOnly, in.now, in.loc) > 1
		facts.mu.RUnlock()
		return &programResultsEntry{
			results:      results,
			factsLoad:    facts.load,
			factsVersion: version,
			took:         time.Since(started),
			expires:      expires,
		}, nil
	}
	return s.resultsCache.get(key, stamp, now, programFactsBackground(ctx), fresh, compute)
}

// buildProgramResults computes the results of loaded facts for the period of
// the input
func buildProgramResults(facts *programFacts, in programResultsInput) *ProgramResults {
	return programFactsStoreOf(facts).programResults(in, nil, false)
}

// programResults computes the results for the period of the input, with the
// result tiles of a time range when tiles is set. The caller holds the read
// lock.
func (st *programFactsStore) programResults(in programResultsInput, tiles *programTilesPeriod, closed bool) *ProgramResults {
	var results *ProgramResults
	var anon *programAnonymousFacts
	if st.anonymous {
		anon = st.anonymousFacts(in.now, in.loc)
		results = st.anonymousResults(anon, in)
	} else {
		results = st.namedResults(in)
	}
	results.Anonymous = st.anonymous
	results.Closed = closed
	if tiles != nil {
		tin := in
		tin.hasTraining = results.Summary.HasTraining
		tin.hasPhishing = results.Summary.HasPhishing
		results.Tiles = st.programTiles(anon, tin, *tiles, closed, results.Summary)
	}
	return results
}

// programResultsInputs reads what the results are computed with besides the
// facts, with the kinds of template the steps of the program send.
func (s *Program) programResultsInputs(
	ctx context.Context,
	program *model.Program,
	now time.Time,
	from, to *time.Time,
) (programResultsInput, programStepKinds, error) {
	programID := program.ID.MustGet()
	enrollment, err := s.enrollmentCounts(ctx, &programID)
	if err != nil {
		return programResultsInput{}, programStepKinds{}, err
	}
	kinds, err := s.stepKinds(ctx, program)
	if err != nil {
		return programResultsInput{}, kinds, err
	}
	in := programResultsInput{
		loc:          program.Location(),
		now:          now,
		from:         from,
		to:           to,
		recipients:   enrollment.total,
		enrollment:   enrollment,
		repeatMonths: s.repeatOffenderMonths(ctx),
		startedAt:    program.StartedAt,
	}
	if t, err := program.ClosedAt.Get(); err == nil {
		in.closedAt = &t
	}
	return in, kinds, nil
}

// ForgetTemplateResults forgets the cached results of the programs that may
// show a campaign template, after it was renamed or deleted, so they show its
// new name. An anonymous program keeps its results for the day otherwise. The
// facts are kept and read the names again on their next use.
func (s *Program) ForgetTemplateResults(templateID *uuid.UUID) {
	if s == nil || templateID == nil {
		return
	}
	for _, id := range s.resultsCache.programsWithTemplate(*templateID) {
		s.resultsCache.invalidateResults(id)
	}
}

// ForgetTemplateFacts forgets the kept facts and the cached results of the
// programs that may show a campaign template. It is used after an update
// wrote a language on sends that had none, or changed the template language,
// as the kept facts hold the language of each send as it was read and the
// recheck does not compare languages.
func (s *Program) ForgetTemplateFacts(templateID *uuid.UUID) {
	if s == nil || templateID == nil {
		return
	}
	for _, id := range s.resultsCache.programsWithTemplate(*templateID) {
		s.resultsCache.invalidate(id)
	}
}

// invalidateResults forgets the cached results of a program, after a change
// the cache can not see. The facts are kept and checked against the database
// on their next use, see programResultsCache.invalidateResults.
func (s *Program) invalidateResults(programID *uuid.UUID) {
	if programID != nil {
		s.resultsCache.invalidateResults(*programID)
	}
}
