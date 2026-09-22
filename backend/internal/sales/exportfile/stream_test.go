package exportfile

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// The streaming workbook writer's own contract (#657, ADR 0075).
//
// EVERY ASSERTION READS THE OUTPUT BACK THROUGH EXCELIZE AND NOTHING ELSE. The
// writer is ours and excelize is not, so excelize is the independent reader: a
// file it cannot open, or opens differently from what was declared, is a file a
// spreadsheet program would get wrong too. Nothing here looks at the writer's
// internals.

// streamLayout is a small data sheet with one column of every kind a cell can
// take.
func streamLayout() SheetLayout {
	return SheetLayout{
		Name:     "Ticket Holders",
		Headings: []string{"ref", "sold_at", "ordinal", "birthday", "attending", "notes"},
		Width:    22,
	}
}

// writeStream begins a workbook onto a buffer, appends the rows and finishes it.
func writeStream(t *testing.T, layout SheetLayout, rows [][]Cell, info []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	wb, err := BeginStream(&buf, layout)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for _, row := range rows {
		if err := wb.Append(row); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := wb.Finish(info); err != nil {
		t.Fatalf("finish: %v", err)
	}
	return buf.Bytes()
}

func openStream(t *testing.T, data []byte) *excelize.File {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("excelize cannot open the streamed workbook: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// THE INFO SHEET OPENS FIRST THOUGH IT IS WRITTEN LAST: sheet order is the
// workbook part's, not the zip's, and the Info sheet is the active one.
func TestStreamInfoSheetOpensFirstThoughWrittenLast(t *testing.T) {
	data := writeStream(t, streamLayout(), [][]Cell{{{Kind: CellText, Text: "ABC123"}}},
		[]string{"Holder Export", "", "Rows: 1 Ticket"})
	f := openStream(t, data)

	if got, want := f.GetSheetList(), []string{InfoSheet, "Ticket Holders"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sheets = %v, want %v", got, want)
	}
	if got := f.GetSheetName(f.GetActiveSheetIndex()); got != InfoSheet {
		t.Fatalf("active sheet = %q, want %q", got, InfoSheet)
	}
	rows, err := f.GetRows(InfoSheet)
	if err != nil {
		t.Fatalf("info rows: %v", err)
	}
	if len(rows) != 3 || rows[0][0] != "Holder Export" || len(rows[1]) != 0 || rows[2][0] != "Rows: 1 Ticket" {
		t.Fatalf("info rows = %q, want the three lines with the blank one blank", rows)
	}
	width, err := f.GetColWidth(InfoSheet, "A")
	if err != nil || width != 110 {
		t.Fatalf("info column width = %v (%v), want 110", width, err)
	}
}

// EVERY KIND ARRIVES TYPED: text as text, a number as a number, a moment and a
// calendar date as real date cells with their own formats, a boolean as a
// boolean, and a blank as no cell at all.
func TestStreamCellsArriveTyped(t *testing.T) {
	quito := time.FixedZone("ECT", -5*60*60)
	soldAt := time.Date(2026, time.July, 1, 9, 30, 0, 0, quito)
	birthday := time.Date(1990, time.March, 4, 0, 0, 0, 0, time.UTC)

	data := writeStream(t, streamLayout(), [][]Cell{{
		{Kind: CellText, Text: "007"},
		{Kind: CellMoment, Date: soldAt},
		{Kind: CellNumber, Number: 3},
		{Kind: CellDate, Date: birthday},
		{Kind: CellBool, Bool: false},
		{Kind: CellBlank},
	}}, []string{"Info"})
	f := openStream(t, data)
	const sheet = "Ticket Holders"

	header, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if got := fmt.Sprint(header[0]); got != fmt.Sprint(streamLayout().Headings) {
		t.Fatalf("header = %v, want %v", header[0], streamLayout().Headings)
	}

	for _, tc := range []struct {
		cell, rendered, raw string
		kind                excelize.CellType
	}{
		// Text that looks like a number stays text.
		{"A2", "007", "007", excelize.CellTypeInlineString},
		// The moment's own wall clock, as the Event's clock was put on it.
		{"B2", "2026-07-01 09:30", "", excelize.CellTypeUnset},
		{"C2", "3", "3", excelize.CellTypeUnset},
		// 32936 is 1990-03-04 as an Excel serial: a whole number, no time.
		{"D2", "1990-03-04", "32936", excelize.CellTypeUnset},
		{"E2", "FALSE", "0", excelize.CellTypeBool},
	} {
		if got, _ := f.GetCellValue(sheet, tc.cell); got != tc.rendered {
			t.Errorf("%s renders %q, want %q", tc.cell, got, tc.rendered)
		}
		if tc.raw != "" {
			if got, _ := f.GetCellValue(sheet, tc.cell, excelize.Options{RawCellValue: true}); got != tc.raw {
				t.Errorf("%s stored as %q, want %q", tc.cell, got, tc.raw)
			}
		}
		if got, _ := f.GetCellType(sheet, tc.cell); got != tc.kind {
			t.Errorf("%s type = %v, want %v", tc.cell, got, tc.kind)
		}
	}
	// A blank is absent, not an empty string.
	if got, _ := f.GetCellValue(sheet, "F2"); got != "" {
		t.Errorf("blank cell reads %q, want nothing", got)
	}
	if len(header[1]) != 5 {
		t.Errorf("row 2 has %d cells, want the blank last one absent", len(header[1]))
	}
}

// Text is escaped, not interpreted: markup, ampersands and leading spaces come
// back exactly as they went in.
func TestStreamTextIsEscapedAndPreserved(t *testing.T) {
	text := `  <b>Ana & "Bo"</b> ` + "\nline two"
	data := writeStream(t, streamLayout(), [][]Cell{{{Kind: CellText, Text: text}}}, []string{"A & B <c>"})
	f := openStream(t, data)
	if got, _ := f.GetCellValue("Ticket Holders", "A2"); got != text {
		t.Fatalf("text = %q, want %q", got, text)
	}
	if got, _ := f.GetCellValue(InfoSheet, "A1"); got != "A & B <c>" {
		t.Fatalf("info text = %q", got)
	}
}

// The declared width spans every declared column, and the data sheet carries
// the declared name.
func TestStreamColumnWidthsAndSheetNameAreAsDeclared(t *testing.T) {
	f := openStream(t, writeStream(t, streamLayout(), nil, []string{"Info"}))
	for _, col := range []string{"A", "F"} {
		if width, err := f.GetColWidth("Ticket Holders", col); err != nil || width != 22 {
			t.Errorf("column %s width = %v (%v), want 22", col, width, err)
		}
	}
	if width, _ := f.GetColWidth("Ticket Holders", "G"); width == 22 {
		t.Errorf("column G is past the declared columns and must keep the default width")
	}
}

// A WORKBOOK ABANDONED BEFORE FINISHING CANNOT BE OPENED. It has no zip
// central directory, which is the property that lets a failed export never
// become a short, well-formed file.
func TestStreamAbandonedWorkbookCannotBeOpened(t *testing.T) {
	var buf bytes.Buffer
	wb, err := BeginStream(&buf, streamLayout())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for i := 0; i < 5000; i++ {
		if err := wb.Append([]Cell{{Kind: CellText, Text: fmt.Sprintf("row %d", i)}}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if buf.Len() == 0 {
		t.Fatal("nothing was streamed before finishing; the test cannot tell abandonment from buffering")
	}
	if f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes())); err == nil {
		_ = f.Close()
		t.Fatal("an unfinished workbook opened; a cut download could be saved as a short roster")
	}
}

// A row wider than the declared columns is refused rather than written into
// columns nobody headed.
func TestStreamRefusesARowWiderThanItsColumns(t *testing.T) {
	wb, err := BeginStream(io.Discard, SheetLayout{Name: "S", Headings: []string{"a"}, Width: 22})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := wb.Append([]Cell{{Kind: CellText, Text: "x"}, {Kind: CellText, Text: "y"}}); err == nil {
		t.Fatal("a two-cell row was accepted onto a one-column sheet")
	}
}

// Rows reports what was appended, which is what the Info sheet states.
func TestStreamCountsTheRowsAppended(t *testing.T) {
	wb, err := BeginStream(io.Discard, streamLayout())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for i := 0; i < 3; i++ {
		_ = wb.Append([]Cell{{Kind: CellText, Text: "x"}})
	}
	if wb.Rows() != 3 {
		t.Fatalf("Rows = %d, want 3", wb.Rows())
	}
}

// MEMORY HOLDS FLAT IN THE ROW COUNT. The live heap while the workbook is open
// is measured after a small roster and after one ten times its size; a writer
// that held rows, or the sheet XML, would grow with them.
func TestStreamHoldsMemoryFlatInTheRowCount(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a hundred thousand wide rows")
	}
	small := liveHeapAfterStreaming(t, 10_000)
	large := liveHeapAfterStreaming(t, 100_000)
	t.Logf("live heap with the workbook open: %.1f MiB at 10,000 rows, %.1f MiB at 100,000 rows",
		float64(small)/(1<<20), float64(large)/(1<<20))
	if large > small+(4<<20) {
		t.Fatalf("live heap grew from %d to %d bytes over ten times the rows; the writer is holding them", small, large)
	}
}

// BenchmarkStream writes a wide roster to nowhere and reports the live heap the
// open workbook holds, which is the number the flat-memory claim is about.
//
//	go test ./internal/sales/exportfile/ -run XXX -bench BenchmarkStream -benchtime 1x
func BenchmarkStream(b *testing.B) {
	for _, rows := range []int{10_000, 100_000} {
		b.Run(fmt.Sprintf("rows=%d", rows), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.ReportMetric(float64(liveHeapAfterStreaming(b, rows))/(1<<20), "live-MiB")
			}
		})
	}
}

