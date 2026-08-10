package platform

import (
	"strings"
	"testing"
)

// The receipt is the mail a Customer is certain to open, and #245 is the ticket
// that stops it arriving in a language they did not choose. These tests assert
// on the RENDERED words for the reason the passcode's do: a test that only
// checked a Locale was carried would pass just as happily against a message that
// was never translated.

func spanishConfirmation() SaleConfirmation {
	return SaleConfirmation{
		To:               "ana@example.com",
		CustomerName:     "Ana",
		EventName:        "Noche de Jazz",
		Reference:        "ABC123",
		AmountCents:      1782,
		Currency:         "USD",
		ConfirmationLink: "https://example.test/tickets/confirm?token=x",
		TaxID:            SaleTaxID{Type: TaxIDTypeCedula, Number: "1712345675"},
		Locale:           LocaleES,
	}
}

// TestSaleConfirmationIsWrittenInEnglishWhenNothingNamedALanguage is the whole
// of the "existing sales are unaffected" promise, at the seam where it is
// decided: a sale that recorded no Locale renders the zero value, and the zero
// value is the English receipt this platform has always sent.
func TestSaleConfirmationIsWrittenInEnglishWhenNothingNamedALanguage(t *testing.T) {
	confirmation := spanishConfirmation()
	confirmation.Locale = ""

	if got := confirmation.Subject(); got != "Your tickets for Noche de Jazz" {
		t.Fatalf("subject = %q, want the English subject", got)
	}
	text := confirmation.Text()
	for _, want := range []string{
		"Hi Ana,",
		"Your purchase for Noche de Jazz is confirmed.",
		"Reference: ABC123",
		"Total paid: 17.82 USD",
		"Present this reference at the event.",
		"View your tickets:",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
}

// TestSaleConfirmationIsWrittenInSpanish is the deliverable of #245.
//
// It also pins the REGISTER: usted, matching apps/storefront/messages/es.json
// ("Sus entradas para {event} están confirmadas"), because a tú-form rewrite
// would pass any test that only checked the language had branched.
func TestSaleConfirmationIsWrittenInSpanish(t *testing.T) {
	confirmation := spanishConfirmation()

	if got := confirmation.Subject(); got != "Sus entradas para Noche de Jazz" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}

	text := confirmation.Text()
	for _, want := range []string{
		"Hola Ana:",
		"Su compra de Noche de Jazz está confirmada.",
		"Referencia: ABC123",
		"Presente esta referencia en el evento.",
		"Vea sus entradas:",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, "Tus ") || strings.Contains(text, "Presenta ") {
		t.Fatalf("text = %q is written in tú; Spanish mail is usted (ADR 0033)", text)
	}
}

// TestSaleConfirmationStatesMoneyAndTheTaxIDTheSameWayInEveryLanguage holds the
// line ADR 0033 draws: localizing mail changes the words and the marks around
// the numbers, and changes nothing about the numbers.
//
// The total stays in the ORGANIZATION's currency — the only currency any of its
// money is ever stated in — and the Tax ID keeps the label printed on the
// document in the buyer's hand, which is Spanish on an English receipt too.
func TestSaleConfirmationStatesMoneyAndTheTaxIDTheSameWayInEveryLanguage(t *testing.T) {
	spanish := spanishConfirmation()
	english := spanishConfirmation()
	english.Locale = LocaleEN

	for _, text := range []string{spanish.Text(), english.Text()} {
		if !strings.Contains(text, "17.82 USD") {
			t.Fatalf("text = %q, want the total unchanged in the Organization's currency", text)
		}
		if !strings.Contains(text, "Cédula: 1712345675") {
			t.Fatalf("text = %q, want the Tax ID line as the buyer's own document prints it", text)
		}
	}
}

// TestSaleConfirmationOmitsWhatTheSaleDoesNotCarryInEveryLanguage: the two
// conditional lines are absent rather than blank in Spanish exactly as they are
// in English — an empty label on a receipt reads as a fault in the platform, and
// a translated message is where a "label with nothing after it" bug hides.
func TestSaleConfirmationOmitsWhatTheSaleDoesNotCarryInEveryLanguage(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		confirmation := spanishConfirmation()
		confirmation.Locale = locale
		confirmation.TaxID = SaleTaxID{}
		confirmation.ConfirmationLink = ""

		text := confirmation.Text()
		for _, unwanted := range []string{"Cédula", "http", "\n\n\n"} {
			if strings.Contains(text, unwanted) {
				t.Fatalf("locale %q text = %q, want no trace of %q", locale, text, unwanted)
			}
		}
	}
}
