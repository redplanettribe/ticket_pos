package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Abandon (#578, parent #575, ADR 0068): the operator's record that the Tax
// Authority never took the document and never will. The body is one optional
// note; who abandoned it comes from the session and is never accepted from
// the caller, exactly as Mark annulled's author and the reissue's are.

// abandonInvoiceBody is the abandon form as submitted: a note, or nothing.
type abandonInvoiceBody struct {
	Note *string `json:"note"`
}

// AbandonInvoice records that the authority never took the document.
//
// @Summary      Abandon a document the SRI refuses by number
// @Description  Records that the SRI never took this document and never will — its recepción answer was 45, `ERROR SECUENCIAL REGISTRADO`, the authority refusing the NUMBER the document carries (ADR 0068) — and answers with the document as it then stands: status `abandoned`, `abandoned_by` the operator's email from the session, `abandoned_at` the moment and `abandon_note` the optional `note` (at most 500 characters). `abandoned` is a TERMINAL state of its own and not an annulment: it says the document was never a legal document, so nothing is owed at the SRI portal and nothing is ever declared for it, where `annulled` says the SRI held the document and the operator disowned it by hand there. Nothing is sent to or asked of the SRI. The row keeps its number, clave de acceso, signed XML, the SRI's last messages and every attempt, and the secuencial stays consumed — an abandoned number is never handed out again. `next_attempt_at` is cleared so the Sale Invoice Drainer never claims it again, and it leaves the needs-attention queue; a late `AUTORIZADO` for the clave is recorded in the attempts ledger and does not heal the row. Offered on any document kind, since the number is dead whoever typed it. Requires A FRESH CHECK STATUS IMMEDIATELY BEFOREHAND: the last attempt on the document's ledger must be a `check` that the SRI answered, made within the last 15 minutes, so the decision rests on the authority's own current answer and the ledger carries it, timestamped, immediately before the act — otherwise INVOICE_CHECK_NOT_FRESH (409), whose remedy is to press Check status and try again. Refused with INVOICE_NOT_REFUSED_BY_NUMBER (409) on a document the SRI refused for any other reason, INVOICE_NOT_ABANDONABLE (409, `details.status`) outside `needs_attention`, `rejected` and `not_authorized`, INVOICE_ALREADY_AUTHORIZED (409) on an authorized document, INVOICE_ABANDONED (409) on one already abandoned — as Check status and Resend also answer there — INVOICE_ANNULLED (409), INVOICE_WITHDRAWN (409), INVOICE_NOT_ISSUED (409) on one still owed and unsigned, INVOICE_NOT_FOUND (404) otherwise. Mark annulled answers INVOICE_ABANDON_INSTEAD (409) wherever this action qualifies. Irreversible. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  string               true   "Document id"
// @Param        body  body  abandonInvoiceBody   false  "An optional note"
// @Success      200  {object}  openapi.EnvelopeInvoiceDetail
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id}/abandon [post]
func (h *Handler) AbandonInvoice(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	// Who abandoned the document comes from the session and nowhere else:
	// the trail names the operator who acted, never one the caller named.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	// An empty body is a valid press: the note is the only field and it is
	// optional, so "no body" and "{}" mean the same thing.
	var body abandonInvoiceBody
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errorIsEmptyBody(err) {
			_ = platform.WriteInvalidJSON(w, reqID)
			return
		}
	}
	note, fields := abandonNote(body.Note)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	invoice, err := h.svc.AbandonInvoice(r.Context(), id, session.Email, note)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, invoice)
}

// errorIsEmptyBody says whether the decode failed only because there was
// nothing to decode. The note is optional, so a press with no body at all is
// the ordinary case and must not be answered as malformed JSON.
func errorIsEmptyBody(err error) bool {
	return err != nil && err.Error() == "EOF"
}

// abandonNote trims the operator's note and bounds it exactly as the
// reissue's is (invoicing.MaxReissueNoteLength, migration 102's 500
// characters): one sentence for a colleague — "not registered at the portal,
// confirmed by phone" — so the reason survives with the record. Blank is no
// note, never an empty one.
func abandonNote(raw *string) (string, []platform.FieldError) {
	if raw == nil {
		return "", nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return "", nil
	}
	if utf8.RuneCountInString(trimmed) > invoicing.MaxReissueNoteLength {
		return "", []platform.FieldError{{
			Field:   "note",
			Code:    platform.CodeTooLong,
			Message: "must be at most " + strconv.Itoa(invoicing.MaxReissueNoteLength) + " characters",
		}}
	}
	return trimmed, nil
}
