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

// What the HOLDER'S Answer Reminder actually says (#328, parent #322, ADR 0046).
//
// SAME DISCIPLINE, DIFFERENT READER. These assert on the rendered words for the
// reason the buyer's do — a job nobody is watching sends this, so the text is
// the only place its wording is ever inspected — and they carry one extra
// burden: this reader is entitled to almost nothing about the purchase, so what
// is ABSENT is as much under test as what is present.

func holderAnswerReminder() HolderAnswerReminder {
	return HolderAnswerReminder{
		To: "carla@example.com",
		Tickets: []HolderAnswerReminderTicket{{
			EventName:      "Noche de Jazz",
			TicketTypeName: "General",
			AnswerURL:      "https://storefront.test/accept?token=x",
		}},
	}
}

// holderAnswerReminderForTwo is the mail #335 ruled into existence: one Holder,
// one sweep, two accepted Tickets, ONE message listing each with its own
// Assignment Link. The buyer's side already fans a Sale's Tickets into one
// mail; per-Ticket envelopes to a Holder were an inconsistency as well as a
// volume problem, and mailing one address twice in one sweep is the shape spam
// filters punish.
func holderAnswerReminderForTwo() HolderAnswerReminder {
	return HolderAnswerReminder{
		To: "carla@example.com",
		Tickets: []HolderAnswerReminderTicket{
			{
				EventName:      "Noche de Jazz",
				TicketTypeName: "General",
				AnswerURL:      "https://storefront.test/accept?token=x",
			},
			{
				EventName:      "Noche de Jazz",
				TicketTypeName: "VIP",
				AnswerURL:      "https://storefront.test/accept?token=y",
			},
		},
	}
}

// The zero Locale is English, as everywhere: the floor of ADR 0033's chain, and
// what a Holder gets when neither their record nor the sale named a language.
func TestHolderAnswerReminderIsWrittenInEnglishWhenNothingNamedALanguage(t *testing.T) {
	reminder := holderAnswerReminder()

	if got := reminder.Subject(); got != "Your ticket for Noche de Jazz still needs an answer" {
		t.Fatalf("subject = %q, want the English subject", got)
	}
	text := reminder.Text()
	for _, want := range []string{
		"The ticket you accepted still needs an answer",
		"Event: Noche de Jazz",
		"Ticket: General",
		"https://storefront.test/accept?token=x",
		"This link opens your ticket only",
		"Answering is optional and your ticket is valid either way.",
		"at most one more reminder",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
}

// Written in the RECIPIENT'S Mail Locale, which for this reader is resolved from
// their own record before the sale's (#325's inversion, ADR 0033, ADR 0046).
// This test pins the copy; where the Locale comes from is the sweep's business
// and is pinned in the integration suite.
//
// It pins the REGISTER too: usted, matching the Assignment mail that reached
// this same person first. A tú-form rewrite would pass any test that only
// checked the language had branched.
func TestHolderAnswerReminderIsWrittenInSpanish(t *testing.T) {
	reminder := holderAnswerReminder()
	reminder.Locale = LocaleES

	if got := reminder.Subject(); got != "Su entrada para Noche de Jazz aún necesita una respuesta" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}
	text := reminder.Text()
	for _, want := range []string{
		"La entrada que aceptó aún necesita respuesta",
		"Evento: Noche de Jazz",
		"Entrada: General",
		"https://storefront.test/accept?token=x",
		"Este enlace abre solo su entrada",
		"Responder es opcional y su entrada es válida igualmente.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	// No English may survive into the Spanish message. "still needs an answer" is
	// the phrase that would leak first if a line were ever added in one language
	// only, since it is the sentence the mail is about.
	if strings.Contains(text, "still needs an answer") {
		t.Fatalf("text = %q, want no English left in it", text)
	}
}

// IT NAMES NOTHING ABOUT THE PURCHASE, and this is the test to keep passing.
//
// The struct has no field for a buyer, a price, a Tax ID or a Sale Confirmation
// reference, which is the real enforcement — but a template is edited by hand
// and a greeting or a "bought for you by" line would be a one-line change. ADR
// 0044's disclosure rule, carried over unchanged by ADR 0046 and applied to an
// inbox: a Holder sees the Event, the Ticket Type and their own link. Being a
// Verified Customer of this platform buys nobody a fact about somebody else's
// purchase.
func TestHolderAnswerReminderNamesNothingAboutThePurchase(t *testing.T) {
	rendered := holderAnswerReminder().Subject() + "\n" + holderAnswerReminder().Text()
	for _, forbidden := range []string{
		"bought", "purchase", "reference", "Reference",
		"compró", "compra", "referencia", "Referencia",
		"$", "Tax ID", "RUC",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("the Holder's reminder contains %q:\n%s\n"+
				"It names the Event, the Ticket Type and their own link and nothing else (ADR 0044, ADR 0046).", forbidden, rendered)
		}
	}
}

// ONE URL AND ONLY ONE, and it is the Assignment Link.
//
// Sharper here than for the buyer's mail, which carries one link for the same
// tidiness reason. This one is a credential that MINTS AN IDENTITY, so a second
// URL beside it — an Answer Link, a Confirmation Link, a tracking wrapper —
// would put a token that proves nothing next to a token that proves everything,
// in one message, for a reader who cannot tell them apart.
func TestHolderAnswerReminderCarriesTheAssignmentLinkAndNoOther(t *testing.T) {
	text := holderAnswerReminder().Text()
	if links := strings.Count(text, "https://"); links != 1 {
		t.Fatalf("the Holder's reminder carries %d links, want exactly one — their own Assignment Link and nothing else", links)
	}
	if strings.Contains(text, "/confirm") {
		t.Fatalf("the Holder's reminder carries a Confirmation Link: that opens a whole Ticket Sale, which this reader may never see.\n%s", text)
	}
}

