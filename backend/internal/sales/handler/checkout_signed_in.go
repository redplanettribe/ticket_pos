package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/customers"
	customersmiddleware "github.com/peter/ticket_pos/backend/internal/customers/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The session-gated begin-checkout (ADR 0054): the online checkout that reads
// the buyer's address from their Customer Session and offers no field by which
// another can be named.
//
// It is the ONLY way to begin an online checkout. It arrived beside a public
// begin-checkout, as the expand half of an expand–contract, but #386 deleted
// that one: there is no second door, and anyone auditing this guarantee should
// find nothing when they go looking for the legacy route this comment used to
// point at. If a public begin-checkout ever appears beside this again, the
// guarantee is off by default for anyone who knows the URL.
//
// Why the enforcement is here at all, rather than in the Storefront: the BFF is
// a hop, not a boundary (ADR 0008). A wall drawn only in the dialog would leave
// this API a guest-checkout endpoint accepting any typed address, and the
// property would be a UI convention that the next stale client quietly drops.

// beginCustomerCheckoutBody is the session-gated begin-checkout's request, and
// the only type any online checkout request is decoded into. It names no
// address.
//
// That absence is the load-bearing part of ADR 0054, and the reason this type
// exists rather than the internal beginCheckoutBody being reused with its
// address field ignored. A field a handler ignores is still a field a client can
// send, still a field that appears in the generated API client, and still a
// mistake somebody will eventually express. There is no address here to get
// wrong.
//
// beginCheckoutBody still carries a CustomerEmail, but it is `json:"-"` and so
// is not part of any wire: it survives as the internal description of an online
// checkout, and the only thing that ever fills it is addressedTo, from the
// session. A reader checking this guarantee should confirm that remains true.
//
// Every remaining field means exactly what it means on the public body, and the
// documentation for each lives there (beginCheckoutBody) so the two cannot
// describe the same wire differently.
type beginCustomerCheckoutBody struct {
	CustomerFirstName   string `json:"customer_first_name"`
	CustomerLastName    string `json:"customer_last_name"`
	CustomerTaxIDType   string `json:"customer_tax_id_type"`
	CustomerTaxIDNumber string `json:"customer_tax_id_number"`
	CustomerPhone       string `json:"customer_phone"`

	AffiliateCodes []string           `json:"affiliate_codes"`
	Lines          []checkoutLineBody `json:"lines"`
	Locale         string             `json:"locale"`

	// The consent boxes, POINTERS with a load-bearing nil exactly as on the
	// public body — absent means the box was not shown, which is a different fact
	// from `false`.
	//
	// On this route the absences are the ordinary case rather than the exception:
	// a Customer meets the boxes at sign-in now, so the dialog behind a session
	// draws only what that Customer has not answered, and a Customer who has
	// answered everything sends none of these at all. Which boxes they were owed
	// is still the SERVICE's finding and never this body's assertion.
	PolicyAcceptance  *bool `json:"policy_acceptance"`
	MarketingConsent  *bool `json:"marketing_consent"`
	NetworkingConsent *bool `json:"networking_consent"`
	// TermsAcceptance is the Términos y Condiciones box (#537, ADR 0066),
	// under the same nil discipline: it is drawn only for a Customer whose
	// live session spans the current Terms edition — the one-time re-gate, or
	// any later bump — and the API refuses the checkout without it exactly as
	// it does the policy's (TERMS_ACCEPTANCE_REQUIRED).
	TermsAcceptance *bool `json:"terms_acceptance"`
	// AdulthoodDeclaration is the 18+ box beside it (#588, ADR 0069), drawn
	// wherever the Terms box is AND the edition in effect carries the
	// `label-adulthood-declaration` Artifact — so on this route, as on the
	// public one, it is absent from every body sent under an edition that does
	// not ask. Owed and not present-and-true is refused 400
	// ADULTHOOD_DECLARATION_REQUIRED with no Payment created and nothing
	// written at all.
	AdulthoodDeclaration *bool `json:"adulthood_declaration"`

	// UpgradeElected is the Upgrade Prompt's checkbox, under the same rules as
	// on the public body it converges into (ADR 0074, #649/#650): a plain bool whose
	// absence means keep both, never a field error, and never trusted — the
	// commit re-evaluates eligibility in its own transaction and ignores an
	// election it did not offer.
	//
	// It is asked only of a SIGNED-IN buyer, which is what makes the prompt
	// possible at all: the free Ticket being surrendered is one this very
	// Customer holds, and the Event page publishes the count for that Customer
	// alone (#648).
	UpgradeElected bool `json:"upgrade_elected"`

	Answers []checkoutAnswerBody `json:"answers"`
}

