// Package service composes and delivers the weekly Follow Digest (#220, parent
// #215, ADR 0030).
//
// It is the platform's SECOND piece of scheduled work, after ADR 0024's Reversal
// Reconciler, and the first that fans out to many recipients. Nothing here runs
// on a timer inside this process: there is no goroutine and no ticker anywhere
// in the feature, exactly as ADR 0024 established, because Cloud Run gives a
// container no life outside a request. Both halves of the pipeline are internal
// HTTP endpoints a scheduler calls and a human can curl.
//
// It is also a module that OWNS ALMOST NOTHING. The Follows belong to customers,
// the Events and Tags to catalog, the Customer's Digest Locale to customers, the
// delivery to platform. What lives here is the two tables nobody else could own
// — the queue and the sent-ledger — and the decision about what one person is
// told this week.
package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/peter/ticket_pos/backend/internal/digest/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// TagNameResolver resolves Tag names in one Locale, keyed by canonical key.
//
// Declared here, on the side that calls it, and satisfied by catalog — which
// owns the shared Tag pool and the one localization rule for it (#219,
// catalog/service.LocalizedTagNames). It is the narrowest statement of the need:
// keys and a language in, names out, with no way to reach anything else about a
// Tag.
//
// The rule it stands for is worth naming, because a Digest is the only place in
// the backend that needs it. A Preset Tag is named in the reader's language from
// a column on `tags`, which is the narrow amendment ADR 0030 made to ADR 0027; a
// Custom Tag is rendered exactly as the Organization coined it, in every
// language, which ADR 0027 already holds correct. Restating either of those here
// would be a second copy of a rule that has to stay one.
type TagNameResolver interface {
	LocalizedTagNames(ctx context.Context, canonicalKeys []string, locale platform.Locale) (map[string]string, error)
}

// Service composes and delivers Follow Digests.
type Service struct {
	repo              *repository.Repository
	email             platform.EmailSender
	tags              TagNameResolver
	storefrontBaseURL string
	logger            platform.Logger
	now               func() time.Time
	// drainBatch narrows how many Digests one run delivers, for tests only. See
	// WithDrainBatch.
	drainBatch int
}

// New builds the Digest service.
func New(
	repo *repository.Repository,
	email platform.EmailSender,
	storefrontBaseURL string,
	logger platform.Logger,
) *Service {
	return &Service{
		repo:              repo,
		email:             email,
		storefrontBaseURL: storefrontBaseURL,
		logger:            logger,
		now:               time.Now,
	}
}

// WithClock overrides the clock, so a test can move a week or a backoff without
// sleeping. Same chaining shape as every other service's.
func (s *Service) WithClock(clock func() time.Time) *Service {
	s.now = clock
	return s
}

// WithTags attaches the resolver that names Tags in a reader's language. Wired
// after construction because catalog is built later, exactly as customers' own
// Tag resolver is.
//
// A service without it still sends Digests; the Events simply carry no "because
// you follow" line for their Tags. That degradation is deliberate: a missing
// attribution is worth less than a Digest nobody gets.
func (s *Service) WithTags(resolver TagNameResolver) *Service {
	s.tags = resolver
	return s
}

// WithDrainBatch narrows how many Digests one run delivers.
//
// It exists so a test can prove the bound is a bound, for the reason
// WithReversalDrainBatch exists: boundedness is a property of the loop and not
// of the number, and reaching the default would mean staging a batch's worth of
// Customers. Nothing in production calls it.
func (s *Service) WithDrainBatch(batch int) *Service {
	s.drainBatch = batch
	return s
}

// EnqueueResult is what one enqueue run did.
//
// It reports ELIGIBLE separately from ENQUEUED because the two answer different
// questions and only the pair is actionable. Eligible says how many Customers
// this feature is about at all — the number that should grow week over week, and
// the one worth alarming on if it collapses. Enqueued says what this run
// changed. A second run in one week reports the same eligible count and zero
// enqueued, which is the correct and unalarming answer.
type EnqueueResult struct {
	// WeekStart is the week this run declared, as a date — the Monday that begins
	// it in Ecuador's zone (platform.DigestWeekStart). It is echoed back because
	// the endpoint takes no arguments, so this is the only way a caller learns
	// which week they just enqueued.
	WeekStart string `json:"week_start"`
	Eligible  int    `json:"eligible"`
	Enqueued  int    `json:"enqueued"`
	// AlreadyEnqueued is the Customers who already had a Digest for this week. It
	// is reported rather than swallowed so that a repeated run reads as
	// idempotent rather than as a failure that enqueued nothing.
	AlreadyEnqueued int `json:"already_enqueued"`
}

