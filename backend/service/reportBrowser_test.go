package service

import (
	"context"
	"os"
	"testing"

	"go.uber.org/zap"
)

func TestReportBrowserFailedLaunch(t *testing.T) {
	rb := newReportBrowser(context.Background(), "/nonexistent/chrome", zap.NewNop().Sugar())
	defer rb.close()

	if s := rb.forScreenshots(); s != nil {
		t.Fatal("expected no browser")
	}
	firstErr := rb.launchErr
	if firstErr == nil {
		t.Fatal("expected the launch error to be kept")
	}
	// screenshots do not try to launch again
	if s := rb.forScreenshots(); s != nil || rb.launchErr != firstErr {
		t.Fatal("screenshots launched the browser again after a failure")
	}
	// the PDF tries again and reports the error
	if _, err := rb.renderPDF("<html></html>"); err == nil {
		t.Fatal("expected the PDF render to fail")
	}
	if rb.launchErr == firstErr {
		t.Fatal("the PDF did not try to launch again")
	}
}

func TestReportBrowserSharesOneLaunch(t *testing.T) {
	bin := os.Getenv("TEST_CHROME_BIN")
	if bin == "" {
		t.Skip("TEST_CHROME_BIN not set")
	}
	rb := newReportBrowser(context.Background(), bin, zap.NewNop().Sugar())
	defer rb.close()
	first := rb.forScreenshots()
	if first == nil {
		t.Fatalf("launch failed: %v", rb.launchErr)
	}
	if rb.forScreenshots() != first {
		t.Fatal("screenshots launched a second browser")
	}
	if _, err := rb.renderPDF("<html><body>report</body></html>"); err != nil {
		t.Fatalf("render pdf: %v", err)
	}
	if rb.session != first {
		t.Fatal("the PDF did not use the screenshot browser")
	}
	rb.close()
	rb.close()
}

func TestReportBrowserCloseWithoutLaunch(t *testing.T) {
	rb := newReportBrowser(context.Background(), "", zap.NewNop().Sugar())
	rb.close()
	rb.close()
}
