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
	currentDefaultReportHash         = "c3e3e2e0d61e452d737e004ef049ebe43fca0ee23ad398fc4d10a4735ec2627d"
	currentDefaultTrainingReportHash = "b417c60a35acceed520a194d897a951bb167a616b43d687a17d092375f3c36bb"
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

// the hash of the current built in program report. When this test fails the
// default changed: add the old hash to embedded.PreviousDefaultProgramReportHashes,
// so installs still on it get the new default on start, then update it here.
const currentDefaultProgramReportHash = "a7eca243881ea95513708572eab639e3f83237dd378ee8fcbe7d713c38b98bc9"

func TestDefaultProgramReportHashIsKept(t *testing.T) {
	if got := embedded.ReportContentHash(embedded.DefaultProgramReportHTML); got != currentDefaultProgramReportHash {
		t.Errorf("default program report changed (now %s): add %s to embedded.PreviousDefaultProgramReportHashes and update currentDefaultProgramReportHash", got, currentDefaultProgramReportHash)
	}
	if embedded.PreviousDefaultProgramReportHashes[currentDefaultProgramReportHash] {
		t.Errorf("the current program default must not be listed as an earlier one")
	}
	// an install may still hold any earlier default, so none is ever dropped
	for _, h := range []string{
		"972453f0365db803f19f0e3ececddc55882118eecdb4b5e489b5e2f1ead2c2b2",
		"412340b82724510037a49075e9f3509867b4171cd32b86e56d48cce57fe748c0",
		"7551b1da0ba3429dc10e14930d4cd8cc938fa952c8ca7b0598546f6dbbe4f2c7",
		"f546287733dbd26da8f3ef5952a7431840766f2c2f57682e7bb481e3791dba96",
	} {
		if !embedded.PreviousDefaultProgramReportHashes[h] {
			t.Errorf("earlier program default %s must stay listed", h)
		}
	}
}
