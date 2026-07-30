package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// CustomerTicketSale is one Ticket Sale as the Customer-initiated Sale Reversal
// path needs to see it: enough to decide whether it may be reversed, and enough
// to reverse it and tell the buyer afterwards.
//
// It is not the Customer Area's row and must not become one. That row exists to
// be rendered; this one exists to be judged, so it carries the Payment Method
// and the Payment's client transaction id — facts a buyer is never shown — and
// none of the presentation the Area needs.
type CustomerTicketSale struct {
	ID              string
	EventID         string
	OrganizationID  string
	EventName       string
	ConfirmationRef string
	// Channel is the Sales Channel: only 'online' can be reversed by a Customer.
	Channel string
	// Status is 'active' or 'reversed'.
	Status string
	// PaymentMethod is how the sale was settled: 'free' when the platform settled
	// a zero-total checkout itself (ADR 0017), otherwise the Payment Provider that
	// collected the money. Null on channels that carry none.
	PaymentMethod sql.NullString
	// SoldAt is the Payment approval instant on an Online Sale — the sale commits
	// in the transaction that approves the Payment — so it opens the Reversal
	// Window without a second lookup.
	SoldAt time.Time
	// EventStartsAt closes the Reversal Window when it falls before the
	// Ecuadorian cutoff. Nullable in the schema; an Online Sale always has one,
	// since only a published Event is sellable and publishing requires a start.
	EventStartsAt sql.NullTime
	// ClientTransactionID is the Payment's id under OUR namespace, which is what
	// PaymentProvider.Reverse is keyed on. Empty on a sale with no Payment row —
	// every Online Sale has one, so this is empty only on the channels that
	// cannot be reversed here anyway.
	//
	// It is the id of the Payment that SETTLED the sale. Today a sale carries at
	// most one Payment row, but the schema does not say so, and this field decides
	// which transaction gets reversed at a Payment Provider: guessing it is
	// somebody's money.
	ClientTransactionID string
	// The buyer as this sale snapshotted them, for the void notice.
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
}

// ReversalFacts narrows this row to the reversal rule's inputs, so the endpoint
// that performs a reversal asks platform.PaymentReversal.EligibilityAt exactly
// the question the Customer Area asked before it offered the button. Neither
// module owns the rule; both feed it.
func (s CustomerTicketSale) ReversalFacts() platform.SaleReversalFacts {
	return platform.SaleReversalFacts{
		Channel:       s.Channel,
		Status:        s.Status,
		SoldAt:        s.SoldAt,
		PaymentMethod: s.PaymentMethod,
		EventStartsAt: s.EventStartsAt,
	}
}

// ticketSaleReversalLockClass namespaces every advisory lock this system takes
// on the reversal of a Ticket Sale, so the key below can never collide with some
// other advisory lock a later feature keys on the same id. The number is
// arbitrary and means nothing beyond "Sale Reversal, ADR 0018".
const ticketSaleReversalLockClass = 18

