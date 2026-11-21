package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
)

// groupPerson adds an enrolled recipient with a department and position and
// one sent message to the fixture campaign
func (f *resultsFixture) groupPerson(department, position string) uuid.UUID {
	f.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	email := id.String() + "@example.com"
	f.create(&database.Recipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, Email: &email, Department: department, Position: position})
	f.enroll(f.programID, id, 0)
	f.send(f.campaignID, id, true)
	return id
}

func (f *resultsFixture) groups(by string) *ProgramResultGroups {
	f.t.Helper()
	g, err := f.svc.programResultGroups(f.ctx, f.loadProgram(f.programID), by, time.Now())
	if err != nil {
		f.t.Fatalf("groups: %v", err)
	}
	return g
}

func rowByName(rows []*ProgramResultRow, name string) *ProgramResultRow {
	for _, r := range rows {
		if r.Name == name {
			return r
		}
	}
	return nil
}

// TestProgramResultGroups checks the sends of a named program are counted by
// the current department or position of each person, with an empty value in
// one row and the sends of a deleted recipient in a last row, so the rows add
// up to every send
func TestProgramResultGroups(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	a := f.groupPerson("Finance", "Accountant")
	f.groupPerson("Finance", "Controller")
	f.groupPerson("Sales", "Accountant")
	f.groupPerson("", "")
	f.event(a, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	// a send whose recipient was deleted keeps only its pseudonym
	now := time.Now().UTC()
	id, pseudonym := uuid.New(), uuid.New()
	f.create(&database.CampaignRecipient{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, SentAt: &now,
		CampaignID: &f.campaignID, AnonymizedID: &pseudonym,
	})

	g := f.groups(ProgramGroupByDepartment)
	if g.Anonymous || g.Omitted != 0 {
		t.Fatalf("anonymous %v omitted %d", g.Anonymous, g.Omitted)
	}
	finance := rowByName(g.Rows, "Finance")
	if finance == nil || *finance.Sent != 2 || *finance.Clicked != 1 {
		t.Fatalf("finance row %+v", finance)
	}
	if g.Rows[0].Name != "Finance" {
		t.Fatalf("the most sent group comes first, got %q", g.Rows[0].Name)
	}
	if r := rowByName(g.Rows, programGroupUnspecified); r == nil || *r.Sent != 1 {
		t.Fatalf("unspecified row %+v", r)
	}
	last := g.Rows[len(g.Rows)-1]
	if last.Name != programGroupDeleted || *last.Sent != 1 {
		t.Fatalf("last row %q, want the deleted recipients row with 1 send", last.Name)
	}
	total := 0
	for _, r := range g.Rows {
		total += *r.Sent
	}
	if total != 5 {
		t.Fatalf("rows add up to %d sends, want 5", total)
	}

	g = f.groups(ProgramGroupByPosition)
	if r := rowByName(g.Rows, "Accountant"); r == nil || *r.Sent != 2 || *r.Clicked != 1 {
		t.Fatalf("accountant row %+v", r)
	}

	// a department changed later counts under the new one
	f.db.Model(&database.Recipient{}).Where("id = ?", a).Update("department", "Legal")
	g = f.groups(ProgramGroupByDepartment)
	if r := rowByName(g.Rows, "Legal"); r == nil || *r.Sent != 1 || *r.Clicked != 1 {
		t.Fatalf("legal row %+v", r)
	}
}

// TestProgramResultGroupsAnonymous checks an anonymous program, and a named
// one whose Anonymize All began, has no groups
func TestProgramResultGroupsAnonymous(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	f.groupPerson("Finance", "Accountant")
	f.setProgram(f.programID, map[string]any{"is_anonymous": true})
	if g := f.groups(ProgramGroupByDepartment); !g.Anonymous || len(g.Rows) != 0 {
		t.Fatalf("anonymous program: anonymous %v rows %d", g.Anonymous, len(g.Rows))
	}
	f.setProgram(f.programID, map[string]any{"is_anonymous": false, "anonymizing_at": time.Now().UTC()})
	if g := f.groups(ProgramGroupByDepartment); !g.Anonymous || len(g.Rows) != 0 {
		t.Fatalf("anonymize began: anonymous %v rows %d", g.Anonymous, len(g.Rows))
	}
}

// TestProgramResultGroupsBy checks only department and position are taken
func TestProgramResultGroupsBy(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	for _, by := range []string{"", "template", "email", "department; drop"} {
		_, err := f.svc.GetResultGroups(f.ctx, nil, &f.programID, by)
		var ve errs.ValidationError
		if err == nil || !errors.As(err, &ve) {
			t.Fatalf("by %q: err %v, want a validation error", by, err)
		}
	}
}
