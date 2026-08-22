package platform

import (
	"strings"
	"testing"
)

// What an Answer Reminder actually says (#317, ADR 0044; #328, ADR 0046; #347,
// ADR 0049).
//
// These assert on the RENDERED words, on email_saleconfirmation_test.go's terms:
// a test that only checked a Locale was carried would pass just as happily
// against a message that was never translated. This mail is sent by a job
// nobody is watching, weeks after the sale, so the rendered text is the only
// place its wording is ever inspected — and it carries one extra burden: this
// reader is entitled to almost nothing about the purchase, so what is ABSENT
// is as much under test as what is present.
//
// THERE IS ONE REMINDER SINCE ADR 0049. The buyer-addressed message with its
// Sale Confirmation reference and Confirmation Link is gone; the buyer reads
// this one, about their own Self-held Ticket, exactly as any Holder does.

const customerAreaURL = "https://storefront.test/tickets"

func holderAnswerReminder() HolderAnswerReminder {
	return HolderAnswerReminder{
		To: "carla@example.com",
		Tickets: []HolderAnswerReminderTicket{{
			EventName:      "Noche de Jazz",
			TicketTypeName: "General",
		}},
		CustomerAreaURL: customerAreaURL,
	}
}

// holderAnswerReminderForTwo is the mail #335 ruled into existence: one Holder,
// one sweep, two owed Tickets, ONE message listing both. Mailing one address
// twice in one sweep is the shape spam filters punish.
func holderAnswerReminderForTwo() HolderAnswerReminder {
	return HolderAnswerReminder{
		To: "carla@example.com",
		Tickets: []HolderAnswerReminderTicket{
			{EventName: "Noche de Jazz", TicketTypeName: "General"},
			{EventName: "Noche de Jazz", TicketTypeName: "VIP"},
		},
		CustomerAreaURL: customerAreaURL,
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
		"Your ticket still needs an answer",
		"Event: Noche de Jazz",
		"Ticket: General",
		"Answer from your tickets page:\n" + customerAreaURL,
		"Sign in there with this email address",
		"until the event starts",
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
		"Su entrada aún necesita respuesta",
		"Evento: Noche de Jazz",
		"Entrada: General",
		"Responda desde su página de entradas:\n" + customerAreaURL,
		"Inicie sesión allí con esta dirección de correo",
		"Responder es opcional y su entrada es válida igualmente.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	// No English may survive into the Spanish message. "still needs an answer" is
	// the phrase that would leak first if a line were ever added in one language
	// only, since it is the sentence the mail is about.
	if strings.Contains(text, "still needs an answer") || strings.Contains(text, "Sign in") {
		t.Fatalf("text = %q, want no English left in it", text)
	}
}

// IT NAMES NOTHING ABOUT THE PURCHASE, and this is the test to keep passing.
//
// The struct has no field for a buyer, a price, a Tax ID or a Sale Confirmation
// reference, which is the real enforcement — but a template is edited by hand
// and a greeting or a "bought for you by" line would be a one-line change. ADR
// 0044's disclosure rule, carried over unchanged by ADR 0046 and applied to an
// inbox: a Holder sees the Event, the Ticket Type and where to sign in. Being a
// Verified Customer of this platform buys nobody a fact about somebody else's
// purchase.
//
// "ACCEPTED" IS FORBIDDEN TOO, since ADR 0049: the buyer reads this about a
// Self-held Ticket they accepted nothing for, and a sentence that was true
// only for one of the two readers would be wrong in somebody's inbox.
func TestHolderAnswerReminderNamesNothingAboutThePurchase(t *testing.T) {
	rendered := holderAnswerReminder().Subject() + "\n" + holderAnswerReminder().Text()
	for _, forbidden := range []string{
		"bought", "purchase", "reference", "Reference", "accepted",
		"compró", "compra", "referencia", "Referencia", "aceptó",
		"$", "Tax ID", "RUC",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("the Answer Reminder contains %q:\n%s\n"+
				"It names the Event, the Ticket Type and the Customer Area and nothing else (ADR 0044, ADR 0046, ADR 0049).", forbidden, rendered)
		}
	}
}

