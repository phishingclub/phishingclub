package service

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// program schedule row statuses
const (
	ProgramScheduleSent      = "sent"
	ProgramScheduleScheduled = "scheduled"
	ProgramScheduleNotSent   = "notSent"
)

// programScheduleMaxRows bounds the rows of a recipient schedule
const programScheduleMaxRows = model.ProgramMaxSteps

// ProgramScheduleWindow is the days a round may send on, as YYYY-MM-DD in the
// program time zone. It is a window, not a send time: the scheduler draws the
// time inside it when the round comes. AfterEnd is set when the round starts
// after the program ends, so nothing is sent then.
type ProgramScheduleWindow struct {
	FirstDay string `json:"firstDay"`
	LastDay  string `json:"lastDay"`
	AfterEnd bool   `json:"afterEnd"`
}

// ProgramScheduleRow is one template of the current round of a recipient
type ProgramScheduleRow struct {
	TemplateName     string                 `json:"templateName"`
	Training         bool                   `json:"training"`
	Status           string                 `json:"status"`
	SentAt           *time.Time             `json:"sentAt"`
	NotableEventName string                 `json:"notableEventName"`
	ScheduledAt      *time.Time             `json:"scheduledAt"`
	Window           *ProgramScheduleWindow `json:"window"`
}

// ProgramRecipientSchedule is the plan of one recipient in a program: the
// round they are in, how many templates of it are sent, and one row per
// template. With random order the remaining templates have no window, as the
// order is decided at send time; NextWindows lists the coming rounds instead.
// When one template is left it is decided and has its window.
type ProgramRecipientSchedule struct {
	EnrollmentStatus string                   `json:"enrollmentStatus"`
	OrderMode        string                   `json:"orderMode"`
	LoopMode         string                   `json:"loopMode"`
	TimeZone         string                   `json:"timeZone"`
	Round            int                      `json:"round"`
	Sent             int                      `json:"sent"`
	TemplateCount    int                      `json:"templateCount"`
	Rows             []*ProgramScheduleRow    `json:"rows"`
	NextWindows      []*ProgramScheduleWindow `json:"nextWindows"`
}

// programRoundWindow returns the window of a round that starts at roundStart:
// the sending days in [roundStart, roundStart+spread) that are not past, or
// the next opening when the round has none. A round that holds less sending
// time than a whole one ends later, the same as the scheduler draws the send
// time, see programPickSendTimeAfter and programRoundEnd.
func programRoundWindow(program *model.Program, roundStart, now time.Time) *ProgramScheduleWindow {
	from := roundStart
	if now.After(from) {
		from = now
	}
	spread := program.SpreadOrDefault()
	to := roundStart.Add(spread)
	if spread > 0 {
		to = programRoundEnd(program, from, to, spread)
	}
	first := programNextWindowSlot(program, from)
	last := first
	spans := programWindowSpans(program, from, to)
	if len(spans) > 0 {
		first = spans[0].start
		last = spans[len(spans)-1].end.Add(-time.Nanosecond)
	}
	loc := program.Location()
	w := &ProgramScheduleWindow{
		FirstDay: first.In(loc).Format(model.ProgramDateLayout),
		LastDay:  last.In(loc).Format(model.ProgramDateLayout),
	}
	if end := program.EndAtTime(); end != nil && !first.Before(*end) {
		w.AfterEnd = true
	}
	return w
}

