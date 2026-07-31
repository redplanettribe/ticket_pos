package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/service"
)

// payoutRequestBody is a Payout Request as an Org Admin submits one: an amount,
// an optional note, and — optionally — the bank details they corrected on the
// form.
//
// payout_profile is a POINTER because its absence is meaningful and its presence
// is an edit. Omitted means "pay me where you already know", and supplied means
// the organizer changed something, which IS a change to the Payout Profile: the
// request form is the profile editor, and correcting the same typo in two places
// is worse than the alternative (ADR 0026).
type payoutRequestBody struct {
	AmountCents int                `json:"amount_cents"`
	Note        string             `json:"note"`
	Profile     *payoutProfileBody `json:"payout_profile"`
}

// SubmitPayoutRequest records the Organization's ask to be paid.
//
// @Summary      Submit a Payout Request
// @Description  Records the acting Member's Organization's ask to be paid (ADR 0026): an amount in the Organization's currency, an optional note, and the bank details to pay it to. The bank details may be supplied as `payout_profile` — the request form is the Payout Profile editor, so anything sent there is validated exactly as PUT /staff/organization/payout-profile validates it and SAVED to the profile as well as snapshotted onto the request. Omit `payout_profile` to be paid where the stored profile says; a complete profile is required either way, and an Organization with none is refused with VALIDATION_FAILED naming each missing field. The ask is bounded by the Payable Balance AT THE MOMENT IT IS MADE and never again — one cent above it is refused with PAYOUT_REQUEST_EXCEEDS_PAYABLE_BALANCE, and the balance moving afterwards changes nothing about a recorded request. An Organization may have only one request outstanding at a time, enforced by a partial unique index: a second submission while one is OUTSTANDING — `pending`, or `processing` because an operator has submitted the transfer and the bank has not confirmed it — returns 200 with the EXISTING request rather than a conflict, so an organizer learns where their earlier ask went, and nothing about the outstanding request is edited by it. A newly recorded request returns 201. The request snapshots the six profile fields and the Payable Balance as they stood, and no later profile edit alters them. The asker is recorded as an email, so the record outlives their Membership. A request moves no money: no balance, no aggregate and no revenue figure knows requests exist. Org Admin only.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      handler.payoutRequestBody  true  "Payout Request"
// @Success      201  {object}  openapi.EnvelopePayoutRequest
// @Success      200  {object}  openapi.EnvelopePayoutRequest
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/organization/payout-requests [post]
func (h *Handler) SubmitPayoutRequest(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body payoutRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	input, fieldErrs := payoutRequestInput(body)
	if len(fieldErrs) > 0 {
		_ = platform.WriteValidationError(w, reqID, fieldErrs)
		return
	}

	result, fieldErrs, err := h.svc.RequestPayout(r.Context(), actorFromRequest(r), input)
	if len(fieldErrs) > 0 {
		// The Organization has nowhere to be paid. It is VALIDATION_FAILED rather
		// than a domain error because the answer is a list of fields the organizer
		// fills in, and the form puts each message beside the input it is about.
		_ = platform.WriteValidationError(w, reqID, fieldErrs)
		return
	}
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	// 201 for a newly recorded ask, 200 for the outstanding one handed back. The
	// distinction is the whole of how a caller tells "recorded" from "you already
	// asked" without an error code, and the body is the same shape either way.
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	_ = platform.WriteSuccess(w, reqID, status, result.Request)
}

