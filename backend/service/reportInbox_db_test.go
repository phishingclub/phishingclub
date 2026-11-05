package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// fakeGraph serves the Graph and login endpoints the report inbox uses
type fakeGraph struct {
	mu          sync.Mutex
	token       string
	mailbox     string
	readable    map[string]bool
	messages    []fakeGraphMessage
	listCalls   int
	tokenCalls  int
	unknownPath []string
	// bodyFetches counts attached item fetches that included the body, per message
	bodyFetches map[string]int
	// valueFetches counts raw downloads of attachments
	valueFetches int
	// attachmentStatus makes the attachment list of a message fail with a status
	attachmentStatus map[string]int
}

type fakeGraphMessage struct {
	id       string
	received time.Time
	from     string
	// attachment is the MIME of an attached item, empty for a plain forward
	attachment string
	// body is the html body of the message itself
	body string
	// itemType is the type of the attached item, a message when empty
	itemType string
	// outerMessageID is the Message-ID of the message itself, a new one when empty
	outerMessageID string
	// references is a Message-ID the message refers to, as a forward does
	references string
}

func fakeJWT(roles []string) string {
	claims, _ := json.Marshal(map[string]any{"roles": roles})
	return "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
}

func (f *fakeGraph) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			f.tokenCalls++
			_ = r.ParseForm()
			if r.Form.Get("client_secret") != "good-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"AADSTS7000215: Invalid client secret provided."}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"` + f.token + `","expires_in":3600}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := strings.TrimPrefix(r.URL.EscapedPath(), "/v1.0/users/")
		parts := strings.Split(path, "/")
		mailbox := strings.ReplaceAll(parts[0], "%40", "@")
		if !f.readable[mailbox] {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"ErrorAccessDenied","message":"Access is denied."}}`))
			return
		}
		find := func(id string) *fakeGraphMessage {
			for i := range f.messages {
				if f.messages[i].id == id {
					return &f.messages[i]
				}
			}
			return nil
		}
		switch {
		case len(parts) == 2 && parts[1] == "messages":
			f.listCalls++
			since, _ := time.Parse(time.RFC3339, strings.TrimPrefix(r.URL.Query().Get("$filter"), "receivedDateTime ge "))
			out := []map[string]any{}
			for _, m := range f.messages {
				if m.received.Before(since) {
					continue
				}
				out = append(out, map[string]any{
					"id":               m.id,
					"receivedDateTime": m.received.UTC().Format(time.RFC3339),
					"hasAttachments":   m.attachment != "",
					"from":             map[string]any{"emailAddress": map[string]any{"address": m.from}},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"value": out})
		case len(parts) == 3 && parts[1] == "mailFolders" && parts[2] == "inbox":
			_, _ = w.Write([]byte(`{"id":"inbox"}`))
		case len(parts) == 4 && parts[3] == "attachments":
			if status := f.attachmentStatus[parts[2]]; status != 0 {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"code":"Failed","message":"failed"}}`))
				return
			}
			m := find(parts[2])
			out := []map[string]any{}
			if m != nil && m.attachment != "" {
				out = append(out, map[string]any{
					"@odata.type": "#microsoft.graph.itemAttachment",
					"id":          "att-" + m.id,
					"name":        "reported",
					"contentType": "",
					"size":        len(m.attachment),
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"value": out})
		case len(parts) == 5 && parts[3] == "attachments":
			m := find(parts[2])
			if m == nil || m.attachment == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			msg, err := mail.ReadMessage(strings.NewReader(m.attachment))
			if err != nil {
				t.Errorf("fake attachment: %v", err)
				return
			}
			expand := r.URL.Query().Get("$expand")
			// like Graph, the item is an outlookItem, so a message field is only
			// accepted with the message type in front of it
			if i := strings.Index(expand, "($select="); i >= 0 {
				for _, field := range strings.Split(strings.TrimSuffix(expand[i+len("($select="):], ")"), ",") {
					if !strings.HasPrefix(field, "microsoft.graph.message/") {
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"error":{"code":"BadRequest","message":"Could not find a property named '` + field + `' on type 'microsoft.graph.outlookItem'."}}`))
						return
					}
				}
			}
			if m.itemType != "" {
				// a cast select on an item of another type returns none of the fields
				_ = json.NewEncoder(w).Encode(map[string]any{"@odata.type": "#microsoft.graph.itemAttachment", "item": map[string]any{"@odata.type": m.itemType}})
				return
			}
			from, _ := mail.ParseAddress(msg.Header.Get("From"))
			item := map[string]any{
				"@odata.type":       "#microsoft.graph.message",
				"internetMessageId": msg.Header.Get("Message-Id"),
				"from":              map[string]any{"emailAddress": map[string]any{"address": from.Address}},
			}
			if strings.Contains(expand, "body") || !strings.Contains(expand, "$select") {
				body, _ := io.ReadAll(msg.Body)
				item["body"] = map[string]any{"contentType": "html", "content": string(body)}
				item["internetMessageHeaders"] = []map[string]string{{"name": "Subject", "value": msg.Header.Get("Subject")}}
				if f.bodyFetches == nil {
					f.bodyFetches = map[string]int{}
				}
				f.bodyFetches[m.id]++
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"@odata.type": "#microsoft.graph.itemAttachment", "item": item})
		case len(parts) == 6 && parts[3] == "attachments" && parts[5] == "$value":
			f.valueFetches++
			m := find(parts[2])
			if m == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(m.attachment))
		case len(parts) == 3 && parts[1] == "messages":
			m := find(parts[2])
			if m == nil {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"code":"ErrorItemNotFound","message":"The specified object was not found in the store."}}`))
				return
			}
			if r.URL.Query().Get("$select") == graphMessageFields {
				// a pending message read again by its id
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":               m.id,
					"receivedDateTime": m.received.UTC().Format(time.RFC3339),
					"hasAttachments":   m.attachment != "",
					"from":             map[string]any{"emailAddress": map[string]any{"address": m.from}},
				})
				return
			}
			if r.URL.Query().Get("$select") != "internetMessageId,internetMessageHeaders,body" {
				t.Errorf("message must be fetched without attachments, got select %q", r.URL.Query().Get("$select"))
			}
			outerID := "<outer-" + m.id + "@company.example>"
			if m.outerMessageID != "" {
				outerID = "<" + m.outerMessageID + ">"
			}
			headers := []map[string]string{{"name": "Subject", "value": "FW: report"}}
			if m.references != "" {
				headers = append(headers, map[string]string{"name": "References", "value": "<" + m.references + ">"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"internetMessageId":      outerID,
				"internetMessageHeaders": headers,
				"body":                   map[string]string{"contentType": "html", "content": m.body},
			})
		default:
			f.unknownPath = append(f.unknownPath, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

type reportInboxFixture struct {
	t        *testing.T
	ctx      context.Context
	db       *gorm.DB
	svc      *ReportInbox
	graph    *fakeGraph
	c1       uuid.UUID
	c2       uuid.UUID
	reported *uuid.UUID
}

func newReportInboxFixture(t *testing.T) *reportInboxFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "reportinbox.db") + "?_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Company{},
		&database.Campaign{},
		&database.CampaignRecipient{},
		&database.Recipient{},
		&database.CampaignEvent{},
		&database.Event{},
		&database.CampaignWebhook{},
		&database.OAuthProvider{},
		&database.ReportInbox{},
		&database.ReportInboxMessage{},
		&database.Email{},
		&database.Domain{},
		&database.CampaignTemplate{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	reported := uuid.New()
	cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED] = &reported
	cache.EventNameByID[reported.String()] = data.EVENT_CAMPAIGN_RECIPIENT_REPORTED

	logger := zap.NewNop().Sugar()
	campaignSvc := &Campaign{
		Common:                      Common{Logger: logger},
		CampaignRepository:          &repository.Campaign{DB: db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: db},
		RecipientRepository:         &repository.Recipient{DB: db},
	}
	fg := &fakeGraph{
		token:    fakeJWT(nil),
		mailbox:  "reports@c1.example",
		readable: map[string]bool{"reports@c1.example": true},
	}
	srv := httptest.NewServer(fg.handler(t))
	t.Cleanup(srv.Close)
	svc := &ReportInbox{
		Common:                      Common{Logger: logger},
		ReportInboxRepository:       &repository.ReportInbox{DB: db},
		CompanyRepository:           &repository.Company{DB: db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: db},
		CampaignService:             campaignSvc,
		graph: &graphMailClient{
			HTTP:      srv.Client(),
			GraphBase: srv.URL + "/v1.0",
			LoginBase: srv.URL,
		},
	}
	f := &reportInboxFixture{
		t: t, ctx: context.Background(), db: db, svc: svc, graph: fg,
		c1: uuid.New(), c2: uuid.New(), reported: &reported,
	}
	for _, id := range []uuid.UUID{f.c1, f.c2} {
		f.create(&database.Company{}, map[string]any{"id": id, "name": "company " + id.String()})
	}
	// the simulation emails of company 1 are sent from sender.example
	f.create(&database.Email{}, map[string]any{
		"id": uuid.New(), "name": "it notice", "content": "", "add_tracking_pixel": false,
		"mail_from": "bounce@sender.example", "subject": "s", "from": "IT <it@sender.example>", "company_id": f.c1,
	})
	return f
}

