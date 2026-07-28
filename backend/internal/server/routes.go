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
	registerOperatorRoutes(mux, app)
	registerPublicRoutes(mux, app)
	registerCustomerRoutes(mux, app)
}

// registerOperatorRoutes wires the Platform Operator's namespace.
//
// It is a fourth namespace beside staff, customer, and public, and its gate is
// unlike any of theirs: a valid Staff Session whose email is on the platform
// operator allowlist, and nothing else. There is deliberately no
// LoadActiveMember in the chain — operator authority is orthogonal to
// Membership, so an operator who belongs to no Organization is served, and an
// Org Admin who is not on the allowlist is refused (ADR 0015).
func registerOperatorRoutes(mux *http.ServeMux, app *App) {
	h := app.OperatorHandler
	svc := app.IdentityService

	operator := func(handler http.Handler) http.Handler {
		return identitymiddleware.SessionAuth(svc)(
			identitymiddleware.RequirePlatformOperator(svc)(handler),
		)
	}

	mux.Handle("GET /api/v1/operator/summary", operator(http.HandlerFunc(h.GetSummary)))
	mux.Handle("GET /api/v1/operator/organizations", operator(http.HandlerFunc(h.ListOrganizations)))
	mux.Handle("GET /api/v1/operator/organizations/{orgID}", operator(http.HandlerFunc(h.GetOrganization)))
	// The operator surface's only write: recording a Payout, which used to mean
	// an INSERT typed by hand into the production database (ADR 0015).
	mux.Handle("POST /api/v1/operator/organizations/{orgID}/payouts", operator(http.HandlerFunc(h.RecordPayout)))
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
	// Undoing one of those sales (ADR 0018). It is served by the SALES handler
	// under this namespace: the credential is a Customer Session, which is this
	// namespace's business, but the operation is on a Ticket Sale — the reversal
	// primitive that restores capacity, the void notice and the Payment Provider
	// all live in the sales module, exactly as the Organization's payout surface
	// does above.
	//
	// It sits behind the same gate as the read, and behind one more the middleware
	// cannot express: the handler refuses a Confirmation Link session, because a
	// forwarded email is not authority to undo somebody's purchase.
	mux.Handle("POST /api/v1/customer/ticket-sales/{ticketSaleId}/reverse",
		signedIn(http.HandlerFunc(app.SalesHandler.ReverseTicketSale)))
	// The Customer Area's one write: "My info" (#102). Scoped by the session
	// like every route above it, and narrowed once more inside the service — a
	// Confirmation Link session may read its one sale but may not rewrite the
	// person's name or Tax ID. That narrowing is the handler's rather than this
	// middleware's because it is a property of this write alone.
	mux.Handle("PATCH /api/v1/customer/profile", signedIn(http.HandlerFunc(h.UpdateProfile)))
	// The Customer Avatar writes: mint an upload URL, attach the uploaded image,
	// remove it. All three draw the same full-session line as the profile PATCH,
	// and for the same reason — a forwarded Confirmation Link must not change how
	// a person is pictured.
	mux.Handle("POST /api/v1/customer/profile/avatar-upload-url", signedIn(http.HandlerFunc(h.CreateAvatarUploadURL)))
	mux.Handle("PUT /api/v1/customer/profile/avatar", signedIn(http.HandlerFunc(h.UpdateAvatar)))
	mux.Handle("DELETE /api/v1/customer/profile/avatar", signedIn(http.HandlerFunc(h.DeleteAvatar)))
}

func registerPublicRoutes(mux *http.ServeMux, app *App) {
	h := app.IdentityHandler
	ch := app.CatalogHandler
	sh := app.SalesHandler
	mux.HandleFunc("GET /api/v1/public/organizations/{slug}", h.GetPublicOrganization)
	mux.HandleFunc("GET /api/v1/public/events", ch.ListPublicEvents)
	mux.HandleFunc("GET /api/v1/public/tags", ch.ListPublicTags)
	mux.HandleFunc("GET /api/v1/public/organizations/{slug}/events", ch.GetPublicOrganizationEvents)
	mux.HandleFunc("GET /api/v1/public/organizations/{slug}/events/{eventSlug}", ch.GetPublicEvent)
	// Online checkout (ADR 0012). Guest by definition: no session is required to
	// buy tickets, only an email, a name, and a Tax ID.
	//
	// The optional session middleware does not gate this route — an absent or
	// dead token still checks out as a guest. It is here so the handler can tell
	// a Customer restating their own Tax ID from an anonymous visitor typing a
	// known email, which is the difference between an override that updates the
	// person's profile and one that only lands on the sale (ADR 0016).
	mux.Handle("POST /api/v1/public/organizations/{slug}/events/{eventSlug}/checkout",
		customersmiddleware.OptionalCustomerSession(app.CustomersService)(http.HandlerFunc(sh.BeginCheckout)))
	// Confirm is keyed by our client transaction id rather than by slugs: the
	// provider's return redirect carries the id and nothing else reliable.
	mux.HandleFunc("POST /api/v1/public/checkout/{clientTransactionId}/confirm", sh.ConfirmCheckout)
	// What the guest who just bought is allowed to know about undoing it (#121,
	// ADR 0018): whether the sale that checkout produced is on offer to be
	// reversed, and by when. Keyed by the same client transaction id confirm is,
	// for the same reason — it is the only thing a buyer with no Customer Session
	// holds.
	//
	// Served by the CUSTOMERS handler although it hangs off a checkout path,
	// because the rule it reports is the Customer Area's own: one function
	// answers this route and the Area's cards, so the deadline cannot differ
	// between the page a buyer sees before signing in and the page they see
	// after.
	//
	// It is public, and read-only, and those two facts belong together. Reversal
	// stays behind a Customer Session because a Confirmation Link travels by
	// email and gets forwarded; this route reveals a deadline and never an
	// action, so nothing here is triggerable by whoever ends up holding the URL.
	mux.HandleFunc("GET /api/v1/public/checkout/{clientTransactionId}/reversal", app.CustomersHandler.GetCheckoutReversal)
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

	// eventOwnerOrAdmin gates the Organization's financial surfaces to the
	// Members entrusted with them; Event Staff are refused.
	eventOwnerOrAdmin := func(handler http.Handler) http.Handler {
		return identitymiddleware.SessionAuth(svc)(
			identitymiddleware.LoadActiveMember(svc)(
				identitymiddleware.RequireEventOwnerOrAdmin(handler),
			),
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
	// The Organization's money: Withdrawable Balance and payout history. It lives
	// on the sales handler because the balance is a sum over Ticket Sale Lines,
	// and it is Org-Admin-only because the Organization's finances are not hired
	// staff's business (ADR 0014).
	mux.Handle("GET /api/v1/staff/organization/payouts", orgAdmin(http.HandlerFunc(sh.GetOrganizationPayouts)))

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
	// The Event's money, though, is not for hired door staff: the Net Proceeds
	// strip above that list is Org Admin and Event Owner only.
	mux.Handle("GET /api/v1/staff/events/{id}/sales/summary", eventOwnerOrAdmin(http.HandlerFunc(sh.GetSalesSummary)))

	mux.Handle("GET /api/v1/staff/events/{eventID}/assignments", orgAdmin(http.HandlerFunc(h.ListEventAssignments)))
	mux.Handle("PUT /api/v1/staff/events/{eventID}/assignments/{memberID}", orgAdmin(http.HandlerFunc(h.UpsertEventAssignment)))
	mux.Handle("DELETE /api/v1/staff/events/{eventID}/assignments/{memberID}", orgAdmin(http.HandlerFunc(h.RemoveEventAssignment)))
}
