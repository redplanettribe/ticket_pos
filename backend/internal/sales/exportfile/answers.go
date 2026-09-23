package exportfile

import (
	"time"

	"github.com/xuri/excelize/v2"
)

// AnswersSheet is the second sheet: one row per Ticket, its Answers spread
// across the Event's Ticket Questions (#314, ADR 0043).
//
// LIKE DataSheet IT IS DELIBERATELY NOT "Sales", and for exactly the same
// reason: the Sale Import parser selects its sheet by that name, so a workbook
// carrying one could be uploaded back as an import, inserting every sale a
// second time and re-emailing every buyer. Every sheet this package adds, now
// and later, must be named around that. Do not rename this to "Sales".
//
// It is a SECOND SHEET rather than more columns on the data sheet, and that is
// the load-bearing part of the choice. The data sheet is one row per Ticket
// SALE, so its amount can be summed; this is one row per TICKET, and a sale of
// four tickets is four rows here. Merged into one sheet, the sale's amount would
// repeat down four rows and the first thing anybody does with a spreadsheet
// would give them four times the money.
const AnswersSheet = "Ticket Answers"

// The per-Ticket sheet's fixed columns. colConfirmationRef is deliberately the
// SAME key and the same heading as the data sheet's, because it is the join: a
// reader who wants a Ticket's buyer, price or Sales Channel looks the reference
// up on the other sheet, and a VLOOKUP cannot span two spellings of one column.
const colTicketTypeName = "ticket_type"

// The Holder columns (#330, parent #322, ADR 0047): who the Ticket was handed
// to, beside what they answered.
//
// FOUR COLUMNS AND NOT TWO. The state is carried as well as the person because
// a blank name is ambiguous without it — an Organizer looking at an empty row
// cannot otherwise tell a Ticket nobody was named for from one whose Holder has
// not clicked yet, and those are different things to do something about.
//
// The names are spelled like the data sheet's `customer_*` trio and not like it:
// same shape, different word, because a Holder is emphatically not the buyer.
// Nothing joins the two sheets on them.
const (
	colAssignmentState = "assignment_state"
	colHolderFirstName = "holder_first_name"
	colHolderLastName  = "holder_last_name"
	colHolderEmail     = "holder_email"
)

// answersFixedColumns are the columns every per-Ticket sheet has, in order, left
// to right. The Event's Ticket Question columns follow them — see
// answersLayoutFor, which is the only place this sheet's layout is decided.
//
// The confirmation ref leads for the reason it leads on the data sheet: it is
// the sale's human-readable identity, and here it is also the key back to the
// row this Ticket came from. Then the Ticket Type, because which question was
// even ASKED of a Ticket depends on it — a Ticket Question belongs to one Ticket
// Type — so a reader scanning a blank cell needs the Ticket Type to know whether
// it means "not answered" or "never asked".
//
// There is deliberately no Ticket identifier and no ordinal. A Ticket's id is
// not something anybody has ever been shown, and its ordinal is an internal
// number that distinguishes two Tickets on one line in nothing else; two
// identical rows on this sheet are two tickets that answered alike, which is the
// truth of the matter.
var answersFixedColumns = []string{colConfirmationRef, colTicketTypeName}

// holderColumns are the four the sheet gains once Ticket Assignment is open,
// spliced in after the Ticket Type and BEFORE the Ticket Questions — see
// answersLayoutFor.
//
// BEFORE THE QUESTIONS ON PURPOSE, and this is the whole shape of the ticket:
// "who is coming and what size are they" is one sheet, read left to right, and
// the person comes before what the person said. Put after the questions they
// would sit off the right edge of a wide Event's sheet, which is the same as
// being in a second file.
//
// The state leads the three person columns because it is the one cell that is
// never blank, and it is what tells a reader how to read the blanks beside it.
var holderColumns = []string{
	colAssignmentState,
	colHolderFirstName,
	colHolderLastName,
	colHolderEmail,
}