// DrainResult is what one drain run did, and what is still waiting once it had
// done it.
//
// The tally is per OUTCOME rather than a single count, for the reason the
// Reversal Reconciler's is: "we worked forty Digests" says nothing an operator
// can act on, and "thirty-eight sent, one had nothing to say, one is retrying"
// says all of it.
type DrainResult struct {
	// Claimed is how many pending Digests this run took out of the queue. Zero is
	// the ordinary answer on all but one hour of the week.
	Claimed int `json:"claimed"`
	// Sent is Digests delivered and recorded in the sent-ledger.
	Sent int `json:"sent"`
	// Empty is Digests whose Customer's Follows matched nothing they had not
	// already been shown. NO EMAIL WAS SENT for these, which is the rule and not
	// a failure: an empty Digest teaches its reader to ignore the next one.
	Empty int `json:"empty"`
	// Retrying is Digests whose delivery failed and which are back in the queue
	// on a backoff. It is the number that says a provider is unwell.
	Retrying int `json:"retrying"`
	// GaveUp counts the Digests THIS RUN abandoned after exhausting their
	// attempts. Each one is a Customer who gets no Digest this week, and it is
	// the number worth alerting on.
	GaveUp int `json:"gave_up"`
	// PendingTotal is how many Digests are still waiting once this run finished,
	// and OldestPendingWeek is the week the oldest of them belongs to (a date,
	// absent when the queue is empty).
	//
	// They are the answer to "is this getting better or worse", and two curls a
	// minute apart answer it without a database session. The oldest week is the
	// one that matters most: a pending Digest from LAST week is a backlog that
	// has outlived the thing it was about.
	PendingTotal      int    `json:"pending_total"`
	OldestPendingWeek string `json:"oldest_pending_week,omitempty"`
}

// digestDrainBatch and digestDrainBudget bound one run, and the budget is the
// real bound.
//
// THE DEADLINE CHAIN, stated here once for whoever writes the Terraform (#226),
// because each term is worthless without the one outside it:
//
//	digestDrainBudget  <  Cloud Scheduler's attempt_deadline  <  Cloud Run's
//	                                                             request timeout
//	       45s         <              90s                     <      300s
//
// It is the same chain ADR 0024's reconciler measured, and it is the same chain
// for the same reason: whichever term is smallest is what actually stops a run,
// and only the innermost one stops it politely. The other two abandon the
// request where it stands, and here that means losing the write that records a
// message the provider has already accepted — which costs a Customer a duplicate
// Digest on the retry.
//
// THE BATCH IS SIZED BY THE PROVIDER'S RATE LIMIT rather than by the budget.
// Roughly two requests a second is what ADR 0009 records, so fifty sends is
// about twenty-five seconds of sending — comfortably inside the budget, and the
// reason a per-minute tick can clear a week's backlog without ever asking the
// provider for more than it will give. A larger batch would not send faster; it
// would only make a run more likely to be cut off holding an accepted send it
// had not recorded.
//
// Neither bound limits how much backlog the pipeline can work through. What they
// bound is one HTTP request.
const (
	digestDrainBatch  = 50
	digestDrainBudget = 45 * time.Second
)

// digestClaimLease is how long a claimed Digest is hidden from other claimants.
//
// It bounds what nothing else can. Every ordinary ending rewrites
// `next_attempt_at` within seconds — a send writes its outcome, a failure writes
// its backoff — so what is left to the lease is the ending that writes nothing
// at all: an instance that died between claiming a Digest and recording what
// became of it.
//
// Five minutes, as the Reversal Reconciler's is. Long enough that no healthy
// send can outlive it, and short enough that a crashed instance costs a Customer
// minutes rather than their week.
const digestClaimLease = 5 * time.Minute

// digestMaxAttempts is how many times delivery is tried before the platform
// gives up on a Digest and records it `failed`.
//
// GIVING UP IS CORRECT HERE, and it is the opposite of the Reversal Reconciler's
// posture, which never stops caring what became of somebody's money. A Digest is
// about the week it names. One that could not be delivered inside that week has
// nothing left to be, and retrying it into the next week would deliver a stale
// email AND consume the Events it carried out of the ledger — so the following
// week's real Digest would be the poorer for a message nobody wanted.
//
// Five attempts on the backoff below spans a little over two hours, which
// outlasts every provider outage this platform has seen and is comfortably
// inside the day the Digest is about.
const digestMaxAttempts = 5

