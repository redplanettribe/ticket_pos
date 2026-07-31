package handler

import (
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Payout Request queue's three reads (#176, ADR 0026). All read-only:
// fulfilling a request and declining one are #177, and nothing in this file
// writes.
//
// They sit on the operator namespace and are gated by it and nothing else — a
// Staff Session whose email is on the allowlist, with no Membership required
// anywhere (ADR 0015). That matters more here than on any other operator route:
// every row on this queue belongs to an Organization the operator is not a
// Member of, and being an Org Admin of one of them grants nothing.

// ListPayoutRequests returns the cross-Organization queue of outstanding asks.
//
// @Summary      List every outstanding Payout Request, oldest first
// @Description  Returns a page of every OUTSTANDING (pending) Payout Request across every Organization on the platform — the operator's work queue, and the second cross-Organization view on this surface after sale lookup. Ordered OLDEST FIRST, deliberately breaking the newest-first convention the payout and sale histories use: this is a work queue rather than a history, and the oldest unanswered request is the one about to become a complaint (ADR 0026). Each row carries the ask (amount, note, asker's email, the instant it was made) with the Payable Balance AS IT STOOD when it was made, and the Organization it belongs to. ACCOUNT NUMBERS ARE MASKED here and nowhere unmasked but the single-request endpoint: this is the one screen showing every Organization's bank details at once, so account_number_masked is "····4821" and the payload carries no whole account number and no Tax ID at all. Answered requests — paid, declined or cancelled — are not in the queue; they stay readable by id and on their Organization's detail page. Response is the ADR-0006 nested envelope { data, pagination }; page_size defaults to 50 (max 100) and page floors at 1. Read-only. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "Page number (1-based; floors at 1)"
// @Param        page_size  query  int  false  "Page size (default 50, max 100)"
// @Success      200  {object}  openapi.EnvelopeOperatorPayoutRequestQueue
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/payout-requests [get]
func (h *Handler) ListPayoutRequests(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	query := r.URL.Query()
	queue, err := h.svc.PayoutRequestQueue(r.Context(), pageParam(query.Get("page")), pageSizeParam(query.Get("page_size")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, queue)
}

// CountPendingPayoutRequests returns the backlog as one number.
//
// @Summary      Count the outstanding Payout Requests
// @Description  Returns pending_count: how many Payout Requests are outstanding across every Organization on the platform. It is the badge the Operator Dashboard's navigation wears, because a queue's whole value is being noticed by somebody who had not already decided to look (ADR 0026). It counts exactly what the queue lists, so the two can never disagree. Zero is an ordinary answer. Read-only. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeOperatorPendingPayoutRequestCount
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/payout-requests/count [get]
func (h *Handler) CountPendingPayoutRequests(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	count, err := h.svc.PendingPayoutRequestCount(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, count)
}

// GetPayoutRequest returns one Payout Request in full.
//
// @Summary      Get one Payout Request with the details needed to pay it
// @Description  Returns everything needed to execute one transfer: the ask (amount, note, the asker's email, the instant), the Organization it belongs to, and the snapshot Payout Profile IN FULL — bank, account type, WHOLE account number, the name on the account and the Organization's Tax ID for the factura. This is the only endpoint that returns an unmasked account number, and it returns one request at a time: an operator who opens a request is about to retype that number into a banking app. The snapshot is frozen and no later edit of the Organization's Payout Profile rewrites it (ADR 0026). Two readings of the money travel with it: request.payable_balance_cents is the Payable Balance AS IT STOOD when the ask was made, while payable_balance_cents and withdrawable_balance_cents are the LIVE figures now — so an operator can tell an Organization that asked for what it had from one that asked for four times as much. The cap was checked at request time and is never checked again; neither live figure gates anything, and the operator at the bank exercises the judgement. Requests in every state are readable here, including paid, declined and cancelled ones that have left the queue. An unknown or malformed id is 404 PAYOUT_REQUEST_NOT_FOUND. Read-only. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        requestID  path  string  true  "Payout Request ID"
// @Success      200  {object}  openapi.EnvelopeOperatorPayoutRequestDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/payout-requests/{requestID} [get]
func (h *Handler) GetPayoutRequest(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	detail, err := h.svc.GetPayoutRequest(r.Context(), strings.TrimSpace(r.PathValue("requestID")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, detail)
}
