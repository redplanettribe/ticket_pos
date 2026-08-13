package handler

import (
	"encoding/json"
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The passcode door of the Consent Withdrawal surface (#270, parent #265,
// ADR 0039): Proof of Email Ownership that buys a withdrawal and nothing else.
//
// IT IS A ROUTE OF ITS OWN RATHER THAN A FLAG ON THE SIGN-IN VERIFY, and that is
// the decision this file exists to make structural. The promise is that nobody
// exercising a right here is left signed in on a shared machine, and the only
// way to keep it is for this path to contain no branch that mints a session. A
// `withhold_session` field on the sign-in door would have moved that promise into
// a client's request body, where the next reader of that handler would find a
// door that mints a session unless asked not to.
//
// It is not a second credential. What it hands back is the same short-lived,
// single-use `pending_consents` row the sign-in door mints when it holds a
// sign-in for consent — the same table, the same lifetime, the same single
// purchase — and it is spent at the same endpoint.

// consentWithdrawalProofBody is the address and the passcode, and nothing else.
//
// No Follow intent, deliberately: a Follow is written against a session, this
// door mints none, and a field that could never be honoured is a field somebody
// will one day try to honour.
type consentWithdrawalProofBody struct {
	Email string `json:"email"`
	Code  string `json:"code"`
	// Locale is the language of the Storefront page the withdrawal is being made
	// from, remembered as the Customer's Mail Locale exactly as it is on the
	// sign-in door (ADR 0033). It matters more here than there: the confirmation
	// of the withdrawal is written in it, and a message about somebody's legal
	// rights is the last one that may arrive in a language they cannot read.
	// Optional, and a language the platform does not serve is dropped rather than
	// made a reason a person cannot exercise a right.
	Locale string `json:"locale"`
}

// ProveEmailForConsentWithdrawal redeems a passcode for the token that buys a
// Consent Withdrawal.
//
// @Summary      Prove email ownership to withdraw consent
// @Description  Redeems a Customer one-time passcode for the short-lived, single-use `pending_consent_token` that a Consent Withdrawal is submitted with, for a Customer who will not or cannot sign in (ADR 0039). IT MINTS NO CUSTOMER SESSION AND NEVER CAN — not for a Customer who owes a Policy Acceptance, and not for one who has accepted the current edition and would have been signed straight in by the ordinary verify — so proving an address in order to withdraw does not leave anybody logged in on a shared machine. The passcode is the ordinary Customer passcode requested at /api/v1/customer/auth/otp/request, redeemed here instead of at the verify: it is proof of email ownership either way, so the Customer record is created or reused and marked verified exactly as a sign-in would, and an optional `locale` is remembered as the Customer's Mail Locale, which is the language the withdrawal's confirmation email is written in. The token it returns is the same credential a held sign-in returns and is spent at the same endpoint, /api/v1/customer/auth/consent, where a submission whose every answer is a denial needs no `policy_acceptance` and one containing any grant still does. Nothing about a Customer is disclosed here: the response is the token and its expiry, never their consent state, and a wrong passcode fails exactly as it fails on the sign-in door.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Param        body  body      consentWithdrawalProofBody  true  "Email and passcode"
// @Success      200   {object}  openapi.EnvelopeCustomerConsentWithdrawalProof
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      429   {object}  platform.Envelope
// @Router       /api/v1/customer/consent/withdrawal/passcode/verify [post]
func (h *Handler) ProveEmailForConsentWithdrawal(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body consentWithdrawalProofBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	fields := validateEmail(body.Email)
	fields = append(fields, validateOTPCode(body.Code)...)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	view, err := h.svc.ProveEmailForConsentWithdrawal(r.Context(), body.Email, body.Code, body.Locale)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}