// digestRetryBackoff is how long a failed Digest waits before the next attempt,
// indexed by how many attempts have already been made.
//
// It starts at a minute rather than at seconds because the failures this
// pipeline actually meets are the provider's — a rate limit, a 5xx, a timeout —
// and none of them is over in ten seconds. It also starts at a minute because
// that is the drain's own cadence: a shorter delay would only mean the next tick
// finds the Digest due, which is what a minute already means.
//
// The last entry stands for every attempt beyond it, though digestMaxAttempts
// stops the run before it is reached twice.
var digestRetryBackoff = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	45 * time.Minute,
	90 * time.Minute,
}

// EnqueueWeek creates one pending Digest for every eligible Customer for the
// week the clock currently falls in.
//
// WHICH WEEK IS NOT THE CALLER'S TO SAY, and that is a deliberate refusal rather
// than an omission. The endpoint takes no body and no path parameter, for the
// reason every internal route takes none (server.registerInternalRoutes): a
// caller who could name a week could re-enqueue an old one and mail every
// Customer on the platform a second time. The week comes from the clock, through
// the one function that defines it (platform.DigestWeekStart).
//
// It is safe to call at any time and repeatedly. A second call in one week
// inserts nothing — the uniqueness on (customer_id, week_start) refuses it — and
// reports what was already there.
func (s *Service) EnqueueWeek(ctx context.Context) (*EnqueueResult, error) {
	now := s.now().UTC()
	weekStart := platform.DigestWeekStart(now)

	eligible, enqueued, err := s.repo.EnqueueWeek(ctx, weekStart, now)
	if err != nil {
		return nil, err
	}

	return &EnqueueResult{
		WeekStart:       weekStart.Format(time.DateOnly),
		Eligible:        eligible,
		Enqueued:        enqueued,
		AlreadyEnqueued: eligible - enqueued,
	}, nil
}

// DrainDigests composes and delivers the pending Digests that are due, one at a
// time, until the queue is empty or this run reaches its bound.
//
// ONE AT A TIME is the shape, and it is what makes the bounds honest — the same
// argument ReconcileReversalRequests makes. Claiming a whole batch up front and
// then stopping at the deadline would leave the unsent remainder leased and
// invisible until the lease expired, so a run that stopped early would have made
// those Customers LATER by stopping. Claiming as it goes means whatever a run
// did not reach is exactly as due as it was found.
//
// Errors from one Digest never stop the run. A backlog exists precisely when
// something is unwell, so one Customer's failure must not cost every other
// Customer their week; each is logged, counted, and left retryable.
func (s *Service) DrainDigests(ctx context.Context) (*DrainResult, error) {
	var out DrainResult
	deadline := s.now().Add(digestDrainBudget)

	for out.Claimed < s.batch() {
		// The caller's context is checked as well as the budget: a client that hung
		// up, or an instance being shut down, must not be answered by starting
		// another send.
		if ctx.Err() != nil {
			break
		}
		now := s.now()
		if !now.Before(deadline) {
			break
		}

		pending, err := s.repo.ClaimDueDigest(ctx, now, now.Add(digestClaimLease))
		if err != nil {
			return nil, err
		}
		if pending == nil {
			// Nothing is due. The ordinary answer, and the reason this endpoint is
			// safe to call every minute of every week: an empty queue costs one
			// query.
			break
		}
		out.Claimed++

		switch outcome, err := s.deliverDigest(ctx, *pending); {
		case err != nil:
			s.recordFailure(ctx, *pending, err, &out)
		case outcome == digestOutcomeEmpty:
			out.Empty++
		default:
			out.Sent++
		}
	}

	// Read once, at the end, so it reflects what this run left behind rather than
	// what it found. A failure to read it is reported the same way the
	// Reconciler's is: the tally describes work that actually happened, and losing
	// that because a count failed would be worse than answering without the count.
	total, oldest, err := s.repo.CountPendingDigests(ctx)
	if err != nil {
		s.logger.Warn("could not count the pending Follow Digest backlog for the drain response; what the run did is reported without it",
			"error", err,
		)
		return &out, nil
	}
	out.PendingTotal = total
	if oldest.Valid {
		out.OldestPendingWeek = oldest.Time.Format(time.DateOnly)
	}
	return &out, nil
}

// digestOutcome is what became of one claimed Digest.
type digestOutcome int

