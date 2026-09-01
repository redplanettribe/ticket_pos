package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
)

// Issue again (#580, parent #575, ADR 0068): the Platform Operator's second
// press, which owes a Ticket Sale a fresh Sale Invoice when the one it had
// is terminally dead.
//
// WHY IT EXISTS. Abandon (#578) records that the authority never took a
// document and never will; Mark annulled (#477) records that it held one
// and the operator disowned it by hand at the portal. Either way the Sale is
// left with no factura and, until this, nothing that would ever owe it
// another — the dead end raised as #480, closed `completed` on 2026-08-31
// and never built. Production's 001-001-000000025 and 26 are the case that
// forced it: two paid House sales whose buyers hold no valid tax document.
//
// IT IS NOT WELDED TO ABANDON, deliberately. Abandoning without reissuing
// stays legal — a manual Tax Invoice, or a Sale the operator does not want
// reinvoiced — and welding the two would make the ledger assert both claims
// at one instant when the reissue is often a later decision, honestly made
// days apart. Two presses, two records.
//
// IT IS NOT A SPECIAL SIGNING ROUTE, and this is the whole of its design.
// It inserts ONE owed Sale Invoice through the same repository path a
// checkout's is born by (OweInvoice), inside a transaction, with no number,
// no clave de acceso and no signature. The Drainer claims it on a later
// round exactly as it claims any owed document and consumes a FRESH
// secuencial when it signs. The Drainer learns nothing new here; the
// abandoned number is never handed out again, because the sequence only
// moves forward and nothing about the dead row is touched.
//
// NO CREDIT NOTE. A reissue owes one because it corrects an AUTHORIZED
// factura, and the authority must see the cancellation before the
// replacement. A terminally dead document has nothing to cancel: an
// abandoned one was never a legal document, and an annulled one was already
// discharged by hand at the portal. That absence is why Issue again reaches
// the Sale the reissue cannot — #579 recorded that a reissue from a dead
// chain correctly answers INVOICE_ALREADY_CREDITED, because a second Credit
// Note against a factura an authorized one already credits is exactly the
// double-credit ADR 0061 forbids. Issue again owes a factura and nothing
// else, so that refusal never arises.
//
// THE CHAIN IS ADR 0061'S, REUSED (ADR 0068). The replacement carries
// supersedes_invoice_id to the document it replaces, and the trail — who,
// when, the optional note — in reissued_by / reissued_at / reissue_note.
// One relation, because "what became of this document" is one question with
// one answer to read: a parallel link would have to be zipped together by
// the detail, the list, the Sale lookup, the buyer's documents and every
// reader after them. Everything downstream therefore shows the chain in
// both directions with no change at all, and the chain survives any number
// of hops — a replacement that is itself abandoned is replaced again, and
// each link still names the one before it.
//
// READ-THEN-GUARDED-WRITE, as the reissue and Mark annulled are. The
// refusals are decided on the row as read, and decided AGAIN inside the
// transaction on the same facts under the Sale's lock
// (repository.IssueSaleInvoiceAgain), so a press that races a reversal — or
// a Drainer write, or a second operator — finds what then stands and
// refuses rather than double-writing. The pre-read exists so that an
// ordinary refusal costs no lock.

// IssueAgainInput is the act's trail: who pressed, from the session, and
// their optional note. There is no Recipient, and that is the ruling rather
// than an omission — #480 left "corrected, or carried over" open and ADR
// 0068 closes it at carried over. Issue again replaces a document the
// authority never made legal; it corrects nothing, and a Recipient the
// operator wants changed is the reissue's business once the replacement is
// authorized.
type IssueAgainInput struct {
	IssuedAgainBy string
	Note          string
}

