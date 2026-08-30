package exportfile

// The Ticket Question and Option columns of an export sheet, and the ONE place
// they are worked out (#520, parent #518, ADR 0065).
//
// IT EXISTS BECAUSE TWO FILES MAY OVERLAP BUT TWO IMPLEMENTATIONS MAY NOT. ADR
// 0065 accepts that the Holder Export and the Sales Export's per-Ticket sheet
// both carry what Tickets answered; what it refuses is two pieces of code
// deciding what an Answer is. The first time somebody corrects `Mediun` to
// `Medium` the two would disagree — one moving a heading, the other forking a
// column — and an Organization would have two files in front of it that count
// the same Option differently. The second caller is the Holder Export (#529),
// and it is why this was extracted BEFORE that file was written: retro-fitting
// the sharing afterwards is how the second implementation becomes permanent.
//
// NOTHING HERE KNOWS A SHEET. Not the fixed columns either sheet leads with, not
// the Holder columns, not AnswersSheet's name, not which column letter anything
// lands in. The two callers have different surrounding columns in different
// orders, and the moment this file learned one of them it would stop being
// usable by the other — which is the whole failure it was extracted to prevent.
// It is about QUESTIONS AND OPTIONS: which columns they are, in what order, what
// their headings read, and where one Ticket's Answers go among them. A caller
// splices Keys and Headings into whatever layout it has, and writes the cells
// CellsFor hands back.

// QuestionColumns is the built column set for a sheet's Ticket Questions: the
// ordered column keys, the headings above them, and the addressing that turns
// one Ticket's Answers into cells.
//
// It is BUILT ONCE PER SHEET and read per row. The order it fixes is the order
// every row is written in, so the keys, the headings and the cells cannot drift
// apart — which is the property that makes a rename move a heading instead of
// forking a column, and the property a caller would lose if it walked the
// questions itself.
type QuestionColumns struct {
	// questions is the input, kept because CellsFor walks it: a question's
	// Options are what decide whether an Answer is one cell or a row of
	// TRUE/FALSEs, and that decision must be the same one the keys were built
	// from.
	questions []QuestionColumn
	// keys and headings are PARALLEL and the same length, one entry per column,
	// left to right. Two slices rather than a map because order is the point and
	// a Go map has none; two slices rather than a slice of pairs because every
	// caller splices them into a layout that already speaks in key-and-heading.
	//
	// A key is an IDENTITY — a Ticket Question's id, or an Option's — and never a
	// heading. An Organization may word two questions on two Ticket Types
	// identically, or word one "ticket_type"; a collision must cost a reader a
	// repeated heading rather than cost the sheet a misplaced Answer.
	keys     []string
	headings []string
}

// AnswerCell is one Ticket's value in one question column: the column's key, and
// the Answer to write there.
//
// The VALUE IS AN Answer even for a multiple-choice Option column, where it is
// an Answer with only Checked set. That is deliberate and it is what keeps the
// callers thin: a caller writes cells, all of one kind, and never asks whether a
// column came from a question or from an Option. The fan-out — which Option
// columns exist, which of them this Answer chose — is decided here, once, so the
// second caller cannot decide it differently.
type AnswerCell struct {
	// Key is the column key: the Ticket Question's id, or the Option's id for a
	// column of a multiple-choice question. The caller resolves it against its
	// own layout, which is the only thing it knows that this file does not.
	Key string
	// Value is what to write. Exactly one of its fields is set, so a caller may
	// switch on it without a case for the fan-out.
	Value Answer
}

// BuildQuestionColumns works out the columns a set of Ticket Questions takes, in
// the order the Organization arranged them.
//
// A question keyed by its own id gets ONE COLUMN headed with its current
// wording. A multiple-choice question contributes NO COLUMN OF ITS OWN and one
// per Option instead, each keyed by the OPTION's id and headed with the Option's
// CURRENT label — which is what makes a correction move a heading rather than
// fork a column, and what keeps the Tickets that chose an Option before a rename
// in the same column as the ones that chose it after.
//
// RETIRED OPTIONS ARE IN HERE TOO, because the caller passes them: an Option is
// retired and never deleted precisely so the Tickets that chose it keep reading,
// and a builder that dropped its column would be the thing that broke that
// promise. Nothing in this file filters the input — the set of questions and the
// set of Options are the caller's decision, and it is the Event's, not the
// exported Tickets'.
func BuildQuestionColumns(questions []QuestionColumn) QuestionColumns {
	out := QuestionColumns{
		questions: questions,
		keys:      make([]string, 0, len(questions)),
		headings:  make([]string, 0, len(questions)),
	}
	for _, q := range questions {
		if len(q.Options) == 0 {
			out.keys = append(out.keys, q.ID)
			out.headings = append(out.headings, q.Label)
			continue
		}
		for _, opt := range q.Options {
			out.keys = append(out.keys, opt.ID)
			out.headings = append(out.headings, opt.Label)
		}
	}
	return out
}

// Len is how many columns the questions take, which is not the number of
// questions: a multiple-choice one takes as many as it has Options.
func (c QuestionColumns) Len() int { return len(c.keys) }

// Keys are the column keys, left to right. The caller adds them to its layout in
// this order and nothing else decides it.
func (c QuestionColumns) Keys() []string { return c.keys }

// Headings are the headings above Keys, position for position — the current
// wording of each question and the current label of each Option, joined live and
// never snapshotted, like the Ticket Type headings on the data sheet and for the
// same reason: the file must not report wording that contradicts the screen it
// was downloaded from.
func (c QuestionColumns) Headings() []string { return c.headings }

// CellsFor turns one Ticket's Answers into the cells they occupy among these
// columns, in column order.
//
// A QUESTION ABSENT FROM THE MAP PRODUCES NO CELL AT ALL, and so leaves its
// whole block blank — including every Option column of a multiple-choice one.
// Writing FALSE across them would claim this Ticket read the question and
// declined every Option, which is a different fact from never having answered
// it, and the one CONTEXT.md calls an Outstanding Answer. It is the same
// absent-versus-zero rule the money columns follow: a blank cell in an export is
// an Answer that is owed.
//
// An ANSWERED multiple-choice question produces a cell for EVERY one of its
// Option columns — TRUE for the ones it chose, FALSE for the rest. The FALSEs
// are written BECAUSE the question was answered: they are what makes the column
// countable, and a pivot over an Option that reads half blanks and half FALSEs
// counts neither.
//
// An Answer with nothing set still produces its cell, and the caller writes
// nothing to it. The write path cannot produce one — migration 073's CHECK
// refuses a row claiming to be two kinds at once and the service refuses one
// claiming to be none — so swallowing it here would only hide the shape of a bug
// elsewhere behind a cell that is indistinguishable from an Outstanding Answer
// anyway.
func (c QuestionColumns) CellsFor(answers map[string]Answer) []AnswerCell {
	cells := make([]AnswerCell, 0, len(c.keys))
	for _, q := range c.questions {
		answer, answered := answers[q.ID]
		if !answered {
			continue
		}
		if len(q.Options) == 0 {
			cells = append(cells, AnswerCell{Key: q.ID, Value: answer})
			continue
		}
		chosen := make(map[string]bool, len(answer.Chosen))
		for _, id := range answer.Chosen {
			chosen[id] = true
		}
		for _, opt := range q.Options {
			// The Option's own cell, as a plain checkbox Answer: the reader of
			// this column is asking "how many larges do I order", and TRUE or
			// FALSE is the answer to it.
			cells = append(cells, AnswerCell{
				Key:   opt.ID,
				Value: Answer{Checked: ptr(chosen[opt.ID])},
			})
		}
	}
	return cells
}
