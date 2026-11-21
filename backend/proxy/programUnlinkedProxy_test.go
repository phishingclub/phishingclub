package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// unlinkedProxyFixture serves proxy requests on a temp SQLite database, so the
// unlinked anonymous program send path can be checked end to end
type unlinkedProxyFixture struct {
	t          *testing.T
	db         *gorm.DB
	handler    *ProxyHandler
	now        time.Time
	domain     *database.Domain
	templateID uuid.UUID
}

const (
	proxyTargetHost = "login.example.com"
	proxyPhishHost  = "phish.test"
)

func newUnlinkedProxyFixture(t *testing.T) *unlinkedProxyFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "proxy.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Identifier{},
		&database.Domain{},
		&database.Proxy{},
		&database.Page{},
		&database.Email{},
		&database.CampaignTemplate{},
		&database.Campaign{},
		&database.CampaignRecipient{},
		&database.CampaignEvent{},
		&database.Recipient{},
		&database.Program{},
		&database.ProgramEnrollment{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := zap.NewNop().Sugar()
	programRepo := &repository.Program{DB: db}
	campaignService := &service.Campaign{
		Common: service.Common{Logger: log},
		ProgramService: &service.Program{
			Common:            service.Common{Logger: log},
			ProgramRepository: programRepo,
		},
	}
	handler := &ProxyHandler{
		logger:                      log,
		PageRepository:              &repository.Page{DB: db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: db},
		CampaignRepository:          &repository.Campaign{DB: db},
		CampaignTemplateRepository:  &repository.CampaignTemplate{DB: db},
		DomainRepository:            &repository.Domain{DB: db},
		ProxyRepository:             &repository.Proxy{DB: db},
		IdentifierRepository:        &repository.Identifier{DB: db},
		CampaignService:             campaignService,
		cookieName:                  "ps",
	}
	f := &unlinkedProxyFixture{
		t:       t,
		db:      db,
		handler: handler,
		now:     time.Now().UTC(),
	}
	urlID, stateID, pageID, emailID, templateID, proxyID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.create(&database.Identifier{ID: &urlID, Name: "id"})
	f.create(&database.Identifier{ID: &stateID, Name: "st"})
	f.create(&database.Page{ID: &pageID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "landing", Content: "<p>x</p>"})
	f.create(&database.Email{
		ID: &emailID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "email", Content: "<p>mail</p>",
		MailFrom: "it@phish.test", Subject: "Hello", From: "IT <it@phish.test>",
	})
	f.create(&database.CampaignTemplate{
		ID: &templateID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "template", IsUsable: true,
		URLIdentifierID: &urlID, StateIdentifierID: &stateID, LandingPageID: &pageID, EmailID: &emailID,
	})
	f.templateID = templateID
	f.create(&database.Proxy{
		ID: &proxyID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "proxy",
		StartURL:    "https://" + proxyTargetHost + "/login",
		ProxyConfig: proxyTargetHost + ":\n  to: " + proxyPhishHost + "\n",
	})
	f.domain = &database.Domain{
		ID: uuid.New(), Name: proxyPhishHost, Type: "proxy",
		ProxyID: &proxyID, ProxyTargetDomain: proxyTargetHost,
	}
	f.create(f.domain)
	return f
}

func (f *unlinkedProxyFixture) create(row any) {
	f.t.Helper()
	if err := f.db.Create(row).Error; err != nil {
		f.t.Fatalf("create %T: %v", row, err)
	}
}

// program creates a started program, anonymous and closed as asked
func (f *unlinkedProxyFixture) program(anonymous, closed bool) uuid.UUID {
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

// campaign creates an open campaign of the template, hidden in programID when set
func (f *unlinkedProxyFixture) campaign(programID *uuid.UUID, anonymous bool) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	row := &database.Campaign{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "campaign " + id.String(),
		CampaignTemplateID: &f.templateID, ProgramID: programID, IsAnonymous: anonymous,
	}
	f.create(row)
	return id
}

