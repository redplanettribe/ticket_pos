package catalog

// The checkout capture: what survives of a buyer's answers on their way onto a
// Payment (#311, ADR 0044).
//
// THE ENTIRE SUBJECT OF THIS FILE IS ONE RULE. Nothing about a Ticket Question
// may refuse or delay a checkout — not a required question left blank, not an
// answer of the wrong shape, not an id that names nothing. So the function below
// RETURNS NO ERROR, and that is a decision rather than an omission: a signature
// with an error in it is a signature a caller can propagate, and the day
// somebody wires that error to the checkout's return value is the day a t-shirt
// size can cost a sale. What does not fit is DROPPED, silently, and the Ticket
// it was meant for simply carries an Outstanding Answer — which is a debt the
// Organization can see and chase, and exactly what `required` was defined to
// mean (ADR 0044, migration 072).
//
// IT IS PURE, AND SITS IN THE DOMAIN PACKAGE RATHER THAN IN THE SALES SERVICE,
// for the reason ParseAnswer does: this is a rule about what a Ticket Question
// can be answered with, and the sales module is a caller of that rule and not a
// second author of it. The caller supplies the questions and the Options; this
// decides what is keepable.

// AskedQuestion is one Ticket Question as the checkout form actually put it to a
// buyer: which Ticket Type it belongs to, what kind of reply it takes, and — for
// the two choice kinds — the Options it offered.
//
// THE OPTIONS ARE THE LIVE ONES AND ONLY THE LIVE ONES, which the caller
// guarantees. A retired Option is gone from new lists, and this is a new list:
// nobody at checkout was shown one, so an id naming one is a stale form or a
// crafted body and is dropped by simply not being here. That is why there is no
// Retired flag on AskedOption — a distinction this surface cannot draw is a
// distinction it should not carry. (The staff correction path DOES keep a
// retired Option that an existing Answer already chose, because there discarding
// it would destroy what somebody said; here there is no Answer yet to protect.)
type AskedQuestion struct {
	ID           string
	TicketTypeID string
	Kind         TicketQuestionKind
	Options      []AskedOption
}

// AskedOption is one selectable value as it read on the checkout form. The label
// travels because it is what gets snapshotted: an Answer keeps its own copy of
// the words the person actually read, and at checkout this is them.
type AskedOption struct {
	ID    string
	Label string
}

// SubmittedCheckoutAnswer is one answer as the buyer's browser stated it: which
// Ticket Type, which of that Ticket Type's units, which question, and the reply.
//
// TicketIndex is ONE-BASED and names one of the line's units, 1..quantity. It is
// the number that becomes `tickets.ordinal` when the sale commits, and the two
// being the same number is the whole of how "written onto the minted Tickets in
// order" works (migrations 070 and 074).
//
// It is UNTRUSTED in every field. The Ticket Type may not be in the cart, the
// question may belong to somebody else's Ticket Type, and the index may point
// past a quantity that only the server knows.
type SubmittedCheckoutAnswer struct {
	TicketTypeID     string
	TicketIndex      int
	TicketQuestionID string
	Answer           SubmittedAnswer
}

// HeldAnswer is one Answer that will be held on the Payment: validated against
// its question's kind, with its chosen Options resolved to identities and the
// words they showed.
type HeldAnswer struct {
	TicketTypeID     string
	TicketIndex      int
	TicketQuestionID string
	Value            AnswerValue
	// Options are the chosen Options in the order they were given, empty for the
	// five kinds that are not answered by choosing.
	Options []HeldAnswerOption
}

// HeldAnswerOption is one chosen Option: its stable identity, and the words it
// read at the moment of choosing.
type HeldAnswerOption struct {
	TicketQuestionOptionID string
	LabelSnapshot          string
}

