package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// seedProgramEventsRead makes a program with two hidden campaigns and events
// that share a time, an event with no recipient, an event of a recipient that
// is gone and recipients whose fields start like a formula. Another program
// and a plain campaign get events that must stay out.
func seedProgramEventsRead(t *testing.T, db *gorm.DB) (*uuid.UUID, []*uuid.UUID) {
	t.Helper()
	sentID, clickID, closedID := ptrUUID(), ptrUUID(), ptrUUID()
	for id, name := range map[*uuid.UUID]string{
		sentID:   "campaign_recipient_message_sent",
		clickID:  "campaign_recipient_page_visited",
		closedID: "campaign_closed",
	} {
		if err := db.Create(&database.Event{ID: id, CreatedAt: ptrNow(), Name: name}).Error; err != nil {
			t.Fatalf("create event type: %v", err)
		}
	}
	recipient := func(first, last, email string) *uuid.UUID {
		id := ptrUUID()
		row := &database.Recipient{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), FirstName: first, LastName: last}
		if email != "" {
			row.Email = &email
		}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create recipient: %v", err)
		}
		return id
	}
	alice := recipient("=Alice", "", "alice@example.com")
	bob := recipient("Bob", "+Builder", "")
	gone := ptrUUID()
	programID := createProgram(t, db, "events", nil)
	otherProgram := createProgram(t, db, "other", nil)
	campaigns := []*uuid.UUID{ptrUUID(), ptrUUID()}
	for _, id := range campaigns {
		createHiddenCampaign(t, db, id, programID)
	}
	otherHidden, plain := ptrUUID(), ptrUUID()
	createHiddenCampaign(t, db, otherHidden, otherProgram)
	if err := db.Create(&database.Campaign{ID: plain, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "plain"}).Error; err != nil {
		t.Fatalf("create campaign: %v", err)
	}
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	// stored as the app stores events, RFC 3339 UTC text
	add := func(campaignID, recipientID, eventID *uuid.UUID, at time.Time, data string) {
		row := map[string]any{
			"id": ptrUUID().String(), "created_at": utils.RFC3339UTC(at), "updated_at": utils.RFC3339UTC(at),
			"data": data, "campaign_id": campaignID.String(), "event_id": eventID.String(),
			"user_agent": "-agent", "ip_address": "10.0.0.1",
		}
		if recipientID != nil {
			row["recipient_id"] = recipientID.String()
		}
		if err := db.Model(&database.CampaignEvent{}).Create(row).Error; err != nil {
			t.Fatalf("add event: %v", err)
		}
	}
	for i := 0; i < 12; i++ {
		// every third pair of events shares a time
		at := base.Add(time.Duration(i/3) * time.Minute)
		add(campaigns[i%2], alice, sentID, at, "@data")
		add(campaigns[(i+1)%2], bob, clickID, at, "")
	}
	add(campaigns[0], nil, closedID, base.Add(time.Minute), "closed")
	add(campaigns[1], gone, clickID, base.Add(2*time.Minute), "")
	add(otherHidden, alice, sentID, base, "")
	add(plain, alice, sentID, base, "")
	return programID, []*uuid.UUID{sentID, clickID}
}

// exportLine is an event as the company export writes it
type exportLine struct {
	campaignID, eventID uuid.UUID
	createdAt           string
	first, last, email  string
	data, userAgent, ip string
}

// TestProgramEventsExportMatchesEventRead checks the streamed export rows are
// the events the program event read returns, in the same order newest and
// oldest first, with the same fields, and that recipients that are missing
// read as no recipient
func TestProgramEventsExportMatchesEventRead(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Campaign{DB: db}
	programID, _ := seedProgramEventsRead(t, db)
	for _, desc := range []bool{true, false} {
		exportMatchesEventRead(t, ctx, r, programID, desc)
	}
}

