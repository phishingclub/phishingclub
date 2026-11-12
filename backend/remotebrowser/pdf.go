package remotebrowser

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/phishingclub/phishingclub/errs"
)

// WipeBrowserCache removes the automatically downloaded Chromium directory.
// The next report browser launch will trigger a fresh download.
func WipeBrowserCache() error {
	dir, err := resolveBrowserRootDir()
	if err != nil {
		return fmt.Errorf("reportpdf: %w", err)
	}
	return os.RemoveAll(dir)
}

// applyGuardFlags points the launcher at the egress guard and removes the
// browser's own ways to leave the box around it: it routes all requests through
// the proxy, removes the implicit loopback and link local proxy bypass (without
// which 127.0.0.1 and 169.254.169.254 are reached directly), blocks the WebRTC
// and QUIC UDP paths, keeps popups blocked, and disables the browser's
// background network calls. It is
// the single definition used by both the renderer and its tests so the guard
// configuration cannot silently drift out of the real render path.
func applyGuardFlags(l *launcher.Launcher, proxyAddr string) *launcher.Launcher {
	// the launcher turns popup blocking off by default, popups would escape a
	// capture's request handling
	l.Delete("disable-popup-blocking")
	return l.
		Set("proxy-server", proxyAddr).
		Set("proxy-bypass-list", "<-loopback>").
		Set("disable-background-networking").
		Set("disable-component-update").
		Set("disable-domain-reliability").
		Set("disable-sync").
		Set("disable-client-side-phishing-detection").
		Set("safebrowsing-disable-auto-update").
		Set("no-pings").
		Set("no-first-run").
		Set("no-default-browser-check").
		Set("force-webrtc-ip-handling-policy", "disable_non_proxied_udp").
		Set("disable-quic")
}

// Session is one launched, guarded headless Chromium instance that can take
// several screenshots and render several PDFs on one browser launch. Callers
// must call Close to release the browser process, its temp dirs and the egress
// guard proxy.
type Session struct {
	browser *rod.Browser
	proxy   *guardedProxy
	l       *launcher.Launcher
	// captureBlocked and captureRefused add up what the egress guards of
	// this session's captures blocked and refused
	captureBlocked atomic.Int64
	captureRefused atomic.Int64
	// localReads holds a slot for each local file being read
	localReads chan struct{}
}

// NewSession launches a headless Chromium instance behind the egress guard.
// If execPath is empty the browser binary is resolved automatically using the
// same path as the runner. This is the single launch path shared by every
// report render (PDF or screenshot) so the guard configuration cannot drift between
// them.
func NewSession(ctx context.Context, execPath string) (*Session, error) {
	rootDir, err := resolveBrowserRootDir()
	if err != nil {
		return nil, fmt.Errorf("reportbrowser: %w", err)
	}

	crashDir := filepath.Join(rootDir, "crashes")
	_ = os.MkdirAll(crashDir, 0755)
	_ = os.MkdirAll(filepath.Join(rootDir, "config"), 0755)
	_ = os.MkdirAll(filepath.Join(rootDir, "cache"), 0755)

	// force browser egress through the loopback guard so an embedded report
	// resource cannot be used for request forgery (see guardedProxy).
	proxy, err := startGuardedProxy()
	if err != nil {
		return nil, fmt.Errorf("reportbrowser: egress guard: %w", err)
	}

	l := launcher.New().
		Headless(true).
		Set("disable-crash-reporter").
		Set("crash-dumps-dir", crashDir).
		Env(chromeEnv(
			"XDG_CONFIG_HOME="+filepath.Join(rootDir, "config"),
			"XDG_CACHE_HOME="+filepath.Join(rootDir, "cache"),
		)...)
	applyGuardFlags(l, proxy.addr())

	if execPath != "" {
		l = l.Bin(execPath)
	} else {
		b := launcher.NewBrowser()
		b.RootDir = rootDir
		binPath := b.BinPath()
		if _, err := os.Stat(binPath); os.IsNotExist(err) {
			if err := b.Download(); err != nil {
				proxy.close()
				return nil, errs.NewOperationalError(
					"The report browser could not be downloaded. Check the server logs for details.",
					err,
				)
			}
		}
		l = l.Bin(binPath)
	}

	u, err := l.Launch()
	if err != nil {
		// Kill does nothing when no process started; Cleanup is skipped here because
		// on an early launch failure it blocks forever waiting on the exit channel.
		// Kill still removes a process that did start.
		l.Kill()
		proxy.close()
		return nil, errs.NewOperationalError(
			"The report browser failed to start. Check the server logs for details.",
			err,
		)
	}

	browser := rod.New().ControlURL(u).Context(ctx)
	if err := browser.Connect(); err != nil {
		l.Kill()
		l.Cleanup()
		proxy.close()
		return nil, errs.NewOperationalError(
			"The report browser failed to start. Check the server logs for details.",
			err,
		)
	}

	return &Session{browser: browser, proxy: proxy, l: l, localReads: make(chan struct{}, sessionLocalReads)}, nil
}

