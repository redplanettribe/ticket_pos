package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// TicketSaleLineRow is one Ticket Type and the quantity bought within a Ticket
// Sale, as the Customer Area shows it.
type TicketSaleLineRow struct {
	TicketTypeName string `json:"ticket_type_name"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int    `json:"unit_price_cents"`
}

// TicketSaleRow is one of a Customer's Ticket Sales with everything the Customer
// Area needs to be meaningful at a glance: the Event and its date, the
// Organization that sold it, what was bought, and the Sale Confirmation
// reference the Customer can quote to a promoter.
type TicketSaleRow struct {
	ID              string
	ConfirmationRef string
	// Channel is the Sales Channel the sale was recorded on: 'online',
	// 'in_person' or 'import'. The Customer Area reads it for one reason — the
	// Reversal Window belongs to an Online Sale alone, since a sale the platform
	// never collected for is not a sale the platform can undo (ADR 0018).
	Channel string
	// SoldAt on an Online Sale is the instant its Payment was approved: the sale
	// is committed in the SAME transaction that flips the Payment to approved, so
	// there is no second instant to carry. That equality is what lets the
	// Reversal Window be computed from this row without reading `payments`.
	SoldAt time.Time
	// PaymentMethod is how the sale was settled — 'free' when a zero-total
	// checkout was settled by the platform itself (ADR 0017), otherwise the name
	// of the Payment Provider that collected the money. Null on the channels that
	// carry none.
	//
	// The Customer Area reads it for one reason: whether a Sale Reversal is on
	// offer depends on whether that Payment can actually be undone, and that is a
	// property of whoever settled it (ADR 0018). Nothing about the window is
	// decided here — an open window on a Payment nobody can reverse is still an
	// open window, it is simply not an offer.
	PaymentMethod    sql.NullString
	Status           string
	AmountCents      int
	Currency         string
	Lines            []TicketSaleLineRow
	EventID          string
	EventName        string
	EventSlug        string
	EventStartsAt    sql.NullTime
	EventEndsAt      sql.NullTime
	EventTimezone    sql.NullString
	EventVenueName   sql.NullString
	OrganizationID   string
	OrganizationName string
	OrganizationSlug string
	// The Tax ID snapshot the sale was transacted under, both halves null
	// together on a legacy or imported sale that carries none (ADR 0016). It is
	// read off the sale and never off the Customer record, so a profile edit or
	// a later purchase leaves what an old sale shows untouched.
	TaxIDType   sql.NullString
	TaxIDNumber sql.NullString
	// ReversalPending is true exactly while a reversal of this sale is in
	// progress: the Customer asked to undo it, and the platform is still working
	// on it (ADR 0024). That is an `in_flight` or `succeeded` Reversal Request
	// over a sale that is still active.
	//
	// Both halves are needed and neither is redundant. A request alone would
	// still be true of a reversal that completed, since a `succeeded` request
	// stands over its now-reversed sale forever; an active sale alone says
	// nothing. Together they name the one situation a card must draw differently:
	// the tickets still work, and we are still working on the refund. A
	// `succeeded` request over an ACTIVE sale is SALE_REVERSAL_NOT_COMMITTED
	// (#162) and belongs here too — the buyer is owed a refund that has not
	// finished landing, which is precisely what this flag says.
	//
	// `needs_attention` is deliberately NOT one of them, and this is the one
	// place where the display question and the database's live-per-sale index
	// part company. That index counts an Unresolved Reversal as live because
	// nothing may be written past it — the money's fate is unknown, so a second
	// ask could refund twice — but nothing is in progress about it: the platform
	// has stopped asking and is waiting on a Platform Operator. Borrowing the
	// index's predicate here would tell the buyer "refund in progress" forever on
	// the one reversal that has permanently stopped moving. What the Customer
	// gets instead is a sale that looks untouched, which it is, and a typed
	// refusal if they press Undo (sales.ErrReversalUnresolved).
	//
	// It changes nothing about the sale itself, and the row above shows exactly
	// that: the status is still `active`, the capacity is still held, and the
	// tickets are still valid, because no money is known to have moved. The only
	// thing this flag entitles a surface to do is withdraw the Undo action and
	// say a refund is being processed.
	//
	// It is read from `sale_reversals`, which the sales module owns, for the same
	// reason this file already reads `payments`: the Customer Area is a
	// composition of what a buyer's purchase looks like across the system, and a
	// second round trip through another module to colour one card would buy
	// nothing.
	ReversalPending bool
	// ReversalStatus is where this sale's most recent Reversal Request stands,
	// null when the buyer has never asked (ADR 0024).
	//
	// ReversalPending answers "is a refund being worked on"; this answers "how did
	// the ask end", which a surface needs precisely where the two disagree. An
	// Unresolved Reversal leaves the sale active and ReversalPending false, so a
	// surface reading only that flag sees a pending refund silently vanish and
	// cannot tell it apart from a refusal — and telling a buyer their refund was
	// refused is the one thing the platform must never say about an Unresolved
	// Reversal, since nobody knows whether the money went back.
	//
	// It carries the request's own word, unmapped, so a surface decides what to
	// say and this file does not decide for it. `refused` is the only value any
	// surface may present as a refusal.
	ReversalStatus sql.NullString
}

// ReversalFacts narrows a Customer Area row to the reversal rule's inputs, so the
// Area, the guest surfaces and the reversal endpoint in the sales module all feed
// the same function — platform.PaymentReversal.EligibilityAt — rather than
// carrying copies of one paragraph of ADR 0018.
func (s TicketSaleRow) ReversalFacts() platform.SaleReversalFacts {
	return platform.SaleReversalFacts{
		Channel:       s.Channel,
		Status:        s.Status,
		SoldAt:        s.SoldAt,
		PaymentMethod: s.PaymentMethod,
		EventStartsAt: s.EventStartsAt,
	}
}

// CheckoutReversalFacts is the Ticket Sale one online checkout produced: which
// sale it is, and the facts that decide whether that sale can be undone.
//
// The id travels beside the facts rather than inside them, because
// platform.SaleReversalFacts is deliberately the rule's inputs and nothing else
// — the id decides nothing. It is here so the checkout success page can send a
// buyer to their own sale in the Customer Area rather than to the whole list
// (#121), and it goes no further than that: it is useless without a Customer
// Session, and it names a sale rather than a person.
type CheckoutReversalFacts struct {
	// TicketSaleID is the sale the checkout produced.
	TicketSaleID string
	// Sale is what the Reversal Window is decided from.
	Sale platform.SaleReversalFacts
}

// ReversalFactsForCheckout returns the Ticket Sale one online checkout produced
// and its reversal facts, or nil when that checkout produced none.
//
// The key is our own client transaction id: the identifier begin-checkout
// generated, the Storefront kept in an httpOnly cookie for the length of the
// round trip, and the Payment Provider's return leg carried back. It is the only
// thing a guest who has just paid actually holds — they have no Customer Session,
// which is the whole problem #121 exists to solve — and it names one checkout
// rather than a person, so it can widen to nothing.
//
// Nil is the ordinary answer for a pending, failed or expired Payment, for a
// checkout that never existed, and for the loudly-logged incident where an
// approved Payment has no sale. The caller reports "no undo on offer" for all of
// them alike; distinguishing them would say more to an anonymous caller than the
// deadline they came for.
func (r *Repository) ReversalFactsForCheckout(ctx context.Context, clientTransactionID string) (*CheckoutReversalFacts, error) {
	var found CheckoutReversalFacts
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT ts.id, ts.channel, ts.status, ts.sold_at, ts.payment_method, e.starts_at
		FROM payments p
		JOIN ticket_sales ts ON ts.id = p.ticket_sale_id
		JOIN events e ON e.id = ts.event_id
		WHERE p.client_transaction_id = $1
	`, clientTransactionID).Scan(
		&found.TicketSaleID,
		&found.Sale.Channel,
		&found.Sale.Status,
		&found.Sale.SoldAt,
		&found.Sale.PaymentMethod,
		&found.Sale.EventStartsAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &found, nil
}

