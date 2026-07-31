package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Payout Request queue's three reads (#176) and the three writes that answer
// one (#177, #184, ADR 0026).
//
// The three reads sit on the operator namespace and are gated by it and nothing else — a
// Staff Session whose email is on the allowlist, with no Membership required
// anywhere (ADR 0015). That matters more here than on any other operator route:
// every row on this queue belongs to an Organization the operator is not a
// Member of, and being an Org Admin of one of them grants nothing.

// ListPayoutRequests returns the cross-Organization queue of outstanding asks.
//
// @Summary      List every outstanding Payout Request, oldest first
// @Description  Returns a page of every OUTSTANDING Payout Request across every Organization on the platform — `pending` ones nobody has answered, and `processing` ones whose transfer an operator has submitted and the bank has not confirmed — the operator's work queue, and the second cross-Organization view on this surface after sale lookup. Ordered OLDEST FIRST, deliberately breaking the newest-first convention the payout and sale histories use: this is a work queue rather than a history, and the oldest unanswered request is the one about to become a complaint (ADR 0026). Each row carries the ask (amount, note, asker's email, the instant it was made) with the Payable Balance AS IT STOOD when it was made, and the Organization it belongs to. ACCOUNT NUMBERS ARE MASKED here and nowhere unmasked but the single-request endpoint: this is the one screen showing every Organization's bank details at once, so account_number_masked is "····4821" and the payload carries no whole account number and no Tax ID at all. Answered requests — paid, failed, declined or cancelled — are not in the queue; they stay readable by id and on their Organization's detail page. A `processing` request IS in the queue, deliberately: a transfer nobody confirmed is precisely the work that must not fall out of sight, and each row carries its status so it does not read as unactioned. Response is the ADR-0006 nested envelope { data, pagination }; page_size defaults to 50 (max 100) and page floors at 1. Read-only. Platform Operator only.
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
// @Description  Returns pending_count: how many Payout Requests are OUTSTANDING across every Organization on the platform — `pending` and `processing` alike, because the badge counts the work outstanding rather than the work untouched, and a number that dropped when an operator submitted a transfer would tell the queue it was emptier than it is. It is the badge the Operator Dashboard's navigation wears, because a queue's whole value is being noticed by somebody who had not already decided to look (ADR 0026). It counts exactly what the queue lists, so the two can never disagree. Zero is an ordinary answer. Read-only. Platform Operator only.
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

// Answering a request: the three writes (#177, #184, ADR 0026).
//
// Every one takes the acting operator's email from the Staff Session and never
// from the body, exactly as recording a Payout and recording an Operator
// Reversal do. Who asserted a money fact, who submitted a transfer, and who
// answered somebody's ask to be paid must not be something a caller can claim
// (ADR 0015, ADR 0019).
//
// Only one of the three writes to the ledger. Marking a request `processing`
// records a transfer that has been SENT, and a Payout is money that ARRIVED
// (ADR 0014) — so the `payouts` table learns nothing until the bank confirms it,
// and a transfer that comes back leaves nothing to unwind.

// declineReasonMaxLength bounds the reason, matching the schema's CHECK on
// payout_requests.resolution_reason (migrations 043, 044). It is stated here so an
// over-long reason comes back as a field error naming the field, rather than as
// a constraint violation the operator cannot act on — the same treatment the
// Operator Reversal's note gets.
const declineReasonMaxLength = 500

// fulfilPayoutRequestBody is the fulfilment form: what actually left the bank,
// the day it did, and an optional note.
//
// It is deliberately the SAME shape recordPayoutBody has, because the Payout it
// produces is the same Payout. The one difference is where the amount starts:
// this form arrives PRE-FILLED with the figure the request asked for.
type fulfilPayoutRequestBody struct {
	AmountCents int     `json:"amount_cents"`
	PaidAt      string  `json:"paid_at"`
	Note        *string `json:"note"`
}

// declinePayoutRequestBody is a refusal and its reason. The reason is not a
// pointer: there is no decline without one.
type declinePayoutRequestBody struct {
	Reason string `json:"reason"`
}

// markProcessingBody is the whole of the mark-as-processing form: an OPTIONAL
// reference the bank handed back, and nothing else.
//
// Nothing else, deliberately. Who submitted the transfer comes from the Staff
// Session and when it was submitted comes from the injected clock, so neither is
// something a caller can claim (ADR 0015, ADR 0026). A body that could carry
// either would be a body that could backdate a transfer the 72-hour stale flag
// is measured from.
type markProcessingBody struct {
	TransferReference *string `json:"transfer_reference"`
}

