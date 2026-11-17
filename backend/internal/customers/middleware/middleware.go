// Package middleware gates Customer-scoped HTTP routes on a Customer Session.
//
// It is deliberately a separate middleware from the staff one rather than a
// branch inside it. A Customer Session and a Staff Session are unrelated records
// authorizing unrelated domains (ADR 0010); a staff token presented here resolves
// to nothing, and a Customer token presented on a staff route resolves to nothing
// either, because neither middleware can even see the other's table.
package middleware

import (
	"context"
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/customers/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

type contextKey int

const customerSessionKey contextKey = 1

// CustomerSession is the authenticated Customer attached to a request.
//
// TicketSaleID is empty for a full Customer Session and names one Ticket Sale for
// a Confirmation Link session.
type CustomerSession struct {
	SessionID    string
	CustomerID   string
	Email        string
	TicketSaleID string
}

// SessionFromContext returns the Customer Session stored on the request context.
func SessionFromContext(ctx context.Context) (*CustomerSession, bool) {
	session, ok := ctx.Value(customerSessionKey).(*CustomerSession)
	return session, ok
}

// withCustomerSession is ctx carrying the authenticated Customer, and the
// request's error reports placed by them as ids only (ADR 0079): the customer
// audience, the Customer id, and the one Ticket Sale a Confirmation Link
// session is for.
func withCustomerSession(ctx context.Context, actor *service.AuthenticatedCustomer) context.Context {
	platform.SetReportActor(ctx, platform.AudienceCustomer, actor.CustomerID)
	platform.SetReportTag(ctx, platform.ReportTagSale, actor.TicketSaleID)
	return context.WithValue(ctx, customerSessionKey, &CustomerSession{
		SessionID:    actor.SessionID,
		CustomerID:   actor.CustomerID,
		Email:        actor.Email,
		TicketSaleID: actor.TicketSaleID,
	})
}

// RequireCustomerSession validates the bearer Customer Session token, extends its
// sliding window, and attaches the authenticated Customer to the request context.
//
// The Customer it attaches is the only identity a downstream handler may act on.
// Nothing in the request body, path, or query may substitute for it.
func RequireCustomerSession(svc *service.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := platform.RequestID(r.Context())
			token := platform.BearerToken(r)
			if token == "" {
				_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
				return
			}

			actor, err := svc.Authenticate(r.Context(), token)
			if err != nil {
				_ = platform.WriteDomainError(w, reqID, err)
				return
			}

			ctx := withCustomerSession(r.Context(), actor)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalCustomerSession attaches the authenticated Customer when the request
// carries a valid Customer Session, and otherwise lets the request through
// untouched.
//
// It exists for the one route that is genuinely open but behaves better when it
// knows who is calling: the public checkout. Guest checkout is the baseline and
// must never require a session (ADR 0012), yet a signed-in Customer's Tax ID
// override is only their own assertion if the request proves they own the
// address (ADR 0016). So a bad, expired, or absent token is not an error here —
// it simply means anonymous — and a handler behind this middleware must treat a
// missing session as the ordinary case rather than as a denial.
func OptionalCustomerSession(svc *service.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := platform.BearerToken(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			actor, err := svc.Authenticate(r.Context(), token)
			if err != nil {
				// A token that authenticates nothing is worth exactly what no
				// token is worth: the checkout proceeds as a guest.
				next.ServeHTTP(w, r)
				return
			}
			ctx := withCustomerSession(r.Context(), actor)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
