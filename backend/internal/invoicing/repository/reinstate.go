package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// What Reinstate needs of storage (#749, spec #742, ADR 0078 section 5,
// migration 134): ONE transaction that reads the annulled factura, its
// standing annulment and its Sale under the locks a reversal takes, hands
// them to the service to decide what is owed, and then returns the factura
// to authorized, closes the annulment and owes the Credit Note, if any,
// through the same OweInvoice every owed document is born by.
//
// THE LOCK ORDER IS THE REVERSAL'S, as Issue again's and the reissue's are:
// the ticket_sales row FOR UPDATE first, the factura after, then its
// standing annulment FOR UPDATE, which is the row the Annulment Check
// records its asks under (RecordAnnulmentAsk), so a verdict cannot move
// under the decision, and an ask that lands after the commit finds the
// annulment reinstated and records nothing.

// ReinstateFacts is what Reinstate is decided on beyond the factura's own
// row, read under the locks.
type ReinstateFacts struct {
	// SaleStatus is the Ticket Sale's status, "" for a manual Tax Invoice.
	SaleStatus string
	// ReversalRoute is, on a reversed Sale, the route it was reversed by,
	// as a reversal's Credit Note names it (reversalRouteSQL).
	ReversalRoute string
	// SaleHasLiveFactura says another Sale Invoice stands for the Sale: an
	// Issue again or Reissue successor that has not died
	// (saleHasLiveFactura, read while this factura is still annulled, so it
	// is never the factura itself).
	SaleHasLiveFactura bool
	// AnnulmentAtMismatch is the mismatch predicate, read after the
	// standing annulment's lock (annulmentAtMismatchSQL).
	AnnulmentAtMismatch bool
	// AnnulmentVerdict is the standing annulment's check verdict as locked,
	// unconfirmed where it has none (recorded before the check existed, or
	// before the ledger did): what a refusal names.
	AnnulmentVerdict invoicing.AnnulmentVerdict
	// AnnulmentID is the standing annulment the Reinstate closes, "" when
	// the factura has none (annulled before the ledger existed).
	AnnulmentID string
}

// FetchedAuthorization is the SRI's authorization of a factura annulled
// before the platform stored one (from pending or needs_attention), as
// Reinstate asked autorizacionComprobante for it: the authorized XML, the
// authorization's number and its date.
type FetchedAuthorization struct {
	XML    []byte
	Number string
	Date   time.Time
}

// FacturaReinstatement is what a Reinstate records: who, when, the optional
// note, and the authorization fetched for a factura that never stored one.
type FacturaReinstatement struct {
	ReinstatedBy  string
	ReinstatedAt  time.Time
	Note          string
	Authorization *FetchedAuthorization
}

// ReinstateDecision is what the service decided under the locks: the
// Credit Note to owe, nil when none.
type ReinstateDecision struct {
	CreditNote *invoicing.Invoice
}

// reversalRouteSQL is the route the Ticket Sale `ts` was reversed by, as
// sales.SaleReversal's Route names it, derived from what ticket_sales keeps
// exactly as the Sales Export derives it (exportedReversalRoute): the actor,
// then for staff whether a Sale Correction replaced it or its import batch
// was undone at the instant it was reversed. Only a reversed Sale with
// money reaches a factura, so the Upgrade's sixth route never arises here.
const reversalRouteSQL = `CASE ts.reversed_by
	WHEN 'customer' THEN 'customer'
	WHEN 'operator' THEN 'platform'
	WHEN 'staff' THEN CASE
		WHEN ts.replaced_by_sale_id IS NOT NULL THEN 'correction'
		WHEN COALESCE((SELECT b.undone_at IS NOT NULL AND b.undone_at = ts.reversed_at
			FROM sale_import_batches b WHERE b.id = ts.import_batch_id), FALSE) THEN 'import_undo'
		ELSE 'staff_reversal' END
	ELSE 'platform' END`

