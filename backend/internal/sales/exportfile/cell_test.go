package exportfile

import (
	"testing"
	"time"
)

// The ONE RULE for what a Ticket Answer looks like as a cell (#656, ADR 0075).
//
// These tests are deliberately about the RULE and not about a workbook: the
// Holder Export and the Sales Export's per-Ticket sheet write the bytes with two
// different writers, and what must not differ between them is the typed value
// each is handed. Nothing here imports an xlsx library, which is the point.

func TestAnswerCellIsTheAnswersOwnTypedValue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer Answer
		want   Cell
	}{
		{"text", Answer{Text: ptr("KL 1234")}, Cell{Kind: CellText, Text: "KL 1234"}},
		// Text that looks like a number stays text: an organizer asking for a
		// flight number gets back what was typed.
		{"numeric-looking text", Answer{Text: ptr("2")}, Cell{Kind: CellText, Text: "2"}},
		{"number", Answer{Number: ptr(3.5)}, Cell{Kind: CellNumber, Number: 3.5}},
		{"checked", Answer{Checked: ptr(true)}, Cell{Kind: CellBool, Bool: true}},
		// FALSE is an Answer, and never a blank.
		{"unchecked", Answer{Checked: ptr(false)}, Cell{Kind: CellBool, Bool: false}},
		// An Answer with nothing set is the shape of a bug elsewhere, and reads as
		// the Outstanding Answer it would be indistinguishable from anyway.
		{"nothing set", Answer{}, Cell{Kind: CellBlank}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.answer.Cell(); got != tc.want {
				t.Errorf("Cell() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A date Answer is a CALENDAR DATE and has no moment to move. However the value
// arrives - here at 23:30 in a zone behind UTC, which is exactly the shape that
// comes back a day late if anybody converts it - the cell is that calendar day
// at midnight UTC, so no writer can read an offset off it.
func TestAnswerCellDateIsTheCalendarDayAtMidnightUTC(t *testing.T) {
	guayaquil := time.FixedZone("ECT", -5*60*60)
	birthday := time.Date(1990, time.March, 4, 23, 30, 0, 0, guayaquil)

	got := Answer{Date: &birthday}.Cell()
	want := Cell{Kind: CellDate, Date: time.Date(1990, time.March, 4, 0, 0, 0, 0, time.UTC)}
	if got != want {
		t.Fatalf("Cell() = %+v, want %+v", got, want)
	}
}

// The fan-out of a multiple-choice Answer is CellsFor's, and the rule reads each
// Option's cell as a real boolean - TRUE where it was chosen and FALSE for the
// rest, never a blank among them.
func TestAnswerCellReadsEveryFannedOutOptionAsABoolean(t *testing.T) {
	cols := BuildQuestionColumns([]QuestionColumn{sizesQuestion()})
	cells := cols.CellsFor(map[string]Answer{"q-sizes": {Chosen: []string{"o-m"}}})

	want := map[string]Cell{
		"o-s": {Kind: CellBool, Bool: false},
		"o-m": {Kind: CellBool, Bool: true},
		"o-l": {Kind: CellBool, Bool: false},
	}
	if len(cells) != len(want) {
		t.Fatalf("got %d cells, want %d", len(cells), len(want))
	}
	for _, cell := range cells {
		if got := cell.Value.Cell(); got != want[cell.Key] {
			t.Errorf("option %q cell = %+v, want %+v", cell.Key, got, want[cell.Key])
		}
	}
}
