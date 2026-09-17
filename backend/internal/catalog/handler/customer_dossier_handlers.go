package handler

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// GetCustomerDossier returns one Customer's Dossier at one Event (#638).
//
// @Summary      Get a customer's dossier at an event
// @Description  The Customer Dossier ("Ficha del cliente"): everything this Event knows about one Customer, addressed by the Customer's id and never their email. `customer` carries the Customer's id and email and nothing else from the platform-global Customer record. `sales` lists every one of THIS Event's Ticket Sales to the Customer, newest first, reversed and corrected ones included: each carries the first and last name and the Tax ID type and number given ON THAT SALE — never the Customer record's current values, which a purchase at another Organization may have changed — with its confirmation reference, sold-at and recorded-at, Sales Channel, `source` and `origin` exactly as the Sales list states them, Ticket Types and quantities, amount and currency, and payment method. `status` is `active`, `reversed` (with `reversed_at`) or `corrected` — a reversed Sale a Sale Correction replaced, naming its replacement in `replaced_by_confirmation_ref`. The Organization's other Events are never read. No Terms Acceptance, Adulthood Declaration, Marketing Consent or avatar is ever included. A READ AND NOTHING ELSE: nothing is sent from here. 404 for an Event outside the caller's Organization, and 404 CUSTOMER_NOT_FOUND for a Customer with no Ticket Sale on this Event — including a malformed id and an id naming no Customer, which are indistinguishable from one who bought only elsewhere. Org Admin and Event Owner only; Event Staff are refused with 403.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id          path      string  true  "Event ID"
// @Param        customerId  path      string  true  "Customer ID"
// @Success      200  {object}  openapi.EnvelopeCustomerDossier
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/customers/{customerId}/dossier [get]
func (h *Handler) GetCustomerDossier(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ok := pathValueRequired(w, r, reqID, "id")
	if !ok {
		return
	}
	customerID, ok := pathValueRequired(w, r, reqID, "customerId")
	if !ok {
		return
	}
	// A malformed Customer id is not found, exactly as a well-formed one with
	// nothing at this Event is: telling them apart would tell a caller probing
	// ids which guesses were at least well formed.
	if _, err := uuid.Parse(customerID); err != nil {
		_ = platform.WriteDomainError(w, reqID, catalog.ErrCustomerNotFoundAtEvent())
		return
	}

	dossier, err := h.svc.GetCustomerDossier(r.Context(), actorFromRequest(r), eventID, customerID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, dossier)
}
