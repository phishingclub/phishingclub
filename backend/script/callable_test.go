package script

import (
	"context"
	"fmt"
	"testing"
)

// TestRunCallable proves callable mode: input flows in, the returned object
// flows out.
func TestRunCallable(t *testing.T) {
	r := newTestRunner()
	out, err := r.RunCallable(
		context.Background(),
		`return { doubled: input.n * 2, who: input.who };`,
		map[string]interface{}{"n": 21, "who": "alice"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fmt.Sprint(out["doubled"]) != "42" {
		t.Fatalf("doubled = %v, want 42", out["doubled"])
	}
	if out["who"] != "alice" {
		t.Fatalf("who = %v, want alice", out["who"])
	}
}

// TestRunCallableStop proves stop() is a clean exit with no output.
func TestRunCallableStop(t *testing.T) {
	r := newTestRunner()
	out, err := r.RunCallable(context.Background(), `stop();`, nil)
	if err != nil || out != nil {
		t.Fatalf("stop() should be a clean nil exit, got out=%v err=%v", out, err)
	}
}

// TestRunCallableNonObject proves a non-object return is an error.
func TestRunCallableNonObject(t *testing.T) {
	r := newTestRunner()
	if _, err := r.RunCallable(context.Background(), `return 5;`, nil); err == nil {
		t.Fatalf("expected an error for a non-object return")
	}
}

// TestRunCallableEmitEventUnavailable proves campaign bindings are inert in
// callable mode (emitEvent throws, surfacing as an error).
func TestRunCallableEmitEventUnavailable(t *testing.T) {
	r := newTestRunner()
	if _, err := r.RunCallable(context.Background(),
		`emitEvent('campaign_recipient_submitted_data', {});`, nil); err == nil {
		t.Fatalf("expected emitEvent to be unavailable in callable mode")
	}
}
