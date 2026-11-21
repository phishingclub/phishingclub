package repository

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProgramTimelineSpan is a part of the timeline in which the program zone
// keeps one offset from UTC. A day is read as the UTC time moved by Offset, so
// a range that crosses a daylight saving change is read as one span per offset.
type ProgramTimelineSpan struct {
	From, To time.Time
	// Offset is the program zone offset from UTC in seconds
	Offset int
}

// modifier is the SQLite date modifier that moves a UTC time to the program
// zone, such as "+3600 seconds"
func (s ProgramTimelineSpan) modifier() string {
	if s.Offset < 0 {
		return "-" + strconv.Itoa(-s.Offset) + " seconds"
	}
	return "+" + strconv.Itoa(s.Offset) + " seconds"
}

// ProgramTimelineEventKinds are the event ids the timeline counts per kind.
// A kind with no id counts nothing. Read, Clicked, Submitted and Reported are
// counted on the phishing campaigns of the program, TrainingStarted and
// TrainingCompleted on its training campaigns.
type ProgramTimelineEventKinds struct {
	Read              *uuid.UUID
	Clicked           []*uuid.UUID
	Submitted         *uuid.UUID
	Reported          *uuid.UUID
	TrainingStarted   *uuid.UUID
	TrainingCompleted *uuid.UUID
}

// the kinds of the timeline outcome rows
const (
	ProgramTimelineKindRead              = 1
	ProgramTimelineKindClicked           = 2
	ProgramTimelineKindSubmitted         = 3
	ProgramTimelineKindReported          = 4
	ProgramTimelineKindTrainingStarted   = 5
	ProgramTimelineKindTrainingCompleted = 6
)

// the kinds of the timeline sent rows
const (
	ProgramTimelineSentPhishing = 0
	ProgramTimelineSentTraining = 1
)

// ProgramTimelineCount is a count on one day, a YYYY-MM-DD date in the
// program zone. Kind is set on outcome rows, and on sent rows it tells
// phishing from training.
type ProgramTimelineCount struct {
	Day   string
	Kind  int
	Count int
}

// the select lists of the timeline counts. strftime reads the local date of a
// stored UTC time, the column and the modifier are bound and the text is
// fixed. The week of a time is its Monday: six days back, then forward to the
// next Monday, which is the same day when it is one.
const (
	programTimelineDayCount     = "strftime('%Y-%m-%d', ?, ?) AS day, COUNT(*) AS count"
	programTimelineDaySentKind  = "strftime('%Y-%m-%d', ?, ?) AS day, CASE WHEN ? IN (?) THEN 1 ELSE 0 END AS kind, COUNT(*) AS count"
	programTimelineDayPeople    = "strftime('%Y-%m-%d', ?, ?) AS day, COUNT(DISTINCT COALESCE(recipient_id, anonymized_id, id)) AS count"
	programTimelineWeekPeople   = "strftime('%Y-%m-%d', ?, ?, '-6 days', 'weekday 1') AS day, COUNT(DISTINCT COALESCE(recipient_id, anonymized_id, id)) AS count"
	programTimelineDayOutcomes  = "strftime('%Y-%m-%d', ?, ?) AS day, CASE WHEN event_id = ? THEN 1 WHEN event_id IN ? THEN 2 WHEN event_id = ? THEN 3 ELSE 4 END AS kind, COUNT(DISTINCT COALESCE(recipient_id, anonymized_id, id)) AS count"
	programTimelineWeekOutcomes = "strftime('%Y-%m-%d', ?, ?, '-6 days', 'weekday 1') AS day, CASE WHEN event_id = ? THEN 1 WHEN event_id IN ? THEN 2 WHEN event_id = ? THEN 3 ELSE 4 END AS kind, COUNT(DISTINCT COALESCE(recipient_id, anonymized_id, id)) AS count"
)

// programTimelinePlanLimit caps the two counts that pick how the rows of a
// timeline range are read. Both counts read one index only and stop at the
// cap, which takes a few milliseconds.
var programTimelinePlanLimit = 200000