// HoldableCheckoutAnswers reduces what a buyer submitted at checkout to what
// this platform will hold on their Payment, dropping everything else.
//
// `asked` is every question the cart's Ticket Types actually put to this buyer,
// `quantities` is how many of each Ticket Type the cart holds — AGGREGATED per
// Ticket Type, exactly as begin-checkout aggregates its lines, because a
// Payment Line is one per Ticket Type and the index counts against that line —
// and `submitted` is the browser's list.
//
// An answer is kept only when ALL of the following hold, and dropped without
// comment otherwise:
//
//   - its Ticket Type is in the cart;
//   - its index is between 1 and that Ticket Type's quantity, so it names a
//     Ticket that will actually be minted;
//   - its question is one that Ticket Type asks;
//   - ParseAnswer accepts the reply for that question's kind;
//   - every Option it names is one the question currently offers.
//
// The output preserves the submission's order and holds at most one answer per
// (Ticket Type, index, question) — the first, so that a body naming the same
// Ticket's question twice resolves here rather than at the UNIQUE, where a
// duplicate would be an error on a path that may not have them.
func HoldableCheckoutAnswers(
	asked []AskedQuestion,
	quantities map[string]int,
	submitted []SubmittedCheckoutAnswer,
) []HeldAnswer {
	if len(asked) == 0 || len(submitted) == 0 {
		return nil
	}

	// Indexed by question id ALONE, with the Ticket Type checked afterwards. A
	// question id is globally unique, so a map keyed on it can answer "is this a
	// real question" and "whose is it" in one lookup — and a buyer naming
	// another Ticket Type's question is then a mismatch rather than a miss,
	// which is what the test asserts and what a nested map would have blurred.
	byQuestion := make(map[string]AskedQuestion, len(asked))
	for _, question := range asked {
		byQuestion[question.ID] = question
	}

	held := make([]HeldAnswer, 0, len(submitted))
	seen := make(map[heldKey]bool, len(submitted))
	for _, entry := range submitted {
		question, ok := byQuestion[entry.TicketQuestionID]
		if !ok || question.TicketTypeID != entry.TicketTypeID {
			continue
		}
		// The index must name a unit this cart is actually buying. Both bounds
		// matter: below 1 there is no ordinal to land on, and above the quantity
		// there is no Ticket. An answer for a Ticket that will not exist is not
		// merely useless — held, it would sit on the Payment forever with
		// nothing to write it onto.
		quantity, inCart := quantities[entry.TicketTypeID]
		if !inCart || entry.TicketIndex < 1 || entry.TicketIndex > quantity {
			continue
		}

		key := heldKey{
			ticketTypeID: entry.TicketTypeID,
			ticketIndex:  entry.TicketIndex,
			questionID:   entry.TicketQuestionID,
		}
		if seen[key] {
			continue
		}

		// The per-kind rules, reused rather than restated. Every route into an
		// Answer goes through ParseAnswer; this one differs only in what it does
		// with the refusal, which is nothing.
		value, problem := ParseAnswer(question.Kind, entry.Answer)
		if problem != AnswerOK {
			continue
		}

		options, ok := chosenOptions(question, value.OptionIDs)
		if !ok {
			continue
		}

		seen[key] = true
		held = append(held, HeldAnswer{
			TicketTypeID:     entry.TicketTypeID,
			TicketIndex:      entry.TicketIndex,
			TicketQuestionID: entry.TicketQuestionID,
			Value:            value,
			Options:          options,
		})
	}
	if len(held) == 0 {
		return nil
	}
	return held
}

// heldKey is the (Ticket Type, index, question) triple that identifies one
// Answer on a Payment — migration 074's UNIQUE, in Go, so that a duplicate is
// resolved here instead of becoming an INSERT error on a path that must not
// have any.
type heldKey struct {
	ticketTypeID string
	ticketIndex  int
	questionID   string
}

// chosenOptions resolves the ids a choice Answer named against the Options its
// question offered, taking a snapshot of each Option's words on the way past.
//
// It reports failure for ANY unresolvable id rather than dropping that one id
// and keeping the rest, and the asymmetry with the rest of this file is
// deliberate. Dropping one Option silently rewrites what somebody said — a
// buyer who ticked Chicken and Fish would be recorded as having said Chicken
// alone, which is not a gap but a wrong answer, and a wrong answer is worse than
// an Outstanding one. An Answer is atomic; it is kept whole or not at all.
//
// The snapshot is the LABEL AS IT READS NOW, which is the label the form showed:
// the caller loaded these Options to build the form the buyer just submitted.
func chosenOptions(question AskedQuestion, optionIDs []string) ([]HeldAnswerOption, bool) {
	if len(optionIDs) == 0 {
		return nil, true
	}
	offered := make(map[string]string, len(question.Options))
	for _, option := range question.Options {
		offered[option.ID] = option.Label
	}

	chosen := make([]HeldAnswerOption, 0, len(optionIDs))
	for _, id := range optionIDs {
		label, ok := offered[id]
		if !ok {
			return nil, false
		}
		chosen = append(chosen, HeldAnswerOption{TicketQuestionOptionID: id, LabelSnapshot: label})
	}
	return chosen, true
}
