package integration

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A CLIENT THAT GIVES UP IS NOT A SERVER ERROR. A Sales list request whose
// client goes away while the list is still being read used to surface as an
// unmapped 500 logged at ERROR with no cause: the cancelled request context
// failed the database read, and the failure was written up as the server's.
// The error that read reports is the cancellation itself, so the request is
// logged at INFO with client_gone=true and the 499 that says the client closed
// the request.
//
// The list is held on a table lock so the client's timeout is certain to pass
// while the handler is inside its database read, which is where a slow list
// loses its client in production.
func TestACancelledListRequestIsLoggedAsAClientThatGaveUp(t *testing.T) {
	env := setupTest(t)
	staff := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, staff, "Slow List", "slow-list")
	srv, requestLog := productionChainServer(t)

	// SQL rather than the API: nothing in the API makes a read wait. The lock is
	// released before the server is closed (cleanups run last-in first-out), so
	// the handler is never left waiting on it.
	lock, err := sharedDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = lock.Rollback() })
	if _, err := lock.Exec(`LOCK TABLE ticket_sales IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock ticket_sales: %v", err)
	}

	requestID := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/staff/events/"+eventID+"/sales", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+staff)
	req.Header.Set("X-Request-ID", requestID)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("the list answered %d while its table was locked; the test no longer holds the request in its read", resp.StatusCode)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("client error = %v, want its own timeout", err)
	}

	line := requestLog.awaitRequestLine(t, requestID)
	if line["level"] != "INFO" || line["status"] != float64(499) {
		t.Fatalf("request line = %v, want INFO with status 499", line)
	}
	if line["client_gone"] != true {
		t.Fatalf("request line = %v, want client_gone=true", line)
	}
	// The 499 is earned only because the error the handler reported is the
	// cancellation itself; any other failure keeps its 500 at ERROR.
	if line["error_class"] != "context_canceled" {
		t.Fatalf("request line = %v, want error_class context_canceled", line)
	}
}
