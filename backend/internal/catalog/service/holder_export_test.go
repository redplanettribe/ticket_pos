package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestTheHolderExportDeadlineIsInsideTheRequestTimeout holds the Holder Export's
// own deadline strictly inside Cloud Run's request timeout (ADR 0075).
//
// With no cap, the timeout is the only ceiling a streaming export has. Expiring
// first is what lets an export that would run past it end as an aborted download
// with a reason this process can log - and give its database connection back -
// instead of being cut off by the platform in a way indistinguishable from a
// client that went away. The margin is for the last batch in flight when the
// deadline passes.
func TestTheHolderExportDeadlineIsInsideTheRequestTimeout(t *testing.T) {
	requestTimeout := terraformSecondsVariable(t,
		filepath.Join(holderPurgeModuleDir(), "variables.tf"), "api_request_timeout_seconds")

	if margin := requestTimeout - holderExportDeadline; margin < 5*time.Second {
		t.Fatalf("holderExportDeadline = %v, Cloud Run's request timeout = %v: the export must expire at least 5s first, or the platform cuts it off before it can say why",
			holderExportDeadline, requestTimeout)
	}
}

// An aborted export's "finished" line says why: the deadline before its
// symptoms, then a client that went away, and only then the database.
func TestHolderExportAbortReason(t *testing.T) {
	live := context.Background()
	gone, cancel := context.WithCancel(live)
	cancel()
	expired, cancelExpired := context.WithDeadline(live, time.Now().Add(-time.Second))
	defer cancelExpired()
	brokenPipe := errors.New("write: broken pipe")
	// What a write blocked on a client that stopped reading returns when the
	// connection's write deadline passes. The failed write cancels the request,
	// so the context beside it reads as cancelled, not as expired.
	writeTimedOut := &net.OpError{Op: "write", Net: "tcp", Err: os.ErrDeadlineExceeded}

	for _, tc := range []struct {
		name     string
		ctx      context.Context
		writeErr error
		want     string
	}{
		{"deadline, even with the client gone too", expired, brokenPipe, "deadline"},
		{"the write deadline, though the request reads as cancelled", gone, writeTimedOut, "deadline"},
		{"request cancelled", gone, nil, "client_gone"},
		{"a write to the response failed", live, brokenPipe, "client_gone"},
		{"nothing else went wrong", live, nil, "database_error"},
	} {
		if got := holderExportAbortReason(tc.ctx, tc.writeErr); got != tc.want {
			t.Errorf("%s: reason = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// An aborted export's error is logged as a CLASS and never as its text: the
// text of a failed write names the client's address and port, and an error
// wrapped further up could carry whatever its wrapper put in it.
func TestHolderExportErrorClassCarriesNoErrorText(t *testing.T) {
	peer := &net.TCPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 51234}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"a client that went away",
			&net.OpError{Op: "write", Net: "tcp", Addr: peer, Err: syscall.ECONNRESET}, "connection_reset"},
		{"a write that timed out",
			&net.OpError{Op: "write", Net: "tcp", Addr: peer, Err: os.ErrDeadlineExceeded}, "write_deadline_exceeded"},
		{"the export's deadline", fmt.Errorf("fetch: %w", context.DeadlineExceeded), "context_deadline_exceeded"},
		{"a Postgres refusal", fmt.Errorf("fetch ana@example.com: %w", &pgconn.PgError{Code: "57P01", Message: "terminating"}), "postgres 57P01"},
		{"anything else, by its innermost type", fmt.Errorf("row for ana@example.com: %w", errors.New("boom")), "*errors.errorString"},
	} {
		got := holderExportErrorClass(tc.err)
		if got != tc.want {
			t.Errorf("%s: class = %q, want %q", tc.name, got, tc.want)
		}
		for _, leak := range []string{"203.0.113.7", "ana@example.com", "boom"} {
			if strings.Contains(got, leak) {
				t.Errorf("%s: class %q carries %q", tc.name, got, leak)
			}
		}
	}
}
