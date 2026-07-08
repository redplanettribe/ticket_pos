package middleware

import (
	"context"
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

type contextKey int

const staffSessionKey contextKey = 1

// StaffSession is the authenticated session attached to staff requests.
type StaffSession struct {
	SessionID      string
	Email          string
	ActiveMemberID string
}

// SessionFromContext returns the staff session stored on the request context.
func SessionFromContext(ctx context.Context) (*StaffSession, bool) {
	session, ok := ctx.Value(staffSessionKey).(*StaffSession)
	return session, ok
}

// SessionAuth validates the bearer session token and extends the session.
func SessionAuth(svc *service.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := platform.RequestID(r.Context())
			token := platform.BearerToken(r)
			if token == "" {
				_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
				return
			}

			view, err := svc.GetSession(r.Context(), token)
			if err != nil {
				_ = platform.WriteDomainError(w, reqID, err)
				return
			}

			staffSession := &StaffSession{
				SessionID: token,
				Email:     view.Email,
			}
			if view.ActiveMember != nil {
				staffSession.ActiveMemberID = view.ActiveMember.MemberID
			}

			ctx := context.WithValue(r.Context(), staffSessionKey, staffSession)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireActiveMember blocks staff workflow routes when no organization is selected.
func RequireActiveMember(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := platform.RequestID(r.Context())
		session, ok := SessionFromContext(r.Context())
		if !ok || session.ActiveMemberID == "" {
			_ = platform.WriteDomainError(w, reqID, identity.ErrNoActiveMember())
			return
		}
		next.ServeHTTP(w, r)
	})
}
