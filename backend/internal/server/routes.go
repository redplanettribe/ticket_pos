package server

import (
	"net/http"

	customersmiddleware "github.com/peter/ticket_pos/backend/internal/customers/middleware"
	identitymiddleware "github.com/peter/ticket_pos/backend/internal/identity/middleware"
	platformhandler "github.com/peter/ticket_pos/backend/internal/platform/handler"
)

// RegisterRoutes wires all HTTP routes onto mux.
func RegisterRoutes(mux *http.ServeMux, app *App) {
	mux.HandleFunc("GET /health", platformhandler.Health)
	mux.Handle("GET /swagger/", SwaggerHandler())

	registerAuthRoutes(mux, app)
	registerStaffRoutes(mux, app)
	registerPublicRoutes(mux, app)
	registerCustomerRoutes(mux, app)
}

// registerCustomerRoutes wires the Storefront's Customer identity surface.
//
// It shares nothing with the staff routes above but the API version prefix: its
// own handler, its own service, its own session table, and its own middleware. A
// Staff Session token presented on these routes authenticates nothing, and a
// Customer Session token presented on a staff route authenticates nothing
// (ADR 0010).
func registerCustomerRoutes(mux *http.ServeMux, app *App) {
	h := app.CustomersHandler
	svc := app.CustomersService

	// Sign-in is unauthenticated by definition: these two mint the credential.
	mux.HandleFunc("POST /api/v1/customer/auth/otp/request", h.RequestOTP)
	mux.HandleFunc("POST /api/v1/customer/auth/otp/verify", h.VerifyOTP)
	// The second Proof of Email Ownership, and the same destination: this mints
	// the identical Customer Session the passcode above does, on an email Google
	// vouched for instead of one a passcode did (ADR 0011).
	mux.HandleFunc("POST /api/v1/customer/auth/google/verify", h.VerifyGoogle)
	// The Confirmation Link's token is itself the credential, so this route is
	// unauthenticated too. It reads Authorization when present, but only to
	// notice that the caller already holds something wider than a link.
	mux.HandleFunc("POST /api/v1/customer/auth/confirmation-link", h.RedeemConfirmationLink)

	// signedIn gates a route on a valid Customer Session and extends its sliding
	// window. Everything behind it is scoped to the Customer on that session.
	signedIn := customersmiddleware.RequireCustomerSession(svc)

	mux.Handle("GET /api/v1/customer/auth/session", signedIn(http.HandlerFunc(h.GetSession)))
	mux.Handle("POST /api/v1/customer/auth/logout", signedIn(http.HandlerFunc(h.Logout)))
	// The Customer Area read. There is deliberately no Customer, email, or
	// Organization in this path: the session is the only scope.
	mux.Handle("GET /api/v1/customer/ticket-sales", signedIn(http.HandlerFunc(h.ListTicketSales)))
}

func registerPublicRoutes(mux *http.ServeMux, app *App) {
	h := app.IdentityHandler
	ch := app.CatalogHandler
	mux.HandleFunc("GET /api/v1/public/organizations/{slug}", h.GetPublicOrganization)
	mux.HandleFunc("GET /api/v1/public/events", ch.ListPublicEvents)
	mux.HandleFunc("GET /api/v1/public/tags", ch.ListPublicTags)
	mux.HandleFunc("GET /api/v1/public/organizations/{slug}/events", ch.GetPublicOrganizationEvents)
	mux.HandleFunc("GET /api/v1/public/organizations/{slug}/events/{eventSlug}", ch.GetPublicEvent)
}

func registerAuthRoutes(mux *http.ServeMux, app *App) {
	h := app.IdentityHandler
	mux.HandleFunc("POST /api/v1/auth/otp/request", h.RequestOTP)
	mux.HandleFunc("POST /api/v1/auth/otp/verify", h.VerifyOTP)
	// The second Proof of Email Ownership, and the same destination: this mints
	// the identical Staff Session the passcode above does, on an email Google
	// vouched for instead of one a passcode did, and the app runs the same
	// auth-fork afterwards (ADR 0011). The exchange uses the staff OAuth client,
	// so a code obtained on the Storefront is not redeemable here.
	mux.HandleFunc("POST /api/v1/auth/google/verify", h.VerifyGoogle)
	mux.HandleFunc("GET /api/v1/auth/session", h.GetSession)
	mux.HandleFunc("POST /api/v1/auth/logout", h.Logout)
}

