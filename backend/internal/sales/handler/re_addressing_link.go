package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	customersservice "github.com/peter/ticket_pos/backend/internal/customers/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/service"
)

// The public routes a Re-addressing Link opens (#421, ADR 0058): the view
// before the button, and the click.
//
// UNDER /public, LIKE THE ASSIGNMENT LINK'S ROUTES, AND UNLIKE THEM THIS ONE
// SIGNS THE READER IN. The click is Proof of Email Ownership and the Sale is
// now theirs, so the accept returns the same sign-in a passcode does — a
// Customer Session, or the consent step when the Policy is outstanding — and
// the Storefront lands them on their tickets (#422). It is still not under
// /customer, because nobody is signed in when they arrive.

// ReAddressingSignIn is what this handler needs from customers in order to
// hand the buyer the session their click earned: the passcode's own sign-in,
// on a proven address. Declared here and satisfied by the customers service;
// unwired, the accept route refuses before it writes anything.
type ReAddressingSignIn interface {
	SignInAcceptedReAddressing(ctx context.Context, correctedEmail string) (*customersservice.SignInOutcome, error)
}

// WithReAddressingSignIn wires the sign-in an accepted Re-addressing Link
// returns. A WithX, because the customers service is built after this
// handler.
func (h *Handler) WithReAddressingSignIn(signIn ReAddressingSignIn) *Handler {
	h.reAddressingSignIn = signIn
	return h
}

// reAddressingLinkBody carries the token on the accept: in the BODY, as the
// Assignment Link's accept takes it, so the click reaches no access log and
// no Referer header.
type reAddressingLinkBody struct {
	Token string `json:"token"`
}

// ViewReAddressingLink opens a Re-addressing Link without accepting it.
//
// @Summary      View a sale re-addressing link
// @Description  Opens the Re-addressing Link a Platform Operator's Sale Re-addressing mailed to the address the buyer meant (ADR 0058), without accepting it: the Event name, the Sale Confirmation reference and the corrected address — the three facts the mail already carried — and `accepted_at` once it has been accepted. Nothing else about the purchase is disclosed before the click. The token travels in the query, as it does in the mailed link. Refused with 401 RE_ADDRESSING_LINK_INVALID when the token was tampered with, truncated, signed for another purpose or by another deployment, or names no current recording; those causes are deliberately indistinguishable. Refused with 401 RE_ADDRESSING_LINK_NO_LONGER_VALID, carrying `details.reason` of `withdrawn`, `sale_reversed` or `event_started`, when the link was genuine but its recording has ended unaccepted — told apart because its reader is the buyer.
// @Tags         public
// @Produce      json
// @Param        token  query     string  true  "The signed re-addressing link token"
// @Success      200  {object}  openapi.EnvelopeReAddressingLink
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/public/re-addressing-link [get]
func (h *Handler) ViewReAddressingLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	token, ok := reAddressingLinkToken(w, reqID, r.URL.Query().Get("token"))
	if !ok {
		return
	}
	view, err := h.svc.ViewReAddressingLink(r.Context(), token)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}

// AcceptReAddressingLink accepts the Sale Re-addressing the token names and
// signs the buyer in.
//
// @Summary      Accept a sale re-addressing link
// @Description  Accepts the Sale Re-addressing the signed Re-addressing Link names (ADR 0058). The click is Proof of Email Ownership: a Customer is created or matched on the normalised corrected address and marked Verified, and in ONE transaction the Sale's Customer and snapshot email move to them, the Sale's Self-held Ticket follows if its Holder is still the wrong address, and the record is stamped accepted; a fresh Sale Confirmation is then sent to the corrected address in the Sale's own locale. First/last name, Tax ID and phone carry from the previous Customer only into a Customer nobody has named — an existing Customer's own facts win. Everything transacted stays: the Sale's reference, snapshot name, timestamps and money figures, the Payment and its snapshot, the Platform Fee, the Reversal Window (not restarted), every other Ticket's Holder and Answers, and the consent records of either Customer. **Accepting grants no consent of any kind.** **Accepting twice is idempotent**: the second click rewrites nothing, sends no second Confirmation and returns the same Sale. The response carries the Sale the buyer now owns (`ticket_sale_id`, `confirmation_ref`, `event_name`) and the sign-in outcome ON THE SAME TERMS AS A PASSCODE SIGN-IN: `session` and `session_id` when a Customer Session was minted, or `consent_required` with a pending consent token when the current Privacy Policy is outstanding for this Customer — finish it at POST /customer/auth/consent exactly as after a passcode. No cookie is set. Refused with 401 RE_ADDRESSING_LINK_INVALID for a tampered, truncated, cross-purpose or unknown token, and with 401 RE_ADDRESSING_LINK_NO_LONGER_VALID (`details.reason` of `withdrawn`, `sale_reversed` or `event_started`) when the recording has ended unaccepted; in both cases nothing is written and nobody is mailed. The Payment Provider is never called.
// @Tags         public
// @Accept       json
// @Produce      json
// @Param        body  body      reAddressingLinkBody  true  "The signed re-addressing link token"
// @Success      200  {object}  openapi.EnvelopeReAddressingAccepted
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/public/re-addressing-link [post]
func (h *Handler) AcceptReAddressingLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body reAddressingLinkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	token, ok := reAddressingLinkToken(w, reqID, body.Token)
	if !ok {
		return
	}
	if h.reAddressingSignIn == nil {
		// Checked BEFORE the write: a Sale moved to a person who is then
		// refused a session is a worse outcome than a 500 that moved nothing.
		_ = platform.WriteDomainError(w, reqID, sales.ErrReAddressingLinkUnavailable())
		return
	}

	accepted, err := h.svc.AcceptReAddressingLink(r.Context(), token)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	outcome, err := h.reAddressingSignIn.SignInAcceptedReAddressing(r.Context(), accepted.CorrectedEmail)
	if err != nil {
		// The Sale has moved; the session did not come. A second click is
		// idempotent and mints it, which is what the reader will do.
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, reAddressingAcceptedResponse(accepted, outcome))
}

func reAddressingAcceptedResponse(accepted *service.ReAddressingAccepted, outcome *customersservice.SignInOutcome) ReAddressingAcceptedResponse {
	return ReAddressingAcceptedResponse{
		TicketSaleID:    accepted.TicketSaleID,
		EventName:       accepted.EventName,
		ConfirmationRef: accepted.ConfirmationRef,
		CorrectedEmail:  accepted.CorrectedEmail,
		AcceptedAt:      accepted.AcceptedAt,
		Session:         outcome.Session,
		SessionID:       outcome.SessionID,
		ConsentRequired: outcome.ConsentRequired,
	}
}

// reAddressingLinkToken applies the one rule the token has at the handler: a
// blank one is a MALFORMED REQUEST and not an invalid link. The Storefront
// only ever sends a token it found in the address, so an empty one means the
// relay is broken rather than that somebody's link is. Everything else about
// the token is the signer's to judge.
func reAddressingLinkToken(w http.ResponseWriter, reqID, raw string) (string, bool) {
	token := strings.TrimSpace(raw)
	if token == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "token", Code: platform.CodeRequired, Message: "is required"},
		})
		return "", false
	}
	return token, true
}
