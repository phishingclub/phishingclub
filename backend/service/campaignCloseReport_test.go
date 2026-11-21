package service

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TestCloseReportRetriesOnceThenAudits checks a close report that finds no
// report browser tries once more later, and when that fails too it is
// dropped with one failed audit event by the system, while repeated drops
// are held back
func TestCloseReportRetriesOnceThenAudits(t *testing.T) {
	prevReserve, prevRetry, prevWait := closeReportReserve, closeReportRetryAfter, closeReportBrowserWait
	prevNeeds := closeReportNeedsBrowser
	t.Cleanup(func() {
		closeReportReserve, closeReportRetryAfter, closeReportBrowserWait = prevReserve, prevRetry, prevWait
		closeReportNeedsBrowser = prevNeeds
	})
	closeReportNeedsBrowser = func(*Campaign, uuid.UUID) bool { return true }
	var tries atomic.Int32
	closeReportReserve = func(ctx context.Context) (context.Context, func(), error) {
		tries.Add(1)
		return nil, nil, errors.New("no free report browser")
	}
	closeReportRetryAfter = 10 * time.Millisecond
	closeReportBrowserWait = time.Second
	// the retry writes the log on its own goroutine while the test reads it
	buf := &lockedBuffer{}
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(buf),
		zapcore.InfoLevel,
	)).Sugar()
	c := &Campaign{Common: Common{Logger: zap.NewNop().Sugar(), AuditLogger: logger}}

	campaignID := uuid.New()
	c.sendCloseReport(nil, campaignID, false)
	if tries.Load() != 1 || c.closeReports.retries.Load() != 1 {
		t.Fatalf("expected one try and one retry waiting: %d %d", tries.Load(), c.closeReports.retries.Load())
	}
	if len(step4Events(t, buf.String(), "Campaign.SendCloseReport")) != 0 {
		t.Fatal("a report that tries again is not dropped yet")
	}
	deadline := time.Now().Add(5 * time.Second)
	for c.closeReports.retries.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the retry did not run")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if tries.Load() != 2 {
		t.Fatalf("expected exactly one retry, got %d tries", tries.Load())
	}
	ev := step4Events(t, buf.String(), "Campaign.SendCloseReport")
	if len(ev) != 1 || ev[0]["result"] != "failed" {
		t.Fatalf("expected one failed event, got %v", ev)
	}
	d := eventDetails(t, ev[0])
	if d["actor"] != "system" || d["campaignId"] != campaignID.String() {
		t.Fatalf("drop event details: %v", d)
	}

	// with the retries full a report is dropped at once, the event is held back
	c.closeReports.retries.Store(closeReportMaxRetries)
	c.sendCloseReport(nil, uuid.New(), false)
	if tries.Load() != 3 || c.closeReports.retries.Load() != closeReportMaxRetries {
		t.Fatalf("expected a drop without a retry: %d %d", tries.Load(), c.closeReports.retries.Load())
	}
	if ev := step4Events(t, buf.String(), "Campaign.SendCloseReport"); len(ev) != 1 {
		t.Fatalf("a repeated drop must be held back, got %d events", len(ev))
	}
}

// lockedBuffer is a log buffer that may be written and read at once
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
