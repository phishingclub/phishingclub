// Package script runs admin authored JavaScript when a subscribed campaign
// event fires. It is the scripting counterpart to webhooks: a script receives
// the event payload, can call out over HTTP, transform data, and create a new
// campaign event in the same context.
//
// Trust model: scripts run only when the feature is enabled at the server level
// (config.Script.Enabled), which is the operator acknowledgement that every
// admin is trusted as a server admin. The engine is a goja VM with a small,
// explicit binding set (no require, no filesystem, no process access). Scripts
// are bounded by a wall clock timeout, run one per fresh VM, and execute on a
// bounded worker pool so a burst of events cannot exhaust memory or goroutines.
//
// All bindings are synchronous: http.fetch blocks and returns the response.
// Each run owns its VM on its own worker goroutine, so a blocking call stalls
// only that run.
package script

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/phishingclub/phishingclub/data"
	"go.uber.org/zap"
	"golang.org/x/net/proxy"
)

const (
	// DefaultTimeout is the wall clock budget for a single script run.
	DefaultTimeout = 10 * time.Second
	// DefaultWorkers is the number of scripts that can run at once.
	DefaultWorkers = 4
	// DefaultQueueSize is how many pending jobs are buffered before new ones
	// are dropped.
	DefaultQueueSize = 256

	// maxResponseBytes caps a fetch response body so a large download cannot
	// exhaust memory.
	maxResponseBytes = 5 << 20 // 5 MB
	// defaultFetchTimeout and maxFetchTimeout bound a single outbound request.
	defaultFetchTimeout = 10 * time.Second
	maxFetchTimeout     = 30 * time.Second

	// maxCallStackSize bounds JS recursion depth so a runaway recursive script
	// throws a StackOverflowError instead of exhausting the Go stack.
	maxCallStackSize = 2000
)

// scriptStopError is thrown by the stop() binding to end a run cleanly. It is
// detected by identity via errors.As (which traverses goja.Exception.Unwrap to
// the wrapped Go error), so a script that catches stop() and then throws a real
// error later is never mistaken for a clean stop.
type scriptStopError struct{}

func (scriptStopError) Error() string { return "script stopped" }

// emittableEvents are the only events a script may create via emitEvent: data the
// script itself authored. Server-detected outcome events (message delivery,
// opens, clicks, page visits, reports, training) are deliberately excluded so a
// script cannot fabricate a campaign's statistics. info() emits the info event
// through its own binding, not emitEvent.
var emittableEvents = map[string]bool{
	data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA: true,
	data.EVENT_CAMPAIGN_RECIPIENT_INFO:           true,
}

// EventContext is the payload handed to a script. The caller fills it after
// applying the anonymization guard and the none/basic/full data level, so a
// script never sees more than its configuration allows.
type EventContext struct {
	CampaignID   string
	RecipientID  string
	Event        string
	CampaignName string
	Email        string
	Data         map[string]interface{}
}

// EmitFunc lets a script create a new campaign event in the same context.
// The caller implements it so the write goes through the native event
// chokepoint (SaveSubmittedData plus anonymization) and does not re-trigger
// scripts.
type EmitFunc func(eventName string, data map[string]interface{}) error

// Job is a single script run.
type Job struct {
	ScriptID string
	Script   string
	Event    EventContext
	Emit     EmitFunc

	// test, when set, puts the run in capture mode: log/info/emitEvent are
	// recorded into it instead of applied, and errors are captured. Set only by
	// RunTest.
	test *TestResult
}

// Runner executes one job in a fresh goja VM.
type Runner struct {
	Logger     *zap.SugaredLogger
	HTTPClient *http.Client
	Timeout    time.Duration
}

