//go:build dev

package service

import (
	"os"
	"strings"

	"github.com/phishingclub/phishingclub/build"
)

// simulatedUpdateVersion is the version shown while an update is simulated
const simulatedUpdateVersion = "99.0.0"

// simulatedUpdateFile turns on a simulated update while it exists. It is read on
// every call so it can be changed without a restart.
// "inapp" shows the in app Update button, "manual" shows the Download button.
// Any other content turns the simulation off.
const simulatedUpdateFile = ".dev/simulate-update"

// simulatedUpdate pretends a newer release exists.
// This file is only built with the dev tag, so release binaries never contain it.
func simulatedUpdate() (inApp bool, ok bool) {
	if build.Flags.Production {
		return false, false
	}
	b, err := os.ReadFile(simulatedUpdateFile)
	if err != nil {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(string(b))) {
	case "inapp":
		return true, true
	case "manual":
		return false, true
	}
	return false, false
}