// ReinstateFactura runs Reinstate's transaction. decide is called with the
// factura as locked (lines included, since a Credit Note copies them) and
// its facts; it returns what to owe, or a domain error to refuse with, in
// which case nothing is written. It reports whether a factura has the id,
// and the owed Credit Note's id, "" when none was owed.
//
// THE WRITE, in one transaction once decide admits it:
//
//   - the factura: back to authorized, annulled_by and annulled_at cleared
//     (migration 099 ties them to the annulled status), the fetched
//     authorization stored when there is one, and on a Sale Invoice never
//     delivered, due for its delivery at once, as any authorized one is;
//   - the standing annulment: stamped reinstated (who, when, note), its
//     check stopped (no next ask, no lease; the verdict stays as the
//     history it is), an owed Annulment Notice dropped, and the
//     reinstatement notice owed exactly when the Annulment Notice was sent
//     and no Credit Note follows;
//   - the Credit Note decide asked for, owed and due at once.
func (r *Repository) ReinstateFactura(ctx context.Context, invoiceID string, rein FacturaReinstatement, decide func(row *InvoiceRow, facts ReinstateFacts) (ReinstateDecision, error)) (found bool, creditNoteID string, err error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return false, "", fmt.Errorf("begin reinstate: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The Sale first, and FOR UPDATE: the reversal's order.
	var ticketSaleID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT ticket_sale_id FROM invoicing_invoices WHERE id = $1`, invoiceID).Scan(&ticketSaleID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("reinstate: read sale of invoice: %w", err)
	}
	var facts ReinstateFacts
	if ticketSaleID.Valid {
		if err := tx.QueryRowContext(ctx, `
			SELECT ts.status, CASE WHEN ts.status = 'reversed' THEN `+reversalRouteSQL+` ELSE '' END
			FROM ticket_sales ts WHERE ts.id = $1 FOR UPDATE
		`, ticketSaleID.String).Scan(&facts.SaleStatus, &facts.ReversalRoute); err != nil {
			return false, "", fmt.Errorf("reinstate: lock sale: %w", err)
		}
	}

	row, err := scanInvoice(tx.QueryRowContext(ctx, `SELECT `+invoiceColumns+invoiceFrom+` WHERE i.id = $1 FOR UPDATE OF i`, invoiceID))
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("reinstate: lock invoice: %w", err)
	}
	if err := loadInvoiceChildrenFrom(ctx, tx, row); err != nil {
		return false, "", err
	}
	if ticketSaleID.Valid {
		// Read after the factura's lock, and while it is still annulled, so
		// what it reports is a factura OTHER than this one.
		if err := tx.QueryRowContext(ctx, `SELECT `+saleHasLiveFactura("$1"), ticketSaleID.String).Scan(&facts.SaleHasLiveFactura); err != nil {
			return false, "", fmt.Errorf("reinstate: read live factura of sale: %w", err)
		}
	}

	var annulmentID, verdict sql.NullString
	var noticeState string
	err = tx.QueryRowContext(ctx, `
		SELECT id, notice_state, check_verdict FROM invoicing_annulments
		WHERE invoice_id = $1 AND reinstated_at IS NULL FOR UPDATE
	`, invoiceID).Scan(&annulmentID, &noticeState, &verdict)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, "", fmt.Errorf("reinstate: lock standing annulment: %w", err)
	}
	facts.AnnulmentID = annulmentID.String
	facts.AnnulmentVerdict = invoicing.AnnulmentUnconfirmed
	if verdict.Valid {
		facts.AnnulmentVerdict = invoicing.AnnulmentVerdict(verdict.String)
	}
	if err := tx.QueryRowContext(ctx, `SELECT `+annulmentAtMismatchSQL+` FROM invoicing_invoices i WHERE i.id = $1`, invoiceID).Scan(&facts.AnnulmentAtMismatch); err != nil {
		return false, "", fmt.Errorf("reinstate: read annulment mismatch: %w", err)
	}

	decision, err := decide(row, facts)
	if err != nil {
		return true, "", err
	}

	var authXML []byte
	if rein.Authorization != nil {
		authXML = rein.Authorization.XML
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			status = 'authorized',
			annulled_by = NULL,
			annulled_at = NULL,
			authorization_xml = COALESCE($2, authorization_xml),
			next_attempt_at = CASE WHEN kind = 'sale' AND delivered_at IS NULL THEN $3::timestamptz ELSE NULL END,
			attention_since = NULL,
			updated_at = NOW()
		WHERE id = $1 AND status = 'annulled'
	`, invoiceID, authXML, rein.ReinstatedAt)
	if err != nil {
		return true, "", fmt.Errorf("reinstate invoice: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return true, "", fmt.Errorf("reinstate invoice: %d rows written, %v", n, err)
	}
	if a := rein.Authorization; a != nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE invoicing_invoices_ec SET authorization_number = $2, authorization_date = $3
			WHERE invoice_id = $1
		`, invoiceID, a.Number, a.Date); err != nil {
			return true, "", fmt.Errorf("reinstate: store authorization: %w", err)
		}
	}

	if annulmentID.Valid {
		reinstatementNotice := invoicing.ReinstatementNoticeNotOwed
		if noticeState == string(invoicing.AnnulmentNoticeSent) && decision.CreditNote == nil {
			reinstatementNotice = invoicing.ReinstatementNoticeOwed
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE invoicing_annulments SET
				reinstated_at = $2,
				reinstated_by = $3,
				reinstate_note = NULLIF($4, ''),
				next_ask_at = NULL,
				check_leased_until = NULL,
				notice_state = CASE WHEN notice_state = 'owed' THEN 'dropped' ELSE notice_state END,
				notice_next_attempt_at = NULL,
				notice_leased_until = NULL,
				reinstatement_notice_state = $5
			WHERE id = $1
		`, annulmentID.String, rein.ReinstatedAt, rein.ReinstatedBy, rein.Note, reinstatementNotice); err != nil {
			return true, "", fmt.Errorf("reinstate: close annulment: %w", err)
		}
	}

	if decision.CreditNote != nil {
		if creditNoteID, err = r.OweInvoice(ctx, tx, *decision.CreditNote); err != nil {
			return true, "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return true, "", fmt.Errorf("commit reinstate: %w", err)
	}
	return true, creditNoteID, nil
}

