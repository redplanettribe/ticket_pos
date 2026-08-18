package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Free checkout settled inside the begin-checkout request (issue #113, ADR
// 0017): a cart that totals zero is settled by the platform there and then. No
// Payment Provider is contacted, no Capacity Hold is taken out, no return leg
// runs — the Ticket Sale is recorded immediately with Payment Method `free`,
// the Sale Confirmation goes out, and capacity is consumed.
//
// A cart is free only when it totals ZERO: one paid ticket anywhere makes the
// whole thing an ordinary provider checkout, unchanged.
//
// These assert at HTTP wherever an API exposes the answer; the fee split, the
// Tax ID snapshots and the Payment row go to SQL, as they do everywhere else in
// this package, because nothing surfaces them.

// freeCheckoutResult is the widened begin-checkout result (#113): the
// settlement status is always present, while the redirect URL and the
// confirmation reference are each present in exactly one of the two outcomes.
// Both are pointers so a MISSING key is distinguishable from an empty one —
// omission is the contract, not blankness.
type freeCheckoutResult struct {
	ClientTransactionID string  `json:"client_transaction_id"`
	Status              string  `json:"status"`
	RedirectURL         *string `json:"redirect_url"`
	ConfirmationRef     *string `json:"confirmation_ref"`
	AmountCents         int     `json:"amount_cents"`
	Currency            string  `json:"currency"`
}

// beginCheckoutSettled begins a checkout and decodes the widened result. The
// token is a Customer Session token, empty for an anonymous guest, exactly as
// beginCheckoutAs takes it.
func beginCheckoutSettled(t *testing.T, env *testEnv, orgSlug, eventSlug, token string, body map[string]any) freeCheckoutResult {
	t.Helper()
	resp, envBody := beginCheckoutAs(t, env, orgSlug, eventSlug, token, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("begin checkout status=%d error=%+v, want 201", resp.StatusCode, envBody.Error)
	}
	var result freeCheckoutResult
	if err := json.Unmarshal(envBody.Data, &result); err != nil {
		t.Fatalf("decode begin checkout result: %v", err)
	}
	return result
}

// approvedRef asserts the shape of a settled free claim and returns its Sale
// Confirmation reference: approved, a TP- reference, and no redirect anywhere.
func approvedRef(t *testing.T, result freeCheckoutResult) string {
	t.Helper()
	if result.Status != "approved" {
		t.Fatalf("begin checkout status = %q, want approved on a free claim", result.Status)
	}
	if result.RedirectURL != nil {
		t.Fatalf("redirect_url = %q, want it omitted on a settled free claim", *result.RedirectURL)
	}
	if result.ConfirmationRef == nil || !strings.HasPrefix(*result.ConfirmationRef, "TP-") {
		t.Fatalf("confirmation_ref = %v, want a TP- reference on a settled free claim", result.ConfirmationRef)
	}
	return *result.ConfirmationRef
}

// cartLine is one cart line, as the checkout body takes it.
func cartLine(ticketTypeID string, quantity int) map[string]any {
	return map[string]any{"ticket_type_id": ticketTypeID, "quantity": quantity}
}

// eventSales reads the staff Sales list for an Event.
func eventSales(t *testing.T, env *testEnv, sessionID, eventID string) []saleListRow {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return salesList(t, body).Data
}

// TestFreeCheckoutSettlesInTheBeginRequest is the tracer bullet: a cart of only
// free tickets comes back approved, with a reference and nothing to pay, and
// with no provider page to send the buyer to.
func TestFreeCheckoutSettlesInTheBeginRequest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishCheckoutEvent(t, env, sessionID, "Free Fest", "free-fest", 0, 10)

	claim := beginCheckoutSettled(t, env, "test-org", "free-fest", "",
		checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(freeID, 2)))
	approvedRef(t, claim)

	if claim.ClientTransactionID == "" {
		t.Fatal("client_transaction_id missing; a settled free claim is still a checkout")
	}
	if claim.AmountCents != 0 {
		t.Fatalf("amount_cents = %d, want 0 — a free cart totals zero", claim.AmountCents)
	}
	if claim.Currency != "USD" {
		t.Fatalf("currency = %q, want USD", claim.Currency)
	}
}