// addressedTo turns this body into the public one by supplying the address the
// session proved.
//
// The two bodies converge here rather than in the service, so that ONE validator
// and ONE service input describe an online checkout however it arrived. The
// address is the only difference between them, and it is the only thing this
// method adds — a caller cannot reach the public body's email field except
// through this argument.
func (b beginCustomerCheckoutBody) addressedTo(email string) beginCheckoutBody {
	return beginCheckoutBody{
		CustomerEmail:        email,
		CustomerFirstName:    b.CustomerFirstName,
		CustomerLastName:     b.CustomerLastName,
		CustomerTaxIDType:    b.CustomerTaxIDType,
		CustomerTaxIDNumber:  b.CustomerTaxIDNumber,
		CustomerPhone:        b.CustomerPhone,
		AffiliateCodes:       b.AffiliateCodes,
		Lines:                b.Lines,
		Locale:               b.Locale,
		PolicyAcceptance:     b.PolicyAcceptance,
		MarketingConsent:     b.MarketingConsent,
		NetworkingConsent:    b.NetworkingConsent,
		TermsAcceptance:      b.TermsAcceptance,
		AdulthoodDeclaration: b.AdulthoodDeclaration,
		UpgradeElected:       b.UpgradeElected,
		Answers:              b.Answers,
	}
}

