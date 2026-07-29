package service

import (
	"context"
	"time"
)

// The Reversal Reconciler: the platform asking the Payment Provider again what
// became of a Reversal Request it never answered, with nobody watching — no
// Customer on the page, no browser open (ADR 0024, #158).
//
// It is the half of the recovery the opportunistic drain cannot be. That drain
// is a buyer loading their Customer Area, which is enough for a buyer who stays
// and useless for one who closes the tab — exactly the case the feature exists
// for. Nothing else in this backend runs outside a request, so this is driven by
// an authenticated internal endpoint a scheduler calls and a human can curl.
//
// It works two kinds of stuck request and the difference is invisible from here,
// because the queue hands them over the same way and resolveReversalRequest
// reads the row to see which is which: one has an unanswered question, and one
// has its answer already and a local write that failed after the money went back
// (finishAgreedReversal, #162). The second never reaches a Payment Provider at
// all.
//
// It ONLY EVER FINDS OUT; it never re-decides. Eligibility is not consulted here
// and must never be, for the reason given where the one evaluation of it lives
// (ReverseOwnSale). Nothing below has to enforce that: what a run does to a
// request is resolveReversalRequest, which is shared with the Customer Area drain
// and evaluates no rule of its own.

// ReversalDrainResult is what one run of the Reconciler did, and — because the
// endpoint is the incident runbook as much as it is the automation's entry
// point — what is still stuck afterwards.
//
// The tally is per OUTCOME rather than a single count, because "we pursued four
// requests" says nothing an operator can act on and "one reversed, one refused,
// two still unknown" says all of it.
type ReversalDrainResult struct {
	// Pursued is how many Reversal Requests this run claimed. Zero is the
	// ordinary answer: nothing was stuck.
	Pursued int `json:"pursued"`
	// Reversed, Refused, StillInFlight and GaveUp are where those requests ended.
	// They sum to Pursued alongside Skipped and Failed.
	Reversed      int `json:"reversed"`
	Refused       int `json:"refused"`
	StillInFlight int `json:"still_in_flight"`
	// GaveUp counts the requests THIS RUN turned into Unresolved Reversals. It is
	// the number worth alerting on, and it is not the size of the queue below.
	GaveUp int `json:"gave_up"`
	// Skipped is requests another actor held or had already settled — a buyer's
	// own press, a Customer Area drain, another instance. Not a failure: the claim
	// is handed straight back and the next tick takes it up.
	Skipped int `json:"skipped"`
	// Failed is requests whose pursuit errored — the database, or a local write
	// after the provider agreed. Each one logged its own line.
	Failed int `json:"failed"`
	// InFlightTotal is how many Reversal Requests are still open once this run
	// finished, and OldestInFlightRequestedAt is when the buyer at the front of
	// that queue pressed (RFC3339 in UTC, absent when there is nobody).
	//
	// They are the answer to "is this getting better or worse", and the reason
	// they are here rather than left to the Unresolved queue below is that the
	// queue below is EMPTY for the first 24 hours of any outage — the give-up
	// bound has not been reached — which is precisely the day an operator is
	// looking. The count says how big the backlog is and the timestamp says how
	// long it has been building, so two curls a minute apart answer the question
	// without a database session.
	InFlightTotal             int    `json:"in_flight_total"`
	OldestInFlightRequestedAt string `json:"oldest_in_flight_requested_at,omitempty"`
	// Unresolved is the standing Unresolved Reversal queue, oldest ask first,
	// capped at unresolvedQueuePageSize. It carries what the backlog above cannot
	// — the client transaction id and last error per row — because these are the
	// ones nobody is pursuing any more, and the next move on each is a human
	// typing something into the provider's dashboard.
	Unresolved []UnresolvedReversal `json:"unresolved"`
	// UnresolvedTotal is how long that queue really is, so a truncated page is
	// never read as the whole backlog.
	UnresolvedTotal int `json:"unresolved_total"`
}

