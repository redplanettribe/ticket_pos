package service

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
)

// Reinstate's pure halves (#749, ADR 0078 section 5): which refusal the
// gate answers, and the one rule for the Credit Note it owes. The
// transaction, the locks, the Drainer's wait and the buyer's mail are the
// integration harness's to prove.

func TestReinstateRefusalAnswersByStateThenWindowThenVerdict(t *testing.T) {
	issuedOn := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	inside := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	after := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	row := func(kind invoicing.DocumentKind, status invoicing.InvoiceStatus, check *invoicing.AnnulmentCheck) *repository.InvoiceRow {
		inv := annulledFactura()
		inv.Kind = kind
		inv.Status = status
		inv.IssuedOn = issuedOn
		r := &repository.InvoiceRow{Invoice: inv}
		if status == invoicing.InvoiceStatusAnnulled {
			r.Annulments = []invoicing.Annulment{{ID: "a", Check: check}}
		}
		return r
	}
	at := func(v invoicing.AnnulmentVerdict) *invoicing.AnnulmentCheck {
		return &invoicing.AnnulmentCheck{Verdict: v}
	}
	cases := []struct {
		name string
		row  *repository.InvoiceRow
		now  time.Time
		want string
	}{
		{"mismatch after the window", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAnnulled, at(invoicing.AnnulmentMismatch)), after, ""},
		{"a manual tax invoice at mismatch", row(invoicing.DocumentKindManual, invoicing.InvoiceStatusAnnulled, at(invoicing.AnnulmentMismatch)), after, ""},
		{"inside the window", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAnnulled, at(invoicing.AnnulmentMismatch)), inside, "INVOICE_REINSTATE_WINDOW_OPEN"},
		{"checking", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAnnulled, at(invoicing.AnnulmentChecking)), after, "INVOICE_REINSTATE_NOT_MISMATCH"},
		{"confirmed", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAnnulled, at(invoicing.AnnulmentConfirmed)), after, "INVOICE_REINSTATE_NOT_MISMATCH"},
		{"unconfirmed", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAnnulled, at(invoicing.AnnulmentUnconfirmed)), after, "INVOICE_REINSTATE_NOT_MISMATCH"},
		{"recorded before the check", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAnnulled, nil), after, "INVOICE_REINSTATE_NOT_MISMATCH"},
		{"authorized", row(invoicing.DocumentKindSale, invoicing.InvoiceStatusAuthorized, nil), after, "INVOICE_NOT_REINSTATABLE"},
		{"a credit note", row(invoicing.DocumentKindCreditNote, invoicing.InvoiceStatusAnnulled, at(invoicing.AnnulmentMismatch)), after, "INVOICE_NOT_REINSTATABLE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := code(reinstateRefusal(tc.row, tc.now)); got != tc.want {
				t.Fatalf("refusal = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestReinstateOwesACreditNoteByOneRule(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	sale := annulledFactura()
	manual := annulledFactura()
	manual.Kind = invoicing.DocumentKindManual
	credited := annulledFactura()
	credited.CreditedByInvoiceID = "note"
	cases := []struct {
		name    string
		factura invoicing.Invoice
		facts   repository.ReinstateFacts
		reason  string // "" for no Credit Note
	}{
		{"a reversed sale", sale, repository.ReinstateFacts{SaleStatus: "reversed", ReversalRoute: "platform"}, "platform"},
		{"a reversed sale with a successor still standing", sale, repository.ReinstateFacts{SaleStatus: "reversed", ReversalRoute: "customer", SaleHasLiveFactura: true}, "customer"},
		{"a live successor", sale, repository.ReinstateFacts{SaleStatus: "active", SaleHasLiveFactura: true}, invoicing.CreditNoteReasonReissue},
		{"nothing after it", sale, repository.ReinstateFacts{SaleStatus: "active"}, ""},
		{"a manual tax invoice", manual, repository.ReinstateFacts{}, ""},
		{"already credited by a live credit note", credited, repository.ReinstateFacts{SaleStatus: "reversed", ReversalRoute: "platform"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			note := reinstateCreditNote(&tc.factura, tc.facts, now)
			if tc.reason == "" {
				if note != nil {
					t.Fatalf("credit note = %+v; want none", note)
				}
				return
			}
			if note == nil {
				t.Fatalf("no credit note; want one with reason %q", tc.reason)
			}
			if note.Kind != invoicing.DocumentKindCreditNote || note.Status != invoicing.InvoiceStatusOwed ||
				note.CreditsInvoiceID != tc.factura.ID || note.CreditNoteReason != tc.reason ||
				note.TotalCents != tc.factura.TotalCents || len(note.Lines) != len(tc.factura.Lines) ||
				note.NextAttemptAt == nil || !note.NextAttemptAt.Equal(now) {
				t.Fatalf("credit note = %+v; want an owed copy of the factura, due now, reason %q", note, tc.reason)
			}
		})
	}
}
