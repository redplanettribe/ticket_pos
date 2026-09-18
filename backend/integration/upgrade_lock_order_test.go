package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// The Upgrade's lock order (#654, ADR 0074, #650).
//
// THE COMMIT TRANSACTION TAKES ONE SORTED `ticket_types` LOCK SET AND NEVER TWO.
// CommitSales locks the basket's Ticket Types FOR UPDATE in sorted order so that
// two concurrent commits can never hold each other's next row. The cross-Sale
// Upgrade reverses an earlier free Sale INSIDE that same transaction, and the
// reversal gives capacity back by locking that Sale's Ticket Type — a row that
// is typically NOT in the basket, since the whole point is free-type-out,
// paid-type-in.
//
// Acquired down there, it would be a SECOND sorted set taken after the first,
// and two independent sorted sets are not an order at all:
//
//	free type F, paid type V, F < V
//	buyer A, paid-only basket, upgrading out of F: V then F
//	buyer B, ordinary mixed free+paid basket:      F then V
//
// A holds V and wants F; B holds F and wants V; Postgres breaks the cycle by
// aborting one side with 40P01. For A that abort lands in the transaction
// committing an ALREADY-APPROVED Payment — money taken, nothing sold — which is
// the one incident this platform resolves by hand.
//
// The fix is to name the Upgrade's candidate Ticket Type into the SAME sorted
// set, before it is taken. This test is the proof that it really is the same
// set, and it is written so that it would FAIL against the two-set version.

// TestTheUpgradeLocksItsFreeTicketTypeInsideTheBasketsSortedSet drives the
// interleaving rather than asserting about the source.
//
// HOW IT SEES A LOCK ORDER FROM OUTSIDE. A second transaction takes the PAID
// type and holds it, so the commit must block on it. The question is what the
// commit is holding WHILE it blocks. With one sorted set and F < V the commit
// has already taken F and is waiting on V; with the basket's set taken alone the
// commit is waiting on V holding nothing, and would reach F only afterwards. A
// third connection probes F with NOWAIT: it is refused in the first world and
// granted in the second.
//
// F < V IS ARRANGED AND NOT ASSUMED. Ticket Type ids are random v4 UUIDs, so
// which one sorts first is a coin toss, and on the other side of that toss the
// probe cannot tell the two worlds apart — the commit would block on V, which it
// reaches first either way. So the free type is created until one lands below
// the paid type in the same byte order sort.Strings uses.
func TestTheUpgradeLocksItsFreeTicketTypeInsideTheBasketsSortedSet(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)

	staff := orgAdminSession(t, env)
	eventID, paidTypeID := publishCheckoutEvent(t, env, staff, "Lock Order", "lock-order", 3000, 20)

	var freeTypeID string
	for attempt := 0; attempt < 32 && freeTypeID == ""; attempt++ {
		id := createTicketTypeWithCapacity(t, env, staff, eventID, fmt.Sprintf("Community %d", attempt), 0, 20)
		if id < paidTypeID {
			freeTypeID = id
		}
	}
	if freeTypeID == "" {
		t.Fatalf("no free Ticket Type id sorted below the paid one (%s) in 32 tries; v4 UUIDs should not do that", paidTypeID)
	}

	buyer := "ada@example.com"
	claim := beginCheckoutSettled(t, env, "test-org", "lock-order", buyerSession(t, env, buyer),
		checkoutBody(buyer, "Ada", "Byron", cartLine(freeTypeID, 1)))
	approvedRef(t, claim)
	freeSaleID := saleIDOfPayment(t, env, claim.ClientTransactionID)

	body := checkoutBody(buyer, "Ada", "Byron", cartLine(paidTypeID, 1))
	body["upgrade_elected"] = true
	begun := beginCheckoutAsOK(t, env, "test-org", "lock-order", buyerSession(t, env, buyer), body)

	ctx := context.Background()

	// The competing transaction, standing in for an ordinary commit that has
	// reached the paid type. It holds nothing else, so the only thing it can
	// teach us is where the Upgrade's commit stops.
	blocker, err := env.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin blocking transaction: %v", err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = blocker.Rollback()
		}
	}
	defer release()
	var ignored int
	if err := blocker.QueryRowContext(ctx,
		`SELECT sold_count FROM ticket_types WHERE id = $1 FOR UPDATE`, paidTypeID).Scan(&ignored); err != nil {
		t.Fatalf("lock the paid Ticket Type: %v", err)
	}

	// The commit, off in its own goroutine because it is about to block. It
	// takes no *testing.T with it: a helper that called t.Fatalf from here would
	// be reporting from the wrong goroutine.
	type confirmOutcome struct {
		status int
		body   []byte
		err    error
	}
	done := make(chan confirmOutcome, 1)
	go func() {
		payload, _ := json.Marshal(map[string]any{"provider_params": map[string]string{"outcome": "approved"}})
		resp, err := http.Post(
			env.server.URL+"/api/v1/public/checkout/"+begun.ClientTransactionID+"/confirm",
			"application/json", bytes.NewReader(payload))
		if err != nil {
			done <- confirmOutcome{err: err}
			return
		}
		defer resp.Body.Close()
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		done <- confirmOutcome{status: resp.StatusCode, body: buf.Bytes()}
	}()

	waitUntilSomebodyIsBlockedOnALock(t, env)

	// THE ASSERTION. The commit is stopped at the paid type; it must already be
	// holding the free type, because both belong to the one sorted acquisition
	// and the free type sorts first.
	if grantedTo(t, env, freeTypeID) {
		release()
		<-done
		t.Fatalf("the free Ticket Type was free to lock while the commit was blocked on the paid one — "+
			"the Upgrade's reversal is taking a SECOND sorted lock set (free %s < paid %s), which deadlocks "+
			"against an ordinary mixed free+paid commit", freeTypeID, paidTypeID)
	}

	release()
	select {
	case out := <-done:
		if out.err != nil {
			t.Fatalf("confirm checkout: %v", out.err)
		}
		if out.status != http.StatusOK {
			t.Fatalf("confirm checkout status=%d body=%s", out.status, out.body)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("the commit never finished after the blocking transaction was released")
	}

	// And the Upgrade really happened, so the lock order above is the order of a
	// transaction that did the work rather than one that quietly skipped it.
	free := readSaleProvenance(t, env, freeSaleID)
	if free.Status != "reversed" {
		t.Fatalf("the free Sale is %q, want reversed — the Upgrade did not run and this test proved nothing", free.Status)
	}
	if !free.ReplacedBy.Valid {
		t.Fatalf("the free Sale names no replacement; the Upgrade did not link the pair")
	}
}