// QuestionColumn is one Ticket Question of the Event, and one or more columns of
// the sheet.
//
// The set comes from the Event's questions rather than from the questions the
// exported Tickets happen to have answered, which is what keeps the shape of the
// sheet stable under the filters — the same rule TicketTypeColumn follows on the
// data sheet, and for the same reason: an empty column is information, a missing
// one makes a reader wonder what they filtered out.
type QuestionColumn struct {
	// ID is the Ticket Question's identity, and the key a Ticket's Answers are
	// addressed by. Nothing here resolves a question by its heading: an
	// Organization may word two questions on two Ticket Types identically, or
	// word one "ticket_type", and a collision must cost the reader a repeated
	// heading rather than cost the sheet a misplaced Answer.
	ID string
	// Label is the question's CURRENT wording, joined live and never snapshotted
	// — like the Ticket Type headings on the data sheet, and for the same reason:
	// the file must not report wording that contradicts the screen it was
	// downloaded from.
	Label string
	// Options fans this question out to ONE COLUMN PER OPTION, each holding TRUE
	// or FALSE. It is filled for `multi_choice` questions and left empty for
	// every other kind, including `single_choice` — which takes one Option and so
	// fits in one cell.
	//
	// ONE COLUMN PER OPTION IS THE WHOLE POINT OF THE SHEET. A `multi_choice`
	// Answer collapsed into a delimited cell — "M; L; S" — cannot be pivoted,
	// filtered or counted without somebody splitting the text back apart first,
	// and "how many larges do I order" is the question this file exists to
	// answer. RETIRED OPTIONS GET A COLUMN TOO: an Option is retired and never
	// deleted precisely so the Tickets that chose it keep reading, and a file
	// that dropped its column would be the thing that broke that promise.
	Options []OptionColumn
}

// OptionColumn is one Option of a multiple-choice Ticket Question, and one
// TRUE/FALSE column.
type OptionColumn struct {
	// ID is the Option's IDENTITY, which is emphatically not its label. It is
	// what the columns are grouped by, so correcting `Mediun` to `Medium` moves a
	// heading and forks nothing: one Option is one column however many times it
	// has been renamed, and the Tickets that chose it before the rename sit in it
	// beside the ones that chose it after.
	ID string
	// Label is the Option's CURRENT label, which is what the heading reads. Not
	// the snapshot each Answer keeps of the words its chooser actually read: that
	// is per Answer and this is per column, and a column headed with one Ticket's
	// snapshot would misname it for every other Ticket in it.
	Label string
}

// TicketRow is one Ticket, and one row of the sheet.
type TicketRow struct {
	// ConfirmationRef is the Sale Confirmation reference of the Ticket Sale this
	// Ticket was sold in — the value that joins this row back to its row on the
	// data sheet. Repeated across every Ticket of a multi-ticket sale, which is
	// what makes the join work in that direction too.
	ConfirmationRef string
	// TicketTypeName is the Ticket Type's CURRENT name, joined live exactly as
	// the data sheet's Ticket Type headings are.
	TicketTypeName string

	// THE HOLDER (#330, ADR 0047). Empty on every row while Ticket Assignment is
	// closed, and Answers.Assignment is what decides whether the columns exist
	// at all — never the presence of a value here, because a column that came
	// and went with the data would make an Organizer wonder what they filtered
	// out.

	// AssignmentState is `unassigned`, `assigned` or `accepted`, derived by
	// catalog.AssignmentState and never stored (migration 080). It is the only
	// one of these four that is written on every row, and it is what makes the
	// three blanks beside it readable.
	//
	// A PURGED TICKET READS `unassigned`, which is migration 081's decision kept
	// rather than re-litigated here: the address was taken at Event start, and
	// nobody holds the Ticket. There is no fourth word for it.
	AssignmentState string
	// HolderFirstName, HolderLastName and HolderEmail are the accepted Holder,
	// and they are EMPTY UNLESS THE TICKET IS `accepted`. That is not a display
	// convention — it is ADR 0047's line, and the one the whole design is drawn
	// around: an address a buyer typed and its owner never accepted has no
	// consent moment behind it, is not the Organization's to see, and never
	// reaches this file. A Ticket in `assigned` therefore exports the WORD
	// `assigned` and three blank cells.
	//
	// They are also structurally incapable of carrying an unaccepted address:
	// the repository reads all three off the joined CUSTOMER row, which exists
	// only where accepted_at does (migration 080's
	// tickets_holder_customer_requires_acceptance_ck). There is no query here
	// that could leak one by being edited carelessly.
	HolderFirstName string
	HolderLastName  string
	HolderEmail     string

	// Answers is what this Ticket has said, keyed by Ticket Question id — never
	// by label, which is wording and not identity.
	//
	// A question ABSENT from this map is an Outstanding Answer or a question this
	// Ticket was never asked, and either way its cells are left BLANK rather than
	// written as an empty string, a zero or a FALSE. That is the same
	// absent-versus-zero rule the money columns follow, and here it carries the
	// meaning CONTEXT.md gives it: a blank cell in a Sales Export is an Answer
	// that is owed.
	Answers map[string]Answer
}

