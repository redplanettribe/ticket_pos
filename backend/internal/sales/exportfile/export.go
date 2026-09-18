// Package exportfile builds the Sales Export: an .xlsx of an Event's Ticket
// Sales, one row per sale, reflecting exactly the filters the Sales list was
// showing when the download was pressed.
//
// It is the mirror image of the importfile package next door — that one hands an
// organizer a sheet to fill in, this one hands them the sales back — and it
// follows the same house style: the column order is decided once, in a slice,
// and every column letter is derived from it.
package exportfile

import (
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"
)

// DataSheet is the sheet the rows live on. It is deliberately NOT "Sales".
//
// This is a safety catch, not a naming preference: the Sale Import parser
// selects its sheet by the name "Sales", so a file named that way could be
// uploaded back as an import, inserting every sale a second time and re-emailing
// every buyer. Naming the sheet something else means an export can never be
// mistaken for an import. Do not rename this to "Sales".
const DataSheet = "Ticket Sales"

// InfoSheet is where the file explains itself: which Event, when it was taken,
// in whose timezone, how many rows, in what currency, and — in words — which
// filters produced it.
//
// It exists because the export mirrors whatever filters were on screen, so the
// file is not canonical: two Owners can produce different files both called
// "sales", and the person who reads one is usually not the person who downloaded
// it. This sheet is what makes that safe.
//
// It is a SEPARATE SHEET rather than a block of preamble above the header row,
// and that is the load-bearing part of the choice: rows above a header break
// select-all, break autofilter, and hand a pivot table the wrong source range.
// The data sheet keeps row 1 as its header and nothing above it.
//
// Like the Sale Import template's Instructions sheet, it sits first and is
// active on open — see addInfoSheet.
const InfoSheet = "Info"

// The export's columns. Named constants rather than bare strings so a rename is
// a compile error rather than a silently mismatched header.
const (
	colConfirmationRef   = "confirmation_ref"
	colSoldAt            = "sold_at"
	colCustomerFirstName = "customer_first_name"
	colCustomerLastName  = "customer_last_name"
	colCustomerEmail     = "customer_email"
	colTaxIDType         = "tax_id_type"
	colTaxIDNumber       = "tax_id_number"
	colTotalQuantity     = "total_quantity"
	colAmount            = "amount"
	colNetProceeds       = "net_proceeds"
	colCurrency          = "currency"
	colChannel           = "channel"
	colSource            = "source"
	colOrigin            = "origin"
	colPaymentMethod     = "payment_method"
	colStatus            = "status"
	colReversedAt        = "reversed_at"
	colReversedBy        = "reversed_by"
	colCorrectedBy       = "corrected_by"
	colCorrects          = "corrects"
)

// The whole value set of the reversed_by column: the ROUTE a Sale Reversal
// arrived by, and nothing else about it.
//
// These are the export's own spellings, deliberately not the stored ones. The
// database records the kind of ACTOR — `customer`, `staff`, `operator` — beside
// the acting operator's email and their free-text note, and the mapping to these
// three is where that record is narrowed to what the Organization may see. See
// ReversedBy on Sale for why the narrowing is the point.
const (
	// ReversedByCustomer: the buyer undid their own Online Sale from the
	// Storefront, inside the Reversal Window.
	ReversedByCustomer = "customer"
	// ReversedByPlatform: an Operator Reversal — the platform refunded the buyer
	// off-platform and recorded it. The word is "platform" and not "operator"
	// because the Organization is told an institution acted, never which person.
	ReversedByPlatform = "platform"
	// ReversedByImportUndo: staff undid the Sale Import batch this sale arrived
	// in. Named after the operation rather than after the actor ("staff"),
	// because the actor is the reader's own Organization and what they need to
	// know is which lever was pulled.
	ReversedByImportUndo = "import_undo"
	// ReversedByStaffReversal: staff reversed this one imported sale on its own,
	// from its row on the Sales list, with no replacement (#350). The same
	// stored actor as the batch undo; a different lever.
	ReversedByStaffReversal = "staff_reversal"
	// ReversedByCorrection: a Sale Correction (#351, ADR 0050) — staff reversed
	// this one imported sale and recorded a replacement in the same act. The
	// replacement's reference sits in corrected_by on the same row.
	ReversedByCorrection = "correction"
	// ReversedByUpgrade: an Upgrade (#650, ADR 0074) — the buyer surrendered
	// their free Online Sale for a paid one, inside the transaction that
	// committed it.
	//
	// THE SIXTH ROUTE AND THE SECOND ONE THE BUYER CAUSED, and it is stated
	// apart from `customer` for the same reason the two staff levers are stated
	// apart from each other: the column names the LEVER, and "the buyer undid
	// their own sale from the Storefront" is a different lever from "the buyer
	// traded it in for a better ticket" — one leaves them with nothing, the
	// other with a Ticket they paid for. It is also why it is not `correction`:
	// nothing was recorded wrongly and no member of staff acted (#651).
	//
	// The corrected_by/corrects pair is BLANK on an Upgrade's rows, because
	// those two columns are ADR 0050's and say a correction happened; the
	// screens state the counterpart Sale instead. See Sale.CorrectedByRef.
	ReversedByUpgrade = "upgrade"
)

