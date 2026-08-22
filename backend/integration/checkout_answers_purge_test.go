package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// Abandoned Payments shed their Answers (#316, ADR 0044): the Answers a buyer
// typed into a checkout that never became a sale are deleted 30 days later, and
// the Payment they were riding on is left exactly as it was.
//
// THIS FILE EXISTS FOR ONE SENTENCE, and every other test here is scaffolding
// around it: the purge may never key on 'expired'. Expiry in this codebase is
// lazy, opportunistic bookkeeping written by whichever begin-checkout happens to
// pass by (repository.ExpireStalePayments), and an 'expired' Payment can still
// flip to 'approved' when the Payment Provider confirms late — which
// capacity_holds_test.go pins as deliberate product behaviour. A purge keyed on
// the status would therefore delete the Answers of a sale that then commits, and
// the buyer would receive Tickets that had forgotten what they told them. The
// 30-day window is the whole of what makes non-approval safe to act on: no
// provider confirms a month late.
//
// Time moves by moving the clock, never by sleeping, and never by backdating a
// row: `created_at` on these Payments is written by the service clock at
// begin-checkout, so moving that clock forward ages them exactly as the calendar
// would. A test that reached into SQL to age a Payment would be asserting
// against its own UPDATE rather than against the product.

// purgeResult is what one run of the purge reports. It is a runbook as much as
// an automation result — the same reason the Reversal Reconciler answers with a
// tally rather than a bare 200 — so an operator running it by hand can tell
// "nothing was due" from "nothing happened".
type purgeResult struct {
	AnswersPurged  int    `json:"answers_purged"`
	PaymentsPurged int    `json:"payments_purged"`
	Cutoff         string `json:"cutoff"`
	AnswersHeld    int    `json:"answers_held"`
}

// purgeAbandonedAnswers runs one purge tick and asserts only that the endpoint
// answered.
//
// It takes no credential, on the same terms as drainReversals: the endpoint is
// authenticated by Cloud Run IAM before the request reaches the API (ADR 0008),
// which is infrastructure this suite does not run. What is exercised here is the
// behaviour behind that gate.
func purgeAbandonedAnswers(t *testing.T, env *testEnv) purgeResult {
	t.Helper()
	resp, body := env.post(t, "/api/v1/internal/checkout-answers/purge", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("purge status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var out purgeResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode purge result: %v", err)
	}
	return out
}

// countHeldAnswerOptions counts the chosen Options riding a Payment.
//
// Asserted SEPARATELY from the Answers they belong to, because they are a second
// table reached only by cascade (migration 074). A purge that deleted the
// Answers and orphaned their Options would leave the health data this feature
// exists to remove — the labels of what somebody picked — sitting in the
// database, and the Answer count alone would report success.
func countHeldAnswerOptions(t *testing.T, env *testEnv, clientTransactionID string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`
		SELECT COUNT(*)
		FROM payment_ticket_answer_options o
		JOIN payment_ticket_answers a ON a.id = o.payment_ticket_answer_id
		JOIN payment_lines l ON l.id = a.payment_line_id
		JOIN payments p ON p.id = l.payment_id
		WHERE p.client_transaction_id = $1
	`, clientTransactionID).Scan(&n); err != nil {
		t.Fatalf("count held answer options: %v", err)
	}
	return n
}

// countPaymentLines counts a Payment's lines, which the purge must not touch.
//
// The Payment is kept forever and its lines with it: they are what the platform
// knows about an attempt to buy, and the retention rule is about the Answers
// alone. A DELETE that walked the wrong way down the foreign key would take the
// lines with it, and — because `payment_lines` cascades from `payments` and not
// the other way — the Payment row would survive to make it look harmless.
func countPaymentLines(t *testing.T, env *testEnv, clientTransactionID string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`
		SELECT COUNT(*)
		FROM payment_lines l
		JOIN payments p ON p.id = l.payment_id
		WHERE p.client_transaction_id = $1
	`, clientTransactionID).Scan(&n); err != nil {
		t.Fatalf("count payment lines: %v", err)
	}
	return n
}

// abandonedCheckoutWithAnswers begins a checkout that answers both of the
// fixture's questions and never confirms it, returning the Payment left behind.
//
// Two answers of two different shapes — a short_text and a single_choice — so
// that both halves of the held representation are staged: the typed column and
// the Option snapshot in the second table.
func abandonedCheckoutWithAnswers(t *testing.T, env *testEnv, ticketTypeID, email, first, last string, size, meal ticketQuestion) beginCheckoutResult {
	t.Helper()
	body := checkoutBody(email, first, last, cartLine(ticketTypeID, 1))
	body["answers"] = []map[string]any{
		checkoutAnswer(ticketTypeID, 1, size.ID, map[string]any{"text": "M"}),
		checkoutAnswer(ticketTypeID, 1, meal.ID, map[string]any{"option_ids": []string{meal.Options[0].ID}}),
	}
	begun := beginCheckoutOK(t, env, "test-org", "answer-fest", body)
	if held := countHeldAnswers(t, env, begun.ClientTransactionID); held != 2 {
		t.Fatalf("the Payment holds %d Answers, want 2 — the fixture never staged anything to purge", held)
	}
	return begun
}

