package platform

import "fmt"

// What a Sale Confirmation actually says, kept apart from the provider that
// delivers it.
//
// The wording used to live inside ResendEmailSender, which put it out of reach
// of the test that most needs it: the CaptureEmailSender the integration harness
// injects records the SaleConfirmation value and never renders it, so nothing
// driving the API over HTTP could assert what a buyer reads. Content is a
// property of the message rather than of the transport, so it lives on the
// message — one composition, rendered identically by the provider in production
// and by the integration suite asserting on a captured receipt.

// Subject is the Sale Confirmation's subject line.
func (c SaleConfirmation) Subject() string {
	return fmt.Sprintf("Your tickets for %s", c.EventName)
}

// Text is the Sale Confirmation's plain-text body: what the Customer keeps as
// their receipt.
//
// Two parts are conditional, and both are absent rather than blank when they do
// not apply — an empty label on a receipt reads as a fault in the platform, and
// there is nothing a Customer could do about it either way.
func (c SaleConfirmation) Text() string {
	// The total is the amount the Customer was charged, all in: the platform's
	// fee is never itemized on a receipt (ADR 0014).
	text := fmt.Sprintf("Hi %s,\n\nYour purchase for %s is confirmed.\nReference: %s\nTotal paid: %s",
		c.CustomerName, c.EventName, c.Reference, formatMoney(c.AmountCents, c.Currency))

	// The Tax ID sits with the reference and the total because it belongs to the
	// same job those two do: this email is the document the buyer files for
	// their own expense records (#99).
	if taxID := c.TaxID.Display(); taxID != "" {
		text += "\n" + taxID
	}

	text += "\n\nPresent this reference at the event."

	// The Confirmation Link is the reason this email is worth keeping: it opens
	// this purchase months later, at the gate, with one tap and no typing.
	if c.ConfirmationLink != "" {
		text += fmt.Sprintf("\n\nView your tickets:\n%s\n\nThis link opens this purchase only, and stays valid until shortly after the event.", c.ConfirmationLink)
	}
	return text
}

// Subject is the void notice's subject line.
func (v SaleVoided) Subject() string {
	return fmt.Sprintf("Your %s purchase has been reversed", v.EventName)
}

// Text is the void notice's body. It quotes the original Sale Confirmation
// reference so the Customer can reconcile it against the receipt they were given.
//
// The wording has to be true for both actors, because one notice serves both
// Sale Reversal paths: the Customer who pressed Undo themselves, and the Sale
// Import undo they had no part in. "Reversed" is the glossary's word — the
// avoid list rules out cancelled, voided and refunded, and "cancelled" would
// also read as the Event having been called off, which is a different thing
// entirely. For the same reason the closing line asks whether they expected
// this rather than whether it was a mistake: a buyer who just pressed Undo did
// not make one.
func (v SaleVoided) Text() string {
	return fmt.Sprintf("Hi %s,\n\nYour purchase for %s (reference %s) has been reversed, and those tickets are no longer valid.\nIf you did not expect this, contact the organizer.",
		v.CustomerName, v.EventName, v.Reference)
}

// Subject is the refused-reversal notice's subject line. It says what happened
// in the subject itself, because a Customer who closed the tab may only ever
// read this line.
func (r SaleReversalRefused) Subject() string {
	return fmt.Sprintf("We could not undo your %s purchase", r.EventName)
}

// Text is the refused-reversal notice's body: the correction to a promise the
// platform made and could not keep.
//
// Three things are said and one is refused. What happened, that THE TICKETS ARE
// STILL VALID — the sentence that decides whether this reader turns up at the
// gate, and the reason it comes before anything else — and who to talk to, named
// by the Sale Confirmation reference they can quote.
//
// What it refuses to say is WHY, for the reason ADR 0018 settled: there is no
// provider answer that means "too late", so a refusal cannot be explained. It
// offers no cause at all rather than a hedged one — "this can happen when…"
// reads as a cause to the person it is guessed at.
//
// It also does not apologise for a delay or mention that anything was pending.
// The reader may have pressed Undo a minute ago or a day ago, and the platform's
// own timeline is not the thing they need from this email.
func (r SaleReversalRefused) Text() string {
	return fmt.Sprintf("Hi %s,\n\nWe could not undo your purchase for %s (reference %s).\n\nYour tickets are still valid — nothing has changed about your purchase, and you can still use them.\n\nIf you need help with this purchase, contact the organizer and quote the reference above.",
		r.CustomerName, r.EventName, r.Reference)
}

// formatMoney renders integer cents for a receipt line: two decimals with the
// currency code alongside, e.g. "17.82 USD". Deliberately plain — the email is
// text, and a Customer reconciling against a card statement needs the number,
// not a locale.
func formatMoney(cents int, currency string) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d %s", sign, cents/100, cents%100, currency)
}