// fixedColumns are the columns every export has, in order, left to right. The
// Event's Ticket Type columns are spliced in immediately before
// colTotalQuantity — see layoutFor, which is the only place the layout is
// decided; the header row is written from it and every column letter is derived
// from it.
//
// The order tells the sale's story in the order a reader needs it. The
// confirmation ref leads because it is the sale's human-readable identity and
// the value somebody pastes back into the product when a buyer emails them.
// Then when it happened, then who bought — including the Tax ID pair, which is
// why this feature exists at all: a Tax ID is mandatory to record a sale on the
// native Sales Channels expressly for the buyer's tax declarations, and until
// this file there was no way to read it back out. Then what was bought: the
// per-Ticket-Type quantities and their total. Then the transaction facts, with
// net_proceeds immediately after amount: what the buyer paid and what the sale
// left the Organization, side by side, which is where they get compared.
//
// The origin sits with those transaction facts, immediately after channel and
// source (#373, ADR 0052), because it is the thing neither of them can say: a
// Sale Import batch, a Manually Recorded Sale somebody typed and a Sale
// Correction's replacement all read `import` in the channel column and `direct`
// in the source column, so before this column the file could not tell an
// accountant how a row they did not recognise got here. It comes AFTER both
// rather than before, because the two stored facts are read first and the
// derived one summarises them.
//
// The reversal pair comes last, after the status it elaborates: on the vast
// majority of rows it says nothing at all, and a reader scanning left to right
// should reach the whole of the sale before reaching the two columns that only
// speak when it was undone.
//
// The Sale Correction linkage comes after even those: corrected_by names the
// replacement on a corrected row and corrects names the mistaken sale on its
// replacement, each by Sale Confirmation reference so a reader can follow the
// trail in either direction with a lookup on the file's own first column.
var fixedColumns = []string{
	colConfirmationRef,
	colSoldAt,
	colCustomerFirstName,
	colCustomerLastName,
	colCustomerEmail,
	colTaxIDType,
	colTaxIDNumber,
	colTotalQuantity,
	colAmount,
	colNetProceeds,
	colCurrency,
	colChannel,
	colSource,
	colOrigin,
	colPaymentMethod,
	colStatus,
	colReversedAt,
	colReversedBy,
	colCorrectedBy,
	colCorrects,
}

// TicketTypeColumn is one Ticket Type of the Event's catalog, and one column of
// the sheet.
//
// The set comes from the Event's LIVE catalog rather than from the Ticket Types
// the exported rows happen to mention, which is what keeps the shape of the
// sheet stable under the filters: an export narrowed to one Ticket Type still
// carries every column, and a Ticket Type nobody bought still gets one. An empty
// column is information; a missing one makes a reader wonder whether they
// filtered something out.
//
// Reading the catalog cannot orphan a sale into a column that does not exist:
// ticket_sale_lines references ticket_types ON DELETE RESTRICT, so a Ticket Type
// that has ever sold cannot be deleted and the catalog is always a superset of
// what the rows reference.
//
// Name is the Ticket Type's CURRENT name, joined live and never snapshotted onto
// a sale line — so renaming a Ticket Type changes the heading on the next
// export, including over sales recorded under the old name. That is deliberate:
// the file must not report a name that contradicts the screen it was downloaded
// from. The schema's split holds — the unit price is a fact of the sale and is
// frozen, the name is a fact of the catalog and is not.
type TicketTypeColumn struct {
	// ID is the Ticket Type's identity, and the key a Sale's quantities are
	// addressed by. Nothing in this package resolves a Ticket Type column by its
	// heading: an organizer may name two Ticket Types the same thing, or name one
	// "amount", and a collision must cost the reader a repeated heading rather
	// than cost the sheet a misplaced number.
	ID   string
	Name string
}

// layout is a built column layout: the columns in order, and the 1-based column
// number each key sits at. A fixed column's key is its header constant; a Ticket
// Type column's key is the Ticket Type's id, which no fixed key can collide with.
type layout struct {
	headers []string
	index   map[string]int
}

