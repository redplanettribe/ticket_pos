package invoicing

import "testing"

// CreditNoteMotivo (#476, ADR 0060; #481, ADR 0061) states a Credit Note's
// reason to the authority in the document's own words: a reversal route's
// Anulación, the reissue's fixed correction text, and a bare Anulación for
// a reason it does not know — never a refusal.
func TestCreditNoteMotivoNamesEveryReason(t *testing.T) {
	for reason, want := range map[string]string{
		"customer":              "Anulación de la venta por el comprador",
		"platform":              "Anulación de la venta por el operador de la plataforma",
		"import_undo":           "Anulación de la venta al deshacer su importación",
		"staff_reversal":        "Anulación de la venta por el personal de la organización",
		"correction":            "Anulación de la venta por corrección",
		CreditNoteReasonReissue: "Corrección de los datos del receptor",
		"something_later":       "Anulación de la venta",
	} {
		if got := CreditNoteMotivo(reason); got != want {
			t.Fatalf("CreditNoteMotivo(%q) = %q, want %q", reason, got, want)
		}
	}
}

// AcknowledgedByAuthority (#513, #515) reads the ledger for one thing: a
// Submit the authority took. The production shape that started this — a
// Submit lost in transport, then queries reporting "received" or nothing
// known — is not an acknowledgement, and the document is owed a resubmit.
func TestAcknowledgedByAuthorityCountsSubmitsOnly(t *testing.T) {
	submit := func(outcome string) Attempt {
		return Attempt{Operation: AttemptSubmit, Outcome: outcome}
	}
	query := func(outcome string) Attempt {
		return Attempt{Operation: AttemptQuery, Outcome: outcome}
	}
	for name, tc := range map[string]struct {
		attempts []Attempt
		want     bool
	}{
		"nothing sent yet":              {nil, false},
		"submit received":               {[]Attempt{submit(string(OutcomeReceived))}, true},
		"submit failed, query received": {[]Attempt{submit(AttemptOutcomeError), query(string(OutcomeReceived))}, false},
		"submit failed, query unknown":  {[]Attempt{submit(AttemptOutcomeError), query(string(OutcomeUnknown))}, false},
		"submit already held, then unknown": {[]Attempt{
			submit(AttemptOutcomeError), submit(string(OutcomeReceived)), query(string(OutcomeUnknown)),
		}, true},
		"submit rejected": {[]Attempt{submit(string(OutcomeRejected))}, false},
	} {
		if got := AcknowledgedByAuthority(tc.attempts); got != tc.want {
			t.Errorf("%s: AcknowledgedByAuthority = %v, want %v", name, got, tc.want)
		}
	}
}
