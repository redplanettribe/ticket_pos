// Package repository provides hand-written SQL data access for the sales domain.
// Sales covers online, in-person, and import sales plus capacity logic.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// Repository provides SQL access for sales data.
type Repository struct {
	db *platform.DB
}

// New returns a repository backed by the given database pool.
func New(db *platform.DB) *Repository {
	return &Repository{db: db}
}

// CommitLine is one Ticket Sale Line to record within a Ticket Sale.
type CommitLine struct {
	TicketTypeID string
	Quantity     int
	// UnitPriceCents overrides the snapshot price; nil uses the catalog price.
	UnitPriceCents *int
	// Fee is the per-unit Platform Fee snapshot the line was sold under, frozen
	// at begin-checkout (ADR 0014). Nil on the channels that carry no fee — the
	// platform withholds only from money it actually held — and those lines
	// record their unit price as the base price with nothing withheld.
	Fee *sales.FeeSnapshot
	// Answers are the Ticket Questions this line's buyer answered at checkout,
	// carried off the Payment that is settling and written onto the Tickets this
	// line is about to mint, index by ordinal (#311, ADR 0044).
	//
	// EMPTY ON EVERY OTHER SALES CHANNEL, AND THAT IS NOT A GAP. A box office
	// sale and a Sale Import have no checkout form and no Payment to have held
	// anything: their Tickets are minted with every question outstanding, and the
	// holder answers by Answer Link afterwards exactly as an online buyer's
	// unanswered ones do. Only ApprovePaymentAndCommitSale ever fills this.
	Answers []LineAnswer
}

// CommitSale is one Ticket Sale to record on any Sales Channel: the buyer as
// transacted, the optional Payment Method, and the Ticket Sale Lines with their
// unit-price snapshots.
type CommitSale struct {
	// Customer is the buyer this sale was transacted with, whole (#111). Not all
	// of it is written onto the Ticket Sale, and the split is deliberate:
	//
	//   - Email and the two name halves are snapshotted onto the sale verbatim,
	//     as transacted, and no later profile edit rewrites them.
	//   - The Tax ID is snapshotted too, exactly once and never again, because
	//     ADR 0016 makes it a fiscal fact of the sale.
	//   - The phone and the self-asserted flag are NOT. They ride this struct
	//     only to reach UpsertCustomer below: the phone's destination is the
	//     Customer profile, so it prefills the buyer's next purchase (#107), and
	//     the flag is what tells that upsert whether this buyer had proven the
	//     email is theirs. Giving the phone a column here would cascade into the
	//     staff sales list, receipts, the Sale Import format and the public
	//     contract for a value none of them read (#103).
	Customer        platform.SaleCustomer
	PaymentMethod   string
	SoldAt          time.Time
	ConfirmationRef string
	// AffiliateLinkID credits this sale to the Affiliate Link the buyer reached
	// the Event page through, copied from the Payment that settled it. Empty on
	// every unattributed sale, and empty by construction on the in-person and
	// import channels — neither travels through a link, and neither has anywhere
	// to have carried a code from (#146).
	AffiliateLinkID string
	// Locale is the Sale Locale: the language of the Storefront page this sale
	// was completed on, copied from the Payment that settled it (ADR 0033).
	//
	// EMPTY IS A REAL ANSWER AND NOT A GAP. A box office sale and an import were
	// produced by no page, so there is nothing for them to record, and the column
	// is nullable precisely so they can say so — 'en' there would be an assertion
	// that the buyer chose English, and the Sale Locale outranks the Customer's
	// remembered language (see migration 059).
	Locale string
	Lines  []CommitLine
}

// UpsertCustomer creates or reuses the Customer for one Ticket Sale inside the
// batch's transaction and returns the Customer id. The sales service supplies it,
// bound to the customers service, so the cross-module call goes through that
// module's service rather than its repository.
//
// It takes the buyer whole rather than a growing row of positional facts (#111).
// The bundle carries values this package never stores — the phone, whose
// destination is the profile (#107), and the flag saying the buyer proved the
// email is theirs — because what may be written back is the customers module's
// rule, and it needs the whole person to apply it.
type UpsertCustomer func(ctx context.Context, tx *sql.Tx, customer platform.SaleCustomer, now time.Time) (string, error)

// CommitSalesInput is a set of prepared Ticket Sales to record on one Sales
// Channel — the channel-agnostic sale-commit spine's input. Source qualifies
// `import` sales; native channels leave it empty.
type CommitSalesInput struct {
	EventID        string
	OrganizationID string
	Channel        string
	Source         string
	Sales          []CommitSale
	Now            time.Time
	// UpsertCustomer resolves each sale's Customer within the transaction.
	// Required: every Ticket Sale must reference a Customer.
	UpsertCustomer UpsertCustomer
	// ExcludePaymentID names the Payment whose own commit this is, so its live
	// Capacity Hold is not counted against it — the hold converts into
	// sold_count instead of double-counting (ADR 0013). Empty for channels that
	// commit without a Payment (import, and in-person later), which must
	// respect every live hold.
	ExcludePaymentID string
	// SelfHeld makes one Ticket of each sale the buyer's own: the first Ticket
	// of the line whose Ticket Type sorts first in the catalog is assigned to
	// the buyer and accepted in the same transaction that mints it (ADR 0048).
	//
	// Set by the online checkout and by all three import routes while
	// TICKET_ASSIGNMENT_ENABLED is on (ADR 0055), and by nothing else: an
	// In-Person Sale's buyer has no surface to reassign from, so a Holder
	// written onto a door sale could be removed by nobody.
	//
	// THE FLAG IS THE WHOLE OF THE CHANNEL RULE. Nothing below reads
	// in.Channel to decide this and nothing should: the spine mints Tickets
	// the same way for every channel, and the decision about which channels
	// presume a Holder belongs to the services that know the flag.
	SelfHeld bool
}

// CommitInput is a fully-prepared Direct Sale Import to record atomically.
type CommitInput struct {
	EventID           string
	OrganizationID    string
	Source            string
	CreatedByMemberID string
	IdempotencyKey    string
	Sales             []CommitSale
	Now               time.Time
	// UpsertCustomer resolves each sale's Customer within the batch transaction.
	// Required: every Ticket Sale must reference a Customer.
	UpsertCustomer UpsertCustomer
	// SelfHeld makes each row's buyer the Holder of that row's Ticket 1, on the
	// spine's terms (ADR 0055). Set from TICKET_ASSIGNMENT_ENABLED by the
	// service, which is the only layer that knows it.
	SelfHeld bool
}

// RecordedSale is one Ticket Sale as actually written: its database id — which
// only exists once the row is inserted — alongside the customer identity and
// reference the Sale Confirmation needs.
//
// The id is what makes a Confirmation Link possible: the link names one Ticket
// Sale, and until the batch commits there is no sale to name.
type RecordedSale struct {
	ID string
	// CustomerID is the Customer this sale was recorded against, as the upsert
	// resolved them inside this very transaction.
	//
	// It is here for the online checkout's Consent Record (#253), which names a
	// Customer and can only be written once there IS one — the record and the
	// Customer are created in the same transaction, in that order, and the id is
	// the only thing that connects them. Nothing else reads it: the Sale
	// Confirmation addresses the email snapshotted on the sale, never the
	// Customer's current one.
	CustomerID        string
	ConfirmationRef   string
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
	// AmountCents is what the Customer paid for this sale — the sum of its
	// lines' buyer prices — so the Sale Confirmation can show a total that
	// matches their card statement.
	AmountCents int
	// CustomerTaxID is the Tax ID snapshot just written onto the sale, echoed
	// back so the Sale Confirmation prints what this sale was transacted under
	// rather than re-reading a Customer record that may already have moved on.
	// Unset on the `import` channel when the file carried no Tax ID.
	CustomerTaxID platform.SaleTaxID
	// Locale is the Sale Locale just written onto the sale, echoed back so the
	// Sale Confirmation can be written in it without re-reading the row it was
	// this instant inserted from (ADR 0033). Empty on every sale no page produced
	// — a box office sale, an import, and every sale recorded before the column
	// existed — which is what sends the resolution on to the Customer's
	// remembered language, and then to English.
	Locale string
}

// CommittedBatch is the outcome of a committed (or replayed) Sale Import.
type CommittedBatch struct {
	ID        string
	SaleCount int
	Status    string
	Replayed  bool
	// Recorded is the sales this call actually wrote, in row order. It is empty
	// on a replay, which recorded nothing new and must therefore re-send nothing.
	Recorded []RecordedSale
}

// CapacityError reports that a Ticket Type would be oversold by the committed
// sales. Row is the index of the first sale referencing it.
type CapacityError struct {
	Row          int
	TicketTypeID string
	Requested    int
	Available    int
}

