package service

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
)

// The acknowledgement's gate (#750, ADR 0078): an Unrecorded Annulment is
// acknowledged only on an authorized factura that carries one, once, and
// only where Mark annulled is refused on it. The sweep itself, the queue
// and the guarded write are the integration harness's to prove.
func TestAcknowledgeRefusalAdmitsOnlyWhereMarkAnnulledIsRefused(t *testing.T) {
	found := time.Date(2026, 11, 8, 0, 5, 0, 0, time.UTC)
	acknowledged := found.Add(time.Hour)
	row := func(kind invoicing.DocumentKind, status invoicing.InvoiceStatus, flag *invoicing.UnrecordedAnnulment, creditNote, successor string) *repository.InvoiceRow {
		inv := annulledFactura()
		inv.Kind = kind
		inv.Status = status
		inv.UnrecordedAnnulment = flag
		inv.SupersededByInvoiceID = successor
		return &repository.InvoiceRow{Invoice: inv, StandingCreditNoteID: creditNote}
	}
	flagged := func() *invoicing.UnrecordedAnnulment { return &invoicing.UnrecordedAnnulment{FoundAt: found} }
	cases := []struct {
		name string
		row  *repository.InvoiceRow
		want string
	}{
		{"credited by a live Credit Note", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAuthorized, flagged(), "cn", ""), ""},
		{"a manual tax invoice credited", row(invoicing.DocumentKindManual, invoicing.InvoiceStatusAuthorized, flagged(), "cn", ""), ""},
		{"superseded by a live Reissue", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAuthorized, flagged(), "cn", "next"), ""},
		{"nothing stands in the way of Mark annulled", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAuthorized, flagged(), "", ""), "INVOICE_ANNUL_INSTEAD"},
		{"acknowledged already", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAuthorized,
			&invoicing.UnrecordedAnnulment{FoundAt: found, AcknowledgedBy: "op@example.com", AcknowledgedAt: &acknowledged}, "cn", ""), "INVOICE_UNRECORDED_ANNULMENT_ACKNOWLEDGED"},
		{"never flagged", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAuthorized, nil, "cn", ""), "INVOICE_NO_UNRECORDED_ANNULMENT"},
		{"annulled", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAnnulled, flagged(), "", ""), "INVOICE_NO_UNRECORDED_ANNULMENT"},
		{"a credit note", row(invoicing.DocumentKindCreditNote, invoicing.InvoiceStatusAuthorized, flagged(), "", ""), "INVOICE_NO_UNRECORDED_ANNULMENT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := code(acknowledgeRefusal(tc.row)); got != tc.want {
				t.Fatalf("refusal = %q; want %q", got, tc.want)
			}
		})
	}
}