// LockTicketSaleForReversal serialises reversal attempts on one Ticket Sale
// across every server process, for the whole attempt INCLUDING the outbound call
// to the Payment Provider.
//
// This is the lock that stops a double-press from returning a buyer's money
// twice. The row lock inside ReverseSales cannot do it: it is taken after the
// provider has already been paid a visit, so two requests that both read an
// active sale both POST the reversal and only then serialise on the local write.
// Reversal is the one provider call this system never retries, precisely because
// a second one may refund a second time (ADR 0018).
//
// It is a SESSION-level advisory lock on its own connection rather than a
// transaction-level one, because the thing being protected spans a network call
// with a ten-second timeout. Holding an open transaction — and therefore real row
// locks — across that would park a database connection on PayPhone's latency and
// block every unrelated write to the same rows. This lock touches no row: it is a
// name two requests agree not to hold at once.
//
// It is TRY rather than wait. A second attempt on a sale already being reversed
// wants an answer now, not a place in a queue behind a ten-second provider call
// it will lose anyway, so a contended lock returns acquired=false immediately and
// the caller answers from what it can already see.
//
// The returned release must be called by the caller — a defer, on every path.
// Nothing else releases a session-level lock while the connection lives, and a
// leaked one would make the sale permanently unreversible until the connection is
// recycled. Release unlocks and closes the connection; if the unlock itself fails
// it returns the error AND discards the connection rather than returning a
// lock-holding session to the pool, because a poisoned pooled connection would
// spread the failure to every later request that happened to draw it.
func (r *Repository) LockTicketSaleForReversal(ctx context.Context, ticketSaleID string) (release func() error, acquired bool, err error) {
	conn, err := r.db.Pool.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	// hashtext maps the id onto the bigint pair advisory locks are keyed on. A
	// hash collision costs at most one unrelated concurrent reversal a retry — it
	// can never reverse the wrong sale, since the lock guards nothing but the
	// right to proceed and every check below it still runs on the real row.
	if err := conn.QueryRowContext(ctx,
		`SELECT pg_try_advisory_lock($1, hashtext($2))`,
		ticketSaleReversalLockClass, ticketSaleID,
	).Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, false, err
	}
	if !acquired {
		_ = conn.Close()
		return nil, false, nil
	}

	return func() error {
		// The release deliberately does not inherit the request's cancellation. A
		// client that hung up mid-reversal must still have its lock let go, and the
		// context that would skip the unlock is exactly the one most likely to be
		// dead by now.
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
		defer cancel()
		_, unlockErr := conn.ExecContext(unlockCtx,
			`SELECT pg_advisory_unlock($1, hashtext($2))`,
			ticketSaleReversalLockClass, ticketSaleID,
		)
		if unlockErr != nil {
			// Never hand a still-locked session back to the pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		if closeErr := conn.Close(); unlockErr == nil && closeErr != nil {
			return closeErr
		}
		return unlockErr
	}, true, nil
}

// unlockTimeout bounds the release of an advisory lock. It is short because the
// statement is trivial and the alternative to giving up is holding a connection
// open on a database that is not answering.
const unlockTimeout = 5 * time.Second

// GetCustomerTicketSale loads one Ticket Sale owned by one Customer.
//
// customerID is not a filter to be relaxed later: it is the authorization. It
// comes from the Customer Session and the sale is invisible to this query under
// any other Customer, so "this sale belongs to somebody else" and "there is no
// such sale" are the same absence here rather than two branches a caller could
// get the wrong way round.
//
// Ownership is the sale's own customer_id, written when the sale was recorded.
// Notably it is NOT the email on the sale: a Customer is keyed on their address
// as normalised (ADR 0011), and matching on the snapshot would let a later
// profile edit change who may reverse an old purchase.
func (r *Repository) GetCustomerTicketSale(ctx context.Context, customerID, saleID string) (*CustomerTicketSale, error) {
	return r.ticketSaleForReversal(ctx,
		ticketSaleForReversalSelect+` WHERE ts.id = $1 AND ts.customer_id = $2`,
		saleID, customerID)
}

// GetTicketSaleForReversal loads one Ticket Sale by id alone, with no Customer
// scope at all.
//
// It exists for the Reversal Reconciler, which has no Customer: nobody is on a
// page, nobody presented a session, and the thing being continued is an ask the
// PLATFORM recorded. Requiring an owner here would mean inventing one, and the
// only way to invent one is to read it off the row being acted on, which
// authorizes nothing.
//
// Nothing a Customer can reach may call this. Ownership is authorization on the
// Customer's routes (GetCustomerTicketSale above), and a caller that wants a
// sale it was not handed by an authenticated scope is asking the wrong question
// of the wrong function.
func (r *Repository) GetTicketSaleForReversal(ctx context.Context, saleID string) (*CustomerTicketSale, error) {
	return r.ticketSaleForReversal(ctx,
		ticketSaleForReversalSelect+` WHERE ts.id = $1`,
		saleID)
}