const (
	// digestOutcomeSent: a message was accepted by the provider and the
	// sent-ledger records everything it carried.
	digestOutcomeSent digestOutcome = iota
	// digestOutcomeEmpty: there was nothing to say, so nothing was sent and
	// nothing was written to the ledger.
	digestOutcomeEmpty
)

// deliverDigest composes one claimed Digest AT SEND TIME, sends it, and records
// it.
//
// COMPOSED HERE AND NOT AT ENQUEUE, which is the property the whole enqueue/drain
// split exists for. A Digest delayed an hour — by a backlog, a retry, a deploy —
// reflects the world as it is when it goes out, not as it was when the week was
// declared. An Event published in between is in it; one unpublished in between is
// not. If a future change snapshots the Events onto the queue row, that property
// disappears silently and nothing will fail.
//
// THE ORDER OF THE LAST TWO STEPS IS THE ONE DECISION IN THIS FUNCTION. The
// message is sent FIRST and the ledger written after, in one transaction with the
// status. The alternative — record, then send — would trade a duplicate email for
// an Event permanently invisible to a reader who was never shown it, since a
// ledger row is forever and nothing would ever notice. The residual risk is real
// and bounded: an instance dying between the accepted send and the commit leaves
// the Digest pending, and its retry sends a second copy. One Customer, one extra
// email, once.
//
// NOTHING IS SENT WHEN NOTHING MATCHED. The Digest is recorded `empty` and no
// message is composed at all — a Customer whose Follows matched nothing gets
// silence rather than an email that says so.
func (s *Service) deliverDigest(ctx context.Context, pending repository.PendingDigest) (digestOutcome, error) {
	recipient, err := s.repo.LoadRecipient(ctx, pending.CustomerID)
	if err != nil {
		return digestOutcomeEmpty, err
	}
	if recipient == nil {
		// The Customer was deleted between the claim and this read. There is
		// nobody to write to and nothing to retry.
		return digestOutcomeEmpty, s.repo.MarkDigestEmpty(ctx, pending.ID)
	}

	candidates, err := s.repo.DigestCandidates(ctx, pending.CustomerID, s.now().UTC())
	if err != nil {
		return digestOutcomeEmpty, err
	}
	if len(candidates) == 0 {
		return digestOutcomeEmpty, s.repo.MarkDigestEmpty(ctx, pending.ID)
	}

	locale := platform.DefaultLocale
	if parsed, ok := platform.ParseLocale(recipient.Locale); ok {
		locale = parsed
	}

	message, err := s.compose(ctx, *recipient, locale, candidates)
	if err != nil {
		return digestOutcomeEmpty, err
	}
	if err := s.email.SendFollowDigest(ctx, message); err != nil {
		return digestOutcomeEmpty, fmt.Errorf("send follow digest: %w", err)
	}

	eventIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		eventIDs = append(eventIDs, candidate.EventID)
	}
	if err := s.repo.MarkDigestSent(ctx, pending.ID, pending.CustomerID, eventIDs, s.now().UTC()); err != nil {
		// The message is already in the reader's inbox and this write is what
		// says so. Reported so the Digest stays pending and is retried, which
		// costs one duplicate email rather than a week of repeated Events — see
		// the ordering note above.
		return digestOutcomeSent, fmt.Errorf("record sent follow digest: %w", err)
	}
	return digestOutcomeSent, nil
}

// compose turns matched Events into the message a person reads, in their Digest
// Locale.
//
// All the language work that is not a sentence happens here in one pass: the Tag
// names are resolved for the whole Digest in ONE call to catalog rather than one
// per Event, because a reader Following four Tags across twenty Events would
// otherwise cost twenty queries to answer one question. The sentences themselves
// belong to the message (platform.FollowDigest.Text).
func (s *Service) compose(
	ctx context.Context,
	recipient repository.DigestRecipient,
	locale platform.Locale,
	candidates []repository.DigestCandidate,
) (platform.FollowDigest, error) {
	names, err := s.tagNames(ctx, candidates, locale)
	if err != nil {
		return platform.FollowDigest{}, err
	}

	events := make([]platform.FollowDigestEvent, 0, len(candidates))
	for _, candidate := range candidates {
		event := platform.FollowDigestEvent{
			Name:                candidate.Name,
			OrganizationName:    candidate.OrganizationName,
			URL:                 s.storefrontEventURL(candidate.OrganizationSlug, candidate.Slug),
			MatchedOrganization: candidate.MatchedOrganization,
		}
		if candidate.StartsAt.Valid {
			event.StartsAt = candidate.StartsAt.Time
		}
		if candidate.Timezone.Valid {
			event.Timezone = candidate.Timezone.String
		}
		for _, key := range candidate.MatchedTagKeys {
			if name, ok := names[key]; ok && name != "" {
				event.MatchedTagNames = append(event.MatchedTagNames, name)
			}
		}
		// Sorted so that an Event matching two Tags reads the same way every time
		// it is composed. The database's row order is not a promise, and a Digest
		// re-composed on a retry that listed the same reasons in a different order
		// would be a difference nobody could explain.
		sort.Strings(event.MatchedTagNames)
		events = append(events, event)
	}

	return platform.FollowDigest{
		To:           recipient.Email,
		CustomerName: recipient.FirstName,
		Locale:       locale,
		Events:       events,
	}, nil
}