// run executes a single job. It never returns an error to the caller: a script
// failure is logged, not propagated, because scripts are out of band.
func (r *Runner) run(job Job) {
	// a broken script or a panic in a native binding must never take down the
	// server, so catch anything that escapes the VM.
	defer func() {
		if rec := recover(); rec != nil {
			msg := fmt.Sprintf("%v", rec)
			// a test run captures failures in its own run log; keep the server log
			// quiet so the test output is the single source of truth
			if job.test == nil {
				r.Logger.Errorw("script panicked",
					"scriptID", job.ScriptID,
					"recover", msg,
				)
			}
			r.reportError(job, "panic", msg)
		}
	}()

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	vm := goja.New()
	// bound recursion so a deeply recursive script throws a JS StackOverflowError
	// instead of growing the Go stack until the process dies.
	vm.SetMaxCallStackSize(maxCallStackSize)

	// interrupt the VM when the run times out. Guard with recover so this third
	// goroutine can never take down the process, upholding the crash isolation
	// invariant even if a future change does more work here.
	go func() {
		defer func() { _ = recover() }()
		<-ctx.Done()
		vm.Interrupt(ctx.Err())
	}()

	r.registerBindings(vm, job, ctx)

	// wrap in an IIFE so the script can use return, matching the remote browser
	_, err := vm.RunString("(function(){\n" + job.Script + "\n})()")
	if err != nil {
		// a clean stop() exit is detected by identity: errors.As traverses the
		// goja exception to the wrapped Go error. A caught and rethrown error is
		// therefore never mistaken for a stop.
		var stopErr scriptStopError
		if errors.As(err, &stopErr) {
			return
		}
		if _, ok := err.(*goja.InterruptedError); ok {
			if job.test == nil {
				r.Logger.Warnw("script timed out", "scriptID", job.ScriptID)
			}
			r.reportError(job, "timeout", "script exceeded its time budget")
			return
		}
		// a native call cancelled by the run budget (for example http.fetch
		// blocked when the deadline passed) surfaces as a thrown exception, not
		// an InterruptedError; classify it as a timeout too
		if ctx.Err() == context.DeadlineExceeded {
			if job.test == nil {
				r.Logger.Warnw("script timed out", "scriptID", job.ScriptID)
			}
			r.reportError(job, "timeout", "script exceeded its time budget")
			return
		}
		// a test run surfaces the error in its own run log; don't also spam the
		// server log
		if job.test == nil {
			r.Logger.Errorw("script error",
				"scriptID", job.ScriptID,
				"error", err.Error(),
			)
		}
		r.reportError(job, "exception", err.Error())
	}
}

// reportError records an uncaught script failure as a campaign info event so
// it is visible beyond the server logs. It goes through the same event funnel as
// emitEvent, so the detail follows the campaign's data-retention and anonymity
// rules (the full message is always in the server logs). Best effort: a failure
// to record is only logged.
func (r *Runner) reportError(job Job, phase, message string) {
	if job.test != nil {
		job.test.setError(phase, message)
		return
	}
	if job.Emit == nil {
		return
	}
	// reportError is called from the deferred panic recover; a panic in Emit (a DB
	// write plus webhook fan out) would escape that recover and kill the worker, so
	// isolate it here.
	defer func() {
		if rec := recover(); rec != nil {
			r.Logger.Errorw("panic recording script error event",
				"scriptID", job.ScriptID,
				"recover", fmt.Sprintf("%v", rec),
			)
		}
	}()
	err := job.Emit(data.EVENT_CAMPAIGN_RECIPIENT_INFO, map[string]interface{}{
		"source":   "script",
		"level":    "error",
		"scriptId": job.ScriptID,
		"phase":    phase, // "exception" | "timeout" | "panic"
		"error":    message,
	})
	if err != nil {
		r.Logger.Errorw("failed to record script error event",
			"scriptID", job.ScriptID,
			"error", err,
		)
	}
}

