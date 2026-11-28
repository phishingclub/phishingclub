package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// languageStatsCampaign stores a campaign on two templates, one written in
// English with a German row and one written in Danish. People: 3 stamped en,
// 1 with no stamp on the English template (counts as English), 4 stamped de,
// 2 with no stamp on the Danish template. Visits: 1 English, 1 German;
// a submit by the English person with no stamp.
func languageStatsCampaign(f *poolFixture, anonymous bool) uuid.UUID {
	f.t.Helper()
	english := f.languageTemplate("en", "de")
	danish := f.languageTemplate("da")
	campaignID := f.campaign([]uuid.UUID{english, danish}, false, anonymous)
	stamped := func(templateID uuid.UUID, code string) uuid.UUID {
		id := f.seedCampaignRecipient(campaignID, &templateID, true, "")
		if code != "" {
			f.stampRecipient(campaignID, id, code)
		}
		return id
	}
	en1 := stamped(english, "en")
	stamped(english, "en")
	stamped(english, "en")
	legacy := stamped(english, "")
	de1 := stamped(english, "de")
	for i := 0; i < 3; i++ {
		stamped(english, "de")
	}
	stamped(danish, "")
	stamped(danish, "")
	f.event(campaignID, en1, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	f.event(campaignID, legacy, data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA)
	f.event(campaignID, de1, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	return campaignID
}

func statsByCode(stats []model.CampaignGroupStat) map[string]model.CampaignGroupStat {
	out := map[string]model.CampaignGroupStat{}
	for _, s := range stats {
		out[s.GroupID] = s
	}
	return out
}

// TestGroupedStatsByLanguageNamed checks a normal campaign gets exact counts
// per language, a row with no stamp counted in its template language, named
// by the language and largest first
func TestGroupedStatsByLanguageNamed(t *testing.T) {
	f := newLanguagePoolFixture(t)
	campaignID := languageStatsCampaign(f, false)
	stats, err := f.svc.GetGroupedResultStats(f.ctx, f.session, &campaignID, repository.GroupedStatByLanguage)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 3 {
		t.Fatalf("want 3 languages, got %+v", stats)
	}
	by := statsByCode(stats)
	en, de, da := by["en"], by["de"], by["da"]
	if en.Total != 4 || en.Clicked != 1 || en.Submitted != 1 || en.Group != "English" {
		t.Fatalf("english: %+v", en)
	}
	if de.Total != 4 || de.Clicked != 1 || de.Group != "German" {
		t.Fatalf("german: %+v", de)
	}
	if da.Total != 2 || da.Clicked != 0 || da.Group != "Danish" {
		t.Fatalf("danish: %+v", da)
	}
	if stats[0].GroupID != "da" || stats[1].GroupID != "en" || stats[2].GroupID != "de" {
		t.Fatalf("groups must be ordered by language name, got %+v", stats)
	}
	if many, err := f.svc.HasLanguageGroupData(f.ctx, f.session, &campaignID); err != nil || !many {
		t.Fatalf("a campaign in three languages has language group data: %v %v", many, err)
	}
	if _, err := f.svc.HasLanguageGroupData(f.ctx, unauthorizedSession(t), &campaignID); err == nil {
		t.Fatal("the language group data check needs the permission")
	}
}

// TestGroupedStatsByLanguageAnonymous checks an anonymous campaign gets
// sizes only with groups under the floor merged, the same in the report
func TestGroupedStatsByLanguageAnonymous(t *testing.T) {
	f := newLanguagePoolFixture(t)
	campaignID := languageStatsCampaign(f, true)
	stats, err := f.svc.GetGroupedResultStats(f.ctx, f.session, &campaignID, repository.GroupedStatByLanguage)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stats {
		if s.Total < anonymityGroupFloor && !s.Suppressed {
			t.Fatalf("a group under the floor must be merged: %+v", s)
		}
		if !s.Suppressed && (s.Clicked != -1 || s.Submitted != -1 || s.Reported != -1) {
			t.Fatalf("an anonymous campaign shows sizes only: %+v", s)
		}
		if s.GroupID == "da" {
			t.Fatal("the small Danish group must not be shown on its own")
		}
	}
	report := f.svc.reportLanguageGroups(f.ctx, &campaignID, true)
	if len(report) != len(stats) {
		t.Fatalf("the report must group the same way, got %+v", report)
	}
	// ordered by name with the merged groups last, like a named campaign
	for i := range stats {
		if report[i].Group != stats[i].Group {
			t.Fatalf("the report must order the same way, got %+v and %+v", report, stats)
		}
		if i > 0 && stats[i].Group != OTHER_GROUP_LABEL && stats[i-1].Group > stats[i].Group {
			t.Fatalf("anonymous groups must be ordered by name, got %+v", stats)
		}
	}
	for i, s := range stats {
		if s.Group == OTHER_GROUP_LABEL && i != len(stats)-1 {
			t.Fatalf("merged groups must be last, got %+v", stats)
		}
	}
	for _, s := range report {
		if !s.Suppressed && s.Clicked != -1 {
			t.Fatalf("the report shows sizes only for an anonymous campaign: %+v", s)
		}
	}
}

// TestReportLanguageGroups checks the report gets results by language only
// when the recipients are in more than one language
func TestReportLanguageGroups(t *testing.T) {
	f := newLanguagePoolFixture(t)
	campaignID := languageStatsCampaign(f, false)
	groups := toReportGroupStats(f.svc.reportLanguageGroups(f.ctx, &campaignID, false))
	if len(groups) != 3 || groups[0].Group != "Danish" || groups[1].Group != "English" || groups[2].Group != "German" {
		t.Fatalf("report language groups by name: %+v", groups)
	}

	single := f.languageTemplate("en")
	one := f.campaign([]uuid.UUID{single}, false, false)
	f.seedCampaignRecipient(one, &single, true, "")
	f.seedCampaignRecipient(one, &single, true, "")
	if got := f.svc.reportLanguageGroups(f.ctx, &one, false); len(got) != 0 {
		t.Fatalf("a campaign in one language gets no language groups, got %+v", got)
	}
}
