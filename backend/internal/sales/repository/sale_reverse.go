package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// SaleNotFoundError reports that no Ticket Sale with that id exists on the
// Event, under the Organization — a sale under somebody else's Event is "not
// found" rather than "not yours", because the id proves nothing either way.
type SaleNotFoundError struct{ SaleID string }

func (e *SaleNotFoundError) Error() string { return "ticket sale not found: " + e.SaleID }

// SaleNotImportedError reports that the sale is not on the `import` channel,
// which is the only one staff may reverse sale-by-sale (ADR 0050).
type SaleNotImportedError struct {
	SaleID  string
	Channel string
}

func (e *SaleNotImportedError) Error() string {
	return "ticket sale " + e.SaleID + " is on channel " + e.Channel + ", not import"
}

// SaleAlreadyReversedError reports that the sale is already reversed.
type SaleAlreadyReversedError struct{ SaleID string }

func (e *SaleAlreadyReversedError) Error() string {
	return "ticket sale already reversed: " + e.SaleID
}

// queryRower is the one method the imported-sale guard needs, so it runs the
// same way on the pool (lock-free) and inside a transaction (FOR UPDATE).
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// requireActiveImportedSale decides the three refusals every single-sale staff
// action shares: *SaleNotFoundError when no such sale is on the Event under the
// Organization, *SaleNotImportedError on any channel but `import`, and
// *SaleAlreadyReversedError when it is no longer active. With lock=true the row
// is taken FOR UPDATE first, so the refusal is decided on what this transaction
// will act on.
func requireActiveImportedSale(ctx context.Context, q queryRower, orgID, eventID, saleID string, lock bool) error {
	query := `
		SELECT channel, status FROM ticket_sales
		WHERE id = $1 AND event_id = $2 AND organization_id = $3
	`
	if lock {
		query += " FOR UPDATE"
	}
	var channel, status string
	err := q.QueryRowContext(ctx, query, saleID, eventID, orgID).Scan(&channel, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return &SaleNotFoundError{SaleID: saleID}
	}
	if err != nil {
		return err
	}
	if channel != "import" {
		return &SaleNotImportedError{SaleID: saleID, Channel: channel}
	}
	if status != "active" {
		return &SaleAlreadyReversedError{SaleID: saleID}
	}
	return nil
}

// reverseOneImportedSaleTx locks and checks the sale, then reverses exactly it
// through the shared primitive as the staff actor. The primitive not finding
// the row it locked and read active a moment ago is a fact about this
// transaction, not a refusal to report, so it is folded into
// *SaleAlreadyReversedError.
func reverseOneImportedSaleTx(ctx context.Context, tx *sql.Tx, orgID, eventID, saleID string, now time.Time) (*ReversedSale, error) {
	if err := requireActiveImportedSale(ctx, tx, orgID, eventID, saleID, true); err != nil {
		return nil, err
	}
	reversed, err := reverseSalesTx(ctx, tx, ReverseSalesInput{
		EventID:        eventID,
		OrganizationID: orgID,
		SaleIDs:        []string{saleID},
		Actor:          sales.ReversalActorStaff,
		Now:            now,
	})
	if err != nil {
		return nil, err
	}
	if len(reversed) != 1 {
		return nil, &SaleAlreadyReversedError{SaleID: saleID}
	}
	return &reversed[0], nil
}

// ReverseImportedSaleInput names one imported Ticket Sale to reverse.
type ReverseImportedSaleInput struct {
	EventID        string
	OrganizationID string
	SaleID         string
	Now            time.Time
}

// ReverseImportedSale reverses ONE imported Ticket Sale in a single
// transaction, through the same primitive the batch undo and the Operator
// Reversal use: the sale goes `reversed`, stamped with the staff actor and the
// moment, its Ticket Types get their sold_count back, and nothing else on the
// row changes — no note, no operator memo, and no correction link (#350,
// ADR 0050). Its Sale Import batch, if any, is not touched: a batch stays
// undoable for whatever active sales it has left, and the latest-only guard
// that protects batch undo does not apply to one sale (ADR 0050).
//
// Refusals, each its own error so the service can name them: a sale not on
// this Event returns *SaleNotFoundError; one on any channel but `import`
// returns *SaleNotImportedError; one already reversed returns
// *SaleAlreadyReversedError. The row is locked before any of that is decided,
// so two staff pressing Reverse together produce one reversal and one refusal.
func (r *Repository) ReverseImportedSale(ctx context.Context, in ReverseImportedSaleInput) (*ReversedSale, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	reversed, err := reverseOneImportedSaleTx(ctx, tx, in.OrganizationID, in.EventID, in.SaleID, in.Now)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return reversed, nil
}
