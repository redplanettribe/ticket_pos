package sales

// A Sale Reversal is the voiding of a recorded Ticket Sale: its tickets cease to
// exist, its capacity returns to the Ticket Type, and any money collected is
// returned to the Customer. It is always whole-Sale.
//
// Two sides can cause one, and a reversed Ticket Sale records which (#117,
// ADR 0018): the Customer reversing their own Online Sale within the Reversal
// Window, or staff undoing a Sale Import. The pair below is the whole value set
// of `ticket_sales.reversed_by`; adding a third (a Platform Operator acting on
// an incident, say) is a constraint change, not a redesign.
const (
	// ReversalActorCustomer marks a Sale Reversal the buyer performed themselves
	// from the Storefront, inside the Reversal Window.
	ReversalActorCustomer = "customer"
	// ReversalActorStaff marks a Sale Reversal an Organization's staff caused —
	// today only through a Sale Import undo, which reverses a whole batch. The
	// acting Member is recorded on the batch itself; this says which side asked.
	ReversalActorStaff = "staff"
)
