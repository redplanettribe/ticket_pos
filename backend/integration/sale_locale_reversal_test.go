package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The two reversal notices in the language of the sale they are about (#246,
// ADR 0033).
//
// What these tests cover that the copy tests cannot is the SEAM: that the
// language recorded at checkout is still loaded, days later, by the queries the
// void and refusal notices are composed from. Nothing here re-runs #245's
// journey — the words themselves are asserted at their own seam in
// platform/email_reversalnotices_test.go — and each test below stands where a
// send site actually is.
//
// Every one of them ends with no page in the story. That is the point of the
// ticket: a buyer who chose Spanish at a checkout is written to in Spanish by an
// operator's button and by a cron-driven drain, neither of which has a language
// of its own to offer.

// voidedNotice returns the single void notice captured, failing unless there is
// exactly one — the count matters as much as the contents, since one sale is
// voided once however many actors race for it.
func voidedNotice(t *testing.T, env *testEnv) platform.SaleVoided {
	t.Helper()
	notices := env.email.Voided()
	if len(notices) != 1 {
		t.Fatalf("captured %d void notices, want exactly 1", len(notices))
	}
	return notices[0]
}

// TestOperatorReversalOfASpanishSaleIsVoidedInSpanish is the story the ticket
// opens with.
//
// A sale is made on a Spanish page. Days later a Platform Operator, working in
// an application that has no Spanish at all, records that they refunded it by
// hand — and the buyer's notice arrives in Spanish. Nothing in that request
// names a language: not the operator's session, not a header, not the page they
// pressed the button on. The sale remembered, so the mail did not have to ask.
func TestOperatorReversalOfASpanishSaleIsVoidedInSpanish(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Noche de Jazz", "noche-de-jazz",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	ref, _ := buyOnlineThroughPayPhoneInLocale(t, "noche-de-jazz", gaID, "ana@example.com", 2, "es")

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(2)),
		PlatformFeeKept:     boolPtr(true),
	})

	notice := voidedNotice(t, env)
	if notice.To != "ana@example.com" || notice.Reference != ref {
		t.Fatalf("void notice = %+v, want it addressed to the buyer with reference %q", notice, ref)
	}
	if got := notice.Subject(); got != "Su compra de Noche de Jazz fue anulada" {
		t.Fatalf("void notice subject = %q, want the Spanish subject", got)
	}
	text := notice.Text()
	for _, want := range []string{"Hola Ana Lopez:", "fue anulada", "ya no son válidas", ref} {
		if !strings.Contains(text, want) {
			t.Fatalf("void notice = %q, want it to contain %q", text, want)
		}
	}
}

// TestRefusedReversalRaisedByTheDrainIsWrittenInSpanish is the hardest case in
// the ticket, and the reason the Sale Locale exists at the top of the chain.
//
// The buyer pressed Undo on a Spanish page and closed the tab. PayPhone said
// nothing, then said no — to a CRON TICK. There is no request in flight
// belonging to this Customer, no session, and no page: the only thing left that
// knows what language to write in is the sale.
//
// It also pins the ORDER. This Customer signed in without naming a language, so
// their record remembers English; Spanish wins because the sale's evidence
// outranks the remembered kind, which is the substance of ADR 0033's decision
// and would be invisible in a test where the two agreed.
func TestRefusedReversalRaisedByTheDrainIsWrittenInSpanish(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Noche de Boleros", "noche-de-boleros",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	payphoneStub.reverseHangsUp()

	ref, _ := buyOnlineThroughPayPhoneInLocale(t, "noche-de-boleros", gaID, "ana@example.com", 2, "es")
	token := customerSignIn(t, payphoneEnv, "ana@example.com")
	saleID := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref).ID
	reverseSalePending(t, payphoneEnv, token, saleID)
	if got := mailLocale(t, env, "ana@example.com"); got != "en" {
		t.Fatalf("remembered Mail Locale = %q, want en — this test is only worth running while the two sources disagree", got)
	}
	env.email.Reset()

	// The tick arrives, not the buyer. Nobody is looking at anything.
	atReversalClock(t, fixedClock.Add(time.Minute))
	payphoneStub.reverseRefuses(http.StatusBadRequest, "El reverso no se puede ejecutar, contáctese con el banco emisor", 42)
	if result := drainReversals(t, payphoneEnv); result.Pursued != 1 || result.Refused != 1 {
		t.Fatalf("drain = %+v, want the one stuck request pursued and refused", result)
	}

	notice := refusedNotice(t, env)
	if got := notice.Subject(); got != "No pudimos deshacer su compra de Noche de Boleros" {
		t.Fatalf("refused notice subject = %q, want the Spanish subject", got)
	}
	text := notice.Text()
	// The sentence that decides whether this reader turns up at the gate, in the
	// language they read it in on the page they bought from.
	for _, want := range []string{"Hola Ana Lopez:", "Sus entradas siguen siendo válidas", "contacte al organizador", ref} {
		if !strings.Contains(text, want) {
			t.Fatalf("refused notice = %q, want it to contain %q", text, want)
		}
	}
	// Still no explanation, in any language: the provider's own Spanish answer is
	// the one thing that must not have travelled here.
	if strings.Contains(text, "banco emisor") {
		t.Fatalf("refused notice leaks the provider's answer:\n%s", text)
	}
}