// programTimelineByCampaign reports whether the rows of the program campaigns
// are fewer than the rows of every campaign in the time range. The fewer rows
// are then read: through the campaign index for a program with a short
// history, through the time index for a range that holds less than the whole
// program. When both reach the cap the time index is used, as the range is
// bounded and the history of a program is not.
func (r *Program) programTimelineByCampaign(ctx context.Context, program *gorm.DB, window *gorm.DB) (bool, error) {
	count := func(q *gorm.DB, limit int) (int, error) {
		var n int64
		err := r.DB.WithContext(ctx).Table("(?) AS capped", q.Limit(limit)).Count(&n).Error
		return int(n), err
	}
	inWindow, err := count(window, programTimelinePlanLimit+1)
	if err != nil {
		return false, errs.Wrap(err)
	}
	if inWindow == 0 {
		return false, nil
	}
	// the program count stops where it is no longer the smaller one, so a
	// short range such as today costs little
	inProgram, err := count(program, inWindow)
	if err != nil {
		return false, errs.Wrap(err)
	}
	return inProgram < inWindow, nil
}

// timelineSpanBounds returns the first and the last instant of the spans
func timelineSpanBounds(spans []ProgramTimelineSpan) (time.Time, time.Time) {
	return spans[0].From, spans[len(spans)-1].To
}

// GetTimelineSent counts the sent rows of every program campaign that name a
// person per day of the spans, by when they were sent, phishing and training
// apart by Kind, ProgramTimelineSentPhishing or ProgramTimelineSentTraining.
// The predicates repeat those of the partial index
// idx_campaign_recipients_campaign_sent_at, which holds the campaign and the
// sent time, so each campaign is read only within the range and never
// through the table rows.
func (r *Program) GetTimelineSent(
	ctx context.Context,
	programID *uuid.UUID,
	spans []ProgramTimelineSpan,
) ([]ProgramTimelineCount, error) {
	out := []ProgramTimelineCount{}
	sentAt := clause.Column{Name: "sent_at"}
	for _, span := range spans {
		rows := []ProgramTimelineCount{}
		err := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Select(programTimelineDaySentKind, sentAt, span.modifier(), clause.Column{Name: "campaign_id"}, r.programTrainingCampaignIDs(programID)).
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Name: "campaign_id"}, r.programCampaignIDs(programID)}}).
			Where(clause.Neq{Column: sentAt, Value: nil}).
			Where(clause.Or(
				clause.Neq{Column: clause.Column{Name: "recipient_id"}, Value: nil},
				clause.Neq{Column: clause.Column{Name: "anonymized_id"}, Value: nil},
			)).
			Where(clause.Expr{SQL: "? >= ?", Vars: []any{sentAt, utils.RFC3339UTC(span.From)}}).
			Where(clause.Expr{SQL: "? < ?", Vars: []any{sentAt, utils.RFC3339UTC(span.To)}}).
			Group("day").
			Group("kind").
			Scan(&rows).Error
		if err != nil {
			return nil, errs.Wrap(err)
		}
		out = append(out, rows...)
	}
	return out, nil
}

// GetTimelinePlanned counts the rows waiting to be sent in the open program
// campaigns per day of the spans, by when they are planned
func (r *Program) GetTimelinePlanned(
	ctx context.Context,
	programID *uuid.UUID,
	spans []ProgramTimelineSpan,
) ([]ProgramTimelineCount, error) {
	out := []ProgramTimelineCount{}
	sendAt := clause.Column{Name: "send_at"}
	for _, span := range spans {
		openCampaigns := r.newDB().
			Model(&database.Campaign{}).
			Select("id").
			Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
			Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil})
		rows := []ProgramTimelineCount{}
		// the rows are found through idx_campaign_recipients_unsent_campaign,
		// which holds only rows waiting to be sent, so send_at is read from
		// the row and not through its index of every row
		err := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Select(programTimelineDayCount, sendAt, span.modifier()).
			Where(unsentSends()).
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Name: "campaign_id"}, openCampaigns}}).
			Where(clause.Expr{SQL: "? >= ?", Vars: []any{notIndexed(sendAt), utils.RFC3339UTC(span.From)}}).
			Where(clause.Expr{SQL: "? < ?", Vars: []any{notIndexed(sendAt), utils.RFC3339UTC(span.To)}}).
			Group("day").
			Scan(&rows).Error
		if err != nil {
			return nil, errs.Wrap(err)
		}
		out = append(out, rows...)
	}
	return out, nil
}