// The reinstatement notice's queue (#749, migration 134), shaped as the
// Annulment Notice's (annulment_notice.go): an owed notice lives on the
// reinstated annulment, and is claimable while its factura is still
// authorized; a factura annulled again before it went owes the buyer the
// new annulment's notice instead, and this one is never sent.

// ReinstatementNoticeClaim is one owed reinstatement notice a tick has
// claimed: the annulment, the sends tried so far, and its factura read in
// full.
type ReinstatementNoticeClaim struct {
	AnnulmentID string
	Attempts    int
	Invoice     *InvoiceRow
}

// ClaimDueReinstatementNotice takes ONE owed reinstatement notice due at now
// and hides it from every other tick until leaseUntil, or returns nil when
// none is. SKIP LOCKED and the lease keep two ticks from sending one twice.
func (r *Repository) ClaimDueReinstatementNotice(ctx context.Context, now, leaseUntil time.Time) (*ReinstatementNoticeClaim, error) {
	var claim ReinstatementNoticeClaim
	var invoiceID string
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE invoicing_annulments
		SET reinstatement_notice_leased_until = $2
		WHERE id = (
			SELECT a.id
			FROM invoicing_annulments a
			JOIN invoicing_invoices i ON i.id = a.invoice_id
			WHERE a.reinstatement_notice_state = 'owed'
			  AND (a.reinstatement_notice_next_attempt_at IS NULL OR a.reinstatement_notice_next_attempt_at <= $1)
			  AND (a.reinstatement_notice_leased_until IS NULL OR a.reinstatement_notice_leased_until <= $1)
			  AND i.status = 'authorized'
			ORDER BY COALESCE(a.reinstatement_notice_next_attempt_at, a.reinstated_at), a.id
			FOR UPDATE OF a SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, invoice_id, reinstatement_notice_attempts
	`, now, leaseUntil).Scan(&claim.AnnulmentID, &invoiceID, &claim.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim due reinstatement notice: %w", err)
	}
	row, err := r.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("claim due reinstatement notice: invoice %s not found", invoiceID)
	}
	claim.Invoice = row
	return &claim, nil
}

// MarkReinstatementNoticeSent records that the sender took the notice, and
// frees the claim. Guarded on it still being owed; it reports whether this
// call wrote it.
func (r *Repository) MarkReinstatementNoticeSent(ctx context.Context, annulmentID string, at time.Time) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_annulments SET
			reinstatement_notice_state = 'sent',
			reinstatement_notice_sent_at = $2,
			reinstatement_notice_next_attempt_at = NULL,
			reinstatement_notice_leased_until = NULL,
			reinstatement_notice_attempts = reinstatement_notice_attempts + 1
		WHERE id = $1 AND reinstatement_notice_state = 'owed'
	`, annulmentID, at)
	if err != nil {
		return false, fmt.Errorf("mark reinstatement notice sent: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark reinstatement notice sent: %w", err)
	}
	return n == 1, nil
}

// RescheduleReinstatementNotice records a refused send: the notice stays
// owed, due again at next, and the claim is freed.
func (r *Repository) RescheduleReinstatementNotice(ctx context.Context, annulmentID string, next time.Time) error {
	if _, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_annulments SET
			reinstatement_notice_next_attempt_at = $2,
			reinstatement_notice_leased_until = NULL,
			reinstatement_notice_attempts = reinstatement_notice_attempts + 1
		WHERE id = $1 AND reinstatement_notice_state = 'owed'
	`, annulmentID, next); err != nil {
		return fmt.Errorf("reschedule reinstatement notice: %w", err)
	}
	return nil
}
