package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// audienceProgram creates a program that has not started, scheduled when
// startAt is set and a draft otherwise
func (p *programTest) audienceProgram(companyID *uuid.UUID, startAt *time.Time) uuid.UUID {
	p.t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	p.create(&database.Program{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "audience " + id.String(),
		CompanyID: companyID, LoopMode: model.ProgramLoopLoop, OrderMode: model.ProgramOrderSequential,
		StartAt: startAt,
	})
	return id
}

// audiencePerson stores a recipient of a company with a name and department
func (p *programTest) audiencePerson(companyID *uuid.UUID, email, first, department string, scimDisabled bool) uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	r := &database.Recipient{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, Email: &email,
		FirstName: first, Department: department, CompanyID: companyID,
	}
	if scimDisabled {
		r.ScimSoftDeletedAt = &now
	}
	p.create(r)
	return id
}

// TestProgramAudienceNotStarted checks a draft or scheduled program lists the
// members of its groups as not started people: static and dynamic groups,
// SCIM disabled left out, each person once, the dynamic rule kept to the
// group company, paged, searched and sorted, and nothing per person for an
// anonymous program
func TestProgramAudienceNotStarted(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	now := time.Now().UTC()
	companyA, companyB := uuid.New(), uuid.New()

	staticID, dynamicID := uuid.New(), uuid.New()
	p.create(&database.RecipientGroup{ID: &staticID, CreatedAt: &now, UpdatedAt: &now, Name: "static", CompanyID: &companyA})
	p.create(&database.RecipientGroup{
		ID: &dynamicID, CreatedAt: &now, UpdatedAt: &now, Name: "dynamic", CompanyID: &companyA,
		IsDynamic: true, FilterField: "department", FilterValue: "sales",
	})
	anna := p.audiencePerson(&companyA, "anna@example.com", "Anna", "", false)
	bert := p.audiencePerson(&companyA, "bert@example.com", "Bert", "sales", false)
	carl := p.audiencePerson(&companyA, "carl@example.com", "Carl", "sales", false)
	gone := p.audiencePerson(&companyA, "gone@example.com", "Gone", "", true)
	// SCIM disabled in the dynamic group, and a sales person of another company
	p.audiencePerson(&companyA, "gonesales@example.com", "Gonesales", "sales", true)
	p.audiencePerson(&companyB, "other@example.com", "Other", "sales", false)
	p.addToGroup(staticID, anna)
	p.addToGroup(staticID, bert)
	p.addToGroup(staticID, gone)

	programID := p.audienceProgram(&companyA, nil)
	p.assignGroup(programID, staticID)
	p.assignGroup(programID, dynamicID)

	view, err := p.svc.GetRecipientsView(p.ctx, session, &programID, &vo.QueryArgs{
		OrderBy: repository.ProgramEnrollmentSortColumns["email"],
	})
	if err != nil {
		t.Fatalf("audience: %v", err)
	}
	want := []uuid.UUID{anna, bert, carl}
	if view.Count != 3 || len(view.Rows) != 3 || view.Total != 0 {
		t.Fatalf("expected 3 people once each, got count %d rows %+v", view.Count, view.Rows)
	}
	for i, row := range view.Rows {
		if row.RecipientID != want[i].String() {
			t.Fatalf("row %d: expected %s, got %s", i, want[i], row.RecipientID)
		}
		if row.Status != ProgramAudienceNotStarted || row.Sends != 0 || row.LastTemplateName != nil ||
			row.NextTemplateName != nil || row.NextDueAt != nil {
			t.Fatalf("expected a not started row, got %+v", row)
		}
	}
	if view.Rows[0].Name != "Anna" || view.Rows[0].Email != "anna@example.com" {
		t.Fatalf("expected the recipient name and email, got %+v", view.Rows[0])
	}

	// paging, descending sort and search
	view, err = p.svc.GetRecipientsView(p.ctx, session, &programID, &vo.QueryArgs{
		OrderBy: repository.ProgramEnrollmentSortColumns["name"], Desc: true, Limit: 2,
	})
	if err != nil || view.Count != 3 || len(view.Rows) != 2 || !view.HasNextPage || view.Rows[0].Name != "Carl" {
		t.Fatalf("expected first page of 2 starting with Carl, got %+v (%v)", view, err)
	}
	view, err = p.svc.GetRecipientsView(p.ctx, session, &programID, &vo.QueryArgs{
		OrderBy: repository.ProgramEnrollmentSortColumns["name"], Desc: true, Limit: 2, Offset: 2,
	})
	if err != nil || len(view.Rows) != 1 || view.HasNextPage || view.Rows[0].Name != "Anna" {
		t.Fatalf("expected the last page with Anna, got %+v (%v)", view, err)
	}
	view, err = p.svc.GetRecipientsView(p.ctx, session, &programID, &vo.QueryArgs{Search: "ber"})
	if err != nil || view.Count != 1 || len(view.Rows) != 1 || view.Rows[0].RecipientID != bert.String() {
		t.Fatalf("expected search to find bert, got %+v (%v)", view, err)
	}
	// a sort that does not apply to people not yet enrolled is left out
	view, err = p.svc.GetRecipientsView(p.ctx, session, &programID, &vo.QueryArgs{
		OrderBy: repository.ProgramEnrollmentSortColumns["next_due_at"],
	})
	if err != nil || view.Count != 3 {
		t.Fatalf("expected the status sort to be ignored, got %+v (%v)", view, err)
	}

	// a scheduled program lists the same people
	future := now.Add(48 * time.Hour)
	scheduledID := p.audienceProgram(&companyA, &future)
	p.assignGroup(scheduledID, dynamicID)
	view, err = p.svc.GetRecipientsView(p.ctx, session, &scheduledID, &vo.QueryArgs{})
	if err != nil || view.Count != 2 || len(view.Rows) != 2 {
		t.Fatalf("expected the 2 sales people of company A, got %+v (%v)", view, err)
	}

	// an anonymous program gives no people
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	view, err = p.svc.GetRecipientsView(p.ctx, session, &programID, &vo.QueryArgs{})
	if err != nil || !view.Anonymous || len(view.Rows) != 0 || view.Count != 0 {
		t.Fatalf("expected no people for an anonymous program, got %+v (%v)", view, err)
	}
	p.setProgram(programID, map[string]any{"is_anonymous": false})

	// a started program lists its enrollments, not its groups
	started := now.Add(-time.Hour)
	p.setProgram(scheduledID, map[string]any{"start_at": started, "started_at": started})
	view, err = p.svc.GetRecipientsView(p.ctx, session, &scheduledID, &vo.QueryArgs{})
	if err != nil || view.Count != 0 || len(view.Rows) != 0 {
		t.Fatalf("expected a started program without enrollments to list nobody, got %+v (%v)", view, err)
	}

	if _, err := p.svc.GetRecipientsView(p.ctx, unauthorizedSession(t), &programID, &vo.QueryArgs{}); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected an unauthorized session refused, got %v", err)
	}
}