// layoutFor splices the Event's Ticket Type columns into the fixed layout, in
// catalog display order, immediately before total_quantity — so the sheet reads
// left to right as the types bought, their total, and then the money.
func layoutFor(types []TicketTypeColumn) layout {
	out := layout{
		headers: make([]string, 0, len(fixedColumns)+len(types)),
		index:   make(map[string]int, len(fixedColumns)+len(types)),
	}
	add := func(key, header string) {
		out.headers = append(out.headers, header)
		out.index[key] = len(out.headers)
	}
	for _, col := range fixedColumns {
		if col == colTotalQuantity {
			for _, tt := range types {
				add(tt.ID, tt.Name)
			}
		}
		add(col, col)
	}
	return out
}

// dateFormat is how a sold-at cell renders. Real Excel date cells carry no
// timezone of their own, so the moment is converted into the Event's zone before
// it is written — the same zone the sold-at filter is interpreted in, so the
// file can never contradict the date range that selected its rows.
const dateFormat = "yyyy-mm-dd hh:mm"

// moneyFormat renders an amount to the cent. The cell holds a number in major
// units; this only decides that 25 reads as "25.00".
const moneyFormat = "0.00"

// Sale is one Ticket Sale as the export writes it: the sale's identity, its
// buyer, and its transaction facts.
//
// The nullable fields are pointers because a blank cell and a zero say different
// things in a spreadsheet, and the difference becomes a SUM. A sale that
// collected no Tax ID leaves both halves blank rather than carrying a
// placeholder, so at a glance the organizer can see which sales they cannot
// invoice.
type Sale struct {
	ConfirmationRef   string
	SoldAt            time.Time
	CustomerFirstName string
	CustomerLastName  string
	CustomerEmail     string
	TaxIDType         *string
	TaxIDNumber       *string
	// Quantities is how many of each Ticket Type this sale was for, keyed by
	// Ticket Type id — never by name, which is a label and not an identity.
	//
	// A sale of several Ticket Types stays ONE Sale here, and so one row: its
	// quantities spread across its columns rather than its Ticket Sale Lines
	// becoming rows. Flattening to lines would repeat the sale's amount on each
	// one, and the first thing anybody does with a spreadsheet is sum a column.
	//
	// A Ticket Type absent from this map is absent from the sale, and its cell is
	// left blank rather than written as 0 — the same absent-versus-zero rule the
	// money columns follow.
	Quantities map[string]int
	// AmountCents is what the buyer paid, as the system stores it. The file
	// writes it in major units: a column that sums to a hundred times too much
	// is worse than no column.
	AmountCents int
	// NetProceedsCents is what this sale left the Organization once the Platform
	// Fee and its Fee IVA were withheld, read off the per-line snapshots the sale
	// froze. Like AmountCents it is written in major units.
	//
	// It is a POINTER, and nil is not zero. nil means the figure does not apply
	// to this sale at all — it was not an Online Sale, so the platform never held
	// the money and withheld nothing, or it was reversed and the money went back
	// — and the cell is left blank. A 0 would be an assertion, and a spreadsheet
	// would add that assertion into a SUM. See ADR 0032.
	NetProceedsCents *int
	Currency         string
	Channel          string
	Source           *string
	// Origin is how this Ticket Sale reached the platform (#373, ADR 0052), and
	// it is one of the four values sales.DeriveSaleOrigin answers with:
	// `sale_import`, `manually_recorded`, `correction_replacement` or
	// `channel_sale`. The caller derives it; nothing here restates the
	// predicate, because a Manually Recorded Sale is recognised by a three-way
	// negative and a second copy of that is the copy nobody updates. It is the
	// same value the Sales list states on its rows (#370), so the file and the
	// screen can never disagree about where a sale came from.
	//
	// It is a plain string and NOT a pointer, unlike ReversedBy beside it,
	// because it is never absent: every Ticket Sale reached the platform
	// somehow, and the derivation is total. That is deliberate and it is the
	// distinction the blank-not-zero rule actually draws — a blank cell means
	// the figure or the event does not apply to this row (no Net Proceeds, no
	// Sale Reversal, no correction), and there is no sale to which "how did
	// this get here" does not apply. A sale that came in on a Sales Channel of
	// its own says `channel_sale` rather than leaving a hole an accountant would
	// have to account for, which is also what makes grouping the file by origin
	// add up to every row in it. The Info sheet names all four values, since
	// they are the platform's vocabulary and not the reader's.
	//
	// The column deliberately does NOT name WHICH Sale Import batch an imported
	// sale arrived in. A batch has no name a reader could use: sale_import_batches
	// carries an id and the uploader's idempotency key, and neither is something
	// the person holding this file ever saw. Printing a UUID per row would add a
	// column of noise to the file the Organization forwards, and the Import
	// history is the surface that lists batches. What the accountant needs from
	// this file is which ROUTE a sale took, and that is what the column states.
	Origin        string
	PaymentMethod *string
	Status        string
	// ReversedAt is when the Sale Reversal happened, written as a real date cell
	// in the Event's timezone exactly as SoldAt is — so a reader can sort by it
	// and subtract it from the sale it undid.
	//
	// nil on every active sale, and also on a sale reversed before the platform
	// recorded any provenance: those rows are shown as they are rather than
	// backfilled with a fabricated moment.
	ReversedAt *time.Time
	// ReversedBy is the ROUTE the Sale Reversal arrived by, and one of the three
	// constants above. It is never the person: not the Platform Operator who
	// recorded an off-platform refund, not their note, not the Member who undid a
	// Sale Import batch.
	//
	// This is a boundary, not a formatting choice. ADR-0019 holds that an
	// Operator Reversal is invisible to the Organization beyond the sale showing
	// as reversed by the platform, and the operator's email and free-text memo
	// sit on the same database row as the value this column is derived from — so
	// this is the field where that boundary is breached by an accidental
	// pass-through. Nothing that is not one of the three routes reaches here;
	// see the mapping in the service, which drops anything it cannot name rather
	// than emitting it.
	//
	// nil leaves the cell blank, for the same rows ReversedAt does.
	ReversedBy *string
	// CorrectedByRef is, on a sale a Sale Correction reversed, the Confirmation
	// reference of the replacement that stands in for it; CorrectsRef is, on
	// that replacement, the reference of the sale it corrects (ADR 0050). Each
	// is nil — a blank cell — on every other row.
	//
	// AN UPGRADE'S TWO SALES ARE "EVERY OTHER ROW" AND LEAVE BOTH BLANK (#651,
	// ADR 0074). They are linked through the same database columns, but these
	// two columns are headed `corrected_by` and `corrects` in the file, and
	// filling them for an Upgrade would tell an accountant, in a header they
	// cannot argue with, that somebody at the Organization recorded a sale
	// wrongly — on a Sale no human there ever touched. reversed_by says
	// `upgrade` on such a row instead, and the Sales list and the Customer
	// Dossier are where the counterpart Sale is named.
	CorrectedByRef *string
	CorrectsRef    *string
}

