package platform

import (
	"strings"
	"testing"
)

// What an Answer Reminder actually says (#317, ADR 0044).
//
// These assert on the RENDERED words, on email_saleconfirmation_test.go's terms:
// a test that only checked a Locale was carried would pass just as happily
// against a message that was never translated. This mail is sent by a job
// nobody is watching, weeks after the sale, so the rendered text is the only
// place its wording is ever inspected.

func answerReminder() AnswerReminder {
	return AnswerReminder{
		To:               "ana@example.com",
		CustomerName:     "Ana",
		EventName:        "Noche de Jazz",
		Reference:        "TP-ABC123",
		ConfirmationLink: "https://storefront.test/tickets/confirm?token=x",
	}
}

// The zero Locale is English, which is what a box office sale, an import and
// every Online Sale that predates the Sale Locale column is written in.
func TestAnswerReminderIsWrittenInEnglishWhenNothingNamedALanguage(t *testing.T) {
	reminder := answerReminder()

	if got := reminder.Subject(); got != "Some tickets for Noche de Jazz still need answers" {
		t.Fatalf("subject = %q, want the English subject", got)
	}
	text := reminder.Text()
	for _, want := range []string{
		"Hi Ana,",
		"Some of the tickets on your purchase for Noche de Jazz still need answers.",
		"Reference: TP-ABC123",
		"or to copy and pass each ticket's own link to whoever will be using it:",
		"https://storefront.test/tickets/confirm?token=x",
		"Answering is optional and your tickets are valid either way.",
		"at most one more reminder about this purchase",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
}

// Written in the recipient's Mail Locale, which is an acceptance criterion of
// #317 and the reason ADR 0033 exists at all.
//
// It pins the REGISTER as well as the language: usted, matching the receipt's
// Spanish and the Storefront's own ("Sus entradas"), because a tú-form rewrite
// would pass any test that only checked the language had branched.
func TestAnswerReminderIsWrittenInSpanish(t *testing.T) {
	reminder := answerReminder()
	reminder.Locale = LocaleES

	if got := reminder.Subject(); got != "Algunas entradas para Noche de Jazz aún necesitan respuestas" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}
	text := reminder.Text()
	for _, want := range []string{
		"Hola Ana:",
		"Algunas de las entradas de su compra de Noche de Jazz aún necesitan respuestas.",
		"Referencia: TP-ABC123",
		"o para copiar y enviar el enlace de cada entrada a quien vaya a usarla:",
		"https://storefront.test/tickets/confirm?token=x",
		"Responder es opcional y sus entradas son válidas igualmente.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	// No English may survive into the Spanish message. "still need answers" is
	// the phrase that would leak first if a line were ever added in one language
	// only, since it is the sentence the mail is about.
	if strings.Contains(text, "still need answers") {
		t.Fatalf("text = %q, want no English left in it", text)
	}
}

// THE MAIL CARRIES ONE URL AND ONLY ONE. The link is the Confirmation Link, and
// an Answer Link must never appear in this body: an Answer Link is built to be
// forwarded into a group chat, and this mail names the buyer and their purchase.
// Putting one inside the other would make "send my friend the t-shirt question"
// and "send my friend my receipt" the same gesture, which is the failure ADR
// 0044 names. Distribution happens on the page.
func TestAnswerReminderCarriesTheConfirmationLinkAndNoOther(t *testing.T) {
	reminder := answerReminder()

	if links := strings.Count(reminder.Text(), "https://"); links != 1 {
		t.Fatalf("the reminder carries %d links, want exactly one — the Confirmation Link, and never an Answer Link", links)
	}
}

// It names no figure. The debt is derived live and a count baked into an inbox
// is wrong the moment the buyer answers one — the same reason the receipt's
// sentence names none and SaleConfirmation.HasOutstandingAnswers is a bool.
func TestAnswerReminderNamesNoNumberOfOutstandingAnswers(t *testing.T) {
	text := answerReminder().Text()
	for _, digit := range []string{"1 ticket", "2 tickets", "3 tickets"} {
		if strings.Contains(text, digit) {
			t.Fatalf("text = %q, want no count of what is owed in it", text)
		}
	}
}

// It offers no way to unsubscribe, and that is correct rather than an omission:
// this is transactional mail, the Follow Digest is the only mail a Customer can
// turn off (ADR 0034), and what bounds this one is catalog.MayRemind's cap. An
// unsubscribe here would be the platform offering to stop sending something it
// is going to stop sending anyway.
func TestAnswerReminderCarriesNoUnsubscribe(t *testing.T) {
	text := strings.ToLower(answerReminder().Text())
	for _, forbidden := range []string{"unsubscribe", "darse de baja", "cancelar la suscripción"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("text = %q, want no unsubscribe in a transactional mail", text)
		}
	}
}