// ListPayoutRequests returns the Organization's own Payout Request history.
//
// @Summary      List the Organization's Payout Requests
// @Description  Returns the acting Member's Organization's Payout Requests, newest first, for the request history shown beside the payout history (ADR 0026). Each carries the amount asked for, the optional note, the status (`pending`, `processing` while the transfer is in flight, then `paid`, `failed`, `declined` or `cancelled`), the asker's email, the Payable Balance as it stood at the moment of asking, the frozen snapshot of the six Payout Profile fields the ask was made against — which no later profile edit alters — and, once answered, the resolver's email, the resolution instant, the decline reason on a decline, and the id of the Payout that settled it. No currency is returned: a request is always in the Organization's own currency, which the payouts summary this history sits beside states once. Org Admin only.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopePayoutRequests
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/staff/organization/payout-requests [get]
func (h *Handler) ListPayoutRequests(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	requests, err := h.svc.ListPayoutRequests(r.Context(), actorFromRequest(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, requests)
}

// CancelPayoutRequest withdraws the Organization's outstanding ask.
//
// It is a POST to a verb and not a DELETE, because nothing is deleted: a
// cancelled request stays in the history saying it was asked for and withdrawn.
//
// @Summary      Cancel an outstanding Payout Request
// @Description  Withdraws the acting Member's Organization's outstanding Payout Request (ADR 0026). Cancelling is the only change an Organization can make to a pending request — it cannot be edited, only cancelled and re-asked, which is what keeps "outstanding" singular and stops an operator looking at a figure that changed under them. The row is kept and moves to `cancelled`, stamped with the cancelling Member's email and the instant, which frees the Organization to submit a new request. It is a compare-and-swap on the PENDING state, and only that state: ONCE AN OPERATOR HAS SUBMITTED THE TRANSFER the request is `processing` and can no longer be cancelled — the bank is already acting on the ask, and withdrawing it would leave a confirmed transfer with nothing to attach it to — so it is refused with 409 PAYOUT_REQUEST_NOT_PENDING whose message says the transfer is already being processed and may take up to 48 hours. A request an operator paid or declined in the meantime is refused the same way, naming the state it actually reached, and a request that is not this Organization's is 404 PAYOUT_REQUEST_NOT_FOUND. Nothing is ever locked. Org Admin only.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        requestId  path  string  true  "Payout Request ID"
// @Success      200  {object}  openapi.EnvelopePayoutRequest
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/organization/payout-requests/{requestId}/cancel [post]
func (h *Handler) CancelPayoutRequest(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	requestID := strings.TrimSpace(r.PathValue("requestId"))
	if requestID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "requestId", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}
	// The column is UUID NOT NULL, so a malformed id would otherwise surface as a
	// database error and a 500 rather than the not-found it actually is.
	if _, err := uuid.Parse(requestID); err != nil {
		_ = platform.WriteDomainError(w, reqID, sales.ErrPayoutRequestNotFound())
		return
	}

	cancelled, err := h.svc.CancelPayoutRequest(r.Context(), actorFromRequest(r), requestID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, cancelled)
}

// payoutRequestInput validates the parts of a submission the handler owns — the
// amount and the note — and hands the bank details to the profile's own
// validator rather than restating any of its rules.
//
// The amount is checked for being an ask at all; whether it is an ask the
// Organization may make is the Payable Balance's business, decided in the
// service where the balance is read. Splitting it that way keeps the two
// refusals honestly different: "that is not an amount" is a typo, and "that is
// more than has cleared" is a fact about the money.
func payoutRequestInput(body payoutRequestBody) (service.RequestPayoutInput, []platform.FieldError) {
	var fieldErrs []platform.FieldError

	if body.AmountCents <= 0 {
		fieldErrs = append(fieldErrs, platform.FieldError{
			Field:   "amount_cents",
			Code:    platform.CodeInvalidPositiveInt,
			Message: "must be greater than zero",
		})
	}

	note, hasNote := sales.NormalizePayoutRequestNote(body.Note)
	if len([]rune(note)) > sales.MaxPayoutRequestNoteLength {
		fieldErrs = append(fieldErrs, platform.FieldError{
			Field:   "note",
			Code:    platform.CodeTooLong,
			Message: "must be at most 500 characters",
		})
	}

	input := service.RequestPayoutInput{AmountCents: body.AmountCents}
	if hasNote {
		input.Note = &note
	}

	if body.Profile != nil {
		profile, profileErrs := sales.PayoutProfile{
			BankName:          body.Profile.BankName,
			AccountType:       body.Profile.AccountType,
			AccountNumber:     body.Profile.AccountNumber,
			AccountHolderName: body.Profile.AccountHolderName,
			TaxIDType:         body.Profile.TaxIDType,
			TaxIDNumber:       body.Profile.TaxIDNumber,
		}.Normalize()
		if len(profileErrs) > 0 {
			// The field names are the profile editor's own, unprefixed, because
			// they are the same fields shown in the same place. A caller keying
			// copy on `account_number` gets one answer wherever it was refused.
			fieldErrs = append(fieldErrs, profileErrs...)
		} else {
			input.Profile = &profile
		}
	}

	return input, fieldErrs
}
