package integration

import (
	"strings"
	"testing"
)

// The buyer-facing paper trail for the Tax ID (#99, ADR 0016): the Sale
// Confirmation email the Customer keeps for their own expense records, and the
// Customer Area list where they can tell a personal purchase from one made under
// a company RUC.
//
// Both surfaces show the *sale's* snapshot and nothing else. That is the property
// worth defending: a Customer who edits their profile, or buys again under a
// different Tax ID, must find every earlier receipt unchanged. Sales recorded
// before the feature, and imported sales that never carried an ID, show nothing
// at all rather than a labelled blank — history is never backfilled.

// taxIDLabels are the labels a buyer reads on their receipt, one per Tax ID
// Type. They are asserted literally here, because "renders as a human label"
// is the acceptance criterion: a receipt reading "cedula: 1712345675" would
// pass any test that only checked the number.
var taxIDLabels = map[string]string{
	"cedula":   "Cédula",
	"ruc":      "RUC",
	"passport": "Pasaporte",
}

// lastConfirmationText renders the most recently captured Sale Confirmation the
// way a provider delivers it. The capture sender records the message, not the
// bytes, so the rendering is done here through the same method the Resend sender
// calls — there is one composition of this email and this is it.
func lastConfirmationText(t *testing.T, env *testEnv) string {
	t.Helper()
	confs := env.email.Confirmations()
	if len(confs) == 0 {
		t.Fatal("no Sale Confirmation was captured")
	}
	return confs[len(confs)-1].Text()
}

// TestSaleConfirmationEmailShowsTheTaxIDTransactedUnder walks the three Tax ID
// Types through an Online Sale and reads the receipt each one produces.
func TestSaleConfirmationEmailShowsTheTaxIDTransactedUnder(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Receipt Fest", "receipt-fest", 1000, 20)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	cases := []struct {
		email     string
		taxIDType string
		number    string
		wantLine  string
	}{
		{"cedula-buyer@example.com", "cedula", validCedula, "Cédula: " + validCedula},
		{"ruc-buyer@example.com", "ruc", companyRUC, "RUC: " + companyRUC},
		// The passport is stored uppercased, and the receipt must echo what was
		// stored rather than what was typed.
		{"passport-buyer@example.com", "passport", lowercasePassprt, "Pasaporte: AB123456"},
	}
	for _, tc := range cases {
		begin := beginCheckoutOK(t, env, "test-org", "receipt-fest",
			taxIDCheckoutBody(tc.email, "Ana", "Lopez", tc.taxIDType, tc.number, line))
		confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

		text := lastConfirmationText(t, env)
		if !strings.Contains(text, tc.wantLine) {
			t.Fatalf("%s: confirmation text %q does not contain %q", tc.email, text, tc.wantLine)
		}
		// The receipt still says everything it said before the Tax ID landed on it.
		if !strings.Contains(text, confirmed.ConfirmationRef) {
			t.Fatalf("%s: confirmation text %q lost the reference %q", tc.email, text, confirmed.ConfirmationRef)
		}
	}
}

// TestSaleConfirmationEmailWithoutTaxIDRendersCleanly pins the other half: an
// imported sale carries no Tax ID (ADR 0016 keeps the `import` channel optional),
// and its receipt must simply not mention one — no label, no empty value, no
// stranded colon where a number should have been.
func TestSaleConfirmationEmailWithoutTaxIDRendersCleanly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Legacy Fest", "legacy-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	ref := importOneSale(t, env, sessionID, eventID, "legacy-1", ttID, "ana@example.com", "Ana", "Lopez")

	text := lastConfirmationText(t, env)
	for _, label := range taxIDLabels {
		if strings.Contains(text, label) {
			t.Fatalf("confirmation text %q mentions %q for a sale with no Tax ID", text, label)
		}
	}
	// Nothing was left dangling either. A label may end in a colon — "View your
	// tickets:" does — but only when something follows it; a label with nothing
	// after it is the placeholder garbage this test exists to rule out.
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if !strings.HasSuffix(strings.TrimSpace(line), ":") {
			continue
		}
		if i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) == "" {
			t.Fatalf("confirmation text has a label with no value: %q (full text %q)", line, text)
		}
	}
	// And it is still a receipt.
	if !strings.Contains(text, ref) {
		t.Fatalf("confirmation text %q does not contain the reference %q", text, ref)
	}
}

