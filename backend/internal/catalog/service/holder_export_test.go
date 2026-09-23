package service_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog/service"
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
//
// Why an export aborted, and the class its error is logged as, are asserted
// where they happen: the reasons by the HTTP integration tests of a database
// failure, a client that went away, a stalled client and a panic, and every
// error class by platform.ErrorClass's own test.
func TestTheHolderExportDeadlineIsInsideTheRequestTimeout(t *testing.T) {
	requestTimeout := terraformSecondsVariable(t,
		filepath.Join(holderPurgeModuleDir(), "variables.tf"), "api_request_timeout_seconds")

	deadline := (&service.Service{}).HolderExportDeadline()
	if margin := requestTimeout - deadline; margin < 5*time.Second {
		t.Fatalf("the Holder Export deadline = %v, Cloud Run's request timeout = %v: the export must expire at least 5s first, or the platform cuts it off before it can say why",
			deadline, requestTimeout)
	}
}
