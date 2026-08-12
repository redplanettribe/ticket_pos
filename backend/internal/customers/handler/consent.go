package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/customers/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// consentSubmitBody is a held sign-in being finished: the token, the boxes, and
// the Follow that has been waiting since before anybody knew who this was.
//
// WHAT IS NOT IN IT is the important half. There is no email — the address is
// the one the pending-consent token was minted for, so this request cannot
// point a consent at somebody else's inbox even in principle, exactly as the
// Google door cannot name an address. There is no Policy Version either: which
// edition is being accepted is resolved server-side at the moment of capture,
// because that is the platform's finding about the moment and not a client's
// assertion about it.
type consentSubmitBody struct {
	// PendingConsentToken is the credential from the consent-required outcome.
	PendingConsentToken string `json:"pending_consent_token"`
	// PolicyAcceptance is the required box. Absent is false, and false is
	// refused: the API is the guarantee, the disabled submit button is a
	// courtesy.
	PolicyAcceptance bool `json:"policy_acceptance"`
	// MarketingConsent and NetworkingConsent are the optional boxes. ABSENT IS
	// FALSE AND FALSE IS AN EXPLICIT NO — an unticked box that was shown is a
	// refusal, recorded as `denied`, and for Marketing that turns the Follow
	// Digest off (ADR 0034). This is the one place in the API where a missing
	// JSON field means something, and it means it because that is what the
	// surface means: the boxes are rendered unticked and a person who submits
	// without touching them has answered.
	//
	// An answer for a box this Customer was not shown is ignored by the service,
	// which recomputes what they were owed rather than trusting this body.
	MarketingConsent  bool `json:"marketing_consent"`
	NetworkingConsent bool `json:"networking_consent"`
	// Follow is the Follow intent, relayed here for the same reason it is
	// relayed on the verify: this is now the request that produces a session, and
	// a Follow is written against the session a sign-in produced and against
	// nothing a body could name (#219). A visitor who pressed Follow and was
	// then stopped for consent must not silently lose it.
	Follow string `json:"follow"`
}

// SubmitConsent exchanges a pending-consent token and the answers for the
// Customer Session that proof of email ownership did not mint (#251).
//
// Unauthenticated, because the token IS the credential — the same reason
// passcode verification is. It reveals nothing to anybody who does not already
// hold one: an unknown, spent or expired token gets one indistinguishable
// refusal, so this endpoint cannot be asked whether an address exists, whether
// it has consent outstanding, or whether somebody else's sign-in is in flight.
//
// @Summary      Submit sign-in consent
// @Description  Finishes a sign-in that was held for consent: exchanges the short-lived, single-use `pending_consent_token` from a verify response for the Customer Session that verification withheld. Writes the immutable Consent Record first — answers, channel, Policy Version, and the technical proof (IP, user agent, session, origin URL) — and mints the session on the far side of it, so nobody is ever signed in without evidence of what they authorized. `policy_acceptance` is required: a submission without it is refused by the API with POLICY_ACCEPTANCE_REQUIRED, not merely disabled in a form. The optional `marketing_consent` and `networking_consent` default to false, and false is an explicit No — it records `denied` and, for marketing, switches the weekly Follow Digest off (ADR 0034). Answers for boxes the Customer was not shown are ignored: standing optional answers are never churned. The Policy Version is resolved server-side and is never accepted from the request. An optional `follow` carries a Follow intent, honoured against the session this call mints exactly as on the verify routes. The token is spent whatever the outcome, so a refused submission is restarted by signing in again; abandoning the step leaves no session at all.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Param        body  body      consentSubmitBody  true  "Pending consent token and answers"
// @Success      200   {object}  openapi.EnvelopeCustomerVerifyOTP
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Router       /api/v1/customer/auth/consent [post]
func (h *Handler) SubmitConsent(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body consentSubmitBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	var fields []platform.FieldError
	if strings.TrimSpace(body.PendingConsentToken) == "" {
		fields = append(fields, platform.FieldError{
			Field:   "pending_consent_token",
			Code:    platform.CodeRequired,
			Message: "is required",
		})
	}
	// Parsed before the token is spent, exactly as it is before a passcode is:
	// a malformed intent must not cost somebody the one proof they had.
	intent, intentFields := service.ParseFollowIntent(body.Follow)
	fields = append(fields, intentFields...)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	outcome, err := h.svc.SubmitConsent(r.Context(), service.ConsentSubmission{
		Token:             body.PendingConsentToken,
		PolicyAcceptance:  body.PolicyAcceptance,
		MarketingConsent:  body.MarketingConsent,
		NetworkingConsent: body.NetworkingConsent,
		// The prueba técnica is derived from the REQUEST and never from the body —
		// see consent.EvidenceFromRequest, which every capture surface shares.
		Evidence: consent.EvidenceFromRequest(r),
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, h.signInResponse(r.Context(), outcome, intent))
}
