package service

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// ProgramRiskSortKey is the sort name of the derived risk in the people table.
// Risk is not a column, so these rows are sorted after they are scored.
const ProgramRiskSortKey = "risk"

// ProgramEnrollmentRow is one recipient's public journey state, including the
// preplanned next step and when it is due.
type ProgramEnrollmentRow struct {
	RecipientID string `json:"recipientID"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Position    int    `json:"position"`
	LoopCount   int    `json:"loopCount"`
	Status      string `json:"status"`
	// RiskScore is nil when the person has too few sends to judge
	RiskScore *float64   `json:"riskScore"`
	RiskLevel string     `json:"riskLevel"`
	Sends     int        `json:"sends"`
	NextStep  string     `json:"nextStep"`
	NextDueAt *time.Time `json:"nextDueAt"`
}

// ProgramRecipientsView is the recipients endpoint response. For an anonymous
// program it carries only the status counts, never per recipient identity or
// behaviour, so the operator cannot tell who clicked. Count is how many rows
// match the search, Rows is one page of them.
type ProgramRecipientsView struct {
	Anonymous bool `json:"anonymous"`
	StepCount int  `json:"stepCount"`
	Total     int  `json:"total"`
	Active    int  `json:"active"`
	Exited    int  `json:"exited"`
	Completed int  `json:"completed"`
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

// programPageFactsMax is the largest page whose facts are loaded by recipient
const programPageFactsMax = 500

// GetRecipientsView returns one page of the per recipient journey of a program,
// withholding identity and behaviour for an anonymous program.
func (s *Program) GetRecipientsView(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	q *vo.QueryArgs,
) (*ProgramRecipientsView, error) {
	ae := NewAuditEvent("Program.GetRecipients", session)
	program, err := s.authorizedProgram(ctx, session, id, ae, &repository.ProgramOption{WithSteps: true})
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
		// no per recipient identity or behaviour for an anonymous program
		return view, nil
	}
	if q == nil {
		q = &vo.QueryArgs{}
	}
	riskSort := q.OrderBy == ProgramRiskSortKey
	pageQuery := *q
	if riskSort {
		// risk is scored after loading, so every match is loaded and paged here
		pageQuery.OrderBy, pageQuery.Offset, pageQuery.Limit = "", 0, 0
	}
	enrollments, count, err := s.ProgramRepository.GetEnrollmentPage(ctx, id, &pageQuery)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	view.Count = count
	// a risk sort scores every match, so it needs the facts of all of them. A
	// page needs only its own people, unless the page is too large to name
	var pageIDs []*uuid.UUID
	if !riskSort && len(enrollments) <= programPageFactsMax {
		pageIDs = make([]*uuid.UUID, 0, len(enrollments))
		for _, e := range enrollments {
			pageIDs = append(pageIDs, e.RecipientID)
		}
	}
	facts, err := s.loadProgramFacts(ctx, session, program, pageIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	now := time.Now()
	risks := facts.risks(now)
	sendCount := map[uuid.UUID]int{}
	for _, sd := range facts.phishing() {
		sendCount[sd.Person]++
	}
	// resolve the scheduled step template names once, to name each recipient's
	// next step
	steps := program.Steps
	stepNames := make(map[uuid.UUID]string, len(steps))
	for _, st := range steps {
		stepNames[st.ID.MustGet()] = s.templateName(ctx, session, st.CampaignTemplateID.MustGet().String())
	}
	// the deliveries of the page only, unless every match is loaded
	deliveries, err := s.ProgramRepository.GetDeliveries(ctx, id, pageIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	delivered := deliveriesByPersonLoop(deliveries)
	shuffle := program.OrderModeOrDefault() == model.ProgramOrderShuffle
	for _, e := range enrollments {
		email, name := recipientDisplayName(e.Recipient)
		nextStep := "Complete"
		var nextDue *time.Time
		if e.Status == model.ProgramEnrollmentActive && len(steps) > 0 {
			given := deliveredSteps(steps, delivered[programPersonLoop{*e.RecipientID, e.LoopCount}])
			left := undeliveredSteps(steps, given)
			if len(left) == 0 {
				// the next send starts a new loop
				left = steps
			}
			switch {
			case shuffle:
				nextStep = "Random, " + strconv.Itoa(len(left)) + " not yet sent"
			default:
				next := programPickStep(program, left, nil)
				if next != nil {
					nextStep = stepNames[next.ID.MustGet()]
				}
			}
			nextDue = e.NextDueAt
		}
		risk := risks[*e.RecipientID]
		view.Rows = append(view.Rows, ProgramEnrollmentRow{
			RecipientID: e.RecipientID.String(),
			Email:       email,
			Name:        name,
			Position:    e.Position,
			LoopCount:   e.LoopCount,
			Status:      e.Status,
			RiskScore:   risk,
			RiskLevel:   programRiskLevel(risk),
			Sends:       sendCount[*e.RecipientID],
			NextStep:    nextStep,
			NextDueAt:   nextDue,
		})
	}
	if riskSort {
		view.Rows = sortAndPageByRisk(view.Rows, q.Desc, q.Offset, q.Limit)
		view.HasNextPage = q.Limit > 0 && int64(q.Offset+q.Limit) < count
		return view, nil
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

// sortAndPageByRisk sorts rows by risk, rows without a risk last, and returns
// one page
func sortAndPageByRisk(rows []ProgramEnrollmentRow, desc bool, offset, limit int) []ProgramEnrollmentRow {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].RiskScore, rows[j].RiskScore
		switch {
		case a == nil && b == nil:
			return rows[i].Email < rows[j].Email
		case a == nil:
			return false
		case b == nil:
			return true
		case *a == *b:
			return rows[i].Email < rows[j].Email
		case desc:
			return *a > *b
		}
		return *a < *b
	})
	if offset >= len(rows) {
		return []ProgramEnrollmentRow{}
	}
	end := len(rows)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return rows[offset:end]
}

// ProgramRecipientSend is one message a program sent to a recipient
type ProgramRecipientSend struct {
	Template            string     `json:"template"`
	Kind                string     `json:"kind"`
	SentAt              time.Time  `json:"sentAt"`
	OpenedAt            *time.Time `json:"openedAt"`
	ClickedAt           *time.Time `json:"clickedAt"`
	SubmittedAt         *time.Time `json:"submittedAt"`
	ReportedAt          *time.Time `json:"reportedAt"`
	TrainingCompletedAt *time.Time `json:"trainingCompletedAt"`
}

// ProgramRecipientActivity is one program a recipient is enrolled in. For an
// anonymous program only the name and enrollment status are given.
type ProgramRecipientActivity struct {
	ProgramID        string                  `json:"programID"`
	Name             string                  `json:"name"`
	Anonymous        bool                    `json:"anonymous"`
	ProgramStatus    string                  `json:"programStatus"`
	EnrollmentStatus string                  `json:"enrollmentStatus"`
	RiskScore        *float64                `json:"riskScore"`
	RiskLevel        string                  `json:"riskLevel"`
	NextDueAt        *time.Time              `json:"nextDueAt"`
	Sends            []*ProgramRecipientSend `json:"sends"`
}

// program send kinds
const (
	ProgramSendKindPhishing = "phishing"
	ProgramSendKindTraining = "training"
)

// GetRecipientPrograms returns the programs a recipient is enrolled in with
// their activity there. Only programs of the recipient's company, or global
// ones, are listed.
func (s *Program) GetRecipientPrograms(
	ctx context.Context,
	session *model.Session,
	recipientID *uuid.UUID,
) ([]*ProgramRecipientActivity, error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent("Program.GetRecipientPrograms", session)
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	recipient, err := s.RecipientRepository.GetByID(ctx, recipientID, &repository.RecipientOption{})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	var recipientCompany *uuid.UUID
	if cid, err := recipient.CompanyID.Get(); err == nil {
		recipientCompany = &cid
	}
	enrollments, err := s.ProgramRepository.GetEnrollmentsByRecipientID(ctx, recipientID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	now := time.Now()
	out := []*ProgramRecipientActivity{}
	for _, e := range enrollments {
		program, err := s.ProgramRepository.GetByID(ctx, e.ProgramID, &repository.ProgramOption{})
		if err != nil {
			continue
		}
		programCompany := optionalID(program.CompanyID)
		if recipientCompany != nil && programCompany != nil && *programCompany != *recipientCompany {
			continue
		}
		anonymous := program.IsAnonymousOn()
		activity := &ProgramRecipientActivity{
			ProgramID:        e.ProgramID.String(),
			Name:             programName(program),
			Anonymous:        anonymous,
			ProgramStatus:    program.Status,
			EnrollmentStatus: e.Status,
			Sends:            []*ProgramRecipientSend{},
		}
		if anonymous {
			// no per person behaviour for an anonymous program
			out = append(out, activity)
			continue
		}
		if e.Status == model.ProgramEnrollmentActive {
			activity.NextDueAt = e.NextDueAt
		}
		facts, err := s.loadProgramFacts(ctx, session, program, []*uuid.UUID{recipientID})
		if err != nil {
			return nil, errs.Wrap(err)
		}
		risk := facts.risks(now)[*recipientID]
		activity.RiskScore = risk
		activity.RiskLevel = programRiskLevel(risk)
		for _, sd := range facts.sends {
			activity.Sends = append(activity.Sends, &ProgramRecipientSend{
				Template:            facts.templateNames[sd.Template],
				Kind:                sd.Kind(),
				SentAt:              sd.SentAt,
				OpenedAt:            sd.ReadAt,
				ClickedAt:           sd.ClickedAt,
				SubmittedAt:         sd.SubmittedAt,
				ReportedAt:          sd.ReportedAt,
				TrainingCompletedAt: sd.TrainingCompletedAt,
			})
		}
		sort.Slice(activity.Sends, func(i, j int) bool { return activity.Sends[i].SentAt.After(activity.Sends[j].SentAt) })
		out = append(out, activity)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
