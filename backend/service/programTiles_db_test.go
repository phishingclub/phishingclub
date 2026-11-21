package service

import (
	"testing"
	"time"
)

// TestProgramResultsTilesByRange checks the program page results count the
// tiles of the chosen range, keep a cached result per range, keep the month
// and template tables since the start, and pick the default range
func TestProgramResultsTilesByRange(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	session := authorizedSession(t)
	now := time.Now()
	f.sentTo(now.AddDate(0, 0, -5))
	f.sentTo(now.AddDate(0, 0, -50))
	f.sentTo(now.AddDate(0, 0, -200))
	want := map[string]int{"30d": 1, "90d": 2, "12m": 3, "all": 3, "": 1}
	for _, key := range []string{"30d", "90d", "12m", "all", "", "30d"} {
		r, err := f.svc.GetResults(f.ctx, session, &f.programID, key)
		if err != nil {
			t.Fatalf("%q: %v", key, err)
		}
		if r.Tiles == nil || r.Tiles.Sent == nil || *r.Tiles.Sent != want[key] {
			t.Fatalf("%q: tiles %+v", key, r.Tiles)
		}
		if key == "" && r.Tiles.Range != ProgramTimeRange30Days {
			t.Fatalf("a named program defaults to 30 days, got %q", r.Tiles.Range)
		}
		if *r.Summary.Sent != 3 || len(r.Templates) != 1 || *r.Templates[0].Sent != 3 {
			t.Fatalf("%q: the tables and summary count since the start: %+v %+v", key, r.Summary, r.Templates)
		}
	}
	// the results without tiles, as the report and the export read them,
	// never carry tiles
	if r := f.results(now); r.Tiles != nil {
		t.Fatalf("results without tiles: %+v", r.Tiles)
	}
	f.setProgram(f.programID, map[string]any{"is_anonymous": true})
	f.svc.invalidateResults(&f.programID)
	r, err := f.svc.GetResults(f.ctx, session, &f.programID, "")
	if err != nil {
		t.Fatalf("anonymous: %v", err)
	}
	if r.Tiles.Range != ProgramTimeRange90Days {
		t.Fatalf("an anonymous program defaults to 90 days, got %q", r.Tiles.Range)
	}
}
