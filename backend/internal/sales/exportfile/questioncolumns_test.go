package exportfile

import (
	"reflect"
	"testing"
)

// The shared Ticket Question and Option column builder (#520, ADR 0065).
//
// These tests are deliberately about the BUILDER and not about a workbook: the
// Sales Export's own tests already assert what its sheet looks like, and the
// point of this file is the layer beneath both callers. If the Holder Export
// (#529) ever disagreed with the per-Ticket sheet about what an Answer is, it
// would be one of these assertions that changed to let it.

// sizesQuestion is the fixture the fan-out assertions hang off: one
// multiple-choice question whose middle Option has been RENAMED since some
// Tickets chose it, which is the case the whole grouping-by-identity rule exists
// for.
func sizesQuestion() QuestionColumn {
	return QuestionColumn{ID: "q-sizes", Label: "T-shirt size", Options: []OptionColumn{
		{ID: "o-s", Label: "S"},
		{ID: "o-m", Label: "Medium"}, // corrected from "Mediun"
		{ID: "o-l", Label: "L"},
	}}
}

// A plain question is one column, keyed by the question and headed by its
// current wording.
func TestBuildQuestionColumnsPlainQuestionIsOneColumn(t *testing.T) {
	cols := BuildQuestionColumns([]QuestionColumn{
		{ID: "q-1", Label: "Flight number"},
		{ID: "q-2", Label: "Dietary needs"},
	})

	if got, want := cols.Keys(), []string{"q-1", "q-2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	if got, want := cols.Headings(), []string{"Flight number", "Dietary needs"}; !reflect.DeepEqual(got, want) {
		t.Errorf("headings = %v, want %v", got, want)
	}
	if cols.Len() != 2 {
		t.Errorf("Len = %d, want 2", cols.Len())
	}
}

// A multiple-choice question contributes NO column of its own and one per
// Option, keyed by the OPTION's identity and headed with its CURRENT label.
//
// This is the assertion a rename must not break: correcting an Option's label
// moves a heading and forks no column, so the Tickets that chose it before the
// correction sit in the same column as the ones that chose it after. A builder
// that keyed columns by label would fail this the first time an Organization
// fixed a typo.
func TestBuildQuestionColumnsFansOutByOptionIdentity(t *testing.T) {
	cols := BuildQuestionColumns([]QuestionColumn{
		{ID: "q-name", Label: "Your name"},
		sizesQuestion(),
	})

	if got, want := cols.Keys(), []string{"q-name", "o-s", "o-m", "o-l"}; !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %v, want %v — the question itself takes no column of its own", got, want)
	}
	if got, want := cols.Headings(), []string{"Your name", "S", "Medium", "L"}; !reflect.DeepEqual(got, want) {
		t.Errorf("headings = %v, want %v — the Option's current label heads its column", got, want)
	}
	for _, key := range cols.Keys() {
		if key == "q-sizes" {
			t.Fatalf("keys contain the multiple-choice question itself: %v", cols.Keys())
		}
	}
}

