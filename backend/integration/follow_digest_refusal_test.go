package integration

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// How the Follow Digest drain treats a send the mail provider refused (#848,
// spec #847). A refusal retrying cannot fix - a 403 for a domain the key may
// not send from, say - is given up on the attempt it happens, and reported
// once. Everything else keeps the backoff and the five-attempt budget it had.
// The reports are read through the Digest service's logger, the seam ADR 0079
// reports Errors behind.

// captureDigestLog points the Digest service's logger at a capture for the
// length of one test.
func captureDigestLog(t *testing.T) *captureLogger {
	t.Helper()
	capture := &captureLogger{}
	sharedApp.DigestService.WithLogger(capture)
	t.Cleanup(func() {
		sharedApp.DigestService.WithLogger(platform.NewSlogLogger(sharedApp.Logger, sharedApp.Reporter))
	})
	return capture
}

// pendingRefusedDigest enqueues one Digest with something to say, for ana.
func pendingRefusedDigest(t *testing.T, env *testEnv) {
	t.Helper()
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Refused Fest", "refused-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
}

func TestFollowDigestPermanentlyRefusedIsAbandonedOnTheFirstAttempt(t *testing.T) {
	env := setupTest(t)
	pendingRefusedDigest(t, env)
	logs := captureDigestLog(t)

	env.email.FailWith(&platform.EmailProviderRefusal{Status: 403, Body: `{"message":"not authorized to send from this domain"}`})
	t.Cleanup(func() { env.email.FailWith(nil) })

	refused := drainFollowDigests(t, env)
	if refused.Claimed != 1 || refused.GaveUp != 1 || refused.Retrying != 0 {
		t.Fatalf("refused drain = %+v, want claimed 1, gave_up 1, retrying 0", refused)
	}
	if status, attempts := digestRowStatus(t, env, "ana@example.com"); status != "failed" || attempts != 1 {
		t.Fatalf("after a permanent refusal status=%q attempts=%d, want failed and 1", status, attempts)
	}

	// Never rescheduled: however long the clock runs, and with the provider
	// healthy again, nothing comes due and nothing is sent.
	env.email.FailWith(nil)
	advanceDigestClock(t, env, 3*time.Hour)
	if later := drainFollowDigests(t, env); later.Claimed != 0 {
		t.Fatalf("drain after a permanent refusal claimed=%d, want 0 (%+v)", later.Claimed, later)
	}
	if got := len(digestsFor(t, env, "ana@example.com")); got != 0 {
		t.Fatalf("ana received %d Digests, want 0", got)
	}

	errs := errorLines(logs)
	if len(errs) != 1 {
		t.Fatalf("errors logged = %+v; want exactly one", errs)
	}
	if !strings.Contains(errs[0].msg, "permanently") {
		t.Fatalf("the give-up %q does not name the refusal as permanent", errs[0].msg)
	}
	if !strings.Contains(fmt.Sprint(errs[0].args...), "403") {
		t.Fatalf("the give-up does not carry the provider's answer: %v", errs[0].args)
	}
}

// The transient path, pinned as it was: back on the queue after each of the
// first four attempts, due again exactly when the backoff says (1, 5, 15, 45
// minutes), and given up on at the fifth, reported once.
func TestFollowDigestTransientFailureKeepsItsBackoffAndFiveAttempts(t *testing.T) {
	for _, cause := range []error{
		errors.New("the mail provider is down"),
		&platform.EmailProviderRefusal{Status: 503, Body: "unavailable"},
		&platform.EmailProviderRefusal{Status: 429, Body: "rate limited"},
	} {
		t.Run(cause.Error(), func(t *testing.T) {
			env := setupTest(t)
			pendingRefusedDigest(t, env)
			logs := captureDigestLog(t)
			env.email.FailWith(cause)
			t.Cleanup(func() { env.email.FailWith(nil) })

			at := time.Duration(0)
			for attempt, backoff := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 45 * time.Minute} {
				failed := drainFollowDigests(t, env)
				if failed.Claimed != 1 || failed.Retrying != 1 || failed.GaveUp != 0 {
					t.Fatalf("attempt %d drained %+v, want claimed 1 and retrying 1", attempt+1, failed)
				}
				if status, attempts := digestRowStatus(t, env, "ana@example.com"); status != "pending" || attempts != attempt+1 {
					t.Fatalf("after attempt %d status=%q attempts=%d, want pending and %d", attempt+1, status, attempts, attempt+1)
				}
				at += backoff
				advanceDigestClock(t, env, at-time.Second)
				if early := drainFollowDigests(t, env); early.Claimed != 0 {
					t.Fatalf("a second before the %v backoff ends the drain claimed %d, want 0", backoff, early.Claimed)
				}
				advanceDigestClock(t, env, at)
			}

			last := drainFollowDigests(t, env)
			if last.Claimed != 1 || last.GaveUp != 1 || last.Retrying != 0 {
				t.Fatalf("fifth attempt drained %+v, want claimed 1 and gave_up 1", last)
			}
			if status, attempts := digestRowStatus(t, env, "ana@example.com"); status != "failed" || attempts != 5 {
				t.Fatalf("after five attempts status=%q attempts=%d, want failed and 5", status, attempts)
			}
			errs := errorLines(logs)
			if len(errs) != 1 || strings.Contains(errs[0].msg, "permanently") {
				t.Fatalf("errors logged = %+v; want exactly one give-up naming exhausted attempts", errs)
			}
		})
	}
}