// Close releases the browser process, its temp dirs and the egress guard
// proxy. Safe to call once per Session, after which the Session must not be
// used again.
func (s *Session) Close() {
	_ = s.browser.Close() //nolint:errcheck
	s.l.Kill()
	s.l.Cleanup()
	s.proxy.close()
}

// RenderPDF renders an HTML string to PDF bytes on this session's browser.
func (s *Session) RenderPDF(htmlContent string) ([]byte, error) {
	page, err := s.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("reportpdf: page create failed: %w", err)
	}
	defer page.Close() //nolint:errcheck

	// Set viewport to A4 at 96 dpi (794×1123 px) so layout fills exactly the paper width.
	// Without this Chrome defaults to a wider viewport and the content may not reach the
	// right edge of the paper, producing a white strip at certain zoom levels.
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             794,
		Height:            1123,
		DeviceScaleFactor: 1,
		Mobile:            false,
	}); err != nil {
		return nil, fmt.Errorf("reportpdf: set viewport failed: %w", err)
	}

	if err := page.SetDocumentContent(htmlContent); err != nil {
		return nil, fmt.Errorf("reportpdf: set content failed: %w", err)
	}

	// not fatal: lets inline resources settle before printing
	_ = page.WaitIdle(3 * time.Second)

	a4Width := 8.27
	a4Height := 11.69
	zero := 0.0
	pdfReader, err := page.PDF(&proto.PagePrintToPDF{
		PrintBackground: true,
		PaperWidth:      &a4Width,
		PaperHeight:     &a4Height,
		MarginTop:       &zero,
		MarginBottom:    &zero,
		MarginLeft:      &zero,
		MarginRight:     &zero,
	})
	if err != nil {
		return nil, fmt.Errorf("reportpdf: PDF render failed: %w", err)
	}
	defer pdfReader.Close() //nolint:errcheck

	data, err := io.ReadAll(pdfReader)
	if err != nil {
		return nil, fmt.Errorf("reportpdf: read PDF failed: %w", err)
	}
	return data, nil
}

// CaptureOptions controls one screenshot taken by Session.Capture.
type CaptureOptions struct {
	// Width and Height set the starting viewport in CSS pixels.
	Width  int
	Height int
	// MaxHeight is the tallest capture in CSS pixels. Content below it is cut off.
	MaxHeight int
	// DisableScripts turns JavaScript off for the page.
	DisableScripts bool
	// Timeout bounds the whole capture.
	Timeout time.Duration
	// Local, when set, is asked about every request that is not a document.
	// When it reports the request as local, the file it returns is the
	// response, or 404 when it returns no file, and the request never reaches
	// the network.
	Local func(u *url.URL) (file *LocalFile, local bool)
	// LocalHost, when set, names the hosts Local answers for. The egress
	// guard refuses them, so a request the browser makes without asking the
	// Fetch handler, such as a prefetch, still never reaches them.
	LocalHost func(host string) bool
}

// LocalFile is a response a capture gives without using the network.
type LocalFile struct {
	Body        []byte
	ContentType string
}

// captureJPEGQuality keeps report screenshots readable at a small size.
const captureJPEGQuality = 85

// captureLocalBudget is the most bytes of local files one capture is given,
// later local requests get 404.
const captureLocalBudget = 64 << 20

// sessionLocalReads is the most local files the captures of one session read
// and hand to the browser at the same time, however many captures run.
const sessionLocalReads = 4

