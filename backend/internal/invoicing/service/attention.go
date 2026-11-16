package service

import (
	"context"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
)

// The documents that need an operator (#477, parent #471, ADR 0060): the
// queue of documents parked needs_attention, counted and listed oldest
// first for the Operator Dashboard; Mark annulled, the operator's record of
// a manual portal act; and a Sale's documents for the operator's Sale
// lookup.
//
// MARK ANNULLED RECORDS; IT NEVER ASKS THE AUTHORITY. The SRI offers no web
// service for annulment (ADR 0060, ADR 0078): the operator annuls the
// document at the portal by hand and then tells the platform so, and what
// the platform writes is that record - who, when, and on a factura why and
// whether late - on a row that keeps its number, its clave, its signed XML
// and the authority's last messages, because the annulment is a fact about
// a document that existed. On a factura it is allowed from pending,
// needs_attention and, since #745, authorized: one act whether the portal
// step came before the recording or after it, confirmed by the number typed
// in full, and each one a row on the annulments ledger. On a Credit Note it
// stays as #477 and #484 built it, from pending and needs_attention alone.
// An owed or unsigned document was never at the authority. It is
// irreversible here but for Reinstate (#749, reinstate.go), and it takes
// the document out of the Drainer's claim set and the queue in the same
// write.
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
// are `abandoned` or, since #747, `annulled` on a Ticket Sale that still
// stands and has no factura standing for it OR, since #746, annulled
// facturas whose Annulment Check stands at mismatch, and the count - the
// Operator Dashboard's daily badge - widens identically. Stated here in
// full because the next reader will otherwise "simplify" it back to one
// status: Abandon (#578) and Issue again (#580)
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
//
// AttentionReason says which door of the union put the document there
// (#746, #747): `needs_attention` (parked), `abandoned` or `annulled` (a
// dead Sale Invoice of a standing Sale no factura stands for, so nothing
// yet owes it one) or `annulment_mismatch` (annulled, and its Annulment
// Check stands at mismatch: the SRI still reports it authorized), which
// wins where an annulled Sale Invoice comes in by both doors. MismatchSince
// is the instant the mismatch was reached, on that reason alone, and
// AnnulledAt the instant the factura was annulled, on the `annulled` reason
// alone: the queue's "since" for each.
//
// ReinstateAdmitted says Reinstate is admitted on the entry as it stands
// (#749): at mismatch, once the factura's Annulment Window has closed, so
// the queue offers it beside the warning. False on every other reason.
type NeedsAttentionItem struct {
	InvoiceListItem
	AttentionReason   string                 `json:"attention_reason"`
	MismatchSince     *time.Time             `json:"mismatch_since"`
	AnnulledAt        *time.Time             `json:"annulled_at"`
	ReinstateAdmitted bool                   `json:"reinstate_admitted"`
	Messages          []AuthorityMessageView `json:"messages"`
}

// The needs-attention queue's reasons, one per door of the union.
const (
	AttentionReasonParked            = "needs_attention"
	AttentionReasonAbandoned         = "abandoned"
	AttentionReasonAnnulled          = "annulled"
	AttentionReasonAnnulmentMismatch = "annulment_mismatch"
)

