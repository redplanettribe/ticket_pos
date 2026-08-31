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
	//
	// IT IS A POINTER SO THAT MENTIONING IT AT ALL MEANS SOMETHING (#270). A
	// submission carrying this field is a sign-in submission whatever it says —
	// `false` is a refusal of the policy, not a withdrawal of a consent — and it
	// follows the rules it always did. Only a submission that does not mention
	// Policy Acceptance at all can be the other act. The two readings agree
	// everywhere they overlap: absent is false on the sign-in path exactly as it
	// has always been.
	PolicyAcceptance *bool `json:"policy_acceptance"`
	// TermsAcceptance is the Terms box (#536, ADR 0066), required exactly where
	// it is OWED — the service judges it against the same recomputed Outstanding
	// the optional answers are read against, so a Customer re-gated by a Policy
	// bump alone is not refused over a box they were never shown. Like the
	// acceptance above it carries no edition: which Terms Version is being
	// accepted is resolved server-side at the moment of capture.
	//
	// A POINTER FOR THE SAME REASON POLICY ACCEPTANCE IS ONE: a submission
	// mentioning it at all is a sign-in submission, whatever it says — the Terms
	// have no withdrawal path at all (ADR 0066), so no reading of this field may
	// ever steer a submission onto the withdrawal branch.
	TermsAcceptance *bool `json:"terms_acceptance"`
	// MarketingConsent and NetworkingConsent are the optional boxes. On a sign-in
	// submission ABSENT IS FALSE AND FALSE IS AN EXPLICIT NO — an unticked box
	// that was shown is a refusal, recorded as `denied`, and for Marketing that
	// turns the Follow Digest off (ADR 0034). That is what the surface means: the
	// boxes are rendered unticked and a person who submits without touching them
	// has answered.
	//
	// An answer for a box this Customer was not shown is ignored by the service,
	// which recomputes what they were owed rather than trusting this body.
	//
	// THEY ARE POINTERS SO THAT A WITHDRAWAL CAN BE TOLD FROM A SIGN-IN (#270,
	// ADR 0039), and nothing about the sign-in reading above changes: an absent
	// field still submits false there, because withdrawalOnly below sends a
	// submission carrying a Policy Acceptance down the same path it always took.
	// What the pointer buys is the OTHER submission — one with no acceptance in
	// it, naming the consents it takes away and nothing else — where absent has
	// to mean "not on this submission at all" rather than "answered No", or a
	// bare token would be a withdrawal of two consents nobody mentioned.
	MarketingConsent  *bool `json:"marketing_consent"`
	NetworkingConsent *bool `json:"networking_consent"`
	// Follow is the Follow intent, relayed here for the same reason it is
	// relayed on the verify: this is now the request that produces a session, and
	// a Follow is written against the session a sign-in produced and against
	// nothing a body could name (#219). A visitor who pressed Follow and was
	// then stopped for consent must not silently lose it.
	Follow string `json:"follow"`
}

// withdrawalOnly reports whether this submission is a Consent Withdrawal rather
// than a sign-in being finished — the one predicate the conditional Policy
// Acceptance rule turns on (ADR 0039).
//
// THE RULE IS: POLICY ACCEPTANCE IS REQUIRED UNLESS EVERY ANSWER PRESENT IS A
// DENIAL. Every clause below is one half of it, and each is load-bearing:
//
//   - A submission MENTIONING POLICY ACCEPTANCE is not a withdrawal, whatever it
//     says about it. `true` is a sign-in being finished; `false` is a person
//     refusing the policy, which is a refusal of the required box and not the
//     withdrawal of an optional consent. Both follow the rules they always did.
//   - A submission WITH NO ANSWER AT ALL is not a withdrawal either. A bare
//     token names nothing to take away, and reading "no answers" as "all answers
//     are denials" would make the empty body — the exact submission the gate has
//     refused since #251 — start succeeding.
//   - A submission CONTAINING ANY GRANT is not a withdrawal, even beside a
//     denial. Withdrawing Networking while granting Marketing is a submission
//     that authorizes something, and authorizing something is what acceptance
//     evidences having been informed about.
//
// Everything that is not a withdrawal goes down the sign-in path, where the
// unconditional gate is exactly where it has always been and refuses exactly
// what it always did. That is the shape to preserve: this predicate decides
// WHICH ACT a submission is, and never whether an act may skip a check.
//
// It reads the body and only the body. Which act this is cannot depend on who
// the Customer is, on what they have already answered, or on which door minted
// the token — a rule that varied with any of those would be one nobody could
// state in an API description, and this one is stated in it verbatim.
func (b consentSubmitBody) withdrawalOnly() bool {
	if b.PolicyAcceptance != nil {
		return false
	}
	// A submission mentioning the Terms is a sign-in submission for the reason a
	// policy mention is: acceptance of either document is a required box being
	// answered, not a consent being taken away — and the Terms cannot be taken
	// away on any channel at all (#536, ADR 0066).
	if b.TermsAcceptance != nil {
		return false
	}
	if b.MarketingConsent == nil && b.NetworkingConsent == nil {
		return false
	}
	if b.MarketingConsent != nil && *b.MarketingConsent {
		return false
	}
	if b.NetworkingConsent != nil && *b.NetworkingConsent {
		return false
	}
	return true
}