// ListTicketSalesForCustomer returns every Ticket Sale belonging to one
// Customer, across every Organization.
//
// customerID is the sole scope, and it comes from the Customer Session — never
// from anything in the request. There is deliberately no Organization, email, or
// customer parameter on this query: the only identifier it accepts is the one the
// caller proved they own, so there is no argument through which one Customer's
// purchase history could be aimed at another's.
//
// ticketSaleID, when non-empty, narrows further to that single sale. It is the
// Confirmation Link scope and can only ever shrink the result, never widen it.
//
// Ticket Sale Lines are aggregated per sale in a lateral subquery so a multi-line
// sale stays one row rather than fanning out. The rows come back newest purchase
// first; the service splits them into upcoming and past and orders each half by
// Event date, since that is what a Customer reads the list by.
func (r *Repository) ListTicketSalesForCustomer(ctx context.Context, customerID, ticketSaleID string) ([]TicketSaleRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			ts.id,
			ts.confirmation_ref,
			ts.channel,
			ts.sold_at,
			ts.payment_method,
			ts.status,
			lines.amount_cents,
			lines.ticket_types,
			org.currency,
			e.id, e.name, e.slug, e.starts_at, e.ends_at, e.timezone, e.venue_name,
			org.id, org.name, org.slug,
			ts.customer_tax_id_type, ts.customer_tax_id_number,
			-- Whether a reversal of this sale is in progress (ADR 0024): a request
			-- the platform is still working on, over a sale that still stands.
			-- The two statuses are named rather than expressed as "not refused" —
			-- see ReversalPending above for why the fourth one is not one of them.
			--
			-- EXISTS rather than a join: the live-per-sale index makes at most one
			-- such row possible per sale, and the question asked here is only
			-- whether there is one — none of its columns are shown to a buyer.
			(ts.status = 'active' AND EXISTS (
				SELECT 1 FROM sale_reversals sr
				WHERE sr.ticket_sale_id = ts.id
				  AND sr.status IN ('in_flight', 'succeeded')
			)),
			-- Where this sale's most recent Reversal Request stands, or null when
			-- the buyer has never asked.
			--
			-- ORDER BY is load-bearing and not tidiness. The live-per-sale index
			-- bounds the non-refused rows at one, but it deliberately excludes
			-- 'refused' — a refusal means nothing happened, so a buyer inside the
			-- Window may ask again — and a sale can therefore carry several refused
			-- rows beside one live one. Without an order this would return an
			-- arbitrary one of them, and the arbitrary choice a surface would act on
			-- is whether to tell somebody their refund failed.
			--
			-- Most recent ask wins, because that is the one the buyer is waiting on:
			-- an older refusal they already saw is not news, and a live request
			-- always postdates the refusals that preceded it.
			(SELECT sr.status FROM sale_reversals sr
			  WHERE sr.ticket_sale_id = ts.id
			  ORDER BY sr.requested_at DESC
			  LIMIT 1)
		FROM ticket_sales ts
		JOIN events e ON e.id = ts.event_id
		JOIN organizations org ON org.id = ts.organization_id
		JOIN LATERAL (
			SELECT
				COALESCE(SUM(tsl.quantity * tsl.unit_price_cents), 0) AS amount_cents,
				COALESCE(
					json_agg(
						json_build_object(
							'ticket_type_name', tt.name,
							'quantity', tsl.quantity,
							'unit_price_cents', tsl.unit_price_cents
						)
						ORDER BY tt.sort_order, tt.name
					),
					'[]'::json
				) AS ticket_types
			FROM ticket_sale_lines tsl
			JOIN ticket_types tt ON tt.id = tsl.ticket_type_id
			WHERE tsl.ticket_sale_id = ts.id
		) lines ON TRUE
		WHERE ts.customer_id = $1
		  AND ($2 = '' OR ts.id = $2::uuid)
		ORDER BY ts.sold_at DESC, ts.id DESC
	`, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TicketSaleRow, 0)
	for rows.Next() {
		var s TicketSaleRow
		var linesJSON []byte
		if err := rows.Scan(
			&s.ID,
			&s.ConfirmationRef,
			&s.Channel,
			&s.SoldAt,
			&s.PaymentMethod,
			&s.Status,
			&s.AmountCents,
			&linesJSON,
			&s.Currency,
			&s.EventID, &s.EventName, &s.EventSlug, &s.EventStartsAt, &s.EventEndsAt, &s.EventTimezone, &s.EventVenueName,
			&s.OrganizationID, &s.OrganizationName, &s.OrganizationSlug,
			&s.TaxIDType, &s.TaxIDNumber,
			&s.ReversalPending,
			&s.ReversalStatus,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(linesJSON, &s.Lines); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// HeldTicketRow is one Ticket a Customer HOLDS rather than bought: an Event
// somebody else paid for and assigned to them, which they accepted (#325,
// parent #322, ADR 0046).
//
// A SEPARATE TYPE FROM TicketSaleRow, AND THE SEPARATION IS THE DISCLOSURE RULE.
// A Holder is not the party of record: the Ticket Sale, the money, the Sale
// Confirmation and the Reversal Window all stayed with the buyer, and CONTEXT.md
// is explicit that a Holder sees the Event, the Ticket Type and their own
// questions and never the buyer, the price, the Tax ID or the Sale's other
// Tickets. TicketSaleRow carries the reference, the amount, the Tax ID snapshot
// and every line of the sale — reusing it here would put all of them one `json:`
// tag away from a screen they must never reach.
//
// So this carries the Event, the Organization and the Ticket Type, and stops.
// There is no amount here, no confirmation_ref, no reversal state and no Undo:
// nothing a Holder can do about a purchase they did not make.
type HeldTicketRow struct {
	// TicketID is the Ticket itself. It is the only identifier here, and it is
	// the Holder's own handle on the thing they hold.
	TicketID         string
	AcceptedAt       time.Time
	TicketTypeName   string
	EventID          string
	EventName        string
	EventSlug        string
	EventStartsAt    sql.NullTime
	EventEndsAt      sql.NullTime
	EventTimezone    sql.NullString
	EventVenueName   sql.NullString
	OrganizationID   string
	OrganizationName string
	OrganizationSlug string
}

// ListHeldTicketsForCustomer returns every Ticket this Customer has accepted.
//
// customerID is the sole scope and comes from the Customer Session, exactly as
// on the sales read above: there is no email, Organization or Ticket parameter
// through which one person's tickets could be aimed at another's.
//
// THE JOIN IS ON holder_customer_id AND NEVER ON AN ADDRESS. That column is
// written only by the accept flow, from a click at the address (migration 080's
// CHECK refuses one without an acceptance), so what this lists is what somebody
// PROVED — never what a buyer typed about them. An address that was named and
// never accepted appears on nobody's Area, which is right: it names a person who
// has agreed to nothing, and it is purged when the Event starts.
//
// REVERSED SALES ARE EXCLUDED, and this is the one place a Holder's view differs
// from the buyer's. The buyer keeps a reversed sale on their Area because it is
// their financial record — they paid, and they were refunded, and both are facts
// about them. A Holder has no financial record here: what they had was a ticket,
// and a reversal means they no longer have it. CONTEXT.md: the Event leaves
// their Customer Area when they stop holding it. Leaving it there would tell
// somebody to turn up to an Event they cannot get into.
//
// A ticket the buyer REASSIGNED away needs no clause: reassignment clears
// holder_customer_id with the address (repository.AssignTicketToHolder), so the
// row simply stops matching.
//
// Ordered soonest Event first, then by acceptance, so the caller's split into
// upcoming and past has a stable order to work from.
func (r *Repository) ListHeldTicketsForCustomer(ctx context.Context, customerID string) ([]HeldTicketRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			tk.id, tk.accepted_at, tt.name,
			e.id, e.name, e.slug, e.starts_at, e.ends_at, e.timezone, e.venue_name,
			org.id, org.name, org.slug
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_types tt ON tt.id = l.ticket_type_id
		JOIN ticket_sales ts ON ts.id = l.ticket_sale_id
		JOIN events e ON e.id = ts.event_id
		JOIN organizations org ON org.id = ts.organization_id
		WHERE tk.holder_customer_id = $1
		  AND ts.status = 'active'
		  -- Never the Customer's own Self-held Ticket (ADR 0048): "tickets
		  -- someone gave you" is about other people's purchases, and their
		  -- own sits on its Ticket Sale one section up.
		  AND ts.customer_id <> $1
		ORDER BY e.starts_at ASC NULLS LAST, tk.accepted_at DESC, tk.id ASC
	`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]HeldTicketRow, 0)
	for rows.Next() {
		var t HeldTicketRow
		if err := rows.Scan(
			&t.TicketID, &t.AcceptedAt, &t.TicketTypeName,
			&t.EventID, &t.EventName, &t.EventSlug, &t.EventStartsAt, &t.EventEndsAt,
			&t.EventTimezone, &t.EventVenueName,
			&t.OrganizationID, &t.OrganizationName, &t.OrganizationSlug,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
