package handler

import (
	"time"

	"github.com/peter/ticket_pos/backend/internal/customers/service"
)

// ReAddressingAcceptedResponse is what the click on a Re-addressing Link
// returns (#421, ADR 0058): the Sale the buyer now owns and the sign-in
// outcome on a passcode's own terms — `session` and `session_id` when a
// Customer Session was minted, or `consent_required` with a pending consent
// token when the Policy is outstanding, exactly as POST /customer/auth/otp/
// verify answers. No cookie is set: the Storefront's BFF holds the session id
// as it does for every other sign-in.
//
// In its own file so that the customers service is imported under its own
// name: the OpenAPI generator resolves the session types by package name, and
// re_addressing_link.go already holds the sales service under that name.
type ReAddressingAcceptedResponse struct {
	TicketSaleID    string     `json:"ticket_sale_id"`
	EventName       string     `json:"event_name"`
	ConfirmationRef string     `json:"confirmation_ref"`
	CorrectedEmail  string     `json:"corrected_email"`
	AcceptedAt      *time.Time `json:"accepted_at"`

	Session         *service.CustomerSessionView `json:"session"`
	SessionID       string                       `json:"session_id"`
	ConsentRequired *service.ConsentRequiredView `json:"consent_required"`
}