// FulfilPayoutRequest records the Payout answering a request and marks it paid.
//
// @Summary      Fulfil a Payout Request by recording the Payout that answers it
// @Description  Records a Payout against the requesting Organization AND marks the Payout Request `paid`, in ONE database transaction. The operator transfers the money by hand off-platform first, as they always have, and this records that it happened — there is no second step to forget and no `approved` state in between. The body is the same shape the direct record-payout endpoint takes: amount_cents (what ACTUALLY left the bank, strictly positive), paid_at (the calendar day, YYYY-MM-DD) and an optional note. The amount is PRE-FILLED from the request on the dashboard form and may be overwritten — a deliberate departure from the no-pre-fill rule of ADR 0019, because a payout amount is a figure the Organization already stated rather than an assertion only the operator can make. Transferring less than was asked needs no special handling: the Payout records what moved, the request keeps what was asked, and the divergence stays visible forever. The resulting Payout is INDISTINGUISHABLE from a directly recorded one — same shape, same recorded_by (taken from the Staff Session, never from the body), and it appears unchanged on the Organization's own payouts page and in its Withdrawable Balance; only the request names it, through payout_id. NO balance is consulted in either direction: recording a settlement is unconditional (ADR 0015), and the Payable Balance cap bound the Organization when it asked and is never re-checked (ADR 0026). FULFILMENT IS A COMPARE-AND-SWAP: the request is updated only while it is still `pending` or `processing` — a transfer submitted days ago and now confirmed by the bank is fulfilled exactly as an instant one — and if it is neither, the WHOLE transaction rolls back so NO Payout row survives — 409 PAYOUT_REQUEST_ALREADY_RESOLVED, naming the current state and who resolved it first. That refusal tells an operator who also transferred to record the Payout directly, because the compare-and-swap prevents a double RECORD and not a double TRANSFER. An unknown or malformed id is 404 PAYOUT_REQUEST_NOT_FOUND. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        requestID  path  string                   true  "Payout Request ID"
// @Param        body       body  fulfilPayoutRequestBody  true  "The Payout that answers the request"
// @Success      201  {object}  openapi.EnvelopeOperatorPayoutFulfilment
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/payout-requests/{requestID}/fulfil [post]
func (h *Handler) FulfilPayoutRequest(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body fulfilPayoutRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	// Validated through the record-payout body's own validator rather than a
	// second copy of the same three rules. The two forms produce the same kind of
	// row, so a rule that tightened on one and not the other would be a bug
	// nobody would find until an operator hit it.
	payout, fields := validateRecordPayout(recordPayoutBody{
		AmountCents: body.AmountCents,
		PaidAt:      body.PaidAt,
		Note:        body.Note,
	})
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	result, err := h.svc.FulfilPayoutRequest(r.Context(), strings.TrimSpace(r.PathValue("requestID")), service.FulfilPayoutRequestInput{
		AmountCents: payout.AmountCents,
		PaidAt:      payout.PaidAt,
		Note:        payout.Note,
		// Stamps the Payout's recorded_by and the request's resolved_by alike.
		Operator: session.Email,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	// 201, because the thing that was created is a Payout — the same status the
	// direct path answers with, for the same row.
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, result)
}

// MarkPayoutRequestProcessing records that the operator submitted the transfer
// and cannot yet confirm it.
//
// @Summary      Mark a Payout Request as processing — the transfer is submitted, the bank has not confirmed it
// @Description  Marks the Payout Request `processing`, stamping who submitted the transfer (taken from the Staff Session, NEVER from the body) and when (the server's clock, never a value a caller supplies), plus an OPTIONAL transfer_reference of up to 200 characters — whatever the provider handed back, or nothing at all when it handed back nothing synchronously. NO PAYOUT IS RECORDED, and that is the whole point of the state: a Payout is money that MOVED (ADR 0014), a PayPhone transfer can take up to 48 hours to reach the account and can come back rejected, and a ledger that recorded it on submission would have to delete or negate rows when it bounces. The request STAYS OUTSTANDING — it keeps the Organization's single request slot, so they cannot ask again for money already on its way to them, it stays in the operator queue and in pending_count, and neither party may take it back: cancelling and declining are refused while it is processing. From here it goes `paid` when the money lands (fulfil it exactly as a pending request, recording the Payout that answers it) or `failed` when the bank returns it. An instant transfer needs none of this and still goes `pending → paid` in one step. Only a `pending` request may be marked processing: a request whose transfer another operator already submitted is 409 PAYOUT_REQUEST_TRANSFER_ALREADY_SUBMITTED naming who submitted it, and one that has already ended is 409 PAYOUT_REQUEST_ALREADY_RESOLVED. An unknown or malformed id is 404 PAYOUT_REQUEST_NOT_FOUND. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        requestID  path  string              true   "Payout Request ID"
// @Param        body       body  markProcessingBody  false  "The reference the bank handed back, if it handed one back"
// @Success      200  {object}  openapi.EnvelopeOperatorPayoutRequest
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/payout-requests/{requestID}/processing [post]
func (h *Handler) MarkPayoutRequestProcessing(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	// The body is optional in a way the other two writes' bodies are not: an
	// operator with no reference to record has nothing to say, and making them
	// send `{}` would be ceremony. An absent body decodes as EOF, which is the
	// same answer as an empty object here and not a malformed request.
	var body markProcessingBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	reference, fields := validateTransferReference(body.TransferReference)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	request, err := h.svc.MarkPayoutRequestProcessing(r.Context(), strings.TrimSpace(r.PathValue("requestID")), service.MarkPayoutRequestProcessingInput{
		Reference: reference,
		// Stamps transfer_submitted_by, which is a different fact from
		// resolved_by: whoever confirms this transfer days from now may be
		// somebody else entirely (ADR 0015, ADR 0026).
		Operator: session.Email,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	// 200 and not 201: nothing was created. A record that already existed learned
	// what an operator did about it, and no Payout was written — which is the
	// invariant this whole state exists to hold.
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, request)
}

// validateTransferReference normalises the optional reference the bank handed
// back.
//
// Absent, empty and whitespace-only are ONE answer — no reference — because all
// three mean the provider gave the operator nothing to record, and storing "" or
// "   " would leave every reader coalescing a blank against a null. Only the
// bound is a refusal, and it names the field so an operator who pasted a whole
// confirmation email is told which one to shorten rather than meeting a
// constraint violation.
func validateTransferReference(raw *string) (*string, []platform.FieldError) {
	if raw == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, nil
	}
	if len([]rune(trimmed)) > sales.MaxPayoutTransferReferenceLength {
		return nil, []platform.FieldError{{
			Field:   "transfer_reference",
			Code:    platform.CodeTooLong,
			Message: "must be at most " + strconv.Itoa(sales.MaxPayoutTransferReferenceLength) + " characters",
		}}
	}
	return &trimmed, nil
}

// DeclinePayoutRequest refuses a request, with the reason its asker is shown.
//
// @Summary      Decline a Payout Request, with a reason the Organization reads
// @Description  Marks the Payout Request `declined` and records why, who said so (taken from the Staff Session, never from the body) and when. THE REASON IS REQUIRED — a blank or whitespace-only one is refused with a field error, and it is bounded at 500 characters — because a queue that swallowed requests silently would generate the support thread it was built to prevent (ADR 0026). The reason is shown to the Organization on its own payouts page, beside the ask it answers. Nothing moves: a decline records no Payout and touches no balance. A decline is a "not this" rather than a lockout — it ends this request, and the Organization may submit a new one immediately, since a resolved request no longer occupies its single outstanding slot. ONLY A `pending` REQUEST MAY BE DECLINED: once an operator has submitted the transfer the request is `processing`, the platform cannot refuse an ask its own operator is acting on, and the answer to a transfer the bank sends back is `failed` rather than `declined` — so it is 409 PAYOUT_REQUEST_TRANSFER_ALREADY_SUBMITTED naming who submitted it. All end states are final, so declining a request that was already paid, declined or cancelled is 409 PAYOUT_REQUEST_ALREADY_RESOLVED naming the state and who reached it. An unknown or malformed id is 404 PAYOUT_REQUEST_NOT_FOUND. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        requestID  path  string                    true  "Payout Request ID"
// @Param        body       body  declinePayoutRequestBody  true  "Why the request is refused"
// @Success      200  {object}  openapi.EnvelopeOperatorPayoutRequest
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/payout-requests/{requestID}/decline [post]
func (h *Handler) DeclinePayoutRequest(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body declinePayoutRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	reason, fields := validateDeclineReason(body.Reason)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	request, err := h.svc.DeclinePayoutRequest(r.Context(), strings.TrimSpace(r.PathValue("requestID")), service.DeclinePayoutRequestInput{
		Reason:   reason,
		Operator: session.Email,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, request)
}

// validateDeclineReason insists on a reason and bounds it.
//
// Missing, empty and whitespace-only are one failure and not three: all of them
// reach the asker as a blank, which is the exact outcome requiring a reason
// exists to prevent. The trimmed value is what gets stored, so no reason ever
// arrives padded.
func validateDeclineReason(raw string) (string, []platform.FieldError) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", []platform.FieldError{{
			Field:   "reason",
			Code:    platform.CodeRequired,
			Message: "is required — the Organization is shown this",
		}}
	}
	if len([]rune(trimmed)) > declineReasonMaxLength {
		return "", []platform.FieldError{{
			Field:   "reason",
			Code:    platform.CodeTooLong,
			Message: "must be at most " + strconv.Itoa(declineReasonMaxLength) + " characters",
		}}
	}
	return trimmed, nil
}