// Answer is what one Ticket said in reply to one Ticket Question, in the shape
// that question takes. At most one field is set on any value, mirroring the
// four typed columns and the child table migration 073 splits an Answer across.
//
// The fields are POINTERS for the reason the Sale's are: a blank cell and a zero
// say different things in a spreadsheet, and the difference becomes a SUM or a
// COUNTIF. FALSE is an Answer — somebody read "I will attend the dinner" and
// left it unticked — and the absence of one is a blank cell.
type Answer struct {
	// Text is a `short_text` or `long_text` Answer, and also a `single_choice`
	// one: the chosen Option's CURRENT label, resolved by the caller, since one
	// choice fits in one cell and needs no fan-out.
	//
	// It is written as a text cell even when it looks like a number, because an
	// organizer who asked for a flight number or a shirt size wants back what
	// somebody typed rather than what a spreadsheet made of it.
	Text *string
	// Number is a `number` Answer, written as a REAL NUMBER. The schema stores it
	// as NUMERIC for exactly this reason: a column of strings that look like
	// numbers sorts lexically, so 10 comes before 9, and sums to nothing at all.
	Number *float64
	// Date is a `date` Answer, written as a REAL DATE cell. It is a CALENDAR DATE
	// and carries no time and no zone — see writeAnswer for why the Event's
	// timezone is deliberately not applied to it.
	Date *time.Time
	// Checked is a `checkbox` Answer, written as a real boolean so it reads TRUE
	// or FALSE and can be counted.
	Checked *bool
	// Chosen is the set of Option ids a `multi_choice` Answer picked, by
	// identity. Every one of the question's Option columns is then written —
	// TRUE for the ones named here, FALSE for the rest — because this Ticket was
	// asked and answered, and a FALSE among them is a fact rather than a silence.
	Chosen []string
}

// Answers is the whole of the per-Ticket sheet's input: the Event's Ticket
// Questions as columns, and the Tickets of the exported sales as rows.
//
// AN EVENT THAT ASKS NOTHING GETS NO SHEET. A zero value is that state, and it
// is the state every Event is in today, so the workbook a reader has always
// received is exactly the workbook they keep receiving.
type Answers struct {
	Questions []QuestionColumn
	// Assignment is whether TICKET_ASSIGNMENT_ENABLED is open, and the whole of
	// the test for whether this sheet carries the four Holder columns (#330).
	//
	// THE FLAG AND NOT THE DATA. With assignment closed the workbook is exactly
	// the one it was before this ticket — same columns, same letters — which is
	// what makes the flag a real off switch rather than a hidden surface, the
	// same property WithTicketQuestions buys for the sheet as a whole (ADR 0045).
	// With it open the columns are there on every Event, whether or not anybody
	// has been named: an empty column is information and a missing one is a
	// reader wondering what they filtered out.
	Assignment bool
	// Tickets are the Tickets of the Ticket Sales the file carries, and only
	// those: the sheet respects whatever filters the Sales list was showing,
	// like the rest of the file. It is built from the very rows the data sheet
	// was built from, so the two sheets cannot disagree about which sales the
	// file is about.
	Tickets []TicketRow
}

// asked reports whether the Event has any Ticket Question — the test for
// whether the sheet carries question columns.
func (a Answers) asked() bool { return len(a.Questions) > 0 }

// present reports whether the sheet exists at all: something is asked, or
// Ticket Assignment is open (#333). An Event that asks nothing still gets the
// sheet once assignment is on, because its Holder columns are the file's answer
// to "who is coming" and that answer must not depend on anything having been
// asked — the Holder columns appear whenever assignment is on, regardless of
// questions, exactly as they do on the Holder List.
func (a Answers) present() bool { return a.asked() || a.Assignment }

