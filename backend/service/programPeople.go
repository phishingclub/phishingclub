package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// ProgramEnrollmentRow is one recipient's journey state. NotableEventName is
// the most notable event across the recipient's sends, named as on a campaign.
// LastTemplateName and LastSentAt name the newest sent template.
// NextTemplateName is set when the next template is known: a send waits in the
// queue, or everyone gets the templates in the same order. It is null when the
// template is picked at send time. NextDueAt is when the next send goes out.
// Language is the language code of the send waiting in the queue, else of the
// newest sent one: its stamp, or its template language when it has none. It is
// set only when the program has Language by, and never for an anonymous
// program, which gives no rows.
type ProgramEnrollmentRow struct {
	RecipientID      string     `json:"recipientID"`
	Email            string     `json:"email"`
	Name             string     `json:"name"`
	Position         int        `json:"position"`
	LoopCount        int        `json:"loopCount"`
	Status           string     `json:"status"`
	NotableEventName string     `json:"notableEventName"`
	Sends            int        `json:"sends"`
	LastTemplateName *string    `json:"lastTemplateName"`
	LastSentAt       *time.Time `json:"lastSentAt"`
	NextTemplateName *string    `json:"nextTemplateName"`
	NextDueAt        *time.Time `json:"nextDueAt"`
	Language         *string    `json:"language"`
}

// ProgramRecipientsView is the recipients endpoint response. For an anonymous
// program it carries only the status counts, never per recipient identity or
// behaviour, so the operator cannot tell who clicked. Count is how many rows
// match the search, Rows is one page of them.
type ProgramRecipientsView struct {
	Anonymous bool `json:"anonymous"`
	// Anonymized is set when the people of the program are replaced with
	// pseudonyms, the view then gives the counts only
	Anonymized bool `json:"anonymized"`
	StepCount  int  `json:"stepCount"`
	Total      int  `json:"total"`
	Active     int  `json:"active"`
	Exited     int  `json:"exited"`
	Completed  int  `json:"completed"`
	// Stopped counts the people who were still active when the program closed
	Stopped     int                    `json:"stopped"`
	Count       int64                  `json:"count"`
	HasNextPage bool                   `json:"hasNextPage"`
	Rows        []ProgramEnrollmentRow `json:"rows"`
}

// recipientDisplayName returns the email and full name of a recipient row
func recipientDisplayName(r *database.Recipient) (string, string) {
	if r == nil {
		return "", ""
	}
	email := ""
	if r.Email != nil {
		email = *r.Email
	}
	return email, fullName(r.FirstName, r.LastName)
}

// fullName joins a first and last name, either may be empty
func fullName(first, last string) string {
	return strings.TrimSpace(first + " " + last)
}

// programPageFactsMax is the largest page whose facts are loaded by recipient,
// the largest page a request may ask for
const programPageFactsMax = vo.MAX_PER_PAGE

