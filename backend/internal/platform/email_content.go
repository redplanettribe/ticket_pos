package platform

import (
	"fmt"
	"time"
)

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

// The five Payout Request notices (#179 and #188, ADR 0026). They are the
// platform's first organizer-facing email, and they read differently from above
// for one reason: their reader is a person doing their job rather than a
// Customer who bought a ticket. No "Hi <name>" — a request records its asker as
// an email and nothing else, and a greeting to a name the platform does not know
// is worse than none.
//
// What none of them says is where the money is going. The account number, the
// bank, the holder's name and the Tax ID stay in the database; the notices say
// "the account on your Payout Profile", which is the sentence an organizer can
// act on and a stranger cannot (ADR 0026).

// Subject is the operator's submission notice subject line. It names the
// Organization and the amount, because this line is the whole of what an
// operator sees in a mailbox list on a Friday evening, and it is what decides
// whether they open the dashboard now or on Monday.
func (p PayoutRequestSubmitted) Subject() string {
	return fmt.Sprintf("%s has asked to be paid %s", p.OrganizationName, formatMoney(p.AmountCents, p.Currency))
}

// Text is the operator's submission notice body: the ask, who made it, and what
// to do about it.
//
// The note is the organizer's own words and is included because it is usually
// the reason the request is urgent; it is absent rather than blank when they
// wrote none, exactly as the receipt's optional lines are.
//
// There is no link. The staff application's public origin is not something this
// module is told (only the Storefront's is), and a wrong link in a money email
// would be worse than no link at all — the queue is one click from the operator
// dashboard either way.
func (p PayoutRequestSubmitted) Text() string {
	text := fmt.Sprintf("%s has submitted a Payout Request for %s.\n\nAsked by: %s",
		p.OrganizationName, formatMoney(p.AmountCents, p.Currency), p.RequestedBy)
	if p.Note != "" {
		text += fmt.Sprintf("\nNote: %s", p.Note)
	}
	text += "\n\nOpen the Payout Request queue on the Operator Dashboard to see the bank details and answer it."
	return text
}

// Subject is the paid notice's subject line: the answer itself, so an organizer
// who only ever reads this line still learns the money has moved.
func (p PayoutRequestPaid) Subject() string {
	return fmt.Sprintf("Your payout of %s has been sent", formatMoney(p.AmountCents, p.Currency))
}

// Text is the paid notice's body.
//
// It says the transfer has ALREADY been made rather than that it is being
// arranged, because that is what happened: the operator wires the money by hand
// and then records it, and there is no approved-but-unpaid state in between
// (ADR 0026).
//
// The one conditional sentence is the shortfall. Partial fulfilment is not
// modelled — the Payout records what moved and the request keeps what was asked
// — so this email is the only place an organizer is ever told that a transfer
// came in under their ask, and learning it from a bank statement instead is
// precisely the support thread this feature exists to remove.
func (p PayoutRequestPaid) Text() string {
	text := fmt.Sprintf("%s has been transferred to the account on your Payout Profile.\n\nThe transfer has already been made, so it will appear in your bank account as soon as your bank posts it.",
		formatMoney(p.AmountCents, p.Currency))
	if p.RequestedCents != p.AmountCents {
		text += fmt.Sprintf("\n\nYou asked for %s, and %s was sent. If you were expecting the full amount, contact the platform.",
			formatMoney(p.RequestedCents, p.Currency), formatMoney(p.AmountCents, p.Currency))
	}
	text += fmt.Sprintf("\n\nThis answers the Payout Request submitted for %s.", p.OrganizationName)
	return text
}

// Subject is the decline notice's subject line. It says the outcome outright,
// for the reason the refused-reversal notice does: an unanswered ask that reads
// as an update in a mailbox list is worse than one that reads as a no.
func (p PayoutRequestDeclined) Subject() string {
	return fmt.Sprintf("Your payout request for %s was declined", formatMoney(p.AmountCents, p.Currency))
}

