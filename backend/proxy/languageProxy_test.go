package proxy

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"go.uber.org/zap"
)

// languageProxyFixture is the proxy fixture with a template written in
// English and a German language row
type languageProxyFixture struct {
	*unlinkedProxyFixture
	germanEmail uuid.UUID
	campaignID  uuid.UUID
}

func newLanguageProxyFixture(t *testing.T) *languageProxyFixture {
	t.Helper()
	f := &languageProxyFixture{unlinkedProxyFixture: newUnlinkedProxyFixture(t)}
	f.handler.TemplateService = &service.Template{Common: service.Common{Logger: zap.NewNop().Sugar()}}
	if err := f.db.Model(&database.CampaignTemplate{}).Where("id = ?", f.templateID).Update("language", "en").Error; err != nil {
		t.Fatalf("set language: %v", err)
	}
	pageID, rowID := uuid.New(), uuid.New()
	f.germanEmail = uuid.New()
	f.create(&database.Page{ID: &pageID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "landing de", Content: "<p>de</p>"})
	f.create(&database.Email{
		ID: &f.germanEmail, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "email de", Content: "<p>mail de</p>",
		MailFrom: "it@de.phish.test", Subject: "Hallo Betreff", From: "IT <it@de.phish.test>",
	})
	f.create(&database.CampaignTemplateLanguage{
		ID: &rowID, CreatedAt: &f.now, UpdatedAt: &f.now, CampaignTemplateID: &f.templateID,
		Language: "de", EmailID: &f.germanEmail, LandingPageID: &pageID, IsUsable: true,
	})
	f.campaignID = f.campaign(nil, false)
	return f
}

func (f *languageProxyFixture) stampedSend(language string) uuid.UUID {
	f.t.Helper()
	id := f.send(f.campaignID, true)
	if language != "" {
		if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("language", language).Error; err != nil {
			f.t.Fatalf("stamp send: %v", err)
		}
	}
	return id
}

func (f *languageProxyFixture) page(content string) *model.Page {
	f.t.Helper()
	id := uuid.New()
	f.create(&database.Page{ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "page " + id.String()[:8], Content: content})
	page, err := f.handler.PageRepository.GetByID(context.Background(), &id, &repository.PageOption{})
	if err != nil {
		f.t.Fatalf("load page: %v", err)
	}
	return page
}

// TestProxyRequestContextLoadsStampedLanguage checks the request context of a
// proxied send holds the email of the stamped language, and a send whose
// language is missing is not served
func TestProxyRequestContextLoadsStampedLanguage(t *testing.T) {
	f := newLanguageProxyFixture(t)
	reqCtx, err := f.handler.initializeRequestContext(context.Background(), f.request(f.stampedSend("de")), f.domain)
	if err != nil || reqCtx == nil || reqCtx.CampaignTemplate == nil {
		t.Fatalf("initialize: %+v %v", reqCtx, err)
	}
	if got := reqCtx.CampaignTemplate.EmailID.MustGet(); got != f.germanEmail {
		t.Fatalf("request context email %v, want the German one", got)
	}
	if reqCtx.CampaignTemplate.Email == nil || reqCtx.CampaignTemplate.Email.MailHeaderSubject.MustGet().String() != "Hallo Betreff" {
		t.Fatal("the loaded email is not the German one")
	}

	reqCtx, err = f.handler.initializeRequestContext(context.Background(), f.request(f.stampedSend("sv")), f.domain)
	if err != nil || reqCtx != nil {
		t.Fatalf("a missing language must not be served, got %+v %v", reqCtx, err)
	}
}

// TestProxyDenyAndEvasionUseStampedEmail checks the deny and evasion renders
// use the email of the recipient's language, from the request context and
// when they read the template themselves
func TestProxyDenyAndEvasionUseStampedEmail(t *testing.T) {
	f := newLanguageProxyFixture(t)
	sendID := f.stampedSend("de")
	req := f.request(sendID)
	reqCtx, err := f.handler.initializeRequestContext(context.Background(), req, f.domain)
	if err != nil || reqCtx == nil {
		t.Fatalf("initialize: %+v %v", reqCtx, err)
	}
	deny := f.page("<p>deny [{{.Subject}}]</p>")
	evasion := f.page("<p>evasion [{{.Subject}}]</p>")
	cTemplate := reqCtx.CampaignTemplate

	check := func(name string, reqCtx *RequestContext) {
		t.Helper()
		out, err := f.handler.renderDenyPageTemplate(req, reqCtx, deny, reqCtx.Campaign, cTemplate)
		if err != nil || !strings.Contains(out, "deny [Hallo Betreff]") {
			t.Fatalf("%s deny page: %q %v", name, out, err)
		}
		out, err = f.handler.renderEvasionPageTemplate(req, reqCtx, evasion, reqCtx.Campaign, cTemplate, "landing", req.URL.String())
		if err != nil || !strings.Contains(out, "evasion [Hallo Betreff]") {
			t.Fatalf("%s evasion page: %q %v", name, out, err)
		}
	}
	check("cached", reqCtx)
	// without the template in the request context the renders read it in the
	// stamped language
	uncached := *reqCtx
	uncached.CampaignTemplate = nil
	check("read", &uncached)
}
