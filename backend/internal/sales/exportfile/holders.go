package exportfile

import (
	"fmt"
	"io"
	"time"
)

// The HOLDER EXPORT (#529, parent #518, ADR 0065): an .xlsx of who is coming to
// an Event, one row per TICKET, reflecting exactly the filters and the sort the
// Holder List was showing when the download was pressed.
//
// IT IS A SECOND FILE AND NOT A SECOND IMPLEMENTATION, which is the distinction
// ADR 0065 draws and the one this package is arranged around. The Sales Export
// next door is left completely untouched — not extended, not retired, not one
// byte of its output changed — because the two artifacts COUNT DIFFERENT THINGS:
// one row per Ticket SALE carrying money that must stay summable, and one row per
// TICKET carrying no money at all. What is shared is the part that must never be
// decided twice: the Ticket Question and Option columns, which come from
// BuildQuestionColumns for this file exactly as they do for the Sales Export's
// per-Ticket sheet (#520). The first time somebody corrects `Mediun` to `Medium`
// the two files move a heading together or they do not move together at all.
//
// SINCE ADR 0075 THE TWO FILES NO LONGER SHARE A WRITER. This one streams
// through Stream, with no cap, so the server never holds the roster or the file;
// the Sales Export stays on excelize. What an Answer looks like in a cell is
// still decided once, by Answer.Cell, and only the bytes are written twice.
//
// IT LIVES IN THIS PACKAGE RATHER THAN IN A SECOND ONE beside the catalog
// service that calls it, and that is deliberate. This package is a LEAF — pure
// formatting, no database, no domain — and the shared column builder is already
// in it. A second workbook package importing this one for BuildQuestionColumns,
// Answer.Cell and the layout type would be a package that exists to hold one
// caller, and moving the builder out to a third package to serve both would put a
// change to the Sales Export's headings in the blast radius of a package the
// Sales Export does not own. Cross-domain imports already run both ways here
// (sales/service imports internal/catalog; catalog/service imports internal/sales),
// so the catalog service importing this leaf is the shape the codebase already
// has.
//
// THERE IS NO MONEY ON THIS SHEET AND THERE MUST NOT BE. No amount, no Net
// Proceeds, no currency, not even a Ticket Type's price. A roster with a price on
// it is a financial document by accident: one row per Ticket means a sale of four
// repeats its amount four times, so the first thing anybody does with a
// spreadsheet gives them four times the money — the same failure AnswersSheet was
// made a separate sheet to avoid. It also means the two files can never be
// mistaken for one another, or one forwarded to an accountant in place of the
// other. If a column of money is ever wanted here, the answer is the Sales
// Export.

// HolderSheet is the sheet the roster's rows live on.
//
// LIKE DataSheet AND AnswersSheet IT IS DELIBERATELY NOT "Sales", and for the
// third time in this package for exactly the same reason: the Sale Import parser
// selects its sheet by the name "Sales", so a workbook carrying one could be
// uploaded back as an import, inserting every sale a second time and re-emailing
// every buyer. Every sheet this package adds, now and later, must be named around
// that. Do not rename this to "Sales".
//
// The name says TICKET HOLDERS and not "Tickets", because what a reader came here
// for is the people: the Ticket is the row's identity and the Holder is its
// subject.
const HolderSheet = "Ticket Holders"

// The Holder Export's own columns. The rest of its fixed columns are the SAME
// KEYS AND HEADINGS the Sales Export uses — colConfirmationRef, colSoldAt,
// colChannel, colTicketTypeName, the customer_* trio and the four holder ones —
// reused rather than re-coined so that a reader holding both files reads one
// vocabulary, and so a VLOOKUP from this file's confirmation_ref onto the Sales
// Export's lands on the same spelling.
const (
	// colTicketOrdinal is which of its Ticket Sale Line's units this Ticket is,
	// 1..quantity.
	//
	// IT IS THE ONE DELIBERATE DIFFERENCE FROM THE SALES EXPORT'S PER-TICKET
	// SHEET, which pointedly omits it (see answersFixedColumns). That omission is
	// right there and wrong here, and the reason is what a row IS on each sheet.
	// There a row is an ANSWER SET: two identical rows are two Tickets that
	// answered alike, which is the truth of the matter, and an internal ordinal
	// would add a column that distinguishes them in nothing a reader can use.
	// Here a row is a TICKET — the roster, one line per person expected — so two
	// Tickets of one Ticket Sale Line are two rows that may differ in nothing
	// else, and without the ordinal an Organizer cannot tell them apart, cannot
	// say "the second of Ana's four", and cannot check that four rows is four
	// tickets rather than one duplicated. It is the same number the Holder List
	// puts on its rows, so the file and the screen name a Ticket the same way.
	//
	// It is NOT a seat number and never was; nothing is reserved by it.
	colTicketOrdinal = "ticket_ordinal"
	// colNeverAccepted marks a Ticket whose assignment the retention purge closed:
	// somebody was named, nobody ever accepted, and the address is gone by
	// definition (#334, migration 081).
	//
	// A COLUMN BESIDE THE STATE AND NOT A FOURTH VALUE IN IT. catalog.AssignmentState
	// knows three words and #331 refused to make it four; this is the
	// presentation the Holder List already derives at read time, carried into the
	// file so the morning-after roster can tell "nobody was named" from "named and
	// never claimed" — which after the Event has started are opposite facts and
	// otherwise both read `assigned`. It discloses nothing personal: the address
	// is gone.
	colNeverAccepted = "never_accepted"
)

