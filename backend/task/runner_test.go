package task

import (
	"testing"

	"go.uber.org/zap"
)

// TestRunTaskRecoversPanic checks a task that panics is logged and the tasks
// after it still run
func TestRunTaskRecoversPanic(t *testing.T) {
	d := &Runner{Logger: zap.NewNop().Sugar()}
	ran := []string{}
	d.runTask("first", func() error {
		ran = append(ran, "first")
		return nil
	})
	d.runTask("panics", func() error {
		var m map[string]int
		m["boom"]++
		return nil
	})
	d.runTask("last", func() error {
		ran = append(ran, "last")
		return nil
	})
	if len(ran) != 2 || ran[1] != "last" {
		t.Fatalf("the tasks after a panic must run, ran %v", ran)
	}
}
