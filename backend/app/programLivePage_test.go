package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// livePage is a page that starts a device code session for the person
const livePage = `<p>{{MicrosoftDeviceCode .}}</p>`

// page adds a page with the given content and returns its id
func (f *pageFixture) page(name string, content string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.create(&database.Page{ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: name + " " + id.String(), Content: content})
	return id
}

// setCampaignPages sets the evasion and deny pages of a campaign
func (f *pageFixture) setCampaignPages(campaignID uuid.UUID, evasion *uuid.UUID, deny *uuid.UUID) {
	f.t.Helper()
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", campaignID).
		Updates(map[string]any{"evasion_page_id": evasion, "deny_page_id": deny}).Error; err != nil {
		f.t.Fatalf("set pages: %v", err)
	}
}

func (f *pageFixture) loadCampaign(campaignID uuid.UUID) *model.Campaign {
	f.t.Helper()
	campaign, err := f.server.repositories.Campaign.GetByID(f.t.Context(), &campaignID, &repository.CampaignOption{})
	if err != nil {
		f.t.Fatalf("load campaign: %v", err)
	}
	return campaign
}

func (f *pageFixture) loadTemplate() *model.CampaignTemplate {
	f.t.Helper()
	t, err := f.server.repositories.CampaignTemplate.GetByID(f.t.Context(), &f.templateID, &repository.CampaignTemplateOption{})
	if err != nil {
		f.t.Fatalf("load template: %v", err)
	}
	return t
}

// TestLivePageCheckReadsEvasionAndDenyPages checks the live page check reads
// the evasion and deny pages of the campaign as well as the template pages
func TestLivePageCheckReadsEvasionAndDenyPages(t *testing.T) {
	f := newPageFixture(t)
	programID := f.program(true, false)
	campaignID := f.campaign(&programID, true, false)
	plain := f.page("plain", "<p>wait</p>")
	live := f.page("live", livePage)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "https://phish.test/", nil)
	for _, tc := range []struct {
		name    string
		evasion *uuid.UUID
		deny    *uuid.UUID
		want    bool
	}{
		{"no pages", nil, nil, false},
		{"plain pages", &plain, &plain, false},
		{"live evasion page", &live, &plain, true},
		{"live deny page", nil, &live, true},
	} {
		f.setCampaignPages(campaignID, tc.evasion, tc.deny)
		got, err := f.server.templateUsesLiveRecipientFuncs(c, f.loadTemplate(), f.loadCampaign(campaignID))
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %v %v, want %v", tc.name, got, err, tc.want)
		}
	}
}

// TestUnlinkedSendLiveEvasionPageExpires checks an unlinked send of an
// anonymous program whose evasion page uses a live function gets the clean
// expired page and no event
func TestUnlinkedSendLiveEvasionPageExpires(t *testing.T) {
	f := newPageFixture(t)
	programID := f.program(true, false)
	campaignID := f.campaign(&programID, true, false)
	live := f.page("live", livePage)
	plain := f.page("deny", "<p>no</p>")
	f.setCampaignPages(campaignID, &live, &plain)
	sendID, _, _ := f.send(campaignID, false)
	w, served := f.visit(http.MethodGet, sendID)
	if !served || w.Code != http.StatusGone {
		t.Fatalf("expected the expired page, got %v %d", served, w.Code)
	}
	if rows := f.events(campaignID); len(rows) != 0 {
		t.Fatalf("no event may be recorded, got %d", len(rows))
	}
}

// TestUnlinkedSendLiveDenyPageExpires checks the deny page an allow or deny
// list serves to an unlinked send of an anonymous program is the clean
// expired page when it uses a live function
func TestUnlinkedSendLiveDenyPageExpires(t *testing.T) {
	f := newPageFixture(t)
	programID := f.program(true, false)
	campaignID := f.campaign(&programID, true, false)
	live := f.page("live", livePage)
	f.setCampaignPages(campaignID, nil, &live)
	sendID, _, _ := f.send(campaignID, false)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "https://phish.test/login?id="+sendID.String(), nil)
	if err := f.server.renderDenyPage(c, f.domain, &live); err != nil {
		t.Fatalf("render deny page: %v", err)
	}
	if w.Code != http.StatusGone {
		t.Fatalf("expected the expired page, got %d", w.Code)
	}
}
