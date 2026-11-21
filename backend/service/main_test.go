package service

import (
	"os"
	"testing"

	"github.com/phishingclub/phishingclub/repository"
)

// TestMain turns off the pause between chunked writes. It lets other writers
// take their turn in production, but the service tests have no other writers
// and would spend most of their time waiting.
func TestMain(m *testing.M) {
	repository.WriteChunkPause = 0
	os.Exit(m.Run())
}
