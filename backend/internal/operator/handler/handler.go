// Package handler exposes the Platform Operator's HTTP endpoints — the
// Operator Dashboard's reads and its writes: recording a Payout, recording an
// Operator Reversal, answering a Payout Request by fulfilling or declining
// it (#177), and designating a House Organization (#472).
//
// Every route under /api/v1/operator/* is gated by the operator middleware,
// which admits a Staff Session whose email is on the platform operator
// allowlist and nothing else. No handler here takes an Active Member: operator
// authority is orthogonal to Membership (ADR 0015).
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Organization list pagination bounds (ADR-0006): page_size defaults to 50 and
// is clamped to a maximum of 100; page floors at 1.
const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// payoutDateFormat is the calendar day a Payout is recorded against. A date, not
// an instant: it is the day the bank moved the money, and reading it as a
// timestamp would invite a timezone to shift it onto the wrong day.
const payoutDateFormat = "2006-01-02"

// Handler exposes HTTP endpoints for the operator surface.
type Handler struct {
	svc *service.Service
}

// New returns an operator HTTP handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// GetSummary returns the platform's own money, grouped by currency.
//
// @Summary      Get platform revenue and what the platform owes
// @Description  Returns the Operator Dashboard headline: for each currency in use, the accumulated Platform Fee and Fee IVA (summed from the amounts every active Online Sale line snapshotted) and total_owed_cents — the sum of the POSITIVE Withdrawable Balances, so an Organization that owes the platform after a post-settlement reversal never reduces what the platform must keep on hand. One row per currency and no exchange rate anywhere; in practice one USD row (ADR 0015). Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopePlatformSummary
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/summary [get]
func (h *Handler) GetSummary(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	summary, err := h.svc.Summary(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, summary)
}

