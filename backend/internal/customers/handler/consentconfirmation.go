package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The confirmation link that resolves a Pending Confirmation (#255, parent
// #249, ADR 0035): the double opt-in's second half, reached from an inbox.
//
// IT IS A POST AND THERE IS NO GET, and here that is not merely the unsubscribe
// link's precaution repeated — it is the reason the whole design is safe. Mail
// security scanners open every link in every message before a human sees it. A
// GET that acted would let a scanner GRANT a marketing opt-in that nobody ever
// confirmed, manufacturing exactly the consent this feature exists to prove was
// given: the platform would hold an evidence row saying an address confirmed
// itself, produced by a robot. The link points at a Storefront PAGE, that page
// renders and acts on nothing, and only its button reaches here. A scanner that
// fetched this endpoint gets the router's 405.
//
// IT TAKES NO CREDENTIAL, like the unsubscribe route beside it and for a reason
// that is, if anything, stronger: a guest checkout creates a Customer nobody has
// ever signed in as, so the person confirming their own opt-in may have no way
// to sign in at all. The signed token is the whole authority and everything it
// can reach is already pending, visible and reversible in the Customer Area.

// consentConfirmationBody is the signed token the Sale Confirmation's link
// carried, handed on by the Storefront page the reader confirmed on.
//
// The token travels in the BODY rather than in the query string, so it does not
// end up in access logs or in a Referer header on the way to anywhere else.
type consentConfirmationBody struct {
	Token string `json:"token"`
}

// ConfirmConsent resolves the Pending Confirmations a signed link names.
//
// @Summary      Confirm a pending consent from a Sale Confirmation link
// @Description  Confirms the optional consents (Marketing, Networking) that a guest checkout left in Pending Confirmation, for the Customer named by a signed, non-expiring confirmation token carried in their Sale Confirmation email. Requires no sign-in and accepts no credential: pressing a link sent to that address is itself the proof of ownership the guest's tick lacked (ADR 0035), and a guest buyer may have no account to sign in to. It flips only what the token was minted for AND is still pending, so a press cannot resurrect an answer the owner has since given themselves — a later proven answer outranks an older pending. Idempotent and never an error when there is nothing left to confirm: a second press, or a press against state the owner has already resolved, answers 200 with already_resolved. Granting Marketing Consent turns the weekly Follow Digest on in lockstep (ADR 0034), and every press that changes something writes an immutable Consent Record on the email_confirmation channel. Deliberately a POST with no GET counterpart, so that a mail security scanner prefetching the link cannot grant consent nobody gave; a GET is answered 405. A malformed, forged, or wrong-purpose token is CONSENT_CONFIRMATION_LINK_INVALID.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Param        body  body      consentConfirmationBody  true  "Signed consent confirmation token"
// @Success      200   {object}  openapi.EnvelopeCustomerConsentConfirmation
// @Failure      400   {object}  platform.Envelope
// @Router       /api/v1/customer/consent/confirm [post]
func (h *Handler) ConfirmConsent(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body consentConfirmationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if strings.TrimSpace(body.Token) == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "token", Code: platform.CodeRequired, Message: "is required"}})
		return
	}

	// The same request-derived evidence every capture surface in this package
	// records: the platform's own reading of the client address, the browser's
	// user agent, and the page the press happened on. Never the body — a body a
	// client composes could say anything.
	view, err := h.svc.ConfirmConsent(r.Context(), body.Token, requestEvidence(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}
