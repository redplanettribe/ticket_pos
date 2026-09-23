package exportfile

import (
	"archive/zip"
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// The Holder Export's workbook builder (#529, parent #518, ADR 0065).
//
// These tests are about the FILE and not about the query behind it: which
// columns exist, what a cell holds, what the Info sheet says, and — the
// load-bearing one — that an address the builder was never handed is nowhere in
// the bytes. The service's own rules (which filters were honoured, the snapshot,
// the disclosure decision) are asserted where they live, in the integration
// suite.

func quito(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Guayaquil")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return loc
}

// holderFixture is one accepted Ticket and one that was named and never
// claimed — the two rows every disclosure assertion below needs.
func holderFixture() HolderRoster {
	return HolderRoster{
		Assignment: true,
		Tickets: []HolderRow{
			{
				ConfirmationRef:   "TP-AAA111",
				SoldAt:            time.Date(2026, 7, 4, 22, 30, 0, 0, time.UTC),
				Channel:           "online",
				TicketTypeName:    "General",
				Ordinal:           1,
				CustomerFirstName: "Ana",
				CustomerLastName:  "Paz",
				CustomerEmail:     "ana@example.com",
				AssignmentState:   "accepted",
				HolderFirstName:   "Beto",
				HolderLastName:    "Ruiz",
				HolderEmail:       "beto@example.com",
			},
			{
				ConfirmationRef:   "TP-AAA111",
				SoldAt:            time.Date(2026, 7, 4, 22, 30, 0, 0, time.UTC),
				Channel:           "online",
				TicketTypeName:    "General",
				Ordinal:           2,
				CustomerFirstName: "Ana",
				CustomerLastName:  "Paz",
				CustomerEmail:     "ana@example.com",
				// Named, never claimed, address since purged. The screen says this
				// and so does the file: `assigned` plus a marker, never a fourth
				// state, and never an address.
				AssignmentState: "assigned",
				NeverAccepted:   true,
			},
		},
	}
}

func holderInfoFixture() HolderInfo {
	return HolderInfo{
		EventName:   "Summer Fest",
		GeneratedAt: time.Date(2026, 8, 29, 15, 4, 0, 0, time.UTC),
	}
}

// buildHolders builds a workbook and opens it, failing the test on either.
func buildHolders(t *testing.T, roster HolderRoster, info HolderInfo) ([]byte, *excelize.File) {
	t.Helper()
	data, err := BuildHolderExport(roster, quito(t), info)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return data, f
}

// holderHeader returns the data sheet's header row.
func holderHeader(t *testing.T, f *excelize.File) []string {
	t.Helper()
	rows, err := f.GetRows(HolderSheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("workbook has no header row")
	}
	return rows[0]
}

// holderCell reads a cell by column HEADING and zero-based data row, so nothing
// in this file names a column letter and a column added later shifts nothing.
func holderCell(t *testing.T, f *excelize.File, row int, heading string) string {
	t.Helper()
	header := holderHeader(t, f)
	for i, h := range header {
		if h != heading {
			continue
		}
		ref, err := excelize.CoordinatesToCellName(i+1, row+2)
		if err != nil {
			t.Fatalf("cell name: %v", err)
		}
		value, err := f.GetCellValue(HolderSheet, ref)
		if err != nil {
			t.Fatalf("get %s: %v", heading, err)
		}
		return value
	}
	t.Fatalf("no %q column in header %v", heading, header)
	return ""
}

// The workbook is TWO SHEETS, Info first and the data sheet NEVER named "Sales"
// — the Sale Import parser selects its sheet by that name, and a roster uploaded
// back as an import would insert every sale a second time and re-email every
// buyer.
func TestBuildHolderExportSheetsAreNamedAwayFromSales(t *testing.T) {
	_, f := buildHolders(t, holderFixture(), holderInfoFixture())

	sheets := f.GetSheetList()
	if want := []string{InfoSheet, HolderSheet}; !reflect.DeepEqual(sheets, want) {
		t.Fatalf("sheets = %v, want %v — Info leads so the file explains itself first", sheets, want)
	}
	for _, sheet := range sheets {
		if sheet == "Sales" {
			t.Fatalf("a sheet is named %q; the Sale Import parser selects by that name", sheet)
		}
	}
	// Nothing above the header, so select-all, autofilter and pivot source ranges
	// work without deleting a preamble. The Info sheet exists precisely so this
	// holds.
	if got := holderHeader(t, f)[0]; got != colConfirmationRef {
		t.Fatalf("row 1 column A = %q, want the header %q", got, colConfirmationRef)
	}
}

// ONE ROW PER TICKET, the agreed columns, and NO MONEY COLUMNS OF ANY KIND.
//
// The money assertion is the point of the test. A roster repeats a sale's
// details once per Ticket, so any amount on it would be summed several times
// over; and a file with money on it can be forwarded as a financial document,
// which this one must never be mistaken for.
func TestBuildHolderExportColumnsCarryNoMoney(t *testing.T) {
	roster := holderFixture()
	roster.Questions = []QuestionColumn{{ID: "q-diet", Label: "Dietary needs"}}
	_, f := buildHolders(t, roster, holderInfoFixture())

	want := []string{
		colConfirmationRef, colSoldAt, colChannel, colTicketTypeName, colTicketOrdinal,
		colCustomerFirstName, colCustomerLastName, colCustomerEmail,
		colAssignmentState, colNeverAccepted,
		colHolderFirstName, colHolderLastName, colHolderEmail,
		"Dietary needs",
	}
	if got := holderHeader(t, f); !reflect.DeepEqual(got, want) {
		t.Fatalf("header = %v, want %v", got, want)
	}

	rows, err := f.GetRows(HolderSheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if got := len(rows) - 1; got != 2 {
		t.Fatalf("data rows = %d, want one per Ticket (2)", got)
	}

	for _, heading := range holderHeader(t, f) {
		lower := strings.ToLower(heading)
		for _, forbidden := range []string{"amount", "net_proceeds", "net proceeds", "currency", "price", "total"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("header %q looks like money (%q); this file carries none", heading, forbidden)
			}
		}
	}
}

// The ordinal is a REAL NUMBER, so 10 sorts after 9 rather than before it — and
// it is here at all because a row is a Ticket: two Tickets of one sale line
// differ in nothing else.
func TestBuildHolderExportOrdinalIsANumber(t *testing.T) {
	_, f := buildHolders(t, holderFixture(), holderInfoFixture())

	header := holderHeader(t, f)
	var col int
	for i, h := range header {
		if h == colTicketOrdinal {
			col = i + 1
		}
	}
	ref, err := excelize.CoordinatesToCellName(col, 3) // the second Ticket
	if err != nil {
		t.Fatalf("cell name: %v", err)
	}
	kind, err := f.GetCellType(HolderSheet, ref)
	if err != nil {
		t.Fatalf("cell type: %v", err)
	}
	// A numeric cell carries no `t` attribute at all in the sheet XML, which
	// excelize reports as CellTypeUnset; a text one is a shared string. The
	// assertion is that this is NOT text, because a column of strings that look
	// like numbers sorts lexically — 10 before 9 — which is the bug worth a test.
	if kind == excelize.CellTypeSharedString || kind == excelize.CellTypeInlineString {
		t.Fatalf("ordinal cell type = %v, want a number and not text", kind)
	}
	if got := holderCell(t, f, 1, colTicketOrdinal); got != "2" {
		t.Fatalf("second Ticket's ordinal = %q, want 2", got)
	}
}

// The sold-at cell is a real date drawn in the EVENT's timezone, so the file can
// never contradict the date range that selected its rows. 2026-07-04 22:30 UTC
// is the 4th at 17:30 in Guayaquil.
func TestBuildHolderExportSoldAtIsTheEventsClock(t *testing.T) {
	_, f := buildHolders(t, holderFixture(), holderInfoFixture())
	if got, want := holderCell(t, f, 0, colSoldAt), "2026-07-04 17:30"; got != want {
		t.Fatalf("sold_at = %q, want %q (the Event's zone)", got, want)
	}
}

// THE LOAD-BEARING ONE. An unaccepted Holder's address must be absent from the
// RAW BYTES OF THE WHOLE WORKBOOK, not merely from a named cell.
//
// A cell-level assertion would pass over a file that carried the address in
// xl/sharedStrings.xml, in a cached formula, in a defined name or in a comment —
// and the person the address belongs to would be no less exposed for it, because
// a spreadsheet is a zip a recipient can open with a text editor. So this unzips
// the workbook and searches EVERY entry.
//
// The builder here is handed exactly what the service hands it, which is a row
// whose Holder fields are empty because fillHolderListEntry left them empty. What
// this test pins is the other half: given nothing, this package writes nothing —
// it invents no placeholder, keeps no copy, and leaks nothing through a string
// table.
func TestBuildHolderExportNeverCarriesAnUnacceptedAddress(t *testing.T) {
	const unaccepted = "never-accepted@example.com"
	const purged = "purged-holder@example.com"

	roster := holderFixture()
	// The row the buyer named and nobody claimed, exactly as the service builds
	// it: the word `assigned` and three blank cells. The two addresses below are
	// what a caller that skipped fillHolderListEntry WOULD have put here, and they
	// are searched for by name.
	roster.Tickets = append(roster.Tickets, HolderRow{
		ConfirmationRef: "TP-BBB222",
		SoldAt:          time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
		Channel:         "in_person",
		TicketTypeName:  "VIP",
		Ordinal:         1,
		AssignmentState: "assigned",
	}, HolderRow{
		ConfirmationRef: "TP-CCC333",
		SoldAt:          time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC),
		Channel:         "import",
		TicketTypeName:  "VIP",
		Ordinal:         1,
		AssignmentState: "assigned",
		NeverAccepted:   true,
	})

	data, _ := buildHolders(t, roster, holderInfoFixture())

	for _, secret := range []string{unaccepted, purged} {
		assertAbsentFromWorkbook(t, data, secret)
	}
	// The control: an address that IS disclosable is in there, so the search above
	// is proving something. A test that could pass over an empty file proves
	// nothing at all.
	assertPresentInWorkbook(t, data, "beto@example.com")
}

