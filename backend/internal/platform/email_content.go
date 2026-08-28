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

// The Question Review's submission notice (#406, ADR 0056): the Operator
// learns an Event's questions are waiting, on the channel ADR 0026 opened.
var (
	questionReviewSubmittedSubjectCopy = translated(
		"%s submitted %s for review: %s",
		"%s envió %s a revisión: %s",
	)
	questionReviewSubmittedOpeningCopy = translated(
		"%s has submitted %s on %s for your review, sent by %s.",
		"%s envió %s de %s para su revisión, enviadas por %s.",
	)
	questionReviewSubmittedNoteCopy = translated(
		"\nNote: %s",
		"\nNota: %s",
	)
	questionReviewSubmittedActionCopy = translated(
		"\n\nReview the questions on the Operator Dashboard. Nothing is asked of a buyer until you approve it.",
		"\n\nRevise las preguntas en el Panel de Operador. No se le pregunta nada a un comprador hasta que usted la apruebe.",
	)
	questionReviewOneQuestionCopy   = translated("1 question", "1 pregunta")
	questionReviewManyQuestionsCopy = translated("%d questions", "%d preguntas")
)

// questionCount is "3 questions" in the Locale, or "1 question".
func (q QuestionReviewSubmitted) questionCount() string {
	if q.QuestionCount == 1 {
		return questionReviewOneQuestionCopy.in(q.Locale)
	}
	return fmt.Sprintf(questionReviewManyQuestionsCopy.in(q.Locale), q.QuestionCount)
}

// Subject names the Organization, the count and the Event: the whole of what an
// Operator sees in a mailbox list, and what decides whether they open the
// dashboard tonight.
func (q QuestionReviewSubmitted) Subject() string {
	return fmt.Sprintf(questionReviewSubmittedSubjectCopy.in(q.Locale), q.OrganizationName, q.questionCount(), q.EventName)
}

// Text is the body: the ask, who made it, their note if any, and what to do.
// No question text travels here; the dashboard is where it is read and ruled
// on, and a notice is not the place to start reading personal-data questions.
func (q QuestionReviewSubmitted) Text() string {
	text := fmt.Sprintf(questionReviewSubmittedOpeningCopy.in(q.Locale),
		q.OrganizationName, q.questionCount(), q.EventName, q.SubmittedBy)
	if q.Note != "" {
		text += fmt.Sprintf(questionReviewSubmittedNoteCopy.in(q.Locale), q.Note)
	}
	text += questionReviewSubmittedActionCopy.in(q.Locale)
	return text
}

// The verdict notice (#407, ADR 0056): what the Operator ruled on each
// question, sent to the Member who submitted the Review.
var (
	questionReviewAnsweredSubjectCopy = translated(
		"Your questions for %s have been reviewed",
		"Sus preguntas para %s fueron revisadas",
	)
	questionReviewAnsweredOpeningCopy = translated(
		"A platform operator (%s) has reviewed the questions %s submitted for %s:\n",
		"Un operador de la plataforma (%s) revisó las preguntas que %s envió para %s:\n",
	)
	questionReviewAnsweredApprovedCopy = translated("approved", "aprobada")
	questionReviewAnsweredRefusedCopy  = translated("refused", "rechazada")
	questionReviewAnsweredReasonCopy   = translated(" — %s", " — %s")
	questionReviewAnsweredClosingCopy  = translated(
		"\nApproved questions are asked from now on. A refused question is not asked; edit it and submit the event's questions for review again.",
		"\nLas preguntas aprobadas se hacen desde ahora. Una pregunta rechazada no se hace; edítela y envíe de nuevo las preguntas del evento a revisión.",
	)
)

// Subject names the Event: the one word the submitter is waiting on.
func (q QuestionReviewAnswered) Subject() string {
	return fmt.Sprintf(questionReviewAnsweredSubjectCopy.in(q.Locale), q.EventName)
}

