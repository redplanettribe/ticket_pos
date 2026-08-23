package platform

// BuyerNoticePolicy is the Buyer Notification Policy (CONTEXT.md): one fact
// about a Sale Reversal that has just committed — whether the reversal is one
// the BUYER is being written to about (#392, parent #391, ADR 0055).
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

// THE TWO PLACES A POLICY ORIGINATES FROM SOMEBODY'S CHOICE.
//
// Everywhere else the policy is a constant, because the route decides it and no
// Member is asked: an Online Sale's reversal, an Operator Reversal and a
// Customer's own undo always write to the buyer, and a single imported Sale
// reversed from the Sales list never does. Those sites name a constant above and
// read as what they are.
//
// The two below are the routes that DO ask, and each asks with a checkbox of its
// own wording. Converting `BuyerNoticePolicy(someBool)` at those sites gives the
// bare bool back the anonymity this type exists to take away (#397): the
// conversion accepts any bool in scope, and the two on offer at a correction —
// "send the confirmation" and "the sale was self-held" — are both plausible and
// only one is right.
//
// THEY BUY A NAME AND NOT A COMPILER ERROR, which is worth being honest about:
// the conversion is still exported and still legal, so nothing here STOPS a
// future call site reaching for the wrong bool. What a constructor per origin
// does is make the right thing the obvious thing to reach for, and make a call
// site say whose choice the policy was — which is the part a reader of a
// reversal path cannot otherwise recover.

// BuyerNoticeFromImportUndoToggle reads the Sale Import undo's `notify_buyers`
// toggle as the buyer's notification policy for the whole batch.
//
// ONE POLICY FOR A WHOLE UNDO, because the toggle is one decision about one act:
// a 185-row batch undone with it off writes to none of those 185 buyers, which
// is the mailshot ADR 0055 exists to prevent.
func BuyerNoticeFromImportUndoToggle(notifyBuyers bool) BuyerNoticePolicy {
	return BuyerNoticePolicy(notifyBuyers)
}

// BuyerNoticeFromSaleCorrectionConfirmation reads a Sale Correction's
// `send_confirmation` checkbox as the buyer's notification policy.
//
// OFF BY DEFAULT, which is the whole reason this route needs saying separately.
// A correction re-seats the buyer on the Ticket it took off them in the same
// transaction, so an unasked-for notice would tell them they had lost something
// that is theirs again by the time they read it.
//
// IT IS THE MEMBER'S CHOICE AND NOT THE DELIVERY: whether the Sale Confirmation
// actually leaves is the provider's business, and a provider having a bad day
// does not change what the Member decided to say.
func BuyerNoticeFromSaleCorrectionConfirmation(sendConfirmation bool) BuyerNoticePolicy {
	return BuyerNoticePolicy(sendConfirmation)
}
