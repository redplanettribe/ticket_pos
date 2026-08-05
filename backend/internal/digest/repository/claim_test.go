// Package repository's one test, and it is here rather than in
// backend/integration/ for the single reason docs/testing.md allows: CONCURRENCY
// AND LOCKING. Everything else about the Follow Digest pipeline is asserted over
// HTTP, and everything else about it should stay there.
//
// What cannot be asserted over HTTP is this: two drain ticks that overlap IN THE
// MIDDLE OF ONE CLAIM must not both come away with the same pending Digest. An
// HTTP test can fire two requests at once and observe that one email was sent,
// which is the user-visible outcome and is paired with this file
// (integration.TestTwoOverlappingDrainTicksSendOneDigest) — but it cannot hold a
// row locked at the instant the second claimant reads it, so it cannot tell a
// query that is safe from one that merely raced and won.
//
// Cloud Scheduler is why this matters at all. It promises at-least-once
// delivery, not that two runs never overlap, and the drain's attempt deadline is
// deliberately longer than its interval (#226, terraform follow_digest.tf). So a
// slow run overlapping the next one is not an exotic failure — it is the
// ordinary Thursday morning — and the whole of the pipeline's safety under it
// rests on the claim below.
package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/migrate"
)

var testDB *platform.DB

func TestMain(m *testing.M) {
	ctx := context.Background()

	pg, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("ticket_pos"),
		postgres.WithUsername("ticket_pos"),
		postgres.WithPassword("ticket_pos"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)
		os.Exit(1)
	}

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "connection string: %v\n", err)
		os.Exit(1)
	}
	db, err := platform.OpenDB(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		os.Exit(1)
	}
	// The same migration runner the API uses, so the table under test is the
	// table production has. Dev seeds are excluded: nothing here needs them.
	if err := migrate.Up(ctx, db.Pool, false); err != nil {
		fmt.Fprintf(os.Stderr, "migrations: %v\n", err)
		os.Exit(1)
	}
	testDB = db

	code := m.Run()

	_ = db.Close()
	_ = pg.Terminate(ctx)
	os.Exit(code)
}

// seedPendingDigest creates one Customer owed one Digest, due now, and returns
// its id.
//
// SQL rather than the API because this package has no API: a repository test is
// below the seam where Follows and weeks are things you can ask for, and the
// only fact it needs is one claimable row.
func seedPendingDigest(t *testing.T, email string, weekStart time.Time) string {
	t.Helper()
	ctx := context.Background()

	var customerID string
	if err := testDB.Pool.QueryRowContext(ctx, `
		INSERT INTO customers (email, first_name, last_name, verified_at)
		VALUES ($1, 'Reader', 'Example', NOW())
		RETURNING id
	`, email).Scan(&customerID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	var digestID string
	if err := testDB.Pool.QueryRowContext(ctx, `
		INSERT INTO follow_digests (customer_id, week_start, status, next_attempt_at)
		VALUES ($1, $2::date, 'pending', NOW() - INTERVAL '1 minute')
		RETURNING id
	`, customerID, weekStart).Scan(&digestID); err != nil {
		t.Fatalf("seed pending digest: %v", err)
	}
	return digestID
}

// TestAContendedDigestIsSkippedRatherThanClaimedTwice is the property, held at
// the one instant it can actually break: a second tick arriving while the first
// is mid-claim.
//
// The first tick is played by an open transaction holding exactly the row lock
// the claim's own subquery takes. That is not a stand-in for a drain — it is
// what a drain IS at that instant — and it is the only way to make the overlap
// deterministic rather than a race the test hopes to win.
//
// TWO THINGS ARE ASSERTED AND BOTH MATTER. The contended claim must come back
// EMPTY, and it must come back QUICKLY. Empty is what stops two ticks sending
// one Customer two emails. Quickly is what stops a drain that meets a contended
// row from spending its whole time budget waiting for a lock instead of sending
// the forty Digests behind it — which is why the claim skips the row rather than
// queueing behind it, and why this test gives the second claimant a context far
// shorter than the transaction it is contending with.
func TestAContendedDigestIsSkippedRatherThanClaimedTwice(t *testing.T) {
	resetDigests(t)
	ctx := context.Background()
	repo := New(testDB)
	now := time.Now().UTC()

	digestID := seedPendingDigest(t, "contended@example.com", now)

	// Tick one, caught mid-claim: its transaction holds the row.
	firstTick, err := testDB.Pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin the first tick: %v", err)
	}
	defer func() { _ = firstTick.Rollback() }()
	var lockedID string
	if err := firstTick.QueryRowContext(ctx, `
		SELECT id FROM follow_digests
		WHERE status = 'pending' AND next_attempt_at <= $1
		ORDER BY next_attempt_at
		FOR UPDATE
		LIMIT 1
	`, now).Scan(&lockedID); err != nil {
		t.Fatalf("the first tick could not take the row: %v", err)
	}
	if lockedID != digestID {
		t.Fatalf("the first tick locked %s, want the seeded Digest %s", lockedID, digestID)
	}

	// Tick two, arriving in that window. The short context is the assertion about
	// blocking: a claim that queues behind the lock cannot answer inside it.
	contended, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	claimed, err := repo.ClaimDueDigest(contended, now, now.Add(5*time.Minute))
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		t.Fatal("the second tick BLOCKED on the Digest the first tick was claiming; a contended row must be skipped, or one slow send stalls every Digest behind it")
	case err != nil:
		t.Fatalf("the second tick's claim failed: %v", err)
	case claimed != nil:
		t.Fatalf("the second tick claimed Digest %s while the first tick held it; that is one Customer receiving two Digests for one week", claimed.ID)
	}

	// The first tick finishes its claim and commits, exactly as ClaimDueDigest
	// does in one statement.
	if _, err := firstTick.ExecContext(ctx, `
		UPDATE follow_digests
		SET next_attempt_at = $2, attempt_count = attempt_count + 1, updated_at = NOW()
		WHERE id = $1
	`, digestID, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("the first tick could not finish its claim: %v", err)
	}
	if err := firstTick.Commit(); err != nil {
		t.Fatalf("commit the first tick: %v", err)
	}

	// And now that nothing is holding it, the row is still not claimable: the
	// claim itself took it out of the queue for the lease. This is the half that
	// would survive the lock being removed and the row simply being waited for,
	// so it is asserted separately.
	after, err := repo.ClaimDueDigest(ctx, now, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("claim after the first tick committed: %v", err)
	}
	if after != nil {
		t.Fatalf("a third claim took Digest %s after it had already been claimed and leased", after.ID)
	}
}