// TestCustomerAreaShowsEachSalesTaxIDSnapshot proves the Customer Area payload
// carries the snapshot per sale, so a Customer can tell which purchases were
// personal and which were under a company RUC.
func TestCustomerAreaShowsEachSalesTaxIDSnapshot(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Area Tax Fest", "area-tax-fest", 1000, 20)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	personal := beginCheckoutOK(t, env, "test-org", "area-tax-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", validCedula, line))
	personalSale := confirmCheckoutOK(t, env, personal.ClientTransactionID, "approved")

	company := beginCheckoutOK(t, env, "test-org", "area-tax-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "ruc", companyRUC, line))
	companySale := confirmCheckoutOK(t, env, company.ClientTransactionID, "approved")

	token := customerSignIn(t, env, "ana@example.com")
	area := readCustomerArea(t, env, token, "")

	byRef := map[string]customerAreaSale{}
	for _, sale := range append(append([]customerAreaSale{}, area.Upcoming...), area.Past...) {
		byRef[sale.ConfirmationRef] = sale
	}
	if len(byRef) != 2 {
		t.Fatalf("customer area holds %d sales, want 2", len(byRef))
	}
	assertAreaSaleTaxID(t, byRef[personalSale.ConfirmationRef], "cedula", validCedula)
	assertAreaSaleTaxID(t, byRef[companySale.ConfirmationRef], "ruc", companyRUC)
}

// TestCustomerAreaTaxIDIsTheSaleSnapshotNotTheProfile is the immutability claim,
// exercised the way it actually breaks: the second purchase refreshes the
// Customer's stored Tax ID (the record is unverified at that point), and the
// first sale must still read as it was transacted rather than following the
// profile.
func TestCustomerAreaTaxIDIsTheSaleSnapshotNotTheProfile(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Snapshot Fest", "snapshot-fest", 1000, 20)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	first := beginCheckoutOK(t, env, "test-org", "snapshot-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", validCedula, line))
	firstSale := confirmCheckoutOK(t, env, first.ClientTransactionID, "approved")

	second := beginCheckoutOK(t, env, "test-org", "snapshot-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "ruc", companyRUC, line))
	confirmCheckoutOK(t, env, second.ClientTransactionID, "approved")

	// The profile moved on, which is exactly the ADR 0016 refresh rule.
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("ruc", companyRUC) {
		t.Fatalf("customer tax id = %s, want the refreshed ruc:%s", got, companyRUC)
	}

	token := customerSignIn(t, env, "ana@example.com")
	area := readCustomerArea(t, env, token, "")
	for _, sale := range append(append([]customerAreaSale{}, area.Upcoming...), area.Past...) {
		if sale.ConfirmationRef == firstSale.ConfirmationRef {
			assertAreaSaleTaxID(t, sale, "cedula", validCedula)
			return
		}
	}
	t.Fatalf("the first sale %q is missing from the Customer Area", firstSale.ConfirmationRef)
}

// TestCustomerAreaImportedSaleCarriesNoTaxID proves the absent case reaches the
// client as a clean pair of nulls, which is what lets the Storefront draw "—"
// rather than inventing a value.
func TestCustomerAreaImportedSaleCarriesNoTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "History Fest", "history-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	importOneSale(t, env, sessionID, eventID, "history-1", ttID, "ana@example.com", "Ana", "Lopez")

	token := customerSignIn(t, env, "ana@example.com")
	area := readCustomerArea(t, env, token, "")

	sales := append(append([]customerAreaSale{}, area.Upcoming...), area.Past...)
	if len(sales) != 1 {
		t.Fatalf("customer area holds %d sales, want 1", len(sales))
	}
	if sales[0].TaxIDType != nil || sales[0].TaxIDNumber != nil {
		t.Fatalf("imported sale tax id = %v / %v, want both null", sales[0].TaxIDType, sales[0].TaxIDNumber)
	}
}

// assertAreaSaleTaxID checks one Customer Area entry's Tax ID snapshot. Both
// halves are set together or null together, so a half-populated pair is a
// failure however the other half reads.
func assertAreaSaleTaxID(t *testing.T, sale customerAreaSale, taxIDType, number string) {
	t.Helper()
	if sale.TaxIDType == nil || sale.TaxIDNumber == nil {
		t.Fatalf("sale %q tax id = %v / %v, want %s:%s", sale.ConfirmationRef, sale.TaxIDType, sale.TaxIDNumber, taxIDType, number)
	}
	if *sale.TaxIDType != taxIDType || *sale.TaxIDNumber != number {
		t.Fatalf("sale %q tax id = %s:%s, want %s:%s", sale.ConfirmationRef, *sale.TaxIDType, *sale.TaxIDNumber, taxIDType, number)
	}
}
