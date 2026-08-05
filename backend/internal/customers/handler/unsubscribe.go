package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Unsubscribe surface (#224, parent #215, ADR 0030): two routes for one
// switch, reached by two people in two different situations.
//
// THE ONLY UNAUTHENTICATED WRITE IN THE CUSTOMER AREA is the first of them, and
// that is a decision rather than an oversight. A Follow Digest is read in a mail
// client months after anybody last signed in, and an opt-out gated behind a
// passcode is not an opt-out. The token in the link is the whole authority, it
// names one Customer, and the only thing it can do is set one reversible flag to
// false — nothing here can reach a Follow, so the worst a stolen or prefetched
// token achieves is a weekly email its owner can switch back on.
//
// IT IS A POST AND THERE IS NO GET, which is the acceptance criterion ADR 0030
// was written around. Mail security scanners open every link in every message
// before a human sees it; the link in the Digest points at a STOREFRONT page,
// and the page confirms with the POST below. A scanner that fetched the page has
// changed nothing, and a scanner that fetched this endpoint gets the router's 405.

// unsubscribeBody is the signed token the Digest's link carried, handed on by
// the Storefront page the reader confirmed on.
type unsubscribeBody struct {
	Token string `json:"token"`
}

// digestSubscriptionBody is the Customer Area toggle's request: the state the
// Customer is asking for, not a flip.
//
// A STATE RATHER THAN A TOGGLE, deliberately. "Turn it off" arriving twice means
// what it meant the first time, where a flip arriving twice turns the Digest
// back on — and a double-tapped switch or a client retry would silently
// resubscribe somebody who asked for quiet.
type digestSubscriptionBody struct {
	Enabled *bool `json:"enabled"`
}

// Unsubscribe turns the Follow Digest off for the Customer a signed link names.
//
// @Summary      Unsubscribe from the Follow Digest
// @Description  Turns the Follow Digest off for the Customer named by a signed unsubscribe token, which is carried in the footer of every Digest. Requires no sign-in and accepts no credential: a Digest is read months after anybody last signed in, and an opt-out gated behind a passcode would not be an opt-out. Unsubscribing is a switch and not a purge — every Follow stands, stays visible in the Customer Area, and the Customer can turn the Digest back on from there. This is deliberately a POST with no GET counterpart, so that a mail security scanner prefetching the link in a message cannot unsubscribe anybody; a GET is answered 405. Idempotent: the same link appears in every Digest a person ever received, and pressing it twice means the same thing once. A malformed, forged or spent token is UNSUBSCRIBE_LINK_INVALID. Touches no transactional mail: One-time Passcodes and Sale Confirmations arrive either way.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Param        body  body      unsubscribeBody  true  "Signed unsubscribe token"
// @Success      200   {object}  openapi.EnvelopeCustomerDigestSubscription
// @Failure      400   {object}  platform.Envelope
// @Router       /api/v1/customer/unsubscribe [post]
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body unsubscribeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if strings.TrimSpace(body.Token) == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "token", Code: platform.CodeRequired, Message: "is required"}})
		return
	}

	view, err := h.svc.Unsubscribe(r.Context(), body.Token)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}

// SetDigestEnabled is the Customer Area's toggle, and the only way to turn the
// Digest back on.
//
// @Summary      Turn the Follow Digest on or off
// @Description  Sets whether the signed-in Customer receives the weekly Follow Digest, and returns the switch as it now stands. This is the Customer Area's toggle beside the Following list, and it is the only way to turn the Digest back ON — the unsubscribe link is unauthenticated because somebody who wants quiet must be able to have it without signing in, and none of that argument applies to switching somebody's mail back on. Changes no Follow either way: the list is exactly as it was, and a Customer who turns the Digest off keeps everything they Followed. Requires a full Customer Session; a Confirmation Link session is refused with CUSTOMER_SESSION_SCOPE_INSUFFICIENT, because a forwarded receipt is not authority to subscribe that inbox to weekly mail. Takes the state asked for rather than flipping, so a retried or double-tapped request means the same thing once.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      digestSubscriptionBody  true  "Whether the Digest is on"
// @Success      200   {object}  openapi.EnvelopeCustomerDigestSubscription
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Router       /api/v1/customer/digest [put]
func (h *Handler) SetDigestEnabled(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body digestSubscriptionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	// A pointer, and an absent field is refused rather than read as false. The
	// zero value of a bool is exactly the destructive half of this switch, and a
	// client that omitted the field by mistake must not silence somebody.
	if body.Enabled == nil {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "enabled", Code: platform.CodeRequired, Message: "is required"}})
		return
	}

	view, err := h.svc.SetDigestEnabled(r.Context(), customerSessionToken(r), *body.Enabled)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}