// TestAnExpiredPaymentInsideTheWindowKeepsItsAnswersAndStillApproves is the
// ticket, and the one test here whose deletion would cost the most.
//
// A buyer answers, is handed to the Payment Provider, and hesitates past the
// 20-minute Capacity Hold window. The next begin-checkout to pass by flips their
// Payment to 'expired' — bookkeeping, not a verdict (ADR 0013). The purge runs
// in that moment and must do NOTHING: the Payment is twenty-one minutes old, and
// nothing about it is settled.
//
// Then the provider confirms, as it is entitled to, and the sale commits with
// the Answers intact on the minted Ticket. A purge keyed on 'expired' would pass
// every other test in this file and fail exactly here — which is why the confirm
// is part of the test rather than a separate one. Deleting the rows and then
// approving is silent: the commit copies nothing, the buyer keeps their Tickets,
// and the Organization is simply told nobody answered.
func TestAnExpiredPaymentInsideTheWindowKeepsItsAnswersAndStillApproves(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID, size, meal := checkoutQuestionFixture(t, env)

	patient := abandonedCheckoutWithAnswers(t, env, ticketTypeID, "pat@example.com", "Pat", "Patient", size, meal)

	// Past the hold window a fresh begin lazily expires the stale pending, which
	// is the only way an 'expired' Payment comes into being in this product.
	// Staging it with an UPDATE would prove nothing about the real transition.
	holdClocksAt(afterHoldWindow())
	beginCheckoutOK(t, env, "test-org", "answer-fest",
		checkoutBody("noa@example.com", "Noa", "New", cartLine(ticketTypeID, 1)))
	if got := paymentStatus(t, env, patient.ClientTransactionID); got != "expired" {
		t.Fatalf("stale payment status = %q, want expired — the test never reached the state it is about", got)
	}

	// The purge runs while the Payment sits 'expired' and twenty-one minutes old.
	result := purgeAbandonedAnswers(t, env)
	if result.AnswersPurged != 0 {
		t.Fatalf("purge deleted %d Answers from a Payment inside the window, want 0 — "+
			"an expired Payment can still flip to approved, and this is that theft", result.AnswersPurged)
	}
	if held := countHeldAnswers(t, env, patient.ClientTransactionID); held != 2 {
		t.Fatalf("the expired Payment holds %d Answers after a purge inside the window, want 2", held)
	}
	if options := countHeldAnswerOptions(t, env, patient.ClientTransactionID); options != 1 {
		t.Fatalf("the expired Payment holds %d chosen Options after the purge, want 1", options)
	}

	// The provider confirms late. Capacity remains, so the sale commits and the
	// Payment flips expired → approved — with everything the buyer typed.
	confirm := confirmCheckoutOK(t, env, patient.ClientTransactionID, "approved")
	if confirm.Status != "approved" || confirm.ConfirmationRef == "" {
		t.Fatalf("late confirm = %+v, want approved with a reference", confirm)
	}
	saleID := saleIDOfPayment(t, env, patient.ClientTransactionID)
	tickets := saleTickets(t, env, sessionID, eventID, saleID)
	if len(tickets) != 1 {
		t.Fatalf("the revived sale minted %d Tickets, want 1", len(tickets))
	}
	if got := answerTo(t, tickets[0], size.ID); got == nil || *got != "M" {
		t.Fatalf("the revived Ticket answers the size question %v, want \"M\" — "+
			"the Answers did not survive the window they were supposed to", got)
	}
	if got := chosenOptionLabels(t, tickets[0], meal.ID); len(got) != 1 || got[0] != "Chicken" {
		t.Fatalf("the revived Ticket chose %v, want [Chicken]", got)
	}
}

// TestAbandonedAnswersArePurgedAfterThirtyDays is the acceptance criterion: a
// Payment that never reached 'approved' loses its Answers, and loses nothing
// else.
//
// The abandoned Payment here is still 'pending' when the purge finds it, and
// that is deliberate: nothing ever expired it, because in this product nothing
// is guaranteed to. Lazy expiry only happens where another buyer turns up at the
// same Event, so a quiet Event's abandoned Payments stay 'pending' forever — and
// a purge that looked for 'expired' would never delete a single one of them.
func TestAbandonedAnswersArePurgedAfterThirtyDays(t *testing.T) {
	env := setupTest(t)
	_, _, ticketTypeID, size, meal := checkoutQuestionFixture(t, env)

	abandoned := abandonedCheckoutWithAnswers(t, env, ticketTypeID, "gone@example.com", "Gia", "Gone", size, meal)

	holdClocksAt(fixedClock.Add(sales.AbandonedAnswerRetention + time.Hour))
	result := purgeAbandonedAnswers(t, env)
	if result.AnswersPurged != 2 || result.PaymentsPurged != 1 {
		t.Fatalf("purge = %+v, want 2 Answers off 1 Payment", result)
	}
	if held := countHeldAnswers(t, env, abandoned.ClientTransactionID); held != 0 {
		t.Fatalf("the abandoned Payment still holds %d Answers, want 0", held)
	}
	if options := countHeldAnswerOptions(t, env, abandoned.ClientTransactionID); options != 0 {
		t.Fatalf("the abandoned Payment still holds %d chosen Options, want 0 — "+
			"the Answers went and the words they picked stayed", options)
	}

	// The Payment and its lines are untouched. This is not a delete of a Payment.
	if got := paymentStatus(t, env, abandoned.ClientTransactionID); got != "pending" {
		t.Fatalf("payment status = %q after the purge, want pending — the purge rewrote the Payment", got)
	}
	if got := countPaymentLines(t, env, abandoned.ClientTransactionID); got != 1 {
		t.Fatalf("payment lines = %d after the purge, want 1", got)
	}

	// Idempotent: the second run finds nothing, rather than erroring or
	// reporting the same work twice.
	if again := purgeAbandonedAnswers(t, env); again.AnswersPurged != 0 || again.PaymentsPurged != 0 {
		t.Fatalf("second purge = %+v, want zeros — the job is not idempotent", again)
	}
}

