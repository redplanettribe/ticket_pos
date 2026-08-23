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

	var channel, status string
	err = tx.QueryRowContext(ctx, `
		SELECT channel, status FROM ticket_sales
		WHERE id = $1 AND event_id = $2 AND organization_id = $3
		FOR UPDATE
	`, in.SaleID, in.EventID, in.OrganizationID).Scan(&channel, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &SaleNotFoundError{SaleID: in.SaleID}
	}
	if err != nil {
		return nil, err
	}
	if channel != "import" {
		return nil, &SaleNotImportedError{SaleID: in.SaleID, Channel: channel}
	}
	if status != "active" {
		return nil, &SaleAlreadyReversedError{SaleID: in.SaleID}
	}

	reversed, err := reverseSalesTx(ctx, tx, ReverseSalesInput{
		EventID:        in.EventID,
		OrganizationID: in.OrganizationID,
		SaleIDs:        []string{in.SaleID},
		Actor:          sales.ReversalActorStaff,
		Now:            in.Now,
	})
	if err != nil {
		return nil, err
	}
	if len(reversed) != 1 {
		// The row was locked and read active a moment ago; the primitive not
		// finding it is a fact about this transaction, not a refusal to report.
		return nil, &SaleAlreadyReversedError{SaleID: in.SaleID}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &reversed[0], nil
}
