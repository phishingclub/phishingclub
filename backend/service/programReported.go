package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// errProgramReportedAnonymous refuses a reported CSV on an anonymous program.
// A file with one named row would show whether that person was sent a phish
// and change the results by their report, so reports of an anonymous program
// come from the report inbox only.
var errProgramReportedAnonymous = errors.New("reported emails cannot be imported on an anonymous program, reports come from the report inbox")

// programReportedPerson is an enrolled person a reported CSV row can match
type programReportedPerson struct {
	recipientID  *uuid.UUID
	anonymizedID *uuid.UUID
}

// ProcessReportedCSV records the reports of a reported CSV on a program. It
// returns the same result as the campaign import. Each email is matched to an
// enrolled person, exact case first and then ignoring case when only one
// person has it, and the report lands on their latest phishing send at or
// before the reported time, or their latest send when the row has no time.
// dateColumn below zero means the file has no time column. A time without a
// zone is read in the program time zone and a date without a time means the
// end of that day. An anonymous program is refused.
//
// On a program that is closing or closed a report time after the hidden
// campaign of the send closed, or after the program closed, is recorded at
// the earlier of those closes: the report is of a send made while the
// program ran, and a row without a time, or with only the date of the close
// day, would else always fail. The campaigns close before the program is
// marked closed, so the time is also within the day after the close that the
// events page lists. On a running program a send whose hidden campaign
// closed before the report time takes no report, the row fails, as a report
// from a button or an inbox would be skipped too. A program that is being
// deleted is refused.
//
// The rows are matched first, then the phishing sends of the matched people
// are read in one pass, so the import reads the sends once and not once per
// row.
func (s *Program) ProcessReportedCSV(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	header []string,
	rows []ReportedCSVRow,
	emailColumn int,
	dateColumn int,
) (_ *ReportedCSVResult, err error) {
	// the cached results may no longer hold
	defer s.invalidateResults(id)
	ae := NewAuditEvent("Program.ReportedCSV", session)
	defer s.auditDone(ae, &err)
	ae.Details["id"] = id.String()
	program, err := s.authorizedProgram(ctx, session, id, ae, nil)
	if err != nil {
		return nil, err
	}
	// the import holds the anonymize claim of the program, so an anonymize
	// can not begin while it writes events that name people. The claim is
	// taken and the program read again under the lifecycle lock, so a close
	// or delete either sees the claim or is seen by the import.
	claimed := false
	err = s.withLifecycleLock(ctx, func() error {
		if !s.claimAnonymizing(*id) {
			return nil
		}
		claimed = true
		p, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
		if err != nil {
			return errs.Wrap(err)
		}
		program = p
		return nil
	})
	if claimed {
		defer s.releaseAnonymizing(*id)
	}
	if err != nil {
		return nil, err
	}
	if !claimed {
		ae.Details["refused"] = "program being anonymized"
		return nil, errs.NewValidationError(errProgramAnonymizing)
	}
	program.SetReadState(time.Now())
	if program.IsAnonymousOn() {
		ae.Details["refused"] = "anonymous program"
		return nil, errs.NewValidationError(errProgramReportedAnonymous)
	}
	if program.AnonymizeBegun() {
		ae.Details["refused"] = "anonymized program"
		return nil, errs.NewValidationError(errProgramReportedAnonymized)
	}
	if program.DeletingAt != nil {
		ae.Details["refused"] = "program being deleted"
		return nil, errs.NewValidationError(errProgramStopping)
	}
	if err := checkReportedCSVColumns(header, rows, emailColumn, dateColumn, false); err != nil {
		return nil, err
	}
	programID := program.ID.MustGet()
	people, err := s.programReportedPeople(ctx, &programID)
	if err != nil {
		return nil, err
	}
	loc := program.Location()
	now := time.Now()
	ending := program.IsClosed() || program.ClosingAt != nil
	var closedAt *time.Time
	if t, err := program.ClosedAt.Get(); err == nil {
		closedAt = &t
	}
	// match every row to a person first, a failed row keeps its reason so
	// the failures are listed in the order of the file
	matched := make([]programReportedRow, len(rows))
	wanted := &programReportedWanted{recipients: map[uuid.UUID]bool{}, pseudonyms: map[uuid.UUID]bool{}}
	for i, row := range rows {
		m := &matched[i]
		m.line = row.Line
		check := &ReportedCSVResult{}
		cell, ok := readReportedCSVRow(check, row, emailColumn, dateColumn, false)
		if !ok {
			m.fail = check.Errors[0].Reason
			continue
		}
		if cell.date != "" {
			t, ok := parseProgramReportedDate(cell.date, loc, now)
			if !ok {
				m.fail = "Invalid date " + quoteCSVValue(cell.date)
				continue
			}
			m.before = &t
		}
		// an email that matches an enrolled person is a stored, valid
		// email, so only an email that matches nobody is checked
		person, found, ambiguous := people.find(cell.email)
		if !found && !ambiguous {
			if _, err := vo.NewEmail(cell.email); err != nil {
				m.fail = "Invalid email " + quoteCSVValue(cell.email)
				continue
			}
		}
		if ambiguous {
			m.fail = quoteCSVValue(cell.email) + " matches more than one recipient, use the exact case"
			continue
		}
		if !found {
			m.fail = quoteCSVValue(cell.email) + " is not enrolled in this program"
			continue
		}
		m.email = cell.email
		m.person = person
		if person.recipientID != nil {
			wanted.recipients[*person.recipientID] = true
		}
		if person.anonymizedID != nil {
			wanted.pseudonyms[*person.anonymizedID] = true
		}
	}
	sends, err := s.programReportedSends(ctx, &programID, wanted)
	if err != nil {
		return nil, err
	}
	campaigns := map[uuid.UUID]*model.Campaign{}
	result := &ReportedCSVResult{Errors: []ReportedCSVRowError{}}
	for i := range matched {
		m := &matched[i]
		if m.fail != "" {
			result.fail(m.line, m.fail)
			continue
		}
		send := sends.latest(m.person, m.before)
		if send == nil {
			if m.before != nil {
				result.fail(m.line, quoteCSVValue(m.email)+" had no phishing template sent at or before the date")
			} else {
				result.fail(m.line, quoteCSVValue(m.email)+" had no phishing template sent")
			}
			continue
		}
		campaign, ok := campaigns[send.campaign]
		if !ok {
			campaign, err = s.CampaignRepository.GetByID(ctx, &send.campaign, &repository.CampaignOption{
				IncludeProgramManaged: true,
			})
			if err != nil {
				s.Logger.Errorw("failed to get the program send campaign of a report", "error", err, "line", m.line)
				result.fail(m.line, "Could not be saved")
				continue
			}
			campaigns[send.campaign] = campaign
		}
		reportedAt := now
		if m.before != nil {
			reportedAt = *m.before
		}
		// a file is often imported again with more rows, after a newer send
		// went out, so one report could land on two sends. A row without a
		// date has no time to tell a new report from one already recorded, so
		// it is taken as the report already recorded on any phishing send of
		// the person. A row with a date is taken as a report already recorded
		// on any phishing send on that day of the program time zone, which
		// also covers a row of today whose time was the import time.
		var from *time.Time
		to := reportedAt
		if m.before != nil {
			day := m.before.In(loc)
			start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
			from = &start
			to = start.AddDate(0, 0, 1).Add(-time.Nanosecond)
		}
		if sends.reportedBetween(m.person, from, to) {
			result.countRecorded(false, false)
			continue
		}
		if closedAt != nil && reportedAt.After(*closedAt) {
			reportedAt = *closedAt
		}
		if c, cErr := campaign.ClosedAt.Get(); cErr == nil && c.Before(reportedAt) {
			if !ending {
				// a send whose hidden campaign closed before the report takes no
				// report, the same rule as a report from a button or an inbox,
				// see Campaign.reportableCampaign
				result.fail(m.line, quoteCSVValue(m.email)+" was reported after the send closed")
				continue
			}
			// the program ended, the report is recorded at the close of the send
			reportedAt = c
		}
		recorded, err := s.CampaignService.recordReportedEvent(ctx, campaign, send.campaignRecipient(), reportedAt)
		if err != nil {
			s.Logger.Errorw("failed to record program report", "error", err, "line", m.line)
			result.fail(m.line, "Could not be saved")
			continue
		}
		// an anonymous program is refused above, so the split is shown
		result.countRecorded(recorded, false)
	}
	ae.Details["processed"] = result.Processed
	ae.Details["skipped"] = result.Skipped
	ae.Details["failed"] = result.Failed
	ae.Details["alreadyReported"] = result.AlreadyReported
	s.AuditLogAuthorized(ae)
	return result, nil
}