// assertAbsentFromWorkbook unzips an .xlsx and fails if any entry contains the
// needle. An .xlsx is a ZIP, so searching the compressed bytes would find
// nothing whatever the file said — the entries have to be read out and searched
// one by one, xl/sharedStrings.xml above all, since that is where excelize puts
// every text cell's value.
func assertAbsentFromWorkbook(t *testing.T, data []byte, needle string) {
	t.Helper()
	for name, content := range workbookEntries(t, data) {
		if strings.Contains(content, needle) {
			t.Fatalf("%q appears in workbook entry %s — an unaccepted address must be nowhere in the file", needle, name)
		}
	}
}

// assertPresentInWorkbook is assertAbsentFromWorkbook's control.
func assertPresentInWorkbook(t *testing.T, data []byte, needle string) {
	t.Helper()
	for _, content := range workbookEntries(t, data) {
		if strings.Contains(content, needle) {
			return
		}
	}
	t.Fatalf("%q appears in NO workbook entry; the absence assertions above are vacuous", needle)
}

// workbookEntries reads every entry of an .xlsx zip into memory, by name.
func workbookEntries(t *testing.T, data []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open .xlsx as a zip: %v", err)
	}
	entries := map[string]string{}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", file.Name, err)
		}
		entries[file.Name] = string(content)
	}
	if _, ok := entries["xl/sharedStrings.xml"]; !ok {
		t.Fatalf("workbook has no xl/sharedStrings.xml; entries were %v — the search would miss every text cell", keysOf(entries))
	}
	return entries
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The Info sheet names the Event, the moment, the timezone as the EVENT's, the
// row count and the filters in words.
func TestHolderInfoSheetSaysWhatThisIs(t *testing.T) {
	info := holderInfoFixture()
	info.Filters = HolderFilters{
		TicketTypeName:  "VIP",
		Channel:         "in_person",
		AssignmentState: "never_accepted",
		SoldFrom:        "2026-07-01",
		SoldTo:          "2026-07-31",
		OwingOnly:       true,
		QuestionLabel:   "T-shirt size",
		Sort:            "holder",
		Dir:             "desc",
	}
	_, f := buildHolders(t, holderFixture(), info)

	text := infoText(t, f)
	for _, want := range []string{
		"Holder Export",
		"Event: Summer Fest",
		"Generated: 2026-08-29 10:04",
		"America/Guayaquil",
		"the Event's timezone",
		"Rows: 2 Tickets",
		"Sold between 2026-07-01 and 2026-07-31",
		"only Tickets of VIP",
		"in-person sales only",
		"named and never claimed",
		"Outstanding Answers only",
		"T-shirt size",
		"Sorted by the holder's name",
		"descending",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Info sheet does not say %q; it said:\n%s", want, text)
		}
	}
	// And it warns off the money it does not carry, so nobody hunts for a column
	// that was deliberately left out.
	if !strings.Contains(text, "no amounts") {
		t.Errorf("Info sheet does not explain the absent money; it said:\n%s", text)
	}
}