// UnresolvedReversal is one Reversal Request the platform gave up on, as the
// Platform Operator who must settle it needs it: the reference the buyer would
// quote, the transaction id that finds it on the Payment Provider's dashboard,
// and what the provider was saying when the platform stopped asking.
//
// There is no Customer email here and no amount. This is a work queue for
// somebody with access to the provider's dashboard, not a rendering of somebody
// else's purchase, and the sale itself is one operator lookup away by reference.
type UnresolvedReversal struct {
	TicketSaleID        string `json:"ticket_sale_id"`
	ConfirmationRef     string `json:"confirmation_ref"`
	ClientTransactionID string `json:"client_transaction_id"`
	// RequestedAt is when the Customer pressed Undo, RFC3339 in UTC — the instant
	// the queue is ordered by, and how long this person has been waiting.
	RequestedAt  string `json:"requested_at"`
	AttemptCount int    `json:"attempt_count"`
	LastError    string `json:"last_error,omitempty"`
	// SaleStatus is where the Ticket Sale stands. `reversed` means somebody else
	// settled the sale while this ask was in flight, so nothing local is owed.
	// `active` means the sale still stands and the operator's move depends on
	// LastError beside it: usually nobody knows whether the money left, and the
	// dashboard has to say — but a reversal the provider agreed to and the
	// platform could not record says so in as many words, and there the money is
	// already gone and only the sale needs voiding (#162).
	SaleStatus string `json:"sale_status"`
}

// reversalDrainBatch and reversalDrainBudget bound one run.
//
// The BUDGET is the real bound and the count is the belt. A single probe can
// cost the full ten-second provider timeout, so a run bounded only by a count of
// fifty could ask for 500 seconds; a run cut off mid-probe loses the outcome of
// a call that may have moved somebody's money, because the write that records
// what the provider said fails on the very context that was cancelled.
//
// THE DEADLINE CHAIN, stated here once and pointed at from Terraform, because
// each term is worthless without the one outside it:
//
//	reversalDrainBudget  <  Cloud Scheduler's attempt_deadline  <  Cloud Run's
//	                                                               request timeout
//	         45s         <              90s                     <      300s
//
// The middle term is terraform's reversal_reconciler_attempt_deadline_seconds
// and the outer one its api_request_timeout_seconds; both variable descriptions
// name this comment rather than repeat it. Whichever is smallest is what
// actually stops a run, and only the innermost one stops it politely — the other
// two abandon the request where it stands, and the write recording what the
// Payment Provider just said fails on the very context that was cancelled,
// losing the answer to a call somebody's money has already paid for. So the
// budget must stay strictly smallest, with room for a probe already in flight
// when it expires (45 + 10 < 90). A change to any of the three that does not
// meet the others is caught by
// TestTheDrainBudgetIsStrictlyInsideTheSchedulerDeadline, which reads both
// Terraform defaults rather than mirroring them.
//
// Forty-five seconds is deliberately short, and it costs nothing. The tick is
// every minute, so a run that stops at its budget is followed by another one
// about fifteen seconds later; and because this loop claims ONE request at a
// time and probes it before claiming the next (see below), stopping leaves
// nothing leased and invisible — everything unreached is exactly as due as it
// was found. A long budget would buy only the ability to work a backlog inside
// one HTTP request instead of across several, which no one is waiting on.
//
// Neither bound limits how much backlog the platform can work through. What they
// bound is one HTTP request.
const (
	reversalDrainBatch  = 50
	reversalDrainBudget = 45 * time.Second
)

// reversalClaimLease is how long a claimed Reversal Request is hidden from other
// claimants and from the Customer Area drain.
//
// It bounds what nothing else can. An ordinary ending overwrites next_attempt_at
// within seconds — a probe writes the backoff its outcome earned, and a run that
// could not probe at all hands the claim back (releaseReversalClaim) — so what is
// left to it is every ending that writes nothing at all: an instance that died
// between claiming a request and recording what it learned, and a pursuit that
// failed on the database itself, which is the one case handing the claim back
// cannot be attempted (see the error branch in ReconcileReversalRequests).
//
// Five minutes is long enough that no healthy probe can outlive it and short
// enough that either of those costs a buyer minutes rather than the half hour the
// late backoff would.
const reversalClaimLease = 5 * time.Minute

