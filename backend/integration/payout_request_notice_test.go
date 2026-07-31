package integration

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The five emails a Payout Request sends (#179 and #188, ADR 0026).
//
// This is the platform's FIRST organizer-facing notification channel: every
// other transactional email is a Customer's receipt or a staff sign-in code. The
// five close the loops that would otherwise each become a support message — the
// operator allowlist learns an ask arrived, and the person who asked learns
// their transfer was sent, and then that it landed, or why it did not.
//
// Four properties are pinned here rather than in a unit test, because only real
// HTTP over a real Postgres sees all of them at once:
//
//   - WHO IS TOLD. The submission notice goes to the operator ALLOWLIST — the
//     whole of operator authority (ADR 0015) — and the two answers go to the
//     ONE email recorded on the request as `requested_by`, never to every Org
//     Admin. One request, one asker, one reply.
//
//   - THE DECLINE AND THE FAILURE CARRY THEIR REASON. A queue that swallows
//     requests silently generates the support thread it was built to prevent,
//     and the reason only does that job if it reaches the asker's inbox. The
//     failure's reason is the one an organizer must ACT on: it names the account
//     number that stopped their money.
//
//   - A NOTICE BELONGS TO A TRANSITION, NOT TO A BUTTON. Every one of the five
//     is sent only when a row actually changed — the submission on `created`,
//     the four answers on a compare-and-swap that WON. A refused press tells
//     nobody anything, or the platform is emailing promises no row backs.
//
//   - NO BANK DETAILS IN ANY BODY. The database now holds account numbers and
//     the rule that they never leave it is otherwise enforced by review alone
//     (ADR 0026). Email is the one place that rule can be tested, so it is.
//
//   - A FAILING SENDER CHANGES NOTHING. The money record is the fact and the
//     email is a courtesy: a Resend outage must not roll back a fulfilment
//     (ADR 0009, and the same posture ADR 0019 takes on every other notice
//     path). The test drives that with a sender that genuinely returns an error
//     rather than by inspection.

// noticeMoney renders cents the way platform.formatMoney does, so an assertion
// on an email body reads the number a human reads: "45.00 USD".
func noticeMoney(cents int, currency string) string {
	return fmt.Sprintf("%d.%02d %s", cents/100, cents%100, currency)
}

// assertNoBankDetailsInEmail is the ADR 0026 rule as an assertion, in the shape
// this seam needs — the operator queue's own version of the rule asserts on an
// error envelope (assertNoBankDetails), and an email is a different payload with
// the same prohibition.
//
// It checks the whole message — subject and body — against every field of the
// snapshot profile the fixtures submit, because a leak in a subject line is a
// leak in a mail-server log, a phone notification and a screenshot.
//
// The account TYPE is deliberately not checked: "ahorros" is the word the
// receiving bank's own form uses and identifies no account. Everything that
// could help a stranger recognise or use the account is.
func assertNoBankDetailsInEmail(t *testing.T, what, subject, body string) {
	t.Helper()
	whole := subject + "\n" + body
	for field, value := range map[string]string{
		"bank name":           "Banco Pichincha",
		"account number":      "2201234821",
		"account number tail": "4821",
		"account holder":      "Fundación Ritmo",
		"tax id":              validCedula,
	} {
		if strings.Contains(whole, value) {
			t.Fatalf("%s email contains the %s (%q) — bank details never leave the database (ADR 0026):\n%s", what, field, value, whole)
		}
	}
}

// TestPayoutRequestNoticeTellsTheOperatorAllowlistAnAskArrived is the loop the
// nav badge cannot close: a badge only works for somebody who already decided to
// look, and a Friday-evening request otherwise waits until Monday (ADR 0026).
//
// It also pins who "the operator" is. There is no operator role and no row to
// grant one — the allowlist IS the authority (ADR 0015) — so every address on it
// is told, including operators who have never signed in and belong to no
// Organization.
func TestPayoutRequestNoticeTellsTheOperatorAllowlistAnAskArrived(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	// Two operators, and the second has never signed in: presence on the
	// allowlist is the whole of the qualification.
	seedPlatformOperator(t, env, "first.operator@example.com")
	seedPlatformOperator(t, env, "second.operator@example.com")

	payable := clearedSale(t, env, adminSessionID, "Notice Fest", "notice-fest", 4)
	request, created := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "for the venue deposit"))
	if !created {
		t.Fatalf("the request was answered as an existing one")
	}

	notices := env.email.PayoutRequestsSubmitted()
	if len(notices) != 2 {
		t.Fatalf("captured %d submission notices, want one per allowlisted operator", len(notices))
	}
	got := []string{notices[0].To, notices[1].To}
	if got[0] != "first.operator@example.com" || got[1] != "second.operator@example.com" {
		t.Fatalf("submission notices addressed to %v, want both allowlisted operators", got)
	}

	// The body is read through the same Text() the Resend sender calls, because
	// what is being asserted is what a person reads.
	notice := notices[0]
	subject, body := notice.Subject(), notice.Text()
	for _, want := range []string{
		// Who is asking, how much for, and who asked — the three facts that
		// decide whether an operator opens the dashboard tonight or on Monday.
		"Test Org",
		noticeMoney(payable, "USD"),
		"admin@example.com",
		// The organizer's own note travels with the ask: it is usually the
		// reason the request is urgent.
		"for the venue deposit",
	} {
		if !strings.Contains(subject+"\n"+body, want) {
			t.Fatalf("submission notice does not mention %q:\nsubject: %s\n%s", want, subject, body)
		}
	}
	assertNoBankDetailsInEmail(t, "submission", subject, body)

	// Asking twice does not tell the operators twice. A second submission is
	// handed the outstanding request back rather than recording a new one, and an
	// organizer who double-clicked has not made a second ask.
	if _, createdAgain := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "for the venue deposit")); createdAgain {
		t.Fatalf("the second submission created a second request")
	}
	if again := env.email.PayoutRequestsSubmitted(); len(again) != 2 {
		t.Fatalf("captured %d submission notices after a repeated submission, want the original 2", len(again))
	}

	// Nothing about the request has been answered, so nobody has been told an
	// answer.
	if paid, declined := env.email.PayoutRequestsPaid(), env.email.PayoutRequestsDeclined(); len(paid) != 0 || len(declined) != 0 {
		t.Fatalf("a pending request sent %d paid and %d declined notices, want none", len(paid), len(declined))
	}
	if request.Status != "pending" {
		t.Fatalf("request status = %q, want pending", request.Status)
	}
}

