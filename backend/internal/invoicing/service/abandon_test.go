package service

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// What Abandon refuses, and what Mark annulled gives up to it (#578, parent
// #575, ADR 0068). One row per refusal reason, read by the code answered,
// in the order the guard decides: the shared floor first (a finished
// document answers what it is, whatever the authority once said about its
// number), then the state a refusal must have left it in, then the
// authority's refusal by number, then the fresh Check the act rests on.
//
// KIND IS NOT A REFUSAL HERE, and the table proves it by carrying all three
// kinds through the same row of facts: the number is dead whoever typed the
// document, so a manual Tax Invoice refused by number is as abandonable as
// a Sale Invoice, and a hand-typed document is never left in a permanently
// misleading state. That is also why the state gate admits `rejected` and
// `not_authorized` beside `needs_attention`: a manual document's refusals
// are recorded as those two and never parked.
//
// The transaction, the guarded write and what the Drainer does with a late
// answer are the integration harness's to prove; here only the answer, by
// error code.

// abandonAt is the instant every case is judged at, and the ledger below is
// written relative to it: the guard's freshness question is "how long ago,
// and was anything done since", never "what o'clock is it".
var abandonAt = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// freshCheckLedger is the ledger an operator leaves by pressing Check status
// and then Abandon: a Submit the authority refused by number, then a query
// answering that it has no record of the clave, a minute ago.
func freshCheckLedger() []invoicing.Attempt {
	return []invoicing.Attempt{
		{Operation: invoicing.AttemptSubmit, Outcome: string(invoicing.OutcomeRejected), StartedAt: abandonAt.Add(-48 * time.Hour)},
		{Operation: invoicing.AttemptQuery, Outcome: string(invoicing.OutcomeUnknown), StartedAt: abandonAt.Add(-time.Minute)},
	}
}

// abandonable is a document exactly as the two production facturas stand:
// a Sale Invoice parked needs_attention, signed, carrying the SRI's 45 as
// an error, with a fresh Check behind it.
func abandonable() invoicing.Invoice {
	return numberRefusedInvoice(authorityError(invoicing.AuthorityMessageSequenceRegistered))
}

