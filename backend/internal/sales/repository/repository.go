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
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/peter/ticket_pos/backend/internal/platform"
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
}

// CommitSale is one Ticket Sale to record within a Sale Import batch.
type CommitSale struct {
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
	PaymentMethod     string
	SoldAt            time.Time
	ConfirmationRef   string
	Line              CommitLine
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
}

// CommittedBatch is the outcome of a committed (or replayed) Sale Import.
type CommittedBatch struct {
	ID        string
	SaleCount int
	Status    string
	Replayed  bool
}

// CapacityError reports that a Ticket Type would be oversold by the batch.
type CapacityError struct {
	Row          int
	TicketTypeID string
	Requested    int
	Available    int
}

func (e *CapacityError) Error() string {
	return fmt.Sprintf("ticket type %s oversold: requested %d, available %d", e.TicketTypeID, e.Requested, e.Available)
}

// UnknownTicketTypeError reports that a row references a Ticket Type absent from the Event.
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

// EventImportContext is the Event metadata a Sale Import needs: its name and the
// timezone (nullable) used to interpret naive sold_at values.
type EventImportContext struct {
	Name     string
	Timezone string
}

// GetEventImportContext returns the Event's name and timezone and whether it
// belongs to the Organization.
func (r *Repository) GetEventImportContext(ctx context.Context, orgID, eventID string) (*EventImportContext, bool, error) {
	var out EventImportContext
	var tz sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT name, timezone FROM events WHERE id = $1 AND organization_id = $2
	`, eventID, orgID).Scan(&out.Name, &tz)
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

// CommitImport records a Sale Import batch, its Ticket Sales and Lines, and the
// resulting capacity decrement in a single transaction. Ticket Type rows are
// locked FOR UPDATE before the capacity check, so concurrent sales cannot
// oversell. The batch is all-or-nothing: any oversell rolls back everything and
// returns a *CapacityError.
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

	// Aggregate requested quantities per Ticket Type, remembering the first row
	// that references each type for error reporting. Lock the involved rows in a
	// stable order to avoid deadlocks between concurrent imports.
	requested := map[string]int{}
	firstRow := map[string]int{}
	var typeIDs []string
	for i, s := range in.Sales {
		id := s.Line.TicketTypeID
		if _, seen := requested[id]; !seen {
			firstRow[id] = i
			typeIDs = append(typeIDs, id)
		}
		requested[id] += s.Line.Quantity
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

	// Capacity check under lock: no Ticket Type may be pushed past capacity.
	for _, id := range typeIDs {
		lt := locked[id]
		if lt.soldCount+requested[id] > lt.capacity {
			return nil, &CapacityError{
				Row:          firstRow[id],
				TicketTypeID: id,
				Requested:    requested[id],
				Available:    lt.capacity - lt.soldCount,
			}
		}
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

	for _, s := range in.Sales {
		unitPrice := locked[s.Line.TicketTypeID].priceCents
		if s.Line.UnitPriceCents != nil {
			unitPrice = *s.Line.UnitPriceCents
		}

		var saleID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO ticket_sales (
				event_id, organization_id, channel, source, payment_method,
				customer_email, customer_first_name, customer_last_name, sold_at, confirmation_ref, status,
				import_batch_id, created_at
			)
			VALUES ($1, $2, 'import', $3, $4, $5, $6, $7, $8, $9, 'active', $10, $11)
			RETURNING id
		`, in.EventID, in.OrganizationID, in.Source, nullString(s.PaymentMethod),
			s.CustomerEmail, s.CustomerFirstName, s.CustomerLastName, s.SoldAt, s.ConfirmationRef, batchID, in.Now).Scan(&saleID)
		if err != nil {
			return nil, err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ticket_sale_lines (ticket_sale_id, ticket_type_id, quantity, unit_price_cents, created_at)
			VALUES ($1, $2, $3, $4, $5)
		`, saleID, s.Line.TicketTypeID, s.Line.Quantity, unitPrice, in.Now); err != nil {
			return nil, err
		}
	}

	for _, id := range typeIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE ticket_types SET sold_count = sold_count + $1, updated_at = $2
			WHERE id = $3
		`, requested[id], in.Now, id); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &CommittedBatch{ID: batchID, SaleCount: len(in.Sales), Status: "committed"}, nil
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
// UPDATE, symmetric to CommitImport), and marks the batch 'reversed'.
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

	// Lock the affected Ticket Types in a stable order (symmetric to CommitImport)
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
}

// ListSalesQuery selects a page of an Event's Ticket Sales for the Sales list.
type ListSalesQuery struct {
	OrganizationID string
	EventID        string
	// Status narrows to Ticket Sales in this lifecycle state (e.g. "active").
	Status string
	Limit  int
	Offset int
}

// ListSales returns one page of an Event's Ticket Sales for the Sales list, one
// row per Ticket Sale, newest first (sold_at DESC with an id tiebreaker so equal
// timestamps do not reorder between pages). The Ticket Sale Lines are aggregated
// per sale in a lateral subquery so a multi-line sale stays a single row (no
// join fan-out): its amount is SUM(quantity × unit_price_cents) and its Ticket
// Types roll up into one ordered list. total is the unpaginated match count via
// COUNT(*) OVER() (ADR-0006).
func (r *Repository) ListSales(ctx context.Context, q ListSalesQuery) ([]SaleRow, int, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
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
		WHERE ts.event_id = $1 AND ts.organization_id = $2 AND ts.status = $3
		ORDER BY ts.sold_at DESC, ts.id DESC
		LIMIT $4 OFFSET $5
	`, q.EventID, q.OrganizationID, q.Status, q.Limit, q.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []SaleRow
	total := 0
	for rows.Next() {
		var s SaleRow
		var typesJSON []byte
		var source, paymentMethod sql.NullString
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
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
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