// TestPayoutRequestNoticeTellsTheAskerItWasPaid closes the loop the organizer
// is waiting on: the money has left, go and look at your bank.
//
// It fulfils for LESS than was asked, because partial fulfilment is not modelled
// and never will be (ADR 0026): the Payout records what moved, the request keeps
// what was asked, and the only place that divergence is ever explained to the
// organizer is this email.
func TestPayoutRequestNoticeTellsTheAskerItWasPaid(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Paid Fest", "paid-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	transferred := payable - 500
	fulfilPayoutRequestOK(t, env, operatorSessionID, request.ID,
		fulfilBody(transferred, "2026-07-20", "July settlement"))

	notices := env.email.PayoutRequestsPaid()
	if len(notices) != 1 {
		t.Fatalf("captured %d paid notices, want exactly one", len(notices))
	}
	notice := notices[0]
	// The address recorded on the request, and not every Org Admin: one request,
	// one asker, one reply. It is an email rather than a member id precisely so
	// it still resolves after that person's Membership ends.
	if notice.To != "admin@example.com" {
		t.Fatalf("paid notice addressed to %q, want the Member who asked", notice.To)
	}

	subject, body := notice.Subject(), notice.Text()
	if !strings.Contains(subject+"\n"+body, noticeMoney(transferred, "USD")) {
		t.Fatalf("paid notice does not state what was transferred (%s):\nsubject: %s\n%s",
			noticeMoney(transferred, "USD"), subject, body)
	}
	// The gap is named rather than left for the organizer to discover in their
	// bank statement.
	if !strings.Contains(body, noticeMoney(payable, "USD")) {
		t.Fatalf("paid notice does not name the amount asked for (%s), which the transfer fell short of:\n%s",
			noticeMoney(payable, "USD"), body)
	}
	assertNoBankDetailsInEmail(t, "paid", subject, body)

	// The operator who answered is not told, and no decline notice exists.
	if declined := env.email.PayoutRequestsDeclined(); len(declined) != 0 {
		t.Fatalf("a fulfilment sent %d decline notices, want none", len(declined))
	}
}