func (f *reportInboxFixture) create(model any, row map[string]any) {
	f.t.Helper()
	now := utils.NowRFC3339UTC()
	if _, ok := row["created_at"]; !ok {
		row["created_at"] = now
	}
	if _, ok := row["updated_at"]; !ok {
		row["updated_at"] = now
	}
	if err := f.db.Model(model).Create(row).Error; err != nil {
		f.t.Fatalf("seed %T: %v", model, err)
	}
}

// campaign seeds a campaign, nil company is global
func (f *reportInboxFixture) campaign(companyID *uuid.UUID, closed bool) uuid.UUID {
	id := uuid.New()
	row := map[string]any{"id": id, "name": "campaign " + id.String()}
	if companyID != nil {
		row["company_id"] = *companyID
	}
	if closed {
		row["closed_at"] = utils.RFC3339UTC(time.Now().Add(-time.Hour))
	}
	f.create(&database.Campaign{}, row)
	return id
}

// recipient seeds a recipient in a campaign and returns the campaign recipient id
// and the recipient id
func (f *reportInboxFixture) recipient(campaignID uuid.UUID, email, messageID string) (uuid.UUID, uuid.UUID) {
	rid := uuid.New()
	f.create(&database.Recipient{}, map[string]any{"id": rid, "email": email})
	crid := uuid.New()
	row := map[string]any{"id": crid, "campaign_id": campaignID, "recipient_id": rid, "self_managed": false}
	if messageID != "" {
		row["message_id"] = messageID
	}
	f.create(&database.CampaignRecipient{}, row)
	return crid, rid
}

func (f *reportInboxFixture) inbox(companyID *uuid.UUID, mailbox string) uuid.UUID {
	id := uuid.New()
	row := map[string]any{
		"id": id, "enabled": true, "auth_mode": "app", "mailbox": mailbox,
		"tenant_id": "contoso.onmicrosoft.com", "client_id": uuid.NewString(), "client_secret": "good-secret",
		"read_since": utils.RFC3339UTC(time.Now().Add(-2 * time.Hour)),
	}
	if companyID != nil {
		row["company_id"] = *companyID
	}
	f.create(&database.ReportInbox{}, row)
	return id
}

func (f *reportInboxFixture) reportedEvents(campaignID, recipientID uuid.UUID) []database.CampaignEvent {
	var rows []database.CampaignEvent
	if err := f.db.Where("campaign_id = ? AND recipient_id = ? AND event_id = ?", campaignID, recipientID, *f.reported).Find(&rows).Error; err != nil {
		f.t.Fatalf("events: %v", err)
	}
	return rows
}

func (f *reportInboxFixture) inboxRow(id uuid.UUID) database.ReportInbox {
	var row database.ReportInbox
	if err := f.db.Where("id = ?", id).First(&row).Error; err != nil {
		f.t.Fatalf("inbox: %v", err)
	}
	return row
}

// pollAll starts polls for due inboxes and waits for them to finish
func (f *reportInboxFixture) pollAll() error {
	err := f.svc.PollDue(f.ctx)
	f.svc.WaitPolls()
	return err
}

func (f *reportInboxFixture) makeDue(id uuid.UUID) {
	if err := f.db.Model(&database.ReportInbox{}).Where("id = ?", id).Update("next_poll_at", nil).Error; err != nil {
		f.t.Fatalf("make due: %v", err)
	}
}

func reportMIME(messageID, body string) string {
	return reportMIMEFrom("it@sender.example", messageID, body)
}

func reportMIMEFrom(from, messageID, body string) string {
	return "Message-ID: <" + messageID + ">\r\n" +
		"From: " + from + "\r\n" +
		"Subject: password expires\r\n" +
		"Content-Type: text/html\r\n\r\n" + body + "\r\n"
}

