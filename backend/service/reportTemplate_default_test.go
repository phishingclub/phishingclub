package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/phishingclub/phishingclub/embedded"
)

func TestIsDefaultReportContent(t *testing.T) {
	if !isDefaultReportContent("\n"+embedded.DefaultReportHTML+"  ", false) {
		t.Fatal("the current default report must count as the default")
	}
	if !isDefaultReportContent(embedded.DefaultTrainingReportHTML, true) {
		t.Fatal("the current default training report must count as the default")
	}
	if isDefaultReportContent(embedded.DefaultReportHTML, true) {
		t.Fatal("the simulation default is not the training default")
	}
	if isDefaultReportContent(embedded.DefaultReportHTML+"<p>edited</p>", false) {
		t.Fatal("an edited template must not count as the default")
	}
	if len(previousDefaultReportHashes[false]) == 0 || len(previousDefaultReportHashes[true]) == 0 {
		t.Fatal("earlier defaults must be listed for both kinds")
	}
}

// the hashes of the current built in defaults. When this test fails, a default
// report template changed: move its old hash into previousDefaultReportHashes,
// so installs still on it keep counting as the default, then update it here.
const (
	currentDefaultReportHash         = "015af55d53580a230f71684fc3962f3c0d45d2e66fec920595c61e114eef9231"
	currentDefaultTrainingReportHash = "98a90ff4c0780b680d48cdab2d10fcf753db6930857f5851d9466090e68a591d"
)

func TestDefaultReportHashesAreKept(t *testing.T) {
	hash := func(s string) string {
		sum := sha256.Sum256([]byte(strings.TrimSpace(s)))
		return hex.EncodeToString(sum[:])
	}
	if got := hash(embedded.DefaultReportHTML); got != currentDefaultReportHash {
		t.Errorf("default report changed (now %s): add %s to previousDefaultReportHashes and update currentDefaultReportHash", got, currentDefaultReportHash)
	}
	if got := hash(embedded.DefaultTrainingReportHTML); got != currentDefaultTrainingReportHash {
		t.Errorf("default training report changed (now %s): add %s to previousDefaultReportHashes and update currentDefaultTrainingReportHash", got, currentDefaultTrainingReportHash)
	}
	// an earlier default of one kind is not the default of the other
	for h := range previousDefaultReportHashes[false] {
		if previousDefaultReportHashes[true][h] {
			t.Errorf("hash %s is listed for both kinds", h)
		}
	}
}