// A filter the caller never set is described NOWHERE. This is the shape of
// #524's rule as the file sees it: the service passes only the filters it
// honoured, so a dropped one is one this package never hears of — and a sheet
// that named it would have a reader believe a whole roster was a filtered one.
func TestHolderInfoSheetDescribesOnlyWhatItWasGiven(t *testing.T) {
	_, f := buildHolders(t, holderFixture(), holderInfoFixture())

	text := infoText(t, f)
	if !strings.Contains(text, "No filters were applied") {
		t.Fatalf("an unfiltered export does not say so; it said:\n%s", text)
	}
	for _, absent := range []string{
		"Ticket Type:", "Sales Channel:", "Holder:", "Outstanding Answers only", "Owing one named question",
	} {
		if strings.Contains(text, absent) {
			t.Errorf("Info sheet describes %q, which was never applied; it said:\n%s", absent, text)
		}
	}
}

// A HONOURED FILTER IS DESCRIBED EVEN WHEN ITS LABEL COULD NOT BE RESOLVED, and
// this is the converse of the test above rather than an exception to it.
//
// The service resolves the Ticket Type's name and the question's wording with
// READS, and a read can fail — a transient catalog error, or an id naming
// nothing on this Event. Deciding the line off the label alone meant a genuinely
// filtered file whose cover sheet read "No filters were applied: this is every
// Ticket… on the Event." A reader then takes a narrowed roster for a whole one
// and concludes people are missing from the EVENT rather than from the FILE,
// which is the exact harm #529's honoured-filters rule exists to prevent,
// arrived at from the opposite direction.
//
// So the id travels beside the label and the sheet falls back to it: uglier than
// a name, and infinitely better than silence.
func TestHolderInfoSheetStillNamesAFilterWhoseLabelIsUnresolvable(t *testing.T) {
	info := holderInfoFixture()
	info.Filters = HolderFilters{
		TicketTypeID: "9f1c0a44-0000-4000-8000-00000000ffff",
		QuestionID:   "3b2d0e55-0000-4000-8000-00000000eeee",
	}
	_, f := buildHolders(t, holderFixture(), info)

	text := infoText(t, f)
	// The load-bearing assertion: the file must NOT claim to be everybody.
	if strings.Contains(text, "No filters were applied") {
		t.Fatalf("a filtered file says no filters were applied; it said:\n%s", text)
	}
	for _, want := range []string{
		"Ticket Type: only Tickets of one Ticket Type",
		"9f1c0a44-0000-4000-8000-00000000ffff",
		"Owing one named question: only Tickets that have not answered one Ticket Question",
		"3b2d0e55-0000-4000-8000-00000000eeee",
		"could not be read",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Info sheet does not say %q; it said:\n%s", want, text)
		}
	}

	// AND THE NAME STILL WINS WHERE THERE IS ONE. The id is a fallback, not a
	// second line: a reader who can be told "VIP" is never shown a UUID.
	info.Filters.TicketTypeName = "VIP"
	info.Filters.QuestionLabel = "T-shirt size"
	_, resolved := buildHolders(t, holderFixture(), info)
	text = infoText(t, resolved)
	if !strings.Contains(text, "Ticket Type: only Tickets of VIP.") {
		t.Errorf("Info sheet does not name the Ticket Type; it said:\n%s", text)
	}
	if strings.Contains(text, "9f1c0a44-0000-4000-8000-00000000ffff") ||
		strings.Contains(text, "3b2d0e55-0000-4000-8000-00000000eeee") {
		t.Errorf("Info sheet prints an id it had a name for; it said:\n%s", text)
	}
}

