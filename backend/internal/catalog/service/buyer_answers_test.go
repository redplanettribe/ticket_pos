package service

import "testing"

// outstandingAnswerCount is the buyer's half of a definition that already lives
// twice on purpose (#313): once in catalog.IsOutstandingAnswer and once in the
// SQL behind the Organization's chase list. These tests exist to hold this
// caller to the SHARED rule rather than to re-test the rule itself — the four
// clauses have their own tests in the catalog package, and duplicating them here
// would be the third statement of the debt that #313 exists to prevent.
//
// What is asserted here is the mapping: that this function's view of a question
// reaches those clauses intact, and in particular that it agrees with the
// Organization about which Tickets owe something. A buyer told they owe nothing
// while the chase list still names them is the failure this guards.

func answeredPair(required, retired bool, answered bool) TicketQuestionAnswerView {
	pair := TicketQuestionAnswerView{
		Question: TicketQuestionView{Required: required, Retired: retired},
	}
	if answered {
		pair.Answer = &AnswerView{}
	}
	return pair
}

func TestOutstandingAnswerCountCountsRequiredUnansweredQuestions(t *testing.T) {
	questions := []TicketQuestionAnswerView{
		answeredPair(true, false, false),
		answeredPair(true, false, false),
		answeredPair(true, false, true),
	}
	if got := outstandingAnswerCount("active", questions); got != 2 {
		t.Fatalf("outstandingAnswerCount = %d, want 2", got)
	}
}

// The OPTIONAL question is not a debt, and neither is the RETIRED one — the two
// clauses that decide most of what a buyer is shown as owing. A page that
// counted either would send buyers chasing friends over questions nobody
// promised to answer, or over questions no write path would even accept.
func TestOutstandingAnswerCountIgnoresOptionalAndRetiredQuestions(t *testing.T) {
	questions := []TicketQuestionAnswerView{
		answeredPair(false, false, false), // optional, unanswered
		answeredPair(true, true, false),   // required but retired
		answeredPair(false, true, false),  // both
	}
	if got := outstandingAnswerCount("active", questions); got != 0 {
		t.Fatalf("outstandingAnswerCount = %d, want 0", got)
	}
}

// A reversed Ticket Sale owes nothing, however many required questions its
// Tickets never answered. Its tickets have ceased to exist and its money has
// gone back — and this is the clause the buyer's surface would most plausibly
// get wrong on its own, because a reversed Sale is still SHOWN in the Customer
// Area and its questions are still readable on it.
func TestOutstandingAnswerCountIsZeroOnAReversedSale(t *testing.T) {
	questions := []TicketQuestionAnswerView{
		answeredPair(true, false, false),
		answeredPair(true, false, false),
	}
	if got := outstandingAnswerCount("reversed", questions); got != 0 {
		t.Fatalf("outstandingAnswerCount = %d on a reversed Sale, want 0", got)
	}
}

// EXISTENCE AND NOT CONTENT. A checkbox answered `false` is an Answer and
// discharges the debt; a blank Answer is never stored, so the pointer's presence
// is the whole test. Asserted here because "answered false" is exactly the case
// a surface counting truthiness instead of presence would get wrong.
func TestOutstandingAnswerCountTreatsAnyAnswerAsDischarging(t *testing.T) {
	no := false
	questions := []TicketQuestionAnswerView{{
		Question: TicketQuestionView{Required: true},
		Answer:   &AnswerView{Checked: &no},
	}}
	if got := outstandingAnswerCount("active", questions); got != 0 {
		t.Fatalf("outstandingAnswerCount = %d, want 0: a checkbox answered false is an Answer", got)
	}
}

// A Ticket Type with no questions at all owes nothing, which is the state of
// every Ticket ever sold by an Organization that never wrote a Ticket Question —
// that is, almost all of them.
func TestOutstandingAnswerCountIsZeroWithNoQuestions(t *testing.T) {
	if got := outstandingAnswerCount("active", nil); got != 0 {
		t.Fatalf("outstandingAnswerCount = %d, want 0", got)
	}
}