// Info is what the Info sheet says about the file: the facts a reader needs to
// know what they are holding, when none of them can be read off the rows.
type Info struct {
	// EventName is the Event the sales belong to, as its organizer named it.
	EventName string
	// GeneratedAt is the moment the file was built, written in the Event's
	// timezone like every other date in it.
	//
	// It is not decoration. The Ticket Type headings are the catalog's CURRENT
	// names, joined live and never snapshotted, so two exports of the same period
	// can carry different headings over identical numbers; this stamp is what
	// lets a reader tell which moment's catalog they are looking at.
	GeneratedAt time.Time
	// Currency the amounts are denominated in — the Event's, stated once here as
	// well as per row, so the reader knows before they reach the data.
	Currency string
	// Filters is what narrowed the rows, ready to be rendered in words.
	Filters Filters
}

// Filters is the applied filter set as the Info sheet describes it: the values
// the Sales list was filtered by, already resolved into what a reader would
// recognise rather than what the query string carried.
//
// Every field is optional except Status, which always has a value because the
// Sales list always filters by one.
type Filters struct {
	// Status is the resolved status filter — "active" or "reversed", never blank,
	// because the list defaults to active rather than to unfiltered. This is the
	// most important field on the struct: see statusLine.
	Status string
	// TicketTypeName is the NAME of the filtered Ticket Type, resolved by the
	// caller against the Event's catalog. A name, never an id: an id is not
	// something the reader ever saw, and the whole sheet is written for somebody
	// who never saw the screen either.
	TicketTypeName string
	// SoldFrom and SoldTo are the sold-at range as calendar dates
	// ("YYYY-MM-DD"), read in the Event's timezone exactly as the filter was.
	SoldFrom string
	SoldTo   string
	Channel  string
	Source   string
	// PaymentMethod is the payment method filter.
	PaymentMethod string
	// Searched records only THAT a free-text search narrowed the rows, never what
	// was typed.
	//
	// This is a boolean on purpose, and the type is the decision. The Sales list's
	// search matches customer email and Tax ID number, so the term is routinely a
	// named buyer's personal data — and it is the searcher's input, not a fact
	// about any sale in the file. Writing it onto the cover sheet would restate
	// somebody's identifier in the one part of the workbook that is there even
	// when the search matched nothing at all. The spec makes the same call for the
	// export's log line, where the term is logged as a boolean and never as its
	// value; there is no reason the file should be more talkative than the log.
	//
	// The reader is still told a search happened, because a file narrower than its
	// stated filters would otherwise be inexplicable.
	Searched bool
}