// GetRecipientsView returns one page of the per recipient journey of a program,
// withholding identity and behaviour for an anonymous program.
func (s *Program) GetRecipientsView(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	q *vo.QueryArgs,
) (*ProgramRecipientsView, error) {
	ae := NewAuditEvent("Program.GetRecipients", session)
	program, err := s.authorizedProgram(ctx, session, id, ae, &repository.ProgramOption{
		WithSteps:           true,
		WithRecipientGroups: true,
	})
	if err != nil {
		return nil, err
	}
	counts, err := s.ProgramRepository.GetEnrollmentStatusCounts(ctx, id)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	anonymous := program.IsAnonymousOn()
	view := &ProgramRecipientsView{
		Anonymous: anonymous,
		StepCount: len(program.Steps),
		Rows:      []ProgramEnrollmentRow{},
	}
	for _, c := range counts {
		view.Total += c.Count
		switch c.Status {
		case model.ProgramEnrollmentActive:
			view.Active += c.Count
		case model.ProgramEnrollmentExited:
			view.Exited += c.Count
		case model.ProgramEnrollmentCompleted:
			view.Completed += c.Count
		case model.ProgramEnrollmentStopped:
			view.Stopped += c.Count
		}
	}
	if anonymous {
		// the people who had every template count as active while the
		// program runs and as stopped after it closed, as on the recipient
		// page, since when a person finished would point at the time of
		// their sends
		if program.IsClosed() {
			view.Stopped += view.Completed
		} else {
			view.Active += view.Completed
		}
		view.Completed = 0
		// no per recipient identity or behaviour for an anonymous program
		return view, nil
	}
	if program.AnonymizeBegun() {
		// the sends no longer name a person, so no row is shown per person
		view.Anonymized = true
		return view, nil
	}
	if q == nil {
		q = &vo.QueryArgs{}
	}
	if view.Total == 0 && programNotStarted(program) {
		return s.audienceView(ctx, program, view, q)
	}
	enrollments, count, err := s.ProgramRepository.GetEnrollmentPage(ctx, id, q)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	view.Count = count
	// a page needs only the facts of its own people, unless the page is too
	// large to name
	var pageIDs []*uuid.UUID
	if len(enrollments) <= programPageFactsMax {
		pageIDs = make([]*uuid.UUID, 0, len(enrollments))
		for _, e := range enrollments {
			pageIDs = append(pageIDs, e.RecipientID)
		}
	}
	facts, err := s.loadProgramFacts(ctx, program, pageIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// every template sent counts, and the most notable event of all of them
	// is the recipient's status
	sendCount := map[uuid.UUID]int{}
	notable := map[uuid.UUID]*uuid.UUID{}
	last := map[uuid.UUID]*programSend{}
	for _, sd := range facts.sends {
		sendCount[sd.Person]++
		if cur := last[sd.Person]; cur == nil || sd.SentAt.After(cur.SentAt) {
			last[sd.Person] = sd
		}
		cur := notable[sd.Person]
		if sd.NotableEventID != nil && (cur == nil || cache.IsMoreNotableCampaignRecipientEventID(cur, sd.NotableEventID)) {
			notable[sd.Person] = sd.NotableEventID
		}
	}
	// resolve the scheduled step template names once, to name each recipient's
	// next step
	steps := program.Steps
	// the deliveries of the page only, unless every match is loaded
	deliveries, err := s.ProgramRepository.GetDeliveries(ctx, id, pageIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	delivered := deliveriesByPersonLoop(deliveries)
	// a send already in the queue names the next template for sure
	queuedSends, err := s.ProgramRepository.GetQueuedSends(ctx, id, pageIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	queued := map[uuid.UUID]repository.ProgramQueuedSend{}
	for _, q := range queuedSends {
		cur, ok := queued[q.RecipientID]
		if !ok || (q.SendAt != nil && (cur.SendAt == nil || q.SendAt.Before(*cur.SendAt))) {
			queued[q.RecipientID] = q
		}
	}
	// the names of the step templates and the queued templates in one read
	templateIDs := make([]*uuid.UUID, 0, len(steps)+len(queued))
	for _, st := range steps {
		tid := st.CampaignTemplateID.MustGet()
		templateIDs = append(templateIDs, &tid)
	}
	for _, q := range queued {
		if q.TemplateID != nil {
			templateIDs = append(templateIDs, q.TemplateID)
		}
	}
	templateNames, err := s.ProgramRepository.GetTemplateNames(ctx, templateIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	nameOf := func(id uuid.UUID) string {
		if n, ok := templateNames[id]; ok {
			return n
		}
		return programDeletedTemplateName
	}
	stepNames := make(map[uuid.UUID]string, len(steps))
	for _, st := range steps {
		stepNames[st.ID.MustGet()] = nameOf(st.CampaignTemplateID.MustGet())
	}
	languageOf, err := s.peopleLanguages(ctx, program, last, queued)
	if err != nil {
		return nil, err
	}
	shuffle := program.OrderModeOrDefault() == model.ProgramOrderShuffle
	for _, e := range enrollments {
		email, name := recipientDisplayName(e.Recipient)
		var nextTemplate *string
		var nextDue *time.Time
		// the progress is the templates had in the current loop, read from the
		// sends, so it also holds for a person who completed
		position := e.Position
		given := map[uuid.UUID]bool{}
		if len(steps) > 0 {
			given = deliveredSteps(steps, delivered[programPersonLoop{*e.RecipientID, e.LoopCount}])
			position = len(given)
		}
		if e.Status == model.ProgramEnrollmentActive && len(steps) > 0 {
			left := undeliveredSteps(steps, given)
			if len(left) == 0 {
				// the next send starts a new loop
				left = steps
			}
			// in random order the template is picked when the send is planned,
			// unless only one is left to pick
			var next *model.ProgramStep
			switch {
			case !shuffle:
				next = programPickStep(program, left, nil)
			case len(left) == 1:
				next = left[0]
			}
			if next != nil {
				n := stepNames[next.ID.MustGet()]
				nextTemplate = &n
			}
			nextDue = e.NextDueAt
		}
		if q, ok := queued[*e.RecipientID]; ok {
			if q.TemplateID != nil {
				n := nameOf(*q.TemplateID)
				nextTemplate = &n
			}
			if q.SendAt != nil {
				nextDue = q.SendAt
			}
		}
		row := ProgramEnrollmentRow{
			RecipientID:      e.RecipientID.String(),
			Email:            email,
			Name:             name,
			Position:         position,
			LoopCount:        e.LoopCount,
			Status:           e.Status,
			Sends:            sendCount[*e.RecipientID],
			NextTemplateName: nextTemplate,
			NextDueAt:        nextDue,
		}
		if code, ok := languageOf[*e.RecipientID]; ok {
			row.Language = &code
		}
		if sd := last[*e.RecipientID]; sd != nil {
			n := facts.templateNames[sd.Template]
			at := sd.SentAt
			row.LastTemplateName = &n
			row.LastSentAt = &at
		}
		if id := notable[*e.RecipientID]; id != nil {
			row.NotableEventName = cache.EventNameByID[id.String()]
		}
		view.Rows = append(view.Rows, row)
	}
	view.HasNextPage = q.Limit > 0 && int64(q.Offset+len(view.Rows)) < count
	return view, nil
}

// peopleLanguages returns the language of each person's queued send, else of
// their newest sent one, keyed by recipient. A send with no stamp is in its
// template language, read for those templates only. A person whose language
// is not known is left out. It is empty for a program without Language by and
// for an anonymous program.
func (s *Program) peopleLanguages(
	ctx context.Context,
	program *model.Program,
	last map[uuid.UUID]*programSend,
	queued map[uuid.UUID]repository.ProgramQueuedSend,
) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if program.IsAnonymousOn() || program.LanguageRouteField() == "" {
		return out, nil
	}
	type pick struct {
		code     string
		template string
	}
	picks := make(map[uuid.UUID]pick, len(last)+len(queued))
	for person, sd := range last {
		picks[person] = pick{sd.Language, sd.Template}
	}
	for person, q := range queued {
		p := pick{}
		if q.Language != nil {
			p.code = *q.Language
		}
		if q.TemplateID != nil {
			p.template = q.TemplateID.String()
		}
		picks[person] = p
	}
	keys := []string{}
	seen := map[string]bool{}
	for _, p := range picks {
		if p.code == "" && p.template != "" && !seen[p.template] {
			seen[p.template] = true
			keys = append(keys, p.template)
		}
	}
	templateLanguages := map[string]string{}
	if len(keys) > 0 {
		var err error
		templateLanguages, err = s.factTemplateLanguages(ctx, keys)
		if err != nil {
			return nil, err
		}
	}
	for person, p := range picks {
		code := p.code
		if code == "" {
			code = templateLanguages[p.template]
		}
		if code != "" {
			out[person] = code
		}
	}
	return out, nil
}

// ProgramAudienceNotStarted is the row status of a person who is in the
// groups of a program that has not started, so is not enrolled yet
const ProgramAudienceNotStarted = "not_started"

// programNotStarted reports whether a program is a draft or scheduled and
// never ran, so its people are only the members of its groups
func programNotStarted(program *model.Program) bool {
	if program.StartedAt != nil {
		return false
	}
	status := program.StatusAt(time.Now())
	return status == model.ProgramStatusDraft || status == model.ProgramStatusScheduled
}

// audienceView fills the view with one page of the people a program that has
// not started would enroll, read by the same group rules the start uses. Each
// row is not started, with nothing sent and no template named yet.
func (s *Program) audienceView(
	ctx context.Context,
	program *model.Program,
	view *ProgramRecipientsView,
	q *vo.QueryArgs,
) (*ProgramRecipientsView, error) {
	groupIDs := []*uuid.UUID{}
	if program.RecipientGroupIDs.IsSpecified() && !program.RecipientGroupIDs.IsNull() {
		groupIDs = program.RecipientGroupIDs.MustGet()
	}
	recipients, count, err := s.ProgramRepository.GetAudiencePage(ctx, groupIDs, q)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	view.Count = count
	for _, r := range recipients {
		email, name := recipientDisplayName(r)
		view.Rows = append(view.Rows, ProgramEnrollmentRow{
			RecipientID: r.ID.String(),
			Email:       email,
			Name:        name,
			Status:      ProgramAudienceNotStarted,
		})
	}
	view.HasNextPage = q.Limit > 0 && int64(q.Offset+len(view.Rows)) < count
	return view, nil
}

// programPersonLoop is one person in one loop of a program
type programPersonLoop struct {
	recipient uuid.UUID
	loop      int
}

// deliveriesByPersonLoop groups deliveries by person and loop
func deliveriesByPersonLoop(deliveries []repository.ProgramDelivery) map[programPersonLoop][]repository.ProgramDelivery {
	out := map[programPersonLoop][]repository.ProgramDelivery{}
	for _, d := range deliveries {
		k := programPersonLoop{d.RecipientID, d.LoopIndex}
		out[k] = append(out[k], d)
	}
	return out
}

// ProgramRecipientTotals counts what one recipient did in a program. Sent,
// Clicked, Submitted and Reported count phishing sends, the training counts
// count training sends.
type ProgramRecipientTotals struct {
	Sent              int `json:"sent"`
	Clicked           int `json:"clicked"`
	Submitted         int `json:"submitted"`
	Reported          int `json:"reported"`
	TrainingSent      int `json:"trainingSent"`
	TrainingCompleted int `json:"trainingCompleted"`
}

// ProgramRecipientActivity is one program a recipient is enrolled in. For an
// anonymous program only the name and enrollment status are given, Totals is
// nil. The events are read through the program events with a recipient filter.
type ProgramRecipientActivity struct {
	ProgramID     string `json:"programID"`
	Name          string `json:"name"`
	Anonymous     bool   `json:"anonymous"`
	ProgramStatus string `json:"programStatus"`
	// ProgramClosingAt and ProgramDeletingAt are set while a close or a
	// delete of the program has not finished
	ProgramClosingAt  *time.Time              `json:"programClosingAt"`
	ProgramDeletingAt *time.Time              `json:"programDeletingAt"`
	EnrollmentStatus  string                  `json:"enrollmentStatus"`
	NextDueAt         *time.Time              `json:"nextDueAt"`
	Totals            *ProgramRecipientTotals `json:"totals"`
}

// program send kinds
const (
	ProgramSendKindPhishing = "phishing"
	ProgramSendKindTraining = "training"
)

// authorizeRecipientPrograms checks the session may read the programs of a
// recipient and loads the recipient company
func (s *Program) authorizeRecipientPrograms(
	ctx context.Context,
	session *model.Session,
	recipientID *uuid.UUID,
	action string,
) (*uuid.UUID, error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent(action, session)
		ae.Details["recipientId"] = recipientID.String()
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	recipient, err := s.RecipientRepository.GetByID(ctx, recipientID, &repository.RecipientOption{})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if cid, err := recipient.CompanyID.Get(); err == nil {
		return &cid, nil
	}
	return nil, nil
}

// GetRecipientPrograms returns one page of the programs a recipient is
// enrolled in with what the recipient did there. Only programs of the
// recipient's company, or global ones, are listed. The page is read in one
// query and the totals of every program on it in one more read each for
// campaigns, sends and events. An anonymous program gives only its name and
// status.
func (s *Program) GetRecipientPrograms(
	ctx context.Context,
	session *model.Session,
	recipientID *uuid.UUID,
	q *vo.QueryArgs,
) (*model.Result[ProgramRecipientActivity], error) {
	recipientCompany, err := s.authorizeRecipientPrograms(ctx, session, recipientID, "Program.GetRecipientPrograms")
	if err != nil {
		return nil, err
	}
	rows, hasNextPage, err := s.ProgramRepository.GetRecipientProgramPage(ctx, recipientID, recipientCompany, q)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	now := time.Now()
	result := model.NewEmptyResult[ProgramRecipientActivity]()
	result.HasNextPage = hasNextPage
	// the programs whose totals may be shown, never an anonymous one
	named := []*uuid.UUID{}
	for _, row := range rows {
		program := row.Program
		program.SetReadState(now)
		programID := program.ID.MustGet()
		anonymous := program.IsAnonymousOn()
		activity := &ProgramRecipientActivity{
			ProgramID:         programID.String(),
			Name:              programName(program),
			Anonymous:         anonymous,
			ProgramStatus:     program.Status,
			ProgramClosingAt:  program.ClosingAt,
			ProgramDeletingAt: program.DeletingAt,
			EnrollmentStatus:  row.Enrollment.Status,
		}
		if anonymous {
			activity.EnrollmentStatus = anonymousEnrollmentStatus(row.Enrollment.Status, program.IsClosed())
		} else {
			if row.Enrollment.Status == model.ProgramEnrollmentActive {
				activity.NextDueAt = row.Enrollment.NextDueAt
			}
			activity.Totals = &ProgramRecipientTotals{}
			named = append(named, &programID)
		}
		result.Rows = append(result.Rows, activity)
	}
	totals, err := s.recipientProgramTotals(ctx, recipientID, named)
	if err != nil {
		return nil, err
	}
	for _, activity := range result.Rows {
		if t, ok := totals[activity.ProgramID]; ok && activity.Totals != nil {
			activity.Totals = t
		}
	}
	return result, nil
}

// anonymousEnrollmentStatus is the status of a person in an anonymous program
// as the recipient page shows it: enrolled, left or closed. A person who has
// had every template is shown as enrolled while the program runs and as
// closed after, since when a person finished would point at the time of
// their sends.
func anonymousEnrollmentStatus(status string, closed bool) string {
	switch {
	case status == model.ProgramEnrollmentExited:
		return model.ProgramEnrollmentExited
	case closed:
		return model.ProgramEnrollmentStopped
	default:
		return model.ProgramEnrollmentActive
	}
}

// recipientProgramTotals counts the sends of one recipient in each of the
// given programs, keyed by program id
func (s *Program) recipientProgramTotals(
	ctx context.Context,
	recipientID *uuid.UUID,
	programIDs []*uuid.UUID,
) (map[string]*ProgramRecipientTotals, error) {
	out := map[string]*ProgramRecipientTotals{}
	if len(programIDs) == 0 {
		return out, nil
	}
	campaigns, err := s.ProgramRepository.GetCampaignsOfPrograms(ctx, programIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	campaignOf := make(map[uuid.UUID]repository.ProgramCampaignOf, len(campaigns))
	for _, c := range campaigns {
		campaignOf[c.ID] = c
	}
	// one send per campaign, as the recipient is fixed
	sends := map[uuid.UUID]*programSend{}
	sendsOf := map[uuid.UUID][]*programSend{}
	err = s.ProgramRepository.EachRecipientSentRow(ctx, recipientID, programIDs, func(r *repository.ProgramSendRow) error {
		c, ok := campaignOf[r.CampaignID]
		if !ok || r.RecipientID == nil {
			return nil
		}
		sd := &programSend{
			Campaign:    r.CampaignID,
			Person:      *r.RecipientID,
			RecipientID: r.RecipientID,
			SentAt:      r.SentAt,
			Training:    c.IsTraining,
		}
		sends[r.CampaignID] = sd
		sendsOf[c.ProgramID] = append(sendsOf[c.ProgramID], sd)
		return nil
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	ids := programResultEventIDs()
	err = s.ProgramRepository.EachRecipientEventRow(ctx, recipientID, programIDs, ids.all(), func(e *repository.ProgramEventRow) error {
		if sd, ok := sends[e.CampaignID]; ok {
			ids.apply(sd, e.EventID, e.CreatedAt)
		}
		return nil
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for programID, list := range sendsOf {
		out[programID.String()] = recipientTotals(list)
	}
	return out, nil
}

// recipientTotals counts the sends of one recipient
func recipientTotals(sends []*programSend) *ProgramRecipientTotals {
	t := &ProgramRecipientTotals{}
	for _, sd := range sends {
		if sd.Training {
			t.TrainingSent++
			if sd.TrainingCompletedAt != nil {
				t.TrainingCompleted++
			}
			continue
		}
		t.Sent++
		if sd.Clicked() {
			t.Clicked++
		}
		if sd.SubmittedAt != nil {
			t.Submitted++
		}
		if sd.ReportedAt != nil {
			t.Reported++
		}
	}
	return t
}
