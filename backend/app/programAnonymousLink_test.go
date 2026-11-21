package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// pageFixture serves the phishing pages of sends on a temp SQLite database
type pageFixture struct {
	t          *testing.T
	db         *gorm.DB
	server     *Server
	now        time.Time
	domain     *database.Domain
	templateID uuid.UUID
}

// the page a send renders, with personal fields
const pageContent = `<p>Hi [{{.FirstName}}] [{{.LastName}}] [{{.Email}}] [{{.Position}}] [{{.Department}}]</p>`

func newPageFixture(t *testing.T) *pageFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := filepath.Join(t.TempDir(), "page.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Identifier{},
		&database.Company{},
		&database.Domain{},
		&database.Page{},
		&database.Email{},
		&database.CampaignTemplate{},
		&database.Campaign{},
		&database.CampaignRecipient{},
		&database.CampaignEvent{},
		&database.CampaignWebhook{},
		&database.Recipient{},
		&database.Program{},
		&database.ProgramEnrollment{},
		&database.AllowDeny{},
		&database.Event{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, name := range []string{
		data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ,
		data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED,
		data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA,
		data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED,
		data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED,
		data.EVENT_CAMPAIGN_RECIPIENT_DENY_PAGE_VISITED,
	} {
		if cache.EventIDByName[name] == nil {
			id := uuid.New()
			cache.EventIDByName[name] = &id
			cache.EventNameByID[id.String()] = name
		}
	}
	log := zap.NewNop().Sugar()
	repos := &Repositories{
		Identifier:        &repository.Identifier{DB: db},
		Campaign:          &repository.Campaign{DB: db},
		CampaignRecipient: &repository.CampaignRecipient{DB: db},
		CampaignTemplate:  &repository.CampaignTemplate{DB: db},
		Recipient:         &repository.Recipient{DB: db},
		Program:           &repository.Program{DB: db},
		Page:              &repository.Page{DB: db},
		Email:             &repository.Email{DB: db},
	}
	services := &Services{
		Template: &service.Template{Common: service.Common{Logger: log}},
		Campaign: &service.Campaign{
			Common:                      service.Common{Logger: log},
			CampaignRepository:          repos.Campaign,
			CampaignRecipientRepository: repos.CampaignRecipient,
			RecipientRepository:         repos.Recipient,
		},
	}
	f := &pageFixture{
		t:      t,
		db:     db,
		now:    time.Now().UTC(),
		domain: &database.Domain{Name: "phish.test"},
		server: &Server{db: db, logger: log, repositories: repos, services: services},
	}
	urlID, stateID, pageID, emailID, templateID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.create(&database.Identifier{ID: &urlID, Name: "id"})
	f.create(&database.Identifier{ID: &stateID, Name: "st"})
	f.create(&database.Page{ID: &pageID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "landing", Content: pageContent})
	f.create(&database.Email{
		ID: &emailID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "email", Content: "<p>mail</p>",
		MailFrom: "it@phish.test", Subject: "Hello", From: "IT <it@phish.test>",
	})
	f.create(&database.CampaignTemplate{
		ID: &templateID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "template", IsUsable: true,
		URLIdentifierID: &urlID, StateIdentifierID: &stateID, LandingPageID: &pageID, EmailID: &emailID,
	})
	f.templateID = templateID
	return f
}

func (f *pageFixture) create(row any) {
	f.t.Helper()
	if err := f.db.Create(row).Error; err != nil {
		f.t.Fatalf("create %T: %v", row, err)
	}
}

// program creates a started program
func (f *pageFixture) program(anonymous bool, closed bool) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	started := f.now.Add(-60 * 24 * time.Hour)
	row := &database.Program{
		ID: &id, CreatedAt: &started, UpdatedAt: &started, Name: "program " + id.String(),
		StartAt: &started, StartedAt: &started, IsAnonymous: anonymous,
	}
	if closed {
		row.ClosedAt = &f.now
	}
	f.create(row)
	return id
}

// campaign creates an open campaign of the template, hidden in programID
// when it is set, closed when closed is set
func (f *pageFixture) campaign(programID *uuid.UUID, anonymous bool, closed bool) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	row := &database.Campaign{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "campaign " + id.String(),
		CampaignTemplateID: &f.templateID, ProgramID: programID, IsAnonymous: anonymous,
	}
	if closed {
		at := f.now.Add(-time.Hour)
		row.ClosedAt = &at
	}
	f.create(row)
	return id
}

