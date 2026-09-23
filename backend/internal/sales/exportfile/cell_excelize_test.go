package exportfile

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/xuri/excelize/v2"
)

// THE CUT CHANGES NO FILE (ADR 0075): excelize's own SetCellStr already cut text
// to Excel's limit before the shared rule existed, and the Sales Export still
// writes through it. This pins that the two cut the same text to the same
// value, astral characters and surrogate pairs included, so moving the cut into
// TextCell changed nothing a user downloads. If an excelize upgrade ever cuts
// differently, this fails and the ADR's claim has to be revisited.
func TestTextCellCutsExactlyAsExcelizeDoes(t *testing.T) {
	const astral = "\U0001F600" // one rune, two UTF-16 code units
	for _, tc := range []struct {
		name, text string
	}{
		{"astral characters only", strings.Repeat(astral, 20_000)},
		{"a pair that would straddle the limit", strings.Repeat("a", 32_766) + astral},
		{"a pair that ends exactly on the limit", strings.Repeat("a", 32_765) + astral + "b"},
		{"astral characters after an odd prefix", "x" + strings.Repeat(astral, 20_000)},
		{"text inside the Basic Multilingual Plane", strings.Repeat("é", 40_000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := TextCell(tc.text)
			if rule.Kind != CellText {
				t.Fatalf("TextCell kind = %v, want text", rule.Kind)
			}

			f := excelize.NewFile()
			defer func() { _ = f.Close() }()
			sheet := f.GetSheetName(0)
			if err := f.SetCellStr(sheet, "A1", tc.text); err != nil {
				t.Fatalf("SetCellStr: %v", err)
			}
			buf, err := f.WriteToBuffer()
			if err != nil {
				t.Fatalf("WriteToBuffer: %v", err)
			}
			read, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("OpenReader: %v", err)
			}
			defer func() { _ = read.Close() }()
			got, err := read.GetCellValue(sheet, "A1")
			if err != nil {
				t.Fatalf("GetCellValue: %v", err)
			}

			if got != rule.Text {
				t.Errorf("excelize kept %d UTF-16 units, the shared rule %d; want identical text",
					len(utf16.Encode([]rune(got))), len(utf16.Encode([]rune(rule.Text))))
			}
			if got == tc.text {
				t.Errorf("excelize did not cut a %d-unit text at all", len(utf16.Encode([]rune(tc.text))))
			}
		})
	}
}