// It names no figure, for the reason the buyer's does not: the debt is derived
// live and a count baked into an inbox is wrong the moment somebody answers one.
// A Holder holds exactly one Ticket anyway, so the copy speaks of "an answer"
// and never of how many.
func TestHolderAnswerReminderNamesNoNumberOfOutstandingAnswers(t *testing.T) {
	text := holderAnswerReminder().Text()
	for _, digit := range []string{"1 question", "2 questions", "3 questions", "1 pregunta", "2 preguntas"} {
		if strings.Contains(text, digit) {
			t.Fatalf("text = %q, want no count of what is owed in it", text)
		}
	}
}

// It offers no way to unsubscribe, and here that is load-bearing rather than
// merely correct. This reader accepted a ticket and consented to NOTHING (ADR
// 0046), so there is no subscription to cancel; the Follow Digest is the only
// mail a Customer can turn off (ADR 0034), and what bounds this one is
// catalog.MaxAnswerReminders. An unsubscribe footer here would imply the reader
// had signed up for something.
func TestHolderAnswerReminderCarriesNoUnsubscribe(t *testing.T) {
	text := strings.ToLower(holderAnswerReminder().Text())
	for _, forbidden := range []string{"unsubscribe", "darse de baja", "cancelar la suscripción"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("text = %q, want no unsubscribe in a transactional mail", text)
		}
	}
}

// ONE MAIL, EVERY OWED TICKET, EACH WITH ITS OWN LINK (#335). The ruling: one
// mail per Holder per sweep, listing each owed Ticket with its own Assignment
// Link. The single-ticket mail above reads as it always did; this is the shape
// the envelope takes when one person accepted two.
func TestHolderAnswerReminderListsEachTicketWithItsOwnLink(t *testing.T) {
	reminder := holderAnswerReminderForTwo()

	if got := reminder.Subject(); got != "Your tickets for Noche de Jazz still need answers" {
		t.Fatalf("subject = %q, want the plural English subject", got)
	}
	text := reminder.Text()
	for _, want := range []string{
		"The tickets you accepted still need answers",
		"Ticket: General",
		"Ticket: VIP",
		"https://storefront.test/accept?token=x",
		"https://storefront.test/accept?token=y",
		"Answering is optional and your tickets are valid either way.",
		"at most one more reminder",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	// EXACTLY ONE LINK PER TICKET AND NO OTHER. Each Assignment Link opens
	// exactly one Ticket; a mail about two carries two, never a third.
	if links := strings.Count(text, "https://"); links != 2 {
		t.Fatalf("the two-ticket reminder carries %d links, want exactly two — one Assignment Link per listed Ticket", links)
	}
	// Each link must sit in the ticket's own block, after its Ticket line, so a
	// reader cannot answer the VIP question through the General ticket's link.
	if strings.Index(text, "Ticket: General") > strings.Index(text, "token=x") ||
		strings.Index(text, "token=x") > strings.Index(text, "Ticket: VIP") ||
		strings.Index(text, "Ticket: VIP") > strings.Index(text, "token=y") {
		t.Fatalf("text = %q, want each Assignment Link listed under its own Ticket", text)
	}
}

// The Spanish two-ticket mail, same discipline: usted register, no English left.
func TestHolderAnswerReminderListsEachTicketInSpanish(t *testing.T) {
	reminder := holderAnswerReminderForTwo()
	reminder.Locale = LocaleES

	if got := reminder.Subject(); got != "Sus entradas para Noche de Jazz aún necesitan respuestas" {
		t.Fatalf("subject = %q, want the plural Spanish subject", got)
	}
	text := reminder.Text()
	for _, want := range []string{
		"Las entradas que aceptó aún necesitan respuesta",
		"Entrada: General",
		"Entrada: VIP",
		"https://storefront.test/accept?token=x",
		"https://storefront.test/accept?token=y",
		"Responder es opcional y sus entradas son válidas igualmente.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, "still need answers") || strings.Contains(text, "Ticket:") {
		t.Fatalf("text = %q, want no English left in it", text)
	}
}

// The disclosure rule does not loosen because the mail became a list: a Holder
// of two Tickets is still told the Events, the Ticket Types and their own links
// and NOTHING about the purchase (ADR 0044, ADR 0046).
func TestHolderAnswerReminderForTwoNamesNothingAboutThePurchase(t *testing.T) {
	reminder := holderAnswerReminderForTwo()
	rendered := reminder.Subject() + "\n" + reminder.Text()
	for _, forbidden := range []string{
		"bought", "purchase", "reference", "Reference",
		"compró", "compra", "referencia", "Referencia",
		"$", "Tax ID", "RUC",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("the two-ticket Holder's reminder contains %q:\n%s", forbidden, rendered)
		}
	}
}

// A single-Ticket mail reads exactly as it did before #335: the envelope only
// grew for the Holder who accepted several, and the ordinary case is untouched.
func TestHolderAnswerReminderForOneTicketReadsAsBefore(t *testing.T) {
	reminder := holderAnswerReminder()
	if got := reminder.Subject(); got != "Your ticket for Noche de Jazz still needs an answer" {
		t.Fatalf("subject = %q, want the singular subject unchanged", got)
	}
	text := reminder.Text()
	if !strings.Contains(text, "The ticket you accepted still needs an answer") ||
		!strings.Contains(text, "This link opens your ticket only") {
		t.Fatalf("text = %q, want the single-ticket wording unchanged", text)
	}
}