// ticketSaleForReversalSelect is the one projection both loaders above read, so
// the reversal path judges the same facts about a sale however it arrived at it.
// Only the WHERE differs, and the difference is the whole of what separates the
// two.
const ticketSaleForReversalSelect = `
	SELECT
		ts.id, ts.event_id, ts.organization_id, e.name, ts.confirmation_ref,
		ts.channel, ts.status, ts.payment_method, ts.sold_at, e.starts_at,
		p.client_transaction_id,
		ts.customer_email, ts.customer_first_name, ts.customer_last_name
	FROM ticket_sales ts
	JOIN events e ON e.id = ts.event_id
	-- One Payment, chosen rather than whichever the planner returned first: the
	-- approved one that settled this sale, newest first to break any tie. A
	-- plain join here would be a QueryRow over an unordered set the day a sale
	-- ever carries two Payment rows, and this column names the transaction a
	-- Payment Provider is asked to reverse.
	LEFT JOIN LATERAL (
		SELECT p.client_transaction_id
		FROM payments p
		WHERE p.ticket_sale_id = ts.id
		ORDER BY (p.status = 'approved') DESC, p.created_at DESC, p.id DESC
		LIMIT 1
	) p ON TRUE`

func (r *Repository) ticketSaleForReversal(ctx context.Context, query string, args ...any) (*CustomerTicketSale, error) {
	var out CustomerTicketSale
	var clientTransactionID sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, query, args...).Scan(
		&out.ID, &out.EventID, &out.OrganizationID, &out.EventName, &out.ConfirmationRef,
		&out.Channel, &out.Status, &out.PaymentMethod, &out.SoldAt, &out.EventStartsAt,
		&clientTransactionID,
		&out.CustomerEmail, &out.CustomerFirstName, &out.CustomerLastName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out.ClientTransactionID = clientTransactionID.String
	return &out, nil
}

// ReversalRequest is one Customer's ask to undo their own paid Online Sale, as
// recorded before the Payment Provider was called (ADR 0024).
//
// It is a record with a lifecycle rather than an audit line: the same row is
// mutated as the platform keeps asking, one row per reversal and not one per
// attempt, because every probe asks the same question about the same money.
type ReversalRequest struct {
	ID           string
	TicketSaleID string
	// ClientTransactionID is the Payment this request is about, snapshotted at
	// the moment of the ask. Every probe presents this same id: re-deriving it
	// later could name a different Payment, and that is somebody's money.
	ClientTransactionID string
	// RequestedAt is when the Customer pressed Undo, and is what the buyer is
	// shown while the request is pending. Never ticket_sales.reversed_at; see the
	// column comment in migration 039.
	RequestedAt time.Time
	// Status is one of the sales.ReversalRequest* values.
	Status       string
	AttemptCount int
	// NextAttemptAt is the earliest instant the platform may ask the provider
	// again about this request. It is what makes the drain a throttle rather than
	// a loop; see ListDueReversalRequestsForCustomer.
	NextAttemptAt time.Time
	LastError     sql.NullString
}

// CreateReversalRequestInput records the ask: which sale, which Payment, and
// when the Customer pressed.
type CreateReversalRequestInput struct {
	TicketSaleID        string
	ClientTransactionID string
	RequestedAt         time.Time
}

