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
}

// CommitSale is one Ticket Sale to record on any Sales Channel: the customer
// identity as transacted, the optional Payment Method, and the Ticket Sale
// Lines with their unit-price snapshots.
type CommitSale struct {
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
	// CustomerTaxID is the Tax ID this sale is transacted under. It is written
	// onto the sale exactly once, here, and no later sale or profile edit ever
	// touches it (ADR 0016). Unset on a sale that carries none — the `import`
	// channel, and every sale recorded before the Tax ID existed.
	CustomerTaxID platform.SaleTaxID
	// CustomerPhone is the buyer's phone number in canonical E.164 form as typed
	// at checkout, empty on every channel that collects none — the `import` file,
	// the staff-recorded sale, and an online buyer who skipped the optional field.
	//
	// It is the one field here that is NOT written onto the Ticket Sale. It rides
	// this struct only to reach UpsertCustomer below, which may write it onto the
	// Customer profile so it prefills their next purchase (#107). The Tax ID is
	// snapshotted beside it because ADR 0016 makes it a fiscal fact of the sale,
	// frozen against later profile edits; a phone number carries no such
	// requirement, and snapshotting it would cascade into the staff sales list,
	// receipts, the Sale Import format and the public contract for a value none of
	// them read (#103).
	CustomerPhone   string
	PaymentMethod   string
	SoldAt          time.Time
	ConfirmationRef string
	Lines           []CommitLine
}

// UpsertCustomer creates or reuses the Customer for one Ticket Sale inside the
// batch's transaction and returns the Customer id. The sales service supplies it,
// bound to the customers service, so the cross-module call goes through that
// module's service rather than its repository.
//
// phone is the buyer's canonical E.164 number, empty on the channels that
// collect none. It is passed here rather than written by this package because
// nothing in sales stores it beyond the Payment: the profile is its destination
// (#107).
type UpsertCustomer func(ctx context.Context, tx *sql.Tx, email, firstName, lastName string, taxID platform.SaleTaxID, phone string, now time.Time) (string, error)

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
}