// TestAnApprovedPaymentKeepsItsAnswersForever pins the other half of the
// predicate. The Answers on an approved Payment have already been copied onto
// Tickets, so deleting them would destroy nothing a holder can see — which is
// exactly why it would go unnoticed for years, and exactly why it is asserted.
//
// The held rows on an approved Payment are the record of what the BUYER typed at
// checkout, as distinct from what the Ticket says now, which Event Staff and the
// holder may both have changed since. Age is no reason to lose that.
func TestAnApprovedPaymentKeepsItsAnswersForever(t *testing.T) {
	env := setupTest(t)
	_, _, ticketTypeID, size, meal := checkoutQuestionFixture(t, env)

	sold := abandonedCheckoutWithAnswers(t, env, ticketTypeID, "sal@example.com", "Sal", "Sold", size, meal)
	confirmCheckoutOK(t, env, sold.ClientTransactionID, "approved")

	// A year later, which is many times the retention window.
	holdClocksAt(fixedClock.Add(365 * 24 * time.Hour))
	result := purgeAbandonedAnswers(t, env)
	if result.AnswersPurged != 0 {
		t.Fatalf("purge deleted %d Answers from an approved Payment, want 0", result.AnswersPurged)
	}
	if held := countHeldAnswers(t, env, sold.ClientTransactionID); held != 2 {
		t.Fatalf("the approved Payment holds %d Answers a year on, want 2", held)
	}
	if options := countHeldAnswerOptions(t, env, sold.ClientTransactionID); options != 1 {
		t.Fatalf("the approved Payment holds %d chosen Options a year on, want 1", options)
	}
}

// TestAbandonedAnswersSurviveUntilTheWindowCloses pins where the boundary is.
//
// Without it, every assertion above would pass on a purge whose window was a
// week, or an hour — the abandoned cases are all far past 30 days and the
// expired-revives case is minutes old, so the two say nothing about what happens
// in between. A window that quietly shortened would be invisible, and its cost
// would be paid by buyers who took a fortnight to sort out a bank transfer.
func TestAbandonedAnswersSurviveUntilTheWindowCloses(t *testing.T) {
	env := setupTest(t)
	_, _, ticketTypeID, size, meal := checkoutQuestionFixture(t, env)

	slow := abandonedCheckoutWithAnswers(t, env, ticketTypeID, "wen@example.com", "Wen", "Wait", size, meal)

	// One hour short of thirty days: still the buyer's, still theirs to complete.
	holdClocksAt(fixedClock.Add(sales.AbandonedAnswerRetention - time.Hour))
	if result := purgeAbandonedAnswers(t, env); result.AnswersPurged != 0 {
		t.Fatalf("purge deleted %d Answers an hour before the window closed, want 0", result.AnswersPurged)
	}
	if held := countHeldAnswers(t, env, slow.ClientTransactionID); held != 2 {
		t.Fatalf("the Payment holds %d Answers an hour before the window closed, want 2", held)
	}

	// Two hours later the window has closed and the same run deletes them.
	holdClocksAt(fixedClock.Add(sales.AbandonedAnswerRetention + time.Hour))
	if result := purgeAbandonedAnswers(t, env); result.AnswersPurged != 2 {
		t.Fatalf("purge deleted %d Answers an hour after the window closed, want 2", result.AnswersPurged)
	}
}

// chosenOptionLabels returns the labels a Ticket's choice Answer picked, in the
// order the staff surface lists them. The SNAPSHOT label is read, not the
// current one: what is being asserted is that the words the buyer read at
// checkout survived the wait, which is the whole point of storing them.
func chosenOptionLabels(t *testing.T, ticket ticketAnswers, questionID string) []string {
	t.Helper()
	for _, entry := range ticket.Questions {
		if entry.Question.ID != questionID {
			continue
		}
		if entry.Answer == nil {
			return nil
		}
		labels := make([]string, 0, len(entry.Answer.Options))
		for _, option := range entry.Answer.Options {
			labels = append(labels, option.Label)
		}
		return labels
	}
	t.Fatalf("ticket %d was never asked question %s", ticket.Ordinal, questionID)
	return nil
}