// reversalContendedRetryDelay is when a claim comes back due after a run claimed
// a Reversal Request and could not probe it — another actor held the sale, or
// the row had moved on under it.
//
// Contention must cost about one tick, never the lease. Being busy is not an
// outcome the backoff is a punishment for: nothing was asked of the provider, so
// there is nothing to back off from, and leaving the lease standing would delay
// a buyer's refund by five minutes for the crime of two actors arriving at once
// — which is likeliest exactly when a backlog makes runs overlap.
//
// HALF the scheduler's cadence rather than all of it, so that "the next tick
// picks it up" is literally true. A delay equal to the cadence is the value that
// looks right and misses: the release is written partway through a run, so the
// row comes due a few seconds AFTER the next tick has already claimed its way
// past that instant, and the request waits two ticks for a delay that reads as
// one. Half a tick comes due while the next run is still ahead of it.
//
// It is not derived from terraform's reversal_reconciler_schedule, and cannot
// usefully be: the cadence is a cron string in another language, and a deployment
// that slows the tick to five minutes wants a contended request retried sooner
// than the tick, not later. What this number promises is "well under one tick",
// which holds for every cadence at or above a minute.
const reversalContendedRetryDelay = 30 * time.Second

// reversalClaimReleaseTimeout bounds handing a claim back. It is short for the
// same reason the advisory unlock's is: the statement is trivial, and the
// alternative to giving up is holding a connection open on a database that is
// not answering. Failing it costs the lease, which is what the lease is for.
const reversalClaimReleaseTimeout = 5 * time.Second

// unresolvedQueuePageSize caps the Unresolved Reversal queue carried in a drain
// response. Twenty is a screen an operator reads; the total beside it is what
// says whether there are more.
const unresolvedQueuePageSize = 20

// ReconcileReversalRequests pursues the Reversal Requests that are due, one at a
// time, until the queue is empty or this run reaches its bound.
//
// ONE AT A TIME is the shape, and it is what makes the bounds honest. Claiming a
// whole batch up front and then stopping at a deadline would leave the unprobed
// remainder of that batch leased and invisible to everybody until the lease
// expired — a run that stopped early would have made those requests LATER by
// stopping. Claiming as it goes means a run that stops leaves everything it did
// not reach exactly as due as it found it.
//
// A contended request is skipped rather than waited for, and its claim is handed
// straight back. The per-sale advisory lock is held by whoever is probing that
// sale — a buyer's own press, a Customer Area load, another instance — and
// queuing behind a ten-second provider call for a sale somebody else is already
// handling is this run spending its budget on work that is already happening.
//
// Errors from one request never stop the run. A backlog exists precisely when
// something is unwell, so a single failure must not cost every other stuck buyer
// their turn; each is logged and counted, and the summary returns.
func (s *Service) ReconcileReversalRequests(ctx context.Context) (*ReversalDrainResult, error) {
	var out ReversalDrainResult
	deadline := s.now().Add(reversalDrainBudget)

	for out.Pursued < s.reversalDrainBatch() {
		// The caller's context is checked as well as the budget: a client that hung
		// up, or a Cloud Run instance being shut down, must not be answered by
		// starting another ten-second call to a Payment Provider.
		if ctx.Err() != nil {
			break
		}
		now := s.now()
		if !now.Before(deadline) {
			break
		}

		leaseUntil := now.Add(reversalClaimLease)
		request, err := s.repo.ClaimDueReversalRequest(ctx, now, leaseUntil)
		if err != nil {
			return nil, err
		}
		if request == nil {
			// Nothing is due. This is the ordinary answer and the reason the endpoint
			// is safe to call by hand at any time: an empty queue costs one query.
			break
		}
		out.Pursued++

		outcome, err := s.resolveReversalRequest(ctx, *request)
		if err != nil {
			// Where the request now stands is deliberately not asserted, for the
			// reason the Customer Area drain's own line gives
			// (ResolveInFlightReversalRequests).
			//
			// The claim is not handed back either. A failure here is the database or
			// a local write, and ReleaseReversalRequestClaim is another write to the
			// same database; the claim lease is the fallback that covers exactly this,
			// and one more failing statement per failing request is how a bad minute
			// becomes a worse one.
			s.logger.Warn("the Reversal Reconciler could not finish pursuing a Reversal Request; whatever the probe learned is recorded on the request row",
				"ticket_sale_id", request.TicketSaleID,
				"reversal_request_id", request.ID,
				"error", err,
			)
			out.Failed++
			continue
		}
		switch outcome {
		case reversalOutcomeReversed:
			out.Reversed++
		case reversalOutcomeRefused:
			out.Refused++
		case reversalOutcomeUnresolved:
			out.GaveUp++
		case reversalOutcomeSkipped:
			out.Skipped++
			s.releaseReversalClaim(ctx, request.ID, leaseUntil)
		default:
			out.StillInFlight++
		}
	}

	// Both standings are read once, at the end, so they reflect what this run
	// left behind rather than what it found.
	//
	// The backlog is read first because it is the one an incident is about. A
	// failure to read it is reported the same way the queue's is — the tally
	// describes work that actually happened, and losing that because a count
	// failed would be worse than answering without the count.
	inFlight, oldest, err := s.repo.CountInFlightReversalRequests(ctx)
	if err != nil {
		s.logger.Warn("could not count the in-flight Reversal Request backlog for the drain response; what the run did is reported without it",
			"error", err,
		)
	} else {
		out.InFlightTotal = inFlight
		if oldest.Valid {
			out.OldestInFlightRequestedAt = oldest.Time.UTC().Format(time.RFC3339)
		}
	}

	unresolved, total, err := s.repo.ListUnresolvedReversals(ctx, unresolvedQueuePageSize)
	if err != nil {
		// The operator can still SELECT the table.
		s.logger.Warn("could not read the Unresolved Reversal queue for the drain response; what the run did is reported without it",
			"error", err,
		)
		return &out, nil
	}
	out.UnresolvedTotal = total
	out.Unresolved = make([]UnresolvedReversal, 0, len(unresolved))
	for _, u := range unresolved {
		out.Unresolved = append(out.Unresolved, UnresolvedReversal{
			TicketSaleID:        u.TicketSaleID,
			ConfirmationRef:     u.ConfirmationRef,
			ClientTransactionID: u.ClientTransactionID,
			RequestedAt:         u.RequestedAt.UTC().Format(time.RFC3339),
			AttemptCount:        u.AttemptCount,
			LastError:           u.LastError.String,
			SaleStatus:          u.SaleStatus,
		})
	}
	return &out, nil
}