// CaptureCloseTimeout is the longest Capture takes after its Timeout to close
// its browser context.
const CaptureCloseTimeout = 2 * time.Second

// captureSettleTimeout is the longest wait for a page to finish loading.
const captureSettleTimeout = 5 * time.Second

// Capture renders htmlContent in a new tab and returns a JPEG screenshot of it.
// Several captures can run on one Session at the same time.
// The page can not leave the given content: every document request, from a
// redirect, a form post or a frame, is answered with 204 No Content, which
// makes the browser keep the current document. JavaScript dialogs are
// dismissed and WebSockets are blocked. Requests opts.Local reports as local
// are answered from Go, up to captureLocalBudget bytes in all. All other
// requests go through an egress guard of the capture's own, which also
// refuses the hosts opts.LocalHost names.
func (s *Session) Capture(htmlContent string, opts CaptureOptions) ([]byte, error) {
	if opts.Width <= 0 || opts.Height <= 0 || opts.MaxHeight <= 0 || opts.Timeout <= 0 {
		return nil, fmt.Errorf("reportcapture: width, height, max height and timeout must be above zero")
	}
	ctx, cancel := context.WithTimeout(s.browser.GetContext(), opts.Timeout)
	defer cancel()

	proxy, err := startGuardedProxyRefusing(opts.LocalHost)
	if err != nil {
		return nil, fmt.Errorf("reportcapture: egress guard start failed: %w", err)
	}
	// closed after the browser context below is disposed
	defer func() {
		proxy.close()
		s.captureBlocked.Add(proxy.blocked.Load())
		s.captureRefused.Add(proxy.refused.Load())
	}()

	// each capture gets its own browser context with the egress guard set on
	// it, so cookies and storage do not carry over and any popup the page
	// manages to open is closed with it. It is disposed through the browser
	// process, which works even when the page renderer is stuck in a script.
	bc, err := proto.TargetCreateBrowserContext{
		ProxyServer:     proxy.addr(),
		ProxyBypassList: "<-loopback>",
	}.Call(s.browser.Context(ctx))
	if err != nil {
		return nil, fmt.Errorf("reportcapture: browser context create failed: %w", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), CaptureCloseTimeout)
		defer closeCancel()
		_ = proto.TargetDisposeBrowserContext{BrowserContextID: bc.BrowserContextID}.Call(s.browser.Context(closeCtx))
	}()

	// created directly, rod's Browser.Page would put it in the default context
	target, err := proto.TargetCreateTarget{
		URL:              "about:blank",
		BrowserContextID: bc.BrowserContextID,
	}.Call(s.browser.Context(ctx))
	if err != nil {
		return nil, fmt.Errorf("reportcapture: page create failed: %w", err)
	}
	tab, err := s.browser.Context(ctx).PageFromTarget(target.TargetID)
	if err != nil {
		return nil, fmt.Errorf("reportcapture: page attach failed: %w", err)
	}

	var localBytes atomic.Int64

	// subscribing enables the Fetch and Page domains before any content loads,
	// so no request or dialog is missed
	waitEvents := tab.EachEvent(
		func(e *proto.FetchRequestPaused) {
			go func() {
				if e.ResourceType == proto.NetworkResourceTypeDocument {
					// an empty body must be set, a nil one is sent as null and refused
					_ = proto.FetchFulfillRequest{RequestID: e.RequestID, ResponseCode: 204, Body: []byte{}}.Call(tab)
					return
				}
				if opts.Local != nil {
					if u, err := url.Parse(e.Request.URL); err == nil {
						// once the budget is spent a local host gets 404 without a read
						if opts.LocalHost != nil && localBytes.Load() >= captureLocalBudget && opts.LocalHost(u.Hostname()) {
							_ = fulfillLocal(tab, e.RequestID, nil)
							return
						}
						if s.serveLocal(tab, e.RequestID, u, opts.Local, &localBytes) {
							return
						}
					}
				}
				_ = proto.FetchContinueRequest{RequestID: e.RequestID}.Call(tab)
			}()
		},
		func(e *proto.PageJavascriptDialogOpening) {
			go func() {
				_ = proto.PageHandleJavaScriptDialog{Accept: false}.Call(tab)
			}()
		},
	)
	go waitEvents()

	// WebSockets are not seen by the Fetch handler above, so they are blocked
	if err := (proto.NetworkEnable{}).Call(tab); err != nil {
		return nil, fmt.Errorf("reportcapture: network enable failed: %w", err)
	}
	if err := (proto.NetworkSetBlockedURLs{Urls: []string{"ws://*", "wss://*"}}).Call(tab); err != nil {
		return nil, fmt.Errorf("reportcapture: block websockets failed: %w", err)
	}

	if opts.DisableScripts {
		if err := (proto.EmulationSetScriptExecutionDisabled{Value: true}).Call(tab); err != nil {
			return nil, fmt.Errorf("reportcapture: disable scripts failed: %w", err)
		}
	}

	if err := tab.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             opts.Width,
		Height:            opts.Height,
		DeviceScaleFactor: 1,
		Mobile:            false,
	}); err != nil {
		return nil, fmt.Errorf("reportcapture: set viewport failed: %w", err)
	}

	// wait until the page has made no request for a short while, images and
	// fonts included, but never longer than captureSettleTimeout. This watches
	// the network only, so it also works with scripts off or a blocked redirect.
	settle := tab.Timeout(captureSettleTimeout).WaitRequestIdle(
		300*time.Millisecond,
		nil,
		nil,
		[]proto.NetworkResourceType{
			proto.NetworkResourceTypeWebSocket,
			proto.NetworkResourceTypeEventSource,
			proto.NetworkResourceTypeMedia,
		},
	)
	if err := tab.SetDocumentContent(htmlContent); err != nil {
		return nil, fmt.Errorf("reportcapture: set content failed: %w", err)
	}
	settle()

	// capture the content up to MaxHeight. The viewport is kept as it is, so
	// a layout sized on the window height looks as it does on screen.
	metrics, err := proto.PageGetLayoutMetrics{}.Call(tab)
	if err != nil {
		return nil, fmt.Errorf("reportcapture: layout metrics failed: %w", err)
	}
	height := opts.Height
	if metrics.CSSContentSize != nil {
		height = int(math.Ceil(metrics.CSSContentSize.Height))
	}
	height = max(1, min(height, opts.MaxHeight))

	quality := captureJPEGQuality
	shot, err := proto.PageCaptureScreenshot{
		Format:                proto.PageCaptureScreenshotFormatJpeg,
		Quality:               &quality,
		CaptureBeyondViewport: true,
		Clip: &proto.PageViewport{
			Width:  float64(opts.Width),
			Height: float64(height),
			Scale:  1,
		},
	}.Call(tab)
	if err != nil {
		return nil, fmt.Errorf("reportcapture: screenshot failed: %w", err)
	}
	return shot.Data, nil
}

