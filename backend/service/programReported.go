package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// programReportedMaxRows bounds how many rows one reported CSV may hold
const programReportedMaxRows = 100000

// errProgramReportedAnonymous refuses a reported CSV on an anonymous program.
// A file with one named row would show whether that person was sent a phish
// and change the results by their report, so reports of an anonymous program
// come from the report inbox only.
var errProgramReportedAnonymous = errors.New("reported emails cannot be imported on an anonymous program, reports come from the report inbox")

// ProgramReportedResult is the outcome of a reported CSV upload. Matched rows
// found a phishing send, Recorded of those got a new report and the rest were
// already reported. Unmatched rows found no enrolled person or no send.
type ProgramReportedResult struct {
	Matched         *int `json:"matched"`
	Recorded        int  `json:"recorded"`
	AlreadyReported *int `json:"alreadyReported"`
	Unmatched       *int `json:"unmatched"`
}

// ProcessReportedCSV records the reports of a reported CSV on a program. Each
// email is matched to an enrolled person and the report lands on their latest
// phishing send at or before the reported time, or their latest send when the
// row has no time. dateColumn below zero means the file has no time column. A
// time without a zone is read in the program time zone and a date without a
// time means the end of that day. An anonymous program is refused.
func (s *Program) ProcessReportedCSV(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	records [][]string,
	emailColumn int,
	dateColumn int,
) (_ *ProgramReportedResult, err error) {
	ae := NewAuditEvent("Program.ReportedCSV", session)
	defer s.auditDone(ae, &err)
	ae.Details["id"] = id.String()
	program, err := s.authorizedProgram(ctx, session, id, ae, nil)
	if err != nil {
		return nil, err
	}
	if program.IsAnonymousOn() {
		ae.Details["refused"] = "anonymous program"
		s.AuditLogAuthorized(ae)
		return nil, errs.NewValidationError(errProgramReportedAnonymous)
	}
	if len(records) < 2 {
		return nil, errs.NewValidationError(errors.New("CSV must have a header row and at least one data row"))
	}
	if len(records) > programReportedMaxRows+1 {
		return nil, errs.NewValidationError(errors.New("CSV has too many rows"))
	}
	if emailColumn < 0 || emailColumn >= len(records[0]) {
		return nil, errs.NewValidationError(errors.New("invalid email column index"))
	}
	if dateColumn >= len(records[0]) {
		return nil, errs.NewValidationError(errors.New("invalid date column index"))
	}
	programID := program.ID.MustGet()
	enrollments, err := s.ProgramRepository.GetEnrollmentsByProgramID(ctx, &programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	recipientIDs := make([]*uuid.UUID, 0, len(enrollments))
	for _, e := range enrollments {
		recipientIDs = append(recipientIDs, e.RecipientID)
	}
	recipients, err := s.RecipientRepository.GetByIDs(ctx, recipientIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	emailOf := map[uuid.UUID]string{}
	for _, r := range recipients {
		if v, err := r.Email.Get(); err == nil {
			emailOf[r.ID.MustGet()] = strings.ToLower(v.String())
		}
	}
	type person struct {
		recipientID  *uuid.UUID
		anonymizedID *uuid.UUID
	}
	byEmail := map[string]person{}
	for _, e := range enrollments {
		if email, ok := emailOf[*e.RecipientID]; ok {
			byEmail[email] = person{e.RecipientID, e.AnonymizedID}
		}
	}
	loc := program.Location()
	now := time.Now()
	campaigns := map[uuid.UUID]*model.Campaign{}
	matched, alreadyReported, unmatched := 0, 0, 0
	out := &ProgramReportedResult{}
	for _, record := range records[1:] {
		if emailColumn >= len(record) {
			unmatched++
			continue
		}
		email := strings.ToLower(strings.TrimSpace(record[emailColumn]))
		p, ok := byEmail[email]
		if !ok || email == "" {
			unmatched++
			continue
		}
		var before *time.Time
		if dateColumn >= 0 && dateColumn < len(record) {
			if v := strings.TrimSpace(record[dateColumn]); v != "" {
				t, ok := parseProgramReportedDate(v, loc, now)
				if !ok {
					unmatched++
					continue
				}
				before = &t
			}
		}
		send, err := s.ProgramRepository.GetLatestPhishingSend(ctx, &programID, p.recipientID, p.anonymizedID, before)
		if err != nil {
			unmatched++
			continue
		}
		campaignID := send.CampaignID.MustGet()
		campaign, ok := campaigns[campaignID]
		if !ok {
			campaign, err = s.CampaignRepository.GetByID(ctx, &campaignID, &repository.CampaignOption{
				IncludeProgramManaged: true,
			})
			if err != nil {
				unmatched++
				continue
			}
			campaigns[campaignID] = campaign
		}
		reportedAt := now
		if before != nil {
			reportedAt = *before
		}
		recorded, err := s.CampaignService.recordReportedEvent(ctx, campaign, send, reportedAt)
		if err != nil {
			s.Logger.Errorw("failed to record program report", "error", err)
			unmatched++
			continue
		}
		matched++
		if recorded {
			out.Recorded++
		} else {
			alreadyReported++
		}
	}
	out.Matched, out.AlreadyReported, out.Unmatched = &matched, &alreadyReported, &unmatched
	ae.Details["matched"] = matched
	ae.Details["unmatched"] = unmatched
	ae.Details["recorded"] = out.Recorded
	s.AuditLogAuthorized(ae)
	return out, nil
}

// parseProgramReportedDate parses a reported CSV date for a program. A value
// with a zone keeps it, a value without one is read in loc, and a date with no
// time is the end of that day in loc, never later than now.
func parseProgramReportedDate(v string, loc *time.Location, now time.Time) (time.Time, bool) {
	for _, format := range reportedDateLayouts {
		if _, err := time.Parse(format, v); err != nil {
			continue
		}
		if reportedFormatHasZone(format) {
			t, err := time.Parse(format, v)
			return t, err == nil
		}
		t, err := time.ParseInLocation(format, v, loc)
		if err != nil {
			return time.Time{}, false
		}
		if !strings.Contains(format, "15") {
			return reportedDayEnd(t, loc, now), true
		}
		return t, true
	}
	return time.Time{}, false
}

// reportedFormatHasZone reports whether a reported date format carries a zone,
// either as an offset or as a fixed Z or offset written in the value
func reportedFormatHasZone(format string) bool {
	return strings.Contains(format, "Z07") || strings.HasSuffix(format, "Z") || strings.Contains(format, "-07") || strings.Contains(format, "+0")
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
