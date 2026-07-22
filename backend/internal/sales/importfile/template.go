package importfile

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// templateSheet is the sheet organizers fill in.
const templateSheet = "Sales"

// templateRefSheet holds the Ticket Type name→id reference the dropdown and the
// hidden id column resolve against. It is hidden from the organizer.
const templateRefSheet = "_ticket_types"

// templateInstructionsSheet greets the organizer on open with a short how-to. It
// sits first and is the active sheet; the parser selects "Sales" by name, so its
// position never affects uploads.
const templateInstructionsSheet = "Instructions"

// templateHeaders are the visible columns, in order. ticket_type_id (column I)
// is a hidden reference the preview matches on when present.
var templateHeaders = []string{
	colCustomerEmail, colCustomerFirstName, colCustomerLastName, colTicketType,
	colQuantity, colPaymentMethod, colSoldAt, colAmount, colTicketTypeID,
}

// templatePrompts are the per-column input-message tooltips Excel shows when a
// cell in the column is selected. Keyed by column header. Kept concrete and
// organizer-facing; the amount prompt spells out the otherwise-invisible
// blank→Ticket-Type-price behaviour.
var templatePrompts = map[string]string{
	colCustomerEmail:     "Required. The buyer's email. The Sale Confirmation is sent to this address.",
	colCustomerFirstName: "Required. The buyer's first name.",
	colCustomerLastName:  "Required. The buyer's last name.",
	colTicketType:        "Required. Pick a Ticket Type from the dropdown.",
	colQuantity:          "Required. Number of tickets sold. A whole number, 1 or more.",
	colPaymentMethod:     "Required. How the sale was paid: pick cash or transfer.",
	colSoldAt:            "Sale date, e.g. 2026-07-15. Not in the future.",
	colAmount:            "Total paid, e.g. 25.00. Leave blank to use the Ticket Type's own price.",
}

// templateHeaderComments are the red-triangle notes attached to each header
// cell, describing the column in a little more depth than the cell tooltip.
var templateHeaderComments = map[string]string{
	colCustomerEmail:     "Buyer's email address. Required on every row. The Sale Confirmation receipt is emailed here.",
	colCustomerFirstName: "Buyer's first name. Required. Stored separately from the last name.",
	colCustomerLastName:  "Buyer's last name. Required. Stored separately from the first name.",
	colTicketType:        "The Ticket Type sold. Required. Choose from the dropdown so it matches this Event's catalog exactly.",
	colQuantity:          "How many tickets of this Ticket Type were sold on this row. Required. A whole number of 1 or more.",
	colPaymentMethod:     "How this Direct Sale was paid. Required. cash or transfer.",
	colSoldAt:            "The date the sale was made, e.g. 2026-07-15. Should not be in the future.",
	colAmount:            "Total amount paid for this row. Leave blank to use the Ticket Type's own price.",
}

// templateInstructions is the friendly how-to shown on the Instructions sheet,
// one line per row.
var templateInstructions = []string{
	"How to record your sales",
	"",
	"1. Open the Sales tab at the bottom and fill in one row per sale.",
	"2. customer_email, customer_first_name and customer_last_name are required — the buyer gets their Sale Confirmation by email.",
	"3. Pick the ticket_type from the dropdown so it matches this Event's Ticket Types exactly.",
	"4. quantity is the number of tickets sold on that row (a whole number, 1 or more).",
	"5. payment_method is how it was paid: cash or transfer.",
	"6. sold_at is the sale date, e.g. 2026-07-15. It should not be in the future.",
	"7. amount is the total paid. Leave it blank to use the Ticket Type's own price.",
	"",
	"Tip: select a cell to see a hint for that column. Warnings are just guidance — the upload preview does the final check.",
	"Keep the Sales tab named \"Sales\" and don't delete the hidden helper columns.",
}

