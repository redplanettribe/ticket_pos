package catalog

import "testing"

// The checkout capture's one rule, asserted from every angle it can be broken:
// a buyer's Answer is either kept or DROPPED, and nothing about it ever produces
// a refusal (ADR 0044). These tests are the reason the function returns no
// error.

func answerPtr[T any](v T) *T { return &v }

// askedGA is the shape most of these tests hang off: one Ticket Type asking one
// short_text question, with three units bought.
func askedGA() ([]AskedQuestion, map[string]int) {
	return []AskedQuestion{{
			ID:           "q-size",
			TicketTypeID: "tt-ga",
			Kind:         TicketQuestionKindShortText,
		}}, map[string]int{"tt-ga": 3}
}

func TestHoldableCheckoutAnswersKeepsAWellFormedAnswer(t *testing.T) {
	asked, quantities := askedGA()
	held := HoldableCheckoutAnswers(asked, quantities, []SubmittedCheckoutAnswer{{
		TicketTypeID:     "tt-ga",
		TicketIndex:      2,
		TicketQuestionID: "q-size",
		Answer:           SubmittedAnswer{Text: answerPtr("  Medium  ")},
	}})

	if len(held) != 1 {
		t.Fatalf("held %d answers, want 1", len(held))
	}
	if held[0].TicketIndex != 2 || held[0].TicketQuestionID != "q-size" {
		t.Fatalf("held the wrong answer: %+v", held[0])
	}
	// Trimmed by ParseAnswer and by nothing here: this function reuses the
	// per-kind rules rather than restating them.
	if held[0].Value.Text != "Medium" {
		t.Fatalf("text = %q, want %q", held[0].Value.Text, "Medium")
	}
}

