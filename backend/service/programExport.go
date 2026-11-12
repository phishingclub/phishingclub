package service

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
)

// csvSafe neutralises a cell a spreadsheet would run as a formula, by putting a
// quote in front of a value that starts with = + - @ tab carriage return or new
// line, or that has one of = + - @ as its first character after white space
func csvSafe(v string) string {
	if v == "" {
		return v
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r', '\n':
		return "'" + v
	}
	trimmed := strings.TrimLeft(v, " \t\r\n\v\f")
	if trimmed != "" {
		switch trimmed[0] {
		case '=', '+', '-', '@':
			return "'" + v
		}
	}
	return v
}

func csvTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func csvCount(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// programName returns the name of a program
func programName(program *model.Program) string {
	if v, err := program.Name.Get(); err == nil {
		return v.String()
	}
	return "program"
}

// ExportCSV returns the program results as CSV rows with every cell made safe
// for spreadsheets. A normal program gives one row per sent message. An
// anonymous program gives month and template totals only, with small groups
// withheld.
func (s *Program) ExportCSV(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (_ [][]string, _ string, err error) {
	ae := NewAuditEvent("Program.Export", session)
	defer s.auditDone(ae, &err)
	ae.Details["id"] = id.String()
	program, err := s.authorizedProgram(ctx, session, id, ae, nil)
	if err != nil {
		return nil, "", err
	}
	facts, err := s.loadProgramFacts(ctx, session, program, nil)
	if err != nil {
		return nil, "", err
	}
	var rows [][]string
	if facts.anonymous {
		results := buildProgramResults(facts, nil, program.Location(), time.Now(), nil, nil)
		rows = programAggregateCSV(results)
	} else {
		rows, err = s.programSendCSV(ctx, facts)
		if err != nil {
			return nil, "", err
		}
	}
	ae.Details["anonymous"] = facts.anonymous
	ae.Details["rows"] = len(rows) - 1
	s.AuditLogAuthorized(ae)
	return rows, programName(program), nil
}

// programAggregateCSV writes the month and template rows of an anonymous program
func programAggregateCSV(results *ProgramResults) [][]string {
	rows := [][]string{{"Group", "Key", "Name", "Sent", "Clicked", "Submitted", "Reported", "Suppressed"}}
	add := func(group string, r *ProgramResultRow) {
		suppressed := ""
		if r.Suppressed {
			suppressed = "yes"
		}
		rows = append(rows, []string{
			group,
			csvSafe(r.Key),
			csvSafe(r.Name),
			csvCount(r.Sent),
			csvCount(r.Clicked),
			csvCount(r.Submitted),
			csvCount(r.Reported),
			suppressed,
		})
	}
	for _, m := range results.Months {
		add("Month", m)
	}
	for _, t := range results.Templates {
		add("Template", t)
	}
	return rows
}

// programSendCSV writes one row per sent message of a normal program
func (s *Program) programSendCSV(ctx context.Context, facts *programFacts) ([][]string, error) {
	ids := []*uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, sd := range facts.sends {
		if sd.RecipientID != nil && !seen[*sd.RecipientID] {
			seen[*sd.RecipientID] = true
			ids = append(ids, sd.RecipientID)
		}
	}
	recipients, err := s.RecipientRepository.GetByIDs(ctx, ids)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	type who struct{ email, name string }
	byID := map[uuid.UUID]who{}
	for _, r := range recipients {
		w := who{}
		if v, err := r.Email.Get(); err == nil {
			w.email = v.String()
		}
		first, last := "", ""
		if v, err := r.FirstName.Get(); err == nil {
			first = v.String()
		}
		if v, err := r.LastName.Get(); err == nil {
			last = v.String()
		}
		w.name = fullName(first, last)
		byID[r.ID.MustGet()] = w
	}
	sends := append([]*programSend(nil), facts.sends...)
	sort.SliceStable(sends, func(i, j int) bool { return sends[i].SentAt.Before(sends[j].SentAt) })
	rows := [][]string{{
		"Email", "Name", "Template", "Kind", "Sent at", "Opened at", "Clicked at",
		"Submitted at", "Reported at", "Training completed at",
	}}
	for _, sd := range sends {
		w := who{}
		if sd.RecipientID != nil {
			w = byID[*sd.RecipientID]
		}
		rows = append(rows, []string{
			csvSafe(w.email),
			csvSafe(w.name),
			csvSafe(facts.templateNames[sd.Template]),
			sd.Kind(),
			csvTime(&sd.SentAt),
			csvTime(sd.ReadAt),
			csvTime(sd.ClickedAt),
			csvTime(sd.SubmittedAt),
			csvTime(sd.ReportedAt),
			csvTime(sd.TrainingCompletedAt),
		})
	}
	return rows, nil
}
