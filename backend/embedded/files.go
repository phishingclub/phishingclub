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

// PreviousDefaultReportHashes are the ReportContentHash values of the earlier
// built in simulation report templates. A global simulation template that
// still holds one of them was never edited.
var PreviousDefaultReportHashes = map[string]bool{
	"eb5108aac0dc0252aa50b61d0094149f5104981a8754d1e4408bf3948a7b7092": true,
	"c5a46d34913b0bc5274789e7643926393351fa1ae62c5166e86144cfe408fef6": true,
	"fe019c52bccdd0bf838caff6f28b25836df57aa0880a7d09d5c1046c8ea3df0f": true,
	"015af55d53580a230f71684fc3962f3c0d45d2e66fec920595c61e114eef9231": true,
	"c3e3e2e0d61e452d737e004ef049ebe43fca0ee23ad398fc4d10a4735ec2627d": true,
	"72a72d075305113f603f986ea5af45b3820dc0370c6b2092d83dddb626ee81e0": true,
}

// PreviousDefaultTrainingReportHashes are the ReportContentHash values of the
// earlier built in training report templates. A global training template that
// still holds one of them was never edited.
var PreviousDefaultTrainingReportHashes = map[string]bool{
	"6edcc3fc69e795baea771235538f657ec734ee3e526f1aaa72c07bca305f013b": true,
	"265f954f73b39be32287ca04d61cad79fb33672586b982032bc774a30334dbcf": true,
	"09a7feb1b0d390ed0cdb0272ad8745a3cc02aa0dbb1eb8db293dd1940bf7f26a": true,
	"98a90ff4c0780b680d48cdab2d10fcf753db6930857f5851d9466090e68a591d": true,
	"b417c60a35acceed520a194d897a951bb167a616b43d687a17d092375f3c36bb": true,
}

// ReportContentHash is the SHA 256 hash of a report template, white space
// trimmed
func ReportContentHash(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(sum[:])
}
