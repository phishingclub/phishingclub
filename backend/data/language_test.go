package data

import (
	"strings"
	"testing"
)

// TestNormalizeLanguage checks codes, tags and English names are read as the
// code, without case and spaces, and that anything else is refused
func TestNormalizeLanguage(t *testing.T) {
	cases := map[string]string{
		"da":                "da",
		" DA ":              "da",
		"da-DK":             "da",
		"da_dk":             "da",
		"DE-at":             "de",
		"zh-Hant-TW":        "zh",
		"Danish":            "da",
		"  danish  ":        "da",
		"GERMAN":            "de",
		"Norwegian":         "no",
		"nb-NO":             "no",
		"nn":                "no",
		"Norwegian Bokmål":  "no",
		"norwegian nynorsk": "no",
		"iw":                "he",
		"":                  "",
		"   ":               "",
	}
	for in, want := range cases {
		got, ok := NormalizeLanguage(in)
		if !ok || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"xx", "Klingon", "d", "dan", "-da", "da DK x", strings.Repeat("a", MaxLanguageInputLength+1)} {
		if got, ok := NormalizeLanguage(in); ok {
			t.Errorf("%q: got %q, want a refusal", in, got)
		}
	}
	// every code and every name of the list is read
	for _, l := range Languages {
		if got, ok := NormalizeLanguage(l.Code); !ok || got != l.Code {
			t.Errorf("code %s: got %q", l.Code, got)
		}
		if got, ok := NormalizeLanguage(strings.ToUpper(l.Name)); !ok || got != l.Code {
			t.Errorf("name %s: got %q", l.Name, got)
		}
	}
}

// TestNormalizeLanguageUnicodeForms checks a name with a separate accent mark
// (NFD) and values with zero width characters are read like the plain text
func TestNormalizeLanguageUnicodeForms(t *testing.T) {
	cases := map[string]string{
		"Bokma\u030al":           "no",
		"Norwegian Bokma\u030al": "no",
		"NORWEGIAN BOKMA\u030aL": "no",
		"bokm\u00e5l":            "no",
		"da\u200b":               "da",
		"\ufeffda-DK":            "da",
		"Dan\u200cish":           "da",
		"Ger\u200dman":           "de",
		"\u200b":                 "",
		"Bokma\u200b\u030al":     "no",
	}
	for in, want := range cases {
		got, ok := NormalizeLanguage(in)
		if !ok || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
}