// registerBindings installs the script API on the VM. This is the entire
// capability surface: an event payload, outbound http, encode/decode helpers,
// log, emitEvent and stop. No require, no filesystem, no process access.
func (r *Runner) registerBindings(vm *goja.Runtime, job Job, ctx context.Context) {
	// event payload, already filtered by the caller
	vm.Set("event", map[string]interface{}{
		"name":         job.Event.Event,
		"campaignId":   job.Event.CampaignID,
		"recipientId":  job.Event.RecipientID,
		"campaignName": job.Event.CampaignName,
		"email":        job.Event.Email,
		"data":         job.Event.Data,
	})

	vm.Set("stop", func(call goja.FunctionCall) goja.Value {
		panic(vm.NewGoError(scriptStopError{}))
	})

	vm.Set("log", func(call goja.FunctionCall) goja.Value {
		msg := call.Argument(0).String()
		var extra interface{}
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) && !goja.IsNull(call.Argument(1)) {
			extra = call.Argument(1).Export()
		}
		if job.test != nil {
			job.test.addLog(msg, extra)
			return goja.Undefined()
		}
		if extra != nil {
			r.Logger.Infow("script log", "scriptID", job.ScriptID, "message", msg, "data", extra)
		} else {
			r.Logger.Infow("script log", "scriptID", job.ScriptID, "message", msg)
		}
		return goja.Undefined()
	})

	// info records a campaign_recipient_info event, visible in the campaign
	// timeline. Unlike log (server logs only) this is observable in the app; the
	// detail follows the campaign's data-retention and anonymity rules.
	vm.Set("info", func(call goja.FunctionCall) goja.Value {
		msg := call.Argument(0).String()
		// the optional second argument is extra structured data
		var extra map[string]interface{}
		if len(call.Arguments) > 1 {
			if m, ok := call.Argument(1).Export().(map[string]interface{}); ok {
				extra = m
			}
		}
		if job.test != nil {
			job.test.addInfo(msg, extra)
			return goja.Undefined()
		}
		if job.Emit == nil {
			return goja.Undefined()
		}
		payload := map[string]interface{}{
			"source":  "script",
			"level":   "info",
			"message": msg,
		}
		for k, v := range extra {
			payload[k] = v
		}
		if err := job.Emit(data.EVENT_CAMPAIGN_RECIPIENT_INFO, payload); err != nil {
			panic(vm.NewGoError(err))
		}
		return goja.Undefined()
	})

	vm.Set("http", map[string]interface{}{
		"fetch": r.makeFetch(vm, ctx, job),
	})

	// encode/decode/hash/hmac/jwt/random data transform toolkit
	registerCodec(vm)

	vm.Set("emitEvent", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		// a script may only author its own data events; it must not be able to
		// fabricate server-detected outcomes (opens, clicks, reports, delivery,
		// training) and skew a campaign's statistics.
		if !emittableEvents[name] {
			panic(vm.NewTypeError(fmt.Sprintf(
				"emitEvent: %q cannot be created by a script (allowed: %s, %s)",
				name,
				data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA,
				data.EVENT_CAMPAIGN_RECIPIENT_INFO,
			)))
		}
		var d map[string]interface{}
		if exp := call.Argument(1).Export(); exp != nil {
			if m, ok := exp.(map[string]interface{}); ok {
				d = m
			}
		}
		if job.test != nil {
			job.test.addEvent(name, d)
			return goja.Undefined()
		}
		if job.Emit == nil {
			panic(vm.NewTypeError("emitEvent is not available for this event"))
		}
		if err := job.Emit(name, d); err != nil {
			panic(vm.NewGoError(err))
		}
		return goja.Undefined()
	})
}

// makeFetch builds the synchronous http.fetch binding.
func (r *Runner) makeFetch(vm *goja.Runtime, ctx context.Context, job Job) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		urlStr := call.Argument(0).String()
		method := "GET"
		var bodyReader io.Reader
		headers := map[string]string{}
		fetchTimeout := defaultFetchTimeout
		proxyStr := ""

		if exp := call.Argument(1).Export(); exp != nil {
			if opts, ok := exp.(map[string]interface{}); ok {
				if v, ok := opts["method"].(string); ok && v != "" {
					method = strings.ToUpper(v)
				}
				if v, ok := opts["body"].(string); ok {
					bodyReader = strings.NewReader(v)
				}
				if v, ok := opts["proxy"].(string); ok {
					proxyStr = v
				}
				if h, ok := opts["headers"].(map[string]interface{}); ok {
					for k, val := range h {
						headers[k] = fmt.Sprint(val)
					}
				}
				if ms := coerceMillis(opts["timeoutMs"]); ms > 0 {
					fetchTimeout = time.Duration(ms) * time.Millisecond
					if fetchTimeout > maxFetchTimeout {
						fetchTimeout = maxFetchTimeout
					}
				}
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, method, urlStr, bodyReader)
		if err != nil {
			panic(vm.NewTypeError(err.Error()))
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		// route through a proxy for this request when the script asks for one
		client := r.HTTPClient
		if proxyStr != "" {
			pc, perr := proxyClient(proxyStr)
			if perr != nil {
				panic(vm.NewTypeError("http.fetch: " + perr.Error()))
			}
			client = pc
		}

		resp, err := client.Do(req)
		if err != nil {
			if job.test != nil {
				job.test.addFetchErr(method, urlStr, err.Error())
			}
			panic(vm.NewGoError(err))
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			if job.test != nil {
				job.test.addFetchErr(method, urlStr, err.Error())
			}
			panic(vm.NewGoError(err))
		}

		if job.test != nil {
			job.test.addFetchOK(method, urlStr, resp.StatusCode)
		}

		respHeaders := map[string]interface{}{}
		for k := range resp.Header {
			respHeaders[k] = resp.Header.Get(k)
		}

		return vm.ToValue(map[string]interface{}{
			"status":  resp.StatusCode,
			"headers": respHeaders,
			"body":    string(body),
		})
	}
}