// Text is the decline notice's body: what happened, why, and what can be done
// next.
//
// The reason is quoted whole and unedited. It is the operator's message to this
// Organization and the only thing standing between a decline and a support
// thread, so nothing here summarises or softens it.
//
// The closing line matters as much. A decline is a "not this" rather than a
// lockout: it ends this request and frees the Organization to ask again
// immediately, and an organizer who does not know that will write to ask
// (ADR 0026).
func (p PayoutRequestDeclined) Text() string {
	return fmt.Sprintf("The Payout Request for %s submitted for %s has been declined.\n\nReason: %s\n\nNothing has moved and no payout was made. You can submit a new request whenever you are ready — being declined once has no bearing on the next ask.",
		formatMoney(p.AmountCents, p.Currency), p.OrganizationName, p.Reason)
}

// Subject is the transfer-sent notice's subject line. It says the money is
// moving rather than that it has arrived, because the whole distinction this
// notice draws is between the two — and an organizer who reads only this line
// must not go and look at a bank account that has nothing in it yet.
func (p PayoutRequestTransferSent) Subject() string {
	return fmt.Sprintf("Your payout of %s is on its way", formatMoney(p.AmountCents, p.Currency))
}

// Text is the transfer-sent notice's body: the amount, the date it was sent, and
// the 48 hours.
//
// The DATE is what makes the rest of the message worth sending. "Up to 48 hours"
// with no starting point is a sentence an organizer cannot check, and one they
// cannot check is one they will write to ask about — which is the message this
// notice exists to prevent. With the date they can count for themselves, and the
// same date is on their payouts page for when they delete the email.
//
// It promises a further email, and that promise is kept on both branches: the
// paid notice when the money lands, the failed one when it comes back. Neither
// state is one the organizer has to poll a page for.
func (p PayoutRequestTransferSent) Text() string {
	return fmt.Sprintf("The transfer of %s to the account on your Payout Profile was submitted on %s.\n\nBank transfers can take up to 48 hours to arrive, so it may not show in your account straight away. We will email you again as soon as we know it has landed.\n\nThis answers the Payout Request submitted for %s.",
		formatMoney(p.AmountCents, p.Currency), formatEcuadorDate(p.SubmittedAt), p.OrganizationName)
}

// Subject is the transfer-failed notice's subject line. "Could not be completed"
// rather than "was refused": the bank sent the money back, and a subject line
// that reads as a judgement is one the organizer answers with an appeal instead
// of a corrected account number.
func (p PayoutRequestTransferFailed) Subject() string {
	return fmt.Sprintf("Your payout of %s could not be completed", formatMoney(p.AmountCents, p.Currency))
}

// Text is the transfer-failed notice's body: what the bank did, why, and the one
// thing to go and fix.
//
// This is the only notice of the five the organizer must ACT on, and the body is
// built around that. The reason is quoted whole and unedited, exactly as the
// decline's is. The sentence after it says outright that this was not a
// decision — `failed` and `declined` share a column and must never share a
// sentence, and an organizer who believes the platform judged them over a typo
// corrects nothing. And the Payout Profile is named, because a wrong account
// number is the commonest cause and this request's copy of it is a frozen
// snapshot: the fix is on the profile, and the next attempt is a new ask.
func (p PayoutRequestTransferFailed) Text() string {
	return fmt.Sprintf("The transfer of %s for %s was submitted to your bank and came back.\n\nReason: %s\n\nThis was not a decision about your request — the transfer was sent and your bank did not accept it. No payout was made and nothing has left your balance.\n\nCheck the details on your Payout Profile; a rejected transfer is most often a wrong account number. Once they are right, submit a new request — this one cannot be retried, because it carries a frozen copy of the details it was sent with.",
		formatMoney(p.AmountCents, p.Currency), p.OrganizationName, p.Reason)
}

// formatEcuadorDate renders an instant as the calendar date it falls on in
// Ecuador — "20 July 2026".
//
// The zone conversion is the point, for the reason StartOfEcuadorDay exists: a
// transfer submitted at 03:00 UTC was submitted the previous evening in
// Guayaquil, and a notice telling an organizer their money left on a day it did
// not is worse than one giving no date at all, since the whole job of the date
// is to let them count 48 hours from it.
func formatEcuadorDate(instant time.Time) string {
	return instant.In(ecuadorLocation()).Format("2 January 2006")
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