func (e *CapacityError) Error() string {
	return fmt.Sprintf("ticket type %s oversold: requested %d, available %d", e.TicketTypeID, e.Requested, e.Available)
}

// UnknownTicketTypeError reports that a sale references a Ticket Type absent from the Event.
type UnknownTicketTypeError struct {
	Row          int
	TicketTypeID string
}

func (e *UnknownTicketTypeError) Error() string {
	return fmt.Sprintf("unknown ticket type %s", e.TicketTypeID)
}

// GetEventName returns the Event's name and whether it belongs to the Organization.
func (r *Repository) GetEventName(ctx context.Context, orgID, eventID string) (string, bool, error) {
	var name string
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT name FROM events WHERE id = $1 AND organization_id = $2
	`, eventID, orgID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return name, true, nil
}

// EventImportContext is the Event metadata a Sale Import needs: its name, the
// timezone (nullable) used to interpret naive sold_at values, and its schedule.
type EventImportContext struct {
	Name string
	// Slug is the Event's URL-safe name, already unique within the Organization.
	// It rides along because a file leaving the platform is named after the
	// Event, and the slug is the one form of the name that is filename-safe by
	// construction rather than by a reduction the download has to invent.
	Slug     string
	Timezone string
	// Currency is the Organization's currency, which the Sale Confirmation's
	// total is denominated in.
	Currency string
	// StartsAt and EndsAt place the Event in time. A Sale Confirmation's
	// Confirmation Link must outlive the Event it is for, so the moment the Event
	// finishes is what its expiry is derived from. Both are nullable: an Event
	// may be scheduled loosely or not at all.
	StartsAt sql.NullTime
	EndsAt   sql.NullTime
	// RegistrationMode is how the Event takes sign-ups (ADR 0028). It rides along
	// with the rest of the context because every import operation must answer
	// "does this Event sell tickets at all?" before it looks at a single Ticket
	// Type, and a second query for one column would be a second round trip to
	// learn something this row already knows.
	//
	// Raw as stored: catalog.RegistrationModeOrDefault interprets it, so a value
	// this binary does not recognise reads as an ordinary ticketed Event rather
	// than locking the Organization out of its own imports.
	RegistrationMode string
}

// End is the moment the Event finishes, or the zero time when it has no
// schedule to place it by. An Event with only a start is over once it has
// started, as far as anything downstream of this needs to know.
func (e *EventImportContext) End() time.Time {
	switch {
	case e.EndsAt.Valid:
		return e.EndsAt.Time
	case e.StartsAt.Valid:
		return e.StartsAt.Time
	default:
		return time.Time{}
	}
}

// GetEventImportContext returns the Event's name, slug, timezone, schedule, and
// registration mode, and whether it belongs to the Organization.
func (r *Repository) GetEventImportContext(ctx context.Context, orgID, eventID string) (*EventImportContext, bool, error) {
	var out EventImportContext
	var tz sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT e.name, e.slug, e.timezone, o.currency, e.starts_at, e.ends_at, e.registration_mode
		FROM events e
		JOIN organizations o ON o.id = e.organization_id
		WHERE e.id = $1 AND e.organization_id = $2
	`, eventID, orgID).Scan(&out.Name, &out.Slug, &tz, &out.Currency, &out.StartsAt, &out.EndsAt, &out.RegistrationMode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	out.Timezone = tz.String
	return &out, true, nil
}

// EventTicketType is a Ticket Type as the sales domain reads it: what it is
// priced at, and how much of its capacity is already spoken for. It serves
// every channel that starts from the Event's catalog — matching import rows and
// computing their capacity impact, and pricing an online cart at
// begin-checkout.
type EventTicketType struct {
	ID         string
	Name       string
	PriceCents int
	Capacity   int
	SoldCount  int
	// Promotion is the Ticket Type's Promotion slot (ADR 0021), nil when empty
	// and carrying its own window when filled — being present is not being live,
	// which only an instant can answer. It is read here rather than in a second
	// query so a cart of any size is still priced from one catalog read.
	//
	// Only the channels that price from the catalog consult it: a Sale Import
	// carries its own amounts and ignores this, as it ignores the List Price.
	Promotion *catalog.Promotion
	// MaxPerCustomer is the Ticket Type's Purchase Limit — the most of it one
	// Customer may hold at once — and nil is the unrestricted state every Ticket
	// Type is in until an Org Admin says otherwise (ADR 0025, migration 041).
	//
	// It rides this struct rather than a catalog read of its own because checkout
	// already loads the Event's Ticket Types here to price the cart, and the
	// refusal must be decided from the same row the price came from. nil is a
	// distinct value and not a zero: no arithmetic may be done on "not rationed".
	MaxPerCustomer *int
	// SortOrder is the Ticket Type's place in the Event's catalog, the order an
	// Org Admin arranged it in. It rides along because a surface that names the
	// catalog back to staff — Sales Trends' legend — must state the position as
	// well as the order it happened to receive the rows in, so a client can
	// colour a Ticket Type by its place and keep that colour across loads.
	SortOrder int
}