// A search is reported as APPLIED without the term ever being written — into the
// Info sheet or into any other byte of the workbook. The Holder List's search
// matches customer addresses, and this file is forwarded.
func TestHolderInfoSheetSaysASearchHappenedAndNeverWhat(t *testing.T) {
	info := holderInfoFixture()
	info.Filters = HolderFilters{Searched: true}
	data, f := buildHolders(t, holderFixture(), info)

	if text := infoText(t, f); !strings.Contains(text, "A search was applied") {
		t.Fatalf("Info sheet does not report the search; it said:\n%s", text)
	}
	// The type is the guarantee: HolderFilters.Searched is a bool, so there is no
	// term to leak. This asserts the property a future `SearchTerm string` field
	// would break.
	assertAbsentFromWorkbook(t, data, "ana@example.com's secret term")
}

// The Holder block is the FLAG's doing and not the data's: with Ticket
// Assignment closed the file has no assignment columns at all, on every Event —
// the same off switch Answers.Assignment is for the Sales Export (ADR 0045).
func TestBuildHolderExportOmitsTheHolderBlockWhileAssignmentIsDark(t *testing.T) {
	roster := holderFixture()
	roster.Assignment = false
	_, f := buildHolders(t, roster, holderInfoFixture())

	for _, absent := range []string{colAssignmentState, colNeverAccepted, colHolderFirstName, colHolderLastName, colHolderEmail} {
		for _, h := range holderHeader(t, f) {
			if h == absent {
				t.Fatalf("column %q exists while assignment is dark", absent)
			}
		}
	}
	if text := infoText(t, f); strings.Contains(text, colAssignmentState) {
		t.Fatalf("Info sheet explains assignment columns that do not exist:\n%s", text)
	}
}