// holderFixedColumns are the columns every Holder Export has, left to right,
// before the Holder block and the Ticket Questions. See holderLayoutFor, which
// is the only place this sheet's layout is decided.
//
// THE ORDER IS THE ROSTER'S STORY. The Sale Confirmation reference leads because
// it is the join: it is the sale's human-readable identity, the value somebody
// pastes back into the product, and the key onto the Sales Export's own first
// column when a reader wants this Ticket's money. Then WHEN the sale happened and
// HOW it was made, because a door sale and an import explain a row that owes
// everything and knows nobody — nobody ever put a question or an assignment form
// to those buyers. Then WHAT was bought: the Ticket Type and the ordinal that
// tells two Tickets of one line apart. Then WHO BOUGHT — the buyer, who stays on
// every row because they are the party of record for the Sale and the person to
// chase for any Ticket nobody has accepted. Then, spliced in by the layout, WHO
// IS COMING, and then what they said.
var holderFixedColumns = []string{
	colConfirmationRef,
	colSoldAt,
	colChannel,
	colTicketTypeName,
	colTicketOrdinal,
	colCustomerFirstName,
	colCustomerLastName,
	colCustomerEmail,
}

// holderStateColumns are the five the sheet gains once Ticket Assignment is
// open: where the Ticket stands, whether it was named and never claimed, and the
// accepted Holder.
//
// THE FLAG AND NOT THE DATA decides whether they exist, exactly as
// Answers.Assignment decides the Sales Export's four (ADR 0045). With assignment
// closed this file is the plain roster it would be on a build that never had the
// feature; with it open the columns stand on every Event whether or not anybody
// has been named, because an empty column is information and a missing one is a
// reader wondering what they filtered out.
//
// THE STATE LEADS because it is the one cell that is never blank, and it is what
// tells a reader how to read the blanks beside it. The never-accepted marker sits
// immediately after it, since it qualifies that word and nothing else.
var holderStateColumns = []string{
	colAssignmentState,
	colNeverAccepted,
	colHolderFirstName,
	colHolderLastName,
	colHolderEmail,
}

