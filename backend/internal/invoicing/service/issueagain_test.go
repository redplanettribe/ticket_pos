package service

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
)

// Issue again's pure halves (#580, parent #575, ADR 0068): which refusal
// wins, and what the replacement is a copy of. The transaction, the locks
// and the Drainer's order are the integration harness's to prove — the
// division of labour the reissue's guard test states, honoured here because
// it is the same division and stating it twice differently would be the
// beginning of two of everything.

// abandonedFactura is the case the feature was built for: production's
// 001-001-000000025, signed and sent, refused by number, given up on.
func abandonedFactura() invoicing.Invoice {
	inv := authorizedFactura()
	inv.Status = invoicing.InvoiceStatusAbandoned
	abandonedAt := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	inv.AbandonedBy = "operator@example.com"
	inv.AbandonedAt = &abandonedAt
	inv.AbandonNote = "not registered at the portal, confirmed by phone"
	return inv
}

// TestIssueAgainRefusalAnswersByKindThenStateThenSale: one row per refusal
// reason, keyed on the code the operator's surface reads. Kind first, then
// the state, then the facts about the Sale — the order the guard is written
// in, and the order a reader should think in.
func TestIssueAgainRefusalAnswersByKindThenStateThenSale(t *testing.T) {
	stands := repository.IssueAgainSaleFacts{SaleStatus: "active"}
	dead := func(status invoicing.InvoiceStatus) func() invoicing.Invoice {
		return func() invoicing.Invoice {
			i := abandonedFactura()
			i.Status = status
			return i
		}
	}
	cases := []struct {
		name  string
		inv   func() invoicing.Invoice
		facts repository.IssueAgainSaleFacts
		want  string
	}{
		{"abandoned factura", abandonedFactura, stands, ""},
		// The absorbed #480 case, and the whole reason the act is gated on
		// "terminally dead" rather than on "abandoned": a Sale whose factura
		// the operator annulled at the portal stops being a dead end.
		{"annulled factura", dead(invoicing.InvoiceStatusAnnulled), stands, ""},

		// Kind, before anything else is asked.
		{"manual", func() invoicing.Invoice {
			i := abandonedFactura()
			i.Kind = invoicing.DocumentKindManual
			return i
		}, stands, "INVOICE_MANUAL_NOT_ISSUABLE_AGAIN"},
		{"credit note", func() invoicing.Invoice {
			i := abandonedFactura()
			i.Kind = invoicing.DocumentKindCreditNote
			return i
		}, stands, "CREDIT_NOTE_NOT_ISSUABLE_AGAIN"},
		// A manual document that is dead in the right way is still refused
		// by its kind: the standing rule is not reversed by the state.
		{"manual outranks the state", func() invoicing.Invoice {
			i := abandonedFactura()
			i.Kind = invoicing.DocumentKindManual
			i.Status = invoicing.InvoiceStatusAuthorized
			return i
		}, stands, "INVOICE_MANUAL_NOT_ISSUABLE_AGAIN"},

		// Then the state. Every status but the two deaths this act reaches.
		{"owed", dead(invoicing.InvoiceStatusOwed), stands, "INVOICE_NOT_TERMINALLY_DEAD"},
		{"pending", dead(invoicing.InvoiceStatusPending), stands, "INVOICE_NOT_TERMINALLY_DEAD"},
		{"authorized", dead(invoicing.InvoiceStatusAuthorized), stands, "INVOICE_NOT_TERMINALLY_DEAD"},
		{"not authorized", dead(invoicing.InvoiceStatusNotAuthorized), stands, "INVOICE_NOT_TERMINALLY_DEAD"},
		{"rejected", dead(invoicing.InvoiceStatusRejected), stands, "INVOICE_NOT_TERMINALLY_DEAD"},
		{"needs attention", dead(invoicing.InvoiceStatusNeedsAttention), stands, "INVOICE_NOT_TERMINALLY_DEAD"},
		// Withdrawn is the third death and is NOT a way in: the document was
		// never sent because its Sale was reversed, or because the Credit
		// Note it followed died — and neither Sale is owed a fresh factura.
		{"withdrawn", dead(invoicing.InvoiceStatusWithdrawn), stands, "INVOICE_NOT_TERMINALLY_DEAD"},

		// Then the Sale. A reversed Sale is answered before the replacement:
		// income that no longer stands is never declared, whatever else is
		// true of the chain.
		{"reversed sale", abandonedFactura, repository.IssueAgainSaleFacts{SaleStatus: "reversed"}, "INVOICE_SALE_REVERSED"},
		{"reversed sale outranks a live replacement", func() invoicing.Invoice {
			i := abandonedFactura()
			i.SupersededByInvoiceID = "replacement"
			return i
		}, repository.IssueAgainSaleFacts{SaleStatus: "reversed"}, "INVOICE_SALE_REVERSED"},
		{"live replacement", func() invoicing.Invoice {
			i := abandonedFactura()
			i.SupersededByInvoiceID = "replacement"
			return i
		}, stands, "INVOICE_ALREADY_REPLACED"},

		// The Sale's own factura, which is the invariant the document's
		// successor only approximates. This row is the two-hop chain: the
		// dead document's successor died too, so it reads unreplaced, while
		// a third document stands for the Sale. Owing another here is what
		// would leave the buyer with two valid facturas.
		{"the sale already has a factura, though this document's successor died", abandonedFactura,
			repository.IssueAgainSaleFacts{SaleStatus: "active", SaleHasLiveFactura: true}, "INVOICE_ALREADY_REPLACED"},
		{"reversed sale outranks the sale's factura", abandonedFactura,
			repository.IssueAgainSaleFacts{SaleStatus: "reversed", SaleHasLiveFactura: true}, "INVOICE_SALE_REVERSED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := tc.inv()
			if got := code(issueAgainRefusal(&inv, tc.facts)); got != tc.want {
				t.Fatalf("refusal = %q; want %q", got, tc.want)
			}
		})
	}
}

