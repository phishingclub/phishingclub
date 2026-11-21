package embedded

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"strings"
)

//go:embed tracking-pixel/sendgrid/open.gif
var TrackingPixel []byte

//go:embed remotebrowser_inject.js
var RemoteBrowserInjectJS string

//go:embed remotebrowser_prelude.js
var RemoteBrowserPreludeJS string

// SigningKey1 is verifing the signed .sig file when updating
//
//go:embed signingkeys/public1.bin
var SigningKey1 []byte

// SigningKey2 is a extra verification key if key 1 is lost
//
//go:embed signingkeys/public2.bin
var SigningKey2 []byte

//go:embed default_report.html
var DefaultReportHTML string

//go:embed default_training_report.html
var DefaultTrainingReportHTML string

//go:embed default_program_report.html
var DefaultProgramReportHTML string

// PreviousDefaultProgramReportHashes are the ReportContentHash values of the
// earlier built in program report templates. A global program template that
// still holds one of them was never edited.
var PreviousDefaultProgramReportHashes = map[string]bool{
	"972453f0365db803f19f0e3ececddc55882118eecdb4b5e489b5e2f1ead2c2b2": true,
	"412340b82724510037a49075e9f3509867b4171cd32b86e56d48cce57fe748c0": true,
	"7551b1da0ba3429dc10e14930d4cd8cc938fa952c8ca7b0598546f6dbbe4f2c7": true,
	"f546287733dbd26da8f3ef5952a7431840766f2c2f57682e7bb481e3791dba96": true,
}

// ReportContentHash is the SHA 256 hash of a report template, white space
// trimmed
func ReportContentHash(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(sum[:])
}