// GetRecipientSchedule returns the plan of a recipient in a program. The
// recipient must be enrolled, an anonymous program gives nothing.
func (s *Program) GetRecipientSchedule(
	ctx context.Context,
	session *model.Session,
	programID *uuid.UUID,
	recipientID *uuid.UUID,
) (*ProgramRecipientSchedule, error) {
	ae := NewAuditEvent("Program.GetRecipientSchedule", session)
	program, err := s.authorizedProgram(ctx, session, programID, ae, &repository.ProgramOption{WithSteps: true})
	if err != nil {
		return nil, err
	}
	// no per recipient plan for an anonymous or anonymized program
	if program.IsAnonymousOn() || program.AnonymizeBegun() {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}
	e, err := s.ProgramRepository.GetEnrollmentByProgramRecipient(ctx, programID, recipientID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	steps := program.Steps
	if len(steps) > programScheduleMaxRows {
		steps = steps[:programScheduleMaxRows]
	}
	links, err := s.ProgramRepository.GetCampaignLinks(ctx, programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	infos, err := s.ProgramRepository.GetCampaignInfos(ctx, programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	sentRows, err := s.ProgramRepository.GetSentRows(ctx, programID, []*uuid.UUID{recipientID})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	queuedRows, err := s.ProgramRepository.GetQueuedSends(ctx, programID, []*uuid.UUID{recipientID})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// the hidden campaigns of the current round, by campaign
	type link struct {
		step uuid.UUID
	}
	roundCampaigns := map[uuid.UUID]link{}
	for _, l := range links {
		if l.CampaignID != nil && l.ProgramStepID != nil && l.LoopIndex == e.LoopCount {
			roundCampaigns[*l.CampaignID] = link{step: *l.ProgramStepID}
		}
	}
	infoOf := map[uuid.UUID]repository.ProgramCampaignInfo{}
	for _, i := range infos {
		infoOf[i.ID] = i
	}
	templateIDs := make([]*uuid.UUID, 0, len(steps)+len(infos))
	for _, st := range steps {
		tid := st.CampaignTemplateID.MustGet()
		templateIDs = append(templateIDs, &tid)
	}
	for _, i := range infos {
		if _, ok := roundCampaigns[i.ID]; ok && i.CampaignTemplateID != nil {
			templateIDs = append(templateIDs, i.CampaignTemplateID)
		}
	}
	names, err := s.ProgramRepository.GetTemplateNames(ctx, templateIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	nameOf := func(id *uuid.UUID) string {
		if id != nil {
			if n, ok := names[*id]; ok {
				return n
			}
		}
		return programDeletedTemplateName
	}

	out := &ProgramRecipientSchedule{
		EnrollmentStatus: e.Status,
		OrderMode:        program.OrderModeOrDefault(),
		LoopMode:         program.LoopModeOrDefault(),
		TimeZone:         program.Location().String(),
		Round:            e.LoopCount + 1,
		TemplateCount:    len(steps),
		Rows:             []*ProgramScheduleRow{},
		NextWindows:      []*ProgramScheduleWindow{},
	}
	// the sends and queued sends of the current round, by step
	given := map[uuid.UUID]*ProgramScheduleRow{}
	history := []*ProgramScheduleRow{}
	// deliveries are the steps given in the round with their template, as
	// the scheduler counts them, see deliveredSteps
	deliveries := []repository.ProgramDelivery{}
	rowFor := func(campaignID uuid.UUID) *ProgramScheduleRow {
		info := infoOf[campaignID]
		return &ProgramScheduleRow{TemplateName: nameOf(info.CampaignTemplateID), Training: info.IsTraining}
	}
	for _, sr := range sentRows {
		l, ok := roundCampaigns[sr.CampaignID]
		if !ok {
			continue
		}
		row := rowFor(sr.CampaignID)
		row.Status = ProgramScheduleSent
		at := sr.SentAt
		row.SentAt = &at
		if sr.NotableEventID != nil {
			row.NotableEventName = cache.EventNameByID[sr.NotableEventID.String()]
		}
		given[l.step] = row
		history = append(history, row)
		deliveries = append(deliveries, repository.ProgramDelivery{StepID: l.step, TemplateID: infoOf[sr.CampaignID].CampaignTemplateID})
		out.Sent++
	}
	for _, q := range queuedRows {
		l, ok := roundCampaigns[q.CampaignID]
		if !ok || given[l.step] != nil {
			continue
		}
		row := rowFor(q.CampaignID)
		row.Status = ProgramScheduleScheduled
		row.ScheduledAt = q.SendAt
		given[l.step] = row
		history = append(history, row)
		deliveries = append(deliveries, repository.ProgramDelivery{StepID: l.step, TemplateID: infoOf[q.CampaignID].CampaignTemplateID})
	}
	// a template removed and added again in the round is a new step, which
	// the scheduler counts as given by the send of the removed step of the
	// same template. That send is shown on the new step.
	givenTo := programScheduleGiven(steps, deliveries, given)
	// history in the order it was sent, then what is queued
	sortTime := func(r *ProgramScheduleRow) time.Time {
		if r.SentAt != nil {
			return *r.SentAt
		}
		if r.ScheduledAt != nil {
			return *r.ScheduledAt
		}
		return time.Time{}
	}
	sort.SliceStable(history, func(i, j int) bool {
		if (history[i].Status == ProgramScheduleSent) != (history[j].Status == ProgramScheduleSent) {
			return history[i].Status == ProgramScheduleSent
		}
		return sortTime(history[i]).Before(sortTime(history[j]))
	})

	remaining := []*model.ProgramStep{}
	for _, st := range steps {
		if givenTo[st.ID.MustGet()] == nil {
			remaining = append(remaining, st)
		}
	}
	active := e.Status == model.ProgramEnrollmentActive && !program.IsClosed()
	sequential := out.OrderMode == model.ProgramOrderSequential

	// the windows of the coming rounds, one per template left, never a send time
	windows := []*ProgramScheduleWindow{}
	if active && len(remaining) > 0 {
		now := time.Now()
		start := now
		if e.RoundStartAt != nil {
			start = *e.RoundStartAt
		} else if e.NextDueAt != nil {
			start = *e.NextDueAt
		}
		for range remaining {
			windows = append(windows, programRoundWindow(program, start, now))
			start = programNextRoundStart(program, start, now)
		}
	}

	if sequential {
		// in the same order the rows follow the template list
		for _, st := range steps {
			if row := givenTo[st.ID.MustGet()]; row != nil {
				out.Rows = append(out.Rows, row)
				continue
			}
			if !active {
				continue
			}
			row := &ProgramScheduleRow{
				TemplateName: nameOf(ptrID(st.CampaignTemplateID.MustGet())),
				Status:       ProgramScheduleNotSent,
			}
			if len(windows) > 0 {
				row.Window = windows[0]
				windows = windows[1:]
			}
			out.Rows = append(out.Rows, row)
		}
	} else {
		out.Rows = append(out.Rows, history...)
		if active {
			for _, st := range remaining {
				row := &ProgramScheduleRow{
					TemplateName: nameOf(ptrID(st.CampaignTemplateID.MustGet())),
					Status:       ProgramScheduleNotSent,
				}
				// the last template left is decided, so it has its window as
				// in the same order
				if len(remaining) == 1 && len(windows) > 0 {
					row.Window = windows[0]
				}
				out.Rows = append(out.Rows, row)
			}
			// the windows are listed only while the order is still open
			if len(remaining) > 1 {
				out.NextWindows = windows
			}
		}
	}
	if len(out.Rows) > programScheduleMaxRows {
		out.Rows = out.Rows[:programScheduleMaxRows]
	}
	// no audit on read, as for the other program reads
	return out, nil
}

// programScheduleGiven returns the row of each step that counts as given in
// the round, by the rule of deliveredSteps: a step's own send or queued send,
// or else the send of a removed step of the same template. given holds the row
// of each step that has one, removed steps included.
func programScheduleGiven(
	steps []*model.ProgramStep,
	deliveries []repository.ProgramDelivery,
	given map[uuid.UUID]*ProgramScheduleRow,
) map[uuid.UUID]*ProgramScheduleRow {
	delivered := deliveredSteps(steps, deliveries)
	current := make(map[uuid.UUID]bool, len(steps))
	for _, st := range steps {
		current[st.ID.MustGet()] = true
	}
	// the rows of removed steps by template, in the order they were given
	leftover := map[uuid.UUID][]*ProgramScheduleRow{}
	seen := map[uuid.UUID]bool{}
	for _, d := range deliveries {
		if current[d.StepID] || seen[d.StepID] || d.TemplateID == nil {
			continue
		}
		seen[d.StepID] = true
		leftover[*d.TemplateID] = append(leftover[*d.TemplateID], given[d.StepID])
	}
	out := map[uuid.UUID]*ProgramScheduleRow{}
	for _, st := range steps {
		id := st.ID.MustGet()
		if !delivered[id] {
			continue
		}
		if row := given[id]; row != nil {
			out[id] = row
			continue
		}
		templateID := st.CampaignTemplateID.MustGet()
		if rows := leftover[templateID]; len(rows) > 0 {
			out[id] = rows[0]
			leftover[templateID] = rows[1:]
		}
	}
	return out
}

// ptrID returns a pointer to a copy of an id
func ptrID(id uuid.UUID) *uuid.UUID {
	return &id
}