// never_accepted is a real boolean on EVERY row, so "how many did I name who
// never claimed" is a COUNTIF. A column of blanks and TRUEs counts neither half.
func TestBuildHolderExportNeverAcceptedIsCountable(t *testing.T) {
	_, f := buildHolders(t, holderFixture(), holderInfoFixture())

	if got := holderCell(t, f, 0, colNeverAccepted); got != "FALSE" {
		t.Errorf("accepted Ticket's never_accepted = %q, want FALSE", got)
	}
	if got := holderCell(t, f, 1, colNeverAccepted); got != "TRUE" {
		t.Errorf("purged Ticket's never_accepted = %q, want TRUE", got)
	}
	// And it still reads `assigned`: three words, never four.
	if got := holderCell(t, f, 1, colAssignmentState); got != "assigned" {
		t.Errorf("purged Ticket's assignment_state = %q, want assigned", got)
	}
}

// The question and Option columns come from the SHARED builder, not a second
// implementation — a seam test in questioncolumns_test.go's spirit: the headers
// this file writes are BuildQuestionColumns's headings, in its order, spliced
// after the fixed columns and nowhere else.
func TestBuildHolderExportQuestionColumnsComeFromTheSharedBuilder(t *testing.T) {
	questions := []QuestionColumn{
		{ID: "q-flight", Label: "Flight number"},
		sizesQuestion(),
	}
	roster := holderFixture()
	roster.Questions = questions
	roster.Tickets[0].Answers = map[string]Answer{
		"q-sizes": {Chosen: []string{"o-m"}},
	}
	_, f := buildHolders(t, roster, holderInfoFixture())

	shared := BuildQuestionColumns(questions)
	header := holderHeader(t, f)
	tail := header[len(header)-shared.Len():]
	if !reflect.DeepEqual(tail, shared.Headings()) {
		t.Fatalf("question headings = %v, want the shared builder's %v", tail, shared.Headings())
	}

	// And the FAN-OUT is the builder's too: an answered multiple-choice question
	// writes TRUE under what it chose and FALSE under the rest, because the
	// question was answered and a FALSE among them is a fact rather than a
	// silence.
	if got := holderCell(t, f, 0, "Medium"); got != "TRUE" {
		t.Errorf("chosen Option = %q, want TRUE", got)
	}
	if got := holderCell(t, f, 0, "S"); got != "FALSE" {
		t.Errorf("unchosen Option of an answered question = %q, want FALSE", got)
	}
	// An UNANSWERED question leaves its whole block blank — an Outstanding Answer
	// is a debt and not a FALSE. The second Ticket answered nothing.
	if got := holderCell(t, f, 1, "Medium"); got != "" {
		t.Errorf("unanswered Option cell = %q, want blank", got)
	}
	if got := holderCell(t, f, 0, "Flight number"); got != "" {
		t.Errorf("unanswered question cell = %q, want blank", got)
	}
}

