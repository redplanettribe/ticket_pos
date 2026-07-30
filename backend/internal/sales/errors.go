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
