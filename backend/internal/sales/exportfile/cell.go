package exportfile

import (
	"time"
	"unicode/utf16"
)

// The ONE RULE for what a Ticket Answer looks like as a cell (#656, ADR 0075).
//
// ADR 0065 accepted two overlapping files and refused two implementations, and
// ADR 0075 puts the two files on two different writers: the Sales Export stays
// on excelize, the Holder Export streams through a writer of its own. What must
// still be decided once is what an Answer IS in a spreadsheet - text, a number,
// a calendar date, a boolean or nothing - and that is this file. Each writer
// keeps only a thin serializer that turns a Cell into its own bytes, and neither
// looks at an Answer's fields itself.
//
// It sits beside BuildQuestionColumns for the same reason that file exists:
// which cells a Ticket's Answers occupy is decided there, what goes in each is
// decided here, and nothing about either knows a sheet or a library.

// CellKind is the type a cell is written as.
type CellKind int

const (
	// CellBlank writes nothing, so the cell is genuinely empty rather than an
	// empty string, a zero or a FALSE. A blank is an Outstanding Answer or a
	// question never asked, and that is a different fact from any value.
	CellBlank CellKind = iota
	// CellText is a text cell, even when it looks like a number.
	CellText
	// CellNumber is a real number.
	CellNumber
	// CellDate is a CALENDAR DATE with no time, formatted answerDateFormat.
	CellDate
	// CellBool is a real TRUE or FALSE.
	CellBool
	// CellMoment is a date AND time, formatted dateFormat and written as the
	// wall clock of the time's own location - so a caller puts the Event's clock
	// on it by converting first. The answer rule never produces one; it is what
	// the Holder Export's sold_at is.
	CellMoment
)

// Cell is one typed spreadsheet value. Only the field its Kind names is read.
type Cell struct {
	Kind   CellKind
	Text   string
	Number float64
	// Date is the calendar day at midnight UTC for a CellDate, so a writer reads
	// no offset off it and a birthday cannot move a day (see Answer.Cell), and
	// the moment itself for a CellMoment.
	Date time.Time
	Bool bool
}

// Cell is what this Answer is written as.
//
// A multiple-choice Answer never reaches here whole: CellsFor fans it out into
// one Checked Answer per Option, so each Option column is a CellBool like a
// checkbox's, and the fan-out stays CellsFor's decision.
func (a Answer) Cell() Cell {
	switch {
	case a.Text != nil:
		return TextCell(*a.Text)
	case a.Number != nil:
		// At the precision it was given: a `number` question may be asked for a
		// headcount or for a measurement, and the schema stores it as
		// unconstrained NUMERIC rather than round somebody's answer away.
		return Cell{Kind: CellNumber, Number: *a.Number}
	case a.Date != nil:
		// Pointedly NOT converted into the Event's timezone the way sold_at is. A
		// date Answer is a calendar date - a birthday, a travel day - and has no
		// moment to move: shifting it into a zone is how a birthday comes back a
		// day early, which is the bug migration 073's DATE column exists to
		// prevent. Rebuilt at midnight UTC so no writer reads an offset off it.
		d := *a.Date
		day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		if day.Before(firstSerialDay) {
			// Excel's 1900 date system has no serial for it, so it cannot be a
			// date cell. It is the text excelize has always written for such a
			// time, `1850-01-01T00:00:00Z`, and deliberately not a tidier
			// `1850-01-01`: both files wrote this before they shared a rule,
			// and nothing a user downloads changes (#656, #655 story 34).
			return TextCell(day.Format(time.RFC3339Nano))
		}
		return Cell{Kind: CellDate, Date: day}
	case a.Checked != nil:
		return Cell{Kind: CellBool, Bool: *a.Checked}
	}
	// An Answer with nothing set. The write path cannot produce one - migration
	// 073's CHECK refuses a row claiming to be two kinds at once and the service
	// refuses one claiming to be none - so this is the shape of a bug elsewhere,
	// and a blank keeps it looking like the Outstanding Answer it would be
	// indistinguishable from anyway.
	return Cell{Kind: CellBlank}
}

// firstSerialDay is the first day Excel's 1900 date system has a serial for.
var firstSerialDay = time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)

// TextCell is what a piece of text is written as, in either file.
//
// EMPTY TEXT IS A BLANK, not a text cell holding nothing: an empty string reads
// as a value to "is blank", and a reader filtering on "is blank" is asking who
// left it empty. A Holder's name, a buyer's and an Answer somebody cleared are
// all written this way, by both writers.
//
// Anything longer than Excel's limit is cut to it (see cutCellText), rather
// than written into a file Excel refuses to open.
func TextCell(v string) Cell {
	if v == "" {
		return Cell{Kind: CellBlank}
	}
	return Cell{Kind: CellText, Text: cutCellText(v)}
}

// maxCellUnits is the most a cell may hold, in UTF-16 code units, which is
// what Excel counts.
const maxCellUnits = 32767

// cutCellText cuts v to at most maxCellUnits UTF-16 code units.
//
// UTF-16 AND NOT RUNES, because Excel's limit is in UTF-16 units: a character
// outside the Basic Multilingual Plane (most emoji) is one rune and two units,
// so a rune count lets through text Excel refuses. The cut is always on a rune
// boundary, so a surrogate pair is never split in half; a character built of
// several runes (an emoji with a skin-tone modifier, a flag) may still be
// split, which costs its last visible character and not the file.
func cutCellText(v string) string {
	// A UTF-8 string is never shorter in bytes than in UTF-16 units.
	if len(v) <= maxCellUnits {
		return v
	}
	units := 0
	for i, r := range v {
		width := utf16.RuneLen(r)
		if width < 0 {
			width = 1 // invalid UTF-8, which a writer escapes as one U+FFFD
		}
		if units+width > maxCellUnits {
			return v[:i]
		}
		units += width
	}
	return v
}