// releaseReversalClaim gives a claimed Reversal Request back to the queue,
// undoing the lease this run took over it so that being busy costs one tick
// rather than five minutes (reversalContendedRetryDelay).
//
// The repository refuses to write unless the row is still unfinished — in flight,
// or succeeded with its commit outstanding, the two states a claim can cover
// since #162 — AND still carries the exact lease this run wrote. The lease is the
// half that matters: a probe that finished in the meantime has already written its
// own schedule, and this must never overwrite an answer somebody paid a provider
// call for.
//
// The write does not inherit the caller's cancellation, for the same reason the
// advisory unlock does not. The context most likely to be dead here is the one
// where handing the claim back matters most — an instance being shut down mid
// run, which would otherwise leave every request it claimed leased for the crash
// bound.
//
// A failure is logged and never returned: the lease is the fallback, so the
// worst case is the delay this exists to avoid, and it must not cost the rest of
// the run.
func (s *Service) releaseReversalClaim(ctx context.Context, requestID string, leaseUntil time.Time) {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reversalClaimReleaseTimeout)
	defer cancel()
	if err := s.repo.ReleaseReversalRequestClaim(
		releaseCtx, requestID, leaseUntil, s.now().Add(reversalContendedRetryDelay),
	); err != nil {
		s.logger.Warn("could not hand a claimed Reversal Request back to the queue; it stays leased until the claim bound expires",
			"reversal_request_id", requestID,
			"error", err,
		)
	}
}

// reversalDrainBatch is how many requests one run may pursue, falling back to
// the constant when nothing has overridden it.
func (s *Service) reversalDrainBatch() int {
	if s.drainBatch > 0 {
		return s.drainBatch
	}
	return reversalDrainBatch
}

// WithReversalDrainBatch narrows how many Reversal Requests one run pursues.
//
// It exists so a test can prove the bound is a bound. Boundedness is a property
// of the loop and not of the number, and reaching the default fifty would mean
// staging fifty stuck purchases — a test that slow gets deleted, and the
// guarantee it protects is that a backlog can never wedge the endpoint.
//
// Nothing in production calls it: the deployed bound is the constant above,
// which is the one measured against Cloud Run's request timeout.
func (s *Service) WithReversalDrainBatch(batch int) *Service {
	s.drainBatch = batch
	return s
}
