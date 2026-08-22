package catalog

// This file holds the definition of an OUTSTANDING ANSWER, and it is the only
// place that definition lives (#313).
//
// An Outstanding Answer is a required Ticket Question that one Ticket has not
// answered yet — A DEBT, NOT A DEFECT. It is the whole meaning of "required" on
// this platform: an Answer that is owed and visible as owed, rather than a gate.
// Nothing anywhere is refused for want of one, on any channel.
//
// IT IS DERIVED AND NEVER STORED. There is no outstanding_answers table and
// there must never be one: the fact is a join between what a Ticket Type asks
// and what a Ticket has said, and both sides move constantly — a question is
// added, retired or made optional, an Answer arrives from checkout, from an
// Answer Link or from Event Staff, a Sale is reversed. A stored copy would be a
// second answer to a question the rows already answer, and every one of those
// six events would be a chance for it to go stale. So the debt is computed on
// every read, from the rows themselves.
//
// WHO ELSE READS THIS. The Outstanding Answers surface (#313) lists these per
// Event; the Sales Export's blank cell (#314) is one; the Answer Reminder (#317)
// is a sweep over Ticket Sales that have any. Those are three surfaces asking
// the SAME question — does this Ticket owe this question an Answer — and three
// answers to it would be three different ideas of what "required" means, which
// is exactly the drift that would let a reminder chase somebody about a debt the
// screen says they do not have.

// OutstandingAnswerInputs are the four facts — and the only four — that decide
// whether one Ticket owes one Ticket Question an Answer.
//
// A struct rather than four positional arguments because three of the four are
// booleans, and a caller that transposed two of them would compile and be wrong
// in a way no test of THIS function could catch.
type OutstandingAnswerInputs struct {
	// Required is the Ticket Question's flag, whose ONLY effect anywhere is
	// producing this. An unanswered OPTIONAL question is not a debt: nobody
	// promised to answer it, so there is nothing to chase and listing it would
	// bury the questions that matter under the ones that do not.
	Required bool

	// QuestionRetired is whether the Ticket Question has been retired.
	//
	// A RETIRED QUESTION OWES NOTHING, and this is the ruling worth stating
	// outright. Retirement is the Organization saying "stop asking this", and
	// the write path already enforces it: service.AnswerTicketQuestion refuses a
	// retired question with ErrTicketQuestionRetired, on every route into an
	// Answer alike — Event Staff, checkout capture and the Answer Link. So a
	// retired required question with no Answer would be A DEBT NOBODY CAN
	// DISCHARGE: a row on the chase list with no working button behind it, and
	// an Answer Reminder mailing a buyer about a question that has left the form
	// they would be sent to.
	//
	// It is also what the PRD's own remedy requires. A question's kind is frozen
	// once anything has answered, and the prescribed way to change one is to
	// "retire the question and add another" — if retiring left its debt behind,
	// the sanctioned fix would mint permanent phantom debt on every Ticket sold
	// before it, and the list would only ever grow.
	//
	// THIS ERASES NO ANSWER. A retired question that WAS answered still reads on
	// the Ticket, still carries its Answer, and still gets its Sales Export
	// column — "retired, never deleted" is untouched. What ends is the debt, not
	// the record.
	QuestionRetired bool

	// SaleStatus is the Ticket Sale's status, which is where a Ticket's liveness
	// is read from: a Ticket carries no status of its own (ADR 0043) and a Sale
	// Reversal is always whole-Sale.
	//
	// A REVERSED SALE'S TICKETS OWE NOTHING. Its tickets have ceased to exist and
	// its money has gone back; nobody is coming, so there is no shirt to order in
	// their size and nobody left to chase. The Answers already given stay
	// readable — a Reversal voids a sale, it does not erase what its Tickets
	// answered — but the debt goes with the sale.
	SaleStatus string

	// Answered is whether an Answer row exists for this (Ticket, question).
	//
	// EXISTENCE AND NOT CONTENT. `false` on a checkbox is an Answer — somebody
	// read "I will attend the dinner" and said no — and a blank Answer is never
	// stored, so the row's presence is the whole test. The way back to "not
	// said" is removing the Answer, which restores the debt.
	Answered bool
}

// IsOutstandingAnswer reports whether one Ticket owes one Ticket Question an
// Answer.
//
// FOUR CLAUSES, ALL OF THEM CONJUNCTIVE, and the SQL in
// repository.outstandingAnswerWhere is the same four in the same order. That
// duplication is deliberate and load-bearing: a list over an Event's whole
// ticket roll cannot be computed by loading every Ticket into Go, so the
// database has to know the rule too. This function is the statement of it, the
// integration tests hold the two together, and neither may be changed alone.
//
// NOTHING ABOUT THE CLOCK. An Event that has already started still shows its
// Outstanding Answers, even though catalog.AnswerWindow has by then frozen every
// route into an Answer. That looks like the retired-question case and is not:
// retirement is the Organization un-asking the question, so the debt is void,
// whereas the doors opening un-asks nothing — the debt was real, it went unpaid,
// and "twelve people never told us their size" is exactly what somebody
// reviewing the event afterwards came to find out. A list that silently emptied
// at the moment the Event began would erase that. Surfaces that must go quiet
// after the start — the Answer Reminder (#317) is the one — impose their own
// silence on top of this, which is a rule about MAILING and not about the debt.
func IsOutstandingAnswer(in OutstandingAnswerInputs) bool {
	return in.Required &&
		!in.QuestionRetired &&
		in.SaleStatus == TicketSaleStatusActive &&
		!in.Answered
}
