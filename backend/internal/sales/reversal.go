package sales

// A Sale Reversal is the voiding of a recorded Ticket Sale: its tickets cease to
// exist, its capacity returns to the Ticket Type, and any money collected is
// returned to the Customer. It is always whole-Sale.
//
// Three sides can cause one, and a reversed Ticket Sale records which (#117,
// ADR 0018): the Customer reversing their own Online Sale within the Reversal
// Window, staff undoing a Sale Import, or a Platform Operator recording a refund
// they made off-platform. The trio below is the whole value set of
// `ticket_sales.reversed_by`.
const (
	// ReversalActorCustomer marks a Sale Reversal the buyer performed themselves
	// from the Storefront, inside the Reversal Window.
	ReversalActorCustomer = "customer"
	// ReversalActorStaff marks a Sale Reversal an Organization's staff caused —
	// today only through a Sale Import undo, which reverses a whole batch. The
	// acting Member is recorded on the batch itself; this says which side asked.
	ReversalActorStaff = "staff"
	// ReversalActorOperator marks an Operator Reversal: a Platform Operator
	// recording that they refunded the buyer off-platform (#125). It is the one
	// actor that names an individual — the acting operator's email is stamped on
	// the sale beside it — because it is the one route where the platform voids
	// somebody's sale on a human's say-so rather than on an action the system
	// itself performed.
	ReversalActorOperator = "operator"
)
