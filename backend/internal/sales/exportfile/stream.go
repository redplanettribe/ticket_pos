package exportfile

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// A STREAMING WORKBOOK WRITER of our own (#657, ADR 0075): one data sheet
// written row by row straight into an output stream, then an Info sheet.
//
// IT IS NOT EXCELIZE, and the reason is the platform rather than the library.
// excelize holds a whole workbook in memory at roughly 600 B a cell (#530), and
// its StreamWriter spills sheet XML to a temp file past 16 MiB - which on Cloud
// Run is memory by another name, because the filesystem is in memory. An .xlsx
// is a zip of a few XML parts, and writing them directly is the only way the
// bytes reach the response without a buffer the size of the file.
//
// THE INTERFACE IS FOUR CALLS: BeginStream declares the data sheet, Append adds
// one typed row, Rows says how many went in, and Finish writes the Info sheet
// and closes the zip. Everything an .xlsx needs besides - content types,
// relationships, styles, the shared strings the Info sheet uses - is decided in
// here and nowhere else.
//
// THE PARTS ARE WRITTEN IN STREAMING ORDER. The workbook part, which fixes sheet
// order, goes out first and names the Info sheet first and active; the data
// sheet follows, deflated as its rows arrive; the Info sheet is written LAST, so
// the row count it states is the rows actually written, and still opens first.
//
// AN UNFINISHED WORKBOOK CANNOT BE OPENED. A zip's central directory is written
// by Finish alone, so a stream abandoned part way - a database failure, a client
// gone, a deadline - is bytes no spreadsheet program will open, and never a
// well-formed roster quietly missing its last people.
//
// Data sheet text is written as INLINE strings, because a shared string table
// has to be complete before the sheet that points into it can be read, and
// holding one is holding every distinct value of the roster. The Info sheet's
// few lines use the shared table, as excelize writes them.

// SheetLayout declares a streamed workbook's data sheet: its name, its header
// row, and the width of every column the header spans.
type SheetLayout struct {
	Name     string
	Headings []string
	Width    float64
}

// Stream is a workbook being written. It is not safe for concurrent use.
type Stream struct {
	zip *zip.Writer
	// sheet buffers the data sheet's XML in front of the zip entry's deflater,
	// so a row is a few large writes to it rather than dozens of tiny ones.
	sheet   *bufio.Writer
	columns []string
	rows    int
	// err is sticky: once a write has failed, every later call returns it and
	// nothing more is written, so Finish can never close a zip over a sheet
	// that stopped part way.
	err      error
	finished bool
}

// maxCellChars is the most characters a cell may hold; longer text is cut to
// it, as excelize cuts it, rather than written into a file Excel refuses.
const maxCellChars = 32767

// The styles every streamed workbook carries, by index into cellXfs; index 0
// is the default a cell with no s attribute takes.
const (
	styleMoment  = 1
	styleDate    = 2
	styleWrapped = 3
)

// BeginStream writes the workbook's fixed parts and the data sheet's header onto
// w, and returns the Stream its rows are appended to.
func BeginStream(w io.Writer, layout SheetLayout) (*Stream, error) {
	if len(layout.Headings) == 0 {
		return nil, errors.New("exportfile: a streamed sheet needs at least one column")
	}
	s := &Stream{zip: zip.NewWriter(w), columns: make([]string, len(layout.Headings))}
	for i := range layout.Headings {
		s.columns[i] = columnName(i + 1)
	}

	for _, part := range []struct{ name, body string }{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", rootRelsXML},
		{"xl/workbook.xml", workbookXML(layout.Name)},
		{"xl/_rels/workbook.xml.rels", workbookRelsXML},
		{"xl/styles.xml", stylesXML},
	} {
		if err := s.writePart(part.name, part.body); err != nil {
			return nil, err
		}
	}

	entry, err := s.zip.Create(dataSheetPart)
	if err != nil {
		return nil, err
	}
	s.sheet = bufio.NewWriterSize(entry, 64<<10)
	s.put(xml.Header)
	s.put(`<worksheet xmlns="` + mainNS + `"><cols><col min="1" max="` +
		strconv.Itoa(len(s.columns)) + `" width="` + formatNumber(layout.Width) +
		`" customWidth="1"/></cols><sheetData>`)
	header := make([]Cell, len(layout.Headings))
	for i, h := range layout.Headings {
		header[i] = Cell{Kind: CellText, Text: h}
	}
	s.row(1, header)
	return s, s.err
}

// Append writes one row under the header. Cells are the row's columns left to
// right; a row may be shorter than the header, and a CellBlank writes nothing,
// so a trailing or inner blank is a genuinely empty cell.
func (s *Stream) Append(cells []Cell) error {
	if s.err != nil {
		return s.err
	}
	if s.finished {
		return errors.New("exportfile: append to a finished workbook")
	}
	if len(cells) > len(s.columns) {
		return fmt.Errorf("exportfile: a row of %d cells on a sheet of %d columns", len(cells), len(s.columns))
	}
	s.rows++
	s.row(s.rows+1, cells) // the header is row 1
	return s.err
}

