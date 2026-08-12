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

// The double opt-in's line (#255, ADR 0035). Its language is the Sale
// Confirmation's language, resolved by the same chain, because it is one
// sentence of one email and not a message of its own.

// TestSaleConfirmationCarriesTheConsentLineOnlyWhenSomethingPends is the first
// acceptance criterion, and the half that is easy to lose: receipts for sales
// that left nothing pending are UNCHANGED.
//
// Both directions are asserted here rather than in two tests, because the
// property is a difference and a test of either side alone would pass against a
// line that was always there or never was.
func TestSaleConfirmationCarriesTheConsentLineOnlyWhenSomethingPends(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		nothingPending := spanishConfirmation()
		nothingPending.Locale = locale
		if got := nothingPending.Text(); strings.Contains(got, "confirm-consent") {
			t.Fatalf("locale %q: a receipt with nothing pending carries a confirmation link:\n%s", locale, got)
		}

		pending := spanishConfirmation()
		pending.Locale = locale
		pending.ConsentConfirmationLink = "https://example.test/confirm-consent?token=x"
		if got := pending.Text(); !strings.Contains(got, "https://example.test/confirm-consent?token=x") {
			t.Fatalf("locale %q: a receipt with something pending carries no confirmation link:\n%s", locale, got)
		}
	}
}

// TestSaleConfirmationConsentLineIsWrittenInEnglish and its Spanish twin assert
// the RENDERED words, for the reason every copy test in this package does: a
// test that only checked the link was present would pass just as happily against
// a line that was never translated.
func TestSaleConfirmationConsentLineIsWrittenInEnglish(t *testing.T) {
	confirmation := spanishConfirmation()
	confirmation.Locale = LocaleEN
	confirmation.ConsentConfirmationLink = "https://example.test/confirm-consent?token=x"

	text := confirmation.Text()
	for _, want := range []string{
		"Confirm your optional preferences:",
		"https://example.test/confirm-consent?token=x",
		// The two sentences that make the non-expiring design honest to the person
		// reading it: nothing is acted on until it is confirmed, and ignoring this
		// is a complete answer.
		"We act on none of them until they are confirmed from this inbox.",
		"If that was not you, ignore this",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
}

// TestSaleConfirmationConsentLineIsWrittenInSpanish also pins the register —
// usted, like the rest of this platform's mail (ADR 0033) — because a tú-form
// rewrite would pass any test that only checked the language had branched.
func TestSaleConfirmationConsentLineIsWrittenInSpanish(t *testing.T) {
	confirmation := spanishConfirmation()
	confirmation.ConsentConfirmationLink = "https://example.test/confirm-consent?token=x"

	text := confirmation.Text()
	for _, want := range []string{
		"Confirme sus preferencias opcionales:",
		"https://example.test/confirm-consent?token=x",
		"No actuamos sobre ninguna hasta que se confirme desde este buzón.",
		"Si no fue usted, ignore este mensaje",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, "Confirma ") || strings.Contains(text, "marcaste") || strings.Contains(text, "tu dirección") {
		t.Fatalf("text = %q is written in tú; Spanish mail is usted (ADR 0033)", text)
	}
}

// TestSaleConfirmationConsentLineAccusesNobody is a copy test with a reason
// behind it rather than a spelling.
//
// Anyone can type anyone's address into a checkout, so this line is read by two
// different people: the buyer who remembers ticking something, and the stranger
// whose address was typed. Copy that said "you ticked" would tell the second one
// they made a choice they did not make, on the one email where the platform is
// asking them to trust it.
func TestSaleConfirmationConsentLineAccusesNobody(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		confirmation := spanishConfirmation()
		confirmation.Locale = locale
		confirmation.ConsentConfirmationLink = "https://example.test/confirm-consent?token=x"

		text := confirmation.Text()
		want := "You, or someone using your address, ticked"
		if locale == LocaleES {
			want = "Usted, o alguien que usó su dirección, marcó"
		}
		if !strings.Contains(text, want) {
			t.Fatalf("locale %q text = %q, want it to allow that somebody else ticked the box (%q)", locale, text, want)
		}
	}
}

// TestSaleConfirmationOmitsWhatTheSaleDoesNotCarryInEveryLanguage: the three
// conditional lines are absent rather than blank in Spanish exactly as they are
// in English — an empty label on a receipt reads as a fault in the platform, and
// a translated message is where a "label with nothing after it" bug hides.
func TestSaleConfirmationOmitsWhatTheSaleDoesNotCarryInEveryLanguage(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		confirmation := spanishConfirmation()
		confirmation.Locale = locale
		confirmation.TaxID = SaleTaxID{}
		confirmation.ConfirmationLink = ""
		confirmation.ConsentConfirmationLink = ""

		text := confirmation.Text()
		for _, unwanted := range []string{"Cédula", "http", "\n\n\n"} {
			if strings.Contains(text, unwanted) {
				t.Fatalf("locale %q text = %q, want no trace of %q", locale, text, unwanted)
			}
		}
	}
}
