package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Staff mail written in the recipient's Staff Locale (#285, parent #281,
// ADR 0041): the one-time passcode, and all five Payout Request notices.
//
// THIS IS WHERE ADR 0033's STAFF BOUNDARY IS PROVEN RETIRED. That ADR localized
// ten Customer messages and stopped at the staff door on a specific obstacle:
// the payout notices are addressed to `request.RequestedBy`, a recorded email
// string kept deliberately untied to a Member id, and therefore "attached to no
// record that could hold a language". The Staff Locale is keyed on the email
// address, so that string is now exactly such a record — and the tests below
// resolve every one of these six messages through it, by address alone.
//
// EVERY ASSERTION IS ON THE RENDERED MESSAGE, subject and body, exactly as the
// Customer mail locale tests are. A test that only checked a Locale had been
// carried would pass just as happily against a notice nobody ever translated.
//
// The English floor is ASSERTED rather than assumed, twice: once for a passcode
// to an address nobody has ever signed in at, and once for a payout answer to an
// asker who has stated no language. Absence is not English in the storage, and
// these are what pin that it is English to the reader.

// spanishMonth is the month name the Spanish copy spells, kept here so a date
// assertion reads as a date rather than as a hardcoded string. The harness's
// fixed clock decides which one is exercised; the map exists so moving that
// clock breaks nothing.
var spanishMonth = map[time.Month]string{
	time.January: "enero", time.February: "febrero", time.March: "marzo",
	time.April: "abril", time.May: "mayo", time.June: "junio",
	time.July: "julio", time.August: "agosto", time.September: "septiembre",
	time.October: "octubre", time.November: "noviembre", time.December: "diciembre",
}

// wantMailContains asserts on the whole rendered message, because a subject is
// as much a thing a person reads as a body is.
func wantMailContains(t *testing.T, what, subject, body string, wants ...string) {
	t.Helper()
	whole := subject + "\n" + body
	for _, want := range wants {
		if !strings.Contains(whole, want) {
			t.Fatalf("%s does not say %q:\nsubject: %s\n%s", what, want, subject, body)
		}
	}
}

