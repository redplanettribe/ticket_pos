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

	// The Sale Invoice Drainer's tick (#474, ADR 0060): signs, submits and
	// polls the Sale Invoices a paid House checkout owed, on the reconciler's
	// terms — a scheduler ticks it, an operator can curl it, and the
	// post-commit kick runs the same round for one Sale in the background.
	mux.HandleFunc("POST /api/v1/internal/sale-invoices/drain", app.InvoicingHandler.DrainSaleInvoices)

	// The weekly Follow Digest, in two halves (#220, ADR 0030). The split is
	// deliberate and is not an implementation detail leaking into the API: the
	// mail provider will not take a whole platform's Digests inside one request
	// (ADR 0009), so declaring the week and working through it are different
	// jobs on different cadences — one a week, and one a minute.
	//
	// Both obey this namespace's rule that a caller cannot aim a route. WHICH
	// week is enqueued comes from the clock, and WHICH Digests are sent is a
	// property of the queue; a caller who could name either could re-mail an old
	// week to every following Customer on the platform.
	mux.HandleFunc("POST /api/v1/internal/follow-digests/enqueue", app.DigestHandler.EnqueueFollowDigests)
	mux.HandleFunc("POST /api/v1/internal/follow-digests/drain", app.DigestHandler.DrainFollowDigests)

	// The Abandoned Answer Purge (#316, ADR 0044): the Answers a buyer typed into
	// a checkout that never became a sale, deleted 30 days on. Served by the SALES
	// handler because the rows are held on a Payment, which is the sales module's
	// (migration 074) — the catalog owns what a Ticket Question IS, and sales owns
	// what a checkout collected.
	//
	// It obeys this namespace's rule in the way that matters most here: the
	// caller cannot name the window. A cutoff parameter would make this route a
	// way to delete every Answer on the platform on demand, which is the same
	// shape of mistake as letting a caller name a Digest week.
	mux.HandleFunc("POST /api/v1/internal/checkout-answers/purge", app.SalesHandler.PurgeAbandonedAnswers)

	// The Answer Reminder sweep (#317, ADR 0044; #328, ADR 0046): one mail to
	// each person who can answer what a Ticket still owes and whose rationing
	// allows it — the Holder of an `accepted` Ticket, the buyer for every other
	// Ticket on the Sale. Served by the SALES handler because both messages are
	// mail, and mail about a purchase is this module's; the catalog decides who
	// is owed one AND who it is addressed to, and this module writes to them.
	//
	// It is the one route in this namespace whose effect is somebody's INBOX, so
	// the rule that a caller cannot aim a route is at its sharpest here: nothing
	// names an Event, an Organization, a Sale, a Ticket or — the parameter that
	// would matter most — a moment. WHO is written to is a property of the
	// database and the clock, and a caller able to name the clock could lift the
	// seven-day silence and mail the platform's whole outstanding backlog on
	// demand. Since #328 that backlog reaches more inboxes than buyers' alone,
	// which makes the rule stricter rather than looser.
	mux.HandleFunc("POST /api/v1/internal/answer-reminders/sweep", app.SalesHandler.SweepAnswerReminders)

	// The Assignment Reminder sweep (#362, parent #361, ADR 0051): the buyer of
	// an online Sale with Tickets still nobody's is pointed at the Sale's page.
	// The Answer Reminder's route beside it, on every one of its terms: Cloud
	// Run IAM authenticates the caller, no application middleware, no body, no
	// parameter, no moment — and the first run is the announcement to buyers
	// who bought before Ticket Assignment existed.
	mux.HandleFunc("POST /api/v1/internal/assignment-reminders/sweep", app.SalesHandler.SweepAssignmentReminders)

	// The Holder Address Purge (#331, parent #322, ADR 0046): the address a
	// buyer typed for a friend who never accepted it, taken once the Event has
	// started. Served by the CATALOG handler, unlike the purge above it, because
	// the columns are on `tickets` (migration 080) and a Ticket is the catalog's
	// — sales owns what a checkout collected, the catalog owns the Ticket and
	// everything said about it.
	//
	// It is the second route in this namespace that DELETES, and the only one
	// that deletes data belonging to somebody who never came to this platform.
	// The rule that a caller cannot aim a route is therefore at its sharpest
	// here: nothing names an Event, an Organization, a Ticket or — the parameter
	// that would matter most — a moment. WHICH addresses go is a property of the
	// database and the backend's own clock, and a caller able to name that clock
	// could take the holder address off every future Event on the platform in
	// one request.
	mux.HandleFunc("POST /api/v1/internal/holder-addresses/purge", app.CatalogHandler.PurgeUnacceptedHolderAddresses)
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
	// The Sale Re-addressing: recording the address a stranded buyer meant, so
	// the platform can mail it a Re-addressing Link (#420, ADR 0058). The
	// second lever on an Online Sale, beside Reverse and hanging off the same
	// lookup. Operator only: moving whom a paid record belongs to is a larger
	// trust grant than any Organization holds. Recording while one is pending
	// replaces it; DELETE on the same path withdraws it (#423).
	mux.Handle("POST /api/v1/operator/sales/{confirmationRef}/re-address", operator(http.HandlerFunc(h.ReAddressSale)))
	mux.Handle("DELETE /api/v1/operator/sales/{confirmationRef}/re-address", operator(http.HandlerFunc(h.WithdrawReAddressing)))
	// Recording a Payout, which used to mean an INSERT typed by hand into the
	// production database (ADR 0015).
	mux.Handle("POST /api/v1/operator/organizations/{orgID}/payouts", operator(http.HandlerFunc(h.RecordPayout)))
	// The House Organization designation (#472, ADR 0060): PUT makes the
	// Organization one the platform's own entity runs, DELETE takes it back.
	// A noun and two verbs rather than a body with a boolean, because the
	// designation is a fact the operator asserts or withdraws, not a setting
	// they edit — and it is the operator's alone; no Organization-scoped
	// route offers it.
	mux.Handle("PUT /api/v1/operator/organizations/{orgID}/house", operator(http.HandlerFunc(h.DesignateHouseOrganization)))
	mux.Handle("DELETE /api/v1/operator/organizations/{orgID}/house", operator(http.HandlerFunc(h.UndesignateHouseOrganization)))
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
	// Saying the transfer is submitted and the bank has not confirmed it (#184).
	// It is the one write on this surface that answers a request WITHOUT writing
	// to the ledger, and the path is a state name rather than a verb because the
	// action has no verb — "mark as processing" is what an operator does, and
	// #185's counterpart is /failed for the same reason.
	mux.Handle("POST /api/v1/operator/payout-requests/{requestID}/processing", operator(http.HandlerFunc(h.MarkPayoutRequestProcessing)))
	// Saying the bank sent the transfer back (#185). The SECOND write here that
	// touches no ledger row, and the only one that ENDS a request without one —
	// there is nothing to unwind, because /processing wrote nothing.
	//
	// A state name rather than a verb, matching /processing above. It is
	// deliberately a separate path from /decline and not a flag on it: a decline
	// is a judgement a person made and a failure is a bank sending money back,
	// they read differently to the organizer, and one endpoint taking a
	// discriminator is how two answers quietly become one (ADR 0026 amendment).
	mux.Handle("POST /api/v1/operator/payout-requests/{requestID}/failed", operator(http.HandlerFunc(h.MarkPayoutRequestFailed)))
	mux.Handle("POST /api/v1/operator/payout-requests/{requestID}/decline", operator(http.HandlerFunc(h.DeclinePayoutRequest)))
	// The Operator's view of an Event's Ticket Questions, and the Revocation
	// (#410, ADR 0056). Keyed on the Event and on the question rather than
	// nested under an Organization, on the terms the Payout Request routes
	// set: the question is what is being answered, and which Organization it
	// belongs to is one of the answers. The revoke path is a verb because the
	// act has one, and it can only ever take an approval away.
	mux.Handle("GET /api/v1/operator/events/{eventID}/ticket-questions", operator(http.HandlerFunc(h.ListEventTicketQuestions)))
	mux.Handle("POST /api/v1/operator/ticket-questions/{questionID}/revoke", operator(http.HandlerFunc(h.RevokeTicketQuestion)))
	// The Question Review queue and the Operator's answer (#407, ADR 0056),
	// on the Payout Request queue's shape: a cross-Organization list, its
	// count for the badge, one Review whole, and one verb that answers it.
	// The static /count path is registered beside the {reviewID} one on the
	// terms the payout routes are, and the mux prefers it.
	mux.Handle("GET /api/v1/operator/question-reviews", operator(http.HandlerFunc(h.ListQuestionReviews)))
	mux.Handle("GET /api/v1/operator/question-reviews/count", operator(http.HandlerFunc(h.CountOutstandingQuestionReviews)))
	mux.Handle("GET /api/v1/operator/question-reviews/{reviewID}", operator(http.HandlerFunc(h.GetQuestionReview)))
	mux.Handle("POST /api/v1/operator/question-reviews/{reviewID}/answer", operator(http.HandlerFunc(h.AnswerQuestionReview)))
	// GONE, DELETED IN #566: GET /api/v1/operator/customers/{email}/consent and
	// POST /api/v1/operator/customers/{email}/consent/withdrawal, which stood
	// here from #271 until the Legal Center landed.
	//
	// They were keyed on the Customer's EMAIL ADDRESS because that was all an
	// Operator holding a posted form had. The acceptance browsers' search (#565)
	// absorbed the one-step lookup from a POSTed body, and the withdrawal moved
	// onto the per-subject record below — so the platform now has NO ROUTE
	// ANYWHERE that carries a data subject's address in a request line, where it
	// would reach the reverse proxy's access log, the browser's history and the
	// Referer header of whatever the operator clicked next.
	//
	// This note stays because the routes were public and somebody will look for
	// them.

	// The Legal Center (#561, spec #556): where the platform's own agreements —
	// the Privacy Policy and the Términos y Condiciones — are written.
	//
	// Beside the Consent Withdrawal above and for the same reason. Both are about
	// the platform's relationship with the people who use it rather than about
	// any venue's business: one text, published once, accepted by Customers of
	// every Organization and by staff of every Organization. An Org Admin who
	// could edit it would be rewriting the contract other venues' buyers are held
	// to, so the operator allowlist on this namespace is the whole of the gate.
	//
	// KEYED ON THE DOCUMENT and not on an edition id, because there is exactly
	// ONE MUTABLE DRAFT per document — the id would name a thing that has no
	// identity of its own. `document` is `policy` or `terms`; anything else is
	// 404, which is also what makes the path total.
	mux.Handle("GET /api/v1/operator/legal/documents/{document}", operator(http.HandlerFunc(h.GetLegalWorkspace)))
	// PUT and not PATCH: the draft is replaced whole, so adding an artifact and
	// removing one need no verbs of their own. DELETE is the discard, and it is
	// idempotent — discarding a document with no draft is a success.
	//
	// NEITHER PUBLISHES. Nothing on this subtree changes a page a reader can see
	// or re-gates anybody; the publication is #563 and arrives as its own route.
	mux.Handle("PUT /api/v1/operator/legal/documents/{document}/draft", operator(http.HandlerFunc(h.SaveLegalDraft)))
	mux.Handle("DELETE /api/v1/operator/legal/documents/{document}/draft", operator(http.HandlerFunc(h.DiscardLegalDraft)))
	// What the operator has LOOKED AT (#562), which #563 turns into publish
	// preconditions. Both are POSTs that RECORD rather than render: the preview
	// is drawn in the browser by the same Markdown component the Storefront
	// renders, and the diff is computed there too, both over text the workspace
	// read already carried. There is deliberately NO PREVIEW ROUTE on the public
	// side — the public route resolves what is current itself and refuses to be
	// told which edition to serve, which is what keeps unpublished words
	// unpublished — and neither of these writes a consent_access_log row (#545):
	// a preview touches nobody's data, so it is a log line.
	mux.Handle("POST /api/v1/operator/legal/documents/{document}/draft/previews", operator(http.HandlerFunc(h.PreviewLegalDraftCell)))
	mux.Handle("POST /api/v1/operator/legal/documents/{document}/draft/diff-seen", operator(http.HandlerFunc(h.SeeLegalDraftDiff)))

	// The publication (#563): the one act in the Legal Center a reader can see.
	//
	// A POST TO A COLLECTION, not a verb on the draft, because a publication
	// CREATES A ROW — an edition — and the draft is only where its words came
	// from. It is off the `/draft` subtree above for the same reason: everything
	// there changes nothing anybody has read, and this changes what everybody is
	// held to.
	//
	// ONE ROUTE FOR BOTH ACTS, with `kind` in the body. A gating edition and a
	// correction differ in the label they take, whether they re-gate and whether
	// they wait a night — and in nothing about the resource, which is one
	// document's next edition either way. Two paths would let a screen offer them
	// as two equal buttons, which is exactly the rail this feature is arranged to
	// avoid: publishing an edition is the default, and the correction is a quiet
	// link beneath it.
	mux.Handle("POST /api/v1/operator/legal/documents/{document}/publications", operator(http.HandlerFunc(h.PublishLegalEdition)))

	// Cancelling a scheduled edition (#564): the night the overnight delay buys,
	// made usable. A publication that has not taken effect can be withdrawn, with
	// no reason, no delay and no approval step — undoing is always cheaper than
	// doing.
	//
	// POST .../cancel AND NOT DELETE .../publications/{version}, which would be
	// the shorter address and the wrong one: NOTHING IS DELETED. The row is
	// retained and marked so the record of what was nearly published survives and
	// its label stays spent, and a verb that told a reader the edition was gone
	// would be a lie about the one property this feature turns on. It is also the
	// house's shape for exactly this act — see the Payout Request's
	// `POST .../{requestId}/cancel` and the Event's `POST .../{id}/cancel`.
	//
	// UNDER `publications/` because that is what is being cancelled: the act, not
	// the document and not the draft.
	mux.Handle("POST /api/v1/operator/legal/documents/{document}/publications/{version}/cancel", operator(http.HandlerFunc(h.CancelLegalEdition)))

	// The two acceptance browsers (#565, ADR 0067): who owes an acceptance, in
	// the two populations that can owe one. TWO ROUTES BECAUSE THERE ARE TWO
	// SCREENS — Customers are UUID-keyed and answer for two documents, staff
	// are email-keyed, answer for one, and have a fourth state (Former) that
	// Customers cannot have — and forcing them through one path would mean a
	// payload with a nullable half.
	//
	// POSTS THAT READ, and the shape is deliberate rather than sloppy. Nothing
	// on either route creates, records or changes anything. What they must not
	// do is put a data subject's email in a URL, a query string or a referer
	// (#565), and TWO of the parameters are addresses: the search fragment, and
	// the CURSOR — paging is keyset on `email ASC`, so the cursor IS the last
	// address of the previous page, and base64 is an encoding rather than a
	// disguise. A GET here would write somebody's address into the access log
	// of every page an operator turned.
	//
	// `document` is `policy` or `terms` for customers and `terms` alone for
	// staff — there is one staff gate (§3, ADR 0066) — and anything else is
	// 404, which is what keeps both paths total.
	mux.Handle("POST /api/v1/operator/legal/acceptances/customers/{document}", operator(http.HandlerFunc(h.BrowseCustomerAcceptances)))
	mux.Handle("POST /api/v1/operator/legal/acceptances/staff/{document}", operator(http.HandlerFunc(h.BrowseStaffAcceptances)))

	// ONE PERSON'S CONSENT RECORD (#566, ADR 0067): where a browser row leads,
	// and where the Consent Withdrawal now lives.
	//
	// TWO SUBTREES BECAUSE THERE ARE TWO SCREENS, for the browsers' reason. A
	// Customer is reached by an opaque UUID; a staff person has no id at all —
	// the person key of the Staff platform is an email — so they are reached by
	// a Staff Digest, which is a URL key and a screen label and is written to no
	// row. Where one human being is both, the two records are CROSS-LINKED
	// SERVER-SIDE and never merged into a single identity the platform cannot
	// evidence.
	//
	// PLAIN GETs AND NOT POSTS-THAT-READ, unlike the browsers above, and the
	// difference is the same rule reaching a different answer: nothing in any of
	// these request lines is an address. A customer id is opaque, a digest is
	// opaque, and the history's cursor is a timestamp and a row id. The rule is
	// NO EMAIL IN A REQUEST LINE, not "no query strings".
	mux.Handle("GET /api/v1/operator/legal/customers/{customerID}", operator(http.HandlerFunc(h.GetCustomerLegalRecord)))
	// The history is ITS OWN ENDPOINT and not a cursor folded into the record
	// read, so the landing request and page 2 do not return different shapes.
	// Keyset on (captured_at DESC, id) at page size 25, `limit` a cap the caller
	// may lower and never raise.
	mux.Handle("GET /api/v1/operator/legal/customers/{customerID}/records", operator(http.HandlerFunc(h.ListCustomerConsentRecords)))
	// The one act this screen offers, moved here from
	// /operator/customers/{email}/consent/withdrawal and rekeyed on the id. A
	// noun and not a verb, because what is created is a record of a Consent
	// Withdrawal — and pointedly singular in what it can do: this path can only
	// ever take something away. `recorded_by` comes from the Staff Session and
	// never from the body.
	//
	// THERE IS NO SIBLING ROUTE THAT GRANTS, RE-GATES ONE PERSON, MANUFACTURES
	// AN ACCEPTANCE, ERASES ANYBODY, OR WITHDRAWS THE TERMS OR A POLICY
	// ACCEPTANCE, and the absences are as ruled as this presence. A contract's
	// basis is performance rather than consent; a deletion request escalates to
	// counsel rather than being automated behind a button; and a gating floor is
	// a property of a lineage, not of a person.
	mux.Handle("POST /api/v1/operator/legal/customers/{customerID}/withdrawal", operator(http.HandlerFunc(h.RecordCustomerConsentWithdrawal)))
	// The staff half. Unpaged — a person holds at most one acceptance per
	// edition per capacity — and 503 on a deployment with no link secret, for
	// the staff browser's reason: with no key the digest match would resolve to
	// an arbitrary person, which is the wrong record rather than a degraded one.
	mux.Handle("GET /api/v1/operator/legal/staff/{digest}", operator(http.HandlerFunc(h.GetStaffLegalRecord)))
	// The Consent Evidence Pack (#568): one deterministic ZIP, generated on
	// demand from the record above and NEVER STORED, so the platform does not
	// accumulate a second copy of its most sensitive data. Two routes and one
	// file — a pack spans both populations for one address, so both serve
	// identical bytes for one human being, and both are keyed on the opaque
	// thing their screen is keyed on.
	//
	// THERE IS NO SUBJECT-FACING EQUIVALENT AND THERE WILL NOT BE ONE. ADR
	// 0039's precedent cuts against self-service here rather than for it: a
	// passcode buys a WITHDRAWAL, an act that only ever takes something away,
	// where a pack DISCLOSES everything the platform holds. The absence is the
	// platform's rather than a screen's, which is why it is written down beside
	// the mux.
	mux.Handle("GET /api/v1/operator/legal/customers/{customerID}/evidence-pack", operator(http.HandlerFunc(h.DownloadCustomerEvidencePack)))
	mux.Handle("GET /api/v1/operator/legal/staff/{digest}/evidence-pack", operator(http.HandlerFunc(h.DownloadStaffEvidencePack)))

	// THE CONSENT ACCESS LOG (#569, ADR 0067): the platform's record of its own
	// reads of people's data, and the last surface of the Legal Center.
	//
	// It exists because every other act above leaves a domain row to hang
	// attribution off and A READ LEAVES NOTHING. Four acts write to it — a page
	// of either browser, one record opened, one pack handed over, and THIS READ
	// — so that a touch of somebody's data is recorded however it is reached.
	// A withdrawal, a preview and a publication write nothing to it: each is
	// already evidenced by a row that says more, and keeping this table to
	// touches of people's data is what makes it readable.
	//
	// A GET WITH A QUERY STRING and not a POST-that-reads, because nothing in
	// the request line is a data subject: the actor is a platform operator, the
	// act is a vocabulary word, the dates are dates, and the cursor is a
	// timestamp and a row id.
	//
	// ONE ROUTE, AND THE MISSING ONES ARE THE DESIGN. There is no purge, no
	// retention window, no export and NO SUBJECT FILTER — an audit log
	// searchable by the person it is about would be a second way to look people
	// up, keyed on the record of people being looked up.
	mux.Handle("GET /api/v1/operator/legal/access-log", operator(http.HandlerFunc(h.GetLegalAccessLog)))

	// Tax invoicing (#450, ADR 0059): the platform's Issuer with each country's
	// Tax Authority, and — from #454 — the Tax Invoices it issues by hand. The
	// platform is the sole Issuer, so this is operator-only by construction and
	// no Member route exists. The country code in the path is the visible seam:
	// a second country is a second adapter behind /issuers/{country}, never a
	// column on this one.
	inv := app.InvoicingHandler
	mux.Handle("GET /api/v1/operator/invoicing/issuers/ec", operator(http.HandlerFunc(inv.GetEcuadorIssuer)))
	mux.Handle("PUT /api/v1/operator/invoicing/issuers/ec", operator(http.HandlerFunc(inv.PutEcuadorIssuer)))
	// The signing certificate goes THROUGH the API as multipart (#453): the
	// object storage buckets are public, so a presigned upload is not an option
	// for a private key.
	mux.Handle("POST /api/v1/operator/invoicing/issuers/ec/certificate", operator(http.HandlerFunc(inv.PostEcuadorIssuerCertificate)))
	// The Tax Invoices (#454): issued from the form, listed newest first, read
	// one at a time. Issue is synchronous within a budget — the response is
	// the invoice as it stands when the authority answered or the budget ran
	// out. The totals preview is the same arithmetic the document carries,
	// served so the form never does it itself.
	mux.Handle("GET /api/v1/operator/invoicing/invoices", operator(http.HandlerFunc(inv.ListInvoices)))
	mux.Handle("POST /api/v1/operator/invoicing/invoices", operator(http.HandlerFunc(inv.IssueInvoice)))
	mux.Handle("POST /api/v1/operator/invoicing/invoices/totals", operator(http.HandlerFunc(inv.PreviewTotals)))
	mux.Handle("GET /api/v1/operator/invoicing/invoices/{id}", operator(http.HandlerFunc(inv.GetInvoice)))
	// Check status and Resend (#455): on a non-authorized invoice, ask the
	// authority again, or send the same document under the same clave.
	mux.Handle("POST /api/v1/operator/invoicing/invoices/{id}/check", operator(http.HandlerFunc(inv.CheckInvoice)))
	mux.Handle("POST /api/v1/operator/invoicing/invoices/{id}/resend", operator(http.HandlerFunc(inv.ResendInvoice)))
	// Mark annulled (#477): the operator's record of a manual portal act,
	// allowed from pending or needs_attention, irreversible.
	mux.Handle("POST /api/v1/operator/invoicing/invoices/{id}/annul", operator(http.HandlerFunc(inv.AnnulInvoice)))
	// Abandon (#578, ADR 0068): the operator's record that the SRI never
	// took the document and never will, because it refuses the number it
	// carries. Any kind, from the three states a refusal leaves, and only
	// with a fresh Check status behind it. Terminal and irreversible.
	mux.Handle("POST /api/v1/operator/invoicing/invoices/{id}/abandon", operator(http.HandlerFunc(inv.AbandonInvoice)))
	// The Sale Invoice Reissue (#483, ADR 0061): a Credit Note and a
	// corrected Sale Invoice owed in one act against an authorized factura
	// whose Recipient is wrong. Operator-only; 404 while Sale Invoicing is
	// closed.
	mux.Handle("POST /api/v1/operator/invoicing/invoices/{id}/reissue", operator(http.HandlerFunc(inv.ReissueInvoice)))
	// Issue again (#580, ADR 0068): a fresh Sale Invoice owed to a Ticket
	// Sale whose document is terminally dead — abandoned or annulled — with
	// the original lines, amounts and Recipient, linked to the one it
	// replaces through the reissue's own chain. No Credit Note and no
	// special signing route: the Drainer signs it under a fresh secuencial
	// on a later round. Operator-only; 404 while Sale Invoicing is closed.
	mux.Handle("POST /api/v1/operator/invoicing/invoices/{id}/issue-again", operator(http.HandlerFunc(inv.IssueInvoiceAgain)))
	// The documents that need an operator (#477): the Operator Dashboard's
	// queue, longest waiting first, and its count. Read-only, on the
	// invoicing module because the documents are its own; the dashboard's
	// other queues live on the operator module because it composes theirs.
	mux.Handle("GET /api/v1/operator/invoicing/needs-attention", operator(http.HandlerFunc(inv.ListNeedsAttention)))
	mux.Handle("GET /api/v1/operator/invoicing/needs-attention/count", operator(http.HandlerFunc(inv.CountNeedsAttention)))
	// The Recipient Warnings (#482, ADR 0061): the dashboard's count beside
	// the needs_attention one; the documents themselves are the invoicing
	// list under its recipient_warning filter.
	mux.Handle("GET /api/v1/operator/invoicing/recipient-warnings/count", operator(http.HandlerFunc(inv.CountRecipientWarnings)))
	// The Uninvoiced House Sales (#507, ADR 0064): the backlog a Sale
	// Invoice Backfill works from, oldest sale first, and its count. The
	// first operator surface listing Ticket Sales — read-only, House-scoped,
	// and 404 while Sale Invoicing is closed.
	mux.Handle("GET /api/v1/operator/invoicing/uninvoiced-sales", operator(http.HandlerFunc(inv.ListUninvoicedHouseSales)))
	mux.Handle("GET /api/v1/operator/invoicing/uninvoiced-sales/count", operator(http.HandlerFunc(inv.CountUninvoicedHouseSales)))
	// The Sale Invoice Backfill (#508, ADR 0064): the act that owes each
	// selected Uninvoiced House Sale an ordinary Sale Invoice, dated today,
	// one transaction per sale, then one Drainer kick.
	mux.Handle("POST /api/v1/operator/invoicing/uninvoiced-sales/backfill", operator(http.HandlerFunc(inv.BackfillSaleInvoices)))
	// The documents handed over (#456): the signed XML in every status, the
	// SRI's authorization XML only once authorized, and the RIDE (#494, ADR
	// 0062), rendered on each request and likewise only once authorized.
	// Files, not envelopes.
	mux.Handle("GET /api/v1/operator/invoicing/invoices/{id}/xml", operator(http.HandlerFunc(inv.DownloadSignedXML)))
	mux.Handle("GET /api/v1/operator/invoicing/invoices/{id}/authorization-xml", operator(http.HandlerFunc(inv.DownloadAuthorizationXML)))
	mux.Handle("GET /api/v1/operator/invoicing/invoices/{id}/ride", operator(http.HandlerFunc(inv.DownloadRIDE)))
	// The Tax Document Archive (#630, spec #629): every authorized production
	// document emitted in a range of Emission Dates, streamed as one ZIP of
	// signed XML for the accountant.
	mux.Handle("GET /api/v1/operator/invoicing/archive", operator(http.HandlerFunc(inv.DownloadTaxDocumentArchive)))
	// Its summary (#631): what that archive would hold, before downloading.
	mux.Handle("GET /api/v1/operator/invoicing/archive/summary", operator(http.HandlerFunc(inv.SummarizeTaxDocumentArchive)))
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
	// The far side of the consent gate (#251, parent #249). Both doors above can
	// answer with a consent-required outcome instead of a session, and this is
	// the only route that turns one back into a session: it records the Consent
	// Record and mints the credential the verify withheld. Unauthenticated for
	// the same reason they are — the pending-consent token IS the credential —
	// and it names no email, so it cannot be pointed at anybody but the address
	// the token was minted for.
	mux.HandleFunc("POST /api/v1/customer/auth/consent", h.SubmitConsent)
	// The Confirmation Link's token is itself the credential, so this route is
	// unauthenticated too. It reads Authorization when present, but only to
	// notice that the caller already holds something wider than a link.
	mux.HandleFunc("POST /api/v1/customer/auth/confirmation-link", h.RedeemConfirmationLink)
	// Unsubscribing from the Follow Digest (#224, ADR 0030). Unauthenticated, and
	// it is the only write in this namespace that is: a Digest is read in a mail
	// client months after anybody last signed in, so an opt-out behind a passcode
	// would be no opt-out. The signed token in the link is the whole authority,
	// it names one Customer, and all it can do is set one reversible flag.
	//
	// POST AND ONLY POST, which is the point. The link in the Digest points at a
	// Storefront page that confirms with this request; mail security scanners
	// prefetch every link in every message, and a GET that acted would let one
	// silence everybody it protects. Registering the method alone is what makes
	// the router answer a bare GET of this path with 405 rather than with an act.
	mux.HandleFunc("POST /api/v1/customer/unsubscribe", h.Unsubscribe)
	// Confirming a Pending Confirmation from the link in a Sale Confirmation
	// (#255, ADR 0035) — the unsubscribe route's mirror image, and the second
	// unauthenticated write in this namespace. A guest checkout created a Customer
	// nobody had ever signed in as, so the owner of an address somebody else typed
	// may have no account to sign in to; the signed token is the whole authority,
	// and pressing a link that only ever travelled to that inbox is itself the
	// proof of ownership the guest's tick lacked.
	//
	// NOTHING PRODUCES A PENDING CONFIRMATION ANY MORE (ADR 0054, #386), and this
	// route stays anyway. The links are already in people's inboxes and the rows
	// they resolve are already recorded; retiring the resolver would strand them,
	// and resolving them by inference is exactly what ADR 0035 refuses. It is a
	// route with no new work and an unfinished backlog, which is the correct shape
	// for a decision that superseded a producer and not its state.
	//
	// POST AND ONLY POST, and here the scanner argument is sharper than it is
	// above. A prefetch of an unsubscribe GET would silence somebody's mail; a
	// prefetch of a confirmation GET would GRANT a marketing opt-in nobody ever
	// confirmed, and leave an evidence row saying an inbox confirmed itself when
	// what confirmed it was a robot. Registering the method alone is what makes
	// the router answer a bare GET with 405 rather than with an act.
	mux.HandleFunc("POST /api/v1/customer/consent/confirm", h.ConfirmConsent)
	// The Consent Withdrawal surface's passcode door (#270, parent #265,
	// ADR 0039). Unauthenticated like the two above, and for a reason of its own:
	// the person it exists for is the one who has just been told that to withdraw
	// their consent they must first accept a policy, and demanding the session
	// that demand comes from would be the same refusal wearing a hat.
	//
	// IT MINTS NO CUSTOMER SESSION. It redeems a passcode for the same
	// short-lived, single-use pending-consent token a held sign-in returns, and
	// that token is spent at the consent submission route above — where a
	// submission whose every answer is a denial needs no Policy Acceptance and one
	// containing any grant still does. A passcode intercepted on this route
	// therefore buys the ability to switch somebody's consent OFF, which its owner
	// can switch back on from their own account, and nothing else.
	mux.HandleFunc("POST /api/v1/customer/consent/withdrawal/passcode/verify", h.ProveEmailForConsentWithdrawal)

	// signedIn gates a route on a valid Customer Session and extends its sliding
	// window. Everything behind it is scoped to the Customer on that session.
	signedIn := customersmiddleware.RequireCustomerSession(svc)

	mux.Handle("GET /api/v1/customer/auth/session", signedIn(http.HandlerFunc(h.GetSession)))
	mux.Handle("POST /api/v1/customer/auth/logout", signedIn(http.HandlerFunc(h.Logout)))
	// Beginning an online checkout, session-gated and with no address on the
	// request (ADR 0054, #384/#386). Served by the SALES handler under this
	// namespace for the same reason the undo below is: the credential is a
	// Customer Session, which is this namespace's business, while the operation is
	// a Payment and a Ticket Sale, which are the sales module's.
	//
	// THE NAMESPACE IS THE STATEMENT, and since #386 it is the ONLY statement:
	// this is the one route in the platform that begins an online checkout, it has
	// no `customer_email`, and it reads the address from the session. The public
	// begin-checkout that would sell to whatever address was typed into it is
	// gone — not refused, deleted — so addressing an online Ticket Sale to an
	// unproven inbox is unrepresentable rather than merely disallowed.
	//
	// It sits behind the same gate as everything else here and behind one more the
	// middleware cannot express: the handler refuses a Confirmation Link session,
	// because a token that travelled inside a receipt is not Proof of Email
	// Ownership and must not be able to buy.
	//
	// Confirm is deliberately NOT moved. It stays public and idempotent under
	// /public/checkout/{clientTransactionId}/confirm: it is the Payment Provider's
	// return leg and has to work for a browser that came back having lost
	// everything, including its session.
	mux.Handle("POST /api/v1/customer/organizations/{slug}/events/{eventSlug}/checkout",
		signedIn(http.HandlerFunc(app.SalesHandler.BeginCustomerCheckout)))
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
	// The buyer's own Tickets and their assignment state (#315, ADR 0044;
	// narrowed by #344, ADR 0049). Served by the CATALOG handler under this
	// namespace, exactly as the undo above is served by the sales one and for
	// the same reason: a Ticket and whose it is belong to the catalog, while
	// who is asking belongs here.
	//
	// A READ ONLY, SINCE ADR 0049. The sale-scoped answer write that sat beside
	// it is gone: an Answer is given only by a Ticket's Holder, through the
	// held-ticket routes below, or by Event Staff. This list carries no Answer,
	// no outstanding count and no Answer Link for any row — of a Ticket the
	// buyer does not hold they see its position, its Ticket Type and its
	// assignment state, and nothing else.
	//
	// BEHIND THE SAME GATE AS THE READ ABOVE AND NOT BEHIND ONE MORE. A
	// Confirmation Link session may use it, unlike the undo beside it: the
	// session narrows to its one Sale inside the service, so a link session
	// asking about any other Ticket Sale is answered as if it did not exist.
	mux.Handle("GET /api/v1/customer/ticket-sales/{ticketSaleId}/tickets",
		signedIn(http.HandlerFunc(app.CatalogHandler.ListBuyerTicketAnswers)))
	// The Sale's Tax Documents and their download (#475, ADR 0060). Served by
	// the INVOICING handler under this namespace, as the undo is served by
	// the sales one: the credential is a Customer Session, the document is
	// the invoicing module's. Behind the same gate and not one more — a
	// Confirmation Link session is the buyer of the one Sale it names, and
	// a forwarded receipt is exactly where "where is my factura" is asked
	// from. The service narrows it to that Sale; another Customer's Sale
	// lists nothing and downloads INVOICE_NOT_FOUND, never a different word.
	mux.Handle("GET /api/v1/customer/ticket-sales/{ticketSaleId}/tax-documents",
		signedIn(http.HandlerFunc(app.InvoicingHandler.ListCustomerSaleDocuments)))
	mux.Handle("GET /api/v1/customer/ticket-sales/{ticketSaleId}/tax-documents/{id}/xml",
		signedIn(http.HandlerFunc(app.InvoicingHandler.DownloadCustomerSaleDocumentXML)))
	// The RIDE beside the XML (#497, ADR 0062): the same gate, the same
	// narrowing, the same one refusal, rendered on request from the same row.
	mux.Handle("GET /api/v1/customer/ticket-sales/{ticketSaleId}/tax-documents/{id}/ride",
		signedIn(http.HandlerFunc(app.InvoicingHandler.DownloadCustomerSaleDocumentRIDE)))
	// The Tickets the Customer HOLDS, and the one write on them (#343, ADR
	// 0049): the buyer's Self-held Ticket and every Ticket they accepted by
	// Assignment Link, through one route, keyed on the holder customer id of
	// the Ticket row and on no Sale at all. Behind the same gate as the buyer's
	// routes above, for the same reason: a Confirmation Link session is the
	// buyer of the Sale it names, and answering a t-shirt size is the thing
	// the person holding a forwarded receipt most likely opened it to do.
	mux.Handle("GET /api/v1/customer/held-tickets",
		signedIn(http.HandlerFunc(app.CatalogHandler.ListHeldTickets)))
	mux.Handle("PUT /api/v1/customer/held-tickets/{ticketId}/answers/{questionId}",
		signedIn(http.HandlerFunc(app.CatalogHandler.AnswerHeldTicketQuestion)))
	// The buyer assigns one of their own Tickets to an email address (#324,
	// parent #322). Registered here rather than under a namespace of its own
	// because it is the same surface as the two routes above it — the
	// Confirmation Link page and the Customer Area, which are one page — and
	// because a Ticket Assignment is a fact about a TICKET, which is the
	// catalog's, while who is asking is this namespace's.
	//
	// NOT A ROUTE OF ITS OWN PER VERB. Assigning, reassigning and correcting a
	// mistyped address are one statement — "this Ticket's Holder address is now
	// X" — so they are one PUT. A second `reassign` route would be a second
	// place to remember that a change of address CLEARS THAT TICKET'S ANSWERS,
	// and the first place it would be forgotten; an Answer is a fact about a
	// person and must never be inherited by a new Holder.
	//
	// BEHIND THE SAME GATE AS ITS NEIGHBOURS AND NOT BEHIND ONE MORE. A
	// Confirmation Link session may assign, exactly as it may answer, and for
	// the reason stated above: this feature exists so that the person holding
	// the receipt can distribute the tickets they bought, and demanding a
	// passcode of the buyer would put the platform's own distribution route
	// behind a stricter door than the forwarded Answer Link it replaces. What it
	// still may not do is undo the purchase. The session narrows to its one Sale
	// inside the service, so a link session naming any other Ticket Sale is
	// answered as if it did not exist.
	//
	// BEHIND ITS OWN FLAG, WHICH SHIPS CLOSED. TICKET_ASSIGNMENT_ENABLED is
	// separate from TICKET_QUESTIONS_ENABLED (ADR 0045 governs both): the
	// service reads it before it reads anything else and answers 404, so this
	// path behaves exactly as an unrouted one until a Policy Version describes
	// the platform storing an address a buyer supplied for somebody else. NO
	// MAIL IS SENT from here — the Assignment mail and the Holder's accept flow
	// are #325, which is also what makes the `accepted` state reachable.
	mux.Handle("PUT /api/v1/customer/ticket-sales/{ticketSaleId}/tickets/{ticketId}/assignment",
		signedIn(http.HandlerFunc(app.CatalogHandler.AssignOwnTicket)))
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
	// The Follows (#217). One listing endpoint for everything the Customer
	// Follows, and a follow/unfollow pair per kind of thing that can be followed
	// — Organizations today, Tags next (#218), joining the SAME list rather than
	// adding a second one.
	//
	// All three draw the same full-session line the profile writes do, and the
	// read is gated with them. A Confirmation Link session is a forwarded email:
	// it is not authority to subscribe that inbox to mail, nor to read back what
	// its owner has subscribed to. That narrowing is the service's rather than
	// this middleware's, exactly as for the profile PATCH.
	mux.Handle("GET /api/v1/customer/follows", signedIn(http.HandlerFunc(h.ListFollows)))
	// Suggested Follows (#231, ADR 0031): what the Customer does NOT Follow,
	// ranked by Activity. Behind the same middleware as the listing above and
	// narrowed to a full session in the same place, because what a person is
	// suggested is derived from what they Follow and is as private as it.
	//
	// A SIXTH ROUTE rather than a field on the listing. That listing is called by
	// the explorer and by every Event and Organization page to resolve their
	// Follow controls, so a ranking query folded into it would run on every
	// render of the hot public surfaces and be thrown away.
	mux.Handle("GET /api/v1/customer/follow-suggestions", signedIn(http.HandlerFunc(h.ListFollowSuggestions)))
	// The Follow Digest switch, as the Customer Area writes it (#224). It sits
	// beside the Follows rather than under them because it is not about any one
	// of them: it decides whether the weekly mail is sent at all, and every
	// Follow stands whichever way it is set. The listing above publishes the same
	// fact so that one read answers "what do I follow" and "am I being written
	// to" together.
	//
	// This is the entry point that requires a session, and the only one that can
	// turn the Digest back ON — see service.SetDigestEnabled for why the
	// unsubscribe link deliberately cannot.
	mux.Handle("PUT /api/v1/customer/digest", signedIn(http.HandlerFunc(h.SetDigestEnabled)))
	// The Privacy page (#268, parent #265): what this Customer has authorized,
	// and a control per optional consent that moves it in EITHER direction.
	//
	// THE READ AND THE WRITES ARE DIFFERENT ROUTES so that rendering the page
	// cannot record anything. Only a PUT below writes a Consent Record; a GET
	// changes nothing, which is what stops "somebody read the page" from
	// becoming "somebody refused" in the evidence log.
	//
	// ONE PURPOSE PER REQUEST, in the path. Moving a control is one act about
	// one consent, and a route that could answer both at once would let the page
	// perform a Withdraw All — a deliberate act, behind a dialog that says what
	// it means, writing a single row (#269) — without anybody being told what
	// they were doing.
	//
	// Both draw the same full-session line as the Digest toggle above: a
	// Confirmation Link session is a forwarded receipt, and it is authority
	// neither to read somebody's standing privacy settings nor to change them.
	mux.Handle("GET /api/v1/customer/privacy", signedIn(http.HandlerFunc(h.Privacy)))
	mux.Handle("PUT /api/v1/customer/privacy/consents/{purpose}", signedIn(http.HandlerFunc(h.SetOptionalConsent)))
	// Withdraw All (#269): every optional consent taken back at once, ONE Consent
	// Record with both denied.
	//
	// A ROUTE OF ITS OWN, and the separation is the ticket rather than an
	// arrangement of URLs. The per-purpose route above cannot express this and
	// must not learn to: two of its requests would leave the same Customer in the
	// same state while writing evidence that says somebody moved two controls,
	// where what happened was one person asking to be left alone — and the log
	// cannot recover that intent afterwards from two rows and a shared timestamp.
	//
	// It is outside `consents/` because it names no consent: "everything" is not
	// a purpose, and a third value in that path segment would be the first step
	// back towards one endpoint that can do both.
	//
	// It takes NO BODY, so nothing a caller sends can turn it into a grant.
	mux.Handle("POST /api/v1/customer/privacy/withdraw-all", signedIn(http.HandlerFunc(h.WithdrawAll)))
	mux.Handle("POST /api/v1/customer/follows/organizations/{slug}", signedIn(http.HandlerFunc(h.FollowOrganization)))
	mux.Handle("DELETE /api/v1/customer/follows/organizations/{slug}", signedIn(http.HandlerFunc(h.UnfollowOrganization)))
	// Tag Follows (#218) join the same listing above rather than adding one of
	// their own. The Tag is named by its canonical key, as the Organization is
	// named by its slug.
	mux.Handle("POST /api/v1/customer/follows/tags/{canonicalKey}", signedIn(http.HandlerFunc(h.FollowTag)))
	mux.Handle("DELETE /api/v1/customer/follows/tags/{canonicalKey}", signedIn(http.HandlerFunc(h.UnfollowTag)))
}

