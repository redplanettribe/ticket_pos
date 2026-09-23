package exportfile

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// THE SALES EXPORT'S FILE IS LEFT EXACTLY AS IT IS (#655 story 34, ADR 0075).
//
// Routing the per-Ticket sheet through the shared answer rule (#656) must not
// change a single cell of the file a user downloads - not its value, not its
// cell type and not its number format. An E2E diff of the branch against main
// once found two cells that did: a date Answer before 1900 lost its date style
// and moved from an inline string to a shared string, and an empty text Answer
// went from an empty-string cell to no cell at all.
//
// The expectation below was pinned by running parityDump over the same input
// on main (5afd8a0), before the shared rule existed. It is not derived from the
// code under test, so it cannot drift with it.

// parityAnswers is a per-Ticket sheet input that reaches every branch of the
// serializer: every kind of Answer, the two a spreadsheet cannot take
// literally, a multiple-choice fan-out, an Outstanding Answer, and the Holder
// columns in all three states with an empty Holder name on an accepted Ticket.
func parityAnswers() Answers {
	birthday := time.Date(1990, time.March, 4, 0, 0, 0, 0, time.UTC)
	longAgo := time.Date(1850, time.January, 1, 0, 0, 0, 0, time.UTC)
	withClock := time.Date(2001, time.December, 31, 23, 30, 0, 0, time.FixedZone("x", -5*3600))
	return Answers{
		Assignment: true,
		Questions: []QuestionColumn{
			{ID: "q-text", Label: "Notes"},
			{ID: "q-num", Label: "Guests"},
			{ID: "q-date", Label: "Birthday"},
			{ID: "q-check", Label: "Dinner"},
			{ID: "q-multi", Label: "Sizes", Options: []OptionColumn{
				{ID: "o-s", Label: "S"},
				{ID: "o-l", Label: "L"},
			}},
		},
		Tickets: []TicketRow{
			{
				ConfirmationRef: "ABC123", TicketTypeName: "GA",
				AssignmentState: "accepted",
				HolderFirstName: "Carla", HolderLastName: "Ruiz", HolderEmail: "carla@example.com",
				Answers: map[string]Answer{
					"q-text":  {Text: ptr("2")},
					"q-num":   {Number: ptr(3.5)},
					"q-date":  {Date: &birthday},
					"q-check": {Checked: ptr(true)},
					"q-multi": {Chosen: []string{"o-l"}},
				},
			},
			{
				ConfirmationRef: "ABC123", TicketTypeName: "GA",
				AssignmentState: "accepted",
				HolderFirstName: "", HolderLastName: "Solo", HolderEmail: "solo@example.com",
				Answers: map[string]Answer{
					"q-text":  {Text: ptr("")},
					"q-num":   {Number: ptr(0.0)},
					"q-date":  {Date: &longAgo},
					"q-check": {Checked: ptr(false)},
					"q-multi": {Chosen: []string{}},
				},
			},
			{
				ConfirmationRef: "ABC123", TicketTypeName: "GA",
				AssignmentState: "assigned",
				Answers: map[string]Answer{
					"q-text": {Text: ptr("  padded  ")},
					"q-date": {Date: &withClock},
				},
			},
			{
				ConfirmationRef: "ABC123", TicketTypeName: "GA",
				AssignmentState: "unassigned",
			},
		},
	}
}

