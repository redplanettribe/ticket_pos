package platform

import (
	"strings"
	"testing"
)

// The two notices a reversal produces, in the language the sale was made in
// (#246, ADR 0033).
//
// They are tested together because they are two halves of one promise: the void
// notice says a purchase is gone, and the refused-reversal notice says one that
// was supposed to be gone is not. A Customer meets exactly one of them, and
// which one they meet is decided long after the sale, by an actor who is not
// them.
//
// The assertions are on the RENDERED words, for the reason the receipt's are: a
// test that only checked a Locale was carried would pass just as happily against
// a message nobody ever translated.

func spanishVoidNotice() SaleVoided {
	return SaleVoided{
		To:           "ana@example.com",
		CustomerName: "Ana",
		EventName:    "Noche de Jazz",
		Reference:    "ABC123",
		Locale:       LocaleES,
	}
}

func spanishRefusedNotice() SaleReversalRefused {
	return SaleReversalRefused{
		To:           "ana@example.com",
		CustomerName: "Ana",
		EventName:    "Noche de Jazz",
		Reference:    "ABC123",
		Locale:       LocaleES,
	}
}

// TestSaleVoidedIsWrittenInSpanish is half the deliverable of #246.
//
// It pins the REGISTER — usted, as ADR 0033 requires and as the receipt and the
// passcode already are — and it pins the WORD for the state: "anulada", which is
// what the Storefront calls the same purchase on the page this Customer will
// open next (`sale.reversedTitle` in messages/es.json). One word for one thing,
// across mail and page.
func TestSaleVoidedIsWrittenInSpanish(t *testing.T) {
	voided := spanishVoidNotice()

	if got := voided.Subject(); got != "Su compra de Noche de Jazz fue anulada" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}

	text := voided.Text()
	for _, want := range []string{
		"Hola Ana:",
		"Su compra de Noche de Jazz (referencia ABC123) fue anulada",
		"esas entradas ya no son válidas",
		"contacte al organizador",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	assertUsted(t, text)
}

// TestSaleReversalRefusedIsWrittenInSpanish is the other half, and the harder
// one: this is the message raised by the drain, with no page anywhere near it.
//
// The line asserted first is the one that decides whether this reader turns up
// at the gate. It is the Storefront's own sentence for the same outcome
// (`sale.refundRefusedToast`), said to the Customer who closed the tab and will
// never see that page.
func TestSaleReversalRefusedIsWrittenInSpanish(t *testing.T) {
	refused := spanishRefusedNotice()

	if got := refused.Subject(); got != "No pudimos deshacer su compra de Noche de Jazz" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}

	text := refused.Text()
	for _, want := range []string{
		"Hola Ana:",
		"No pudimos deshacer su compra de Noche de Jazz (referencia ABC123)",
		"Sus entradas siguen siendo válidas",
		"contacte al organizador",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	assertUsted(t, text)
}

// assertUsted catches a tú-form rewrite, which every other assertion in this
// file would pass unchanged. The forms listed are the ones this copy would
// actually take if somebody translated it again without reading ADR 0033.
func assertUsted(t *testing.T, text string) {
	t.Helper()
	for _, tuForm := range []string{"Tus ", "tus ", "Tu compra", "tu compra", "contacta ", "puedes ", "esperabas"} {
		if strings.Contains(text, tuForm) {
			t.Fatalf("text = %q is written in tú (%q); Spanish mail is usted (ADR 0033)", text, tuForm)
		}
	}
}