// HolderRow is one Ticket of the Event, and one row of the sheet.
//
// EVERY FIELD HERE IS ALREADY DISCLOSABLE. Nothing in this file decides what an
// Organization may see about a Holder: that rule is
// catalog/service.fillHolderListEntry's, it is the SAME function the Holder List
// screen is drawn from, and the caller routes its rows through it before filling
// this struct. See the note on HolderFirstName.
type HolderRow struct {
	// ConfirmationRef is the Sale Confirmation reference of the Ticket Sale this
	// Ticket was sold in — repeated across every Ticket of a multi-ticket sale,
	// which is what makes the join work in both directions.
	ConfirmationRef string
	// SoldAt is when the Ticket Sale was made, written as a REAL DATE CELL in the
	// Event's timezone — the same zone the sold-at filter was read in, so the file
	// can never contradict the date range that selected its rows.
	SoldAt time.Time
	// Channel is 'online', 'in_person' or 'import'. It EXPLAINS a row rather than
	// filtering it: a door sale and a Sale Import were never asked anything and
	// were never handed an assignment form, and a roster that could not say so
	// would read as lost data.
	Channel string
	// TicketTypeName is the Ticket Type's CURRENT name, joined live and never
	// snapshotted — like every other catalog word in this package, and for the
	// same reason: the file must not report a name that contradicts the screen it
	// was downloaded from.
	TicketTypeName string
	// Ordinal is which of its Ticket Sale Line's units this Ticket is. See
	// colTicketOrdinal for why this file carries it and the Sales Export's
	// per-Ticket sheet does not.
	Ordinal int
	// The BUYER: the party of record for the Sale. Kept on every row even where a
	// Holder is named, because a Holder is the person a Ticket was handed to and
	// never its owner, and the Sale stays whole with the buyer either way.
	//
	// The two name parts stay APART, as they are everywhere else on this platform
	// (ADR 0005): which part leads a person's name is the reader's question.
	CustomerFirstName string
	CustomerLastName  string
	CustomerEmail     string

	// THE HOLDER (ADR 0047). Written only while HolderColumns.Assignment is open.

	// AssignmentState is `unassigned`, `assigned` or `accepted`. It is the only
	// one of these that is written on every row, and it is what makes the blanks
	// beside it readable — an empty name is otherwise ambiguous between a Ticket
	// nobody was named for and one whose Holder has not clicked yet, and those are
	// different things to do something about.
	AssignmentState string
	// NeverAccepted is the retention purge's marker (#334). See colNeverAccepted.
	NeverAccepted bool
	// HolderFirstName, HolderLastName and HolderEmail are the accepted Holder, and
	// they are EMPTY UNLESS THE TICKET IS `accepted`.
	//
	// THAT IS NOT THIS FILE'S RULE AND IT IS NOT RE-DECIDED HERE. An address a
	// buyer typed and its owner never accepted has no consent moment behind it —
	// the person may not know a ticket was bought for them — and ADR 0047 refuses
	// to disclose it. The rule is stated once, in catalog/service.fillHolderListEntry,
	// and the export builds its rows out of the very view that function fills, so
	// the file and the screen cannot disagree and a change to one is a change to
	// both. A test on the state HERE would be a second copy of the rule and a
	// second chance to disagree with it; what this package guarantees instead is
	// that it writes exactly what it was handed.
	HolderFirstName string
	HolderLastName  string
	HolderEmail     string

	// Answers is what this Ticket has said, keyed by Ticket Question id — never by
	// label, which is wording and not identity. A question ABSENT from the map is
	// an Outstanding Answer or one this Ticket was never asked, and either way its
	// cells are left blank rather than written as an empty string, a zero or a
	// FALSE. CellsFor decides that, for this file and the Sales Export's per-Ticket
	// sheet identically.
	Answers map[string]Answer
}

// HolderColumns is what decides the Holder Export's columns: the Event's Ticket
// Questions, and whether the Holder block exists.
type HolderColumns struct {
	// Questions are the Event's Ticket Questions - the Event's, and not the ones
	// the exported Tickets happen to have answered, which is what keeps the shape
	// of the sheet stable under the filters. Empty on a build with Ticket
	// Questions dark, and on an Event that asks nothing.
	Questions []QuestionColumn
	// Assignment is whether TICKET_ASSIGNMENT_ENABLED is open, and the whole of
	// the test for whether the sheet carries holderStateColumns.
	Assignment bool
}

// asked reports whether the Event has any Ticket Question, and so whether the
// sheet carries question columns at all.
func (c HolderColumns) asked() bool { return len(c.Questions) > 0 }

// HolderInfo is what the Holder Export's Info sheet says about the file: the
// facts a reader needs to know what they are holding, none of which can be read
// off the rows.
//
// THERE IS NO CURRENCY FIELD, unlike the Sales Export's Info. That absence is
// load-bearing rather than an oversight: this file states no money, so naming a
// currency on its cover would invite a reader to look for amounts that are
// deliberately not there.
type HolderInfo struct {
	// EventName is the Event the roster belongs to, as its organizer named it.
	EventName string
	// GeneratedAt is the moment the file was built, written in the Event's
	// timezone like every other date in it. It is not decoration: the Ticket Type
	// names and the question wordings are joined LIVE and never snapshotted, so two
	// exports of one roster can carry different headings over the same people, and
	// this stamp is what tells a reader which moment's catalog they hold.
	GeneratedAt time.Time
	// Filters is what narrowed the roster, ready to be rendered in words — and it
	// is the HONOURED set, never the request. See HolderFilters.
	Filters HolderFilters
}

