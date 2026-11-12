package service

import (
	"context"

	"github.com/phishingclub/phishingclub/remotebrowser"
	"go.uber.org/zap"
)

// reportBrowser launches the report browser on first use and shares it
// between the screenshots of a report and its PDF, so a report needs one
// browser launch. Callers must call close.
type reportBrowser struct {
	ctx      context.Context
	execPath string
	logger   *zap.SugaredLogger
	session  *remotebrowser.Session
	// launchErr is the error of a failed launch. Screenshots do not try again
	// after it, the PDF does.
	launchErr error
}

func newReportBrowser(ctx context.Context, execPath string, logger *zap.SugaredLogger) *reportBrowser {
	return &reportBrowser{ctx: ctx, execPath: execPath, logger: logger}
}

// forScreenshots returns the browser, or nil when it could not be launched.
func (b *reportBrowser) forScreenshots() *remotebrowser.Session {
	if b.session != nil || b.launchErr != nil {
		return b.session
	}
	if err := b.launch(); err != nil {
		b.logger.Warnw("failed to start browser for report screenshots", "error", err)
		return nil
	}
	return b.session
}

// renderPDF renders htmlContent to PDF, launching the browser when it is not
// running yet. When the browser the screenshots used fails, the PDF is
// rendered once more on a new one, so a screenshot can not fail the report.
func (b *reportBrowser) renderPDF(htmlContent string) ([]byte, error) {
	reused := b.session != nil
	if !reused {
		if err := b.launch(); err != nil {
			return nil, err
		}
	}
	pdf, err := b.session.RenderPDF(htmlContent)
	if err == nil || !reused || b.ctx.Err() != nil {
		return pdf, err
	}
	b.logger.Warnw("report PDF failed on the screenshot browser, trying a new one", "error", err)
	b.close()
	if err := b.launch(); err != nil {
		return nil, err
	}
	return b.session.RenderPDF(htmlContent)
}

func (b *reportBrowser) launch() error {
	s, err := remotebrowser.NewSession(b.ctx, b.execPath)
	if err != nil {
		b.launchErr = err
		return err
	}
	b.session = s
	b.launchErr = nil
	return nil
}

// close stops the browser. It is safe to call more than once.
func (b *reportBrowser) close() {
	if b.session != nil {
		b.session.Close()
		b.session = nil
	}
}
