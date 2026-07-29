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

// A Reversal Request is a Customer's ask to undo their own paid Online Sale,
// recorded the moment they ask and pursued by the platform until the Payment
// Provider gives a definite answer (ADR 0024). The constants below are the whole
// value set of `sale_reversals.status`.
//
// Only a paid Online Sale ever makes one. A free Online Sale has no provider to
// wait on and stays fully synchronous, and neither an Operator Reversal nor a
// Sale Import undo asks anybody's permission.
const (
	// ReversalRequestInFlight is the ask still open: the provider has not given a
	// definite answer, so the money may or may not have moved. The Ticket Sale
	// stays active and its capacity stays held for exactly that reason — nothing
	// is known to have happened yet.
	ReversalRequestInFlight = "in_flight"
	// ReversalRequestSucceeded is the provider confirming the money went back,
	// either with its documented `true` or with the receipt that it had already
	// gone (PayPhone's errorCode 24). The Ticket Sale becomes a Sale Reversal
	// through the shared ReverseSales primitive.
	ReversalRequestSucceeded = "succeeded"
	// ReversalRequestRefused is the provider considering the reversal and
	// declining it. NOTHING HAPPENED: the money never moved and the Ticket Sale
	// is untouched, which is why a refused request does not block a later ask —
	// a buyer still inside their Reversal Window may genuinely try again.
	ReversalRequestRefused = "refused"
	// ReversalRequestNeedsAttention is an Unresolved Reversal: the platform gave
	// up asking with the answer still unknown, and only the Payment Provider's
	// own dashboard can say. It awaits a Platform Operator, who settles it as an
	// Operator Reversal if the money did in fact leave.
	//
	// Reached by two routes. The Ticket Sale was reversed by somebody else while
	// the request was in flight, so asking the provider again could refund the
	// buyer twice and the platform stops rather than find out; or a whole day of
	// unknown answers passed and the platform stopped asking (ReversalGiveUpAfter).
	// Both are the same admission — nobody here knows what became of the money.
	ReversalRequestNeedsAttention = "needs_attention"
)
