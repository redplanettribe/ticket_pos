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
	return fmt.Sprintf("Your %s purchase has been cancelled", v.EventName)
}

// Text is the void notice's body. It quotes the original Sale Confirmation
// reference so the Customer can reconcile it against the receipt they were given.
func (v SaleVoided) Text() string {
	return fmt.Sprintf("Hi %s,\n\nYour purchase for %s (reference %s) has been cancelled.\nIf you believe this is a mistake, contact the organizer.",
		v.CustomerName, v.EventName, v.Reference)
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
