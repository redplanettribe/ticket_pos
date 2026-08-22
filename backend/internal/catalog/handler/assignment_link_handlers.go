package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The three public routes behind an Assignment Link (#325, parent #322,
// ADR 0046).
//
// PUBLIC AND UNAUTHENTICATED IN THE ORDINARY SENSE, AND YET THIS IS WHERE A
// PERSON IS MINTED. There is no Security annotation on any of these and no
// session is read — and yet the first call here
// creates or matches a Verified Customer, because clicking a link that only ever
// travelled to one address is Proof of Email Ownership (ADR 0035). No session is
// MINTED either: accepting a ticket does not sign anybody in.
//
// THE TOKEN TRAVELS IN THE BODY AND NEVER IN THE PATH OR QUERY, on all three
// verbs, including the one that reads. A credential in a URL ends up in access
// logs and in a Referer header on the way to the next request — and this
// credential is the strongest one this platform hands out to a stranger, since
// what it does is assert who somebody is.
//
// NO TICKET ID APPEARS IN ANY PATH HERE, and that is a decision rather than an
// accident of style. The token names the Ticket. A path segment naming it too
// would be a second, unsigned way to say which Ticket this is, and the two could
// disagree — which is precisely the shape of a bug that lets one person's link
// act on another person's Ticket.
//
// THAT THE ACCEPT IS A POST is right rather than merely convenient: it WRITES.
// The Storefront's page deliberately acts on nothing when it is fetched, because
// mail security scanners open every link in every message before a human sees
// one, and a page that accepted on render would let a scanner mint a Customer
// nobody proved. The press is the act.

// assignmentLinkBody carries the token an Assignment Link's page was reached by.
type assignmentLinkBody struct {
	// Token is the signed Assignment Link token, exactly as it arrived in the
	// address. The Storefront reads it out of its own URL and relays it here;
	// nothing else about the caller is asked for, or would be believed.
	Token string `json:"token"`
}

// linkToken pulls the token out of a decoded body, refusing an empty one
// before the service is troubled.
//
// A blank token is a MALFORMED REQUEST and not an invalid link: the Storefront
// only sends these handlers a token it found in the address, so an empty one
// means the relay is broken rather than that somebody's link is. The page
// renders its own "this link arrived without its token" copy without ever
// calling here.
func linkToken(w http.ResponseWriter, reqID, token string) (string, bool) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "token", Code: platform.CodeRequired, Message: "is required"},
		})
		return "", false
	}
	return trimmed, true
}

// assignmentLinkNameBody is the token plus the name the Holder gave.
//
// TWO NAME FIELDS AND NO THIRD FIELD OF ANY KIND. There is no tax_id here, no
// phone, no password and no marketing checkbox — a Holder is asked for their
// name and their Ticket Questions and nothing else (ADR 0046), and a body that
// cannot carry the others is a stronger guarantee of that than a service that
// remembers to ignore them.
type assignmentLinkNameBody struct {
	assignmentLinkBody
	// FirstName and LastName are stored separately (ADR 0005) and written to the
	// Customer as their current asserted name. Required and bounded HERE, in the
	// handler, as the standard VALIDATION_FAILED envelope (#336): the token
	// names the Ticket, so unlike the buyer's Holder email write there is no id
	// for an early 400 to leak (see the INVALID_HOLDER_EMAIL exception in the
	// api-errors skill). The bound is catalog.MaxHolderNameLength, so the
	// domain still owns the number.
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// validateHolderName checks the two halves of a Holder's name the way every
// handler checks required fields: blank is REQUIRED, over the domain's cap is
// TOO_LONG. Trimming here matches catalog.ParseHolderName, which is what the
// service will parse the same values with.
func validateHolderName(firstName, lastName string) []platform.FieldError {
	var fields []platform.FieldError
	for _, half := range []struct {
		field, value string
	}{
		{"first_name", firstName},
		{"last_name", lastName},
	} {
		trimmed := strings.TrimSpace(half.value)
		switch {
		case trimmed == "":
			fields = append(fields, platform.FieldError{
				Field: half.field, Code: platform.CodeRequired, Message: "is required",
			})
		case len(trimmed) > catalog.MaxHolderNameLength:
			fields = append(fields, platform.FieldError{
				Field: half.field,
				Code:  platform.CodeTooLong,
				Message: "must be at most " +
					strconv.Itoa(catalog.MaxHolderNameLength) + " characters",
			})
		}
	}
	return fields
}

// assignmentLinkAnswerBody is the token plus the Answer being given.
//
// It embeds answerBody rather than restating five fields, so that what a Holder
// posts and what Event Staff post cannot drift apart — they are answering the
// same Ticket Question against the same catalog.ParseAnswer.
type assignmentLinkAnswerBody struct {
	answerBody
	// Token is the signed Assignment Link token, and it is the ONLY authority
	// this write has.
	Token string `json:"token"`
}

// AcceptAssignmentLink accepts the Ticket Assignment a signed Assignment Link
// names, and returns the page its Holder lands on.
//
// @Summary      Accept a ticket assignment
// @Description  Accepts the Ticket Assignment the signed Assignment Link names: the Ticket moves to `accepted`, and a Customer is created or matched on the normalised email address the buyer gave and marked Verified — the click being Proof of Email Ownership (ADR 0035, ADR 0046). Requires no sign-in, no passcode and no password, and mints no session. **Accepting twice is idempotent**, so a second click lands on the same page and keeps the first acceptance's instant. **It grants no Marketing Consent and no consent of any kind.** The response discloses only the Event name, the Ticket Type name, the Holder's own current name for the form to prefill, and this Ticket's Ticket Questions — never the buyer's name or email, the price, the Tax ID, the Sale Confirmation reference, or the Sale's other Tickets. **The name is prefilled only here, after the click**: no route reports it before, which would make this an oracle for whether an address is registered. Refused with 401 ASSIGNMENT_LINK_INVALID when the token was tampered with, truncated, signed by another deployment or for another purpose, names a Ticket that no longer exists, names one whose Ticket Sale has been reversed, or names an assignment the Ticket no longer carries because it was reassigned; those causes are deliberately indistinguishable, because telling them apart would disclose what the buyer did. Refused with 401 ASSIGNMENT_LINK_EXPIRED once the Event has started, which is told apart only because an Event's start is already published. Answers 404 while TICKET_ASSIGNMENT_ENABLED is off. The questions list is empty while the separate Ticket Question flag is off.
// @Tags         public
// @Accept       json
// @Produce      json
// @Param        body  body      assignmentLinkBody  true  "The signed assignment link token"
// @Success      200  {object}  openapi.EnvelopeAssignmentLink
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/public/assignment-link [post]
func (h *Handler) AcceptAssignmentLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body assignmentLinkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	// The same "a blank token is a MALFORMED REQUEST" rule the Answer Link's
	// routes are held to, and the same helper: the Storefront only ever sends a
	// token it found in the address, so an empty one means the relay is broken
	// rather than that somebody's link is.
	token, ok := linkToken(w, reqID, body.Token)
	if !ok {
		return
	}

	view, err := h.svc.AcceptAssignmentLink(r.Context(), token)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}

