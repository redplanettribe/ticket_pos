package catalog

import "testing"

// liveDebt is the one combination that IS an Outstanding Answer: a required,
// live question on an active sale that nothing has answered. Every case below
// changes exactly one fact away from it, so each test names the single clause it
// is about.
func liveDebt() OutstandingAnswerInputs {
	return OutstandingAnswerInputs{
		Required:        true,
		QuestionRetired: false,
		SaleStatus:      TicketSaleStatusActive,
		Answered:        false,
	}
}

func TestIsOutstandingAnswerRequiredAndUnanswered(t *testing.T) {
	if !IsOutstandingAnswer(liveDebt()) {
		t.Fatal("a required, live, unanswered question on an active sale is an Outstanding Answer")
	}
}

// The whole meaning of `required`: an OPTIONAL question nobody answered is not a
// debt. Nobody promised to answer it, so there is nothing to chase — and listing
// it would bury the questions that matter under the ones that do not.
func TestIsOutstandingAnswerOptionalQuestionIsNoDebt(t *testing.T) {
	in := liveDebt()
	in.Required = false
	if IsOutstandingAnswer(in) {
		t.Fatal("an unanswered OPTIONAL question must not be an Outstanding Answer")
	}
}

// THE RETIRED-QUESTION RULING. A retired question is refused by every write path
// into an Answer, so a debt under one could never be discharged by anybody. It
// is not owed.
func TestIsOutstandingAnswerRetiredQuestionIsNoDebt(t *testing.T) {
	in := liveDebt()
	in.QuestionRetired = true
	if IsOutstandingAnswer(in) {
		t.Fatal("a RETIRED question must not be an Outstanding Answer: nothing could discharge it")
	}
}

// A retired question that is ALSO optional is doubly not a debt — the two
// clauses are independent, and neither rescues the other.
func TestIsOutstandingAnswerRetiredOptionalQuestionIsNoDebt(t *testing.T) {
	in := liveDebt()
	in.QuestionRetired = true
	in.Required = false
	if IsOutstandingAnswer(in) {
		t.Fatal("a retired optional question must not be an Outstanding Answer")
	}
}

// A reversed Ticket Sale's Tickets never appear. Its tickets have ceased to
// exist and its money has gone back; there is nobody left to chase and no shirt
// to order in their size.
func TestIsOutstandingAnswerReversedSaleIsNoDebt(t *testing.T) {
	in := liveDebt()
	in.SaleStatus = "reversed"
	if IsOutstandingAnswer(in) {
		t.Fatal("a reversed Ticket Sale's Ticket must never carry an Outstanding Answer")
	}
}

// Anything that is not the active status is treated as not standing. The column
// is CHECKed to two values, so this is the safe way to be wrong about a third.
func TestIsOutstandingAnswerUnknownSaleStatusIsNoDebt(t *testing.T) {
	in := liveDebt()
	in.SaleStatus = "something_else"
	if IsOutstandingAnswer(in) {
		t.Fatal("an unrecognised sale status must not produce an Outstanding Answer")
	}
}

// The debt is discharged by the EXISTENCE of an Answer, from whichever of the
// three routes wrote it — staff, checkout capture, or the Answer Link. This
// function cannot tell them apart and must not try: the Answer stands wherever
// it came from.
func TestIsOutstandingAnswerAnsweredDischargesTheDebt(t *testing.T) {
	in := liveDebt()
	in.Answered = true
	if IsOutstandingAnswer(in) {
		t.Fatal("an answered required question is no longer outstanding")
	}
}

// EXISTENCE AND NOT CONTENT. A checkbox answered `false` is an Answer — somebody
// read the question and said no — and it discharges the debt exactly as any
// other reply does. There is no field here for the value, and that absence is
// the point: nothing about this rule may start inspecting what was said.
func TestIsOutstandingAnswerIsAboutExistenceNotContent(t *testing.T) {
	in := liveDebt()
	in.Answered = true
	if IsOutstandingAnswer(in) {
		t.Fatal("an Answer of `false` still discharges the debt; the rule reads existence, not content")
	}
}

// The four clauses are conjunctive: everything must hold at once. A case with
// two facts wrong is still not a debt, which guards against a future rewrite
// that reached for an OR.
func TestIsOutstandingAnswerClausesAreConjunctive(t *testing.T) {
	for name, in := range map[string]OutstandingAnswerInputs{
		"optional and answered":  {Required: false, SaleStatus: TicketSaleStatusActive, Answered: true},
		"retired and reversed":   {Required: true, QuestionRetired: true, SaleStatus: "reversed"},
		"answered and reversed":  {Required: true, SaleStatus: "reversed", Answered: true},
		"nothing true at all":    {},
		"required only, no sale": {Required: true},
	} {
		if IsOutstandingAnswer(in) {
			t.Fatalf("%s: must not be an Outstanding Answer", name)
		}
	}
}