// GetTimelineDue counts the active people of a program per day of the spans by
// when their next send is due. The scheduler stores a send row only when it is
// due, so this is the send time drawn for the next round of each person.
// next_due_at is stored as a time value, so it is compared as one. A person
// missing from the groups waits and is not counted, and nothing is counted at
// or after endAt when it is set.
func (r *Program) GetTimelineDue(
	ctx context.Context,
	programID *uuid.UUID,
	spans []ProgramTimelineSpan,
	endAt *time.Time,
) ([]ProgramTimelineCount, error) {
	out := []ProgramTimelineCount{}
	nextDueAt := clause.Column{Name: "next_due_at"}
	for _, span := range spans {
		to := span.To
		if endAt != nil && endAt.Before(to) {
			to = *endAt
		}
		if !span.From.Before(to) {
			continue
		}
		rows := []ProgramTimelineCount{}
		// the program, status and due index holds the program, the status and
		// the time, so only the rows in the range are read for missing_since
		err := r.DB.WithContext(ctx).
			Model(&database.ProgramEnrollment{}).
			Select(programTimelineDayCount, nextDueAt, span.modifier()).
			Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
			Where(clause.Eq{Column: clause.Column{Name: "status"}, Value: model.ProgramEnrollmentActive}).
			Where(clause.Gte{Column: nextDueAt, Value: span.From.UTC()}).
			Where(clause.Lt{Column: nextDueAt, Value: to.UTC()}).
			Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "missing_since"})}}).
			Group("day").
			Scan(&rows).Error
		if err != nil {
			return nil, errs.Wrap(err)
		}
		out = append(out, rows...)
	}
	return out, nil
}

// programTimelineOutcomeQuery holds what the outcome counts of a range share:
// the event ids per kind and how the rows are read
type programTimelineOutcomeQuery struct {
	read, submitted string
	clicked         []any
	eventID         clause.IN
	byCampaign      bool
	// campaigns selects the ids of the campaigns the events are read from
	campaigns func(programID *uuid.UUID) *gorm.DB
}

// timelineOutcomeQuery prepares the outcome counts of the spans. It returns
// nil when no event type is known, which counts nothing.
func (r *Program) timelineOutcomeQuery(
	ctx context.Context,
	programID *uuid.UUID,
	kinds ProgramTimelineEventKinds,
	spans []ProgramTimelineSpan,
) (*programTimelineOutcomeQuery, error) {
	ids := []string{}
	add := func(id *uuid.UUID) string {
		if id == nil {
			// a missing event type matches no row
			return uuid.Nil.String()
		}
		ids = append(ids, id.String())
		return id.String()
	}
	q := &programTimelineOutcomeQuery{campaigns: r.programPhishingCampaignIDs}
	q.read = add(kinds.Read)
	for _, id := range kinds.Clicked {
		q.clicked = append(q.clicked, add(id))
	}
	if len(q.clicked) == 0 {
		q.clicked = append(q.clicked, uuid.Nil.String())
	}
	q.submitted = add(kinds.Submitted)
	// every other row the read matches is a report
	add(kinds.Reported)
	if len(ids) == 0 {
		return nil, nil
	}
	q.eventID = clause.IN{Column: clause.Column{Name: "event_id"}, Values: eventIDValues(ids)}
	if err := r.planTimelineOutcomes(ctx, programID, q, spans); err != nil {
		return nil, err
	}
	return q, nil
}

