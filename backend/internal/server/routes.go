package server

import (
	"net/http"

	identitymiddleware "github.com/peter/ticket_pos/backend/internal/identity/middleware"
	platformhandler "github.com/peter/ticket_pos/backend/internal/platform/handler"
)

// RegisterRoutes wires all HTTP routes onto mux.
func RegisterRoutes(mux *http.ServeMux, app *App) {
	mux.HandleFunc("GET /health", platformhandler.Health)
	mux.Handle("GET /swagger/", SwaggerHandler())

	registerAuthRoutes(mux, app)
	registerStaffRoutes(mux, app)
}

func registerAuthRoutes(mux *http.ServeMux, app *App) {
	h := app.IdentityHandler
	mux.HandleFunc("POST /api/v1/auth/otp/request", h.RequestOTP)
	mux.HandleFunc("POST /api/v1/auth/otp/verify", h.VerifyOTP)
	mux.HandleFunc("GET /api/v1/auth/session", h.GetSession)
	mux.HandleFunc("POST /api/v1/auth/logout", h.Logout)
}

func registerStaffRoutes(mux *http.ServeMux, app *App) {
	h := app.IdentityHandler
	ch := app.CatalogHandler
	svc := app.IdentityService

	mux.HandleFunc("POST /api/v1/staff/organizations", h.CreateOrganization)
	mux.HandleFunc("GET /api/v1/staff/memberships", h.ListMemberships)
	mux.HandleFunc("POST /api/v1/staff/session/organization", h.SelectOrganization)
	mux.Handle(
		"GET /api/v1/staff/me",
		identitymiddleware.SessionAuth(svc)(
			identitymiddleware.LoadActiveMember(svc)(
				http.HandlerFunc(h.GetStaffMe),
			),
		),
	)

	orgAdmin := func(handler http.Handler) http.Handler {
		return identitymiddleware.SessionAuth(svc)(
			identitymiddleware.LoadActiveMember(svc)(
				identitymiddleware.RequireOrgAdmin(handler),
			),
		)
	}

	mux.Handle("GET /api/v1/staff/organization", orgAdmin(http.HandlerFunc(h.GetOrganization)))
	mux.Handle("PATCH /api/v1/staff/organization", orgAdmin(http.HandlerFunc(h.UpdateOrganization)))
	mux.Handle("DELETE /api/v1/staff/organization", orgAdmin(http.HandlerFunc(h.DeleteOrganization)))

	mux.Handle("GET /api/v1/staff/members", orgAdmin(http.HandlerFunc(h.ListMembers)))
	mux.Handle("POST /api/v1/staff/members", orgAdmin(http.HandlerFunc(h.AddMember)))
	mux.Handle("PATCH /api/v1/staff/members/{memberID}", orgAdmin(http.HandlerFunc(h.UpdateMember)))
	mux.Handle("DELETE /api/v1/staff/members/{memberID}", orgAdmin(http.HandlerFunc(h.RemoveMember)))

	mux.Handle("GET /api/v1/staff/events", orgAdmin(http.HandlerFunc(ch.ListEvents)))
	mux.Handle("POST /api/v1/staff/events", orgAdmin(http.HandlerFunc(ch.CreateEvent)))
	mux.Handle("GET /api/v1/staff/events/{id}", orgAdmin(http.HandlerFunc(ch.GetEvent)))
	mux.Handle("PATCH /api/v1/staff/events/{id}", orgAdmin(http.HandlerFunc(ch.UpdateEvent)))
	mux.Handle("DELETE /api/v1/staff/events/{id}", orgAdmin(http.HandlerFunc(ch.DeleteEvent)))
	mux.Handle("POST /api/v1/staff/events/{id}/publish", orgAdmin(http.HandlerFunc(ch.PublishEvent)))
	mux.Handle("POST /api/v1/staff/events/{id}/cancel", orgAdmin(http.HandlerFunc(ch.CancelEvent)))
	mux.Handle("POST /api/v1/staff/events/{id}/cover-upload-url", orgAdmin(http.HandlerFunc(ch.CreateCoverUploadURL)))
	mux.Handle("GET /api/v1/staff/events/{id}/ticket-types", orgAdmin(http.HandlerFunc(ch.ListTicketTypes)))
	mux.Handle("POST /api/v1/staff/events/{id}/ticket-types", orgAdmin(http.HandlerFunc(ch.CreateTicketType)))
	mux.Handle("PATCH /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}", orgAdmin(http.HandlerFunc(ch.UpdateTicketType)))
	mux.Handle("DELETE /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}", orgAdmin(http.HandlerFunc(ch.DeleteTicketType)))
	mux.Handle("GET /api/v1/staff/events/{eventID}/assignments", orgAdmin(http.HandlerFunc(h.ListEventAssignments)))
	mux.Handle("PUT /api/v1/staff/events/{eventID}/assignments/{memberID}", orgAdmin(http.HandlerFunc(h.UpsertEventAssignment)))
	mux.Handle("DELETE /api/v1/staff/events/{eventID}/assignments/{memberID}", orgAdmin(http.HandlerFunc(h.RemoveEventAssignment)))
}