// proxyClient builds an http.Client that routes a request through the given
// proxy. Supports http/https and socks5. Keep-alives are disabled so a per
// request proxy client does not accumulate idle connections.
func proxyClient(proxyStr string) (*http.Client, error) {
	u, err := url.Parse(proxyStr)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return &http.Client{
			Timeout: maxFetchTimeout,
			Transport: &http.Transport{
				Proxy:             http.ProxyURL(u),
				DisableKeepAlives: true,
			},
		}, nil
	case "socks5", "socks5h":
		dialer, err := proxy.FromURL(u, proxy.Direct)
		if err != nil {
			return nil, err
		}
		tr := &http.Transport{DisableKeepAlives: true}
		if cd, ok := dialer.(proxy.ContextDialer); ok {
			tr.DialContext = cd.DialContext
		} else {
			tr.DialContext = func(_ context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			}
		}
		return &http.Client{Timeout: maxFetchTimeout, Transport: tr}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (use http, https or socks5)", u.Scheme)
	}
}

// coerceMillis reads a JS number that goja may export as int64 or float64.
func coerceMillis(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return 0
	}
}

// Dispatcher runs jobs on a bounded worker pool.
type Dispatcher struct {
	jobs     chan Job
	runner   *Runner
	logger   *zap.SugaredLogger
	workers  int
	wg       sync.WaitGroup
	stopOnce sync.Once
	// mu guards closed so Enqueue never sends on a channel Stop has closed.
	mu     sync.RWMutex
	closed bool
}

// NewDispatcher builds a dispatcher. Zero values fall back to the defaults.
func NewDispatcher(
	logger *zap.SugaredLogger,
	workers int,
	queueSize int,
	timeout time.Duration,
) *Dispatcher {
	if workers <= 0 {
		workers = DefaultWorkers
	}
	if queueSize <= 0 {
		queueSize = DefaultQueueSize
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Dispatcher{
		jobs: make(chan Job, queueSize),
		runner: &Runner{
			Logger:     logger,
			HTTPClient: &http.Client{Timeout: maxFetchTimeout},
			Timeout:    timeout,
		},
		logger:  logger,
		workers: workers,
	}
}

// Start launches the worker goroutines.
func (d *Dispatcher) Start() {
	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			for job := range d.jobs {
				d.runOne(job)
			}
		}()
	}
}

// runOne isolates a single job. run() has its own recover, but the error
// reporting path runs inside that recover, so a panic there could still escape.
// This last line of defence guarantees one bad job can never crash the worker
// goroutine (and with it the process).
func (d *Dispatcher) runOne(job Job) {
	defer func() {
		if rec := recover(); rec != nil {
			d.logger.Errorw("script worker recovered from panic",
				"scriptID", job.ScriptID,
				"recover", fmt.Sprintf("%v", rec),
			)
		}
	}()
	d.runner.run(job)
}

// Enqueue submits a job. It returns false when the queue is full, in which case
// the job is dropped rather than blocking the caller on the event capture path.
func (d *Dispatcher) Enqueue(job Job) bool {
	// hold the read lock across the send so Stop cannot close the channel
	// between the closed check and the send
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.closed {
		return false
	}
	select {
	case d.jobs <- job:
		return true
	default:
		return false
	}
}

// Stop closes the queue and waits for in flight jobs to finish.
func (d *Dispatcher) Stop() {
	d.stopOnce.Do(func() {
		// take the write lock so no Enqueue is mid send when the channel closes
		d.mu.Lock()
		d.closed = true
		close(d.jobs)
		d.mu.Unlock()
	})
	d.wg.Wait()
}
