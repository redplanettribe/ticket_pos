package service

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// The two hints on the operator's invoice page (#455, tightened by #516):
// who holds the document, told from the attempts ledger.
//
// The check-status hint says "the SRI has this document — check status
// rather than resending it", and is true only when the authority really
// holds it: some Submit came back received (invoicing.AcknowledgedByAuthority,
// the Submit-only rule of #513) and nothing has been decided since. A query
// answer, whatever it says, is never acknowledgement — the production ledger
// of 001-001-000000001 was [Submit error, Query received ×N] and the SRI had
// never seen the document.
//
// The resend hint says the opposite — "the SRI has no record of this
// document, resend it" — and is true when the last thing the authority said
// was `unknown` (#514) and no Submit was ever received.
//
// Neither hint is shown where its action cannot be taken: both read the
// status, and a document that is authorized, annulled or withdrawn is past
// checking and past resending (actionable refuses both there).
//
// The two are mutually exclusive by construction, and this file proves it
// over every ledger shape below: a resend hint means unacknowledged, and an
// unacknowledged document never carries the check-status hint.

func query(outcome string) invoicing.Attempt {
	return invoicing.Attempt{Operation: invoicing.AttemptQuery, Outcome: outcome}
}

func submit(outcome string) invoicing.Attempt {
	return invoicing.Attempt{Operation: invoicing.AttemptSubmit, Outcome: outcome}
}

var (
	pendingInvoice    = &invoicing.Invoice{Status: invoicing.InvoiceStatusPending}
	parkedInvoice     = &invoicing.Invoice{Status: invoicing.InvoiceStatusNeedsAttention}
	authorizedInvoice = &invoicing.Invoice{Status: invoicing.InvoiceStatusAuthorized}
	rejectedInvoice   = &invoicing.Invoice{Status: invoicing.InvoiceStatusRejected}
	withdrawnInvoice  = &invoicing.Invoice{Status: invoicing.InvoiceStatusWithdrawn}
)