// programReportedRow is one row of a reported CSV matched to a person, or the
// reason it failed
type programReportedRow struct {
	line   int
	fail   string
	email  string
	person programReportedPerson
	before *time.Time
}

// programReportedPeople reads the enrolled people of a program by email,
// reading only the ids and the email of each
func (s *Program) programReportedPeople(ctx context.Context, programID *uuid.UUID) (*reportedCSVRecipients[programReportedPerson], error) {
	rows, err := s.ProgramRepository.GetEnrollmentEmails(ctx, programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	people := newReportedCSVRecipients[programReportedPerson]()
	for i := range rows {
		people.add(rows[i].Email, programReportedPerson{&rows[i].RecipientID, rows[i].AnonymizedID})
	}
	return people, nil
}

// programReportedSend is a sent phishing message a report can land on
type programReportedSend struct {
	id, campaign             uuid.UUID
	recipient, anon, notable uuid.NullUUID
	sentAt                   time.Time
}

// campaignRecipient returns the send in the form the report is recorded on,
// with the fields recordReportedEvent reads
func (sd *programReportedSend) campaignRecipient() *model.CampaignRecipient {
	cr := &model.CampaignRecipient{
		ID:         nullable.NewNullableWithValue(sd.id),
		CampaignID: nullable.NewNullableWithValue(sd.campaign),
		SentAt:     nullable.NewNullableWithValue(sd.sentAt),
	}
	if sd.recipient.Valid {
		cr.RecipientID = nullable.NewNullableWithValue(sd.recipient.UUID)
	}
	if sd.anon.Valid {
		cr.AnonymizedID = nullable.NewNullableWithValue(sd.anon.UUID)
	}
	if sd.notable.Valid {
		cr.NotableEventID = nullable.NewNullableWithValue(sd.notable.UUID)
	}
	return cr
}

// programReportedWanted names the people of an import by recipient id and by
// pseudonym
type programReportedWanted struct {
	recipients, pseudonyms map[uuid.UUID]bool
}

// programReportedSends holds the phishing sends of the people of an import,
// found by recipient id and by pseudonym
type programReportedSends struct {
	sends []programReportedSend
	byID  map[uuid.UUID][]int32
	// reports holds the times of the reports already recorded on the phishing
	// sends of each person, by recipient id and by pseudonym. Reports on
	// training messages are left out.
	reports map[uuid.UUID][]time.Time
}

// reportedBetween reports whether a report is already recorded on any
// phishing send of the person at or after from, when it is set, and at or
// before to
func (p *programReportedSends) reportedBetween(person programReportedPerson, from *time.Time, to time.Time) bool {
	for _, pid := range []*uuid.UUID{person.recipientID, person.anonymizedID} {
		if pid == nil {
			continue
		}
		for _, at := range p.reports[*pid] {
			if (from == nil || !at.Before(*from)) && !at.After(to) {
				return true
			}
		}
	}
	return false
}

// programReportedSends reads the phishing sends of a program to the wanted
// people, found by recipient id or pseudonym, through the indexes of those
// columns, so only the sends of the people in the file are read
func (s *Program) programReportedSends(ctx context.Context, programID *uuid.UUID, wanted *programReportedWanted) (*programReportedSends, error) {
	out := &programReportedSends{byID: map[uuid.UUID][]int32{}, reports: map[uuid.UUID][]time.Time{}}
	if len(wanted.recipients) == 0 && len(wanted.pseudonyms) == 0 {
		return out, nil
	}
	infos, err := s.ProgramRepository.GetCampaignInfos(ctx, programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	phishing := make(map[uuid.UUID]bool, len(infos))
	for _, info := range infos {
		if !info.IsTraining {
			phishing[info.ID] = true
		}
	}
	list := func(set map[uuid.UUID]bool) []*uuid.UUID {
		out := make([]*uuid.UUID, 0, len(set))
		for id := range set {
			out = append(out, &id)
		}
		return out
	}
	err = s.ProgramRepository.EachSentRowOfPeople(ctx, programID, list(wanted.recipients), list(wanted.pseudonyms), func(r *repository.ProgramSendRow) error {
		if !phishing[r.CampaignID] {
			return nil
		}
		i := int32(len(out.sends))
		out.sends = append(out.sends, programReportedSend{
			id:        r.ID,
			campaign:  r.CampaignID,
			recipient: nullUUIDOf(r.RecipientID),
			anon:      nullUUIDOf(r.AnonymizedID),
			notable:   nullUUIDOf(r.NotableEventID),
			sentAt:    r.SentAt,
		})
		if id := r.RecipientID; id != nil && wanted.recipients[*id] {
			out.byID[*id] = append(out.byID[*id], i)
		}
		if id := r.AnonymizedID; id != nil && wanted.pseudonyms[*id] {
			out.byID[*id] = append(out.byID[*id], i)
		}
		return nil
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	reportedEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]
	if reportedEventID == nil {
		return out, nil
	}
	reports, err := s.ProgramRepository.GetReportsOfPeople(ctx, programID, list(wanted.recipients), list(wanted.pseudonyms), reportedEventID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, r := range reports {
		// a report of a training message is not a report of a simulation
		if !phishing[r.CampaignID] {
			continue
		}
		if id := r.RecipientID; id != nil && wanted.recipients[*id] {
			out.reports[*id] = append(out.reports[*id], r.CreatedAt)
		}
		if id := r.AnonymizedID; id != nil && wanted.pseudonyms[*id] {
			out.reports[*id] = append(out.reports[*id], r.CreatedAt)
		}
	}
	return out, nil
}

// latest returns the latest phishing send to a person, matched by recipient
// id or pseudonym, sent at or before the given time when it is set
func (p *programReportedSends) latest(person programReportedPerson, before *time.Time) *programReportedSend {
	var best *programReportedSend
	for _, pid := range []*uuid.UUID{person.recipientID, person.anonymizedID} {
		if pid == nil {
			continue
		}
		for _, i := range p.byID[*pid] {
			sd := &p.sends[i]
			if before != nil && sd.sentAt.After(*before) {
				continue
			}
			if best == nil || sd.sentAt.After(best.sentAt) {
				best = sd
			}
		}
	}
	return best
}

// nullUUIDOf returns the id as a value that may be null
func nullUUIDOf(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

// parseProgramReportedDate parses a reported CSV date for a program. A value
// with a zone keeps it, a value without one is read in loc, and a date with no
// time is the end of that day in loc. The result is never later than now.
func parseProgramReportedDate(v string, loc *time.Location, now time.Time) (time.Time, bool) {
	t, hasTime, err := parseReportedDateIn(v, loc)
	if err != nil {
		return time.Time{}, false
	}
	if !hasTime {
		return reportedDayEnd(t, loc, now), true
	}
	if t.After(now) {
		return now, true
	}
	return t, true
}

// reportedDayEnd returns the end of the reported day in loc, never later than
// now. The day is the calendar date as written in the file.
func reportedDayEnd(t time.Time, loc *time.Location, now time.Time) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	end := endOfDay(day, loc)
	if end.After(now) {
		return now
	}
	return end
}