// A cart of three of one Ticket Type presents three separate answer sets, and
// all three are held apart. This is the acceptance criterion that the Ticket —
// not the sale — is what an Answer belongs to (ADR 0043).
func TestHoldableCheckoutAnswersKeepsOneAnswerPerTicket(t *testing.T) {
	asked, quantities := askedGA()
	held := HoldableCheckoutAnswers(asked, quantities, []SubmittedCheckoutAnswer{
		{TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-size", Answer: SubmittedAnswer{Text: answerPtr("S")}},
		{TicketTypeID: "tt-ga", TicketIndex: 2, TicketQuestionID: "q-size", Answer: SubmittedAnswer{Text: answerPtr("M")}},
		{TicketTypeID: "tt-ga", TicketIndex: 3, TicketQuestionID: "q-size", Answer: SubmittedAnswer{Text: answerPtr("L")}},
	})

	if len(held) != 3 {
		t.Fatalf("held %d answers, want 3", len(held))
	}
	for i, want := range []string{"S", "M", "L"} {
		if held[i].TicketIndex != i+1 || held[i].Value.Text != want {
			t.Fatalf("answer %d = %+v, want index %d text %q", i, held[i], i+1, want)
		}
	}
}

// Skipping every question is the ordinary case, not an error: it holds nothing
// and says nothing about it.
func TestHoldableCheckoutAnswersHoldsNothingWhenNothingIsAnswered(t *testing.T) {
	asked, quantities := askedGA()
	if held := HoldableCheckoutAnswers(asked, quantities, nil); len(held) != 0 {
		t.Fatalf("held %d answers for an empty submission, want 0", len(held))
	}
}

// Everything a stricter surface would REFUSE, this one DROPS. Each case is a
// thing the staff API answers with a 400 and the checkout answers with tickets.
func TestHoldableCheckoutAnswersDropsRatherThanRefuses(t *testing.T) {
	asked := []AskedQuestion{
		{ID: "q-size", TicketTypeID: "tt-ga", Kind: TicketQuestionKindShortText},
		{ID: "q-age", TicketTypeID: "tt-ga", Kind: TicketQuestionKindNumber},
		{ID: "q-meal", TicketTypeID: "tt-ga", Kind: TicketQuestionKindSingleChoice, Options: []AskedOption{
			{ID: "opt-chicken", Label: "Chicken"},
		}},
		{ID: "q-vip", TicketTypeID: "tt-vip", Kind: TicketQuestionKindShortText},
	}
	quantities := map[string]int{"tt-ga": 2}

	for _, tc := range []struct {
		name      string
		submitted SubmittedCheckoutAnswer
	}{
		{"an unknown question", SubmittedCheckoutAnswer{
			TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-nope",
			Answer: SubmittedAnswer{Text: answerPtr("hello")},
		}},
		{"another Ticket Type's question", SubmittedCheckoutAnswer{
			TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-vip",
			Answer: SubmittedAnswer{Text: answerPtr("hello")},
		}},
		{"a Ticket Type not in the cart", SubmittedCheckoutAnswer{
			TicketTypeID: "tt-vip", TicketIndex: 1, TicketQuestionID: "q-vip",
			Answer: SubmittedAnswer{Text: answerPtr("hello")},
		}},
		{"an index past the quantity", SubmittedCheckoutAnswer{
			TicketTypeID: "tt-ga", TicketIndex: 3, TicketQuestionID: "q-size",
			Answer: SubmittedAnswer{Text: answerPtr("hello")},
		}},
		{"a zero index", SubmittedCheckoutAnswer{
			TicketTypeID: "tt-ga", TicketIndex: 0, TicketQuestionID: "q-size",
			Answer: SubmittedAnswer{Text: answerPtr("hello")},
		}},
		{"a negative index", SubmittedCheckoutAnswer{
			TicketTypeID: "tt-ga", TicketIndex: -1, TicketQuestionID: "q-size",
			Answer: SubmittedAnswer{Text: answerPtr("hello")},
		}},
		{"the wrong shape for the kind", SubmittedCheckoutAnswer{
			TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-age",
			Answer: SubmittedAnswer{Text: answerPtr("forty")},
		}},
		{"something that is not a number", SubmittedCheckoutAnswer{
			TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-age",
			Answer: SubmittedAnswer{Number: answerPtr("forty")},
		}},
		{"an empty reply", SubmittedCheckoutAnswer{
			TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-size",
			Answer: SubmittedAnswer{Text: answerPtr("   ")},
		}},
		{"an Option this question does not offer", SubmittedCheckoutAnswer{
			TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-meal",
			Answer: SubmittedAnswer{OptionIDs: []string{"opt-somebody-elses"}},
		}},
		{"two Options on a single_choice question", SubmittedCheckoutAnswer{
			TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-meal",
			Answer: SubmittedAnswer{OptionIDs: []string{"opt-chicken", "opt-chicken"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if held := HoldableCheckoutAnswers(asked, quantities, []SubmittedCheckoutAnswer{tc.submitted}); len(held) != 0 {
				t.Fatalf("held %+v, want nothing held", held)
			}
		})
	}
}

// One bad answer must not take a good one with it. A buyer who fills three
// fields and fat-fingers the fourth keeps three.
func TestHoldableCheckoutAnswersDropsOnlyTheOffendingAnswer(t *testing.T) {
	asked := []AskedQuestion{
		{ID: "q-size", TicketTypeID: "tt-ga", Kind: TicketQuestionKindShortText},
		{ID: "q-age", TicketTypeID: "tt-ga", Kind: TicketQuestionKindNumber},
	}
	held := HoldableCheckoutAnswers(asked, map[string]int{"tt-ga": 1}, []SubmittedCheckoutAnswer{
		{TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-age", Answer: SubmittedAnswer{Number: answerPtr("nope")}},
		{TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-size", Answer: SubmittedAnswer{Text: answerPtr("M")}},
	})

	if len(held) != 1 || held[0].TicketQuestionID != "q-size" {
		t.Fatalf("held %+v, want only the short_text answer", held)
	}
}

// A choice Answer resolves to the Option's IDENTITY plus a SNAPSHOT of the words
// it showed. The snapshot is what makes a later rename legible.
func TestHoldableCheckoutAnswersSnapshotsTheWordsTheBuyerRead(t *testing.T) {
	asked := []AskedQuestion{{
		ID: "q-meal", TicketTypeID: "tt-ga", Kind: TicketQuestionKindMultiChoice,
		Options: []AskedOption{
			{ID: "opt-chicken", Label: "Chicken"},
			{ID: "opt-veg", Label: "Vegetarian"},
		},
	}}
	held := HoldableCheckoutAnswers(asked, map[string]int{"tt-ga": 1}, []SubmittedCheckoutAnswer{{
		TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-meal",
		// Given veg-first: the order is kept, because a multi_choice Answer
		// reads back the way it was given.
		Answer: SubmittedAnswer{OptionIDs: []string{"opt-veg", "opt-chicken"}},
	}})

	if len(held) != 1 {
		t.Fatalf("held %d answers, want 1", len(held))
	}
	got := held[0].Options
	if len(got) != 2 {
		t.Fatalf("chose %d options, want 2", len(got))
	}
	if got[0].TicketQuestionOptionID != "opt-veg" || got[0].LabelSnapshot != "Vegetarian" {
		t.Fatalf("option 0 = %+v", got[0])
	}
	if got[1].TicketQuestionOptionID != "opt-chicken" || got[1].LabelSnapshot != "Chicken" {
		t.Fatalf("option 1 = %+v", got[1])
	}
}

// FALSE IS AN ANSWER: somebody who read "I will attend the dinner" and left it
// unticked has said no, and that is a fact worth holding — distinct from never
// having been asked, which is an Outstanding Answer and no row at all.
func TestHoldableCheckoutAnswersHoldsAnUntickedCheckbox(t *testing.T) {
	asked := []AskedQuestion{{ID: "q-dinner", TicketTypeID: "tt-ga", Kind: TicketQuestionKindCheckbox}}
	held := HoldableCheckoutAnswers(asked, map[string]int{"tt-ga": 1}, []SubmittedCheckoutAnswer{{
		TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-dinner",
		Answer: SubmittedAnswer{Checked: answerPtr(false)},
	}})

	if len(held) != 1 || held[0].Value.Checked {
		t.Fatalf("held %+v, want one answer of false", held)
	}
}

// The same (Ticket, question) answered twice in one body is one Answer, and it
// is the FIRST that stands. A well-formed form never does this; a hand-crafted
// body would otherwise decide the outcome by row order at the database, and the
// UNIQUE would refuse the second — which on this path must never be a refusal.
func TestHoldableCheckoutAnswersKeepsTheFirstOfADuplicatePair(t *testing.T) {
	asked, quantities := askedGA()
	held := HoldableCheckoutAnswers(asked, quantities, []SubmittedCheckoutAnswer{
		{TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-size", Answer: SubmittedAnswer{Text: answerPtr("first")}},
		{TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-size", Answer: SubmittedAnswer{Text: answerPtr("second")}},
	})

	if len(held) != 1 || held[0].Value.Text != "first" {
		t.Fatalf("held %+v, want one answer reading %q", held, "first")
	}
}

// A retired Option is gone from new lists, so nothing at checkout may choose
// one: this surface only ever shows LIVE Options, and an id for a retired one
// is a stale form or a crafted body. "Kept on the Tickets that chose it" is
// about Answers that already exist, and no Answer exists yet here.
func TestHoldableCheckoutAnswersDropsARetiredOption(t *testing.T) {
	asked := []AskedQuestion{{
		ID: "q-meal", TicketTypeID: "tt-ga", Kind: TicketQuestionKindSingleChoice,
		// The caller passes LIVE Options only; a retired one simply is not here,
		// which is why the drop needs no flag of its own.
		Options: []AskedOption{{ID: "opt-chicken", Label: "Chicken"}},
	}}
	held := HoldableCheckoutAnswers(asked, map[string]int{"tt-ga": 1}, []SubmittedCheckoutAnswer{{
		TicketTypeID: "tt-ga", TicketIndex: 1, TicketQuestionID: "q-meal",
		Answer: SubmittedAnswer{OptionIDs: []string{"opt-retired-fish"}},
	}})

	if len(held) != 0 {
		t.Fatalf("held %+v, want nothing held", held)
	}
}