// send creates a send of the campaign to a new recipient, sent 20 days ago.
// linked keeps the person link, else only the pseudonym is left.
func (f *pageFixture) send(campaignID uuid.UUID, linked bool) (sendID, recipientID, pseudonym uuid.UUID) {
	f.t.Helper()
	recipientID, pseudonym, sendID = uuid.New(), uuid.New(), uuid.New()
	email := "alice." + recipientID.String()[:8] + "@example.com"
	f.create(&database.Recipient{
		ID: &recipientID, CreatedAt: &f.now, UpdatedAt: &f.now, Email: &email,
		FirstName: "Alice", LastName: "Smith", Position: "Clerk", Department: "Finance",
	})
	sentAt := f.now.Add(-20 * 24 * time.Hour)
	row := &database.CampaignRecipient{
		ID: &sendID, CreatedAt: &sentAt, UpdatedAt: &sentAt, SentAt: &sentAt,
		CampaignID: &campaignID, AnonymizedID: &pseudonym,
	}
	if linked {
		row.RecipientID = &recipientID
	}
	f.create(row)
	f.enroll(campaignID, recipientID, pseudonym)
	return sendID, recipientID, pseudonym
}

// enroll adds the enrollment of the recipient that carries the pseudonym in
// the program of the campaign, none for a campaign of no program
func (f *pageFixture) enroll(campaignID, recipientID, pseudonym uuid.UUID) {
	f.t.Helper()
	var campaign database.Campaign
	if err := f.db.Where("id = ?", campaignID).First(&campaign).Error; err != nil {
		f.t.Fatalf("load campaign: %v", err)
	}
	if campaign.ProgramID == nil {
		return
	}
	id := uuid.New()
	f.create(&database.ProgramEnrollment{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now,
		ProgramID: campaign.ProgramID, RecipientID: &recipientID,
		Status: "active", AnonymizedID: &pseudonym,
	})
}