// send creates a send of the campaign sent 20 days ago. linked keeps the person
// link, else only the pseudonym is left, as after the identity retention.
func (f *unlinkedProxyFixture) send(campaignID uuid.UUID, linked bool) uuid.UUID {
	f.t.Helper()
	recipientID, pseudonym, sendID := uuid.New(), uuid.New(), uuid.New()
	email := "alice." + recipientID.String()[:8] + "@example.com"
	f.create(&database.Recipient{
		ID: &recipientID, CreatedAt: &f.now, UpdatedAt: &f.now, Email: &email,
		FirstName: "Alice", LastName: "Smith",
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
	var campaign database.Campaign
	if err := f.db.Where("id = ?", campaignID).First(&campaign).Error; err != nil {
		f.t.Fatalf("load campaign: %v", err)
	}
	// the person stays enrolled with the pseudonym
	if campaign.ProgramID != nil {
		enrollmentID := uuid.New()
		f.create(&database.ProgramEnrollment{
			ID: &enrollmentID, CreatedAt: &f.now, UpdatedAt: &f.now,
			ProgramID: campaign.ProgramID, RecipientID: &recipientID,
			Status: "active", AnonymizedID: &pseudonym,
		})
	}
	return sendID
}

func (f *unlinkedProxyFixture) request(sendID uuid.UUID) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "https://"+proxyPhishHost+"/login?id="+sendID.String(), nil)
	req.RemoteAddr = "192.0.2.10:40000"
	return req
}

func (f *unlinkedProxyFixture) events(campaignID uuid.UUID) int64 {
	f.t.Helper()
	var count int64
	if err := f.db.Model(&database.CampaignEvent{}).Where("campaign_id = ?", campaignID).Count(&count).Error; err != nil {
		f.t.Fatalf("count events: %v", err)
	}
	return count
}

// TestUnlinkedAnonymousProgramProxyServesExpired checks the proxy never runs for
// a send of an anonymous program that lost its person link. A clean expired page
// is served and nothing is stored.
func TestUnlinkedAnonymousProgramProxyServesExpired(t *testing.T) {
	f := newUnlinkedProxyFixture(t)
	programID := f.program(true, false)
	campaignID := f.campaign(&programID, true)
	sendID := f.send(campaignID, false)

	req := f.request(sendID)
	reqCtx, err := f.handler.initializeRequestContext(context.Background(), req, f.domain)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if reqCtx == nil || !reqCtx.ServeExpired {
		t.Fatalf("an unlinked anonymous program send must serve the expired page, got %+v", reqCtx)
	}
	if reqCtx.RecipientID != nil {
		t.Fatalf("an unlinked send must carry no recipient id, got %v", reqCtx.RecipientID)
	}

	// the whole request serves the clean expired page and stores nothing
	w := httptest.NewRecorder()
	if err := f.handler.HandleHTTPRequest(w, f.request(sendID), f.domain); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if w.Code != http.StatusGone {
		t.Fatalf("expired page status, want %d got %d", http.StatusGone, w.Code)
	}
	if !strings.Contains(w.Body.String(), "expired") {
		t.Fatalf("expired page body, got %q", w.Body.String())
	}
	if n := f.events(campaignID); n != 0 {
		t.Fatalf("the proxy must store nothing for an unlinked send, got %d events", n)
	}
}

// TestLinkedProxySendProceeds checks a send that keeps its person link is still
// put through the proxy, with its recipient loaded, as before.
func TestLinkedProxySendProceeds(t *testing.T) {
	f := newUnlinkedProxyFixture(t)
	programID := f.program(true, false)
	campaignID := f.campaign(&programID, true)
	sendID := f.send(campaignID, true)

	reqCtx, err := f.handler.initializeRequestContext(context.Background(), f.request(sendID), f.domain)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if reqCtx == nil || reqCtx.ServeExpired {
		t.Fatalf("a linked send must not serve the expired page, got %+v", reqCtx)
	}
	if reqCtx.RecipientID == nil {
		t.Fatal("a linked send must carry its recipient id")
	}
}

// TestUnlinkedProxyOutsideAnonymousProgramErrors checks every other send without
// a person link does not take the expired path: a normal anonymous campaign, a
// named program and a closed anonymous program. The missing recipient id still
// errors as before, so the proxy never runs on a send it can not place.
func TestUnlinkedProxyOutsideAnonymousProgramErrors(t *testing.T) {
	f := newUnlinkedProxyFixture(t)
	named := f.program(false, false)
	closedProgram := f.program(true, true)
	cases := map[string]uuid.UUID{
		"normal anonymous campaign": f.campaign(nil, true),
		"named program":             f.campaign(&named, false),
		"closed anonymous program":  f.campaign(&closedProgram, true),
	}
	for name, campaignID := range cases {
		sendID := f.send(campaignID, false)
		reqCtx, err := f.handler.initializeRequestContext(context.Background(), f.request(sendID), f.domain)
		if err == nil {
			t.Fatalf("%s: a send without a person link must error, got %+v", name, reqCtx)
		}
		if reqCtx != nil {
			t.Fatalf("%s: no request context on error, got %+v", name, reqCtx)
		}
	}
}

