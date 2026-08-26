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