// TestReversalNoticesAreWrittenInEnglishWhenNothingNamedALanguage is the whole
// of the "sales made before any of this shipped are unaffected" promise, at the
// seam where it is decided.
//
// The zero Locale is what a box office sale, an import, and every Online Sale
// older than the column all resolve to, and it must render the exact English
// these notices have always been sent in — including the em-dashed sentence in
// the middle of the refusal, which is the one a reader acts on.
func TestReversalNoticesAreWrittenInEnglishWhenNothingNamedALanguage(t *testing.T) {
	voided := spanishVoidNotice()
	voided.Locale = ""
	if got := voided.Subject(); got != "Your Noche de Jazz purchase has been reversed" {
		t.Fatalf("void subject = %q, want the English subject", got)
	}
	voidedText := voided.Text()
	for _, want := range []string{
		"Hi Ana,",
		"Your purchase for Noche de Jazz (reference ABC123) has been reversed, and those tickets are no longer valid.",
		"If you did not expect this, contact the organizer.",
	} {
		if !strings.Contains(voidedText, want) {
			t.Fatalf("void text = %q, want it to contain %q", voidedText, want)
		}
	}

	refused := spanishRefusedNotice()
	refused.Locale = ""
	if got := refused.Subject(); got != "We could not undo your Noche de Jazz purchase" {
		t.Fatalf("refused subject = %q, want the English subject", got)
	}
	refusedText := refused.Text()
	for _, want := range []string{
		"Hi Ana,",
		"We could not undo your purchase for Noche de Jazz (reference ABC123).",
		"Your tickets are still valid — nothing has changed about your purchase, and you can still use them.",
		"If you need help with this purchase, contact the organizer and quote the reference above.",
	} {
		if !strings.Contains(refusedText, want) {
			t.Fatalf("refused text = %q, want it to contain %q", refusedText, want)
		}
	}
}

// TestReversalNoticesSayTheSameThingsInEitherLanguage guards the failure mode
// the per-sentence lookup exists to prevent: a Spanish reader missing a line an
// English reader gets.
//
// Every one of these facts is load-bearing. The reference is what a buyer quotes
// to the organizer, the Event name is how they know which purchase this is
// about, and — on the refusal — the fact that the tickets still work is the
// whole message. None of them may go missing in translation.
func TestReversalNoticesSayTheSameThingsInEitherLanguage(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		voided := spanishVoidNotice()
		voided.Locale = locale
		voidedText := voided.Text()
		for _, want := range []string{"Ana", "Noche de Jazz", "ABC123"} {
			if !strings.Contains(voidedText, want) {
				t.Fatalf("void notice in %q = %q, want it to name %q", locale, voidedText, want)
			}
		}
		if !strings.Contains(voided.Subject(), "Noche de Jazz") {
			t.Fatalf("void subject in %q = %q, want it to name the Event", locale, voided.Subject())
		}

		refused := spanishRefusedNotice()
		refused.Locale = locale
		refusedText := refused.Text()
		for _, want := range []string{"Ana", "Noche de Jazz", "ABC123"} {
			if !strings.Contains(refusedText, want) {
				t.Fatalf("refused notice in %q = %q, want it to name %q", locale, refusedText, want)
			}
		}
		if !strings.Contains(refused.Subject(), "Noche de Jazz") {
			t.Fatalf("refused subject in %q = %q, want it to name the Event", locale, refused.Subject())
		}
	}
}

// TestSaleReversalRefusedExplainsNothingInEitherLanguage keeps ADR 0018's
// refusal where translation could quietly undo it.
//
// There is no provider answer that means "too late", so a refusal cannot be
// explained — and a translator writing the same message from scratch is exactly
// the person who would add the reassuring "esto puede ocurrir cuando…" that the
// English deliberately does without.
func TestSaleReversalRefusedExplainsNothingInEitherLanguage(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		refused := spanishRefusedNotice()
		refused.Locale = locale
		body := refused.Text() + "\n" + refused.Subject()
		for _, cause := range []string{"because", "porque", "puede ocurrir", "can happen", "banco", "bank", "PayPhone", "proveedor de pagos", "payment provider"} {
			if strings.Contains(body, cause) {
				t.Fatalf("refused notice in %q offers a cause (%q):\n%s", locale, cause, body)
			}
		}
	}
}
