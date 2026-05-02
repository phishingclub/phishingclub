package remotebrowser

import (
	"bytes"
	"context"
	"io"

	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/dop251/goja"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
)

// Config holds browser connection and execution settings configurable by platform admins.
type Config struct {
	Mode       string   `json:"mode"`       // "local" or "remote"
	Remote     string   `json:"remote"`     // DevTools WS URL (mode=remote)
	Proxy      string   `json:"proxy"`      // socks5:// or http:// (mode=local)
	Headless   bool     `json:"headless"`   // run Chrome in headless mode (mode=local)
	Timeout    int      `json:"timeout"`    // ms, 0 = use DefaultTimeout
	Lang       string   `json:"lang"`       // BCP 47 locale e.g. "en-US" (mode=local)
	ExtraFlags []string `json:"extraFlags"` // additional Chrome CLI flags e.g. ["--use-gl=egl"] (mode=local)
}

const DefaultTimeout = 60_000 // ms

// DefaultChromiumUA default user-agent
const DefaultChromiumUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// DefaultConfig returns a default config.
func DefaultConfig() Config {
	return Config{
		Mode:    "local",
		Timeout: DefaultTimeout,
	}
}

// ParseConfig parses a JSON config string, returning defaults for empty input.
func ParseConfig(raw string) (Config, error) {
	cfg := DefaultConfig()
	if raw == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, err
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Mode != "remote" {
		cfg.Remote = ""
	}
	return cfg, nil
}

// chromeEnv builds a minimal environment for the Chrome subprocess, plus any
// overrides. Passing os.Environ() to Chrome leaks secrets into the renderer process.
// Only variables Chrome actually needs are forwarded.
func chromeEnv(overrides ...string) []string {
	allowed := map[string]bool{
		"HOME": true, "PATH": true, "USER": true, "LOGNAME": true,
		"DISPLAY": true, "XAUTHORITY": true, "DBUS_SESSION_BUS_ADDRESS": true,
		"FONTCONFIG_PATH": true, "FONTCONFIG_FILE": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if allowed[key] {
			env = append(env, kv)
		}
	}
	return append(env, overrides...)
}

// resolveBrowserRootDir returns the directory Rod uses to cache its auto-downloaded
// Chromium. We use a path next to the running binary rather than $HOME/.cache
func resolveBrowserRootDir() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate browser cache dir: %w", err)
	}
	return filepath.Join(filepath.Dir(execPath), "data", "browser"), nil
}

// chromeSterrWriter is an io.Writer that fans Chrome stdout/stderr lines out to
// the session emitter (when emitter != nil) and/or the app logger (when logger != nil).
type chromeSterrWriter struct {
	emitter *channelEmitter    // non-nil → forward to session event stream
	logger  *zap.SugaredLogger // non-nil → forward to app debug log
	buf     []byte
}

func (w *chromeSterrWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSpace(string(w.buf[:idx]))
		w.buf = w.buf[idx+1:]
		if line != "" {
			if w.emitter != nil {
				w.emitter.log("[chrome] " + line)
			}
			if w.logger != nil {
				w.logger.Debugw(line, "source", "chrome")
			}
		}
	}
	return len(p), nil
}

// scriptStopError is thrown by stop() for a clean script exit with no error emitted.
type scriptStopError struct{}

func (scriptStopError) Error() string { return "script stopped" }

// knownGoErrors maps substrings in a GoError message to a friendlier single line description.
// Only Rod/CDP/network-specific errors belong here. context.Canceled and
// context.DeadlineExceeded are handled earlier via errors.Is and never reach cleanGoError.
var knownGoErrors = []struct {
	substr  string
	message string
}{
	{"connection refused", "browser connection refused — is Chrome running?"},
	{"use of closed network connection", "browser connection closed unexpectedly"},
	{"i/o timeout", "browser CDP connection timed out"},
	{"EOF", "browser disconnected unexpectedly"},
	{"Target closed", "browser tab was closed"},
	{"page not found", "browser tab was closed"},
}

// cleanGoError returns a friendly single line message for common GoError strings,
// stripping the "GoError: " prefix and JS stack trace. Unknown errors are returned
// with the prefix stripped but the stack trace removed.
func cleanGoError(s string) string {
	// Strip leading "GoError: " prefix if present.
	msg := strings.TrimPrefix(s, "GoError: ")
	// Remove everything from the first newline (stack trace).
	if idx := strings.IndexByte(msg, '\n'); idx >= 0 {
		msg = msg[:idx]
	}
	// Map messages to friendly descriptions.
	lower := strings.ToLower(msg)
	for _, e := range knownGoErrors {
		if strings.Contains(lower, e.substr) {
			return e.message
		}
	}
	return msg
}

// Runner executes a JS script against a Chrome instance and streams events
// to the Events channel. The caller must close or drain Events after Run returns.
type Runner struct {
	Script string
	Config Config
	// ExecPath is the server-configured Chrome binary path (from config.json,
	// not user-supplied). Empty = Rod auto-download.
	ExecPath string
	// Logger, when set, receives Chrome process stdout/stderr at debug level.
	Logger   *zap.SugaredLogger
	Events   chan RunEvent    // server → client
	Incoming chan IncomingMsg // client → script (victim events / test injections)
	// BrowserCh receives the *rod.Page as soon as newSession() spawns the browser.
	// The caller reads this once to obtain the page for streaming.
	BrowserCh chan *rod.Page
	// LiveCh receives the *rod.Page when s.keepAlive() is called (kept for compat).
	LiveCh chan *rod.Page
	// StreamCh receives commands from s.stream(selector, name) / stop().
	StreamCh chan StreamCmd
	// keepAliveActive is set by s.keepAlive() so Run() parks after the script
	// finishes, waiting for the operator to explicitly end the session.
	keepAliveActive atomic.Bool
}