// CreateReversalRequest writes a Reversal Request in flight. It is called BEFORE
// the Payment Provider is asked anything (ADR 0024); the call site in
// service.ReverseOwnSale is where that order is enacted and explained.
//
// The caller holds the advisory lock and has already established that this sale
// has no live request, so the partial unique index is not expected to fire here;
// if it ever does — a hash collision on the lock key, say — the error surfaces
// rather than being swallowed, because quietly proceeding would mean two open
// asks about one payment.
func (r *Repository) CreateReversalRequest(ctx context.Context, in CreateReversalRequestInput) (*ReversalRequest, error) {
	out := ReversalRequest{
		TicketSaleID:        in.TicketSaleID,
		ClientTransactionID: in.ClientTransactionID,
		RequestedAt:         in.RequestedAt,
		Status:              sales.ReversalRequestInFlight,
	}
	// next_attempt_at starts at the ask itself: the provider is called immediately
	// after this row lands, so the request is due from the instant it exists.
	err := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO sale_reversals (
			ticket_sale_id, client_transaction_id, requested_at,
			status, attempt_count, next_attempt_at
		)
		VALUES ($1, $2, $3, 'in_flight', 0, $3)
		RETURNING id, attempt_count, next_attempt_at
	`, in.TicketSaleID, in.ClientTransactionID, in.RequestedAt).Scan(
		&out.ID, &out.AttemptCount, &out.NextAttemptAt,
	)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetLiveReversalRequest returns the LIVE Reversal Request for one Ticket Sale,
// or nil when there is none.
//
// Live means `status <> 'refused'`, which is the definition the partial unique
// index `sale_reversals_live_per_sale_key` enforces, and it is deliberately the
// same words here. There is exactly one notion of "this sale already has an ask"
// in the system, and the database owns it: a reader using a narrower one — only
// `in_flight`, say — concludes there is no request where the index will refuse
// to let it write one, and the buyer gets a unique violation instead of an
// answer. That is not hypothetical. A `succeeded` request whose local commit
// failed is exactly SALE_REVERSAL_NOT_COMMITTED (#162), and a `needs_attention`
// one is an Unresolved Reversal awaiting a Platform Operator; both are live rows
// about money that has very possibly already moved, and neither may read as
// absent.
//
// `refused` is the one status that is not live, for the reason the index
// excludes it: a refusal means nothing happened, so a buyer still inside their
// Reversal Window is entitled to a genuinely new ask.
//
// It is what makes a second press a READ. A buyer who presses Undo again while
// the platform is still finding out gets the state of the ask they already made
// — no second row, and above all no second call to the Payment Provider, which
// could return their money twice.
func (r *Repository) GetLiveReversalRequest(ctx context.Context, ticketSaleID string) (*ReversalRequest, error) {
	var out ReversalRequest
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, ticket_sale_id, client_transaction_id, requested_at,
		       status, attempt_count, next_attempt_at, last_error
		FROM sale_reversals
		WHERE ticket_sale_id = $1 AND status <> 'refused'
	`, ticketSaleID).Scan(
		&out.ID, &out.TicketSaleID, &out.ClientTransactionID, &out.RequestedAt,
		&out.Status, &out.AttemptCount, &out.NextAttemptAt, &out.LastError,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDueReversalRequestsForCustomer returns the in-flight Reversal Requests
// belonging to one Customer's own Ticket Sales that are DUE to be asked about
// again, oldest ask first, at most limit of them.
//
// customerID is the sole scope and it comes from the Customer Session. This is
// the opportunistic drain: a buyer loading the Customer Area makes the platform
// ask the Payment Provider again about their own stuck reversal, which is what
// makes the common case resolve in seconds rather than waiting for a Reversal
// Reconciler tick.
//
// DUE is what keeps that from being an attack the buyer performs on themselves.
// A probe costs a ten-second provider timeout and they run serially, so without
// `next_attempt_at` a Customer refreshing their purchases during a provider
// outage re-posts Reverse on every render and hangs their own page doing it.
// The column has always been written on every attempt; reading it here is what
// makes it mean something. The SCHEDULE that spaces the retries out lives in
// sales.ReversalRetryDelay; what this query owns is only that a request asked
// about a moment ago is not asked about again now.
//
// `now` is the caller's clock rather than the database's, for the same reason
// every other decision in this module takes one: the instant a reversal is
// judged against must be the instant the service is reasoning about, and a test
// that cannot move it cannot prove a throttle throttles.
//
// limit bounds what a single page load may spend on somebody else's provider.
// The requests beyond it are not lost: the ordering is oldest-ask-first, so what
// the limit leaves behind are the NEWER asks, and the next render or the
// Reconciler takes them up.
//
// It deliberately cannot reach anybody else's request. The unscoped claim query
// a Reconciler needs is a different query, and giving this one an optional
// customer filter is how it would become one by accident.
func (r *Repository) ListDueReversalRequestsForCustomer(ctx context.Context, customerID string, now time.Time, limit int) ([]ReversalRequest, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT sr.id, sr.ticket_sale_id, sr.client_transaction_id, sr.requested_at,
		       sr.status, sr.attempt_count, sr.next_attempt_at, sr.last_error
		FROM sale_reversals sr
		JOIN ticket_sales ts ON ts.id = sr.ticket_sale_id
		WHERE ts.customer_id = $1
		  AND sr.status = 'in_flight'
		  AND sr.next_attempt_at <= $2
		ORDER BY sr.requested_at
		LIMIT $3
	`, customerID, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ReversalRequest, 0)
	for rows.Next() {
		var req ReversalRequest
		if err := rows.Scan(
			&req.ID, &req.TicketSaleID, &req.ClientTransactionID, &req.RequestedAt,
			&req.Status, &req.AttemptCount, &req.NextAttemptAt, &req.LastError,
		); err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, rows.Err()
}

// ClaimDueReversalRequest takes ONE unfinished Reversal Request that is due to
// be worked again — the longest overdue first — and hands it to this caller
// alone. It returns nil when nothing is due, which is the Reversal Reconciler's
// ordinary answer and the reason a drain against an empty queue costs one query.
//
// UNFINISHED is two states and not one, and the second arm is the whole of
// #162. A request the provider ACCEPTED whose Ticket Sale is still active is a
// reversal that only half happened: the money went back and the local write did
// not, so the buyer keeps both their refund and their tickets
// (SALE_REVERSAL_NOT_COMMITTED). Nothing is left to ask anybody — the answer is
// recorded on the row — so what the queue hands out here is a LOCAL COMMIT to
// finish, and the service pursues it without a provider in reach
// (service.finishAgreedReversal).
//
// `sale_voided_at IS NULL` is what keeps that arm from being every reversal ever
// made. Every completed reversal leaves a succeeded row behind for good, and
// what distinguishes the broken ones is that the sale never followed the money
// (migration 040).
//
// DUE HAS TO MEAN "NEEDS WORK", and that is why the test is a column here rather
// than a lookup at the sale. Asking `EXISTS (… ticket_sales … status = 'active')`
// answers the same question and makes every tick read the whole history of both
// tables: measured on this schema at 200k ticket_sales and 30k succeeded
// reversals, that plan is a sequential scan of sale_reversals over a sequential
// scan of ticket_sales — 5,300 shared buffer hits and 92ms with NOTHING STUCK,
// every minute, forever. Against the column it is a BitmapOr of two partial
// indexes on next_attempt_at, each covering exactly the rows that need working
// (sale_reversals_due_idx for in-flight, sale_reversals_uncommitted_idx for
// succeeded-and-uncommitted): 21 buffer hits and 0.24ms on the same data.
//
// A BitmapOr discards index order, so the ORDER BY below is a real Sort rather
// than a walk of an already-ordered index. That is deliberate and cheap: it sorts
// only the rows that are actually due, which is the whole point of the column —
// a queue the size of what is broken. It is worth knowing before anybody reads
// the plan and assumes the index provides the ordering.
//
// The in-flight arm carries no such test, and must not. An in-flight request
// over a sale somebody else reversed is exactly the case the platform gives up
// on rather than probes, and being claimed is how it becomes the Unresolved
// Reversal an operator can read (service.resolveReversalRequest); filtering it
// out here would leave it in flight forever with nobody looking.
//
// It is UNSCOPED, unlike ListDueReversalRequestsForCustomer: the Reconciler has
// no Customer and pursues whatever is stuck. The two are separate functions
// rather than one with an optional filter, because a customer scope that can be
// passed as empty is a customer scope that will be, on the path where it is the
// authorization.
//
// CLAIMING, not listing, and the difference is what makes more than one Cloud
// Run instance useful. The per-sale advisory lock already makes a second probe
// on one sale IMPOSSIBLE, so listing would be safe — and useless: two instances
// reading the same overdue-first list would walk the same rows in the same
// order, and the one that lost every advisory lock would spend its whole run
// skipping rows it can never take while the backlog behind them waits. A claim
// is how the second instance finds different work.
//
// It is claimed by pushing next_attempt_at forward by a lease, so "somebody is
// working on this" becomes a fact the query itself can see. An advisory lock
// cannot say it: it is held on another connection and is invisible to SQL, which
// is exactly why it can guard the provider call and not the queue. The lease is
// also what stops the OPPORTUNISTIC drain from picking up a request the
// Reconciler is mid-probe on, since that drain reads the same due column.
//
// FOR UPDATE SKIP LOCKED is required and not decoration. Without it, two
// concurrent claims choose the same row: the second blocks on the row lock, and
// when it is released Postgres re-checks the outer `id = <that id>`, which still
// holds, so both statements update and both return the same request. SKIP LOCKED
// makes the loser choose a different row instead of waiting for one it should
// not have.
//
// leaseUntil is when an unfinished claim falls due again. What it is for, and
// what it deliberately is not, is service.reversalClaimLease.
func (r *Repository) ClaimDueReversalRequest(ctx context.Context, now, leaseUntil time.Time) (*ReversalRequest, error) {
	var out ReversalRequest
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE sale_reversals
		SET next_attempt_at = $2
		WHERE id = (
			SELECT sr.id
			FROM sale_reversals sr
			WHERE sr.next_attempt_at <= $1
			  AND (
			    sr.status = 'in_flight'
			    OR (sr.status = 'succeeded' AND sr.sale_voided_at IS NULL)
			  )
			ORDER BY sr.next_attempt_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, ticket_sale_id, client_transaction_id, requested_at,
		          status, attempt_count, next_attempt_at, last_error
	`, now, leaseUntil).Scan(
		&out.ID, &out.TicketSaleID, &out.ClientTransactionID, &out.RequestedAt,
		&out.Status, &out.AttemptCount, &out.NextAttemptAt, &out.LastError,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ReleaseReversalRequestClaim undoes a claim: the caller took this request out
// of the queue and could not probe it, so it goes back with a short delay
// instead of serving out the crash lease.
//
// leaseUntil is the value the claim wrote, and matching on it is what makes this
// safe to call after the fact. If anything else has touched the row since — the
// actor that held the advisory lock finishing its own probe and writing the
// backoff its answer earned — the WHERE fails, nothing is written, and that
// answer stands. A blind write here would throw away the outcome of a call
// somebody already paid a Payment Provider to make.
//
// The status guard is the queue's own membership, in the same two states
// ClaimDueReversalRequest hands out: a request that has stopped being worked —
// refused, or an Unresolved Reversal — is not put back into a queue it has left.
// A `succeeded` one may well still be in it, since a reversal whose local commit
// failed is claimed and can be contended exactly like any other.
func (r *Repository) ReleaseReversalRequestClaim(ctx context.Context, id string, leaseUntil, nextAttemptAt time.Time) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE sale_reversals
		SET next_attempt_at = $3
		WHERE id = $1 AND status IN ('in_flight', 'succeeded') AND next_attempt_at = $2
	`, id, leaseUntil, nextAttemptAt)
	return err
}

// CountInFlightReversalRequests reports how many Reversal Requests are still
// open and when the oldest of them was asked. The time is null exactly when the
// count is zero.
//
// It is the backlog, and it is a different question from the Unresolved Reversal
// queue below: that queue is what the platform has GIVEN UP on, and it stays
// empty for the first day of any outage. This is what is still being pursued,
// which during an outage is the number that moves.
//
// A count rather than a page of rows, because nobody acts on an individual
// in-flight request — it is being handled — and a page of a thousand of them
// would answer a question nobody asked.
//
// The predicate is the one sale_reversals_due_idx is partial on (migration 039:
// on next_attempt_at WHERE status = 'in_flight'), so what is walked is the
// backlog rather than the history. MIN(requested_at) is not in that index and
// costs a heap fetch per row, which is a fetch per stuck reversal — the same
// order as the queue this endpoint just worked.
func (r *Repository) CountInFlightReversalRequests(ctx context.Context) (total int, oldestRequestedAt sql.NullTime, err error) {
	err = r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*), MIN(requested_at)
		FROM sale_reversals
		WHERE status = 'in_flight'
	`).Scan(&total, &oldestRequestedAt)
	if err != nil {
		return 0, sql.NullTime{}, err
	}
	return total, oldestRequestedAt, nil
}

