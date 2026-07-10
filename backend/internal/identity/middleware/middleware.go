package middleware

import (
	"context"
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

type contextKey int

const (
	staffSessionKey contextKey = 1
	activeMemberKey contextKey = 2
)

// StaffSession is the authenticated session attached to staff requests.
type StaffSession struct {
	SessionID      string
	Email          string
	ActiveMemberID string
}

// ActiveMember is the selected member context on a staff request.
type ActiveMember struct {
	MemberID       string
	OrganizationID string
	Role           string
	Email          string
}

// SessionFromContext returns the staff session stored on the request context.
func SessionFromContext(ctx context.Context) (*StaffSession, bool) {
	session, ok := ctx.Value(staffSessionKey).(*StaffSession)
	return session, ok
}

// ActiveMemberFromContext returns the active member stored on the request context.
func ActiveMemberFromContext(ctx context.Context) (*ActiveMember, bool) {
	member, ok := ctx.Value(activeMemberKey).(*ActiveMember)
	return member, ok
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

// ActiveMemberFromContextRole returns the active member role from context.
func ActiveMemberFromContextRole(r *http.Request) string {
	member, ok := ActiveMemberFromContext(r.Context())
	if !ok {
		return ""
	}
	return member.Role
}

// LoadActiveMember resolves the active member and attaches it to the request context.
func LoadActiveMember(svc *service.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := platform.RequestID(r.Context())
			session, ok := SessionFromContext(r.Context())
			if !ok || session.ActiveMemberID == "" {
				_ = platform.WriteDomainError(w, reqID, identity.ErrNoActiveMember())
				return
			}

			actor, err := svc.LoadActiveMemberContext(r.Context(), session.SessionID)
			if err != nil {
				_ = platform.WriteDomainError(w, reqID, err)
				return
			}

			ctx := context.WithValue(r.Context(), activeMemberKey, &ActiveMember{
				MemberID:       actor.MemberID,
				OrganizationID: actor.OrganizationID,
				Role:           string(actor.Role),
				Email:          actor.Email,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireOrgAdmin blocks requests unless the active member is an Org Admin.
func RequireOrgAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := platform.RequestID(r.Context())
		member, ok := ActiveMemberFromContext(r.Context())
		if !ok || member.Role != "org_admin" {
			_ = platform.WriteDomainError(w, reqID, identity.ErrForbidden())
			return
		}
		next.ServeHTTP(w, r)
	})
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
