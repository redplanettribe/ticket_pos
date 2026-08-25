package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// What Check status, Resend and the Issuer freezes need of storage (#455):
// two existence questions the freezes are decided on, and the one write a
// resend makes besides the outcome — replacing the document on file.

// IssuerHasInvoices reports whether any Tax Invoice exists for the Issuer,
// in either environment. One is enough to freeze the RUC: it is inside every
// clave de acceso already issued.
func (r *Repository) IssuerHasInvoices(ctx context.Context, issuerID string) (bool, error) {
	var exists bool
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM invoicing_invoices WHERE issuer_id = $1)
	`, issuerID).Scan(&exists); err != nil {
		return false, fmt.Errorf("issuer has invoices: %w", err)
	}
	return exists, nil
}

// SequenceExists reports whether a sequence row has started under the
// Issuer's establecimiento and punto de emisión, in any environment and for
// any document type. One is enough to freeze both codes: numbering under
// them must stay continuous.
func (r *Repository) SequenceExists(ctx context.Context, issuerID, estab, ptoEmi string) (bool, error) {
	var exists bool
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM invoicing_sequences_ec
			WHERE issuer_id = $1 AND estab = $2 AND pto_emi = $3
		)
	`, issuerID, estab, ptoEmi).Scan(&exists); err != nil {
		return false, fmt.Errorf("sequence exists: %w", err)
	}
	return exists, nil
}

// ReplaceSignedDocument puts a re-signed document on file in place of the
// one there, together with the Issuer snapshot it was built from, so the two
// always describe the same bytes. Called only once the authority has taken
// the resend (RECIBIDA): the artifact on file is always the one the
// authority holds. The number, the clave and the status are untouched.
func (r *Repository) ReplaceSignedDocument(ctx context.Context, invoiceID string, signedXML []byte, snapshot invoicing.IssuerSnapshot) error {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal issuer snapshot: %w", err)
	}
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			signed_xml = $2,
			issuer_snapshot = $3,
			updated_at = NOW()
		WHERE id = $1
	`, invoiceID, signedXML, encoded)
	if err != nil {
		return fmt.Errorf("replace signed document: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("replace signed document: invoice %s: %w", invoiceID, sql.ErrNoRows)
	}
	return nil
}
