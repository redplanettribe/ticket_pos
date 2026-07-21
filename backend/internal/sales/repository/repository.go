// Package repository provides hand-written SQL data access for the sales domain.
// Sales covers online, in-person, and import sales plus capacity logic.
package repository

import (
	"context"
	"database/sql"
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
	CustomerEmail   string
	CustomerName    string
	PaymentMethod   string
	SoldAt          time.Time
	ConfirmationRef string
	Line            CommitLine
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
				customer_email, customer_name, sold_at, confirmation_ref, status,
				import_batch_id, created_at
			)
			VALUES ($1, $2, 'import', $3, $4, $5, $6, $7, $8, 'active', $9, $10)
			RETURNING id
		`, in.EventID, in.OrganizationID, in.Source, nullString(s.PaymentMethod),
			s.CustomerEmail, s.CustomerName, s.SoldAt, s.ConfirmationRef, batchID, in.Now).Scan(&saleID)
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