// tagNames resolves every Tag named across a whole Digest in one call.
//
// A service with no resolver wired returns no names, and the Digest goes out
// without its Tag attributions rather than not at all — the degradation
// WithTags describes.
func (s *Service) tagNames(
	ctx context.Context,
	candidates []repository.DigestCandidate,
	locale platform.Locale,
) (map[string]string, error) {
	if s.tags == nil {
		return nil, nil
	}
	var keys []string
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		for _, key := range candidate.MatchedTagKeys {
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}
	return s.tags.LocalizedTagNames(ctx, keys, locale)
}

// storefrontEventURL builds {storefrontBase}/{orgSlug}/events/{eventSlug}, the
// same address an Affiliate Link points at (affiliates/service.storefrontURL).
//
// It carries no Locale segment, for the same reason that one does not: the
// Storefront resolves a language for an address that names none, and a Digest
// that hard-coded one would send a Spanish reader to an English page whenever
// the remembered Locale and the page's own resolution disagreed.
func (s *Service) storefrontEventURL(organizationSlug, eventSlug string) string {
	if s.storefrontBaseURL == "" {
		return ""
	}
	return s.storefrontBaseURL + "/" + organizationSlug + "/events/" + eventSlug
}

// recordFailure puts a failed Digest back on its backoff, or gives up on it once
// the attempts are spent.
//
// A failure to WRITE the failure is logged and swallowed. The claim lease is the
// fallback that covers exactly this, and one more failing statement per failing
// Digest is how a bad minute becomes a worse one.
func (s *Service) recordFailure(ctx context.Context, pending repository.PendingDigest, cause error, out *DrainResult) {
	if pending.AttemptCount >= digestMaxAttempts {
		out.GaveUp++
		s.logger.Warn("giving up on a Follow Digest after exhausting its attempts; this Customer gets no Digest for this week",
			"follow_digest_id", pending.ID,
			"customer_id", pending.CustomerID,
			"week_start", pending.WeekStart.Format(time.DateOnly),
			"attempt_count", pending.AttemptCount,
			"error", cause,
		)
		if err := s.repo.AbandonDigest(ctx, pending.ID, cause.Error()); err != nil {
			s.logger.Warn("could not record an abandoned Follow Digest; it stays claimed until its lease expires",
				"follow_digest_id", pending.ID, "error", err)
		}
		return
	}

	out.Retrying++
	s.logger.Warn("a Follow Digest could not be delivered; it goes back on the queue",
		"follow_digest_id", pending.ID,
		"customer_id", pending.CustomerID,
		"attempt_count", pending.AttemptCount,
		"error", cause,
	)
	if err := s.repo.RescheduleDigest(ctx, pending.ID, s.now().Add(retryDelay(pending.AttemptCount)), cause.Error()); err != nil {
		s.logger.Warn("could not reschedule a failed Follow Digest; it stays claimed until its lease expires",
			"follow_digest_id", pending.ID, "error", err)
	}
}

// retryDelay is the wait after the given number of attempts, saturating at the
// last step of the schedule.
func retryDelay(attempts int) time.Duration {
	index := attempts - 1
	if index < 0 {
		index = 0
	}
	if index >= len(digestRetryBackoff) {
		index = len(digestRetryBackoff) - 1
	}
	return digestRetryBackoff[index]
}

// batch is how many Digests one run may deliver, falling back to the constant
// when nothing has overridden it.
func (s *Service) batch() int {
	if s.drainBatch > 0 {
		return s.drainBatch
	}
	return digestDrainBatch
}
