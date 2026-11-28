package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
)

// languagePageFixture is a page fixture whose template is written in English
// with a German language row
type languagePageFixture struct {
	*pageFixture
	campaignID uuid.UUID
	denyPageID uuid.UUID
}

func newLanguagePageFixture(t *testing.T) *languagePageFixture {
	t.Helper()
	f := &languagePageFixture{pageFixture: newPageFixture(t)}
	if err := f.db.Model(&database.CampaignTemplate{}).Where("id = ?", f.templateID).Update("language", "en").Error; err != nil {
		t.Fatalf("set language: %v", err)
	}
	pageID, emailID, rowID := uuid.New(), uuid.New(), uuid.New()
	f.create(&database.Page{ID: &pageID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "landing de", Content: `<p>Hallo [{{.FirstName}}]</p>`})
	f.create(&database.Email{
		ID: &emailID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "email de", Content: "<p>mail de</p>",
		MailFrom: "it@de.phish.test", Subject: "Hallo Betreff", From: "IT <it@de.phish.test>",
	})
	f.create(&database.CampaignTemplateLanguage{
		ID: &rowID, CreatedAt: &f.now, UpdatedAt: &f.now, CampaignTemplateID: &f.templateID,
		Language: "de", EmailID: &emailID, LandingPageID: &pageID, IsUsable: true,
	})
	f.denyPageID = uuid.New()
	f.create(&database.Page{ID: &f.denyPageID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "deny", Content: `<p>deny [{{.Subject}}]</p>`})
	f.campaignID = f.campaign(nil, false, false)
	return f
}

// stampedSend is a linked send of the campaign stamped with language, none
// when it is empty
func (f *languagePageFixture) stampedSend(language string) uuid.UUID {
	f.t.Helper()
	sendID, _, _ := f.send(f.campaignID, true)
	if language != "" {
		if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", sendID).Update("language", language).Error; err != nil {
			f.t.Fatalf("stamp send: %v", err)
		}
	}
	return sendID
}

// deny renders the deny page of a send
func (f *languagePageFixture) deny(sendID uuid.UUID) *httptest.ResponseRecorder {
	f.t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "https://phish.test/login?id="+sendID.String(), nil)
	c.Request.RemoteAddr = "192.0.2.10:40000"
	if err := f.server.renderDenyPage(c, f.domain, &f.denyPageID); err != nil {
		f.t.Fatalf("deny page: %v", err)
	}
	return w
}

// TestLanguagePageServesStampedLanguage checks each recipient gets the page
// of the language stamped on its send, a send with no stamp the template
// language, and a request can not pick another language
func TestLanguagePageServesStampedLanguage(t *testing.T) {
	f := newLanguagePageFixture(t)
	german := f.stampedSend("de")
	english := f.stampedSend("en")
	legacy := f.stampedSend("")

	w, served := f.visit(http.MethodGet, german)
	if !served || !strings.Contains(w.Body.String(), "Hallo [Alice]") {
		t.Fatalf("German send: %v %q", served, w.Body.String())
	}
	for name, id := range map[string]uuid.UUID{"English": english, "no stamp": legacy} {
		w, served := f.visit(http.MethodGet, id)
		if !served || !strings.Contains(w.Body.String(), "Hi [Alice]") || strings.Contains(w.Body.String(), "Hallo") {
			t.Fatalf("%s send: %v %q", name, served, w.Body.String())
		}
	}

	// language request data is ignored
	w = httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "https://phish.test/login?id="+english.String()+"&lang=de", nil)
	c.Request.Header.Set("Accept-Language", "de-DE,de;q=0.9")
	c.Request.AddCookie(&http.Cookie{Name: "lang", Value: "de"})
	c.Request.RemoteAddr = "192.0.2.10:40000"
	if served, err := f.server.checkAndServePhishingPage(c, f.domain); err != nil || !served || strings.Contains(w.Body.String(), "Hallo") {
		t.Fatalf("request data picked a language: %v %v %q", served, err, w.Body.String())
	}
}

// TestLanguagePageMissingLanguageIsNotServed checks a send stamped with a
// language the template does not have, or no longer has usable, is not served,
// like a deleted template, and never gets the template language in its place
func TestLanguagePageMissingLanguageIsNotServed(t *testing.T) {
	f := newLanguagePageFixture(t)
	missing := f.stampedSend("sv")
	w, served := f.visit(http.MethodGet, missing)
	if served || w.Body.Len() != 0 {
		t.Fatalf("a missing language must not be served, got %v %q", served, w.Body.String())
	}
	broken := f.stampedSend("de")
	if err := f.db.Model(&database.CampaignTemplateLanguage{}).Where("campaign_template_id = ?", f.templateID).Update("is_usable", false).Error; err != nil {
		t.Fatalf("break language: %v", err)
	}
	w, served = f.visit(http.MethodGet, broken)
	if served || w.Body.Len() != 0 {
		t.Fatalf("a language that is not usable must not be served, got %v %q", served, w.Body.String())
	}
	if rows := f.events(f.campaignID); len(rows) != 0 {
		t.Fatalf("no event may be recorded, got %d", len(rows))
	}
	if w := f.deny(missing); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "deny") {
		t.Fatalf("the deny page of a missing language: %d %q", w.Code, w.Body.String())
	}
}

// TestLanguageDenyPageUsesStampedEmail checks the deny page renders with the
// email of the recipient's language
func TestLanguageDenyPageUsesStampedEmail(t *testing.T) {
	f := newLanguagePageFixture(t)
	if w := f.deny(f.stampedSend("de")); !strings.Contains(w.Body.String(), "deny [Hallo Betreff]") {
		t.Fatalf("German deny page: %q", w.Body.String())
	}
	if w := f.deny(f.stampedSend("")); !strings.Contains(w.Body.String(), "deny [Hello]") {
		t.Fatalf("English deny page: %q", w.Body.String())
	}
}