func TestReportInboxPoll_MatchesInScopeOnly(t *testing.T) {
	f := newReportInboxFixture(t)
	camp1 := f.campaign(&f.c1, false)
	camp2 := f.campaign(&f.c2, false)
	closed := f.campaign(&f.c1, true)

	cr1, r1 := f.recipient(camp1, "alice@c1.example", "11111111-aaaa@send.example")
	cr2, r2 := f.recipient(camp1, "bob@c1.example", "")
	crOther, rOther := f.recipient(camp2, "eve@c2.example", "22222222-bbbb@send.example")
	crClosed, rClosed := f.recipient(closed, "carol@c1.example", "33333333-cccc@send.example")
	_ = cr1

	now := time.Now().Add(-time.Hour)
	safe := "https://eur01.safelinks.protection.outlook.com/?url=https%3A%2F%2Flure.example%2F%3Fid%3D" + cr2.String() + "&amp;data=05"
	// cr3 was sent without a stored Message-ID, like an API sender send
	cr3, r3 := f.recipient(camp1, "frank@c1.example", "")
	f.graph.messages = []fakeGraphMessage{
		// built in report button: the original is attached, matched on Message-ID
		{id: "m1", received: now, from: "alice@c1.example",
			attachment: reportMIME("11111111-aaaa@send.example", "hello"),
			body:       ""},
		// a report of another company's recipient is ignored
		{id: "m2", received: now.Add(time.Minute), from: "mallory@c1.example",
			attachment: reportMIME("22222222-bbbb@send.example", `<img src="https://t.example/?rid=`+crOther.String()+`">`),
			body:       ""},
		// a plain forward with the id behind safe links
		{id: "m3", received: now.Add(2 * time.Minute), from: "bob@c1.example",
			body: `<a href="` + safe + `">reset</a>`},
		// reporting the same email again, for example one message of a thread
		{id: "m4", received: now.Add(3 * time.Minute), from: "alice@c1.example",
			attachment: reportMIME("11111111-aaaa@send.example", "hello"),
			body:       ""},
		// a closed campaign is not recorded
		{id: "m5", received: now.Add(4 * time.Minute), from: "carol@c1.example",
			attachment: reportMIME("33333333-cccc@send.example", ""),
			body:       ""},
		// real phishing with no ids at all
		{id: "m6", received: now.Add(5 * time.Minute), from: "dave@c1.example",
			attachment: reportMIMEFrom("ceo@bad.example", "real@bad.example", "pay this invoice "+cr3.String()),
			body:       ""},
	}
	// sent from our domain without a known Message-ID, matched on the link
	f.graph.messages = append(f.graph.messages, fakeGraphMessage{
		id: "m7", received: now.Add(6 * time.Minute), from: "frank@c1.example",
		attachment: reportMIME("api-sent@sender.example", `<a href="https://lure.example/?id=`+cr3.String()+`">x</a>`),
		body:       "",
	})
	inboxID := f.inbox(&f.c1, "reports@c1.example")

	if err := f.pollAll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if f.graph.valueFetches != 0 {
		t.Fatalf("attached items must not be downloaded, got %d downloads", f.graph.valueFetches)
	}
	for _, id := range []string{"m1", "m2", "m4", "m5"} {
		if f.graph.bodyFetches[id] != 0 {
			t.Fatalf("%s has a known Message-ID, its body must not be fetched", id)
		}
	}
	if f.graph.bodyFetches["m6"] != 0 {
		t.Fatalf("real phish from a foreign domain must not be read, even though it holds a recipient id")
	}
	if f.graph.bodyFetches["m7"] != 1 {
		t.Fatalf("our domain without a known Message-ID must be read once, got %d", f.graph.bodyFetches["m7"])
	}
	if n := len(f.reportedEvents(camp1, r3)); n != 1 {
		t.Fatalf("frank: expected 1 reported event, got %d", n)
	}
	if len(f.graph.unknownPath) > 0 {
		t.Fatalf("unexpected graph paths: %v", f.graph.unknownPath)
	}

	ev1 := f.reportedEvents(camp1, r1)
	if len(ev1) != 1 {
		t.Fatalf("alice: expected 1 reported event, got %d", len(ev1))
	}
	if !strings.Contains(ev1[0].Data, `"reportedByRecipient":true`) || !strings.Contains(ev1[0].Data, `"source":"reporting inbox"`) {
		t.Fatalf("alice: unexpected event data %q", ev1[0].Data)
	}
	if ev1[0].CreatedAt == nil || ev1[0].CreatedAt.Sub(now).Abs() > time.Second {
		t.Fatalf("alice: event time should be the mailbox receive time, got %v want %v", ev1[0].CreatedAt, now)
	}
	if n := len(f.reportedEvents(camp1, r2)); n != 1 {
		t.Fatalf("bob: expected 1 reported event, got %d", n)
	}
	if n := len(f.reportedEvents(camp2, rOther)); n != 0 {
		t.Fatalf("other company recipient must not be marked, got %d", n)
	}
	if n := len(f.reportedEvents(closed, rClosed)); n != 0 {
		t.Fatalf("closed campaign recipient must not be marked, got %d", n)
	}
	_ = crClosed

	row := f.inboxRow(inboxID)
	if row.LastError != "" {
		t.Fatalf("unexpected error: %s", row.LastError)
	}
	if row.ProcessedCount != 7 {
		t.Fatalf("processed: got %d want 7", row.ProcessedCount)
	}
	// m1, m3 and m7 record an event. m4 is a repeat and m5 a closed campaign, so
	// they record nothing. m2 and m6 are not in scope.
	if row.MatchedCount != 3 {
		t.Fatalf("matched: got %d want 3", row.MatchedCount)
	}
	if row.Watermark == nil || row.LastSuccessAt == nil || row.NextPollAt == nil {
		t.Fatalf("poll state not saved: %+v", row)
	}

	// a second poll reads the overlap again but processes nothing twice
	f.makeDue(inboxID)
	if err := f.pollAll(); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	row = f.inboxRow(inboxID)
	if row.ProcessedCount != 7 {
		t.Fatalf("second poll processed again: %d", row.ProcessedCount)
	}
	if n := len(f.reportedEvents(camp1, r1)); n != 1 {
		t.Fatalf("alice: duplicate after second poll, got %d", n)
	}
	if f.graph.tokenCalls != 1 {
		t.Fatalf("token should be cached, got %d token calls", f.graph.tokenCalls)
	}
}

func TestReportInboxPoll_GlobalCoversGlobalCampaignsOnly(t *testing.T) {
	f := newReportInboxFixture(t)
	global := f.campaign(nil, false)
	companyCamp := f.campaign(&f.c1, false)
	_, rg := f.recipient(global, "g@x.example", "g-1@send.example")
	_, rc := f.recipient(companyCamp, "c@c1.example", "c-1@send.example")

	f.graph.readable["reports@global.example"] = true
	now := time.Now().Add(-time.Hour)
	f.graph.messages = []fakeGraphMessage{
		{id: "g1", received: now, attachment: reportMIME("g-1@send.example", ""), body: ""},
		{id: "g2", received: now.Add(time.Minute), attachment: reportMIME("c-1@send.example", ""), body: ""},
	}
	f.inbox(nil, "reports@global.example")
	if err := f.pollAll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if n := len(f.reportedEvents(global, rg)); n != 1 {
		t.Fatalf("global campaign: expected 1, got %d", n)
	}
	if n := len(f.reportedEvents(companyCamp, rc)); n != 0 {
		t.Fatalf("company campaign must not be matched by the global inbox, got %d", n)
	}
}