// HolderFilters is the applied filter set as the Info sheet describes it: the
// values the roster was actually queried with, already resolved into what a
// reader would recognise rather than what the query string carried.
//
// IT MUST BE THE HONOURED SET AND NOT THE REQUEST, and that is the whole reason
// this type is separate from the caller's parameters (ADR 0065). The Holder List
// IGNORES a filter belonging to a dark feature rather than refusing it — a
// refusal would turn a stale bookmark into an error page and force the client to
// know a flag ADR 0045 exists to keep it from knowing — so a request may name
// `assignment_state=accepted` on a build where assignment is closed and be
// answered with the WHOLE ROSTER. An Info sheet built from the request would then
// print "Only the Tickets whose Holder has accepted" over a file containing
// everybody, and somebody would read a complete roster believing it was a
// filtered one and act on the difference. The service passes what it queried
// with; nothing here re-derives it, because nothing here can see a flag.
//
// Every field is optional: a zero value is that dimension unfiltered, and says
// nothing at all on the sheet. A blank "Ticket Type:" label would make a reader
// hunt for a value that was never set.
type HolderFilters struct {
	// OwingOnly is the Outstanding Answers filter — one narrowing of the roster
	// and never its definition.
	OwingOnly bool
	// QuestionLabel is the WORDING of the named question the roster was narrowed
	// to, resolved by the caller against the Event's questions. A label and never
	// an id: an id is not something the reader ever saw, and this sheet is written
	// for somebody who never saw the screen either.
	QuestionLabel string
	// QuestionID is the same filter's raw id, set BY THE CALLER WHENEVER THE
	// FILTER WAS HONOURED — whether or not the label beside it could be resolved.
	//
	// IT EXISTS SO THAT A HONOURED FILTER CAN NEVER GO UNDESCRIBED. The label is
	// resolved against a read of the Event's questions, and that read can fail, or
	// return nothing for an id that names no question of this Event. Deciding the
	// line off the LABEL alone meant a genuinely narrowed file whose cover sheet
	// said "No filters were applied: this is every Ticket… on the Event." — a
	// reader taking a filtered roster for a whole one, which is the exact failure
	// #529 exists to prevent, arrived at from the opposite direction.
	//
	// So the two fields divide the work: the ID says THAT the roster was narrowed
	// and the LABEL says by what. With both, the sheet names the question; with
	// the id alone it says the file is narrowed and prints the id, which is
	// uglier than a name and infinitely better than silence. A filter the service
	// DROPPED still sets neither, which is what keeps the honoured-filters
	// mechanism intact — see holderFilterLines.
	QuestionID string
	// AssignmentState is the state filter's stored value — `unassigned`,
	// `assigned`, `accepted` or `never_accepted`, the last being a value of the
	// filter and not a fourth state.
	AssignmentState string
	// TicketTypeName is the NAME of the filtered Ticket Type, resolved by the
	// caller against the Event's catalog, for QuestionLabel's reason.
	TicketTypeName string
	// TicketTypeID is the same filter's raw id, set whenever the filter was
	// honoured. It carries QuestionID's meaning and exists for QuestionID's
	// reason: the catalog read that resolves the name is allowed to fail — a
	// failed read is not a failed export — and a file narrowed to one Ticket Type
	// must say so even on the day the name could not be fetched.
	TicketTypeID string
	// Channel is the Sales Channel filter.
	Channel string
	// SoldFrom and SoldTo are the sold-at range as calendar dates ("YYYY-MM-DD"),
	// read in the Event's timezone exactly as the filter was.
	SoldFrom string
	SoldTo   string
	// Sort and Dir are the order the roster was read in — one of the Holder List's
	// keys, and `asc` or `desc`. Blank is the list's default, oldest sale first,
	// and says nothing on the sheet.
	//
	// THE ORDER IS ON THE COVER because this file is a snapshot of a view and the
	// row order is part of what the reader chose. A reader who cannot see which
	// order was asked for cannot tell a deliberately ranked file from an
	// arbitrarily ordered one.
	Sort string
	Dir  string
	// Searched records only THAT a free-text search narrowed the roster, never
	// what was typed.
	//
	// A BOOLEAN ON PURPOSE, AND THE TYPE IS THE DECISION. The Holder List's search
	// matches a buyer's name and email address, the Sale Confirmation reference
	// and an accepted Holder's name and address, so the term is routinely a named
	// person's contact detail — and it is the SEARCHER's input, not a fact about
	// anybody in the file. Writing it onto the cover sheet would restate somebody's
	// address in the one part of the workbook that exists even when the search
	// matched nothing at all, and this file is forwarded. The audit line makes the
	// same call from the same value; there is no reason the file should be more
	// talkative than the log.
	//
	// The reader is still TOLD a search happened, because a file narrower than its
	// stated filters would otherwise be inexplicable.
	Searched bool
}