// waitUntilSomebodyIsBlockedOnALock waits for the commit goroutine to reach the
// row the blocking transaction holds. Polling pg_stat_activity is how the test
// learns that the commit has stopped ADVANCING — without it, a probe that found
// the free type unlocked could simply be early.
func waitUntilSomebodyIsBlockedOnALock(t *testing.T, env *testEnv) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := env.db.QueryRow(`
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
			  AND pid <> pg_backend_pid()
		`).Scan(&waiting); err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if waiting > 0 {
			// Settle: the backend is blocked, and a lock it took before blocking
			// is already held. Nothing else in this test can move it.
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no backend ever blocked on a lock; the commit did not reach the paid Ticket Type")
}

// grantedTo reports whether this Ticket Type row can be locked right now, asked
// with NOWAIT so the answer comes back instead of the test joining the queue.
func grantedTo(t *testing.T, env *testEnv, ticketTypeID string) bool {
	t.Helper()
	ctx := context.Background()
	probe, err := env.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin probe transaction: %v", err)
	}
	defer func() { _ = probe.Rollback() }()
	var ignored int
	err = probe.QueryRowContext(ctx,
		`SELECT sold_count FROM ticket_types WHERE id = $1 FOR UPDATE NOWAIT`, ticketTypeID).Scan(&ignored)
	switch {
	case err == nil:
		return true
	case isLockNotAvailable(err), err == sql.ErrNoRows:
		return false
	default:
		t.Fatalf("probe %s FOR UPDATE NOWAIT: %v", ticketTypeID, err)
		return false
	}
}

func isLockNotAvailable(err error) bool {
	// 55P03 lock_not_available. Matched on the message because the probe goes
	// through database/sql and the driver's typed error is not worth reaching
	// for in one place.
	return err != nil && (bytes.Contains([]byte(err.Error()), []byte("55P03")) ||
		bytes.Contains([]byte(err.Error()), []byte("could not obtain lock")))
}
