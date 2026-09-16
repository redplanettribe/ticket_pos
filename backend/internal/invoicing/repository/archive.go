package repository

import (
	"context"
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// The Tax Document Archive's reads (#630, spec #629). WHICH documents a
// period holds is the service's decision (service.TaxDocumentArchiveIncluded
// and service.TaxDocumentArchiveUnsettled); this file only knows how to ask
// for the documents a criteria names, so the archive and its summary (#631)
// are two readers of one WHERE clause and cannot disagree about a document.

// ArchiveCriteria names a set of documents by the facts the archive is
// defined over: whose they are, where the authority holds them, what it made
// of them, and the Emission Date range they were emitted in, both ends
// included. Every field is required: a criteria is always built whole by the
// service, never narrowed piecemeal by a request.
type ArchiveCriteria struct {
	IssuerID    string
	Environment invoicing.Environment
	Statuses    []invoicing.InvoiceStatus
	// From and To are "YYYY-MM-DD", compared against issued_on as calendar
	// days. A document with no Emission Date (an owed one) fails both.
	From string
	To   string
}

// ArchiveDocument is one document as the archive writes it: its kind (a
// factura or a Credit Note), its Emission Date, its clave de acceso and the
// signed XML exactly as stored.
type ArchiveDocument struct {
	Kind      invoicing.DocumentKind
	IssuedOn  string
	AccessKey string
	SignedXML []byte
}

// ArchiveCounts is how many documents a criteria names, by kind.
type ArchiveCounts struct {
	Manual      int
	Sale        int
	CreditNotes int
}

// Facturas is the manual and Sale Invoices together: both are facturas to
// the authority (codDoc 01) and to the accountant.
func (c ArchiveCounts) Facturas() int { return c.Manual + c.Sale }

// Total is every document counted.
func (c ArchiveCounts) Total() int { return c.Manual + c.Sale + c.CreditNotes }

// archiveWhere is the ONE clause both reads below are built on. The Ecuador
// detail row is joined INNER: a document without one has no clave and no
// signed XML, and is never an archive document.
func archiveWhere(c ArchiveCriteria) (string, []any) {
	statuses := make([]string, len(c.Statuses))
	for i, status := range c.Statuses {
		statuses[i] = string(status)
	}
	return `
		FROM invoicing_invoices i
		JOIN invoicing_invoices_ec e ON e.invoice_id = i.id
		WHERE i.issuer_id = $1
		  AND i.environment = $2
		  AND i.status = ANY($3)
		  AND i.issued_on >= $4::date
		  AND i.issued_on <= $5::date`,
		[]any{c.IssuerID, string(c.Environment), statuses, c.From, c.To}
}

// StreamArchiveDocuments reads the documents a criteria names ONE ROW AT A
// TIME, in Emission Date then clave order, handing each to yield before the
// next is read. Nothing is collected: a period of any length holds one
// document's XML in memory at a time. An error from yield stops the read and
// is returned as it is.
func (r *Repository) StreamArchiveDocuments(ctx context.Context, c ArchiveCriteria, yield func(ArchiveDocument) error) error {
	where, args := archiveWhere(c)
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT i.kind, to_char(i.issued_on, 'YYYY-MM-DD'), e.access_key, i.signed_xml`+where+`
		ORDER BY i.issued_on, e.access_key`, args...)
	if err != nil {
		return fmt.Errorf("stream archive documents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var doc ArchiveDocument
		var kind string
		if err := rows.Scan(&kind, &doc.IssuedOn, &doc.AccessKey, &doc.SignedXML); err != nil {
			return fmt.Errorf("scan archive document: %w", err)
		}
		doc.Kind = invoicing.DocumentKind(kind)
		if err := yield(doc); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("stream archive documents: %w", err)
	}
	return nil
}

// CountArchiveDocuments counts the documents a criteria names, by kind.
func (r *Repository) CountArchiveDocuments(ctx context.Context, c ArchiveCriteria) (ArchiveCounts, error) {
	where, args := archiveWhere(c)
	var counts ArchiveCounts
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE i.kind = 'manual'),
			COUNT(*) FILTER (WHERE i.kind = 'sale'),
			COUNT(*) FILTER (WHERE i.kind = 'credit_note')`+where, args...,
	).Scan(&counts.Manual, &counts.Sale, &counts.CreditNotes); err != nil {
		return ArchiveCounts{}, fmt.Errorf("count archive documents: %w", err)
	}
	return counts, nil
}