// answerDateFormat is how a date Answer renders: a calendar date, with no time
// beside it. Deliberately narrower than the data sheet's dateFormat, which
// stamps a moment — a birthday has no 00:00 to show.
const answerDateFormat = "yyyy-mm-dd"

// answersLayoutFor splices the Event's Ticket Question columns onto the fixed
// ones, in the order the Organization arranged its catalog and its questions.
//
// It is the only place THIS SHEET's layout is decided: the header row is written
// from it and every column letter is derived from it, exactly as layoutFor does
// for the data sheet. What the question columns themselves are is emphatically
// NOT decided here — BuildQuestionColumns decides that, once, for this sheet and
// for the Holder Export both (#520, ADR 0065). All this function knows is where
// they are spliced in: after the fixed columns and the Holder, in the order the
// builder hands them back.
func answersLayoutFor(answers Answers) layout {
	questions := BuildQuestionColumns(answers.Questions)
	width := len(answersFixedColumns) + len(holderColumns) + questions.Len()
	out := layout{
		headers: make([]string, 0, width),
		index:   make(map[string]int, width),
	}
	add := func(key, header string) {
		out.headers = append(out.headers, header)
		out.index[key] = len(out.headers)
	}
	for _, col := range answersFixedColumns {
		add(col, col)
	}
	// The Holder, between the Ticket Type and the questions: the person, then
	// what the person said.
	if answers.Assignment {
		for _, col := range holderColumns {
			add(col, col)
		}
	}
	headings := questions.Headings()
	for i, key := range questions.Keys() {
		add(key, headings[i])
	}
	return out
}

// addAnswersSheet writes the per-Ticket sheet and places it after the data
// sheet, so the file reads Info, then the sales, then what their Tickets said.
func addAnswersSheet(f *excelize.File, answers Answers) error {
	if _, err := f.NewSheet(AnswersSheet); err != nil {
		return err
	}

	cols := answersLayoutFor(answers)
	// The same builder the layout was made from, so the cells a row writes and
	// the columns they are written to cannot be worked out two different ways.
	questions := BuildQuestionColumns(answers.Questions)
	for i, h := range cols.headers {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return err
		}
		if err := f.SetCellStr(AnswersSheet, cell, h); err != nil {
			return err
		}
	}

	dateStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: ptr(answerDateFormat)})
	if err != nil {
		return err
	}

	for i, ticket := range answers.Tickets {
		row := i + 2 // the header is row 1

		if err := setStr(f, AnswersSheet, cols, colConfirmationRef, row, ticket.ConfirmationRef); err != nil {
			return err
		}
		if err := setStr(f, AnswersSheet, cols, colTicketTypeName, row, ticket.TicketTypeName); err != nil {
			return err
		}
		if answers.Assignment {
			if err := writeHolder(f, cols, ticket, row); err != nil {
				return err
			}
		}

		// What this Ticket said, in cells. Which cells those are — including
		// whether an unanswered question leaves its whole block blank and how a
		// multiple-choice Answer fans out across its Options — is CellsFor's
		// decision and not this sheet's, because the Holder Export must make it
		// identically.
		for _, cell := range questions.CellsFor(ticket.Answers) {
			if err := writeAnswer(f, AnswersSheet, cols, cell, row, dateStyle); err != nil {
				return err
			}
		}
	}

	// Wide enough that a question's wording — and a Holder's email address, which
	// is the longest thing on the sheet — is readable without the recipient
	// widening every column first. The same 22 the data sheet gives
	// customer_email, and it spans every column the layout produced, so the
	// Holder columns are covered by arriving in it rather than by a rule of
	// their own.
	first, err := excelize.ColumnNumberToName(1)
	if err != nil {
		return err
	}
	last, err := excelize.ColumnNumberToName(len(cols.headers))
	if err != nil {
		return err
	}
	return f.SetColWidth(AnswersSheet, first, last, 22)
}

