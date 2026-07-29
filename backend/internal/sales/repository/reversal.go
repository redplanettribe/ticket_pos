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
	var out CustomerTicketSale
	var clientTransactionID sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
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
		) p ON TRUE
		WHERE ts.id = $1 AND ts.customer_id = $2
	`, saleID, customerID).Scan(
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
// Reconciler tick (#158).
//
// DUE is what keeps that from being an attack the buyer performs on themselves.
// A probe costs a ten-second provider timeout and they run serially, so without
// `next_attempt_at` a Customer refreshing their purchases during a provider
// outage re-posts Reverse on every render and hangs their own page doing it.
// The column has always been written on every attempt; reading it here is what
// makes it mean something. The SCHEDULE that spaces the retries out — 10s, 30s,
// 2m, 5m, 15m, then every 30m — is the Reversal Reconciler's (#158); what this
// query owns is only that a request asked about a moment ago is not asked about
// again now.
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
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE sale_reversals
		SET status = $2,
		    attempt_count = attempt_count + 1,
		    next_attempt_at = $3,
		    last_error = NULLIF($4, ''),
		    updated_at = $5
		WHERE id = $1 AND status = 'in_flight'
	`, in.ID, in.Status, in.NextAttemptAt, truncateReversalError(in.LastError), in.Now)
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
