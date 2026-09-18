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
//
// IT STOPPED BEING A bool WHEN THE UPGRADE ARRIVED (#650, ADR 0074). Two values
// could only say whether the BUYER hears; the Upgrade needs a third thing said,
// which is that NOBODY does — and a bool has nowhere to put it. The underlying
// type is now an opaque uint8 so that no call site can go on treating the policy
// as a yes/no and quietly lose the new case.
type BuyerNoticePolicy uint8

const (
	// BuyerIsBeingWrittenTo says the buyer of this reversed Ticket Sale is hearing
	// from the platform about it — the Sale Voided notice every Online Sale's
	// reversal sends, an import undo whose notify toggle is on, or a Sale
	// Correction whose new Sale Confirmation the Member chose to send.
	//
	// FIRST, SO IT IS NEVER THE ZERO VALUE. A policy nobody set must not read as
	// "write to everybody"; it reads as an unusable zero, and the argument is
	// required at every call site precisely so no such value can be constructed.
	BuyerIsBeingWrittenTo BuyerNoticePolicy = iota + 1

	// BuyerIsNotBeingWrittenTo says the platform is saying nothing to this buyer.
	// It is only ever true of the `import` channel, whose buyers dealt with the
	// Organization's sales rep and may not know this platform exists: a batch undo
	// with the toggle off, a single imported Sale reversed from the Sales list,
	// and a Sale Correction with the confirmation checkbox left alone.
	//
	// EVERY OTHER DISPLACED HOLDER IS STILL TOLD. This value spares the buyer and
	// nobody else: somebody who came here, proved their address and accepted a
	// Ticket Assignment hears that they have stopped holding it, on every route.
	BuyerIsNotBeingWrittenTo

	// NobodyIsBeingWrittenTo says this reversal is SILENT: not the buyer, not any
	// Holder, nobody at all (#650, ADR 0074).
	//
	// IT IS THE UPGRADE'S, AND SO FAR ONLY THE UPGRADE'S. A buyer who elected an
	// Upgrade surrendered a free Ticket to take a paid one in the same
	// transaction; they have not lost a sale, and telling them they have —
	// thirty seconds after paying for an improvement — would be the platform
	// reporting a loss that did not happen. The eligibility rule guarantees the
	// free Sale carried exactly one Ticket and that the buyer themself held it,
	// so there is no third party being silenced here: the one person who could
	// be written to is the one person who asked for this.
	//
	// IT IS A REASON AND NOT A SWITCH, which is the whole of why it lives beside
	// the other two rather than as a flag at the Upgrade's call site. Two mails
	// must stay quiet and they are composed in two different modules — the Sale
	// Voided notice in sales, the No Longer Holding mail in catalog — so a rule
	// remembered at one call site is a rule half-kept. Carried as a policy value,
	// the silence travels to both by the same road every other reversal's
	// notification policy travels.
	NobodyIsBeingWrittenTo
)

// WritesToTheBuyer reads the policy as the question its holder asks of it.
//
// A SILENT REVERSAL ANSWERS NO, so a reader that only ever knew two values
// cannot accidentally write to the buyer of an Upgrade.
func (p BuyerNoticePolicy) WritesToTheBuyer() bool { return p == BuyerIsBeingWrittenTo }

// WritesToAnybody reads the second question, the one only the Upgrade makes
// worth asking: is there any recipient at all under this policy?
//
// It is asked BEFORE a displaced-Holder read rather than per recipient, because
// "nobody" is not a filter over a list of people — it is the absence of the
// question, and a silent reversal should not go looking for addresses it has
// already decided not to use.
func (p BuyerNoticePolicy) WritesToAnybody() bool { return p != NobodyIsBeingWrittenTo }

// THE TWO PLACES A POLICY ORIGINATES FROM SOMEBODY'S CHOICE.
//
// Everywhere else the policy is a constant, because the route decides it and no
// Member is asked: an Online Sale's reversal, an Operator Reversal and a
// Customer's own undo always write to the buyer, and a single imported Sale
// reversed from the Sales list never does. Those sites name a constant above and
// read as what they are.
//
// The two below are the routes that DO ask, and each asks with a checkbox of its
// own wording. An `if` at those sites would give the bare bool back the anonymity
// this type exists to take away (#397): any bool in scope would do, and the two
// on offer at a correction — "send the confirmation" and "the sale was
// self-held" — are both plausible and only one is right.
//
// THEY BUY A NAME AND NOT A COMPILER ERROR, which is worth being honest about:
// nothing here STOPS a future call site branching on the wrong bool. What a
// constructor per origin does is make the right thing the obvious thing to reach
// for, and make a call site say whose choice the policy was — which is the part
// a reader of a reversal path cannot otherwise recover.
//
// A THIRD ORIGIN IS NOT ADDED HERE. The Upgrade's silence is nobody's checkbox:
// it is what an Upgrade IS, decided by the route rather than asked of anybody,
// so it names NobodyIsBeingWrittenTo directly like the other constant routes do.

// BuyerNoticeFromImportUndoToggle reads the Sale Import undo's `notify_buyers`
// toggle as the buyer's notification policy for the whole batch.
//
// ONE POLICY FOR A WHOLE UNDO, because the toggle is one decision about one act:
// a 185-row batch undone with it off writes to none of those 185 buyers, which
// is the mailshot ADR 0055 exists to prevent.
func BuyerNoticeFromImportUndoToggle(notifyBuyers bool) BuyerNoticePolicy {
	if notifyBuyers {
		return BuyerIsBeingWrittenTo
	}
	return BuyerIsNotBeingWrittenTo
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
	if sendConfirmation {
		return BuyerIsBeingWrittenTo
	}
	return BuyerIsNotBeingWrittenTo
}
