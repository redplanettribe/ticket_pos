package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
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

// THE QUEUE IS A UNION, NOT A STATUS EQUALITY (#581, parent #575, ADR
// 0068). It lists documents parked `needs_attention` OR Sale Invoices that
// are `abandoned` with no live successor on a Ticket Sale that still
// stands, and the count — the Operator Dashboard's daily badge — widens
// identically. Stated here in full because the next reader will otherwise
// "simplify" it back to one status: Abandon (#578) and Issue again (#580)
// are deliberately two presses, so a Sale can sit abandoned-and-unreplaced
// between them with its buyer holding no valid tax document, and until the
// widening nothing on any surface said so. It self-clears — the moment
// Issue again owes a replacement the abandoned document has a live
// successor and drops out — which is what makes a union safe here. The
// membership rule and the composition of its two orderings live in one
// place, repository.needsAttentionWhere and needsAttentionOrder.

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
//
// "Since when" is attention_since on a parked document and abandoned_at on
// an abandoned one, the two instants the queue's order composes (#581); the
// row carries both and the surface reads whichever the document has.
type NeedsAttentionItem struct {
	InvoiceListItem
	Messages []AuthorityMessageView `json:"messages"`
}

// NeedsAttentionCount is the queue's size as one number: the badge on the
// Operator Dashboard. It counts exactly what the queue lists — the same
// union, from the same rule, so the badge and the page can never disagree.
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

// ListNeedsAttention returns one page of the needs-attention queue — every
// kind parked needs_attention, and every abandoned-and-unreplaced Sale
// Invoice whose Sale still stands — longest waiting first.
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

// CountNeedsAttention returns how many documents are in the queue: the
// same union the list reports, never a status count.
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
//
// EXCEPT WHERE ABANDON IS THE TRUE ACT (#578, ADR 0068). Mark annulled
// records an annulment the operator performed BY HAND AT THE AUTHORITY'S
// PORTAL. On a document the authority refuses by NUMBER — its 45,
// "secuencial registrado" — the portal shows nothing under that number, so
// there is nothing there to have been annulled and the press would write a
// true-looking record of an act that never happened. That was the only
// escape production's 001-001-000000025 and 26 offered their operator, and
// it was a falsehood. It is refused here, naming the act that is true.
//
// This NARROWS an existing operator capability, deliberately: ADR 0061's
// line that "Mark annulled is for documents the authority refused" is
// amended to documents the authority HELD and the operator disowned.
//
// The refusal is gated on the authority's refusal alone and not on
// everything Abandon needs (abandonRefusal): a document that qualifies but
// has not been checked must be CHECKED, never annulled instead, or the
// falsehood is one stale minute away.
func annullable(inv *invoicing.Invoice) error {
	switch inv.Status {
	case invoicing.InvoiceStatusPending, invoicing.InvoiceStatusNeedsAttention:
		if !inv.Signed() {
			return invoicing.ErrInvoiceNotIssued()
		}
		// Handed to Abandon only where Abandon would take it (#578, ADR
		// 0068). A code-45 document that is still `pending` is with the
		// authority and is CHECKED, not given up on — and Abandon refuses
		// it — so refusing the annulment here too would leave it with no
		// act at all, which is the dead end this epic exists to close.
		if invoicing.RefusedByNumberIn(inv.Messages) && abandonableState(inv.Status) {
			return invoicing.ErrInvoiceAbandonInstead()
		}
		return nil
	case invoicing.InvoiceStatusOwed:
		return invoicing.ErrInvoiceNotIssued()
	default:
		return invoicing.ErrInvoiceNotAnnullable()
	}
}

// DocumentRole is a document's place in its Ticket Sale's chain (#486, ADR
// 0061), derived on every read and stored nowhere.
type DocumentRole string

const (
	// DocumentRoleCurrent: the Sale's current Sale Invoice — sale-kind,
	// no live successor, and not itself terminally dead. A later Sale
	// Reversal credits this one. A factura whose successor died — withdrawn,
	// annulled or abandoned (#579, ADR 0068) — is current again: it is
	// superseded by nothing, because nothing stands in its place.
	DocumentRoleCurrent DocumentRole = "current"
	// DocumentRoleSuperseded: a Sale Invoice a reissue corrected; still
	// authorized and on file, no longer the Sale's current one.
	DocumentRoleSuperseded DocumentRole = "superseded"
	// DocumentRoleCreditNote: a Credit Note, for a reversal or a reissue;
	// its reason says which.
	DocumentRoleCreditNote DocumentRole = "credit_note"
	// DocumentRoleNotCurrent: a Sale Invoice that is neither current nor
	// superseded — withdrawn before it was sent, annulled at the portal, or
	// abandoned because the authority never took it (#578, ADR 0068) — a
	// Sale Invoice the Sale no longer has (#477, #480).
	DocumentRoleNotCurrent DocumentRole = "not_current"
)

