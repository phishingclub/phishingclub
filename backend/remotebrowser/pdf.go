package remotebrowser

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// WipeBrowserCache removes the auto-downloaded Chromium directory.
// The next call to RenderHTMLToPDF will trigger a fresh download.
func WipeBrowserCache() error {
	dir, err := resolveBrowserRootDir()
	if err != nil {
		return fmt.Errorf("reportpdf: %w", err)
	}
	return os.RemoveAll(dir)
}

// RenderHTMLToPDF renders an HTML string to PDF bytes using a headless Chromium instance.
// If execPath is empty the browser binary is auto-resolved using the same path as the runner.
func RenderHTMLToPDF(ctx context.Context, htmlContent string, execPath string) ([]byte, error) {
	rootDir, err := resolveBrowserRootDir()
	if err != nil {
		return nil, fmt.Errorf("reportpdf: %w", err)
	}

	crashDir := filepath.Join(rootDir, "crashes")
	_ = os.MkdirAll(crashDir, 0755)
	_ = os.MkdirAll(filepath.Join(rootDir, "config"), 0755)
	_ = os.MkdirAll(filepath.Join(rootDir, "cache"), 0755)

	l := launcher.New().
		Headless(true).
		Set("disable-crash-reporter").
		Set("crash-dumps-dir", crashDir).
		Env(chromeEnv(
			"XDG_CONFIG_HOME="+filepath.Join(rootDir, "config"),
			"XDG_CACHE_HOME="+filepath.Join(rootDir, "cache"),
		)...)

	if execPath != "" {
		l = l.Bin(execPath)
	} else {
		b := launcher.NewBrowser()
		b.RootDir = rootDir
		binPath := b.BinPath()
		if _, err := os.Stat(binPath); os.IsNotExist(err) {
			if err := b.Download(); err != nil {
				return nil, fmt.Errorf("reportpdf: browser download failed: %w", err)
			}
		}
		l = l.Bin(binPath)
	}

	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("reportpdf: browser launch failed: %w", err)
	}
	defer func() { l.Kill(); l.Cleanup() }()

	browser := rod.New().ControlURL(u).Context(ctx)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("reportpdf: browser connect failed: %w", err)
	}
	defer browser.Close() //nolint:errcheck

	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("reportpdf: page create failed: %w", err)
	}

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

	// non-fatal: lets inline resources settle before printing
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
