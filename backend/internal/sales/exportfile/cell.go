package exportfile

import "time"

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
)

// Cell is one typed spreadsheet value. Only the field its Kind names is read.
type Cell struct {
	Kind   CellKind
	Text   string
	Number float64
	// Date is the calendar day at midnight UTC, so a writer reads no offset off
	// it and a birthday cannot move a day. See Answer.Cell.
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
		return Cell{Kind: CellText, Text: *a.Text}
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
		return Cell{Kind: CellDate, Date: time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)}
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
