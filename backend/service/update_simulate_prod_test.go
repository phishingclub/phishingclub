//go:build !dev

package service

import (
	"os"
	"path/filepath"
	"testing"
)

// release builds must ignore the dev simulation file entirely
func TestSimulatedUpdateOffInReleaseBuild(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"inapp", "manual"} {
		if err := os.WriteFile(filepath.Join(dir, ".dev", "simulate-update"), []byte(mode), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		if _, ok := simulatedUpdate(); ok {
			t.Fatalf("simulation active in release build with %q", mode)
		}
		u := &Update{}
		if !u.UpdateInApp(true) || u.UpdateInApp(false) {
			t.Fatalf("UpdateInApp must pass the systemd value through with %q", mode)
		}
	}
}
