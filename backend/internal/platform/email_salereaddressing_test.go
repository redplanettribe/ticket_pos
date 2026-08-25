package platform

import (
	"strings"
	"testing"
)

// What the Re-addressing mail actually says (#420, parent #419, ADR 0058).
//
// These assert on the RENDERED words, on the Assignment mail's terms: a test
// that only checked a Locale was carried would pass against a message that was
// never translated. And this message may reach a STRANGER — an address typed
// wrong a second time — so the sentence that says accepting takes on somebody
// else's purchase is pinned here as hard as the link is.

func saleReAddressing() SaleReAddressing {
	return SaleReAddressing{
		To:        "ana.lopez@example.com",
		EventName: "Noche de Jazz",
		Reference: "TP-J7K2QX9M",
		AcceptURL: "https://storefront.test/re-addressing?token=abc.def",
	}
}

func TestSaleReAddressingIsWrittenInEnglishWhenNothingNamedALanguage(t *testing.T) {
	mail := saleReAddressing()

	if got := mail.Subject(); got != "Your purchase for Noche de Jazz has been re-addressed to you" {
		t.Fatalf("subject = %q, want the English subject", got)
	}
	text := mail.Text()
	for _, want := range []string{
		// Who is doing this, and on whose request.
		"At the organizer's request",
		"Event: Noche de Jazz",
		"Reference: TP-J7K2QX9M",
		// WHAT ACCEPTING DOES, stated as taking the purchase on — the sentence a
		// stranger needs (#419, story 24).
		"Accepting takes this purchase on as your own",
		"https://storefront.test/re-addressing?token=abc.def",
		// Ignoring is a real option with no consequence.
		"you do not have to do anything",
		"Nothing changes unless you accept",
		"this link stops working when the event starts",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	// The barred vocabulary (CONTEXT.md): the verb is accept.
	for _, barred := range []string{"claim", "transfer", "confirm"} {
		if strings.Contains(strings.ToLower(text), barred) {
			t.Fatalf("text uses the barred word %q", barred)
		}
	}
}

// Written in the Sale's own locale (ADR 0058), and pinned for REGISTER as well
// as language: usted, matching every other Customer-facing message.
func TestSaleReAddressingIsWrittenInSpanish(t *testing.T) {
	mail := saleReAddressing()
	mail.Locale = LocaleES

	if got := mail.Subject(); got != "Su compra para Noche de Jazz ha sido redirigida a usted" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}
	text := mail.Text()
	for _, want := range []string{
		"A pedido de la organización",
		"Evento: Noche de Jazz",
		"Referencia: TP-J7K2QX9M",
		"usted asume esta compra como propia",
		"https://storefront.test/re-addressing?token=abc.def",
		"no tiene que hacer nada",
		"este enlace deja de funcionar cuando empieza el evento",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, " tú ") || strings.Contains(text, "tienes") {
		t.Fatalf("text = %q, want usted throughout", text)
	}
}