func registerPublicRoutes(mux *http.ServeMux, app *App) {
	h := app.IdentityHandler
	ch := app.CatalogHandler
	sh := app.SalesHandler
	// The Privacy Policy (#250, parent #249). Public because a privacy notice
	// that only a signed-up person could read would be the wrong way round, and
	// it discloses nothing about anybody — the same bytes for every caller.
	//
	// THE ONLY LOCALIZED ROUTE IN THE API, and the exception is narrow and
	// argued: ADR 0027 keeps the API Locale-unaware because words this product
	// chooses for concepts the database owns belong in the Storefront catalog.
	// This is not copy but evidence — the text a Policy Version's SHA-256 is
	// taken over — so it has to be served from the same place it is hashed, or
	// the hash proves nothing. See internal/consent/policy's package doc.
	//
	// The Locale is in the PATH rather than a query parameter or a header,
	// because it names WHICH DOCUMENT this is rather than how to present one:
	// the Spanish policy and the English policy are two texts, each with its own
	// address, and each cacheable at that address by anything in front of this.
	mux.HandleFunc("GET /api/v1/public/privacy-policy/{locale}", app.ConsentHandler.GetPrivacyPolicy)

	// The Términos y Condiciones, beside the policy and shaped like it — but
	// answered with the Spanish document whatever the {locale} says: the Terms
	// are published in Spanish only and the Spanish text legally prevails
	// (§37, ADR 0066), so no locale 404s here. See the handler.
	mux.HandleFunc("GET /api/v1/public/terms/{locale}", app.ConsentHandler.GetTerms)
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
	// THERE IS NO PUBLIC BEGIN-CHECKOUT (ADR 0054, #386). Beginning an online
	// checkout is a session-gated Customer route and nothing else; the guest
	// checkout that used to live here — a POST taking whatever `customer_email`
	// was typed into it — was deleted rather than deprecated, because a route that
	// merely refuses the mistake still describes it. A stale client posting here
	// gets a 404 from the mux, which is the correct answer: this is not a door
	// that is locked, it is a door that is not there.
	//
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
	// The Event Page View counter (ADR 0057), reported by the Storefront on
	// EVERY render of an Event page, carrying the Affiliate Link code the
	// visitor arrived through when there was one — this one route is the whole
	// write path for both the hourly buckets and the lifetime click counter.
	// Public and unauthenticated because the caller is a page load by whoever
	// reached the page, and shaped like the checkout route above because it
	// identifies the same thing: an Event by its two Storefront slugs. It
	// answers 202 to everything — see the handler.
	mux.HandleFunc("POST /api/v1/public/organizations/{slug}/events/{eventSlug}/page-views",
		app.AffiliatesHandler.RecordEventPageView)
	// The Registration Link hand-off counter (#210), reported by the Storefront
	// redirect route a Customer passes through on their way to the registration
	// site. Public and unauthenticated, and shaped like the two routes above
	// because it names the same thing by the same two Storefront slugs. It takes
	// NO destination: the Event owns where it sends people, and a target this
	// route accepted would be an open redirect wearing the platform's own domain.
	// It answers 202 to everything — see the handler.
	mux.HandleFunc("POST /api/v1/public/organizations/{slug}/events/{eventSlug}/registration-link/click",
		app.CatalogHandler.RecordRegistrationClick)

	// The Answer Link (#312, ADR 0044): a signed, stateless, per-Ticket link
	// opening ONE Ticket's Ticket Questions and nothing else, for the buyer to
	// pass to whoever will hold that ticket.
	//
	// PUBLIC AND UNAUTHENTICATED, and unlike every other public route here that
	// is a deliberate refusal rather than an absence of anything to protect.
	// These two carry a credential — the signed token — and still take no
	// session, mint none, and create no Customer. ADR 0044: requiring proof of
	// identity would mean collecting an address from somebody who never came to
	// this platform, which is the third-party collection problem the whole
	// feature was shaped to avoid.
	//
	// NOTE WHAT SITS UNDER /public HERE AND WHAT DOES NOT. The Confirmation Link
	// redeems under /customer because it mints a Customer Session; this one is
	// under /public because it mints nothing. The namespace is the honest
	// statement of what holding this link makes somebody: nothing.
	//
	// THE TOKEN IS IN THE BODY ON BOTH VERBS, the read included, so it never
	// reaches an access log or a Referer header. That is why the read is a POST —
	// see the handler, which defends the choice at length. Neither route takes an
	// id in its address: the token names the Ticket, and a Ticket id in the path
	// would be a second, unsigned way to say which Ticket this is.

	// The Assignment Link's three routes (#325, parent #322, ADR 0046): accept,
	// give the Holder's name, answer a Ticket Question as the Holder.
	//
	// UNDER /public LIKE THE ANSWER LINK'S PAIR, AND UNLIKE THEM THESE MINT A
	// PERSON. The click is Proof of Email Ownership (ADR 0035), so the first call
	// creates or matches a Verified Customer — and still mints no session, which
	// is why they are not under /customer: nothing here signs anybody in.
	//
	// A DIFFERENT TOKEN FROM THE ONE ABOVE, and that is the security property of
	// the feature rather than a routing detail. The Answer Link is copyable off
	// the buyer's own sale page; if these routes accepted one, a buyer could
	// accept on their friend's behalf and the Verified Customer minted from it
	// would be a fiction. The two are separately keyed, so a token of one kind
	// fails cryptographically on the other's routes.
	//
	// NO TICKET ID IN ANY PATH, on all three: the token names the Ticket, and a
	// path segment naming it too would be a second, unsigned way to say which.
	// The token is in the BODY on every verb, the accept included, so it reaches
	// no access log and no Referer header.
	mux.HandleFunc("POST /api/v1/public/assignment-link", app.CatalogHandler.AcceptAssignmentLink)
	mux.HandleFunc("PUT /api/v1/public/assignment-link/name", app.CatalogHandler.NameByAssignmentLink)
	mux.HandleFunc("PUT /api/v1/public/assignment-link/questions/{questionId}", app.CatalogHandler.AnswerByAssignmentLink)

	// The Re-addressing Link (#421, ADR 0058): the Assignment Link's twin for a
	// whole purchase. GET opens it without accepting — the token in the query,
	// as the mailed link carries it — and POST accepts, the token in the body.
	// Its own token under its own key, so an Assignment Link fails here
	// cryptographically. UNLIKE the Assignment Link the accept SIGNS THE READER
	// IN: the Sale is now theirs, and the response is the passcode's own
	// sign-in outcome.
	mux.HandleFunc("GET /api/v1/public/re-addressing-link", app.SalesHandler.ViewReAddressingLink)
	mux.HandleFunc("POST /api/v1/public/re-addressing-link", app.SalesHandler.AcceptReAddressingLink)
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
	// The terms step both doors above can be held at (#538, ADR 0066): spends
	// the pending-terms token a gated verify returned and mints the Staff
	// Session the sign-in withheld. Unauthenticated because the token IS the
	// credential, exactly as the customer consent submission is.
	mux.HandleFunc("POST /api/v1/auth/terms/accept", h.AcceptTerms)
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
	// The Staff Locale write, and note what it is NOT wrapped in: no
	// LoadActiveMember and no role gate. The language belongs to the person, not
	// to the Organization they are looking at, so a Platform Operator who is a
	// Member of nothing must be able to set one and an Event Staff member must
	// not need an Org Admin to do it for them. A Staff Session is the whole
	// requirement, which is why it registers bare like the memberships routes
	// above rather than behind the staff middleware chain.
	mux.HandleFunc("PUT /api/v1/staff/me/locale", h.SetStaffLocale)

	// The Terms gate on a LIVE session (#570, ADR 0067): the read a navigation
	// interstitial makes, and the acceptance it records. A Staff Session is the
	// whole requirement — no LoadActiveMember and no role gate, for the locale
	// route's reason: the contract binds the PERSON, and a Platform Operator
	// who is a Member of nothing owes it exactly as an Org Admin does.
	//
	// They are `/api/` routes serving a navigation gate, and never a gate on
	// `/api/` themselves: nothing here refuses another request, and no other
	// staff route consults the Terms. A sale in progress commits.
	mux.Handle(
		"GET /api/v1/staff/terms/gate",
		identitymiddleware.SessionAuth(svc)(http.HandlerFunc(h.GetStaffTermsGate)),
	)
	mux.Handle(
		"POST /api/v1/staff/terms/accept",
		identitymiddleware.SessionAuth(svc)(http.HandlerFunc(h.AcceptStaffTermsOnSession)),
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
	// A Ticket Type's Ticket Questions (#309, ADR 0045). Gated exactly as editing
	// the Ticket Type they belong to is, for the reason its Promotion is: a
	// Ticket Question is part of defining the Ticket Type, not a separate thing
	// with a separate audience, and a surface that decided its own gate would be
	// a second answer to a question `orgAdmin` already answers here.
	//
	// THESE ROUTES ARE REGISTERED WHETHER OR NOT THE FEATURE FLAG IS ON, and
	// answer 404 while it is off. The flag lives in the catalog service rather
	// than in this file deliberately: a route that exists only under a condition
	// is a shape no test can exercise both sides of, and the property this
	// feature has to prove — that with the flag off nothing differs from today —
	// needs both sides reachable in one process. See ADR 0045 and
	// catalog.ErrTicketQuestionsUnavailable for why the refusal is a 404.
	mux.Handle("GET /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions", orgAdmin(http.HandlerFunc(ch.ListTicketQuestions)))
	mux.Handle("POST /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions", orgAdmin(http.HandlerFunc(ch.CreateTicketQuestion)))
	// The reorder registers before the {questionId} routes so that "order" is
	// read as the verb it is rather than as somebody's question id.
	mux.Handle("PUT /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/order", orgAdmin(http.HandlerFunc(ch.ReorderTicketQuestions)))
	mux.Handle("PATCH /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId}", orgAdmin(http.HandlerFunc(ch.UpdateTicketQuestion)))
	// DELETE retires and never deletes, on both of these: what has been answered
	// keeps reading on its Ticket and in the Sales Export.
	mux.Handle("DELETE /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId}", orgAdmin(http.HandlerFunc(ch.RetireTicketQuestion)))
	mux.Handle("POST /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId}/options", orgAdmin(http.HandlerFunc(ch.AddTicketQuestionOption)))
	mux.Handle("PATCH /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId}/options/{optionId}", orgAdmin(http.HandlerFunc(ch.RenameTicketQuestionOption)))
	mux.Handle("DELETE /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId}/options/{optionId}", orgAdmin(http.HandlerFunc(ch.RetireTicketQuestionOption)))
	// The Question Review (#406, ADR 0056): the Event's drafts submitted as one
	// ask to the Platform Operator, and taken back while outstanding. Gated
	// `eventOwnerOrAdmin` and not `orgAdmin` like the authoring routes above,
	// because ADR 0056 names both as the people who may ask — and Event Staff
	// are refused, which is the "by Event Staff" refusal the ticket asks for.
	// The 404-while-dark rule is the catalog service's, as it is for every
	// question route.
	mux.Handle("POST /api/v1/staff/events/{id}/question-reviews", eventOwnerOrAdmin(http.HandlerFunc(ch.SubmitQuestionReview)))
	mux.Handle("GET /api/v1/staff/events/{id}/question-reviews/current", eventOwnerOrAdmin(http.HandlerFunc(ch.GetCurrentQuestionReview)))
	mux.Handle("POST /api/v1/staff/events/{id}/question-reviews/{reviewId}/withdraw", eventOwnerOrAdmin(http.HandlerFunc(ch.WithdrawQuestionReview)))
	// The Answer: what one Ticket says in reply to one Ticket Question (#310).
	//
	// GATED `orgAdmin`, EXACTLY AS THE QUESTION-AUTHORING ROUTES ABOVE ARE, and
	// the choice is worth stating because the ticket is titled for Event Staff.
	// It is the same gate for two reasons. It is the one the sibling Ticket
	// Question routes already use, and a surface that decided its own would be a
	// second answer to a question already answered here. And `event_staff` is
	// refused every catalog verb today — `canManageEventSales`, which is named
	// for the door and is where this would widen to, resolves to `orgAdmin`
	// itself until Event assignments land (V7). So `orgAdmin` is what "Event
	// Staff" can be given on this platform at this moment, and widening it later
	// is one word in one place.
	//
	// THE READS ARE UNGATED BY THE EDIT WINDOW, deliberately. A reversed Ticket
	// Sale's Tickets and a started Event's Answers stay listable and readable:
	// a Sale Reversal voids a sale, it does not erase what its Tickets answered,
	// and hiding them would make it look as though it had. Only the writes are
	// refused — see catalog.AnswerWindow.
	//
	// Registered in every build and answering 404 while the flag is off, on the
	// same terms as the questions above (ADR 0045).
	//
	// The Ticket Sale's Tickets register under `ticket-sales` and not under the
	// sales handler's `/sales`, because what this returns is a Ticket and its
	// questions rather than anything about the sale — the Ticket Sale is only
	// how staff reach a Ticket at all.
	mux.Handle("GET /api/v1/staff/events/{id}/ticket-sales/{ticketSaleId}/tickets", orgAdmin(http.HandlerFunc(ch.ListTicketSaleAnswers)))
	mux.Handle("GET /api/v1/staff/events/{id}/tickets/{ticketId}", orgAdmin(http.HandlerFunc(ch.GetTicketAnswers)))
	// PUT and not POST: there is exactly one Answer per (Ticket, question) and
	// the address names it, so answering and correcting are the same request
	// with a different body.
	mux.Handle("PUT /api/v1/staff/events/{id}/tickets/{ticketId}/answers/{questionId}", orgAdmin(http.HandlerFunc(ch.AnswerTicketQuestion)))
	// The one real DELETE in this feature, and not an exception to "retired,
	// never deleted": an Answer points at nothing, so removing one restores the
	// state the Ticket was in before anybody answered.
	mux.Handle("DELETE /api/v1/staff/events/{id}/tickets/{ticketId}/answers/{questionId}", orgAdmin(http.HandlerFunc(ch.RemoveTicketAnswer)))
	// The Holder List (#333; the Outstanding Answers list of #313, widened to
	// the roster): every Ticket of this Event, who is coming on each, and —
	// where the Event asks Ticket Questions — what each still owes.
	//
	// HUNG OFF THE EVENT and not off a Ticket Sale, because that is the question
	// being asked. The per-sale route above is how staff reach ONE Ticket when
	// somebody rings up about their order; this is the Organization looking at
	// the whole Event before it orders the shirts, and the two cannot be the
	// same address because they are aggregated over different things.
	//
	// NAMED AFTER THE LIST AND NOT AFTER ONE OF ITS FILTERS (#519, ADR 0065).
	// The read was built as Outstanding Answers (#313) and kept that name
	// through #333, which turned Outstanding Answers into `outstanding=true` —
	// a filter of the roster rather than its definition. ADR 0065 makes it one
	// filter of seven and hangs a Holder Export off the same path, at which
	// point an endpoint called `outstanding-answers` would be serving a file
	// that contradicts it. Renamed now because there is exactly one caller
	// chain and no public or partner route reaches it; it will never be
	// cheaper.
	//
	// A READ WITH NO STATE BEHIND IT. There is no outstanding_answers table:
	// the debt is derived on every request from what the Ticket Type asks and
	// what the Ticket has said, which is exactly why the debts clear by
	// themselves as Answers arrive from any of the three routes and why a Sale
	// Reversal drops a whole sale out of the roster without anything having to
	// sweep.
	//
	// OPEN TO AN EVENT OWNER, NOT ONLY AN ORG ADMIN (#521, ADR 0065). This is a
	// DELIBERATE WIDENING OF ACCESS TO PERSONAL DATA — the platform's densest
	// concentration of it — made here and recorded in the ADR rather than left
	// to be discovered in an audit.
	//
	// It takes `eventOwnerOrAdmin`, the same gate the Sales Export below
	// carries, and that is the whole argument: the Sales Export's per-Ticket
	// sheet ALREADY emits this Event's assignment states and its accepted
	// Holders' names and addresses to an Event Owner. So the `orgAdmin` gate
	// that stood here held nothing in — an Event Owner could not open the
	// roster and could already download its contents. That gate was never a
	// judgement about roster data either; it was inherited from the Ticket
	// Questions routes above, which this list grew out of. One rule for holder
	// data, matching the glossary's "an Event Owner is equivalent in scope to
	// an Org Admin within that Event".
	//
	// WHAT IT STILL REFUSES, deliberately: Event Staff, who are Members of the
	// Event and are refused this read exactly as before. They work the door;
	// they get the Sales list and no roster, and that line does not move. If
	// this is ever widened to `member`, ADR 0065 is what has to be reopened.
	//
	// Its 404-while-dark is its own: the service answers only when EITHER
	// TICKET_ASSIGNMENT_ENABLED or TICKET_QUESTIONS_ENABLED is open (#333),
	// because an Organization that assigns tickets and asks nothing still has a
	// Holder List.
	holderList := eventOwnerOrAdmin(http.HandlerFunc(ch.ListHolderList))
	mux.Handle("GET /api/v1/staff/events/{id}/holder-list", holderList)
	// THE OLD PATH, ALIASED FOR ONE RELEASE, THEN DELETED (#519 → #531).
	// The staff app and the API deploy separately, so there is a window in
	// which a new backend serves an old frontend — and with CI and Deploy
	// refused for billing since 2026-08-28, a bad deploy ordering is caught by
	// nobody. The alias is the width of that window and nothing more.
	//
	// THE SAME HANDLER VALUE and not a second registration of the same
	// function, so the alias cannot drift: there is one gate, one handler and
	// one behaviour, and the only difference between the two addresses is the
	// address. That is why #521's widening to `eventOwnerOrAdmin` needed no
	// edit here and cannot have been applied to one address and not the other —
	// re-registering `ch.ListHolderList` below instead of reusing this value
	// would give the alias a gate of its own to fall out of step with, which is
	// exactly the drift the shared value exists to make impossible. It is
	// deliberately NOT documented in the OpenAPI spec — a
	// generated client that learns this path would outlive the shim it exists
	// to cover, and every caller that matters is already being moved to the
	// new one. Delete this line, not the one above it (#531).
	mux.Handle("GET /api/v1/staff/events/{id}/outstanding-answers", holderList)
	// THE HOLDER EXPORT (#529, ADR 0065): the same roster, under the same
	// filters, handed over as an .xlsx.
	//
	// THE SAME GATE AS THE READ ABOVE, and that is deliberate rather than
	// incidental: `eventOwnerOrAdmin` is what the Holder List read carries, what
	// the Sales Export beside it carries, and what ADR 0065 settles for holder
	// data on both surfaces. A gate on the page with a wider one on the download
	// of it — or the reverse — is the arrangement #521 was opened to end. EVENT
	// STAFF ARE REFUSED here exactly as they are on the read; they work the door,
	// and this file is the platform's densest concentration of attendee personal
	// data in a form that is forwarded and kept.
	//
	// It is registered SEPARATELY rather than sharing the handler value above,
	// because it is a different handler doing a different thing — a file, not a
	// page — and the shared-value trick up there exists to keep an ALIAS of one
	// route from drifting, which this is not. What must not drift is the GATE, and
	// that is one call to the same middleware standing two lines apart.
	mux.Handle("GET /api/v1/staff/events/{id}/holder-list/export", eventOwnerOrAdmin(http.HandlerFunc(ch.ExportHolderList)))
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
	// Recording ONE Ticket Sale by hand, and the live verdict on it (#368,
	// ADR 0052). The method split gives the gate split for free: the same
	// collection is READ by every Member of the Event and WRITTEN only by
	// whoever can manage its sales — the existing rule that the Sales list is
	// open to Event Staff while the tool that records sales is not.
	//
	// Not on the Sale Import route above, and deliberately: that one returns a
	// batch id this act has no value for, and names a resource it does not
	// create. A Manually Recorded Sale belongs to no batch at all.
	mux.Handle("POST /api/v1/staff/events/{id}/sales", canManageEventSales(http.HandlerFunc(sh.RecordManualSale)))
	mux.Handle("POST /api/v1/staff/events/{id}/sales/preview", canManageEventSales(http.HandlerFunc(sh.PreviewManualSale)))
	// Reversing ONE imported Ticket Sale from its row (#350, ADR 0050). The
	// same gate as the Sale Import and its undo, because it is the same lever
	// pointed at one row: Event Staff see the reversed state on the list above
	// and are refused here.
	mux.Handle("POST /api/v1/staff/events/{id}/sales/{saleId}/reverse", canManageEventSales(http.HandlerFunc(sh.ReverseSale)))
	mux.Handle("POST /api/v1/staff/events/{id}/sales/{saleId}/correct", canManageEventSales(http.HandlerFunc(sh.CorrectSale)))
	mux.Handle("POST /api/v1/staff/events/{id}/sales/{saleId}/correct/preview", canManageEventSales(http.HandlerFunc(sh.PreviewSaleCorrection)))
	// The Event's money, though, is not for hired door staff: the Net Proceeds
	// strip above that list is Org Admin and Event Owner only.
	mux.Handle("GET /api/v1/staff/events/{id}/sales/summary", eventOwnerOrAdmin(http.HandlerFunc(sh.GetSalesSummary)))
	// The Sales Export takes the summary's guard rather than the list's, though
	// it carries the list's own rows: a file is forwarded, retained, and outlives
	// an Event assignment in a way a paginated screen is not, and this one
	// concentrates every buyer's email and Tax ID for the Event (#236).
	mux.Handle("GET /api/v1/staff/events/{id}/sales/export", eventOwnerOrAdmin(http.HandlerFunc(sh.ExportSales)))
	// Sales Trends takes the same guard, and for the plainest reason of the
	// three: it IS the Event's money, drawn day by day (#275). Door staff hired
	// for the evening have no business reading the shape of the Event's takings,
	// and the staff app hides the tab from them rather than offering something
	// that would refuse them.
	mux.Handle("GET /api/v1/staff/events/{id}/sales/trends", eventOwnerOrAdmin(http.HandlerFunc(sh.GetSalesTrends)))

	// Affiliate Links: the Event's promotion surface. Full-access only — an
	// Event Staff hired for the door has no business minting links that credit
	// somebody with the Event's sales, so they are refused both the read and the
	// write and see no nav entry (#145).
	ah := app.AffiliatesHandler
	mux.Handle("GET /api/v1/staff/events/{id}/affiliate-links", eventOwnerOrAdmin(http.HandlerFunc(ah.ListAffiliateLinks)))
	// The trends payload behind the tab's graphs (#413, ADR 0057). Same guard as
	// everything else here: the graphs draw the Event's money as well as its
	// traffic, and neither is for hired door staff.
	mux.Handle("GET /api/v1/staff/events/{id}/affiliate-links/trends", eventOwnerOrAdmin(http.HandlerFunc(ah.GetAffiliateTrends)))
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