// BuildTemplate produces a per-event .xlsx: an Instructions sheet that greets the
// organizer, then a Sales sheet with a header row, a locked dropdown of the
// Event's Ticket Types in the ticket_type column, a cash|transfer dropdown for
// payment_method, per-column tooltips and header notes, gentle (Warning-style)
// value checks, and a hidden ticket_type_id column that resolves the selected
// name to its internal id via the hidden reference sheet.
func BuildTemplate(eventName string, types []TicketTypeRef) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName("Sheet1", templateSheet); err != nil {
		return nil, err
	}

	for i, h := range templateHeaders {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellStr(templateSheet, cell, h); err != nil {
			return nil, err
		}
	}

	// Hidden reference sheet: column A = Ticket Type name, column B = id.
	if _, err := f.NewSheet(templateRefSheet); err != nil {
		return nil, err
	}
	for i, tt := range types {
		row := i + 1
		if err := f.SetCellStr(templateRefSheet, fmt.Sprintf("A%d", row), tt.Name); err != nil {
			return nil, err
		}
		if err := f.SetCellStr(templateRefSheet, fmt.Sprintf("B%d", row), tt.ID); err != nil {
			return nil, err
		}
	}
	if err := f.SetSheetVisible(templateRefSheet, false); err != nil {
		return nil, err
	}

	const lastRow = MaxRows + 1 // header is row 1

	// Column letters for the visible headers, in order: A..H.
	col := func(header string) string {
		for i, h := range templateHeaders {
			if h == header {
				name, _ := excelize.ColumnNumberToName(i + 1)
				return name
			}
		}
		return ""
	}
	rangeOf := func(header string) string {
		c := col(header)
		return fmt.Sprintf("%s2:%s%d", c, c, lastRow)
	}

	// ticket_type (column D): a locked dropdown sourced from the reference sheet,
	// carrying its own input-message tooltip. Falls back to a prompt-only
	// validation when the Event has no Ticket Types yet.
	if len(types) > 0 {
		dv := excelize.NewDataValidation(true)
		dv.Sqref = rangeOf(colTicketType)
		dv.SetSqrefDropList(fmt.Sprintf("%s!$A$1:$A$%d", templateRefSheet, len(types)))
		dv.ShowDropDown = false // false => show the dropdown arrow
		dv.SetInput(colTicketType, templatePrompts[colTicketType])
		if err := f.AddDataValidation(templateSheet, dv); err != nil {
			return nil, err
		}

		// Hidden id column (I) resolves the chosen name back to its id.
		for row := 2; row <= lastRow; row++ {
			formula := fmt.Sprintf(
				`=IFERROR(VLOOKUP(D%d,%s!$A$1:$B$%d,2,FALSE),"")`,
				row, templateRefSheet, len(types),
			)
			if err := f.SetCellFormula(templateSheet, fmt.Sprintf("I%d", row), formula); err != nil {
				return nil, err
			}
		}
	} else if err := addPromptValidation(f, colTicketType, rangeOf(colTicketType)); err != nil {
		return nil, err
	}

	// payment_method (column F): a fixed cash|transfer dropdown with a tooltip.
	pm := excelize.NewDataValidation(true)
	pm.Sqref = rangeOf(colPaymentMethod)
	if err := pm.SetDropList([]string{"cash", "transfer"}); err != nil {
		return nil, err
	}
	pm.ShowDropDown = false
	pm.SetInput(colPaymentMethod, templatePrompts[colPaymentMethod])
	if err := f.AddDataValidation(templateSheet, pm); err != nil {
		return nil, err
	}

	// quantity (column E): gentle whole-number >= 1 check with a tooltip.
	qty := excelize.NewDataValidation(true)
	qty.Sqref = rangeOf(colQuantity)
	if err := qty.SetRange(1, 1, excelize.DataValidationTypeWhole, excelize.DataValidationOperatorGreaterThanOrEqual); err != nil {
		return nil, err
	}
	qty.SetError(excelize.DataValidationErrorStyleWarning, "Check quantity", "Quantity should be a whole number of 1 or more.")
	qty.SetInput(colQuantity, templatePrompts[colQuantity])
	if err := f.AddDataValidation(templateSheet, qty); err != nil {
		return nil, err
	}

	// amount (column H): gentle decimal >= 0 check (blank stays allowed) with a
	// tooltip that spells out the blank→Ticket-Type-price behaviour.
	amt := excelize.NewDataValidation(true)
	amt.Sqref = rangeOf(colAmount)
	if err := amt.SetRange(0, 0, excelize.DataValidationTypeDecimal, excelize.DataValidationOperatorGreaterThanOrEqual); err != nil {
		return nil, err
	}
	amt.SetError(excelize.DataValidationErrorStyleWarning, "Check amount", "Amount should be 0 or more, or left blank to use the Ticket Type's price.")
	amt.SetInput(colAmount, templatePrompts[colAmount])
	if err := f.AddDataValidation(templateSheet, amt); err != nil {
		return nil, err
	}

	// sold_at (column G): real date cells (yyyy-mm-dd) plus a gentle "not in the
	// future" check. Excel evaluates TODAY() itself.
	dateFmt := "yyyy-mm-dd"
	dateStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &dateFmt})
	if err != nil {
		return nil, err
	}
	soldCol := col(colSoldAt)
	if err := f.SetCellStyle(templateSheet, fmt.Sprintf("%s2", soldCol), fmt.Sprintf("%s%d", soldCol, lastRow), dateStyle); err != nil {
		return nil, err
	}
	sold := excelize.NewDataValidation(true)
	sold.Sqref = rangeOf(colSoldAt)
	if err := sold.SetRange("TODAY()", "TODAY()", excelize.DataValidationTypeDate, excelize.DataValidationOperatorLessThanOrEqual); err != nil {
		return nil, err
	}
	sold.SetError(excelize.DataValidationErrorStyleWarning, "Check sale date", "The sale date should not be in the future.")
	sold.SetInput(colSoldAt, templatePrompts[colSoldAt])
	if err := f.AddDataValidation(templateSheet, sold); err != nil {
		return nil, err
	}

	// Prompt-only tooltips for the remaining visible columns (email, names).
	for _, h := range []string{colCustomerEmail, colCustomerFirstName, colCustomerLastName} {
		if err := addPromptValidation(f, h, rangeOf(h)); err != nil {
			return nil, err
		}
	}

	// Header-row notes (red triangle) describing each visible column.
	for _, h := range templateHeaders {
		text, ok := templateHeaderComments[h]
		if !ok {
			continue
		}
		if err := f.AddComment(templateSheet, excelize.Comment{
			Cell:   fmt.Sprintf("%s1", col(h)),
			Author: "Template",
			Text:   text,
		}); err != nil {
			return nil, err
		}
	}

	// Hide the ticket_type_id reference column (I) from the organizer.
	if err := f.SetColVisible(templateSheet, "I", false); err != nil {
		return nil, err
	}
	if err := f.SetColWidth(templateSheet, "A", "H", 22); err != nil {
		return nil, err
	}

	// Instructions sheet: friendly how-to, placed first and made active so it
	// greets the organizer on open. The Sales sheet keeps its name, so the parser
	// (which selects "Sales" by name) is unaffected by the ordering.
	if _, err := f.NewSheet(templateInstructionsSheet); err != nil {
		return nil, err
	}
	for i, line := range templateInstructions {
		if err := f.SetCellStr(templateInstructionsSheet, fmt.Sprintf("A%d", i+1), line); err != nil {
			return nil, err
		}
	}
	if err := f.SetColWidth(templateInstructionsSheet, "A", "A", 100); err != nil {
		return nil, err
	}
	if err := f.MoveSheet(templateInstructionsSheet, templateSheet); err != nil {
		return nil, err
	}
	idx, err := f.GetSheetIndex(templateInstructionsSheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return bytes.NewBuffer(buf.Bytes()).Bytes(), nil
}

// addPromptValidation attaches a prompt-only data validation to a column on the
// Sales sheet so it carries an input-message tooltip even when it has no value
// check of its own.
func addPromptValidation(f *excelize.File, header, sqref string) error {
	dv := excelize.NewDataValidation(true)
	dv.Sqref = sqref
	dv.SetInput(header, templatePrompts[header])
	return f.AddDataValidation(templateSheet, dv)
}
