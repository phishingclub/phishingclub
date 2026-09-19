package script

import (
	"fmt"
	"sync"
	"time"
)

// TestEntry is one line in the run log. It mirrors the remote browser editor's
// runLog entry shape so the editor renders both the same way.
//   - type "log":   Message (+ optional Data)
//   - type "info":  Message
//   - type "event": Key (event name) + Value (event data) — an emitEvent call
//   - type "error": Message
//   - type "done":  end marker
type TestEntry struct {
	Type    string      `json:"type"`
	Time    string      `json:"time"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Key     string      `json:"key,omitempty"`
	Value   interface{} `json:"value,omitempty"`
}

// TestResult is the captured output of a test run: an ordered run log plus the
// terminal error, if any.
type TestResult struct {
	OK         bool        `json:"ok"`
	DurationMs int64       `json:"durationMs"`
	Entries    []TestEntry `json:"entries"`
	Error      string      `json:"error,omitempty"`
	ErrorPhase string      `json:"errorPhase,omitempty"`

	mu sync.Mutex
}

// maxTestEntries caps the captured run log so a script that logs in a tight loop
// cannot grow the response unboundedly and exhaust memory in the request handler.
const maxTestEntries = 2000

func (t *TestResult) add(e TestEntry) {
	e.Time = time.Now().UTC().Format(time.RFC3339Nano)
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.Entries) >= maxTestEntries {
		if len(t.Entries) == maxTestEntries {
			t.Entries = append(t.Entries, TestEntry{
				Type:    "log",
				Time:    e.Time,
				Message: "… output truncated (too many entries)",
			})
		}
		return
	}
	t.Entries = append(t.Entries, e)
}

func (t *TestResult) addLog(msg string, data interface{}) {
	t.add(TestEntry{Type: "log", Message: msg, Data: data})
}

func (t *TestResult) addInfo(msg string, data interface{}) {
	t.add(TestEntry{Type: "info", Message: msg, Data: data})
}

func (t *TestResult) addEvent(name string, data map[string]interface{}) {
	t.add(TestEntry{Type: "event", Key: name, Value: data})
}

func (t *TestResult) addFetchOK(method, url string, status int) {
	t.add(TestEntry{Type: "log", Message: fmt.Sprintf("%s %s → %d", method, url, status)})
}

func (t *TestResult) addFetchErr(method, url, errMsg string) {
	t.add(TestEntry{Type: "log", Message: fmt.Sprintf("%s %s ✗ %s", method, url, errMsg)})
}

func (t *TestResult) setError(phase, msg string) {
	t.mu.Lock()
	if t.Error == "" {
		t.Error = msg
		t.ErrorPhase = phase
	}
	t.mu.Unlock()
	t.add(TestEntry{Type: "error", Message: msg})
}

// RunTest runs a script against a simulated event and captures what it does as
// an ordered run log, without touching the campaign: log, info and emitEvent are
// recorded rather than applied, and no webhooks fire. http.fetch runs for real.
// The Runner's Timeout bounds the run.
func (r *Runner) RunTest(script string, event EventContext) *TestResult {
	res := &TestResult{Entries: []TestEntry{}}
	start := time.Now()
	r.run(Job{ScriptID: "test", Script: script, Event: event, test: res})
	res.DurationMs = time.Since(start).Milliseconds()
	res.OK = res.Error == ""
	if res.OK {
		res.add(TestEntry{Type: "done"})
	}
	return res
}
