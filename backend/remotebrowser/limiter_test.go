package remotebrowser

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/phishingclub/phishingclub/errs"
)

// TestBrowserLimiterCapsAndQueues checks that only the set number of slots are
// handed out and that a waiter gets a slot as soon as one is given back.
func TestBrowserLimiterCapsAndQueues(t *testing.T) {
	b := newBrowserLimiter(2)
	ctx := context.Background()
	r1, err := b.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := b.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan func(), 1)
	go func() {
		r, err := b.acquire(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		got <- r
	}()
	select {
	case <-got:
		t.Fatal("a third slot was handed out while two were taken")
	case <-time.After(50 * time.Millisecond):
	}
	r1()
	// giving a slot back twice must not free a second slot
	r1()
	select {
	case r3 := <-got:
		r3()
	case <-time.After(2 * time.Second):
		t.Fatal("the waiter did not get the freed slot")
	}
	if len(b.slots) != 1 {
		t.Fatalf("slots in use = %d, want 1", len(b.slots))
	}
	r2()
	if len(b.slots) != 0 {
		t.Fatalf("slots in use = %d, want 0", len(b.slots))
	}
}

// TestBrowserLimiterContext checks that a waiter gives up when its context
// times out or is cancelled, with an error the controllers show to the user.
func TestBrowserLimiterContext(t *testing.T) {
	b := newBrowserLimiter(1)
	release, err := b.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = b.acquire(ctx)
	var opErr errs.OperationalError
	if !errors.As(err, &opErr) {
		t.Fatalf("timeout error = %v, want an OperationalError", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v, want it to wrap the deadline", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("the waiter did not give up at its deadline")
	}

	cctx, ccancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := b.acquire(cctx)
		done <- err
	}()
	ccancel()
	select {
	case err := <-done:
		if !errors.As(err, &opErr) || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error = %v, want a cancelled OperationalError", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the waiter did not give up when cancelled")
	}
	if len(b.slots) != 1 {
		t.Fatalf("slots in use = %d, want 1", len(b.slots))
	}
}

// TestBrowserLimiterReserve checks that a reserved slot is used by the session
// started with its context, so the session does not wait for a second slot,
// and that the time limit set after the reservation does not count the wait.
func TestBrowserLimiterReserve(t *testing.T) {
	b := newBrowserLimiter(1)
	other, err := b.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	type reserved struct {
		ctx     context.Context
		release func()
	}
	got := make(chan reserved, 1)
	go func() {
		ctx, release, err := b.reserve(context.Background(), false)
		if err != nil {
			t.Error(err)
			return
		}
		got <- reserved{ctx, release}
	}()
	// the reservation waits in line behind the slot in use
	time.Sleep(50 * time.Millisecond)
	other()
	var r reserved
	select {
	case r = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the reservation did not get the freed slot")
	}

	// a short time limit made after the reservation still gets the slot at once
	ctx, cancel := context.WithTimeout(r.ctx, 50*time.Millisecond)
	defer cancel()
	inner, err := b.acquire(ctx)
	if err != nil {
		t.Fatalf("session on a reserved slot: %v", err)
	}
	// the reserved slot is in use, a second session waits and times out
	if _, err := b.acquire(ctx); err == nil {
		t.Fatal("a second session took the reserved slot that was in use")
	}
	inner()
	// after the session closes the reserved slot can be used again
	again, err := b.acquire(r.ctx)
	if err != nil {
		t.Fatalf("reserved slot after close: %v", err)
	}
	again()
	if len(b.slots) != 1 {
		t.Fatalf("slots in use = %d, want 1", len(b.slots))
	}
	r.release()
	if len(b.slots) != 0 {
		t.Fatalf("slots in use = %d, want 0", len(b.slots))
	}
}

// TestBrowserLimiterReserveReleased checks that a reservation can not be used
// after it is given back: a session started with its context waits for a new
// slot instead of running without one
func TestBrowserLimiterReserveReleased(t *testing.T) {
	b := newBrowserLimiter(1)
	ctx, release, err := b.reserve(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if len(b.slots) != 0 {
		t.Fatalf("slots in use = %d, want 0", len(b.slots))
	}
	// the context still carries the reservation, it must take a real slot
	inner, err := b.acquire(ctx)
	if err != nil {
		t.Fatalf("session after release: %v", err)
	}
	if len(b.slots) != 1 {
		t.Fatalf("slots in use = %d, want 1, a released reservation was reused", len(b.slots))
	}
	// with that slot taken a second session waits and times out
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := b.acquire(short); err == nil {
		t.Fatal("a released reservation handed out a slot")
	}
	inner()
	// a second release does nothing
	release()
	if len(b.slots) != 0 {
		t.Fatalf("slots in use = %d, want 0", len(b.slots))
	}
}

// TestBrowserLimiterReserveOneSession checks that a one session reservation
// gives its slot back when the session on it closes, before release
func TestBrowserLimiterReserveOneSession(t *testing.T) {
	b := newBrowserLimiter(1)
	ctx, release, err := b.reserve(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	inner, err := b.acquire(ctx)
	if err != nil {
		t.Fatalf("session on the reservation: %v", err)
	}
	if len(b.slots) != 1 {
		t.Fatalf("slots in use = %d, want 1", len(b.slots))
	}
	inner()
	if len(b.slots) != 0 {
		t.Fatalf("slots in use after the session closed = %d, want 0", len(b.slots))
	}
	// another caller gets the slot while the reserving work goes on
	other, err := b.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// the closing release does not free the slot the other caller holds
	release()
	if len(b.slots) != 1 {
		t.Fatalf("slots in use = %d, want 1", len(b.slots))
	}
	other()
}