// infoLines is the Info sheet's copy: one line per row, in the order a reader
// needs them — what this is, then what produced it.
//
// rowCount is the number of data rows the file actually carries, so the reader
// can check nothing was truncated between the screen and the file.
func infoLines(info Info, loc *time.Location, rowCount int, answers Answers) []string {
	lines := []string{
		"Sales Export",
		"",
		"Event: " + info.EventName,
		"Generated: " + info.GeneratedAt.In(loc).Format(infoStampFormat),
		// Excel date cells carry no timezone of their own, so this line is the
		// only place the file can say which clock its dates were drawn on — and
		// naming it as the Event's is what ties it to the sold-at filter, which is
		// read in the same zone.
		"Times shown in " + loc.String() + " (the Event's timezone).",
		rowCountLine(rowCount),
		"Currency: " + info.Currency,
		"",
		"Filters applied",
	}

	// The status filter first, and always: it is the one the reader did not
	// choose and the one most likely to mislead them.
	applied := []string{statusLine(info.Filters.Status)}
	applied = append(applied, otherFilterLines(info.Filters)...)
	if len(applied) == 1 {
		applied = append(applied, "No other filters were applied: this is every "+
			statusNoun(info.Filters.Status)+" Ticket Sale on the Event.")
	}
	lines = append(lines, applied...)

	lines = append(lines,
		"",
		"This file reflects the filters that were on screen when it was downloaded, so two "+
			"downloads of the same Event can differ. The Ticket Type columns are the Event's "+
			"catalog as it stood at the moment above.",
	)

	// The origin column, defined value by value (#373, ADR 0052).
	//
	// This sheet is the only place in the workbook that can explain it. The
	// column holds four tokens in the platform's vocabulary — nobody has met
	// `correction_replacement` before, and a reader who did not download the
	// file cannot ask the screen — while `channel` says `import` for three of
	// the four, so the values cannot be worked out from the neighbouring
	// columns either. Said as one paragraph rather than four lines, because it
	// is a legend for one column and reads as a sentence.
	lines = append(lines,
		"",
		"Each row's "+colOrigin+" says how the sale reached the platform: "+
			"sale_import — it arrived in an uploaded Sale Import batch; "+
			"manually_recorded — somebody typed it in here as a single sale, in no batch; "+
			"correction_replacement — it stands in for a sale that was corrected, and the "+
			colCorrects+" column names that sale; "+
			"channel_sale — it was sold on the platform itself and not imported at all.",
	)

	// The second sheet is announced, because a reader who did not download the
	// file has no other way to learn that the workbook is plural — and because
	// what a blank cell on it MEANS is the one thing about it that cannot be
	// read off it.
	if answers.present() {
		if answers.asked() {
			lines = append(lines,
				"",
				"The "+AnswersSheet+" sheet lists one row per ticket, for the same sales as this "+
					"file's other sheet, with one column per Ticket Question — and one TRUE/FALSE "+
					"column per option where a question takes several. Join it back on "+
					colConfirmationRef+". A blank cell there is a question that ticket has not "+
					"answered, or was never asked because it belongs to another Ticket Type.",
			)
		} else {
			// The sheet exists for its Holder columns alone (#333): nothing was
			// asked, so there are no question columns to explain, and saying so
			// beats a reader hunting for columns that were never there.
			lines = append(lines,
				"",
				"The "+AnswersSheet+" sheet lists one row per ticket, for the same sales as this "+
					"file's other sheet. Join it back on "+colConfirmationRef+". This event asks "+
					"no Ticket Questions, so the sheet carries no question columns.",
			)
		}
		// And who the ticket is for, once assignment is open. The sentence about
		// what is NOT there is the load-bearing half (ADR 0047): an Organizer who
		// typed an address into their own event and cannot find it in the file
		// would otherwise report the export as broken, and the answer is that the
		// address is not theirs to see until the person it belongs to has said so.
		if answers.Assignment {
			lines = append(lines,
				"",
				"That sheet also names each ticket's holder: its "+colAssignmentState+" — "+
					"unassigned, assigned or accepted — and, for a ticket whose holder has "+
					"accepted, their name and email address. An address a buyer entered that "+
					"its owner has not accepted is not shown here: it is theirs to disclose, "+
					"not ours, and it is deleted when the event starts.",
			)
		}
	}

	return lines
}

// rowCountLine states how many Ticket Sales the file carries, which is what a
// reader checks nothing was truncated against. It counts the rows that were
// actually written rather than any total from elsewhere, so the claim and the
// sheet cannot disagree.
func rowCountLine(rowCount int) string {
	if rowCount == 1 {
		return "Rows: 1 Ticket Sale"
	}
	return fmt.Sprintf("Rows: %d Ticket Sales", rowCount)
}

// infoStampFormat is how the generated-at moment reads. Deliberately the same
// shape as the date cells on the data sheet, so the reader sees one clock and
// one format throughout — but written as text, because nobody does arithmetic on
// a stamp and a sentence should read as a sentence.
const infoStampFormat = "2006-01-02 15:04"

