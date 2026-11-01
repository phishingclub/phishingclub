package service

import (
	"testing"

	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// res builds an installed resource provenance row with a recorded baseline hash,
// the only field reconcileConflicts reads.
func res(baseline string) *database.InstalledResource {
	return &database.InstalledResource{InstalledContentHash: baseline}
}

// TestReconcileConflicts walks every combination of the three way inputs and
// asserts a conflict is raised ONLY when the upstream content changed AND the
// local entity drifted from the recorded baseline. An item without content, an
// added (unmatched) item and a dropped resource must never be flagged.
func TestReconcileConflicts(t *testing.T) {
	const (
		baseline = "base"
		upNew    = "upstream-new"
		curEdit  = "local-edited"
	)
	cases := []struct {
		name string
		// the single package item under test
		match      string
		hasContent bool
		upstreamFP string
		// the installed resource, nil when the item is unmatched (added)
		matched   bool
		currentFP string
		// whether this item (match) should be present in currents under a different
		// key, to model a dropped resource (a current with no item of its own)
		droppedOnly  bool
		wantConflict bool
	}{
		{
			name:  "upstream unchanged, local unchanged -> no conflict",
			match: "A", hasContent: true, upstreamFP: baseline,
			matched: true, currentFP: baseline, wantConflict: false,
		},
		{
			name:  "upstream changed only -> no conflict (applied silently)",
			match: "A", hasContent: true, upstreamFP: upNew,
			matched: true, currentFP: baseline, wantConflict: false,
		},
		{
			name:  "local edited only -> no conflict (kept)",
			match: "A", hasContent: true, upstreamFP: baseline,
			matched: true, currentFP: curEdit, wantConflict: false,
		},
		{
			name:  "both changed -> conflict",
			match: "A", hasContent: true, upstreamFP: upNew,
			matched: true, currentFP: curEdit, wantConflict: true,
		},
		{
			name:  "both changed but no content -> no conflict (skipped)",
			match: "A", hasContent: false, upstreamFP: upNew,
			matched: true, currentFP: curEdit, wantConflict: false,
		},
		{
			name:  "both changed but unmatched (added) -> no conflict",
			match: "A", hasContent: true, upstreamFP: upNew,
			matched: false, currentFP: curEdit, wantConflict: false,
		},
		{
			name:  "dropped resource (current with no item) -> no conflict",
			match: "A", hasContent: true, upstreamFP: upNew,
			droppedOnly: true, currentFP: curEdit, wantConflict: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := []reconcileItem{{
				match:      tc.match,
				hasContent: tc.hasContent,
				upstreamFP: tc.upstreamFP,
			}}
			currents := map[string]reconcileCurrent{}
			if tc.droppedOnly {
				// a resource the new version no longer declares: it lives in currents
				// under its own key but no item names it, so reconcileConflicts (which
				// iterates items) must never consider it.
				currents["dropped-key"] = reconcileCurrent{
					res: res(baseline), currentFP: tc.currentFP, currentName: "Dropped",
				}
			} else if tc.matched {
				currents[tc.match] = reconcileCurrent{
					res: res(baseline), currentFP: tc.currentFP, currentName: tc.match,
				}
			}
			got := reconcileConflicts("page", items, currents)
			if tc.wantConflict && len(got) != 1 {
				t.Fatalf("expected exactly 1 conflict, got %d (%+v)", len(got), got)
			}
			if !tc.wantConflict && len(got) != 0 {
				t.Fatalf("expected no conflict, got %d (%+v)", len(got), got)
			}
			if tc.wantConflict {
				if got[0].ResourceType != "page" || got[0].SourceName != tc.match {
					t.Fatalf("conflict carries wrong identity: %+v", got[0])
				}
			}
		})
	}
}

// TestReconcileConflictsMultiple proves the conflict pass collects a conflict per
// resource both edited and changed across a set, leaving untouched ones out.
func TestReconcileConflictsMultiple(t *testing.T) {
	const baseline = "base"
	items := []reconcileItem{
		{match: "clean", hasContent: true, upstreamFP: baseline},        // neither side changed
		{match: "upstreamOnly", hasContent: true, upstreamFP: "up-new"}, // upstream only
		{match: "localOnly", hasContent: true, upstreamFP: baseline},    // local only
		{match: "conflict1", hasContent: true, upstreamFP: "up-new"},    // both
		{match: "conflict2", hasContent: true, upstreamFP: "up-new"},    // both
	}
	currents := map[string]reconcileCurrent{
		"clean":        {res: res(baseline), currentFP: baseline},
		"upstreamOnly": {res: res(baseline), currentFP: baseline},
		"localOnly":    {res: res(baseline), currentFP: "edited"},
		"conflict1":    {res: res(baseline), currentFP: "edited"},
		"conflict2":    {res: res(baseline), currentFP: "edited"},
	}
	got := reconcileConflicts("page", items, currents)
	if len(got) != 2 {
		t.Fatalf("expected 2 conflicts, got %d (%+v)", len(got), got)
	}
	seen := map[string]bool{}
	for _, c := range got {
		seen[c.SourceName] = true
	}
	if !seen["conflict1"] || !seen["conflict2"] {
		t.Fatalf("expected conflict1 and conflict2, got %+v", got)
	}
}