// exportMatchesEventRead compares the export rows of a program with a read of
// its events in one direction
func exportMatchesEventRead(t *testing.T, ctx context.Context, r *Campaign, programID *uuid.UUID, desc bool) {
	t.Helper()
	read, err := r.GetEventsByProgramID(ctx, programID, &CampaignEventOption{
		QueryArgs: &vo.QueryArgs{OrderBy: TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"), Desc: desc},
		WithUser:  true,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := []exportLine{}
	for _, e := range read.Rows {
		line := exportLine{
			campaignID: *e.CampaignID, eventID: *e.EventID, createdAt: utils.CSVFromDate(e.CreatedAt),
			first: "anonymized", last: "anonymized", email: "anonymized",
			data: e.Data.String(), userAgent: e.UserAgent.String(), ip: e.IP.String(),
		}
		if e.Recipient != nil {
			line.first = utils.NullableToString(e.Recipient.FirstName)
			line.last = utils.NullableToString(e.Recipient.LastName)
			line.email = utils.NullableToString(e.Recipient.Email)
		}
		want = append(want, line)
	}
	got := []exportLine{}
	err = r.EachProgramEventForExport(ctx, programID, desc, func(e *ProgramEventExportRow) error {
		line := exportLine{
			campaignID: e.CampaignID, eventID: e.EventID, createdAt: utils.CSVFromDate(&e.CreatedAt),
			first: "anonymized", last: "anonymized", email: "anonymized",
			data: e.Data, userAgent: e.UserAgent, ip: e.IP,
		}
		if e.HasRecipient {
			line.first, line.last, line.email = e.FirstName, e.LastName, e.Email
		}
		got = append(got, line)
		return nil
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(want) != 26 || len(got) != len(want) {
		t.Fatalf("export has %d rows, read has %d, want 26", len(got), len(want))
	}
	anonymized := 0
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("desc %v row %d differs:\nexport %+v\nread   %+v", desc, i, got[i], want[i])
		}
		if got[i].first == "anonymized" {
			anonymized++
		}
	}
	if anonymized != 2 {
		t.Fatalf("expected the event with no recipient and the gone recipient anonymized, got %d", anonymized)
	}
}

// TestProgramEventsBothPlans checks a page of program events is the same
// whether the program events are read through the campaign index and sorted
// or found by a walk of the created_at index, newest and oldest first, over
// every page and with a time window
func TestProgramEventsBothPlans(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Campaign{DB: db}
	programID, types := seedProgramEventsRead(t, db)
	defer func(v int) { programEventsSortLimit = v }(programEventsSortLimit)
	defer func(v int) { programEventsWalkLimit = v }(programEventsWalkLimit)

	from := time.Now().Add(-time.Hour).Add(time.Minute)
	pages := func(limit, walk int, desc bool, window bool) [][]uuid.UUID {
		out := [][]uuid.UUID{}
		for offset := 0; ; offset += 5 {
			options := &CampaignEventOption{
				QueryArgs: &vo.QueryArgs{
					OrderBy: TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"),
					Desc:    desc, Limit: 5, Offset: offset,
				},
				WithUser:     true,
				EventTypeIDs: []string{types[0].String(), types[1].String()},
			}
			if window {
				options.CreatedFrom = &from
			}
			programEventsSortLimit = limit
			programEventsWalkLimit = walk
			res, err := r.GetEventsByProgramID(ctx, programID, options)
			if err != nil {
				t.Fatalf("page: %v", err)
			}
			ids := []uuid.UUID{}
			for _, e := range res.Rows {
				ids = append(ids, *e.ID)
			}
			out = append(out, ids)
			if !res.HasNextPage {
				return out
			}
		}
	}
	for _, desc := range []bool{true, false} {
		for _, window := range []bool{false, true} {
			sorted := pages(1000, 50000, desc, window)
			// a walk cut short finds no full page on some pages and reads
			// those through the campaign index, the pages stay the same
			for _, walk := range []int{0, 3, 12, 50000} {
				if w := pages(0, walk, desc, window); !equalPages(sorted, w) {
					t.Fatalf("desc %v window %v walk %d: pages differ", desc, window, walk)
				}
			}
			walked := pages(0, 50000, desc, window)
			if len(sorted) != len(walked) {
				t.Fatalf("desc %v window %v: %d pages sorted, %d walked", desc, window, len(sorted), len(walked))
			}
			n := 0
			for i := range sorted {
				if len(sorted[i]) != len(walked[i]) {
					t.Fatalf("desc %v window %v page %d: sizes differ", desc, window, i)
				}
				for j := range sorted[i] {
					if sorted[i][j] != walked[i][j] {
						t.Fatalf("desc %v window %v page %d row %d differs", desc, window, i, j)
					}
				}
				n += len(sorted[i])
			}
			if !window && n != 25 {
				t.Fatalf("desc %v: expected the 25 recipient events, got %d", desc, n)
			}
			if window && (n == 0 || n >= 25) {
				t.Fatalf("desc %v: the window kept %d events", desc, n)
			}
		}
	}
	// the count stops at the limit
	programEventsSortLimit = 24
	if few, err := r.programHasFewEvents(ctx, programID, nil); err != nil || few {
		t.Fatalf("26 events are not few under a limit of 24: %v %v", few, err)
	}
	programEventsSortLimit = 26
	if few, err := r.programHasFewEvents(ctx, programID, nil); err != nil || !few {
		t.Fatalf("26 events are few under a limit of 26: %v %v", few, err)
	}
}

// equalPages reports whether two reads gave the same rows on the same pages
func equalPages(a, b [][]uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}
