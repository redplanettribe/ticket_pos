package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// errDial stands in for the dial timeout the Cloud Run migrate Job hit while its
// VPC network interface was still coming up.
var errDial = errors.New("dial error: timeout")

func TestRetryPingSucceedsAfterTransientFailures(t *testing.T) {
	attempts := 0
	ping := func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errDial
		}
		return nil
	}

	if err := retryPing(context.Background(), ping, time.Second, time.Millisecond); err != nil {
		t.Fatalf("retryPing: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetryPingGivesUpAfterWindowAndReportsLastError(t *testing.T) {
	attempts := 0
	ping := func(context.Context) error {
		attempts++
		return errDial
	}

	err := retryPing(context.Background(), ping, 50*time.Millisecond, 10*time.Millisecond)
	if !errors.Is(err, errDial) {
		t.Fatalf("err = %v, want it to wrap errDial", err)
	}
	if !strings.Contains(err.Error(), "attempts over") {
		t.Fatalf("err = %v, want the attempt count in the message", err)
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want more than one before giving up", attempts)
	}
}

func TestRetryPingStopsWhenCallerContextIsDone(t *testing.T) {
	ping := func(context.Context) error { return errDial }

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	// The window is far longer than the caller's deadline: the caller's deadline
	// must win, so a migrate Job never hangs past its own timeout.
	err := retryPing(ctx, ping, time.Minute, 5*time.Millisecond)
	if !errors.Is(err, errDial) {
		t.Fatalf("err = %v, want it to wrap errDial", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("returned after %s, want it to stop at the caller's deadline", elapsed)
	}
}