// TestResolutionForDefault checks the defaulting: an unspecified or unknown
// resolution falls back to keep so an unresolved conflict never silently
// destroys a local edit, and a specified valid value is returned as is.
func TestResolutionForDefault(t *testing.T) {
	cases := []struct {
		name        string
		resolutions map[string]string
		want        string
	}{
		{"nil map defaults to keep", nil, resolveKeep},
		{"missing key defaults to keep", map[string]string{"email:Other": resolveOverwrite}, resolveKeep},
		{"unknown value defaults to keep", map[string]string{"page:Login": "garbage"}, resolveKeep},
		{"explicit keep", map[string]string{"page:Login": resolveKeep}, resolveKeep},
		{"explicit overwrite", map[string]string{"page:Login": resolveOverwrite}, resolveOverwrite},
		{"explicit both", map[string]string{"page:Login": resolveBoth}, resolveBoth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolutionFor(tc.resolutions, "page", "Login"); got != tc.want {
				t.Fatalf("resolutionFor = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestUnresolvedConflicts checks that only conflicts without a resolution key
// remain, so the conflict pass stops the update with nothing changed when any are
// left unanswered.
func TestUnresolvedConflicts(t *testing.T) {
	conflicts := []UpdateConflict{
		{ResourceType: "page", SourceName: "Login"},
		{ResourceType: "email", SourceName: "Welcome"},
	}
	// nothing resolved -> both unresolved
	if got := unresolvedConflicts(conflicts, nil); len(got) != 2 {
		t.Fatalf("expected 2 unresolved, got %d", len(got))
	}
	// one resolved -> one unresolved
	got := unresolvedConflicts(conflicts, map[string]string{"page:Login": resolveKeep})
	if len(got) != 1 || got[0].SourceName != "Welcome" {
		t.Fatalf("expected only email Welcome unresolved, got %+v", got)
	}
	// all resolved -> none
	all := map[string]string{"page:Login": resolveKeep, "email:Welcome": resolveOverwrite}
	if got := unresolvedConflicts(conflicts, all); len(got) != 0 {
		t.Fatalf("expected 0 unresolved, got %d", len(got))
	}
}

// TestHashParts documents the fingerprint field separation: the parts are joined
// with a NUL so a split cannot be forged by sliding a boundary (a+b must not
// collide with ab), and the hash is deterministic across calls.
func TestHashParts(t *testing.T) {
	// deterministic and stable across calls
	if hashParts("a", "b") != hashParts("a", "b") {
		t.Fatal("hashParts is not deterministic")
	}
	if contentHash("x") != contentHash("x") {
		t.Fatal("contentHash is not deterministic")
	}
	// boundary cannot be slid: ("a","b") != ("ab") and != ("a","b","c") variants
	if hashParts("a", "b") == hashParts("ab") {
		t.Fatal("a+b collides with ab (missing separator)")
	}
	if hashParts("a", "b") == contentHash("ab") {
		t.Fatal("a+b collides with plain ab hash")
	}
	if hashParts("ab", "c") == hashParts("a", "bc") {
		t.Fatal("boundary slid between fields collides")
	}
	// an added empty trailing field is distinguishable from the field being absent
	if hashParts("a", "b") == hashParts("a", "b", "") {
		t.Fatal("trailing empty field collides with its absence")
	}
	// different content differs
	if hashParts("a", "b") == hashParts("a", "c") {
		t.Fatal("different fields hash the same")
	}
}

// TestPageFingerprintSeparation proves the entity level fingerprint keeps name
// and content on opposite sides of the NUL join, so content bleeding into the
// name (or back) is not a false match and reads as drift.
func TestPageFingerprintSeparation(t *testing.T) {
	mk := func(name, content string) *model.Page {
		p := &model.Page{}
		n, err := vo.NewString64(name)
		if err != nil {
			t.Fatalf("name %q: %v", name, err)
		}
		p.Name = nn(*n)
		c, err := vo.NewOptionalString1MB(content)
		if err != nil {
			t.Fatalf("content %q: %v", content, err)
		}
		p.Content = nn(*c)
		return p
	}
	// stable across calls
	if pageFingerprint(mk("Login", "body")) != pageFingerprint(mk("Login", "body")) {
		t.Fatal("pageFingerprint is not deterministic")
	}
	// name="a" content="b" must not collide with name="ab" content=""
	if pageFingerprint(mk("a", "b")) == pageFingerprint(mk("ab", "")) {
		t.Fatal("name/content boundary can be slid")
	}
	// a content edit alone reads as drift
	if pageFingerprint(mk("Login", "v1")) == pageFingerprint(mk("Login", "v2")) {
		t.Fatal("content edit did not change the fingerprint")
	}
	// a rename alone reads as drift
	if pageFingerprint(mk("Login", "v1")) == pageFingerprint(mk("Signin", "v1")) {
		t.Fatal("rename did not change the fingerprint")
	}
}

func TestSingleTokenNames(t *testing.T) {
	for raw, want := range map[string]string{"@@PROV@@": "PROV", " @@_X1@@ ": "_X1"} {
		if got, ok := singleToken(raw); !ok || got != want {
			t.Errorf("%q: got %q %v, want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{
		"@@@@", "@@prov@@", "@@A@@B@@", "@@ns/pick\x1fPROV@@", "@@PROV\n@@", "x@@PROV@@", "@@PROV@@x",
	} {
		if got, ok := singleToken(raw); ok {
			t.Errorf("%q: got %q, want no token", raw, got)
		}
	}
}