// planTimelineOutcomes picks the index the events of q are read through
func (r *Program) planTimelineOutcomes(
	ctx context.Context,
	programID *uuid.UUID,
	q *programTimelineOutcomeQuery,
	spans []ProgramTimelineSpan,
) error {
	createdAt := clause.Column{Name: "created_at"}
	campaignID := clause.Column{Name: "campaign_id"}
	start, end := timelineSpanBounds(spans)
	// both counts read one index only: the campaign and event index, and the
	// created_at index within the range
	byCampaign, err := r.programTimelineByCampaign(ctx,
		r.newDB().Model(&database.CampaignEvent{}).Select("1").
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{campaignID, q.campaigns(programID)}}).
			Where(q.eventID),
		r.newDB().Model(&database.CampaignEvent{}).Select("1").
			Where(clause.Expr{SQL: "? >= ?", Vars: []any{createdAt, utils.RFC3339UTC(start)}}).
			Where(clause.Expr{SQL: "? < ?", Vars: []any{createdAt, utils.RFC3339UTC(end)}}),
	)
	if err != nil {
		return err
	}
	q.byCampaign = byCampaign
	return nil
}

// where limits the events to the campaigns of q and to [from, to), reading
// the index the plan picked. The read, click, submit and report counts read
// the phishing campaigns only: opening a training page or sending its form is
// not a click or a submit on a phish, the same as the results facts.
func (q *programTimelineOutcomeQuery) where(r *Program, db *gorm.DB, programID *uuid.UUID, from, to time.Time) *gorm.DB {
	createdAt := clause.Column{Name: "created_at"}
	campaignID := clause.Column{Name: "campaign_id"}
	var scope, after, before clause.Expression
	if q.byCampaign {
		scope = clause.Expr{SQL: "? IN (?)", Vars: []any{campaignID, q.campaigns(programID)}}
		after = clause.Expr{SQL: "? >= ?", Vars: []any{notIndexed(createdAt), utils.RFC3339UTC(from)}}
		before = clause.Expr{SQL: "? < ?", Vars: []any{notIndexed(createdAt), utils.RFC3339UTC(to)}}
	} else {
		scope = clause.Expr{SQL: "? IN (?)", Vars: []any{notIndexed(campaignID), q.campaigns(programID)}}
		after = clause.Expr{SQL: "? >= ?", Vars: []any{createdAt, utils.RFC3339UTC(from)}}
		before = clause.Expr{SQL: "? < ?", Vars: []any{createdAt, utils.RFC3339UTC(to)}}
	}
	return db.Where(scope).Where(q.eventID).Where(after).Where(before)
}

// GetTimelineOutcomes counts the people with a read, a click, a submit or a
// report per day of the spans and per kind, by when the event happened. A
// person counts once per kind and day, so a person who opens two pages counts
// once. The spans are read in one query, so a day that crosses a daylight
// saving change still counts each person once.
func (r *Program) GetTimelineOutcomes(
	ctx context.Context,
	programID *uuid.UUID,
	kinds ProgramTimelineEventKinds,
	spans []ProgramTimelineSpan,
) ([]ProgramTimelineCount, error) {
	out := []ProgramTimelineCount{}
	if len(spans) == 0 {
		return out, nil
	}
	q, err := r.timelineOutcomeQuery(ctx, programID, kinds, spans)
	if err != nil || q == nil {
		return out, err
	}
	createdAt := clause.Column{Name: "created_at"}
	start, end := timelineSpanBounds(spans)
	db := r.DB.WithContext(ctx).
		Model(&database.CampaignEvent{}).
		Select(programTimelineDayOutcomes, createdAt, timelineModifier(createdAt, spans), q.read, q.clicked, q.submitted)
	err = q.where(r, db, programID, start, end).
		Group("day").
		Group("kind").
		Scan(&out).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return out, nil
}