// serveLocal answers a paused request from local when local reports its URL
// as local, and tells whether it did. A session slot is held from the read
// until the file is handed to the browser, so the captures of a session hold
// at most sessionLocalReads local files in memory at once. localBytes counts
// the bytes of the files the capture was given, a file that takes it past
// captureLocalBudget gets 404.
func (s *Session) serveLocal(
	tab *rod.Page,
	id proto.FetchRequestID,
	u *url.URL,
	local func(u *url.URL) (*LocalFile, bool),
	localBytes *atomic.Int64,
) bool {
	s.localReads <- struct{}{}
	defer func() { <-s.localReads }()
	file, isLocal := local(u)
	if !isLocal {
		return false
	}
	if file != nil && localBytes.Add(int64(len(file.Body))) > captureLocalBudget {
		file = nil
	}
	_ = fulfillLocal(tab, id, file)
	return true
}

// fulfillLocal answers a paused request with file, or with 404 when file is nil.
func fulfillLocal(tab *rod.Page, id proto.FetchRequestID, file *LocalFile) error {
	if file == nil {
		return proto.FetchFulfillRequest{RequestID: id, ResponseCode: 404, Body: []byte{}}.Call(tab)
	}
	body := file.Body
	if body == nil {
		body = []byte{}
	}
	return proto.FetchFulfillRequest{
		RequestID:    id,
		ResponseCode: 200,
		ResponseHeaders: []*proto.FetchHeaderEntry{
			{Name: "Content-Type", Value: file.ContentType},
		},
		Body: body,
	}.Call(tab)
}
