package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// What Issue again needs of storage (#580, parent #575, ADR 0068): ONE
// transaction that reads the dead document and the fact its Sale still
// stands under the locks a reversal takes, hands them to the service to
// decide, and — if it decides to — owes the replacement through the same
// OweInvoice every owed document is born by. Nothing about the Sale is
// written: its row is locked to be READ, so that the decision and the
// reversal it might race are serialised, and released as it was.
//
// THIS IS THE REISSUE'S TRANSACTION MINUS A DOCUMENT (reissue.go). The
// reissue owes two documents — a Credit Note against an authorized factura
// and the corrected one — and must read what stands against the factura to
// know whether it may. Issue again owes ONE, because there is nothing to
// credit: the document it replaces was never authorized, or was disowned at
// the portal, and no obligation survives it. So the facts it reads are
// fewer, and the reissue's ReissueSaleFacts is deliberately not reused for
// them: two of its four fields are questions about Credit Notes that Issue
// again must never ask, and a shared struct with fields one caller leaves
// zero is a guard waiting to be written against a lie.
//
// THE LOCK ORDER IS THE REVERSAL'S, as the reissue's is. A Sale Reversal
// takes the ticket_sales row FOR UPDATE first and the Sale's invoices FOR
// UPDATE after (sales/repository/sale_reverse.go); this takes them in the
// same order, so the two queue behind each other and never deadlock.
// Whichever commits first, the other decides on what it then finds: an
// Issue again behind a reversal finds the Sale reversed and refuses; a
// reversal behind an Issue again finds a fresh owed factura and withdraws
// it as it withdraws any unsent document.
//
// TWO OPERATORS PRESSING AT ONCE are serialised by the Sale's lock, and the
// live-successor read the decision rests on is made inside this transaction
// against the row as locked. The floor under both is migration 120's
// partial unique index, which admits ONE live successor per document
// whatever any guard believes; a race that got past the read fails there
// rather than forking the chain.

// IssueAgainSaleFacts is what Issue again is decided on beyond the dead
// document's own row, read under the Sale's lock. One field, and the file
// comment says why there is not a fourth of the reissue's.
type IssueAgainSaleFacts struct {
	// SaleStatus is the Ticket Sale's status as it stands: committed, or
	// reversed. Never "" — a document with no Sale is not of kind sale, and
	// the service's guard has refused it long before this transaction.
	SaleStatus string
}

// IssueSaleInvoiceAgain runs Issue again's transaction. decide is called
// with the dead document as locked and the Sale's facts; it returns the
// replacement to owe, or a domain error to refuse with, in which case
// nothing is written. It returns "" and no error when no document has the
// id.
func (r *Repository) IssueSaleInvoiceAgain(ctx context.Context, deadID string, decide func(dead *InvoiceRow, facts IssueAgainSaleFacts) (invoicing.Invoice, error)) (replacementID string, err error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin issue again: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The Sale first, and FOR UPDATE: the reversal's order.
	var ticketSaleID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT ticket_sale_id FROM invoicing_invoices WHERE id = $1`, deadID).Scan(&ticketSaleID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("issue again: read sale of invoice: %w", err)
	}
	var facts IssueAgainSaleFacts
	if ticketSaleID.Valid {
		if err := tx.QueryRowContext(ctx, `
			SELECT status FROM ticket_sales WHERE id = $1 FOR UPDATE
		`, ticketSaleID.String).Scan(&facts.SaleStatus); err != nil {
			return "", fmt.Errorf("issue again: lock sale: %w", err)
		}
	}

	// Then the dead document, in full, FOR UPDATE. In full because the
	// replacement is a copy of it: the lines are children and the live
	// successor is a subquery in invoiceColumns, and both are read here so
	// the decision and the copy rest on one snapshot.
	dead, err := scanInvoice(tx.QueryRowContext(ctx, `SELECT `+invoiceColumns+invoiceFrom+` WHERE i.id = $1 FOR UPDATE OF i`, deadID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("issue again: lock invoice: %w", err)
	}
	if err := loadInvoiceChildrenFrom(ctx, tx, dead); err != nil {
		return "", err
	}

	replacement, err := decide(dead, facts)
	if err != nil {
		return "", err
	}
	replacementID, err = r.OweInvoice(ctx, tx, replacement)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit issue again: %w", err)
	}
	return replacementID, nil
}
