package handler

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The documents that need an operator (#477): the needs-attention queue
// and its count for the Operator Dashboard, and Mark annulled on the
// document detail. The queue takes a page and nothing else; Mark annulled
// takes, on a factura, the typed confirm (#745) and nothing on a Credit
// Note - who annulled comes from the session, and the answer is the
// document as it then stands.
//
// THE QUEUE IS A UNION OF TWO CONDITIONS (#581, ADR 0068), and both the
// list's and the count's descriptions say so in full, because an API
// description that said "status = needs_attention" would be a lie an
// integrator could act on: documents parked `needs_attention`, OR
// `abandoned` Sale Invoices with no live successor whose Ticket Sale still
// stands. The rule and the composition of its two orderings live in the
// repository (needsAttentionWhere, needsAttentionOrder).

// ListNeedsAttention lists the needs-attention queue, oldest first.
//
// @Summary      List the documents that need attention
// @Description  Returns a page of the needs-attention queue, which is a UNION OF THREE CONDITIONS and not one status (ADR 0068, ADR 0078): every document parked `needs_attention` (ADR 0060) - a Sale Invoice or Credit Note the SRI refused, one unanswered for 24 hours and still polled, or one that could not be signed - OR every `abandoned` or `annulled` Sale Invoice whose Ticket Sale still stands and has no factura standing for it: a Sale whose buyer holds no valid factura and which nothing yet owes one, the state a Sale sits in between Abandon or Mark annulled and its Issue again or Reissue - OR every annulled factura (Sale Invoice or manual Tax Invoice) whose Annulment Check stands at mismatch: the SRI still reports it authorized. The first two self-clear: the moment Issue again or a Reissue owes a replacement, or the Sale is reversed, the dead document leaves; the third clears when the SRI reports ANULADO. A dead document on a reversed Sale is not listed by the second door, nor is an abandoned or annulled Credit Note or manual Tax Invoice, since no act could ever clear such a row. Each row says which door it came in by in `attention_reason`: `needs_attention`, `abandoned`, `annulled` or `annulment_mismatch`, the last winning where an annulled Sale Invoice comes in by both, with `mismatch_since` on that reason alone, and `reinstate_admitted` true there once the factura's Annulment Window has closed, when Reinstate is offered (#749). LONGEST WAITING FIRST by the instant each entered the queue - `attention_since` when it was parked, `abandoned_at` when it was abandoned, `mismatch_since` at mismatch, `annulled_at` when annulled - one clock, so the doors interleave. Every row is the invoicing list row (kind, number when signed, Sale Confirmation reference, Recipient, total, status, `attention_since` and `abandoned_at`) plus `messages`: the SRI's last messages verbatim, or the platform's own PLATFORM-typed message saying why the document could not be signed. Each row opens the document detail by its id. An empty page is the ordinary answer. Response is the ADR-0006 nested envelope { data, pagination }; page_size defaults to 50 (max 100). Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "Page number (default 1)"
// @Param        page_size  query  int  false  "Page size (default 50, max 100)"
// @Success      200  {object}  openapi.EnvelopeNeedsAttentionQueue
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/needs-attention [get]
func (h *Handler) ListNeedsAttention(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	query := r.URL.Query()
	queue, err := h.svc.ListNeedsAttention(r.Context(), pageParam(query.Get("page")), pageSizeParam(query.Get("page_size")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, queue)
}

// CountNeedsAttention returns the queue's size as one number.
//
// @Summary      Count the documents that need attention
// @Description  Returns needs_attention_count: how many documents are in the needs-attention queue - the badge the Operator Dashboard shows so that a stuck document is never silent (ADR 0060). It counts the same UNION the queue lists and not one status (ADR 0068, ADR 0078): documents parked `needs_attention` across every kind, plus `abandoned` or `annulled` Sale Invoices of a Ticket Sale that still stands and has no factura standing for it, plus annulled facturas whose Annulment Check stands at mismatch, so a Sale left without a factura between Abandon or Mark annulled and its replacement is counted in the number the operator watches daily. It counts exactly what the queue lists, so the two can never disagree. Zero is an ordinary answer. Read-only. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeNeedsAttentionCount
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/needs-attention/count [get]
func (h *Handler) CountNeedsAttention(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	count, err := h.svc.CountNeedsAttention(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, count)
}

// CountRecipientWarnings returns how many documents carry a Recipient Warning.
//
// @Summary      Count the documents carrying a Recipient Warning
// @Description  Returns recipient_warning_count: how many authorized Sale Invoices the SRI warned about — the Recipient's Tax ID does not exist (advertencia 59) or is incorrect (62) — and that have not been superseded (ADR 0061). The badge the Operator Dashboard shows beside the needs_attention count, so a factura declared to the wrong taxpayer is learned of without a complaint arriving. It counts exactly what the invoicing list's `recipient_warning=true` filter lists. Zero is an ordinary answer. Read-only. Behind SALE_INVOICING_ENABLED: while the flag is closed this answers 404 SALE_INVOICING_UNAVAILABLE and the dashboard shows no count. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeRecipientWarningCount
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/recipient-warnings/count [get]
func (h *Handler) CountRecipientWarnings(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	count, err := h.svc.CountRecipientWarnings(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, count)
}

// annulInvoiceBody is Mark annulled's confirm as submitted (#745, ADR 0078).
// On a factura: the document's number typed in full, an optional note of
// at most 500 characters, and past the Annulment Window the operator's
// acknowledgement that the portal took the annulment. A Credit Note's
// Mark annulled reads none of it, so no body at all stays a valid press.
type annulInvoiceBody struct {
	Number           *string `json:"number"`
	Note             *string `json:"note"`
	LateAcknowledged *bool   `json:"late_acknowledged"`
}

// AnnulInvoice marks a document annulled.
//
// @Summary      Mark a document annulled
// @Description  Records that the Platform Operator annulled the document by hand at the SRI portal - the SRI offers no web service for annulment (ADR 0060, ADR 0078) - and answers with the document as it then stands: status `annulled`, `annulled_by` the operator's email from the session and `annulled_at` the moment. Nothing is sent to or asked of the SRI. The row keeps its number, clave de acceso, signed XML, authorization and the SRI's last messages; `next_attempt_at` is cleared so the Sale Invoice Drainer never claims it again, and it leaves the needs-attention queue. Irreversible here, but for Reinstate on a factura whose window has closed with its Annulment Check at mismatch (#749). ON A FACTURA (kind `sale` or `manual`, #745) it is one act from `pending`, `needs_attention` or `authorized`, whether the portal step came before the recording or after it, with one set of gates in every state, and the test environment is admitted as production is. The body is the confirm: `number`, the document's printed number `estab-ptoEmi-secuencial` typed in full (required); `note`, optional, at most 500 characters (blank is no note); `late_acknowledged`, the operator's word that the portal took the annulment, REQUIRED once the Annulment Window has closed and REFUSED while it is open. The window closes at the end of day 7 of the month after the Emission Date, in America/Guayaquil, read strictly - the SRI's rollover to the next business day is never claimed; `annulability.window` on the detail carries it. Each Mark annulled on a factura is a row of the detail's `annulment` (`current`: `from_status`, `annulled_by`, `annulled_at`, `note`, `late_acknowledged`). Refused with INVOICE_NUMBER_MISMATCH (409) when the typed number is missing or not the document's, INVOICE_REISSUE_STANDS (409) while a live Reissue successor stands in its place, INVOICE_CREDIT_NOTE_STANDS (409) on an authorized factura while a live Credit Note - owed, pending, needs_attention or authorized - stands against it (a withdrawn, abandoned, annulled, rejected or not_authorized one does not refuse; on a pending or needs_attention factura a Credit Note can only be owed and waiting for it, and annulling the factura withdraws it, so it never refuses there), INVOICE_ANNULMENT_LATE (409, `details.deadline`) past the deadline without the acknowledgement, INVOICE_ANNULMENT_NOT_LATE (409, `details.deadline`) with it before. The factura's facts are judged before what was typed. ON A CREDIT NOTE it is unchanged: only from `pending` or `needs_attention`, the body is not read, no window and no annulments row; annulling a Reissue's Credit Note withdraws the unsigned corrected factura in the same transaction (#484). On either: INVOICE_NOT_ANNULLABLE (409) on a document in any other state, INVOICE_ABANDON_INSTEAD (409) on a document parked `needs_attention` that the SRI refuses for its NUMBER - error 45, `secuencial registrado` (ADR 0068) - since the SRI never took that number and there is nothing at its portal to have been annulled; that document is abandoned instead, INVOICE_NOT_ISSUED (409) on one still owed or parked unsigned, INVOICE_NOT_FOUND (404) otherwise. The write is guarded on the state the document was read in, so a press racing a late AUTORIZADO or a reversal is refused rather than double-written. Afterwards Resend answers INVOICE_ANNULLED, and Check status asks the SRI's ConsultaComprobante and changes nothing (#744). Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  string            true   "Document id"
// @Param        body  body  annulInvoiceBody  false  "The confirm, on a factura"
// @Success      200  {object}  openapi.EnvelopeInvoiceDetail
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id}/annul [post]
func (h *Handler) AnnulInvoice(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	// Who annulled comes from the session and nowhere else: the trail names
	// the operator who acted, never one the caller named.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	// An empty body is a valid press: a Credit Note's Mark annulled takes
	// none, and on a factura the missing number is the service's refusal.
	var body annulInvoiceBody
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errorIsEmptyBody(err) {
			_ = platform.WriteInvalidJSON(w, reqID)
			return
		}
	}
	// The note is bounded and trimmed exactly as the Abandon's is: one
	// sentence for a colleague, blank is no note.
	note, fields := operatorNote(body.Note)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	req := invoicing.AnnulmentRequest{Note: note}
	if body.Number != nil {
		req.Number = *body.Number
	}
	if body.LateAcknowledged != nil {
		req.LateAcknowledged = *body.LateAcknowledged
	}
	invoice, err := h.svc.AnnulInvoice(r.Context(), id, session.Email, req)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, invoice)
}