// consentSubmitResponse is what the consent submission endpoint answers with,
// and it has THREE shapes now: the session a finished sign-in mints, the
// consent-required outcome it never actually returns, and — for a denials-only
// submission — the withdrawal that minted nothing (#270).
//
// One type with nils in it rather than three return paths, which is the same
// choice service.SignInOutcome makes and for the same reason: every client has
// to handle each shape, and a shape that can be forgotten is a client reporting
// a successful withdrawal as a broken sign-in. `withdrawal` is null on every
// sign-in submission, so a client that reads `session_id` sees exactly the
// field it always did.
type consentSubmitResponse struct {
	Session         *service.CustomerSessionView `json:"session"`
	SessionID       string                       `json:"session_id"`
	Follow          *service.FollowView          `json:"follow"`
	ConsentRequired *service.ConsentRequiredView `json:"consent_required"`
	// Withdrawal is the Consent Withdrawal a denials-only submission performed:
	// the states as they stand afterwards and what the act took away. NO SESSION
	// IS BESIDE IT AND NONE EXISTS — the person is as signed out as they were
	// before the request.
	Withdrawal *service.ConsentWithdrawalView `json:"withdrawal"`
}

// SubmitConsent exchanges a pending-consent token and the answers for the
// Customer Session that proof of email ownership did not mint (#251) — or, when
// every answer it carries is a denial, performs a Consent Withdrawal and mints
// nothing at all (#270, ADR 0039).
//
// Unauthenticated, because the token IS the credential — the same reason
// passcode verification is. It reveals nothing to anybody who does not already
// hold one: an unknown, spent or expired token gets one indistinguishable
// refusal, so this endpoint cannot be asked whether an address exists, whether
// it has consent outstanding, or whether somebody else's sign-in is in flight.
//
// WHICH ACT A SUBMISSION IS, IS DECIDED HERE AND FROM THE BODY ALONE, by
// withdrawalOnly above, before either service call. The two acts are then each
// enforced whole: the sign-in path still refuses a submission without Policy
// Acceptance, in the same line it always did, and the withdrawal path is
// incapable of granting anything. Neither is a relaxed version of the other, and
// there is no argument by which a submission containing a grant can reach the
// second — which is the property to keep when editing this file.
//
// @Summary      Submit sign-in consent, or withdraw consent
// @Description  Spends the short-lived, single-use `pending_consent_token` from a verify response, or from the Consent Withdrawal passcode door, on ONE of two acts — decided by the API from the submission's contents and never by the form that sent it. POLICY ACCEPTANCE IS REQUIRED UNLESS EVERY ANSWER PRESENT IN THE SUBMISSION IS A DENIAL (ADR 0039). A submission mentioning `policy_acceptance` at all — true or false — one containing any grant, and one carrying no answer at all are all sign-in submissions, and a sign-in submission without acceptance is refused with POLICY_ACCEPTANCE_REQUIRED exactly as before; a submission whose only answers are `false` is a Consent Withdrawal and needs no acceptance, because an act that grants nothing, opens nothing and authorizes nothing has no processing for an acceptance to have informed anybody about. FINISHING A SIGN-IN writes the immutable Consent Record first — answers, channel, Policy Version, and the technical proof (IP, user agent, session, origin URL) — and mints the session on the far side of it, so nobody is ever signed in without evidence of what they authorized; `marketing_consent` and `networking_consent` default to false there, and false is an explicit No that records `denied` and, for marketing, switches the weekly Follow Digest off (ADR 0034); answers for boxes the Customer was not shown are ignored, so standing optional answers are never churned; an optional `follow` carries a Follow intent, honoured against the session this call mints exactly as on the verify routes; the response carries `session` and `session_id`. WITHDRAWING carries no `policy_acceptance` and names only the consents to take away, each as `false` — an omitted consent is not on the submission and is left exactly as it stands, so a bare token withdraws nothing and is refused. It MINTS NO CUSTOMER SESSION: the response carries `withdrawal` with the state of each optional consent afterwards and what the act actually took away, and `session`, `session_id`, `consent_required` and `follow` are all null. It can only ever move a consent to `denied` — no submission on this path can grant a consent, accept a Policy Version or open a session — so an intercepted passcode buys nothing its owner cannot undo from their own account. A withdrawal that moved a consent out of `granted` or `pending_confirmation` writes its Consent Record with the prior state and is confirmed to the Customer by email in their Mail Locale; one that moved nothing records the act and sends nothing. THE TERMS BOX (#536, ADR 0066) rides the same submission: when the verify's `boxes.terms_acceptance` was true the Customer owes acceptance of the current Términos y Condiciones edition, and a sign-in submission without `terms_acceptance: true` is refused with TERMS_ACCEPTANCE_REQUIRED; where the box was not owed the field is ignored. Each required box is judged only where owed, so a Customer re-gated by one document alone is never refused over the other's box. A recorded Terms acceptance stamps the Customer with the edition and is carried on the same Consent Record; it is contractual, has no withdrawal path, and no submission on the withdrawal branch can name it. The Policy Version and the Terms Version are resolved server-side and are never accepted from the request. The token is spent whatever the outcome and whichever act was attempted, so a refused submission is restarted by proving the address again; abandoning the step records nothing and leaves no session at all.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Param        body  body      consentSubmitBody  true  "Pending consent token and answers"
// @Success      200   {object}  openapi.EnvelopeCustomerConsentSubmission
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

	// The technical proof is derived from the REQUEST and never from the body —
	// see consent.EvidenceFromRequest, which every capture surface shares.
	evidence := consent.EvidenceFromRequest(r)

	if body.withdrawalOnly() {
		// A Follow intent is not read on this branch and cannot be: it is written
		// against the session a sign-in mints, and this act mints none. A
		// withdrawal carrying one silently drops it rather than being refused —
		// what the person asked for is the withdrawal, and it must not fail over a
		// field that has nothing to attach to.
		view, err := h.svc.WithdrawConsent(r.Context(), service.ConsentWithdrawal{
			Token: body.PendingConsentToken,
			// Present on the submission means "take this one away". The values
			// themselves are known to be false — withdrawalOnly returned true — so
			// nothing but a selection crosses this boundary.
			MarketingConsent:  body.MarketingConsent != nil,
			NetworkingConsent: body.NetworkingConsent != nil,
			Evidence:          evidence,
		})
		if err != nil {
			_ = platform.WriteDomainError(w, reqID, err)
			return
		}
		_ = platform.WriteSuccess(w, reqID, http.StatusOK, consentSubmitResponse{Withdrawal: view})
		return
	}

	outcome, err := h.svc.SubmitConsent(r.Context(), service.ConsentSubmission{
		Token: body.PendingConsentToken,
		// Absent is false and false is refused where the box is owed, exactly as
		// before — for both required boxes.
		PolicyAcceptance: body.PolicyAcceptance != nil && *body.PolicyAcceptance,
		TermsAcceptance:  body.TermsAcceptance != nil && *body.TermsAcceptance,
		// Absent is false on this path, which is what it has always meant here: a
		// box that was shown and left unticked is an explicit No.
		MarketingConsent:  body.MarketingConsent != nil && *body.MarketingConsent,
		NetworkingConsent: body.NetworkingConsent != nil && *body.NetworkingConsent,
		Evidence:          evidence,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	signedIn := h.signInResponse(r.Context(), outcome, intent)
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, consentSubmitResponse{
		Session:         signedIn.Session,
		SessionID:       signedIn.SessionID,
		Follow:          signedIn.Follow,
		ConsentRequired: signedIn.ConsentRequired,
	})
}