// statusLine states the status filter, and it is the reason this whole sheet
// exists.
//
// The Sales list defaults to `active`, so the DEFAULT download — the one
// somebody gets by pressing Download without touching anything — silently omits
// every reversed sale. Naming the filter value ("status: active") would satisfy
// a checklist and mislead the reader: it leaves them to infer what was left out,
// and the inference is exactly the one nobody makes. So the line says what was
// excluded, in the words the reader would use.
//
// The reversed export gets the opposite sentence rather than a missing one: a
// file that reaches the reversed sales must never carry a line claiming they
// were left out.
func statusLine(status string) string {
	switch status {
	case statusReversed:
		return "Reversed sales only: every Ticket Sale in this file was reversed, and active sales are not in it."
	default:
		return "Reversed sales are excluded. This file lists active Ticket Sales only."
	}
}

// statusNoun names the sales a file holds, for the sentence that says nothing
// else narrowed them.
func statusNoun(status string) string {
	if status == statusReversed {
		return "reversed"
	}
	return "active"
}

// statusReversed is the stored spelling of the status filter that reaches
// reversed sales; anything else is the active default.
const statusReversed = "reversed"

// otherFilterLines renders the filters the person actually chose, one sentence
// each, and nothing at all for the dimensions they left alone — a blank
// "Ticket Type:" label would make a reader hunt for a value that was never set.
func otherFilterLines(f Filters) []string {
	var lines []string

	if line := soldRangeLine(f.SoldFrom, f.SoldTo); line != "" {
		lines = append(lines, line)
	}
	if f.TicketTypeName != "" {
		lines = append(lines, "Ticket Type: only sales that include "+f.TicketTypeName+".")
	}
	if f.Channel != "" {
		lines = append(lines, "Sales Channel: "+phrase(channelPhrases, f.Channel)+".")
	}
	if f.Source != "" {
		lines = append(lines, "Sales Source: "+phrase(sourcePhrases, f.Source)+".")
	}
	if f.PaymentMethod != "" {
		lines = append(lines, "Payment Method: "+phrase(paymentMethodPhrases, f.PaymentMethod)+".")
	}
	if f.Searched {
		// Said, but never quoted. See Filters.Searched.
		lines = append(lines, "A search was applied, so these rows are narrower than the filters "+
			"above alone would give. The term is not recorded here: the Sales list's search "+
			"matches a buyer's email and Tax ID number, and that is what was typed rather than "+
			"a fact about any sale in this file.")
	}
	return lines
}

// soldRangeLine renders the sold-at range, which may be open at either end. The
// zone is named again because the range is a calendar range: whether a
// late-night sale fell inside it was decided in the Event's clock, and a reader
// checking a boundary row needs to know that.
func soldRangeLine(from, to string) string {
	switch {
	case from != "" && to != "":
		return "Sold between " + from + " and " + to + ", inclusive, in the Event's timezone."
	case from != "":
		return "Sold on or after " + from + ", in the Event's timezone."
	case to != "":
		return "Sold on or before " + to + ", in the Event's timezone."
	default:
		return ""
	}
}

// The stored filter values, said the way a reader would say them. A value with
// no phrase of its own falls back to itself, so a filter added to the Sales list
// later reads awkwardly rather than silently vanishing off the stamp.
var (
	channelPhrases = map[string]string{
		"online":    "online sales only",
		"in_person": "in-person sales only",
		"import":    "imported sales only",
	}
	sourcePhrases = map[string]string{
		"direct":            "direct sales only",
		"external_platform": "sales that came from an external platform only",
	}
	paymentMethodPhrases = map[string]string{
		"cash":     "cash only",
		"transfer": "transfer only",
		"payphone": "Payphone only",
		"free":     "free sales only, where no payment was taken",
	}
)

func phrase(phrases map[string]string, value string) string {
	if p, ok := phrases[value]; ok {
		return p
	}
	return value + " only"
}