// A retired Option keeps its column. An Option is retired and never deleted
// precisely so the Tickets that chose it keep reading; the builder does not
// filter its input, and this asserts it never grows the urge to.
func TestBuildQuestionColumnsKeepsRetiredOptions(t *testing.T) {
	q := sizesQuestion()
	q.Options = append(q.Options, OptionColumn{ID: "o-xxl", Label: "XXL (discontinued)"})

	cols := BuildQuestionColumns([]QuestionColumn{q})

	if got, want := cols.Keys(), []string{"o-s", "o-m", "o-l", "o-xxl"}; !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

// An Event that asks nothing produces no columns, and no panic on the way there.
func TestBuildQuestionColumnsEmpty(t *testing.T) {
	cols := BuildQuestionColumns(nil)
	if cols.Len() != 0 {
		t.Fatalf("Len = %d, want 0", cols.Len())
	}
	if len(cols.CellsFor(map[string]Answer{"q-1": {}})) != 0 {
		t.Fatal("cells produced for a question that has no column")
	}
}

// An answered multiple-choice question writes EVERY one of its Option columns:
// TRUE for the chosen, FALSE for the rest. The FALSEs are what make the column
// countable — a pivot over an Option that reads half blanks and half FALSEs
// counts neither.
func TestCellsForWritesTrueAndFalseAcrossEveryOption(t *testing.T) {
	cols := BuildQuestionColumns([]QuestionColumn{sizesQuestion()})

	cells := cols.CellsFor(map[string]Answer{"q-sizes": {Chosen: []string{"o-m"}}})

	want := map[string]bool{"o-s": false, "o-m": true, "o-l": false}
	if len(cells) != len(want) {
		t.Fatalf("got %d cells, want %d: %+v", len(cells), len(want), cells)
	}
	for _, cell := range cells {
		if cell.Value.Checked == nil {
			t.Fatalf("cell %q is not a boolean: %+v", cell.Key, cell.Value)
		}
		if got := *cell.Value.Checked; got != want[cell.Key] {
			t.Errorf("cell %q = %v, want %v", cell.Key, got, want[cell.Key])
		}
	}
}

// An UNANSWERED question produces no cells at all, so its whole block is left
// blank — including every Option column of a multiple-choice one. FALSEs there
// would claim the Ticket read the question and declined every Option, which is a
// different fact from an Outstanding Answer.
func TestCellsForLeavesAnUnansweredQuestionBlank(t *testing.T) {
	cols := BuildQuestionColumns([]QuestionColumn{
		{ID: "q-name", Label: "Your name"},
		sizesQuestion(),
	})

	cells := cols.CellsFor(map[string]Answer{})

	if len(cells) != 0 {
		t.Fatalf("got %d cells for a Ticket that has answered nothing: %+v", len(cells), cells)
	}
}

// An answered multiple-choice question that chose NOTHING still writes its
// Options, all FALSE. Somebody read the question and ticked nothing, and that is
// an Answer rather than a silence.
func TestCellsForAnsweredWithNoChoiceIsAllFalse(t *testing.T) {
	cols := BuildQuestionColumns([]QuestionColumn{sizesQuestion()})

	cells := cols.CellsFor(map[string]Answer{"q-sizes": {}})

	if len(cells) != 3 {
		t.Fatalf("got %d cells, want 3: %+v", len(cells), cells)
	}
	for _, cell := range cells {
		if cell.Value.Checked == nil || *cell.Value.Checked {
			t.Errorf("cell %q = %+v, want FALSE", cell.Key, cell.Value)
		}
	}
}

// The cells come back IN COLUMN ORDER, and a scalar Answer comes back as itself
// — the caller writes it, and does not re-derive which shape it takes.
func TestCellsForIsInColumnOrderAndPassesScalarsThrough(t *testing.T) {
	text := "AA123"
	number := 3.5
	cols := BuildQuestionColumns([]QuestionColumn{
		{ID: "q-flight", Label: "Flight"},
		sizesQuestion(),
		{ID: "q-guests", Label: "Guests"},
	})

	cells := cols.CellsFor(map[string]Answer{
		"q-flight": {Text: &text},
		"q-sizes":  {Chosen: []string{"o-l"}},
		"q-guests": {Number: &number},
	})

	var gotKeys []string
	for _, cell := range cells {
		gotKeys = append(gotKeys, cell.Key)
	}
	wantKeys := []string{"q-flight", "o-s", "o-m", "o-l", "q-guests"}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("cell keys = %v, want %v", gotKeys, wantKeys)
	}
	if cells[0].Value.Text == nil || *cells[0].Value.Text != text {
		t.Errorf("flight cell = %+v, want the Answer's own Text", cells[0].Value)
	}
	if cells[4].Value.Number == nil || *cells[4].Value.Number != number {
		t.Errorf("guests cell = %+v, want the Answer's own Number", cells[4].Value)
	}
}

// Every key the builder produces has a column in the sheet the Sales Export
// builds from it, and every question column of that sheet came from the builder.
// This is the seam the refactor created: the layout and the cells are worked out
// by the same call, and a caller that let them drift apart would write Answers
// into the wrong columns — which cellRef would refuse by name rather than
// silently resolve, but only if the two agree about the keys.
func TestQuestionColumnsAgreeWithTheSalesExportLayout(t *testing.T) {
	questions := []QuestionColumn{{ID: "q-name", Label: "Your name"}, sizesQuestion()}
	cols := answersLayoutFor(Answers{Questions: questions})

	built := BuildQuestionColumns(questions)
	for _, key := range built.Keys() {
		if _, ok := cols.index[key]; !ok {
			t.Errorf("the sheet has no column for builder key %q", key)
		}
	}
	if got, want := len(cols.headers), len(answersFixedColumns)+built.Len(); got != want {
		t.Errorf("sheet width = %d, want %d — the fixed columns plus the builder's", got, want)
	}
}
