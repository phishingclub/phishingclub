package controller

import "testing"

// TestUniqueZipName checks every entry of the company export gets its own
// name: programs named "a/b", "a-b" and "a-b (2)" clean to names that would
// repeat, and a name that differs only in case would overwrite another file
// when unzipped on a case blind file system
func TestUniqueZipName(t *testing.T) {
	used := map[string]bool{}
	got := []string{}
	for _, name := range []string{"a/b", "a-b", "a-b (2)", "A-B", "other"} {
		got = append(got, uniqueZipName(used, "program_events/", sanitizeProgramFilename(name), ".csv"))
	}
	want := []string{
		"program_events/a-b.csv",
		"program_events/a-b (2).csv",
		"program_events/a-b (2) (2).csv",
		"program_events/A-B (3).csv",
		"program_events/other.csv",
	}
	seen := map[string]bool{}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("name %d = %q, want %q (all %v)", i, got[i], want[i], got)
		}
		if seen[got[i]] {
			t.Fatalf("name %q repeats", got[i])
		}
		seen[got[i]] = true
	}
	// the campaign events entries use their own folder and set
	campaigns := map[string]bool{}
	if a, b := uniqueZipName(campaigns, "campaign_events/", "x", ".csv"), uniqueZipName(campaigns, "campaign_events/", "x", ".csv"); a == b {
		t.Fatalf("campaign entries repeat: %q", a)
	}
}

// TestProgramFilenameFallback checks a program name left empty after the
// unsafe characters go becomes program, not campaign
func TestProgramFilenameFallback(t *testing.T) {
	if got := sanitizeProgramFilename("  "); got != "program" {
		t.Fatalf("blank program name gives %q", got)
	}
	if got := sanitizeFilename(" "); got != "campaign" {
		t.Fatalf("blank campaign name gives %q", got)
	}
	used := map[string]bool{}
	if got := uniqueZipName(used, "program_events/", sanitizeProgramFilename(""), ".csv"); got != "program_events/program.csv" {
		t.Fatalf("zip entry of a blank program name: %q", got)
	}
}
