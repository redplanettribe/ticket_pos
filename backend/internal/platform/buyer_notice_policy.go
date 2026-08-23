package platform

// BuyerNoticePolicy is one fact about a Sale Reversal that has just committed:
// whether the reversal is one the BUYER is being written to about (#392, parent
// #391, ADR 0055).
//
// IT EXISTS SO TWO MODULES CAN EACH KEEP WHAT THEY KNOW. Sales knows whether an
// act writes to its buyers — the Sale Import undo's notify toggle, the Sale
// Correction's off-by-default Sale Confirmation checkbox, the Sale Voided notice
// an Operator Reversal and a Customer's own undo always send — and knows nothing
// about Tickets, Holders or acceptance. Catalog knows which displaced Holder IS
// the buyer and which came here and clicked an Assignment Link, and must never
// be told why a Sale was reversed, because the message it composes is forbidden
// to say. This value is the whole of what crosses between them: the buyer's
// notification policy travels one way, and no Holder semantics travel the other.
//
// IT LIVES IN platform BECAUSE NEITHER MODULE MAY DEPEND ON THE OTHER. The
// catalog service is built after the sales service and satisfies an interface
// sales declares; a type owned by either side would make that dependency run
// both ways.
//
// A NAMED TYPE RATHER THAN A bool, because a bare bool at five call sites is
// five chances to pass the wrong one and no way for a reader to see it. The
// constants below say what is true of the reversal, not what should happen to a
// mail — deciding that is the far side's job, and phrasing the value as an
// instruction would move the policy into the module that must not hold it.
type BuyerNoticePolicy bool

const (
	// BuyerIsBeingWrittenTo says the buyer of this reversed Ticket Sale is hearing
	// from the platform about it — the Sale Voided notice every Online Sale's
	// reversal sends, an import undo whose notify toggle is on, or a Sale
	// Correction whose new Sale Confirmation the Member chose to send.
	BuyerIsBeingWrittenTo BuyerNoticePolicy = true

	// BuyerIsNotBeingWrittenTo says the platform is saying nothing to this buyer.
	// It is only ever true of the `import` channel, whose buyers dealt with the
	// Organization's sales rep and may not know this platform exists: a batch undo
	// with the toggle off, a single imported Sale reversed from the Sales list,
	// and a Sale Correction with the confirmation checkbox left alone.
	BuyerIsNotBeingWrittenTo BuyerNoticePolicy = false
)

// WritesToTheBuyer reads the policy as the question its holder asks of it.
func (p BuyerNoticePolicy) WritesToTheBuyer() bool { return bool(p) }