// Rows is how many rows have been appended under the header.
func (s *Stream) Rows() int { return s.rows }

// Finish closes the data sheet, writes the Info sheet from infoLines - one line
// per row, a blank line an empty row - and writes the zip's central directory.
// Until it returns nil the output is not an openable workbook.
func (s *Stream) Finish(infoLines []string) error {
	if s.err != nil {
		return s.err
	}
	if s.finished {
		return errors.New("exportfile: workbook already finished")
	}
	s.finished = true
	s.put(`</sheetData></worksheet>`)
	if s.err != nil {
		return s.err
	}
	if err := s.sheet.Flush(); err != nil {
		return s.fail(err)
	}

	info, shared := infoSheetXML(infoLines)
	if err := s.writePart(infoSheetPart, info); err != nil {
		return err
	}
	if err := s.writePart(sharedStringsPart, shared); err != nil {
		return err
	}
	if err := s.zip.Close(); err != nil {
		return s.fail(err)
	}
	return nil
}

// row writes one <row> of the data sheet.
func (s *Stream) row(number int, cells []Cell) {
	r := strconv.Itoa(number)
	s.put(`<row r="` + r + `">`)
	for i, cell := range cells {
		s.cell(s.columns[i]+r, cell)
	}
	s.put(`</row>`)
}

// cell is the streamed sheet's serializer of a Cell: the part of each kind that
// is about SpreadsheetML and nothing else.
func (s *Stream) cell(ref string, c Cell) {
	switch c.Kind {
	case CellText:
		s.put(`<c r="` + ref + `" t="inlineStr"><is><t xml:space="preserve">`)
		s.text(c.Text)
		s.put(`</t></is></c>`)
	case CellNumber:
		s.put(`<c r="` + ref + `"><v>` + formatNumber(c.Number) + `</v></c>`)
	case CellBool:
		v := "0"
		if c.Bool {
			v = "1"
		}
		s.put(`<c r="` + ref + `" t="b"><v>` + v + `</v></c>`)
	case CellDate, CellMoment:
		style := styleDate
		if c.Kind == CellMoment {
			style = styleMoment
		}
		serial, ok := excelSerial(c.Date)
		if !ok {
			// Before 1900, which Excel has no serial for: written as text, as
			// excelize writes such a time, rather than as a wrong date.
			s.cell(ref, Cell{Kind: CellText, Text: c.Date.Format(time.RFC3339Nano)})
			return
		}
		s.put(`<c r="` + ref + `" s="` + strconv.Itoa(style) + `"><v>` + formatNumber(serial) + `</v></c>`)
	}
}

// text writes escaped cell text, cut to maxCellChars.
func (s *Stream) text(v string) {
	if s.err != nil {
		return
	}
	if utf8.RuneCountInString(v) > maxCellChars {
		v = string([]rune(v)[:maxCellChars])
	}
	// EscapeText also replaces characters XML cannot carry at all, so a stray
	// control character somebody typed costs one cell a U+FFFD rather than
	// costing the whole file its well-formedness.
	if err := xml.EscapeText(s.sheet, []byte(v)); err != nil {
		s.fail(err)
	}
}

func (s *Stream) put(v string) {
	if s.err != nil {
		return
	}
	if _, err := s.sheet.WriteString(v); err != nil {
		s.fail(err)
	}
}

func (s *Stream) writePart(name, body string) error {
	entry, err := s.zip.Create(name)
	if err != nil {
		return s.fail(err)
	}
	if _, err := io.WriteString(entry, body); err != nil {
		return s.fail(err)
	}
	return nil
}

func (s *Stream) fail(err error) error {
	if s.err == nil {
		s.err = err
	}
	return s.err
}

// excelSerial is t's wall clock as an Excel 1900-system date serial: whole days
// since 1899-12-30 plus the fraction of the day. The wall clock and not the
// instant, so a time converted into the Event's zone lands on the Event's clock
// - the same reading excelize makes of a time.
//
// Excel believes 1900 was a leap year, so its serials before 1900-03-01 are one
// fewer than the arithmetic gives; before 1900-01-01 there is no serial at all.
func excelSerial(t time.Time) (float64, bool) {
	wall := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	days := float64((wall.Unix() - excelEpoch.Unix()) / 86400)
	if wall.Before(excelFirstRealDay) {
		days--
	}
	if days < 1 {
		return 0, false
	}
	seconds := time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute +
		time.Duration(t.Second())*time.Second + time.Duration(t.Nanosecond())
	return days + seconds.Seconds()/86400, true
}

