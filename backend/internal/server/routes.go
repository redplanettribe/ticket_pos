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
	registerInternalRoutes(mux, app)
}

// registerInternalRoutes wires the namespace nothing outside the deployment
// calls: work the platform drives for itself, on a schedule or by hand during an
// incident.
//
// It is a fifth namespace beside staff, operator, customer and public, and it is
// the first one whose gate is not in this process. Every route below is
// authenticated exactly as service-to-service calls already are: the API runs
// --no-allow-unauthenticated, so Cloud Run IAM requires a Google-signed OIDC ID
// token and rejects everyone else before the container is reached (ADR 0008).
// That is why there is no middleware here — not an omission, and not a gap to be
// plugged with a shared secret. There is no application credential that grants
// this: a Customer Session and a staff token authenticate nothing on these
// routes because nothing on them reads either.
//
// THE TOKEN ARRIVES IN `Authorization`, which is worth knowing before the first
// 403 rather than during it. The frontends present theirs in
// `X-Serverless-Authorization` so as not to displace the end-user session token
// (ADR 0008); Cloud Scheduler cannot set that header for an OIDC token and uses
// `Authorization`. Cloud Run accepts either, so both work — but an operator
// reproducing the cron's call with curl, or reading a rejected request, is
// looking at `Authorization`. It displaces nothing here because these routes
// read no session at all.
//
// What a route in here must therefore be is safe for its caller to invoke at any
// time, with no arguments worth trusting. Nothing here takes a body or a path
// parameter, so a caller cannot aim it: WHAT is worked on is a property of the
// queue in the database, never of the request.
func registerInternalRoutes(mux *http.ServeMux, app *App) {
	// The Reversal Reconciler's tick (ADR 0024). Cloud Scheduler calls it (#159)
	// and a Platform Operator can curl it, which is the order this shipped in:
	// the runbook before the automation.
	//
	// Served by the SALES handler, like the Customer's own undo above it: this is
	// the same Reversal Request being pursued by a different actor, through the
	// same reversal primitive and the same per-sale lock.
	mux.HandleFunc("POST /api/v1/internal/reversals/drain", app.SalesHandler.DrainReversalRequests)
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
	// The lookup a support thread starts: one Ticket Sale by its Sale
	// Confirmation reference, across every Organization. It is deliberately not
	// nested under an Organization — the reference is all the operator has, and
	// which Organization's sale it is is one of the answers (#124).
	mux.Handle("GET /api/v1/operator/sales/{confirmationRef}", operator(http.HandlerFunc(h.LookUpSale)))
	// The Operator Reversal: recording that the operator refunded a buyer
	// off-platform, keyed on the same reference the lookup takes because the
	// action hangs off that lookup and the operator has nothing else (#125).
	mux.Handle("POST /api/v1/operator/sales/{confirmationRef}/reverse", operator(http.HandlerFunc(h.ReverseSale)))
	// Recording a Payout, which used to mean an INSERT typed by hand into the
	// production database (ADR 0015).
	mux.Handle("POST /api/v1/operator/organizations/{orgID}/payouts", operator(http.HandlerFunc(h.RecordPayout)))
	// Who is waiting to be paid: the second cross-Organization view on this
	// surface, and not nested under an Organization for the same reason the sale
	// lookup is not — the request is the reason to open the dashboard, and which
	// Organization it belongs to is one of the answers (#176, ADR 0026).
	mux.Handle("GET /api/v1/operator/payout-requests", operator(http.HandlerFunc(h.ListPayoutRequests)))
	// The navigation badge. A literal path segment, which Go's router prefers
	// over the {requestID} wildcard below it, so `count` can never be read as an
	// id — and no id could be `count` anyway, since ids are UUIDs.
	mux.Handle("GET /api/v1/operator/payout-requests/count", operator(http.HandlerFunc(h.CountPendingPayoutRequests)))
	// The one endpoint on the platform that returns a whole account number, one
	// request at a time (#176).
	mux.Handle("GET /api/v1/operator/payout-requests/{requestID}", operator(http.HandlerFunc(h.GetPayoutRequest)))
	// Answering the ask (#177). Both hang off the request rather than off the
	// Organization, because the request is what is being answered — the Payout
	// fulfilment produces is an ordinary Payout against the Organization, but the
	// Organization is not what the operator named to get here.
	//
	// Fulfilment records the Payout and marks the request paid in one
	// transaction, and the guarded update inside it is what makes this path
	// strictly safer than recording directly (ADR 0026).
	mux.Handle("POST /api/v1/operator/payout-requests/{requestID}/fulfil", operator(http.HandlerFunc(h.FulfilPayoutRequest)))
	mux.Handle("POST /api/v1/operator/payout-requests/{requestID}/decline", operator(http.HandlerFunc(h.DeclinePayoutRequest)))
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
	// The Storefront event page. Public, and it stays public: the optional session
	// middleware does not gate this route either — an absent, expired or garbage
	// token reads the Event as an ordinary visitor and never a 401.
	//
	// It is here for the same shape of reason as on the checkout route below. A
	// signed-in Customer is additionally told how many of each Ticket Type THEY
	// already hold, so the quantity picker can bound itself by the Purchase Limit
	// and a Ticket Type whose allowance is spent says so rather than claiming to
	// be sold out (ADR 0025, #168). The session is the only way to name whose
	// holdings those are, which is the point: no query parameter or header may
	// ever ask about an address the caller has not proven they own.
	mux.Handle("GET /api/v1/public/organizations/{slug}/events/{eventSlug}",
		customersmiddleware.OptionalCustomerSession(app.CustomersService)(http.HandlerFunc(ch.GetPublicEvent)))
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
	// The Affiliate Link click counter, reported by the Storefront while it
	// renders an Event page carrying a ref. Public and unauthenticated because
	// the caller is a page load by whoever followed the link, and shaped like the
	// checkout route above because it identifies the same thing: an Event by its
	// two Storefront slugs. It answers 202 to everything — see the handler.
	mux.HandleFunc("POST /api/v1/public/organizations/{slug}/events/{eventSlug}/affiliate-links/{code}/click",
		app.AffiliatesHandler.RecordAffiliateLinkClick)
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
	// Where that money goes: the Payout Profile (ADR 0026). It sits beside the
	// balance above and is gated identically — same handler, same Org-Admin-only
	// authority — because it answers the other half of the same question, and a
	// bank account is even less hired staff's business than a balance is. It is
	// on the sales handler and not identity's for the reason the table is in the
	// sales module: `identity` owns organizations but must not learn what a bank
	// account or a factura is.
	mux.Handle("GET /api/v1/staff/organization/payout-profile", orgAdmin(http.HandlerFunc(sh.GetOrganizationPayoutProfile)))
	mux.Handle("PUT /api/v1/staff/organization/payout-profile", orgAdmin(http.HandlerFunc(sh.UpdateOrganizationPayoutProfile)))
	// Asking to be paid: the Payout Request (#175, ADR 0026). Same handler and
	// the same Org-Admin-only gate as the two above, and the gate is the point
	// here rather than a convenience.
	//
	// INTEGRATION PARTNERS ARE EXCLUDED, deliberately and in advance. Org-wide
	// programmatic access (V8) is Org-Admin-equivalent for catalog and sales, and
	// it was never meant to include moving money to a bank account. `orgAdmin`
	// admits the `org_admin` MEMBER ROLE and nothing else, which is what excludes
	// them today because a partner credential is not a Membership at all. When
	// that credential lands, these three routes must NOT be widened to accept it
	// — nor must the Payout Profile above them.
	//
	// There is no route to edit a request, and that absence is a decision. A
	// pending request cannot be edited, only cancelled and re-asked, which is what
	// keeps "outstanding" genuinely singular and stops an operator being shown a
	// figure that changed under them.
	mux.Handle("GET /api/v1/staff/organization/payout-requests", orgAdmin(http.HandlerFunc(sh.ListPayoutRequests)))
	mux.Handle("POST /api/v1/staff/organization/payout-requests", orgAdmin(http.HandlerFunc(sh.SubmitPayoutRequest)))
	mux.Handle("POST /api/v1/staff/organization/payout-requests/{requestId}/cancel", orgAdmin(http.HandlerFunc(sh.CancelPayoutRequest)))

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
	mux.Handle("POST /api/v1/staff/events/{id}/video-upload-url", orgAdmin(http.HandlerFunc(ch.CreateVideoUploadURL)))
	mux.Handle("GET /api/v1/staff/events/{id}/ticket-types", orgAdmin(http.HandlerFunc(ch.ListTicketTypes)))
	mux.Handle("POST /api/v1/staff/events/{id}/ticket-types", orgAdmin(http.HandlerFunc(ch.CreateTicketType)))
	mux.Handle("PATCH /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}", orgAdmin(http.HandlerFunc(ch.UpdateTicketType)))
	mux.Handle("DELETE /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}", orgAdmin(http.HandlerFunc(ch.DeleteTicketType)))
	// A Ticket Type's Promotion is a price edit by another name, so it is gated
	// exactly as editing the Ticket Type is (ADR 0021).
	mux.Handle("POST /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/promotion", orgAdmin(http.HandlerFunc(ch.SetTicketTypePromotion)))
	mux.Handle("PATCH /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/promotion", orgAdmin(http.HandlerFunc(ch.UpdateTicketTypePromotion)))
	mux.Handle("DELETE /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/promotion", orgAdmin(http.HandlerFunc(ch.RemoveTicketTypePromotion)))
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

	// Affiliate Links: the Event's promotion surface. Full-access only — an
	// Event Staff hired for the door has no business minting links that credit
	// somebody with the Event's sales, so they are refused both the read and the
	// write and see no nav entry (#145).
	ah := app.AffiliatesHandler
	mux.Handle("GET /api/v1/staff/events/{id}/affiliate-links", eventOwnerOrAdmin(http.HandlerFunc(ah.ListAffiliateLinks)))
	mux.Handle("POST /api/v1/staff/events/{id}/affiliate-links", eventOwnerOrAdmin(http.HandlerFunc(ah.CreateAffiliateLink)))
	// The lifecycle: rename and the activate/deactivate toggle share one PATCH,
	// and DELETE removes a link that never did anything (#148). Same gate as the
	// two above — deciding whose link stops attributing is the same authority as
	// deciding there is a link at all.
	mux.Handle("PATCH /api/v1/staff/events/{id}/affiliate-links/{linkId}", eventOwnerOrAdmin(http.HandlerFunc(ah.UpdateAffiliateLink)))
	mux.Handle("DELETE /api/v1/staff/events/{id}/affiliate-links/{linkId}", eventOwnerOrAdmin(http.HandlerFunc(ah.DeleteAffiliateLink)))

	mux.Handle("GET /api/v1/staff/events/{eventID}/assignments", orgAdmin(http.HandlerFunc(h.ListEventAssignments)))
	mux.Handle("PUT /api/v1/staff/events/{eventID}/assignments/{memberID}", orgAdmin(http.HandlerFunc(h.UpsertEventAssignment)))
	mux.Handle("DELETE /api/v1/staff/events/{eventID}/assignments/{memberID}", orgAdmin(http.HandlerFunc(h.RemoveEventAssignment)))
}
