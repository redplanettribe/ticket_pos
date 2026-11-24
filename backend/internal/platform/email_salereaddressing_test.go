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

// The Re-addressing mail's text is byte-identical to what it was before its
// HTML part (#879) split its copy into blocks: frozen against a literal, in
// both languages, and the Event the HTML part shows changes nothing in it.
func TestSaleReAddressingTextIsUnchangedByItsHTMLPart(t *testing.T) {
	for _, tc := range []struct {
		locale Locale
		want   string
	}{
		{LocaleEN, "At the organizer's request, we are re-addressing a ticket purchase to this email address.\n\nEvent: Noche de Jazz\nReference: TP-J7K2QX9M\n\nThe purchase was made under an email address that could not be reached, and the organizer has told us this is the address it was meant for.\n\nIf this purchase is yours, accept it here and the tickets and every message about them will come to this address from now on. Accepting takes this purchase on as your own:\nhttps://storefront.test/re-addressing?token=abc.def\n\nIf this purchase is not yours, or you did not expect this message, you do not have to do anything. Nothing changes unless you accept, and this link stops working when the event starts."},
		{LocaleES, "A pedido de la organización, estamos redirigiendo una compra de entradas a esta dirección de correo.\n\nEvento: Noche de Jazz\nReferencia: TP-J7K2QX9M\n\nLa compra se hizo con una dirección de correo a la que no se podía llegar, y la organización nos ha indicado que esta es la dirección a la que estaba destinada.\n\nSi esta compra es suya, acéptela aquí y las entradas y todos los mensajes sobre ellas llegarán a esta dirección de ahora en adelante. Al aceptar, usted asume esta compra como propia:\nhttps://storefront.test/re-addressing?token=abc.def\n\nSi esta compra no es suya, o no esperaba este mensaje, no tiene que hacer nada. Nada cambia a menos que usted acepte, y este enlace deja de funcionar cuando empieza el evento."},
	} {
		mail := saleReAddressing()
		mail.Locale = tc.locale
		mail.Event = MailEvent{OrganizationName: "Club de Jazz", WideURL: "https://media.example.com/w.jpg", VenueName: "Teatro"}
		if got := mail.Text(); got != tc.want {
			t.Fatalf("locale %q:\n got: %q\nwant: %q", tc.locale, got, tc.want)
		}
	}
}