var (
	excelEpoch        = time.Date(1899, time.December, 30, 0, 0, 0, 0, time.UTC)
	excelFirstRealDay = time.Date(1900, time.March, 1, 0, 0, 0, 0, time.UTC)
)

// formatNumber writes a number the way excelize writes one: as short as it can
// be while still reading back as the same float.
func formatNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// columnName is a 1-based column number's letters: 1 is A, 27 is AA.
func columnName(n int) string {
	var name []byte
	for n > 0 {
		n--
		name = append([]byte{byte('A' + n%26)}, name...)
		n /= 26
	}
	return string(name)
}

// infoSheetXML is the Info sheet and the shared strings its lines are held in.
// Every row carries the wrapped style, blank ones included, as excelize's
// addInfoSheet styles the whole range.
func infoSheetXML(lines []string) (sheet, shared string) {
	var b, strs strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<worksheet xmlns="` + mainNS + `"><sheetViews><sheetView tabSelected="1" workbookViewId="0"/></sheetViews>` +
		`<cols><col min="1" max="1" width="110" customWidth="1"/></cols><sheetData>`)
	count := 0
	for i, line := range lines {
		r := strconv.Itoa(i + 1)
		b.WriteString(`<row r="` + r + `"><c r="A` + r + `" s="` + strconv.Itoa(styleWrapped) + `"`)
		if line == "" {
			b.WriteString(`/></row>`)
			continue
		}
		b.WriteString(` t="s"><v>` + strconv.Itoa(count) + `</v></c></row>`)
		strs.WriteString(`<si><t xml:space="preserve">`)
		_ = xml.EscapeText(&strs, []byte(line))
		strs.WriteString(`</t></si>`)
		count++
	}
	b.WriteString(`</sheetData></worksheet>`)
	n := strconv.Itoa(count)
	return b.String(), xml.Header + `<sst xmlns="` + mainNS + `" count="` + n + `" uniqueCount="` + n + `">` + strs.String() + `</sst>`
}

// The workbook's fixed parts. The Info sheet is sheet1 and the data sheet
// sheet2, which is the workbook's order and not the zip's.
const (
	mainNS            = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	relsNS            = "http://schemas.openxmlformats.org/package/2006/relationships"
	docRelNS          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	infoSheetPart     = "xl/worksheets/sheet1.xml"
	dataSheetPart     = "xl/worksheets/sheet2.xml"
	sharedStringsPart = "xl/sharedStrings.xml"
)

func workbookXML(dataSheet string) string {
	var name strings.Builder
	_ = xml.EscapeText(&name, []byte(dataSheet))
	return xml.Header + `<workbook xmlns="` + mainNS + `" xmlns:r="` + docRelNS + `">` +
		`<bookViews><workbookView activeTab="0"/></bookViews><sheets>` +
		`<sheet name="` + InfoSheet + `" sheetId="1" r:id="rId1"/>` +
		`<sheet name="` + name.String() + `" sheetId="2" r:id="rId2"/>` +
		`</sheets></workbook>`
}

var contentTypesXML = xml.Header + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
	`<Override PartName="/` + infoSheetPart + `" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
	`<Override PartName="/` + dataSheetPart + `" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
	`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
	`<Override PartName="/` + sharedStringsPart + `" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>` +
	`</Types>`

var rootRelsXML = xml.Header + `<Relationships xmlns="` + relsNS + `">` +
	`<Relationship Id="rId1" Type="` + docRelNS + `/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

var workbookRelsXML = xml.Header + `<Relationships xmlns="` + relsNS + `">` +
	`<Relationship Id="rId1" Type="` + docRelNS + `/worksheet" Target="worksheets/sheet1.xml"/>` +
	`<Relationship Id="rId2" Type="` + docRelNS + `/worksheet" Target="worksheets/sheet2.xml"/>` +
	`<Relationship Id="rId3" Type="` + docRelNS + `/styles" Target="styles.xml"/>` +
	`<Relationship Id="rId4" Type="` + docRelNS + `/sharedStrings" Target="sharedStrings.xml"/>` +
	`</Relationships>`

// stylesXML holds the four cellXfs the style constants index: the default, the
// moment and calendar-date number formats the Sales Export also uses, and the
// Info sheet's wrapped, top-aligned text.
var stylesXML = xml.Header + `<styleSheet xmlns="` + mainNS + `">` +
	`<numFmts count="2"><numFmt numFmtId="164" formatCode="` + dateFormat + `"/>` +
	`<numFmt numFmtId="165" formatCode="` + answerDateFormat + `"/></numFmts>` +
	`<fonts count="1"><font><sz val="11"/><name val="Calibri"/><family val="2"/></font></fonts>` +
	`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="4">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf>` +
	`</cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`</styleSheet>`
