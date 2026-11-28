package service

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// the recipient fields the results of a named program can be grouped by
const (
	ProgramGroupByDepartment = "department"
	ProgramGroupByPosition   = "position"
)

// programResultGroupsMax caps the rows of a grouping, the groups with the
// most sends are kept
const programResultGroupsMax = 500

// the names of the rows that hold no group value. A recipient with an empty
// field is named as on the campaign page.
const (
	programGroupUnspecified = "(unspecified)"
	programGroupDeleted     = "Deleted recipients"
)

// IsValidProgramGroupBy reports whether by is a field the results can be
// grouped by
func IsValidProgramGroupBy(by string) bool {
	return by == ProgramGroupByDepartment || by == ProgramGroupByPosition || by == ProgramGroupByLanguage
}

// ProgramResultGroups is the results of a named program since the start,
// grouped by the current department or position of each recipient, or by the
// language of each send. The rows count sends, as the template rows do. An
// anonymous program, or one whose Anonymize All began, has no department or
// position groups and Anonymous is set. By language it has rows with the
// sizes only and Anonymous is set, see programLanguageGroups.
type ProgramResultGroups struct {
	By        string              `json:"by"`
	Anonymous bool                `json:"anonymous"`
	Rows      []*ProgramResultRow `json:"rows"`
	// Omitted counts the groups left out over programResultGroupsMax
	Omitted int `json:"omitted"`
}

// GetResultGroups returns the results of a program grouped by the current
// department or position of each recipient, or by language
func (s *Program) GetResultGroups(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	by string,
) (*ProgramResultGroups, error) {
	if !IsValidProgramGroupBy(by) {
		return nil, errs.NewValidationError(errProgramGroupBy)
	}
	ae := NewAuditEvent("Program.GetResultGroups", session)
	program, err := s.authorizedProgram(ctx, session, id, ae, &repository.ProgramOption{WithSteps: true})
	if err != nil {
		return nil, err
	}
	if by == ProgramGroupByLanguage {
		return s.programLanguageGroups(ctx, program, time.Now())
	}
	return s.programResultGroups(ctx, program, by, time.Now())
}

// programResultGroups computes the grouped results of a loaded program at now
func (s *Program) programResultGroups(
	ctx context.Context,
	program *model.Program,
	by string,
	now time.Time,
) (*ProgramResultGroups, error) {
	out := &ProgramResultGroups{By: by, Rows: []*ProgramResultRow{}}
	// a group can be small enough to point at one person, and the sends of
	// an anonymized program name no one
	if program.IsAnonymousOn() || programPersonByPseudonym(program) {
		out.Anonymous = true
		return out, nil
	}
	programID := program.ID.MustGet()
	kinds, err := s.stepKinds(ctx, program)
	if err != nil {
		return nil, err
	}
	people, err := s.ProgramRepository.GetEnrollmentGroups(ctx, &programID)
	if err != nil {
		return nil, err
	}
	groupOf := make(map[uuid.UUID]string, len(people))
	for _, p := range people {
		v := p.Department
		if by == ProgramGroupByPosition {
			v = p.Position
		}
		groupOf[p.RecipientID] = v.String
	}
	facts, err := s.programFactsFor(ctx, program, now)
	if err != nil {
		return nil, err
	}
	facts.mu.RLock()
	defer facts.mu.RUnlock()
	hasPhishing := kinds.phishing || facts.hasPhishing
	hasTraining := kinds.training || facts.hasTraining
	rows, omitted := facts.groupRows(groupOf, hasPhishing, hasTraining)
	out.Rows = rows
	out.Omitted = omitted
	return out, nil
}

// groupRows counts the sends of a named program by the group of each person.
// groupOf gives the group of a recipient, a person not in it is a deleted
// recipient. The rows are sorted most sent first, then by name, and capped
// at programResultGroupsMax. The caller holds the read lock.
func (st *programFactsStore) groupRows(
	groupOf map[uuid.UUID]string,
	hasPhishing, hasTraining bool,
) ([]*ProgramResultRow, int) {
	const deleted = -1
	// the group index of each person index
	groups := map[string]int{}
	names := []string{}
	personGroup := make([]int, len(st.persons))
	for id, i := range st.persons {
		v, ok := groupOf[id]
		if !ok {
			personGroup[i] = deleted
			continue
		}
		g, ok := groups[v]
		if !ok {
			g = len(names)
			groups[v] = g
			names = append(names, v)
		}
		personGroup[i] = g
	}
	counts := make([]programRowSends, len(names))
	var deletedCounts programRowSends
	for i := range st.sends {
		sd := &st.sends[i]
		g := personGroup[sd.person]
		if g == deleted {
			deletedCounts.add(sd)
			continue
		}
		counts[g].add(sd)
	}
	type groupRow struct {
		row  *ProgramResultRow
		sent int
	}
	row := func(key, name string, c *programRowSends) groupRow {
		r := &ProgramResultRow{Key: key, Name: name}
		if hasPhishing {
			r.Sent, r.Clicked, r.Submitted, r.Reported = intPtr(c.sent), intPtr(c.clicked), intPtr(c.submitted), intPtr(c.reported)
		}
		if hasTraining {
			r.TrainingSent = intPtr(c.trainingSent)
			r.TrainingStarted = intPtr(c.trainingStarted)
			r.TrainingCompleted = intPtr(c.trainingCompleted)
		}
		return groupRow{r, c.sent + c.trainingSent}
	}
	list := make([]groupRow, 0, len(names))
	for g, v := range names {
		if !counts[g].used {
			continue
		}
		key, name := "group:"+v, v
		if v == "" {
			key, name = "unspecified", programGroupUnspecified
		}
		list = append(list, row(key, name, &counts[g]))
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].sent != list[j].sent {
			return list[i].sent > list[j].sent
		}
		return list[i].row.Name < list[j].row.Name
	})
	omitted := 0
	if len(list) > programResultGroupsMax {
		omitted = len(list) - programResultGroupsMax
		list = list[:programResultGroupsMax]
	}
	out := make([]*ProgramResultRow, 0, len(list)+1)
	for _, r := range list {
		out = append(out, r.row)
	}
	// the sends of deleted recipients come last, so the rows add up to the
	// totals
	if deletedCounts.used {
		out = append(out, row("deleted", programGroupDeleted, &deletedCounts).row)
	}
	return out, omitted
}