func TestAbandonRefusal(t *testing.T) {
	cases := []struct {
		name     string
		inv      func() invoicing.Invoice
		attempts []invoicing.Attempt
		want     string
	}{
		// ---- the act, on each kind the platform issues -------------------
		{"a sale invoice refused by number, checked a minute ago", abandonable, freshCheckLedger(), ""},
		{"a manual tax invoice refused by number, rejected at recepción", func() invoicing.Invoice {
			i := abandonable()
			i.Kind = invoicing.DocumentKindManual
			i.Status = invoicing.InvoiceStatusRejected
			return i
		}, freshCheckLedger(), ""},
		{"a manual tax invoice refused by number, not authorized", func() invoicing.Invoice {
			i := abandonable()
			i.Kind = invoicing.DocumentKindManual
			i.Status = invoicing.InvoiceStatusNotAuthorized
			return i
		}, freshCheckLedger(), ""},
		{"a credit note refused by number", func() invoicing.Invoice {
			i := abandonable()
			i.Kind = invoicing.DocumentKindCreditNote
			return i
		}, freshCheckLedger(), ""},

		// ---- the shared floor, answered first ----------------------------
		{"authorized", func() invoicing.Invoice {
			i := abandonable()
			i.Status = invoicing.InvoiceStatusAuthorized
			return i
		}, freshCheckLedger(), "INVOICE_ALREADY_AUTHORIZED"},
		{"already abandoned", func() invoicing.Invoice {
			i := abandonable()
			i.Status = invoicing.InvoiceStatusAbandoned
			return i
		}, freshCheckLedger(), "INVOICE_ABANDONED"},
		{"annulled", func() invoicing.Invoice {
			i := abandonable()
			i.Status = invoicing.InvoiceStatusAnnulled
			return i
		}, freshCheckLedger(), "INVOICE_ANNULLED"},
		{"withdrawn", func() invoicing.Invoice {
			i := abandonable()
			i.Status = invoicing.InvoiceStatusWithdrawn
			return i
		}, freshCheckLedger(), "INVOICE_WITHDRAWN"},
		{"owed and unsigned", func() invoicing.Invoice {
			i := abandonable()
			i.Status = invoicing.InvoiceStatusOwed
			i.SignedXML = nil
			return i
		}, freshCheckLedger(), "INVOICE_NOT_ISSUED"},

		// ---- the state a refusal must have left it in --------------------
		{"pending: still with the authority, and checked, not given up on", func() invoicing.Invoice {
			i := abandonable()
			i.Status = invoicing.InvoiceStatusPending
			return i
		}, freshCheckLedger(), "INVOICE_NOT_ABANDONABLE"},

		// ---- the authority's refusal by number ---------------------------
		{"refused for its content, which has a remedy", func() invoicing.Invoice {
			return numberRefusedInvoice(authorityError("35"))
		}, freshCheckLedger(), "INVOICE_NOT_REFUSED_BY_NUMBER"},
		{"45 as an advertencia is not the refusal", func() invoicing.Invoice {
			return numberRefusedInvoice(invoicing.AuthorityMessage{
				Identifier: invoicing.AuthorityMessageSequenceRegistered, Type: invoicing.AuthorityMessageTypeWarning,
			})
		}, freshCheckLedger(), "INVOICE_NOT_REFUSED_BY_NUMBER"},
		{"the authority said nothing at all", func() invoicing.Invoice {
			return numberRefusedInvoice()
		}, freshCheckLedger(), "INVOICE_NOT_REFUSED_BY_NUMBER"},

		// ---- the fresh Check the act rests on -----------------------------
		{"no ledger at all", abandonable, nil, "INVOICE_CHECK_NOT_FRESH"},
		{"never checked, only submitted", abandonable, []invoicing.Attempt{
			{Operation: invoicing.AttemptSubmit, Outcome: string(invoicing.OutcomeRejected), StartedAt: abandonAt.Add(-time.Minute)},
		}, "INVOICE_CHECK_NOT_FRESH"},
		{"checked, then resent: the send is what the ledger ends on", abandonable, []invoicing.Attempt{
			{Operation: invoicing.AttemptQuery, Outcome: string(invoicing.OutcomeUnknown), StartedAt: abandonAt.Add(-2 * time.Minute)},
			{Operation: invoicing.AttemptSubmit, Outcome: string(invoicing.OutcomeRejected), StartedAt: abandonAt.Add(-time.Minute)},
		}, "INVOICE_CHECK_NOT_FRESH"},
		{"checked yesterday", abandonable, []invoicing.Attempt{
			{Operation: invoicing.AttemptQuery, Outcome: string(invoicing.OutcomeUnknown), StartedAt: abandonAt.Add(-24 * time.Hour)},
		}, "INVOICE_CHECK_NOT_FRESH"},
		{"checked just past the window", abandonable, []invoicing.Attempt{
			{Operation: invoicing.AttemptQuery, Outcome: string(invoicing.OutcomeUnknown), StartedAt: abandonAt.Add(-AbandonCheckFreshness - time.Second)},
		}, "INVOICE_CHECK_NOT_FRESH"},
		{"checked at the edge of the window", abandonable, []invoicing.Attempt{
			{Operation: invoicing.AttemptQuery, Outcome: string(invoicing.OutcomeUnknown), StartedAt: abandonAt.Add(-AbandonCheckFreshness)},
		}, ""},
		{"the check could not be made: no answer, no evidence", abandonable, []invoicing.Attempt{
			{Operation: invoicing.AttemptQuery, Outcome: invoicing.AttemptOutcomeError, StartedAt: abandonAt.Add(-time.Minute)},
		}, "INVOICE_CHECK_NOT_FRESH"},
		{"a check the authority answered was still processing", abandonable, []invoicing.Attempt{
			{Operation: invoicing.AttemptQuery, Outcome: string(invoicing.OutcomeReceived), StartedAt: abandonAt.Add(-time.Minute)},
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := tc.inv()
			if got := code(abandonRefusal(&inv, tc.attempts, abandonAt)); got != tc.want {
				t.Fatalf("abandonRefusal = %q; want %q", got, tc.want)
			}
		})
	}
}

// TestMarkAnnulledIsRefusedWhereAbandonQualifies is the narrowing (#578, ADR
// 0068), and the point of the whole feature: Mark annulled records an
// annulment performed BY HAND at the authority's portal, and for a number
// the authority never took there is nothing there to have been annulled.
// The operator's only escape from production's 25 and 26 was that
// falsehood; it is now refused, and the refusal names the act that is true.
//
// The fresh Check is deliberately NOT part of this: a document that
// qualifies for Abandon but has not been checked must be checked, never
// annulled instead — otherwise the falsehood is one stale minute away.
//
// QUALIFIES is the whole of the narrowing, and the pending row is what
// proves it means something. The two guards ask one question
// (abandonableState), so every document keeps exactly one act: a code-45
// document the authority is still holding is not abandonable, so Mark
// annulled stays open on it. Refusing there as well would hand the
// operator a document with no escape at all — this epic's own dead end,
// rebuilt one state to the left.
func TestMarkAnnulledIsRefusedWhereAbandonQualifies(t *testing.T) {
	cases := []struct {
		name string
		inv  func() invoicing.Invoice
		want string
	}{
		{"parked and refused by number", abandonable, "INVOICE_ABANDON_INSTEAD"},
		{"pending and refused by number: Abandon will not take it, so Mark annulled must", func() invoicing.Invoice {
			i := abandonable()
			i.Status = invoicing.InvoiceStatusPending
			return i
		}, ""},
		{"parked for another refusal: the portal act is still the operator's to record", func() invoicing.Invoice {
			return numberRefusedInvoice(authorityError("35"))
		}, ""},
		{"already abandoned", func() invoicing.Invoice {
			i := abandonable()
			i.Status = invoicing.InvoiceStatusAbandoned
			return i
		}, "INVOICE_NOT_ANNULLABLE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := tc.inv()
			if got := code(annullable(&inv)); got != tc.want {
				t.Fatalf("annullable = %q; want %q", got, tc.want)
			}
		})
	}
}
