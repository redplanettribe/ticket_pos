package integration

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The three emails a Payout Request sends (#179, ADR 0026).
//
// This is the platform's FIRST organizer-facing notification channel: every
// other transactional email is a Customer's receipt or a staff sign-in code. The
// three close the three loops that would otherwise each become a support
// message — the operator allowlist learns an ask arrived, and the person who
// asked learns it was paid, or why it was not.
//
// Four properties are pinned here rather than in a unit test, because only real
// HTTP over a real Postgres sees all of them at once:
//
//   - WHO IS TOLD. The submission notice goes to the operator ALLOWLIST — the
//     whole of operator authority (ADR 0015) — and the two answers go to the
//     ONE email recorded on the request as `requested_by`, never to every Org
//     Admin. One request, one asker, one reply.
//
//   - THE DECLINE CARRIES ITS REASON. A queue that swallows requests silently
//     generates the support thread it was built to prevent, and the reason only
//     does that job if it reaches the asker's inbox.
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

// TestPayoutRequestNoticeFailureNeverBlocksTheAnswer is the acceptance criterion
// with the most consequence: with the email provider down, all three actions
// still happen and still stand.
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

	// Fulfilment: the Payout row is the one that must survive. A decline frees
	// the Organization to ask again immediately, which is how there is a second
	// pending request to pay.
	second, createdAgain := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "outage, again"))
	if !createdAgain {
		t.Fatalf("the re-ask after a decline was answered as an existing request")
	}
	result := fulfilPayoutRequestOK(t, env, operatorSessionID, second.ID, fulfilBody(payable, "2026-07-20", ""))
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

	// And the records the organizer reads back agree with what the API said.
	history := listPayoutRequests(t, env, adminSessionID)
	if len(history) != 2 {
		t.Fatalf("request history = %d rows, want the declined ask and the paid one", len(history))
	}
	statuses := map[string]bool{history[0].Status: true, history[1].Status: true}
	if !statuses["paid"] || !statuses["declined"] {
		t.Fatalf("request history statuses = %+v, want one paid and one declined", statuses)
	}
}
