// Package sales holds sales domain errors and shared types.
package sales

import (
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// ErrEventNotFound is returned when the target Event does not exist in the active Organization.
func ErrEventNotFound() apperror.DomainError {
	return apperror.New("EVENT_NOT_FOUND", "Event not found.", nil)
}

// ErrTicketTypeNotFound is returned when a row references a Ticket Type not on the Event.
func ErrTicketTypeNotFound(ticketTypeID string) apperror.DomainError {
	return apperror.New("TICKET_TYPE_NOT_FOUND", "Ticket Type not found on this Event.", map[string]any{
		"ticket_type_id": ticketTypeID,
	})
}

// ErrCapacityExceeded is returned when a checkout requests more tickets of a
// Ticket Type than remain. Available is capacity minus sold count at the moment
// of the check.
func ErrCapacityExceeded(ticketTypeID string, requested, available int) apperror.DomainError {
	return apperror.New("CAPACITY_EXCEEDED", "Not enough tickets remaining.", map[string]any{
		"ticket_type_id": ticketTypeID,
		"requested":      requested,
		"available":      available,
	})
}

// ErrPurchaseLimitExceeded is returned when a checkout would take one Customer
// past a Ticket Type's Purchase Limit: what they already hold plus what they are
// asking for exceeds it (ADR 0025).
//
// It is deliberately NOT CAPACITY_EXCEEDED, though it shares its shape and its
// 409. The two say different things to the buyer — the Event is full, versus you
// already have yours — and a Customer must never be told a Ticket Type is sold
// out when the only thing spent is their own allowance. The Storefront keys its
// copy on the code (ADR 0023), so the distinction has to live here.
//
// alreadyHeld is what the Customer holds at the moment of the check: active
// Ticket Sales plus live Capacity Holds. It may legitimately be at or above the
// limit, because lowering a Purchase Limit is never retroactive — a buyer holding
// three under a limit since dropped to one is a normal state, not a corruption,
// and this error is how they are told so.
func ErrPurchaseLimitExceeded(ticketTypeID string, limit, alreadyHeld, requested int) apperror.DomainError {
	return apperror.New("PURCHASE_LIMIT_EXCEEDED", "You already have the maximum number of these tickets.", map[string]any{
		"ticket_type_id": ticketTypeID,
		"limit":          limit,
		"already_held":   alreadyHeld,
		"requested":      requested,
	})
}

// ErrTicketTypeClosed is returned when a begin-checkout asks for a Ticket Type
// whose Sales Cutoff has passed: the shop window has shut, and the buyer's tab
// was open when it did (ADR 0070).
//
// It is deliberately NOT CAPACITY_EXCEEDED, on the terms ADR 0023 lets the
// Storefront key its copy on the code: a Customer must never be told an Event is
// full when it is half empty. That distinction has to survive a Ticket Type that
// is both closed and exhausted, which is why refuseClosedTicketType runs before
// the capacity check rather than after it.
//
// It names the Ticket Type by the same word its two neighbours use and carries
// nothing else. No `available` figure, because no smaller retry succeeds: a
// closing time is terminal for every buyer until an organizer moves it, and
// inviting a retry would be a lie. The closing INSTANT is not carried either —
// the Storefront already has it from the Event payload that drew the card, and
// the refusal is not the place to teach a second copy of it.
func ErrTicketTypeClosed(ticketTypeID string) apperror.DomainError {
	return apperror.New("TICKET_TYPE_CLOSED", "Sales for this ticket type have closed.", map[string]any{
		"ticket_type_id": ticketTypeID,
	})
}

// ErrPaymentNotFound is returned when no Payment carries the given client
// transaction id.
func ErrPaymentNotFound() apperror.DomainError {
	return apperror.New("PAYMENT_NOT_FOUND", "Payment not found.", nil)
}

// ErrPaymentSaleCommitFailed is returned for the one loudly-logged incident
// case: the Payment Provider approved the charge but recording the Ticket Sale
// failed, leaving an approved Payment without a sale for the platform operator
// to resolve by hand.
func ErrPaymentSaleCommitFailed() apperror.DomainError {
	return apperror.New("PAYMENT_SALE_COMMIT_FAILED", "Your payment was approved but your tickets could not be recorded. Please contact support.", nil)
}

// The Customer-initiated Sale Reversal refusals (ADR 0018). Every one of them
// leaves the Ticket Sale exactly as it was: nothing here is a partial outcome.
//
// The buyer-facing word throughout is "undo". Not "cancel", which belongs to an
// Event's lifecycle status and would read as the show being called off, and not
// "refund", which is wrong for a free claim where no money ever moved.

// ErrTicketSaleNotFound is returned when the Customer Session presented owns no
// Ticket Sale with that id.
//
// One code covers both "no such sale" and "that sale is somebody else's", and
// the message is the same for both. Distinguishing them would turn the endpoint
// into an oracle for whether a given id exists, which is exactly what somebody
// probing other people's purchases wants to know.
func ErrTicketSaleNotFound() apperror.DomainError {
	return apperror.New("TICKET_SALE_NOT_FOUND", "We couldn't find that purchase.", nil)
}

// ErrTicketSaleRefNotFound is returned when no Ticket Sale on the platform
// carries the given Sale Confirmation reference.
//
// It shares TICKET_SALE_NOT_FOUND with the buyer-facing absence above, because
// it is the same absence, and it says so in the words its own reader needs: the
// only caller is a Platform Operator who pasted a reference out of a support
// thread, and what they must be told apart from "no such sale" is nothing. The
// reference is echoed back in details precisely because a mistyped one is the
// likeliest cause, and this surface is operator-only (ADR 0015).
func ErrTicketSaleRefNotFound(confirmationRef string) apperror.DomainError {
	return apperror.New(
		"TICKET_SALE_NOT_FOUND",
		"No Ticket Sale carries that Sale Confirmation reference.",
		map[string]any{"confirmation_ref": confirmationRef},
	)
}

// ErrSaleAlreadyReversed is returned when the Ticket Sale has already been
// undone — by the Customer moments ago on a double submit, or by staff undoing
// a Sale Import.
//
// It is not an error the buyer needs to act on and it is not a failure of their
// request: the world is already how they wanted it. It says so rather than
// pretending to have done the work a second time, because a silent success on
// an already-reversed sale would make a double-press indistinguishable from a
// second reversal that never happened.
func ErrSaleAlreadyReversed() apperror.DomainError {
	return apperror.New("SALE_ALREADY_REVERSED", "This purchase has already been undone.", nil)
}

// ErrSaleNotReversible is returned when the Ticket Sale could never be undone by
// its buyer: it is not an Online Sale, or its Payment cannot be reversed by the
// Payment Provider that collected it.
//
// The two share a code because they share everything that matters to the person
// reading it — this is not yours to undo, and waiting will not change that — and
// because the second is temporary in a way no message should promise. When the
// launch provider's reversal API is integrated, paid sales start reporting
// `reversible` and this refusal simply stops happening to them.
func ErrSaleNotReversible() apperror.DomainError {
	return apperror.New("SALE_NOT_REVERSIBLE", "This purchase can't be undone here. Contact the organizer for help.", nil)
}

// ErrOperatorReversalNotAnOnlineSale is returned when a Platform Operator tries
// to record an out-of-band refund against a Ticket Sale that is not an Online
// Sale (#125).
//
// It shares SALE_NOT_REVERSIBLE with the buyer-facing refusal above because it
// is the same fact — this sale is not one this route can void — and it says so
// in the words its own reader needs. No money for an imported or In-Person Sale
// ever passed through the platform, so there is nothing here for an operator to
// assert about; an imported sale's undo is the batch-level Sale Import undo,
// which is where the refusal points.
func ErrOperatorReversalNotAnOnlineSale(channel string) apperror.DomainError {
	return apperror.New(
		"SALE_NOT_REVERSIBLE",
		"Only an Online Sale can be reversed here — no money for this sale passed through the platform. An imported sale is undone through its Sale Import.",
		map[string]any{"channel": channel},
	)
}

// ErrSaleNotImported is returned when staff try to reverse a single Ticket
// Sale that is not on the `import` channel (#350, ADR 0050).
//
// The mirror of ErrOperatorReversalNotAnOnlineSale, and its own code rather
// than a second message under SALE_NOT_REVERSIBLE: the staff app picks its
// sentence by code, and the sentence SALE_NOT_REVERSIBLE already carries says
// the opposite of what this reader needs to hear. An Online Sale involves the
// platform's money and is the Customer's or a Platform Operator's to reverse
// (ADR 0018, 0019); an In-Person Sale has no route yet.
func ErrSaleNotImported(channel string) apperror.DomainError {
	return apperror.New(
		"SALE_NOT_IMPORTED",
		"Only an imported sale can be reversed here. An Online Sale is reversed by the buyer within the Reversal Window, or by the platform.",
		map[string]any{"channel": channel},
	)
}

// ErrTicketSaleIDNotFound is returned to staff naming a Ticket Sale id that is
// not on this Event. It shares TICKET_SALE_NOT_FOUND with the two absences
// above because it is the same absence; the id is echoed because the only
// caller is a staff app that just read it off a row.
func ErrTicketSaleIDNotFound(saleID string) apperror.DomainError {
	return apperror.New("TICKET_SALE_NOT_FOUND", "That Ticket Sale is not on this Event.", map[string]any{"sale_id": saleID})
}

// ErrRefundedAmountExceedsCollected is returned when a Platform Operator states
// they refunded a buyer more than the buyer ever paid.
//
// The ceiling is the sale's own collected amount, and it is checked here rather
// than in the handler because it is a fact about the sale rather than about the
// request. The figures both travel in details: the operator is most likely a
// digit out, and the number they were measured against is what tells them so.
func ErrRefundedAmountExceedsCollected(refundedCents, collectedCents int, currency string) apperror.DomainError {
	return apperror.New(
		"REFUNDED_AMOUNT_EXCEEDS_COLLECTED",
		"The refunded amount is more than this Ticket Sale collected.",
		map[string]any{
			"refunded_amount_cents": refundedCents,
			"amount_cents":          collectedCents,
			"currency":              currency,
		},
	)
}

// ErrNothingToRefund is returned when a Platform Operator states a money fact
// about a Ticket Sale that collected nothing (#126).
//
// A free Online Sale took no money and was charged no Platform Fee, so both
// halves of the memo are claims about money that never existed — and a zero
// would be worse than a refusal, because the record must keep "nothing to
// refund" distinguishable from "zero refunded". The marking a free sale accepts
// is the one with no money in it at all, which is what the message says.
func ErrNothingToRefund() apperror.DomainError {
	return apperror.New(
		"NOTHING_TO_REFUND",
		"This sale collected nothing, so there was nothing to refund. Record the reversal without a refunded amount or a fee decision.",
		nil,
	)
}

// ErrRefundedAmountRequired is returned when a Platform Operator marks a Ticket
// Sale that DID collect money without saying what the buyer got back (#126).
//
// It is the mirror of ErrNothingToRefund, and lives here for the same reason:
// whether the money facts are required is a fact about the sale, which the
// handler validating the request's shape cannot know. Neither has a default —
// a pre-filled amount invites rubber-stamping and a defaulted fee decision
// would record a revenue choice nobody made (#125) — so the collected amount
// travels in details as the figure the operator is being asked about.
func ErrRefundedAmountRequired(collectedCents int, currency string) apperror.DomainError {
	return apperror.New(
		"REFUNDED_AMOUNT_REQUIRED",
		"This sale collected money. State what the buyer got back and whether the platform kept its fee.",
		map[string]any{
			"amount_cents": collectedCents,
			"currency":     currency,
		},
	)
}

// ErrReversalWindowClosed is returned when the Reversal Window has shut: it is
// past 20:00 Ecuador time on the day of purchase, or the Event has started.
//
// This is the refusal the server owes regardless of what any client believed.
// A Storefront hiding its Undo button is a courtesy; this is the enforcement,
// and it is checked against the server's own clock on every request.
func ErrReversalWindowClosed() apperror.DomainError {
	return apperror.New("REVERSAL_WINDOW_CLOSED", "The time to undo this purchase has passed. Contact the organizer for help.", nil)
}

// ErrSaleReversalFailed is returned when the Payment Provider refused to reverse
// a Payment it could normally reverse.
//
// The provider's own code goes to the log and never to the buyer (ADR 0018):
// PayPhone's catalogue has no "too late" code, so any explanation this endpoint
// offered would be a guess, and guessing wrong about somebody's money is worse
// than saying less. What the buyer gets instead is the truth that nothing
// changed, their Sale Confirmation reference, and a person to ask.
func ErrSaleReversalFailed(confirmationRef string) apperror.DomainError {
	return apperror.New(
		"SALE_REVERSAL_FAILED",
		"We couldn't undo this purchase. Nothing has changed — contact the organizer with your confirmation reference.",
		map[string]any{"confirmation_ref": confirmationRef},
	)
}

// ErrReversalUnresolved is returned to a Customer pressing Undo on a sale whose
// Reversal Request became an Unresolved Reversal: the platform asked the Payment
// Provider and never learned what it did, and has stopped asking (ADR 0024).
//
// It is a refusal rather than a fresh ask, and that is the whole point. The
// money's state is UNKNOWN — it may already be on its way back — so posting
// another reversal is the double-refund this feature exists to avoid, and it is
// exactly what a second press would otherwise do once the sale reads as
// untouched again. Nothing here reaches the provider.
//
// It is not ErrSaleAlreadyReversed, which would claim their money is back, and
// not the pending result, which would promise it is coming: both are guesses
// about somebody's money, and the platform is in this state precisely because it
// cannot make one. The message says what is true — somebody is looking into it —
// and hands them the Sale Confirmation reference the operator settling it (ADR
// 0019) works from.
func ErrReversalUnresolved(confirmationRef string) apperror.DomainError {
	return apperror.New(
		"REVERSAL_UNRESOLVED",
		"We're still checking with the payment provider what happened to this refund. Nothing has changed here — contact the organizer with your confirmation reference.",
		map[string]any{"confirmation_ref": confirmationRef},
	)
}

// ErrSaleReversalInProgress is returned to a Platform Operator whose Operator
// Reversal contended with a reversal already in flight on the same Ticket Sale —
// the buyer's own undo, or the Reversal Reconciler mid-probe (ADR 0024).
//
// It is deliberately not ErrSaleAlreadyReversed. The contending attempt may yet
// be refused by the Payment Provider, in which case the sale is still active and
// "already reversed" would have been false; and unlike a buyer, who has one
// reversal happening and no second one to make, an operator is asserting a
// refund that already happened off-platform and whose claim is just as true a
// moment later. Retrying is the right advice and the only honest one.
func ErrSaleReversalInProgress(confirmationRef string) apperror.DomainError {
	return apperror.New(
		"SALE_REVERSAL_IN_PROGRESS",
		"A reversal of this sale is already being processed. Try again in a moment.",
		map[string]any{"confirmation_ref": confirmationRef},
	)
}

// ErrPayoutRequestExceedsPayableBalance is returned when an Organization asks
// for more than has cleared (ADR 0026).
//
// The cap binds the Organization and NOT the operator, and the asymmetry is the
// whole point. A Payout is a record of money that already moved, so recording
// one is never refused for exceeding any balance — an endpoint that refused
// would make the books lie to protect a workflow (ADR 0015, ADR 0019). A request
// is a claim about money that has not moved, so refusing an impossible one costs
// nothing and is kinder than letting an organizer wait three days to be told no
// by a human.
//
// It carries both figures because the refusal is only actionable with both: the
// organizer needs to see what they asked for beside what they may ask for, and
// the difference is the sentence the UI writes for them.
func ErrPayoutRequestExceedsPayableBalance(requestedCents, payableCents int, currency string) apperror.DomainError {
	return apperror.New(
		"PAYOUT_REQUEST_EXCEEDS_PAYABLE_BALANCE",
		// "Payable Balance", never "available balance": the glossary names the
		// concept and lists that synonym under _Avoid_, and this is the one
		// sentence an organizer reads when the feature says no.
		"You can only request up to your Payable Balance.",
		map[string]any{
			"requested_cents":       requestedCents,
			"payable_balance_cents": payableCents,
			"currency":              currency,
		},
	)
}

// ErrPayoutRequestNotFound is returned when the target Payout Request is not one
// of the acting Organization's. Another Organization's request is "not found"
// and never "forbidden": which Organizations have asked to be paid is not a fact
// this surface leaks.
func ErrPayoutRequestNotFound() apperror.DomainError {
	return apperror.New("PAYOUT_REQUEST_NOT_FOUND", "Payout Request not found.", nil)
}

// ErrPayoutRequestNotPending is returned when a cancellation reaches a request
// that is no longer the Organization's to withdraw. All four end states are
// final (ADR 0026), so this is a 409 the caller cannot retry into success rather
// than a silent no-op — the current status is named so the asker learns what
// actually happened to it.
//
// A `processing` request gets a DIFFERENT SENTENCE, and that difference is the
// point of the branch. It has not been resolved: an operator has submitted the
// transfer and the bank has not confirmed it, so telling the organizer their ask
// was "already resolved" would be false, and telling them a status code would
// leave them to guess whether their money is coming. What they need to know is
// that it is on its way and cannot be called back — a cancellation here would
// withdraw an ask that is thirty seconds from landing, leaving a confirmed
// transfer with nothing to attach it to (ADR 0026 amendment). The 48 hours is
// the provider's advertised worst case and is what makes the sentence
// checkable rather than a shrug.
//
// The CODE is deliberately the same. It is the same refusal — this request is
// not yours to cancel — and a second code would make every caller branch on two
// things to render one banner. The status in details is what a caller keys on.
func ErrPayoutRequestNotPending(status string) apperror.DomainError {
	message := "This Payout Request has already been resolved."
	if status == PayoutRequestProcessing {
		message = "Your transfer is already being processed and can no longer be cancelled. " +
			"It can take up to 48 hours to reach the account."
	}
	return apperror.New("PAYOUT_REQUEST_NOT_PENDING", message, map[string]any{
		"status": status,
	})
}

// ErrPayoutRequestTransferAlreadySubmitted is returned to a Platform Operator
// whose decline — or whose own mark-as-processing — reached a request whose
// transfer another operator has already submitted (#184, ADR 0026 amendment).
//
// It is deliberately NOT ErrPayoutRequestAlreadyResolved, whose whole message is
// about a request that ENDED and about recording a Payout directly. Neither
// sentence is true here: nothing has ended, no Payout may exist yet for a
// transfer the bank has not confirmed, and telling an operator to record one
// directly would put money in the ledger that might still come back — the one
// thing the `processing` state exists to prevent.
//
// The platform cannot refuse an ask its own operator is already acting on. The
// answer to a transfer that bounces is `failed` with a reason, not `declined`,
// and the answer to one that lands is a Payout.
//
// It names who submitted, because that is what turns "somebody got there first"
// into a colleague to go and ask about a transfer nobody can see from here.
func ErrPayoutRequestTransferAlreadySubmitted(submittedBy *string) apperror.DomainError {
	who := "another operator"
	if submittedBy != nil && *submittedBy != "" {
		who = *submittedBy
	}
	details := map[string]any{"status": PayoutRequestProcessing}
	if submittedBy != nil {
		details["transfer_submitted_by"] = *submittedBy
	}
	return apperror.New(
		"PAYOUT_REQUEST_TRANSFER_ALREADY_SUBMITTED",
		fmt.Sprintf(
			"The transfer for this Payout Request was already submitted by %s and is being processed. "+
				"Wait for the bank to confirm it — then record the Payout that answers it.",
			who,
		),
		details,
	)
}

// ErrPayoutRequestTransferNotSubmitted is returned to a Platform Operator who
// marks a request `failed` when no transfer was ever submitted for it (#185,
// ADR 0026 amendment).
//
// It is the mirror of the refusal above, and it exists for the same reason that
// one does: `failed` is the bank's answer to a transfer, so a request nobody
// submitted a transfer for cannot have had one bounce. Recording it would be an
// operator refusing an untouched ask in a word that blames the bank — which the
// organizer reads as "your account number is wrong" about an account nobody
// tried to pay, and which makes every failure figure count refusals as bank
// errors.
//
// It is deliberately NOT ErrPayoutRequestAlreadyResolved, which would be false
// on the ordinary way of arriving here: a `pending` request has not been
// resolved by anybody, and that message would name "another operator" for an
// event that never happened while telling the reader to record a Payout for
// money nobody sent.
//
// The way out is named, because it is a different button and the operator is one
// click from the wrong one: an untouched ask an operator wants to refuse is
// DECLINED, with a judgement and their name on it. The current status travels in
// details so a caller can key on it; it is `pending` in the case worth having a
// message for, and one of the end states when somebody got there first.
func ErrPayoutRequestTransferNotSubmitted(status string) apperror.DomainError {
	return apperror.New(
		"PAYOUT_REQUEST_TRANSFER_NOT_SUBMITTED",
		fmt.Sprintf(
			"No transfer has been submitted for this Payout Request (it is %s), so there is none to have failed. "+
				"Mark it processing once you submit one — or decline it, if the answer is no.",
			status,
		),
		map[string]any{"status": status},
	)
}

// ErrPayoutRequestAlreadyResolved is returned to a Platform Operator whose
// fulfilment or decline reached a request that had already ended (#177,
// ADR 0026).
//
// It is deliberately NOT ErrPayoutRequestNotPending, which the Organization's
// own cancellation uses. The two refusals are about the same fact and are read
// by different people with different next moves, and this one's message is the
// whole of a mitigation the system has no other defence for.
//
// THE COMPARE-AND-SWAP PREVENTS A DOUBLE RECORD, NOT A DOUBLE TRANSFER. Two
// operators can both wire the money at the bank; only one of them can write the
// Payout through this route, and the other's transaction is rolled back so no
// orphan Payout survives. If that loser walks away, the books understate what
// actually left the account — silently, and with no figure anywhere saying so.
// So the message tells them to record the Payout directly, which is a path that
// is unconditional by design and stays so (ADR 0019). Anything vaguer, or a
// generic conflict, would drop a real payment on the floor.
//
// It names who got there first because that is what turns "somebody beat you"
// into a person to go and ask. resolvedBy is a pointer only because the column
// is nullable; the schema's payout_requests_resolution_matches_status CHECK
// makes it present on every resolved row, and the fallback wording exists so a
// hand-edited row cannot produce a sentence with a hole in it.
func ErrPayoutRequestAlreadyResolved(status string, resolvedBy *string) apperror.DomainError {
	who := "another operator"
	if resolvedBy != nil && *resolvedBy != "" {
		who = *resolvedBy
	}
	details := map[string]any{"status": status}
	if resolvedBy != nil {
		details["resolved_by"] = *resolvedBy
	}
	return apperror.New(
		"PAYOUT_REQUEST_ALREADY_RESOLVED",
		fmt.Sprintf(
			"This Payout Request was already resolved by %s (%s), and nothing was recorded here. "+
				"If you also transferred the money, record the Payout directly against the Organization — "+
				"otherwise the books will understate what left the account.",
			who, status,
		),
		details,
	)
}

// ErrImportFileUnreadable is returned when an uploaded Sale Import file cannot be
// parsed (wrong format, missing columns, missing Sales sheet, corrupt or empty
// contents). The reason is a human-readable sentence produced by the importfile
// parser; it becomes the message the organizer sees so both the preview UI and
// any API consumer get the specific dead-end without translating error codes.
func ErrImportFileUnreadable(reason string) apperror.DomainError {
	return apperror.New("IMPORT_FILE_INVALID", reason, map[string]any{
		"reason": reason,
	})
}

// ErrImportFileTooLarge is returned when an uploaded Sale Import file exceeds the
// per-batch row limit. The message states the limit; details carries it too.
func ErrImportFileTooLarge(limit int) apperror.DomainError {
	return apperror.New("IMPORT_FILE_TOO_LARGE", fmt.Sprintf("Import file has too many rows. The maximum is %d rows per import.", limit), map[string]any{
		"limit": limit,
	})
}

// ErrImportBatchNotFound is returned when the target Sale Import batch does not
// exist on the Event.
func ErrImportBatchNotFound(batchID string) apperror.DomainError {
	return apperror.New("IMPORT_BATCH_NOT_FOUND", "Sale Import batch not found on this Event.", map[string]any{
		"batch_id": batchID,
	})
}

// ErrImportNotLatestBatch is returned when an undo targets a batch that is not
// the most recent one on the Event. Only the latest batch is reversible.
func ErrImportNotLatestBatch(batchID string) apperror.DomainError {
	return apperror.New("IMPORT_NOT_LATEST_BATCH", "Only the most recent Sale Import can be undone.", map[string]any{
		"batch_id": batchID,
	})
}

// ErrImportAlreadyReversed is returned when an undo targets a batch that has
// already been reversed. Reversal is not repeatable.
func ErrImportAlreadyReversed(batchID string) apperror.DomainError {
	return apperror.New("IMPORT_ALREADY_REVERSED", "This Sale Import has already been undone.", map[string]any{
		"batch_id": batchID,
	})
}

// ErrImportBatchFailed is returned when a Sale Import cannot be committed as a
// whole. The batch is all-or-nothing; details identify the first offending row
// and the reason (e.g. CAPACITY_EXCEEDED).
func ErrImportBatchFailed(row int, reason string, details map[string]any) apperror.DomainError {
	d := map[string]any{"row": row, "reason": reason}
	for k, v := range details {
		d[k] = v
	}
	return apperror.New("IMPORT_BATCH_FAILED", "Import could not be completed.", d)
}

// The Sale Re-addressing's refusals (#420, ADR 0058). Each is its own code
// rather than one SALE_NOT_RE_ADDRESSABLE with a reason in details, because
// the staff app picks its sentence by code and an Operator reading "cannot be
// re-addressed" needs to know which of four different facts stands in the way
// — three of which they can do nothing about and one of which (the address)
// they can.

// ErrSaleNotReAddressable is returned when the Ticket Sale is not an Online
// Sale. An imported Sale is corrected by Sale Correction (ADR 0050) and a door
// sale has no buyer surface waiting to be unlocked; the Operator's two levers
// share one channel rule.
func ErrSaleNotReAddressable(channel string) apperror.DomainError {
	return apperror.New(
		"SALE_NOT_RE_ADDRESSABLE",
		"Only an Online Sale can be re-addressed. An imported sale is corrected through its Sale Import.",
		map[string]any{"channel": channel},
	)
}

// ErrReAddressingEventStarted is returned when the Sale's Event has already
// started. Past the doors "give me my tickets" has no meaning; only the money
// question remains, which the Operator Reversal answers.
func ErrReAddressingEventStarted() apperror.DomainError {
	return apperror.New(
		"RE_ADDRESSING_EVENT_STARTED",
		"This sale's event has already started, so it can no longer be re-addressed.",
		nil,
	)
}

// ErrReAddressingSameAddress is returned when the corrected address normalises
// to the address the Sale already carries: a no-op is never recorded and never
// mailed.
func ErrReAddressingSameAddress(email string) apperror.DomainError {
	return apperror.New(
		"RE_ADDRESSING_SAME_ADDRESS",
		"That is already the address this sale is addressed to.",
		map[string]any{"email": email},
	)
}

// ErrReAddressingNothingPending is returned when the Operator withdraws and
// the Sale carries no pending re-addressing: nothing was recorded, or what was
// recorded has already been accepted, withdrawn, replaced or has expired. There
// is nothing to end, and nothing is written.
func ErrReAddressingNothingPending() apperror.DomainError {
	return apperror.New(
		"RE_ADDRESSING_NOTHING_PENDING",
		"No re-addressing of this sale is pending.",
		nil,
	)
}

// The Re-addressing Link's refusals (#421, ADR 0058). Two codes and not one,
// because the page keys its sentence on the code and the two are different
// facts to the person reading it: a link that never was, and a link that was
// real once and is not now.

// ErrReAddressingLinkInvalid is returned when the token was tampered with,
// truncated, invented, signed for another purpose or by another deployment,
// names a record that does not exist, or names an instant the record was not
// minted at (a link an earlier recording produced, replaced since).
//
// One code for all of them, on the Assignment Link's rule: whoever holds a
// link that does not work is entitled to learn nothing beyond that.
func ErrReAddressingLinkInvalid() apperror.DomainError {
	return apperror.New("RE_ADDRESSING_LINK_INVALID", "This link is not valid.", nil)
}

// ReAddressingLinkNoLongerValidReason names why a link that once opened no
// longer does — the value carried in RE_ADDRESSING_LINK_NO_LONGER_VALID's
// details.reason.
const (
	// ReAddressingLinkWithdrawn: the Operator withdrew the record, or replaced
	// it with another (#423).
	ReAddressingLinkWithdrawn = "withdrawn"
	// ReAddressingLinkSaleReversed: the Sale was reversed while the record was
	// pending, so there is nothing left to accept.
	ReAddressingLinkSaleReversed = "sale_reversed"
	// ReAddressingLinkEventStarted: the Event's doors have opened, and "give me
	// my tickets" has no meaning past them (ADR 0058).
	ReAddressingLinkEventStarted = "event_started"
)

// ErrReAddressingLinkNoLongerValid is returned when a genuine link names a
// record that has ended without being accepted: withdrawn or replaced by the
// Operator, or expired because the Sale was reversed or the Event started.
//
// Told apart from ErrReAddressingLinkInvalid because the reader IS the buyer —
// the corrected address is the address the buyer meant — and a buyer is
// entitled to know that the purchase they were told to accept can no longer be
// accepted, as against being told their link is broken. The reason travels in
// details so the page can say which, and nothing else about the Sale does.
func ErrReAddressingLinkNoLongerValid(reason string) apperror.DomainError {
	return apperror.New(
		"RE_ADDRESSING_LINK_NO_LONGER_VALID",
		"This link is no longer valid. Contact the organizer with your confirmation reference if you still need help.",
		map[string]any{"reason": reason},
	)
}

// ErrReAddressingLinkUnavailable is returned when the deployment cannot accept
// a Re-addressing Link at all: no link secret to verify with, or no Customer
// writer or sign-in wired into the service. A deployment fault and never the
// reader's, so it is a 500 — refusing beats moving a paid Sale to nobody.
func ErrReAddressingLinkUnavailable() apperror.DomainError {
	return apperror.New("RE_ADDRESSING_LINK_UNAVAILABLE", "Re-addressing links are not available right now.", nil)
}