// TestFreeCheckoutRecordsTheSaleImmediately: the Ticket Sale exists the moment
// begin returns — there is no confirm leg to wait for. It is an Online Sale
// with Payment Method `free` and nothing paid, and it snapshots the buyer's Tax
// ID like any other recorded sale.
func TestFreeCheckoutRecordsTheSaleImmediately(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID := publishCheckoutEvent(t, env, sessionID, "Declarable Fest", "declarable-fest", 0, 10)

	claim := beginCheckoutSettled(t, env, "test-org", "declarable-fest", "",
		taxIDCheckoutBody("guest@example.com", "Ana", "Lopez", "cedula", validCedula, cartLine(freeID, 2)))
	ref := approvedRef(t, claim)

	rows := eventSales(t, env, sessionID, eventID)
	if len(rows) != 1 {
		t.Fatalf("sales rows = %d, want 1 recorded by begin alone", len(rows))
	}
	sale := rows[0]
	if sale.Channel != "online" {
		t.Fatalf("channel = %q, want online", sale.Channel)
	}
	if sale.Source != nil {
		t.Fatalf("source = %v, want none on an Online Sale", *sale.Source)
	}
	if sale.PaymentMethod == nil || *sale.PaymentMethod != "free" {
		t.Fatalf("payment_method = %v, want free", sale.PaymentMethod)
	}
	if sale.AmountCents != 0 {
		t.Fatalf("sale amount = %d, want 0 — the buyer paid nothing", sale.AmountCents)
	}
	if sale.Status != "active" {
		t.Fatalf("sale status = %q, want active", sale.Status)
	}
	if sale.ConfirmationRef != ref {
		t.Fatalf("sale confirmation ref = %q, want the %q begin returned", sale.ConfirmationRef, ref)
	}
	if sale.CustomerEmail != "guest@example.com" || sale.CustomerFirstName != "Ana" || sale.CustomerLastName != "Lopez" {
		t.Fatalf("sale customer = %s %s <%s>, want Ana Lopez <guest@example.com>",
			sale.CustomerFirstName, sale.CustomerLastName, sale.CustomerEmail)
	}
	if len(sale.TicketTypes) != 1 || sale.TicketTypes[0].TicketTypeName != "GA" || sale.TicketTypes[0].Quantity != 2 {
		t.Fatalf("sale lines = %+v, want GA×2", sale.TicketTypes)
	}

	// The buyer's Customer record was upserted inside the same commit.
	if got := customerCountByEmail(t, env, "guest@example.com"); got != 1 {
		t.Fatalf("customers for the buyer = %d, want 1", got)
	}
	// And the sale carries the immutable Tax ID it was transacted under.
	if got := readSaleTaxID(t, env, ref); !got.is("cedula", validCedula) {
		t.Fatalf("sale tax id = %s, want cedula:%s", got, validCedula)
	}
}

// TestFreeSaleLinesCarryNoFeeInEitherHandlingMode: there is nothing to withhold
// from zero, so a free line records a zero base price, zero Platform Fee and
// zero Fee IVA — and the buyer pays nothing under both Fee Handling modes,
// because pass_on has no fee to add and absorb has none to swallow.
//
// The organizer's side of that is asserted too: a free claim leaves the
// Organization nothing, so the Event's Net Proceeds stay at zero under both
// modes — free to the buyer must not mean the platform withheld something.
func TestFreeSaleLinesCarryNoFeeInEitherHandlingMode(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	for _, mode := range []string{"pass_on", "absorb"} {
		name, slug := "Fee "+mode+" Fest", "fee-"+strings.ReplaceAll(mode, "_", "-")+"-fest"
		eventID, freeID := publishCheckoutEvent(t, env, sessionID, name, slug, 0, 10)
		setFeeHandling(t, env, sessionID, eventID, name, slug, mode)

		claim := beginCheckoutSettled(t, env, "test-org", slug, "",
			checkoutBody(mode+"@example.com", "Ana", "Lopez", cartLine(freeID, 2)))
		ref := approvedRef(t, claim)
		if claim.AmountCents != 0 {
			t.Fatalf("%s: amount_cents = %d, want 0 — a free ticket is free either way", mode, claim.AmountCents)
		}

		line := saleLineFees(t, env, ref, freeID)
		if line.Quantity != 2 {
			t.Fatalf("%s: sale line quantity = %d, want 2", mode, line.Quantity)
		}
		if line.UnitPriceCents != 0 || line.BasePriceCents != 0 || line.FeeCents != 0 || line.FeeIVACents != 0 {
			t.Fatalf("%s: sale line = %+v, want no money anywhere in it", mode, line)
		}

		// The Event is this claim alone, so the whole stat strip is the claim's
		// contribution: one recorded sale that earned the Organization nothing.
		// The summary carries the take-home figure and nothing else (#92), so
		// zero Net Proceeds is the entire money claim to make here.
		summary := salesSummaryOK(t, env, sessionID, eventID)
		want := salesSummary{NetProceedsCents: 0, Currency: "USD", SalesCount: 1, TicketsSold: 2}
		if summary != want {
			t.Fatalf("%s: sales summary = %+v, want %+v — a free claim earns the Organization nothing", mode, summary, want)
		}
	}
}