// addInfoSheet writes the Info sheet, places it FIRST and makes it active, so
// the file explains itself before it shows itself.
//
// The ordering is safe for the Sale Import parser, which selects its sheet by
// the name "Sales" and finds none here — and now cannot fall back to the sole
// sheet of a single-sheet workbook either, because this second sheet is what
// makes the workbook plural. The export's data sheet name has always been the
// intended catch; from here it is the one that actually bites.
//
// `before` is the sheet the Info sheet is moved in front of — the data sheet of
// whichever workbook is being built. It is a PARAMETER since #529, because the
// Holder Export has its own data sheet and its Info sheet has to lead that one:
// a hard-coded DataSheet here would leave the second file's cover page in the
// middle of the workbook, and MoveSheet would be naming a sheet that file does
// not have.
func addInfoSheet(f *excelize.File, before string, lines []string) error {
	if _, err := f.NewSheet(InfoSheet); err != nil {
		return err
	}
	for i, line := range lines {
		if line == "" {
			continue
		}
		if err := f.SetCellStr(InfoSheet, fmt.Sprintf("A%d", i+1), line); err != nil {
			return err
		}
	}
	// Wide enough that a sentence is a sentence rather than a column of clipped
	// words, and wrapped so the long ones are readable without widening anything.
	if err := f.SetColWidth(InfoSheet, "A", "A", 110); err != nil {
		return err
	}
	wrapped, err := f.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}})
	if err != nil {
		return err
	}
	if err := f.SetCellStyle(InfoSheet, "A1", fmt.Sprintf("A%d", len(lines)), wrapped); err != nil {
		return err
	}
	if err := f.MoveSheet(InfoSheet, before); err != nil {
		return err
	}
	idx, err := f.GetSheetIndex(InfoSheet)
	if err != nil {
		return err
	}
	f.SetActiveSheet(idx)
	return nil
}