// The Holder Export writes the same cells as the Sales Export for the two
// answers a spreadsheet cannot take literally, because both come from the one
// cell rule: empty text is no cell, and a date before 1900 is its ISO date text.
func TestBuildHolderExportWritesWhatTheCellRuleDecides(t *testing.T) {
	longAgo := time.Date(1850, time.January, 1, 0, 0, 0, 0, time.UTC)
	roster := holderFixture()
	roster.Questions = []QuestionColumn{
		{ID: "q-notes", Label: "Notes"},
		{ID: "q-birthday", Label: "Birthday"},
	}
	roster.Tickets[0].Answers = map[string]Answer{
		"q-notes":    {Text: ptr("")},
		"q-birthday": {Date: &longAgo},
	}
	_, f := buildHolders(t, roster, holderInfoFixture())

	header := holderHeader(t, f)
	ref := func(heading string) string {
		t.Helper()
		for i, h := range header {
			if h == heading {
				name, err := excelize.CoordinatesToCellName(i+1, 2)
				if err != nil {
					t.Fatalf("cell name: %v", err)
				}
				return name
			}
		}
		t.Fatalf("no %q column in %v", heading, header)
		return ""
	}
	if got, _ := f.GetCellType(HolderSheet, ref("Notes")); got != excelize.CellTypeUnset {
		t.Fatalf("empty text answer written as a cell of type %v, want no cell", got)
	}
	if v, _ := f.GetCellValue(HolderSheet, ref("Notes"), excelize.Options{RawCellValue: true}); v != "" {
		t.Fatalf("empty text answer holds %q, want nothing", v)
	}
	if got := holderCell(t, f, 0, "Birthday"); got != "1850-01-01" {
		t.Fatalf("pre-1900 date answer reads %q, want the ISO date 1850-01-01", got)
	}
}

// infoText is the Info sheet flattened to one string, for assertions about what
// it says rather than about which row says it.
func infoText(t *testing.T, f *excelize.File) string {
	t.Helper()
	rows, err := f.GetRows(InfoSheet)
	if err != nil {
		t.Fatalf("get Info rows: %v", err)
	}
	var b strings.Builder
	for _, row := range rows {
		for _, cell := range row {
			b.WriteString(cell)
			b.WriteString("\n")
		}
	}
	return b.String()
}