// GetTimelineOutcomesByWeek counts the people with a read, a click, a submit
// or a report per week of the spans and per kind, by when the event happened.
// A week starts on Monday in the program zone and Day is that Monday. A
// person counts once per kind and week, also when active on several days of
// it. The spans are read in one query, so a week that crosses a daylight
// saving change still counts each person once.
func (r *Program) GetTimelineOutcomesByWeek(
	ctx context.Context,
	programID *uuid.UUID,
	kinds ProgramTimelineEventKinds,
	spans []ProgramTimelineSpan,
) ([]ProgramTimelineCount, error) {
	out := []ProgramTimelineCount{}
	if len(spans) == 0 {
		return out, nil
	}
	q, err := r.timelineOutcomeQuery(ctx, programID, kinds, spans)
	if err != nil || q == nil {
		return out, err
	}
	createdAt := clause.Column{Name: "created_at"}
	start, end := timelineSpanBounds(spans)
	db := r.DB.WithContext(ctx).
		Model(&database.CampaignEvent{}).
		Select(programTimelineWeekOutcomes, createdAt, timelineModifier(createdAt, spans), q.read, q.clicked, q.submitted)
	err = q.where(r, db, programID, start, end).
		Group("day").
		Group("kind").
		Scan(&out).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return out, nil
}

// GetTimelineTrainingOutcomes counts the people who started and who
// completed a training per day of the spans, or per week by its Monday when
// weekly, on the training campaigns of the program, by when the event
// happened. A completion counts as a start on its day too, so the started
// count of a day or week is never below its completed count, as on the
// tiles. A person counts once per kind and day or week.
func (r *Program) GetTimelineTrainingOutcomes(
	ctx context.Context,
	programID *uuid.UUID,
	kinds ProgramTimelineEventKinds,
	spans []ProgramTimelineSpan,
	weekly bool,
) ([]ProgramTimelineCount, error) {
	out := []ProgramTimelineCount{}
	if len(spans) == 0 || (kinds.TrainingStarted == nil && kinds.TrainingCompleted == nil) {
		return out, nil
	}
	ids := func(list ...*uuid.UUID) []string {
		out := []string{}
		for _, id := range list {
			if id != nil {
				out = append(out, id.String())
			}
		}
		return out
	}
	selectText := programTimelineDayPeople
	if weekly {
		selectText = programTimelineWeekPeople
	}
	createdAt := clause.Column{Name: "created_at"}
	start, end := timelineSpanBounds(spans)
	for _, part := range []struct {
		kind int
		ids  []string
	}{
		{ProgramTimelineKindTrainingStarted, ids(kinds.TrainingStarted, kinds.TrainingCompleted)},
		{ProgramTimelineKindTrainingCompleted, ids(kinds.TrainingCompleted)},
	} {
		if len(part.ids) == 0 {
			continue
		}
		q := &programTimelineOutcomeQuery{
			campaigns: r.programTrainingCampaignIDs,
			eventID:   clause.IN{Column: clause.Column{Name: "event_id"}, Values: eventIDValues(part.ids)},
		}
		if err := r.planTimelineOutcomes(ctx, programID, q, spans); err != nil {
			return nil, err
		}
		rows := []ProgramTimelineCount{}
		db := r.DB.WithContext(ctx).
			Model(&database.CampaignEvent{}).
			Select(selectText, createdAt, timelineModifier(createdAt, spans))
		err := q.where(r, db, programID, start, end).
			Group("day").
			Scan(&rows).Error
		if err != nil {
			return nil, errs.Wrap(err)
		}
		for i := range rows {
			rows[i].Kind = part.kind
		}
		out = append(out, rows...)
	}
	return out, nil
}

// timelineModifier picks the modifier of the span a time of column falls in,
// as nested CASE expressions with every value bound
func timelineModifier(column clause.Column, spans []ProgramTimelineSpan) clause.Expression {
	var expr clause.Expression = clause.Expr{SQL: "?", Vars: []any{spans[len(spans)-1].modifier()}}
	for i := len(spans) - 2; i >= 0; i-- {
		expr = clause.Expr{
			SQL:  "CASE WHEN ? < ? THEN ? ELSE ? END",
			Vars: []any{column, utils.RFC3339UTC(spans[i].To), spans[i].modifier(), expr},
		}
	}
	return expr
}

// newDB starts a query that shares nothing with r.DB
func (r *Program) newDB() *gorm.DB {
	return r.DB.Session(&gorm.Session{NewDB: true})
}
