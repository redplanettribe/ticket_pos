package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// DrainSaleInvoices runs the Sale Invoice Drainer: the platform signs,
// submits and polls the Sale Invoices its House checkouts owed, with nobody
// watching (#474, ADR 0060).
//
// Internal-scoped on the Reversal Reconciler's terms (sales/handler
// DrainReversalRequests, registerInternalRoutes): Cloud Run IAM authenticates
// the caller by OIDC token before the request reaches the API, and no
// application credential is read here. There is nothing to validate — no
// body, no path parameter — and WHICH documents are worked is a property of
// the queue, never of the caller.
//
// Safe to call by hand at any time and repeatedly: an empty queue is a 200
// with zeros, a document another drain holds is invisible under its lease,
// and the response counts documents and names states, never buyers.
//
// @Summary      Work the owed Sale Invoices
// @Description  Runs the Sale Invoice Drainer (ADR 0060): claims the Sale Invoices due for work one at a time under a `next_attempt_at` lease, signs each `owed` one from its stored snapshot — consuming a secuencial only then, under the Issuer's current environment, with `fechaEmision` the signing date in America/Guayaquil — submits it to the SRI and polls autorización; polls a `pending` one the SRI already holds without resubmitting it; resends only a document the SRI never acknowledged. Transport failures and RECIBIDA / EN PROCESAMIENTO reschedule on the ladder 1 min, 5 min, 15 min, then hourly from the signing instant. A definite refusal parks the document `needs_attention` with the SRI's messages; 24 hours without a definite answer parks it `needs_attention` while polling continues, and a late AUTORIZADO heals it. A document that cannot be signed — no Issuer, no certificate, an expired certificate, an Issuer the schema refuses — is parked `needs_attention` at once with no number consumed and retried hourly. Every SRI call is an attempts row. Bounded by its own budget, which expires before Cloud Scheduler's attempt deadline. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token (ADR 0008). Safe to call by hand at any time; a no-op on an empty queue. Behind SALE_INVOICING_ENABLED: while the flag is closed this answers 404 SALE_INVOICING_UNAVAILABLE and signs nothing. The response tallies what the run did and how many Sale Invoices stand in each state afterwards.
// @Tags         internal
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeSaleInvoiceDrain
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/internal/sale-invoices/drain [post]
func (h *Handler) DrainSaleInvoices(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	result, err := h.svc.DrainSaleInvoices(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