// IssueSaleInvoiceAgain owes the replacement Sale Invoice, kicks the
// Drainer as a checkout does, and returns the replacement.
func (s *Service) IssueSaleInvoiceAgain(ctx context.Context, id string, in IssueAgainInput) (*InvoiceDetail, error) {
	if !s.saleInvoicingEnabled {
		return nil, invoicing.ErrSaleInvoicingUnavailable()
	}
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	if err := issuableAgainDocument(&row.Invoice); err != nil {
		return nil, err
	}

	now := s.clock()
	replacementID, err := s.repo.IssueSaleInvoiceAgain(ctx, id, func(dead *repository.InvoiceRow, facts repository.IssueAgainSaleFacts) (invoicing.Invoice, error) {
		if err := issueAgainRefusal(&dead.Invoice, facts); err != nil {
			return invoicing.Invoice{}, err
		}
		return replacementSaleInvoiceOf(&dead.Invoice, in, now), nil
	})
	if err != nil {
		return nil, err
	}
	if replacementID == "" {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	// Counts and ids only: no Recipient, no buyer (#471 story 35).
	s.logger.Info("invoicing: sale invoice issued again",
		"replaced_invoice_id", id, "replacement_invoice_id", replacementID,
		"replaced_status", row.Invoice.Status, "issued_again_by", in.IssuedAgainBy, "has_note", in.Note != "")
	s.KickSaleInvoiceDrainer(ctx, row.Invoice.TicketSaleID)
	return s.GetInvoice(ctx, replacementID)
}

// issuableAgainDocument is what can be refused from the document's own row:
// its kind, then its state. The order is the reissue guard's, and for the
// same reason — what a document IS never depends on what happened to it.
//
// A manual Tax Invoice is typed again by hand; a Credit Note is never
// re-owed; and only a terminally dead Sale Invoice is replaced, which means
// `abandoned` or `annulled` and nothing else. `withdrawn` is the third
// death and is deliberately excluded: a document is withdrawn because its
// Sale was reversed or the Credit Note it followed died, and neither of
// those Sales is owed a fresh factura. Everything else — owed, pending,
// parked, refused — is still on its way somewhere, and an authorized
// document is the Sale's factura, corrected by the reissue (ADR 0061) and
// never replaced.
func issuableAgainDocument(inv *invoicing.Invoice) error {
	switch inv.Kind {
	case invoicing.DocumentKindManual:
		return invoicing.ErrInvoiceManualNotIssuableAgain()
	case invoicing.DocumentKindCreditNote:
		return invoicing.ErrCreditNoteNotIssuableAgain()
	}
	switch inv.Status {
	case invoicing.InvoiceStatusAbandoned, invoicing.InvoiceStatusAnnulled:
		return nil
	}
	return invoicing.ErrInvoiceNotTerminallyDead(inv.Status)
}

// issueAgainRefusal is the whole decision, on the dead document as locked
// and the Sale's facts: the document's own refusals, then the Sale's.
//
// A reversed Sale is answered first among the Sale's facts, as the
// reissue's guard answers it: income that no longer stands is never
// declared at all, so whether the document also has a replacement is beside
// the point. Then the live replacement, which is the invariant this act
// could otherwise break — one Sale, one factura owed at a time.
//
// THE REPLACEMENT IS REFUSED ON THE SALE'S FACTURA, NOT THE DOCUMENT'S
// SUCCESSOR. Both are asked, and they answer different questions. The
// document's own successor is the direct case and carries the plainer
// story — "this document has already been replaced, open the replacement"
// — but it agrees with the invariant only on a chain one hop long: with 1
// replaced by 2 and 2 replaced by a live 3, document 1's successor is dead,
// so 1 reads unreplaced while its Sale is perfectly well invoiced. The
// Sale's fact is what actually holds the line #575 story 41 draws, that the
// buyer ends with exactly one valid factura.
//
// LIVE IS #579'S LIVE throughout. A replacement that is itself withdrawn,
// annulled or abandoned stands for nothing, so a Sale whose every document
// has died is issued again — the second hop, and the first behavioural
// exercise migration 120's widened index has had. The new live successor is
// written into a slot a dead row still occupies in the old index, and the
// write proves the new one.
func issueAgainRefusal(inv *invoicing.Invoice, facts repository.IssueAgainSaleFacts) error {
	if err := issuableAgainDocument(inv); err != nil {
		return err
	}
	switch {
	case facts.SaleStatus == "reversed":
		return invoicing.ErrInvoiceSaleReversed()
	case inv.SupersededByInvoiceID != "", facts.SaleHasLiveFactura:
		return invoicing.ErrInvoiceAlreadyReplaced()
	}
	return nil
}

// replacementSaleInvoiceOf is the fresh Sale Invoice Issue again owes: the
// dead document's own document minus everything that needs an Issuer, of
// kind sale, naming the document it replaces and carrying the act's trail.
//
// COPIED FROM THE DEAD ROW AND FROM NOWHERE ELSE. The Recipient — Tax ID,
// legal name, address and email — the lines, the totals, the rate, the
// currency and the payment method are the ones the authority was asked to
// take, verbatim. Reinvoicing must not silently change what was sold or to
// whom (#575 story 23), and the Sale, the Customer and the catalog may all
// have moved since. This is where Issue again parts from the reissue, which
// takes the Recipient as the operator has just typed it and the email the
// Sale carries at that instant: a reissue exists to CORRECT the Recipient,
// and this exists to reach an authority that never took the document.
//
// UNSIGNED, AND DUE AT ONCE. No Issuer, no environment, no number, no clave
// and no bytes: the Drainer fills those in on a later round and allocates a
// fresh secuencial then, which is what keeps the abandoned number consumed
// and unreissued. NextAttemptAt is now because nothing gates the
// replacement — there is no Credit Note for it to wait behind, so unlike a
// reissue's corrected factura it is claimable the moment it exists.
//
// THE TRAIL IS THE REISSUE'S COLUMNS, and deliberately so: one chain, one
// set of columns, one lateral join already reading them beside every
// document a replacement concerns (ADR 0068). ReissuedBy is the operator
// who pressed Issue again, ReissuedAt the instant, ReissueNote their words.
//
// Pure, so the copy is testable without a database.
func replacementSaleInvoiceOf(dead *invoicing.Invoice, in IssueAgainInput, now time.Time) invoicing.Invoice {
	replacement := invoicing.Invoice{
		Kind:          invoicing.DocumentKindSale,
		Country:       dead.Country,
		Status:        invoicing.InvoiceStatusOwed,
		TicketSaleID:  dead.TicketSaleID,
		IVARate:       dead.IVARate,
		Recipient:     dead.Recipient,
		Currency:      dead.Currency,
		SubtotalCents: dead.SubtotalCents,
		DiscountCents: dead.DiscountCents,
		IVACents:      dead.IVACents,
		TotalCents:    dead.TotalCents,
		PaymentMethod: dead.PaymentMethod,

		NextAttemptAt:       &now,
		SupersedesInvoiceID: dead.ID,
		ReissuedBy:          in.IssuedAgainBy,
		ReissuedAt:          &now,
		ReissueNote:         in.Note,
	}
	replacement.Lines = append(replacement.Lines, dead.Lines...)
	return replacement
}