// Text lists every item with its verdict and, on a refusal, the reason: an
// Option is listed under its question's words so "Chicken: refused" is never
// read without knowing which question offered it.
func (q QuestionReviewAnswered) Text() string {
	text := fmt.Sprintf(questionReviewAnsweredOpeningCopy.in(q.Locale), q.AnsweredBy, q.OrganizationName, q.EventName)
	for _, item := range q.Items {
		verdict := questionReviewAnsweredApprovedCopy.in(q.Locale)
		if item.Verdict == "refused" {
			verdict = questionReviewAnsweredRefusedCopy.in(q.Locale)
		}
		label := item.QuestionLabel
		if item.OptionLabel != "" {
			label = item.QuestionLabel + " / " + item.OptionLabel
		}
		line := "\n- " + label + ": " + verdict
		if item.Reason != "" {
			line += fmt.Sprintf(questionReviewAnsweredReasonCopy.in(q.Locale), item.Reason)
		}
		text += line
	}
	text += questionReviewAnsweredClosingCopy.in(q.Locale)
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
	// The Sale Invoice's one line (#473, ADR 0060), carried only by receipts
	// for a paid House sale. It sits with the reference, the total and the
	// Tax ID because it is about the same thing they are — the buyer's fiscal
	// record of this purchase — and above "present this reference" because
	// the buyer reads the money lines first and the door lines second.
	//
	// "Tax invoice (factura)" in English names the document by the glossary's
	// word and the word the buyer will see on it; Spanish says factura alone,
	// which is what everyone in Ecuador calls it. It names no deadline and no
	// sender: the SRI decides when, and the mail arrives from the same place
	// this one did.
	saleConfirmationSaleInvoiceCopy = translated(
		"A tax invoice (factura) for this purchase will follow in a separate email.",
		"La factura de esta compra le llegará en un correo aparte.",
	)
	saleConfirmationLinkCopy = translated(
		"View your tickets:\n%s\n\nThis link opens this purchase only, and stays valid until shortly after the event.",
		"Vea sus entradas:\n%s\n\nEste enlace abre solo esta compra y sigue siendo válido hasta poco después del evento.",
	)
	// The Outstanding Answers line (#315, ADR 0044), carried only by receipts for
	// a Sale whose Tickets still owe a required Ticket Question an Answer.
	//
	// IT TAKES NO ARGUMENT, which is the whole of the mail half of this feature.
	// Every other conditional line here is a Sprintf with a URL in it; this one
	// is prose, and says "the link above" rather than printing a link of its own.
	// An Answer Link opens one Ticket and is built to be forwarded into a group
	// chat; this receipt holds the reference, the total and the Tax ID and is
	// built not to be. Putting one inside the other would make forwarding a
	// t-shirt question the same gesture as forwarding a receipt, which is the
	// failure ADR 0044 names. Since ADR 0049 the buyer answers only their own
	// Ticket; every other Ticket is handed on by naming an address on the page
	// behind the Confirmation Link, and its Holder answers for themself.
	//
	// IT NAMES NO NUMBER, for the reason HasOutstandingAnswers is a bool: the
	// debt is derived live, and a count baked into an inbox is wrong the moment
	// the buyer answers one.
	//
	// "Tickets" here is the buyer's word for the things they bought, not the
	// domain's Ticket — the receipt has always called them that ("View your
	// tickets"), and a receipt that switched vocabulary to match a schema would
	// be the platform talking to itself.
	saleConfirmationOutstandingCopy = translated(
		"Some of the tickets on this purchase still need answers. Open the link above to answer for your own ticket, or to enter the email address of whoever will be using each of the others.",
		"Algunas de las entradas de esta compra aún necesitan respuestas. Abra el enlace anterior para responder por su propia entrada, o para indicar el correo electrónico de quien vaya a usar cada una de las demás.",
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
// Four parts are conditional, and all are absent rather than blank when they
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

	// The factura promise, directly under the fiscal facts it belongs with
	// and only for a sale that owed one (#473).
	if c.SaleInvoiceFollows {
		text += "\n\n" + saleConfirmationSaleInvoiceCopy.in(c.Locale)
	}

	text += "\n\n" + saleConfirmationPresentCopy.in(c.Locale)

	// The Confirmation Link is the reason this email is worth keeping: it opens
	// this purchase months later, at the gate, with one tap and no typing.
	if c.ConfirmationLink != "" {
		text += "\n\n" + fmt.Sprintf(saleConfirmationLinkCopy.in(c.Locale), c.ConfirmationLink)
	}

	// The Outstanding Answers sentence sits DIRECTLY BELOW the Confirmation Link,
	// because "the link above" is the whole of what it says and a line between
	// them would make that phrase point at the wrong thing. It is above the
	// consent line for the same reason the consent line is last: this is about
	// the tickets the buyer just bought, and that is the platform's own business.
	//
	// GATED ON THE LINK AS WELL AS ON THE DEBT. With no Confirmation Link there
	// is no link above to open, and the sentence would be an instruction the
	// reader cannot follow — worse than silence, since they would go looking for
	// it. A missing link already means a misconfigured deployment rather than
	// anything about this Sale, and the receipt still goes out without it.
	if c.HasOutstandingAnswers && c.ConfirmationLink != "" {
		text += "\n\n" + saleConfirmationOutstandingCopy.in(c.Locale)
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

// The Answer Reminder (#317, ADR 0044; #328, ADR 0046; #347, ADR 0049): the
// message telling the Holder of a Ticket that it still owes an answer, and
// where to give it.
//
// IT IS THE RECEIPT'S ONE SENTENCE, SENT ON ITS OWN, TO WHOEVER HOLDS THE
// TICKET. saleConfirmationOutstandingCopy above says the same thing to a buyer
// already reading about their purchase; this says it weeks later to the person
// who will actually use the ticket — the buyer about their own Self-held
// Ticket, a Holder about the one they accepted — because a question was added
// after the sale or because they skipped the form.
//
// IT NAMES NOBODY AND NO PURCHASE. There is no greeting by name even though the
// platform may know the name — the Assignment mail set that precedent and mail
// gets forwarded — and above all no buyer, no price, no Tax ID and no Sale
// Confirmation reference. That is ADR 0044's disclosure rule carried over
// unchanged, and it holds despite this reader being a Verified Customer: being
// a Customer of this platform buys nobody a fact about somebody else's
// purchase. The buyer reading it about their own Ticket loses nothing: their
// receipt already said everything this withholds.
//
// IT NAMES THE TICKET TYPE, because that is how a Holder tells their ticket
// apart — a public fact on the Event's own Storefront page — and NO NUMBER AND
// NO QUESTION. Not "two questions still need answers" and not "we still need a
// t-shirt size": the debt is derived live and would be wrong the moment one
// was answered, and the questions are the Organization's words on a page this
// mail sends the reader to.
//
// IT POINTS AT THE CUSTOMER AREA, ONCE. Since ADR 0049 the reader answers from
// their own held-ticket panel, behind a sign-in to the address this mail
// reached, so the message carries one ordinary address for however many
// Tickets it lists — never a per-Ticket credential. The Answer Link is retired
// and the Assignment Link stays in the Assignment mail, where accepting is the
// point of clicking.
//
// IT SAYS ANSWERING IS OPTIONAL AND THAT THE CHASE IS NEARLY OVER, enforcing
// the same constant the receipt's sentence leans on: this mail carries no
// unsubscribe, because it is transactional (ADR 0034 keeps that footer for the
// one message it belongs on), so catalog.MaxAnswerReminders is the only thing
// standing between this reader and an unbounded chase. Anyone raising the cap
// has to come here and either change the closing sentence or break it.
//
// The Spanish is usted throughout and takes "entrada" for the thing the reader
// holds, matching the Assignment mail that reached a Holder first.
var (
	holderAnswerReminderSubjectCopy = translated(
		"Your ticket for %s still needs an answer",
		"Su entrada para %s aún necesita una respuesta",
	)
	// What is owed and which ticket it is about, declared whole in both
	// languages: one block of prose, so a translator cannot reorder half of it.
	//
	// "Your ticket" and never "the ticket you accepted": the buyer reads this
	// about a Self-held Ticket they never accepted anything for, and the
	// sentence has to be true for both readers.
	holderAnswerReminderOpeningCopy = translated(
		"Your ticket still needs an answer to a question from the organizer.\n\nEvent: %s\nTicket: %s",
		"Su entrada aún necesita respuesta a una pregunta de la organización.\n\nEvento: %s\nEntrada: %s",
	)
	// The instruction and the address, which are the whole point of the message.
	// It says to sign in with THIS address, because the Customer Area shows a
	// reader the Tickets held by the address they signed in with and nothing
	// else — a reader who signs in as somebody else finds an empty panel and no
	// explanation. And it says when the door closes, because the answer window
	// shuts at the Event's start and a reader who waits past it finds the same.
	holderAnswerReminderActionCopy = translated(
		"Answer from your tickets page:\n%s\n\nSign in there with this email address. Answers can be given until the event starts.",
		"Responda desde su página de entradas:\n%s\n\nInicie sesión allí con esta dirección de correo. Puede responder hasta que empiece el evento.",
	)
	// The closing, and the sentence that makes the rationing visible to the
	// person it protects. "At most one more" is true of the first reminder and
	// generous about the second, which is the safe direction for a promise
	// printed in an inbox.
	holderAnswerReminderClosingCopy = translated(
		"Answering is optional and your ticket is valid either way. We will send at most one more reminder about it.",
		"Responder es opcional y su entrada es válida igualmente. Enviaremos como máximo un recordatorio más al respecto.",
	)

	// THE PLURAL SHAPE, WHICH #335 RULED INTO EXISTENCE: one mail per Holder per
	// sweep, listing each owed Ticket. The singular copy above is what a Holder
	// of one Ticket reads; these variants exist only for the person who holds
	// several. Same reader, same register (usted), same disclosure rule: Events,
	// Ticket Types, one sign-in address, and nothing about anybody's purchase.
	holderAnswerReminderSubjectPluralCopy = translated(
		"Your tickets for %s still need answers",
		"Sus entradas para %s aún necesitan respuestas",
	)
	// When the listed Tickets span more than one Event, no single Event can
	// honestly headline the subject, so none does. Each Ticket's own block names
	// its Event in the body.
	holderAnswerReminderSubjectMixedCopy = translated(
		"Your tickets still need answers",
		"Sus entradas aún necesitan respuestas",
	)
	holderAnswerReminderOpeningPluralCopy = translated(
		"Your tickets still need answers to questions from the organizer.",
		"Sus entradas aún necesitan respuesta a preguntas de la organización.",
	)
	// One block per Ticket: what it is. The one address that opens all of them
	// follows the list, since the panel shows every Ticket the reader holds.
	holderAnswerReminderTicketPluralCopy = translated(
		"Event: %s\nTicket: %s",
		"Evento: %s\nEntrada: %s",
	)
	// "About each of them" rather than "about it": the promise is per Ticket
	// because the cap is (catalog.MaxAnswerReminders), and only the envelope was
	// ever shared. Anyone raising the cap has to come here and either change
	// this sentence or break it, exactly as for the singular closing.
	holderAnswerReminderClosingPluralCopy = translated(
		"Answering is optional and your tickets are valid either way. We will send at most one more reminder about each of them.",
		"Responder es opcional y sus entradas son válidas igualmente. Enviaremos como máximo un recordatorio más sobre cada una.",
	)
)

// Subject is the Answer Reminder's subject line: the reader's ticket or
// tickets, the Event where one can honestly headline it, and what is owed.
//
// IT SAYS "YOUR TICKET(S)" AND NEVER "SOME TICKETS": this reader is told about
// what THEY hold, never about a purchase. Since #335 the mail may list several
// Tickets, so the subject goes plural when it does — named after the Event
// when every listed Ticket shares one, and Event-less when they do not,
// because a subject that named one Event over a list spanning two would be
// wrong about half of what it announces.
func (r HolderAnswerReminder) Subject() string {
	if len(r.Tickets) == 1 {
		return fmt.Sprintf(holderAnswerReminderSubjectCopy.in(r.Locale), r.Tickets[0].EventName)
	}
	event := r.Tickets[0].EventName
	for _, ticket := range r.Tickets[1:] {
		if ticket.EventName != event {
			return holderAnswerReminderSubjectMixedCopy.in(r.Locale)
		}
	}
	return fmt.Sprintf(holderAnswerReminderSubjectPluralCopy.in(r.Locale), event)
}

// Text is the Answer Reminder's plain-text body.
//
// NOTHING HERE IS CONDITIONAL, unlike the receipt's optional lines: a reminder
// without its address is not a shorter reminder, it is an instruction its
// reader cannot follow. The sweep refuses to compose one at all — see the sales
// module's SweepAnswerReminders — so by the time this renders, every part of it
// is present.
//
// ONE TICKET RENDERS A SINGLE BLOCK; a list only appears for the Holder of
// several (#335). The two shapes share the discipline: the one address follows
// whatever is listed, and the closing states the per-Ticket cap in the reader's
// own terms.
func (r HolderAnswerReminder) Text() string {
	if len(r.Tickets) == 1 {
		ticket := r.Tickets[0]
		text := fmt.Sprintf(holderAnswerReminderOpeningCopy.in(r.Locale), ticket.EventName, ticket.TicketTypeName)
		text += "\n\n" + fmt.Sprintf(holderAnswerReminderActionCopy.in(r.Locale), r.CustomerAreaURL)
		text += "\n\n" + holderAnswerReminderClosingCopy.in(r.Locale)
		return text
	}

	text := holderAnswerReminderOpeningPluralCopy.in(r.Locale)
	for _, ticket := range r.Tickets {
		text += "\n\n" + fmt.Sprintf(holderAnswerReminderTicketPluralCopy.in(r.Locale),
			ticket.EventName, ticket.TicketTypeName)
	}
	text += "\n\n" + fmt.Sprintf(holderAnswerReminderActionCopy.in(r.Locale), r.CustomerAreaURL)
	text += "\n\n" + holderAnswerReminderClosingPluralCopy.in(r.Locale)
	return text
}

// The Assignment Reminder (#362, parent #361, ADR 0051): the message telling the
// buyer that some of their Tickets still have nobody, and pointing at the page
// where they can name somebody.
//
// IT GREETS BY NAME, unlike the Answer Reminder and the Assignment mail, because
// this reader is the buyer and the platform knows their name from the same
// checkout it knows their address from — the receipt greeted them the same
// way. It is the only one of the three reminders that does.
//
// IT PRINTS A TALLY AND NOT A LIST. "2 of your 3 tickets" is the Customer
// Area's own count, so the mail and the page agree to the number; a list of
// Ticket Types or of who holds the others would repeat the receipt and say
// things about third parties in a mail that gets forwarded.
//
// IT DISCLOSES WHAT ASSIGNING DOES, BEFORE THE LINK, and both halves are
// acceptance criteria rather than niceties (ADR 0047): the Holder is emailed,
// and the Organization sees the address beside the Ticket. Anyone shortening
// this message has to keep both.
//
// IT SAYS IGNORING IS FINE, exactly as the Answer Reminder does, and states
// the cap in the reader's own terms: "at most one more" is true of the first
// and generous about the second, the safe direction for a promise printed in
// an inbox. Anyone raising catalog.MaxAssignmentReminders has to come here and
// either change that sentence or break it.
//
// WHAT IS ABSENT: the price, the Tax ID, the Sale Confirmation reference, the
// Sale's Tickets and their Holders, and any second URL. ADR 0044's disclosure
// rule, kept for a message that may be forwarded: a forwarded Confirmation
// Link already opens the Sale, and the mail around it should give away
// nothing more.
//
// THE GO-LIVE SENTENCE IS CONDITIONAL ON THE SALE'S AGE (ADR 0051, #364). A
// buyer whose Sale predates TicketAssignmentWentLiveAt bought when no such
// page existed, and a mail that told them to "assign" without saying the
// feature is new would read as an accusation of having forgotten. One
// sentence, between the tally and the action, says so. A newer Sale renders
// without it, byte for byte the mail it got before the sentence existed.
//
// The Spanish is usted throughout and takes "entrada" for the thing, matching
// the receipt.
var (
	assignmentReminderSubjectCopy = translated(
		"%d of your %d tickets for %s have no name yet",
		"%d de sus %d entradas para %s aún no tienen nombre",
	)
	assignmentReminderSubjectOneCopy = translated(
		"%d of your %d tickets for %s has no name yet",
		"%d de sus %d entradas para %s aún no tiene nombre",
	)
	assignmentReminderGreetingCopy = translated(
		"Hi %s,",
		"Hola %s,",
	)
	// The Event and, when the zone can be loaded, when it starts — in the
	// Event's own zone, so the buyer is told the day the doors open where the
	// doors are.
	assignmentReminderEventCopy = translated(
		"Event: %s",
		"Evento: %s",
	)
	assignmentReminderEventDateCopy = translated(
		"Starts: %s",
		"Empieza: %s",
	)
	// The tally, in two numbers: what still has nobody, of what was bought.
	// "Address" rather than "name" in the body, because an address is what the
	// page asks for and what the disclosure below is about; the subject says
	// "name" because that is what the reader is deciding.
	assignmentReminderTallyCopy = translated(
		"%d of your %d tickets have no address yet. You can give each ticket to the person who will use it.",
		"%d de sus %d entradas aún no tienen dirección. Puede asignar cada entrada a la persona que la usará.",
	)
	assignmentReminderTallyOneCopy = translated(
		"%d of your %d tickets has no address yet. You can give it to the person who will use it.",
		"%d de sus %d entradas aún no tiene dirección. Puede asignarla a la persona que la usará.",
	)
	// The go-live sentence, for a Sale older than TicketAssignmentWentLiveAt
	// only. It keeps the tally's voice: plain, usted, no apology and no date.
	assignmentReminderGoLiveCopy = translated(
		"When you bought, tickets could not yet be assigned; now they can.",
		"Cuando compró, las entradas aún no se podían asignar; ahora sí.",
	)
	// The instruction and the address, which are the whole point of the
	// message. No sign-in is needed: the Confirmation Link opens the Sale.
	assignmentReminderActionCopy = translated(
		"Assign them from your tickets page:\n%s",
		"Asígnelas desde su página de entradas:\n%s",
	)
	// ADR 0047's disclosure, whole in both languages so a translator cannot
	// keep one half and drop the other.
	assignmentReminderDisclosureCopy = translated(
		"When you add an address, we will email the ticket to that address and the organizer will see the address next to the ticket.",
		"Cuando añada una dirección, enviaremos la entrada a esa dirección y la organización verá la dirección junto a la entrada.",
	)
	assignmentReminderClosingCopy = translated(
		"Assigning is optional and your tickets are valid either way. We will send at most one more reminder about this.",
		"Asignar es opcional y sus entradas son válidas igualmente. Enviaremos como máximo un recordatorio más al respecto.",
	)
)

// TicketAssignmentWentLiveAt is the moment Ticket Assignment first existed in
// production: the deploy of commit 5c567c9, in which ticket_assignment_enabled
// first became true there. A Sale created before it was made when no buyer
// could assign anything, and its Assignment Reminder says so (ADR 0051, #364).
//
// THIS IS A HISTORICAL FACT, NOT A SETTING. It is not read from configuration,
// is the same in every environment, and must never be moved: changing it would
// not change when the feature went live, only which buyers are told the truth
// about it. It is also TEMPORARY. Once no upcoming Event has a Sale older than
// this moment the comparison selects nothing, and the constant, the sentence
// and the branch in Text() may be deleted together.
var TicketAssignmentWentLiveAt = time.Date(2026, 8, 22, 16, 33, 38, 0, time.UTC)

// Subject is the Assignment Reminder's subject line: the tally and the Event,
// in the singular when one Ticket is left.
func (r AssignmentReminder) Subject() string {
	if r.UnassignedTickets == 1 {
		return fmt.Sprintf(assignmentReminderSubjectOneCopy.in(r.Locale), r.UnassignedTickets, r.TotalTickets, r.EventName)
	}
	return fmt.Sprintf(assignmentReminderSubjectCopy.in(r.Locale), r.UnassignedTickets, r.TotalTickets, r.EventName)
}

// Text is the Assignment Reminder's plain-text body.
//
// ONE LINE IS CONDITIONAL, and it is the date: an Event whose zone Go cannot
// load gets no date rather than a wrong one, exactly as the Follow Digest
// does. Everything else is present by the time this renders, because the
// sweep refuses to compose a reminder without its link.
func (r AssignmentReminder) Text() string {
	text := fmt.Sprintf(assignmentReminderGreetingCopy.in(r.Locale), r.FirstName)
	text += "\n\n" + fmt.Sprintf(assignmentReminderEventCopy.in(r.Locale), r.EventName)
	if when := formatEventDate(r.EventStartsAt, r.EventTimezone, r.Locale); when != "" {
		text += "\n" + fmt.Sprintf(assignmentReminderEventDateCopy.in(r.Locale), when)
	}
	tally := assignmentReminderTallyCopy
	if r.UnassignedTickets == 1 {
		tally = assignmentReminderTallyOneCopy
	}
	text += "\n\n" + fmt.Sprintf(tally.in(r.Locale), r.UnassignedTickets, r.TotalTickets)
	if r.SaleCreatedAt.Before(TicketAssignmentWentLiveAt) {
		text += "\n\n" + assignmentReminderGoLiveCopy.in(r.Locale)
	}
	text += "\n\n" + fmt.Sprintf(assignmentReminderActionCopy.in(r.Locale), r.ConfirmationLink)
	text += "\n\n" + assignmentReminderDisclosureCopy.in(r.Locale)
	text += "\n\n" + assignmentReminderClosingCopy.in(r.Locale)
	return text
}

// The Assignment mail (#325, parent #322, ADR 0046): the message telling
// somebody a friend bought them a ticket, and carrying the link whose click
// accepts it.
//
// IT IS WRITTEN FOR A STRANGER, and every sentence below is shaped by that. The
// reader never came to this platform, did not give it their address, and has no
// idea why this arrived — so the first thing the message does is say where the
// address came from. "Someone who bought a ticket for this event gave us your
// email address" is the whole explanation, and it is deliberately as close as
// the copy ever gets to the buyer: it NAMES NOBODY. Who bought it is a fact
// about the purchase, and mail gets forwarded.
//
// IT STATES WHAT ACCEPTING DISCLOSES, BEFORE THE LINK. Accepting hands the
// reader's email address to the Organization running the Event — a separate
// controller — and somebody deciding whether to click is entitled to know that
// before they do, not after. That sentence is an acceptance criterion of #325
// and not a nicety; anyone shortening this message has to keep it.
//
// IT SAYS IGNORING IS FINE, and means it: nothing happens to the ticket, the
// buyer can still answer for them, and #322's purge takes the address when the
// Event starts. There is no decline button anywhere in this feature, because
// ignoring the mail IS the decline (ADR 0046).
//
// WHAT IS ABSENT: the buyer's name or email, the price, the Tax ID, the Sale
// Confirmation reference, the Sale's other Tickets, and any Answer Link. The
// first six are ADR 0044's disclosure rule carried over unchanged. The last is
// sharper — an Answer Link is copyable off the buyer's own page, so putting one
// in this mail would put a token that proves nothing beside a token that proves
// an identity, in one message, for a reader who cannot tell them apart.
//
// The Spanish is usted throughout, as every Customer-facing message here is, and
// takes "entrada" for the thing the reader now has, matching the receipt.
var (
	ticketAssignmentSubjectCopy = translated(
		"You have a ticket for %s",
		"Tiene una entrada para %s",
	)
	// Where the address came from and what the reader now holds, declared whole
	// in both languages: one block of prose, so a translator cannot reorder half
	// of it and lose the disclaimer.
	//
	// NO GREETING BY NAME. The platform does not know this person's name — that
	// is what accepting is for — and "Hi there" reads worse than beginning with
	// the fact.
	ticketAssignmentOpeningCopy = translated(
		"Someone who bought tickets for %s gave us your email address so that one of them could be yours.\n\nEvent: %s\nTicket: %s",
		"Alguien que compró entradas para %s nos dio su dirección de correo para que una de ellas sea suya.\n\nEvento: %s\nEntrada: %s",
	)
	// The disclosure and the link, in that order and never the other way round.
	// A reader must be able to decide before they press, and a link above the
	// sentence explaining it is a link some readers will press first.
	ticketAssignmentActionCopy = translated(
		"If you accept, your email address is shared with the organizer of this event, and you can give your name and answer any questions they ask about your ticket.\n\nAccept your ticket here:\n%s",
		"Si la acepta, su dirección de correo se comparte con la organización de este evento, y podrá dar su nombre y responder las preguntas que hagan sobre su entrada.\n\nAcepte su entrada aquí:\n%s",
	)
	// The closing, and the sentence that makes ignoring a real option rather than
	// a silence the reader has to interpret. It promises three things the code
	// enforces: the ticket is unaffected (the Answer Link and the buyer's own
	// routes keep working while a Ticket is merely `assigned`), the link dies at
	// the Event's start (catalog.AnswerWindow, read live), and the address is
	// deleted then (#322's purge). Anyone changing one of those has to come here.
	ticketAssignmentClosingCopy = translated(
		"You do not have to do anything. If you ignore this message the ticket still works and the person who bought it can still use it, this link stops working when the event starts, and we delete your email address then.",
		"No tiene que hacer nada. Si ignora este mensaje la entrada sigue siendo válida y quien la compró puede seguir usándola, este enlace deja de funcionar cuando empieza el evento, y entonces eliminamos su dirección de correo.",
	)
)

// Subject is the Assignment mail's subject line: that the reader has a ticket,
// and for what.
//
// IT LEADS WITH THE FACT AND NOT WITH THE ACTION. "You have a ticket for X" is
// what makes somebody open a message from a platform they have never heard of;
// "Accept your ticket" reads like every phishing mail ever written, and this
// message already has the hardest deliverability job on the platform — it is the
// one going to somebody with no prior relationship to the sender.
func (a TicketAssignment) Subject() string {
	return fmt.Sprintf(ticketAssignmentSubjectCopy.in(a.Locale), a.EventName)
}

// Text is the Assignment mail's plain-text body: where the address came from,
// what the ticket is, what accepting discloses, the link, and that ignoring it
// costs nothing.
//
// NOTHING HERE IS CONDITIONAL. A message with no link is not a shorter message,
// it is a notification its reader cannot act on — so the caller refuses to
// compose one at all rather than sending it linkless (see the catalog service's
// mailTicketAssignment).
func (a TicketAssignment) Text() string {
	text := fmt.Sprintf(ticketAssignmentOpeningCopy.in(a.Locale), a.EventName, a.EventName, a.TicketTypeName)
	text += "\n\n" + fmt.Sprintf(ticketAssignmentActionCopy.in(a.Locale), a.AcceptURL)
	text += "\n\n" + ticketAssignmentClosingCopy.in(a.Locale)
	return text
}

// The Re-addressing mail (#420, parent #419, ADR 0058): the message telling the
// address a Sale Re-addressing names that a purchase is being re-addressed to
// them, and how to accept it.
//
// WRITTEN FOR THE STRANGER AS MUCH AS FOR THE BUYER. The Operator typed this
// address from a support thread; if it is right, the reader is the person who
// paid and has been locked out of their own tickets for weeks; if it is wrong
// again, the reader never bought anything. Every sentence was checked against
// both readers. It says who is doing this (the platform, at the organizer's
// request), what it is (a purchase, named by Event and reference), that
// ACCEPTING TAKES THE PURCHASE ON — the sentence #419's story 24 asks for, so
// that a stranger sees plainly that this is somebody else's — and that ignoring
// it does nothing.
//
// THE VERB IS ACCEPT. Not "claim", not "transfer", not "confirm" — the first two
// are barred by the glossary and the third is taken four times over (ADR 0046).
//
// WHAT IT DOES NOT SAY: the wrong address, the buyer's name, the price, the Tax
// ID, or anything else about the purchase beyond the two public facts and the
// reference. The reference is safe — it is not a credential, and it is the
// thing a buyer with two stranded Sales needs to tell their two mails apart.
//
// The Spanish is usted throughout, and takes "compra" for the purchase and
// "entradas" for the tickets, matching the receipt.
var (
	saleReAddressingSubjectCopy = translated(
		"Your purchase for %s has been re-addressed to you",
		"Su compra para %s ha sido redirigida a usted",
	)
	// Who, what, and on whose request — in that order, because a message from
	// a platform the reader may never have heard of has to say why it is
	// writing before it says anything else. The reference is stated as data,
	// never explained: the buyer knows it and a stranger has no use for it.
	saleReAddressingOpeningCopy = translated(
		"At the organizer's request, we are re-addressing a ticket purchase to this email address.\n\nEvent: %s\nReference: %s\n\nThe purchase was made under an email address that could not be reached, and the organizer has told us this is the address it was meant for.",
		"A pedido de la organización, estamos redirigiendo una compra de entradas a esta dirección de correo.\n\nEvento: %s\nReferencia: %s\n\nLa compra se hizo con una dirección de correo a la que no se podía llegar, y la organización nos ha indicado que esta es la dirección a la que estaba destinada.",
	)
	// What accepting does, stated as taking the purchase ON — the sentence that
	// makes a stranger stop. Then the link.
	saleReAddressingActionCopy = translated(
		"If this purchase is yours, accept it here and the tickets and every message about them will come to this address from now on. Accepting takes this purchase on as your own:\n%s",
		"Si esta compra es suya, acéptela aquí y las entradas y todos los mensajes sobre ellas llegarán a esta dirección de ahora en adelante. Al aceptar, usted asume esta compra como propia:\n%s",
	)
	// The closing, and the sentence that makes ignoring a real option: nothing
	// happens, nothing is theirs, and the link dies at the Event's start —
	// which is when #424's purge takes the address.
	saleReAddressingClosingCopy = translated(
		"If this purchase is not yours, or you did not expect this message, you do not have to do anything. Nothing changes unless you accept, and this link stops working when the event starts.",
		"Si esta compra no es suya, o no esperaba este mensaje, no tiene que hacer nada. Nada cambia a menos que usted acepte, y este enlace deja de funcionar cuando empieza el evento.",
	)
)

// Subject is the Re-addressing mail's subject line: the fact, and what it is
// for. It leads with "your purchase" because for the reader the feature is
// meant for, that is the news: the tickets they paid for are finally reachable.
func (r SaleReAddressing) Subject() string {
	return fmt.Sprintf(saleReAddressingSubjectCopy.in(r.Locale), r.EventName)
}

// Text is the Re-addressing mail's plain-text body: who is writing and why,
// the Event and the reference, what accepting does, the link, and that ignoring
// it costs nothing. NOTHING HERE IS CONDITIONAL: a message with no link is not a
// shorter message, so the caller refuses to compose one at all.
func (r SaleReAddressing) Text() string {
	text := fmt.Sprintf(saleReAddressingOpeningCopy.in(r.Locale), r.EventName, r.Reference)
	text += "\n\n" + fmt.Sprintf(saleReAddressingActionCopy.in(r.Locale), r.AcceptURL)
	text += "\n\n" + saleReAddressingClosingCopy.in(r.Locale)
	return text
}

// The No Longer Holding mail (#327, parent #322, ADR 0046): the message telling
// somebody who accepted a ticket that it is not theirs any more.
//
// ONE MESSAGE FOR TWO CAUSES, AND THE COPY IS WHERE THAT IS SPENT. A Holder
// stops holding a Ticket because the buyer reassigned it or because the Ticket
// Sale was reversed. From where the reader sits those are the same fact — they
// had a ticket and now they do not — so there is one set of words, and it must
// be true of both. Every sentence below was written by asking whether it stays
// true if the other cause had happened instead. That is the whole discipline of
// this copy, and it is why nothing here says "cancelled", "reversed", "refunded",
// "given to somebody else" or "changed hands".
//
// IT GIVES NO CAUSE, and that is a disclosure decision rather than a stylistic
// one. Both causes are facts about the BUYER'S decisions — they asked for their
// money back, or they gave the ticket to another friend — and this reader is
// never told who the buyer is, let alone what they chose. ADR 0044's disclosure
// rule, carried over unchanged by ADR 0046 and binding here exactly as it binds
// the Assignment mail: Event only, never the buyer, the price, the Tax ID, the
// Sale Confirmation reference or the Sale's other Tickets. The Storefront's own
// dead-link copy is written to the same rule — "Tickets can change hands, and a
// link stops working when that happens" — so a Holder who presses their old link
// after reading this meets one explanation rather than two.
//
// IT NAMES NOBODY. Not the buyer, and not the reader either: the platform may
// know this Holder's name, but a greeting buys nothing in a message this short
// and putting a name beside a lost ticket reads worse than beginning with the
// fact. The Assignment mail's opening set the same precedent for the same
// reason.
//
// IT CARRIES NO LINK AND NO BUTTON. There is nothing to press: the Ticket is not
// theirs, the Event has left their Customer Area, and their Assignment Link
// stopped opening at the same moment. A message with an action on it would be an
// instruction whose only outcome is a refusal.
//
// IT SAYS WHAT DID NOT HAPPEN, WHICH IS THE KINDEST TRUE THING AVAILABLE. The
// reader keeps their account, stays Verified, and keeps the name and the Answers
// they gave — nothing about them was deleted, and somebody who has just been
// told they lost something is entitled to know the rest of it is still there.
// That sentence is enforced by code: nothing in this flow deletes a Customer.
//
// The Spanish is usted throughout, and takes "entrada" for the ticket, matching
// the Assignment mail and the receipt so one word means one thing across the
// flow.
var (
	noLongerHoldingSubjectCopy = translated(
		"You no longer have a ticket for %s",
		"Ya no tiene una entrada para %s",
	)
	// The fact, declared whole in both languages and stated twice — once as the
	// subject and once as the first line — because a subject line is often all
	// that is read, and a body that opened on anything else would bury it.
	//
	// "is no longer yours" and NOT "has been cancelled" or "was given to someone
	// else": the passive here is not evasion, it is the disclosure rule. Either
	// alternative would name a cause, and each is false in the other case.
	noLongerHoldingOpeningCopy = translated(
		"You are no longer holding a ticket for %s.\n\nWe are telling you because you accepted that ticket, and it is no longer yours. It has been removed from your account.",
		"Ya no tiene una entrada para %s.\n\nLe avisamos porque usted aceptó esa entrada y ya no es suya. La hemos quitado de su cuenta.",
	)
	// What is left, and what the reader should do — in that order, because the
	// reassurance is worth more than the instruction and a person who has just
	// lost a ticket should not have to read to the end to find out whether they
	// also lost their account.
	//
	// The last sentence points at the organizer, who is the only party this
	// reader can be sent to: they do not know who bought the ticket, so "ask
	// whoever sent it to you" is advice they cannot follow.
	noLongerHoldingClosingCopy = translated(
		"There is nothing you need to do. Your account stays as it is, along with your name and anything you told us about your ticket.\n\nIf you were planning to go, you can still get a ticket from the event's page.",
		"No tiene que hacer nada. Su cuenta sigue igual, junto con su nombre y lo que nos haya dicho sobre su entrada.\n\nSi pensaba asistir, todavía puede conseguir una entrada en la página del evento.",
	)
)

// Subject is the No Longer Holding mail's subject line: the fact, and what it is
// about.
//
// IT LEADS WITH THE LOSS AND NAMES THE EVENT, because a subject that hedged
// would be opened late by exactly the person who most needs to read it early —
// somebody who would otherwise travel to an Event they cannot get into.
func (n NoLongerHolding) Subject() string {
	return fmt.Sprintf(noLongerHoldingSubjectCopy.in(n.Locale), n.EventName)
}

// Text is the No Longer Holding mail's plain-text body: the fact, that nothing
// is required of them, and that everything else about them is untouched.
//
// NOTHING HERE IS CONDITIONAL, and there is no branch on why the Ticket was
// lost, because there is no field to branch on — see platform.NoLongerHolding.
// Anyone adding one is undoing the decision this message exists to make.
func (n NoLongerHolding) Text() string {
	text := fmt.Sprintf(noLongerHoldingOpeningCopy.in(n.Locale), n.EventName)
	text += "\n\n" + noLongerHoldingClosingCopy.in(n.Locale)
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

var (
	questionRevokedSubjectCopy = translated(
		`A ticket question for "%s" was revoked`,
		`Se revocó una pregunta de entrada de "%s"`,
	)
	// One block in both languages: what was revoked, on which Event, why, and
	// what stays. The closing line is the half an organizer who does not read
	// it will write to ask about — the Answers already given are not lost.
	questionRevokedTextCopy = translated(
		"A Platform Operator has revoked the approval of the ticket question \"%s\" on the event \"%s\" run by %s.\n\nReason: %s\n\nFrom now on the question is no longer asked at checkout or anywhere else. The answers already given to it stay on the tickets that gave them and in your sales export. To ask something in its place, add a new question and submit it for review.",
		"Un Operador de la Plataforma revocó la aprobación de la pregunta de entrada \"%s\" del evento \"%s\" organizado por %s.\n\nMotivo: %s\n\nDesde este momento la pregunta ya no se hace en el checkout ni en ningún otro lugar. Las respuestas ya dadas se conservan en las entradas que las dieron y en su exportación de ventas. Para preguntar algo en su lugar, agregue una nueva pregunta y envíela a revisión.",
	)
)

// Subject is the Revocation notice's subject line, naming the Event: an Org
// Admin of several Events reads which one from the mailbox list.
func (r TicketQuestionRevoked) Subject() string {
	return fmt.Sprintf(questionRevokedSubjectCopy.in(r.Locale), r.EventName)
}

// Text is the Revocation notice's body. The reason is quoted whole and
// unedited: it is the Operator's message to this Organization.
func (r TicketQuestionRevoked) Text() string {
	return fmt.Sprintf(questionRevokedTextCopy.in(r.Locale), r.QuestionLabel, r.EventName, r.OrganizationName, r.Reason)
}

// The Tax Document delivery (#475, ADR 0060): the mail that hands a buyer
// the factura the receipt promised, and — through the same words keyed on
// Kind — the nota de crédito a later reversal owes.
//
// It is short on purpose. The document is the attachment; the prose only
// says what it is, which purchase it belongs to (the Event and the Sale
// Confirmation reference the buyer already holds), and where to find it
// again. It names no amount: the amount is on the document, and a figure
// repeated in prose is a figure that can disagree with it.
//
// WHAT THE READER IS NEVER TOLD is anything the authority said. The mail
// exists only once the document is authorized, so there is nothing to
// relay; and the same rule holds on the Customer Area, where a document
// still in flight is "on its way" and never a message from the SRI (#471
// story 15).
//
// "Tax invoice (factura)" and "credit note (nota de crédito)" in English
// name each document by the glossary's word and the word printed on it;
// Spanish says factura and nota de crédito alone, which is what everyone in
// Ecuador calls them.
var (
	taxDocumentSaleInvoiceSubjectCopy = translated(
		"Your tax invoice (factura) for %s",
		"Su factura de %s",
	)
	taxDocumentCreditNoteSubjectCopy = translated(
		"Your credit note (nota de crédito) for %s",
		"Su nota de crédito de %s",
	)
	// The opening names the document, the purchase and the reference as one
	// block, for the receipt's reason: the three are one sentence of prose
	// in either language, and a translator must not be able to reorder half
	// of it without the other half.
	taxDocumentSaleInvoiceOpeningCopy = translated(
		"Hi %s,\n\nAttached is the tax invoice (factura) for your purchase for %s, authorized by the SRI.\nReference: %s",
		"Hola %s:\n\nAdjuntamos la factura de su compra de %s, autorizada por el SRI.\nReferencia: %s",
	)
	taxDocumentCreditNoteOpeningCopy = translated(
		"Hi %s,\n\nAttached is the credit note (nota de crédito) for your reversed purchase for %s, authorized by the SRI.\nReference: %s",
		"Hola %s:\n\nAdjuntamos la nota de crédito de su compra anulada de %s, autorizada por el SRI.\nReferencia: %s",
	)
	// A reissue's Credit Note (#481, ADR 0061) is about the document, not the
	// purchase: the earlier factura is cancelled to correct its Recipient's
	// details, and a corrected factura follows by mail. The purchase and the
	// tickets stand, and the copy says so, because a reader holding a nota
	// de crédito will otherwise assume they were refunded.
	taxDocumentCreditNoteReissueOpeningCopy = translated(
		"Hi %s,\n\nAttached is the credit note (nota de crédito) that cancels the earlier tax invoice (factura) for your purchase for %s, authorized by the SRI, so that its recipient details can be corrected. Your purchase and your tickets are unchanged: a corrected tax invoice (factura) will follow by email.\nReference: %s",
		"Hola %s:\n\nAdjuntamos la nota de crédito, autorizada por el SRI, que deja sin efecto la factura anterior de su compra de %s para corregir los datos del receptor. Su compra y sus entradas no cambian: recibirá la factura corregida por correo.\nReferencia: %s",
	)
	// The corrected factura of a reissue (#485) is delivered as any factura,
	// with one line more: the reader holds an earlier factura and a nota de
	// crédito, and must know this one stands in place of that one.
	taxDocumentSaleInvoiceSupersedesCopy = translated(
		"This corrected tax invoice (factura) replaces the earlier one for this purchase, which the credit note (nota de crédito) you received cancelled.",
		"Esta factura corregida sustituye a la anterior de esta compra, que quedó sin efecto con la nota de crédito que recibió.",
	)
	// Both forms the SRI obliges the emisor to deliver (#496, ADR 0062),
	// named in the order they are attached: the XML is the record, the RIDE
	// its reading.
	taxDocumentAttachmentCopy = translated(
		"Attached are the XML — the document itself, exactly as the SRI authorized it — and its RIDE, the same document in printable form (PDF).",
		"Adjuntamos el XML — el documento en sí, tal como lo autorizó el SRI — y su RIDE, el mismo documento en formato imprimible (PDF).",
	)
	// The way back to the file once the mail is gone: the Sale in the
	// Customer Area, behind a sign-in. It says "sign in" outright, because
	// unlike the receipt's link this one opens nothing on its own.
	taxDocumentLinkCopy = translated(
		"You can download it again from your purchase, after signing in:\n%s",
		"Puede volver a descargarlo desde su compra, después de iniciar sesión:\n%s",
	)
)

// Subject is the delivery's subject line, naming the document and the Event
// in the Sale's language.
func (d TaxDocumentDelivery) Subject() string {
	if d.Kind == TaxDocumentKindCreditNote {
		return fmt.Sprintf(taxDocumentCreditNoteSubjectCopy.in(d.Locale), d.EventName)
	}
	return fmt.Sprintf(taxDocumentSaleInvoiceSubjectCopy.in(d.Locale), d.EventName)
}

// Text is the delivery's plain-text body: what is attached, which purchase
// it belongs to, and where to find it again.
func (d TaxDocumentDelivery) Text() string {
	opening := taxDocumentSaleInvoiceOpeningCopy
	if d.Kind == TaxDocumentKindCreditNote {
		opening = taxDocumentCreditNoteOpeningCopy
		if d.Reason == TaxDocumentReasonReissue {
			opening = taxDocumentCreditNoteReissueOpeningCopy
		}
	}
	text := fmt.Sprintf(opening.in(d.Locale), d.CustomerName, d.EventName, d.Reference)
	if d.Kind != TaxDocumentKindCreditNote && d.Supersedes {
		text += "\n\n" + taxDocumentSaleInvoiceSupersedesCopy.in(d.Locale)
	}
	text += "\n\n" + taxDocumentAttachmentCopy.in(d.Locale)
	if d.CustomerAreaURL != "" {
		text += "\n\n" + fmt.Sprintf(taxDocumentLinkCopy.in(d.Locale), d.CustomerAreaURL)
	}
	return text
}

// The Certificate Expiry Warning (#502, ADR 0063): the Operator learns the
// signing certificate is about to lapse, on the channel ADR 0026 opened.
//
// Four rungs, four subjects. The rung is what the reader sees in a mailbox
// list, and "in 30 days" and "tomorrow" ask for different evenings; the body
// changes tense at 0, because on that day the date is behind the reader and
// "expires on" would send them to a calendar that says today. What never
// changes is the shape: the date in Ecuador, whose certificate, what lapsing
// costs, and where the .p12 goes. No count of documents — the attention
// queue has that — and nobody's name, because nothing here is about a buyer.
var (
	certificateExpiryInDaysSubjectCopy = translated(
		"Signing certificate expires in %d days",
		"El certificado de firma vence en %d días",
	)
	certificateExpiryTomorrowSubjectCopy = translated(
		"Signing certificate expires tomorrow",
		"El certificado de firma vence mañana",
	)
	certificateExpiredSubjectCopy = translated(
		"Signing certificate has expired",
		"El certificado de firma ha vencido",
	)
	certificateExpiryHeadingCopy = translated(
		"Certificate expiry warning",
		"Aviso de vencimiento del certificado",
	)
	// The date and the consequence in one block per tense: the sentence
	// after the date is what the date is for, and a translation carrying one
	// without the other would be worse than none.
	certificateExpiringBodyCopy = translated(
		"The signing certificate of the Issuer with RUC %s expires on %s (Ecuador time).\n\nOnce it lapses, every Sale Invoice owed is parked unsigned — no sequential number, no submission — while the SRI's 24-hour window for transmitting each one keeps running.",
		"El certificado de firma del Emisor con RUC %s vence el %s (hora de Ecuador).\n\nCuando venza, toda Factura de venta pendiente queda detenida sin firmar — sin secuencial y sin envío — mientras el plazo de 24 horas del SRI para transmitir cada una sigue corriendo.",
	)
	certificateExpiredBodyCopy = translated(
		"The signing certificate of the Issuer with RUC %s expired on %s (Ecuador time).\n\nEvery Sale Invoice owed is now parked unsigned — no sequential number, no submission — while the SRI's 24-hour window for transmitting each one keeps running.",
		"El certificado de firma del Emisor con RUC %s venció el %s (hora de Ecuador).\n\nToda Factura de venta pendiente queda ahora detenida sin firmar — sin secuencial y sin envío — mientras el plazo de 24 horas del SRI para transmitir cada una sigue corriendo.",
	)
	certificateExpiryActionCopy = translated(
		"Upload the renewed .p12 on the Issuer page:\n%s",
		"Suba el .p12 renovado en la página del Emisor:\n%s",
	)
)

// Subject names the rung: how long is left, or that nothing is.
func (w CertificateExpiryWarning) Subject() string {
	switch {
	case w.Threshold <= 0:
		return certificateExpiredSubjectCopy.in(w.Locale)
	case w.Threshold == 1:
		return certificateExpiryTomorrowSubjectCopy.in(w.Locale)
	default:
		return fmt.Sprintf(certificateExpiryInDaysSubjectCopy.in(w.Locale), w.Threshold)
	}
}

// Text is the body: the heading the glossary names it by, the date in
// Ecuador and whose certificate it is, what lapsing costs, and the link.
//
// The date goes through formatEcuadorDate for the reason the transfer-sent
// notice's does: a NotAfter at 03:00 UTC is the previous evening in
// Guayaquil, and the reader counting days needs the day it is there.
func (w CertificateExpiryWarning) Text() string {
	body := certificateExpiringBodyCopy
	if w.Threshold <= 0 {
		body = certificateExpiredBodyCopy
	}
	text := certificateExpiryHeadingCopy.in(w.Locale)
	text += "\n\n" + fmt.Sprintf(body.in(w.Locale), w.RUC, formatEcuadorDate(w.NotAfter, w.Locale))
	text += "\n\n" + fmt.Sprintf(certificateExpiryActionCopy.in(w.Locale), w.IssuerURL)
	return text
}