// TestVoidNoticeFallsThroughToTheRecipientsRememberedLanguage is the second step
// of the chain, at a send site.
//
// The sale names nothing — this buyer's checkout carried no language, exactly as
// a box office sale and an import carry none — so the notice is written in what
// the Customer's own record remembers from the Storefront they last signed in
// on. Absence on the sale is not English (migration 059); it is a question
// passed on.
func TestVoidNoticeFallsThroughToTheRecipientsRememberedLanguage(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Sin Idioma Fest", "sin-idioma-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	ref, _ := buyOnlineThroughPayPhone(t, "sin-idioma-fest", gaID, "ana@example.com", 2)
	if got := saleLocale(t, env, ref); got.Valid {
		t.Fatalf("ticket_sales.locale = %+v, want none — this checkout named no language", got)
	}
	customerSignInWithLocale(t, payphoneEnv, "ana@example.com", "es")

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()
	operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(2)),
		PlatformFeeKept:     boolPtr(true),
	})

	if got := voidedNotice(t, env).Subject(); got != "Su compra de Sin Idioma Fest fue anulada" {
		t.Fatalf("void notice subject = %q, want the remembered Spanish", got)
	}
}

// TestUndoneImportWritesToEachBuyerInTheirOwnLanguage covers the third send
// site — the undo of a Sale Import — and the consequence ADR 0033 accepts
// outright: two buyers of the same batch can be written to in two languages.
//
// An imported sale was produced by no page and records no Sale Locale at all, so
// every notice here falls through to what each recipient's own record remembers.
// One of these two has signed in on the Spanish Storefront and one has never
// signed in anywhere, and both are answered correctly rather than uniformly.
func TestUndoneImportWritesToEachBuyerInTheirOwnLanguage(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Feria Importada", "feria-importada")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	batchID := commitBatch(t, env, sessionID, eventID, "batch-locale", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": ttID, "quantity": 1, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
	})
	customerSignInWithLocale(t, env, "ana@example.com", "es")
	env.email.Reset()

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+batchID+"/undo", map[string]any{
		"notify_buyers": true,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status=%d error=%+v", resp.StatusCode, body.Error)
	}

	subjects := map[string]string{}
	for _, notice := range env.email.Voided() {
		subjects[notice.To] = notice.Subject()
	}
	if got := subjects["ana@example.com"]; got != "Su compra de Feria Importada fue anulada" {
		t.Fatalf("Ana's void notice subject = %q, want the language her record remembers", got)
	}
	if got := subjects["bob@example.com"]; got != "Your Feria Importada purchase has been reversed" {
		t.Fatalf("Bob's void notice subject = %q, want English — nothing anywhere has ever named a language for him", got)
	}
}

// TestVoidNoticeForASaleThatNamedNoLanguageAndARecipientWhoNeverSignedIn is the
// floor of the chain, and the promise that sales made before any of this shipped
// still produce notices exactly as before.
//
// Nothing here has ever named a language: a guest checkout that carried none,
// and a buyer who has never signed in and therefore has no remembered one. The
// notice is the English one this platform has always sent, word for word.
func TestVoidNoticeForASaleThatNamedNoLanguageAndARecipientWhoNeverSignedIn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Plain Fest", "plain-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	ref, _ := buyOnlineThroughPayPhone(t, "plain-fest", gaID, "bruno@example.com", 1)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()
	operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)),
		PlatformFeeKept:     boolPtr(true),
	})

	notice := voidedNotice(t, env)
	if got := notice.Subject(); got != "Your Plain Fest purchase has been reversed" {
		t.Fatalf("void notice subject = %q, want the English subject unchanged", got)
	}
	if !strings.Contains(notice.Text(), "has been reversed, and those tickets are no longer valid.") {
		t.Fatalf("void notice = %q, want the English body unchanged", notice.Text())
	}
}