// TestFreeCheckoutSendsTheSaleConfirmation: the claim is a real sale, so the
// buyer gets the same Sale Confirmation a paid one produces — one email, their
// address, the reference begin returned, a working Confirmation Link, and a
// total of nothing.
func TestFreeCheckoutSendsTheSaleConfirmation(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishCheckoutEvent(t, env, sessionID, "Mailed Fest", "mailed-fest", 0, 10)

	claim := beginCheckoutSettled(t, env, "test-org", "mailed-fest", "",
		checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(freeID, 1)))
	ref := approvedRef(t, claim)

	confs := env.email.Confirmations()
	if len(confs) != 1 {
		t.Fatalf("sale confirmations sent = %d, want 1", len(confs))
	}
	conf := confs[0]
	if conf.To != "guest@example.com" {
		t.Fatalf("confirmation to = %q, want the buyer guest@example.com", conf.To)
	}
	if conf.Reference != ref {
		t.Fatalf("confirmation reference = %q, want the %q begin returned", conf.Reference, ref)
	}
	if conf.EventName != "Mailed Fest" {
		t.Fatalf("confirmation event = %q, want Mailed Fest", conf.EventName)
	}
	if conf.ConfirmationLink == "" {
		t.Fatal("confirmation carried no Confirmation Link")
	}
	if conf.AmountCents != 0 {
		t.Fatalf("confirmation total = %d, want 0", conf.AmountCents)
	}
}

