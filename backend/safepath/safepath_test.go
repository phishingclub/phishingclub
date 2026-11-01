package safepath

import "testing"

func TestCheck(t *testing.T) {
	good := []string{
		"logo.png",
		"img/logo.png",
		"a..b.png",
		"my logo.png",
		"dir/sub dir/file.css",
		"baitbox.json",
		"æøå/ünïcode.png",
	}
	for _, p := range good {
		if err := Check(p); err != nil {
			t.Errorf("Check(%q) = %v, want ok", p, err)
		}
	}
	bad := []string{
		"",
		"assets/logo.png ",
		" logo.png",
		"img /logo.png",
		"img/\tlogo.png",
		"img/logo.png\u00a0",
		"img/gnp.\u202egol",
		"img/lo\u200bgo.png",
		"img/\ufefflogo.png",
		"img/lo\x01go.png",
		"img/lo\x00go.png",
		"img/logo\x7f.png",
		"img/\xff.png",
		"..",
		"../logo.png",
		"img/../logo.png",
		"img/..",
		".",
		"./logo.png",
		"img/./logo.png",
		"img\\logo.png",
		"..\\logo.png",
		"/etc/passwd",
		"img//logo.png",
		"img/",
		"/",
	}
	for _, p := range bad {
		if err := Check(p); err == nil {
			t.Errorf("Check(%q) = nil, want an error", p)
		}
	}
}

func TestFoldKeyFindsCaseDuplicates(t *testing.T) {
	if FoldKey("img/Logo.PNG") != FoldKey("img/logo.png") {
		t.Fatalf("paths that differ only by case must share a key")
	}
	if FoldKey("img/logo.png") == FoldKey("img/logo2.png") {
		t.Fatalf("different paths must not share a key")
	}
}
