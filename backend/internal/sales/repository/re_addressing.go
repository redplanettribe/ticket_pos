package repository

import (
	"context"
	"database/sql"
	"time"
)

// SaleReAddressingRow is one row of sale_re_addressings (migration 093): the
// Operator's record that an Online Sale is being moved to the address its buyer
// meant, and how far that has got.
//
// THERE IS NO STATE COLUMN, and none is scanned. The state is derived by the
// service from the two ends below and from the Sale and Event beside the row,
// which is why the row carries neither: a reversal or an Event moved after the
// recording changes the state of a row it never touches.
type SaleReAddressingRow struct {
	ID            string
	TicketSaleID  string
	OperatorEmail string
	// PreviousEmail is the Sale's address at request time — the wrong one.
	PreviousEmail string
	// CorrectedEmail is NULL once #424's purge has taken an unaccepted
	// address at the Event's start. Never NULL on a row written here.
	CorrectedEmail sql.NullString
	Note           sql.NullString
	RequestedAt    time.Time
	AcceptedAt     sql.NullTime
	WithdrawnAt    sql.NullTime
}

// RecordSaleReAddressingInput is one recording as the service has already
// judged it: the Sale, the operator, the normalised corrected address, the
// note, and the instant.
type RecordSaleReAddressingInput struct {
	TicketSaleID   string
	OperatorEmail  string
	CorrectedEmail string
	Note           *string
	Now            time.Time
}

// RecordSaleReAddressingResult is what came of a recording. Exactly one of
// Recorded and Pending is set when SaleActive is true: either the row was
// written, or a pending row already stood and nothing was written. SaleActive
// false means the Sale was reversed between the service's read and this lock,
// and nothing was written either.
type RecordSaleReAddressingResult struct {
	Recorded   *SaleReAddressingRow
	Pending    *SaleReAddressingRow
	SaleActive bool
}

// RecordSaleReAddressing writes one Sale Re-addressing under the Sale's own row
// lock (#420, ADR 0058).
//
// THE LOCK IS THE SALE'S ROW, taken FOR UPDATE, which serialises this against
// every reversal path — all of which lock the same row — and against a second
// Operator recording at the same moment. Under it the Sale's status and address
// are re-read, so the guards the service applied before the lock are re-judged
// on the row as it stands, and the previous_email written is the address the
// Sale carries NOW rather than the one a stale read saw.
//
// ONE PENDING PER SALE is the partial unique index's rule (migration 093), and
// this reads for a pending row under the same lock rather than catching the
// unique violation: a refusal that names the address already pending is worth
// more to the Operator than a constraint error, and the index stays as the
// backstop for any path that forgets the lock.
func (r *Repository) RecordSaleReAddressing(ctx context.Context, in RecordSaleReAddressingInput) (*RecordSaleReAddressingResult, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var status, previousEmail string
	if err := tx.QueryRowContext(ctx, `
		SELECT status, customer_email
		FROM ticket_sales
		WHERE id = $1
		FOR UPDATE
	`, in.TicketSaleID).Scan(&status, &previousEmail); err != nil {
		return nil, err
	}
	if status != "active" {
		return &RecordSaleReAddressingResult{SaleActive: false}, nil
	}

	pending, err := scanSaleReAddressing(tx.QueryRowContext(ctx, `
		SELECT `+saleReAddressingColumns+`
		FROM sale_re_addressings
		WHERE ticket_sale_id = $1 AND accepted_at IS NULL AND withdrawn_at IS NULL
	`, in.TicketSaleID))
	if err != nil {
		return nil, err
	}
	if pending != nil {
		return &RecordSaleReAddressingResult{Pending: pending, SaleActive: true}, nil
	}

	recorded, err := scanSaleReAddressing(tx.QueryRowContext(ctx, `
		INSERT INTO sale_re_addressings
			(ticket_sale_id, operator_email, previous_email, corrected_email, note, requested_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+saleReAddressingColumns,
		in.TicketSaleID, in.OperatorEmail, previousEmail, in.CorrectedEmail, in.Note, in.Now))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RecordSaleReAddressingResult{Recorded: recorded, SaleActive: true}, nil
}

// ListSaleReAddressings returns every re-addressing ever recorded against one
// Sale, oldest first — the history the Operator lookup reads.
func (r *Repository) ListSaleReAddressings(ctx context.Context, ticketSaleID string) ([]SaleReAddressingRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+saleReAddressingColumns+`
		FROM sale_re_addressings
		WHERE ticket_sale_id = $1
		ORDER BY requested_at, id
	`, ticketSaleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SaleReAddressingRow
	for rows.Next() {
		row, err := scanSaleReAddressing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

const saleReAddressingColumns = `id, ticket_sale_id, operator_email, previous_email, corrected_email, note, requested_at, accepted_at, withdrawn_at`

// scanSaleReAddressing reads one row, or nil when the query found none.
func scanSaleReAddressing(row rowScanner) (*SaleReAddressingRow, error) {
	var out SaleReAddressingRow
	err := row.Scan(
		&out.ID,
		&out.TicketSaleID,
		&out.OperatorEmail,
		&out.PreviousEmail,
		&out.CorrectedEmail,
		&out.Note,
		&out.RequestedAt,
		&out.AcceptedAt,
		&out.WithdrawnAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}