func TestReportInboxPoll_AnonymousStoresNoReporterInfo(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(&f.c1, false)
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", camp).Update("is_anonymous", true).Error; err != nil {
		t.Fatalf("anon: %v", err)
	}
	crid, _ := f.recipient(camp, "anon@c1.example", "anon-1@send.example")
	aid := uuid.New()
	if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", crid).Update("anonymized_id", aid).Error; err != nil {
		t.Fatalf("aid: %v", err)
	}
	f.graph.messages = []fakeGraphMessage{
		{id: "a1", received: time.Now().Add(-time.Hour), from: "anon@c1.example",
			attachment: reportMIME("anon-1@send.example", ""), body: ""},
	}
	f.inbox(&f.c1, "reports@c1.example")
	if err := f.pollAll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	var rows []database.CampaignEvent
	if err := f.db.Where("campaign_id = ? AND event_id = ?", camp, *f.reported).Find(&rows).Error; err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 event, got %d", len(rows))
	}
	if rows[0].RecipientID != nil {
		t.Fatalf("anonymous event must not carry a recipient id")
	}
	if strings.Contains(rows[0].Data, "reportedByRecipient") {
		t.Fatalf("anonymous event must not carry reporter info: %q", rows[0].Data)
	}
}

func TestReportInboxPoll_FailureBacksOff(t *testing.T) {
	f := newReportInboxFixture(t)
	id := f.inbox(&f.c1, "reports@c1.example")
	if err := f.db.Model(&database.ReportInbox{}).Where("id = ?", id).Update("client_secret", "wrong").Error; err != nil {
		t.Fatalf("secret: %v", err)
	}
	if err := f.pollAll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	row := f.inboxRow(id)
	if row.FailureCount != 1 || !strings.Contains(row.LastError, "AADSTS7000215") {
		t.Fatalf("expected recorded failure, got %d %q", row.FailureCount, row.LastError)
	}
	if row.NextPollAt == nil || time.Until(*row.NextPollAt) < reportInboxPollInterval {
		t.Fatalf("expected backoff, next poll at %v", row.NextPollAt)
	}
	// not due yet, so a new round does nothing
	if err := f.pollAll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if f.graph.tokenCalls != 1 {
		t.Fatalf("inbox in backoff was polled again")
	}
}

func TestReportInboxTest_WarnsAboutBroadAccess(t *testing.T) {
	f := newReportInboxFixture(t)
	session := authorizedSession(t)
	f.graph.token = fakeJWT([]string{"Mail.Read"})
	f.graph.readable["ceo@c1.example"] = true
	// the access check picks a recipient on the same domain in the same company
	f.create(&database.Recipient{}, map[string]any{"id": uuid.New(), "email": "ceo@c1.example", "company_id": f.c1})
	f.create(&database.Recipient{}, map[string]any{"id": uuid.New(), "email": "x@other.example", "company_id": f.c1})

	incoming := &model.ReportInbox{
		AuthMode:     model.ReportInboxAuthApp,
		Mailbox:      "reports@c1.example",
		TenantID:     "contoso.onmicrosoft.com",
		ClientID:     uuid.NewString(),
		ClientSecret: "good-secret",
	}
	res, err := f.svc.Test(f.ctx, session, &f.c1, incoming)
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if res.OK {
		t.Fatalf("readable control mailbox must fail the test: %+v", res)
	}
	if len(res.Warnings) != 2 {
		t.Fatalf("expected a role warning and an access warning, got %v", res.Warnings)
	}

	// limited access: no broad role and the control mailbox is denied
	f.graph.token = fakeJWT(nil)
	delete(f.graph.readable, "ceo@c1.example")
	incoming.ClientSecret = "good-secret"
	res, err = f.svc.Test(f.ctx, session, &f.c1, incoming)
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if !res.OK || len(res.Warnings) != 0 {
		t.Fatalf("expected a clean pass, got %+v", res)
	}
	if last := res.Steps[len(res.Steps)-1]; last.Name != "Access is limited" || !strings.Contains(last.Message, "ceo@c1.example") {
		t.Fatalf("access check did not use the same domain recipient: %+v", last)
	}

	// global scope has no recipients on the domain, so the check is skipped
	res, err = f.svc.Test(f.ctx, session, nil, incoming)
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if last := res.Steps[len(res.Steps)-1]; !strings.HasPrefix(last.Message, "Skipped") {
		t.Fatalf("expected a skipped access check, got %+v", last)
	}
}