// NameByAssignmentLink writes the Holder's own first and last name.
//
// PUT, because it states the whole current fact every time: there is one name
// per person, and correcting a typo captured at some door sale years ago is the
// same request with a different body.
//
// @Summary      Give the holder's name through an assignment link
// @Description  Writes the first and last name of the Holder who accepted the Ticket the signed Assignment Link names, as that Customer's current asserted name — stored separately (ADR 0005), and overwriting whatever the record held, since the person editing is the person the record is about. **A Holder is never asked for a Tax ID**: it is a fact about the sale's buyer, never about an attendee, and this body has nowhere to put one. It accepts the assignment first if it has not been accepted already, so the name and the click are one act. No sign-in, no session minted, and no consent granted. Refused with 400 VALIDATION_FAILED carrying `details.fields` when either half of the name is blank or over 100 characters, and with the same 401s the accept route gives. Answers 404 while TICKET_ASSIGNMENT_ENABLED is off.
// @Tags         public
// @Accept       json
// @Produce      json
// @Param        body  body      assignmentLinkNameBody  true  "The signed token and the holder's name"
// @Success      200  {object}  openapi.EnvelopeAssignmentLink
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/public/assignment-link/name [put]
func (h *Handler) NameByAssignmentLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body assignmentLinkNameBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	token, ok := linkToken(w, reqID, body.Token)
	if !ok {
		return
	}
	if fields := validateHolderName(body.FirstName, body.LastName); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	view, err := h.svc.NameByAssignmentLink(r.Context(), token, body.FirstName, body.LastName)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}

// AnswerByAssignmentLink writes one Answer on the authority of an Assignment
// Link — the Holder answering for themselves, which is what the whole accept
// step exists to make possible.
//
// The question is named in the PATH while the token stays in the body, exactly
// as on the Answer Link's write: the question id is not a credential — it is one
// of the ids the page just rendered — and putting it in the address is what
// makes these read as one resource with sub-resources rather than as three RPCs.
// The TICKET id is still nowhere, because the token names it.
//
// @Summary      Answer a ticket question through an assignment link
// @Description  Writes the Answer to one Ticket Question on the Ticket a signed Assignment Link names, given by the Holder themselves. It accepts the assignment first if it has not been accepted already. Once a Ticket is accepted its Answer Link stops opening, so an answer given here cannot be overwritten by somebody still holding a forwarded link — which is what accepting buys. The buyer and Event Staff keep their own routes to correct an Answer (ADR 0044). Refused with 400 INVALID_ANSWER when the value does not fit the question's kind, and with the same 401s the accept route gives. Answers 404 while TICKET_ASSIGNMENT_ENABLED is off, and 404 while the separate Ticket Question flag is off.
// @Tags         public
// @Accept       json
// @Produce      json
// @Param        questionId  path      string                    true  "Ticket question ID"
// @Param        body        body      assignmentLinkAnswerBody  true  "The signed token, and the answer in the shape its question's kind takes"
// @Success      200  {object}  openapi.EnvelopeAssignmentLink
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/public/assignment-link/questions/{questionId} [put]
func (h *Handler) AnswerByAssignmentLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}

	var body assignmentLinkAnswerBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	token, ok := linkToken(w, reqID, body.Token)
	if !ok {
		return
	}

	// Nothing about the Answer is validated here, exactly as on the three other
	// answering routes: every rule needs the question's KIND, which this layer
	// does not have and must not guess.
	view, err := h.svc.AnswerByAssignmentLink(r.Context(), token, questionID, service.AnswerInput{
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
