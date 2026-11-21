package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// ProgramEventsView is one page of the recipient events of a program. An
// anonymous program lists no events: an event names when one person acted,
// which the settled counts of an anonymous program keep hidden. Templates
// names the template of each hidden campaign on the page by campaign id.
type ProgramEventsView struct {
	Anonymous   bool                   `json:"anonymous"`
	Rows        []*model.CampaignEvent `json:"rows"`
	HasNextPage bool                   `json:"hasNextPage"`
	Templates   map[string]string      `json:"templates"`
}

// programRecipientEventPrefix starts the name of every recipient event
const programRecipientEventPrefix = "campaign_recipient_"

// programRecipientEventIDs returns the ids of the recipient events, so the
// events of the hidden campaigns themselves are left out
func programRecipientEventIDs() []string {
	out := []string{}
	for name, id := range cache.EventIDByName {
		if id != nil && strings.HasPrefix(name, programRecipientEventPrefix) {
			out = append(out, id.String())
		}
	}
	return out
}

// programEventMargin is how far the event window of a program reaches past
// its start and close. The first sends can be planned just before the start
// mark is stored, and a close stores its mark after its campaigns are closed.
const programEventMargin = 24 * time.Hour

// programEventWindow returns the time the events of a program fall in: from
// its start to its close, or with no end while it is open. A page sorted by
// time then starts at the bound on the created_at index, and the events of
// other programs and campaigns after the close are not walked. A bound that is
// not known is nil.
func programEventWindow(program *model.Program) (*time.Time, *time.Time) {
	var from, to *time.Time
	if program.StartedAt != nil {
		t := program.StartedAt.Add(-programEventMargin)
		from = &t
	}
	if closedAt, err := program.ClosedAt.Get(); err == nil {
		t := closedAt.Add(programEventMargin)
		to = &t
	}
	return from, to
}

// programEventSortColumn is the only column the events of a program are
// sorted by. A page sorted by any other column reads and sorts every event of
// the program, which takes seconds on a large program, while a page sorted by
// time walks the created_at index and stops after one page.
var programEventSortColumn = repository.TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at")

// programEventsQuery returns the query args of a page of program events,
// sorted by time whatever column was asked for
func programEventsQuery(q *vo.QueryArgs) *vo.QueryArgs {
	out := vo.QueryArgs{}
	if q != nil {
		out = *q
	}
	out.OrderBy = programEventSortColumn
	return &out
}

// GetEvents returns one page of the recipient events across the hidden
// campaigns of a program, always sorted by time, see programEventSortColumn.
// recipientID, when set, limits the events to one recipient, who must be
// enrolled in the program.
func (s *Program) GetEvents(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	q *vo.QueryArgs,
	recipientID *uuid.UUID,
) (*ProgramEventsView, error) {
	ae := NewAuditEvent("Program.GetEvents", session)
	program, err := s.authorizedProgram(ctx, session, id, ae, nil)
	if err != nil {
		return nil, err
	}
	view := &ProgramEventsView{Rows: []*model.CampaignEvent{}, Templates: map[string]string{}}
	if program.IsAnonymousOn() {
		view.Anonymous = true
		return view, nil
	}
	// while an Anonymize All runs the campaigns it has not reached still name
	// people, so no events are listed until it finished
	if programAnonymizeRunning(program) {
		return view, nil
	}
	if recipientID != nil {
		// the events of an anonymized program no longer name a person
		if program.AnonymizeBegun() {
			return view, nil
		}
		if _, err := s.ProgramRepository.GetEnrollmentByProgramRecipient(ctx, id, recipientID); err != nil {
			return nil, errs.Wrap(err)
		}
	}
	ids := programRecipientEventIDs()
	if len(ids) == 0 {
		return view, nil
	}
	from, to := programEventWindow(program)
	result, err := s.CampaignRepository.GetEventsByProgramID(ctx, id, &repository.CampaignEventOption{
		QueryArgs:    programEventsQuery(q),
		WithUser:     true,
		EventTypeIDs: ids,
		RecipientID:  recipientID,
		CreatedFrom:  from,
		CreatedTo:    to,
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	view.Rows = result.Rows
	view.HasNextPage = result.HasNextPage
	if len(view.Rows) > 0 {
		names, err := s.CampaignRepository.GetProgramTemplateNames(ctx, id)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		for _, ev := range view.Rows {
			if ev.CampaignID == nil {
				continue
			}
			if name, ok := names[*ev.CampaignID]; ok {
				view.Templates[ev.CampaignID.String()] = name
			}
		}
	}
	// no audit on read, as for the events of a campaign
	return view, nil
}

// UnlinkedAnonymousProgramSend reports whether a send belongs to an anonymous
// program and lost its link to the person after the identity retention, and
// returns its pseudonym. Such a send keeps its links working until the
// program closes: its pages are served with empty personal fields and its
// events are recorded on the pseudonym only. The person must still be
// enrolled in the program and not be deleted or disabled by SCIM. Every other
// send gives false, so a normal campaign and a named program never take this
// path.
func UnlinkedAnonymousProgramSend(
	ctx context.Context,
	programRepository *repository.Program,
	campaign *model.Campaign,
	campaignRecipient *model.CampaignRecipient,
) (uuid.UUID, bool) {
	if programRepository == nil || campaign == nil || campaignRecipient == nil {
		return uuid.UUID{}, false
	}
	if _, err := campaignRecipient.RecipientID.Get(); err == nil {
		return uuid.UUID{}, false
	}
	pseudonym, err := campaignRecipient.AnonymizedID.Get()
	if err != nil {
		return uuid.UUID{}, false
	}
	// only a sent row is unlinked by the retention
	if _, err := campaignRecipient.SentAt.Get(); err != nil {
		return uuid.UUID{}, false
	}
	if anonymous, err := campaign.IsAnonymous.Get(); err != nil || !anonymous {
		return uuid.UUID{}, false
	}
	// a campaign whose close finished has released its links
	if campaign.AnonymizedAt.IsSpecified() && !campaign.AnonymizedAt.IsNull() {
		return uuid.UUID{}, false
	}
	programID, err := campaign.ProgramID.Get()
	if err != nil {
		return uuid.UUID{}, false
	}
	program, err := programRepository.GetByID(ctx, &programID, &repository.ProgramOption{})
	if err != nil || !program.IsAnonymousOn() || program.IsClosed() {
		return uuid.UUID{}, false
	}
	// the person must still be enrolled with this pseudonym, exist and not be
	// disabled by SCIM. Deleting a recipient removes its enrollments, so the
	// links of a deleted or disabled person stop working as in a normal
	// campaign. Errors fail closed.
	live, err := programRepository.HasLiveEnrollmentByPseudonym(ctx, &programID, &pseudonym)
	if err != nil || !live {
		return uuid.UUID{}, false
	}
	return pseudonym, true
}