func TestReportInboxUpsert_ScopeAndSecret(t *testing.T) {
	f := newReportInboxFixture(t)
	session := authorizedSession(t)

	// an oauth provider owned by another company can not be used
	otherProvider := uuid.New()
	f.create(&database.OAuthProvider{}, map[string]any{
		"id": otherProvider, "name": "p", "auth_url": "https://a", "token_url": "https://t",
		"scopes": "s", "client_id": "c", "client_secret": "s", "company_id": f.c2,
	})
	_, err := f.svc.Upsert(f.ctx, session, &f.c1, &model.ReportInbox{
		Enabled:         true,
		AuthMode:        model.ReportInboxAuthDelegated,
		Mailbox:         "reports@c1.example",
		OAuthProviderID: nullable.NewNullableWithValue(otherProvider),
	})
	if err == nil {
		t.Fatalf("expected a scope error for another company's oauth provider")
	}

	// the secret is stored, never returned and kept when left empty
	saved, err := f.svc.Upsert(f.ctx, session, &f.c1, &model.ReportInbox{
		Enabled:      true,
		AuthMode:     model.ReportInboxAuthApp,
		Mailbox:      "reports@c1.example",
		TenantID:     "contoso.onmicrosoft.com",
		ClientID:     uuid.NewString(),
		ClientSecret: "good-secret",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	out, _ := json.Marshal(saved)
	if strings.Contains(string(out), "good-secret") || !saved.HasClientSecret {
		t.Fatalf("secret leaked or missing: %s", out)
	}
	saved2, err := f.svc.Upsert(f.ctx, session, &f.c1, &model.ReportInbox{
		Enabled:  true,
		AuthMode: model.ReportInboxAuthApp,
		Mailbox:  "reports@c1.example",
		TenantID: "contoso.onmicrosoft.com",
		ClientID: saved.ClientID,
	})
	if err != nil {
		t.Fatalf("upsert without secret: %v", err)
	}
	if saved2.ClientSecret != "good-secret" {
		t.Fatalf("stored secret was not kept")
	}

	// a tenant that would change the token url path is rejected
	_, err = f.svc.Upsert(f.ctx, session, &f.c1, &model.ReportInbox{
		AuthMode: model.ReportInboxAuthApp,
		TenantID: "../evil",
	})
	if err == nil {
		t.Fatalf("expected tenant validation error")
	}

	// a company that does not exist is rejected
	missing := uuid.New()
	if _, err := f.svc.Upsert(f.ctx, session, &missing, &model.ReportInbox{AuthMode: model.ReportInboxAuthApp}); err == nil {
		t.Fatalf("expected an error for a missing company")
	}

	// only authorized sessions
	if _, err := f.svc.Upsert(f.ctx, unauthorizedSession(t), &f.c1, &model.ReportInbox{AuthMode: model.ReportInboxAuthApp}); err == nil {
		t.Fatalf("expected an authorization error")
	}
}

func TestReportInboxPollDue_ConcurrentAndNotBlocked(t *testing.T) {
	f := newReportInboxFixture(t)
	var mu sync.Mutex
	active, maxActive := 0, 0
	listCalls := map[string]int{}
	release := make(chan struct{})
	token := fakeJWT(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"` + token + `","expires_in":3600}`))
			return
		}
		mailbox := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1.0/users/"), "/")[0]
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		listCalls[mailbox]++
		mu.Unlock()
		if mailbox == "slow@c1.example" {
			// a tenant that hangs until the test lets it go
			<-release
		} else {
			time.Sleep(50 * time.Millisecond)
		}
		mu.Lock()
		active--
		mu.Unlock()
		_, _ = w.Write([]byte(`{"value":[]}`))
	}))
	t.Cleanup(srv.Close)
	f.svc.graph = &graphMailClient{HTTP: srv.Client(), GraphBase: srv.URL + "/v1.0", LoginBase: srv.URL}

	// one hanging inbox and 20 normal ones, each in its own company
	ids := []uuid.UUID{f.inbox(&f.c1, "slow@c1.example")}
	for i := 0; i < 20; i++ {
		c := uuid.New()
		f.create(&database.Company{}, map[string]any{"id": c, "name": "company " + c.String()})
		ids = append(ids, f.inbox(&c, fmt.Sprintf("r%d@x.example", i)))
	}

	// run poller rounds until every normal inbox was polled, while the slow one hangs
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := f.svc.PollDue(f.ctx); err != nil {
			t.Fatalf("poll: %v", err)
		}
		mu.Lock()
		done := 0
		for i := 0; i < 20; i++ {
			if listCalls[fmt.Sprintf("r%d@x.example", i)] > 0 {
				done++
			}
		}
		mu.Unlock()
		if done == 20 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of 20 inboxes were polled while one tenant hung", done)
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(release)
	f.svc.WaitPolls()

	mu.Lock()
	defer mu.Unlock()
	if maxActive > reportInboxWorkers {
		t.Fatalf("polled %d inboxes at once, limit is %d", maxActive, reportInboxWorkers)
	}
	if maxActive < 2 {
		t.Fatalf("expected parallel polls, max was %d", maxActive)
	}
	if listCalls["slow@c1.example"] != 1 {
		t.Fatalf("the hanging inbox was started %d times", listCalls["slow@c1.example"])
	}
	for i := 0; i < 20; i++ {
		if n := listCalls[fmt.Sprintf("r%d@x.example", i)]; n != 1 {
			t.Fatalf("inbox r%d was polled %d times in one interval", i, n)
		}
	}
	// next polls are spread out by the jitter
	var rows []database.ReportInbox
	if err := f.db.Where("id IN ?", ids[1:]).Find(&rows).Error; err != nil {
		t.Fatalf("rows: %v", err)
	}
	distinct := map[int64]bool{}
	for _, row := range rows {
		if row.NextPollAt == nil || time.Until(*row.NextPollAt) < reportInboxPollInterval-time.Minute {
			t.Fatalf("next poll not scheduled: %v", row.NextPollAt)
		}
		distinct[row.NextPollAt.Unix()] = true
	}
	if len(distinct) < 5 {
		t.Fatalf("next polls are not spread out: %d distinct seconds", len(distinct))
	}
}

func TestReportInboxPoll_PanicIsContainedAndBacksOff(t *testing.T) {
	f := newReportInboxFixture(t)
	id := f.inbox(&f.c1, "reports@c1.example")
	// a client without an http client panics on the first request
	f.svc.graph = &graphMailClient{GraphBase: "http://127.0.0.1:1/v1.0", LoginBase: "http://127.0.0.1:1"}

	if err := f.pollAll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	row := f.inboxRow(id)
	if row.FailureCount != 1 || !strings.Contains(row.LastError, "internal error") {
		t.Fatalf("panic not recorded: %d %q", row.FailureCount, row.LastError)
	}
	if row.NextPollAt == nil || time.Until(*row.NextPollAt) < reportInboxPollInterval {
		t.Fatalf("panicking inbox must back off, next poll %v", row.NextPollAt)
	}
	f.svc.inflightMu.Lock()
	n := len(f.svc.inflight)
	f.svc.inflightMu.Unlock()
	if n != 0 {
		t.Fatalf("slot not freed after panic")
	}
	// not due, so the next round does not start it again
	if err := f.pollAll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if f.inboxRow(id).FailureCount != 1 {
		t.Fatalf("panicking inbox was polled again right away")
	}
}

// blockingGraph serves one message and holds the message list until released
func blockingGraph(t *testing.T, release chan struct{}, listed chan struct{}) *httptest.Server {
	token := fakeJWT(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"` + token + `","expires_in":3600}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages") {
			close(listed)
			<-release
			_ = json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{{
				"id": "late-1", "receivedDateTime": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
				"hasAttachments": false, "from": map[string]any{"emailAddress": map[string]any{"address": "a@c1.example"}},
			}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"internetMessageId": "<x@y>", "body": map[string]any{"content": ""}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestReportInboxPoll_DeletedWhilePolling(t *testing.T) {
	f := newReportInboxFixture(t)
	release, listed := make(chan struct{}), make(chan struct{})
	srv := blockingGraph(t, release, listed)
	f.svc.graph = &graphMailClient{HTTP: srv.Client(), GraphBase: srv.URL + "/v1.0", LoginBase: srv.URL}
	id := f.inbox(&f.c1, "reports@c1.example")

	if err := f.svc.PollDue(f.ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	<-listed
	if _, err := f.svc.Upsert(f.ctx, authorizedSession(t), &f.c1, &model.ReportInbox{AuthMode: model.ReportInboxAuthApp}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := f.svc.DeleteByCompanyID(f.ctx, authorizedSession(t), &f.c1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	close(release)
	f.svc.WaitPolls()

	var count int64
	f.db.Model(&database.ReportInboxMessage{}).Where("report_inbox_id = ?", id).Count(&count)
	if count != 0 {
		t.Fatalf("a deleted inbox left %d message ids behind", count)
	}
	f.db.Model(&database.ReportInbox{}).Where("id = ?", id).Count(&count)
	if count != 0 {
		t.Fatalf("a poll brought a deleted inbox back")
	}
}

func TestReportInboxPoll_ChangedWhilePolling(t *testing.T) {
	f := newReportInboxFixture(t)
	release, listed := make(chan struct{}), make(chan struct{})
	srv := blockingGraph(t, release, listed)
	f.svc.graph = &graphMailClient{HTTP: srv.Client(), GraphBase: srv.URL + "/v1.0", LoginBase: srv.URL}
	id := f.inbox(&f.c1, "reports@c1.example")

	if err := f.svc.PollDue(f.ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	<-listed
	// the admin points the inbox at another mailbox while the poll runs
	if _, err := f.svc.Upsert(f.ctx, authorizedSession(t), &f.c1, &model.ReportInbox{
		Enabled: true, AuthMode: model.ReportInboxAuthApp, Mailbox: "new@c1.example",
		TenantID: "contoso.onmicrosoft.com", ClientID: uuid.NewString(), ClientSecret: "good-secret",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	close(release)
	f.svc.WaitPolls()

	row := f.inboxRow(id)
	if row.Watermark != nil || row.LastPolledAt != nil {
		t.Fatalf("an old poll wrote over the new settings: watermark %v polled %v",
			row.Watermark, row.LastPolledAt)
	}
	// the old poll read another mailbox, its messages and counts are dropped so
	// nothing of the old mailbox is carried over to the new one
	if row.ProcessedCount != 0 {
		t.Fatalf("counts of the old mailbox must not be added, got %d", row.ProcessedCount)
	}
	var left int64
	f.db.Model(&database.ReportInboxMessage{}).Where("report_inbox_id = ?", id).Count(&left)
	if left != 0 {
		t.Fatalf("messages of the old mailbox must be removed, %d left", left)
	}
	if row.NextPollAt != nil {
		t.Fatalf("the changed inbox must stay due right away")
	}
}

func TestReportInboxPoll_NeverReadsMailFromBeforeReadSince(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(nil, false)
	_, before := f.recipient(camp, "before@x.example", "before-1@send.example")
	_, after := f.recipient(camp, "after@x.example", "after-1@send.example")

	f.graph.readable["reports@global.example"] = true
	start := time.Now().Add(-30 * time.Minute)
	f.graph.messages = []fakeGraphMessage{
		{id: "old", received: start.Add(-5 * time.Minute), attachment: reportMIME("before-1@send.example", ""), body: ""},
		{id: "new", received: start.Add(time.Minute), attachment: reportMIME("after-1@send.example", ""), body: ""},
	}
	id := f.inbox(nil, "reports@global.example")
	// a watermark close to read_since must not pull the overlap from before it
	if err := f.db.Model(&database.ReportInbox{}).Where("id = ?", id).Updates(map[string]any{
		"read_since": utils.RFC3339UTC(start),
		"watermark":  utils.RFC3339UTC(start.Add(time.Minute)),
	}).Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := f.pollAll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if n := len(f.reportedEvents(camp, before)); n != 0 {
		t.Fatalf("mail from before read_since was read, got %d events", n)
	}
	if n := len(f.reportedEvents(camp, after)); n != 1 {
		t.Fatalf("expected 1 event for mail after read_since, got %d", n)
	}
}

func TestReportInboxUpsert_SetsReadSinceWhenTurnedOn(t *testing.T) {
	f := newReportInboxFixture(t)
	session := authorizedSession(t)
	cfg := func(enabled bool, mailbox string) *model.ReportInbox {
		return &model.ReportInbox{
			Enabled: enabled, AuthMode: model.ReportInboxAuthApp, Mailbox: mailbox,
			TenantID: "contoso.onmicrosoft.com", ClientID: "8768e461-6446-4b42-ae25-9a4ec239be61",
		}
	}
	first := cfg(false, "reports@c1.example")
	first.ClientSecret = "good-secret"
	saved, err := f.svc.Upsert(f.ctx, session, &f.c1, first)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if saved.ReadSince != nil {
		t.Fatalf("a disabled inbox must not have read_since")
	}
	before := time.Now().Add(-time.Second)
	saved, err = f.svc.Upsert(f.ctx, session, &f.c1, cfg(true, "reports@c1.example"))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if saved.ReadSince == nil || saved.ReadSince.Before(before) {
		t.Fatalf("read_since not set when turned on: %v", saved.ReadSince)
	}
	turnedOn := *saved.ReadSince
	// saving again without changes keeps it
	saved, err = f.svc.Upsert(f.ctx, session, &f.c1, cfg(true, "reports@c1.example"))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if saved.ReadSince == nil || !saved.ReadSince.Equal(turnedOn) {
		t.Fatalf("read_since changed on a save without changes: %v", saved.ReadSince)
	}
	// another mailbox starts again from now
	time.Sleep(1100 * time.Millisecond)
	saved, err = f.svc.Upsert(f.ctx, session, &f.c1, cfg(true, "other@c1.example"))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if saved.ReadSince == nil || !saved.ReadSince.After(turnedOn) {
		t.Fatalf("read_since not moved for a new mailbox: %v", saved.ReadSince)
	}
}

func TestReportInboxPoll_WithoutReadSinceFollowsWatermark(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(nil, false)
	_, rid := f.recipient(camp, "r@x.example", "r-1@send.example")

	f.graph.readable["reports@global.example"] = true
	mark := time.Now().Add(-20 * time.Minute)
	f.graph.messages = []fakeGraphMessage{
		{id: "m", received: mark.Add(5 * time.Minute), attachment: reportMIME("r-1@send.example", ""), body: ""},
	}
	id := f.inbox(nil, "reports@global.example")
	// an inbox saved before read_since existed
	if err := f.db.Model(&database.ReportInbox{}).Where("id = ?", id).Updates(map[string]any{
		"read_since": nil,
		"watermark":  utils.RFC3339UTC(mark),
	}).Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := f.pollAll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if n := len(f.reportedEvents(camp, rid)); n != 1 {
		t.Fatalf("expected the message after the watermark to be read, got %d", n)
	}
}

func TestReportInboxPoll_PassesOverAMessageThatKeepsFailing(t *testing.T) {
	f := newReportInboxFixture(t)
	giveUpAfter := reportInboxGiveUpAfter
	reportInboxGiveUpAfter = 0
	t.Cleanup(func() { reportInboxGiveUpAfter = giveUpAfter })
	camp := f.campaign(nil, false)
	_, rid := f.recipient(camp, "r@x.example", "r-1@send.example")

	f.graph.readable["reports@global.example"] = true
	start := time.Now().Add(-10 * time.Minute)
	f.graph.messages = []fakeGraphMessage{
		{id: "bad", received: start, attachment: reportMIME("other@send.example", ""), body: ""},
		{id: "good", received: start.Add(time.Minute), attachment: reportMIME("r-1@send.example", ""), body: ""},
	}
	f.graph.attachmentStatus = map[string]int{"bad": http.StatusInternalServerError}
	id := f.inbox(nil, "reports@global.example")

	// the message after the failing one is not held up
	_ = f.pollAll()
	if n := len(f.reportedEvents(camp, rid)); n != 1 {
		t.Fatalf("the message after a failing one must be recorded right away, got %d events", n)
	}
	if row := f.inboxRow(id); row.LastError != "" || row.ProcessedCount != 1 {
		t.Fatalf("one failing message must not fail the poll: error %q processed %d", row.LastError, row.ProcessedCount)
	}
	// the failing message is tried again on each poll and passed over in the end
	for i := 1; i < reportInboxMessageTries; i++ {
		f.makeDue(id)
		_ = f.pollAll()
	}
	if row := f.inboxRow(id); row.ProcessedCount != 2 {
		t.Fatalf("after %d failed polls the failing message must be passed over, processed %d", reportInboxMessageTries, row.ProcessedCount)
	}
}

func TestReportInboxPoll_AccessDeniedIsNotPassedOver(t *testing.T) {
	f := newReportInboxFixture(t)
	f.graph.readable["reports@global.example"] = true
	f.graph.messages = []fakeGraphMessage{
		{id: "m", received: time.Now().Add(-time.Minute), attachment: reportMIME("x@send.example", ""), body: ""},
	}
	f.graph.attachmentStatus = map[string]int{"m": http.StatusForbidden}
	id := f.inbox(nil, "reports@global.example")
	_ = f.pollAll()
	var row database.ReportInbox
	if err := f.db.First(&row, "id = ?", id).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if row.ProcessedCount != 0 {
		t.Fatalf("a 403 must not mark the message processed after one poll, processed %d", row.ProcessedCount)
	}
}

func TestCampaignRecipient_MessageIDKeepsFirstAndAnonymizeClearsIt(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(nil, false)
	crWith, rid := f.recipient(camp, "a@x.example", "first@send.example")
	crWithout, _ := f.recipient(camp, "b@x.example", "")
	repo := &repository.CampaignRecipient{DB: f.db}

	if err := repo.SetMessageIDByID(f.ctx, &crWith, "second@send.example"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := repo.SetMessageIDByID(f.ctx, &crWithout, "new@send.example"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got, err := repo.GetByMessageID(f.ctx, "first@send.example"); err != nil || got.ID.MustGet() != crWith {
		t.Fatalf("the first Message-ID must be kept: %v", err)
	}
	if _, err := repo.GetByMessageID(f.ctx, "second@send.example"); err == nil {
		t.Fatalf("a later Message-ID must not replace the first")
	}
	if got, err := repo.GetByMessageID(f.ctx, "new@send.example"); err != nil || got.ID.MustGet() != crWithout {
		t.Fatalf("a recipient without a Message-ID must get one: %v", err)
	}

	anon := uuid.New()
	if err := repo.Anonymize(f.ctx, &camp, &rid, &anon); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	if _, err := repo.GetByMessageID(f.ctx, "first@send.example"); err == nil {
		t.Fatalf("anonymize must clear the Message-ID")
	}
}

func TestOAuthProviderDelete_RefusedWhileAReportingInboxUsesIt(t *testing.T) {
	f := newReportInboxFixture(t)
	provider := uuid.New()
	f.create(&database.OAuthProvider{}, map[string]any{
		"id": provider, "name": "p", "auth_url": "https://a", "token_url": "https://t",
		"scopes": "s", "client_id": "c", "client_secret": "s",
	})
	id := f.inbox(nil, "reports@global.example")
	if err := f.db.Model(&database.ReportInbox{}).Where("id = ?", id).Updates(map[string]any{
		"auth_mode": "delegated", "o_auth_provider_id": provider,
	}).Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	svc := &OAuthProvider{
		Common:                  Common{Logger: zap.NewNop().Sugar()},
		OAuthProviderRepository: &repository.OAuthProvider{DB: f.db},
		ReportInboxRepository:   &repository.ReportInbox{DB: f.db},
	}
	if err := svc.DeleteByID(f.ctx, authorizedSession(t), &provider); !isValidationError(err) {
		t.Fatalf("expected a validation error, got %v", err)
	}
	var n int64
	f.db.Model(&database.OAuthProvider{}).Where("id = ?", provider).Count(&n)
	if n != 1 {
		t.Fatalf("the provider must not be deleted")
	}
}

func TestCampaignRecipient_SharedMessageIDDoesNotMatch(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(nil, false)
	f.recipient(camp, "a@x.example", "fixed@send.example")
	f.recipient(camp, "b@x.example", "fixed@send.example")
	repo := &repository.CampaignRecipient{DB: f.db}
	if _, err := repo.GetByMessageID(f.ctx, "fixed@send.example"); err == nil {
		t.Fatalf("a Message-ID shared by two recipients must not match either")
	}
}

func TestReportInboxPoll_AttachedEventIsNotTheReportedEmail(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(nil, false)
	cr, rid := f.recipient(camp, "r@x.example", "")
	f.graph.readable["reports@global.example"] = true
	f.graph.messages = []fakeGraphMessage{{
		id: "m", received: time.Now().Add(-time.Minute), from: "user@x.example",
		attachment: reportMIME("invite@x.example", ""), itemType: "#microsoft.graph.event",
		// the forward around the invite carries the link
		body: `<a href="https://lure.example/?id=` + cr.String() + `">join</a>`,
	}}
	id := f.inbox(nil, "reports@global.example")
	_ = f.pollAll()
	row := f.inboxRow(id)
	if row.LastError != "" {
		t.Fatalf("an attached event must not fail the poll: %s", row.LastError)
	}
	if n := len(f.reportedEvents(camp, rid)); n != 1 {
		t.Fatalf("the forward around an attached event must be read, got %d events", n)
	}
}

func TestReportInboxPoll_FailingMessageNotPassedOverBeforeTheTimeSpan(t *testing.T) {
	f := newReportInboxFixture(t)
	f.graph.readable["reports@global.example"] = true
	f.graph.messages = []fakeGraphMessage{
		{id: "bad", received: time.Now().Add(-time.Minute), attachment: reportMIME("other@send.example", ""), body: ""},
	}
	f.graph.attachmentStatus = map[string]int{"bad": http.StatusServiceUnavailable}
	id := f.inbox(nil, "reports@global.example")
	// many quick polls during an outage must not pass the message over
	for i := 0; i < reportInboxMessageTries*2; i++ {
		f.makeDue(id)
		_ = f.pollAll()
	}
	if row := f.inboxRow(id); row.ProcessedCount != 0 {
		t.Fatalf("a message failing for less than the time span must not be passed over, processed %d", row.ProcessedCount)
	}
}

func TestReportInboxPoll_FailedMessageIsReadAgainByID(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(nil, false)
	_, rid := f.recipient(camp, "r@x.example", "r-1@send.example")
	f.graph.readable["reports@global.example"] = true
	failedAt := time.Now().Add(-50 * time.Minute)
	f.graph.messages = []fakeGraphMessage{
		{id: "failed", received: failedAt, attachment: reportMIME("r-1@send.example", ""), body: ""},
		{id: "later", received: time.Now().Add(-time.Minute), attachment: reportMIME("other@send.example", ""), body: ""},
	}
	f.graph.attachmentStatus = map[string]int{"failed": http.StatusServiceUnavailable}
	id := f.inbox(nil, "reports@global.example")
	_ = f.pollAll()
	row := f.inboxRow(id)
	if row.ProcessedCount != 1 || row.Watermark == nil || !row.Watermark.After(failedAt) {
		t.Fatalf("the poll must move on past a failed message: processed %d watermark %v", row.ProcessedCount, row.Watermark)
	}
	// the failed message is far behind the watermark now and is read again by
	// its id once it works
	delete(f.graph.attachmentStatus, "failed")
	f.makeDue(id)
	_ = f.pollAll()
	if n := len(f.reportedEvents(camp, rid)); n != 1 {
		t.Fatalf("the failed message must be read again by its id, got %d events", n)
	}
	if row := f.inboxRow(id); row.ProcessedCount != 2 {
		t.Fatalf("processed %d, want 2", row.ProcessedCount)
	}
}

func TestReportInboxPoll_WatermarkNeverMovesBack(t *testing.T) {
	f := newReportInboxFixture(t)
	f.graph.readable["reports@global.example"] = true
	mark := time.Now().Add(-time.Minute).Truncate(time.Second)
	f.graph.messages = []fakeGraphMessage{
		{id: "old", received: mark.Add(-5 * time.Minute), attachment: reportMIME("x@send.example", ""), body: ""},
	}
	id := f.inbox(nil, "reports@global.example")
	if err := f.db.Model(&database.ReportInbox{}).Where("id = ?", id).Update("watermark", utils.RFC3339UTC(mark)).Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	_ = f.pollAll()
	if row := f.inboxRow(id); row.Watermark == nil || row.Watermark.Before(mark) {
		t.Fatalf("the watermark moved back to %v from %v", row.Watermark, mark)
	}
}

func TestReportInboxPoll_PendingMessageDeletedIsResolved(t *testing.T) {
	f := newReportInboxFixture(t)
	f.graph.readable["reports@global.example"] = true
	f.graph.messages = []fakeGraphMessage{
		{id: "gone", received: time.Now().Add(-time.Minute), attachment: reportMIME("x@send.example", ""), body: ""},
	}
	f.graph.attachmentStatus = map[string]int{"gone": http.StatusServiceUnavailable}
	id := f.inbox(nil, "reports@global.example")
	_ = f.pollAll()
	if row := f.inboxRow(id); row.ProcessedCount != 0 {
		t.Fatalf("a failed message must be pending, processed %d", row.ProcessedCount)
	}
	// the message is deleted from the mailbox while pending
	f.graph.messages = nil
	f.makeDue(id)
	_ = f.pollAll()
	if row := f.inboxRow(id); row.ProcessedCount != 1 || row.LastError != "" {
		t.Fatalf("a deleted pending message must be resolved: processed %d error %q", row.ProcessedCount, row.LastError)
	}
}

func TestReportInboxPoll_CopyOfTheOriginalEmailIsNotAReport(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(nil, false)
	cr, rid := f.recipient(camp, "r@x.example", "orig-1@send.example")
	f.graph.readable["reports@global.example"] = true
	// the delivered simulation email itself, copied into the mailbox by a rule:
	// same Message-ID, links with the recipient id
	f.graph.messages = []fakeGraphMessage{{
		id: "copy", received: time.Now().Add(-time.Minute), from: "it@sender.example",
		outerMessageID: "orig-1@send.example",
		body:           `<a href="https://lure.example/?id=` + cr.String() + `">login</a>`,
	}}
	f.inbox(nil, "reports@global.example")
	_ = f.pollAll()
	if n := len(f.reportedEvents(camp, rid)); n != 0 {
		t.Fatalf("a copy of the delivered email must not count as a report, got %d events", n)
	}
}

func TestReportInboxPoll_ForwardMatchesOnReferences(t *testing.T) {
	f := newReportInboxFixture(t)
	camp := f.campaign(nil, false)
	_, rid := f.recipient(camp, "r@x.example", "orig-2@send.example")
	f.graph.readable["reports@global.example"] = true
	// a forward without any link, only the reference to the original
	f.graph.messages = []fakeGraphMessage{{
		id: "fw", received: time.Now().Add(-time.Minute), from: "r@x.example",
		references: "orig-2@send.example", body: "please check this",
	}}
	f.inbox(nil, "reports@global.example")
	_ = f.pollAll()
	if n := len(f.reportedEvents(camp, rid)); n != 1 {
		t.Fatalf("a forward that refers to the original must match, got %d events", n)
	}
}

func TestReportInboxPoll_ForwardMatchesOnLureCode(t *testing.T) {
	f := newReportInboxFixture(t)
	domainID := uuid.New()
	f.create(&database.Domain{}, map[string]any{"id": domainID, "name": "lure.example", "host_website": false})
	templateID := uuid.New()
	f.create(&database.CampaignTemplate{}, map[string]any{"id": templateID, "name": "t", "domain_id": domainID, "url_identifier_id": uuid.New()})
	camp := f.campaign(nil, false)
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", camp).Update("campaign_template_id", templateID).Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	crGenerated, ridGenerated := f.recipient(camp, "a@x.example", "")
	crCustom, ridCustom := f.recipient(camp, "b@x.example", "")
	f.db.Model(&database.CampaignRecipient{}).Where("id = ?", crGenerated).Update("lure_code", "K7Q2M9XR4TW8")
	f.db.Model(&database.CampaignRecipient{}).Where("id = ?", crCustom).Updates(map[string]any{"lure_code": "bobsmithcustom", "lure_code_custom": true})

	f.graph.readable["reports@global.example"] = true
	now := time.Now().Add(-2 * time.Minute)
	f.graph.messages = []fakeGraphMessage{
		// plain forwards with only the lure link, no recipient id anywhere
		{id: "gen", received: now, from: "a@x.example", body: `<a href="https://lure.example/login/K7Q2M9XR4TW8">sign in</a>`},
		{id: "custom", received: now.Add(time.Second), from: "b@x.example", body: `<a href="https://lure.example/login/bobsmithcustom">sign in</a>`},
		// the same code on a domain Phishing Club does not serve
		{id: "other", received: now.Add(2 * time.Second), from: "c@x.example", body: `<a href="https://elsewhere.example/K7Q2M9XR4TW8">x</a>`},
	}
	f.inbox(nil, "reports@global.example")
	_ = f.pollAll()
	if n := len(f.reportedEvents(camp, ridGenerated)); n != 1 {
		t.Fatalf("a forward with a generated lure code must match, got %d events", n)
	}
	if n := len(f.reportedEvents(camp, ridCustom)); n != 0 {
		t.Fatalf("a code set by hand must not be used to match, got %d events", n)
	}
}
