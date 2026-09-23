package exportfile

import (
	"archive/zip"
	"bytes"
	"encoding/json"
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
// change a single cell of the file a user downloads, on any of its sheets - not
// its value, not its cell type and not its number format. An E2E diff of the
// #655 branch against the Sales Export before #655 once found two cells that
// did: a date Answer before 1900 lost its date style and moved from an inline
// string to a shared string, and an empty text Answer went from an empty-string
// cell to no cell at all.
//
// "Exactly as it is" covers the file's layout too, not only its cells: which
// sheet opens active and selected, each sheet's view, frozen or split panes,
// merged ranges, column widths and row heights, and every cell's alignment
// (wrap included), font, fill and border.
//
// The expectation, salesExportBefore655, was pinned by running this test file
// on a `git archive` of 5afd8a0, the last commit before #655. It is not derived
// from the code under test, so it cannot drift with it.

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

// sheetXML is what workbookDump reads of a sheet straight out of the file's
// XML, because excelize's reader cannot tell a number from an absent cell, nor
// an empty shared string from an empty inline one, and has no call that lists
// every column width or row height a sheet sets.
type sheetXML struct {
	// encodings maps a cell reference to its encoding: "n" for a number, or
	// the cell's own `t` attribute ("s" for a shared string, "inlineStr",
	// "b", ...). An absent cell has no entry.
	encodings map[string]string
	// cols is every <col> the sheet declares, as "min-max|width|customWidth".
	cols []string
	// rowHeights is every row that sets a height, as "row|ht|customHeight".
	rowHeights []string
	// views is every <sheetView>'s tab selection and cell selections, as
	// "tabSelected|pane activeCell sqref|...", which excelize's GetSheetView
	// does not report.
	views []string
}

func readSheetXML(t *testing.T, file []byte, sheet string) sheetXML {
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
		Views []struct {
			TabSelected string `xml:"tabSelected,attr"`
			Selections  []struct {
				Pane       string `xml:"pane,attr"`
				ActiveCell string `xml:"activeCell,attr"`
				Sqref      string `xml:"sqref,attr"`
			} `xml:"selection"`
		} `xml:"sheetViews>sheetView"`
		Cols []struct {
			Min         string `xml:"min,attr"`
			Max         string `xml:"max,attr"`
			Width       string `xml:"width,attr"`
			CustomWidth string `xml:"customWidth,attr"`
		} `xml:"cols>col"`
		Rows []struct {
			Ref          string `xml:"r,attr"`
			Height       string `xml:"ht,attr"`
			CustomHeight string `xml:"customHeight,attr"`
			Cells        []struct {
				Ref  string `xml:"r,attr"`
				Type string `xml:"t,attr"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	read(target, &ws)
	out := sheetXML{encodings: map[string]string{}}
	for _, view := range ws.Views {
		line := view.TabSelected
		for _, sel := range view.Selections {
			line += fmt.Sprintf("|%s %s %s", sel.Pane, sel.ActiveCell, sel.Sqref)
		}
		out.views = append(out.views, line)
	}
	for _, col := range ws.Cols {
		out.cols = append(out.cols, fmt.Sprintf("%s-%s|%s|%s", col.Min, col.Max, col.Width, col.CustomWidth))
	}
	for _, row := range ws.Rows {
		if row.Height != "" {
			out.rowHeights = append(out.rowHeights, fmt.Sprintf("%s|%s|%s", row.Ref, row.Height, row.CustomHeight))
		}
		for _, c := range row.Cells {
			typ := c.Type
			if typ == "" {
				typ = "n"
			}
			out.encodings[c.Ref] = typ
		}
	}
	return out
}

// withoutZeros is a decoded JSON tree with every false, 0, "", null, and
// empty object or array dropped from its objects and arrays.
func withoutZeros(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, member := range v {
			if kept := withoutZeros(member); kept != nil {
				out[k] = kept
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		var out []any
		for _, member := range v {
			if kept := withoutZeros(member); kept != nil {
				out = append(out, kept)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case bool:
		if !v {
			return nil
		}
	case float64:
		if v == 0 {
			return nil
		}
	case string:
		if v == "" {
			return nil
		}
	}
	return v
}

// workbookDump renders the whole workbook, its layout as well as its cells:
// first its sheets' names in order and the active sheet, then for each sheet
// its view (which carries whether its tab is selected), its panes (frozen or
// split), its merged ranges, every column width and row height it sets, and
// every cell, blanks included, as
// "sheet!ref|encoding|raw value|number format|look", one line per cell. The
// look is the cell's alignment (wrap included), font, fill and border as JSON.
// Each sheet is read over the smallest rectangle holding every cell it wrote,
// so an absent cell inside it reads "absent". A shared string is dumped as its
// text, so the order of the shared-string table does not count.
func workbookDump(t *testing.T, file []byte) []string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()

	// asJSON renders v as JSON with every zero value left out, keys sorted,
	// so a line names only what is set.
	asJSON := func(v any) string {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json: %v", err)
		}
		var tree any
		if err := json.Unmarshal(b, &tree); err != nil {
			t.Fatalf("json: %v", err)
		}
		if b, err = json.Marshal(withoutZeros(tree)); err != nil {
			t.Fatalf("json: %v", err)
		}
		return string(b)
	}

	sheets := f.GetSheetList()
	out := []string{
		"sheets|" + strings.Join(sheets, "|"),
		"active|" + f.GetSheetName(f.GetActiveSheetIndex()),
	}
	for _, sheet := range sheets {
		view, err := f.GetSheetView(sheet, 0)
		if err != nil {
			t.Fatalf("view %s: %v", sheet, err)
		}
		out = append(out, sheet+"!view|"+asJSON(view))
		panes, err := f.GetPanes(sheet)
		if err != nil {
			t.Fatalf("panes %s: %v", sheet, err)
		}
		out = append(out, sheet+"!panes|"+asJSON(panes))
		merged, err := f.GetMergeCells(sheet)
		if err != nil {
			t.Fatalf("merged cells %s: %v", sheet, err)
		}
		for _, m := range merged {
			out = append(out, sheet+"!merged|"+m.GetStartAxis()+":"+m.GetEndAxis())
		}

		xmlSheet := readSheetXML(t, file, sheet)
		for _, col := range xmlSheet.cols {
			out = append(out, sheet+"!col|"+col)
		}
		for _, view := range xmlSheet.views {
			out = append(out, sheet+"!selected|"+view)
		}
		for _, row := range xmlSheet.rowHeights {
			out = append(out, sheet+"!row|"+row)
		}

		var rows, cols int
		for ref := range xmlSheet.encodings {
			c, r, err := excelize.CellNameToCoordinates(ref)
			if err != nil {
				t.Fatalf("%s!%s: %v", sheet, ref, err)
			}
			rows, cols = max(rows, r), max(cols, c)
		}
		for r := 1; r <= rows; r++ {
			for c := 1; c <= cols; c++ {
				ref, err := excelize.CoordinatesToCellName(c, r)
				if err != nil {
					t.Fatalf("cell name: %v", err)
				}
				encoding, ok := xmlSheet.encodings[ref]
				if !ok {
					encoding = "absent"
				}
				raw, err := f.GetCellValue(sheet, ref, excelize.Options{RawCellValue: true})
				if err != nil {
					t.Fatalf("value %s!%s: %v", sheet, ref, err)
				}
				styleID, err := f.GetCellStyle(sheet, ref)
				if err != nil {
					t.Fatalf("style %s!%s: %v", sheet, ref, err)
				}
				style, err := f.GetStyle(styleID)
				if err != nil {
					t.Fatalf("style %d: %v", styleID, err)
				}
				numFmt := fmt.Sprint(style.NumFmt)
				if style.CustomNumFmt != nil {
					numFmt = *style.CustomNumFmt
				}
				look := asJSON(struct {
					Alignment *excelize.Alignment
					Font      *excelize.Font
					Fill      excelize.Fill
					Border    []excelize.Border
				}{style.Alignment, style.Font, style.Fill, style.Border})
				out = append(out, fmt.Sprintf("%s!%s|%s|%s|%s|%s", sheet, ref, encoding, raw, numFmt, look))
			}
		}
	}
	return out
}

// buildParityWorkbook is the Sales Export of parityAnswers, as the handler
// builds it. The Info sheet's "Generated" stamp is this fixed clock.
func buildParityWorkbook(t *testing.T) []byte {
	t.Helper()
	file, err := Build(oneSale(), gaColumn(), parityAnswers(), time.UTC, Info{
		EventName:   "Answer Fest",
		GeneratedAt: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		Currency:    "USD",
		Filters:     Filters{Status: "active"},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return file
}

func TestSalesExportIsIdenticalToTheSalesExportBefore655(t *testing.T) {
	got := workbookDump(t, buildParityWorkbook(t))
	if len(got) != len(salesExportBefore655) {
		t.Errorf("%d lines, the Sales Export before #655 wrote %d", len(got), len(salesExportBefore655))
	}
	for i := range min(len(got), len(salesExportBefore655)) {
		if got[i] != salesExportBefore655[i] {
			t.Errorf("line differs from the Sales Export before #655:\n   got %s\nbefore %s", got[i], salesExportBefore655[i])
		}
	}
}

// salesExportBefore655 is workbookDump of buildParityWorkbook as the Sales
// Export before #655 (5afd8a0) wrote it: this same test file, run on a
// `git archive 5afd8a0` of the backend. It is not derived from the code under
// test, so it cannot drift with it.
//
// On the per-Ticket sheet, row 3 carries the two cells the shared rule once
// changed: G3, the empty text Answer, is an empty shared string and not an
// absent cell, and I3, the date before 1900, is an inline string that keeps the
// yyyy-mm-dd style. D3, the accepted Holder's empty first name, was already
// absent before #655. The Info sheet's own copy holds em dashes, written here
// as the Go escape for U+2014.
var salesExportBefore655 = []string{
	"sheets|Info|Ticket Sales|Ticket Answers",
	"active|Info",
	"Info!view|{\"DefaultGridColor\":true,\"ShowGridLines\":true,\"ShowRowColHeaders\":true,\"ShowRuler\":true,\"ShowZeros\":true,\"View\":\"normal\",\"ZoomScale\":100}",
	"Info!panes|null",
	"Info!col|1-1|110|true",
	"Info!selected|true",
	"Info!A1|s|Sales Export|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A2|n||0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A3|s|Event: Answer Fest|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A4|s|Generated: 2026-08-21 12:00|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A5|s|Times shown in UTC (the Event's timezone).|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A6|s|Rows: 1 Ticket Sale|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A7|s|Currency: USD|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A8|n||0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A9|s|Filters applied|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A10|s|Reversed sales are excluded. This file lists active Ticket Sales only.|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A11|s|No other filters were applied: this is every active Ticket Sale on the Event.|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A12|n||0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A13|s|This file reflects the filters that were on screen when it was downloaded, so two downloads of the same Event can differ. The Ticket Type columns are the Event's catalog as it stood at the moment above.|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A14|n||0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A15|s|Each row's origin says how the sale reached the platform: sale_import \u2014 it arrived in an uploaded Sale Import batch; manually_recorded \u2014 somebody typed it in here as a single sale, in no batch; correction_replacement \u2014 it stands in for a sale that was corrected, and the corrects column names that sale; channel_sale \u2014 it was sold on the platform itself and not imported at all.|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A16|n||0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A17|s|The Ticket Answers sheet lists one row per ticket, for the same sales as this file's other sheet, with one column per Ticket Question \u2014 and one TRUE/FALSE column per option where a question takes several. Join it back on confirmation_ref. A blank cell there is a question that ticket has not answered, or was never asked because it belongs to another Ticket Type.|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A18|n||0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Info!A19|s|That sheet also names each ticket's holder: its assignment_state \u2014 unassigned, assigned or accepted \u2014 and, for a ticket whose holder has accepted, their name and email address. An address a buyer entered that its owner has not accepted is not shown here: it is theirs to disclose, not ours, and it is deleted when the event starts.|0|{\"Alignment\":{\"Vertical\":\"top\",\"WrapText\":true},\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!view|{\"DefaultGridColor\":true,\"ShowGridLines\":true,\"ShowRowColHeaders\":true,\"ShowRuler\":true,\"ShowZeros\":true,\"View\":\"normal\",\"ZoomScale\":100}",
	"Ticket Sales!panes|null",
	"Ticket Sales!col|1-21|22|true",
	"Ticket Sales!selected|",
	"Ticket Sales!A1|s|confirmation_ref|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!B1|s|sold_at|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!C1|s|customer_first_name|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!D1|s|customer_last_name|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!E1|s|customer_email|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!F1|s|tax_id_type|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!G1|s|tax_id_number|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!H1|s|GA|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!I1|s|total_quantity|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!J1|s|amount|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!K1|s|net_proceeds|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!L1|s|currency|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!M1|s|channel|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!N1|s|source|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!O1|s|origin|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!P1|s|payment_method|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!Q1|s|status|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!R1|s|reversed_at|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!S1|s|reversed_by|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!T1|s|corrected_by|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!U1|s|corrects|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!A2|s|ABC123|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!B2|n|46204.416666666664|yyyy-mm-dd hh:mm|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!C2|s|Ana|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!D2|s|Lopez|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!E2|s|ana@example.com|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!F2|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!G2|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!H2|n|1|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!I2|n|1|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!J2|n|25.00|0.00|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!K2|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!L2|s|USD|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!M2|s|import|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!N2|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!O2|s|sale_import|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!P2|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!Q2|s|active|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!R2|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!S2|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!T2|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Sales!U2|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!view|{\"DefaultGridColor\":true,\"ShowGridLines\":true,\"ShowRowColHeaders\":true,\"ShowRuler\":true,\"ShowZeros\":true,\"View\":\"normal\",\"ZoomScale\":100}",
	"Ticket Answers!panes|null",
	"Ticket Answers!col|1-12|22|true",
	"Ticket Answers!selected|",
	"Ticket Answers!A1|s|confirmation_ref|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!B1|s|ticket_type|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!C1|s|assignment_state|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!D1|s|holder_first_name|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!E1|s|holder_last_name|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!F1|s|holder_email|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!G1|s|Notes|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!H1|s|Guests|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!I1|s|Birthday|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!J1|s|Dinner|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!K1|s|S|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!L1|s|L|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!A2|s|ABC123|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!B2|s|GA|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!C2|s|accepted|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!D2|s|Carla|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!E2|s|Ruiz|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!F2|s|carla@example.com|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!G2|s|2|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!H2|n|3.5|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!I2|n|32936|yyyy-mm-dd|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!J2|b|1|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!K2|b|0|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!L2|b|1|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!A3|s|ABC123|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!B3|s|GA|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!C3|s|accepted|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!D3|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!E3|s|Solo|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!F3|s|solo@example.com|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!G3|s||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!H3|n|0|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!I3|inlineStr|1850-01-01T00:00:00Z|yyyy-mm-dd|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!J3|b|0|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!K3|b|0|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!L3|b|0|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!A4|s|ABC123|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!B4|s|GA|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!C4|s|assigned|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!D4|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!E4|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!F4|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!G4|s|  padded  |0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!H4|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!I4|n|37256|yyyy-mm-dd|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!J4|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!K4|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!L4|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!A5|s|ABC123|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!B5|s|GA|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!C5|s|unassigned|0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!D5|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!E5|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!F5|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!G5|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!H5|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!I5|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!J5|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!K5|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
	"Ticket Answers!L5|absent||0|{\"Fill\":{\"Type\":\"pattern\"},\"Font\":{\"ColorTheme\":1,\"Family\":\"Calibri\",\"Size\":11}}",
}
