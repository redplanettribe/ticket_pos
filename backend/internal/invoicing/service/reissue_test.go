package service

import (
	"errors"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// The reissue's pure halves (#483, ADR 0061): which refusal wins, and what
// the corrected document is a copy of. The transaction, the locks and the
// Drainer's order are the integration harness's to prove.

func authorizedFactura() invoicing.Invoice {
	return invoicing.Invoice{
		ID:            "factura",
		Kind:          invoicing.DocumentKindSale,
		Country:       invoicing.CountryEcuador,
		Status:        invoicing.InvoiceStatusAuthorized,
		TicketSaleID:  "sale",
		IVARate:       invoicing.SaleInvoiceIVARate,
		Recipient:     invoicing.Recipient{TaxIDType: "cedula", TaxID: "1712345675", LegalName: "Ana Lopez", Email: "old@example.com"},
		Currency:      "USD",
		SubtotalCents: 970,
		IVACents:      145,
		TotalCents:    1115,
		PaymentMethod: "19",
		Lines:         []invoicing.InvoiceLine{{Position: 1, Description: "GA — House Fest", QuantityMillionths: 1_000_000, UnitPriceCents: 1115, IVARate: "15", BaseCents: 970, IVACents: 145}},
		SignedXML:     []byte("<factura/>"),
	}
}

func code(err error) string {
	var domain apperror.DomainError
	if errors.As(err, &domain) {
		return domain.Code()
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestReissueRefusalAnswersByKindThenStateThenSale(t *testing.T) {
	stands := repository.ReissueSaleFacts{SaleStatus: "active", SaleEmail: "now@example.com"}
	cases := []struct {
		name  string
		inv   func() invoicing.Invoice
		facts repository.ReissueSaleFacts
		want  string
	}{
		{"current authorized factura", authorizedFactura, stands, ""},
		{"manual", func() invoicing.Invoice { i := authorizedFactura(); i.Kind = invoicing.DocumentKindManual; return i }, stands, "INVOICE_MANUAL_NOT_REISSUABLE"},
		{"credit note", func() invoicing.Invoice {
			i := authorizedFactura()
			i.Kind = invoicing.DocumentKindCreditNote
			return i
		}, stands, "CREDIT_NOTE_NOT_REISSUABLE"},
		{"owed", func() invoicing.Invoice { i := authorizedFactura(); i.Status = invoicing.InvoiceStatusOwed; return i }, stands, "INVOICE_NOT_AUTHORIZED"},
		{"pending", func() invoicing.Invoice {
			i := authorizedFactura()
			i.Status = invoicing.InvoiceStatusPending
			return i
		}, stands, "INVOICE_NOT_AUTHORIZED"},
		{"needs attention", func() invoicing.Invoice {
			i := authorizedFactura()
			i.Status = invoicing.InvoiceStatusNeedsAttention
			return i
		}, stands, "INVOICE_NOT_AUTHORIZED"},
		{"withdrawn", func() invoicing.Invoice {
			i := authorizedFactura()
			i.Status = invoicing.InvoiceStatusWithdrawn
			return i
		}, stands, "INVOICE_NOT_AUTHORIZED"},
		{"annulled", func() invoicing.Invoice {
			i := authorizedFactura()
			i.Status = invoicing.InvoiceStatusAnnulled
			return i
		}, stands, "INVOICE_NOT_AUTHORIZED"},
		{"reversed sale", authorizedFactura, repository.ReissueSaleFacts{SaleStatus: "reversed", ReissueInFlight: true, CreditedByAuthorized: true}, "INVOICE_SALE_REVERSED"},
		{"reissue in flight outranks superseded", func() invoicing.Invoice { i := authorizedFactura(); i.SupersededByInvoiceID = "corrected"; return i }, repository.ReissueSaleFacts{SaleStatus: "active", ReissueInFlight: true}, "REISSUE_IN_FLIGHT"},
		{"superseded", func() invoicing.Invoice { i := authorizedFactura(); i.SupersededByInvoiceID = "corrected"; return i }, repository.ReissueSaleFacts{SaleStatus: "active", CreditedByAuthorized: true}, "INVOICE_SUPERSEDED"},
		{"credited with no live successor", authorizedFactura, repository.ReissueSaleFacts{SaleStatus: "active", CreditedByAuthorized: true}, "INVOICE_ALREADY_CREDITED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := tc.inv()
			if got := code(reissueRefusal(&inv, tc.facts)); got != tc.want {
				t.Fatalf("refusal = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestCorrectedSaleInvoiceCopiesTheFacturaAndNamesTheCorrectedRecipient(t *testing.T) {
	factura := authorizedFactura()
	now := time.Date(2026, 7, 7, 15, 0, 0, 0, time.UTC)
	in := ReissueInput{
		ReissuedBy: "operator@example.com",
		Recipient:  invoicing.Recipient{TaxIDType: "ruc", TaxID: "1790012346001", LegalName: "ORGANIZACION EJEMPLO S.A.", Address: "Quito", Email: "typed@example.com"},
		Note:       "the buyer wrote in",
	}

	corrected := correctedSaleInvoiceOf(&factura, in, "now@example.com", now)

	if corrected.Kind != invoicing.DocumentKindSale || corrected.Status != invoicing.InvoiceStatusOwed || corrected.ID != "" || corrected.Signed() {
		t.Fatalf("corrected = %s %s id %q signed %v; want a fresh owed Sale Invoice", corrected.Kind, corrected.Status, corrected.ID, corrected.Signed())
	}
	want := invoicing.Recipient{TaxIDType: "ruc", TaxID: "1790012346001", LegalName: "ORGANIZACION EJEMPLO S.A.", Address: "Quito", Email: "now@example.com"}
	if corrected.Recipient != want {
		t.Fatalf("recipient = %+v; want the corrected one with the Sale's email, never the typed one", corrected.Recipient)
	}
	if corrected.SupersedesInvoiceID != "factura" || corrected.ReissuedBy != "operator@example.com" || corrected.ReissuedAt == nil || !corrected.ReissuedAt.Equal(now) || corrected.ReissueNote != "the buyer wrote in" {
		t.Fatalf("trail = supersedes %q by %q at %v note %q", corrected.SupersedesInvoiceID, corrected.ReissuedBy, corrected.ReissuedAt, corrected.ReissueNote)
	}
	if corrected.TicketSaleID != "sale" || corrected.IVARate != factura.IVARate || corrected.TotalCents != 1115 || corrected.SubtotalCents != 970 || corrected.IVACents != 145 || corrected.PaymentMethod != "19" || corrected.Currency != "USD" {
		t.Fatalf("money = %+v; want the factura's", corrected)
	}
	if len(corrected.Lines) != 1 || corrected.Lines[0] != factura.Lines[0] {
		t.Fatalf("lines = %+v; want the factura's", corrected.Lines)
	}
	if corrected.NextAttemptAt == nil || !corrected.NextAttemptAt.Equal(now) {
		t.Fatalf("next_attempt_at = %v; want due at once", corrected.NextAttemptAt)
	}
	if corrected.CreditsInvoiceID != "" || corrected.CreditNoteReason != "" {
		t.Fatalf("a corrected factura credits nothing: %+v", corrected)
	}

	// The reissue's Credit Note names the factura's own Recipient, never
	// the corrected one, and the reissue as its reason.
	note := creditNoteOf(&factura, invoicing.CreditNoteReasonReissue, now)
	if note.Kind != invoicing.DocumentKindCreditNote || note.CreditsInvoiceID != "factura" || note.CreditNoteReason != "reissue" || note.Recipient != factura.Recipient || note.TotalCents != 1115 {
		t.Fatalf("credit note = %+v", note)
	}
}
