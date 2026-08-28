package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Uninvoiced House Sales (#507, parent #506, ADR 0064): the operator's
// platform-wide backlog of paid House sales with no Sale Invoice, listed
// oldest first, and its count. Read-only; the Sale Invoice Backfill act is
// #508. Both take nothing but a page.

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
