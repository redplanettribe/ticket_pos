// Package handler exposes the Platform Operator's HTTP endpoints — the
// Operator Dashboard's reads and its one write, recording a Payout.
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
// @Description  Returns the operator's drill-down into one Organization: the Organization itself, its signed Withdrawable Balance, its Events in every status (soonest-last, unscheduled Events last), and its full payout history newest first with the recording operator's email — null for Payouts entered directly in the database before the Operator Dashboard existed. Platform Operator only.
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
		fields = append(fields, platform.FieldError{Field: "amount_cents", Message: "must be greater than zero"})
	}

	var paidAt time.Time
	raw := strings.TrimSpace(body.PaidAt)
	if raw == "" {
		fields = append(fields, platform.FieldError{Field: "paid_at", Message: "is required"})
	} else if parsed, err := time.Parse(payoutDateFormat, raw); err != nil {
		fields = append(fields, platform.FieldError{Field: "paid_at", Message: "must be a date (YYYY-MM-DD)"})
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
// @Description  Returns one Ticket Sale named by its Sale Confirmation reference (e.g. TP-3F9K2), across EVERY Organization on the platform — the operator is a Member of none, and the flow always starts from a support thread that carries a reference and nothing else. The payload identifies the sale before anybody acts on it: the Event and the Organization it belongs to, the buyer as the sale snapshotted them, the rolled-up Ticket Types and ticket count, the amount collected split into Platform Fee, Fee IVA and Net Proceeds (the snapshots frozen at sale time, so a later rate change moves none of them), the Sales Channel, the Payment Method, the status, and the Sale Reversal provenance on a reversed row. reversal_window_closes_at is when this sale's Reversal Window shuts — the earlier of 20:00 Ecuador time on the day of purchase and the Event's start — and is null on a sale that never had one (anything but an Online Sale, or an Event with no recorded start); reversal_window_passed is true once it has shut, and true as well when there was never a window. Matching ignores the reference's case, since a quoted reference loses it. A reversed sale is found exactly as an active one is: it keeps its reference and is never deleted. Read-only. Platform Operator only.
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
