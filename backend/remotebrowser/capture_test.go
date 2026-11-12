package remotebrowser

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

func newTestSession(t *testing.T) *Session {
	t.Helper()
	bin := findChromeBin()
	if bin == "" {
		t.Skip("no chrome/chromium binary available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	s, err := NewSession(ctx, bin)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func testCaptureOptions() CaptureOptions {
	return CaptureOptions{Width: 400, Height: 300, MaxHeight: 1000, Timeout: 10 * time.Second}
}

func decodeJPEG(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("capture is not a JPEG: %v", err)
	}
	return img
}

// isRed reports whether the pixel at x, y is close to pure red. The pages in
// these tests are red only while they still show their own content. Callers
// sample away from the top left corner, where text is drawn.
func isRed(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return r>>8 > 200 && g>>8 < 60 && b>>8 < 60
}

const redBody = `<body style="margin:0;background:#ff0000">`

func TestCaptureRoutesThroughGuard(t *testing.T) {
	s := newTestSession(t)
	var hits int
	var mu sync.Mutex
	victim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	defer victim.Close()

	shot, err := s.Capture(`<html><body>Report<img src="`+victim.URL+`/ssrf"></body></html>`, testCaptureOptions())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	decodeJPEG(t, shot)
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatalf("loopback resource reached %d times through Capture; guard wiring missing", hits)
	}
	// positive control: the image request reached the guard and was refused there
	if s.proxy.blocked.Load() == 0 {
		t.Fatalf("the guard refused nothing, the request did not go through it")
	}
}

func TestSessionCapturesAndRendersPDFOnOneLaunch(t *testing.T) {
	s := newTestSession(t)
	shot, err := s.Capture(`<html><body>Email</body></html>`, testCaptureOptions())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	decodeJPEG(t, shot)
	pdf, err := s.RenderPDF(`<html><body>Report</body></html>`)
	if err != nil {
		t.Fatalf("render pdf: %v", err)
	}
	if len(pdf) == 0 {
		t.Fatalf("no PDF produced")
	}
}

func TestCaptureKeepsPageWhenItRedirects(t *testing.T) {
	s := newTestSession(t)
	pages := map[string]string{
		"script":  `<html>` + redBody + `<script>location.href="https://example.test/next"</script></body></html>`,
		"meta":    `<html><head><meta http-equiv="refresh" content="0;url=https://example.test/next"></head>` + redBody + `</body></html>`,
		"form":    `<html>` + redBody + `<form id="f" method="post" action="https://example.test/login"></form><script>document.getElementById("f").submit()</script></body></html>`,
		"delayed": `<html>` + redBody + `<script>setTimeout(function(){location.href="https://example.test/next"},200)</script></body></html>`,
	}
	for name, html := range pages {
		t.Run(name, func(t *testing.T) {
			shot, err := s.Capture(html, testCaptureOptions())
			if err != nil {
				t.Fatalf("capture: %v", err)
			}
			if !isRed(decodeJPEG(t, shot), 200, 200) {
				t.Fatalf("page navigated away before the capture")
			}
		})
	}
}

func TestCaptureDismissesDialogs(t *testing.T) {
	s := newTestSession(t)
	html := `<html>` + redBody + `<script>alert("a");confirm("b");prompt("c")</script></body></html>`
	shot, err := s.Capture(html, testCaptureOptions())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !isRed(decodeJPEG(t, shot), 200, 200) {
		t.Fatalf("unexpected capture content")
	}
}

func TestCaptureStopsStuckScriptAndSessionStillWorks(t *testing.T) {
	s := newTestSession(t)
	opts := testCaptureOptions()
	opts.Timeout = 3 * time.Second
	start := time.Now()
	_, err := s.Capture(`<html><body><script>for(;;){}</script></body></html>`, opts)
	if err == nil {
		t.Fatalf("expected a timeout error for a stuck script")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("stuck capture took %s, the timeout did not hold", took)
	}

	shot, err := s.Capture(`<html>`+redBody+`ok</body></html>`, testCaptureOptions())
	if err != nil {
		t.Fatalf("capture after a stuck page: %v", err)
	}
	if !isRed(decodeJPEG(t, shot), 200, 200) {
		t.Fatalf("unexpected capture content after a stuck page")
	}
	pdf, err := s.RenderPDF(`<html><body>Report</body></html>`)
	if err != nil || len(pdf) == 0 {
		t.Fatalf("render pdf after a stuck page: %v", err)
	}
}

