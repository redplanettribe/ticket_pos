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

// The Outstanding Answers line (#315, ADR 0044) — ONE conditional sentence, and
// the whole of what this feature adds to the receipt.
//
// The Sale Confirmation is where the buyer learns that some of the Tickets they
// bought still owe Answers, because the platform can write to the buyer and to
// nobody else: it holds no holder addresses and asks for none (ADR 0044). What
// it must NOT do is carry the per-Ticket Answer Links themselves — forwarding
// one would forward the receipt, and the receipt is the reference, the total and
// the Tax ID. So the sentence points back at the Confirmation Link the mail
// already carries, and the distribution happens on the page behind it.

// TestSaleConfirmationIsByteIdenticalWhenNothingIsOutstanding is the acceptance
// criterion this feature is most likely to break and least likely to notice.
//
// It is frozen against a LITERAL rather than compared to a second render,
// because a test that rendered the message twice and diffed the two would pass
// against a sentence that had been added to both. These two strings are the
// receipt exactly as this platform sent it before #315, and any change to them
// at all is a change to every receipt for every Sale that owes nothing — which
// is the great majority of them, and every one ever sent by an Organization that
// never wrote a Ticket Question.
func TestSaleConfirmationIsByteIdenticalWhenNothingIsOutstanding(t *testing.T) {
	for _, tc := range []struct {
		locale Locale
		want   string
	}{
		{LocaleEN, "Hi Ana,\n\nYour purchase for Noche de Jazz is confirmed.\nReference: ABC123\nTotal paid: 17.82 USD\nCédula: 1712345675\n\nPresent this reference at the event.\n\nView your tickets:\nhttps://example.test/tickets/confirm?token=x\n\nThis link opens this purchase only, and stays valid until shortly after the event."},
		{LocaleES, "Hola Ana:\n\nSu compra de Noche de Jazz está confirmada.\nReferencia: ABC123\nTotal pagado: 17.82 USD\nCédula: 1712345675\n\nPresente esta referencia en el evento.\n\nVea sus entradas:\nhttps://example.test/tickets/confirm?token=x\n\nEste enlace abre solo esta compra y sigue siendo válido hasta poco después del evento."},
	} {
		// The zero value of the new field is the state every Sale that owes
		// nothing is in, and it is what every caller that never heard of #315
		// passes. Asserting on the zero value rather than on an explicit false is
		// the point: a receipt built by code written before this feature must
		// render as it always did.
		confirmation := spanishConfirmation()
		confirmation.Locale = tc.locale

		if got := confirmation.Text(); got != tc.want {
			t.Fatalf("locale %q: the receipt for a Sale owing nothing changed.\n got: %q\nwant: %q", tc.locale, got, tc.want)
		}
	}
}

// TestSaleConfirmationCarriesTheOutstandingAnswersLineOnlyWhenSomethingIsOwed
// asserts the difference rather than either side of it, for the reason the
// consent line's twin does: a test of one side alone would pass against a
// sentence that was always there or never was.
func TestSaleConfirmationCarriesTheOutstandingAnswersLineOnlyWhenSomethingIsOwed(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		owing := spanishConfirmation()
		owing.Locale = locale
		owing.HasOutstandingAnswers = true

		nothing := spanishConfirmation()
		nothing.Locale = locale

		if owing.Text() == nothing.Text() {
			t.Fatalf("locale %q: a Sale owing Answers reads exactly like one owing none", locale)
		}
		// The sentence is an ADDITION and never a rewrite: everything the receipt
		// said before is still in it, in the same order, with the new line after.
		if !strings.HasPrefix(owing.Text(), nothing.Text()) {
			t.Fatalf("locale %q: the outstanding line rewrote the receipt instead of appending to it:\n%q", locale, owing.Text())
		}
	}
}

// TestSaleConfirmationOutstandingAnswersLineCarriesNoNewLink is the rule the
// mail half of #315 exists to keep, stated as a test.
//
// An Answer Link opens ONE Ticket and is meant to be forwarded; the receipt
// carries the reference, the total and the Tax ID and is meant not to be. Put
// one in the other and the buyer forwarding a t-shirt-size question to a friend
// forwards their own receipt. So the sentence points at the Confirmation Link
// already above it and introduces no URL of its own — the only URL in this mail
// is the one that was in it before.
func TestSaleConfirmationOutstandingAnswersLineCarriesNoNewLink(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		owing := spanishConfirmation()
		owing.Locale = locale
		owing.HasOutstandingAnswers = true

		text := owing.Text()
		if strings.Count(text, "http") != 1 {
			t.Fatalf("locale %q: the receipt carries %d links; it must carry only the Confirmation Link it already had:\n%s",
				locale, strings.Count(text, "http"), text)
		}
		if strings.Contains(text, "/answer") {
			t.Fatalf("locale %q: an Answer Link reached the mail body; forwarding it would forward the receipt:\n%s", locale, text)
		}
	}
}

// TestSaleConfirmationOutstandingAnswersLineIsSilentWithoutTheLinkItPointsAt.
//
// The sentence's whole content is "open the link above". With no Confirmation
// Link there is no link above, and the sentence becomes an instruction to press
// something that is not on the page — worse than silence, because the reader can
// do nothing about it and will go looking. A missing Confirmation Link already
// means a misconfigured deployment rather than anything about this Sale.
func TestSaleConfirmationOutstandingAnswersLineIsSilentWithoutTheLinkItPointsAt(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		owing := spanishConfirmation()
		owing.Locale = locale
		owing.HasOutstandingAnswers = true
		owing.ConfirmationLink = ""

		nothing := spanishConfirmation()
		nothing.Locale = locale
		nothing.ConfirmationLink = ""

		if owing.Text() != nothing.Text() {
			t.Fatalf("locale %q: the outstanding line was printed with no Confirmation Link to point at:\n%q", locale, owing.Text())
		}
	}
}

// The rendered words, in both languages, for the reason every copy test in this
// package gives: a test that only checked the line had appeared would pass just
// as happily against one that was never translated.
func TestSaleConfirmationOutstandingAnswersLineIsWrittenInEnglish(t *testing.T) {
	owing := spanishConfirmation()
	owing.Locale = LocaleEN
	owing.HasOutstandingAnswers = true

	text := owing.Text()
	for _, want := range []string{
		"Some of the tickets on this purchase still need answers.",
		// The two things the buyer can do, both of which happen on the page and
		// neither of which happens in this mail.
		"answer for your own ticket",
		"enter the email address of whoever will be using each of the others",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
}

// The Spanish twin also pins the REGISTER — usted, like the rest of this
// platform's mail (ADR 0033) — because a tú-form rewrite would pass any test
// that only checked the language had branched.
func TestSaleConfirmationOutstandingAnswersLineIsWrittenInSpanish(t *testing.T) {
	owing := spanishConfirmation()
	owing.Locale = LocaleES
	owing.HasOutstandingAnswers = true

	text := owing.Text()
	for _, want := range []string{
		"Algunas de las entradas de esta compra aún necesitan respuestas.",
		"responder por su propia entrada",
		"el correo electrónico de quien vaya a usar",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, "Abre ") || strings.Contains(text, "respóndelas") || strings.Contains(text, "tu compra") {
		t.Fatalf("text = %q is written in tú; Spanish mail is usted (ADR 0033)", text)
	}
}
