package integration

import (
	"net/http"
	"testing"
	"time"
)

// The Platform Operator's lookup of one Ticket Sale by its Sale Confirmation
// reference (issue #124, parent #123).
//
// It is the door the Operator Reversal will hang on, and read-only until then.
// A support thread carries a reference and nothing else, so the operator pastes
// it and is shown enough to be sure they have the right sale before touching
// anybody's money: the Event, the Organization, the buyer, the amounts, the
// status, the Payment Method, and whether the Reversal Window has passed.
//
// Two properties make it what it is. It spans every Organization — the operator
// is a Member of none and the reference names the sale globally — and it exists
// for nobody but operators, gated by the same allowlist as the rest of the
// namespace (ADR 0015).

// operatorSaleEvent is the Event a looked-up sale belongs to.
type operatorSaleEvent struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Slug     string  `json:"slug"`
	StartsAt *string `json:"starts_at"`
	Timezone string  `json:"timezone"`
}

// operatorSaleCustomer is the buyer as the sale snapshotted them.
type operatorSaleCustomer struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type operatorSaleLine struct {
	TicketTypeName string `json:"ticket_type_name"`
	Quantity       int    `json:"quantity"`
}

type operatorSale struct {
	ID              string  `json:"id"`
	ConfirmationRef string  `json:"confirmation_ref"`
	Status          string  `json:"status"`
	Channel         string  `json:"channel"`
	Source          *string `json:"source"`
	PaymentMethod   *string `json:"payment_method"`
	SoldAt          string  `json:"sold_at"`
	RecordedAt      string  `json:"recorded_at"`
	ReversedAt      *string `json:"reversed_at"`
	ReversedBy      *string `json:"reversed_by"`
	// OperatorReversal is the money memo an Operator Reversal leaves (#125),
	// null on every sale reversed any other way. Its type lives in
	// operator_sale_reversal_test.go, which is the feature it belongs to.
	OperatorReversal *operatorReversalMemo `json:"operator_reversal"`

	Customer    operatorSaleCustomer `json:"customer"`
	TicketTypes []operatorSaleLine   `json:"ticket_types"`
	TicketCount int                  `json:"ticket_count"`

	Currency         string `json:"currency"`
	AmountCents      int    `json:"amount_cents"`
	PlatformFeeCents int    `json:"platform_fee_cents"`
	FeeIVACents      int    `json:"fee_iva_cents"`
	NetProceedsCents int    `json:"net_proceeds_cents"`

	Event operatorSaleEvent `json:"event"`

	ReversalWindowClosesAt *string `json:"reversal_window_closes_at"`
	ReversalWindowPassed   bool    `json:"reversal_window_passed"`
}

// operatorSaleOrganization is the Organization the sale belongs to, which is
// the fact the reference alone hides.
type operatorSaleOrganization struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Currency string `json:"currency"`
}

// operatorSaleLookup is the whole response: the sale as sales knows it, and the
// Organization it belongs to as identity knows it.
type operatorSaleLookup struct {
	Sale         operatorSale             `json:"sale"`
	Organization operatorSaleOrganization `json:"organization"`
}

func operatorSaleLookupPath(confirmationRef string) string {
	return "/api/v1/operator/sales/" + confirmationRef
}

func lookUpSale(t *testing.T, env *testEnv, sessionID, confirmationRef string) operatorSaleLookup {
	t.Helper()
	var out operatorSaleLookup
	operatorGetOK(t, env, sessionID, operatorSaleLookupPath(confirmationRef), &out)
	return out
}