// SaleDocument is one document as the operator's Sale lookup lists it
// (#477, #486): the invoicing list's own row, its role in the chain, the
// links to its neighbours and the reissue's trail — who, when, the note —
// read beside every document a reissue concerns, exactly as the detail
// shows them. A buyer's question about their factura is answered from the
// one page.
type SaleDocument struct {
	InvoiceListItem
	Role DocumentRole `json:"role"`
	// SupersedesInvoiceID is, on a corrected Sale Invoice, the factura it
	// corrects; CreditsInvoiceID and CreditNoteReason are, on a Credit
	// Note, the factura it credits and why — a reversal route or "reissue".
	// Null where they do not apply.
	SupersedesInvoiceID *string `json:"supersedes_invoice_id"`
	CreditsInvoiceID    *string `json:"credits_invoice_id"`
	CreditNoteReason    *string `json:"credit_note_reason"`
	// ReissuedBy, ReissuedAt and ReissueNote are the reissue's trail, on the
	// corrected factura, the superseded one and the reissue's Credit Note
	// alike; a document's own reissue wins over one that later superseded
	// it. Null where no reissue concerns the document.
	ReissuedBy  *string    `json:"reissued_by"`
	ReissuedAt  *time.Time `json:"reissued_at"`
	ReissueNote *string    `json:"reissue_note"`
}

// SaleDocuments returns every document about one Ticket Sale in CHAIN
// ORDER — a factura, the Credit Notes crediting it, then the factura that
// superseded it, and onward — with each document's role and the reissue
// trail beside it: what the operator's Sale lookup shows beside the Sale
// (#477, #486). An empty list, never nil, for a Sale that owes nothing.
//
// The order is the chain's and never the clock's: a reissue writes its
// Credit Note and its corrected factura in one transaction, so their
// timestamps tie and their ids say nothing. Creation order breaks the
// remaining ties — two Credit Notes on one factura, a withdrawn successor
// before the live one.
func (s *Service) SaleDocuments(ctx context.Context, ticketSaleID string) ([]SaleDocument, error) {
	rows, err := s.repo.ListInvoicesBySale(ctx, ticketSaleID)
	if err != nil {
		return nil, err
	}
	ordered := chainOrder(rows)
	items := make([]SaleDocument, 0, len(ordered))
	for i := range ordered {
		items = append(items, s.saleDocument(&ordered[i]))
	}
	return items, nil
}

func (s *Service) saleDocument(row *repository.InvoiceRow) SaleDocument {
	inv := &row.Invoice
	item := s.listItem(row)
	return SaleDocument{
		InvoiceListItem:     item,
		Role:                documentRole(&item),
		SupersedesInvoiceID: optional(inv.SupersedesInvoiceID),
		CreditsInvoiceID:    optional(inv.CreditsInvoiceID),
		CreditNoteReason:    optional(inv.CreditNoteReason),
		ReissuedBy:          optional(inv.ReissuedBy),
		ReissuedAt:          optionalTime(inv.ReissuedAt),
		ReissueNote:         optional(inv.ReissueNote),
	}
}

// documentRole derives a document's role from the list row as shown — so
// a row whose superseded marker the flag hides is never called superseded
// beside a null marker.
func documentRole(item *InvoiceListItem) DocumentRole {
	switch {
	case item.Kind == string(invoicing.DocumentKindCreditNote):
		return DocumentRoleCreditNote
	case item.SupersededByInvoiceID != nil:
		return DocumentRoleSuperseded
	case item.Status == string(invoicing.InvoiceStatusWithdrawn),
		item.Status == string(invoicing.InvoiceStatusAnnulled),
		item.Status == string(invoicing.InvoiceStatusAbandoned):
		return DocumentRoleNotCurrent
	default:
		return DocumentRoleCurrent
	}
}

// chainOrder walks a Sale's documents, given oldest first, in chain order:
// each root factura (one that supersedes nothing), then the Credit Notes
// crediting it, then its successors — a withdrawn one before the live one,
// by creation — each walked the same way. A document whose link points
// outside the Sale, which the schema does not allow, is appended at the
// end rather than dropped.
func chainOrder(rows []repository.InvoiceRow) []repository.InvoiceRow {
	successors := map[string][]int{}
	credits := map[string][]int{}
	var roots []int
	for i := range rows {
		inv := &rows[i].Invoice
		switch {
		case inv.Kind == invoicing.DocumentKindCreditNote && inv.CreditsInvoiceID != "":
			credits[inv.CreditsInvoiceID] = append(credits[inv.CreditsInvoiceID], i)
		case inv.SupersedesInvoiceID != "":
			successors[inv.SupersedesInvoiceID] = append(successors[inv.SupersedesInvoiceID], i)
		default:
			roots = append(roots, i)
		}
	}
	out := make([]repository.InvoiceRow, 0, len(rows))
	seen := make([]bool, len(rows))
	var walk func(i int)
	walk = func(i int) {
		if seen[i] {
			return
		}
		seen[i] = true
		out = append(out, rows[i])
		id := rows[i].Invoice.ID
		for _, c := range credits[id] {
			walk(c)
		}
		for _, next := range successors[id] {
			walk(next)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	for i := range rows {
		walk(i)
	}
	return out
}
