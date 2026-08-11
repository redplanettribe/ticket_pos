package service

import (
	"context"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// SaleReversalResult is what the Customer is told after asking to undo a
// purchase: the sale they asked about, still identified by the Sale Confirmation
// reference on their receipt, and where the ask has got to.
//
// The reference is deliberately still here. A reversed Ticket Sale is never
// deleted — it keeps its reference and stays visible to both the Customer and
// the Organization — so the thing a buyer would quote in a support thread is the
// same thing before and after.
//
// Two of the three timestamps a reversal involves never appear together, because
// only one of them has happened. A completed reversal carries ReversedAt, the
// moment the platform learned the money went back; a Reversal Request still in
// flight carries RequestedAt, the moment the buyer pressed. Publishing the
// absent one as an empty string would invite a client to draw a time for
// something that has not occurred, so each is omitted on the other's path.
type SaleReversalResult struct {
	TicketSaleID    string `json:"ticket_sale_id"`
	ConfirmationRef string `json:"confirmation_ref"`
	// Status is "reversed" when the purchase is undone, and "pending" while a
	// Reversal Request is still in flight (ADR 0024).
	//
	// "pending" is NOT a softer failure. It says the platform is still finding
	// out what the Payment Provider did, so the Ticket Sale is still active and
	// the buyer's tickets are still valid — a reversal that was definitely
	// refused is an error, never a result carrying a word.
	Status string `json:"status"`
	// ReversedAt is when the reversal committed, RFC3339 in UTC. Present only on
	// "reversed".
	ReversedAt string `json:"reversed_at,omitempty"`
	// RequestedAt is when the Customer pressed Undo, RFC3339 in UTC. Present only
	// on "pending", and it is the instant of the ORIGINAL ask: a buyer who
	// presses again while the platform is still finding out is told when their
	// one request was made, not when they pressed the second time.
	RequestedAt string `json:"requested_at,omitempty"`
}

// SaleReversalStatusReversed and SaleReversalStatusPending are the two words the
// result can carry. The handler chooses 200 or 202 from them, so the split
// between "done" and "still finding out" is decided once, here, rather than
// re-derived from whichever field happens to be set.
const (
	SaleReversalStatusReversed = "reversed"
	SaleReversalStatusPending  = "pending"
)

// Pending reports whether this result describes a Reversal Request still in
// flight rather than a completed reversal. It exists so the HTTP layer never has
// to compare status strings of its own.
func (r SaleReversalResult) Pending() bool {
	return r.Status == SaleReversalStatusPending
}

// ReverseOwnSale is a Customer undoing their own Online Sale within the Reversal
// Window (ADR 0018), recorded as a Reversal Request the platform pursues to a
// definite answer (ADR 0024).
//
// customerID is the authorization and the only one there is. It comes from a
// Customer Session — never from a Confirmation Link, which travels by email and
// can be forwarded, and never from anything in the request body — and the sale
// is loaded scoped to it, so a sale belonging to somebody else is not refused so
// much as invisible.
//
// The order of the checks below is not arbitrary. Everything that can refuse
// without touching anything runs first, so a refusal is always a no-op: the
// Ticket Sale, the Ticket Types' capacity and the buyer's inbox are all exactly
// as they were. Only then is the ask recorded and the Payment Provider called,
// and only on its agreement is the sale voided. The email goes last of all,
// after the reversal has committed, because a buyer must never be told their
// tickets are gone while the transaction that removes them can still roll back.
//
// The whole attempt is serialised per Ticket Sale, and the serialisation spans
// the provider call rather than merely the local write. See the lock below: a
// double-press that gets past this point twice returns somebody's money twice.
//
// What ADR 0024 changed is the answer to silence: the Reversal Request is
// written before the provider is called, so silence leaves something to come
// back to (see THE ORDER below). Every DEFINITE answer behaves exactly as it did
// before — a prompt success is still an instant undo, and a refusal is still an
// immediate error with nothing changed.
func (s *Service) ReverseOwnSale(ctx context.Context, customerID, ticketSaleID string) (*SaleReversalResult, error) {
	// The ownership read comes FIRST, before the lock. The lock is keyed on a
	// Ticket Sale id, and a caller who does not own that sale must not be able to
	// take it: contending for a stranger's lock would let anyone make a sale they
	// cannot see report itself already reversed to the buyer who can.
	sale, err := s.repo.GetCustomerTicketSale(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	if sale == nil {
		return nil, sales.ErrTicketSaleNotFound()
	}

	// One reversal attempt at a time per Ticket Sale, across every server process,
	// held from here until this function returns — the provider call included.
	//
	// Without it, two presses of one button both read an active sale, both pass
	// every check below, and both POST the reversal to the Payment Provider; only
	// afterwards does one lose the row lock on the local write. Capacity would
	// still be correct and the buyer would still see one 200 — and the money would
	// have been returned twice. A reversal is never called twice on one ask, for
	// exactly that reason (ADR 0018), so a concurrent second call is no more
	// acceptable than a retried one.
	//
	// A contended lock is not an error to report as one. It means another attempt
	// on this very sale is in flight, so the honest answer is the one a request
	// arriving a moment later gets: this purchase has already been undone. That
	// answer is occasionally premature — the attempt in flight may yet be refused
	// by the provider — and it is still the right one to give, because a buyer who
	// pressed twice has one reversal happening and no second one to make. Inventing
	// a "try again" code would invite exactly the retry that must not happen.
	//
	// Note what this is NOT. A press that arrives after a Reversal Request was
	// left in flight finds the lock free — the previous request released it when
	// it answered 202 — and is answered from the request row further down, which
	// is a read. Contention here means two presses genuinely overlapping inside
	// one provider call.
	release, locked, err := s.repo.LockTicketSaleForReversal(ctx, sale.ID)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, sales.ErrSaleAlreadyReversed()
	}
	defer func() {
		if err := release(); err != nil {
			s.logger.Error("could not release the Sale Reversal lock; further reversals of this Ticket Sale may be refused until the connection is recycled",
				"ticket_sale_id", sale.ID,
				"error", err,
			)
		}
	}()

	// Re-read under the lock. The row that decided anything must be the row as it
	// stands now that nobody else can be acting on it: the read above may have
	// happened while another attempt was mid-reversal, and a stale 'active' is
	// precisely how a second reversal reaches the provider. The loser of the race
	// re-reads a reversed sale here and answers correctly.
	sale, err = s.repo.GetCustomerTicketSale(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	if sale == nil {
		return nil, sales.ErrTicketSaleNotFound()
	}

	// A live Reversal Request is answered, not repeated. The buyer's ask exists,
	// the platform is pursuing it, and there is no second one to make: pressing
	// again produces no new row and — the part that matters — no second call to
	// the Payment Provider, which could return the money twice.
	//
	// LIVE and not merely in-flight, because live is what the database will let
	// this path write past (repository.GetLiveReversalRequest). A `succeeded`
	// request whose local commit failed, or a `needs_attention` one, is a row this
	// sale already has: reading it as absent would send the press on to
	// CreateReversalRequest and answer the buyer a unique violation.
	//
	// Whether it is pending is asked of the SALE and not of the request. A live
	// request over a sale that still stands is a reversal in progress; once the
	// sale is reversed the request is the reversal that happened, and the buyer
	// pressing again is simply late — an answer the eligibility check below
	// already gives, correctly, as SALE_ALREADY_REVERSED. Falling through to it is
	// how this path avoids inventing a second way to say the same thing.
	//
	// It is asked BEFORE eligibility, and that order is load-bearing. The Reversal
	// Window governs when a Customer may ask; it says nothing about how long the
	// platform may take to find out what happened (ADR 0024). A buyer who pressed
	// at 19:58 and presses again at 20:05 must be told their refund is being
	// processed, not that they are too late for the request they already made.
	live, err := s.repo.GetLiveReversalRequest(ctx, sale.ID)
	if err != nil {
		return nil, err
	}
	if live != nil && sale.Status == platform.ActiveSaleStatus {
		// An Unresolved Reversal is live to the database and finished to the
		// platform, so it is the one live request a press is not answered "pending"
		// on: the platform stopped asking with the money's fate unknown, and
		// telling this buyer their refund is being processed would be a promise
		// nobody can keep. It cannot fall through to a new ask either — that is a
		// second reversal posted on a payment that may already have been reversed.
		if live.Status == sales.ReversalRequestNeedsAttention {
			return nil, sales.ErrReversalUnresolved(sale.ConfirmationRef)
		}
		return pendingResult(sale, live), nil
	}

	// The four checks, asked of the same shared rule the Customer Area asked
	// before it offered the button (platform.PaymentReversal.EligibilityAt). The
	// enforcing side maps each refusal to its own error; the deciding is not
	// re-implemented here, because an endpoint that refuses what the offer
	// promised is the bug that separating them would produce.
	//
	// This is the ONLY place a reversal's eligibility is ever evaluated. Writing
	// the Reversal Request below is the record of the authorisation, and every
	// later probe continues that authorised ask rather than making a new one — a
	// retry that re-checked the Window would refuse its own continuation minutes
	// later and permanently abandon the case where the money has already left.
	now := s.now()
	_, refusal := platform.NewPaymentReversal(s.provider).EligibilityAt(sale.ReversalFacts(), now)
	if err := reversalRefusalError(refusal); err != nil {
		return nil, err
	}

	// A free claim has no provider to ask and no ask to record: no money was
	// collected, by anyone, so there is nothing anybody could be silent about.
	// It stays fully synchronous, exactly as it has always been, and creates no
	// Reversal Request at all (ADR 0024).
	//
	// Nothing else may skip the provider: the branch is on the Payment Method the
	// sale was settled under, not on convenience.
	if sale.PaymentMethod.String == platform.FreePaymentMethod {
		return s.commitSaleReversal(ctx, sale, nil, now)
	}

	// A paid Online Sale with no Payment to name is not a reversal anybody can
	// make. The id below is what the Payment Provider is keyed on, and a Reversal
	// Request may not be written without one: the column refuses an empty string
	// precisely so a row can never stand for an ask about no transaction.
	//
	// It should not be reachable — every Online Sale settles through a Payment —
	// so this is the shape of a bug elsewhere, and it is answered as the buyer's
	// undo failing rather than as a constraint violation, which is what the honest
	// 502 said before ADR 0024 and what it should keep saying.
	if sale.ClientTransactionID == "" {
		s.logger.Error("a paid Online Sale carries no Payment to reverse; no Reversal Request can be made for it",
			"ticket_sale_id", sale.ID,
			"confirmation_ref", sale.ConfirmationRef,
			"payment_method", sale.PaymentMethod.String,
		)
		return nil, sales.ErrSaleReversalFailed(sale.ConfirmationRef)
	}

	// THE ORDER: the ask is recorded BEFORE the provider is called, and this is
	// the whole point of ADR 0024. Until this row exists, a provider that goes
	// silent teaches the platform nothing — ADR 0018 wrote nothing first, so a
	// timeout left the buyer with a false error and the Ticket Sale active with
	// its capacity held while the money may already have gone. Every other mention
	// of this rule in the codebase points here; this is the line that enacts it,
	// and moving the call above it is how the feature would be silently undone.
	//
	// It changes nothing about the money or the tickets. The sale stays active and
	// keeps counting in every aggregate, because nothing is known to have
	// happened yet.
	request, err := s.repo.CreateReversalRequest(ctx, repository.CreateReversalRequestInput{
		TicketSaleID:        sale.ID,
		ClientTransactionID: sale.ClientTransactionID,
		RequestedAt:         now,
	})
	if err != nil {
		return nil, err
	}

	result, outcome, err := s.pursueReversalRequest(ctx, sale, request)
	if err != nil {
		return nil, err
	}
	if outcome == reversalOutcomeRefused {
		// The buyer pressed and the provider said no. They get the same immediate,
		// honest error they have always got, with nothing changed.
		return nil, sales.ErrSaleReversalFailed(sale.ConfirmationRef)
	}
	return result, nil
}

// ResolveInFlightReversalRequests asks the Payment Provider again about every
// Reversal Request the given Customer has left in flight, and applies whatever
// it answers.
//
// This is the opportunistic drain, and it is what stops the pending state from
// being a regression. Before ADR 0024 the recovery mechanism for a timed-out
// reversal was the buyer pressing Undo again; now that a second press is
// answered from the request row, something else has to do the asking. The
// Reversal Reconciler (reconciler.go) is what pursues a stuck request when
// nobody is watching; this is the accelerator, so the common case — a buyer who
// stayed on the page — still resolves in seconds rather than on a tick.
//
// It never evaluates eligibility, and neither does anything it calls. A Reversal
// Request that was inside the Reversal Window when it was made stays authorised
// however long the answer takes; the reason that rule cannot be softened is
// beside the one place the Window is ever evaluated (ReverseOwnSale).
//
// It pursues only requests that are DUE, and at most maxOpportunisticDrain of
// them. A probe costs a ten-second provider timeout and they run one after
// another inside the page load that triggered them, so a drain with no throttle
// is a Customer refreshing during a provider outage re-posting Reverse forever
// and hanging their own Area doing it.
//
// customerID scopes it to the caller's own asks and comes from the Customer
// Session. Errors are the caller's to log and swallow: a provider that is still
// unwell must not stop a Customer reading their purchases.
func (s *Service) ResolveInFlightReversalRequests(ctx context.Context, customerID string) error {
	requests, err := s.repo.ListDueReversalRequestsForCustomer(ctx, customerID, s.now(), maxOpportunisticDrain)
	if err != nil {
		return err
	}
	for _, request := range requests {
		if _, err := s.resolveReversalRequest(ctx, request); err != nil {
			// One stuck request must not stop the next one being asked about, and
			// none of this is the reader's problem.
			//
			// The line says what failed and NOT where the request now stands, because
			// this caller cannot know: a probe that never got an answer leaves it in
			// flight, while one that got a success and then failed to void the sale
			// leaves it succeeded over an active sale — SALE_REVERSAL_NOT_COMMITTED,
			// which logs its own line saying so. Asserting "it stays in flight" here
			// would state the opposite of the truth in exactly the case an operator
			// is woken for.
			s.logger.Warn("could not resolve a Reversal Request while loading the Customer Area; whatever the probe learned is recorded on the request row",
				"ticket_sale_id", request.TicketSaleID,
				"reversal_request_id", request.ID,
				"error", err,
			)
		}
	}
	return nil
}

// maxOpportunisticDrain bounds how many Reversal Requests one Customer Area load
// may pursue.
//
// It is ONE, and the number is the whole of the throttle that matters. Each
// pursuit runs serially inside the buyer's own GET and can cost the full
// provider timeout, so this constant is measured in seconds added to a page
// load, not in rows: at three, a buyer with three stuck asks and an unwell
// PayPhone waits half a minute for their own Customer Area to render, which is
// a worse thing to do to them than leaving the second ask until the next render.
//
// One also matches the case that actually occurs. A buyer has one stuck Reversal
// Request; more than one means PayPhone has been unwell for a while, and that is
// the Reversal Reconciler's problem to work through in the background, not
// something to make the buyer's page pay for. Requests beyond the limit are the
// NEWER ones — the query returns oldest first — and the next render takes them up.
const maxOpportunisticDrain = 1

// reversalOutcome is where one pursuit of a Reversal Request left it. It exists
// because the two callers owe the answer to different people: a buyer who just
// pressed is owed an error or a result, while a drain is owed a tally of what it
// learned — and a refusal, which is bad news for the buyer, is a SUCCESS for a
// drain whose whole job is to find out.
type reversalOutcome string

const (
	// reversalOutcomeReversed: the provider agreed and the Ticket Sale is voided.
	reversalOutcomeReversed reversalOutcome = "reversed"
	// reversalOutcomeRefused: the provider considered it and said no. Nothing
	// happened and the ask is over.
	reversalOutcomeRefused reversalOutcome = "refused"
	// reversalOutcomeInFlight: still no definite answer. The question stays open
	// and the request comes due again after the backoff.
	reversalOutcomeInFlight reversalOutcome = "in_flight"
	// reversalOutcomeUnresolved: the platform gave up. An Unresolved Reversal,
	// awaiting a Platform Operator.
	reversalOutcomeUnresolved reversalOutcome = "unresolved"
	// reversalOutcomeSkipped: nothing was asked of the provider, because another
	// actor holds the sale or had already settled the request. Not a failure —
	// the next pass takes it up.
	reversalOutcomeSkipped reversalOutcome = "skipped"
)

// resolveReversalRequest carries one unfinished Reversal Request a step further
// under the same advisory lock a press takes, so the buyer's press, the Customer
// Area drain and the Reversal Reconciler can never overlap on one sale and reach
// the provider together.
//
// The row's own status says which of the two jobs is left, and they are not
// variations of one thing. An `in_flight` request has an unanswered question and
// the work is to ASK the Payment Provider. A `succeeded` one has its answer
// already and the work is to finish the LOCAL COMMIT that failed after the money
// went back — a job with no provider in it at all (finishAgreedReversal, #162).
//
// The request AND the sale are re-read under the lock before anything is asked.
// Whatever produced the request read it outside the lock, and neither a request
// another actor has just settled nor a sale another actor has just reversed may
// be probed: that second probe is a second call about somebody's money.
//
// It takes no Customer, and that is deliberate rather than an omission. Both
// callers arrive with a request the PLATFORM recorded, and pursuing it is only
// ever finding out what the provider did with an ask that was authorised once,
// when it was written. The Customer Area's drain is scoped where scope is
// authorization — its list query joins on the session's own Customer — and the
// Reconciler has no Customer to scope by at all.
func (s *Service) resolveReversalRequest(ctx context.Context, request repository.ReversalRequest) (reversalOutcome, error) {
	release, locked, err := s.repo.LockTicketSaleForReversal(ctx, request.TicketSaleID)
	if err != nil {
		return reversalOutcomeSkipped, err
	}
	if !locked {
		// Somebody else is already working this very sale, so this pass does not
		// queue behind them: the request keeps its status and the next pass — a page
		// load, or the next tick — takes it up. What contention costs, and why it is
		// a tick rather than the claim lease, is reversalContendedRetryDelay.
		return s.ageUnaskableReversalRequest(ctx, request,
			"the sale was held by another reversal attempt every time the platform tried to ask")
	}
	defer func() {
		if err := release(); err != nil {
			s.logger.Error("could not release the Sale Reversal lock after resolving a Reversal Request; further reversals of this Ticket Sale may be refused until the connection is recycled",
				"ticket_sale_id", request.TicketSaleID,
				"error", err,
			)
		}
	}()

	current, err := s.repo.GetLiveReversalRequest(ctx, request.TicketSaleID)
	if err != nil {
		return reversalOutcomeSkipped, err
	}
	if current == nil || current.ID != request.ID {
		return reversalOutcomeSkipped, nil
	}
	if current.Status == sales.ReversalRequestSucceeded {
		return s.finishAgreedReversal(ctx, *current)
	}
	if current.Status != sales.ReversalRequestInFlight {
		return reversalOutcomeSkipped, nil
	}

	sale, err := s.repo.GetTicketSaleForReversal(ctx, request.TicketSaleID)
	if err != nil {
		return reversalOutcomeSkipped, err
	}
	if sale == nil {
		// The sale is gone. Nothing can be reversed and nothing should be asked of
		// the provider on its behalf.
		return s.ageUnaskableReversalRequest(ctx, request,
			"the Ticket Sale this request was made about could not be found")
	}

	// THE SALE MUST STILL BE ACTIVE BEFORE THE PROVIDER IS ASKED ANYTHING. A
	// Reversal Request in flight is a question about money the platform believes
	// is still collected; if the sale has been settled out from under it — an
	// Operator Reversal (ADR 0019), a Sale Import undo — then somebody has already
	// given this buyer their money back by hand, and posting Reverse now is how
	// they get it twice.
	//
	// The reason this cannot be probed "just to check" is that the answer is
	// indistinguishable from the act. An Operator who refunded through the
	// provider's own dashboard would answer errorCode 24 and cost nothing; one who
	// refunded by BANK TRANSFER left the provider's transaction live, so the same
	// probe would reverse it for real and pay the buyer a second time. Nothing in
	// the request, the sale or the provider's catalogue distinguishes the two
	// beforehand, so the only safe move is not to ask.
	//
	// Giving up is therefore the correct outcome and needs_attention is what it
	// is called: an Unresolved Reversal, the state for an ask whose outcome is
	// unknown and which only a Platform Operator can settle. That is the honest
	// record here — the platform has no idea what became of its own in-flight
	// ask, and the row must say so rather than quietly disappear.
	if sale.Status != platform.ActiveSaleStatus {
		s.logger.Error("UNRESOLVED_REVERSAL: a Ticket Sale was reversed by somebody else while a Reversal Request was in flight; the platform will not ask the Payment Provider again, because the reversal it asked for may or may not have happened and asking could refund the buyer twice — a Platform Operator must settle it against the provider's own dashboard",
			"ticket_sale_id", sale.ID,
			"confirmation_ref", sale.ConfirmationRef,
			"client_transaction_id", current.ClientTransactionID,
			"reversal_request_id", current.ID,
			"sale_status", sale.Status,
		)
		givenUpAt := s.now()
		return reversalOutcomeUnresolved, s.repo.RecordReversalRequestAttempt(ctx, repository.ReversalRequestAttempt{
			ID:            current.ID,
			Status:        sales.ReversalRequestNeedsAttention,
			LastError:     "the Ticket Sale was reversed by another actor while this request was in flight; the provider was not asked again",
			Now:           givenUpAt,
			NextAttemptAt: givenUpAt,
		})
	}

	_, outcome, err := s.pursueReversalRequest(ctx, sale, current)

	// THE REFUSED NOTICE FIRES HERE AND NOWHERE ELSE, and being here is the whole
	// of what makes it correct (#161).
	//
	// Here means: on a request that WAS PENDING. Only a Customer who was told
	// "we're processing your refund" was promised anything, and this function is
	// the only way a pending request ever reaches an answer — both drains come
	// through it, and the buyer's own press does not. A reversal refused
	// synchronously on the press returns its error to a Customer who is looking
	// at the page; emailing them as well would be telling somebody something they
	// are already reading (ReverseOwnSale).
	//
	// Here also means: EXACTLY ONCE, without a sent flag or a de-duplication
	// table. The advisory lock above serialises every actor on this sale, and
	// under it the in_flight -> refused transition can happen only once: the
	// request was re-read as in_flight, RecordReversalRequestAttempt writes only
	// `WHERE status = 'in_flight'`, and once it is refused the partial unique
	// index excludes it, so the next actor's GetLiveReversalRequest finds nothing
	// and returns skipped without a word to anybody. Whoever performed that one
	// transition is whoever is standing here, so the Reconciler and the Customer
	// Area drain racing on one request produce one email between them.
	//
	// err == nil is part of that. A refusal whose write failed left the row in
	// flight and will be probed again, so sending on it would be the second email
	// this guard exists to prevent — and the first one would be about a request
	// the platform does not yet record as refused.
	//
	// An Unresolved Reversal is the deliberate silence beside it: the platform
	// does not know what became of the money, so it has nothing true to say
	// (giveUpOnReversalRequest).
	if outcome == reversalOutcomeRefused && err == nil {
		s.sendReversalRefusedNotice(ctx, sale)
	}
	return outcome, err
}

// sendReversalRefusedNotice tells a Customer that the refund the platform said
// it was processing could not be made, and that their tickets are still valid.
//
// The send is best-effort and its error is swallowed, exactly as every other
// notice in this module is. The refusal is a fact about the Payment Provider's
// answer and it is already recorded; failing the pursuit over an undelivered
// email would leave the request in flight and send the platform back to ask
// PayPhone about money it has been definitively told did not move.
//
// The sale is the one just re-read under the lock, so the name and address are
// this sale's own snapshot of the buyer — the same ones the void notice uses, and
// not the Customer's current profile.
//
// The send does not inherit the caller's cancellation, for the reason the
// advisory unlock and releaseReversalClaim do not: the context most likely to be
// dead here is the one where this matters most. It hangs off a one-time state
// transition — in_flight to refused, which the caller above explains can happen
// only once — so an instance shut down or a Scheduler attempt abandoned in the
// moment after that transition loses the only correction anybody will ever
// attempt, and no path retries it.
func (s *Service) sendReversalRefusedNotice(ctx context.Context, sale *repository.CustomerTicketSale) {
	noticeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reversalRefusedNoticeTimeout)
	defer cancel()
	if err := s.email.SendSaleReversalRefused(noticeCtx, platform.SaleReversalRefused{
		To:           sale.CustomerEmail,
		CustomerName: displayName(sale.CustomerFirstName, sale.CustomerLastName),
		EventName:    sale.EventName,
		Reference:    sale.ConfirmationRef,
		// The one message with nothing else to read (#246, ADR 0033). Whoever is
		// standing here is whoever performed the in_flight -> refused transition:
		// a Reconciler run on a cron, or a stranger's page draining this request.
		// Neither of them is this buyer, and neither is on a page in this buyer's
		// language — so the language comes off the sale, which is the last thing
		// still standing that knows it.
		Locale: s.mailLocale(noticeCtx, sale.ID, sale.Locale, sale.CustomerEmail),
	}); err != nil {
		// Worth a line of its own rather than a discarded error: this is the only
		// message that ever corrects the promise, so a Customer it never reaches is
		// one who goes on believing a refund is coming.
		s.logger.Warn("could not tell a Customer that their refund was refused; they were last told it was being processed",
			"ticket_sale_id", sale.ID,
			"confirmation_ref", sale.ConfirmationRef,
			"error", err,
		)
	}
}

// reversalRefusedNoticeTimeout bounds the send above once it has stopped
// inheriting the caller's deadline. Something has to: an unbounded send on a
// context nothing can cancel would hold a Reconciler run — or a Cloud Run
// instance in its shutdown grace period — on an email provider that is not
// answering. Ten seconds is the outbound-call budget this module already gives
// the Payment Provider, and this is a less important call than that one.
const reversalRefusedNoticeTimeout = 10 * time.Second

// finishAgreedReversal finishes a reversal the Payment Provider already carried
// out and the platform then failed to record locally: the
// SALE_REVERSAL_NOT_COMMITTED incident, where the buyer keeps both the money and
// the tickets and the Organization's dashboard shows revenue that no longer
// exists (#162).
//
// THE PAYMENT PROVIDER IS NOT REACHED FROM HERE, and that is the decision rather
// than an implementation detail. The answer is already recorded on this row, so
// there is no question left to ask — and asking anyway would post a second
// Reverse against a payment that has already been reversed, which is the double
// refund the whole design is built to avoid. Nothing below touches s.provider,
// and nothing here ever may: the work left is the local write and only the local
// write.
//
// The caller holds the per-sale advisory lock and has just re-read this row
// under it, so the sale read below cannot be raced by another actor on this sale.
//
// The active-sale guard is the same one the probing path takes and means
// something different here. There, a sale settled by somebody else makes it
// unsafe to ask; here it makes the repair unnecessary — the sale is voided,
// which is the outcome this was trying to produce — so the only thing written is
// that there is nothing left to do, and it is not an error.
//
// Removing that guard does not merely lose a shortcut. commitSaleReversal would
// run on a reversed sale, restore no capacity, send nothing, and hand back
// ErrSaleAlreadyReversed — so a repair with nothing to repair would be counted as
// a failed one, log an incident, and burn the request's attempts and its give-up
// bound on a case that is already settled
// (TestTheRepairLeavesASaleSomebodyElseReversedAlone).
func (s *Service) finishAgreedReversal(ctx context.Context, request repository.ReversalRequest) (reversalOutcome, error) {
	sale, err := s.repo.GetTicketSaleForReversal(ctx, request.TicketSaleID)
	if err != nil {
		return reversalOutcomeSkipped, err
	}
	if sale == nil || sale.Status != platform.ActiveSaleStatus {
		s.recordTheSaleIsVoided(ctx, request.ID)
		return reversalOutcomeSkipped, nil
	}

	now := s.now()
	if _, err := s.commitSaleReversal(ctx, sale, &request, now); err != nil {
		return s.retryAgreedReversalLater(ctx, sale, request, err, now)
	}

	// No attempt is counted and no status is written. It says succeeded, which is
	// still exactly what happened, and there was no attempt at a provider here to
	// count; what takes it out of the queue is the sale_voided_at that
	// commitSaleReversal has just recorded.
	return reversalOutcomeReversed, nil
}

// recordTheSaleIsVoided notes that this Reversal Request has no local work left,
// which is what takes it out of the Reconciler's queue
// (repository.ClaimDueReversalRequest).
//
// The failure is swallowed, and swallowing it costs a tick rather than
// correctness. The reversal itself has landed — that is the fact this is trailing
// — so failing the caller over it would report a completed reversal as broken to
// a buyer looking at the page. A row left unmarked simply comes due once more,
// and the next pass finds the sale already voided and marks it then, which is the
// branch above.
func (s *Service) recordTheSaleIsVoided(ctx context.Context, requestID string) {
	if err := s.repo.MarkReversalSaleVoided(ctx, requestID, s.now()); err != nil {
		s.logger.Warn("could not record that a reversed Ticket Sale had settled its Reversal Request; the request stays in the Reconciler's queue until a later pass sees the sale is voided",
			"reversal_request_id", requestID,
			"error", err,
		)
	}
}

// retryAgreedReversalLater records a local commit that has failed again, and
// ends the pursuit if the platform has been at it for as long as it is willing
// to be (sales.ReversalGiveUpAfter, measured from the buyer's press).
//
// It shares the provider's backoff schedule and the provider's bound although
// nothing here is the provider's fault, because what is being waited on has the
// same shape: a platform that cannot complete a write — a database under load, a
// deploy, a lock it keeps losing — is not helped by being asked again every
// minute for a day, and a bound is what stops a row nobody can finish from
// retrying forever instead of becoming something a person reads.
//
// The status is deliberately left at succeeded until the bound. The provider's
// answer is not what failed, and rewriting it would lose the one fact that makes
// this repairable at all.
//
// Giving up here is a DIFFERENT admission from every other Unresolved Reversal,
// and the queue must not blur the two. Everywhere else the platform does not
// know what became of the money; here it knows exactly — it went back — and what
// it could not do is void the sale. So the operator's next move differs too:
// there is nothing to look up on the provider's dashboard, and the sale is
// settled by recording an Operator Reversal (ADR 0019).
func (s *Service) retryAgreedReversalLater(ctx context.Context, sale *repository.CustomerTicketSale, request repository.ReversalRequest, cause error, now time.Time) (reversalOutcome, error) {
	if sales.ReversalGivenUp(request.RequestedAt, now) {
		s.logger.Error("UNRESOLVED_REVERSAL: the Payment Provider reversed this payment and the platform has failed to void the Ticket Sale for as long as it is willing to keep trying; the buyer holds both their refund and their tickets, and a Platform Operator must settle it by recording an Operator Reversal on this sale",
			"ticket_sale_id", sale.ID,
			"confirmation_ref", sale.ConfirmationRef,
			"client_transaction_id", request.ClientTransactionID,
			"reversal_request_id", request.ID,
			"requested_at", request.RequestedAt,
			"attempt_count", request.AttemptCount+1,
			"last_error", cause.Error(),
		)
		return reversalOutcomeUnresolved, s.repo.RecordReversalCommitAttempt(ctx, repository.ReversalRequestAttempt{
			ID:            request.ID,
			Status:        sales.ReversalRequestNeedsAttention,
			LastError:     cause.Error(),
			Now:           now,
			NextAttemptAt: now,
		})
	}

	if recErr := s.repo.RecordReversalCommitAttempt(ctx, repository.ReversalRequestAttempt{
		ID:        request.ID,
		Status:    sales.ReversalRequestSucceeded,
		LastError: cause.Error(),
		Now:       now,
		NextAttemptAt: now.Add(sales.ReversalRetryDelay(
			request.AttemptCount+1,
			sales.ReversalRetryJitter(request.ID),
		)),
	}); recErr != nil {
		return reversalOutcomeSkipped, recErr
	}

	// The failure is returned rather than folded into an outcome: this run did
	// not finish the request, and a drain that counted it as anything else would
	// report an incident as work done. commitSaleReversal has already logged the
	// SALE_REVERSAL_NOT_COMMITTED line naming the sale.
	return reversalOutcomeSkipped, cause
}

// pursueReversalRequest asks the Payment Provider about one authorised Reversal
// Request and applies the three-way outcome ADR 0024 turns on. The caller holds
// the advisory lock; eligibility has already been decided, once, when the
// request was created.
//
// The three answers are three different facts about somebody's money, and the
// platform does something completely different with each:
//
//   - SUCCESS — the money is on its way back, or errorCode 24 says it already
//     went. The request is resolved succeeded FIRST and the Ticket Sale is
//     voided after, so a local write that fails leaves a succeeded request over
//     an active sale: the SALE_REVERSAL_NOT_COMMITTED incident, now a row a
//     later loop can finish rather than a log line and a hand-repair (#162).
//   - DEFINITE REFUSAL — the provider considered it and said no, so NOTHING
//     HAPPENED. The ask is over and the Ticket Sale is untouched. It is reported
//     as an outcome rather than as an error; see reversalOutcome for why the two
//     callers cannot share one word for it.
//   - UNKNOWN OUTCOME — a timeout, a 5xx, an answer this integration cannot
//     place. The money may or may not have moved, so the request stays in flight
//     and the buyer is told their refund is being processed. This is the case the
//     whole feature exists for, and the only one that can repeat: it is where the
//     backoff below applies, and where the platform eventually gives up.
func (s *Service) pursueReversalRequest(ctx context.Context, sale *repository.CustomerTicketSale, request *repository.ReversalRequest) (result *SaleReversalResult, outcome reversalOutcome, err error) {
	err = s.provider.Reverse(ctx, request.ClientTransactionID)

	// The clock is read AFTER the provider answered, and everything below is
	// scheduled from here. The provider timeout is ten seconds and so is the
	// backoff's first step, so a `now` captured before the call would have been
	// entirely spent by the call itself: the first retry of the exact case ADR
	// 0024 was written for — a PayPhone that goes quiet — would come due the
	// instant the probe returned, which is no backoff at all. A wait means a wait
	// since we last asked.
	probedAt := s.now()

	// OUR OWN cancellation is not the provider's silence. A client that hung up
	// and an instance being shut down both surface here as a context error, and
	// they say nothing whatsoever about what PayPhone did — so recording one as
	// last_error would overwrite the provider's actual last words on the very row
	// an operator reads during an incident, and letting it count toward the
	// give-up bound would abandon an ask because we walked away from it.
	//
	// It is asked of the CALLER'S context rather than of the error, because the
	// provider's own per-call timeout produces the same error type and IS an
	// unknown answer: the question is whose deadline expired, and only the
	// context knows. The request keeps its schedule untouched and is asked about
	// again — the money may well have moved, and nobody here found out. The
	// buyer, if they are somehow still there, is told exactly that: their ask is
	// recorded and being pursued.
	if err != nil && ctx.Err() != nil {
		return pendingResult(sale, request), reversalOutcomeSkipped, nil
	}

	if err != nil && platform.PaymentReverseOutcomeUnknown(err) {
		// THE GIVE-UP BOUND, and it is asked AFTER the provider was asked rather
		// than before. A day-old request gets one last probe — it may be the one
		// that answers — and only an answer that is still silence ends the pursuit.
		// Checking first would abandon a request without ever asking it again.
		if sales.ReversalGivenUp(request.RequestedAt, probedAt) {
			return nil, reversalOutcomeUnresolved, s.giveUpOnReversalRequest(ctx, sale, request, err.Error(), probedAt)
		}

		// Not a failure to report: a question still open. The provider may well
		// have acted on a request whose answer never came back, so the ask stays
		// recorded and the platform asks again rather than telling the buyer
		// nothing happened to money that may already have left them.
		s.logger.Warn("the payment provider gave no definite answer about a Sale Reversal; the Reversal Request stays in flight",
			"ticket_sale_id", sale.ID,
			"confirmation_ref", sale.ConfirmationRef,
			"client_transaction_id", request.ClientTransactionID,
			"reversal_request_id", request.ID,
			"attempt_count", request.AttemptCount+1,
			"error", err,
		)
		if recErr := s.repo.RecordReversalRequestAttempt(ctx, repository.ReversalRequestAttempt{
			ID:        request.ID,
			Status:    sales.ReversalRequestInFlight,
			LastError: err.Error(),
			Now:       probedAt,
			// The request stays open and becomes due again after the backoff its
			// attempt count has earned (ADR 0024: 10s, 30s, 2m, 5m, 15m, then every
			// 30m, with jitter). Writing a due date in the future is what makes the
			// next page load — and the next Reconciler tick — a read rather than
			// another ten seconds spent on a provider that has just failed to answer.
			//
			// The count is the row's own plus this attempt, because the increment
			// happens inside the same write and cannot be read back before it.
			NextAttemptAt: probedAt.Add(sales.ReversalRetryDelay(
				request.AttemptCount+1,
				sales.ReversalRetryJitter(request.ID),
			)),
		}); recErr != nil {
			return nil, reversalOutcomeInFlight, recErr
		}
		return pendingResult(sale, request), reversalOutcomeInFlight, nil
	}

	if err != nil {
		// A definite refusal. The provider's code goes to the log and not to the
		// buyer: PayPhone publishes no "too late" code, so any explanation offered
		// here would be a guess about somebody's money.
		s.logger.Error("sale reversal refused by the payment provider; the Ticket Sale is untouched",
			"ticket_sale_id", sale.ID,
			"confirmation_ref", sale.ConfirmationRef,
			"client_transaction_id", request.ClientTransactionID,
			"reversal_request_id", request.ID,
			"error", err,
		)
		if recErr := s.repo.RecordReversalRequestAttempt(ctx, repository.ReversalRequestAttempt{
			ID:            request.ID,
			Status:        sales.ReversalRequestRefused,
			LastError:     err.Error(),
			Now:           probedAt,
			NextAttemptAt: probedAt,
		}); recErr != nil {
			return nil, reversalOutcomeRefused, recErr
		}
		return nil, reversalOutcomeRefused, nil
	}

	// The money went back. The request is settled before the sale is voided so
	// that the fact the platform just learned survives a local write that fails.
	if recErr := s.repo.RecordReversalRequestAttempt(ctx, repository.ReversalRequestAttempt{
		ID:            request.ID,
		Status:        sales.ReversalRequestSucceeded,
		Now:           probedAt,
		NextAttemptAt: probedAt,
	}); recErr != nil {
		return nil, reversalOutcomeReversed, recErr
	}
	result, err = s.commitSaleReversal(ctx, sale, request, probedAt)
	return result, reversalOutcomeReversed, err
}

// ageUnaskableReversalRequest is the skip that can still end: a pass that could
// not work the request at all, on one the platform has now been pursuing for
// longer than it is willing to (sales.ReversalGiveUpAfter).
//
// It exists because a skip is otherwise weightless — no probe, no attempt
// counted, no bound consulted — so a request that only ever skips would churn
// past its own deadline forever and never become the queue entry a human reads.
// The bound runs from when the Customer pressed rather than from anything about
// the attempts, so a request that could never be worked is exactly as overdue as
// one that was asked a hundred times; the only thing missing was somebody
// evaluating it here.
//
// A skip inside the bound stays a skip and costs nothing. Nothing was learned
// and nothing is written.
//
// THE GUARD FOLLOWS THE ROW, and the request's own status is what it is given.
// Both kinds of unfinished request arrive here — an in-flight ask and a
// succeeded one whose local commit never landed — because a sale another actor
// is holding is held whichever this is. Guarding on a fixed status would match
// nothing on the other kind: the row would stay exactly as it was, this would
// still report that it had given up, and the same row would come due and be
// logged again on every tick for the rest of the deployment's life while the
// buyer holds both their refund and their tickets.
//
// Losing the write is a normal ending rather than a failure. An actor that
// reached a real answer in the meantime moved the row on, so the guard matches
// nothing and this pass gave up on nothing — reported as the skip it turned out
// to be, and never as an incident logged over somebody else's answer.
func (s *Service) ageUnaskableReversalRequest(ctx context.Context, request repository.ReversalRequest, reason string) (reversalOutcome, error) {
	now := s.now()
	if !sales.ReversalGivenUp(request.RequestedAt, now) {
		return reversalOutcomeSkipped, nil
	}
	// The write comes before the line, so what is logged is a transition that
	// happened. An UNRESOLVED_REVERSAL is read by a human during an incident, and
	// one logged over a row that did not move sends them looking for a state
	// nothing is in.
	if err := s.repo.RecordUnaskableReversalAttempt(ctx, repository.ReversalRequestAttempt{
		ID:            request.ID,
		Status:        sales.ReversalRequestNeedsAttention,
		LastError:     reason,
		Now:           now,
		NextAttemptAt: now,
	}, request.Status); err != nil {
		if errors.Is(err, repository.ErrReversalRequestMovedOn) {
			return reversalOutcomeSkipped, nil
		}
		return reversalOutcomeUnresolved, err
	}
	// claimed_status is what the operator's next move turns on, which is why it is
	// on the line: an in-flight request is one nobody knows the answer to and only
	// the provider's dashboard can settle, while a succeeded one is money that
	// demonstrably went back and a sale that needs voiding (ADR 0019).
	s.logger.Error("UNRESOLVED_REVERSAL: the platform never managed to work a Reversal Request to an end, and has now been trying for as long as it is willing to; a Platform Operator must settle it",
		"ticket_sale_id", request.TicketSaleID,
		"reversal_request_id", request.ID,
		"requested_at", request.RequestedAt,
		"attempt_count", request.AttemptCount,
		"claimed_status", request.Status,
		"reason", reason,
	)
	return reversalOutcomeUnresolved, nil
}

// giveUpOnReversalRequest records an Unresolved Reversal: the platform pursued
// this ask for as long as it is willing to and never learned what the Payment
// Provider did with it (ADR 0024).
//
// Nothing else happens, and the absences are the decision. The Ticket Sale stays
// ACTIVE — its capacity stays held and it keeps counting in every aggregate —
// because giving up is not learning that the money came back; it is learning
// nothing. And the Customer is emailed NOTHING, because there is no true thing
// to tell them: "your refund failed" may be false and "your refund is done" may
// be false, and the platform cannot tell which. Only the provider's own
// dashboard can, which is why this ends with a Platform Operator rather than
// with a message (ADR 0019).
//
// So the log line IS the mechanism. It is an error rather than a warning because
// somebody's money is in an unknown state, and it carries the two things that
// find the transaction on the provider's dashboard: the client transaction id
// and what the provider was last saying. The same row is listable — see
// ListUnresolvedReversals — so an incident does not have to be reconstructed
// from logs.
func (s *Service) giveUpOnReversalRequest(ctx context.Context, sale *repository.CustomerTicketSale, request *repository.ReversalRequest, lastError string, now time.Time) error {
	s.logger.Error("UNRESOLVED_REVERSAL: the platform gave up asking the Payment Provider what became of a Reversal Request; the money may or may not have gone back and only the provider's own dashboard can say — a Platform Operator must settle it, and the Customer has been told nothing because there is nothing true to tell them",
		"ticket_sale_id", sale.ID,
		"confirmation_ref", sale.ConfirmationRef,
		"client_transaction_id", request.ClientTransactionID,
		"reversal_request_id", request.ID,
		"requested_at", request.RequestedAt,
		"attempt_count", request.AttemptCount+1,
		"last_error", lastError,
	)
	return s.repo.RecordReversalRequestAttempt(ctx, repository.ReversalRequestAttempt{
		ID:            request.ID,
		Status:        sales.ReversalRequestNeedsAttention,
		LastError:     lastError,
		Now:           now,
		NextAttemptAt: now,
	})
}

// commitSaleReversal voids the Ticket Sale, restores its capacity and tells the
// buyer. It runs only once the money is known to have gone back — or, on a free
// Online Sale, once it is known that there was never any to return.
//
// request is the Reversal Request this completes, and is nil on the free path,
// which records none. It is here only so the incident log below can name the row
// an operator would go looking for.
func (s *Service) commitSaleReversal(ctx context.Context, sale *repository.CustomerTicketSale, request *repository.ReversalRequest, now time.Time) (*SaleReversalResult, error) {
	// The shared reversal primitive: it voids the sale and returns every line's
	// quantity to its Ticket Type's sold_count in one transaction, under the same
	// locks a Sale Import undo takes. Capacity restoration is defined once, in
	// that primitive, and never re-implemented per caller.
	//
	// It skips a sale that is no longer active, and takes a row lock while doing
	// so, which is the second of this path's two guards against a double
	// submit: a request that somehow reached here on an already-reversed sale
	// finds nothing to do and comes back empty. Capacity is therefore restored
	// exactly once however many times the button is pressed — but note that this
	// guard alone never protected the money, since by the time it runs the
	// provider has already been called. The advisory lock above is what does.
	reversedSales, err := s.repo.ReverseSales(ctx, repository.ReverseSalesInput{
		EventID:        sale.EventID,
		OrganizationID: sale.OrganizationID,
		SaleIDs:        []string{sale.ID},
		Actor:          sales.ReversalActorCustomer,
		Now:            now,
	})
	if err != nil {
		// The money is already on its way back and the Ticket Sale still stands:
		// the buyer keeps both their tickets and their payment, and the
		// Organization's dashboard shows revenue that no longer exists. Re-calling
		// the provider would risk reversing twice and the local write is the thing
		// that just failed, so nothing here can fix it in the moment.
		//
		// What has changed since ADR 0018 is that it is no longer only a log line
		// and a hand-repair. The Reversal Request stands in state succeeded over a
		// sale that is still active, which is a queue entry the Reversal Reconciler
		// picks up and finishes (finishAgreedReversal, #162) — so this line records
		// an incident that is already being repaired, and stays an error because it
		// is one until the repair lands.
		//
		// The ids are the whole point of the line: the Ticket Sale to correct, and
		// the client transaction id to find the reversal on the provider's
		// dashboard.
		if request != nil {
			s.logger.Error("SALE_REVERSAL_NOT_COMMITTED: the Payment Provider reversed the payment but the Ticket Sale could not be marked reversed; the buyer keeps both the money and the tickets, and the Reversal Request stands succeeded over an active sale",
				"ticket_sale_id", sale.ID,
				"confirmation_ref", sale.ConfirmationRef,
				"client_transaction_id", request.ClientTransactionID,
				"reversal_request_id", request.ID,
				"error", err,
			)
		}
		return nil, err
	}
	if len(reversedSales) == 0 {
		// Somebody else got there first — a Sale Import undo landing in between, or
		// an Operator Reversal. The honest answer is that this request reversed
		// nothing, and it is the same answer a request arriving a minute later
		// would get.
		return nil, sales.ErrSaleAlreadyReversed()
	}

	// The reversal has landed, so the Reversal Request has nothing left to do and
	// stops being due (repository.ClaimDueReversalRequest). It is recorded here,
	// where the local commit is known to have succeeded, rather than inferred later
	// from the sale — which is the whole of migration 040.
	//
	// Nothing is recorded on the free path, which has no request: a free Online
	// Sale calls no provider and creates none (ADR 0024).
	if request != nil {
		s.recordTheSaleIsVoided(ctx, request.ID)
	}

	// Only now, with the reversal committed, is the buyer told. A failure to send
	// is swallowed exactly as every other notice in this module is: the tickets
	// are gone whether or not the email lands, and re-running the reversal to
	// retry an email would be far worse than a missing one.
	//
	// Exactly once per reversal, however the reversal was reached. The primitive
	// above returns an empty set for a sale that was already reversed, so a probe
	// that resolves a request whose sale somebody else voided sends nothing.
	reversed := reversedSales[0]
	_ = s.email.SendSaleVoided(ctx, platform.SaleVoided{
		To:           reversed.CustomerEmail,
		CustomerName: displayName(reversed.CustomerFirstName, reversed.CustomerLastName),
		EventName:    sale.EventName,
		Reference:    reversed.ConfirmationRef,
		// The language of the sale being voided (#246, ADR 0033), read off the
		// row the reversal primitive just locked — and NOT off the press that got
		// here. The buyer may have pressed Undo on a Spanish page, but a drain
		// finishing their request hours later did not, and both must produce the
		// same notice.
		Locale: s.mailLocale(ctx, reversed.ID, reversed.Locale, reversed.CustomerEmail),
	})

	return &SaleReversalResult{
		TicketSaleID:    sale.ID,
		ConfirmationRef: reversed.ConfirmationRef,
		Status:          SaleReversalStatusReversed,
		ReversedAt:      now.UTC().Format(time.RFC3339),
	}, nil
}

// pendingResult is what a buyer is told while their Reversal Request is still in
// flight: we are processing your refund, and here is when you asked.
//
// The instant is the request's own requested_at and never the clock, so a second
// press reports the ask the platform is actually pursuing rather than the moment
// the buyer pressed again.
func pendingResult(sale *repository.CustomerTicketSale, request *repository.ReversalRequest) *SaleReversalResult {
	return &SaleReversalResult{
		TicketSaleID:    sale.ID,
		ConfirmationRef: sale.ConfirmationRef,
		Status:          SaleReversalStatusPending,
		RequestedAt:     request.RequestedAt.UTC().Format(time.RFC3339),
	}
}

// reversalRefusalError is how this module answers the shared reversal decision:
// one typed, buyer-facing error per refusal, and nil when there is none.
//
// The mapping is the whole of what this module adds to the decision it shares
// with the Customer Area (platform.PaymentReversal.EligibilityAt). The Area
// collapses every refusal into silence — no offer, no deadline — because a
// surface has nothing useful to say about which; an endpoint being pressed does,
// since the buyer is owed the difference between "this was never yours to undo
// here" and "you are too late".
//
// Two refusals share one code deliberately. A sale that is not an Online Sale
// and one whose Payment Provider cannot give the money back are the same fact to
// the person reading it — this is not yours to undo, and waiting will not change
// that — and the second is temporary in a way no message should promise.
func reversalRefusalError(refusal platform.ReversalRefusal) error {
	switch refusal {
	case platform.ReversalAllowed:
		return nil
	case platform.ReversalAlreadyReversed:
		return sales.ErrSaleAlreadyReversed()
	case platform.ReversalWindowClosed:
		return sales.ErrReversalWindowClosed()
	default:
		return sales.ErrSaleNotReversible()
	}
}
