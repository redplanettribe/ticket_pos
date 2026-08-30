package platform

import (
	"fmt"
	"strconv"
	"strings"
)

// The ONE STATEMENT of how an export refuses to be too big.
//
// Two exports carry a synchronous row cap — the Sales Export
// (sales/service.exportTooManyRows) and the Holder Export
// (catalog/service.holderExportTooManyRows) — and each had its own copy of this
// file's two functions, character for character, sign branch included, for
// counts that are never negative. ADR 0065 is explicit about where the line
// falls: "We accept two overlapping FILES; we refuse two IMPLEMENTATIONS."
//
// WHAT IS SHARED IS THE MECHANISM AND NOT THE WORDING. Each caller keeps its own
// cap constant — the Sales Export's is importfile.MaxRows and means "no more
// rows than the importer would take back", the Holder Export's is a measured
// memory ceiling and means something else entirely — and each keeps its own
// sentence, because one says "sales" and the other says "tickets" and a reader
// told the wrong noun cannot work out which number the cap applies to. What must
// not be stated twice is the DIGIT GROUPING and the FIELD-ERROR CONSTRUCTION,
// which are about rendering a FieldError and belong here beside the codes.
//
// THIS IS THE NATURAL HOME rather than either service, because promoting the
// helper into one of them would make catalog depend on sales (or the reverse)
// for six lines of formatting — a module edge bought with a `strings.Builder`.
// platform is already what both import for FieldError and CodeTooManyItems.

// GroupDigits renders a count with thousands separators, because these numbers
// are read by a person deciding how much to narrow a filter and "24,318" is
// legible at a glance where "24318" is not.
//
// The sign branch is kept although no caller can reach it with a negative count:
// a formatter that silently mangles "-5" into "-,5" is a trap for the next
// caller, and the branch is one line.
func GroupDigits(n int) string {
	digits := strconv.Itoa(n)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return sign + b.String()
}

// ExportTooManyRows builds the field error an export over its row cap carries:
// `filters`, CodeTooManyItems, and the caller's own sentence with the matched
// count and the cap grouped into it.
//
// THE FIELD IS `filters` AND NOT ANY ONE PARAMETER, for both callers and for one
// reason: no single filter is at fault, and blaming `sold_from` would be wrong
// for somebody whose lever is the Ticket Type or the channel. The staff app
// renders this message inline beside the filter bar, so the message IS the
// feature — it names how many matched (which is how the person knows how much
// narrower to go), how many may travel at once, and the lever to reach for.
//
// It is a FIELD ERROR rather than a domain error because of what the reader is
// meant to do next: the filters that produced the request are on screen beside
// the button that sent it, and narrowing them is the fix.
//
// `format` takes exactly two %s verbs, the matched count then the cap, and stays
// at the call site because the noun in it is the file's own — "sales" for one
// export, "tickets" for the other. Integration tests pin both sentences
// verbatim.
func ExportTooManyRows(format string, matched, rowCap int) FieldError {
	return FieldError{
		Field:   "filters",
		Code:    CodeTooManyItems,
		Message: fmt.Sprintf(format, GroupDigits(matched), GroupDigits(rowCap)),
	}
}
