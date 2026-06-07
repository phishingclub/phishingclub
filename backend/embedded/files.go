package embedded

import (
	_ "embed"
)

//go:embed tracking-pixel/sendgrid/open.gif
var TrackingPixel []byte

//go:embed remotebrowser_inject.js
var RemoteBrowserInjectJS string

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