// requestStaffPasscode asks the staff door for a passcode. It names no language:
// the whole point is that the recipient's stored one decides, and there is no
// field on this request that could say otherwise.
func requestStaffPasscode(t *testing.T, env *testEnv, email string) {
	t.Helper()
	resp, body := env.post(t, otpRequestPath, map[string]string{"email": email}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request staff passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// TestStaffPasscodeIsWrittenInTheRecipientsStaffLocale is the message that has
// to be readable before any other one can be: somebody who has chosen Spanish
// cannot reach the Spanish application without it.
//
// The request itself says nothing about language — it carries an email and
// nothing else — so the only thing that could have produced Spanish here is the
// stored row, looked up by the address the code is being sent to.
func TestStaffPasscodeIsWrittenInTheRecipientsStaffLocale(t *testing.T) {
	env := setupTest(t)

	sessionID := staffSignIn(t, env, "ana@example.com", "")
	setStaffLocale(t, env, sessionID, "es")

	requestStaffPasscode(t, env, "ana@example.com")

	message := lastPasscodeSent(t, env)
	if got := message.Subject(); got != "Su código de acceso de Multiticketing" {
		t.Fatalf("passcode subject = %q, want the Spanish subject", got)
	}
	if got := message.Text(); !strings.Contains(got, "Su código de acceso es "+env.email.LastCode) {
		t.Fatalf("passcode text = %q, want the Spanish body carrying the delivered code", got)
	}
}

// TestStaffPasscodeIsEnglishForARecipientWithNoStaffLocale is the floor, and it
// is the case the storage is absent-by-default for: nobody is given a language
// by being invited, so the great majority of addresses this platform sends a
// passcode to have stated nothing at all.
//
// The address here has never signed in and never chosen — there is no session
// anywhere in this test — which is exactly the condition ADR 0041 says must
// produce English rather than a missing email.
func TestStaffPasscodeIsEnglishForARecipientWithNoStaffLocale(t *testing.T) {
	env := setupTest(t)

	if got := storedStaffLocale(t, env, "bruno@example.com"); got.Valid {
		t.Fatalf("stored locale=%q for an address that has never signed in, want none", got.String)
	}

	requestStaffPasscode(t, env, "bruno@example.com")

	message := lastPasscodeSent(t, env)
	if got := message.Subject(); got != "Your Multiticketing passcode" {
		t.Fatalf("passcode subject = %q, want the English floor", got)
	}
	if got := message.Text(); !strings.Contains(got, "Your one-time passcode is "+env.email.LastCode) {
		t.Fatalf("passcode text = %q, want the English body carrying the delivered code", got)
	}
}

// TestPayoutRequestSubmittedNoticeIsWrittenInEachOperatorsStaffLocale pins the
// property that a whole fan-out has: one ask, one notice per allowlisted
// address, and each of them in the language of the person who opens it.
//
// TWO OPERATORS, ONE OF THEM WITH NO STATED LANGUAGE, is the shape that makes
// this worth a test. Resolving once for the notice and reusing it would pass a
// single-recipient test and write one of these two people a language they never
// asked for — and it is the same assertion that proves the English floor is
// applied per reader rather than as a property of the message.
func TestPayoutRequestSubmittedNoticeIsWrittenInEachOperatorsStaffLocale(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)

	spanishOperator := operatorSession(t, env, "operadora@example.com")
	setStaffLocale(t, env, spanishOperator, "es")
	seedPlatformOperator(t, env, "silent.operator@example.com")

	payable := clearedSale(t, env, adminSessionID, "Fiesta Fest", "fiesta-fest", 4)
	if _, created := submitPayoutRequestOK(t, env, adminSessionID,
		requestBody(payable, "le debemos al local el lunes")); !created {
		t.Fatalf("the request was answered as an existing one")
	}

	notices := env.email.PayoutRequestsSubmitted()
	if len(notices) != 2 {
		t.Fatalf("captured %d submission notices, want one per allowlisted operator", len(notices))
	}
	byAddress := map[string]int{}
	for i, notice := range notices {
		byAddress[notice.To] = i
	}

	spanish, ok := byAddress["operadora@example.com"]
	if !ok {
		t.Fatalf("no submission notice was addressed to the Spanish-reading operator: %+v", byAddress)
	}
	subject, body := notices[spanish].Subject(), notices[spanish].Text()
	if want := fmt.Sprintf("Test Org ha solicitado un pago de %s", noticeMoney(payable, "USD")); subject != want {
		t.Fatalf("submission subject = %q, want %q", subject, want)
	}
	wantMailContains(t, "the Spanish submission notice", subject, body,
		// The ask, its asker and their own words: what decides whether an operator
		// opens the dashboard tonight or on Monday.
		fmt.Sprintf("Test Org ha enviado una solicitud de pago de %s", noticeMoney(payable, "USD")),
		"Solicitado por: admin@example.com",
		"Nota: le debemos al local el lunes",
		// Where to go and answer it.
		"Panel de Operador",
	)
	// The Organization is an Organización on a staff surface, never an
	// Organizador: that word is the same entity's PUBLIC name and belongs to
	// Customer surfaces alone (CONTEXT.md, ADR 0041).
	if strings.Contains(strings.ToLower(subject+"\n"+body), "organizador") {
		t.Fatalf("the Spanish submission notice calls the Organization an Organizador:\nsubject: %s\n%s", subject, body)
	}
	assertNoBankDetailsInEmail(t, "Spanish submission", subject, body)

	english, ok := byAddress["silent.operator@example.com"]
	if !ok {
		t.Fatalf("no submission notice was addressed to the operator with no stated language: %+v", byAddress)
	}
	englishSubject, englishBody := notices[english].Subject(), notices[english].Text()
	if want := fmt.Sprintf("Test Org has asked to be paid %s", noticeMoney(payable, "USD")); englishSubject != want {
		t.Fatalf("the floor operator's subject = %q, want the English %q", englishSubject, want)
	}
	wantMailContains(t, "the English submission notice", englishSubject, englishBody,
		"has submitted a Payout Request for",
		"Asked by: admin@example.com",
	)
}

// TestPayoutRequestAnswersAreWrittenInTheAskersStaffLocale walks all four
// answers for one Spanish-reading Org Admin: declined, transfer sent, transfer
// failed, and paid.
//
// They share a test because they share the fact being proven — the language
// comes off `requested_by`, the recorded address, and nothing else — and because
// each answer is FINAL, so reaching the next one means asking again. That
// sequence is the ordinary life of an Organization that was told no, corrected
// something, and was eventually paid.
//
// Each answer is asserted against its own capture list, which the notices are
// appended to in send order, so nothing here depends on the harness's fixed
// clock ordering rows for it.
func TestPayoutRequestAnswersAreWrittenInTheAskersStaffLocale(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	setStaffLocale(t, env, adminSessionID, "es")
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	payable := clearedSale(t, env, adminSessionID, "Fiesta Fest", "fiesta-fest", 5)

	// DECLINED. The reason is the operator's own sentence and is quoted verbatim
	// in either language — it is the only thing standing between a decline and a
	// support thread, and translating a person's words is not this platform's
	// business.
	declined, _ := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "para el local"))
	reason := "The name on the account does not match the Tax ID."
	resp, envBody := declinePayoutRequest(t, env, operatorSessionID, declined.ID, map[string]any{"reason": reason})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("decline status=%d error=%+v", resp.StatusCode, envBody.Error)
	}
	declines := env.email.PayoutRequestsDeclined()
	if len(declines) != 1 {
		t.Fatalf("captured %d decline notices, want exactly one", len(declines))
	}
	subject, body := declines[0].Subject(), declines[0].Text()
	if want := fmt.Sprintf("Su solicitud de pago de %s fue rechazada", noticeMoney(payable, "USD")); subject != want {
		t.Fatalf("decline subject = %q, want %q", subject, want)
	}
	wantMailContains(t, "the Spanish decline notice", subject, body,
		fmt.Sprintf("La solicitud de pago de %s enviada para Test Org fue rechazada", noticeMoney(payable, "USD")),
		"Motivo: "+reason,
		// A decline is a "not this" rather than a lockout, and a Spanish reader is
		// told so as plainly as an English one (ADR 0026).
		"Puede enviar una nueva solicitud cuando quiera",
	)
	assertNoBankDetailsInEmail(t, "Spanish decline", subject, body)

	// TRANSFER SENT. The date is the day the money left in Ecuador, spelled in
	// Spanish and moved by nothing: a locale decides the words around a date and
	// never the zone it is read in.
	sent, _ := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "otra vez"))
	markProcessingOK(t, env, operatorSessionID, sent.ID, map[string]any{"transfer_reference": "PP-77120"})
	sentNotices := env.email.PayoutRequestTransfersSent()
	if len(sentNotices) != 1 {
		t.Fatalf("captured %d transfer-sent notices, want exactly one", len(sentNotices))
	}
	_, _, submittedAt := transferStamp(t, env, sent.ID)
	if submittedAt == nil {
		t.Fatalf("a processing request carries no transfer_submitted_at")
	}
	guayaquil, err := time.LoadLocation("America/Guayaquil")
	if err != nil {
		t.Fatalf("load Ecuador zone: %v", err)
	}
	inEcuador := submittedAt.In(guayaquil)
	wantDate := fmt.Sprintf("%d de %s de %d", inEcuador.Day(), spanishMonth[inEcuador.Month()], inEcuador.Year())

	subject, body = sentNotices[0].Subject(), sentNotices[0].Text()
	if want := fmt.Sprintf("Su pago de %s está en camino", noticeMoney(payable, "USD")); subject != want {
		t.Fatalf("transfer-sent subject = %q, want %q", subject, want)
	}
	wantMailContains(t, "the Spanish transfer-sent notice", subject, body,
		fmt.Sprintf("La transferencia de %s a la cuenta de su Perfil de Pagos se envió el %s",
			noticeMoney(payable, "USD"), wantDate),
		// The expectation, without which the date is just a date.
		"48 horas",
	)
	assertNoBankDetailsInEmail(t, "Spanish transfer sent", subject, body)

	// TRANSFER FAILED. The one answer of the four the organizer must ACT on, and
	// the one whose Spanish is most load-bearing: a bank sending money back and an
	// operator refusing an ask share a column and must not share a sentence in
	// either language.
	bankReason := "The bank rejected the transfer: account number does not exist."
	markFailedOK(t, env, operatorSessionID, sent.ID, map[string]any{"reason": bankReason})
	failures := env.email.PayoutRequestTransfersFailed()
	if len(failures) != 1 {
		t.Fatalf("captured %d transfer-failed notices, want exactly one", len(failures))
	}
	subject, body = failures[0].Subject(), failures[0].Text()
	if want := fmt.Sprintf("No se pudo completar su pago de %s", noticeMoney(payable, "USD")); subject != want {
		t.Fatalf("transfer-failed subject = %q, want %q", subject, want)
	}
	wantMailContains(t, "the Spanish transfer-failed notice", subject, body,
		"Motivo: "+bankReason,
		// Not a judgement, and the one place to go and fix it.
		"Esto no es una decisión sobre su solicitud",
		"Perfil de Pagos",
	)
	// The Spanish must not read as a refusal. "rechazada" is the DECLINE's word,
	// and the two share a column precisely so that they never share a sentence —
	// an organizer who believes the platform judged them over a typo corrects
	// nothing. The bank's own reason is exempt: it is quoted verbatim, whatever it
	// says.
	if written := strings.ToLower(strings.Replace(subject+"\n"+body, bankReason, "", 1)); strings.Contains(written, "rechaz") {
		t.Fatalf("the Spanish transfer-failed notice reads as a refusal:\nsubject: %s\n%s", subject, body)
	}
	assertNoBankDetailsInEmail(t, "Spanish transfer failed", subject, body)

	// PAID, for LESS than was asked. Partial fulfilment is not modelled, so this
	// email is the only place an organizer is ever told a transfer came in under
	// their ask — and a Spanish reader learning it from a bank statement instead
	// is the support thread this whole notice exists to prevent.
	paidRequest, _ := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "y otra vez"))
	transferred := payable - 500
	fulfilPayoutRequestOK(t, env, operatorSessionID, paidRequest.ID,
		fulfilBody(transferred, "2026-07-20", "liquidación de julio"))
	paidNotices := env.email.PayoutRequestsPaid()
	if len(paidNotices) != 1 {
		t.Fatalf("captured %d paid notices, want exactly one", len(paidNotices))
	}
	subject, body = paidNotices[0].Subject(), paidNotices[0].Text()
	if want := fmt.Sprintf("Su pago de %s fue enviado", noticeMoney(transferred, "USD")); subject != want {
		t.Fatalf("paid subject = %q, want %q", subject, want)
	}
	wantMailContains(t, "the Spanish paid notice", subject, body,
		fmt.Sprintf("Se transfirieron %s a la cuenta de su Perfil de Pagos", noticeMoney(transferred, "USD")),
		// The shortfall, named rather than left to a bank statement.
		fmt.Sprintf("Usted solicitó %s y se enviaron %s", noticeMoney(payable, "USD"), noticeMoney(transferred, "USD")),
		"Esto responde a la solicitud de pago enviada para Test Org",
	)
	assertNoBankDetailsInEmail(t, "Spanish paid", subject, body)

	// The money is stated in the Organization's currency in either language: a
	// locale decides words and marks and nothing about money (CONTEXT.md).
	if !strings.Contains(body, "USD") {
		t.Fatalf("the Spanish paid notice does not state the Organization's currency:\n%s", body)
	}
}