// UnresolvedReversal is one Reversal Request the platform gave up on with the
// answer still unknown, as the operator who has to settle it needs to see it.
//
// Every field is there to be acted on rather than displayed. The client
// transaction id is what finds the reversal on the Payment Provider's own
// dashboard — the only place that can say whether the money left — and the last
// error is what the provider was saying when the platform stopped asking. The
// Sale Confirmation reference is what a buyer would quote, and what an Operator
// Reversal (ADR 0019) is keyed on if the answer turns out to be that it did
// leave.
type UnresolvedReversal struct {
	ID                  string
	TicketSaleID        string
	ConfirmationRef     string
	ClientTransactionID string
	RequestedAt         time.Time
	AttemptCount        int
	LastError           sql.NullString
	// SaleStatus is where the Ticket Sale stands. It is nearly always 'active' —
	// nothing is known to have moved — and it is read anyway, because the one
	// other route into this state is a sale somebody else reversed mid-flight.
	SaleStatus string
}

// ListUnresolvedReversals reads the Unresolved Reversal queue, oldest ask first,
// and reports how long the queue is regardless of the limit.
//
// Oldest first because that is how the queue is worked: the buyer who has been
// waiting longest for somebody to find out what happened to their money is the
// one to settle next. The total is returned alongside so a truncated page never
// reads as an empty backlog — an operator seeing twenty rows must know whether
// there are twenty or two hundred.
func (r *Repository) ListUnresolvedReversals(ctx context.Context, limit int) (out []UnresolvedReversal, total int, err error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT sr.id, sr.ticket_sale_id, ts.confirmation_ref, sr.client_transaction_id,
		       sr.requested_at, sr.attempt_count, sr.last_error, ts.status,
		       COUNT(*) OVER () AS total
		FROM sale_reversals sr
		JOIN ticket_sales ts ON ts.id = sr.ticket_sale_id
		WHERE sr.status = 'needs_attention'
		ORDER BY sr.requested_at, sr.id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out = make([]UnresolvedReversal, 0)
	for rows.Next() {
		var u UnresolvedReversal
		if err := rows.Scan(
			&u.ID, &u.TicketSaleID, &u.ConfirmationRef, &u.ClientTransactionID,
			&u.RequestedAt, &u.AttemptCount, &u.LastError, &u.SaleStatus, &total,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

// ReversalRequestAttempt is what one probe of the Payment Provider learned: the
// status the request now stands in, and what the provider said if it was not a
// success.
type ReversalRequestAttempt struct {
	ID string
	// Status is where the ask now stands — one of the sales.ReversalRequest*
	// values. Passing in_flight again is the ordinary unknown outcome: the
	// attempt is counted and the error kept, and the request stays open.
	Status string
	// LastError is the provider's own words, for the operator reading the queue
	// during an incident. Empty on success.
	LastError string
	Now       time.Time
	// NextAttemptAt is the earliest instant the platform may ask again, and it is
	// READ — the drain pursues only requests that are due. On an unknown outcome
	// it is what stops the next page load re-posting Reverse; once the status is
	// definite it is meaningless, and written anyway so the column never carries
	// a stale due date.
	NextAttemptAt time.Time
}

// RecordReversalRequestAttempt applies the outcome of one probe: the attempt is
// counted whatever it answered, and the status moves — or deliberately does not.
//
// Counting the attempt on every path is what makes the row an account of what
// the platform actually did rather than of what it concluded. An unknown outcome
// leaves the status in_flight and is the case the whole table exists for: the
// question is still open, so it stays open.
//
// The write is guarded on the row still being in_flight, so a probe that raced
// another actor to a definite answer cannot overwrite it. Two actors can only
// reach one request through a lock hash collision, and the answer that landed
// first is the one that acted on the money.
func (r *Repository) RecordReversalRequestAttempt(ctx context.Context, in ReversalRequestAttempt) error {
	return r.recordReversalAttempt(ctx, in, sales.ReversalRequestInFlight)
}

// RecordReversalCommitAttempt applies the outcome of one attempt to FINISH a
// reversal the Payment Provider has already agreed to — the local write that
// failed after the money went back (#162).
//
// It is the same write under a different guard, and the guard is the whole
// difference: this row is `succeeded` and stays `succeeded` while the platform
// keeps trying, so the in-flight guard above would silently write nothing here
// and the retry would never back off or ever end. What it must still refuse is a
// row that has moved on — a Ticket Sale reversed by another actor in the
// meantime leaves nothing to finish, and an Unresolved Reversal is not pursued
// again.
func (r *Repository) RecordReversalCommitAttempt(ctx context.Context, in ReversalRequestAttempt) error {
	return r.recordReversalAttempt(ctx, in, sales.ReversalRequestSucceeded)
}

// RecordUnaskableReversalAttempt ends a pass that could not work the request at
// all — the sale was held by somebody else, or it has vanished — where `from` is
// the status the row was read in.
//
// The guard has to FOLLOW THE ROW here, and cannot be either constant above,
// because both kinds of unfinished request reach this: the queue hands out
// in-flight ones and succeeded-but-uncommitted ones alike, and a contended claim
// is contended whichever it was. A fixed `in_flight` guard would match nothing on
// a succeeded row, and the pass that thought it had queued an incident for a
// human would have written nothing at all.
func (r *Repository) RecordUnaskableReversalAttempt(ctx context.Context, in ReversalRequestAttempt, from string) error {
	return r.recordReversalAttempt(ctx, in, from)
}

// ErrReversalRequestMovedOn is a guarded write that matched no row: the request
// is no longer in the status the caller read it in, so somebody else answered it
// first and nothing was written.
//
// It is an error rather than a silent success because the callers act on having
// written — they log an incident, they count an outcome, they tell a buyer their
// refund was refused — and every one of those is a lie about a row that did not
// move. Whether it is a FAILURE is the caller's to decide: under the per-sale
// advisory lock it should be unreachable, while the pass that gives up on a
// request it could not ask about races nobody and simply loses (see
// service.ageUnaskableReversalRequest).
var ErrReversalRequestMovedOn = errors.New("the Reversal Request is no longer in the status this write was guarded on")

// recordReversalAttempt is the one write every entry point above makes. `from` is
// the status the row must still be in for it to land, and it is a parameter
// rather than a second statement so that the counting, the schedule and the error
// message can never drift apart between the things the platform retries.
func (r *Repository) recordReversalAttempt(ctx context.Context, in ReversalRequestAttempt, from string) error {
	result, err := r.db.Pool.ExecContext(ctx, `
		UPDATE sale_reversals
		SET status = $2,
		    attempt_count = attempt_count + 1,
		    next_attempt_at = $3,
		    last_error = NULLIF($4, ''),
		    updated_at = $5
		WHERE id = $1 AND status = $6
	`, in.ID, in.Status, in.NextAttemptAt, truncateReversalError(in.LastError), in.Now, from)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrReversalRequestMovedOn
	}
	return nil
}

// MarkReversalSaleVoided records that the Ticket Sale this request is about is
// voided, which is what takes a succeeded request out of the Reconciler's queue
// (see ClaimDueReversalRequest).
//
// It writes no status and counts no attempt: the status already says succeeded,
// which is still exactly what the Payment Provider answered, and there was no
// attempt at anybody to count.
//
// It is idempotent and deliberately unguarded on the existing value. Two actors
// observing the same voided sale write the same fact, and the older observation
// is not worth keeping over the newer one — nothing reads the instant, only its
// presence.
func (r *Repository) MarkReversalSaleVoided(ctx context.Context, id string, at time.Time) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE sale_reversals
		SET sale_voided_at = $2, updated_at = $2
		WHERE id = $1
	`, id, at)
	return err
}

// reversalErrorLimit is what `sale_reversals.last_error` accepts (migration
// 039). A provider error is a line; the constraint refuses a pasted stack trace,
// and a Reversal Request must never fail to record its outcome because the
// message that came back was long.
const reversalErrorLimit = 500

// truncateReversalError bounds a provider's message to what the column holds,
// counting runes rather than bytes so a Spanish refusal cannot be cut mid-
// character — PayPhone's messages are Spanish, and char_length counts characters.
func truncateReversalError(msg string) string {
	runes := []rune(msg)
	if len(runes) <= reversalErrorLimit {
		return msg
	}
	return string(runes[:reversalErrorLimit])
}