// RecordedSale is one Ticket Sale as actually written: its database id — which
// only exists once the row is inserted — alongside the customer identity and
// reference the Sale Confirmation needs.
//
// The id is what makes a Confirmation Link possible: the link names one Ticket
// Sale, and until the batch commits there is no sale to name.
type RecordedSale struct {
	ID                string
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
	Name     string
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

// GetEventImportContext returns the Event's name, timezone, and schedule, and
// whether it belongs to the Organization.
func (r *Repository) GetEventImportContext(ctx context.Context, orgID, eventID string) (*EventImportContext, bool, error) {
	var out EventImportContext
	var tz sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT e.name, e.timezone, o.currency, e.starts_at, e.ends_at
		FROM events e
		JOIN organizations o ON o.id = e.organization_id
		WHERE e.id = $1 AND e.organization_id = $2
	`, eventID, orgID).Scan(&out.Name, &tz, &out.Currency, &out.StartsAt, &out.EndsAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	out.Timezone = tz.String
	return &out, true, nil
}

// ImportTicketType is a Ticket Type snapshot used to match import rows and
// compute capacity impact.
type ImportTicketType struct {
	ID         string
	Name       string
	PriceCents int
	Capacity   int
	SoldCount  int
}

// ListTicketTypesForImport returns the Event's Ticket Types for template
// generation and import validation.
func (r *Repository) ListTicketTypesForImport(ctx context.Context, orgID, eventID string) ([]ImportTicketType, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, name, price_cents, capacity, sold_count
		FROM ticket_types
		WHERE event_id = $1 AND organization_id = $2
		ORDER BY sort_order, name
	`, eventID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ImportTicketType
	for rows.Next() {
		var tt ImportTicketType
		if err := rows.Scan(&tt.ID, &tt.Name, &tt.PriceCents, &tt.Capacity, &tt.SoldCount); err != nil {
			return nil, err
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
func (r *Repository) ListActiveSaleKeys(ctx context.Context, orgID, eventID string) ([]ExistingSaleKey, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT ts.customer_email, tsl.ticket_type_id, ts.sold_at
		FROM ticket_sales ts
		JOIN ticket_sale_lines tsl ON tsl.ticket_sale_id = ts.id
		WHERE ts.event_id = $1 AND ts.organization_id = $2 AND ts.status = 'active'
	`, eventID, orgID)
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
		if err := sales.RequireTaxID(in.Channel, s.CustomerTaxID); err != nil {
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
			SELECT price_cents, capacity, sold_count
			FROM ticket_types
			WHERE id = $1 AND event_id = $2 AND organization_id = $3
			FOR UPDATE
		`, id, in.EventID, in.OrganizationID).Scan(&lt.priceCents, &lt.capacity, &lt.soldCount)
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
		// The phone goes through here and stops: the INSERT below has no column
		// for it, deliberately (see CommitSale.CustomerPhone).
		customerID, err := in.UpsertCustomer(ctx, tx, s.CustomerEmail, s.CustomerFirstName, s.CustomerLastName, s.CustomerTaxID, s.CustomerPhone, in.Now)
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
				created_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $13, $14, $10, $11, 'active', $12)
			RETURNING id
		`, in.EventID, in.OrganizationID, in.Channel, nullString(in.Source), nullString(s.PaymentMethod),
			customerID, s.CustomerEmail, s.CustomerFirstName, s.CustomerLastName, s.SoldAt, s.ConfirmationRef, in.Now,
			nullString(s.CustomerTaxID.Type), nullString(s.CustomerTaxID.Number)).Scan(&saleID)
		if err != nil {
			return nil, err
		}

		amountCents := 0
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
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO ticket_sale_lines (
					ticket_sale_id, ticket_type_id, quantity, unit_price_cents,
					base_price_cents, fee_cents, fee_iva_cents, fee_basis_points, fee_iva_basis_points,
					created_at
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			`, saleID, line.TicketTypeID, line.Quantity, unitPrice,
				fee.BasePriceCents, fee.FeeCents, fee.FeeIVACents,
				fee.FeeBasisPoints, fee.FeeIVABasisPoints, in.Now); err != nil {
				return nil, err
			}
		}

		recorded = append(recorded, RecordedSale{
			ID:                saleID,
			ConfirmationRef:   s.ConfirmationRef,
			CustomerEmail:     s.CustomerEmail,
			CustomerFirstName: s.CustomerFirstName,
			CustomerLastName:  s.CustomerLastName,
			AmountCents:       amountCents,
			CustomerTaxID:     s.CustomerTaxID,
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

// liveHoldsForUpdate returns the quantities live Capacity Holds claim per
// Ticket Type on the Event, read inside the commit transaction, optionally
// excluding one Payment (the one whose own commit is running).
func (r *Repository) liveHoldsForUpdate(ctx context.Context, tx *sql.Tx, eventID string, cutoff time.Time, excludePaymentID string) (map[string]int, error) {
	var rows *sql.Rows
	var err error
	if excludePaymentID == "" {
		rows, err = tx.QueryContext(ctx, sales.LiveHoldsSQL("$2", "$1", ""), eventID, cutoff)
	} else {
		rows, err = tx.QueryContext(ctx, sales.LiveHoldsSQL("$2", "$1", "$3"), eventID, cutoff, excludePaymentID)
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
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
	ConfirmationRef   string
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

// ReverseBatch reverses a committed Sale Import batch in a single transaction:
// it marks the batch's active Ticket Sales 'reversed', restores each affected
// Ticket Type's sold_count by the reversed quantities (locking ticket_types FOR
// UPDATE, symmetric to CommitSales), and marks the batch 'reversed'.
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

	// Aggregate the quantities to restore per Ticket Type from the batch's active
	// sales, and collect the sales for the void notices.
	quantityRows, err := tx.QueryContext(ctx, `
		SELECT tsl.ticket_type_id, SUM(tsl.quantity)
		FROM ticket_sale_lines tsl
		JOIN ticket_sales ts ON ts.id = tsl.ticket_sale_id
		WHERE ts.import_batch_id = $1 AND ts.status = 'active'
		GROUP BY tsl.ticket_type_id
	`, in.BatchID)
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

	saleRows, err := tx.QueryContext(ctx, `
		SELECT customer_email, customer_first_name, customer_last_name, confirmation_ref
		FROM ticket_sales
		WHERE import_batch_id = $1 AND status = 'active'
	`, in.BatchID)
	if err != nil {
		return nil, err
	}
	var reversed []ReversedSale
	for saleRows.Next() {
		var s ReversedSale
		if err := saleRows.Scan(&s.CustomerEmail, &s.CustomerFirstName, &s.CustomerLastName, &s.ConfirmationRef); err != nil {
			saleRows.Close()
			return nil, err
		}
		reversed = append(reversed, s)
	}
	if err := saleRows.Err(); err != nil {
		saleRows.Close()
		return nil, err
	}
	saleRows.Close()

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

	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_sales SET status = 'reversed'
		WHERE import_batch_id = $1 AND status = 'active'
	`, in.BatchID); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE sale_import_batches SET status = 'reversed'
		WHERE id = $1
	`, in.BatchID); err != nil {
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
type SaleLineRollup struct {
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
	Currency          string
	SoldAt            time.Time
	Channel           string
	Source            *string
	Status            string
	ConfirmationRef   string
	RecordedAt        time.Time
	PaymentMethod     *string
	// CustomerTaxIDType/Number are the Tax ID snapshot the sale was transacted
	// under, nil together on sales recorded without one (legacy rows and
	// imports that never collected it — ADR 0016).
	CustomerTaxIDType   *string
	CustomerTaxIDNumber *string
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
	Sort   string
	Dir    string
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
// amount is SUM(quantity × unit_price_cents) and its Ticket Types roll up into
// one ordered list. total is the unpaginated match count via COUNT(*) OVER()
// (ADR-0006).
//
// Optional filters (ticket type, sold-at range, search, channel/source/payment
// method) are appended to the WHERE clause; the ticket-type filter uses an
// EXISTS sub-query so it narrows sales without touching the rollup or fanning
// the row out.
func (r *Repository) ListSales(ctx context.Context, q ListSalesQuery) ([]SaleRow, int, error) {
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
	args = append(args, q.Limit)
	limitP := len(args)
	args = append(args, q.Offset)
	offsetP := len(args)

	query := fmt.Sprintf(`
		SELECT
			ts.id,
			ts.customer_first_name,
			ts.customer_last_name,
			ts.customer_email,
			lines.amount_cents,
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
			COUNT(*) OVER() AS total
		FROM ticket_sales ts
		JOIN organizations org ON org.id = ts.organization_id
		JOIN LATERAL (
			SELECT
				COALESCE(SUM(tsl.quantity * tsl.unit_price_cents), 0) AS amount_cents,
				COALESCE(
					json_agg(
						json_build_object('ticket_type_name', tt.name, 'quantity', tsl.quantity)
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
		LIMIT $%d OFFSET $%d
	`, filterSQL, limitP, offsetP)

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
		var source, paymentMethod, taxIDType, taxIDNumber sql.NullString
		if err := rows.Scan(
			&s.ID,
			&s.CustomerFirstName,
			&s.CustomerLastName,
			&s.CustomerEmail,
			&s.AmountCents,
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
			&total,
		); err != nil {
			return nil, 0, err
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
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// lineNetProceedsSQL is the Net Proceeds of one Ticket Sale Line (aliased tsl),
// read off the snapshot it froze at sale time: quantity × (what the Customer
// paid − the Platform Fee − the Fee IVA withheld). The single definition serves
// both the Event's sales summary and the Organization's Withdrawable Balance,
// so the arithmetic cannot drift between the two surfaces (ADR 0014).
const lineNetProceedsSQL = `tsl.quantity * (tsl.unit_price_cents - tsl.fee_cents - tsl.fee_iva_cents)`

// SalesSummaryRow is the Sales tab's stat strip, read straight off the Event's
// recorded sales: what the Event has left the Organization, and how many active
// Ticket Sales it has made.
type SalesSummaryRow struct {
	NetProceedsCents int
	SalesCount       int
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
func (r *Repository) SalesSummary(ctx context.Context, orgID, eventID string) (SalesSummaryRow, error) {
	var out SalesSummaryRow
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(net.net_proceeds_cents) FILTER (WHERE ts.channel = 'online'), 0),
			COUNT(*)
		FROM ticket_sales ts
		JOIN LATERAL (
			SELECT COALESCE(SUM(`+lineNetProceedsSQL+`), 0) AS net_proceeds_cents
			FROM ticket_sale_lines tsl
			WHERE tsl.ticket_sale_id = ts.id
		) net ON TRUE
		WHERE ts.event_id = $1 AND ts.organization_id = $2 AND ts.status = 'active'
	`, eventID, orgID).Scan(&out.NetProceedsCents, &out.SalesCount)
	if err != nil {
		return SalesSummaryRow{}, err
	}
	return out, nil
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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
