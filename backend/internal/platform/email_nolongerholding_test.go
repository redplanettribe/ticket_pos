package platform

import (
	"strings"
	"testing"
)

// What the No Longer Holding mail actually says (#327, parent #322, ADR 0046).
//
// These assert on the RENDERED words, on email_ticketassignment_test.go's terms:
// a test that only checked a Locale was carried would pass against a message
// that was never translated. It matters especially here because this message has
// to be TRUE OF TWO DIFFERENT SITUATIONS — the buyer reassigned the Ticket, or
// the Ticket Sale was reversed — and the way that stays true is that the words
// never mention either. The forbidden-words test below is the real subject of
// this file.

func noLongerHolding() NoLongerHolding {
	return NoLongerHolding{
		To:        "carla@example.com",
		EventName: "Noche de Jazz",
	}
}

// The zero Locale is English, the floor of ADR 0033's chain.
func TestNoLongerHoldingIsWrittenInEnglishWhenNothingNamedALanguage(t *testing.T) {
	mail := noLongerHolding()

	if got := mail.Subject(); got != "You no longer have a ticket for Noche de Jazz" {
		t.Fatalf("subject = %q, want the English subject", got)
	}
	text := mail.Text()
	for _, want := range []string{
		// The fact, first, and naming the Event — a reader who skims must learn
		// the one thing that would otherwise send them to a door they cannot get
		// through.
		"You are no longer holding a ticket for Noche de Jazz.",
		// Why they are hearing from the platform at all: they accepted. This is
		// also the sentence that explains why somebody who never accepted gets
		// nothing — there would be no true version of it to write them.
		"because you accepted that ticket",
		// The Event leaves their Customer Area, said in the message as well as
		// done in the read, so the two agree.
		"removed from your account",
		// Nothing is asked of them. There is no link and no button in this
		// message, so a call to action would be an instruction with no target.
		"There is nothing you need to do.",
		// THE HOLDER STAYS A CUSTOMER, with the name and the Answers they gave.
		// An acceptance criterion of #327, and the kindest true thing available
		// to somebody who has just been told they lost something.
		"Your account stays as it is",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
}

// Written in the recipient's Mail Locale, resolved recipient-first by the caller
// because this reader is not party to the sale (ADR 0033, service.assignmentMail
// Locale).
//
// It pins the REGISTER as well as the language: usted, matching every other
// Customer-facing message here.
func TestNoLongerHoldingIsWrittenInSpanish(t *testing.T) {
	mail := noLongerHolding()
	mail.Locale = LocaleES

	if got := mail.Subject(); got != "Ya no tiene una entrada para Noche de Jazz" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}
	text := mail.Text()
	for _, want := range []string{
		"Ya no tiene una entrada para Noche de Jazz.",
		"porque usted aceptó esa entrada",
		"La hemos quitado de su cuenta",
		"No tiene que hacer nada.",
		"Su cuenta sigue igual",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	// usted and never tú.
	for _, forbidden := range []string{"tienes", "tu entrada", "puedes", "tu cuenta"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Errorf("text uses the tú form (%q); every Customer-facing message here is usted", forbidden)
		}
	}
}

// THE MAIL GIVES NO CAUSE AND NAMES NO BUYER, AND THIS IS THE TEST THIS FILE
// EXISTS FOR.
//
// #327's whole premise is that a Holder cannot tell a reassignment from a Sale
// Reversal, because from where they sit the two are the same event. Every word
// below would break that: "cancelled", "reversed" and "refunded" describe one
// cause, "reassigned" and "given to" describe the other, and each is a false
// statement in the case it does not describe.
//
// Naming the buyer is worse than merely untrue — it is a disclosure. Who bought
// the ticket and what they chose to do with it are facts about somebody else's
// purchase, and ADR 0044's rule (carried over unchanged by ADR 0046) says a
// Holder never learns them. Mail gets forwarded.
//
// It is enforced structurally too: NoLongerHolding has nowhere to put a name, a
// cause, a price or a reference. This test is what fails if somebody gives it
// somewhere — or, more likely, if somebody writes a cause into the copy because
// the message felt too abrupt without one.
func TestNoLongerHoldingGivesNoCauseAndNamesNoBuyer(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		mail := noLongerHolding()
		mail.Locale = locale
		rendered := strings.ToLower(mail.Subject() + "\n" + mail.Text())

		for _, forbidden := range []string{
			// The reversal cause.
			"cancel", "cancel", "revers", "anulad", "refund", "reembols", "devolv",
			// The reassignment cause.
			"reassign", "reasign", "someone else", "otra persona", "transferr", "transferid",
			// The buyer, however obliquely.
			"buyer", "comprador", "the person who bought", "quien la compró", "your friend", "su amigo",
			// The purchase itself.
			"$", "usd", "tp-", "cedula", "cédula", "ruc", "tax", "reference", "referencia",
		} {
			if strings.Contains(rendered, forbidden) {
				t.Errorf("the %s No Longer Holding mail contains %q.\n"+
					"It names the Event and nothing else: no cause, because one message must be true of\n"+
					"BOTH a reassignment and a Sale Reversal, and no buyer, because who bought the ticket\n"+
					"and what they decided is a fact about somebody else's purchase (ADR 0044, ADR 0046).",
					locale, forbidden)
			}
		}
	}
}

// THE MESSAGE CARRIES NO LINK, in either language.
//
// A deliberate absence rather than an unfinished one: there is nothing left for
// this reader to press. The Ticket is not theirs, the Event has left their
// Customer Area, and their Assignment Link stopped opening at the same moment.
// A button here would be an invitation to a refusal.
func TestNoLongerHoldingOffersNothingToPress(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		mail := noLongerHolding()
		mail.Locale = locale
		if strings.Contains(mail.Text(), "http") {
			t.Errorf("the %s No Longer Holding mail carries a link; there is nothing left for its reader to press", locale)
		}
	}
}
