package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
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

	for _, tc := range []struct {
		name     string
		ctx      context.Context
		writeErr error
		want     string
	}{
		{"deadline, even with the client gone too", expired, brokenPipe, "deadline"},
		{"request cancelled", gone, nil, "client_gone"},
		{"a write to the response failed", live, brokenPipe, "client_gone"},
		{"nothing else went wrong", live, nil, "database_error"},
	} {
		if got := holderExportAbortReason(tc.ctx, tc.writeErr); got != tc.want {
			t.Errorf("%s: reason = %q, want %q", tc.name, got, tc.want)
		}
	}
}