// TestPayoutRequestNoticeTellsTheAskerWhyItWasDeclined carries the one thing a
// decline exists to carry.
//
// The reason is required by the handler, by the service and by a CHECK
// constraint under both (ADR 0026) — and all three of those are wasted if it
// never reaches the person who has to decide what to do next.
func TestPayoutRequestNoticeTellsTheAskerWhyItWasDeclined(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Declined Fest", "declined-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	reason := "The name on the account does not match the Tax ID; please correct it and ask again."
	resp, envBody := declinePayoutRequest(t, env, operatorSessionID, request.ID, map[string]any{"reason": reason})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("decline status=%d error=%+v", resp.StatusCode, envBody.Error)
	}

	notices := env.email.PayoutRequestsDeclined()
	if len(notices) != 1 {
		t.Fatalf("captured %d decline notices, want exactly one", len(notices))
	}
	notice := notices[0]
	if notice.To != "admin@example.com" {
		t.Fatalf("decline notice addressed to %q, want the Member who asked", notice.To)
	}

	subject, body := notice.Subject(), notice.Text()
	if !strings.Contains(body, reason) {
		t.Fatalf("decline notice does not carry the reason:\n%s", body)
	}
	if !strings.Contains(subject+"\n"+body, noticeMoney(payable, "USD")) {
		t.Fatalf("decline notice does not name the ask it answers (%s):\nsubject: %s\n%s",
			noticeMoney(payable, "USD"), subject, body)
	}
	assertNoBankDetailsInEmail(t, "decline", subject, body)

	if paid := env.email.PayoutRequestsPaid(); len(paid) != 0 {
		t.Fatalf("a decline sent %d paid notices, want none — nothing moved", len(paid))
	}
}