// IncomingMsg is an event sent from the client into the running script.
// Wire format: {"event": "credentials", "data": {"username": "...", "password": "..."}}
type IncomingMsg struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// StreamCmd is sent on Runner.StreamCh when the script calls s.stream() or the returned stop().
type StreamCmd struct {
	Op       string    // "start" | "stop"
	Selector string    // CSS selector (Op=start)
	Name     string    // stream name
	Page     *rod.Page // rod page (Op=start)
	MaxFps   int       // 0 = unlimited
	Quality  int       // JPEG re-encode quality 1-100; 0 = default (92)
}

// NewRunner creates a Runner with buffered event channels.
func NewRunner(script string, cfg Config) *Runner {
	return &Runner{
		Script:    script,
		Config:    cfg,
		Events:    make(chan RunEvent, 256),
		Incoming:  make(chan IncomingMsg, 256),
		BrowserCh: make(chan *rod.Page, 1),
		LiveCh:    make(chan *rod.Page, 1),
		StreamCh:  make(chan StreamCmd, 16),
	}
}

// Run executes the script. It blocks until the script finishes, the context is
// cancelled, or the global timeout fires. The Events channel is closed when Run
// returns.
func (r *Runner) Run(ctx context.Context) error {
	defer close(r.Events)
	defer close(r.StreamCh)

	emitter := newChannelEmitter(r.Events)

	// Last resort recovery: rod can panic with nil-pointer dereferences inside goja
	// native callbacks. Goja panics non-Value panics, which would crash the process.
	// Catch anything that escapes vm.RunString so a broken script never takes down the server.
	defer func() {
		if rec := recover(); rec != nil {
			emitter.errorf(fmt.Sprintf("internal error: %v", rec))
		}
	}()

	timeout := time.Duration(r.Config.Timeout) * time.Millisecond
	outerCtx := ctx // operator-level context; only cancelled explicitly
	ctx, timeoutCancel := context.WithTimeout(outerCtx, timeout)
	defer timeoutCancel()

	vm := goja.New()

	// Interrupt the JS VM on termination. Two cases:
	//   DeadlineExceeded — real timeout: interrupt immediately.
	//   Canceled         — either keepAlive() cancelled the script timeout to
	//                      park the session, or the operator cancelled. Either
	//                      way wait for the outer context so we only interrupt
	//                      when the operator actually ends the session.
	go func() {
		<-ctx.Done()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			vm.Interrupt(ctx.Err())
			return
		}
		<-outerCtx.Done()
		vm.Interrupt(outerCtx.Err())
	}()

	// vmArgStr returns "" for undefined/null instead of the string "undefined".
	vmArgStr := func(v goja.Value) string {
		if goja.IsUndefined(v) || goja.IsNull(v) {
			return ""
		}
		return v.String()
	}

	vm.Set("stop", func(call goja.FunctionCall) goja.Value {
		panic(vm.NewGoError(scriptStopError{}))
	})

	vm.Set("emit", func(call goja.FunctionCall) goja.Value {
		key := vmArgStr(call.Argument(0))
		value := call.Argument(1).Export()
		emitter.emit(key, value)
		return goja.Undefined()
	})

	vm.Set("log", func(call goja.FunctionCall) goja.Value {
		msg := vmArgStr(call.Argument(0))
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) && !goja.IsNull(call.Argument(1)) {
			emitter.log(msg, call.Argument(1).Export())
		} else {
			emitter.log(msg)
		}
		return goja.Undefined()
	})

	vm.Set("info", func(call goja.FunctionCall) goja.Value {
		msg := vmArgStr(call.Argument(0))
		emitter.info(msg)
		return goja.Undefined()
	})

	vm.Set("submitData", func(call goja.FunctionCall) goja.Value {
		data := call.Argument(0).Export()
		emitter.submitData(data)
		return goja.Undefined()
	})

	// eventQueue buffers events received by race() that didn't match any race condition,
	// so they remain available for subsequent waitForEvent / waitForAny calls.
	var eventQueue []IncomingMsg
	enqueue := func(msg IncomingMsg) {
		if len(eventQueue) < 512 {
			eventQueue = append(eventQueue, msg)
		}
	}

	// nextMatchingEvent returns the first event in eventQueue whose name is in keySet,
	// removing it from the queue. Returns ok=false if no match is buffered.
	nextMatchingEvent := func(keySet map[string]bool) (IncomingMsg, bool) {
		for i, msg := range eventQueue {
			if keySet[msg.Event] {
				eventQueue = append(eventQueue[:i], eventQueue[i+1:]...)
				return msg, true
			}
		}
		return IncomingMsg{}, false
	}

	vm.Set("waitForEvent", func(call goja.FunctionCall) goja.Value {
		key := vmArgStr(call.Argument(0))
		emitter.log(fmt.Sprintf("[waitForEvent] waiting for %q", key))
		ks := map[string]bool{key: true}
		if msg, ok := nextMatchingEvent(ks); ok {
			emitter.log(fmt.Sprintf("[waitForEvent] received %q (queued)", key))
			return vm.ToValue(msg.Data)
		}
		for {
			select {
			case <-ctx.Done():
				panic(vm.NewGoError(ctx.Err()))
			case msg := <-r.Incoming:
				if msg.Event == key {
					emitter.log(fmt.Sprintf("[waitForEvent] received %q", key))
					return vm.ToValue(msg.Data)
				}
				enqueue(msg)
			}
		}
	})

	vm.Set("waitForAny", func(call goja.FunctionCall) goja.Value {
		// waitForAny(["password", "username"]) or waitForAny("password", "username")
		// Returns {event: "...", data: ...} for whichever arrives first.
		keySet := make(map[string]bool)
		if len(call.Arguments) == 1 {
			if arr, ok := call.Argument(0).Export().([]interface{}); ok {
				for _, v := range arr {
					keySet[fmt.Sprintf("%v", v)] = true
				}
			} else {
				keySet[vmArgStr(call.Argument(0))] = true
			}
		} else {
			for _, a := range call.Arguments {
				keySet[vmArgStr(a)] = true
			}
		}
		if msg, ok := nextMatchingEvent(keySet); ok {
			result := vm.NewObject()
			result.Set("event", msg.Event)
			result.Set("data", vm.ToValue(msg.Data))
			return result
		}
		for {
			select {
			case <-ctx.Done():
				panic(vm.NewGoError(ctx.Err()))
			case msg := <-r.Incoming:
				if keySet[msg.Event] {
					result := vm.NewObject()
					result.Set("event", msg.Event)
					result.Set("data", vm.ToValue(msg.Data))
					return result
				}
				enqueue(msg)
			}
		}
	})

	// retry(max, fn) or retry({max, wait}, fn)
	// fn receives a RetryContext and returns truthy to break (the value is returned),
	// or false/undefined to retry. Returns null when all attempts are exhausted.
	vm.Set("retry", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewTypeError("retry: expected (max, fn) or ({max, wait}, fn)"))
		}

		maxAttempts := 10
		waitMs := 0

		firstArg := call.Argument(0)
		switch firstArg.Export().(type) {
		case int64, float64, int, int32, int16, int8, uint, uint64, uint32, uint16, uint8:
			maxAttempts = int(firstArg.ToInteger())
		default:
			obj := firstArg.ToObject(vm)
			if mv := obj.Get("max"); mv != nil && !goja.IsUndefined(mv) && !goja.IsNull(mv) {
				maxAttempts = int(mv.ToInteger())
			}
			if wv := obj.Get("wait"); wv != nil && !goja.IsUndefined(wv) && !goja.IsNull(wv) {
				waitMs = int(wv.ToInteger())
			}
		}

		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(vm.NewTypeError("retry: second argument must be a function"))
		}

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			loopCtx := vm.NewObject()
			loopCtx.Set("attempt", attempt)
			loopCtx.Set("max", maxAttempts)
			loopCtx.Set("isFirst", attempt == 1)
			loopCtx.Set("isLast", attempt == maxAttempts)

			result, err := fn(goja.Undefined(), loopCtx)
			if err != nil {
				panic(err)
			}

			// explicit false → retry; any other truthy value (including true) → done
			if !goja.IsNull(result) && !goja.IsUndefined(result) {
				if b, isBool := result.Export().(bool); !isBool || b {
					return result
				}
			}

			if waitMs > 0 && attempt < maxAttempts {
				timer := time.NewTimer(time.Duration(waitMs) * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					panic(vm.NewGoError(ctx.Err()))
				case <-timer.C:
				}
			}
		}

		return goja.Null()
	})

	// Note: withTimeout is intentionally NOT available at the top level because
	// there is no page context to thread through. Use session.withTimeout(ms, fn) instead.

	vm.Set("newSession", func(call goja.FunctionCall) goja.Value {
		type sessionOpts struct {
			Proxy        string
			Remote       string
			Headless     bool
			IdleTimeout  int // ms; 0 = disabled
			Debug        bool
			ChromeDebug  bool
			QueryTimeout int // ms; 0 = no timeout on read ops
			UserAgent    string
			Lang         string   // BCP 47 locale, e.g. "en-US" - sets --lang flag (local mode only)
			ExtraFlags   []string // additional Chrome CLI flags e.g. ["--use-gl=egl"] (local mode only)
		}
		opts := sessionOpts{
			Headless:   r.Config.Headless,
			Lang:       r.Config.Lang,
			ExtraFlags: r.Config.ExtraFlags,
		}
		if r.Config.Proxy != "" {
			opts.Proxy = r.Config.Proxy
		}
		if r.Config.Mode == "remote" && r.Config.Remote != "" {
			opts.Remote = r.Config.Remote
		}

		if len(call.Arguments) > 0 {
			obj := call.Argument(0).ToObject(vm)
			if obj != nil {
				if v := obj.Get("proxy"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					opts.Proxy = v.String()
				}
				if v := obj.Get("remote"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					opts.Remote = v.String()
				}
				if v := obj.Get("headless"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					opts.Headless = v.ToBoolean()
				}
				if v := obj.Get("idleTimeout"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					opts.IdleTimeout = int(v.ToInteger())
				}
				if v := obj.Get("debug"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					opts.Debug = v.ToBoolean()
				}
				if v := obj.Get("chromeDebug"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					opts.ChromeDebug = v.ToBoolean()
				}
				if v := obj.Get("queryTimeout"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					opts.QueryTimeout = int(v.ToInteger())
				}
				if v := obj.Get("userAgent"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					opts.UserAgent = v.String()
				}
				if v := obj.Get("lang"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					opts.Lang = v.String()
				}
				if v := obj.Get("extraFlags"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					if arr, ok := v.Export().([]interface{}); ok {
						for _, item := range arr {
							if s, ok := item.(string); ok && s != "" {
								opts.ExtraFlags = append(opts.ExtraFlags, s)
							}
						}
					}
				}
			}
		}

		var browser *rod.Browser
		var page *rod.Page

		if opts.Remote != "" {
			if opts.Proxy != "" {
				emitter.log(fmt.Sprintf("[session] warning: proxy %q ignored for remote browser", opts.Proxy))
			}
			// ResolveURL normalises any of: bare port "9222", "http://host:9222",
			// or a full "ws://..." URL to the actual DevTools WebSocket URL by
			// fetching /json/version. Users don't need to copy the UUID manually.
			wsURL, resolveErr := launcher.ResolveURL(opts.Remote)
			if resolveErr != nil {
				emitter.errorf(fmt.Sprintf("browser connect failed: cannot resolve remote URL %q: %v", opts.Remote, resolveErr))
				return goja.Undefined()
			}
			emitter.log(fmt.Sprintf("[session] connecting to remote browser at %s", wsURL))
			browser = rod.New().ControlURL(wsURL).Context(outerCtx)
			if connectErr := browser.Connect(); connectErr != nil {
				emitter.errorf(fmt.Sprintf("browser connect failed: %v", connectErr))
				return goja.Undefined()
			}
			var pageErr error
			page, pageErr = browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
			if pageErr != nil {
				emitter.errorf(fmt.Sprintf("page create failed: %v", pageErr))
				browser.Close() //nolint:errcheck
				return goja.Undefined()
			}
		} else {
			rootDir, err := resolveBrowserRootDir()
			if err != nil {
				emitter.errorf(err.Error())
				return goja.Undefined()
			}
			// Newer Chromium requires writable XDG dirs and a crash-dumps-dir at
			// startup (see go-rod#1126). Use subdirs of the browser root so all
			// Chrome-related data lives in one place.
			crashDir := filepath.Join(rootDir, "crashes")
			if err := os.MkdirAll(crashDir, 0755); err != nil {
				emitter.log(fmt.Sprintf("[session] warning: could not create crash dir: %v", err))
			}
			if err := os.MkdirAll(filepath.Join(rootDir, "config"), 0755); err != nil {
				emitter.log(fmt.Sprintf("[session] warning: could not create config dir: %v", err))
			}
			if err := os.MkdirAll(filepath.Join(rootDir, "cache"), 0755); err != nil {
				emitter.log(fmt.Sprintf("[session] warning: could not create cache dir: %v", err))
			}

			var chromeLogger io.Writer = io.Discard
			if opts.ChromeDebug || r.Logger != nil {
				cw := &chromeSterrWriter{logger: r.Logger}
				if opts.ChromeDebug {
					cw.emitter = emitter
				}
				chromeLogger = cw
			}
			l := launcher.New().
				Headless(opts.Headless).
				Logger(chromeLogger).
				Set("disable-crash-reporter").
				Set("crash-dumps-dir", crashDir).
				Set("disable-blink-features", "AutomationControlled").
				Delete("enable-automation").
				Env(chromeEnv(
					"XDG_CONFIG_HOME="+filepath.Join(rootDir, "config"),
					"XDG_CACHE_HOME="+filepath.Join(rootDir, "cache"),
				)...)

			if r.ExecPath != "" {
				emitter.log(fmt.Sprintf("[session] using browser: %s", r.ExecPath))
				l = l.Bin(r.ExecPath)
			} else {
				b := launcher.NewBrowser()
				b.RootDir = rootDir
				binPath := b.BinPath()
				if _, err := os.Stat(binPath); os.IsNotExist(err) {
					if err := b.Download(); err != nil {
						emitter.errorf(fmt.Sprintf("browser download failed: %v", err))
						return goja.Undefined()
					}
				}
				emitter.log(fmt.Sprintf("[session] using browser: %s", binPath))
				l = l.Bin(binPath)
			}
			if opts.Proxy != "" {
				emitter.log(fmt.Sprintf("[session] using proxy: %s", opts.Proxy))
				l = l.Proxy(opts.Proxy).Set("proxy-bypass-list", "")
			}
			if opts.Lang != "" {
				// Sets navigator.language, navigator.languages, and the Accept-Language
				// HTTP header at the process level - consistent across main frame and
				// Web Workers (unlike Page.addScriptToEvaluateOnNewDocument which only
				// runs in the main frame).
				emitter.log(fmt.Sprintf("[session] using lang: %s", opts.Lang))
				l = l.Set("lang", opts.Lang).Set("accept-lang", opts.Lang)
			}
			for _, rawFlag := range opts.ExtraFlags {
				rawFlag = strings.TrimSpace(rawFlag)
				// "!--flag-name" removes a flag from the launcher (e.g. to strip a rod default).
				if strings.HasPrefix(rawFlag, "!--") {
					name := flags.Flag(strings.TrimPrefix(rawFlag, "!--"))
					l = l.Delete(name)
					emitter.log(fmt.Sprintf("[session] removed flag: --%s", string(name)))
					continue
				}
				if !strings.HasPrefix(rawFlag, "--") {
					continue
				}
				rawFlag = strings.TrimPrefix(rawFlag, "--")
				parts := strings.SplitN(rawFlag, "=", 2)
				key := flags.Flag(parts[0])
				if len(parts) == 2 {
					l = l.Set(key, parts[1])
				} else {
					l = l.Set(key)
				}
				emitter.log(fmt.Sprintf("[session] extra flag: --%s", parts[0]))
			}
			u, err := l.Launch()
			if err != nil {
				emitter.errorf(fmt.Sprintf("browser launch failed: %v", err))
				return goja.Undefined()
			}
			// Kill and clean up Chrome when the session ends. Without this, stopping
			// a run before s.close() is called leaves a zombie Chrome process (~200-300 MB).
			go func() {
				<-outerCtx.Done()
				l.Kill()
				l.Cleanup()
			}()
			browser = rod.New().ControlURL(u).Context(outerCtx)
			if err := browser.Connect(); err != nil {
				emitter.errorf(fmt.Sprintf("browser connect failed: %v", err))
				return goja.Undefined()
			}
			page, err = browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
			if err != nil {
				emitter.errorf(fmt.Sprintf("page create failed: %v", err))
				browser.Close() //nolint:errcheck
				return goja.Undefined()
			}
			/*
				// ignore "debugger;" code lines explicitly
				err = proto.DebuggerSetSkipAllPauses{Skip: true}.Call(page)
				if err != nil {
					emitter.errorf(fmt.Sprintf("skip debug failed: %v", err))
					browser.Close() //nolint:errcheck
					return goja.Undefined()
				}
				// patch console Log
				_, err = page.EvalOnNewDocument(`() => {
						const noop = () => {};

						console.log = noop;
						console.table = noop;
						console.clear = noop;
						console.debug = noop;
						console.info = noop;
						console.warn = noop;
				}`)
				if err != nil {
					emitter.errorf(fmt.Sprintf("console patch failed: %v", err))
					browser.Close() //nolint:errcheck
					return goja.Undefined()
				}
			*/
		}

		// Apply user-agent override. When headless and no explicit UA is set we use
		// DefaultChromiumUA to strip "HeadlessChrome" from the header
		ua := opts.UserAgent
		if ua == "" && opts.Headless {
			ua = DefaultChromiumUA
		}
		if ua != "" {
			if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: ua}); err != nil {
				emitter.log(fmt.Sprintf("[session] warning: failed to set user-agent: %v", err))
			}
		}

		// Signal the controller that a page is ready for streaming.
		select {
		case r.BrowserCh <- page:
		default:
		}

		session := vm.NewObject()
		RegisterBrowserBindings(vm, session, page, emitter, opts.Debug, opts.QueryTimeout)

		session.Set("withTimeout", func(call goja.FunctionCall) goja.Value {
			ms := call.Argument(0).ToInteger()
			fn, ok := goja.AssertFunction(call.Argument(1))
			if !ok {
				panic(vm.NewTypeError("withTimeout: second argument must be a function"))
			}
			tCtx, tCancel := context.WithTimeout(page.GetContext(), time.Duration(ms)*time.Millisecond)
			defer tCancel()
			tPage := page.Context(tCtx)
			tmpSession := vm.NewObject()
			RegisterBrowserBindings(vm, tmpSession, tPage, nil, opts.Debug, opts.QueryTimeout)
			_, err := fn(goja.Undefined(), tmpSession)
			if err != nil {
				// If our own timeout context expired, return false instead of
				// propagating — lets callers branch without try/catch.
				if tCtx.Err() != nil {
					return vm.ToValue(false)
				}
				panic(err)
			}
			return vm.ToValue(true)
		})

		session.Set("close", func(call goja.FunctionCall) goja.Value {
			emitter.log("[session] closing")
			if opts.Remote != "" {
				page.Close() //nolint:errcheck
			} else {
				browser.Close() //nolint:errcheck
			}
			return goja.Undefined()
		})

		// keepAlive signals the caller that the page is available for operator
		// takeover and cancels the script timeout so the parked session isn't
		// killed after 60 s. It is intentionally non-blocking: the script
		// continues after the call (so emit() calls after keepAlive() reach
		// the victim). Run() parks itself after RunString returns.
		session.Set("keepAlive", func(call goja.FunctionCall) goja.Value {
			emitter.log("[session] keeping alive for remote takeover")
			// Re-bind page to outerCtx before cancelling the timeout context.
			// The browser was created with the timed ctx; if we cancel it first,
			// page.GetContext() closes and StreamLiveSession fires "Session ended"
			// before the operator even connects.
			livePage := page.Context(outerCtx)
			select {
			case r.LiveCh <- livePage:
			default:
			}
			emitter.sendMust(outerCtx, RunEvent{
				Type: "keep_alive",
				Time: time.Now().UTC().Format(time.RFC3339Nano),
			})
			r.keepAliveActive.Store(true)
			timeoutCancel() // release script timeout — operator controls lifetime now
			return goja.Undefined()
		})

		// Event-driven API: s.on(event, fn) + s.listen() + s.done()
		handlers := map[string]goja.Callable{}
		listenDone := make(chan struct{}, 1)

		session.Set("on", func(call goja.FunctionCall) goja.Value {
			event := call.Argument(0).String()
			fn, ok := goja.AssertFunction(call.Argument(1))
			if !ok {
				panic(vm.NewTypeError("on: second argument must be a function"))
			}
			handlers[event] = fn
			return goja.Undefined()
		})

		session.Set("done", func(call goja.FunctionCall) goja.Value {
			select {
			case listenDone <- struct{}{}:
			default:
			}
			return goja.Undefined()
		})

		session.Set("listen", func(call goja.FunctionCall) goja.Value {
			emitter.log("[session] listening for events")

			var idleCh <-chan time.Time
			var idleTimer *time.Timer
			resetIdle := func() {
				if opts.IdleTimeout > 0 {
					if idleTimer != nil {
						idleTimer.Stop()
					}
					idleTimer = time.NewTimer(time.Duration(opts.IdleTimeout) * time.Millisecond)
					idleCh = idleTimer.C
				}
			}
			defer func() {
				if idleTimer != nil {
					idleTimer.Stop()
				}
			}()
			resetIdle()

			for {
				select {
				case <-ctx.Done():
					return goja.Undefined()
				case <-listenDone:
					return goja.Undefined()
				case <-idleCh:
					emitter.log(fmt.Sprintf("[session] idle timeout (%dms), closing", opts.IdleTimeout))
					if opts.Remote != "" {
						page.Close() //nolint:errcheck
					} else {
						browser.Close() //nolint:errcheck
					}
					return goja.Undefined()
				case msg := <-r.Incoming:
					resetIdle()
					fn, exists := handlers[msg.Event]
					if !exists {
						emitter.log(fmt.Sprintf("[session] no handler for %q, ignoring", msg.Event))
						continue
					}
					if _, err := fn(goja.Undefined(), vm.ToValue(msg.Data)); err != nil {
						panic(err)
					}
				}
			}
		})

		// s.stream(selector, name) — non-blocking; returns {stop()} to end the stream.
		// The caller (controller) watches StreamCh to start/stop cropped frame forwarding.
		streamDebug := opts.Debug // capture bool, not struct field, to match RegisterBrowserBindings pattern
		session.Set("stream", func(call goja.FunctionCall) goja.Value {
			selector := vmArgStr(call.Argument(0))
			name := vmArgStr(call.Argument(1))
			if selector == "" || name == "" {
				panic(vm.NewTypeError("stream: selector and name are required"))
			}
			maxFps := 0
			quality := 0
			if len(call.Arguments) > 2 {
				if obj := call.Argument(2).ToObject(vm); obj != nil {
					if v := obj.Get("maxFps"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
						maxFps = int(v.ToInteger())
					}
					if v := obj.Get("quality"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
						quality = int(v.ToInteger())
					}
				}
			}
			if streamDebug {
				emitter.log(fmt.Sprintf("[dbg] → stream %s as %q (maxFps=%d, quality=%d)", selector, name, maxFps, quality))
			}
			select {
			case r.StreamCh <- StreamCmd{Op: "start", Selector: selector, Name: name, Page: page, MaxFps: maxFps, Quality: quality}:
			default:
				emitter.log(fmt.Sprintf("[stream] warning: StreamCh full, dropping start for %q", name))
			}
			if streamDebug {
				emitter.log(fmt.Sprintf("[dbg] ✓ stream %s as %q started", selector, name))
			}
			stopObj := vm.NewObject()
			stopped := false
			stopObj.Set("stop", func(call goja.FunctionCall) goja.Value {
				if !stopped {
					stopped = true
					if streamDebug {
						emitter.log(fmt.Sprintf("[dbg] → stream stop %q", name))
					}
					select {
					case r.StreamCh <- StreamCmd{Op: "stop", Name: name}:
					default:
						emitter.log(fmt.Sprintf("[stream] warning: StreamCh full, dropping stop for %q", name))
					}
					if streamDebug {
						emitter.log(fmt.Sprintf("[dbg] ✓ stream stop %q", name))
					}
				}
				return goja.Undefined()
			})
			return stopObj
		})

		session.Set("capture", func(call goja.FunctionCall) goja.Value {
			// Options:
			//   domains      []string  - filter cookies to these domains via CDP (e.g. ["google.com"])
			//   cookieNames  []string  - only keep cookies with these names
			//   localStorage    bool   - include localStorage (default true when no domains given)
			//   sessionStorage  bool   - include sessionStorage (default true when no domains given)
			var domains []string
			var cookieNames []string
			lsExplicit, ssExplicit := false, false
			capLS := true
			capSS := true

			if len(call.Arguments) > 0 {
				obj := call.Argument(0).ToObject(vm)
				if obj != nil {
					if v := obj.Get("domains"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
						if arr, ok := v.Export().([]any); ok {
							for _, d := range arr {
								domains = append(domains, fmt.Sprintf("%v", d))
							}
						}
					}
					if v := obj.Get("cookieNames"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
						if arr, ok := v.Export().([]any); ok {
							for _, n := range arr {
								cookieNames = append(cookieNames, fmt.Sprintf("%v", n))
							}
						}
					}
					if v := obj.Get("localStorage"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
						capLS = v.ToBoolean()
						lsExplicit = true
					}
					if v := obj.Get("sessionStorage"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
						capSS = v.ToBoolean()
						ssExplicit = true
					}
				}
			}

			// When domains are specified, skip storage by default unless the caller explicitly opted in.
			if len(domains) > 0 {
				if !lsExplicit {
					capLS = false
				}
				if !ssExplicit {
					capSS = false
				}
			}

			result := map[string]any{}

			if len(domains) > 0 {
				urls := make([]string, len(domains))
				for i, d := range domains {
					d = strings.TrimPrefix(d, ".")
					if !strings.HasPrefix(d, "http") {
						d = "https://" + d
					}
					urls[i] = d
				}
				res, err := proto.NetworkGetCookies{Urls: urls}.Call(page)
				if err == nil {
					cookies := res.Cookies
					// Filter by name if requested.
					if len(cookieNames) > 0 {
						nameSet := make(map[string]bool, len(cookieNames))
						for _, n := range cookieNames {
							nameSet[n] = true
						}
						filtered := cookies[:0]
						for _, c := range cookies {
							if nameSet[c.Name] {
								filtered = append(filtered, c)
							}
						}
						cookies = filtered
					}
					result["cookies"] = cookies
				} else {
					emitter.log(fmt.Sprintf("[capture] cookies: %s", err))
				}
			} else {
				res, err := proto.StorageGetCookies{}.Call(page)
				if err == nil {
					cookies := res.Cookies
					// Filter by name if requested.
					if len(cookieNames) > 0 {
						nameSet := make(map[string]bool, len(cookieNames))
						for _, n := range cookieNames {
							nameSet[n] = true
						}
						filtered := cookies[:0]
						for _, c := range cookies {
							if nameSet[c.Name] {
								filtered = append(filtered, c)
							}
						}
						cookies = filtered
					}
					result["cookies"] = cookies
				} else {
					emitter.log(fmt.Sprintf("[capture] cookies: %s", err))
				}
			}

			evalStorage := func(key string, storeName string) {
				script := fmt.Sprintf(`() => (function(){try{var s=window[%q],o={};for(var i=0;i<s.length;i++){var k=s.key(i);o[k]=s.getItem(k);}return JSON.stringify(o);}catch(e){return "{}"}})()`, storeName)
				res, err := page.Eval(script)
				if err != nil {
					emitter.log(fmt.Sprintf("[capture] %s: %s", key, err))
					return
				}
				var data interface{}
				if json.Unmarshal([]byte(res.Value.Str()), &data) == nil {
					result[key] = data
				}
			}

			if capLS {
				evalStorage("localStorage", "localStorage")
			}
			if capSS {
				evalStorage("sessionStorage", "sessionStorage")
			}

			emitter.capture(result)
			return vm.ToValue(result)
		})

		// makeFrameSession builds a DOM-only sub-session bound to framePage.
		// Exposes RegisterBrowserBindings + withTimeout + frame (for nested iframes).
		var makeFrameSession func(framePage *rod.Page) goja.Value
		makeFrameSession = func(framePage *rod.Page) goja.Value {
			fs := vm.NewObject()
			RegisterBrowserBindings(vm, fs, framePage, emitter, opts.Debug, opts.QueryTimeout)
			fs.Set("withTimeout", func(call goja.FunctionCall) goja.Value {
				ms := call.Argument(0).ToInteger()
				fn, ok := goja.AssertFunction(call.Argument(1))
				if !ok {
					panic(vm.NewTypeError("withTimeout: second argument must be a function"))
				}
				tCtx, tCancel := context.WithTimeout(framePage.GetContext(), time.Duration(ms)*time.Millisecond)
				defer tCancel()
				tFramePage := framePage.Context(tCtx)
				tmpFrame := vm.NewObject()
				RegisterBrowserBindings(vm, tmpFrame, tFramePage, nil, opts.Debug, opts.QueryTimeout)
				_, err := fn(goja.Undefined(), tmpFrame)
				if err != nil {
					if tCtx.Err() != nil {
						return vm.ToValue(false)
					}
					panic(err)
				}
				return vm.ToValue(true)
			})
			fs.Set("frame", func(call goja.FunctionCall) goja.Value {
				sel := vmArgStr(call.Argument(0))
				if sel == "" {
					panic(vm.NewTypeError("frame: selector is required"))
				}
				if opts.Debug {
					emitter.log("[dbg] → frame " + sel)
				}
				el, err := framePage.Element(sel)
				if err != nil {
					emitter.log(fmt.Sprintf("[frame] %q not found: %v", sel, err))
					return goja.Null()
				}
				nestedPage, err := el.Frame()
				if err != nil {
					emitter.log(fmt.Sprintf("[frame] %q frame resolve failed: %v", sel, err))
					return goja.Null()
				}
				if opts.Debug {
					emitter.log("[dbg] ✓ frame " + sel)
				}
				return makeFrameSession(nestedPage.Context(framePage.GetContext()))
			})
			return fs
		}

		session.Set("frame", func(call goja.FunctionCall) goja.Value {
			sel := vmArgStr(call.Argument(0))
			if sel == "" {
				panic(vm.NewTypeError("frame: selector is required"))
			}
			if opts.Debug {
				emitter.log("[dbg] → frame " + sel)
			}
			el, err := page.Element(sel)
			if err != nil {
				emitter.log(fmt.Sprintf("[frame] %q not found: %v", sel, err))
				return goja.Null()
			}
			framePage, err := el.Frame()
			if err != nil {
				emitter.log(fmt.Sprintf("[frame] %q frame resolve failed: %v", sel, err))
				return goja.Null()
			}
			if opts.Debug {
				emitter.log("[dbg] ✓ frame " + sel)
			}
			return makeFrameSession(framePage.Context(page.GetContext()))
		})

		// raceDOMCondJS returns a JS function body that evaluates the given condition
		// type against sel, returning the selector string on match or null otherwise.
		raceDOMCondJS := func(condType, sel string) string {
			q := fmt.Sprintf("document.querySelector(%q)", sel)
			r := fmt.Sprintf("%q", sel)
			switch condType {
			case "visible":
				return fmt.Sprintf(`()=>{var el=%s;if(!el)return null;var b=el.getBoundingClientRect();return(b.width!==0||b.height!==0)?%s:null}`, q, r)
			case "ready":
				return fmt.Sprintf(`()=>{var el=%s;if(!el)return null;var b=el.getBoundingClientRect();if(b.width===0&&b.height===0)return null;return el.disabled?null:%s}`, q, r)
			case "enabled":
				return fmt.Sprintf(`()=>{var el=%s;return(el&&!el.disabled)?%s:null}`, q, r)
			case "notVisible":
				return fmt.Sprintf(`()=>{var el=%s;if(!el)return %s;var b=el.getBoundingClientRect();return(b.width===0&&b.height===0)?%s:null}`, q, r, r)
			case "notPresent":
				return fmt.Sprintf(`()=>{return !%s?%s:null}`, q, r)
			default: // "present"
				return fmt.Sprintf(`()=>{return %s?%s:null}`, q, r)
			}
		}

		// s.race(conditions) races DOM conditions, URL substrings, and incoming events
		// simultaneously. condition keys: visible, ready, enabled, notVisible, notPresent,
		// present (DOM), url (substring), event (name).
		// Returns { key, value } for whichever condition fires first.
		session.Set("race", func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				panic(vm.NewTypeError("race: expected an object with named conditions"))
			}
			condObj := call.Argument(0).ToObject(vm)

			type domCond struct{ key, selector, condJS string }
			type urlCond struct {
				key     string
				match   func(string) bool
				display string // for logging
			}
			type evtCond struct{ key, event string }

			var doms []domCond
			var urls []urlCond
			var evts []evtCond
			evtSet := map[string]string{}
			var timeoutCh <-chan time.Time
			var timeoutKey string

			domTypes := []string{"visible", "ready", "enabled", "notVisible", "notPresent", "present"}

			for _, k := range condObj.Keys() {
				v := condObj.Get(k).ToObject(vm)
				if v == nil {
					continue
				}
				matched := false
				for _, ct := range domTypes {
					if sel := v.Get(ct); sel != nil && !goja.IsUndefined(sel) && !goja.IsNull(sel) {
						doms = append(doms, domCond{k, sel.String(), raceDOMCondJS(ct, sel.String())})
						matched = true
						break
					}
				}
				if matched {
					continue
				}
				if u := v.Get("urlContains"); u != nil && !goja.IsUndefined(u) && !goja.IsNull(u) {
					pat := u.String()
					urls = append(urls, urlCond{k, func(s string) bool { return strings.Contains(s, pat) }, pat})
				} else if u := v.Get("urlMatch"); u != nil && !goja.IsUndefined(u) && !goja.IsNull(u) {
					re, ok := u.Export().(*regexp.Regexp)
					if !ok {
						panic(vm.NewTypeError("race: urlMatch value must be a RegExp"))
					}
					urls = append(urls, urlCond{k, re.MatchString, re.String()})
				} else if e := v.Get("event"); e != nil && !goja.IsUndefined(e) && !goja.IsNull(e) {
					evts = append(evts, evtCond{k, e.String()})
					evtSet[e.String()] = k
				} else if a := v.Get("after"); a != nil && !goja.IsUndefined(a) && !goja.IsNull(a) {
					timeoutCh = time.After(time.Duration(a.ToInteger()) * time.Millisecond)
					timeoutKey = k
				}
			}

			makeResult := func(key string, value interface{}) goja.Value {
				emitter.log(fmt.Sprintf("[race] matched %q = %v", key, value))
				res := vm.NewObject()
				res.Set("key", key)
				res.Set("value", vm.ToValue(value))
				return res
			}

			// Drain event queue before blocking
			for i, msg := range eventQueue {
				if key, ok := evtSet[msg.Event]; ok {
					eventQueue = append(eventQueue[:i], eventQueue[i+1:]...)
					return makeResult(key, msg.Data)
				}
			}

			urlDisplays := make([]string, len(urls))
			for i, u := range urls {
				urlDisplays[i] = u.display
			}
			emitter.log(fmt.Sprintf("[race] waiting: doms=%d urls=%v events=%d", len(doms), urlDisplays, len(evts)))

			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					panic(vm.NewGoError(ctx.Err()))

				case <-timeoutCh:
					return makeResult(timeoutKey, "timeout")

				case msg := <-r.Incoming:
					if key, matched := evtSet[msg.Event]; matched {
						return makeResult(key, msg.Data)
					}
					enqueue(msg)

				case <-ticker.C:
					for _, d := range doms {
						res, err := page.Eval(d.condJS)
						if err != nil {
							continue
						}
						if res.Value.Nil() {
							continue
						}
						if s := res.Value.Str(); s != "" && s != "null" && s != "undefined" {
							return makeResult(d.key, d.selector)
						}
					}
					if len(urls) > 0 {
						info, err := page.Info()
						if err == nil {
							for _, u := range urls {
								if u.match(info.URL) {
									return makeResult(u.key, info.URL)
								}
							}
						}
					}
				}
			}
		})

		return session
	})

	_, err := vm.RunString("(function(){\n" + r.Script + "\n})()")
	if err != nil {
		// errors.Is/As traverse goja.Exception.Unwrap(), which extracts the Go error
		// stored in the "value" property of a GoError object. Do NOT use
		// ex.Value().Export().(error) — that returns map[string]interface{} for JS
		// objects and always fails the type assertion.
		var stopErr scriptStopError
		if errors.As(err, &stopErr) {
			emitter.done()
			return nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			// Check whether the *global* script timeout fired. If ctx.Err() is
			// DeadlineExceeded, the outer context expired — surface a clear timeout
			// message and send the session_timeout lifecycle event to the victim page.
			// Otherwise this is a per-operation timeout (withTimeout, queryTimeout).
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				emitter.errorf(fmt.Sprintf("script timed out after %ds", r.Config.Timeout/1000))
				emitter.emit("session_timeout", nil)
			} else {
				emitter.errorf("operation timed out")
			}
			emitter.done()
			return nil
		}
		if errors.Is(err, context.Canceled) {
			// Three distinct cancellation sources share this error value:
			//   1. keepAlive() called timeoutCancel() → ctx canceled, outerCtx still live.
			//      Any subsequent ctx.Done() select in race/waitFor/listen fires immediately
			//      with context.Canceled, causing RunString to return here before the park
			//      block below is ever reached. Park explicitly so the browser stays alive.
			//   2. Operator or visitor ended the session → outerCtx canceled.
			//      Emit session_closed so the victim page can react (e.g. redirect).
			if r.keepAliveActive.Load() {
				emitter.log("[session] script complete (after keepAlive), parked for operator takeover")
				<-outerCtx.Done()
				emitter.log("[session] keep-alive ended")
				emitter.done()
				return nil
			}
			emitter.emit("session_closed", nil)
			emitter.done()
			return nil
		}
		// Script-level error: surface the formatted exception.
		var ex *goja.Exception
		if errors.As(err, &ex) {
			emitter.errorf(cleanGoError(ex.String()))
			return fmt.Errorf("script error: %s", ex.String())
		}
		emitter.errorf(err.Error())
		return err
	}

	// keepAlive() was called: script has finished but the browser must stay
	// alive for operator takeover. Block here until the operator explicitly
	// cancels the session (outerCtx), then emit done so the Events channel
	// drains cleanly.
	if r.keepAliveActive.Load() {
		emitter.log("[session] script complete, parked for operator takeover")
		<-outerCtx.Done()
		emitter.log("[session] keep-alive ended")
	}

	emitter.done()
	return nil
}