// ListEventTicketTypes returns the Event's Ticket Types with their Promotion
// slots, in catalog display order, for import template generation and
// validation, for online checkout pricing, and for the Sales Trends legend.
func (r *Repository) ListEventTicketTypes(ctx context.Context, orgID, eventID string) ([]EventTicketType, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT tt.id, tt.name, tt.price_cents, tt.capacity, tt.sold_count,
		       tt.sort_order, tt.max_per_customer,
		       p.promotional_price_cents, p.starts_at, p.ends_at
		FROM ticket_types tt
		LEFT JOIN ticket_type_promotions p ON p.ticket_type_id = tt.id
		WHERE tt.event_id = $1 AND tt.organization_id = $2
		ORDER BY tt.sort_order, tt.name
	`, eventID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EventTicketType
	for rows.Next() {
		var tt EventTicketType
		var maxPerCustomer sql.NullInt64
		var promotionalPriceCents sql.NullInt64
		var startsAt, endsAt sql.NullTime
		if err := rows.Scan(
			&tt.ID, &tt.Name, &tt.PriceCents, &tt.Capacity, &tt.SoldCount,
			&tt.SortOrder, &maxPerCustomer,
			&promotionalPriceCents, &startsAt, &endsAt,
		); err != nil {
			return nil, err
		}
		if maxPerCustomer.Valid {
			limit := int(maxPerCustomer.Int64)
			tt.MaxPerCustomer = &limit
		}
		// The end is NOT NULL on a Promotion row, so the join either produced a
		// whole Promotion or none at all.
		if promotionalPriceCents.Valid && endsAt.Valid {
			promotion := &catalog.Promotion{
				PromotionalPriceCents: int(promotionalPriceCents.Int64),
				EndsAt:                endsAt.Time,
			}
			if startsAt.Valid {
				start := startsAt.Time
				promotion.StartsAt = &start
			}
			tt.Promotion = promotion
		}
		out = append(out, tt)
	}
	return out, rows.Err()
}

// ExistingSaleKey is the identity of an existing active Ticket Sale used for
// soft duplicate detection: buyer email, Ticket Type, and the raw sold_at
// (the caller buckets it to a date in the Event timezone).
type ExistingSaleKey struct {
	CustomerEmail string
	TicketTypeID  string
	SoldAt        time.Time
}

// ListActiveSaleKeys returns the (email, ticket type, sold_at) tuples of every
// active Ticket Sale on the Event, for soft duplicate detection in the preview.
// excludeSaleID, when non-empty, leaves one sale out: the Sale Correction
// preview compares the replacement against every active sale BUT the one it is
// about to reverse, which would otherwise always match itself (#352).
func (r *Repository) ListActiveSaleKeys(ctx context.Context, orgID, eventID, excludeSaleID string) ([]ExistingSaleKey, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT ts.customer_email, tsl.ticket_type_id, ts.sold_at
		FROM ticket_sales ts
		JOIN ticket_sale_lines tsl ON tsl.ticket_sale_id = ts.id
		WHERE ts.event_id = $1 AND ts.organization_id = $2 AND ts.status = 'active'
		  AND ($3 = '' OR ts.id::text <> $3)
	`, eventID, orgID, excludeSaleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExistingSaleKey
	for rows.Next() {
		var k ExistingSaleKey
		if err := rows.Scan(&k.CustomerEmail, &k.TicketTypeID, &k.SoldAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

type lockedType struct {
	priceCents int
	capacity   int
	soldCount  int
	sortOrder  int
	name       string
}

// CommitSales records prepared Ticket Sales, their Lines, and the resulting
// capacity decrement inside the caller's transaction — the sale-commit spine
// every Sales Channel shares. Ticket Type rows are locked FOR UPDATE before the
// capacity check, so concurrent sales cannot oversell. All-or-nothing: any
// oversell fails the whole call with a *CapacityError.
func (r *Repository) CommitSales(ctx context.Context, tx *sql.Tx, in CommitSalesInput) ([]RecordedSale, error) {
	if in.UpsertCustomer == nil {
		return nil, errors.New("sales: UpsertCustomer is required — every Ticket Sale must reference a Customer")
	}
	// Native channels never record a sale without its Tax ID (ADR 0016). The
	// entry points validate this against the user; the spine asserts it so a
	// future channel cannot skip the rule by never having heard of it.
	for _, s := range in.Sales {
		if err := sales.RequireTaxID(in.Channel, s.Customer.TaxID); err != nil {
			return nil, err
		}
	}

	// Aggregate requested quantities per Ticket Type, remembering the first sale
	// that references each type for error reporting. Lock the involved rows in a
	// stable order to avoid deadlocks between concurrent commits.
	requested := map[string]int{}
	firstRow := map[string]int{}
	var typeIDs []string
	for i, s := range in.Sales {
		for _, line := range s.Lines {
			id := line.TicketTypeID
			if _, seen := requested[id]; !seen {
				firstRow[id] = i
				typeIDs = append(typeIDs, id)
			}
			requested[id] += line.Quantity
		}
	}
	sort.Strings(typeIDs)

	locked := make(map[string]lockedType, len(typeIDs))
	for _, id := range typeIDs {
		var lt lockedType
		err := tx.QueryRowContext(ctx, `
			SELECT price_cents, capacity, sold_count, sort_order, name
			FROM ticket_types
			WHERE id = $1 AND event_id = $2 AND organization_id = $3
			FOR UPDATE
		`, id, in.EventID, in.OrganizationID).Scan(&lt.priceCents, &lt.capacity, &lt.soldCount, &lt.sortOrder, &lt.name)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &UnknownTicketTypeError{Row: firstRow[id], TicketTypeID: id}
		}
		if err != nil {
			return nil, err
		}
		locked[id] = lt
	}

	// Capacity check under lock: no Ticket Type may be pushed past capacity,
	// counting both sold_count and the quantities live Capacity Holds claim
	// (ADR 0013) — minus this commit's own Payment, whose hold is converting
	// into sold_count right here. Reading the holds AFTER the ticket_types row
	// locks are held means any competing online commit has either finished
	// (its sold_count increment is visible, its Payment no longer pending) or
	// has not started converting (its hold is still visible): never both,
	// never neither.
	held, err := r.liveHoldsForUpdate(ctx, tx, in.EventID, sales.HoldCutoff(in.Now), in.ExcludePaymentID)
	if err != nil {
		return nil, err
	}
	for _, id := range typeIDs {
		lt := locked[id]
		if lt.soldCount+held[id]+requested[id] > lt.capacity {
			return nil, &CapacityError{
				Row:          firstRow[id],
				TicketTypeID: id,
				Requested:    requested[id],
				Available:    lt.capacity - lt.soldCount - held[id],
			}
		}
	}

	recorded := make([]RecordedSale, 0, len(in.Sales))
	for _, s := range in.Sales {
		// The Customer is created or reused in this same transaction, so a sale and
		// the Customer it references are never recorded apart. The sale keeps its own
		// copy of the recorded name and email verbatim; the upsert never rewrites it.
		//
		// The buyer goes across whole, and the parts of them the sale does not
		// record — the phone, and the flag saying they proved the email is theirs
		// — go through here and stop: the INSERT below has no column for either,
		// deliberately (see CommitSale.Customer).
		customerID, err := in.UpsertCustomer(ctx, tx, s.Customer, in.Now)
		if err != nil {
			return nil, err
		}

		var saleID string
		err = tx.QueryRowContext(ctx, `
			INSERT INTO ticket_sales (
				event_id, organization_id, channel, source, payment_method,
				customer_id, customer_email, customer_first_name, customer_last_name,
				customer_tax_id_type, customer_tax_id_number,
				sold_at, confirmation_ref, status,
				affiliate_link_id, locale, created_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $13, $14, $10, $11, 'active', $15, $16, $12)
			RETURNING id
		`, in.EventID, in.OrganizationID, in.Channel, nullString(in.Source), nullString(s.PaymentMethod),
			customerID, s.Customer.Email, s.Customer.FirstName, s.Customer.LastName, s.SoldAt, s.ConfirmationRef, in.Now,
			nullString(s.Customer.TaxID.Type), nullString(s.Customer.TaxID.Number),
			nullString(s.AffiliateLinkID), nullString(s.Locale)).Scan(&saleID)
		if err != nil {
			return nil, err
		}

		amountCents := 0
		// The buyer's own Ticket, chosen as the lines are written: the first
		// Ticket of the line whose Ticket Type comes first in catalog order
		// (sort_order, then name — the order every storefront list shows).
		var selfHeldTicketID string
		var selfHeldType lockedType
		for _, line := range s.Lines {
			unitPrice := locked[line.TicketTypeID].priceCents
			if line.UnitPriceCents != nil {
				unitPrice = *line.UnitPriceCents
			}
			// A line with no fee snapshot was sold on a channel the platform took
			// no cut of: its base price is simply what it sold for.
			fee := sales.FeeSnapshot{BasePriceCents: unitPrice}
			if line.Fee != nil {
				fee = *line.Fee
			}
			amountCents += line.Quantity * unitPrice
			var lineID string
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO ticket_sale_lines (
					ticket_sale_id, ticket_type_id, quantity, unit_price_cents,
					base_price_cents, fee_cents, fee_iva_cents, fee_basis_points, fee_iva_basis_points,
					created_at
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				RETURNING id
			`, saleID, line.TicketTypeID, line.Quantity, unitPrice,
				fee.BasePriceCents, fee.FeeCents, fee.FeeIVACents,
				fee.FeeBasisPoints, fee.FeeIVABasisPoints, in.Now).Scan(&lineID); err != nil {
				return nil, err
			}
			// The line's Tickets, minted in the same breath as the line itself.
			// Nothing between these two statements may fail without both rolling
			// back together: that is the whole of how "one Ticket per ticket sold"
			// is kept true (ADR 0043).
			ticketIDs, err := mintTickets(ctx, tx, lineID, line.Quantity, in.Now)
			if err != nil {
				return nil, err
			}
			if lt := locked[line.TicketTypeID]; in.SelfHeld && (selfHeldTicketID == "" ||
				lt.sortOrder < selfHeldType.sortOrder ||
				(lt.sortOrder == selfHeldType.sortOrder && lt.name < selfHeldType.name)) {
				selfHeldTicketID = ticketIDs[1]
				selfHeldType = lt
			}
			// And the Answers the buyer gave at checkout, landing on those very
			// Tickets in the same transaction (#311). The Payment held them keyed
			// by (payment line, index) across the provider redirect; here index n
			// becomes ordinal n, which is what `tickets.ordinal` exists for.
			//
			// Inside the transaction and not after it, for mintTickets' reason
			// exactly: a Payment that fails or expires must produce no Tickets and
			// no Answers on any Ticket, and the only thing making that true is
			// that all three writes roll back together.
			//
			// THE IMPLICATION RUNS ONE WAY ONLY, and the SAVEPOINT is what keeps it
			// one-way. A failed sale must take its Answers down with it; a failed
			// Answer must NOT take the sale down with it. ADR 0044 says nothing
			// about an Answer may refuse or delay a checkout, and the worst
			// reachable reading of the alternative is the worst outcome this
			// system has: an approved Payment with no Ticket Sale — money taken,
			// nothing sold, because a t-shirt size would not insert.
			//
			// Rolling back to the savepoint costs the Answers and keeps the sale.
			// That is the right way round to fail: the buyer can give them again
			// from their sale's page, and the holder can give them through an
			// Answer Link, whereas nobody can un-take a payment. The Tickets are
			// left carrying Outstanding Answers, which is a state the whole
			// feature is already built to chase.
			if _, err := tx.ExecContext(ctx, `SAVEPOINT ticket_answers`); err != nil {
				return nil, err
			}
			if err := writeTicketAnswers(ctx, tx, ticketIDs, line.Answers, in.Now); err != nil {
				if _, rbErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT ticket_answers`); rbErr != nil {
					return nil, rbErr
				}
			} else if _, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT ticket_answers`); err != nil {
				return nil, err
			}
		}

		if selfHeldTicketID != "" {
			if err := holdOwnTicket(ctx, tx, selfHeldTicketID, customerID, s.Customer.Email, in.Now); err != nil {
				return nil, err
			}
		}

		recorded = append(recorded, RecordedSale{
			ID:                saleID,
			CustomerID:        customerID,
			ConfirmationRef:   s.ConfirmationRef,
			CustomerEmail:     s.Customer.Email,
			CustomerFirstName: s.Customer.FirstName,
			CustomerLastName:  s.Customer.LastName,
			AmountCents:       amountCents,
			CustomerTaxID:     s.Customer.TaxID,
			Locale:            s.Locale,
		})
	}

	for _, id := range typeIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE ticket_types SET sold_count = sold_count + $1, updated_at = $2
			WHERE id = $3
		`, requested[id], in.Now, id); err != nil {
			return nil, err
		}
	}

	return recorded, nil
}

// mintTickets writes one Ticket per unit of a Ticket Sale Line's quantity, in
// the caller's transaction — the act that turns a quantity into the units of
// admission it counts (#308, ADR 0043).
//
// IT TAKES A TRANSACTION AND NO POOL, deliberately, and it is unexported for
// the same reason. A Ticket exists only because a Ticket Sale Line does, so
// there is no honest caller outside the transaction that wrote the line: a
// Ticket minted afterwards is a Ticket that a crash in between could have left
// unminted, and the invariant this table ships with — one Ticket per ticket
// sold — is held by exactly that atomicity and by nothing else. Anything that
// ever writes a Ticket Sale Line outside the commit spine must call this beside
// it, in the same transaction.
//
// The insert mirrors migration 071's backfill statement, generate_series and
// all, so that a Ticket minted at sale time and a Ticket backfilled onto an old
// sale are the same row written by the same shape.
//
// IT RETURNS THE TICKETS KEYED BY ORDINAL, and the key is the ordinal rather
// than a slice position deliberately. The Answers held on a Payment are keyed by
// an index that MEANS the ordinal (#311, migration 074), so handing the caller a
// map straight from the column removes the one place an off-by-one could live —
// and `RETURNING` makes no promise about row order, so a slice would have had to
// be sorted back into the order the map already has.
func mintTickets(ctx context.Context, tx *sql.Tx, ticketSaleLineID string, quantity int, now time.Time) (map[int]string, error) {
	rows, err := tx.QueryContext(ctx, `
		INSERT INTO tickets (ticket_sale_line_id, ordinal, created_at)
		SELECT $1, ordinals.n, $3
		FROM generate_series(1, $2) AS ordinals (n)
		RETURNING ordinal, id
	`, ticketSaleLineID, quantity, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ticketIDs := make(map[int]string, quantity)
	for rows.Next() {
		var ordinal int
		var id string
		if err := rows.Scan(&ordinal, &id); err != nil {
			return nil, err
		}
		ticketIDs[ordinal] = id
	}
	return ticketIDs, rows.Err()
}

// holdOwnTicket makes one freshly minted Ticket the buyer's own Self-held
// Ticket: assigned to the buyer's address and accepted at once, in the caller's
// transaction (ADR 0048).
//
// ACCEPTED BY PURCHASE OR BY TRANSCRIPTION, NEVER BY LINK. Checking out is the
// buyer's own act, and an imported Sale transcribes a transaction the buyer
// already made somewhere else (ADR 0055), so on neither route does "who is this
// one for" need asking and on neither is an Assignment mail written. It writes
// holder_customer_id and accepted_at together, as migration 080's CHECK
// requires, and touches nothing on the customers row — neither a payment nor a
// transcription is Proof of Email Ownership, and whether the buyer is Verified
// stays the sign-in module's authority. The Organization sees an ordinary
// accepted Ticket under the name and address the Sale was made with, which it
// already sees on the Sale.
//
// THE WARRANT DIFFERS BY CHANNEL AND THE WRITE DOES NOT. A payment is a proof
// and a transcription is a presumption, which ADR 0055 states plainly rather
// than dressing up; what it refused was a fourth assignment state saying "we
// think so", because the roster's question is who is coming and not how they
// came to hold the Ticket. So there is one row shape here and one only.
//
// Like mintTickets it takes a transaction and not a pool: a Ticket that is
// the buyer's own from the start must be so in the commit that minted it, or
// a crash in between leaves a Sale whose buyer holds nothing.
func holdOwnTicket(ctx context.Context, tx *sql.Tx, ticketID, customerID, email string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE tickets
		SET holder_email = $2, holder_customer_id = $3, assigned_at = $4, accepted_at = $4
		WHERE id = $1
	`, ticketID, platform.NormalizeEmail(email), customerID, now)
	return err
}

// liveHoldsForUpdate returns the quantities live Capacity Holds claim per
// Ticket Type on the Event, read inside the commit transaction, optionally
// excluding one Payment (the one whose own commit is running).
func (r *Repository) liveHoldsForUpdate(ctx context.Context, tx *sql.Tx, eventID string, cutoff time.Time, excludePaymentID string) (map[string]int, error) {
	var rows *sql.Rows
	var err error
	if excludePaymentID == "" {
		rows, err = tx.QueryContext(ctx, sales.LiveHoldsSQL(sales.HoldsFilter{CutoffExpr: "$2", EventExpr: "$1"}), eventID, cutoff)
	} else {
		rows, err = tx.QueryContext(ctx, sales.LiveHoldsSQL(sales.HoldsFilter{CutoffExpr: "$2", EventExpr: "$1", ExcludePaymentExpr: "$3"}), eventID, cutoff, excludePaymentID)
	}
	if err != nil {
		return nil, err
	}
	return sales.ScanHeldQuantities(rows)
}

// CommitImport records a Sale Import batch in a single transaction: the sales
// go through the shared CommitSales spine on the `import` channel, then the
// batch row is written and the recorded sales are linked to it. The batch is
// all-or-nothing: any oversell rolls back everything and returns a
// *CapacityError.
//
// Idempotency: a batch already recorded for (organization, idempotency key) is
// returned unchanged with Replayed set, and nothing new is written.
func (r *Repository) CommitImport(ctx context.Context, in CommitInput) (*CommittedBatch, error) {
	if existing, err := r.findBatch(ctx, in.OrganizationID, in.IdempotencyKey); err != nil {
		return nil, err
	} else if existing != nil {
		existing.Replayed = true
		return existing, nil
	}

	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	recorded, err := r.CommitSales(ctx, tx, CommitSalesInput{
		EventID:        in.EventID,
		OrganizationID: in.OrganizationID,
		Channel:        "import",
		Source:         in.Source,
		Sales:          in.Sales,
		Now:            in.Now,
		UpsertCustomer: in.UpsertCustomer,
		SelfHeld:       in.SelfHeld,
	})
	if err != nil {
		return nil, err
	}

	var batchID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO sale_import_batches (
			event_id, organization_id, source, created_by_member_id,
			idempotency_key, sale_count, status, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'committed', $7)
		RETURNING id
	`, in.EventID, in.OrganizationID, in.Source, nullString(in.CreatedByMemberID),
		in.IdempotencyKey, len(in.Sales), in.Now).Scan(&batchID)
	if err != nil {
		if isUniqueViolation(err) {
			// A concurrent request committed the same idempotency key first.
			_ = tx.Rollback()
			if existing, e := r.findBatch(ctx, in.OrganizationID, in.IdempotencyKey); e == nil && existing != nil {
				existing.Replayed = true
				return existing, nil
			}
		}
		return nil, err
	}

	// Link the batch's sales to it so the import stays reversible (latest-only
	// undo walks ticket_sales by import_batch_id).
	if len(recorded) > 0 {
		saleIDs := make([]string, len(recorded))
		for i, rs := range recorded {
			saleIDs[i] = rs.ID
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE ticket_sales SET import_batch_id = $1 WHERE id = ANY($2)
		`, batchID, saleIDs); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &CommittedBatch{ID: batchID, SaleCount: len(in.Sales), Status: "committed", Recorded: recorded}, nil
}

// ReverseInput identifies the Sale Import batch to reverse.
type ReverseInput struct {
	EventID        string
	OrganizationID string
	BatchID        string
	Now            time.Time
}

// ReversedSale is one Ticket Sale that was reversed, carrying the fields needed
// to email its Customer a void/cancellation notice.
type ReversedSale struct {
	// ID is the voided sale's own id. It identifies the sale in the log line the
	// notice's language resolution writes when it cannot read the recipient's
	// remembered one — a confirmation ref would name the purchase to a human but
	// not the row to whoever goes looking.
	ID                string
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
	ConfirmationRef   string
	// Locale is the Sale Locale recorded on the sale being voided, read back here
	// so the void notice can be written in the language the sale was made in
	// (#246, ADR 0033).
	//
	// It is selected by the reversal primitive rather than looked up by each
	// caller because every void notice this platform sends comes out of this one
	// query — the operator's reversal, the buyer's own, and the Sale Import undo
	// — and a locale fetched per caller would be three chances to forget it.
	//
	// Empty on every sale no page produced, and on every sale older than the
	// column, which is what sends the resolution on to the recipient's remembered
	// language and then to English.
	Locale string
}

// ReversedBatch is the outcome of a reversed Sale Import batch.
type ReversedBatch struct {
	ID    string
	Sales []ReversedSale
}

// BatchNotFoundError reports that the target Sale Import batch does not exist on
// the Event.
type BatchNotFoundError struct{ BatchID string }

func (e *BatchNotFoundError) Error() string { return "sale import batch not found: " + e.BatchID }

// BatchNotLatestError reports that the target batch is not the most recent one
// on the Event, so it cannot be undone (latest-only).
type BatchNotLatestError struct{ BatchID string }

func (e *BatchNotLatestError) Error() string {
	return "sale import batch is not the latest: " + e.BatchID
}

// BatchAlreadyReversedError reports that the target batch has already been reversed.
type BatchAlreadyReversedError struct{ BatchID string }

func (e *BatchAlreadyReversedError) Error() string {
	return "sale import batch already reversed: " + e.BatchID
}

// ReverseSalesInput is a set of Ticket Sales to void together. SaleIDs is the
// candidate set: sales already reversed are skipped, so the operation is
// idempotent and safe to retry. Actor is which side caused the Sale Reversal —
// one of the sales.ReversalActor* values — and Now the moment it happened; both
// are stamped on every sale actually reversed.
type ReverseSalesInput struct {
	EventID        string
	OrganizationID string
	SaleIDs        []string
	Actor          string
	Now            time.Time
	// Operator is the money memo an Operator Reversal carries, and is nil on
	// every other route. It rides the same input rather than a second write
	// because it must land in the transaction that flips the status: a sale
	// recorded as reversed-by-an-operator without the assertion that justified it
	// would be a money claim with nobody's name on it.
	Operator *OperatorReversalMemo
}

// OperatorReversalMemo is what a Platform Operator asserted when they recorded
// an out-of-band refund (#125): who they are, what the buyer actually got back,
// whether the platform kept its Platform Fee and Fee IVA, and any note.
//
// The money pair is a pair of pointers rather than plain values because absent
// and zero are different answers. A free Online Sale refunds nothing and keeps
// no fee, and says so with nulls; zero cents refunded on a paid sale is not a
// thing this system records.
type OperatorReversalMemo struct {
	// Operator is the acting operator's email, taken from their Staff Session.
	Operator            string
	Note                *string
	RefundedAmountCents *int
	PlatformFeeKept     *bool
}

// ReverseSales voids an arbitrary set of Ticket Sales in one transaction. It is
// the single definition of what reversing a sale does, shared by every reversal
// path (Sale Import undo today, Customer-initiated Sale Reversal next) so
// capacity restoration is never reimplemented.
//
// It returns the sales actually reversed, so the caller can send void notices
// after the transaction commits.
func (r *Repository) ReverseSales(ctx context.Context, in ReverseSalesInput) ([]ReversedSale, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	reversed, err := reverseSalesTx(ctx, tx, in)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return reversed, nil
}

// reverseSalesTx is the reversal primitive itself, run inside a caller's
// transaction so a reversal can be composed with the rest of a larger unit of
// work (the Sale Import undo also flips its batch row, in the same transaction
// or not at all).
//
// It locks the candidate sales' rows FOR UPDATE in id order so a concurrent
// reversal of the same sale serializes and finds nothing left to do, restores
// each affected Ticket Type's sold_count by the reversed quantities (locking
// ticket_types FOR UPDATE in a stable order, symmetric to CommitSales), and
// marks each sale 'reversed' with the reversal time and actor.
func reverseSalesTx(ctx context.Context, tx *sql.Tx, in ReverseSalesInput) ([]ReversedSale, error) {
	if len(in.SaleIDs) == 0 {
		return nil, nil
	}

	// Lock and read the sales that are still active. Everything below works off
	// this set, so a sale reversed by a racing transaction is simply not in it.
	// The ordered lock keeps concurrent reversals of overlapping sets deadlock-free.
	// The locale rides along with the buyer's snapshot because it is one: it is
	// the language the sale was made in, and the void notice below is written in
	// it (#246, ADR 0033). NULL on every sale no page produced, which the scan
	// turns into the empty string the resolution chain treats as "nothing here".
	saleRows, err := tx.QueryContext(ctx, `
		SELECT id, customer_email, customer_first_name, customer_last_name, confirmation_ref, locale
		FROM ticket_sales
		WHERE id = ANY($1) AND event_id = $2 AND organization_id = $3 AND status = 'active'
		ORDER BY id
		FOR UPDATE
	`, in.SaleIDs, in.EventID, in.OrganizationID)
	if err != nil {
		return nil, err
	}
	var reversed []ReversedSale
	var activeIDs []string
	for saleRows.Next() {
		var s ReversedSale
		var locale sql.NullString
		if err := saleRows.Scan(&s.ID, &s.CustomerEmail, &s.CustomerFirstName, &s.CustomerLastName, &s.ConfirmationRef, &locale); err != nil {
			saleRows.Close()
			return nil, err
		}
		s.Locale = locale.String
		activeIDs = append(activeIDs, s.ID)
		reversed = append(reversed, s)
	}
	if err := saleRows.Err(); err != nil {
		saleRows.Close()
		return nil, err
	}
	saleRows.Close()
	if len(activeIDs) == 0 {
		return nil, nil
	}

	// Aggregate the quantities to restore per Ticket Type from those sales' lines.
	quantityRows, err := tx.QueryContext(ctx, `
		SELECT tsl.ticket_type_id, SUM(tsl.quantity)
		FROM ticket_sale_lines tsl
		WHERE tsl.ticket_sale_id = ANY($1)
		GROUP BY tsl.ticket_type_id
	`, activeIDs)
	if err != nil {
		return nil, err
	}
	restore := map[string]int{}
	var typeIDs []string
	for quantityRows.Next() {
		var id string
		var qty int
		if err := quantityRows.Scan(&id, &qty); err != nil {
			quantityRows.Close()
			return nil, err
		}
		restore[id] = qty
		typeIDs = append(typeIDs, id)
	}
	if err := quantityRows.Err(); err != nil {
		quantityRows.Close()
		return nil, err
	}
	quantityRows.Close()
	sort.Strings(typeIDs)

	// Lock the affected Ticket Types in a stable order (symmetric to CommitSales)
	// then restore capacity.
	for _, id := range typeIDs {
		var dummy int
		if err := tx.QueryRowContext(ctx, `
			SELECT sold_count FROM ticket_types
			WHERE id = $1 AND event_id = $2 AND organization_id = $3
			FOR UPDATE
		`, id, in.EventID, in.OrganizationID).Scan(&dummy); err != nil {
			return nil, err
		}
	}
	for _, id := range typeIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE ticket_types SET sold_count = sold_count - $1, updated_at = $2
			WHERE id = $3
		`, restore[id], in.Now, id); err != nil {
			return nil, err
		}
	}

	// The status flip carries its own provenance: a reversed sale says when it
	// went and which side asked (#117, ADR 0018), and on an Operator Reversal it
	// also carries the memo of what the operator asserted (#125). The memo is
	// written by the same statement, so a sale can never be found reversed by an
	// operator with the assertion missing.
	var operator, note *string
	var refundedAmountCents *int
	var platformFeeKept *bool
	if in.Operator != nil {
		operator = &in.Operator.Operator
		note = in.Operator.Note
		refundedAmountCents = in.Operator.RefundedAmountCents
		platformFeeKept = in.Operator.PlatformFeeKept
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_sales
		SET status = 'reversed',
		    reversed_at = $2,
		    reversed_by = $3,
		    reversed_by_operator = $4,
		    reversal_note = $5,
		    refunded_amount_cents = $6,
		    platform_fee_kept = $7
		WHERE id = ANY($1)
	`, activeIDs, in.Now, in.Actor,
		operator, note, refundedAmountCents, platformFeeKept,
	); err != nil {
		return nil, err
	}

	return reversed, nil
}

// ReverseBatch reverses a committed Sale Import batch in a single transaction:
// it reverses the batch's active Ticket Sales through the shared reversal
// primitive — voiding them and restoring each affected Ticket Type's sold_count,
// stamped with the staff actor — and marks the batch 'reversed'.
//
// Only the most recent batch on the Event is reversible: if the target is not
// the newest batch it returns *BatchNotLatestError; an already-reversed batch
// returns *BatchAlreadyReversedError; a missing batch returns *BatchNotFoundError.
// It returns the reversed sales so the caller can send void notices after commit.
func (r *Repository) ReverseBatch(ctx context.Context, in ReverseInput) (*ReversedBatch, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Lock the target batch row so a concurrent undo of the same batch serializes.
	var status string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM sale_import_batches
		WHERE id = $1 AND event_id = $2 AND organization_id = $3
		FOR UPDATE
	`, in.BatchID, in.EventID, in.OrganizationID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &BatchNotFoundError{BatchID: in.BatchID}
	}
	if err != nil {
		return nil, err
	}

	// Latest-only: reject unless the target is the newest batch on the Event.
	// Order by the monotonic seq so "latest" is deterministic even when two
	// batches share a created_at (see migration 012).
	var latestID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM sale_import_batches
		WHERE event_id = $1 AND organization_id = $2
		ORDER BY seq DESC
		LIMIT 1
	`, in.EventID, in.OrganizationID).Scan(&latestID)
	if err != nil {
		return nil, err
	}
	if latestID != in.BatchID {
		return nil, &BatchNotLatestError{BatchID: in.BatchID}
	}

	if status == "reversed" {
		return nil, &BatchAlreadyReversedError{BatchID: in.BatchID}
	}

	// The batch's sales are the set to reverse; the primitive does the voiding
	// and the capacity restoration, inside this same transaction.
	idRows, err := tx.QueryContext(ctx, `
		SELECT id FROM ticket_sales WHERE import_batch_id = $1 AND status = 'active'
	`, in.BatchID)
	if err != nil {
		return nil, err
	}
	var saleIDs []string
	for idRows.Next() {
		var id string
		if err := idRows.Scan(&id); err != nil {
			idRows.Close()
			return nil, err
		}
		saleIDs = append(saleIDs, id)
	}
	if err := idRows.Err(); err != nil {
		idRows.Close()
		return nil, err
	}
	idRows.Close()

	reversed, err := reverseSalesTx(ctx, tx, ReverseSalesInput{
		EventID:        in.EventID,
		OrganizationID: in.OrganizationID,
		SaleIDs:        saleIDs,
		Actor:          sales.ReversalActorStaff,
		Now:            in.Now,
	})
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE sale_import_batches SET status = 'reversed', undone_at = $2
		WHERE id = $1
	`, in.BatchID, in.Now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &ReversedBatch{ID: in.BatchID, Sales: reversed}, nil
}

// ImportBatchSummary is one committed Sale Import batch for the per-event
// history: when it ran, how many sales it recorded, its source and status, and
// the acting Member's identity (email; nil when the Member was since removed).
type ImportBatchSummary struct {
	ID          string
	CreatedAt   time.Time
	SaleCount   int
	Source      string
	Status      string
	ActorMember *string
	ActorEmail  *string
}

// ListImportBatches returns the Event's Sale Import batches, newest first, with
// the acting Member's email joined in (nil when the Member was removed).
func (r *Repository) ListImportBatches(ctx context.Context, orgID, eventID string) ([]ImportBatchSummary, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT b.id, b.created_at, b.sale_count, b.source, b.status,
		       b.created_by_member_id, m.email
		FROM sale_import_batches b
		LEFT JOIN members m ON m.id = b.created_by_member_id
		WHERE b.event_id = $1 AND b.organization_id = $2
		ORDER BY b.seq DESC
	`, eventID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ImportBatchSummary
	for rows.Next() {
		var b ImportBatchSummary
		var memberID, email sql.NullString
		if err := rows.Scan(&b.ID, &b.CreatedAt, &b.SaleCount, &b.Source, &b.Status, &memberID, &email); err != nil {
			return nil, err
		}
		if memberID.Valid {
			b.ActorMember = &memberID.String
		}
		if email.Valid {
			b.ActorEmail = &email.String
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SaleLineRollup is one Ticket Type and its quantity within a Ticket Sale, as
// rolled up for the Sales list (one entry per Ticket Sale Line).
//
// TicketTypeID rides alongside the name because the Sales Export must place a
// quantity under the right column of the Event's catalog, and the name cannot do
// that: an organizer may give two Ticket Types the same name, and nothing stops
// one being called "amount". The id is the identity; the name is what a reader
// sees. Both are read live — no name is snapshotted onto a sale line, so a rename
// moves every surface at once.
type SaleLineRollup struct {
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
	Quantity       int    `json:"quantity"`
}

// SaleRow is one Ticket Sale row for the Sales list: the Customer, the rolled-up
// Ticket Types and total amount (in the Event/Organization currency), and the
// channel/source/status/reference plus the recorded-at and payment method the
// row-detail expand reveals.
type SaleRow struct {
	ID                string
	CustomerFirstName string
	CustomerLastName  string
	CustomerEmail     string
	TicketTypes       []SaleLineRollup
	AmountCents       int
	// NetProceedsCents is what this sale's lines left the Organization once the
	// Platform Fee and its Fee IVA were withheld, summed off the snapshots the
	// lines froze at sale time — the same expression the Event's sales summary
	// sums, grouped per sale rather than per Event (lineNetProceedsSQL, ADR 0014).
	//
	// It is the raw arithmetic and nothing more: it says nothing about whether
	// the figure APPLIES to this sale. A sale on any channel but `online` carries
	// fee snapshots of zero and so reads back as its full price, which would be a
	// lie about money the platform never held; a reversed sale reads back as
	// money that was given away again. Deciding which sales have a Net Proceeds
	// figure at all is the caller's, and the Sales Export leaves both blank.
	NetProceedsCents int
	Currency         string
	SoldAt           time.Time
	Channel          string
	Source           *string
	Status           string
	ConfirmationRef  string
	RecordedAt       time.Time
	PaymentMethod    *string
	// CustomerTaxIDType/Number are the Tax ID snapshot the sale was transacted
	// under, nil together on sales recorded without one (legacy rows and
	// imports that never collected it — ADR 0016).
	CustomerTaxIDType   *string
	CustomerTaxIDNumber *string
	// ReversedAt/ReversedBy are the Sale Reversal's provenance: when the sale was
	// voided and which side caused it. Null together on an active sale, and on a
	// sale reversed before either was recorded (#117).
	ReversedAt *time.Time
	ReversedBy *string
	// ReplacedBySaleID/ReplacesSaleID are the Sale Correction linkage (#350,
	// ADR 0050): on a reversed sale, the replacement that corrected it; on the
	// replacement, the sale it stands in for. Nil on every sale until a
	// correction writes them; a plain single-sale reversal sets neither.
	ReplacedBySaleID *string
	ReplacesSaleID   *string
	// ReplacedByConfirmationRef/ReplacesConfirmationRef are the linked sales'
	// Sale Confirmation references (#351), read alongside the ids so a row can
	// say "Corrected → TP-X" without a second lookup. Nil exactly when the
	// matching id is.
	ReplacedByConfirmationRef *string
	ReplacesConfirmationRef   *string
	// ImportBatchID is the Sale Import batch the sale arrived in, nil on every
	// sale that never came out of an uploaded file: an Online Sale, a Sale
	// Correction's replacement, and a Manually Recorded Sale. It is read for
	// one purpose — sales.DeriveSaleOrigin, which needs the batch's ABSENCE to
	// tell the last two apart from the first (#370, ADR 0052) — and is not on
	// the wire: the Sales list states the derived origin, not the batch id.
	ImportBatchID *string
	// ReversedByBatchUndo is true on a reversed sale that went with its Sale
	// Import batch's undo — its reversed_at is the batch's undone_at (migration
	// 086) — and false on one reversed singly or corrected, even if its batch
	// was undone afterwards. The Sales Export reads it to name the route (#352).
	ReversedByBatchUndo bool
	// HeldTicketCount is how many of the sale's Tickets have an accepted
	// Holder — the people a Sale Reversal would tell (#327). Read off
	// accepted_at, the one fact that makes somebody a Holder.
	HeldTicketCount int
}

// ListSalesQuery selects a page of an Event's Ticket Sales for the Sales list.
// Every filter beyond OrganizationID/EventID/Status is optional: a zero value
// (empty string or nil bound) leaves that dimension unfiltered.
type ListSalesQuery struct {
	OrganizationID string
	EventID        string
	// Status narrows to Ticket Sales in this lifecycle state (e.g. "active").
	Status string
	// TicketTypeID, when set, keeps only sales that INCLUDE this Ticket Type —
	// matched via an EXISTS sub-query so a multi-line sale is never fanned out or
	// counted twice, and its full Ticket Type rollup is preserved.
	TicketTypeID string
	// SoldFrom/SoldTo bound sold_at as a half-open interval [SoldFrom, SoldTo):
	// SoldFrom is the inclusive lower bound and SoldTo the exclusive upper bound,
	// both already resolved to absolute time by the caller (the Event timezone is
	// applied in the service). Either may be nil.
	SoldFrom *time.Time
	SoldTo   *time.Time
	// Search is a case-insensitive substring matched over customer email, the
	// joined customer name, confirmation_ref, and the sale's Tax ID number
	// snapshot (empty means no search).
	Search string
	// Channel, Source, PaymentMethod are single-valued equality filters (empty
	// means unfiltered).
	Channel       string
	Source        string
	PaymentMethod string
	// Sort and Dir are the validated sort column and direction (the service
	// guarantees they are allowlisted; see salesSortColumns). Sort selects the
	// primary ORDER BY expression; Dir is "asc" or "desc".
	Sort string
	Dir  string
	// Limit and Offset paginate the result. Limit must be positive and ListSales
	// refuses anything else: the Sales list passes its page size, the Sales
	// Export the row cap plus one — one past the cap being how it learns it has
	// been exceeded without reading an Event's whole history to find out.
	Limit  int
	Offset int
}

// likeEscape escapes the LIKE/ILIKE metacharacters (\, %, _) in a search term so
// it is matched as a literal substring rather than a pattern. The backslash is
// Postgres's default ILIKE escape character.
func likeEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// salesSortColumns maps an allowlisted sort key to the ordered list of primary
// ORDER BY columns for that sort. Because these values come from a fixed
// allowlist enforced upstream, they are safe to interpolate into the query.
// `customer` orders by last then first name; `amount` orders by the per-sale
// summed line total computed in the lateral subquery. Every sort carries a
// trailing `ts.id` tiebreaker (added when the clause is built) so equal primary
// values keep a stable order across pages.
var salesSortColumns = map[string][]string{
	"sold_at":     {"ts.sold_at"},
	"recorded_at": {"ts.created_at"},
	"customer":    {"ts.customer_last_name", "ts.customer_first_name"},
	"amount":      {"lines.amount_cents"},
}

// salesOrderBy builds the ORDER BY clause for a Sales list query from the
// validated sort key and direction, always appending the `ts.id` tiebreaker in
// the same direction so equal primary values do not reorder between pages
// (ADR-0006). Unknown values fall back to the default sold_at ordering.
func salesOrderBy(sort, dir string) string {
	cols, ok := salesSortColumns[sort]
	if !ok {
		cols = salesSortColumns["sold_at"]
	}
	direction := "DESC"
	if strings.EqualFold(dir, "asc") {
		direction = "ASC"
	}
	// Apply the direction to each primary column, then the id tiebreaker.
	parts := make([]string, 0, len(cols)+1)
	for _, c := range cols {
		parts = append(parts, c+" "+direction)
	}
	parts = append(parts, "ts.id "+direction)
	return "ORDER BY " + strings.Join(parts, ", ")
}

// ListSales returns one page of an Event's Ticket Sales for the Sales list, one
// row per Ticket Sale, ordered by the query's sort/dir (defaulting to sold_at
// DESC) with an id tiebreaker so equal primary values do not reorder between
// pages — see salesOrderBy. The Ticket Sale Lines are aggregated per sale in a
// lateral subquery so a multi-line sale stays a single row (no join fan-out): its
// amount is SUM(quantity × unit_price_cents), its Net Proceeds the same sum over
// lineNetProceedsSQL, and its Ticket Types roll up into one ordered list. total
// is the unpaginated match count via COUNT(*) OVER() (ADR-0006).
//
// Optional filters (ticket type, sold-at range, search, channel/source/payment
// method) are appended to the WHERE clause; the ticket-type filter uses an
// EXISTS sub-query so it narrows sales without touching the rollup or fanning
// the row out.
//
// Limit is always applied and must be positive. total carries COUNT(*) OVER(),
// which rides on the returned rows — so it is the true unpaginated match count
// whenever the page holds anything, and zero when the page is empty. The Sales
// Export reads one row past its cap from offset zero, so a page it cares about
// is never empty and its refusal count is exact.
func (r *Repository) ListSales(ctx context.Context, q ListSalesQuery) ([]SaleRow, int, error) {
	// A non-positive Limit would mean LIMIT 0: Postgres returns no rows, total
	// stays zero for want of a row to carry it, and the caller gets a confident
	// empty answer instead of an error. Refuse it rather than serve it.
	if q.Limit <= 0 {
		return nil, 0, fmt.Errorf("sales: ListSales requires a positive Limit, got %d", q.Limit)
	}
	// $1..$3 are the always-present event/org/status scope; further filters
	// append their own placeholders so the query only mentions active filters.
	args := []any{q.EventID, q.OrganizationID, q.Status}
	var conds []string
	addCond := func(format string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(format, len(args)))
	}

	if q.TicketTypeID != "" {
		addCond(`EXISTS (
			SELECT 1 FROM ticket_sale_lines f
			WHERE f.ticket_sale_id = ts.id AND f.ticket_type_id = $%d
		)`, q.TicketTypeID)
	}
	if q.SoldFrom != nil {
		addCond(`ts.sold_at >= $%d`, *q.SoldFrom)
	}
	if q.SoldTo != nil {
		addCond(`ts.sold_at < $%d`, *q.SoldTo)
	}
	if q.Search != "" {
		// Case-insensitive substring over email, joined name, confirmation ref,
		// and the sale's Tax ID number snapshot, scoped within the already
		// event_id-narrowed set — an organizer looks a buyer up inside the Event
		// they sold, never across the platform (#100). The Tax ID branch matches
		// substrings so door staff can type the last digits read off an ID card;
		// a null snapshot simply never matches. A pg_trgm trigram index
		// (ADR-0006) is the documented upgrade path if a single Event's volume
		// ever makes this scan too slow.
		args = append(args, "%"+likeEscape(q.Search)+"%")
		p := len(args)
		conds = append(conds, fmt.Sprintf(`(
			ts.customer_email ILIKE $%d
			OR (ts.customer_first_name || ' ' || ts.customer_last_name) ILIKE $%d
			OR ts.confirmation_ref ILIKE $%d
			OR ts.customer_tax_id_number ILIKE $%d
		)`, p, p, p, p))
	}
	if q.Channel != "" {
		addCond(`ts.channel = $%d`, q.Channel)
	}
	if q.Source != "" {
		addCond(`ts.source = $%d`, q.Source)
	}
	if q.PaymentMethod != "" {
		addCond(`ts.payment_method = $%d`, q.PaymentMethod)
	}

	filterSQL := ""
	if len(conds) > 0 {
		filterSQL = " AND " + strings.Join(conds, " AND ")
	}
	// Every read is bounded. The Sales list passes its page size and the Sales
	// Export the row cap plus one; there is deliberately no "fetch everything"
	// path, because a caller that reached it by passing a zero would pull an
	// Event's entire sales history into memory without saying so.
	args = append(args, q.Limit)
	limitP := len(args)
	args = append(args, q.Offset)
	offsetP := len(args)
	pageSQL := fmt.Sprintf("LIMIT $%d OFFSET $%d", limitP, offsetP)

	query := fmt.Sprintf(`
		SELECT
			ts.id,
			ts.customer_first_name,
			ts.customer_last_name,
			ts.customer_email,
			lines.amount_cents,
			lines.net_proceeds_cents,
			lines.ticket_types,
			org.currency,
			ts.sold_at,
			ts.channel,
			ts.source,
			ts.status,
			ts.confirmation_ref,
			ts.created_at,
			ts.payment_method,
			ts.customer_tax_id_type,
			ts.customer_tax_id_number,
			ts.reversed_at,
			ts.reversed_by,
			ts.replaced_by_sale_id,
			ts.replaces_sale_id,
			ts.import_batch_id,
			(SELECT confirmation_ref FROM ticket_sales r WHERE r.id = ts.replaced_by_sale_id),
			(SELECT confirmation_ref FROM ticket_sales r WHERE r.id = ts.replaces_sale_id),
			COALESCE((
				SELECT b.undone_at IS NOT NULL AND b.undone_at = ts.reversed_at
				FROM sale_import_batches b WHERE b.id = ts.import_batch_id
			), FALSE) AS reversed_by_batch_undo,
			(
				SELECT COUNT(*) FROM tickets tk
				JOIN ticket_sale_lines tkl ON tkl.id = tk.ticket_sale_line_id
				WHERE tkl.ticket_sale_id = ts.id AND tk.accepted_at IS NOT NULL
			) AS held_ticket_count,
			COUNT(*) OVER() AS total
		FROM ticket_sales ts
		JOIN organizations org ON org.id = ts.organization_id
		JOIN LATERAL (
			SELECT
				COALESCE(SUM(tsl.quantity * tsl.unit_price_cents), 0) AS amount_cents,
				COALESCE(SUM(`+lineNetProceedsSQL+`), 0) AS net_proceeds_cents,
				COALESCE(
					json_agg(
						json_build_object('ticket_type_id', tt.id, 'ticket_type_name', tt.name, 'quantity', tsl.quantity)
						ORDER BY tt.sort_order, tt.name
					),
					'[]'::json
				) AS ticket_types
			FROM ticket_sale_lines tsl
			JOIN ticket_types tt ON tt.id = tsl.ticket_type_id
			WHERE tsl.ticket_sale_id = ts.id
		) lines ON TRUE
		WHERE ts.event_id = $1 AND ts.organization_id = $2 AND ts.status = $3%s
		`+salesOrderBy(q.Sort, q.Dir)+`
		%s
	`, filterSQL, pageSQL)

	rows, err := r.db.Pool.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []SaleRow
	total := 0
	for rows.Next() {
		var s SaleRow
		var typesJSON []byte
		var source, paymentMethod, taxIDType, taxIDNumber, reversedBy sql.NullString
		var replacedBy, replaces, importBatchID, replacedByRef, replacesRef sql.NullString
		var reversedAt sql.NullTime
		if err := rows.Scan(
			&s.ID,
			&s.CustomerFirstName,
			&s.CustomerLastName,
			&s.CustomerEmail,
			&s.AmountCents,
			&s.NetProceedsCents,
			&typesJSON,
			&s.Currency,
			&s.SoldAt,
			&s.Channel,
			&source,
			&s.Status,
			&s.ConfirmationRef,
			&s.RecordedAt,
			&paymentMethod,
			&taxIDType,
			&taxIDNumber,
			&reversedAt,
			&reversedBy,
			&replacedBy,
			&replaces,
			&importBatchID,
			&replacedByRef,
			&replacesRef,
			&s.ReversedByBatchUndo,
			&s.HeldTicketCount,
			&total,
		); err != nil {
			return nil, 0, err
		}
		if replacedBy.Valid {
			s.ReplacedBySaleID = &replacedBy.String
		}
		if replaces.Valid {
			s.ReplacesSaleID = &replaces.String
		}
		if importBatchID.Valid {
			s.ImportBatchID = &importBatchID.String
		}
		if replacedByRef.Valid {
			s.ReplacedByConfirmationRef = &replacedByRef.String
		}
		if replacesRef.Valid {
			s.ReplacesConfirmationRef = &replacesRef.String
		}
		if err := json.Unmarshal(typesJSON, &s.TicketTypes); err != nil {
			return nil, 0, err
		}
		if source.Valid {
			s.Source = &source.String
		}
		if paymentMethod.Valid {
			s.PaymentMethod = &paymentMethod.String
		}
		// The pair is written and read together: a database CHECK keeps both
		// halves null or both set (migration 026).
		if taxIDType.Valid {
			s.CustomerTaxIDType = &taxIDType.String
		}
		if taxIDNumber.Valid {
			s.CustomerTaxIDNumber = &taxIDNumber.String
		}
		// Also a pair kept together by a database CHECK (migration 031); a sale
		// reversed before #117 has neither half, and is reported as it is.
		if reversedAt.Valid {
			s.ReversedAt = &reversedAt.Time
		}
		if reversedBy.Valid {
			s.ReversedBy = &reversedBy.String
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// lineNetProceedsSQL is this package's name for the one per-line money
// definition (sales.LineNetProceedsSQL): quantity × (what the Customer paid −
// the Platform Fee − the Fee IVA withheld), read off the snapshot the line froze
// at sale time. It serves the Event's sales summary and the Organization's
// Withdrawable Balance here; the affiliates module sums the same expression for
// an Affiliate Link's attributed figures, so the arithmetic cannot drift between
// the surfaces that show it (ADR 0014).
//
// It now means two different things, told apart ONLY by whether a channel filter
// accompanies it (ADR 0040). With `ts.channel = 'online'` it is Net Proceeds:
// what the platform will hand over. Without one it is Takings: what the Event
// made on whatever channel it sold, because in-person and imported lines carry
// zero fee snapshots and the subtraction leaves the full price. The filter is
// therefore load-bearing at every call site — a caller that forgets it is not
// computing Net Proceeds, it is computing Takings under the wrong name.
const lineNetProceedsSQL = sales.LineNetProceedsSQL

// SalesSummaryRow is the Sales tab's stat strip, read straight off the Event's
// recorded sales: what the Event has left the Organization, how many active
// Ticket Sales it has made, and how many tickets those sales moved.
type SalesSummaryRow struct {
	NetProceedsCents int
	SalesCount       int
	TicketsSold      int
}

// SalesSummary totals an Event's active Ticket Sales two ways.
//
// Net Proceeds sums quantity × (unit_price_cents − fee_cents − fee_iva_cents)
// over the lines of active ONLINE sales: the money the platform actually held,
// less what it withheld. The subtraction reads the same under either Fee
// Handling because unit_price_cents is always what the Customer paid, so
// nothing here branches on the Event's mode — and because the operands are the
// snapshots the sale froze, a later rate change moves nothing (ADR 0014). The
// channel filter is load-bearing: in-person and imported lines carry fee 0, so
// without it they would contribute their full price as if the platform had held
// that cash.
//
// SalesCount counts every active Ticket Sale, whatever the channel — it is the
// Event's sales, not the subset that earned Net Proceeds.
//
// TicketsSold sums those sales' line quantities, on every channel for the same
// reason: a ticket sold at the door and a ticket imported from elsewhere each
// put a body in the room. It is the count that answers "how many people are
// coming", where SalesCount answers "how many times did somebody check out" —
// one Ticket Sale of four tickets is 1 and 4. The absence of a channel filter
// here is deliberate and load-bearing, exactly as its presence is on the money
// above: adding one would silently turn Tickets Sold into online tickets only
// and understate the room worst at the Events that sell hardest at the door.
//
// Summed off the sale lines rather than read from ticket_types.sold_count,
// which maintains the same quantity for capacity. Both are correct, but the
// strip sits directly above the Sales list drawn from these same rows, and a
// reader can check one against the other by eye — so the figure that must never
// drift is the one against the list.
func (r *Repository) SalesSummary(ctx context.Context, orgID, eventID string) (SalesSummaryRow, error) {
	var out SalesSummaryRow
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(net.net_proceeds_cents) FILTER (WHERE ts.channel = 'online'), 0),
			COUNT(*),
			COALESCE(SUM(net.tickets_sold), 0)
		FROM ticket_sales ts
		JOIN LATERAL (
			SELECT COALESCE(SUM(`+lineNetProceedsSQL+`), 0) AS net_proceeds_cents,
			       COALESCE(SUM(tsl.quantity), 0) AS tickets_sold
			FROM ticket_sale_lines tsl
			WHERE tsl.ticket_sale_id = ts.id
		) net ON TRUE
		WHERE ts.event_id = $1 AND ts.organization_id = $2 AND ts.status = 'active'
	`, eventID, orgID).Scan(&out.NetProceedsCents, &out.SalesCount, &out.TicketsSold)
	if err != nil {
		return SalesSummaryRow{}, err
	}
	return out, nil
}

