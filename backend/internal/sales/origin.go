package sales

import "github.com/peter/ticket_pos/backend/internal/catalog"

// A Ticket Sale's ORIGIN is how it reached the platform (#370, ADR 0052). It is
// DERIVED FROM THE SALE AND NEVER STORED: no column records a route, and this
// feature deliberately added none. ADR 0052 chose derivation over a marker
// because a derived value cannot drift out of agreement with the facts it
// describes, which for an immutable Ticket Sale is the whole game — and because
// the platform already does exactly this next door, telling a batch undo from a
// single-sale reversal by comparing `undone_at` with `reversed_at` rather than
// by storing the lever that was pulled.
//
// The four values below are the whole set. They are the API's spellings: the
// Sales list states one on every row (#370) and the Sales Export carries one as
// a column (#373), and both read them from DeriveSaleOrigin rather than
// deciding for themselves.
const (
	// SaleOriginSaleImport: the sale arrived in an uploaded Sale Import batch —
	// the file route, and the only origin that has a batch behind it in the
	// Import history.
	SaleOriginSaleImport = "sale_import"
	// SaleOriginManuallyRecorded: a Manually Recorded Sale — one Sale Import row
	// typed into a form instead of uploaded (ADR 0052). It belongs to no batch,
	// so nothing in the Import history names it.
	SaleOriginManuallyRecorded = "manually_recorded"
	// SaleOriginCorrectionReplacement: the sale is a Sale Correction's
	// replacement (ADR 0050) — recorded to stand in for a mistaken imported
	// sale, which it points at through replaces_sale_id. Also batchless, which
	// is why it must be tested for BEFORE the manual case below.
	SaleOriginCorrectionReplacement = "correction_replacement"
	// SaleOriginChannelSale: the sale was made on a Sales Channel of its own —
	// an Online Sale through the Storefront today, an In-Person Sale when that
	// slice lands — so no import brought it in and its channel is its origin.
	SaleOriginChannelSale = "channel_sale"
)

// DeriveSaleOrigin answers how one Ticket Sale reached the platform, from the
// three facts its row already states: the Sales Channel it is on, the Sale
// Import batch it belongs to (if any), and the sale it replaces (if any).
//
// THIS IS THE ONE PLACE THE MANUALLY RECORDED SALE PREDICATE IS SPELLED OUT, and
// that is a requirement rather than tidiness. The predicate is a THREE-WAY
// NEGATIVE — `channel = 'import' AND import_batch_id IS NULL AND
// replaces_sale_id IS NULL` — so it recognises a Manually Recorded Sale by what
// the row is NOT, and a negative admits new members silently:
//
//	A THIRD BATCHLESS WRITER ON THE `import` CHANNEL WILL BE LABELLED
//	"manually_recorded" BY DEFAULT — with no line of code changed here, and no
//	test failing anywhere. There are two such writers today: a Sale Correction's
//	replacement (repository.commitBatchlessImportSaleTx via sale_correct.go,
//	told apart only because it carries replaces_sale_id) and the manual form
//	itself (repository.RecordBatchlessImportSale, #368). If you are adding a
//	third, you must either give it a POSITIVE marker and a case in the switch
//	below, or accept the "typed by hand" label deliberately. ADR 0052 records
//	this as the accepted cost of not storing the route, and names a marker
//	column as the first option to revisit if it starts to hurt.
//
// Callers pass the columns as they are read, nils and all, so the code reads as
// the predicate the ADR states. Every sale has exactly one origin: there is no
// nil answer, because a sale that matched nothing more specific still sold on a
// channel.
func DeriveSaleOrigin(channel string, importBatchID, replacesSaleID *string) string {
	if channel != catalog.SalesChannelImport {
		// Not imported at all: it sold where it says it sold. An imported sale
		// is the only kind that has a route worth naming, because it is the
		// only kind the Organization put here rather than the platform.
		return SaleOriginChannelSale
	}
	switch {
	case importBatchID != nil:
		return SaleOriginSaleImport
	case replacesSaleID != nil:
		// Checked before the manual case, and the ordering is the whole of how
		// the two batchless writers are told apart.
		return SaleOriginCorrectionReplacement
	default:
		return SaleOriginManuallyRecorded
	}
}
