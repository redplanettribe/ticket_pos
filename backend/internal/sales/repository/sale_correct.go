package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CorrectImportedSaleInput names one imported Ticket Sale to reverse and the
// replacement to record in its place, already validated and priced.
type CorrectImportedSaleInput struct {
	EventID        string
	OrganizationID string
	SaleID         string
	Replacement    CommitSale
	Now            time.Time
	UpsertCustomer UpsertCustomer
}

// CorrectedSale is the outcome of a Sale Correction: the sale that was reversed
// and the replacement that now stands in its place.
type CorrectedSale struct {
	Reversed    ReversedSale
	Replacement RecordedSale
}

// CorrectImportedSale is a Sale Correction's whole write (#351, ADR 0050): ONE
// transaction that reverses the mistaken imported Ticket Sale through the
// shared primitive, records the replacement through the sale-commit spine every
// channel shares, and links the two — replaced_by_sale_id on the old row,
// replaces_sale_id on the new one.
//
// THE ORDER IS THE ACCOUNTING. The reversal runs first, so by the time the spine
// locks the replacement's Ticket Type and checks capacity, the old sale's
// quantity has already been given back: capacity is net of the sale being
// reversed without anybody subtracting anything. The same ordering is what
// makes the failure mode honest — a capacity race lost between the service's
// validation and this lock fails the spine, the transaction rolls back, and the
// original sale is exactly as active as it was.
//
// THE REPLACEMENT BELONGS TO NO BATCH: import_batch_id stays NULL, so a later
// undo of the original's batch walks past it. Channel `import`, Source as the
// caller set it (`direct`): it is an imported sale in every respect but the
// spreadsheet.
//
// Refusals are the single-sale reversal's: *SaleNotFoundError,
// *SaleNotImportedError, *SaleAlreadyReversedError, decided under the row lock
// so two corrections of one sale yield one correction and one refusal.
func (r *Repository) CorrectImportedSale(ctx context.Context, in CorrectImportedSaleInput) (*CorrectedSale, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	reversed, err := reverseOneImportedSaleTx(ctx, tx, in.OrganizationID, in.EventID, in.SaleID, in.Now)
	if err != nil {
		return nil, err
	}

	recorded, err := r.CommitSales(ctx, tx, CommitSalesInput{
		EventID:        in.EventID,
		OrganizationID: in.OrganizationID,
		Channel:        "import",
		Source:         "direct",
		Sales:          []CommitSale{in.Replacement},
		Now:            in.Now,
		UpsertCustomer: in.UpsertCustomer,
	})
	if err != nil {
		return nil, err
	}
	if len(recorded) != 1 {
		return nil, errors.New("sales: a Sale Correction recorded no replacement")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_sales SET replaced_by_sale_id = $2 WHERE id = $1
	`, in.SaleID, recorded[0].ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_sales SET replaces_sale_id = $2 WHERE id = $1
	`, recorded[0].ID, in.SaleID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &CorrectedSale{Reversed: *reversed, Replacement: recorded[0]}, nil
}

// SaleLineQuantity is one Ticket Sale Line's Ticket Type and quantity.
type SaleLineQuantity struct {
	TicketTypeID string
	Quantity     int
}

// ImportedSaleLines reads the lines of one imported, active Ticket Sale on the
// Event: what a Sale Correction gives back before the replacement is judged.
// The same three refusals as CorrectImportedSale, decided without a lock —
// the transaction decides them again under one.
func (r *Repository) ImportedSaleLines(ctx context.Context, orgID, eventID, saleID string) ([]SaleLineQuantity, error) {
	if err := requireActiveImportedSale(ctx, r.db.Pool, orgID, eventID, saleID, false); err != nil {
		return nil, err
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT ticket_type_id, quantity FROM ticket_sale_lines WHERE ticket_sale_id = $1
	`, saleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SaleLineQuantity
	for rows.Next() {
		var l SaleLineQuantity
		if err := rows.Scan(&l.TicketTypeID, &l.Quantity); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