func TestCaptureHeightIsCapped(t *testing.T) {
	s := newTestSession(t)
	opts := testCaptureOptions()
	shot, err := s.Capture(`<html><body style="margin:0"><div style="height:2000000px"></div></body></html>`, opts)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	b := decodeJPEG(t, shot).Bounds()
	if b.Dy() != opts.MaxHeight || b.Dx() != opts.Width {
		t.Fatalf("capture is %dx%d, want %dx%d", b.Dx(), b.Dy(), opts.Width, opts.MaxHeight)
	}

	shot, err = s.Capture(`<html><body style="margin:0"><div style="height:600px"></div></body></html>`, opts)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got := decodeJPEG(t, shot).Bounds().Dy(); got != 600 {
		t.Fatalf("capture height is %d, want the content height 600", got)
	}
}

func TestCaptureCanDisableScripts(t *testing.T) {
	s := newTestSession(t)
	html := `<html>` + redBody + `<script>document.body.style.background="#0000ff"</script></body></html>`
	opts := testCaptureOptions()
	opts.DisableScripts = true
	shot, err := s.Capture(html, opts)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !isRed(decodeJPEG(t, shot), 200, 200) {
		t.Fatalf("script ran although scripts were disabled")
	}

	opts.DisableScripts = false
	shot, err = s.Capture(html, opts)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if isRed(decodeJPEG(t, shot), 200, 200) {
		t.Fatalf("script did not run with scripts enabled")
	}
}

func TestCaptureKeepsPageWhenFrameOrPopupNavigates(t *testing.T) {
	s := newTestSession(t)
	pages := map[string]string{
		"iframe":   `<html>` + redBody + `<iframe src="https://example.test/frame" style="border:0;width:10px;height:10px"></iframe></body></html>`,
		"popup":    `<html>` + redBody + `<script>window.open("https://example.test/popup")</script></body></html>`,
		"blank":    `<html>` + redBody + `<a id="a" href="https://example.test/tab" target="_blank">x</a><script>document.getElementById("a").click()</script></body></html>`,
		"stuckpop": `<html>` + redBody + `<script>var w=window.open("about:blank");if(w){w.document.write("<script>for(;;){}<\/script>")}</script></body></html>`,
	}
	// the browser starts with one tab of its own, only pages opened by a
	// capture must be gone afterwards
	openPages := func() map[proto.TargetTargetID]string {
		targets, err := proto.TargetGetTargets{}.Call(s.browser)
		if err != nil {
			t.Fatalf("list targets: %v", err)
		}
		out := map[proto.TargetTargetID]string{}
		for _, ti := range targets.TargetInfos {
			if ti.Type == proto.TargetTargetInfoTypePage {
				out[ti.TargetID] = ti.URL
			}
		}
		return out
	}
	for name, html := range pages {
		t.Run(name, func(t *testing.T) {
			before := openPages()
			shot, err := s.Capture(html, testCaptureOptions())
			if err != nil {
				t.Fatalf("capture: %v", err)
			}
			if !isRed(decodeJPEG(t, shot), 200, 200) {
				t.Fatalf("page changed before the capture")
			}
			for id, u := range openPages() {
				if _, ok := before[id]; !ok {
					t.Fatalf("a page is still open after the capture: %s", u)
				}
			}
		})
	}
}

func TestCaptureKeepsWindowHeightLayout(t *testing.T) {
	s := newTestSession(t)
	html := `<html><body style="margin:0">` +
		`<div style="height:100px;background:#ff0000"></div>` +
		`<div style="min-height:100vh;background:#ff0000"></div>` +
		`<div style="height:100px;background:#0000ff"></div></body></html>`
	opts := testCaptureOptions()
	shot, err := s.Capture(html, opts)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	img := decodeJPEG(t, shot)
	want := 100 + opts.Height + 100
	if got := img.Bounds().Dy(); got != want {
		t.Fatalf("capture height is %d, want %d", got, want)
	}
	r, g, b, _ := img.At(200, want-10).RGBA()
	if !(b>>8 > 200 && r>>8 < 60 && g>>8 < 60) {
		t.Fatalf("the footer is missing from the capture")
	}
}

func TestCaptureRejectsMissingOptions(t *testing.T) {
	s := newTestSession(t)
	for _, opts := range []CaptureOptions{
		{Height: 300, MaxHeight: 1000, Timeout: time.Second},
		{Width: 400, MaxHeight: 1000, Timeout: time.Second},
		{Width: 400, Height: 300, Timeout: time.Second},
		{Width: 400, Height: 300, MaxHeight: 1000},
	} {
		if _, err := s.Capture(`<html></html>`, opts); err == nil {
			t.Fatalf("options %+v were accepted", opts)
		}
	}
}