// TestOnlyOneOfManySimultaneousTicksClaimsAPendingDigest is the same property
// under real parallelism rather than a staged lock.
//
// It proves nothing the test above does not, when it passes. It is here because
// the two fail differently: the staged one fails when the claim stops skipping,
// and this one fails when the claim stops being ONE statement — a future
// refactor that read the due row and then updated it in two round trips would
// still skip locked rows and would still hand the same Digest to two ticks.
func TestOnlyOneOfManySimultaneousTicksClaimsAPendingDigest(t *testing.T) {
	resetDigests(t)
	ctx := context.Background()
	repo := New(testDB)

	// Several rounds, because a race lost once is a race that passed by accident.
	for round := range 20 {
		resetDigests(t)
		now := time.Now().UTC()
		digestID := seedPendingDigest(t, fmt.Sprintf("racer-%d@example.com", round), now)

		const ticks = 8
		start := make(chan struct{})
		var wg sync.WaitGroup
		claims := make([]*PendingDigest, ticks)
		errs := make([]error, ticks)
		for i := range ticks {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				claims[i], errs[i] = repo.ClaimDueDigest(ctx, now, now.Add(5*time.Minute))
			}()
		}
		close(start)
		wg.Wait()

		var claimed int
		for i := range ticks {
			if errs[i] != nil {
				t.Fatalf("round %d: tick %d failed to claim: %v", round, i, errs[i])
			}
			if claims[i] == nil {
				continue
			}
			claimed++
			if claims[i].ID != digestID {
				t.Fatalf("round %d: tick %d claimed %s, want the one pending Digest %s", round, i, claims[i].ID, digestID)
			}
		}
		if claimed != 1 {
			t.Fatalf("round %d: %d of %d simultaneous ticks claimed the one pending Digest, want exactly 1 — every extra one is a duplicate email",
				round, claimed, ticks)
		}
	}
}

// resetDigests clears what one test wrote. There is no shared harness here and
// no truncate-everything: this package touches two tables.
func resetDigests(t *testing.T) {
	t.Helper()
	if _, err := testDB.Pool.ExecContext(context.Background(),
		`TRUNCATE TABLE follow_digests, follow_digest_sent_events, customers RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset: %v", err)
	}
}
