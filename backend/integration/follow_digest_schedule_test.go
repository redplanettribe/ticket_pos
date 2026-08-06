package integration

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Turning the Digest on (#226, parent #215, ADR 0030): what the two Cloud
// Scheduler jobs will meet when they start calling the endpoints on their own.
//
// The schedule itself is Terraform and is asserted where it lives, against the
// Go constants it has to agree with (digest/service.TestTheDigest... in
// schedule_test.go). What is asserted HERE is everything the schedule makes
// ordinary rather than exotic: ticks that overlap, weeks with nothing in them,
// and a ledger that would otherwise grow for as long as the platform runs.

// TestTwoOverlappingDrainTicksSendOneDigest is the user-visible half of the
// claim's concurrency property, and it is the pair docs/testing.md requires for
// the repository test that holds the other half
// (digest/repository.TestAContendedDigestIsSkippedRatherThanClaimedTwice).
//
// The repository test proves a contended row is skipped at the instant of the
// claim, which no HTTP test can stage. This one proves the thing a person would
// notice: however many ticks arrive at once, ONE EMAIL. That is not a
// restatement — a claim can be perfectly safe and the pipeline still send twice
// if anything after the claim re-reads the queue — and it is the assertion that
// would survive the claim being rewritten entirely.
//
// The drain's attempt deadline is deliberately longer than its own interval
// (#226), so Cloud Scheduler overlapping two runs is the ordinary Thursday
// morning rather than an incident.
func TestTwoOverlappingDrainTicksSendOneDigest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Overlap Fest", "overlap-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "overlap@example.com")

	if enqueued := enqueueFollowDigests(t, env).Enqueued; enqueued != 1 {
		t.Fatalf("enqueued=%d, want 1", enqueued)
	}

	// Eight ticks at once rather than two. Overlap is a window, and widening the
	// crowd is the only honest way to make a test that hopes to land inside it
	// land there often.
	const ticks = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	claimed := make([]int, ticks)
	for i := range ticks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp, body := env.post(t, followDigestDrainPath, nil, nil)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("tick %d: drain status=%d error=%+v, want 200", i, resp.StatusCode, body.Error)
				return
			}
			var result followDigestDrainResult
			if err := json.Unmarshal(body.Data, &result); err != nil {
				t.Errorf("tick %d: decode drain result: %v", i, err)
				return
			}
			claimed[i] = result.Claimed
		}()
	}
	close(start)
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}

	digests := digestsFor(t, env, "overlap@example.com")
	if len(digests) != 1 {
		t.Fatalf("%d Digests reached overlap@example.com from %d overlapping ticks, want exactly 1", len(digests), ticks)
	}

	total := 0
	for _, c := range claimed {
		total += c
	}
	if total != 1 {
		t.Fatalf("%d ticks claimed the one pending Digest between them, want exactly 1 claim in total", total)
	}

	if status, _ := digestRowStatus(t, env, "overlap@example.com"); status != "sent" {
		t.Fatalf("Digest status=%q after the overlapping ticks, want \"sent\"", status)
	}
}

// TestAWeekWithNothingToSendCompletesCleanly is the state this pipeline is in
// for all but one hour of the week, and for every week before the first Customer
// ever presses Follow.
//
// It is asserted because the failure it guards against is not a wrong email but
// a red job: a scheduler tick that 500s on an empty week trains whoever is
// watching to ignore this job's failures, and the week it fails for a real
// reason looks exactly the same.
//
// Both halves are exercised — an enqueue that finds nobody eligible, and a drain
// that finds nothing queued — because the two run on different schedules and
// either can meet an empty week alone.
func TestAWeekWithNothingToSendCompletesCleanly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	// A published, listed, upcoming Event that nobody Follows: the week is empty
	// because of the Follows, not because the catalogue is.
	discoverableEvent(t, env, sessionID, "Unfollowed Fest", "unfollowed-fest", env.fixedClock.Add(72*time.Hour))
	customerSignIn(t, env, "nobody@example.com")

	enqueued := enqueueFollowDigests(t, env)
	if enqueued.Eligible != 0 || enqueued.Enqueued != 0 || enqueued.AlreadyEnqueued != 0 {
		t.Fatalf("enqueue on an empty week = %+v, want zeros", enqueued)
	}
	if enqueued.WeekStart == "" {
		t.Fatal("the enqueue reported no week; the week it declared is the only thing a caller learns from a run that enqueued nothing")
	}

	drained := drainFollowDigests(t, env)
	if drained.Claimed != 0 || drained.Sent != 0 || drained.Empty != 0 || drained.Retrying != 0 || drained.GaveUp != 0 {
		t.Fatalf("drain on an empty queue = %+v, want zeros", drained)
	}
	if drained.PendingTotal != 0 {
		t.Fatalf("drain reported %d Digests still pending on an empty queue", drained.PendingTotal)
	}
	if drained.OldestPendingFor != "" {
		t.Fatalf("drain reported an oldest pending week %q with nothing pending", drained.OldestPendingFor)
	}

	if digests := env.email.FollowDigestsSent(); len(digests) != 0 {
		t.Fatalf("%d Digests were sent in a week with nothing to send", len(digests))
	}
	if rows := digestRowCount(t, env); rows != 0 {
		t.Fatalf("%d rows in the queue after an empty week, want none", rows)
	}
}

