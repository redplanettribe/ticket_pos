package handler

import (
	"encoding/json"
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// payoutProfileBody is a Payout Profile as an Org Admin states it. Every field
// is required — the profile is stated whole or not at all (ADR 0026) — so there
// are no pointers here to tell "left out" from "cleared": both are simply
// missing, and both are refused by name.
type payoutProfileBody struct {
	BankName          string `json:"bank_name"`
	AccountType       string `json:"account_type"`
	AccountNumber     string `json:"account_number"`
	AccountHolderName string `json:"account_holder_name"`
	TaxIDType         string `json:"tax_id_type"`
	TaxIDNumber       string `json:"tax_id_number"`
}

// GetOrganizationPayoutProfile returns where the Organization is paid.
//
// @Summary      Get the Organization's Payout Profile
// @Description  Returns the acting Member's Organization's Payout Profile — the bank, whether the account is `ahorros` or `corriente`, the account number, the name on the account, and the Organization's own Tax ID for the factura (ADR 0026). The data payload is `null` when the Organization has never recorded one, which is an ordinary state rather than an error. The account number is returned whole, because this is the Organization's own editor; masking belongs to the operator surfaces that show many Organizations at once. Org Admin only — an Event Owner or Event Staff is refused.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopePayoutProfile
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/staff/organization/payout-profile [get]
func (h *Handler) GetOrganizationPayoutProfile(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	profile, err := h.svc.OrganizationPayoutProfile(r.Context(), actorFromRequest(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, profile)
}

// UpdateOrganizationPayoutProfile records where the Organization is paid,
// replacing whatever was there.
//
// It is a PUT and not a PATCH because the profile is one instruction rather than
// a bag of independent fields: half a bank account is not a smaller bank
// account, it is one nobody can transfer to.
//
// @Summary      Set the Organization's Payout Profile
// @Description  Records where the acting Member's Organization is paid, replacing any existing profile — there is at most one per Organization (ADR 0026). Every field is required. The account number is normalised by stripping spaces and dashes and must then be digits only; its leading zeros are preserved exactly, because an account number that loses one reaches the wrong account. The Tax ID identifies the party being paid and invoiced, so it is `cedula` or `ruc` and never `passport`, validated with the same check digits as a buyer's Tax ID (ADR 0016). Bad fields come back one by one as VALIDATION_FAILED, naming the field and never echoing what was typed. Org Admin only.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      handler.payoutProfileBody  true  "Payout Profile"
// @Success      200  {object}  openapi.EnvelopePayoutProfile
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/staff/organization/payout-profile [put]
func (h *Handler) UpdateOrganizationPayoutProfile(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body payoutProfileBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	// What makes bank details acceptable is a domain rule, not a handler one, so
	// it lives in package sales where the Payout Request will reach for the same
	// verdict rather than growing a second one.
	profile, fieldErrs := sales.PayoutProfile{
		BankName:          body.BankName,
		AccountType:       body.AccountType,
		AccountNumber:     body.AccountNumber,
		AccountHolderName: body.AccountHolderName,
		TaxIDType:         body.TaxIDType,
		TaxIDNumber:       body.TaxIDNumber,
	}.Normalize()
	if len(fieldErrs) > 0 {
		_ = platform.WriteValidationError(w, reqID, fieldErrs)
		return
	}

	saved, err := h.svc.SavePayoutProfile(r.Context(), actorFromRequest(r), profile)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, saved)
}
