package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/invoicing/ride"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Delivery (#475, parent #471, ADR 0060): the Drainer's last step, where an
// authorized document reaches the buyer it was issued to. One mail in the
// Sale's Locale through the transactional sender, the signed XML and its
// RIDE attached (#496, ADR 0062), a link to the Sale's card in the Customer
// Area, and delivered_at recorded only once the sender took it.
//
// THE AUTHORIZATION IS NEVER TOUCHED BY WHAT THE MAIL DOES. A sender that
// refuses leaves the document authorized and undelivered, due again on the
// same ladder for delivery alone (repository.ClaimDueInvoice claims it by
// that pair of facts); a sender that took it makes MarkDelivered's guard the
// reason it is never sent twice. Nothing here asks the authority anything.
//
// THE LADDER IS FOR THE TRANSPORT ALONE (ADR 0062 §2). The RIDE is rendered
// before anything is sent, and a RIDE that cannot be rendered is a
// deterministic failure over stored data — a bug, not weather — so it does
// not retry: the document stays authorized and undelivered and leaves the
// queue, the same dead end as a document with no Sale or no signed bytes,
// logged for an operator. Neither does the platform degrade to mailing the
// XML by itself: delivered_at vouches for a compliant delivery, both files,
// or for nothing.
//
// KIND-AGNOSTIC BY CONSTRUCTION. What is mailed is whatever document the
// row is — the Sale Invoice, the Credit Note — and the only place the kind
// matters is the copy's choice of words, which platform.TaxDocumentDelivery
// keys on Kind and, for a Credit Note, on its Reason (#481): a reissue's
// says a correction follows, a reversal's says the sale was reversed.

// customerAreaPath is the Customer Area on the Storefront, and the fragment
// the Sale's card carries there (apps/storefront/lib/destination.ts,
// ticketSaleAnchorId). Spelled here as the Answer Reminder spells the Area
// and the Digest spells "/tickets": a link into the Area behind a sign-in,
// never a Confirmation Link.
const customerAreaPath = "/tickets"

// deliverDocument mails one authorized document to its buyer and records
// the delivery, or reschedules delivery on the ladder when the sender
// refused. It reports whether the buyer was mailed; the error is the
// database's alone, since a refused send is an ordinary outcome here.
func (s *Service) deliverDocument(ctx context.Context, row *repository.InvoiceRow) (bool, error) {
	inv := &row.Invoice
	if inv.Status != invoicing.InvoiceStatusAuthorized || inv.DeliveredAt != nil {
		return false, nil
	}
	now := s.clock()

	facts, err := s.repo.GetSaleDeliveryFacts(ctx, inv.TicketSaleID)
	if err != nil {
		return false, err
	}
	if facts == nil || row.Ecuador == nil || !inv.Signed() {
		// Nothing a retry would change; it stays authorized and undelivered
		// for an operator to see, and off the queue.
		s.logger.Error("invoicing: an authorized document cannot be delivered; it has no Sale or no signed bytes",
			"invoice_id", inv.ID, "kind", inv.Kind)
		return false, s.rescheduleDelivery(ctx, inv, nil, now)
	}

	// The RIDE, before the send: the mail carries both files or goes out
	// not at all. A render failure is the same dead end as above — off the
	// queue, not on the ladder — because retrying a deterministic render
	// hourly forever would only hide the bug.
	rideDoc, err := ride.Render(inv, row.Ecuador)
	if err != nil {
		s.logger.Error("invoicing: an authorized document cannot be delivered; its RIDE could not be rendered",
			"invoice_id", inv.ID, "kind", inv.Kind, "error", err)
		return false, s.rescheduleDelivery(ctx, inv, nil, now)
	}

	// A reissue's Credit Note promises a corrected factura. When the Sale was
	// reversed while that nota was still unanswered (#484), the corrected
	// factura was withdrawn and the reversal's own Credit Note stood down
	// because this one already credits the factura — so this mail is the
	// buyer's only word that the purchase is undone, and it reads as a
	// reversal's would. The document itself is unchanged: the SRI holds the
	// correction motivo it was signed with.
	reason := inv.CreditNoteReason
	if reason == invoicing.CreditNoteReasonReissue && facts.Reversed {
		reason = ""
	}
	delivery := platform.TaxDocumentDelivery{
		To:           inv.Recipient.Email,
		Kind:         string(inv.Kind),
		Reason:       reason,
		Supersedes:   inv.SupersedesInvoiceID != "",
		CustomerName: inv.Recipient.LegalName,
		EventName:    facts.EventName,
		Reference:    inv.SaleConfirmationRef,
		Attachments: []platform.EmailAttachment{
			{
				Filename:    row.Ecuador.AccessKey + ".xml",
				ContentType: invoicing.ContentTypeXML,
				Body:        inv.SignedXML,
			},
			{
				Filename:    rideDoc.Filename,
				ContentType: rideDoc.ContentType,
				Body:        rideDoc.Body,
			},
		},
		Locale: platform.ResolveMailLocale(facts.SaleLocale, facts.CustomerLocale),
	}
	if s.storefrontBaseURL != "" {
		delivery.CustomerAreaURL = s.storefrontBaseURL + customerAreaPath + "#sale-" + inv.TicketSaleID
	}

	if err := s.email.SendTaxDocumentDelivery(ctx, delivery); err != nil {
		// The ladder from the signing instant, as every other retry of this
		// document: a minute, five, fifteen, then hourly.
		next := now.Add(ladderDelay(now.Sub(inv.IssuedAt)))
		s.logger.Warn("invoicing: tax document delivery failed; due again on the ladder",
			"invoice_id", inv.ID, "kind", inv.Kind, "next_attempt_at", next, "error", err)
		return false, s.rescheduleDelivery(ctx, inv, &next, now)
	}
	if err := s.repo.MarkDelivered(context.WithoutCancel(ctx), inv.ID, now); err != nil {
		return true, err
	}
	s.logger.Info("invoicing: tax document delivered", "invoice_id", inv.ID, "kind", inv.Kind, "locale", delivery.Locale)
	return true, nil
}

// rescheduleDelivery writes when delivery is next due — or that it never
// is — on a document that is still authorized; one that is not any more
// (nothing moves an authorized document but a later ticket) is left to
// whoever moved it.
func (s *Service) rescheduleDelivery(ctx context.Context, inv *invoicing.Invoice, next *time.Time, now time.Time) error {
	written, err := s.repo.Reschedule(context.WithoutCancel(ctx), inv.ID, inv.Status, inv.Status, nil, next, now)
	if err != nil {
		return err
	}
	if !written {
		s.logger.Info("invoicing: the document moved during its delivery; its schedule was not written", "invoice_id", inv.ID, "read_as", inv.Status)
	}
	return nil
}