// ONE URL AND ONLY ONE, and it is the Customer Area (ADR 0049).
//
// Not an Assignment Link, which is a credential that mints an identity and
// belongs in the Assignment mail where accepting is the point of clicking; not
// an Answer Link, which is retired; not a Confirmation Link, which opens a
// whole Ticket Sale this reader may never see. The one address here signs
// nothing: a forwarded copy opens nothing.
func TestHolderAnswerReminderCarriesTheCustomerAreaLinkAndNoOther(t *testing.T) {
	text := holderAnswerReminder().Text()
	if links := strings.Count(text, "https://"); links != 1 {
		t.Fatalf("the Answer Reminder carries %d links, want exactly one — the Customer Area and nothing else", links)
	}
	for _, forbidden := range []string{"/confirm", "/accept", "token="} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("the Answer Reminder carries %q: the only address in it is the Customer Area, which is not a credential.\n%s", forbidden, text)
		}
	}
}

// It names no figure: the debt is derived live and a count baked into an inbox
// is wrong the moment somebody answers one. The copy speaks of "an answer" and
// never of how many.
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

// ONE MAIL, EVERY OWED TICKET, ONE ADDRESS (#335, ADR 0049). The ruling: one
// mail per Holder per sweep, listing each owed Ticket. Since the reader
// answers from a panel that shows everything they hold, the list is followed
// by the one Customer Area address rather than a link per Ticket.
func TestHolderAnswerReminderListsEachTicketAndOneAddress(t *testing.T) {
	reminder := holderAnswerReminderForTwo()

	if got := reminder.Subject(); got != "Your tickets for Noche de Jazz still need answers" {
		t.Fatalf("subject = %q, want the plural English subject", got)
	}
	text := reminder.Text()
	for _, want := range []string{
		"Your tickets still need answers",
		"Ticket: General",
		"Ticket: VIP",
		"Answer from your tickets page:\n" + customerAreaURL,
		"Answering is optional and your tickets are valid either way.",
		"at most one more reminder",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if links := strings.Count(text, "https://"); links != 1 {
		t.Fatalf("the two-ticket reminder carries %d links, want exactly one — the Customer Area shows every Ticket the reader holds", links)
	}
	// The address follows the list, so a reader finds what is owed before where
	// to go.
	if strings.Index(text, "Ticket: General") > strings.Index(text, "Ticket: VIP") ||
		strings.Index(text, "Ticket: VIP") > strings.Index(text, customerAreaURL) {
		t.Fatalf("text = %q, want both Tickets listed before the Customer Area address", text)
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
		"Sus entradas aún necesitan respuesta",
		"Entrada: General",
		"Entrada: VIP",
		"Responda desde su página de entradas:\n" + customerAreaURL,
		"Responder es opcional y sus entradas son válidas igualmente.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, "still need answers") || strings.Contains(text, "Ticket:") || strings.Contains(text, "Sign in") {
		t.Fatalf("text = %q, want no English left in it", text)
	}
}

// Listed Tickets spanning two Events cannot share a subject that names one, so
// the subject names none and each block names its own.
func TestHolderAnswerReminderAcrossTwoEventsHeadlinesNeither(t *testing.T) {
	reminder := holderAnswerReminderForTwo()
	reminder.Tickets[1].EventName = "Feria del Libro"

	if got := reminder.Subject(); got != "Your tickets still need answers" {
		t.Fatalf("subject = %q, want the Event-less plural subject", got)
	}
	text := reminder.Text()
	if !strings.Contains(text, "Event: Noche de Jazz") || !strings.Contains(text, "Event: Feria del Libro") {
		t.Fatalf("text = %q, want each Ticket's own Event named", text)
	}
}

// The disclosure rule does not loosen because the mail became a list: a Holder
// of two Tickets is still told the Events, the Ticket Types and where to sign in
// and NOTHING about the purchase (ADR 0044, ADR 0046).
func TestHolderAnswerReminderForTwoNamesNothingAboutThePurchase(t *testing.T) {
	reminder := holderAnswerReminderForTwo()
	rendered := reminder.Subject() + "\n" + reminder.Text()
	for _, forbidden := range []string{
		"bought", "purchase", "reference", "Reference", "accepted",
		"compró", "compra", "referencia", "Referencia", "aceptó",
		"$", "Tax ID", "RUC",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("the two-ticket Answer Reminder contains %q:\n%s", forbidden, rendered)
		}
	}
}