// TestPayoutRequestNoticeTellsTheAskerTheTransferWasSent closes the loop that
// opens the moment `processing` exists (#188, ADR 0026 amendment).
//
// Before this notice, a request an operator has read and acted on and one nobody
// has opened read identically from the organizer's side: both say "waiting". The
// email is the sentence that stops the "where is my money" message on day one,
// and the DATE is what makes it worth sending — "up to 48 hours" counted from
// nothing is a claim the organizer cannot check, and one they cannot check is
// one they will write to ask about.
func TestPayoutRequestNoticeTellsTheAskerTheTransferWasSent(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Sent Fest", "sent-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	markProcessingOK(t, env, operatorSessionID, request.ID, map[string]any{"transfer_reference": "PP-77120"})

	notices := env.email.PayoutRequestTransfersSent()
	if len(notices) != 1 {
		t.Fatalf("captured %d transfer-sent notices, want exactly one", len(notices))
	}
	notice := notices[0]
	// The one address on the request, as both other answers are: one request, one
	// asker, one reply.
	if notice.To != "admin@example.com" {
		t.Fatalf("transfer-sent notice addressed to %q, want the Member who asked", notice.To)
	}

	// The instant the notice states is the one STORED against the request, not a
	// second reading of the clock: the organizer counts 48 hours from the date on
	// this email, and their payouts page must show them the same day.
	_, _, submittedAt := transferStamp(t, env, request.ID)
	if submittedAt == nil {
		t.Fatalf("a processing request carries no transfer_submitted_at")
	}
	if !notice.SubmittedAt.Equal(*submittedAt) {
		t.Fatalf("transfer-sent notice states %s, want the stored submission instant %s",
			notice.SubmittedAt.UTC(), submittedAt.UTC())
	}
	// The date as an organizer in Guayaquil reads it. A transfer submitted at
	// 03:00 UTC was submitted the previous evening there, and a notice quoting UTC
	// would hand them a day to count from that their bank does not agree with.
	guayaquil, err := time.LoadLocation("America/Guayaquil")
	if err != nil {
		t.Fatalf("load Ecuador zone: %v", err)
	}
	wantDate := submittedAt.In(guayaquil).Format("2 January 2006")

	subject, body := notice.Subject(), notice.Text()
	for _, want := range []string{
		// What is on its way, and how much of it.
		noticeMoney(payable, "USD"),
		wantDate,
		// The expectation, without which the date is just a date.
		"48 hours",
	} {
		if !strings.Contains(subject+"\n"+body, want) {
			t.Fatalf("transfer-sent notice does not mention %q:\nsubject: %s\n%s", want, subject, body)
		}
	}
	// The bank's own reference is an operational fact for operators chasing a
	// stalled transfer, and means nothing to the organizer. It is not a bank
	// DETAIL, but it has no business in this email either.
	if strings.Contains(subject+"\n"+body, "PP-77120") {
		t.Fatalf("transfer-sent notice quotes the bank's transfer reference:\nsubject: %s\n%s", subject, body)
	}
	assertNoBankDetailsInEmail(t, "transfer sent", subject, body)

	// Nothing has moved, so nobody has been told it did — and a transfer that was
	// sent is not one that was refused.
	if paid, declined := env.email.PayoutRequestsPaid(), env.email.PayoutRequestsDeclined(); len(paid) != 0 || len(declined) != 0 {
		t.Fatalf("marking a transfer processing sent %d paid and %d declined notices, want none", len(paid), len(declined))
	}
	if failed := env.email.PayoutRequestTransfersFailed(); len(failed) != 0 {
		t.Fatalf("marking a transfer processing sent %d transfer-failed notices, want none", len(failed))
	}

	// A SECOND operator pressing the same button loses the compare-and-swap, and a
	// swap that wrote nothing tells nobody anything. This is the property that
	// keeps the notice attached to the transition rather than to the request.
	secondOperator := operatorSession(t, env, "second.operator@example.com")
	resp, envBody := markProcessing(t, env, secondOperator, request.ID, map[string]any{})
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("a second mark-processing succeeded against an already-submitted transfer: %+v", envBody)
	}
	if again := env.email.PayoutRequestTransfersSent(); len(again) != 1 {
		t.Fatalf("captured %d transfer-sent notices after a lost compare-and-swap, want the original 1", len(again))
	}
}