// holderLayoutFor splices the Holder block and the Event's Ticket Question
// columns onto the fixed ones.
//
// It is the only place THIS SHEET's layout is decided: the header row is written
// from it and every cell's position is derived from it, exactly as layoutFor and
// answersLayoutFor do for the Sales Export's two sheets. What the question
// columns themselves ARE is emphatically not decided here - BuildQuestionColumns
// decides that, once, for this file and the Sales Export's per-Ticket sheet both
// (#520, ADR 0065). All this function knows is where they are spliced in: last,
// after the person, in the order the builder hands them back.
func holderLayoutFor(columns HolderColumns) layout {
	questions := BuildQuestionColumns(columns.Questions)
	width := len(holderFixedColumns) + len(holderStateColumns) + questions.Len()
	out := layout{
		headers: make([]string, 0, width),
		index:   make(map[string]int, width),
	}
	add := func(key, header string) {
		out.headers = append(out.headers, header)
		out.index[key] = len(out.headers)
	}
	for _, col := range holderFixedColumns {
		add(col, col)
	}
	if columns.Assignment {
		for _, col := range holderStateColumns {
			add(col, col)
		}
	}
	headings := questions.Headings()
	for i, key := range questions.Keys() {
		add(key, headings[i])
	}
	return out
}

// HolderExport is a Holder Export being streamed (ADR 0075): an "Info" sheet
// that explains the file, then a "Ticket Holders" sheet holding a header row and
// one row per Ticket, with nothing above the header so select-all, autofilter
// and pivot source ranges all work without deleting a preamble.
//
// ROWS GO STRAIGHT THROUGH. Each Append is written, compressed and handed to the
// output before the next row exists, so the roster is never held here, and the
// Info sheet - written by Finish, opening first - states the rows actually
// written. A stream abandoned before Finish is not an openable file; see
// Stream.
//
// It writes the rows in the order it is given them and narrows nothing: the
// filtering and the sort happened in SQL, against the same query the screen ran.
type HolderExport struct {
	stream  *Stream
	columns HolderColumns
	cols    layout
	// questions is the builder the layout was made from, so the cells a row
	// writes and the columns they are written to cannot be worked out two
	// different ways.
	questions QuestionColumns
	loc       *time.Location
	// row is reused for every Ticket: one row's worth of cells is the most this
	// type ever holds.
	row []Cell
}

// BeginHolderExport writes the workbook's fixed parts and the data sheet's
// header onto w.
//
// loc is the EVENT's timezone, which every date is drawn in and which the Info
// sheet names outright, since an Excel date cell carries no timezone of its own.
// generatedAt is the moment the export was taken, which every part of the zip
// is stamped with, on the Event's clock like every other time in the file.
func BeginHolderExport(w io.Writer, columns HolderColumns, loc *time.Location, generatedAt time.Time) (*HolderExport, error) {
	cols := holderLayoutFor(columns)
	stream, err := BeginStream(w, SheetLayout{
		Name:     HolderSheet,
		Headings: cols.headers,
		// Wide enough that an email address - the longest thing on the sheet -
		// is readable without the recipient widening every column first. The
		// same 22 both of the Sales Export's sheets use.
		Width:    22,
		Modified: generatedAt.In(loc),
	})
	if err != nil {
		return nil, err
	}
	return &HolderExport{
		stream:    stream,
		columns:   columns,
		cols:      cols,
		questions: BuildQuestionColumns(columns.Questions),
		loc:       loc,
		row:       make([]Cell, len(cols.headers)),
	}, nil
}

