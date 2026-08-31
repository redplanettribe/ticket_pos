package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// staffTermsGateBody is the interstitial being answered: the token that pinned
// what was shown, and the one box.
//
// There is no email on it and no edition on it, and that is the guarantee: the
// person is the session's, and the edition is the token's. A client can assert
// neither.
type staffTermsGateBody struct {
	// GateToken is the single-use token the interstitial's read handed out.
	GateToken string `json:"gate_token"`
	// TermsAcceptance is the one required box. Absent is false and false is
	// refused by the API, not merely by a disabled button.
	TermsAcceptance bool `json:"terms_acceptance"`
}

// staffTermsGateView is the interstitial's read: what this signed-in person
// owes, or that they owe nothing.
type staffTermsGateView struct {
	// Outstanding is false when this session may carry on. TermsRequired is then
	// null, and the staff app sends the person where they were going.
	Outstanding bool `json:"outstanding"`
	// TermsRequired is the box to show, worded in the language actually served —
	// identical in shape to the sign-in door's terms step, because it is the
	// same step asked of somebody who is already inside.
	TermsRequired *service.TermsRequiredView `json:"terms_required"`
}

// staffTermsAcceptedView is what recording an acceptance on a live session
// reports back.
//
// Deliberately not a session: nothing was minted, nothing was re-minted and
// nothing was revoked, so there is no new credential to hand over and the app
// simply resumes the navigation it was diverted from (#570).
type staffTermsAcceptedView struct {
	Accepted bool `json:"accepted"`
}

// GetStaffTermsGate reports whether this live Staff Session owes a Terms
// Acceptance, and issues the box when it does (#570, ADR 0067).
//
// The read has a side effect, and it is the same one the sign-in door's gate
// has: issuing the box PINS, server-side and single-use, the edition shown and
// the language it was shown in, so that the later request which ticks the box
// records evidence of the text that was actually on screen (#537, #567). That
// is why this is a POST-shaped fact behind a GET — the alternative is letting
// the client tell the platform what it displayed.
//
// It costs the session nothing: no passcode, no re-mint, no revocation.
//
// @Summary      What a signed-in staff member owes the Terms gate
// @Description  Reports whether the authenticated Staff Session's holder owes a Staff Terms Acceptance of an edition that still satisfies the gate, and when they do, returns the box to show: the edition label, the acceptance label as the published artifact words it, the language that label was actually served in, and a short-lived single-use `pending_terms_token` that pins both server-side. `locale` is the language the staff app is rendered in for this reader; a language the current edition does not publish is floored at the prevailing one rather than served blank. Reading this revokes nothing, mints nothing and spends no passcode — the session is untouched (#570, ADR 0067).
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        locale  query     string  false  "Language the staff app is rendered in (en or es)"
// @Success      200     {object}  openapi.EnvelopeStaffTermsGate
// @Failure      401     {object}  platform.Envelope
// @Router       /api/v1/staff/terms/gate [get]
func (h *Handler) GetStaffTermsGate(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	// Permissive, exactly as the sign-in doors are with their detected language:
	// this is a browser's guess about a page, not a person's stated choice, and
	// a malformed one must never be the reason somebody cannot be shown the
	// contract. Anything unrecognised falls to the platform default, which
	// acceptanceLabel then floors at the prevailing text if the edition does not
	// publish it.
	pageLocale := platform.DefaultLocale
	if parsed, ok := platform.ParseLocale(r.URL.Query().Get("locale")); ok {
		pageLocale = parsed
	}

	required, err := h.svc.StaffTermsGate(r.Context(), session.Email, pageLocale)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, staffTermsGateView{
		Outstanding:   required != nil,
		TermsRequired: required,
	})
}

// AcceptStaffTermsOnSession records the acceptance the navigation gate asked
// for, without touching the session (#570, ADR 0067).
//
// @Summary      Accept the Terms from a signed-in session
// @Description  Records one append-only Staff Terms Acceptance for the authenticated Staff Session's holder — the edition and language pinned on the `gate_token` when the interstitial rendered the box, the capacity "organizer", the timestamp, and the technical proof (IP, user agent, session, origin URL). `terms_acceptance` must be true; an unticked box is refused with TERMS_ACCEPTANCE_REQUIRED and does NOT spend the token, because the token proves nothing here — the session is the credential. A token issued to another address is refused indistinguishably from an unknown or expired one. Nothing is minted, re-minted or revoked and no passcode is spent: the caller resumes the navigation the gate diverted (#570, ADR 0067).
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      staffTermsGateBody  true  "Gate token and the ticked box"
// @Success      200   {object}  openapi.EnvelopeStaffTermsAccepted
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Router       /api/v1/staff/terms/accept [post]
func (h *Handler) AcceptStaffTermsOnSession(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	var body staffTermsGateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if strings.TrimSpace(body.GateToken) == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "gate_token", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}

	if err := h.svc.AcceptTermsOnSession(r.Context(), service.SessionTermsAcceptance{
		SessionID: session.SessionID,
		// The person is the SESSION'S, never the body's.
		Email:           session.Email,
		Token:           body.GateToken,
		TermsAcceptance: body.TermsAcceptance,
		// The technical proof is derived from the REQUEST — the one spelling
		// every capture surface in every module shares.
		Evidence: consent.EvidenceFromRequest(r),
	}); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, staffTermsAcceptedView{Accepted: true})
}