// TestOperatorLooksUpSaleByConfirmationReferenceAcrossOrganizations: the sale
// belongs to an Organization the operator is not a Member of, which is the whole
// point — a reference from a support thread is enough, with no idea whose sale
// it is.
func TestOperatorLooksUpSaleByConfirmationReferenceAcrossOrganizations(t *testing.T) {
	env := setupTest(t)

	// An Organization the operator has nothing to do with, whose Org Admin is
	// somebody else entirely.
	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	eventID, ga := publishCheckoutEvent(t, env, otherSessionID, "Lookup Fest", "lookup-fest", feeTestBaseCents, 10)
	begun := beginCheckoutOK(t, env, "other-org", "lookup-fest",
		checkoutBody("bea@example.com", "Bea", "Ruiz", cartLine(ga, 2)))
	settled := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	found := lookUpSale(t, env, operatorSessionID, settled.ConfirmationRef)

	sale := found.Sale
	if sale.ConfirmationRef != settled.ConfirmationRef || sale.ID == "" {
		t.Fatalf("sale = %+v; want the sale the reference names", sale)
	}
	if sale.Status != "active" || sale.Channel != "online" ||
		sale.PaymentMethod == nil || *sale.PaymentMethod != "payphone" {
		t.Fatalf("sale status/channel/payment method = %+v", sale)
	}
	if sale.ReversedAt != nil || sale.ReversedBy != nil {
		t.Fatalf("active sale carries reversal provenance: %+v", sale)
	}
	if sale.SoldAt == "" || sale.RecordedAt == "" {
		t.Fatalf("sale timestamps = %+v", sale)
	}

	// The buyer, as the sale snapshotted them.
	wantCustomer := operatorSaleCustomer{Email: "bea@example.com", FirstName: "Bea", LastName: "Ruiz"}
	if sale.Customer != wantCustomer {
		t.Fatalf("customer = %+v; want %+v", sale.Customer, wantCustomer)
	}

	// What the buyer paid, and how it splits: two tickets under pass_on Fee
	// Handling, so the Organization nets the price it set and the platform keeps
	// the fee and its Fee IVA.
	if sale.TicketCount != 2 || len(sale.TicketTypes) != 1 ||
		sale.TicketTypes[0] != (operatorSaleLine{TicketTypeName: "GA", Quantity: 2}) {
		t.Fatalf("ticket types = %+v (count %d)", sale.TicketTypes, sale.TicketCount)
	}
	wantAmount := 2 * (feeTestBaseCents + feeTestFeeCents + feeTestIVACents)
	if sale.Currency != "USD" || sale.AmountCents != wantAmount ||
		sale.PlatformFeeCents != 2*feeTestFeeCents || sale.FeeIVACents != 2*feeTestIVACents ||
		sale.NetProceedsCents != 2*feeTestBaseCents {
		t.Fatalf("amounts = %+v; want %d collected, %d fee, %d Fee IVA, %d net",
			sale, wantAmount, 2*feeTestFeeCents, 2*feeTestIVACents, 2*feeTestBaseCents)
	}

	// The Event the sale is for.
	if sale.Event.ID != eventID || sale.Event.Name != "Lookup Fest" ||
		sale.Event.Slug != "lookup-fest" || sale.Event.StartsAt == nil ||
		sale.Event.Timezone != "America/Guayaquil" {
		t.Fatalf("event = %+v", sale.Event)
	}

	// The Organization, which the operator belongs to not at all.
	if found.Organization.Name != "Other Org" || found.Organization.Slug != "other-org" ||
		found.Organization.Currency != "USD" || found.Organization.ID == "" {
		t.Fatalf("organization = %+v", found.Organization)
	}

	// Bought at 07:00 Ecuador time on an Event three days out: the window closes
	// at 20:00 that same day and has not passed.
	if sale.ReversalWindowPassed {
		t.Fatalf("reversal window passed at %s; want the window still open", env.fixedClock)
	}
	if sale.ReversalWindowClosesAt == nil {
		t.Fatalf("reversal window closes at null while the window is open")
	}
	wantClosesAt := time.Date(2026, 7, 8, 1, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if *sale.ReversalWindowClosesAt != wantClosesAt {
		t.Fatalf("reversal window closes at %q; want 20:00 Ecuador time on the day of purchase (%q)",
			*sale.ReversalWindowClosesAt, wantClosesAt)
	}
}

// TestOperatorSaleLookupReportsAPassedReversalWindow: the one fact the lookup
// exists to state that the Sales list never had to — the operator is about to
// act precisely because the buyer's own undo is no longer available.
func TestOperatorSaleLookupReportsAPassedReversalWindow(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	_, ga := publishCheckoutEvent(t, env, adminSessionID, "Late Fest", "late-fest", feeTestBaseCents, 10)
	begun := beginCheckoutOK(t, env, testOrgSlug, "late-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(ga, 1)))
	settled := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	operatorSessionID := operatorSession(t, env, "operator@example.com")

	// The morning after: past 20:00 Ecuador time on the day of purchase, and the
	// Event has not even happened yet.
	holdClocksAt(env.fixedClock.Add(24 * time.Hour))
	found := lookUpSale(t, env, operatorSessionID, settled.ConfirmationRef)
	if !found.Sale.ReversalWindowPassed {
		t.Fatalf("reversal window passed = false a day after the purchase: %+v", found.Sale)
	}
	if found.Sale.Status != "active" {
		t.Fatalf("status = %q; a passed window reverses nothing by itself", found.Sale.Status)
	}
}

// TestOperatorSaleLookupShowsAReversedSale: a reversed Ticket Sale is never
// deleted — it keeps its Sale Confirmation reference — so the reference in a
// support thread still resolves, and says what happened to it.
func TestOperatorSaleLookupShowsAReversedSale(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	_, ga := publishFreeEvent(t, env, adminSessionID, "Free Fest", "free-fest", 10)
	ref := claimFree(t, env, "free-fest", ga, "ana@example.com", 1)
	undoOwnSale(t, env, "ana@example.com", ref)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	sale := lookUpSale(t, env, operatorSessionID, ref).Sale

	if sale.Status != "reversed" || sale.ConfirmationRef != ref {
		t.Fatalf("reversed sale = %+v; want the same reference, status reversed", sale)
	}
	if sale.ReversedAt == nil || sale.ReversedBy == nil || *sale.ReversedBy != "customer" {
		t.Fatalf("reversal provenance = %+v; want the buyer's own undo", sale)
	}
	// A free claim collected nothing, and the platform withheld nothing.
	if sale.PaymentMethod == nil || *sale.PaymentMethod != "free" ||
		sale.AmountCents != 0 || sale.PlatformFeeCents != 0 || sale.FeeIVACents != 0 {
		t.Fatalf("free sale amounts = %+v", sale)
	}
}

// TestOperatorSaleLookupIsOperatorsOnly: the gate is the namespace's, so an
// Org Admin of the very Organization that made the sale is refused — reading
// across every Organization is not something an org role can grant itself.
func TestOperatorSaleLookupIsOperatorsOnly(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	_, ga := publishCheckoutEvent(t, env, adminSessionID, "Gate Fest", "gate-fest", feeTestBaseCents, 10)
	begun := beginCheckoutOK(t, env, testOrgSlug, "gate-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(ga, 1)))
	settled := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	path := operatorSaleLookupPath(settled.ConfirmationRef)

	resp, body := env.get(t, path, nil)
	if resp.StatusCode != http.StatusUnauthorized || body.Error == nil || body.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("unauthenticated lookup status=%d error=%+v; want 401 UNAUTHORIZED", resp.StatusCode, body.Error)
	}

	resp, body = env.get(t, path, authHeader(adminSessionID))
	if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("org_admin lookup status=%d error=%+v; want 403 FORBIDDEN", resp.StatusCode, body.Error)
	}

	// And the operator, who is a Member of nothing, is served the same sale.
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	if got := lookUpSale(t, env, operatorSessionID, settled.ConfirmationRef); got.Sale.ConfirmationRef != settled.ConfirmationRef {
		t.Fatalf("operator lookup = %+v", got.Sale)
	}
}

// TestOperatorSaleLookupUnknownReference: a reference nothing carries is a 404
// with the house envelope, not an empty sale.
func TestOperatorSaleLookupUnknownReference(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	resp, body := env.get(t, operatorSaleLookupPath("TP-NOSUCHREF"), authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown reference status=%d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_SALE_NOT_FOUND" || !isNullData(body.Data) {
		t.Fatalf("unknown reference envelope: data=%s error=%+v", body.Data, body.Error)
	}
}

// TestOperatorSaleLookupIgnoresReferenceCase: an operator pastes what a support
// thread quoted, and quoted text loses its case. References are generated
// uppercase and are unique, so matching case-insensitively can find nothing but
// the one sale.
func TestOperatorSaleLookupIgnoresReferenceCase(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	_, ga := publishFreeEvent(t, env, adminSessionID, "Case Fest", "case-fest", 10)
	ref := claimFree(t, env, "case-fest", ga, "ana@example.com", 1)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	lowered := lowercaseRef(ref)
	if got := lookUpSale(t, env, operatorSessionID, lowered).Sale; got.ConfirmationRef != ref {
		t.Fatalf("lookup of %q = %+v; want the sale referenced by %q", lowered, got, ref)
	}
}

func lowercaseRef(ref string) string {
	out := []byte(ref)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + ('a' - 'A')
		}
	}
	return string(out)
}
