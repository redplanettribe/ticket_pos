package handler

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The documents that need an operator (#477): the needs_attention queue
// and its count for the Operator Dashboard, and Mark annulled on the
// document detail. The queue takes a page and nothing else; Mark annulled
// takes no body — who annulled comes from the session, and the answer is
// the document as it then stands.

// ListNeedsAttention lists the documents parked needs_attention, oldest first.
//
// @Summary      List the documents that need attention
// @Description  Returns a page of every document parked `needs_attention` (ADR 0060) — a Sale Invoice or Credit Note the SRI refused, one unanswered for 24 hours and still polled, or one that could not be signed — LONGEST WAITING FIRST by `attention_since`, the instant each was parked. Every row is the invoicing list row (kind, number when signed, Sale Confirmation reference, Recipient, total, status) plus `messages`: the SRI's last messages verbatim, or the platform's own PLATFORM-typed message saying why the document could not be signed. Each row opens the document detail by its id. An empty page is the ordinary answer. Response is the ADR-0006 nested envelope { data, pagination }; page_size defaults to 50 (max 100). Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "Page number (default 1)"
// @Param        page_size  query  int  false  "Page size (default 50, max 100)"
// @Success      200  {object}  openapi.EnvelopeNeedsAttentionQueue
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/needs-attention [get]
func (h *Handler) ListNeedsAttention(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	query := r.URL.Query()
	queue, err := h.svc.ListNeedsAttention(r.Context(), pageParam(query.Get("page")), pageSizeParam(query.Get("page_size")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, queue)
}

// CountNeedsAttention returns the queue's size as one number.
//
// @Summary      Count the documents that need attention
// @Description  Returns needs_attention_count: how many documents are parked `needs_attention` across every kind — the badge the Operator Dashboard shows so that a stuck document is never silent (ADR 0060). It counts exactly what the queue lists, so the two can never disagree. Zero is an ordinary answer. Read-only. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeNeedsAttentionCount
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/needs-attention/count [get]
func (h *Handler) CountNeedsAttention(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	count, err := h.svc.CountNeedsAttention(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, count)
}

// AnnulInvoice marks a document annulled.
//
// @Summary      Mark a document annulled
// @Description  Records that the Platform Operator annulled the document by hand at the SRI portal — the SRI offers no web service for annulment (ADR 0060) — and answers with the document as it then stands: status `annulled`, `annulled_by` the operator's email from the session and `annulled_at` the moment. Nothing is sent to or asked of the SRI. The row keeps its number, clave de acceso, signed XML and the SRI's last messages; `next_attempt_at` is cleared so the Sale Invoice Drainer never claims it again, and it leaves the needs-attention queue. Allowed only from `pending` or `needs_attention` — the states in which the operator may have acted at the portal — and irreversible: INVOICE_NOT_ANNULLABLE (409) on an authorized, withdrawn or already annulled document, INVOICE_NOT_ISSUED (409) on one still owed or parked unsigned (nothing exists at the SRI to have been annulled), INVOICE_NOT_FOUND (404) otherwise. Afterwards Check status and Resend answer INVOICE_ANNULLED. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Document id"
// @Success      200  {object}  openapi.EnvelopeInvoiceDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id}/annul [post]
func (h *Handler) AnnulInvoice(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	// Who annulled comes from the session and nowhere else: the trail names
	// the operator who acted, never one the caller named.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	invoice, err := h.svc.AnnulInvoice(r.Context(), id, session.Email)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, invoice)
}