// writeHolder writes the four Holder columns for one Ticket (#330, ADR 0047).
//
// EVERY BLANK HERE IS DELIBERATE AND NONE OF THEM IS A ZERO. An empty value is
// left unwritten rather than written as "", so the cell is genuinely blank — the
// same absent-versus-empty rule the Answers beside it follow, and the same one
// the data sheet's money columns follow. A reader filtering on "is blank" gets
// the rows nobody has accepted, which is the question they are asking.
//
// The state is written on every row and the three person columns only where
// there is a person. NOTHING HERE TESTS THE STATE TO DECIDE THAT: the values are
// already empty unless the Ticket is `accepted`, because the repository read
// them off a Customer row that only an acceptance can produce. A test on the
// word would be a second copy of ADR 0047's rule, and a second copy is a chance
// to disagree with the first.
func writeHolder(f *excelize.File, cols layout, ticket TicketRow, row int) error {
	for _, col := range []struct {
		key   string
		value string
	}{
		{colAssignmentState, ticket.AssignmentState},
		{colHolderFirstName, ticket.HolderFirstName},
		{colHolderLastName, ticket.HolderLastName},
		{colHolderEmail, ticket.HolderEmail},
	} {
		// Through the cell rule, which leaves an empty value genuinely blank,
		// exactly as the Holder Export writes the same four columns.
		if err := writeCell(f, AnswersSheet, cols, col.key, row, TextCell(col.value), 0); err != nil {
			return err
		}
	}
	return nil
}

// writeAnswer writes one AnswerCell to a sheet, through the answer rule.
//
// IT DECIDES NO VALUE. Which cells exist is CellsFor's decision and what each
// one holds is Answer.Cell's, both shared with the Holder Export (ADR 0075).
//
// IT DOES DECIDE THE ENCODING, and keeps this file's exactly as it was before
// the shared rule (#655 story 34: the Sales Export is left exactly as it is).
// Two of the rule's answers are encoded here as this sheet always encoded them,
// not as the Holder Export encodes them:
//
//   - Empty text is an empty-string cell, not an absent one. The rule's blank
//     is the Holder Export's choice; this file wrote SetCellStr("") before it.
//   - A date before 1900, which Excel has no serial for, is the rule's text in
//     an inline string that keeps the date style. That is what excelize's
//     SetCellValue wrote for such a time, and SetCellDefault writes the same
//     inline string from the rule's text.
//
// TestSalesExportIsCellIdenticalToTheSalesExportBefore655 pins both, cell by
// cell, with every other cell of the workbook.
//
// THE SHEET IS A PARAMETER because it writes onto whichever sheet the caller is
// building; nothing else about the function differs between callers.
func writeAnswer(f *excelize.File, sheet string, cols layout, ac AnswerCell, row int, dateStyle int) error {
	value := ac.Value.Cell()
	switch {
	case ac.Value.Text != nil && value.Kind == CellBlank:
		cell, err := cellRef(cols, ac.Key, row)
		if err != nil {
			return err
		}
		return f.SetCellStr(sheet, cell, "")
	case ac.Value.Date != nil && value.Kind == CellText:
		cell, err := cellRef(cols, ac.Key, row)
		if err != nil {
			return err
		}
		if err := f.SetCellDefault(sheet, cell, value.Text); err != nil {
			return err
		}
		return f.SetCellStyle(sheet, cell, cell, dateStyle)
	}
	return writeCell(f, sheet, cols, ac.Key, row, value, dateStyle)
}

// writeCell is the thin excelize serializer of a Cell: which call a text, a
// number, a date or a boolean takes, and the date style. A blank writes
// nothing, so the cell is genuinely empty. dateStyle is read only for a
// CellDate.
func writeCell(f *excelize.File, sheet string, cols layout, key string, row int, value Cell, dateStyle int) error {
	if value.Kind == CellBlank {
		return nil
	}
	cell, err := cellRef(cols, key, row)
	if err != nil {
		return err
	}
	switch value.Kind {
	case CellText:
		return f.SetCellStr(sheet, cell, value.Text)
	case CellNumber:
		// Not SetCellFloat with a fixed scale: the rule hands over the precision
		// it was given, and this writes it as given.
		return f.SetCellValue(sheet, cell, value.Number)
	case CellDate:
		if err := f.SetCellValue(sheet, cell, value.Date); err != nil {
			return err
		}
		// Set after the value: excelize stamps a default date-and-time style of
		// its own when writing a time, and this replaces it with the date alone.
		return f.SetCellStyle(sheet, cell, cell, dateStyle)
	case CellBool:
		return f.SetCellBool(sheet, cell, value.Bool)
	}
	return nil
}
