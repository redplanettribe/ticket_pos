package handler

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Closing Sweep's operator surfaces (#750, spec #742, ADR 0078): the
// Operator Dashboard's line of facturas that couldn't be swept, its Sweep
// again, and the acknowledgement of an Unrecorded Annulment on a factura's
// detail and needs-attention entry. The sweep itself runs on the Sale
// Invoice Drainer's tick; nothing here asks the SRI.

// GetClosingSweepStatus returns the dashboard's unswept count.
//
// @Summary      Count the facturas the Closing Sweep couldn't sweep
// @Description  Returns the Operator Dashboard's Closing Sweep line (#750, spec #742, ADR 0078). Once a month, from 00:00 America/Guayaquil on day 8 of the following month through the end of day 12, the Sale Invoice Drainer's tick asks the SRI's ConsultaComprobante about every production factura of the month just closed that the platform holds as authorized (Sale Invoices and manual Tax Invoices, credited ones included), one at a time and paced; one reading per factura is final. `month` is the latest month whose sweep has ended ("YYYY-MM": the month just closed from day 13, the month before it until then), `unswept_count` how many of its facturas still have no reading - the dashboard says "N facturas from <month> couldn't be swept" when it is not zero - and `due_again_count` how many of those a Sweep again has made due that no tick has asked yet. Zero is the ordinary answer. Read-only. Not behind SALE_INVOICING_ENABLED: manual Tax Invoices are swept with the flag closed. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeClosingSweepStatus
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/closing-sweep [get]
func (h *Handler) GetClosingSweepStatus(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	status, err := h.svc.GetClosingSweepStatus(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, status)
}

// SweepAgain makes the unswept facturas due again for the next tick.
//
// @Summary      Sweep the unswept facturas again
// @Description  Makes the facturas the Operator Dashboard counts as unswept - those of the latest month whose Closing Sweep has ended that still have no reading - and just those, due again on the Sale Invoice Drainer's next tick, whatever the day (#750, ADR 0078). The tick asks each once more, paced as the sweep always is; one that again gets no reading stays unswept. A factura with a reading is final and is never asked again. Answers the dashboard's line as it then stands. Safe to press twice; with nothing unswept it does nothing. No body. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeClosingSweepStatus
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/closing-sweep/sweep-again [post]
func (h *Handler) SweepAgain(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	status, err := h.svc.SweepAgain(r.Context(), session.Email)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, status)
}

// AcknowledgeUnrecordedAnnulment records an operator's acknowledgement of
// a factura's Unrecorded Annulment.
//
// @Summary      Acknowledge an Unrecorded Annulment
// @Description  Records that a Platform Operator has seen a factura's Unrecorded Annulment - the Closing Sweep found the SRI reporting annulled a factura the platform holds as authorized - where Mark annulled cannot record it because a live Credit Note (or a live Reissue successor) stands against the factura (#750, spec #742, ADR 0078). Who (the session's email) and when are recorded on `unrecorded_annulment`; the needs-attention entry clears; the flag itself stays, for the Tax Document Archive. The factura stays authorized and credited, and nothing is sent to the buyer: whether that Credit Note holds against a factura the SRI holds as annulled is the accountant's question. No body. Refused with INVOICE_NO_UNRECORDED_ANNULMENT (409) on a document carrying none, INVOICE_UNRECORDED_ANNULMENT_ACKNOWLEDGED (409) when acknowledged already, INVOICE_ANNUL_INSTEAD (409) when Mark annulled is admitted and is the true act, INVOICE_NOT_FOUND (404) otherwise. Not behind SALE_INVOICING_ENABLED. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  string  true  "Document id"
// @Success      200  {object}  openapi.EnvelopeInvoiceDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id}/acknowledge-unrecorded-annulment [post]
func (h *Handler) AcknowledgeUnrecordedAnnulment(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	// Who acknowledged comes from the session and nowhere else.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	invoice, err := h.svc.AcknowledgeUnrecordedAnnulment(r.Context(), id, session.Email)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, invoice)
}