// TestFreeCheckoutConsumesCapacity: a claim sells the tickets outright, so
// sold_count moves at begin, and claiming beyond what is left is refused with
// the ordinary capacity conflict rather than a settlement error.
func TestFreeCheckoutConsumesCapacity(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID := publishCheckoutEvent(t, env, sessionID, "Scarce Fest", "scarce-fest", 0, 3)

	claim := beginCheckoutSettled(t, env, "test-org", "scarce-fest", "",
		checkoutBody("first@example.com", "Ana", "Lopez", cartLine(freeID, 2)))
	approvedRef(t, claim)

	if got := soldCount(t, env, sessionID, eventID, freeID); got != 2 {
		t.Fatalf("sold_count after the claim = %d, want 2 — a free claim sells immediately", got)
	}

	// 2 of 3 are gone: a claim for 2 more is over capacity.
	resp, body := beginCheckout(t, env, "test-org", "scarce-fest",
		checkoutBody("second@example.com", "Bea", "Ruiz", cartLine(freeID, 2)))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("over-capacity claim status = %d, want 409", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "CAPACITY_EXCEEDED" {
		t.Fatalf("over-capacity claim error = %+v, want CAPACITY_EXCEEDED", body.Error)
	}
	if got := soldCount(t, env, sessionID, eventID, freeID); got != 2 {
		t.Fatalf("sold_count after the refusal = %d, want the unchanged 2", got)
	}
}

// TestFreeCheckoutLeavesNoCapacityHold: a settled claim never passes through
// the pending state a Capacity Hold is derived from (ADR 0013). Its Payment is
// approved and linked to the sale the instant it exists, and the tickets it did
// NOT take are claimable immediately — without advancing the clock past the
// hold window, which is the only thing that would free a lingering hold.
func TestFreeCheckoutLeavesNoCapacityHold(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID := publishCheckoutEvent(t, env, sessionID, "Unheld Fest", "unheld-fest", 0, 3)

	claim := beginCheckoutSettled(t, env, "test-org", "unheld-fest", "",
		checkoutBody("first@example.com", "Ana", "Lopez", cartLine(freeID, 1)))
	ref := approvedRef(t, claim)

	status, ticketSaleID, _ := paymentRecord(t, env, claim.ClientTransactionID)
	if status != "approved" {
		t.Fatalf("payment status = %q, want approved — a free Payment is settled on creation", status)
	}
	if ticketSaleID == nil {
		t.Fatalf("payment ticket_sale_id = nil, want the sale %q it committed", ref)
	}

	// The remaining 2 are claimable right now, at the unmoved clock: nothing is
	// being held on the first claim's behalf.
	second := beginCheckoutSettled(t, env, "test-org", "unheld-fest", "",
		checkoutBody("second@example.com", "Bea", "Ruiz", cartLine(freeID, 2)))
	approvedRef(t, second)

	if got := soldCount(t, env, sessionID, eventID, freeID); got != 3 {
		t.Fatalf("sold_count = %d, want 3 — both claims sold without waiting out a hold", got)
	}
}

// TestFreeCheckoutStillRequiresATaxID: settling in-request changes who pays,
// not what the Organization must declare (ADR 0016). Omitting the Tax ID fails
// exactly as it does on a paid checkout, and nothing is recorded.
func TestFreeCheckoutStillRequiresATaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID := publishCheckoutEvent(t, env, sessionID, "Declared Fest", "declared-fest", 0, 10)

	body := checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(freeID, 1))
	delete(body, "customer_tax_id_type")
	delete(body, "customer_tax_id_number")

	resp, envBody := beginCheckout(t, env, "test-org", "declared-fest", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if envBody.Error == nil || envBody.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("error = %+v, want VALIDATION_FAILED", envBody.Error)
	}
	if fields := fieldErrors(t, envBody); fields["customer_tax_id_type"] == "" {
		t.Fatalf("fields = %+v, want an error on customer_tax_id_type", fields)
	}

	// The rejection is total: no sale, no capacity, no email.
	if got := salesCountByEmail(t, env, eventID, "guest@example.com"); got != 0 {
		t.Fatalf("sales after the rejected claim = %d, want 0", got)
	}
	if got := soldCount(t, env, sessionID, eventID, freeID); got != 0 {
		t.Fatalf("sold_count after the rejected claim = %d, want 0", got)
	}
	if got := len(env.email.Confirmations()); got != 0 {
		t.Fatalf("confirmations sent = %d, want 0", got)
	}
}

