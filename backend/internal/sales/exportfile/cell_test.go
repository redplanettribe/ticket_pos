package exportfile

import (
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"
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
		// Empty text is no value at all, so it is a blank and not an empty
		// string: a reader filtering on "is blank" must find it.
		{"empty text", Answer{Text: ptr("")}, Cell{Kind: CellBlank}},
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

// A DATE EXCEL CANNOT HOLD IS WRITTEN AS ITS ISO DATE TEXT. Excel's 1900 date
// system has no serial before 1900-01-01, so a date Answer before then - a
// great-grandparent's birthday, a typo - is the calendar date as text, rather
// than a wrong date or a timestamp with a time and a zone nobody typed.
func TestAnswerCellDateBefore1900IsItsISODateAsText(t *testing.T) {
	for _, tc := range []struct {
		name string
		date time.Time
		want Cell
	}{
		{"before 1900", time.Date(1850, time.January, 1, 0, 0, 0, 0, time.UTC),
			Cell{Kind: CellText, Text: "1850-01-01"}},
		{"the last day before 1900, late in a zone behind UTC",
			time.Date(1899, time.December, 31, 23, 30, 0, 0, time.FixedZone("ECT", -5*60*60)),
			Cell{Kind: CellText, Text: "1899-12-31"}},
		{"the first day Excel has a serial for",
			time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC),
			Cell{Kind: CellDate, Date: time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Answer{Date: &tc.date}).Cell(); got != tc.want {
				t.Errorf("Cell() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TEXT IS ONE RULE TOO: empty is a blank, and anything longer than Excel's
// limit is cut to it, so the two writers cannot disagree about either.
func TestTextCellIsBlankWhenEmpty(t *testing.T) {
	if got := TextCell(""); got != (Cell{Kind: CellBlank}) {
		t.Fatalf(`TextCell("") = %+v, want a blank`, got)
	}
	if got := TextCell("M"); got != (Cell{Kind: CellText, Text: "M"}) {
		t.Fatalf(`TextCell("M") = %+v, want text M`, got)
	}
}

// TEXT IS CUT WHERE EXCEL CUTS IT, which is 32,767 UTF-16 code units and not
// 32,767 characters: a character outside the Basic Multilingual Plane - most
// emoji, some CJK - is two units to Excel and one rune to Go, so a rune count
// lets through a cell Excel refuses. The cut never splits a surrogate pair,
// because half of one is not a character at all.
func TestTextCellCutsAtExcelsLimitInUTF16Units(t *testing.T) {
	const astral = "\U0001F600" // one rune, two UTF-16 code units
	for _, tc := range []struct {
		name, text, want string
	}{
		{"astral characters count as two",
			strings.Repeat(astral, 20_000), strings.Repeat(astral, 16_383)},
		{"a pair that would straddle the limit is left out whole",
			strings.Repeat("a", 32_766) + astral, strings.Repeat("a", 32_766)},
		{"a pair that ends exactly on the limit is kept",
			strings.Repeat("a", 32_765) + astral, strings.Repeat("a", 32_765) + astral},
		{"text inside the Basic Multilingual Plane is one unit a character",
			strings.Repeat("é", 40_000), strings.Repeat("é", 32_767)},
		{"text at the limit is untouched",
			strings.Repeat("a", 32_767), strings.Repeat("a", 32_767)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TextCell(tc.text)
			if got.Kind != CellText || got.Text != tc.want {
				t.Errorf("cell holds %d UTF-16 units (%d runes), want %d units (%d runes)",
					len(utf16.Encode([]rune(got.Text))), utf8.RuneCountInString(got.Text),
					len(utf16.Encode([]rune(tc.want))), utf8.RuneCountInString(tc.want))
			}
		})
	}
	// An Answer's text goes through the same cut.
	long := strings.Repeat(astral, 20_000)
	if got := (Answer{Text: &long}).Cell(); got.Text != strings.Repeat(astral, 16_383) {
		t.Errorf("an Answer's text holds %d runes, want it cut like any text", utf8.RuneCountInString(got.Text))
	}
}