// Append writes one Ticket's row.
func (h *HolderExport) Append(ticket HolderRow) error {
	clear(h.row) // every cell back to CellBlank

	for _, col := range []struct {
		key   string
		value string
	}{
		{colConfirmationRef, ticket.ConfirmationRef},
		{colChannel, ticket.Channel},
		{colTicketTypeName, ticket.TicketTypeName},
		{colCustomerFirstName, ticket.CustomerFirstName},
		{colCustomerLastName, ticket.CustomerLastName},
		{colCustomerEmail, ticket.CustomerEmail},
	} {
		h.text(col.key, col.value)
	}

	// A real date cell, drawn in the Event's timezone: the writer reads the
	// wall clock off the time itself, so converting first is what puts the
	// Event's clock in the cell. The same format the Sales Export stamps sold_at
	// with, because it is the same fact about the same sale.
	h.set(colSoldAt, Cell{Kind: CellMoment, Date: ticket.SoldAt.In(h.loc)})

	// A REAL NUMBER and not text, so "10" sorts after "9" rather than before
	// it. It is the only number on the sheet and it is a counter, not money.
	h.set(colTicketOrdinal, Cell{Kind: CellNumber, Number: float64(ticket.Ordinal)})

	if h.columns.Assignment {
		h.holderState(ticket)
	}

	// What this Ticket said, in cells. Which cells those are - including whether
	// an unanswered question leaves its whole block blank and how a
	// multiple-choice Answer fans out across its Options - is CellsFor's
	// decision, and what each one holds is Answer.Cell's; the Sales Export's
	// per-Ticket sheet makes both identically.
	for _, cell := range h.questions.CellsFor(ticket.Answers) {
		h.set(cell.Key, cell.Value.Cell())
	}

	return h.stream.Append(h.row)
}

// holderState fills the five Holder columns for one Ticket.
//
// EVERY BLANK HERE IS DELIBERATE AND NONE OF THEM IS A ZERO. An empty value is
// left unwritten rather than written as "", so the cell is genuinely blank and a
// reader filtering on "is blank" gets the rows nobody has accepted - which is the
// question they are asking. NOTHING HERE TESTS THE STATE to decide that: the
// three person values are already empty unless the Ticket is `accepted`, because
// the caller filled them from the view fillHolderListEntry wrote. A test on the
// word would be a second copy of ADR 0047's rule.
//
// The never-accepted marker is the exception and is written on EVERY row, as a
// real boolean. It is a derived fact that is total - every Ticket either was
// named and never claimed or was not - so a blank would be an absence with no
// meaning, and a column of blanks and TRUEs cannot be counted: "how many did I
// name who never claimed" is a COUNTIF, and a pivot over a column that is half
// empty counts neither half.
func (h *HolderExport) holderState(ticket HolderRow) {
	h.text(colAssignmentState, ticket.AssignmentState)
	h.set(colNeverAccepted, Cell{Kind: CellBool, Bool: ticket.NeverAccepted})
	h.text(colHolderFirstName, ticket.HolderFirstName)
	h.text(colHolderLastName, ticket.HolderLastName)
	h.text(colHolderEmail, ticket.HolderEmail)
}

// text sets a text cell, leaving an empty value genuinely blank.
func (h *HolderExport) text(key, value string) {
	if value != "" {
		h.set(key, Cell{Kind: CellText, Text: value})
	}
}

// set places a cell by column key. Every key comes from the slices the layout
// was built from, so a miss is unreachable.
func (h *HolderExport) set(key string, c Cell) {
	if column, ok := h.cols.index[key]; ok {
		h.row[column-1] = c
	}
}

// Rows is how many Tickets have been written.
func (h *HolderExport) Rows() int { return h.stream.Rows() }

// Finish writes the Info sheet - last, so the row count it states is the number
// of rows that were written - and completes the file. Until it returns nil the
// output cannot be opened.
func (h *HolderExport) Finish(info HolderInfo) error {
	return h.stream.Finish(holderInfoLines(info, h.loc, h.stream.Rows(), h.columns))
}

