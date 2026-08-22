package platform

import (
	"strings"
	"testing"
)

// What the Assignment mail actually says (#325, parent #322, ADR 0046).
//
// These assert on the RENDERED words, on email_answerreminder_test.go's terms: a
// test that only checked a Locale was carried would pass against a message that
// was never translated. It matters more for this message than for any other on
// the platform, because THIS ONE GOES TO SOMEBODY WHO HAS NEVER BEEN HERE. There
// is no prior relationship to lean on, no account, and no page they were just
// reading — the text below is the entire relationship.

func ticketAssignment() TicketAssignment {
	return TicketAssignment{
		To:             "carla@example.com",
		EventName:      "Noche de Jazz",
		TicketTypeName: "General Admission",
		AcceptURL:      "https://storefront.test/accept?token=abc.def",
	}
}

// The zero Locale is English, which is the floor of ADR 0033's chain and what a
// Holder gets when neither they nor the sale named a language.
func TestTicketAssignmentIsWrittenInEnglishWhenNothingNamedALanguage(t *testing.T) {
	mail := ticketAssignment()

	if got := mail.Subject(); got != "You have a ticket for Noche de Jazz" {
		t.Fatalf("subject = %q, want the English subject", got)
	}
	text := mail.Text()
	for _, want := range []string{
		// Where the address came from. The reader gave it to nobody, so the
		// message says how it got here before it asks for anything.
		"gave us your email address",
		"Event: Noche de Jazz",
		"Ticket: General Admission",
		// WHAT ACCEPTING DISCLOSES, and it is an acceptance criterion of #325
		// rather than a nicety: the Organization is a separate controller, and
		// somebody deciding whether to click is entitled to know before they do.
		"your email address is shared with the organizer",
		"https://storefront.test/accept?token=abc.def",
		// Ignoring is a real option with no consequence, which is the whole of
		// how a Holder declines — there is no decline button anywhere (ADR 0046).
		"You do not have to do anything.",
		"this link stops working when the event starts",
		"we delete your email address then",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
}

// Written in the recipient's Mail Locale, which is an acceptance criterion of
// #325 and the reason ADR 0033 exists.
//
// It pins the REGISTER as well as the language: usted, matching every other
// Customer-facing message here, because a tú-form rewrite would pass any test
// that only checked the language had branched.
func TestTicketAssignmentIsWrittenInSpanish(t *testing.T) {
	mail := ticketAssignment()
	mail.Locale = LocaleES

	if got := mail.Subject(); got != "Tiene una entrada para Noche de Jazz" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}
	text := mail.Text()
	for _, want := range []string{
		"nos dio su dirección de correo",
		"Evento: Noche de Jazz",
		"Entrada: General Admission",
		"su dirección de correo se comparte con la organización",
		"https://storefront.test/accept?token=abc.def",
		"No tiene que hacer nada.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	// usted and never tú. "tienes" or "tu entrada" would be a register slip that
	// a language check alone cannot see.
	for _, forbidden := range []string{"tienes", "tu entrada", "puedes"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Errorf("text uses the tú form (%q); every Customer-facing message here is usted", forbidden)
		}
	}
}

// THE MAIL NAMES NO BUYER, NO PRICE, NO TAX ID AND NO CONFIRMATION REFERENCE.
//
// This is the disclosure rule of ADR 0044 carried into an inbox, and it is
// asserted on the rendered bytes rather than on the struct's fields because a
// struct with no buyer field is only half the property: somebody could compose
// one into the copy — "Ana bought you a ticket" reads friendlier and is exactly
// the widening ADR 0046 forbids. The reader is owed the fact that they have a
// ticket, not the identity of the person who bought it.
//
// It is enforced structurally too: TicketAssignment simply has nowhere to put
// any of these. This test is what fails if somebody gives it somewhere.
func TestTicketAssignmentNamesNoBuyerNoPriceAndNoReference(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		mail := ticketAssignment()
		mail.Locale = locale
		rendered := mail.Subject() + "\n" + mail.Text()

		// Words that would only appear if somebody had widened the message.
		for _, forbidden := range []string{
			"$", "USD", "TP-", "cedula", "cédula", "RUC", "Tax", "reference", "Referencia",
		} {
			if strings.Contains(rendered, forbidden) {
				t.Errorf("the %s Assignment mail contains %q.\n"+
					"It may name the Event and the Ticket Type and nothing else about the purchase:\n"+
					"never the buyer, the price, the Tax ID or the Sale Confirmation reference (ADR 0046).",
					locale, forbidden)
			}
		}
	}
}

// THE LINK IS THE MESSAGE. Nothing here is conditional, so a caller that could
// not sign a token must send nothing at all rather than composing a message
// whose reader has no way to act on it — see the catalog service's
// mailTicketAssignment, which is where that refusal lives.
//
// This test states the consequence rather than the rule: rendered without a
// link, the message still tells somebody they have a ticket and then offers them
// nowhere to go, which is why the composition site refuses.
func TestTicketAssignmentWithoutALinkIsAnInstructionNobodyCanFollow(t *testing.T) {
	mail := ticketAssignment()
	mail.AcceptURL = ""

	if strings.Contains(mail.Text(), "Accept your ticket here:\nhttps") {
		t.Fatal("a linkless Assignment mail rendered a link")
	}
	if !strings.Contains(mail.Text(), "Accept your ticket here:") {
		t.Fatal("the copy is unconditional by design; a caller must refuse to compose a linkless message rather than the renderer hiding the gap")
	}
}