// TestPayoutRequestNoticeTellsTheAskerWhyTheTransferFailed carries the one thing
// of the five that the organizer has to ACT on (#188, ADR 0026 amendment).
//
// The commonest failure is a wrong account number on the Organization's own
// Payout Profile, and this request's copy of it is a frozen snapshot: the fix is
// on the profile and the next attempt is a new ask. A failure that appears only
// on a page the organizer would have to think to visit is a week of waiting
// followed by the support thread this whole feature exists to prevent.
//
// It also pins the distinction `resolution_reason` was renamed for. A bank
// sending money back and an operator refusing an ask share that column, and this
// notice must not read as the latter: an organizer who believes the platform
// judged them over a typo corrects nothing.
func TestPayoutRequestNoticeTellsTheAskerWhyTheTransferFailed(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Bounced Fest", "bounced-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	markProcessingOK(t, env, operatorSessionID, request.ID, map[string]any{})
	reason := "The bank rejected the transfer: account number does not exist at Banco del Pacífico."
	markFailedOK(t, env, operatorSessionID, request.ID, map[string]any{"reason": reason})

	notices := env.email.PayoutRequestTransfersFailed()
	if len(notices) != 1 {
		t.Fatalf("captured %d transfer-failed notices, want exactly one", len(notices))
	}
	notice := notices[0]
	if notice.To != "admin@example.com" {
		t.Fatalf("transfer-failed notice addressed to %q, want the Member who asked", notice.To)
	}

	subject, body := notice.Subject(), notice.Text()
	// VERBATIM. The operator's sentence is the only thing standing between a
	// bounced transfer and a support thread, and nothing summarises or softens it.
	if !strings.Contains(body, reason) {
		t.Fatalf("transfer-failed notice does not carry the operator's reason verbatim:\n%s", body)
	}
	if !strings.Contains(subject+"\n"+body, noticeMoney(payable, "USD")) {
		t.Fatalf("transfer-failed notice does not name the ask it answers (%s):\nsubject: %s\n%s",
			noticeMoney(payable, "USD"), subject, body)
	}
	// The one click that fixes it.
	if !strings.Contains(body, "Payout Profile") {
		t.Fatalf("transfer-failed notice does not point at the Payout Profile:\n%s", body)
	}
	// A failure is not a refusal. `declined` and `failed` share a column and must
	// not share a sentence.
	for _, forbidden := range []string{"declined", "refused", "rejected your"} {
		if strings.Contains(strings.ToLower(subject+"\n"+body), forbidden) {
			t.Fatalf("transfer-failed notice reads as a refusal (%q):\nsubject: %s\n%s", forbidden, subject, body)
		}
	}
	assertNoBankDetailsInEmail(t, "transfer failed", subject, body)

	// The decline notice is a different email for a different event, and neither
	// transition sent the other's.
	if declined := env.email.PayoutRequestsDeclined(); len(declined) != 0 {
		t.Fatalf("a failed transfer sent %d decline notices, want none — nobody judged this ask", len(declined))
	}
	if paid := env.email.PayoutRequestsPaid(); len(paid) != 0 {
		t.Fatalf("a failed transfer sent %d paid notices, want none — the money came back", len(paid))
	}
	// And nothing was ever written to the ledger to have to be unwound.
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payouts rows after a failed transfer = %d, want 0", count)
	}

	// `failed` is terminal: a second marking loses its swap and tells nobody
	// twice.
	resp, envBody := markFailed(t, env, operatorSessionID, request.ID, map[string]any{"reason": "again"})
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("a second mark-failed succeeded against an already-failed request: %+v", envBody)
	}
	if again := env.email.PayoutRequestTransfersFailed(); len(again) != 1 {
		t.Fatalf("captured %d transfer-failed notices after a lost compare-and-swap, want the original 1", len(again))
	}
}