// TestMixedFreeAndPaidCartGoesToTheProvider: free is a property of the CART,
// not of a line. One paid ticket makes the whole checkout an ordinary provider
// checkout — pending, with a redirect and no reference, recording nothing until
// confirm.
func TestMixedFreeAndPaidCartGoesToTheProvider(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID := publishCheckoutEvent(t, env, sessionID, "Mixed Fest", "mixed-fest", 0, 10)
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 10)

	begin := beginCheckoutSettled(t, env, "test-org", "mixed-fest", "",
		checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(freeID, 2), cartLine(gaID, 1)))

	if begin.Status != "pending" {
		t.Fatalf("status = %q, want pending — one paid ticket makes it a provider checkout", begin.Status)
	}
	if begin.RedirectURL == nil || *begin.RedirectURL == "" {
		t.Fatalf("redirect_url = %v, want the provider page a pending checkout needs", begin.RedirectURL)
	}
	if begin.ConfirmationRef != nil {
		t.Fatalf("confirmation_ref = %q, want it omitted while the Payment is pending", *begin.ConfirmationRef)
	}
	// The all-in price of the one paid ticket under the default pass_on Fee
	// Handling; the free tickets add nothing (ADR 0014).
	if begin.AmountCents != 1115 {
		t.Fatalf("amount_cents = %d, want the 1115 the paid ticket costs", begin.AmountCents)
	}

	// Nothing is recorded until the buyer returns from the provider.
	status, ticketSaleID, _ := paymentRecord(t, env, begin.ClientTransactionID)
	if status != "pending" || ticketSaleID != nil {
		t.Fatalf("payment = %s/%v, want pending with no sale", status, ticketSaleID)
	}
	if got := salesCountByEmail(t, env, eventID, "guest@example.com"); got != 0 {
		t.Fatalf("sales before confirm = %d, want 0", got)
	}
	if got := soldCount(t, env, sessionID, eventID, freeID); got != 0 {
		t.Fatalf("free sold_count before confirm = %d, want 0", got)
	}
	if got := len(env.email.Confirmations()); got != 0 {
		t.Fatalf("confirmations before confirm = %d, want 0", got)
	}

	// And the ordinary return leg still settles it whole.
	confirm := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	if confirm.Status != "approved" || !strings.HasPrefix(confirm.ConfirmationRef, "TP-") {
		t.Fatalf("confirm = %+v, want approved with a TP- reference", confirm)
	}
	if got := soldCount(t, env, sessionID, eventID, freeID); got != 2 {
		t.Fatalf("free sold_count after confirm = %d, want 2", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 1 {
		t.Fatalf("paid sold_count after confirm = %d, want 1", got)
	}
}

// TestFreeClaimAppliesTheSelfAssertedTaxIDRules: settling in-request does not
// exempt a free claim from ADR 0016. A Customer claiming under their own
// Customer Session corrects their stored Tax ID; an anonymous claim under a
// Verified Customer's email records what was typed on the sale but leaves the
// person's own assertion alone.
func TestFreeClaimAppliesTheSelfAssertedTaxIDRules(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishCheckoutEvent(t, env, sessionID, "Asserted Fest", "asserted-fest", 0, 10)
	line := cartLine(freeID, 1)

	// She claims once as a guest, filling her blank Tax ID, then claims the
	// record by signing in.
	first := beginCheckoutSettled(t, env, "test-org", "asserted-fest", "",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", validCedula, line))
	approvedRef(t, first)
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("cedula", validCedula) {
		t.Fatalf("customer tax id after the guest claim = %s, want the filled cedula:%s", got, validCedula)
	}
	token := customerSignIn(t, env, "ana@example.com")

	// Signed in, she claims again under her company RUC: the person correcting
	// herself, so the write-back applies.
	own := beginCheckoutSettled(t, env, "test-org", "asserted-fest", token,
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "ruc", companyRUC, line))
	ownRef := approvedRef(t, own)
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("ruc", companyRUC) {
		t.Fatalf("customer tax id after her own claim = %s, want the override ruc:%s", got, companyRUC)
	}
	if got := readSaleTaxID(t, env, ownRef); !got.is("ruc", companyRUC) {
		t.Fatalf("her sale tax id = %s, want ruc:%s", got, companyRUC)
	}

	// A stranger claims a free ticket under her address with a number of their
	// choosing. The sale records what was typed; her profile does not move.
	stranger := beginCheckoutSettled(t, env, "test-org", "asserted-fest", "",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", otherCedula, line))
	strangerRef := approvedRef(t, stranger)
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("ruc", companyRUC) {
		t.Fatalf("customer tax id after the anonymous claim = %s, want the untouched ruc:%s", got, companyRUC)
	}
	if got := readSaleTaxID(t, env, strangerRef); !got.is("cedula", otherCedula) {
		t.Fatalf("stranger's sale tax id = %s, want the typed cedula:%s", got, otherCedula)
	}
}

// TestFreeClaimsAreFilterableByPaymentMethod: `free` is a Payment Method like
// any other on the Sales list, so an Org Admin can separate the claims that
// collected no money from the Online Sales that did. On one Event holding both,
// payment_method=free returns only the claim and payment_method=payphone only
// the paid sale — neither leaks into the other.
func TestFreeClaimsAreFilterableByPaymentMethod(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID := publishCheckoutEvent(t, env, sessionID, "Filter Fest", "filter-fest", 0, 10)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 2500, 10)

	// The claim settles in the begin request; the paid sale needs the ordinary
	// return leg before it is a row at all.
	claim := beginCheckoutSettled(t, env, "test-org", "filter-fest", "",
		checkoutBody("claimed@example.com", "Ana", "Lopez", cartLine(freeID, 1)))
	approvedRef(t, claim)

	paid := beginCheckoutOK(t, env, "test-org", "filter-fest",
		checkoutBody("paid@example.com", "Bea", "Ruiz", cartLine(vipID, 1)))
	if got := confirmCheckoutOK(t, env, paid.ClientTransactionID, "approved"); got.Status != "approved" {
		t.Fatalf("paid confirm status = %q, want approved", got.Status)
	}

	// Both are on the Event's unfiltered list, so a filter narrowing to one is
	// filtering rather than failing to find the other.
	if got := len(eventSales(t, env, sessionID, eventID)); got != 2 {
		t.Fatalf("unfiltered rows = %d, want 2 — the free claim and the paid sale", got)
	}

	_, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?payment_method=free", authHeader(sessionID))
	free := salesList(t, body)
	if got := listSalesEmails(free); len(got) != 1 || got[0] != "claimed@example.com" {
		t.Fatalf("payment_method=free = %v, want [claimed@example.com]", got)
	}
	if free.Pagination.Total != 1 {
		t.Fatalf("payment_method=free total = %d, want 1", free.Pagination.Total)
	}
	if free.Data[0].PaymentMethod == nil || *free.Data[0].PaymentMethod != "free" {
		t.Fatalf("filtered row payment_method = %v, want free", free.Data[0].PaymentMethod)
	}
	if free.Data[0].AmountCents != 0 {
		t.Fatalf("filtered row amount = %d, want 0 — the claim collected no money", free.Data[0].AmountCents)
	}

	// And the paid Online Sale is still reachable under its own method, with the
	// claim excluded.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?payment_method=payphone", authHeader(sessionID))
	payphone := salesList(t, body)
	if got := listSalesEmails(payphone); len(got) != 1 || got[0] != "paid@example.com" {
		t.Fatalf("payment_method=payphone = %v, want [paid@example.com]", got)
	}
	if payphone.Pagination.Total != 1 {
		t.Fatalf("payment_method=payphone total = %d, want 1", payphone.Pagination.Total)
	}
}

// TestFreeSaleAppearsInCustomerArea: a free claim belongs to its Customer like
// any other Ticket Sale, and sits beside a paid one in the Customer Area.
func TestFreeSaleAppearsInCustomerArea(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID := publishCheckoutEvent(t, env, sessionID, "Both Fest", "both-fest", 0, 10)
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2500, 10)

	free := beginCheckoutSettled(t, env, "test-org", "both-fest", "",
		checkoutBody("area@example.com", "Ana", "Lopez", cartLine(freeID, 1)))
	freeRef := approvedRef(t, free)

	paid := beginCheckoutOK(t, env, "test-org", "both-fest",
		checkoutBody("area@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	paidSale := confirmCheckoutOK(t, env, paid.ClientTransactionID, "approved")
	if paidSale.Status != "approved" {
		t.Fatalf("paid confirm status = %q, want approved", paidSale.Status)
	}

	token := customerSignIn(t, env, "area@example.com")
	area := readCustomerArea(t, env, token, "")
	if len(area.Upcoming) != 2 || len(area.Past) != 0 {
		t.Fatalf("customer area upcoming=%d past=%d, want the free and the paid sale upcoming",
			len(area.Upcoming), len(area.Past))
	}

	byRef := map[string]customerAreaSale{}
	for _, sale := range area.Upcoming {
		byRef[sale.ConfirmationRef] = sale
	}
	freeSale, ok := byRef[freeRef]
	if !ok {
		t.Fatalf("customer area refs = %+v, want the free claim %q among them", byRef, freeRef)
	}
	if freeSale.AmountCents != 0 {
		t.Fatalf("free sale amount in the Customer Area = %d, want 0", freeSale.AmountCents)
	}
	if len(freeSale.Lines) != 1 || freeSale.Lines[0].Quantity != 1 || freeSale.Lines[0].UnitPriceCents != 0 {
		t.Fatalf("free sale lines = %+v, want 1 free ticket at 0", freeSale.Lines)
	}
	if _, ok := byRef[paidSale.ConfirmationRef]; !ok {
		t.Fatalf("customer area refs = %+v, want the paid sale %q among them", byRef, paidSale.ConfirmationRef)
	}
}