// holderInfoLines is the Holder Export's Info sheet copy: one line per row, in
// the order a reader needs them — what this is, then what produced it.
//
// It is a SEPARATE SHEET and not a preamble above the header, which is the
// load-bearing half of the choice and the same one the Sales Export made: rows
// above a header break select-all, break autofilter, and hand a pivot table the
// wrong source range. The data sheet keeps row 1 as its header and nothing above
// it.
//
// rowCount is the number of rows the file actually carries, so a reader can check
// nothing was lost between the screen and the file. It is the rows WRITTEN and
// never a total from elsewhere, so the claim and the sheet cannot disagree.
func holderInfoLines(info HolderInfo, loc *time.Location, rowCount int, columns HolderColumns) []string {
	lines := []string{
		"Holder Export",
		"",
		"Event: " + info.EventName,
		"Generated: " + info.GeneratedAt.In(loc).Format(infoStampFormat),
		// Excel date cells carry no timezone of their own, so this line is the only
		// place the file can say which clock its dates were drawn on — and naming it
		// as the EVENT's is what ties it to the sold-at filter, which is read in the
		// same zone.
		"Times shown in " + loc.String() + " (the Event's timezone).",
		holderRowCountLine(rowCount),
		"",
		"This file lists ONE ROW PER TICKET — who is coming, not what was paid. It carries " +
			"no amounts, no net proceeds and no currency: a roster repeats a sale's " +
			"details once per ticket, so any money on it would be counted several times " +
			"over. For the money, download the Sales Export from the Sales tab.",
		"",
		"Filters applied",
	}

	applied := holderFilterLines(info.Filters)
	if len(applied) == 0 {
		applied = []string{"No filters were applied: this is every Ticket of every live Ticket Sale on the Event."}
	}
	lines = append(lines, applied...)

	// The order, stated separately from the narrowings because it is not one: a
	// sort hides nobody, and listing it among the filters would suggest rows are
	// missing when none are.
	lines = append(lines, "", holderSortLine(info.Filters.Sort, info.Filters.Dir))

	lines = append(lines,
		"",
		"This file reflects the filters and the order that were on screen when it was "+
			"downloaded, so two downloads of the same Event can differ. The Ticket Type "+
			"names and question wordings are the Event's as they stood at the moment above.",
	)

	// What the assignment columns mean, and — the load-bearing half — what is NOT
	// in them (ADR 0047). An Organizer who typed an address into their own Event
	// and cannot find it in the file would otherwise report the export as broken,
	// and the answer is that the address is not theirs to see until the person it
	// belongs to has said so.
	if columns.Assignment {
		lines = append(lines,
			"",
			"Each row's "+colAssignmentState+" says where the Ticket stands with its holder: "+
				"unassigned — nobody has been named; assigned — somebody was named and has not "+
				"accepted yet; accepted — they have. An address a buyer entered that its owner "+
				"has not accepted is NOT in this file: it is theirs to disclose, not ours, and "+
				"it is deleted when the event starts. The "+colHolderFirstName+", "+
				colHolderLastName+" and "+colHolderEmail+" columns are therefore filled only "+
				"on an accepted Ticket.",
			"",
			colNeverAccepted+" is TRUE where somebody was named, nobody ever accepted, and the "+
				"address has since been deleted — which after the event has started is a "+
				"different fact from nobody having been named. Such a Ticket still reads "+
				"assigned; there is no fourth state for it.",
		)
	}

	// The question columns, and what a blank one means — the one thing about them
	// that cannot be read off them.
	if columns.asked() {
		lines = append(lines,
			"",
			"The remaining columns are this Event's Ticket Questions — one column each, and "+
				"one TRUE/FALSE column per option where a question takes several. A blank cell "+
				"is a question that ticket has not answered, or was never asked because it "+
				"belongs to another Ticket Type.",
		)
	}

	return lines
}

// holderRowCountLine states how many Tickets the file carries, which is what a
// reader checks nothing was truncated against.
func holderRowCountLine(rowCount int) string {
	if rowCount == 1 {
		return "Rows: 1 Ticket"
	}
	return fmt.Sprintf("Rows: %d Tickets", rowCount)
}

