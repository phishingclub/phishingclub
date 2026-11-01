package registry

import (
	"strings"
	"testing"
)

// TestManifestRejectsLoosePaths checks that entry paths with white space, hidden
// characters or case duplicates are refused instead of cleaned.
func TestManifestRejectsLoosePaths(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, p := range []string{
		"assets/logo.png ",
		"assets/ logo.png",
		"assets/gnp.\u202egol",
		"assets/lo\u200bgo.png",
		"assets/\xff.png",
		"assets\\logo.png",
	} {
		e := []Entry{{Path: p, SHA256: sha}}
		m := &Manifest{ID: "acme/x", Type: "asset", Version: "1.0.0", Entries: e, ContentHash: ComputeContentHash(e)}
		if err := m.Validate(); err == nil {
			t.Errorf("entry %q accepted", p)
		}
	}
	e := []Entry{{Path: "assets/Logo.png", SHA256: sha}, {Path: "assets/logo.png", SHA256: sha}}
	m := &Manifest{ID: "acme/x", Type: "asset", Version: "1.0.0", Entries: e, ContentHash: ComputeContentHash(e)}
	if err := m.Validate(); err == nil {
		t.Fatal("entries that differ only by case accepted")
	}
}

func TestUntarRejectsLoosePaths(t *testing.T) {
	loose := []string{"assets/logo.png ", "assets/lo\u200bgo.png", "assets//logo.png", "./assets/logo.png"}
	for _, name := range loose {
		a := buildTarGz(t, []tarFile{{name: name, body: []byte("x")}})
		if _, err := untar(a); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
	a := buildTarGz(t, []tarFile{
		{name: "assets/Logo.png", body: []byte("1")},
		{name: "assets/logo.png", body: []byte("2")},
	})
	if _, err := untar(a); err == nil {
		t.Fatal("entries that differ only by case accepted")
	}
}