// visit requests the landing page of a send, a POST submits a password
func (f *pageFixture) visit(method string, sendID uuid.UUID) (*httptest.ResponseRecorder, bool) {
	f.t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var body *strings.Reader
	if method == http.MethodPost {
		body = strings.NewReader("password=hunter2-secret")
	} else {
		body = strings.NewReader("")
	}
	c.Request = httptest.NewRequest(method, "https://phish.test/login?id="+sendID.String(), body)
	if method == http.MethodPost {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c.Request.RemoteAddr = "192.0.2.10:40000"
	served, err := f.server.checkAndServePhishingPage(c, f.domain)
	if err != nil {
		f.t.Fatalf("serve: %v", err)
	}
	return w, served
}

func (f *pageFixture) events(campaignID uuid.UUID) []database.CampaignEvent {
	f.t.Helper()
	rows := []database.CampaignEvent{}
	if err := f.db.Where("campaign_id = ?", campaignID).Find(&rows).Error; err != nil {
		f.t.Fatalf("events: %v", err)
	}
	return rows
}

func eventsNamed(rows []database.CampaignEvent, name string) []database.CampaignEvent {
	out := []database.CampaignEvent{}
	for _, r := range rows {
		if r.EventID != nil && *r.EventID == *cache.EventIDByName[name] {
			out = append(out, r)
		}
	}
	return out
}

// TestAnonymousProgramOldLinkServes checks a send of an anonymous program that
// lost its person link after 14 days still serves its page, with every
// personal field empty, and records the click and the submit on its
// pseudonym only, with no identity and no submitted data
func TestAnonymousProgramOldLinkServes(t *testing.T) {
	f := newPageFixture(t)
	programID := f.program(true, false)
	campaignID := f.campaign(&programID, true, false)
	sendID, _, pseudonym := f.send(campaignID, false)

	w, served := f.visit(http.MethodGet, sendID)
	if !served || w.Code != http.StatusOK {
		t.Fatalf("the page must be served, got %v %d", served, w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "Hi [] [] [] [] []") || strings.Contains(body, "Alice") || strings.Contains(body, "@example.com") {
		t.Fatalf("personal fields must render empty, got %q", body)
	}
	if w, served := f.visit(http.MethodPost, sendID); !served || w.Code != http.StatusOK {
		t.Fatalf("the submit must be served, got %v %d", served, w.Code)
	}
	rows := f.events(campaignID)
	for _, name := range []string{data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA, data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ} {
		if len(eventsNamed(rows, name)) == 0 {
			t.Fatalf("no %s event recorded", name)
		}
	}
	// the message read event is recorded once, deduplicated on the pseudonym
	if n := len(eventsNamed(rows, data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ)); n != 1 {
		t.Fatalf("message read recorded %d times, want 1", n)
	}
	for _, r := range rows {
		if r.RecipientID != nil || r.AnonymizedID == nil || *r.AnonymizedID != pseudonym {
			t.Fatalf("an event must carry the pseudonym only: %+v", r)
		}
		if r.IPAddress != "" || r.UserAgent != "" || r.Data != "" || r.Metadata != "" {
			t.Fatalf("an event must carry no identity or data: %+v", r)
		}
	}
	// the notable event is written alone and the person link stays removed
	var send database.CampaignRecipient
	if err := f.db.Where("id = ?", sendID).First(&send).Error; err != nil {
		t.Fatalf("load send: %v", err)
	}
	if send.RecipientID != nil || send.NotableEventID == nil ||
		*send.NotableEventID != *cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA] {
		t.Fatalf("send after the visits: %+v", send)
	}
}

// TestUnlinkedSendOutsideAnonymousProgramStops checks every other send without
// a person link still stops before any page or event: a normal anonymous
// campaign, a closed normal anonymized campaign, a named program and a closed
// anonymous program
func TestUnlinkedSendOutsideAnonymousProgramStops(t *testing.T) {
	f := newPageFixture(t)
	named := f.program(false, false)
	closedProgram := f.program(true, true)
	cases := map[string]uuid.UUID{
		"open anonymous campaign":   f.campaign(nil, true, false),
		"closed anonymous campaign": f.campaign(nil, true, true),
		"named program":             f.campaign(&named, false, false),
		"closed anonymous program":  f.campaign(&closedProgram, true, false),
	}
	for name, campaignID := range cases {
		sendID, _, _ := f.send(campaignID, false)
		w, served := f.visit(http.MethodGet, sendID)
		if served || w.Body.Len() != 0 {
			t.Fatalf("%s: an unlinked send must not be served, got %v %q", name, served, w.Body.String())
		}
		if rows := f.events(campaignID); len(rows) != 0 {
			t.Fatalf("%s: no event may be recorded, got %d", name, len(rows))
		}
	}
}

// TestLinkedSendsServeAsBefore checks a named program send renders the person
// and records events on the recipient, and an anonymous program send within
// its first 14 days records on the pseudonym, as before
func TestLinkedSendsServeAsBefore(t *testing.T) {
	f := newPageFixture(t)
	named := f.program(false, false)
	campaignID := f.campaign(&named, false, false)
	sendID, recipientID, _ := f.send(campaignID, true)
	w, served := f.visit(http.MethodGet, sendID)
	want := "Hi [Alice] [Smith] [alice." + recipientID.String()[:8] + "@example.com] [Clerk] [Finance]"
	if !served || !strings.Contains(w.Body.String(), want) {
		t.Fatalf("a named send renders the person, got %v %q", served, w.Body.String())
	}
	rows := f.events(campaignID)
	if len(eventsNamed(rows, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)) != 1 {
		t.Fatalf("named visit not recorded: %+v", rows)
	}
	for _, r := range rows {
		if r.RecipientID == nil || *r.RecipientID != recipientID || r.AnonymizedID != nil {
			t.Fatalf("a named event carries the recipient: %+v", r)
		}
	}

	anon := f.program(true, false)
	anonCampaign := f.campaign(&anon, true, false)
	anonSend, _, pseudonym := f.send(anonCampaign, true)
	if _, served := f.visit(http.MethodGet, anonSend); !served {
		t.Fatal("a recent anonymous program send must be served")
	}
	for _, r := range f.events(anonCampaign) {
		if r.RecipientID != nil || r.AnonymizedID == nil || *r.AnonymizedID != pseudonym {
			t.Fatalf("an anonymous event carries the pseudonym only: %+v", r)
		}
	}
}

// TestUnlinkedSendOfGonePersonStops checks the link of an unlinked anonymous
// program send stops when its person was deleted or disabled by SCIM, as in a
// normal campaign, while the send of a person still enrolled is served
func TestUnlinkedSendOfGonePersonStops(t *testing.T) {
	f := newPageFixture(t)
	programID := f.program(true, false)
	campaignID := f.campaign(&programID, true, false)

	// a deleted recipient: its enrollments and its row are removed
	deleted, deletedRecipient, _ := f.send(campaignID, false)
	if err := f.db.Where("recipient_id = ?", deletedRecipient).Delete(&database.ProgramEnrollment{}).Error; err != nil {
		t.Fatalf("remove enrollment: %v", err)
	}
	if err := f.db.Where("id = ?", deletedRecipient).Delete(&database.Recipient{}).Error; err != nil {
		t.Fatalf("delete recipient: %v", err)
	}
	// a SCIM disabled recipient keeps its enrollment
	disabled, disabledRecipient, _ := f.send(campaignID, false)
	if err := f.db.Model(&database.Recipient{}).Where("id = ?", disabledRecipient).
		Update("scim_soft_deleted_at", f.now).Error; err != nil {
		t.Fatalf("disable recipient: %v", err)
	}
	for name, sendID := range map[string]uuid.UUID{"deleted": deleted, "scim disabled": disabled} {
		w, served := f.visit(http.MethodGet, sendID)
		if served || w.Body.Len() != 0 {
			t.Fatalf("%s: the link must stop, got %v %q", name, served, w.Body.String())
		}
	}
	if rows := f.events(campaignID); len(rows) != 0 {
		t.Fatalf("no event may be recorded for a gone person, got %d", len(rows))
	}
	// a person still enrolled is served
	kept, _, _ := f.send(campaignID, false)
	if w, served := f.visit(http.MethodGet, kept); !served || w.Code != http.StatusOK {
		t.Fatalf("an enrolled person must be served, got %v %d", served, w.Code)
	}
}

// TestLinkedAnonymousReadRecordedOnce checks the message read event of a
// linked send of an anonymous campaign is recorded once over many visits,
// and that of a normal campaign too
func TestLinkedAnonymousReadRecordedOnce(t *testing.T) {
	f := newPageFixture(t)
	anon := f.program(true, false)
	named := f.program(false, false)
	for name, campaignID := range map[string]uuid.UUID{
		"anonymous program":  f.campaign(&anon, true, false),
		"named program":      f.campaign(&named, false, false),
		"anonymous campaign": f.campaign(nil, true, false),
	} {
		sendID, _, _ := f.send(campaignID, true)
		for i := 0; i < 3; i++ {
			if _, served := f.visit(http.MethodGet, sendID); !served {
				t.Fatalf("%s: the page must be served", name)
			}
		}
		rows := f.events(campaignID)
		if n := len(eventsNamed(rows, data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ)); n != 1 {
			t.Fatalf("%s: message read recorded %d times, want 1", name, n)
		}
		if n := len(eventsNamed(rows, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)); n != 3 {
			t.Fatalf("%s: page visit recorded %d times, want 3", name, n)
		}
	}
}

// setLandingContent replaces the shared template's landing page content, so a
// test can serve a page that uses a given template function
func (f *pageFixture) setLandingContent(content string) {
	f.t.Helper()
	if err := f.db.Model(&database.Page{}).Where("name = ?", "landing").
		Update("content", content).Error; err != nil {
		f.t.Fatalf("set landing content: %v", err)
	}
}

// TestUnlinkedSendOfLivePageServesExpired checks a device code or remote
// browser page is never served to a send that lost its person link: a clean
// 410 expired page is served and nothing is stored, while a linked send of the
// same page still renders
func TestUnlinkedSendOfLivePageServesExpired(t *testing.T) {
	for _, content := range []string{
		`<p>code {{MicrosoftDeviceCode}}</p>`,
		`<p>{{RemoteBrowserScript "rb"}}</p>`,
	} {
		f := newPageFixture(t)
		f.setLandingContent(content)
		programID := f.program(true, false)
		campaignID := f.campaign(&programID, true, false)
		unlinked, _, _ := f.send(campaignID, false)
		w, served := f.visit(http.MethodGet, unlinked)
		if !served || w.Code != http.StatusGone {
			t.Fatalf("%q: an unlinked live page must serve the expired page, got %v %d", content, served, w.Code)
		}
		if !strings.Contains(strings.ToLower(w.Body.String()), "expired") {
			t.Fatalf("%q: expired page body, got %q", content, w.Body.String())
		}
		if rows := f.events(campaignID); len(rows) != 0 {
			t.Fatalf("%q: no event may be recorded, got %d", content, len(rows))
		}
		// a linked send of the same page still renders
		linked, _, _ := f.send(campaignID, true)
		if w, served := f.visit(http.MethodGet, linked); !served || w.Code != http.StatusOK {
			t.Fatalf("%q: a linked send must still render, got %v %d", content, served, w.Code)
		}
	}
}