// holderFilterLines renders the filters that were actually HONOURED, one
// sentence each, and nothing at all for the dimensions that were not.
//
// A filter the service dropped never reaches this function, because it was never
// put on HolderFilters — see that type. That is the whole mechanism, and it is
// why there is no flag, no "if enabled" and no second opinion here: this file
// cannot describe a filter that was not applied, because it is not told about
// one.
//
// AND THE CONVERSE HOLDS TOO, WHICH IS THE HALF THAT WAS MISSING. "Describes
// only the filters honoured" was being read as a licence to stay silent whenever
// a filter's LABEL could not be resolved — a Ticket Type read that errored, an
// id naming nothing on this Event — and silence here means the sheet says "No
// filters were applied: this is every Ticket… on the Event." over a genuinely
// narrowed file. That is the same lie as describing a dropped filter, told the
// other way round, and on this file it is the more dangerous direction: a reader
// takes a filtered roster for a whole one and concludes people are missing from
// the Event rather than from the file. So each id-bearing filter falls back to
// its id when its label is blank; a filter that is not there at all still says
// nothing, because it narrowed nothing.
func holderFilterLines(f HolderFilters) []string {
	var lines []string

	if line := soldRangeLine(f.SoldFrom, f.SoldTo); line != "" {
		lines = append(lines, line)
	}
	if f.TicketTypeName != "" {
		lines = append(lines, "Ticket Type: only Tickets of "+f.TicketTypeName+".")
	} else if f.TicketTypeID != "" {
		lines = append(lines, unresolvedFilterLine(
			"Ticket Type", "only Tickets of one Ticket Type", "its name", f.TicketTypeID))
	}
	if f.Channel != "" {
		lines = append(lines, "Sales Channel: "+phrase(channelPhrases, f.Channel)+".")
	}
	if f.AssignmentState != "" {
		lines = append(lines, "Holder: "+phrase(assignmentStatePhrases, f.AssignmentState)+".")
	}
	if f.OwingOnly {
		lines = append(lines, "Outstanding Answers only: every Ticket in this file still owes at "+
			"least one required Ticket Question an answer.")
	}
	if f.QuestionLabel != "" {
		lines = append(lines, "Owing one named question: only Tickets that have not answered "+
			"\""+f.QuestionLabel+"\".")
	} else if f.QuestionID != "" {
		lines = append(lines, unresolvedFilterLine(
			"Owing one named question", "only Tickets that have not answered one Ticket Question",
			"its wording", f.QuestionID))
	}
	if f.Searched {
		// Said, but never quoted. See HolderFilters.Searched.
		lines = append(lines, "A search was applied, so these rows are narrower than the filters "+
			"above alone would give. The term is not recorded here: the Holder List's search "+
			"matches a buyer's name and email address, the confirmation reference and an "+
			"accepted holder's name and address, and that is what was typed rather than a "+
			"fact about anybody in this file.")
	}
	return lines
}

// unresolvedFilterLine is what an id-bearing filter says when its label could
// not be resolved: that the file IS narrowed, by what kind of thing, and the id
// as the only handle anybody has on it.
//
// THE WORDING'S JOB IS TO STOP A READER TRUSTING THE FILE AS A WHOLE ROSTER. It
// leads with the narrowing and mentions the unresolved name second, because a
// reader who skims has to come away knowing rows are missing; the id is offered
// last, for the one reader who can look it up, and nobody else is expected to
// find it useful.
//
// IT IS ONE FUNCTION FOR BOTH CALLERS rather than two sentences, since the only
// thing that differs between the Ticket Type and the named question is which
// noun narrowed the roster and what could not be read about it.
func unresolvedFilterLine(heading, narrowing, label, id string) string {
	return heading + ": " + narrowing + ", but " + label +
		" could not be read when this file was made. The rows below are narrowed to it; " +
		"it is identified only by its id, " + id + "."
}

// assignmentStatePhrases says the stored state filter values the way a reader
// would say them. A value with no phrase of its own falls back to itself, so a
// state added to the Holder List later reads awkwardly rather than silently
// vanishing off the stamp.
var assignmentStatePhrases = map[string]string{
	"unassigned":     "only Tickets nobody has been named for",
	"assigned":       "only Tickets whose named holder has not accepted yet",
	"accepted":       "only Tickets whose holder has accepted",
	"never_accepted": "only Tickets that were named and never claimed, and whose address has since been deleted",
}

// holderSortLine states the order the rows are in, in words.
//
// THE DEFAULT GETS A SENTENCE TOO rather than silence, because the order of a
// roster is not self-evident from looking at it: a reader who downloaded nothing
// and was forwarded this file cannot tell "oldest sale first" from "whatever the
// database returned", and one of those can be trusted to page through.
func holderSortLine(sort, dir string) string {
	field, ok := holderSortPhrases[sort]
	if !ok {
		return "Sorted by when the sale was made, oldest first (the default order)."
	}
	direction := "ascending"
	if dir == "desc" {
		direction = "descending"
	}
	return "Sorted by " + field + ", " + direction + "."
}

// holderSortPhrases names the Holder List's sort keys the way a reader would.
// An unrecognised key — including the blank one — falls through to the default
// sentence, exactly as the repository answers an unrecognised sort with the
// default order, so the file cannot claim an order the query did not use.
var holderSortPhrases = map[string]string{
	"sold_at":     "when the sale was made",
	"buyer":       "the buyer's name",
	"holder":      "the holder's name (Tickets with no holder name last, in both directions)",
	"ticket_type": "Ticket Type, in the Event's own catalog order",
	"owes":        "how many required answers the Ticket still owes",
}
