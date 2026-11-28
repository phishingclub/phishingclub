package model

import (
	"strings"
	"testing"
)

// TestNormalizeLanguageRoutesRefusesLongRawInput checks a key or code far over
// the limit is refused before it is normalized, and a key at the limit with
// spaces around it is still taken
func TestNormalizeLanguageRoutesRefusesLongRawInput(t *testing.T) {
	if _, err := NormalizeLanguageRoutes(map[string]string{strings.Repeat("a", 1<<20): "de"}); err == nil {
		t.Fatal("a very long key was taken")
	}
	if _, err := NormalizeLanguageRoutes(map[string]string{"Germany": strings.Repeat("d", 1<<20)}); err == nil {
		t.Fatal("a very long code was taken")
	}
	key := "  " + strings.Repeat("ä", MaxLanguageRouteKeyLength) + "  "
	got, err := NormalizeLanguageRoutes(map[string]string{key: "de"})
	if err != nil || len(got) != 1 {
		t.Fatalf("a key at the limit was refused: %v", err)
	}
	if _, err := NormalizeLanguageRoutes(map[string]string{strings.Repeat("a", MaxLanguageRouteKeyLength+1): "de"}); err == nil {
		t.Fatal("a key one over the limit was taken")
	}
}

// TestAnonymousSelfManagedLanguageCount checks the routed language count
func TestAnonymousSelfManagedLanguageCount(t *testing.T) {
	if n := RoutedLanguageCount("en", map[string]string{"a": "en", "b": "en"}); n != 1 {
		t.Fatalf("want 1, got %d", n)
	}
	if n := RoutedLanguageCount("en", map[string]string{"a": "de"}); n != 2 {
		t.Fatalf("want 2, got %d", n)
	}
	if err := AnonymousSelfManagedLanguagesError("", "en", map[string]string{"a": "de"}); err != nil {
		t.Fatalf("routing off refused: %v", err)
	}
}
