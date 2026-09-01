package handler

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Issue again (#580, parent #575, ADR 0068): the one action on a terminally
// dead Sale Invoice. The body is an optional note and nothing else — there
// is no Recipient, because the replacement carries the dead document's — and
// who pressed comes from the session, never from the caller, exactly as the
// abandonment's author and the reissue's do.

// issueAgainBody is the Issue again form as submitted: a note, or nothing.
type issueAgainBody struct {
	Note *string `json:"note"`
}

// IssueInvoiceAgain owes a Ticket Sale a fresh Sale Invoice to replace a
// terminally dead one.
//
// @Summary      Issue a terminally dead Sale Invoice again
// @Description  Owes the Ticket Sale a FRESH Sale Invoice to replace one that is terminally dead — `abandoned`, because the SRI refuses the number it carries and never took it (ADR 0068), or `annulled`, because a Platform Operator disowned it by hand at the SRI portal — and answers 201 with the replacement's detail, `owed` and unsigned. The replacement carries the dead document's lines, amounts, IVA rate, currency, payment method and RECIPIENT verbatim, including its email: reinvoicing never changes what was sold or to whom, and a Recipient that needs correcting is the reissue's business (ADR 0061) once the replacement is authorized. It is NOT a special signing route: it is inserted as an ordinary owed document inside a transaction, with no number, no clave de acceso and no signature, and the Sale Invoice Drainer signs it on a later round under a FRESHLY allocated secuencial by the ordinary path. The abandoned number stays consumed and is never handed out again. No Credit Note is owed — a document the SRI never authorized has nothing to cancel — which is why this reaches a Sale a reissue answers INVOICE_ALREADY_CREDITED on. The replacement is linked to the document it replaces through ADR 0061's supersede chain, so both detail pages show the chain in both directions, and it records who pressed (the session's email), when, and the optional `note` (at most 500 characters) in the same trail a reissue writes. The chain survives any number of hops: a replacement that is itself abandoned or annulled is issued again in its turn. The dead document is untouched — its number, clave, signed bytes, attempts and trail stand forever — and is never delivered to the buyer, who receives the replacement alone. Refused with INVOICE_MANUAL_NOT_ISSUABLE_AGAIN (409) on a manual Tax Invoice, which is typed again by hand, CREDIT_NOTE_NOT_ISSUABLE_AGAIN (409) on a Credit Note, INVOICE_NOT_TERMINALLY_DEAD (409, `details.status`) on a Sale Invoice that is owed, pending, authorized, rejected, not_authorized, needs_attention or withdrawn, INVOICE_SALE_REVERSED (409) when the Ticket Sale no longer stands, INVOICE_ALREADY_REPLACED (409) when a live replacement already exists — one that is itself withdrawn, annulled or abandoned does not count — and INVOICE_NOT_FOUND (404) otherwise. The decision is made under the Ticket Sale's lock, so a press racing a reversal is refused rather than double-written. Behind SALE_INVOICING_ENABLED: while the flag is closed this answers 404 SALE_INVOICING_UNAVAILABLE. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  string          true   "Terminally dead Sale Invoice id"
// @Param        body  body  issueAgainBody  false  "An optional note"
// @Success      201  {object}  openapi.EnvelopeInvoiceDetail
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id}/issue-again [post]
func (h *Handler) IssueInvoiceAgain(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	// Who issued it again comes from the session and nowhere else: the
	// trail names the operator who acted, never one the caller named.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	// An empty body is a valid press: the note is the only field and it is
	// optional, so "no body" and "{}" mean the same thing — the Abandon's
	// rule, and for the same reason.
	var body issueAgainBody
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errorIsEmptyBody(err) {
			_ = platform.WriteInvalidJSON(w, reqID)
			return
		}
	}
	// Bounded by the column the trail is stored in, which is the reissue's:
	// one chain, one note, one limit (abandonNote states it in full).
	note, fields := abandonNote(body.Note)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	invoice, err := h.svc.IssueSaleInvoiceAgain(r.Context(), id, service.IssueAgainInput{
		IssuedAgainBy: session.Email,
		Note:          note,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, invoice)
}