// TestTheSentLedgerIsPrunedOnceAnEventHasEnded is the one piece of housekeeping
// this feature owes the database.
//
// `follow_digest_sent_events` is the only table in the system whose size is
// driven by READING rather than by selling (ADR 0030): it gains a row per
// follower per Event mailed, forever, whether or not anybody ever buys anything.
// An ended Event can never appear in another Digest — eligibility is
// `COALESCE(ends_at, starts_at) >= now()`, the public explorer's own filter — so
// its ledger rows have stopped answering any question and are only occupying an
// index the composition query reads on every send.
//
// THE UPCOMING EVENT IS THE ASSERTION THAT MATTERS. A prune that took too much
// would make an Event this reader has already been shown NEW to them again, and
// they would be mailed about it a second time with nothing anywhere to notice.
// That is why both Events are seeded from one Digest: they differ in exactly one
// fact, which is whether their doors have closed.
func TestTheSentLedgerIsPrunedOnceAnEventHasEnded(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Ended Fest", "ended-fest", env.fixedClock.Add(72*time.Hour))
	discoverableEvent(t, env, sessionID, "Later Fest", "later-fest", env.fixedClock.Add(60*24*time.Hour))
	followingCustomer(t, env, "pruned@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	if got := digestLedgerEvents(t, env, "pruned@example.com"); len(got) != 2 {
		t.Fatalf("sent-ledger = %v after the first Digest, want both Events", got)
	}

	// A month on: one Event's doors have closed and the other's have not.
	advanceDigestClock(t, env, 30*24*time.Hour)

	drained := drainFollowDigests(t, env)
	if drained.Pruned != 1 {
		t.Fatalf("drain pruned %d ledger rows, want 1 — the row for the Event that has ended", drained.Pruned)
	}

	remaining := digestLedgerEvents(t, env, "pruned@example.com")
	if len(remaining) != 1 || remaining[0] != "Later Fest" {
		t.Fatalf("sent-ledger = %v after the prune, want only [Later Fest]: an ended Event's rows are dead weight, and a still-upcoming Event's row is what stops it being mailed out twice", remaining)
	}
}

// TestPruningNeverMakesAnAlreadyShownEventNewAgain is the same rule read from
// the reader's inbox rather than from the table, and it is the failure the prune
// could plausibly cause.
//
// A prune that dropped a row for an Event that has NOT ended would restore that
// Event's novelty — the ledger is the whole definition of New to You — and the
// following week's Digest would advertise it again. Nothing about that would
// look like an error anywhere: one extra email, to everybody, every week.
func TestPruningNeverMakesAnAlreadyShownEventNewAgain(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Standing Fest", "standing-fest", env.fixedClock.Add(60*24*time.Hour))
	followingCustomer(t, env, "unrepeated@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)
	if digests := digestsFor(t, env, "unrepeated@example.com"); len(digests) != 1 {
		t.Fatalf("%d Digests in the first week, want 1", len(digests))
	}

	// A week later, with many ticks in between — every one of them running the
	// prune at its tail.
	advanceDigestClock(t, env, 7*24*time.Hour)
	for range 3 {
		drainFollowDigests(t, env)
	}
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "unrepeated@example.com")
	if len(digests) != 1 {
		t.Fatalf("%d Digests after a second week, want 1: the second week's Digest was empty, because the only Event this reader Follows had already been shown to them", len(digests))
	}
	if got := digestLedgerEvents(t, env, "unrepeated@example.com"); len(got) != 1 || got[0] != "Standing Fest" {
		t.Fatalf("sent-ledger = %v, want [Standing Fest] left untouched by the prune", got)
	}
}
