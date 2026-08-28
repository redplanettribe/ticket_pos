package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/invoicing/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Uninvoiced House Sales (#507, parent #506, ADR 0064): the operator's
// platform-wide backlog of paid House sales with no Sale Invoice, listed
// oldest first, and its count. Read-only; the Sale Invoice Backfill act is
// #508. The list takes nothing but a page; the count takes nothing.

// ListUninvoicedHouseSales lists the Uninvoiced House Sales, oldest first.
//
// @Summary      List the Uninvoiced House Sales
// @Description  Returns a page of every Uninvoiced House Sale platform-wide (ADR 0064): a paid Online Sale — approved Payment above zero — that is `active` with no Reversal Request in flight or parked, has NO Sale Invoice row of any status (a sale whose factura was annulled or withdrawn had one and is not listed), and belongs to an Organization that is House NOW — when it was designated is irrelevant, and an Organization no longer House contributes nothing. Free, imported and Manually Recorded sales are never listed. OLDEST SALE FIRST by `sold_at`, then id. Each row carries the sale's own date, the Organization and Event, the buyer's name and Tax ID as the checkout was transacted, the total paid and the Sale Confirmation reference; a factura owed from this list will be dated the day of the act, never `sold_at`. An empty page means the backlog is clear. Response is the ADR-0006 nested envelope { data, pagination }; page_size defaults to 50 (max 100). Behind SALE_INVOICING_ENABLED: while the flag is closed this answers 404 SALE_INVOICING_UNAVAILABLE. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "Page number (default 1)"
// @Param        page_size  query  int  false  "Page size (default 50, max 100)"
// @Success      200  {object}  openapi.EnvelopeUninvoicedHouseSaleList
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/uninvoiced-sales [get]
func (h *Handler) ListUninvoicedHouseSales(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	query := r.URL.Query()
	list, err := h.svc.ListUninvoicedHouseSales(r.Context(), pageParam(query.Get("page")), pageSizeParam(query.Get("page_size")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, list)
}

// CountUninvoicedHouseSales returns the backlog's size as one number.
//
// @Summary      Count the Uninvoiced House Sales
// @Description  Returns uninvoiced_house_sale_count: how many Uninvoiced House Sales there are platform-wide (ADR 0064) — the count shown beside the invoicing list's other counts. It counts exactly what the list shows, so the two can never disagree. Zero is an ordinary answer: the backlog is clear. Read-only. Behind SALE_INVOICING_ENABLED: while the flag is closed this answers 404 SALE_INVOICING_UNAVAILABLE and no count is shown. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeUninvoicedHouseSaleCount
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/uninvoiced-sales/count [get]
func (h *Handler) CountUninvoicedHouseSales(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	count, err := h.svc.CountUninvoicedHouseSales(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, count)
}

// backfillBody is the operator's selection: the Uninvoiced House Sales to
// owe a Sale Invoice each.
type backfillBody struct {
	TicketSaleIDs []string `json:"ticket_sale_ids"`
}

// BackfillSaleInvoices performs a Sale Invoice Backfill over the selected
// Uninvoiced House Sales.
//
// @Summary      Backfill Sale Invoices for the selected Uninvoiced House Sales
// @Description  Performs a Sale Invoice Backfill (ADR 0064): for each `ticket_sale_ids` entry, in request order and EACH IN ITS OWN TRANSACTION, locks the Ticket Sale row, re-checks that it is still an Uninvoiced House Sale, describes it exactly as the checkout does and owes it an ordinary Sale Invoice through the checkout's own builder, stamped with `backfilled_by` (the operator, from the session) and `backfilled_at` (now). The document is DATED THE DAY OF THE ACT, never the day of the sale — the SRI refuses a past `fechaEmision` — and from then on it is drained, delivered to the buyer with the standard mail (XML + RIDE), credited on reversal and reissuable like any Sale Invoice. Then the Sale Invoice Drainer is kicked ONCE for everything owed; the answer does not wait for it. Answers 200 whenever the request is valid, with `owed` ({ticket_sale_id, invoice_id}) and `refused` ({ticket_sale_id, code}) in request order: `not_a_candidate` for an unknown id or a sale that is not (or no longer) an Uninvoiced House Sale — already invoiced, reversed, not House, not online, free — and `unsupported_sale` when the builder refuses the sale because its lines do not match its Payment, in which case nothing was written and no sequence number consumed. A refused sale never blocks the others; the same id twice is refused the second time. 1 to 200 ids; empty, missing, more than 200 or a non-UUID → 400 VALIDATION_FAILED. Behind SALE_INVOICING_ENABLED: while the flag is closed this answers 404 SALE_INVOICING_UNAVAILABLE. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      backfillBody  true  "The selected Ticket Sale ids (1–200)"
// @Success      200   {object}  openapi.EnvelopeSaleInvoiceBackfillResult
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/uninvoiced-sales/backfill [post]
func (h *Handler) BackfillSaleInvoices(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	// Who owed the documents comes from the session and nowhere else: the
	// trail names the operator who acted, never one the caller named.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	var body backfillBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	ids, fields := validateBackfill(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	result, err := h.svc.BackfillSaleInvoices(r.Context(), ids, session.Email)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// validateBackfill bounds the selection to 1..MaxBackfillSelection UUIDs and
// names every entry that is not one. The ids come back in canonical form
// so the answer echoes what the list shows, whatever casing was sent.
func validateBackfill(body backfillBody) ([]string, []platform.FieldError) {
	if len(body.TicketSaleIDs) == 0 {
		return nil, []platform.FieldError{{Field: "ticket_sale_ids", Code: "REQUIRED", Message: "select at least one sale"}}
	}
	if len(body.TicketSaleIDs) > service.MaxBackfillSelection {
		return nil, []platform.FieldError{{Field: "ticket_sale_ids", Code: "TOO_MANY", Message: fmt.Sprintf("at most %d sales per request", service.MaxBackfillSelection)}}
	}
	var fields []platform.FieldError
	ids := make([]string, 0, len(body.TicketSaleIDs))
	for i, raw := range body.TicketSaleIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			fields = append(fields, platform.FieldError{Field: fmt.Sprintf("ticket_sale_ids[%d]", i), Code: "INVALID", Message: "must be a UUID"})
			continue
		}
		ids = append(ids, id.String())
	}
	if len(fields) > 0 {
		return nil, fields
	}
	return ids, nil
}
