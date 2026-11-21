package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
)

// filterHolder is a campaign or program with its filter fields. open is a
// campaign or program that is not closed.
type filterHolder struct {
	deny, evasion *uuid.UUID
	lists         []*uuid.UUID
	open          bool
}

// TestFilterCleanupOnDelete checks a deleted page or list leaves campaigns and
// programs the same, valid way: a deny page goes with the lists and the
// evasion page that need it, an evasion page goes alone, and a list delete
// clears the deny page only when no list or evasion page needs it. A page is
// cleared from closed campaigns and programs only, one that is not closed
// keeps it, as the delete is refused while it uses the page.
func TestFilterCleanupOnDelete(t *testing.T) {
	db := newProgramTestDB(t)
	if err := db.AutoMigrate(&database.CampaignAllowDeny{}, &database.ProgramAllowDeny{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	campaigns := &Campaign{DB: db}
	programs := &Program{DB: db}

	page, evasion, keep := ptrUUID(), ptrUUID(), ptrUUID()
	listA, listB := ptrUUID(), ptrUUID()
	setups := map[string]filterHolder{
		// uses the deleted page as deny page with lists and an evasion page
		"denyUser": {deny: page, evasion: evasion, lists: []*uuid.UUID{listA}},
		// uses the deleted page as evasion page
		"evasionUser": {deny: keep, evasion: page},
		// only list A, so its deny page goes with it
		"onlyListA": {deny: keep, lists: []*uuid.UUID{listA}},
		// list A and B, the deny page stays for B
		"listsAB": {deny: keep, lists: []*uuid.UUID{listA, listB}},
		// list A and an evasion page, the deny page stays for the evasion page
		"listAEvasion": {deny: keep, evasion: evasion, lists: []*uuid.UUID{listA}},
		// not closed, keeps the page as deny page or as evasion page
		"openDenyUser":    {deny: page, open: true},
		"openEvasionUser": {deny: keep, evasion: page, open: true},
	}
	campaignIDs := map[string]*uuid.UUID{}
	programIDs := map[string]*uuid.UUID{}
	for name, h := range setups {
		cid, pid := ptrUUID(), ptrUUID()
		campaignIDs[name], programIDs[name] = cid, pid
		var closedAt *time.Time
		if !h.open {
			closedAt = ptrNow()
		}
		if err := db.Create(&database.Campaign{
			ID: cid, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: name,
			DenyPageID: h.deny, EvasionPageID: h.evasion, ClosedAt: closedAt,
		}).Error; err != nil {
			t.Fatalf("create campaign: %v", err)
		}
		if err := db.Create(&database.Program{
			ID: pid, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: name,
			DenyPageID: h.deny, EvasionPageID: h.evasion, ClosedAt: closedAt,
		}).Error; err != nil {
			t.Fatalf("create program: %v", err)
		}
		for _, l := range h.lists {
			if err := db.Create(&database.CampaignAllowDeny{CampaignID: cid, AllowDenyID: l}).Error; err != nil {
				t.Fatalf("link campaign list: %v", err)
			}
			if err := db.Create(&database.ProgramAllowDeny{ProgramID: pid, AllowDenyID: l}).Error; err != nil {
				t.Fatalf("link program list: %v", err)
			}
		}
	}

	if err := campaigns.RemovePageFromFilters(ctx, page); err != nil {
		t.Fatalf("campaign page cleanup: %v", err)
	}
	if err := programs.RemovePageFromProgramFilters(ctx, page); err != nil {
		t.Fatalf("program page cleanup: %v", err)
	}
	if err := campaigns.RemoveAllowDenyFromFilters(ctx, listA); err != nil {
		t.Fatalf("campaign list cleanup: %v", err)
	}
	if err := programs.RemoveAllowDenyFromProgramFilters(ctx, listA); err != nil {
		t.Fatalf("program list cleanup: %v", err)
	}

	want := map[string]filterHolder{
		"denyUser":        {},
		"evasionUser":     {deny: keep},
		"onlyListA":       {},
		"listsAB":         {deny: keep, lists: []*uuid.UUID{listB}},
		"listAEvasion":    {deny: keep, evasion: evasion},
		"openDenyUser":    {deny: page},
		"openEvasionUser": {deny: keep, evasion: page},
	}
	sameID := func(a, b *uuid.UUID) bool {
		if a == nil || b == nil {
			return a == nil && b == nil
		}
		return *a == *b
	}
	for name, w := range want {
		var c database.Campaign
		if err := db.Where("id = ?", campaignIDs[name]).First(&c).Error; err != nil {
			t.Fatalf("read campaign: %v", err)
		}
		var p database.Program
		if err := db.Where("id = ?", programIDs[name]).First(&p).Error; err != nil {
			t.Fatalf("read program: %v", err)
		}
		if !sameID(c.DenyPageID, w.deny) || !sameID(c.EvasionPageID, w.evasion) {
			t.Fatalf("campaign %s: deny %v evasion %v", name, c.DenyPageID, c.EvasionPageID)
		}
		if !sameID(p.DenyPageID, w.deny) || !sameID(p.EvasionPageID, w.evasion) {
			t.Fatalf("program %s: deny %v evasion %v", name, p.DenyPageID, p.EvasionPageID)
		}
		var campaignLists []database.CampaignAllowDeny
		db.Where("campaign_id = ?", campaignIDs[name]).Find(&campaignLists)
		var programLists []database.ProgramAllowDeny
		db.Where("program_id = ?", programIDs[name]).Find(&programLists)
		if len(campaignLists) != len(w.lists) || len(programLists) != len(w.lists) {
			t.Fatalf("%s: lists campaign %d program %d, want %d", name, len(campaignLists), len(programLists), len(w.lists))
		}
		for i := range w.lists {
			if *campaignLists[i].AllowDenyID != *w.lists[i] || *programLists[i].AllowDenyID != *w.lists[i] {
				t.Fatalf("%s: wrong list kept", name)
			}
		}
	}
}

// TestOpenNamesByFilterPage checks the campaigns and programs that are not
// closed and use a page as deny or evasion page are named, sorted and
// limited with the full count, while closed ones and the hidden campaigns of
// a program are not named
func TestOpenNamesByFilterPage(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	campaigns := &Campaign{DB: db}
	programs := &Program{DB: db}
	page, other := ptrUUID(), ptrUUID()
	programID := ptrUUID()
	add := func(name string, deny, evasion *uuid.UUID, closed bool, program *uuid.UUID) {
		var closedAt *time.Time
		if closed {
			closedAt = ptrNow()
		}
		if err := db.Create(&database.Campaign{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: name,
			DenyPageID: deny, EvasionPageID: evasion, ClosedAt: closedAt, ProgramID: program,
		}).Error; err != nil {
			t.Fatalf("create campaign: %v", err)
		}
		if program == nil {
			if err := db.Create(&database.Program{
				ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "p " + name,
				DenyPageID: deny, EvasionPageID: evasion, ClosedAt: closedAt,
			}).Error; err != nil {
				t.Fatalf("create program: %v", err)
			}
		}
	}
	add("c", page, nil, false, nil)
	add("a", other, page, false, nil)
	add("b", page, nil, false, nil)
	add("closed", page, nil, true, nil)
	add("unrelated", other, nil, false, nil)
	add("hidden", page, nil, false, programID)

	names, count, err := campaigns.GetOpenNamesByFilterPage(ctx, page, 2)
	if err != nil {
		t.Fatalf("campaign names: %v", err)
	}
	if count != 3 || len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("campaign names %v count %d", names, count)
	}
	names, count, err = programs.GetOpenNamesByFilterPage(ctx, page, 5)
	if err != nil {
		t.Fatalf("program names: %v", err)
	}
	if count != 3 || len(names) != 3 || names[0] != "p a" || names[2] != "p c" {
		t.Fatalf("program names %v count %d", names, count)
	}
	if names, count, err := campaigns.GetOpenNamesByFilterPage(ctx, ptrUUID(), 5); err != nil || count != 0 || len(names) != 0 {
		t.Fatalf("an unused page names nothing: %v %d %v", names, count, err)
	}
}