// hintCases are the ledgers both hints are read from, each with what the page
// should say about it. Shared by the two hint tests and by the mutual
// exclusion proof, so that no shape is covered by one and not the others.
var hintCases = []struct {
	name       string
	inv        *invoicing.Invoice
	attempts   []invoicing.Attempt
	checkHint  bool
	resendHint bool
}{
	{
		// The production shape of #513: the Submit died in transport and
		// every query since answers "nothing known under this clave".
		name:       "submit error then query unknown",
		inv:        pendingInvoice,
		attempts:   []invoicing.Attempt{submit(invoicing.AttemptOutcomeError), query(string(invoicing.OutcomeUnknown))},
		checkHint:  false,
		resendHint: true,
	},
	{
		name:       "submit received then query received",
		inv:        pendingInvoice,
		attempts:   []invoicing.Attempt{submit(string(invoicing.OutcomeReceived)), query(string(invoicing.OutcomeReceived))},
		checkHint:  true,
		resendHint: false,
	},
	{
		// The old production shape, before #514 told the two answers apart:
		// a query answer is not acknowledgement, so neither hint shows.
		name:       "submit error then query received",
		inv:        pendingInvoice,
		attempts:   []invoicing.Attempt{submit(invoicing.AttemptOutcomeError), query(string(invoicing.OutcomeReceived))},
		checkHint:  false,
		resendHint: false,
	},
	{
		// The SRI took it and now cannot find it: acknowledged, so the
		// resend hint stays silent; undecided, so check status stands.
		name:       "submit received then query unknown",
		inv:        pendingInvoice,
		attempts:   []invoicing.Attempt{submit(string(invoicing.OutcomeReceived)), query(string(invoicing.OutcomeUnknown))},
		checkHint:  true,
		resendHint: false,
	},
	{
		name:       "submit received alone",
		inv:        pendingInvoice,
		attempts:   []invoicing.Attempt{submit(string(invoicing.OutcomeReceived))},
		checkHint:  true,
		resendHint: false,
	},
	{
		name:       "submit error alone",
		inv:        pendingInvoice,
		attempts:   []invoicing.Attempt{submit(invoicing.AttemptOutcomeError)},
		checkHint:  false,
		resendHint: false,
	},
	{
		name:       "no attempts",
		inv:        pendingInvoice,
		attempts:   nil,
		checkHint:  false,
		resendHint: false,
	},
	{
		// Parked by the 24-hour rule (#474) with the SRI still holding it:
		// undecided, so the hint stands.
		name:       "parked and held",
		inv:        parkedInvoice,
		attempts:   []invoicing.Attempt{submit(string(invoicing.OutcomeReceived))},
		checkHint:  true,
		resendHint: false,
	},
	{
		name:       "parked and unknown",
		inv:        parkedInvoice,
		attempts:   []invoicing.Attempt{submit(invoicing.AttemptOutcomeError), query(string(invoicing.OutcomeUnknown))},
		checkHint:  false,
		resendHint: true,
	},
	{
		// Decided: the authority held it and answered. Nothing to check.
		name:       "authorized after a received submit",
		inv:        authorizedInvoice,
		attempts:   []invoicing.Attempt{submit(string(invoicing.OutcomeReceived)), query(string(invoicing.OutcomeAuthorized))},
		checkHint:  false,
		resendHint: false,
	},
	{
		// A Sale Invoice the authority refused: the Drainer parks it
		// needs_attention for the operator, but the answer is in — there is
		// nothing left to check, and the refusal is what the page shows.
		name:       "parked by a refusal",
		inv:        parkedInvoice,
		attempts:   []invoicing.Attempt{submit(string(invoicing.OutcomeReceived)), query(string(invoicing.OutcomeNotAuthorized))},
		checkHint:  false,
		resendHint: false,
	},
	{
		// A call that could not be made decides nothing: the authority still
		// holds the document, and asking again is still the thing to do.
		name:       "submit received then a failed query",
		inv:        pendingInvoice,
		attempts:   []invoicing.Attempt{submit(string(invoicing.OutcomeReceived)), query(invoicing.AttemptOutcomeError)},
		checkHint:  true,
		resendHint: false,
	},
	{
		// The Sale was reversed before this document was ever taken, so it
		// will never be sent again (ErrInvoiceWithdrawn refuses the resend):
		// the ledger a Submit lost in transport left behind is history, and
		// asking the operator to resend would be asking the impossible.
		name:       "withdrawn after a lost submit",
		inv:        withdrawnInvoice,
		attempts:   []invoicing.Attempt{submit(invoicing.AttemptOutcomeError), query(string(invoicing.OutcomeUnknown))},
		checkHint:  false,
		resendHint: false,
	},
	{
		// Refused at recepción: the authority never took it, and the
		// rejection — not silence — is what the page shows.
		name:       "rejected at recepcion",
		inv:        rejectedInvoice,
		attempts:   []invoicing.Attempt{submit(string(invoicing.OutcomeRejected))},
		checkHint:  false,
		resendHint: false,
	},
}

func TestCheckStatusHint(t *testing.T) {
	for _, c := range hintCases {
		t.Run(c.name, func(t *testing.T) {
			if got := checkStatusHint(c.inv, c.attempts); got != c.checkHint {
				t.Fatalf("checkStatusHint = %v, want %v", got, c.checkHint)
			}
		})
	}
}

func TestResendHint(t *testing.T) {
	for _, c := range hintCases {
		t.Run(c.name, func(t *testing.T) {
			if got := resendHint(c.inv, c.attempts); got != c.resendHint {
				t.Fatalf("resendHint = %v, want %v", got, c.resendHint)
			}
		})
	}
}

// TestHintsAreMutuallyExclusive is the promise the page is built on: the
// operator is never told both that the SRI has the document and that it has
// no record of it. Read over every ledger shape above, and over every
// invoice status, since the check-status hint is the only one of the two
// that reads the status.
func TestHintsAreMutuallyExclusive(t *testing.T) {
	statuses := []*invoicing.Invoice{pendingInvoice, parkedInvoice, authorizedInvoice, rejectedInvoice, withdrawnInvoice}
	for _, c := range hintCases {
		for _, inv := range statuses {
			t.Run(c.name+"/"+string(inv.Status), func(t *testing.T) {
				if checkStatusHint(inv, c.attempts) && resendHint(inv, c.attempts) {
					t.Fatalf("both hints true for ledger %q at status %s", c.name, inv.Status)
				}
			})
		}
	}
}

