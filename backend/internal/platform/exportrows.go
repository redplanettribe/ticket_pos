package platform

import (
	"fmt"
	"strconv"
	"strings"
)

// The ONE STATEMENT of how an export refuses to be too big.
//
// The Sales Export carries a synchronous row cap
// (sales/service.exportTooManyRows); its cap is importfile.MaxRows and means "no
// more rows than the importer would take back". The Holder Export carried one
// too until ADR 0075 streamed it and removed it, which is why this was ever
// shared, and why it stays here rather than moving into the sales service: the
// digit grouping and the field-error construction are about rendering a
// FieldError and belong beside the codes, and the next export that needs a
// refusal should find them here rather than write a second copy. Each caller
// keeps its own sentence, because the noun in it is the file's own.

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
// THE FIELD IS `filters` AND NOT ANY ONE PARAMETER, and for one
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
// at the call site because the noun in it is the file's own. Integration tests
// pin the sentence verbatim.
func ExportTooManyRows(format string, matched, rowCap int) FieldError {
	return FieldError{
		Field:   "filters",
		Code:    CodeTooManyItems,
		Message: fmt.Sprintf(format, GroupDigits(matched), GroupDigits(rowCap)),
	}
}
