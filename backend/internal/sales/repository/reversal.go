package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
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
