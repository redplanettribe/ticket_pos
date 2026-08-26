package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// The documents that need an operator (#477, parent #471, ADR 0060): the
// queue of documents parked needs_attention, counted and listed oldest
// first for the Operator Dashboard; Mark annulled, the operator's record of
// a manual portal act; and a Sale's documents for the operator's Sale
// lookup.
//
// MARK ANNULLED RECORDS; IT NEVER ASKS THE AUTHORITY. The SRI offers no web
// service for annulment (ADR 0060): the operator annuls the document at the
// portal by hand and then tells the platform so, and what the platform
// writes is that record — who, when — on a row that keeps its number, its
// clave, its signed XML and the authority's last messages, because the
// annulment is a fact about a document that existed. It is allowed from
// needs_attention and from pending, the two states in which the operator
// may have acted at the portal before the platform heard; an authorized
// document is credited, never annulled here, and an owed or unsigned one
// was never at the authority. It is irreversible, and it takes the
// document out of the Drainer's claim set and the queue in the same write.
//
// MARK ANNULLED ON A REISSUE'S CREDIT NOTE UNDOES THE REISSUE (#484, ADR
// 0061). The corrected factura was waiting, unsigned, for that Credit Note
// to be authorized; it never will be, so the same transaction withdraws
// the corrected factura — no number consumed, nothing sent — and the old
// factura is the Sale's current one again, credited by nothing live and
// open to another reissue. The Drainer would do the same on its next round
// (drainer.go); doing it here is what lets the operator reissue at once.

// NeedsAttentionQueue is one page of the documents parked for an operator,
// longest waiting first, with the total: the ADR-0006 nested envelope.
type NeedsAttentionQueue struct {
	Data              []NeedsAttentionItem `json:"data"`
	InvoicePagination InvoicePagination    `json:"pagination"`
}

// NeedsAttentionItem is one queued document: the list row — kind, Sale
// Confirmation reference, state, since when — and what the authority (or
// the platform, when it could not be signed) last said about it, so the
// queue answers "why" without a click through.
type NeedsAttentionItem struct {
	InvoiceListItem
	Messages []AuthorityMessageView `json:"messages"`
}

// NeedsAttentionCount is the queue's size as one number: the badge on the
// Operator Dashboard. It counts exactly what the queue lists.
type NeedsAttentionCount struct {
	NeedsAttentionCount int `json:"needs_attention_count"`
}

// RecipientWarningCount is how many documents carry a Recipient Warning
// (#482, ADR 0061): the Operator Dashboard's badge beside the
// needs_attention count. It counts exactly what the list's filter finds.
type RecipientWarningCount struct {
	RecipientWarningCount int `json:"recipient_warning_count"`
}

// CountRecipientWarnings returns how many documents carry a Recipient
// Warning. Behind SALE_INVOICING_ENABLED with the fact itself: closed, it
// answers SALE_INVOICING_UNAVAILABLE, and the dashboard shows no count.
func (s *Service) CountRecipientWarnings(ctx context.Context) (*RecipientWarningCount, error) {
	if !s.saleInvoicingEnabled {
		return nil, invoicing.ErrSaleInvoicingUnavailable()
	}
	n, err := s.repo.CountRecipientWarnings(ctx)
	if err != nil {
		return nil, err
	}
	return &RecipientWarningCount{RecipientWarningCount: n}, nil
}

// ListNeedsAttention returns one page of the documents parked
// needs_attention, of every kind, longest waiting first.
func (s *Service) ListNeedsAttention(ctx context.Context, page, pageSize int) (*NeedsAttentionQueue, error) {
	rows, total, err := s.repo.ListNeedsAttention(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}
	items := make([]NeedsAttentionItem, 0, len(rows))
	for i := range rows {
		items = append(items, NeedsAttentionItem{
			InvoiceListItem: s.listItem(&rows[i]),
			Messages:        messagesView(rows[i].Invoice.Messages),
		})
	}
	return &NeedsAttentionQueue{Data: items, InvoicePagination: pagination(page, pageSize, total)}, nil
}

// CountNeedsAttention returns how many documents are parked needs_attention.
func (s *Service) CountNeedsAttention(ctx context.Context) (*NeedsAttentionCount, error) {
	n, err := s.repo.CountNeedsAttention(ctx)
	if err != nil {
		return nil, err
	}
	return &NeedsAttentionCount{NeedsAttentionCount: n}, nil
}

// AnnulInvoice records that the operator annulled the document by hand at
// the authority's portal, and returns it as it then stands.
//
// The refusals are decided on the row as read, and the write is guarded on
// the same states, so a press that races a late AUTORIZADO — the Drainer
// healing the document between the read and the write — finds no row to
// update and is told the document is not annullable rather than annulling
// an authorized artifact.
func (s *Service) AnnulInvoice(ctx context.Context, id, annulledBy string) (*InvoiceDetail, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	if err := annullable(&row.Invoice); err != nil {
		return nil, err
	}
	now := s.clock()
	successorMessages := []invoicing.AuthorityMessage{{
		Identifier: correctedInvoiceWithdrawnCode, Message: correctedInvoiceWithdrawnMessage, Type: platformMessageType,
	}}
	done, withdrawnSuccessorID, err := s.repo.AnnulInvoice(ctx, id, annulledBy, now, successorMessages)
	if err != nil {
		return nil, err
	}
	if !done {
		return nil, invoicing.ErrInvoiceNotAnnullable()
	}
	s.logger.Info("invoicing: document marked annulled",
		"invoice_id", id, "kind", row.Invoice.Kind, "from_status", row.Invoice.Status, "annulled_by", annulledBy)
	if withdrawnSuccessorID != "" {
		s.logger.Info("invoicing: corrected sale invoice withdrawn; the credit note it follows was marked annulled",
			"invoice_id", withdrawnSuccessorID, "credit_note_id", id, "superseded_invoice_id", row.Invoice.CreditsInvoiceID)
	}
	return s.GetInvoice(ctx, id)
}

// annullable says whether Mark annulled may be pressed on the document as
// it stands: signed — there is a number at the authority to have been
// annulled — and pending or needs_attention.
func annullable(inv *invoicing.Invoice) error {
	switch inv.Status {
	case invoicing.InvoiceStatusPending, invoicing.InvoiceStatusNeedsAttention:
		if !inv.Signed() {
			return invoicing.ErrInvoiceNotIssued()
		}
		return nil
	case invoicing.InvoiceStatusOwed:
		return invoicing.ErrInvoiceNotIssued()
	default:
		return invoicing.ErrInvoiceNotAnnullable()
	}
}

// SaleDocuments returns every document about one Ticket Sale — its Sale
// Invoice and, once one exists, its Credit Note — oldest first, as list
// rows: what the operator's Sale lookup shows beside the Sale (#477). An
// empty list, never nil, for a Sale that owes nothing.
func (s *Service) SaleDocuments(ctx context.Context, ticketSaleID string) ([]InvoiceListItem, error) {
	rows, err := s.repo.ListInvoicesBySale(ctx, ticketSaleID)
	if err != nil {
		return nil, err
	}
	items := make([]InvoiceListItem, 0, len(rows))
	for i := range rows {
		items = append(items, s.listItem(&rows[i]))
	}
	return items, nil
}