func registerStaffRoutes(mux *http.ServeMux, app *App) {
	h := app.IdentityHandler
	ch := app.CatalogHandler
	sh := app.SalesHandler
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

	// member gates a route to any active Member of the organization, regardless of role.
	member := func(handler http.Handler) http.Handler {
		return identitymiddleware.SessionAuth(svc)(
			identitymiddleware.LoadActiveMember(svc)(handler),
		)
	}

	// canManageEventSales gates Sale Import (and future selling) actions. Today
	// this resolves to Org Admin; when Event assignments land (V7), widen this one
	// definition to also admit Event Owner and assigned Event Staff — callers need
	// no change.
	canManageEventSales := orgAdmin

	mux.Handle("GET /api/v1/staff/organization", orgAdmin(http.HandlerFunc(h.GetOrganization)))
	mux.Handle("PATCH /api/v1/staff/organization", orgAdmin(http.HandlerFunc(h.UpdateOrganization)))
	mux.Handle("DELETE /api/v1/staff/organization", orgAdmin(http.HandlerFunc(h.DeleteOrganization)))
	mux.Handle("POST /api/v1/staff/organization/logo-upload-url", orgAdmin(http.HandlerFunc(h.CreateLogoUploadURL)))

	mux.Handle("GET /api/v1/staff/members", orgAdmin(http.HandlerFunc(h.ListMembers)))
	mux.Handle("POST /api/v1/staff/members", orgAdmin(http.HandlerFunc(h.AddMember)))
	mux.Handle("PATCH /api/v1/staff/members/{memberID}", orgAdmin(http.HandlerFunc(h.UpdateMember)))
	mux.Handle("DELETE /api/v1/staff/members/{memberID}", orgAdmin(http.HandlerFunc(h.RemoveMember)))

	mux.Handle("GET /api/v1/staff/events", member(http.HandlerFunc(ch.ListEvents)))
	mux.Handle("POST /api/v1/staff/events", orgAdmin(http.HandlerFunc(ch.CreateEvent)))
	mux.Handle("GET /api/v1/staff/events/{id}", member(http.HandlerFunc(ch.GetEvent)))
	mux.Handle("PATCH /api/v1/staff/events/{id}", orgAdmin(http.HandlerFunc(ch.UpdateEvent)))
	mux.Handle("PUT /api/v1/staff/events/{id}/discoverable", member(http.HandlerFunc(ch.SetEventDiscoverable)))
	mux.Handle("DELETE /api/v1/staff/events/{id}", orgAdmin(http.HandlerFunc(ch.DeleteEvent)))
	mux.Handle("POST /api/v1/staff/events/{id}/publish", orgAdmin(http.HandlerFunc(ch.PublishEvent)))
	mux.Handle("POST /api/v1/staff/events/{id}/cancel", orgAdmin(http.HandlerFunc(ch.CancelEvent)))
	mux.Handle("POST /api/v1/staff/events/{id}/cover-upload-url", orgAdmin(http.HandlerFunc(ch.CreateCoverUploadURL)))
	mux.Handle("GET /api/v1/staff/events/{id}/ticket-types", orgAdmin(http.HandlerFunc(ch.ListTicketTypes)))
	mux.Handle("POST /api/v1/staff/events/{id}/ticket-types", orgAdmin(http.HandlerFunc(ch.CreateTicketType)))
	mux.Handle("PATCH /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}", orgAdmin(http.HandlerFunc(ch.UpdateTicketType)))
	mux.Handle("DELETE /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}", orgAdmin(http.HandlerFunc(ch.DeleteTicketType)))
	mux.Handle("GET /api/v1/staff/tags", member(http.HandlerFunc(ch.SearchTags)))
	mux.Handle("GET /api/v1/staff/tags/popular", member(http.HandlerFunc(ch.ListPopularTags)))
	mux.Handle("GET /api/v1/staff/events/{id}/tags", member(http.HandlerFunc(ch.ListEventTags)))
	mux.Handle("PUT /api/v1/staff/events/{id}/tags", orgAdmin(http.HandlerFunc(ch.SetEventTags)))
	// Sale Import (Direct source).
	mux.Handle("GET /api/v1/staff/events/{id}/sale-imports/template", canManageEventSales(http.HandlerFunc(sh.DownloadSaleImportTemplate)))
	mux.Handle("POST /api/v1/staff/events/{id}/sale-imports/preview", canManageEventSales(http.HandlerFunc(sh.PreviewSaleImport)))
	mux.Handle("POST /api/v1/staff/events/{id}/sale-imports", canManageEventSales(http.HandlerFunc(sh.CommitDirectSaleImport)))
	mux.Handle("GET /api/v1/staff/events/{id}/sale-imports", canManageEventSales(http.HandlerFunc(sh.ListSaleImports)))
	mux.Handle("POST /api/v1/staff/events/{id}/sale-imports/{batchId}/undo", canManageEventSales(http.HandlerFunc(sh.UndoSaleImport)))
	// The Sales list is readable by any Member of the Event (Org Admin, Event
	// Owner, Event Staff), unlike the owner-only Sale Import tool above.
	mux.Handle("GET /api/v1/staff/events/{id}/sales", member(http.HandlerFunc(sh.ListSales)))

	mux.Handle("GET /api/v1/staff/events/{eventID}/assignments", orgAdmin(http.HandlerFunc(h.ListEventAssignments)))
	mux.Handle("PUT /api/v1/staff/events/{eventID}/assignments/{memberID}", orgAdmin(http.HandlerFunc(h.UpsertEventAssignment)))
	mux.Handle("DELETE /api/v1/staff/events/{eventID}/assignments/{memberID}", orgAdmin(http.HandlerFunc(h.RemoveEventAssignment)))
}