// ListOrganizations returns a page of every Organization on the platform.
//
// @Summary      List every Organization with its Withdrawable Balance
// @Description  Returns a page of every Organization on the platform — name, slug, currency, Event count across all statuses, and withdrawable_balance_cents, which is SIGNED (negative when the Organization owes the platform after a sale was reversed post-settlement). Sorted by name ascending with an id tiebreaker so equal names keep a stable order across pages. Response is the ADR-0006 nested envelope { data, pagination }; page_size defaults to 50 (max 100) and page floors at 1. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "Page number (1-based; floors at 1)"
// @Param        page_size  query  int  false  "Page size (default 50, max 100)"
// @Success      200  {object}  openapi.EnvelopeOperatorOrganizationList
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/organizations [get]
func (h *Handler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	query := r.URL.Query()
	result, err := h.svc.ListOrganizations(r.Context(), pageParam(query.Get("page")), pageSizeParam(query.Get("page_size")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// GetOrganization returns one Organization's operator drill-down.
//
// @Summary      Get one Organization's Events and payout history
// @Description  Returns the operator's drill-down into one Organization: the Organization itself, its signed Withdrawable Balance, its signed Payable Balance — the same arithmetic counting only sales recorded before today in America/Guayaquil with no Reversal Request still open (ADR 0026), never larger than the Withdrawable Balance and negative when a settlement got ahead of what had cleared — its Events in every status (soonest-last, unscheduled Events last), and its full payout history newest first with the recording operator's email, null for Payouts entered directly in the database before the Operator Dashboard existed. Both balances are shown to the operator and neither gates recording a Payout, which stays unconditional (ADR 0015). Carries sale_invoicing_enabled, the platform's SALE_INVOICING_ENABLED flag rather than a fact about this Organization, so the staff app knows whether to offer the House Organization designation at all. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        orgID  path  string  true  "Organization ID"
// @Success      200  {object}  openapi.EnvelopeOperatorOrganizationDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/organizations/{orgID} [get]
func (h *Handler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	detail, err := h.svc.GetOrganization(r.Context(), strings.TrimSpace(r.PathValue("orgID")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, detail)
}

// recordPayoutBody is a Payout an operator is recording after settling
// off-platform.
type recordPayoutBody struct {
	AmountCents int     `json:"amount_cents"`
	PaidAt      string  `json:"paid_at"`
	Note        *string `json:"note"`
}

// RecordPayout records a Payout against an Organization.
//
// @Summary      Record a Payout for an Organization
// @Description  Records a settlement against an Organization — amount in cents, the calendar day the money left the bank (YYYY-MM-DD), and an optional note — stamped with the recording operator's email, and returns the created entry. The amount is NEVER refused for exceeding the Withdrawable Balance: by recording time the money has already moved, so an over-balance Payout is accepted and drives the signed balance negative (ADR 0015). A Payout has no states and cannot be edited or deleted. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        orgID  path  string            true  "Organization ID"
// @Param        body   body  recordPayoutBody  true  "Payout to record"
// @Success      201  {object}  openapi.EnvelopeOperatorPayout
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/organizations/{orgID}/payouts [post]
func (h *Handler) RecordPayout(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body recordPayoutBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	input, fields := validateRecordPayout(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	// The audit trail is taken from the session, never from the body: "who
	// entered this number" must not be something a caller can claim.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	input.RecordedBy = session.Email

	payout, err := h.svc.RecordPayout(r.Context(), strings.TrimSpace(r.PathValue("orgID")), input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, payout)
}

// validateRecordPayout checks the record-payout body, returning the service
// input and any field errors. The Withdrawable Balance is deliberately not among
// the checks — that is a fact about the Organization, not about this request.
func validateRecordPayout(body recordPayoutBody) (service.RecordPayoutInput, []platform.FieldError) {
	var fields []platform.FieldError

	if body.AmountCents <= 0 {
		fields = append(fields, platform.FieldError{Field: "amount_cents", Code: platform.CodeInvalidPositiveInt, Message: "must be greater than zero"})
	}

	var paidAt time.Time
	raw := strings.TrimSpace(body.PaidAt)
	if raw == "" {
		fields = append(fields, platform.FieldError{Field: "paid_at", Code: platform.CodeRequired, Message: "is required"})
	} else if parsed, err := time.Parse(payoutDateFormat, raw); err != nil {
		fields = append(fields, platform.FieldError{Field: "paid_at", Code: platform.CodeInvalidDate, Message: "must be a date (YYYY-MM-DD)"})
	} else {
		paidAt = parsed
	}

	var note *string
	if body.Note != nil {
		if trimmed := strings.TrimSpace(*body.Note); trimmed != "" {
			note = &trimmed
		}
	}

	return service.RecordPayoutInput{
		AmountCents: body.AmountCents,
		PaidAt:      paidAt,
		Note:        note,
	}, fields
}

// pageParam parses the `page` query value, flooring at 1: a missing, invalid, or
// below-one value becomes page 1.
func pageParam(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// pageSizeParam parses the `page_size` query value, defaulting to 50 and
// clamping to [1, 100].
func pageSizeParam(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return defaultPageSize
	}
	if n > maxPageSize {
		return maxPageSize
	}
	return n
}

// LookUpSale returns one Ticket Sale by its Sale Confirmation reference.
//
// @Summary      Look up a Ticket Sale by its Sale Confirmation reference
// @Description  Returns one Ticket Sale named by its Sale Confirmation reference (e.g. TP-3F9K2), across EVERY Organization on the platform — the operator is a Member of none, and the flow always starts from a support thread that carries a reference and nothing else. The payload identifies the sale before anybody acts on it: the Event and the Organization it belongs to, the buyer as the sale snapshotted them, the rolled-up Ticket Types and ticket count, the amount collected split into Platform Fee, Fee IVA and Net Proceeds (the snapshots frozen at sale time, so a later rate change moves none of them), the Sales Channel, the Payment Method, the status, and the Sale Reversal provenance on a reversed row. operator_reversal carries the money memo an Operator Reversal left — the acting operator, what they said the buyer got back, whether the platform kept its fee, and their note — and is null on every sale reversed any other way; it is operator-facing only and appears on no Organization surface. reversal_window_closes_at is when this sale's Reversal Window shuts — the earlier of 20:00 Ecuador time on the day of purchase and the Event's start — and is null on a sale that never had one (anything but an Online Sale, or an Event with no recorded start); reversal_window_passed is true once it has shut, and true as well when there was never a window. Matching ignores the reference's case, since a quoted reference loses it. A reversed sale is found exactly as an active one is: it keeps its reference and is never deleted. Read-only. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        confirmationRef  path  string  true  "Sale Confirmation reference (case-insensitive)"
// @Success      200  {object}  openapi.EnvelopeOperatorSaleLookup
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/sales/{confirmationRef} [get]
func (h *Handler) LookUpSale(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	found, err := h.svc.LookUpSale(r.Context(), strings.TrimSpace(r.PathValue("confirmationRef")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, found)
}

// reversalNoteMaxLength bounds the free-text note an Operator Reversal carries.
// A note is a sentence — "refunded via PayPhone dashboard", "bank transfer,
// waived fee as goodwill" — and the bound is the schema's, stated here so the
// caller is told which field is wrong rather than shown a constraint violation.
const reversalNoteMaxLength = 500

// reverseSaleBody is an Operator Reversal as the operator states it.
//
// The money fields are pointers because absent, zero and false are three
// different answers. Absent is the free Online Sale's answer — nothing was
// collected, so there is nothing to refund and no fee to keep (#126) — and on a
// paid sale it is a refusal rather than a default, because a defaulted amount
// or fee decision would put a claim about somebody's money in the schema's
// mouth instead of the operator's.
type reverseSaleBody struct {
	RefundedAmountCents *int    `json:"refunded_amount_cents"`
	PlatformFeeKept     *bool   `json:"platform_fee_kept"`
	Note                *string `json:"note"`
}

// ReverseSale records an Operator Reversal against one Ticket Sale.
//
// @Summary      Record an out-of-band refund and reverse a Ticket Sale
// @Description  Marks the active Online Sale named by a Sale Confirmation reference `reversed`, recording that the Platform Operator already refunded the buyer OFF-PLATFORM — by hand in the Payment Provider's dashboard, or by bank transfer. It is a pure record: the Payment Provider is NEVER called from this endpoint, so recording a refund that already happened can never trigger a second one. The Payment stays `approved` (the checkout genuinely settled; the reversal is a later event on the Sale). On commit the sale carries the third reversal actor `operator` with the acting operator's email (taken from the Staff Session, never from the body) and the moment, capacity returns to the Ticket Types, and the buyer receives the same Sale Voided email every reversal sends. The money memo depends on what the sale COLLECTED. On a paid sale refunded_amount_cents and platform_fee_kept are both required, with no pre-fill and no default: the amount is strictly positive and at most what the sale collected, and the one fee flag decides both the Platform Fee and its Fee IVA, which always travel together. On a FREE Online Sale both are refused — it collected nothing, so there was nothing to refund and no fee to keep, and the record says so with nulls rather than zeros. The two always travel together: giving one without the other is refused on any sale. note is optional and at most 500 characters on both. There is NO Reversal Window check on this path — a sale inside its window is marked exactly as one past it, which is the point of the operation. Refused for a sale that is not an Online Sale (an imported sale is undone through its Sale Import) and for one already reversed, including when the buyer's own undo committed first. Irreversible: no un-reversal exists. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        confirmationRef  path  string           true  "Sale Confirmation reference (case-insensitive)"
// @Param        body             body  reverseSaleBody  true  "What the buyer got back, and whether the platform kept its fee"
// @Success      200  {object}  openapi.EnvelopeOperatorSaleReversal
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Failure      503  {object}  platform.Envelope
// @Router       /api/v1/operator/sales/{confirmationRef}/reverse [post]
func (h *Handler) ReverseSale(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body reverseSaleBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	input, fields := validateReverseSale(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	// Who asserted this comes from the session and nowhere else. A money
	// assertion made on human say-so is never anonymous, and never signed by
	// somebody the caller named.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	input.Operator = session.Email

	result, err := h.svc.ReverseSale(r.Context(), strings.TrimSpace(r.PathValue("confirmationRef")), input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// validateReverseSale checks the marking's shape: the two money facts stated
// together or not at all, a stated refund positive, the note within bounds.
//
// What it deliberately does NOT check is either question that needs the sale —
// whether money must be stated at all, and whether a stated refund exceeds what
// was collected. Both are facts about the Ticket Sale rather than about this
// request, so they are judged where the sale is loaded: a free Online Sale takes
// neither money fact and a paid one requires both (#126). This is the same split
// the record payout body makes against the Withdrawable Balance.
func validateReverseSale(body reverseSaleBody) (service.OperatorReversalInput, []platform.FieldError) {
	var fields []platform.FieldError

	// The pair rule holds on every sale, free or paid: an amount with no fee
	// decision cannot say what the platform kept, and a fee decision with no
	// amount cannot say what the buyer got. Neither is ever defaulted into
	// existence — a pre-filled amount invites rubber-stamping, and a defaulted
	// fee decision would record a revenue choice nobody made.
	if body.RefundedAmountCents == nil && body.PlatformFeeKept != nil {
		fields = append(fields, platform.FieldError{
			Field:   "refunded_amount_cents",
			Code:    platform.CodeRequiredWithPlatformFeeKept,
			Message: "is required when platform_fee_kept is given",
		})
	}
	if body.PlatformFeeKept == nil && body.RefundedAmountCents != nil {
		fields = append(fields, platform.FieldError{
			Field:   "platform_fee_kept",
			Code:    platform.CodeRequiredWithRefundedAmount,
			Message: "is required when refunded_amount_cents is given",
		})
	}
	// Zero is refused rather than read as "nothing to refund": absence is how
	// that is said, and a refund of nothing is not a refund.
	if body.RefundedAmountCents != nil && *body.RefundedAmountCents <= 0 {
		fields = append(fields, platform.FieldError{Field: "refunded_amount_cents", Code: platform.CodeInvalidPositiveInt, Message: "must be greater than zero"})
	}

	var note *string
	if body.Note != nil {
		if trimmed := strings.TrimSpace(*body.Note); trimmed != "" {
			if len([]rune(trimmed)) > reversalNoteMaxLength {
				fields = append(fields, platform.FieldError{
					Field:   "note",
					Code:    platform.CodeTooLong,
					Message: "must be at most " + strconv.Itoa(reversalNoteMaxLength) + " characters",
				})
			}
			note = &trimmed
		}
	}

	return service.OperatorReversalInput{
		RefundedAmountCents: body.RefundedAmountCents,
		PlatformFeeKept:     body.PlatformFeeKept,
		Note:                note,
	}, fields
}
