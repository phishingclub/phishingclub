package data

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// languagesJSPath is the frontend copy of the language list, seen from this
// package folder
var languagesJSPath = filepath.Join("..", "..", "frontend", "src", "lib", "utils", "languages.js")

var languageEntryJS = regexp.MustCompile(`\{\s*code:\s*'([^']*)',\s*name:\s*'([^']*)'\s*\}`)

// TestLanguageListMatchesFrontend checks the frontend language list holds the
// same codes and names in the same order. The backend container mounts only
// the backend, so run it with the frontend mounted next to it, for example
// docker compose run --rm --no-deps -v ./phishingclub/frontend:/frontend backend
func TestLanguageListMatchesFrontend(t *testing.T) {
	src, err := os.ReadFile(languagesJSPath)
	if os.IsNotExist(err) {
		t.Skipf("frontend not found at %s", languagesJSPath)
	}
	if err != nil {
		t.Fatal(err)
	}
	matches := languageEntryJS.FindAllStringSubmatch(string(src), -1)
	if len(matches) != len(Languages) {
		t.Fatalf("frontend lists %d languages, backend %d", len(matches), len(Languages))
	}
	for i, m := range matches {
		if m[1] != Languages[i].Code || m[2] != Languages[i].Name {
			t.Errorf("entry %d: frontend %s %s, backend %s %s", i, m[1], m[2], Languages[i].Code, Languages[i].Name)
		}
	}
}

// TestLanguageList checks the codes are unique lowercase two letter codes
func TestLanguageList(t *testing.T) {
	code := regexp.MustCompile(`^[a-z]{2}$`)
	seen := map[string]bool{}
	for _, l := range Languages {
		if !code.MatchString(l.Code) {
			t.Errorf("code %q is not two lowercase letters", l.Code)
		}
		if seen[l.Code] {
			t.Errorf("code %q is listed twice", l.Code)
		}
		seen[l.Code] = true
		if l.Name == "" {
			t.Errorf("code %q has no name", l.Code)
		}
	}
	if len(Languages) != 43 {
		t.Errorf("got %d languages, want 43", len(Languages))
	}
	if !IsValidLanguage("no") || IsValidLanguage("NO") || IsValidLanguage("pt-br") || IsValidLanguage("") {
		t.Error("IsValidLanguage must match list codes exactly")
	}
	if LanguageName("de") != "German" || LanguageName("xx") != "xx" {
		t.Error("LanguageName gives the English name or the code")
	}
}
