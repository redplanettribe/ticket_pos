package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The two public routes behind an Answer Link (#312, ADR 0044).
//
// PUBLIC AND UNAUTHENTICATED, and the whole authority is the signed token in the
// body. There is no Security annotation on either of these, no session is read,
// and none is minted: holding an Answer Link makes nobody a Customer.
//
// THE TOKEN TRAVELS IN THE BODY AND NEVER IN THE PATH OR QUERY, on both verbs,
// including the one that only reads. A credential in a URL ends up in access
// logs, in a Referer header on the way to the next request, and in whatever a
// proxy keeps — and unlike the Confirmation Link's, this token has no expiry to
// limit how long a leaked copy stays useful. The Consent Confirmation Link's
// POST is the precedent; the reason applies here for a longer time.
//
// That the READ is a POST is the one thing about this shape worth defending. It
// is not REST, and it is chosen anyway: the alternative is a GET with the
// credential in the query string, which is exactly what the paragraph above
// rules out. What a GET normally buys — caching, prefetching, a bookmarkable
// address — is unwanted here to the point of being a hazard. The idempotence a
// GET would promise is kept: this handler reads and writes nothing.

// answerLinkBody carries the token an Answer Link's page was reached by.
type answerLinkBody struct {
	// Token is the signed Answer Link token, exactly as it arrived in the
	// address. The Storefront reads it out of its own URL and relays it here;
	// nothing else about the caller is asked for, or would be believed.
	Token string `json:"token"`
}

// answerLinkAnswerBody is answerLinkBody plus the Answer being given.
//
// It embeds answerBody rather than restating five fields, so the shape a holder
// posts and the shape Event Staff post cannot drift apart — they are answering
// the same Ticket Question against the same catalog.ParseAnswer.
type answerLinkAnswerBody struct {
	answerBody
	Token string `json:"token"`
}

// answerLinkToken pulls the token out of a decoded body, refusing an empty one
// before the service is troubled.
//
// A blank token is a MALFORMED REQUEST and not an invalid link: the Storefront
// only sends this handler a token it found in the address, so an empty one means
// the relay is broken rather than that somebody's link is. The page renders its
// own "this link arrived without its token" copy without ever calling here.
func answerLinkToken(w http.ResponseWriter, reqID, token string) (string, bool) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "token", Code: platform.CodeRequired, Message: "is required"},
		})
		return "", false
	}
	return trimmed, true
}

// OpenAnswerLink returns the one Ticket's questions an Answer Link opens.
//
// @Summary      Open an answer link
// @Description  Opens the Ticket Questions of the one Ticket a signed Answer Link names, for whoever holds the link. Requires no sign-in, creates no Customer and mints no session. **It discloses nothing about the purchase** — the Event name, the Ticket Type name and the questions with this Ticket's answers, and never the buyer's name or email, the price, the Tax ID, the Sale Confirmation reference, or the Sale's other Tickets — because this link is meant to be forwarded (ADR 0044). Refused with 401 ANSWER_LINK_INVALID when the token was tampered with, truncated, signed by another deployment, names a Ticket that no longer exists, or names one whose Ticket Sale has been reversed; those causes are deliberately indistinguishable, because telling them apart would disclose a fact about somebody else's purchase. Refused with 401 ANSWER_LINK_EXPIRED once the Event has started, which is told apart only because an Event's start is already published. Answers 404 while the Ticket Question feature flag is off.
// @Tags         public
// @Accept       json
// @Produce      json
// @Param        body  body      answerLinkBody  true  "The signed answer link token"
// @Success      200  {object}  openapi.EnvelopeAnswerLink
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/public/answer-link [post]
func (h *Handler) OpenAnswerLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body answerLinkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	token, ok := answerLinkToken(w, reqID, body.Token)
	if !ok {
		return
	}

	view, err := h.svc.OpenAnswerLink(r.Context(), token)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}

// AnswerByAnswerLink writes one Answer on the authority of an Answer Link.
//
// PUT for the reason its staff neighbour is: it is the whole Answer every time,
// there is exactly one per (Ticket, question), and a correction is the same
// request with a different body. An Answer given here REPLACES one the buyer
// gave at checkout, which needs no special verb because it is the same row.
//
// The question is named in the PATH while the token stays in the body. The
// question id is not a credential — it is one of the ids the page just rendered
// — and putting it in the address is what makes the two routes read as one
// resource with a sub-resource rather than as two RPCs.
//
// @Summary      Answer a ticket question through an answer link
// @Description  Writes the Answer to one Ticket Question on the Ticket a signed Answer Link names, creating it or replacing what the buyer entered at checkout. Requires no sign-in, creates no Customer and mints no session (ADR 0044). The response is the same disclosure-limited payload the open returns. Refused with 400 INVALID_ANSWER when the value does not fit the question's kind, with 401 ANSWER_LINK_INVALID for a tampered, truncated or reversed-sale link, and with 401 ANSWER_LINK_EXPIRED once the Event has started. Answers 404 while the Ticket Question feature flag is off.
// @Tags         public
// @Accept       json
// @Produce      json
// @Param        questionId  path      string                true  "Ticket question ID"
// @Param        body        body      answerLinkAnswerBody  true  "The signed token, and the answer in the shape its question's kind takes"
// @Success      200  {object}  openapi.EnvelopeAnswerLink
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/public/answer-link/questions/{questionId} [put]
func (h *Handler) AnswerByAnswerLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}

	var body answerLinkAnswerBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	token, ok := answerLinkToken(w, reqID, body.Token)
	if !ok {
		return
	}

	// Nothing about the Answer is validated here, exactly as on the staff route:
	// every rule needs the question's KIND, which this layer does not have and
	// must not guess. See AnswerTicketQuestion.
	view, err := h.svc.AnswerByAnswerLink(r.Context(), token, questionID, service.AnswerInput{
		Text:      body.Text,
		Number:    body.Number,
		Date:      body.Date,
		Checked:   body.Checked,
		OptionIDs: body.OptionIDs,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}