// TestSessionEndsWhenSendIsUnlinked checks a proxy session that began while
// its anonymous program send named the person ends once the identity
// retention removed the link: the next request serves the expired page and
// the session is deleted. A session of a send that keeps its link goes on.
func TestSessionEndsWhenSendIsUnlinked(t *testing.T) {
	f := newUnlinkedProxyFixture(t)
	f.handler.SessionManager = service.NewProxySessionManager(service.Common{Logger: zap.NewNop().Sugar()})
	programID := f.program(true, false)
	campaignID := f.campaign(&programID, true)
	sendID := f.send(campaignID, true)

	reqCtx, err := f.handler.initializeRequestContext(context.Background(), f.request(sendID), f.domain)
	if err != nil || reqCtx == nil || reqCtx.ServeExpired {
		t.Fatalf("a linked send starts a session, got %+v %v", reqCtx, err)
	}
	session := &service.ProxySession{
		ID: "s1", CampaignRecipientID: &sendID, CampaignID: &campaignID,
		RecipientID: reqCtx.RecipientID, Campaign: reqCtx.Campaign,
	}
	f.handler.SessionManager.StoreSession(session.ID, session)
	resolve := func() *RequestContext {
		t.Helper()
		next := &RequestContext{SessionID: session.ID}
		if err := f.handler.resolveSessionContext(httptest.NewRequest(http.MethodGet, "https://"+proxyPhishHost+"/next", nil), next, false); err != nil {
			t.Fatalf("resolve: %v", err)
		}
		return next
	}
	if next := resolve(); next.ServeExpired {
		t.Fatal("a session of a linked send goes on")
	}
	if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", sendID).Update("recipient_id", nil).Error; err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if next := resolve(); !next.ServeExpired {
		t.Fatal("a session of an unlinked send must serve the expired page")
	}
	if _, ok := f.handler.SessionManager.GetSession(session.ID); ok {
		t.Fatal("the session of an unlinked send must be deleted")
	}
}

// a session must also end when the person of the unlinked send was disabled,
// even though the link would then no longer be served on the pseudonym
func TestSessionEndsWhenSendIsUnlinkedAndPersonDisabled(t *testing.T) {
	f := newUnlinkedProxyFixture(t)
	f.handler.SessionManager = service.NewProxySessionManager(service.Common{Logger: zap.NewNop().Sugar()})
	programID := f.program(true, false)
	campaignID := f.campaign(&programID, true)
	sendID := f.send(campaignID, true)

	reqCtx, err := f.handler.initializeRequestContext(context.Background(), f.request(sendID), f.domain)
	if err != nil || reqCtx == nil || reqCtx.ServeExpired {
		t.Fatalf("a linked send starts a session, got %+v %v", reqCtx, err)
	}
	session := &service.ProxySession{
		ID: "s1", CampaignRecipientID: &sendID, CampaignID: &campaignID,
		RecipientID: reqCtx.RecipientID, Campaign: reqCtx.Campaign,
	}
	f.handler.SessionManager.StoreSession(session.ID, session)

	var row database.CampaignRecipient
	if err := f.db.Where("id = ?", sendID).First(&row).Error; err != nil || row.RecipientID == nil {
		t.Fatalf("read send: %v", err)
	}
	if err := f.db.Model(&database.Recipient{}).Where("id = ?", row.RecipientID).Update("scim_soft_deleted_at", f.now).Error; err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", sendID).Update("recipient_id", nil).Error; err != nil {
		t.Fatalf("unlink: %v", err)
	}
	next := &RequestContext{SessionID: session.ID}
	if err := f.handler.resolveSessionContext(httptest.NewRequest(http.MethodGet, "https://"+proxyPhishHost+"/next", nil), next, false); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !next.ServeExpired {
		t.Fatal("a session of an unlinked send of a disabled person must serve the expired page")
	}
	if _, ok := f.handler.SessionManager.GetSession(session.ID); ok {
		t.Fatal("the session must be deleted")
	}
}
