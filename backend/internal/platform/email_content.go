package platform

import (
	"fmt"
	"strings"
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

// EVERY MESSAGE IN THIS FILE IS WRITTEN IN THE READER'S OWN LANGUAGE. There is
// no English section and no staff exception: the copy machinery below is the
// whole of it, and a message that did not go through translated() would be a
// message one of this platform's two audiences cannot read.

// The five Payout Request notices (#179 and #188, ADR 0026). They are the
// platform's first organizer-facing email, and they read differently from the
// Customer mail below for one reason: their reader is a person doing their job
// rather than a Customer who bought a ticket. No "Hi <name>" — a request records
// its asker as an email and nothing else, and a greeting to a name the platform
// does not know is worse than none.
//
// THAT RECORDED EMAIL IS ALSO WHAT MAKES THEM BILINGUAL (#285, ADR 0041). ADR
// 0033 stopped here deliberately, because a notice addressed to
// `request.RequestedBy` was "attached to no record that could hold a language".
// The Staff Locale is keyed on the email address, so that string is now exactly
// such a record: the caller reads the row by recipient address and hands the
// answer down as Locale, with English as the floor. The obstacle dissolved
// rather than being overruled — nothing about the addressing changed.
//
// What none of them says is where the money is going. The account number, the
// bank, the holder's name and the Tax ID stay in the database; the notices say
// "the account on your Payout Profile", which is the sentence an organizer can
// act on and a stranger cannot (ADR 0026).
//
// The Spanish is usted throughout and takes its nouns from CONTEXT.md's staff
// vocabulary: an Organización asks to be paid, never an Organizador, which is
// the same entity's PUBLIC word and belongs to Customer surfaces alone.
var (
	payoutSubmittedSubjectCopy = translated(
		"%s has asked to be paid %s",
		"%s ha solicitado un pago de %s",
	)
	// The ask and its asker, declared whole in both languages: the sentence and
	// the line under it are one block of prose, and splitting them would let a
	// translator reorder half of it.
	payoutSubmittedOpeningCopy = translated(
		"%s has submitted a Payout Request for %s.\n\nAsked by: %s",
		"%s ha enviado una solicitud de pago de %s.\n\nSolicitado por: %s",
	)
	payoutSubmittedNoteCopy = translated(
		"\nNote: %s",
		"\nNota: %s",
	)
	payoutSubmittedActionCopy = translated(
		"\n\nOpen the Payout Request queue on the Operator Dashboard to see the bank details and answer it.",
		"\n\nAbra la cola de solicitudes de pago en el Panel de Operador para ver los datos bancarios y responderla.",
	)
)

// Subject is the operator's submission notice subject line. It names the
// Organization and the amount, because this line is the whole of what an
// operator sees in a mailbox list on a Friday evening, and it is what decides
// whether they open the dashboard now or on Monday.
func (p PayoutRequestSubmitted) Subject() string {
	return fmt.Sprintf(payoutSubmittedSubjectCopy.in(p.Locale), p.OrganizationName, formatMoney(p.AmountCents, p.Currency))
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
	text := fmt.Sprintf(payoutSubmittedOpeningCopy.in(p.Locale),
		p.OrganizationName, formatMoney(p.AmountCents, p.Currency), p.RequestedBy)
	if p.Note != "" {
		text += fmt.Sprintf(payoutSubmittedNoteCopy.in(p.Locale), p.Note)
	}
	text += payoutSubmittedActionCopy.in(p.Locale)
	return text
}

var (
	payoutPaidSubjectCopy = translated(
		"Your payout of %s has been sent",
		"Su pago de %s fue enviado",
	)
	payoutPaidOpeningCopy = translated(
		"%s has been transferred to the account on your Payout Profile.\n\nThe transfer has already been made, so it will appear in your bank account as soon as your bank posts it.",
		"Se transfirieron %s a la cuenta de su Perfil de Pagos.\n\nLa transferencia ya se realizó, así que aparecerá en su cuenta bancaria en cuanto su banco la registre.",
	)
	// The shortfall, which is the only conditional sentence of the five and the
	// only place an organizer is ever told a transfer came in under their ask.
	payoutPaidShortfallCopy = translated(
		"\n\nYou asked for %s, and %s was sent. If you were expecting the full amount, contact the platform.",
		"\n\nUsted solicitó %s y se enviaron %s. Si esperaba el monto completo, contacte a la plataforma.",
	)
	payoutPaidClosingCopy = translated(
		"\n\nThis answers the Payout Request submitted for %s.",
		"\n\nEsto responde a la solicitud de pago enviada para %s.",
	)
)

// Subject is the paid notice's subject line: the answer itself, so an organizer
// who only ever reads this line still learns the money has moved.
func (p PayoutRequestPaid) Subject() string {
	return fmt.Sprintf(payoutPaidSubjectCopy.in(p.Locale), formatMoney(p.AmountCents, p.Currency))
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
	text := fmt.Sprintf(payoutPaidOpeningCopy.in(p.Locale), formatMoney(p.AmountCents, p.Currency))
	if p.RequestedCents != p.AmountCents {
		text += fmt.Sprintf(payoutPaidShortfallCopy.in(p.Locale),
			formatMoney(p.RequestedCents, p.Currency), formatMoney(p.AmountCents, p.Currency))
	}
	text += fmt.Sprintf(payoutPaidClosingCopy.in(p.Locale), p.OrganizationName)
	return text
}

var (
	payoutDeclinedSubjectCopy = translated(
		"Your payout request for %s was declined",
		"Su solicitud de pago de %s fue rechazada",
	)
	// One block in both languages: the reason is quoted between two sentences
	// that only make sense around it, and the closing line — that a decline ends
	// this request and frees the Organization to ask again — is the half an
	// organizer who does not read it will write to ask about.
	payoutDeclinedTextCopy = translated(
		"The Payout Request for %s submitted for %s has been declined.\n\nReason: %s\n\nNothing has moved and no payout was made. You can submit a new request whenever you are ready — being declined once has no bearing on the next ask.",
		"La solicitud de pago de %s enviada para %s fue rechazada.\n\nMotivo: %s\n\nNo se movió nada y no se realizó ningún pago. Puede enviar una nueva solicitud cuando quiera: que una haya sido rechazada no tiene ninguna consecuencia sobre la siguiente.",
	)
)

// Subject is the decline notice's subject line. It says the outcome outright,
// for the reason the refused-reversal notice does: an unanswered ask that reads
// as an update in a mailbox list is worse than one that reads as a no.
func (p PayoutRequestDeclined) Subject() string {
	return fmt.Sprintf(payoutDeclinedSubjectCopy.in(p.Locale), formatMoney(p.AmountCents, p.Currency))
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
	return fmt.Sprintf(payoutDeclinedTextCopy.in(p.Locale),
		formatMoney(p.AmountCents, p.Currency), p.OrganizationName, p.Reason)
}

var (
	payoutTransferSentSubjectCopy = translated(
		"Your payout of %s is on its way",
		"Su pago de %s está en camino",
	)
	// The amount, the date and the 48 hours in one block, in both languages: the
	// expectation is meaningless without the date it is counted from, and a
	// translation that carried one without the other would be worse than none.
	payoutTransferSentTextCopy = translated(
		"The transfer of %s to the account on your Payout Profile was submitted on %s.\n\nBank transfers can take up to 48 hours to arrive, so it may not show in your account straight away. We will email you again as soon as we know it has landed.\n\nThis answers the Payout Request submitted for %s.",
		"La transferencia de %s a la cuenta de su Perfil de Pagos se envió el %s.\n\nLas transferencias bancarias pueden tardar hasta 48 horas en llegar, así que puede que no aparezca en su cuenta de inmediato. Le escribiremos de nuevo apenas sepamos que llegó.\n\nEsto responde a la solicitud de pago enviada para %s.",
	)
)

// Subject is the transfer-sent notice's subject line. It says the money is
// moving rather than that it has arrived, because the whole distinction this
// notice draws is between the two — and an organizer who reads only this line
// must not go and look at a bank account that has nothing in it yet.
func (p PayoutRequestTransferSent) Subject() string {
	return fmt.Sprintf(payoutTransferSentSubjectCopy.in(p.Locale), formatMoney(p.AmountCents, p.Currency))
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
	return fmt.Sprintf(payoutTransferSentTextCopy.in(p.Locale),
		formatMoney(p.AmountCents, p.Currency), formatEcuadorDate(p.SubmittedAt, p.Locale), p.OrganizationName)
}

var (
	payoutTransferFailedSubjectCopy = translated(
		"Your payout of %s could not be completed",
		"No se pudo completar su pago de %s",
	)
	// One block in both languages, because the sentence AFTER the reason is what
	// stops this reading as a refusal, and a translation that dropped it would
	// leave a Spanish reader believing the platform judged them over a typo.
	//
	// The Spanish says "no la aceptó" of the bank and never a word that judges the
	// ask: `failed` and `declined` share a column and must not share a sentence in
	// either language (ADR 0026 amendment).
	payoutTransferFailedTextCopy = translated(
		"The transfer of %s for %s was submitted to your bank and came back.\n\nReason: %s\n\nThis was not a decision about your request — the transfer was sent and your bank did not accept it. No payout was made and nothing has left your balance.\n\nCheck the details on your Payout Profile; a rejected transfer is most often a wrong account number. Once they are right, submit a new request — this one cannot be retried, because it carries a frozen copy of the details it was sent with.",
		"La transferencia de %s para %s se envió a su banco y volvió.\n\nMotivo: %s\n\nEsto no es una decisión sobre su solicitud: la transferencia se envió y su banco no la aceptó. No se realizó ningún pago y nada salió de su saldo.\n\nRevise los datos de su Perfil de Pagos; una transferencia devuelta suele deberse a un número de cuenta incorrecto. Cuando estén correctos, envíe una nueva solicitud: esta no se puede reintentar, porque lleva una copia congelada de los datos con los que se envió.",
	)
)

// Subject is the transfer-failed notice's subject line. "Could not be completed"
// rather than "was refused": the bank sent the money back, and a subject line
// that reads as a judgement is one the organizer answers with an appeal instead
// of a corrected account number.
func (p PayoutRequestTransferFailed) Subject() string {
	return fmt.Sprintf(payoutTransferFailedSubjectCopy.in(p.Locale), formatMoney(p.AmountCents, p.Currency))
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
	return fmt.Sprintf(payoutTransferFailedTextCopy.in(p.Locale),
		formatMoney(p.AmountCents, p.Currency), p.OrganizationName, p.Reason)
}

// The copy machinery every message above and below goes through.
//
// There was a line here once, drawn by ADR 0033: Customer mail below it was
// written in the reader's language and staff mail above it was English, because
// no Member had a language recorded anywhere and the staff app had none either.
// Both supports were removed by #281, and the line went with them — a Payout
// Request notice is now looked up by the recipient's address in the Staff Locale
// exactly as a receipt is resolved from the sale (ADR 0041). Nothing this file
// composes is English by decision any more.
//
// THE BRANCHING IS A LOOKUP PER SENTENCE, never two whole methods. Two methods
// drift, and the drift shows up as a Spanish reader missing a line an English
// reader gets.

// mailCopy is one sentence in every language this platform writes. Every piece
// of localized copy in this file is declared as one of these, so a line added in
// English cannot be shipped without its Spanish, and the two are read side by
// side rather than a screen apart.
//
// It began as the Digest's own type (ADR 0030) and is now the whole file's
// (ADR 0033): the Digest is no longer the only message with a reader whose
// language the platform knows, and a second copy of this machinery per message
// would be a second place for a language to go missing.
type mailCopy struct {
	en string
	es string
}

// copyRegistry collects every sentence declared on it, in declaration order.
//
// It exists for one test (TestEveryMailCopyIsWrittenInBothLanguages), and it is
// the backend's counterpart to the Storefront's messages parity test: an
// explicit list would only record the copy somebody remembered to add to it,
// where this one cannot be out of date because there is no other way to make a
// mailCopy.
//
// It is a TYPE rather than a bare package-level slice so that the registering is
// something a caller does to a named thing it can see. A test needing a throwaway
// sentence declares it on a registry of its own and the production one is
// untouchable from outside this file — which matters, because a fixture that
// landed in allMailCopy would be walked by the parity test forever after and
// asserted on as if it were copy somebody ships.
type copyRegistry struct {
	all []mailCopy
}

// declare records one sentence in both languages, ENGLISH FIRST.
//
// It is a method rather than a struct literal for the guarantee in its argument
// list: an unkeyed call with a missing translation does not compile, so a
// sentence cannot reach a reader in a language nobody wrote.
func (r *copyRegistry) declare(en, es string) mailCopy {
	c := mailCopy{en: en, es: es}
	r.all = append(r.all, c)
	return c
}

// allMailCopy holds every sentence this platform can actually put in an email.
var allMailCopy = &copyRegistry{}

// translated declares one sentence of real, shipped copy, and is the only way a
// mailCopy reaches a recipient. Every call registers itself in allMailCopy,
// which is what lets one test walk all of them without a list to maintain.
func translated(en, es string) mailCopy {
	return allMailCopy.declare(en, es)
}

// in picks the sentence for a Locale, falling back to English for anything this
// platform does not write — which is the same fallback DefaultLocale states, and
// is unreachable while ParseLocale and the mail_locale CHECK both hold.
func (c mailCopy) in(locale Locale) string {
	if locale == LocaleES {
		return c.es
	}
	return c.en
}

// The Customer and staff One-time Passcode (#244, ADR 0033), which is one
// message read through two doors.
//
// It lived in the Resend provider until this file's reason for existing caught
// up with it: copy is a property of the message and not of the transport, and
// while it sat in the provider no test driving the API could assert on a word a
// recipient reads. It is now the same two sentences either door sends, written
// in whichever language the caller names — the Storefront page a visitor asked
// from, and the recipient's own Staff Locale for staff (#285, ADR 0041), which
// is what makes the first message a new Spanish-speaking organizer ever receives
// one they can read.
//
// The body says the code, that it expires, and what to do if it was not asked
// for. It names no person: a passcode request proves nothing about who is
// asking, so the platform will not greet an address it cannot yet claim to know.
var (
	otpSubjectCopy = translated(
		"Your Multiticketing passcode",
		"Su código de acceso de Multiticketing",
	)
	otpTextCopy = translated(
		"Your one-time passcode is %s.\n\nIt expires shortly. If you did not request it, ignore this email.",
		"Su código de acceso es %s.\n\nCaduca en breve. Si no lo solicitó, ignore este correo.",
	)
)

// Subject is the passcode's subject line, in the recipient's language.
func (o OTPMessage) Subject() string {
	return otpSubjectCopy.in(o.Locale)
}

// Text is the passcode's body: the code itself and the two things a recipient
// needs to know about it.
func (o OTPMessage) Text() string {
	return fmt.Sprintf(otpTextCopy.in(o.Locale), o.Code)
}

// The Sale Confirmation (#245, ADR 0033) — the receipt, and the mail a Customer
// is most certain to open.
//
// It is the message the whole Sale Locale exists for. A visitor can read a
// Spanish Event page, check out in Spanish as a guest and never sign in, so the
// only record of the language they chose is the one the sale itself keeps; the
// language reaching Locale below has already been resolved from it (see
// SaleConfirmation.Locale).
//
// WHAT DOES NOT CHANGE with the language is as decided as what does. The total
// stays in the Organization's currency and the Tax ID keeps the label printed on
// the document in the buyer's hand — "Cédula" is what an Ecuadorian buyer looks
// for on an English receipt too — because this email is the paper trail they
// file, and a translated number is a number they cannot reconcile (ADR 0016,
// #99).
var (
	saleConfirmationSubjectCopy = translated(
		"Your tickets for %s",
		"Sus entradas para %s",
	)
	// The opening is one sentence and the three facts under it, declared whole
	// rather than line by line: the greeting, what is confirmed, the reference
	// and the total are one block of prose in either language, and splitting
	// them would let a translator reorder half of it without the other half.
	saleConfirmationOpeningCopy = translated(
		"Hi %s,\n\nYour purchase for %s is confirmed.\nReference: %s\nTotal paid: %s",
		"Hola %s:\n\nSu compra de %s está confirmada.\nReferencia: %s\nTotal pagado: %s",
	)
	saleConfirmationPresentCopy = translated(
		"Present this reference at the event.",
		"Presente esta referencia en el evento.",
	)
	saleConfirmationLinkCopy = translated(
		"View your tickets:\n%s\n\nThis link opens this purchase only, and stays valid until shortly after the event.",
		"Vea sus entradas:\n%s\n\nEste enlace abre solo esta compra y sigue siendo válido hasta poco después del evento.",
	)
	// The double opt-in's one line (#255, ADR 0035), carried only by receipts
	// whose checkout left an optional consent in Pending Confirmation.
	//
	// IT NAMES NO PARTICULAR BOX, and that is a decision. Anyone at all can type
	// anyone's address into a checkout, so this line is read by two different
	// people: the buyer, who remembers ticking something, and — when a stranger
	// typed their address — somebody who ticked nothing and is owed an
	// explanation rather than a bill of particulars. "You or someone using your
	// address" covers both without accusing the second of a choice they did not
	// make, and without the platform having to tell an uninvolved reader which
	// marketing lists somebody tried to sign them up for.
	//
	// It says explicitly that ignoring it is safe and complete, because it is:
	// unconfirmed is the same as No for every purpose except the record, nothing
	// expires and nothing chases. That sentence is what makes the whole non-
	// expiring design honest to the person reading it.
	saleConfirmationConsentCopy = translated(
		"Confirm your optional preferences:\n%s\n\nYou, or someone using your address, ticked one or more optional boxes at checkout. We act on none of them until they are confirmed from this inbox. If that was not you, ignore this — nothing will be sent, and nothing else will be asked.",
		"Confirme sus preferencias opcionales:\n%s\n\nUsted, o alguien que usó su dirección, marcó una o más casillas opcionales al finalizar la compra. No actuamos sobre ninguna hasta que se confirme desde este buzón. Si no fue usted, ignore este mensaje: no se enviará nada ni se le volverá a preguntar.",
	)
)

// Subject is the Sale Confirmation's subject line, in the language the sale was
// made in.
func (c SaleConfirmation) Subject() string {
	return fmt.Sprintf(saleConfirmationSubjectCopy.in(c.Locale), c.EventName)
}

// Text is the Sale Confirmation's plain-text body: what the Customer keeps as
// their receipt.
//
// Three parts are conditional, and all are absent rather than blank when they
// do not apply — an empty label on a receipt reads as a fault in the platform,
// and there is nothing a Customer could do about it either way.
func (c SaleConfirmation) Text() string {
	// The total is the amount the Customer was charged, all in: the platform's
	// fee is never itemized on a receipt (ADR 0014).
	text := fmt.Sprintf(saleConfirmationOpeningCopy.in(c.Locale),
		c.CustomerName, c.EventName, c.Reference, formatMoney(c.AmountCents, c.Currency))

	// The Tax ID sits with the reference and the total because it belongs to the
	// same job those two do: this email is the document the buyer files for
	// their own expense records (#99).
	if taxID := c.TaxID.Display(); taxID != "" {
		text += "\n" + taxID
	}

	text += "\n\n" + saleConfirmationPresentCopy.in(c.Locale)

	// The Confirmation Link is the reason this email is worth keeping: it opens
	// this purchase months later, at the gate, with one tap and no typing.
	if c.ConfirmationLink != "" {
		text += "\n\n" + fmt.Sprintf(saleConfirmationLinkCopy.in(c.Locale), c.ConfirmationLink)
	}

	// Last, and only when something actually pends. It goes below the tickets
	// because that is the order of the reader's interest: they opened this for
	// the reference and the link, and a consent question above either would be
	// the platform's business interrupting theirs.
	if c.ConsentConfirmationLink != "" {
		text += "\n\n" + fmt.Sprintf(saleConfirmationConsentCopy.in(c.Locale), c.ConsentConfirmationLink)
	}
	return text
}

// The Sale Voided notice (#246, ADR 0033) — the mail that says a purchase is
// gone.
//
// IT IS THE MESSAGE THE CHAIN'S ORDERING WAS DECIDED FOR. A receipt could
// plausibly have read a language off the request that produced it; this one
// cannot, because there is no such request. It is sent days later, by a Platform
// Operator's button, by a Sale Import undo, or by the reversal drain, and at
// none of those moments is there a page whose address states a language. The
// sale carries its own, so this mail does not have to ask.
//
// The wording is the same in either language on the point that matters: it has
// to be true for a buyer who pressed Undo themselves AND for one whose imported
// sale was undone without their knowledge. The Spanish takes "anulada" from the
// Storefront's own account of the same state (`sale.reversedTitle`, "Esta
// compra fue anulada"), so a Customer who reads this mail and then opens their
// tickets meets one word for one thing rather than two.
var (
	saleVoidedSubjectCopy = translated(
		"Your %s purchase has been reversed",
		"Su compra de %s fue anulada",
	)
	// The body is one block in both languages, greeting and all, for the reason
	// the receipt's opening is: the closing sentence only makes sense after the
	// one above it, and splitting them would let one be translated without the
	// other.
	//
	// The closing line asks whether they EXPECTED this rather than whether it was
	// a mistake — a buyer who just pressed Undo did not make one — and names the
	// organizer as the person to talk to, which is who the Storefront sends them
	// to about the same state.
	saleVoidedTextCopy = translated(
		"Hi %s,\n\nYour purchase for %s (reference %s) has been reversed, and those tickets are no longer valid.\nIf you did not expect this, contact the organizer.",
		"Hola %s:\n\nSu compra de %s (referencia %s) fue anulada, y esas entradas ya no son válidas.\nSi no esperaba esto, contacte al organizador.",
	)
)

// Subject is the void notice's subject line, in the language the sale was made
// in.
func (v SaleVoided) Subject() string {
	return fmt.Sprintf(saleVoidedSubjectCopy.in(v.Locale), v.EventName)
}

// Text is the void notice's body. It quotes the original Sale Confirmation
// reference so the Customer can reconcile it against the receipt they were given
// — which is now a receipt in this same language, because both read the sale.
//
// "Reversed" is the glossary's word in English: the avoid list rules out
// cancelled, voided and refunded, and "cancelled" would also read as the Event
// having been called off, which is a different thing entirely.
func (v SaleVoided) Text() string {
	return fmt.Sprintf(saleVoidedTextCopy.in(v.Locale),
		v.CustomerName, v.EventName, v.Reference)
}

// The Sale Reversal Refused notice (#246, ADR 0033), which corrects a promise
// this platform made and could not keep.
//
// It has less context available to it than anything else here. It is raised by
// the reversal drain — a Reconciler run, or another Customer's page draining a
// stranger's request — long after the press it answers, so the only language
// anywhere in reach is the one the sale recorded.
//
// THREE THINGS ARE SAID AND ONE IS REFUSED, in either language. What happened,
// that THE TICKETS ARE STILL VALID — the sentence that decides whether this
// reader turns up at the gate, and the reason it comes before anything else —
// and who to talk to, named by the reference they can quote.
//
// What it refuses to say is WHY, for the reason ADR 0018 settled: there is no
// provider answer that means "too late", so a refusal cannot be explained. It
// offers no cause at all rather than a hedged one — "this can happen when…"
// reads as a cause to the person it is guessed at. It also does not apologise
// for a delay or mention that anything was pending: the reader may have pressed
// Undo a minute ago or a day ago, and the platform's own timeline is not what
// they need from this email.
var (
	saleReversalRefusedSubjectCopy = translated(
		"We could not undo your %s purchase",
		"No pudimos deshacer su compra de %s",
	)
	// The Spanish is the Storefront's own sentence for this exact outcome,
	// lengthened into a mail. `sale.refundRefusedToast` says "Sus entradas siguen
	// siendo válidas: no se pudo procesar su reembolso" to a Customer looking at
	// the page; this says the same thing in the same words to the one who closed
	// the tab, which is the only reader this message has.
	saleReversalRefusedTextCopy = translated(
		"Hi %s,\n\nWe could not undo your purchase for %s (reference %s).\n\nYour tickets are still valid — nothing has changed about your purchase, and you can still use them.\n\nIf you need help with this purchase, contact the organizer and quote the reference above.",
		"Hola %s:\n\nNo pudimos deshacer su compra de %s (referencia %s).\n\nSus entradas siguen siendo válidas: no cambió nada en su compra y puede seguir usándolas.\n\nSi necesita ayuda con esta compra, contacte al organizador e indique la referencia anterior.",
	)
)

// Subject is the refused-reversal notice's subject line. It says what happened
// in the subject itself, because a Customer who closed the tab may only ever
// read this line — and it says it in their language for the same reason.
func (r SaleReversalRefused) Subject() string {
	return fmt.Sprintf(saleReversalRefusedSubjectCopy.in(r.Locale), r.EventName)
}

// Text is the refused-reversal notice's body.
func (r SaleReversalRefused) Text() string {
	return fmt.Sprintf(saleReversalRefusedTextCopy.in(r.Locale),
		r.CustomerName, r.EventName, r.Reference)
}

// The Consent Withdrawal confirmation (#267, parent #265, ADR 0033) — the mail
// that tells a Customer their withdrawal happened.
//
// It is the only message here whose CONTENT IS CONSTRAINED BY LAW AND BY TRUTH
// in equal measure, and three rules decide every sentence of it.
//
// IT MUST NOT CLAIM THAT PROCESSING HAS STOPPED. Counsel's clarifying email
// calls the post-withdrawal condition "passive", and CONTEXT.md scopes that word
// to consent: this platform keeps doing contract-based processing for anybody
// holding a Ticket, and keeps sending Sale Confirmations, passcodes and reversal
// notices. Copy promising that data stopped being processed would be untrue, so
// the second paragraph says what CONTINUES before it is asked.
//
// IT NAMES WHAT WAS WITHDRAWN AND THAT IT CAN BE TURNED BACK ON. A confirmation
// that a person cannot act on is a notification; the last line is what makes it
// a right the reader still holds. It says "from your account" without naming a
// page, because the surface that can do it is the Customer Area today and #265
// adds another — and a mail sitting in an inbox for a year must not be the thing
// that pins a URL.
//
// IT DOES NOT ACCUSE ANYBODY OF ANYTHING. "You, or someone using this address"
// is the Sale Confirmation's own phrasing for the same problem, and it is here
// for a sharper reason: the unsubscribe link is the one surface ANYBODY holding
// a forwarded Digest can press, so this mail is precisely how an address's real
// owner learns that somebody acted on their behalf. Wording it as "you asked us
// to" would tell that reader something false in the one message written to
// correct it.
//
// IT GREETS NOBODY BY NAME, as the passcode does not. This is a message about
// somebody's rights over an address, sent to that address, and a name adds
// nothing to it — while an act performed through the unsubscribe link
// authenticates nobody, so the platform saying "Hi Ana" would be dressing an
// unauthenticated press as a recognised person.
//
// The Spanish says REVOCATORIA, which is the word counsel's form uses and the
// word a Customer holding that form will look for. The ubiquitous language
// governs the code — Consent Withdrawal, never revocation — and translations
// render it (ADR 0038).
var (
	consentWithdrawalSubjectCopy = translated(
		"Your Marketing Consent has been withdrawn",
		"Se revocó su consentimiento de marketing",
	)
	// One block in both languages, greeting and all, for the reason the receipt's
	// opening is one: the second paragraph only makes sense as a correction to
	// what the first might be read to imply, and splitting them would let one be
	// translated without the other.
	consentWithdrawalTextCopy = translated(
		"You, or someone using this address, withdrew the Marketing Consent recorded for this address. We have stopped sending marketing email to it, including the weekly digest of what you follow.\n\nNothing else changes. Your account and any tickets you hold are unaffected, you can still buy tickets, and you will still receive purchase confirmations, sign-in passcodes and notices about your purchases. We continue to hold your data for the tickets you hold and for our legal and security obligations.\n\nYou can turn marketing email back on at any time from your account.",
		"Usted, o alguien que usó esta dirección, revocó el consentimiento de marketing registrado para esta dirección. Hemos dejado de enviarle correos de marketing, incluido el resumen semanal de lo que usted sigue.\n\nNada más cambia. Su cuenta y las entradas que tenga no se ven afectadas, puede seguir comprando entradas y seguirá recibiendo las confirmaciones de compra, los códigos de acceso y los avisos sobre sus compras. Seguimos conservando sus datos para las entradas que tiene y para cumplir nuestras obligaciones legales y de seguridad.\n\nPuede volver a activar los correos de marketing cuando quiera desde su cuenta.",
	)
	// The two wordings a surface able to withdraw Networking Consent needs
	// (#270), written to the rules above rather than assembled from the marketing
	// one. THE SECOND PARAGRAPH IS THE SAME SENTENCE IN ALL THREE and is repeated
	// verbatim rather than concatenated at send time: it is the paragraph that
	// keeps this platform's promise about what continues, and a message about
	// somebody's rights is not a place to build sentences out of parts.
	//
	// What networking withdrawal actually stops is stated as visibility to other
	// attendees and nothing more, because that is all it can be: nothing
	// publishes Networking Consent to any external system and nothing caches it,
	// so there is no propagation to promise and none to have failed (ADR 0038).
	consentWithdrawalNetworkingSubjectCopy = translated(
		"Your Networking Consent has been withdrawn",
		"Se revocó su consentimiento de networking",
	)
	consentWithdrawalNetworkingTextCopy = translated(
		"You, or someone using this address, withdrew the Networking Consent recorded for this address. Your profile is no longer shown to other people attending the same events.\n\nNothing else changes. Your account and any tickets you hold are unaffected, you can still buy tickets, and you will still receive purchase confirmations, sign-in passcodes and notices about your purchases. We continue to hold your data for the tickets you hold and for our legal and security obligations.\n\nYou can turn networking back on at any time from your account.",
		"Usted, o alguien que usó esta dirección, revocó el consentimiento de networking registrado para esta dirección. Su perfil ya no se muestra a otras personas que asisten a los mismos eventos.\n\nNada más cambia. Su cuenta y las entradas que tenga no se ven afectadas, puede seguir comprando entradas y seguirá recibiendo las confirmaciones de compra, los códigos de acceso y los avisos sobre sus compras. Seguimos conservando sus datos para las entradas que tiene y para cumplir nuestras obligaciones legales y de seguridad.\n\nPuede volver a activar el networking cuando quiera desde su cuenta.",
	)
	consentWithdrawalBothSubjectCopy = translated(
		"Your consents have been withdrawn",
		"Se revocaron sus consentimientos",
	)
	consentWithdrawalBothTextCopy = translated(
		"You, or someone using this address, withdrew the Marketing Consent and the Networking Consent recorded for this address. We have stopped sending marketing email to it, including the weekly digest of what you follow, and your profile is no longer shown to other people attending the same events.\n\nNothing else changes. Your account and any tickets you hold are unaffected, you can still buy tickets, and you will still receive purchase confirmations, sign-in passcodes and notices about your purchases. We continue to hold your data for the tickets you hold and for our legal and security obligations.\n\nYou can turn either of them back on at any time from your account.",
		"Usted, o alguien que usó esta dirección, revocó el consentimiento de marketing y el consentimiento de networking registrados para esta dirección. Hemos dejado de enviarle correos de marketing, incluido el resumen semanal de lo que usted sigue, y su perfil ya no se muestra a otras personas que asisten a los mismos eventos.\n\nNada más cambia. Su cuenta y las entradas que tenga no se ven afectadas, puede seguir comprando entradas y seguirá recibiendo las confirmaciones de compra, los códigos de acceso y los avisos sobre sus compras. Seguimos conservando sus datos para las entradas que tiene y para cumplir nuestras obligaciones legales y de seguridad.\n\nPuede volver a activar cualquiera de ellos cuando quiera desde su cuenta.",
	)
)

// Subject is the Consent Withdrawal confirmation's subject line, in the
// Customer's Mail Locale.
//
// It says what happened rather than what the mail is about, because a reader who
// never opens it has still been told the one fact this message exists to
// deliver — and a reader who did not perform the act has been told it in the
// only line they are certain to see.
func (c ConsentWithdrawalConfirmation) Subject() string {
	if c.MarketingConsent && c.NetworkingConsent {
		return consentWithdrawalBothSubjectCopy.in(c.Locale)
	}
	if c.NetworkingConsent {
		return consentWithdrawalNetworkingSubjectCopy.in(c.Locale)
	}
	return consentWithdrawalSubjectCopy.in(c.Locale)
}

// Text is the confirmation's body: what was withdrawn, what continues anyway,
// and how to undo it.
//
// It interpolates nothing. One of three whole messages is CHOSEN by what the act
// took away, and none of them is assembled: a message about somebody's rights is
// not a place to build sentences out of parts, and a first paragraph stitched
// from clauses would have to be translated as clauses.
//
// The marketing-only wording is the one #267 shipped, unchanged, because the
// acts that produce it are unchanged — the unsubscribe link and the digest
// toggle can still withdraw nothing else.
//
// The fall-through is marketing-only rather than a fourth "something was
// withdrawn" message, and the case is unreachable: this is composed only where
// a receipt reported that something moved (see confirmWithdrawal), so at least
// one flag is always set.
func (c ConsentWithdrawalConfirmation) Text() string {
	if c.MarketingConsent && c.NetworkingConsent {
		return consentWithdrawalBothTextCopy.in(c.Locale)
	}
	if c.NetworkingConsent {
		return consentWithdrawalNetworkingTextCopy.in(c.Locale)
	}
	return consentWithdrawalTextCopy.in(c.Locale)
}

// The weekly Follow Digest (#220, parent #215, ADR 0030). It reads unlike every
// other message here for two reasons, and both are worth stating before the
// code.
//
// IT WAS THE FIRST MESSAGE THAT BRANCHED ON LANGUAGE, and the copy machinery
// above is the machinery it introduced. Every other message answers something
// its reader just did on a page; this one arrives unbidden, in whatever language
// the reader last used the Storefront in, so every sentence exists twice.
//
// IT NAMES TAGS IT DID NOT TRANSLATE. The Tag names arriving on each entry have
// already been resolved by catalog's LocalizedTagNames — a Preset Tag in the
// reader's language, a Custom Tag exactly as its Organization coined it. Nothing
// below touches them. There is one localization rule for Tags in this system and
// it lives in catalog; a second one here would only have to agree with it.
var (
	digestSubjectCopy = translated(
		"What's on from the things you follow",
		"Novedades de lo que usted sigue",
	)
	digestGreetingCopy = translated(
		"Hi %s,\n\nHere is what's coming up from the things you follow.",
		"Hola %s:\n\nEsto es lo que viene de las cosas que usted sigue.",
	)
	// The two section headings (#221). They are the whole difference between a
	// list and a Digest: one answers "what is there that I did not know about",
	// the other "what do I need to be ready for", and a reader who cannot tell
	// which is which has to read every entry to find out.
	digestNewHeadingCopy = translated(
		"New this week",
		"Nuevo esta semana",
	)
	digestHappeningHeadingCopy = translated(
		"Happening this week",
		"Esta semana",
	)
	// The three CALLS TO ACTION (#223), of which every entry carries exactly one.
	//
	// Before #223 an entry printed a bare URL and left the reader to work out
	// what pressing it would do. It could afford to, because there was only ever
	// one answer. There are now three, and which one an entry carries is a fact
	// about the Event and about this reader — so the line has to say what it is
	// for, or a reader who already holds a ticket cannot tell their entry from
	// anybody else's.
	//
	// digestGetTicketsCopy is the ordinary one: a ticketed Event this reader does
	// not hold a ticket for.
	digestGetTicketsCopy = translated(
		"Get tickets: %s",
		"Consiga entradas: %s",
	)
	// digestRegisterCopy is an externally registered Event (ADR 0028), which has
	// no Ticket Types to sell and whose only way in is to sign up. It still
	// points at the Storefront Event page rather than at the Registration Link
	// itself: that page is where the Registration Link's clicks are counted, and
	// a Digest that jumped straight to the third-party site would spend the
	// Organization's traffic without ever recording it.
	digestRegisterCopy = translated(
		"Register: %s",
		"Regístrese: %s",
	)
	// digestYourTicketsCopy replaces the purchase line for a reader who already
	// holds one, sending them to what they own instead of to a checkout they have
	// already been through.
	digestYourTicketsCopy = translated(
		"Your tickets: %s",
		"Sus entradas: %s",
	)
	// digestAttendingCopy is the mark itself, printed directly under the Event's
	// name so it is read before the date rather than after the address. It is the
	// answer to the question the reader would otherwise ask of every line below
	// it: "is this the one I already booked?"
	digestAttendingCopy = translated(
		"You're going",
		"Va a asistir",
	)
	// The attribution line, which is the Digest answering "why am I being told
	// this?" before the reader has to ask. ADR 0030 wants the matching Follow
	// recorded; this is the half of that the reader sees.
	digestBecauseCopy = translated(
		"Because you follow: %s",
		"Porque usted sigue: %s",
	)
	// The overflow line of a capped section (#222), in its two forms.
	//
	// It says the NUMBER and then where to see them, in that order, because the
	// number is the part that changes what the reader believes about the section
	// above it: ten Events with nothing after them is a complete list, and ten
	// with "+37 more" is a sample. The link form is what is sent; the bare form
	// exists only for a deployment with no Storefront origin configured, where
	// admitting to the cap without an address is still better than a truncation
	// nobody can see.
	digestMoreCopy = translated(
		"+%d more: %s",
		"+%d más: %s",
	)
	digestMoreWithoutLinkCopy = translated(
		"+%d more",
		"+%d más",
	)
	digestClosingCopy = translated(
		"You are getting this because you follow organizers and topics on Multiticketing.",
		"Usted recibe esto porque sigue organizadores y temas en Multiticketing.",
	)
	// The unsubscribe line (#224, ADR 0030). It says what pressing the link does
	// AND what it does not do, because those are two different acts with two
	// different names (CONTEXT.md): Unsubscribing silences the Digest,
	// Unfollowing removes a Follow. A reader who wanted fewer emails and feared
	// losing what they follow would otherwise have no way to tell, and the
	// safest-looking answer available to them is to stop opening the mail.
	digestUnsubscribeCopy = translated(
		"Don't want these? Turn off the digest — you'll keep everything you follow: %s",
		"¿No quiere recibirlos? Desactive el resumen y conservará todo lo que sigue: %s",
	)
)

// Subject is the Follow Digest's subject line.
//
// It names no Event and counts none. The obvious alternative — "3 new events
// from the things you follow" — reads as a campaign, and this message has to
// survive arriving every week for a year without being trained away as one; a
// count also makes a week with one Event look like a mistake. What the line says
// is what the mail is, which is the only thing that stays true every week.
func (d FollowDigest) Subject() string {
	return digestSubjectCopy.in(d.Locale)
}

// Text is the Follow Digest's plain-text body: a greeting, two sections of
// Events, and one closing line saying why this arrived.
//
// NEW COMES FIRST, and that order is a decision rather than a convenience
// (#221). The Digest's job is to tell a reader something they did not know; an
// agenda of things they were already told about, printed above the news, buries
// the only part of the message that could not have reached them any other way.
//
// A SECTION WITH NOTHING IN IT PRINTS NO HEADING. A "New this week" with
// nothing under it reads as a mail that had nothing to say and said it anyway,
// which is the same failure an empty Digest would be — and the agenda-only week
// is an ordinary one, not an error.
//
// A CAPPED SECTION ADMITS TO ITS OWN EDGE (#222). Ten Events is where a section
// stops, and a section that stopped there because more matched prints a "+N
// more" line pointing at a surface that holds them. Without it the two cases —
// a short list and a truncated one — are indistinguishable to the reader, and
// the truncated one quietly teaches them that Following something busy is worth
// less than it is.
//
// EVERY ENTRY CARRIES EXACTLY ONE CALL TO ACTION (#223), and which one is a
// fact about the Event and about this reader: their own Ticket Sale when they
// already hold one, Register when the Event signs its audience up elsewhere, and
// otherwise the purchase. See FollowDigestEvent.callToAction.
//
// THE FOOTER CARRIES THE UNSUBSCRIBE LINK (#224), and it is the one part of
// this message that is not about Events. ADR 0030 makes the Digest the only mail
// a Customer can turn off, so it is also the only mail that must always say how
// — every Digest, not the first one and not a sample. The line says that
// pressing it keeps their Follows, because Unsubscribe and Unfollow are
// different acts and a reader who cannot tell them apart will choose the one
// that risks nothing: ignoring the mail forever.
//
// The link is absent only when the platform holds no signing key. The footer
// then degrades to the closing line rather than rendering a dead address, and
// the Digest still goes out — a missing footer link is worth less than a Digest
// nobody gets, and the deployment fault is visible in the logs of whatever
// refused to mint it.
//
// An empty Digest cannot be rendered here because it is never composed: the
// caller sends nothing at all when nothing matched (digest/service.deliverDigest).
func (d FollowDigest) Text() string {
	text := fmt.Sprintf(digestGreetingCopy.in(d.Locale), d.CustomerName)
	text += digestSection(digestNewHeadingCopy.in(d.Locale), d.New, d.NewOverflow, d.Locale)
	text += digestSection(digestHappeningHeadingCopy.in(d.Locale), d.Happening, d.HappeningOverflow, d.Locale)
	text += "\n\n" + digestClosingCopy.in(d.Locale)
	if d.UnsubscribeURL != "" {
		text += "\n" + fmt.Sprintf(digestUnsubscribeCopy.in(d.Locale), d.UnsubscribeURL)
	}
	return text
}

// digestSection renders one heading and everything under it, or NOTHING AT ALL
// when the section is empty (#221).
//
// The empty case is the whole reason this is a function. A week with news and no
// agenda, and a week with an agenda and nothing new, are both ordinary; printing
// a bare heading for the missing half would tell the reader the Digest is broken
// on the most common weeks it will ever be sent.
// The overflow line closes the section it belongs to, BELOW the Events rather
// than beside the heading: a reader who has just finished the tenth entry is
// exactly the reader who needs to be told there are more, and a count announced
// before the list would be read as the size of the list.
func digestSection(heading string, events []FollowDigestEvent, overflow FollowDigestOverflow, locale Locale) string {
	if len(events) == 0 {
		// A section with nothing in it prints nothing, overflow included — an
		// overflow with no section above it is impossible, since only a full
		// section can shed anything.
		return ""
	}
	text := "\n\n" + heading
	for _, event := range events {
		text += "\n\n" + event.render(locale)
	}
	if overflow.Count > 0 {
		if overflow.URL != "" {
			text += "\n\n" + fmt.Sprintf(digestMoreCopy.in(locale), overflow.Count, overflow.URL)
		} else {
			text += "\n\n" + fmt.Sprintf(digestMoreWithoutLinkCopy.in(locale), overflow.Count)
		}
	}
	return text
}

// render is one Event's block in the Digest: what it is, when and where, a link,
// and why the reader is hearing about it.
//
// The date is rendered in the EVENT's own timezone rather than the reader's,
// which is how every other Event-facing surface in this system states a time and
// the only way a Digest read from another country names the day the doors
// actually open.
func (e FollowDigestEvent) render(locale Locale) string {
	block := e.Name
	// The attending mark goes directly under the name (#223), above everything
	// else the entry says. A reader scanning an agenda is looking for exactly one
	// thing — which of these have I already booked — and an answer printed below
	// the venue is an answer they have to read four lines to find.
	if e.Attending {
		block += "\n" + digestAttendingCopy.in(locale)
	}
	if when := formatEventDate(e.StartsAt, e.Timezone, locale); when != "" {
		block += "\n" + when
	}
	if e.Venue != "" {
		block += "\n" + e.Venue
	}
	if e.OrganizationName != "" {
		block += "\n" + e.OrganizationName
	}
	if cta := e.callToAction(locale); cta != "" {
		block += "\n" + cta
	}
	if reasons := e.reasons(); len(reasons) > 0 {
		block += "\n" + fmt.Sprintf(digestBecauseCopy.in(locale), strings.Join(reasons, ", "))
	}
	return block
}

// callToAction is the one thing this entry asks the reader to do, and there is
// never more than one of them (#223).
//
// The order of the branches is the order of the rules, and it is not
// interchangeable. ATTENDING WINS OUTRIGHT: a reader holding a live Ticket Sale
// is sent to what they already own, and the purchase line is not softened or
// moved but removed. EXTERNAL REGISTRATION comes next, because ADR 0028 makes
// the two registration modes exclusive — such an Event has no Ticket Types, so
// there is nothing a purchase line could point at. Everything else is an
// ordinary ticketed Event nobody here has bought yet.
//
// An attending reader with no Storefront origin configured gets NO line at all
// rather than the purchase one. That is the deliberate degradation: an entry
// with nowhere to press is a small loss, and telling somebody to buy the ticket
// they are holding is the exact failure this whole function exists to prevent.
func (e FollowDigestEvent) callToAction(locale Locale) string {
	switch {
	case e.Attending:
		if e.TicketSaleURL == "" {
			return ""
		}
		return fmt.Sprintf(digestYourTicketsCopy.in(locale), e.TicketSaleURL)
	case e.URL == "":
		return ""
	case e.ExternallyRegistered:
		return fmt.Sprintf(digestRegisterCopy.in(locale), e.URL)
	default:
		return fmt.Sprintf(digestGetTicketsCopy.in(locale), e.URL)
	}
}

// reasons is the Follows this Event matched, as the reader would name them: the
// Organization putting it on, and the Tags they follow that it carries.
//
// The Organization comes first because it is the more specific subscription —
// somebody who followed this organizer chose them, where a Tag is a whole
// category — and because it is the one a reader recognises without thinking.
func (e FollowDigestEvent) reasons() []string {
	reasons := make([]string, 0, len(e.MatchedTagNames)+1)
	if e.MatchedOrganization && e.OrganizationName != "" {
		reasons = append(reasons, e.OrganizationName)
	}
	reasons = append(reasons, e.MatchedTagNames...)
	return reasons
}

// The localized calendar, which is SHARED BY EVERY MESSAGE THAT PRINTS A DATE
// rather than the Digest's own (#245).
//
// It arrived with the Digest because the Digest was the first message that
// branched on language at all, and it read as digest machinery for exactly as
// long as the Digest was the only bilingual mail on the platform. It no longer
// is: every Customer-facing message below the line above is written in the
// reader's language, and an Event's date is the same date in whichever of them
// prints it. A second copy scoped to receipts would be a second place for a
// month to be spelled wrong.
//
// formatEventDate renders an Event's start in the Event's own zone, in the
// reader's language — "Friday 10 July, 20:00" / "viernes 10 de julio, 20:00".
// The EVENT's zone and not the reader's, always: localizing mail changes the
// words and the marks around the numbers and never the numbers themselves
// (ADR 0033), and a Spanish receipt showing a shifted time is the sort of thing
// that gets reported as a bug.
//
// The month and weekday names are spelled out here rather than taken from
// time.Format's English-only names, because Go's standard library carries no
// localized calendar and pulling in one for twelve words would be a dependency
// bigger than the feature. An unknown or unloadable timezone yields no date line
// at all rather than one in the wrong zone: a message that tells somebody the
// wrong day is worse than one that tells them to open the link.
func formatEventDate(startsAt time.Time, timezone string, locale Locale) string {
	if startsAt.IsZero() {
		return ""
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return ""
	}
	local := startsAt.In(loc)
	if locale == LocaleES {
		return fmt.Sprintf("%s %d de %s, %s",
			spanishWeekdays[local.Weekday()], local.Day(), spanishMonths[local.Month()], local.Format("15:04"))
	}
	return local.Format("Monday 2 January, 15:04")
}

var spanishWeekdays = map[time.Weekday]string{
	time.Sunday:    "domingo",
	time.Monday:    "lunes",
	time.Tuesday:   "martes",
	time.Wednesday: "miércoles",
	time.Thursday:  "jueves",
	time.Friday:    "viernes",
	time.Saturday:  "sábado",
}

var spanishMonths = map[time.Month]string{
	time.January:   "enero",
	time.February:  "febrero",
	time.March:     "marzo",
	time.April:     "abril",
	time.May:       "mayo",
	time.June:      "junio",
	time.July:      "julio",
	time.August:    "agosto",
	time.September: "septiembre",
	time.October:   "octubre",
	time.November:  "noviembre",
	time.December:  "diciembre",
}

// formatEcuadorDate renders an instant as the calendar date it falls on in
// Ecuador, in the reader's language — "20 July 2026" / "20 de julio de 2026".
//
// The zone conversion is the point, for the reason StartOfEcuadorDay exists: a
// transfer submitted at 03:00 UTC was submitted the previous evening in
// Guayaquil, and a notice telling an organizer their money left on a day it did
// not is worse than one giving no date at all, since the whole job of the date
// is to let them count 48 hours from it.
//
// THE LOCALE MOVES THE WORDS AND NOT THE DAY, which is the same rule
// formatEventDate keeps: the zone is Ecuador's in either language, and a Spanish
// notice naming a different date than the English one would be the bug this
// whole date exists to prevent.
func formatEcuadorDate(instant time.Time, locale Locale) string {
	local := instant.In(ecuadorLocation())
	if locale == LocaleES {
		return fmt.Sprintf("%d de %s de %d", local.Day(), spanishMonths[local.Month()], local.Year())
	}
	return local.Format("2 January 2006")
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
