package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
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

// RecordSaleReAddressingResult is what came of a recording. Recorded is the
// row written when SaleActive is true; Replaced is the row that was pending
// until this recording withdrew it, or nil when nothing was. SaleActive false
// means the Sale was reversed between the service's read and this lock, and
// nothing was written.
type RecordSaleReAddressingResult struct {
	Recorded   *SaleReAddressingRow
	Replaced   *SaleReAddressingRow
	SaleActive bool
}

// RecordSaleReAddressing writes one Sale Re-addressing under the Sale's own row
// lock (#420, #423, ADR 0058).
//
// THE LOCK IS THE SALE'S ROW, taken FOR UPDATE, which serialises this against
// every reversal path — all of which lock the same row — and against a second
// Operator recording at the same moment. Under it the Sale's status and address
// are re-read, so the guards the service applied before the lock are re-judged
// on the row as it stands, and the previous_email written is the address the
// Sale carries NOW rather than the one a stale read saw.
//
// ONE PENDING PER SALE is the partial unique index's rule (migration 093). A
// recording made while one is pending REPLACES it in this same transaction:
// the pending row is stamped withdrawn_at and the new one inserted, so the
// index is satisfied at commit, the old row stays as evidence of what was typed
// first, and its link — bound to that row's id and instant — stops opening the
// moment this commits. That is the Operator fixing their own typo, and it is
// also "send again": the same address recorded again is a new row with a new
// link. The index stays as the backstop for any path that forgets the lock.
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

	replaced, err := withdrawPendingSaleReAddressing(ctx, tx, in.TicketSaleID, in.Now)
	if err != nil {
		return nil, err
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
	return &RecordSaleReAddressingResult{Recorded: recorded, Replaced: replaced, SaleActive: true}, nil
}

// WithdrawSaleReAddressing ends the Sale's pending re-addressing record under
// the Sale's row lock (#423, ADR 0058): withdrawn_at is stamped and nothing
// else changes. The row is KEPT — a withdrawn record is the evidence that an
// address was typed and then taken back — and its link dies with the stamp,
// since the link's open reads the row's state. Returns nil, and writes
// nothing, when no record is PENDING: none recorded, every one already ended,
// or the one unended row expired beneath the Sale's reversal or the Event's
// start — judged under the lock, from the Sale and Event as they stand.
//
// The same lock as the recording and the acceptance take, so a click landing
// at the same instant either completes first (and this finds nothing pending)
// or waits and finds the row ended.
func (r *Repository) WithdrawSaleReAddressing(ctx context.Context, ticketSaleID string, now time.Time) (*SaleReAddressingRow, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var saleStatus string
	var eventStartsAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT ts.status, e.starts_at
		FROM ticket_sales ts
		JOIN events e ON e.id = ts.event_id
		WHERE ts.id = $1
		FOR UPDATE OF ts
	`, ticketSaleID).Scan(&saleStatus, &eventStartsAt); err != nil {
		return nil, err
	}
	unended, err := scanSaleReAddressing(tx.QueryRowContext(ctx, `
		SELECT `+saleReAddressingColumns+`
		FROM sale_re_addressings
		WHERE ticket_sale_id = $1 AND accepted_at IS NULL AND withdrawn_at IS NULL
	`, ticketSaleID))
	if err != nil {
		return nil, err
	}
	if unended == nil {
		return nil, nil
	}
	state := sales.DeriveReAddressingState(nil, nil, saleStatus, nullTimeOrNil(eventStartsAt), now)
	if state != sales.ReAddressingPending {
		return nil, nil
	}
	withdrawn, err := withdrawPendingSaleReAddressing(ctx, tx, ticketSaleID, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return withdrawn, nil
}

// withdrawPendingSaleReAddressing stamps withdrawn_at on the Sale's one unended
// row, if there is one, and returns it as it now stands. Called only under the
// Sale's row lock, by the withdrawal and by a recording that replaces.
//
// It judges by the two ends alone and not by the derived state: a row the
// Sale's reversal or the Event's start has expired beneath is still the row
// that would collide with the partial unique index, so a replacement must end
// it too. Whether such a row was worth withdrawing on its own is the service's
// question, answered from the Sale and Event read beside it.
func withdrawPendingSaleReAddressing(ctx context.Context, tx *sql.Tx, ticketSaleID string, now time.Time) (*SaleReAddressingRow, error) {
	return scanSaleReAddressing(tx.QueryRowContext(ctx, `
		UPDATE sale_re_addressings
		SET withdrawn_at = $2
		WHERE ticket_sale_id = $1 AND accepted_at IS NULL AND withdrawn_at IS NULL
		RETURNING `+saleReAddressingColumns,
		ticketSaleID, now))
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