// Build produces the .xlsx: an "Info" sheet that explains the file, then a
// "Ticket Sales" sheet holding a header row and one row per Ticket Sale, with
// nothing above the header so select-all, autofilter and pivot source ranges all
// work without deleting a preamble.
//
// types is the Event's catalog in display order, one column each — see
// TicketTypeColumn for why it is the catalog rather than the types the rows
// mention.
//
// answers is the per-Ticket sheet, and it is OPTIONAL: an Event with no Ticket
// Question hands over a zero value and the workbook is exactly the two-sheet one
// it has always been. When it is present the file gains a THIRD sheet after the
// data sheet — never columns on the data sheet, whose money must stay summable
// at one row per Ticket Sale. See AnswersSheet.
//
// Cells are really typed — dates as date cells, money as numbers in major units
// — because the recipient's next move is to sort, subtract and SUM, and a
// column of strings that look like numbers cannot be done arithmetic to. loc is
// the Event's timezone, which every date is drawn in and which the Info sheet
// names outright, since an Excel date cell carries no timezone of its own.
func Build(sales []Sale, types []TicketTypeColumn, answers Answers, loc *time.Location, info Info) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	cols := layoutFor(types)

	if err := f.SetSheetName(f.GetSheetName(0), DataSheet); err != nil {
		return nil, err
	}

	for i, h := range cols.headers {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellStr(DataSheet, cell, h); err != nil {
			return nil, err
		}
	}

	dateStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: ptr(dateFormat)})
	if err != nil {
		return nil, err
	}
	moneyStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: ptr(moneyFormat)})
	if err != nil {
		return nil, err
	}

	for i, sale := range sales {
		row := i + 2 // the header is row 1

		text := map[string]string{
			colConfirmationRef:   sale.ConfirmationRef,
			colCustomerFirstName: sale.CustomerFirstName,
			colCustomerLastName:  sale.CustomerLastName,
			colCustomerEmail:     sale.CustomerEmail,
			colCurrency:          sale.Currency,
			colChannel:           sale.Channel,
			// Written with the always-present text, never with the optional
			// pointers below it: an origin is a fact of every row. See
			// Sale.Origin.
			colOrigin: sale.Origin,
			colStatus: sale.Status,
		}
		for header, value := range text {
			if err := setStr(f, DataSheet, cols, header, row, value); err != nil {
				return nil, err
			}
		}
		// The optional ones are written only when the sale has them; an absent
		// value leaves the cell untouched, and so blank.
		for header, value := range map[string]*string{
			colTaxIDType:     sale.TaxIDType,
			colTaxIDNumber:   sale.TaxIDNumber,
			colSource:        sale.Source,
			colPaymentMethod: sale.PaymentMethod,
			colReversedBy:    sale.ReversedBy,
			colCorrectedBy:   sale.CorrectedByRef,
			colCorrects:      sale.CorrectsRef,
		} {
			if value == nil {
				continue
			}
			if err := setStr(f, DataSheet, cols, header, row, *value); err != nil {
				return nil, err
			}
		}

		// One cell per Ticket Type of the catalog, holding how many of it this
		// sale was for — and nothing at all where the sale included none. A 0
		// would claim the buyer considered that type and took none of it, and a
		// spreadsheet would then count that claim as a row in a pivot.
		//
		// The total is summed over the columns actually written, so it always
		// equals what a reader adds up across the row.
		total := 0
		for _, tt := range types {
			quantity, ok := sale.Quantities[tt.ID]
			if !ok {
				continue
			}
			total += quantity
			cell, err := cellRef(cols, tt.ID, row)
			if err != nil {
				return nil, err
			}
			if err := f.SetCellInt(DataSheet, cell, int64(quantity)); err != nil {
				return nil, err
			}
		}
		totalCell, err := cellRef(cols, colTotalQuantity, row)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellInt(DataSheet, totalCell, int64(total)); err != nil {
			return nil, err
		}

		// A real date cell, drawn in the Event's timezone: excelize reads the
		// value's zone offset off the time itself, so converting first is what
		// puts the Event's wall clock in the cell.
		soldAt, err := cellRef(cols, colSoldAt, row)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellValue(DataSheet, soldAt, sale.SoldAt.In(loc)); err != nil {
			return nil, err
		}
		// Set after the value: excelize stamps a default date style of its own
		// when writing a time, and this replaces it.
		if err := f.SetCellStyle(DataSheet, soldAt, soldAt, dateStyle); err != nil {
			return nil, err
		}

		// When the sale was undone, in the same zone and the same format as when
		// it was made — so the two can be read against each other, and subtracted.
		// A sale that was never reversed leaves the cell untouched, and so blank.
		if sale.ReversedAt != nil {
			reversedAt, err := cellRef(cols, colReversedAt, row)
			if err != nil {
				return nil, err
			}
			if err := f.SetCellValue(DataSheet, reversedAt, sale.ReversedAt.In(loc)); err != nil {
				return nil, err
			}
			if err := f.SetCellStyle(DataSheet, reversedAt, reversedAt, dateStyle); err != nil {
				return nil, err
			}
		}

		// Money as a number in major units — 25.00, never 2500 and never
		// "$25.00" — with the currency in its own column so the amounts stay
		// arithmetic rather than becoming text.
		amount, err := cellRef(cols, colAmount, row)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellFloat(DataSheet, amount, float64(sale.AmountCents)/100, 2, 64); err != nil {
			return nil, err
		}
		if err := f.SetCellStyle(DataSheet, amount, amount, moneyStyle); err != nil {
			return nil, err
		}

		// Net Proceeds, beside it — but only where the figure applies. A sale
		// with none leaves the cell untouched and so blank, never 0: writing a
		// zero would claim the platform withheld nothing from money it never
		// held, and that claim would then be summed. The cell is skipped
		// entirely, style included, so nothing distinguishes it from empty.
		if sale.NetProceedsCents != nil {
			net, err := cellRef(cols, colNetProceeds, row)
			if err != nil {
				return nil, err
			}
			if err := f.SetCellFloat(DataSheet, net, float64(*sale.NetProceedsCents)/100, 2, 64); err != nil {
				return nil, err
			}
			if err := f.SetCellStyle(DataSheet, net, net, moneyStyle); err != nil {
				return nil, err
			}
		}
	}

	// Wide enough that an email or a confirmation ref is readable without the
	// recipient having to widen every column first.
	first, err := excelize.ColumnNumberToName(1)
	if err != nil {
		return nil, err
	}
	last, err := excelize.ColumnNumberToName(len(cols.headers))
	if err != nil {
		return nil, err
	}
	if err := f.SetColWidth(DataSheet, first, last, 22); err != nil {
		return nil, err
	}

	// The per-Ticket sheet, when the Event asks anything at all or Ticket
	// Assignment is open (#333). It is added after the data sheet so it lands
	// to the right of it, and before the Info sheet so the move that puts Info
	// first still puts it first.
	if answers.present() {
		if err := addAnswersSheet(f, answers); err != nil {
			return nil, err
		}
	}

	// Last, so the row count it states is the number of rows that were written.
	if err := addInfoSheet(f, DataSheet, infoLines(info, loc, len(sales), answers)); err != nil {
		return nil, err
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// setStr writes a text cell in the column with the given key, on the named
// sheet. Both sheets of the workbook write through it, so neither of them names
// a column letter.
func setStr(f *excelize.File, sheet string, cols layout, key string, row int, value string) error {
	cell, err := cellRef(cols, key, row)
	if err != nil {
		return err
	}
	return f.SetCellStr(sheet, cell, value)
}

// cellRef resolves a column key and a 1-based row to a cell reference, so
// nothing in this file names a column letter — and nothing resolves a column by
// the heading a reader sees, which an organizer's Ticket Type name or a Ticket
// Question's wording could duplicate.
//
// A key with no column is refused by name rather than resolved to column zero.
// It is unreachable — every layout is built from the same slice the values are
// written from — so this exists to make the day somebody splits those two apart
// a failed download with a name in it, rather than a workbook quietly missing a
// value.
func cellRef(cols layout, key string, row int) (string, error) {
	column, ok := cols.index[key]
	if !ok {
		return "", fmt.Errorf("exportfile: no column for %q", key)
	}
	return excelize.CoordinatesToCellName(column, row)
}

func ptr[T any](v T) *T { return &v }