// TestTheReplacementCopiesTheDeadDocumentWholeAndCreditsNothing: what the
// operator is promised — the original lines, amounts and Recipient — plus
// what makes it an ordinary owed document rather than a special one.
func TestTheReplacementCopiesTheDeadDocumentWholeAndCreditsNothing(t *testing.T) {
	dead := abandonedFactura()
	now := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	in := IssueAgainInput{IssuedAgainBy: "operator@example.com", Note: "the SRI refuses 25; issued again under a fresh number"}

	replacement := replacementSaleInvoiceOf(&dead, in, now)

	if replacement.Kind != invoicing.DocumentKindSale || replacement.Status != invoicing.InvoiceStatusOwed || replacement.ID != "" || replacement.Signed() {
		t.Fatalf("replacement = %s %s id %q signed %v; want a fresh owed Sale Invoice", replacement.Kind, replacement.Status, replacement.ID, replacement.Signed())
	}
	// Nothing that needs an Issuer is set: the Drainer allocates a fresh
	// secuencial on a later round, which is what keeps the abandoned number
	// consumed and never handed out again.
	if replacement.IssuerID != "" || replacement.Environment != "" || replacement.Issuer != nil || !replacement.IssuedOn.IsZero() || replacement.IssuedBy != "" {
		t.Fatalf("replacement carries issue-time facts: %+v; want none until the Drainer signs it", replacement)
	}
	// The Recipient is the dead document's, verbatim and including the
	// email: Issue again corrects nothing, unlike a reissue.
	if replacement.Recipient != dead.Recipient {
		t.Fatalf("recipient = %+v; want the dead document's %+v, unchanged", replacement.Recipient, dead.Recipient)
	}
	if replacement.TicketSaleID != "sale" || replacement.IVARate != dead.IVARate || replacement.Currency != "USD" ||
		replacement.SubtotalCents != 970 || replacement.DiscountCents != dead.DiscountCents || replacement.IVACents != 145 ||
		replacement.TotalCents != 1115 || replacement.PaymentMethod != "19" {
		t.Fatalf("money = %+v; want the dead document's", replacement)
	}
	if len(replacement.Lines) != 1 || replacement.Lines[0] != dead.Lines[0] {
		t.Fatalf("lines = %+v; want the dead document's", replacement.Lines)
	}
	// The chain: ADR 0061's link and trail, reused.
	if replacement.SupersedesInvoiceID != "factura" || replacement.ReissuedBy != "operator@example.com" ||
		replacement.ReissuedAt == nil || !replacement.ReissuedAt.Equal(now) || replacement.ReissueNote != in.Note {
		t.Fatalf("trail = supersedes %q by %q at %v note %q", replacement.SupersedesInvoiceID, replacement.ReissuedBy, replacement.ReissuedAt, replacement.ReissueNote)
	}
	// NOTHING IS CREDITED. This is the difference from a reissue that lets
	// Issue again reach a Sale the reissue answers INVOICE_ALREADY_CREDITED
	// on (#579): there is no second Credit Note, so the double-credit ADR
	// 0061 forbids never arises.
	if replacement.CreditsInvoiceID != "" || replacement.CreditNoteReason != "" {
		t.Fatalf("the replacement credits something: %+v; want a factura with no Credit Note at all", replacement)
	}
	// Due at once, and gated by nothing: unlike a reissue's corrected
	// factura there is no Credit Note for it to wait behind.
	if replacement.NextAttemptAt == nil || !replacement.NextAttemptAt.Equal(now) {
		t.Fatalf("next_attempt_at = %v; want due at once", replacement.NextAttemptAt)
	}
	// The abandonment's own trail belongs to the dead row and stays there.
	if replacement.AbandonedBy != "" || replacement.AbandonedAt != nil || replacement.AbandonNote != "" {
		t.Fatalf("the replacement carries the abandonment's trail: %+v", replacement)
	}
}
