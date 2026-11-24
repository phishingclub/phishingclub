//go:build !dev

package service

// simulatedUpdateVersion is unused outside dev builds
const simulatedUpdateVersion = ""

// simulatedUpdate is never active outside dev builds
func simulatedUpdate() (inApp bool, ok bool) {
	return false, false
}
