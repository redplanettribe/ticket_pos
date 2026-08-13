package handler

import (
	"encoding/json"
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/customers/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Customer's Privacy page (#268, parent #265): one read and one write per
// control.
//
// THE READ IS A GET AND CHANGES NOTHING, which is the acceptance criterion
// rather than a convention obeyed. Only moving a control writes a Consent
// Record, and the two acts are two routes precisely so that "the page was
// rendered" and "the person answered" can never be the same request.

// optionalConsentBody is one control's new position: the state the Customer is
// asking for, not a flip.
//
// A STATE RATHER THAN A TOGGLE, for the reason the Digest switch takes one:
// "withdraw this" arriving twice means what it meant the first time, where a
// flip arriving twice would grant back what somebody had just taken away — and
// a double-tapped control or a client retry must not be able to do that.
type optionalConsentBody struct {
	Granted *bool `json:"granted"`
}

// Privacy reports what the signed-in Customer has authorized.
//
// @Summary      Read the Customer's privacy settings
// @Description  Reports what the signed-in Customer has authorized: the Policy Version they accepted and when, read-only, and the current state of each optional consent. THIS READ WRITES NOTHING — rendering a settings page must leave the platform exactly as it was, because a page that recorded a refusal when somebody merely looked at it would turn "never asked" into "denied" for everyone who opened it and did nothing. Each optional consent is one of four values, and they mean four different things: `granted`, `denied`, `pending_confirmation` (somebody who had not proven this address ticked the box, so it stands unresolved and is not the owner's answer) and `unanswered` (never asked, which is not a refusal). `unanswered` exists on the wire only; the platform stores NULL, because the absence of an answer is not a fourth kind of answer. The Policy Version named is the edition THIS CUSTOMER ACCEPTED and not necessarily the one in effect, so a Customer who accepted a superseded edition is told what they actually agreed to; both policy fields are null where no acceptance was ever recorded. Policy Acceptance carries no control anywhere: it is not withdrawable, being absent from counsel's withdrawal form and resting on a basis other than consent. Requires a full Customer Session; a Confirmation Link session is refused with CUSTOMER_SESSION_SCOPE_INSUFFICIENT, because a forwarded receipt is not authority to read somebody's standing privacy settings.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeCustomerPrivacy
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/customer/privacy [get]
func (h *Handler) Privacy(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	view, err := h.svc.Privacy(r.Context(), customerSessionToken(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}

// SetOptionalConsent moves one optional consent, in either direction.
//
// @Summary      Grant or withdraw one optional consent
// @Description  Moves ONE optional consent — `marketing` or `networking`, named in the path — to the state the body asks for, and answers with both as they now stand. Moving it off is a CONSENT WITHDRAWAL and moving it on is an affirmative grant behind a session established by Proof of Email Ownership; both directions are offered deliberately, because every surface that can grant an optional consent shows the box only while the state is unanswered, so a one-way page would leave a Customer who withdrew by mistake with no route back at all. Exactly one Consent Record is written per request, under the `account_settings` channel, carrying the state each optional consent was in immediately before and the technical proof of the act. Only the named consent is answered; the other and the Policy Acceptance box are recorded as not shown, so nothing standing is churned. Withdrawing Marketing Consent switches the weekly Follow Digest off with it, in the same transaction and the same statement — they are one switch and cannot drift. A withdrawal that actually took something away, out of `granted` or `pending_confirmation`, is confirmed to the Customer by email in their Mail Locale; a grant sends nothing, and neither does answering No to a consent that was already denied, because nobody is told about a change that did not happen. A Customer whose consent sits in `pending_confirmation` settles it either way here by answering for themselves. Takes a state rather than a flip, so a retried or double-tapped request means the same thing once. Requires a full Customer Session; a Confirmation Link session is refused with CUSTOMER_SESSION_SCOPE_INSUFFICIENT. A purpose this API does not recognise is VALIDATION_FAILED — Policy Acceptance is deliberately not one of them, because it is not withdrawable.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        purpose  path      string               true  "Which optional consent to move"  Enums(marketing, networking)
// @Param        body     body      optionalConsentBody  true  "The state the Customer is asking for"
// @Success      200      {object}  openapi.EnvelopeCustomerOptionalConsents
// @Failure      400      {object}  platform.Envelope
// @Failure      401      {object}  platform.Envelope
// @Failure      403      {object}  platform.Envelope
// @Router       /api/v1/customer/privacy/consents/{purpose} [put]
func (h *Handler) SetOptionalConsent(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	// Validated here rather than left to the service, so that a purpose nobody
	// recognises is answered as the bad request it is instead of as a failure of
	// this Customer's settings. The vocabulary is the service's closed type; this
	// only refuses what is outside it. INVALID_ENUM rather than a code of its
	// own, because the accepted set is the whole of what a caller needs and it is
	// in the message where that code's contract says it belongs.
	purpose := service.ConsentPurpose(r.PathValue("purpose"))
	if !purpose.Valid() {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{
			Field:   "purpose",
			Code:    platform.CodeInvalidEnum,
			Message: "must be one of: marketing, networking",
		}})
		return
	}

	var body optionalConsentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	// A pointer, and an absent field is refused rather than read as false. A
	// bool's zero value is the destructive half of this control, and a client
	// that omitted the field by mistake must not withdraw somebody's consent.
	if body.Granted == nil {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{
			Field:   "granted",
			Code:    platform.CodeRequired,
			Message: "is required",
		}})
		return
	}

	view, err := h.svc.SetOptionalConsent(r.Context(), customerSessionToken(r), purpose, *body.Granted, consent.EvidenceFromRequest(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}

// WithdrawAll takes back every optional consent in one act.
//
// IT READS NO BODY AND THERE IS NOTHING TO READ. The act names nothing and
// chooses nothing, so the request carries no field in which a caller could ask
// for a grant, name a purpose or select one consent — which is what makes "this
// route can only withdraw everything" a property of the contract rather than a
// rule enforced somewhere below.
//
// @Summary      Withdraw every optional consent
// @Description  Takes back EVERY optional consent in one act, writing exactly ONE Consent Record with both Marketing Consent and Networking Consent answered No and carrying the state each was in immediately before. It is a route of its own rather than two calls to the per-purpose control precisely so that the evidence says "this person asked for everything" rather than "this person happened to move two controls" — a distinction the log cannot recover afterwards from two rows and a shared timestamp. The request takes NO BODY: this endpoint can only ever withdraw, and nothing in it can express a grant. Policy Acceptance is untouched, because it is not withdrawable — it rests on a basis other than consent, and clearing it would re-gate the Customer rather than free them. Withdrawing Marketing Consent switches the weekly Follow Digest off in the same transaction and the same statement. A Pending Confirmation left standing by somebody else's tick is settled as No by the same act. THIS IS NOT DELETION AND PROCESSING DOES NOT STOP: the account keeps working, Tickets are unaffected, tickets can still be bought, Sale Confirmations, passcodes and reversal notices still arrive because they rest on contract rather than consent, and data is retained for the Tickets held and for legal and security obligations. Anything withdrawn can be granted again from the same page. The response reports both consents as they now stand AND what this act actually took away, which are different facts: `denied` reads the same whether a consent was just given up or was already refused. A confirmation email is sent, in the Customer's Mail Locale, naming only what actually moved — and nothing at all is sent when both consents were already denied, because nobody is told about a change that did not happen. Requires a full Customer Session; a Confirmation Link session is refused with CUSTOMER_SESSION_SCOPE_INSUFFICIENT.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeCustomerWithdrawAll
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/customer/privacy/withdraw-all [post]
func (h *Handler) WithdrawAll(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	view, err := h.svc.WithdrawAll(r.Context(), customerSessionToken(r), consent.EvidenceFromRequest(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}