// sheetEncodings reads a sheet's cells straight out of the file's XML, because
// excelize's reader cannot tell a number from an absent cell, nor an empty
// shared string from an empty inline one. It maps a cell reference to its
// encoding: "n" for a number, or the cell's own `t` attribute ("s" for a
// shared string, "inlineStr", "b", ...). An absent cell has no entry.
func sheetEncodings(t *testing.T, file []byte, sheet string) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	read := func(name string, into any) {
		t.Helper()
		for _, zf := range zr.File {
			if zf.Name != name {
				continue
			}
			rc, err := zf.Open()
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			defer func() { _ = rc.Close() }()
			body, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if err := xml.Unmarshal(body, into); err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			return
		}
		t.Fatalf("no %s in the file", name)
	}

	var workbook struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	read("xl/workbook.xml", &workbook)
	var rels struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	read("xl/_rels/workbook.xml.rels", &rels)

	var target string
	for _, s := range workbook.Sheets {
		if s.Name != sheet {
			continue
		}
		for _, r := range rels.Rels {
			if r.ID == s.RID {
				target = r.Target
			}
		}
	}
	if target == "" {
		t.Fatalf("no sheet %q in the workbook", sheet)
	}
	if strings.HasPrefix(target, "/") {
		target = strings.TrimPrefix(target, "/")
	} else {
		target = path.Join("xl", target)
	}

	var ws struct {
		Rows []struct {
			Cells []struct {
				Ref  string `xml:"r,attr"`
				Type string `xml:"t,attr"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	read(target, &ws)
	out := map[string]string{}
	for _, row := range ws.Rows {
		for _, c := range row.Cells {
			typ := c.Type
			if typ == "" {
				typ = "n"
			}
			out[c.Ref] = typ
		}
	}
	return out
}

// parityDump renders every cell of the per-Ticket sheet, blanks included, as
// "ref|encoding|raw value|number format", one line per cell. An absent cell's
// encoding reads "absent".
func parityDump(t *testing.T, file []byte, rows, cols int) []string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()
	encodings := sheetEncodings(t, file, AnswersSheet)

	var out []string
	for r := 1; r <= rows; r++ {
		for c := 1; c <= cols; c++ {
			ref, err := excelize.CoordinatesToCellName(c, r)
			if err != nil {
				t.Fatalf("cell name: %v", err)
			}
			encoding, ok := encodings[ref]
			if !ok {
				encoding = "absent"
			}
			raw, err := f.GetCellValue(AnswersSheet, ref, excelize.Options{RawCellValue: true})
			if err != nil {
				t.Fatalf("value %s: %v", ref, err)
			}
			styleID, err := f.GetCellStyle(AnswersSheet, ref)
			if err != nil {
				t.Fatalf("style %s: %v", ref, err)
			}
			style, err := f.GetStyle(styleID)
			if err != nil {
				t.Fatalf("style %d: %v", styleID, err)
			}
			numFmt := fmt.Sprint(style.NumFmt)
			if style.CustomNumFmt != nil {
				numFmt = *style.CustomNumFmt
			}
			out = append(out, fmt.Sprintf("%s|%s|%s|%s", ref, encoding, raw, numFmt))
		}
	}
	return out
}

// mainPerTicketSheet is what main (5afd8a0) wrote for parityAnswers, cell by
// cell, as "ref|encoding|raw value|number format". Row 3 carries the two cells
// the shared rule once changed: G3, the empty text Answer, is an empty shared
// string and not an absent cell, and I3, the date before 1900, is an inline
// string that keeps the yyyy-mm-dd style. D3, the accepted Holder's empty first
// name, was already absent on main.
var mainPerTicketSheet = []string{
	// Row 1.
	"A1|s|confirmation_ref|0",
	"B1|s|ticket_type|0",
	"C1|s|assignment_state|0",
	"D1|s|holder_first_name|0",
	"E1|s|holder_last_name|0",
	"F1|s|holder_email|0",
	"G1|s|Notes|0",
	"H1|s|Guests|0",
	"I1|s|Birthday|0",
	"J1|s|Dinner|0",
	"K1|s|S|0",
	"L1|s|L|0",
	// Row 2.
	"A2|s|ABC123|0",
	"B2|s|GA|0",
	"C2|s|accepted|0",
	"D2|s|Carla|0",
	"E2|s|Ruiz|0",
	"F2|s|carla@example.com|0",
	"G2|s|2|0",
	"H2|n|3.5|0",
	"I2|n|32936|yyyy-mm-dd",
	"J2|b|1|0",
	"K2|b|0|0",
	"L2|b|1|0",
	// Row 3.
	"A3|s|ABC123|0",
	"B3|s|GA|0",
	"C3|s|accepted|0",
	"D3|absent||0",
	"E3|s|Solo|0",
	"F3|s|solo@example.com|0",
	"G3|s||0",
	"H3|n|0|0",
	"I3|inlineStr|1850-01-01T00:00:00Z|yyyy-mm-dd",
	"J3|b|0|0",
	"K3|b|0|0",
	"L3|b|0|0",
	// Row 4.
	"A4|s|ABC123|0",
	"B4|s|GA|0",
	"C4|s|assigned|0",
	"D4|absent||0",
	"E4|absent||0",
	"F4|absent||0",
	"G4|s|  padded  |0",
	"H4|absent||0",
	"I4|n|37256|yyyy-mm-dd",
	"J4|absent||0",
	"K4|absent||0",
	"L4|absent||0",
	// Row 5.
	"A5|s|ABC123|0",
	"B5|s|GA|0",
	"C5|s|unassigned|0",
	"D5|absent||0",
	"E5|absent||0",
	"F5|absent||0",
	"G5|absent||0",
	"H5|absent||0",
	"I5|absent||0",
	"J5|absent||0",
	"K5|absent||0",
	"L5|absent||0",
}

func TestSalesExportPerTicketSheetIsCellIdenticalToMain(t *testing.T) {
	answers := parityAnswers()
	file, err := Build(oneSale(), gaColumn(), answers, time.UTC, Info{
		EventName:   "Answer Fest",
		GeneratedAt: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		Currency:    "USD",
		Filters:     Filters{Status: "active"},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// 2 fixed columns, 4 Holder columns, 4 questions of which one fans out to
	// two Options.
	got := parityDump(t, file, len(answers.Tickets)+1, 12)
	if len(got) != len(mainPerTicketSheet) {
		t.Fatalf("%d cells, main wrote %d", len(got), len(mainPerTicketSheet))
	}
	for i := range got {
		if got[i] != mainPerTicketSheet[i] {
			t.Errorf("cell differs from main:\n got %s\nmain %s", got[i], mainPerTicketSheet[i])
		}
	}
}