// reinstateInvoiceBody is Reinstate's confirm as submitted (#749): the
// factura's number typed in full and an optional note of at most 500
// characters.
type reinstateInvoiceBody struct {
	Number *string `json:"number"`
	Note   *string `json:"note"`
}

// ReinstateInvoice returns an annulled factura to authorized.
//
// @Summary      Reinstate an annulled factura
// @Description  Returns an annulled factura (a Sale Invoice or a manual Tax Invoice) to `authorized` because the SRI never took the annulment and its Annulment Window has closed (#749, spec #742, ADR 0078): the platform's record follows the SRI's. Never automatic. Admitted only while the current annulment's Annulment Check stands at MISMATCH - the SRI reported the factura AUTORIZADO on two asks - and only once the window has closed (the end of day 7 of the month after the Emission Date, in America/Guayaquil). The body is the confirm: `number`, the factura's printed number `estab-ptoEmi-secuencial` typed in full (required), and `note`, optional, at most 500 characters (blank is no note). In one transaction: the status returns to `authorized`, `annulled_by` and `annulled_at` are cleared, and the annulment is kept as history (`annulment.past`) stamped with `reinstated_by` (the session's email), `reinstated_at` and `reinstate_note`, its check stopped and its held Annulment Notice dropped, never sent. ONE RULE decides what it owes: a Credit Note when the Ticket Sale was reversed (the reversal's route as its reason) or when another factura stands for the Sale in its place, an Issue again or Reissue successor (reason `reissue`, so the pair takes a Reissue's shape, and an unsigned successor then waits for that Credit Note before it is signed); otherwise none, and the factura is the Sale's current one again. A manual Tax Invoice simply returns to authorized. The Credit Note is owed and drained by the Sale Invoice Drainer's ordinary path. A Sale Invoice never delivered is delivered as any authorized one is. A buyer already sent the Annulment Notice is sent, when no Credit Note follows, a reinstatement notice carrying the RIDE and XML again (`reinstatement_notice` on the past annulment); when one follows, its own mail is what they hear. A factura annulled from `pending` or `needs_attention` never stored its authorization: Reinstate asks the SRI's autorizacionComprobante for it at the press (an attempt on the ledger) and stores it, and is refused with INVOICE_REINSTATE_UNAUTHORIZED (409) unless the answer is AUTORIZADO with the authorized XML. A reinstated factura can be annulled again, past the deadline with the late acknowledgement, which starts a fresh Annulment Check. Refused with INVOICE_NOT_REINSTATABLE (409, `details.status`) on a document that is not an annulled factura, INVOICE_REINSTATE_WINDOW_OPEN (409, `details.deadline`) while the window is open, INVOICE_REINSTATE_NOT_MISMATCH (409, `details.verdict`) when the check is checking, confirmed or unconfirmed, INVOICE_NUMBER_MISMATCH (409) when the typed number is missing or not the factura's, INVOICE_NOT_FOUND (404) otherwise. The facts are judged before what was typed. Not behind SALE_INVOICING_ENABLED, as Mark annulled is not: a manual Tax Invoice is reinstated with the flag closed. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  string                true  "Document id"
// @Param        body  body  reinstateInvoiceBody  true  "The confirm"
// @Success      200  {object}  openapi.EnvelopeInvoiceDetail
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id}/reinstate [post]
func (h *Handler) ReinstateInvoice(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	// Who reinstated comes from the session and nowhere else.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	// An empty body is a valid press: the missing number is the service's
	// refusal, as Mark annulled's is.
	var body reinstateInvoiceBody
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errorIsEmptyBody(err) {
			_ = platform.WriteInvalidJSON(w, reqID)
			return
		}
	}
	note, fields := operatorNote(body.Note)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	req := invoicing.ReinstateRequest{Note: note}
	if body.Number != nil {
		req.Number = *body.Number
	}
	invoice, err := h.svc.ReinstateInvoice(r.Context(), id, session.Email, req)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, invoice)
}