// attentionReason is the door a queued document came in by: read off its
// status, and for an annulled one off whether its current annulment stands
// at mismatch, since an annulled Sale Invoice may come in by the
// unreplaced door, the mismatch's, or both, and the mismatch is named
// first.
func attentionReason(status invoicing.InvoiceStatus, atMismatch bool) string {
	switch status {
	case invoicing.InvoiceStatusAbandoned:
		return AttentionReasonAbandoned
	case invoicing.InvoiceStatusAnnulled:
		if atMismatch {
			return AttentionReasonAnnulmentMismatch
		}
		return AttentionReasonAnnulled
	default:
		return AttentionReasonParked
	}
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

// ListNeedsAttention returns one page of the needs-attention queue - every
// kind parked needs_attention, every abandoned or annulled Sale Invoice of
// a standing Sale no factura stands for, and every annulled factura at
// mismatch - longest waiting first.
func (s *Service) ListNeedsAttention(ctx context.Context, page, pageSize int) (*NeedsAttentionQueue, error) {
	rows, total, err := s.repo.ListNeedsAttention(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}
	var annulled []string
	for i := range rows {
		if rows[i].Invoice.Status == invoicing.InvoiceStatusAnnulled {
			annulled = append(annulled, rows[i].Invoice.ID)
		}
	}
	mismatchSince, err := s.repo.AnnulmentMismatchSince(ctx, annulled)
	if err != nil {
		return nil, err
	}
	now := s.clock()
	items := make([]NeedsAttentionItem, 0, len(rows))
	for i := range rows {
		at, atMismatch := mismatchSince[rows[i].Invoice.ID]
		item := NeedsAttentionItem{
			InvoiceListItem: s.listItem(&rows[i]),
			AttentionReason: attentionReason(rows[i].Invoice.Status, atMismatch),
			Messages:        messagesView(rows[i].Invoice.Messages),
		}
		switch item.AttentionReason {
		case AttentionReasonAnnulmentMismatch:
			item.MismatchSince = &at
			item.ReinstateAdmitted = !annulmentWindowOf(&rows[i].Invoice).Open(now)
		case AttentionReasonAnnulled:
			item.AnnulledAt = rows[i].Invoice.AnnulledAt
		}
		items = append(items, item)
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
// A FACTURA AND A CREDIT NOTE ARE TWO ACTS UNDER ONE ENDPOINT (#745, spec
// #742, ADR 0078). On a factura - a Sale Invoice or a manual Tax Invoice -
// Mark annulled admits `authorized` beside `pending` and `needs_attention`,
// with one set of gates in every state (annulFacturaRefusal) and the typed
// confirm in req, and it writes a row on the annulments ledger. On a Credit
// Note it stays exactly as ADR 0061 and #484 built it (annulCreditNote):
// pending or needs_attention, a plain confirm, req unread, no window and no
// record beyond the row's own pair.
func (s *Service) AnnulInvoice(ctx context.Context, id, annulledBy string, req invoicing.AnnulmentRequest) (*InvoiceDetail, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	if !invoicing.IsFactura(row.Invoice.Kind) {
		return s.annulCreditNote(ctx, row, annulledBy)
	}
	now := s.clock()
	if err := annulFacturaRefusal(row); err != nil {
		return nil, err
	}
	if err := annulConfirmRefusal(row, req, now); err != nil {
		return nil, err
	}
	done, err := s.repo.AnnulFactura(ctx, id, repository.FacturaAnnulment{
		FromStatus:       row.Invoice.Status,
		AnnulledBy:       annulledBy,
		AnnulledAt:       now,
		Note:             req.Note,
		LateAcknowledged: req.LateAcknowledged,
	})
	if err != nil {
		return nil, err
	}
	if !done {
		// The row moved between the read and the guarded write: a late
		// AUTORIZADO, a reversal owing a Credit Note, a second press. Say
		// what it now earns, read afresh, rather than a generic refusal.
		return nil, s.annulRaceRefusal(ctx, id)
	}
	s.logger.Info("invoicing: factura marked annulled",
		"invoice_id", id, "kind", row.Invoice.Kind, "from_status", row.Invoice.Status,
		"annulled_by", annulledBy, "late_acknowledged", req.LateAcknowledged)
	return s.GetInvoice(ctx, id)
}

// annulRaceRefusal is the refusal a factura whose guarded write found
// nothing to update earns as it now stands, or INVOICE_NOT_ANNULLABLE when
// the fresh read names nothing more specific.
func (s *Service) annulRaceRefusal(ctx context.Context, id string) error {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return err
	}
	if row != nil {
		if refusal := annulFacturaRefusal(row); refusal != nil {
			return refusal
		}
	}
	return invoicing.ErrInvoiceNotAnnullable()
}

// annulCreditNote is a Credit Note's Mark annulled, unchanged by #745.
//
// The refusals are decided on the row as read, and the write is guarded on
// the same states, so a press that races a late AUTORIZADO - the Drainer
// healing the document between the read and the write - finds no row to
// update and is told the document is not annullable rather than annulling
// an authorized artifact.
func (s *Service) annulCreditNote(ctx context.Context, row *repository.InvoiceRow, annulledBy string) (*InvoiceDetail, error) {
	id := row.Invoice.ID
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

// annulFacturaRefusal says whether Mark annulled may be pressed on a factura
// as it stands, whatever the operator types (#745, ADR 0078): the facts the
// staff page reads to offer the record form or shrink the panel to one
// sentence. In the order a reader should think about it:
//
//  1. The state. Authorized is admitted outright; pending and
//     needs_attention by annullable's rules, which keep #477's signed floor
//     and #578's INVOICE_ABANDON_INSTEAD for a number the SRI never took;
//     every other state is refused there.
//  2. A live Reissue successor: the factura is in the middle of a
//     correction. A factura that is pending, parked or authorized can only
//     have been superseded by a Reissue, never by Issue again, which
//     follows a dead one. Asked before the Credit Note, because a reissued
//     factura always has the reissue's Credit Note too, and the reissue is
//     the truer reason.
//  3. A live Credit Note against an AUTHORIZED factura (owed, pending,
//     needs_attention, authorized): the factura is already being undone at
//     the authority. A dead one - withdrawn, abandoned, annulled, rejected,
//     not authorized - does not refuse, which is what lets a reversed Sale
//     whose Credit Note was refused and annulled be cured by annulling its
//     factura. On a pending or parked factura a Credit Note can only be
//     owed and WAITING for it - the Drainer signs a Credit Note only once
//     its factura is authorized - and annulling the factura is exactly
//     what withdraws it (#476), so it never refuses there: refusing would
//     leave a refused factura and its reversal's Credit Note with no way
//     out.
//
// CONSUMIDOR FINAL HAS NO GATE HERE, deliberately: the platform cannot issue
// one (identification codes 07 and 08 are absent), and ADR 0078 records the
// invariant that whoever adds them adds the gate. Tax-refund and
// already-declared facturas are unknowable to the platform; the confirm
// names them and the operator answers for it.
func annulFacturaRefusal(row *repository.InvoiceRow) error {
	inv := &row.Invoice
	if inv.Status != invoicing.InvoiceStatusAuthorized {
		if err := annullable(inv); err != nil {
			return err
		}
	}
	if inv.SupersededByInvoiceID != "" {
		return invoicing.ErrInvoiceReissueStands()
	}
	if inv.Status == invoicing.InvoiceStatusAuthorized && row.StandingCreditNoteID != "" {
		return invoicing.ErrInvoiceCreditNoteStands()
	}
	return nil
}

// annulConfirmRefusal checks what the operator typed against the factura as
// it stands, once its facts admit the act: the number in full, and the late
// acknowledgement exactly when the Annulment Window has closed - required
// past it, refused before it, because it is a claim about a portal act
// after the deadline and is never recorded where it is not true. No fresh
// check of the SRI is required (ADR 0078).
func annulConfirmRefusal(row *repository.InvoiceRow, req invoicing.AnnulmentRequest, now time.Time) error {
	if row.Ecuador == nil || strings.TrimSpace(req.Number) != FormatNumber(row.Ecuador.Estab, row.Ecuador.PtoEmi, row.Ecuador.Secuencial) {
		return invoicing.ErrInvoiceNumberMismatch()
	}
	window := annulmentWindowOf(&row.Invoice)
	deadline := window.Deadline.Format(time.DateOnly)
	open := window.Open(now)
	if !open && !req.LateAcknowledged {
		return invoicing.ErrInvoiceAnnulmentLate(deadline)
	}
	if open && req.LateAcknowledged {
		return invoicing.ErrInvoiceAnnulmentNotLate(deadline)
	}
	return nil
}

// annulmentWindowOf is a signed factura's Annulment Window, read in
// Guayaquil.
func annulmentWindowOf(inv *invoicing.Invoice) invoicing.AnnulmentWindow {
	return invoicing.AnnulmentWindowFor(inv.IssuedOn, sri.Guayaquil)
}

// annullable says whether Mark annulled may be pressed on the document as
// it stands: signed — there is a number at the authority to have been
// annulled - and pending or needs_attention. The whole gate on a Credit
// Note; on a factura the floor annulFacturaRefusal stands on for every
// state but authorized.
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
