package sales

import (
	"hash/fnv"
	"time"
)

// How long the platform waits before asking the Payment Provider again what
// became of a Reversal Request it never answered, and when it stops asking
// (ADR 0024).
//
// The two rules live here, in the domain package, because they are policy about
// somebody's money rather than mechanism: the Reversal Reconciler applies them,
// the opportunistic Customer Area drain applies the same ones, and neither owns
// them. They are pure functions of the row — how many times it has been asked
// about, and when it was asked — so the schedule can be read and checked without
// a database, a provider, or a clock.

// reversalBackoffSchedule is the delay after each unknown answer: 10s, 30s, 2m,
// 5m, 15m (ADR 0024). Index n is the wait after the (n+1)th attempt.
//
// It opens fast and ends slow because the two ends are different situations.
// PayPhone taking twelve seconds instead of ten is the common case and resolves
// on the first retry, so the buyer who is still on the page gets their answer in
// seconds. A request still unknown a quarter of an hour later is an outage, and
// asking an unwell provider every ten seconds for a day is how the platform
// would add its own load to somebody else's incident.
var reversalBackoffSchedule = [...]time.Duration{
	10 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

// reversalBackoffCeiling is the interval the schedule settles at once its steps
// are exhausted: every half hour until the give-up bound below. Half an hour
// over the remaining day is a few dozen probes per stuck request, which is a
// queue an operator can read rather than a log nobody can.
const reversalBackoffCeiling = 30 * time.Minute

// reversalJitterSpread is how much of a step jitter may ADD — up to a fifth of
// it, never less than the step itself.
//
// It is one-sided deliberately. Jitter exists to break up a herd: a provider
// outage strands many Reversal Requests at once, and without it they all come
// due on the same instant forever and arrive back at the recovering provider
// together. Spreading them EARLIER would mean a probe landing sooner than the
// schedule says it may, and the schedule is the throttle protecting a provider
// that has already failed to answer.
const reversalJitterSpread = 0.2

// ReversalGiveUpAfter is how long the platform pursues a Reversal Request whose
// answer never becomes definite before recording it as an Unresolved Reversal
// (ADR 0024).
//
// A day, measured from when the Customer pressed and not from the last attempt,
// so the bound is a promise to the buyer about their own ask rather than a
// property of how often it happened to be probed.
//
// It exists because a permanently unwell provider must produce a queue somebody
// can read rather than a row retrying forever. What it does NOT mean is that the
// money is settled: giving up is the platform admitting it never found out, and
// only the provider's own dashboard can say — which is why an Unresolved
// Reversal awaits a Platform Operator and the Customer is told nothing.
const ReversalGiveUpAfter = 24 * time.Hour

// ReversalRetryDelay is how long a Reversal Request waits after its attempts'th
// unknown answer before it may be asked about again.
//
// attempts is the number of probes the request has now had, so the first unknown
// answer (attempts = 1) waits the schedule's first step. jitter is a fraction in
// [0, 1) chosen by the caller — see ReversalRetryJitter — and only ever adds.
func ReversalRetryDelay(attempts int, jitter float64) time.Duration {
	var step time.Duration
	switch {
	// A count below one is a caller that has not asked yet, and the honest answer
	// is the first step rather than the ceiling: nothing should wait half an hour
	// for its first retry because a count arrived wrong.
	case attempts < 1:
		step = reversalBackoffSchedule[0]
	case attempts <= len(reversalBackoffSchedule):
		step = reversalBackoffSchedule[attempts-1]
	default:
		step = reversalBackoffCeiling
	}
	if jitter < 0 {
		jitter = 0
	}
	if jitter > 1 {
		jitter = 1
	}
	return step + time.Duration(float64(step)*reversalJitterSpread*jitter)
}

// ReversalRetryJitter is the jitter fraction for one Reversal Request, derived
// from its own id.
//
// Derived rather than drawn, and that is the whole design. Jitter's job is to
// spread requests APART from each other, which a stable per-request fraction
// does at every step of the schedule: two rows stranded by the same outage keep
// the gap they were given instead of colliding on every retry. A random draw
// would do the same job and cost the schedule its testability — a delay nobody
// can predict is a delay no test can prove was honoured, and the backoff is the
// one thing standing between an outage and this platform hammering it.
func ReversalRetryJitter(reversalRequestID string) float64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(reversalRequestID))
	return float64(h.Sum32()) / float64(1<<32)
}

// ReversalGivenUp reports whether a Reversal Request whose answer is still
// unknown has been pursued for as long as the platform is willing to pursue it.
//
// requestedAt is when the Customer pressed. It is deliberately not the row's
// last attempt or its creation time in the database: the clock the buyer is owed
// runs from their ask.
func ReversalGivenUp(requestedAt, now time.Time) bool {
	return !now.Before(requestedAt.Add(ReversalGiveUpAfter))
}
