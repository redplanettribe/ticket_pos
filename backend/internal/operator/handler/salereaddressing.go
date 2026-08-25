package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// reAddressSaleBody is a Sale Re-addressing as the operator states it: the
// address the buyer meant, and an optional note for whoever reads the record
// later. Who recorded it is never in the body.
type reAddressSaleBody struct {
	Email string  `json:"email"`
	Note  *string `json:"note"`
}

// ReAddressSale records a Sale Re-addressing against one Ticket Sale.
//
// @Summary      Re-address an Online Sale to the address its buyer meant
// @Description  Records a Sale Re-addressing against the active Online Sale named by a Sale Confirmation reference: the address the buyer meant, and an optional note (at most 500 characters). The platform then mails THAT address a Re-addressing Link, in the Sale's own locale, saying the purchase is being re-addressed to them at the organizer's request and that accepting takes it on; the wrong address is told nothing. NOTHING MOVES YET: the Sale still belongs to whoever it belonged to until the corrected address clicks the link (ADR 0058), and the response is the pending record — corrected address (normalised as a Customer's is), the Sale's address at request time, the acting operator (taken from the Staff Session, never from the body), the note and requested_at. THE RESPONSE CARRIES NO TOKEN AND NO LINK: the link is delivered to the corrected address alone, so the Operator cannot complete the acceptance themself. The Payment Provider is never called — no money moves. Refused for a sale that is not an Online Sale (SALE_NOT_RE_ADDRESSABLE: an imported sale is corrected through its Sale Import), one already reversed (SALE_ALREADY_REVERSED), one whose Event has already started (RE_ADDRESSING_EVENT_STARTED, read live in the Event's timezone), a correction that normalises to the address the sale already carries (RE_ADDRESSING_SAME_ADDRESS), and a sale with a pending re-addressing already (RE_ADDRESSING_ALREADY_PENDING). Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        confirmationRef  path  string             true  "Sale Confirmation reference (case-insensitive)"
// @Param        body             body  reAddressSaleBody  true  "The address the buyer meant, and an optional note"
// @Success      201  {object}  openapi.EnvelopeOperatorSaleReAddressing
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/sales/{confirmationRef}/re-address [post]
func (h *Handler) ReAddressSale(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body reAddressSaleBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	input, fields := validateReAddressSale(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	// Who recorded this comes from the session and nowhere else: a record of
	// who moved a paid Sale to a new address is never anonymous, and never
	// signed by somebody the caller named.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	input.Operator = session.Email

	recorded, err := h.svc.ReAddressSale(r.Context(), strings.TrimSpace(r.PathValue("confirmationRef")), input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, recorded)
}

// validateReAddressSale checks the recording's shape: an address that is an
// address, normalised as a Customer's is, and a note within bounds.
//
// What it deliberately does NOT check is anything that needs the sale — the
// channel, the status, the Event's start, and whether the address is the one
// the sale already carries. Those are facts about the Ticket Sale rather than
// about this request, and are judged where the sale is loaded.
func validateReAddressSale(body reAddressSaleBody) (service.ReAddressSaleInput, []platform.FieldError) {
	var fields []platform.FieldError

	var email string
	if strings.TrimSpace(body.Email) == "" {
		fields = append(fields, platform.FieldError{Field: "email", Code: platform.CodeRequired, Message: "is required"})
	} else if parsed, ok := sales.ParseCorrectedEmail(body.Email); !ok {
		fields = append(fields, platform.FieldError{Field: "email", Code: platform.CodeInvalidEmail, Message: "must be a valid email address"})
	} else {
		email = parsed
	}

	var note *string
	if body.Note != nil {
		if trimmed := strings.TrimSpace(*body.Note); trimmed != "" {
			if len([]rune(trimmed)) > sales.MaxReAddressingNoteLength {
				fields = append(fields, platform.FieldError{
					Field:   "note",
					Code:    platform.CodeTooLong,
					Message: "must be at most " + strconv.Itoa(sales.MaxReAddressingNoteLength) + " characters",
				})
			}
			note = &trimmed
		}
	}

	return service.ReAddressSaleInput{CorrectedEmail: email, Note: note}, fields
}
