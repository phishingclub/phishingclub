package script

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// newTestRunner builds a Runner with a no-op logger for tests.
func newTestRunner() *Runner {
	return &Runner{
		Logger:     zap.NewNop().Sugar(),
		HTTPClient: &http.Client{Timeout: maxFetchTimeout},
		Timeout:    5 * time.Second,
	}
}

// TestScriptFetchEncodeEmit proves the engine can read the event payload, call
// out over http.fetch, use encode/decode, and write back through emitEvent.
func TestScriptFetchEncodeEmit(t *testing.T) {
	// a server that echoes the request body back
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true,"token":"abc123"}`))
	}))
	defer srv.Close()

	var mu sync.Mutex
	emitted := map[string]map[string]interface{}{}
	emit := func(name string, data map[string]interface{}) error {
		mu.Lock()
		defer mu.Unlock()
		emitted[name] = data
		return nil
	}

	script := `
		log('starting', { event: event.name });
		// send the campaign name to the echo server, base64 encoded
		var res = http.fetch('` + srv.URL + `', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: encode.json({ name: event.campaignName, b: encode.base64('hi') })
		});
		if (res.status !== 200) { throw new Error('bad status ' + res.status); }
		var parsed = decode.json(res.body);
		// write the returned token back as a submit event
		emitEvent('campaign_recipient_submitted_data', { token: parsed.token });
	`

	r := newTestRunner()
	r.run(Job{
		ScriptID: "test-1",
		Script:   script,
		Event: EventContext{
			CampaignID:   "c1",
			Event:        "campaign_recipient_page_visited",
			CampaignName: "Q3 Phish",
		},
		Emit: emit,
	})

	mu.Lock()
	defer mu.Unlock()

	// the server must have received the base64 of "hi" = "aGk="
	if gotBody == "" {
		t.Fatalf("server received no body")
	}
	if want := "aGk="; !contains(gotBody, want) {
		t.Fatalf("request body %q did not contain %q", gotBody, want)
	}
	// emitEvent must have fired with the decoded token
	ev, ok := emitted["campaign_recipient_submitted_data"]
	if !ok {
		t.Fatalf("emitEvent was not called; emitted=%v", emitted)
	}
	if ev["token"] != "abc123" {
		t.Fatalf("emit token = %v, want abc123", ev["token"])
	}
}

// TestCodec exercises the encode/decode/hash/hmac/jwt/random toolkit.
func TestCodec(t *testing.T) {
	var mu sync.Mutex
	var got map[string]interface{}
	emit := func(name string, data map[string]interface{}) error {
		mu.Lock()
		defer mu.Unlock()
		got = data
		return nil
	}

	script := `
		var r = {};
		r.b64url = encode.base64url('hi');
		r.b64urlRound = decode.base64url(encode.base64url('héllo'));
		r.sha256abc = hash.sha256('abc');
		r.hmacLen = hmac.sha256('key', 'msg').length;
		r.hmacDet = hmac.sha256('key', 'msg') === hmac.sha256('key', 'msg');
		r.hmacB64Len = hmac.sha256('key', 'msg', 'base64').length > 0;
		r.gzipRound = decode.gzip(encode.gzip('the quick brown fox'));
		r.deflateRound = decode.deflate(encode.deflate('deflate me'));
		r.form = encode.form({ a: '1', b: 'two' });
		var f = decode.form('a=1&b=two&b=three');
		r.formA = f.a;
		r.formBArr = Array.isArray(f.b) ? f.b.length : 0;
		var tok = encode.base64url('{"alg":"HS256"}') + '.' + encode.base64url('{"sub":"123"}') + '.xxx';
		var jd = jwt.decode(tok);
		r.jwtSub = jd.payload.sub;
		r.uuidLen = random.uuid().length;
		r.randHexLen = random.bytes(16, 'hex').length;
		r.b32 = encode.base32('foo');
		r.htmlRound = decode.html(encode.html('<b>&"x"</b>'));
		emitEvent('campaign_recipient_submitted_data', r);
	`

	r := newTestRunner()
	r.run(Job{ScriptID: "codec", Script: script, Emit: emit})

	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatalf("codec script emitted nothing")
	}
	want := map[string]string{
		"b64url":       "aGk",
		"b64urlRound":  "héllo",
		"sha256abc":    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		"hmacLen":      "64",
		"hmacDet":      "true",
		"hmacB64Len":   "true",
		"gzipRound":    "the quick brown fox",
		"deflateRound": "deflate me",
		"form":         "a=1&b=two",
		"formA":        "1",
		"formBArr":     "2",
		"jwtSub":       "123",
		"uuidLen":      "36",
		"randHexLen":   "32",
		"b32":          "MZXW6===",
		"htmlRound":    `<b>&"x"</b>`,
	}
	for k, w := range want {
		if g := fmt.Sprint(got[k]); g != w {
			t.Errorf("codec %s = %q, want %q", k, g, w)
		}
	}
}

// TestUncaughtErrorRecordsEvent proves an uncaught exception is captured as an
// info event (level=error) through the Emit funnel, not just the server logs.
func TestUncaughtErrorRecordsEvent(t *testing.T) {
	var mu sync.Mutex
	var name string
	var payload map[string]interface{}
	emit := func(n string, d map[string]interface{}) error {
		mu.Lock()
		defer mu.Unlock()
		name = n
		payload = d
		return nil
	}
	r := newTestRunner()
	r.run(Job{ScriptID: "boom", Script: `throw new Error('kaboom');`, Emit: emit})

	mu.Lock()
	defer mu.Unlock()
	if name != "campaign_recipient_info" {
		t.Fatalf("error event name = %q, want campaign_recipient_info", name)
	}
	if payload["level"] != "error" || payload["phase"] != "exception" {
		t.Fatalf("error payload = %v", payload)
	}
	if s, _ := payload["error"].(string); s == "" || !contains(s, "kaboom") {
		t.Fatalf("error detail did not include the message: %v", payload["error"])
	}
}

// TestInfoBinding proves info() records a visible info event distinct from log().
func TestInfoBinding(t *testing.T) {
	var mu sync.Mutex
	var name string
	var payload map[string]interface{}
	emit := func(n string, d map[string]interface{}) error {
		mu.Lock()
		defer mu.Unlock()
		name = n
		payload = d
		return nil
	}
	r := newTestRunner()
	r.run(Job{ScriptID: "info", Script: `info('hello', { step: 3 });`, Emit: emit})

	mu.Lock()
	defer mu.Unlock()
	if name != "campaign_recipient_info" {
		t.Fatalf("info event name = %q", name)
	}
	if payload["message"] != "hello" || payload["level"] != "info" {
		t.Fatalf("info payload = %v", payload)
	}
	if fmt.Sprint(payload["step"]) != "3" {
		t.Fatalf("info extra field lost: %v", payload["step"])
	}
}

// TestRunTestCapture proves RunTest records log/info/emitEvent instead of
// applying them, and captures an uncaught error.
func TestRunTestCapture(t *testing.T) {
	r := newTestRunner()
	res := r.RunTest(`
		log('a log', { x: 1 });
		info('an info');
		emitEvent('campaign_recipient_submitted_data', { token: hmac.sha256('k', 'm') });
	`, EventContext{Event: "campaign_recipient_submitted_data", CampaignName: "Demo"})

	if !res.OK {
		t.Fatalf("expected ok, got error %q", res.Error)
	}
	// ordered run log: log, info, event(emitEvent), done
	types := []string{}
	for _, e := range res.Entries {
		types = append(types, e.Type)
	}
	if len(res.Entries) != 4 ||
		res.Entries[0].Type != "log" || res.Entries[0].Message != "a log" ||
		res.Entries[1].Type != "info" ||
		res.Entries[2].Type != "event" || res.Entries[2].Key != "campaign_recipient_submitted_data" ||
		res.Entries[3].Type != "done" {
		t.Fatalf("entries = %+v (types %v)", res.Entries, types)
	}
	if v, _ := res.Entries[2].Value.(map[string]interface{}); v["token"] == "" || v["token"] == nil {
		t.Fatalf("emitEvent data not captured: %+v", res.Entries[2].Value)
	}

	// an uncaught throw is captured, not panicked
	bad := r.RunTest(`throw new Error('nope');`, EventContext{})
	if bad.OK || bad.ErrorPhase != "exception" || bad.Error == "" {
		t.Fatalf("expected captured exception, got %+v", bad)
	}
}

// TestEmitPanicDoesNotCrash proves that a panic in the Emit callback (e.g. a DB
// failure) during the error-reporting path is contained, not propagated. A
// script throws (triggering reportError), and Emit panics; the run must return.
func TestEmitPanicDoesNotCrash(t *testing.T) {
	panicEmit := func(string, map[string]interface{}) error {
		panic("emit blew up")
	}
	r := newTestRunner()
	done := make(chan struct{})
	go func() {
		// throw -> reportError -> Emit panics; must not escape run()
		r.run(Job{ScriptID: "emit-panic", Script: `throw new Error('x');`, Emit: panicEmit})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("run() did not return after a panicking Emit")
	}
}

// TestWorkerSurvivesPanickingJob proves the dispatcher keeps working after a job
// whose Emit panics: a later job still runs.
func TestWorkerSurvivesPanickingJob(t *testing.T) {
	d := NewDispatcher(zap.NewNop().Sugar(), 1, 8, time.Second)
	d.Start()
	defer d.Stop()

	panicEmit := func(string, map[string]interface{}) error { panic("boom") }
	d.Enqueue(Job{ScriptID: "bad", Script: `throw new Error('x');`, Emit: panicEmit})

	ran := make(chan struct{}, 1)
	okEmit := func(string, map[string]interface{}) error { ran <- struct{}{}; return nil }
	d.Enqueue(Job{ScriptID: "good", Script: `emitEvent('x', {});`, Emit: okEmit})

	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatalf("worker died after a panicking job; later job never ran")
	}
}

// TestDeepRecursionBounded proves runaway recursion throws instead of crashing.
func TestDeepRecursionBounded(t *testing.T) {
	r := newTestRunner()
	done := make(chan struct{})
	go func() {
		r.run(Job{ScriptID: "recurse", Script: `function f(){ return f(); } f();`})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("deep recursion was not bounded")
	}
}

// TestTestCaptureCapped proves a looping test script cannot grow the run log
// without bound.
func TestTestCaptureCapped(t *testing.T) {
	r := newTestRunner()
	res := r.RunTest(`for (var i = 0; i < 100000; i++) { log('x'); }`, EventContext{})
	if len(res.Entries) > maxTestEntries+2 {
		t.Fatalf("captured %d entries, want <= %d", len(res.Entries), maxTestEntries+2)
	}
}

// TestEmitEventAllowlist proves a script may only emit its own data events and
// cannot fabricate a server-detected outcome (e.g. a report).
func TestEmitEventAllowlist(t *testing.T) {
	bad := newTestRunner().RunTest(`emitEvent('campaign_recipient_reported', {});`, EventContext{})
	if bad.OK {
		t.Fatalf("emitEvent('campaign_recipient_reported') should be rejected")
	}
	good := newTestRunner().RunTest(`emitEvent('campaign_recipient_submitted_data', { a: 1 });`, EventContext{})
	if !good.OK {
		t.Fatalf("emitEvent('campaign_recipient_submitted_data') should be allowed: %s", good.Error)
	}
}

// TestScriptTimeout proves a runaway script is interrupted, not hung.
func TestScriptTimeout(t *testing.T) {
	r := newTestRunner()
	r.Timeout = 300 * time.Millisecond

	done := make(chan struct{})
	go func() {
		r.run(Job{ScriptID: "loop", Script: `while (true) {}`})
		close(done)
	}()

	select {
	case <-done:
		// interrupted and returned
	case <-time.After(3 * time.Second):
		t.Fatalf("runaway script was not interrupted")
	}
}

// TestScriptStop proves stop() exits cleanly and code after it does not run.
func TestScriptStop(t *testing.T) {
	var called bool
	emit := func(name string, data map[string]interface{}) error {
		called = true
		return nil
	}
	r := newTestRunner()
	r.run(Job{
		ScriptID: "stop",
		Script:   `stop(); emitEvent('should_not_fire', {});`,
		Emit:     emit,
	})
	if called {
		t.Fatalf("code after stop() ran")
	}
}

// TestScriptStopCaughtDoesNotSwallowLaterError proves stop() is detected by
// identity, not a latched flag: a script that catches stop() and then throws a
// real error still has that error reported instead of being treated as a clean
// stop.
func TestScriptStopCaughtDoesNotSwallowLaterError(t *testing.T) {
	var reported bool
	var phase string
	emit := func(name string, data map[string]interface{}) error {
		if lvl, _ := data["level"].(string); lvl == "error" {
			reported = true
			phase, _ = data["phase"].(string)
		}
		return nil
	}
	r := newTestRunner()
	r.run(Job{
		ScriptID: "caught-stop",
		Script:   `try { stop(); } catch (e) {} throw new Error('boom');`,
		Emit:     emit,
	})
	if !reported {
		t.Fatalf("a real error after a caught stop() was swallowed")
	}
	if phase != "exception" {
		t.Fatalf("expected phase 'exception', got %q", phase)
	}
}

// TestDispatcherDropsWhenFull proves the pool bounds work: a full queue drops.
func TestDispatcherDropsWhenFull(t *testing.T) {
	// one worker, tiny queue, a slow script so the queue fills
	d := NewDispatcher(zap.NewNop().Sugar(), 1, 1, time.Second)
	d.Start()
	defer d.Stop()

	block := make(chan struct{})
	// fill the single worker with a job that blocks via a slow emit
	slow := func(string, map[string]interface{}) error {
		<-block
		return nil
	}
	// occupy the worker
	d.Enqueue(Job{Script: `emitEvent('x', {});`, Emit: slow})
	time.Sleep(50 * time.Millisecond)
	// fill the queue (depth 1)
	d.Enqueue(Job{Script: `1;`})
	// next enqueue should be dropped
	dropped := !d.Enqueue(Job{Script: `1;`})
	close(block)
	if !dropped {
		t.Fatalf("expected a full queue to drop the job")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