// BeginCustomerCheckout starts an online checkout for the signed-in Customer:
// same validation, same service, same result as the public begin-checkout, with
// the buyer's address taken from their Customer Session instead of from the
// request.
//
// @Summary      Begin an online checkout as the signed-in Customer
// @Description  The session-gated begin-checkout (ADR 0054). It does the same work as the public begin-checkout — validates ticket types, quantities, remaining capacity and each Ticket Type's Purchase Limit, snapshots current unit prices into a Payment, and returns our client transaction id with how the checkout was left — and differs from it in exactly one way: THE BUYER'S EMAIL IS READ FROM THE CUSTOMER SESSION AND IS NOT A REQUEST FIELD. There is no `customer_email` on this body and nothing here reads one, so a Ticket Sale begun on this route can only ever be addressed to an address the platform has proof of ownership for. A request with no Customer Session is refused 401. A request carrying a CONFIRMATION LINK session is refused 403 CUSTOMER_SESSION_SCOPE_INSUFFICIENT: that credential is minted from a token which travelled in an email and may have been forwarded, so it is not Proof of Email Ownership and cannot buy. A checkout with money to collect comes back status "pending" with the Payment Provider's redirect_url; a checkout whose cart totals zero — Free Ticket Types only — is settled here and now, comes back status "approved" with confirmation_ref and no redirect_url, and is gated by the session identically, because a free Ticket is still a Ticket that needs a reachable inbox (ADR 0017). Because the buyer is proven, the details they give here are their own assertion about themselves: first and last name, the Tax ID (required, ADR 0016) and the optional phone are written back onto the Customer as well as snapshotted onto the sale. Consent given here is recorded as ANSWERED and never as a Pending Confirmation, and which boxes the buyer was owed is recomputed server-side from the Customer on the session and never taken from this body — a Customer who has already accepted the current Policy Version and answered both optional boxes sends no consent fields at all, is owed nothing, writes no Consent Record, and is not asked again; one who still owes Policy Acceptance must send it present and true or the checkout is refused 400 POLICY_ACCEPTANCE_REQUIRED with no Payment created. The Terms box behaves identically where owed (#537, ADR 0066): a Customer whose live session spans the current Terms edition — the one-time re-gate, or a later bump — must send terms_acceptance present and true or the checkout is refused 400 TERMS_ACCEPTANCE_REQUIRED, and the answer is held on the Payment together with the edition it was answered about, so the Consent Record written at commit evidences the text the buyer was shown rather than whichever edition is current when the provider answers. The Adulthood Declaration rides that same box (#588, ADR 0069): where the Terms edition in effect publishes the `label-adulthood-declaration` Artifact, the same buyer must also send adulthood_declaration present and true, or the checkout is refused 400 ADULTHOOD_DECLARATION_REQUIRED with no Payment created and nothing whatever written down — the platform keeps no record of anybody who says they are a minor. It is a declaration and never a verification: no date of birth is collected anywhere, and eighteen is a number in prose inside the Artifact. Where it is owed and ticked the answer is held on the Payment beside the Terms answer and the edition it was declared under, and reaches the Consent Record at commit by the same road. An edition that does not carry the Artifact owes no declaration and draws no box. An answer for a box the buyer was not owed is dropped rather than applied. A Ticket Type whose Sales Cutoff has passed is refused 409 TICKET_TYPE_CLOSED naming it in `details.ticket_type_id`, and that is deliberately NOT the sold-out code: a shut window and an exhausted stock are worded differently on the Storefront, so a Ticket Type that is both closed and exhausted answers TICKET_TYPE_CLOSED rather than CAPACITY_EXCEEDED (ADR 0070). The cutoff is judged once, here, and the Sales Cutoff binds this route alone — a Sale Import row, a Manually Recorded Sale and a Sale Correction's replacement are never refused by it — so a Payment already under way settles on the terms it started on and nothing is re-judged on the way back from the Payment Provider. Purchase Limits, Affiliate Link attribution, `locale`, and the skippable `answers` section all behave exactly as they do on the public route, including that nothing about an answer can ever refuse or delay a checkout (ADR 0044). The technical proof stored with a consent record (IP, user agent, origin URL) is taken from the request and never from this body, and the Policy Version accepted is resolved server-side. The response REPORTS THE ADDRESS THE SALE WAS ADDRESSED TO in `addressed_to` (#387): the address read off the session, echoed back so the caller learns it from this API rather than inferring it from a browser. That is what lets the Storefront's return leg still recognise a buyer whose Customer Session did not survive the trip to the Payment Provider — a cleared cookie jar, a provider webview, a revoked session, a different browser — and it grants nothing, because signing in still costs a passcode or a Google round trip. Confirming this checkout uses the same public confirm route, which stays public and idempotent: it is the Payment Provider's return leg and must work for a browser that has lost everything, which is the same fact `addressed_to` exists to survive.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        slug       path      string                     true  "Organization slug"
// @Param        eventSlug  path      string                     true  "Event slug"
// @Param        body       body      beginCustomerCheckoutBody  true  "Checkout lines, the buyer's name, Tax ID and optional phone — no email"
// @Success      201        {object}  openapi.EnvelopeBeginCheckout
// @Failure      400        {object}  platform.Envelope
// @Failure      401        {object}  platform.Envelope
// @Failure      403        {object}  platform.Envelope
// @Failure      404        {object}  platform.Envelope
// @Failure      409        {object}  platform.Envelope
// @Router       /api/v1/customer/organizations/{slug}/events/{eventSlug}/checkout [post]
func (h *Handler) BeginCustomerCheckout(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	// The buyer comes from the session the middleware validated, and from nowhere
	// else. Its absence here would mean the route was wired without that
	// middleware, which must fail closed rather than sell somebody tickets under
	// an address nobody proved.
	session, ok := customersmiddleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	// A Confirmation Link session may READ the one sale it names and may not buy.
	// It is minted from a token that travelled inside a Sale Confirmation, so
	// holding it says somebody opened a receipt and nothing about who owns the
	// inbox it arrived in — and this whole route exists to make "the address is
	// proven" true of every Sale it records. Refused with the same code the
	// profile edit and the reversal use, because it is the same fact: this
	// credential is too narrow and a wider one is what would help.
	if session.TicketSaleID != "" {
		_ = platform.WriteDomainError(w, reqID, customers.ErrCheckoutRequiresFullSession())
		return
	}

	orgSlug := strings.TrimSpace(r.PathValue("slug"))
	eventSlug := strings.TrimSpace(r.PathValue("eventSlug"))
	if orgSlug == "" || eventSlug == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "slug", Code: platform.CodeRequired, Message: "organization and event slug are required"},
		})
		return
	}

	var body beginCustomerCheckoutBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	// The address is supplied HERE, from the session, and the same validator the
	// public route uses then sees an ordinary, well-formed checkout. Its
	// customer_email branch cannot fail on this path — a session's email is an
	// address the platform itself stored — and that is the point: one validator,
	// one service input, one set of rules about a cart.
	//
	// The same address comes back out in the result's addressed_to, because the
	// Storefront's return leg needs it and this is the only party that can state
	// it authoritatively (#387). A browser that reported its own would be exactly
	// the field ADR 0054 deleted, wearing a different name and pointing the other
	// way down the wire.
	fields, input := validateBeginCheckout(orgSlug, eventSlug, body.addressedTo(session.Email))
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	// COMPUTED, NOT ASSUMED, and this is a deliberate refusal to simplify.
	//
	// selfAssertedCheckout asks the same question here that it asks on the public
	// route — does this request carry a full Customer Session for the very address
	// being bought under? — and on this route the answer is always yes, because
	// the address came from that session and a narrow one was turned away above.
	// The constant is a CONSEQUENCE of the gate rather than a shortcut past it.
	//
	// SelfAsserted still varies on the staff channels and on every historical row,
	// where it means "we checked", so a hardcoded true here would leave the column
	// with two different meanings depending on which handler wrote it. It decides
	// what may be written back onto the Customer's profile and which consent boxes
	// the buyer was owed, and those two must never be able to disagree.
	input.SessionCustomerID, input.Customer.SelfAsserted = selfAssertedCheckout(r, input.Customer.Email)
	// The technical proof of the consent act, derived from the request and never
	// taken from the body, exactly as on the public route.
	input.ConsentEvidence = consent.EvidenceFromRequest(r)

	result, err := h.svc.BeginCheckout(r.Context(), input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, result)
}
