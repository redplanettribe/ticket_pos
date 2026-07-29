package service

import (
	"context"
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

	result, refused, err := s.pursueReversalRequest(ctx, sale, request, now)
	if err != nil {
		return nil, err
	}
	if refused {
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
// answered from the request row, something else has to do the asking, and until
// the Reversal Reconciler ships (#158) that something is the Customer loading
// their own Area. The common case therefore still resolves in seconds.
//
// It never evaluates eligibility. A Reversal Request that was inside the
// Reversal Window when it was made stays authorised however long the answer
// takes: this function only ever finds out what happened, and re-deciding
// whether the buyer was allowed to ask would abandon the one case that matters
// most — the money already gone.
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
		if err := s.resolveReversalRequest(ctx, customerID, request); err != nil {
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
// the Reversal Reconciler's (#158) problem to work through in the background, not
// something to make the buyer's page pay for. Requests beyond the limit are the
// NEWER ones — the query returns oldest first — and the next render takes them up.
const maxOpportunisticDrain = 1

// reversalRetryDelay is how long a Reversal Request waits after an unknown
// answer before it may be asked about again.
//
// It is the FIRST step of the backoff ADR 0024 describes (10s, 30s, 2m, 5m, 15m,
// then every 30m with jitter) and deliberately not the whole schedule, which
// belongs to the Reversal Reconciler (#158). A flat delay is enough for what
// ships now: the opportunistic drain's job is to make the common case resolve in
// seconds, and its danger is a page that re-asks on every render. Ten seconds
// removes the danger without slowing the case worth having.
const reversalRetryDelay = 10 * time.Second

// resolveReversalRequest pursues one in-flight Reversal Request under the same
// advisory lock a press takes, so the buyer's press and this drain can never
// overlap on one sale and reach the provider together.
//
// The request AND the sale are re-read under the lock before anything is asked.
// The list that produced the request was read outside the lock, and neither a
// request another actor has just settled nor a sale another actor has just
// reversed may be probed: that second probe is a second call about somebody's
// money.
func (s *Service) resolveReversalRequest(ctx context.Context, customerID string, request repository.ReversalRequest) error {
	release, locked, err := s.repo.LockTicketSaleForReversal(ctx, request.TicketSaleID)
	if err != nil {
		return err
	}
	if !locked {
		// Somebody else is already asking about this very sale. The request stays
		// in flight and the next reader — or the Reconciler — picks it up; a drain
		// that queued behind a ten-second provider call would hold the Customer
		// Area open on it.
		return nil
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
		return err
	}
	if current == nil || current.ID != request.ID || current.Status != sales.ReversalRequestInFlight {
		return nil
	}

	sale, err := s.repo.GetCustomerTicketSale(ctx, customerID, request.TicketSaleID)
	if err != nil {
		return err
	}
	if sale == nil {
		// The sale is gone or was never this Customer's. Nothing can be reversed
		// and nothing should be asked of the provider on its behalf.
		return nil
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
		return s.repo.RecordReversalRequestAttempt(ctx, repository.ReversalRequestAttempt{
			ID:        current.ID,
			Status:    sales.ReversalRequestNeedsAttention,
			LastError: "the Ticket Sale was reversed by another actor while this request was in flight; the provider was not asked again",
			Now:       givenUpAt,
			// Nothing is due about a request nobody will ask about again; the column
			// is written only so it never carries a stale date.
			NextAttemptAt: givenUpAt,
		})
	}

	// A definite refusal is not reported here, and the discarded flag is the
	// point. On the press path a refusal is the buyer's answer; on this one it is
	// a RESOLUTION — the question closed with a definite answer, which is a
	// success for a drain whose whole job is to find out. Telling the caller it
	// failed would log an incident every time the platform learned something.
	//
	// Nobody is told. The refusal email ADR 0024 promises this buyer is #158's,
	// alongside the Reconciler that will send it.
	_, _, err = s.pursueReversalRequest(ctx, sale, current, s.now())
	return err
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
//     later loop can finish rather than a log line and a hand-repair (#158).
//   - DEFINITE REFUSAL — the provider considered it and said no, so NOTHING
//     HAPPENED. The ask is over and the Ticket Sale is untouched. It is reported
//     as refused=true rather than as an error, because the two callers owe the
//     answer to different people: a buyer who just pressed gets the same
//     immediate error they have always got, while a drain that merely went
//     looking has succeeded at finding out.
//   - UNKNOWN OUTCOME — a timeout, a 5xx, an answer this integration cannot
//     place. The money may or may not have moved, so the request stays in flight
//     and the buyer is told their refund is being processed. This is the case the
//     whole feature exists for.
func (s *Service) pursueReversalRequest(ctx context.Context, sale *repository.CustomerTicketSale, request *repository.ReversalRequest, now time.Time) (result *SaleReversalResult, refused bool, err error) {
	err = s.provider.Reverse(ctx, request.ClientTransactionID)

	if err != nil && platform.PaymentReverseOutcomeUnknown(err) {
		// Not a failure to report: a question still open. The provider may well
		// have acted on a request whose answer never came back, so the ask stays
		// recorded and the platform asks again rather than telling the buyer
		// nothing happened to money that may already have left them.
		s.logger.Warn("the payment provider gave no definite answer about a Sale Reversal; the Reversal Request stays in flight",
			"ticket_sale_id", sale.ID,
			"confirmation_ref", sale.ConfirmationRef,
			"client_transaction_id", request.ClientTransactionID,
			"reversal_request_id", request.ID,
			"error", err,
		)
		if recErr := s.repo.RecordReversalRequestAttempt(ctx, repository.ReversalRequestAttempt{
			ID:        request.ID,
			Status:    sales.ReversalRequestInFlight,
			LastError: err.Error(),
			Now:       now,
			// The request stays open and becomes due again after the delay. Writing
			// a due date in the future is what makes the next page load a read
			// rather than another ten seconds spent on a provider that has just
			// failed to answer.
			NextAttemptAt: now.Add(reversalRetryDelay),
		}); recErr != nil {
			return nil, false, recErr
		}
		return pendingResult(sale, request), false, nil
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
			Now:           now,
			NextAttemptAt: now,
		}); recErr != nil {
			return nil, false, recErr
		}
		return nil, true, nil
	}

	// The money went back. The request is settled before the sale is voided so
	// that the fact the platform just learned survives a local write that fails.
	if recErr := s.repo.RecordReversalRequestAttempt(ctx, repository.ReversalRequestAttempt{
		ID:            request.ID,
		Status:        sales.ReversalRequestSucceeded,
		Now:           now,
		NextAttemptAt: now,
	}); recErr != nil {
		return nil, false, recErr
	}
	result, err = s.commitSaleReversal(ctx, sale, request, now)
	return result, false, err
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
		// What has changed since ADR 0018 is that it is no longer only a log line.
		// The Reversal Request stands in state succeeded over a sale that is still
		// active, which is a row a later loop can finish and, until then, a row an
		// operator can find in SQL (#158).
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
