package exportfile

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// The per-Ticket sheet (#314): one row per Ticket, so an Organization can pivot
// "how many larges do I order".
//
// These are unit tests rather than integration ones because what they are about
// is the SHAPE of the workbook — which sheets exist, what the header row says,
// and whether a cell holds a number or a picture of one — and that is decided
// here, in the layout, rather than anywhere further up. The end-to-end seam is
// asserted over HTTP in the integration suite.

// openBuilt builds a workbook and opens it back, as a recipient would.
func openBuilt(t *testing.T, sales []Sale, types []TicketTypeColumn, answers Answers) *excelize.File {
	t.Helper()
	data, err := Build(sales, types, answers, time.UTC, Info{
		EventName:   "Answer Fest",
		GeneratedAt: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		Currency:    "USD",
		Filters:     Filters{Status: "active"},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// oneSale is the sale every fixture here hangs its Tickets off.
func oneSale() []Sale {
	return []Sale{{
		ConfirmationRef:   "ABC123",
		SoldAt:            time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		CustomerFirstName: "Ana",
		CustomerLastName:  "Lopez",
		CustomerEmail:     "ana@example.com",
		Quantities:        map[string]int{"tt-1": 1},
		AmountCents:       2500,
		Currency:          "USD",
		Channel:           "import",
		Status:            "active",
	}}
}

func gaColumn() []TicketTypeColumn {
	return []TicketTypeColumn{{ID: "tt-1", Name: "GA"}}
}

// TestAnswersSheetIsNotNamedSales is the assertion this whole sheet is one
// rename away from failing, and the reason it has a test of its own.
//
// The Sale Import parser selects its sheet by the name "Sales". A workbook
// carrying one could be uploaded back as an import, inserting every sale a
// second time and re-emailing every buyer. The data sheet has always been named
// around that; this second sheet must be too.
func TestAnswersSheetIsNotNamedSales(t *testing.T) {
	if AnswersSheet == "Sales" {
		t.Fatalf("AnswersSheet = %q — an export sheet named that can be parsed as a Sale Import", AnswersSheet)
	}
	if AnswersSheet == DataSheet {
		t.Fatalf("AnswersSheet = %q, the same name as the data sheet", AnswersSheet)
	}

	f := openBuilt(t, oneSale(), gaColumn(), Answers{
		Questions: []QuestionColumn{{ID: "q-1", Label: "T-shirt size"}},
		Tickets: []TicketRow{{
			ConfirmationRef: "ABC123",
			TicketTypeName:  "GA",
			Answers:         map[string]Answer{"q-1": {Text: ptr("M")}},
		}},
	})
	for _, name := range f.GetSheetList() {
		if name == "Sales" {
			t.Fatalf("sheets = %v — an export must never look like a Sale Import", f.GetSheetList())
		}
	}
}

// TestAnswersSheetAppearsOnlyWithQuestions: an Event that asks nothing gets the
// workbook it always got, sheet for sheet.
func TestAnswersSheetAppearsOnlyWithQuestions(t *testing.T) {
	f := openBuilt(t, oneSale(), gaColumn(), Answers{})
	got := f.GetSheetList()
	if len(got) != 2 || got[0] != InfoSheet || got[1] != DataSheet {
		t.Fatalf("sheets = %v, want exactly %q then %q when the Event asks nothing", got, InfoSheet, DataSheet)
	}

	f = openBuilt(t, oneSale(), gaColumn(), Answers{
		Questions: []QuestionColumn{{ID: "q-1", Label: "T-shirt size"}},
		Tickets: []TicketRow{{
			ConfirmationRef: "ABC123",
			TicketTypeName:  "GA",
		}},
	})
	got = f.GetSheetList()
	if len(got) != 3 || got[2] != AnswersSheet {
		t.Fatalf("sheets = %v, want %q last once the Event has a Ticket Question", got, AnswersSheet)
	}
	// Info still opens the file, and still comes first.
	if name := f.GetSheetName(f.GetActiveSheetIndex()); name != InfoSheet {
		t.Fatalf("active sheet = %q, want %q", name, InfoSheet)
	}
}

// TestDataSheetIsUnchangedByTheAnswersSheet is the promise the money columns
// rest on: the existing sheet is one row per Ticket Sale, column for column, and
// the arrival of a per-Ticket sheet beside it moves nothing.
func TestDataSheetIsUnchangedByTheAnswersSheet(t *testing.T) {
	want := [][]string{
		{
			"confirmation_ref", "sold_at",
			"customer_first_name", "customer_last_name", "customer_email",
			"tax_id_type", "tax_id_number",
			"GA", "total_quantity",
			"amount", "net_proceeds", "currency",
			"channel", "source", "payment_method", "status",
			"reversed_at", "reversed_by", "corrected_by", "corrects",
		},
		{
			"ABC123", "2026-07-01 10:00", "Ana", "Lopez", "ana@example.com",
			"", "", "1", "1", "25.00", "", "USD", "import", "", "", "active", "", "",
		},
	}

	// The same sale, exported twice: once by an Event that asks nothing, once by
	// an Event asking a question of a Ticket with several Answers on it. The
	// data sheet must read identically both times.
	for _, tc := range []struct {
		name    string
		answers Answers
	}{
		{"no questions", Answers{}},
		{"questions and answers", Answers{
			Questions: []QuestionColumn{
				{ID: "q-1", Label: "T-shirt size", Options: []OptionColumn{{ID: "o-1", Label: "M"}}},
				{ID: "q-2", Label: "Guests"},
			},
			Tickets: []TicketRow{{
				ConfirmationRef: "ABC123",
				TicketTypeName:  "GA",
				Answers: map[string]Answer{
					"q-1": {Chosen: []string{"o-1"}},
					"q-2": {Number: ptr(2.0)},
				},
			}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openBuilt(t, oneSale(), gaColumn(), tc.answers)
			rows, err := f.GetRows(DataSheet)
			if err != nil {
				t.Fatalf("get rows: %v", err)
			}
			if len(rows) != len(want) {
				t.Fatalf("data sheet rows = %d, want %d", len(rows), len(want))
			}
			for i := range want {
				// GetRows trims trailing empty cells, so compare the cells that
				// were written and require the rest to be blank.
				for j, cell := range want[i] {
					var got string
					if j < len(rows[i]) {
						got = rows[i][j]
					}
					if got != cell {
						t.Fatalf("data sheet row %d col %d = %q, want %q (row: %v)", i, j, got, cell, rows[i])
					}
				}
				if len(rows[i]) > len(want[i]) {
					t.Fatalf("data sheet row %d has %d cells, want %d: %v", i, len(rows[i]), len(want[i]), rows[i])
				}
			}
		})
	}
}

// TestAnswersSheetColumns: the confirmation ref to join back on, the Ticket Type
// name, then the Event's Ticket Questions — with a multiple-choice question
// fanned out to one column per Option rather than collapsed into a delimited
// cell, which cannot be pivoted.
func TestAnswersSheetColumns(t *testing.T) {
	f := openBuilt(t, oneSale(), gaColumn(), Answers{
		Questions: []QuestionColumn{
			{ID: "q-1", Label: "T-shirt size", Options: []OptionColumn{
				{ID: "o-s", Label: "S"},
				{ID: "o-m", Label: "M"},
				{ID: "o-l", Label: "L"},
			}},
			{ID: "q-2", Label: "Dietary notes"},
		},
		Tickets: []TicketRow{{
			ConfirmationRef: "ABC123",
			TicketTypeName:  "GA",
			Answers: map[string]Answer{
				"q-1": {Chosen: []string{"o-m"}},
				"q-2": {Text: ptr("No shellfish")},
			},
		}},
	})
	rows, err := f.GetRows(AnswersSheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	want := []string{"confirmation_ref", "ticket_type", "S", "M", "L", "Dietary notes"}
	if len(rows) != 2 || !sameStrings(rows[0], want) {
		t.Fatalf("header = %v, want %v", rows[0], want)
	}
	wantRow := []string{"ABC123", "GA", "FALSE", "TRUE", "FALSE", "No shellfish"}
	if !sameStrings(rows[1], wantRow) {
		t.Fatalf("row = %v, want %v", rows[1], wantRow)
	}
}

// TestAnswersSheetGroupsByOptionIdentity: a renamed Option is one column, not
// two. The heading is the Option's CURRENT label — the words the reader sees on
// the screen the file came from — while the column is keyed by the Option's id,
// which is what migration 072 gave Options an id for.
func TestAnswersSheetGroupsByOptionIdentity(t *testing.T) {
	f := openBuilt(t, oneSale(), gaColumn(), Answers{
		Questions: []QuestionColumn{{ID: "q-1", Label: "Meal", Options: []OptionColumn{
			// Renamed since June. Both Tickets below chose it, one before the
			// rename and one after, and both must land in this one column.
			{ID: "o-chicken", Label: "Chicken (halal)"},
			{ID: "o-vegan", Label: "Vegan"},
		}}},
		Tickets: []TicketRow{
			{ConfirmationRef: "ABC123", TicketTypeName: "GA", Answers: map[string]Answer{
				"q-1": {Chosen: []string{"o-chicken"}},
			}},
			{ConfirmationRef: "ABC123", TicketTypeName: "GA", Answers: map[string]Answer{
				"q-1": {Chosen: []string{"o-chicken"}},
			}},
		},
	})
	rows, err := f.GetRows(AnswersSheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	want := []string{"confirmation_ref", "ticket_type", "Chicken (halal)", "Vegan"}
	if !sameStrings(rows[0], want) {
		t.Fatalf("header = %v, want %v — a rename must not fork one Option into two columns", rows[0], want)
	}
	for _, row := range rows[1:] {
		if !sameStrings(row, []string{"ABC123", "GA", "TRUE", "FALSE"}) {
			t.Fatalf("row = %v, want both Tickets counted in the one Chicken column", row)
		}
	}
}

// TestAnswersSheetTypesItsCells: a number is a number and a date is a date, not
// a picture of one. The whole promise of the sheet is that "how many larges do I
// order" is a pivot table, and a column of strings cannot be summed or sorted.
func TestAnswersSheetTypesItsCells(t *testing.T) {
	birthday := time.Date(1990, 3, 4, 0, 0, 0, 0, time.UTC)
	f := openBuilt(t, oneSale(), gaColumn(), Answers{
		Questions: []QuestionColumn{
			{ID: "q-num", Label: "Guests"},
			{ID: "q-date", Label: "Birthday"},
			{ID: "q-check", Label: "Attending dinner"},
			{ID: "q-text", Label: "Notes"},
		},
		Tickets: []TicketRow{{
			ConfirmationRef: "ABC123",
			TicketTypeName:  "GA",
			Answers: map[string]Answer{
				"q-num":   {Number: ptr(3.5)},
				"q-date":  {Date: &birthday},
				"q-check": {Checked: ptr(false)},
				"q-text":  {Text: ptr("2")},
			},
		}},
	})

	raw := func(cell string) string {
		t.Helper()
		v, err := f.GetCellValue(AnswersSheet, cell, excelize.Options{RawCellValue: true})
		if err != nil {
			t.Fatalf("get %s: %v", cell, err)
		}
		return v
	}
	// The number is stored as a number.
	if got := raw("C2"); got != "3.5" {
		t.Fatalf("number answer stored as %q, want the number 3.5", got)
	}
	// The date is stored as an Excel serial and renders as a calendar date. It
	// carries no time, and no timezone: a birthday that moves by a day is the
	// bug migration 073's DATE column exists to prevent.
	// 32936 is 1990-03-04 as an Excel date serial: a whole number, so the cell
	// carries a date and no time at all.
	if got := raw("D2"); got != "32936" {
		t.Fatalf("date answer stored as %q, want the Excel date serial 32936", got)
	}
	rendered, err := f.GetCellValue(AnswersSheet, "D2")
	if err != nil {
		t.Fatalf("get D2: %v", err)
	}
	if rendered != "1990-03-04" {
		t.Fatalf("date answer renders %q, want 1990-03-04", rendered)
	}
	// FALSE is an Answer: somebody read the question and left the box unticked.
	if got := raw("E2"); got != "0" {
		t.Fatalf("checkbox answer stored as %q, want a boolean", got)
	}
	if got, _ := f.GetCellValue(AnswersSheet, "E2"); got != "FALSE" {
		t.Fatalf("checkbox answer renders %q, want FALSE", got)
	}
	// And a text answer stays text, even when it looks like a number: an
	// organizer asking for a flight number gets back what was typed.
	if got := raw("F2"); got != "2" {
		t.Fatalf("text answer stored as %q, want the text it was given", got)
	}
	cellType, err := f.GetCellType(AnswersSheet, "F2")
	if err != nil {
		t.Fatalf("cell type: %v", err)
	}
	if cellType == excelize.CellTypeNumber {
		t.Fatalf("text answer stored as a number — a short_text Answer is what somebody typed")
	}
}

// TestAnswersSheetLeavesOutstandingAnswersBlank: a blank cell means an
// Outstanding Answer — a question this Ticket has not answered, or was never
// asked because it belongs to another Ticket Type. A multiple-choice question
// with no Answer at all leaves every one of its Option columns blank rather than
// writing FALSE across them, which would claim somebody read it and declined.
func TestAnswersSheetLeavesOutstandingAnswersBlank(t *testing.T) {
	f := openBuilt(t, oneSale(), gaColumn(), Answers{
		Questions: []QuestionColumn{
			{ID: "q-1", Label: "Meal", Options: []OptionColumn{
				{ID: "o-a", Label: "Chicken"},
				{ID: "o-b", Label: "Vegan"},
			}},
			{ID: "q-2", Label: "Notes"},
		},
		Tickets: []TicketRow{{ConfirmationRef: "ABC123", TicketTypeName: "GA"}},
	})
	for _, cell := range []string{"C2", "D2", "E2"} {
		got, err := f.GetCellValue(AnswersSheet, cell, excelize.Options{RawCellValue: true})
		if err != nil {
			t.Fatalf("get %s: %v", cell, err)
		}
		if got != "" {
			t.Fatalf("%s = %q, want a blank cell: an unanswered question is an Outstanding Answer, not a FALSE", cell, got)
		}
	}
}

// threeStates is one Ticket in each of the three assignment states, on one sale,
// as the repository would hand them over: an accepted Ticket carries a Holder, an
// `assigned` one carries the word and nothing else, and an `unassigned` one
// carries only the word.
//
// NOTE WHAT THE `assigned` ROW DOES NOT HAVE. There is no address on it, and
// there is nowhere on TicketRow to put one — that is ADR 0047's line, and the
// fixture is shaped the way the production read is.
func threeStates() []TicketRow {
	return []TicketRow{
		{
			ConfirmationRef: "ABC123",
			TicketTypeName:  "GA",
			AssignmentState: "accepted",
			HolderFirstName: "Carla",
			HolderLastName:  "Ruiz",
			HolderEmail:     "carla@example.com",
			Answers:         map[string]Answer{"q-1": {Text: ptr("L")}},
		},
		{
			ConfirmationRef: "ABC123",
			TicketTypeName:  "GA",
			AssignmentState: "assigned",
		},
		{
			ConfirmationRef: "ABC123",
			TicketTypeName:  "GA",
			AssignmentState: "unassigned",
		},
	}
}

// TestAnswersSheetHolderColumns: who the Ticket is for, beside what they
// answered (#330, ADR 0047).
//
// THE `assigned` ROW IS THE ONE THAT MATTERS. Its address exists — a buyer typed
// it and the platform mailed it — and it is NOT IN THIS FILE, because nobody at
// that address has agreed to be in it. The row says `assigned` and stops, which
// is precisely what an Organizer needs to know: somebody was named, and they
// have not clicked.
func TestAnswersSheetHolderColumns(t *testing.T) {
	f := openBuilt(t, oneSale(), gaColumn(), Answers{
		Assignment: true,
		Questions:  []QuestionColumn{{ID: "q-1", Label: "T-shirt size"}},
		Tickets:    threeStates(),
	})
	rows, err := f.GetRows(AnswersSheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}

	// The Holder sits between the Ticket Type and the questions: the person,
	// then what the person said, read left to right in one sheet.
	want := []string{
		"confirmation_ref", "ticket_type",
		"assignment_state", "holder_first_name", "holder_last_name", "holder_email",
		"T-shirt size",
	}
	if len(rows) != 4 || !sameStrings(rows[0], want) {
		t.Fatalf("header = %v, want %v", rows[0], want)
	}

	for _, tc := range []struct {
		name string
		row  int
		want []string
	}{
		{
			// Accepted: the whole person, and their size on the same row. This
			// is the one sheet "who is coming and what size are they" is meant
			// to be.
			name: "accepted",
			row:  1,
			want: []string{"ABC123", "GA", "accepted", "Carla", "Ruiz", "carla@example.com", "L"},
		},
		{
			// Assigned: the word, and three blanks where an unconsented address
			// is not. Blanks and not zeros, empty strings or a placeholder — an
			// Organizer filtering the sheet on "holder_email is blank" is asking
			// "who has not claimed their ticket", and must get this row.
			name: "assigned, and the address stays out of the file",
			row:  2,
			want: []string{"ABC123", "GA", "assigned", "", "", "", ""},
		},
		{
			// Unassigned: nobody was ever named. Also the state a Ticket whose
			// address was purged at Event start reads as (migration 081) — there
			// is no fourth word, and this file does not invent one.
			name: "unassigned",
			row:  3,
			want: []string{"ABC123", "GA", "unassigned", "", "", "", ""},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := rows[tc.row]
			// GetRows trims trailing empty cells, and a blank IS the value under
			// test on two of these rows.
			for len(got) < len(tc.want) {
				got = append(got, "")
			}
			if !sameStrings(got, tc.want) {
				t.Fatalf("row = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAnswersSheetHolderColumnsAppearOnlyWithAssignment: with
// TICKET_ASSIGNMENT_ENABLED closed the sheet is the one #314 built, column for
// column — and it is closed on every deployment until a Policy Version describes
// the disclosure (ADR 0045).
//
// The Tickets handed in below are FULLY POPULATED HOLDERS. The flag has to be a
// real off switch rather than a filter on empty data: an address must not leave
// the building in a file just because a row happened to carry one.
func TestAnswersSheetHolderColumnsAppearOnlyWithAssignment(t *testing.T) {
	f := openBuilt(t, oneSale(), gaColumn(), Answers{
		Questions: []QuestionColumn{{ID: "q-1", Label: "T-shirt size"}},
		Tickets:   threeStates(),
	})
	rows, err := f.GetRows(AnswersSheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	want := []string{"confirmation_ref", "ticket_type", "T-shirt size"}
	if !sameStrings(rows[0], want) {
		t.Fatalf("header = %v, want %v while assignment is dark", rows[0], want)
	}
	for _, row := range rows[1:] {
		for _, cell := range row {
			if cell == "carla@example.com" || cell == "Carla" || cell == "accepted" {
				t.Fatalf("row = %v carries a Holder while assignment is dark", row)
			}
		}
	}
}

// sameStrings compares two string slices element for element.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