// liveHeapAfterStreaming streams rows of a 200-column sheet to io.Discard and
// returns the heap still live, after a collection, while the workbook is open.
func liveHeapAfterStreaming(tb testing.TB, rows int) uint64 {
	tb.Helper()
	const width = 200
	headings := make([]string, width)
	for i := range headings {
		headings[i] = fmt.Sprintf("column %d", i)
	}
	wb, err := BeginStream(io.Discard, SheetLayout{Name: "S", Headings: headings, Width: 22})
	if err != nil {
		tb.Fatalf("begin: %v", err)
	}
	row := make([]Cell, width)
	for i := 0; i < rows; i++ {
		row[0] = Cell{Kind: CellText, Text: fmt.Sprintf("ref-%d", i)}
		row[1] = Cell{Kind: CellNumber, Number: float64(i)}
		for c := 2; c < width; c++ {
			row[c] = Cell{Kind: CellBool, Bool: (i+c)%3 == 0}
		}
		if err := wb.Append(row); err != nil {
			tb.Fatalf("append: %v", err)
		}
	}
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	if err := wb.Finish([]string{"Info"}); err != nil {
		tb.Fatalf("finish: %v", err)
	}
	return stats.HeapAlloc
}

// TEXT IS CUT WHERE EXCEL CUTS IT, which is 32,767 UTF-16 code units and not
// 32,767 characters: a character outside the Basic Multilingual Plane - most
// emoji, some CJK - is two units to Excel and one rune to Go, so a rune count
// lets through a cell Excel refuses. The cut never splits a surrogate pair,
// because half of one is not a character at all.
func TestStreamCutsTextAtExcelsLimitInUTF16Units(t *testing.T) {
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
		data := writeStream(t, streamLayout(), [][]Cell{{{Kind: CellText, Text: tc.text}}}, []string{"Info"})
		got, err := openStream(t, data).GetCellValue("Ticket Holders", "A2")
		if err != nil {
			t.Fatalf("%s: read: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: cell holds %d UTF-16 units (%d runes), want %d units (%d runes)", tc.name,
				len(utf16.Encode([]rune(got))), utf8.RuneCountInString(got),
				len(utf16.Encode([]rune(tc.want))), utf8.RuneCountInString(tc.want))
		}
	}
}