// What Resend refuses and Check status does not (#577, parent #575, ADR
// 0068): the two guards read side by side, because the whole point of the
// refusal by number is that it falls on ONE of the two levers. The operator
// must still be able to ask the authority what it holds — that answer is
// what an Abandon (#578) will later rest on — while the send that provably
// cannot succeed is refused outright. The transaction, the signing and what
// reaches the SRI are the integration harness's to prove; here only the
// answer, by error code.

func numberRefusedInvoice(messages ...invoicing.AuthorityMessage) invoicing.Invoice {
	return invoicing.Invoice{
		ID:        "parked",
		Kind:      invoicing.DocumentKindSale,
		Status:    invoicing.InvoiceStatusNeedsAttention,
		SignedXML: []byte("<factura/>"),
		Messages:  messages,
	}
}

func authorityError(identifier string) invoicing.AuthorityMessage {
	return invoicing.AuthorityMessage{Identifier: identifier, Message: "x", Type: invoicing.AuthorityMessageTypeError}
}

func TestResendRefusalAnswersTheNumberRefusalAndCheckStatusStaysAvailable(t *testing.T) {
	cases := []struct {
		name       string
		inv        func() invoicing.Invoice
		wantResend string
		wantCheck  string
	}{
		{"a document refused by number", func() invoicing.Invoice {
			return numberRefusedInvoice(authorityError(invoicing.AuthorityMessageSequenceRegistered))
		}, "INVOICE_REFUSED_BY_NUMBER", ""},
		{"beside another refusal", func() invoicing.Invoice {
			return numberRefusedInvoice(authorityError("35"), authorityError(invoicing.AuthorityMessageSequenceRegistered))
		}, "INVOICE_REFUSED_BY_NUMBER", ""},
		{"a schema refusal is resent as it always was", func() invoicing.Invoice {
			return numberRefusedInvoice(authorityError("35"))
		}, "", ""},
		{"45 as an advertencia is not the refusal", func() invoicing.Invoice {
			return numberRefusedInvoice(invoicing.AuthorityMessage{Identifier: invoicing.AuthorityMessageSequenceRegistered, Type: invoicing.AuthorityMessageTypeWarning})
		}, "", ""},
		{"a document the authority said nothing about", func() invoicing.Invoice { return numberRefusedInvoice() }, "", ""},
		{"authorized", func() invoicing.Invoice {
			i := numberRefusedInvoice()
			i.Status = invoicing.InvoiceStatusAuthorized
			return i
		}, "INVOICE_ALREADY_AUTHORIZED", "INVOICE_ALREADY_AUTHORIZED"},
		{"annulled", func() invoicing.Invoice {
			i := numberRefusedInvoice()
			i.Status = invoicing.InvoiceStatusAnnulled
			return i
		}, "INVOICE_ANNULLED", "INVOICE_ANNULLED"},
		{"withdrawn", func() invoicing.Invoice {
			i := numberRefusedInvoice()
			i.Status = invoicing.InvoiceStatusWithdrawn
			return i
		}, "INVOICE_WITHDRAWN", "INVOICE_WITHDRAWN"},
		{"owed and unsigned", func() invoicing.Invoice {
			i := numberRefusedInvoice()
			i.Status = invoicing.InvoiceStatusOwed
			i.SignedXML = nil
			return i
		}, "INVOICE_NOT_ISSUED", "INVOICE_NOT_ISSUED"},
		// The state is answered first wherever both would refuse: what a
		// finished document answers must not change because of what the
		// authority once said about its number.
		{"annulled and refused by number answers annulled", func() invoicing.Invoice {
			i := numberRefusedInvoice(authorityError(invoicing.AuthorityMessageSequenceRegistered))
			i.Status = invoicing.InvoiceStatusAnnulled
			return i
		}, "INVOICE_ANNULLED", "INVOICE_ANNULLED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := tc.inv()
			if got := code(resendRefusal(&inv)); got != tc.wantResend {
				t.Fatalf("resendRefusal = %q; want %q", got, tc.wantResend)
			}
			if got := code(actionableRefusal(&inv)); got != tc.wantCheck {
				t.Fatalf("actionableRefusal = %q; want %q", got, tc.wantCheck)
			}
		})
	}
}