// ReversedSalesCount counts an Event's reversed Ticket Sales.
//
// It is the counterpart to SalesSummary's active figures rather than a column of
// it, and deliberately so on two counts. SalesSummary is the Event's MONEY —
// Net Proceeds, which Event Staff are refused at the route — while a Sale
// Reversal has to be visible to every Member of the Event, so this figure rides
// the Sales list instead of the stat strip (#122). And SalesSummary's existing
// figures keep their meanings untouched: sales_count still counts active sales
// only, and Net Proceeds still sums active online lines, so no caller reads a
// different number than it did before.
//
// The count ignores the Sales list's filters on purpose. It answers "has
// anything on this Event been reversed", which is the question an organizer
// watching a total drop actually has; a figure that moved as they narrowed the
// view could not answer it.
func (r *Repository) ReversedSalesCount(ctx context.Context, orgID, eventID string) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM ticket_sales ts
		WHERE ts.event_id = $1 AND ts.organization_id = $2 AND ts.status = 'reversed'
	`, eventID, orgID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *Repository) findBatch(ctx context.Context, orgID, idempotencyKey string) (*CommittedBatch, error) {
	var b CommittedBatch
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, sale_count, status
		FROM sale_import_batches
		WHERE organization_id = $1 AND idempotency_key = $2
	`, orgID, idempotencyKey).Scan(&b.ID, &b.SaleCount, &b.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullBool passes a *bool to the driver with nil intact, for the columns where
// SQL NULL is a third answer rather than a missing one — the consent boxes a
// surface did not show (migration 064). Dereferencing into a plain bool here
// would silently turn "not shown" into "No".
func nullBool(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