// AN EMPTY STRING IS AN EMPTY CELL. A text value with nothing in it - an
// Answer somebody cleared - is written as no cell at all, exactly as a
// CellBlank is, so a reader filtering on "is blank" finds it.
func TestStreamWritesEmptyTextAsNoCell(t *testing.T) {
	data := writeStream(t, streamLayout(), [][]Cell{{
		{Kind: CellText, Text: ""},
		{Kind: CellText, Text: "x"},
	}}, []string{"Info"})
	f := openStream(t, data)
	if got, _ := f.GetCellType("Ticket Holders", "A2"); got != excelize.CellTypeUnset {
		t.Fatalf("an empty text value was written as a cell of type %v, want no cell", got)
	}
	if got, _ := f.GetCellValue("Ticket Holders", "B2"); got != "x" {
		t.Fatalf("the cell after it reads %q, want %q", got, "x")
	}
}

// EVERY PART CARRIES THE WORKBOOK'S OWN TIME, not the zip format's zero date,
// so an archive tool listing the file shows when it was generated.
func TestStreamStampsEveryPartWithItsModifiedTime(t *testing.T) {
	generatedAt := time.Date(2026, time.July, 1, 9, 30, 0, 0, time.FixedZone("ECT", -5*60*60))
	layout := streamLayout()
	layout.Modified = generatedAt
	data := writeStream(t, layout, [][]Cell{{{Kind: CellText, Text: "x"}}}, []string{"Info"})

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	if len(archive.File) == 0 {
		t.Fatal("the workbook has no parts")
	}
	for _, part := range archive.File {
		if !part.Modified.Equal(generatedAt) {
			t.Errorf("%s modified %v, want %v", part.Name, part.Modified, generatedAt)
		}
	}
}