// TestPayoutRequestNoticeFailureNeverBlocksTheAnswer is the acceptance criterion
// with the most consequence: with the email provider down, every one of the five
// actions still happens and still stands.
//
// The money record is the fact and the email is a courtesy (ADR 0019). A Resend
// outage that rolled back a fulfilment would mean an operator who had already
// wired money by hand being told it did not happen — the worst outcome this
// feature can produce, and strictly worse than an organizer never getting an
// email.
//
// Every session is established BEFORE the sender starts failing, because the
// one email in this system that is NOT best-effort is the staff passcode: it is
// a credential, and a login that could not deliver one is a login that failed.
func TestPayoutRequestNoticeFailureNeverBlocksTheAnswer(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	payable := clearedSale(t, env, adminSessionID, "Outage Fest", "outage-fest", 5)

	env.email.FailWith(errors.New("resend is down"))
	t.Cleanup(func() { env.email.FailWith(nil) })

	// Submission: the ask is recorded even though nobody could be told about it.
	request, created := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "outage"))
	if !created || request.Status != "pending" {
		t.Fatalf("submission during an email outage = %+v (created=%t), want a pending request", request, created)
	}

	// Decline: the answer stands, reason and all, with nobody told.
	resp, envBody := declinePayoutRequest(t, env, operatorSessionID, request.ID, map[string]any{"reason": "not this month"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("decline during an email outage status=%d error=%+v", resp.StatusCode, envBody.Error)
	}

	// The transfer: submitted, and STILL submitted with nobody told. A decline
	// frees the Organization to ask again immediately, which is how there is a
	// second request to send money against.
	//
	// This is the transition where a returned delivery error would do the most
	// damage. The money has left the platform's bank account by the time an
	// operator presses this, and a 500 from an email outage would leave them
	// looking at a `pending` request for a transfer that is genuinely in flight —
	// which is exactly how the same request gets transferred twice.
	second, createdAgain := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "outage, again"))
	if !createdAgain {
		t.Fatalf("the re-ask after a decline was answered as an existing request")
	}
	processing := markProcessingOK(t, env, operatorSessionID, second.ID, map[string]any{"transfer_reference": "PP-OUTAGE"})
	if processing.Status != "processing" {
		t.Fatalf("mark processing during an email outage = %+v, want a processing request", processing)
	}

	// Failure: the answer stands, reason and all, with nobody told. The notice
	// nobody received is the ACTIONABLE one, and it still cannot touch the state.
	failed := markFailedOK(t, env, operatorSessionID, second.ID, map[string]any{"reason": "the account number was rejected"})
	if failed.Status != "failed" {
		t.Fatalf("mark failed during an email outage = %+v, want a failed request", failed)
	}
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payouts rows after a failed transfer during an email outage = %d, want 0", count)
	}

	// Fulfilment: the Payout row is the one that must survive. A failed request
	// frees the Organization to ask again, exactly as a declined one does.
	third, createdThird := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "outage, once more"))
	if !createdThird {
		t.Fatalf("the re-ask after a failed transfer was answered as an existing request")
	}
	result := fulfilPayoutRequestOK(t, env, operatorSessionID, third.ID, fulfilBody(payable, "2026-07-20", ""))
	if result.Payout.ID == "" || result.Request.Status != "paid" {
		t.Fatalf("fulfilment during an email outage = %+v, want a recorded Payout and a paid request", result)
	}
	if count := payoutRowCount(t, env, "test-org"); count != 1 {
		t.Fatalf("payouts rows after a fulfilment during an email outage = %d, want 1 — the ledger is the fact", count)
	}

	// Nothing was captured, which is what makes the assertions above load-bearing
	// rather than a re-run of the happy path.
	if s, p, d := env.email.PayoutRequestsSubmitted(), env.email.PayoutRequestsPaid(), env.email.PayoutRequestsDeclined(); len(s)+len(p)+len(d) != 0 {
		t.Fatalf("captured %d/%d/%d submitted/paid/declined notices from a failing sender, want none delivered", len(s), len(p), len(d))
	}
	if sent, bounced := env.email.PayoutRequestTransfersSent(), env.email.PayoutRequestTransfersFailed(); len(sent)+len(bounced) != 0 {
		t.Fatalf("captured %d/%d transfer-sent/transfer-failed notices from a failing sender, want none delivered", len(sent), len(bounced))
	}

	// And the records the organizer reads back agree with what the API said.
	history := listPayoutRequests(t, env, adminSessionID)
	if len(history) != 3 {
		t.Fatalf("request history = %d rows, want the declined ask, the failed one and the paid one", len(history))
	}
	statuses := map[string]bool{}
	for _, row := range history {
		statuses[row.Status] = true
	}
	if !statuses["paid"] || !statuses["declined"] || !statuses["failed"] {
		t.Fatalf("request history statuses = %+v, want one paid, one declined and one failed", statuses)
	}
}