// TestPayoutRequestAnswerIsEnglishForAnAskerWithNoStaffLocale is the floor for
// the notices, asserted rather than assumed.
//
// It is the ordinary case rather than an edge one, and will stay so: no existing
// Member was backfilled, and an address that has only ever appeared on a Payout
// Request has stated nothing. English here is what makes absence safe to keep
// meaning "nobody has chosen".
func TestPayoutRequestAnswerIsEnglishForAnAskerWithNoStaffLocale(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	if got := storedStaffLocale(t, env, "admin@example.com"); got.Valid {
		t.Fatalf("stored locale=%q for an Org Admin who never chose one, want none", got.String)
	}

	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Floor Fest", "floor-fest", 5, 1)
	fulfilPayoutRequestOK(t, env, operatorSessionID, request.ID, fulfilBody(payable, "2026-07-20", ""))

	notices := env.email.PayoutRequestsPaid()
	if len(notices) != 1 {
		t.Fatalf("captured %d paid notices, want exactly one", len(notices))
	}
	subject, body := notices[0].Subject(), notices[0].Text()
	if want := fmt.Sprintf("Your payout of %s has been sent", noticeMoney(payable, "USD")); subject != want {
		t.Fatalf("paid subject = %q, want the English floor %q", subject, want)
	}
	wantMailContains(t, "the English paid notice", subject, body,
		"has been transferred to the account on your Payout Profile",
		"This answers the Payout Request submitted for Test Org",
	)
}
