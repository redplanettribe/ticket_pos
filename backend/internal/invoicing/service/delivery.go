package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Delivery (#475, parent #471, ADR 0060): the Drainer's last step, where an
// authorized document reaches the buyer it was issued to. One mail in the
// Sale's Locale through the transactional sender, the signed XML attached,
// a link to the Sale's card in the Customer Area, and delivered_at recorded
// only once the sender took it.
//
// THE AUTHORIZATION IS NEVER TOUCHED BY WHAT THE MAIL DOES. A sender that
// refuses leaves the document authorized and undelivered, due again on the
// same ladder for delivery alone (repository.ClaimDueInvoice claims it by
// that pair of facts); a sender that took it makes MarkDelivered's guard the
// reason it is never sent twice. Nothing here asks the authority anything.
//
// KIND-AGNOSTIC BY CONSTRUCTION. What is mailed is whatever document the
// row is — the Sale Invoice today, the Credit Note when its ticket lands —
// and the only place the kind matters is the copy's choice of words, which
// platform.TaxDocumentDelivery keys on Kind. The Credit Note rides this
// unchanged.

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
		return false, s.repo.Reschedule(context.WithoutCancel(ctx), inv.ID, inv.Status, nil, nil, now)
	}

	delivery := platform.TaxDocumentDelivery{
		To:           inv.Recipient.Email,
		Kind:         string(inv.Kind),
		CustomerName: inv.Recipient.LegalName,
		EventName:    facts.EventName,
		Reference:    inv.SaleConfirmationRef,
		Attachment: platform.EmailAttachment{
			Filename:    row.Ecuador.AccessKey + ".xml",
			ContentType: invoicing.ContentTypeXML,
			Body:        inv.SignedXML,
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
		return false, s.repo.Reschedule(context.WithoutCancel(ctx), inv.ID, inv.Status, nil, &next, now)
	}
	if err := s.repo.MarkDelivered(context.WithoutCancel(ctx), inv.ID, now); err != nil {
		return true, err
	}
	s.logger.Info("invoicing: tax document delivered", "invoice_id", inv.ID, "kind", inv.Kind, "locale", delivery.Locale)
	return true, nil
}
