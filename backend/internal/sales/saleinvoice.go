package sales

import (
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PaidOnlineSale is what the sale-commit spine tells the invoicing module
// about a Ticket Sale that owes a Sale Invoice (#473, ADR 0060): a paid
// `online` sale of a House Organization's Event, described entirely from
// what the commit has just written, so the document can be owed inside the
// same transaction and never re-read from the Customer or the catalog
// afterwards.
//
// IT IS A SNAPSHOT AND NOT A REFERENCE, deliberately. The Recipient of a Sale
// Invoice is the buyer AS TRANSACTED — "First Last", the Tax ID the checkout
// was made under, the Sale's email — and a Sale Re-addressing or a profile
// edit later must leave the document alone (CONTEXT.md, Recipient). Handing
// over ids and letting invoicing look the buyer up would make that a hope
// rather than a property.
//
// THE MONEY IS WHAT THE BUYER PAID, per line and in all: the buyer unit
// price with the Platform Fee inside it, because the platform sold the whole
// ticket and the fee is its own money whichever way Fee Handling went. The
// fee is never a line (ADR 0060).
type PaidOnlineSale struct {
	TicketSaleID    string
	OrganizationID  string
	EventID         string
	EventName       string
	ConfirmationRef string
	// Buyer is the Ticket Sale's own snapshot of who bought: email, the two
	// name halves and the Tax ID. The phone and the self-asserted flag ride
	// the same struct elsewhere but mean nothing to a document.
	Buyer platform.SaleCustomer
	// Locale is the Sale Locale, for the mail the document is delivered
	// with; "" when the page named none.
	Locale string
	// PaymentProvider is who collected the money (e.g. "payphone"), for the
	// authority's forma de pago.
	PaymentProvider string
	// AmountCents is the sum of the lines: what the card was charged.
	AmountCents int
	Lines       []PaidOnlineSaleLine
}

// PaidOnlineSaleLine is one Ticket Sale Line as the buyer paid it.
type PaidOnlineSaleLine struct {
	TicketTypeName string
	Quantity       int
	// UnitPriceCents is the buyer unit price snapshotted on the line: the
	// price paid per ticket, IVA and fee inside.
	UnitPriceCents int
}

// The reversal routes, as a Ticket Sale's documents name them (#476, ADR
// 0060): the same five words the Sales Export states in its reversed_by
// column, because a Credit Note's reason and a spreadsheet's route are one
// fact told twice. The stored `reversed_by` actor is coarser — `staff`
// covers three levers — so the route is decided where the lever is pulled
// and handed down, never derived afterwards.
const (
	// ReversalRouteCustomer: the buyer undid their own Online Sale.
	ReversalRouteCustomer = "customer"
	// ReversalRoutePlatform: an Operator Reversal (ADR 0019).
	ReversalRoutePlatform = "platform"
	// ReversalRouteImportUndo: a whole Sale Import batch undone.
	ReversalRouteImportUndo = "import_undo"
	// ReversalRouteStaffReversal: one imported sale reversed on its own.
	ReversalRouteStaffReversal = "staff_reversal"
	// ReversalRouteCorrection: one imported sale replaced by a Sale Correction.
	ReversalRouteCorrection = "correction"
)

// SaleReversal is what the reversal primitive tells the invoicing module
// about a Ticket Sale it has just marked reversed (#476, ADR 0060), still
// inside that transaction: which Sale, by which route, and when. Nothing
// about money or lines travels — the documents to settle are the Sale's
// own rows on the invoicing side, and a Credit Note copies the Sale
// Invoice it credits, never the Sale as it stands now.
//
// An Operator Reversal's memo (what was refunded, whether the fee was
// kept) is deliberately absent: the Credit Note is always for the whole
// amount the factura stated (#471 story 30).
type SaleReversal struct {
	TicketSaleID string
	// Route is one of the ReversalRoute* values.
	Route string
	// At is the reversal's instant, the one stamped on the sale.
	At time.Time
}
