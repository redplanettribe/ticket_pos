package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The Customer Dossier (#638, spec #635; CONTEXT.md "Customer Dossier"):
// everything one Event knows about one Customer, addressed by the Customer's id.
//
// Everything is asserted at the HTTP seam — the Dossier's status and body — with
// the fixtures made through the staff and checkout routes, and the Customer id
// read off the Sales list row that links to the Dossier.

// dossierBody mirrors GET /api/v1/staff/events/{id}/customers/{customerId}/dossier.
type dossierBody struct {
	Customer struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"customer"`
	Sales []dossierSale `json:"sales"`
}

type dossierSale struct {
	ID                        string  `json:"id"`
	ConfirmationRef           string  `json:"confirmation_ref"`
	Status                    string  `json:"status"`
	ReversedAt                *string `json:"reversed_at"`
	ReplacedByConfirmationRef *string `json:"replaced_by_confirmation_ref"`
	SoldAt                    string  `json:"sold_at"`
	RecordedAt                string  `json:"recorded_at"`
	Channel                   string  `json:"channel"`
	Source                    *string `json:"source"`
	Origin                    string  `json:"origin"`
	TicketTypes               []struct {
		TicketTypeID   string `json:"ticket_type_id"`
		TicketTypeName string `json:"ticket_type_name"`
		Quantity       int    `json:"quantity"`
	} `json:"ticket_types"`
	AmountCents       int     `json:"amount_cents"`
	Currency          string  `json:"currency"`
	PaymentMethod     *string `json:"payment_method"`
	CustomerFirstName string  `json:"customer_first_name"`
	CustomerLastName  string  `json:"customer_last_name"`
	TaxIDType         *string `json:"tax_id_type"`
	TaxIDNumber       *string `json:"tax_id_number"`
}

func dossierPath(eventID, customerID string) string {
	return "/api/v1/staff/events/" + eventID + "/customers/" + customerID + "/dossier"
}

// readDossier reads a Dossier, failing on any refusal, and returns it with the
// raw payload it was decoded from.
func readDossier(t *testing.T, env *testEnv, sessionID, eventID, customerID string) (dossierBody, json.RawMessage) {
	t.Helper()
	resp, body := env.get(t, dossierPath(eventID, customerID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dossier status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var out dossierBody
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode dossier: %v", err)
	}
	return out, body.Data
}

// dossierStatus reads the Dossier's status code alone, for refusals.
func dossierStatus(t *testing.T, env *testEnv, sessionID, eventID, customerID string) (int, string) {
	t.Helper()
	resp, body := env.get(t, dossierPath(eventID, customerID), authHeader(sessionID))
	code := ""
	if body.Error != nil {
		code = body.Error.Code
	}
	return resp.StatusCode, code
}

// customerIDOnSalesList is the Customer id the Sales list row carries for a
// buyer — the id the list links the Dossier by.
func customerIDOnSalesList(t *testing.T, env *testEnv, sessionID, eventID, email, status string) string {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?status="+status, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page struct {
		Data []struct {
			CustomerID    string `json:"customer_id"`
			CustomerEmail string `json:"customer_email"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode sales list: %v", err)
	}
	for _, row := range page.Data {
		if row.CustomerEmail == email {
			if row.CustomerID == "" {
				t.Fatalf("sales list row for %s carries no customer_id", email)
			}
			return row.CustomerID
		}
	}
	t.Fatalf("no %s sale for %s on the sales list", status, email)
	return ""
}

// addOrgMember adds a colleague at the given role and signs them in.
func addOrgMember(t *testing.T, env *testEnv, adminSession, email, role string) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": email, "role": role,
	}, authHeader(adminSession))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
	}
	return verifyOTP(t, env, email)
}

// dossierSaleByRef picks one Sale off a Dossier, failing when it is absent.
func dossierSaleByRef(t *testing.T, dossier dossierBody, ref string) dossierSale {
	t.Helper()
	for _, sale := range dossier.Sales {
		if sale.ConfirmationRef == ref {
			return sale
		}
	}
	t.Fatalf("sale %s is not on the Dossier", ref)
	return dossierSale{}
}

func strOf(p *string) string {
	if p == nil {
		return "<null>"
	}
	return *p
}

// All three roles in one test, as the Holder List's is: the decision is the
// shape of the gate. Org Admin and Event Owner read the same Dossier, Event
// Staff are refused, and a member of another Organization is told the Event
// does not exist.
func TestTheCustomerDossierIsForTheOrgAdminAndTheEventOwner(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSession, "Dossier Fest", "dossier-fest")
	gaID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "GA", 1000, 50)
	recordManualSaleOK(t, env, adminSession, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "active")

	admin, adminRaw := readDossier(t, env, adminSession, eventID, customerID)
	if len(admin.Sales) != 1 {
		t.Fatalf("Org Admin's Dossier has %d sales, want 1", len(admin.Sales))
	}

	ownerSession := addOrgMember(t, env, adminSession, "owner@example.com", "event_owner")
	_, ownerRaw := readDossier(t, env, ownerSession, eventID, customerID)
	if !bytes.Equal(ownerRaw, adminRaw) {
		t.Errorf("the Event Owner's Dossier differs from the Org Admin's:\nowner %s\nadmin %s", ownerRaw, adminRaw)
	}

	staffSession := addOrgMember(t, env, adminSession, "door@example.com", "event_staff")
	if status, _ := dossierStatus(t, env, staffSession, eventID, customerID); status != http.StatusForbidden {
		t.Errorf("event staff status=%d, want 403", status)
	}

	outsider := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, outsider, "Other Org", "other-org")
	if status, _ := dossierStatus(t, env, outsider, eventID, customerID); status != http.StatusNotFound {
		t.Errorf("another Organization's admin status=%d, want 404", status)
	}
}

// Nothing at this Event is not found, and indistinguishable from nobody: a
// Customer who bought only at another Event of the same Organization, an id
// naming no Customer, and a malformed id all answer the same 404.
func TestTheCustomerDossierIsNotFoundWithoutASaleOnThisEvent(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSession, "Here Fest", "here-fest")
	gaID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "GA", 1000, 50)
	otherEventID := createDraftEvent(t, env, adminSession, "There Fest", "there-fest")
	otherGA := createTicketTypeWithCapacity(t, env, adminSession, otherEventID, "GA", 1000, 50)

	here := recordManualSaleOK(t, env, adminSession, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
	recordManualSaleOK(t, env, adminSession, otherEventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", otherGA, 3, "transfer", "2026-07-02T10:00:00Z"))
	recordManualSaleOK(t, env, adminSession, otherEventID,
		manualSaleBody("elsewhere@example.com", "Eli", "Vera", otherGA, 1, "cash", "2026-07-02T10:00:00Z"))
	ana := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "active")
	elsewhere := customerIDOnSalesList(t, env, adminSession, otherEventID, "elsewhere@example.com", "active")

	// Only this Event's Sale is on Ana's Dossier.
	dossier, _ := readDossier(t, env, adminSession, eventID, ana)
	if len(dossier.Sales) != 1 || dossier.Sales[0].ConfirmationRef != here.ConfirmationRef {
		t.Fatalf("Dossier sales = %+v, want only %s", dossier.Sales, here.ConfirmationRef)
	}

	cases := map[string]string{
		"bought only at another Event of the Organization": elsewhere,
		"no such Customer": "7d9f1a52-3c4b-4e1f-9a2b-0c6d8e7f5a31",
		"malformed id":     "not-a-uuid",
	}
	for name, customerID := range cases {
		status, code := dossierStatus(t, env, adminSession, eventID, customerID)
		if status != http.StatusNotFound || code != "CUSTOMER_NOT_FOUND" {
			t.Errorf("%s: status=%d code=%s, want 404 CUSTOMER_NOT_FOUND", name, status, code)
		}
	}
}

// The Dossier states what THIS Event was told. A later Sale at another
// Organization, under a different name and Tax ID, rewrites nothing on it.
func TestALaterSaleAtAnotherOrganizationChangesNothingOnTheDossier(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSession, "First Fest", "first-fest")
	gaID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "GA", 1000, 50)
	body := manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z")
	body["customer_tax_id_type"] = "cedula"
	body["customer_tax_id_number"] = validCedula
	recordManualSaleOK(t, env, adminSession, eventID, body)
	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "active")

	_, before := readDossier(t, env, adminSession, eventID, customerID)

	otherSession := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSession, "Other Org", "other-org")
	otherEventID := createDraftEvent(t, env, otherSession, "Other Fest", "other-fest")
	otherGA := createTicketTypeWithCapacity(t, env, otherSession, otherEventID, "GA", 1000, 50)
	later := manualSaleBody("ana@example.com", "Anastasia", "Otra", otherGA, 4, "transfer", "2026-07-05T10:00:00Z")
	later["customer_tax_id_type"] = "ruc"
	later["customer_tax_id_number"] = naturalRUC
	recordManualSaleOK(t, env, otherSession, otherEventID, later)
	if other := customerIDOnSalesList(t, env, otherSession, otherEventID, "ana@example.com", "active"); other != customerID {
		t.Fatalf("the other Organization's sale went to Customer %s, want the same Customer %s — the fixture proves nothing", other, customerID)
	}

	after, afterRaw := readDossier(t, env, adminSession, eventID, customerID)
	if !bytes.Equal(afterRaw, before) {
		t.Errorf("the Dossier changed after a sale at another Organization:\nbefore %s\nafter  %s", before, afterRaw)
	}
	sale := after.Sales[0]
	if sale.CustomerFirstName != "Ana" || sale.CustomerLastName != "Lopez" ||
		strOf(sale.TaxIDType) != "cedula" || strOf(sale.TaxIDNumber) != validCedula {
		t.Errorf("sale identity = %s %s %s:%s, want Ana Lopez cedula:%s",
			sale.CustomerFirstName, sale.CustomerLastName, strOf(sale.TaxIDType), strOf(sale.TaxIDNumber), validCedula)
	}
}

// Two of this Event's Sales under different names and Tax IDs each keep their
// own, newest first — and each Sale carries what the Sales list states for it.
func TestEachSaleOnTheDossierKeepsTheIdentityGivenOnIt(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSession, "Two Fest", "two-fest")
	gaID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "GA", 1000, 50)
	vipID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "VIP", 5000, 10)

	personal := manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z")
	personal["customer_tax_id_type"] = "cedula"
	personal["customer_tax_id_number"] = validCedula
	first := recordManualSaleOK(t, env, adminSession, eventID, personal)

	company := manualSaleBody("ana@example.com", "Ana María", "López Vera", vipID, 1, "transfer", "2026-07-03T10:00:00Z")
	company["customer_tax_id_type"] = "ruc"
	company["customer_tax_id_number"] = naturalRUC
	second := recordManualSaleOK(t, env, adminSession, eventID, company)

	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "active")
	dossier, _ := readDossier(t, env, adminSession, eventID, customerID)

	if dossier.Customer.ID != customerID || dossier.Customer.Email != "ana@example.com" {
		t.Errorf("customer = %+v, want %s ana@example.com", dossier.Customer, customerID)
	}
	if len(dossier.Sales) != 2 || dossier.Sales[0].ConfirmationRef != second.ConfirmationRef || dossier.Sales[1].ConfirmationRef != first.ConfirmationRef {
		t.Fatalf("sales = %+v, want %s then %s (newest first)", dossier.Sales, second.ConfirmationRef, first.ConfirmationRef)
	}

	older := dossierSaleByRef(t, dossier, first.ConfirmationRef)
	if older.CustomerFirstName != "Ana" || older.CustomerLastName != "Lopez" || strOf(older.TaxIDType) != "cedula" || strOf(older.TaxIDNumber) != validCedula {
		t.Errorf("first sale identity = %s %s %s:%s", older.CustomerFirstName, older.CustomerLastName, strOf(older.TaxIDType), strOf(older.TaxIDNumber))
	}
	newer := dossierSaleByRef(t, dossier, second.ConfirmationRef)
	if newer.CustomerFirstName != "Ana María" || newer.CustomerLastName != "López Vera" || strOf(newer.TaxIDType) != "ruc" || strOf(newer.TaxIDNumber) != naturalRUC {
		t.Errorf("second sale identity = %s %s %s:%s", newer.CustomerFirstName, newer.CustomerLastName, strOf(newer.TaxIDType), strOf(newer.TaxIDNumber))
	}

	// The rest of the Sale, as the Sales list states it.
	row := saleRowByID(t, env, adminSession, eventID, first.SaleID, "active")
	if older.ID != row.ID || older.Status != "active" || older.ReversedAt != nil ||
		older.SoldAt != row.SoldAt || older.RecordedAt != row.RecordedAt ||
		older.Channel != row.Channel || strOf(older.Source) != strOf(row.Source) || older.Origin != row.Origin ||
		older.AmountCents != row.AmountCents || older.Currency != row.Currency ||
		strOf(older.PaymentMethod) != strOf(row.PaymentMethod) {
		t.Errorf("Dossier sale = %+v\nwant the Sales list's %+v", older, row)
	}
	if older.Origin != "manually_recorded" || older.AmountCents != 2000 {
		t.Errorf("origin=%s amount=%d, want manually_recorded 2000", older.Origin, older.AmountCents)
	}
	if len(older.TicketTypes) != 1 || older.TicketTypes[0].TicketTypeID != gaID || older.TicketTypes[0].TicketTypeName != "GA" || older.TicketTypes[0].Quantity != 2 {
		t.Errorf("ticket types = %+v, want GA ×2", older.TicketTypes)
	}
}

// A reversed Sale stays on the Dossier, marked reversed with its time; a
// corrected one is marked corrected and names its replacement, which is on the
// Dossier too when it went to the same Customer.
func TestReversedAndCorrectedSalesStayOnTheDossierMarked(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSession, "Undo Fest", "undo-fest")
	gaID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "GA", 1000, 50)

	reversed := recordManualSaleOK(t, env, adminSession, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
	corrected := recordManualSaleOK(t, env, adminSession, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-02T10:00:00Z"))
	reverseImportedSaleOK(t, env, adminSession, eventID, reversed.SaleID)
	correction := correctImportedSaleOK(t, env, adminSession, eventID, corrected.SaleID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 3, "cash", "2026-07-02T10:00:00Z"))

	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "reversed")
	dossier, _ := readDossier(t, env, adminSession, eventID, customerID)
	if len(dossier.Sales) != 3 {
		t.Fatalf("sales = %+v, want the reversed, the corrected and its replacement", dossier.Sales)
	}

	gone := dossierSaleByRef(t, dossier, reversed.ConfirmationRef)
	listed := saleRowByID(t, env, adminSession, eventID, reversed.SaleID, "reversed")
	if gone.Status != "reversed" || gone.ReversedAt == nil || listed.ReversedAt == nil || *gone.ReversedAt != *listed.ReversedAt || gone.ReplacedByConfirmationRef != nil {
		t.Errorf("reversed sale status=%s reversed_at=%s replaced_by=%s, want reversed at %s with no replacement",
			gone.Status, strOf(gone.ReversedAt), strOf(gone.ReplacedByConfirmationRef), strOf(listed.ReversedAt))
	}

	fixed := dossierSaleByRef(t, dossier, corrected.ConfirmationRef)
	if fixed.Status != "corrected" || fixed.ReversedAt == nil || strOf(fixed.ReplacedByConfirmationRef) != correction.ReplacementConfirmationRef {
		t.Errorf("corrected sale status=%s reversed_at=%s replaced_by=%s, want corrected → %s",
			fixed.Status, strOf(fixed.ReversedAt), strOf(fixed.ReplacedByConfirmationRef), correction.ReplacementConfirmationRef)
	}

	replacement := dossierSaleByRef(t, dossier, correction.ReplacementConfirmationRef)
	if replacement.Status != "active" || replacement.Origin != "correction_replacement" {
		t.Errorf("replacement status=%s origin=%s, want active correction_replacement", replacement.Status, replacement.Origin)
	}
}

// The Dossier is about the Event, and the declarations a Customer makes to the
// platform — Terms Acceptance, Adulthood Declaration, Marketing Consent — and
// their avatar are never on it. Asserted on the raw payload, over a buyer who
// made them through a real Online checkout.
func TestTheCustomerDossierCarriesNoPlatformDeclarations(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, adminSession, "Online Fest", "online-fest", 1000, 10)
	begun := beginCheckoutOK(t, env, "test-org", "online-fest",
		taxIDCheckoutBody("olga@example.com", "Olga", "Nieto", "cedula", validCedula,
			map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if confirmed := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}
	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "olga@example.com", "active")

	dossier, raw := readDossier(t, env, adminSession, eventID, customerID)
	if len(dossier.Sales) != 1 || dossier.Sales[0].Channel != "online" {
		t.Fatalf("sales = %+v, want the one Online Sale", dossier.Sales)
	}

	var customer map[string]json.RawMessage
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("decode raw dossier: %v", err)
	}
	if err := json.Unmarshal(top["customer"], &customer); err != nil {
		t.Fatalf("decode raw customer: %v", err)
	}
	if len(customer) != 2 || customer["id"] == nil || customer["email"] == nil {
		t.Errorf("customer keys = %s, want exactly id and email", top["customer"])
	}
	lower := strings.ToLower(string(raw))
	for _, forbidden := range []string{"terms", "adulthood", "consent", "marketing", "avatar", "picture", "photo"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("the Dossier payload mentions %q: %s", forbidden, raw)
		}
	}
}

// ---------------------------------------------------------------------------
// What surrounds each Sale (#639): the phone given on its checkout, the
// Affiliate Link that attributed it, its Sale Invoices and whether it was
// re-addressed. Decoded on their own so the Sale mirror above stays the
// tracer's.

type dossierSaleSurroundings struct {
	ConfirmationRef   string                    `json:"confirmation_ref"`
	Phone             *string                   `json:"phone"`
	AffiliateLinkName *string                   `json:"affiliate_link_name"`
	TaxInvoices       []dossierTaxInvoiceMirror `json:"tax_invoices"`
	ReAddressedAt     *string                   `json:"re_addressed_at"`
}

type dossierTaxInvoiceMirror struct {
	Kind               string  `json:"kind"`
	Number             *string `json:"number"`
	Status             string  `json:"status"`
	RecipientLegalName string  `json:"recipient_legal_name"`
	RecipientTaxIDType string  `json:"recipient_tax_id_type"`
	RecipientTaxID     string  `json:"recipient_tax_id"`
}

// dossierSurroundingsByRef picks one Sale's surroundings off a raw Dossier,
// failing when the Sale is absent or a key is missing rather than null.
func dossierSurroundingsByRef(t *testing.T, raw json.RawMessage, ref string) dossierSaleSurroundings {
	t.Helper()
	var keyed struct {
		Sales []map[string]json.RawMessage `json:"sales"`
	}
	if err := json.Unmarshal(raw, &keyed); err != nil {
		t.Fatalf("decode raw dossier sales: %v", err)
	}
	for _, sale := range keyed.Sales {
		var got dossierSaleSurroundings
		encoded, _ := json.Marshal(sale)
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatalf("decode sale surroundings: %v", err)
		}
		if got.ConfirmationRef != ref {
			continue
		}
		for _, key := range []string{"phone", "affiliate_link_name", "tax_invoices", "re_addressed_at"} {
			if _, ok := sale[key]; !ok {
				t.Fatalf("sale %s carries no %q key: %s", ref, key, encoded)
			}
		}
		return got
	}
	t.Fatalf("sale %s is not on the Dossier: %s", ref, raw)
	return dossierSaleSurroundings{}
}

// dossierEventOfSale is the Event a Sale was made at. SQL because the fixture
// that makes the Sale hands back a reference and not the Event.
func dossierEventOfSale(t *testing.T, env *testEnv, ref string) string {
	t.Helper()
	var eventID string
	if err := env.db.QueryRow(`SELECT event_id FROM ticket_sales WHERE confirmation_ref = $1`, ref).Scan(&eventID); err != nil {
		t.Fatalf("read the event of %s: %v", ref, err)
	}
	return eventID
}

// The phone is the one given on THIS Event's checkout through PayPhone, read
// off its Payment (ADR 0073) — a later checkout at another Organization under
// another number does not change it, and that number is nowhere in the payload.
func TestTheDossierShowsThePhoneGivenOnThisEventsCheckout(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, adminSession, "Call Fest", "call-fest", 1000, 10)
	ref := paidCheckoutApproved(t, "call-fest", phoneCheckoutBody("pia@example.com", ecuadorMobileTyped, cartLine(gaID, 1)))
	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "pia@example.com", "active")

	_, before := readDossier(t, env, adminSession, eventID, customerID)
	if got := dossierSurroundingsByRef(t, before, ref); strOf(got.Phone) != ecuadorMobile {
		t.Fatalf("phone = %s, want the checkout's canonical %s", strOf(got.Phone), ecuadorMobile)
	}

	otherAdmin := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherAdmin, "Other Org", "other-org")
	_, otherGA := publishCheckoutEvent(t, env, otherAdmin, "Elsewhere Fest", "elsewhere-fest", 1000, 10)
	begun := beginCheckoutOK(t, payphoneEnv, "other-org", "elsewhere-fest",
		phoneCheckoutBody("pia@example.com", foreignMobile, cartLine(otherGA, 1)))
	if resp, body := confirmCheckoutParams(t, payphoneEnv, begun.ClientTransactionID, payphoneReturnParams(begun.ClientTransactionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("other-org confirm status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := readPaymentPhone(t, env, begun.ClientTransactionID); got == nil || *got != foreignMobile {
		t.Fatalf("the other checkout's phone = %v, want %s — the fixture proves nothing", got, foreignMobile)
	}
	// The Customer record now says the other number: a Dossier reading it
	// would have changed. SQL because no staff surface shows it.
	var customerPhone *string
	if err := env.db.QueryRow(`SELECT phone FROM customers WHERE id = $1`, customerID).Scan(&customerPhone); err != nil {
		t.Fatalf("read the customer's phone: %v", err)
	}
	if customerPhone == nil || *customerPhone != foreignMobile {
		t.Fatalf("the Customer record's phone = %v, want %s — the fixture proves nothing", customerPhone, foreignMobile)
	}

	_, after := readDossier(t, env, adminSession, eventID, customerID)
	if !bytes.Equal(after, before) {
		t.Errorf("the Dossier changed after a checkout at another Organization:\nbefore %s\nafter  %s", before, after)
	}
	if strings.Contains(string(after), foreignMobile) {
		t.Errorf("the other Organization's phone is on the Dossier: %s", after)
	}
}

// A Sale with no checkout behind it has no phone: a manually recorded Sale and
// an imported one both say null — never a blank, and never the phone the same
// Customer gave on an Online checkout of this very Event.
func TestASaleWithoutACheckoutHasNoPhoneOnTheDossier(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, adminSession, "Door Fest", "door-fest", 1000, 10)
	online := paidCheckoutApproved(t, "door-fest", phoneCheckoutBody("ana@example.com", ecuadorMobile, cartLine(gaID, 1)))
	recordManualSaleOK(t, env, adminSession, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "dossier-batch-1",
		"source":          "direct",
		"sales": []map[string]any{{
			"customer_email":      "ana@example.com",
			"customer_first_name": "Ana",
			"customer_last_name":  "Lopez",
			"ticket_type_id":      gaID,
			"quantity":            1,
			"payment_method":      "transfer",
			"sold_at":             "2026-07-02T10:00:00Z",
		}},
	}, authHeader(adminSession))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("sale import status=%d error=%+v", resp.StatusCode, body.Error)
	}
	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "active")

	dossier, raw := readDossier(t, env, adminSession, eventID, customerID)
	origins := map[string]bool{}
	for _, sale := range dossier.Sales {
		origins[sale.Origin] = true
		got := dossierSurroundingsByRef(t, raw, sale.ConfirmationRef)
		if sale.ConfirmationRef == online {
			if strOf(got.Phone) != ecuadorMobile {
				t.Errorf("online sale phone = %s, want %s", strOf(got.Phone), ecuadorMobile)
			}
			continue
		}
		if got.Phone != nil {
			t.Errorf("%s sale %s phone = %q, want null", sale.Origin, sale.ConfirmationRef, *got.Phone)
		}
	}
	if len(dossier.Sales) != 3 || !origins["manually_recorded"] {
		t.Fatalf("fixture: sales = %+v, want an online, a manually recorded and an imported Sale", dossier.Sales)
	}
}

// An attributed Online Sale names its Affiliate Link by display name; an
// unattributed one says null.
func TestAnAttributedSaleShowsItsAffiliateLinkOnTheDossier(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, adminSession, "Ref Fest", "ref-fest", 1000, 10)
	code := newAffiliateLink(t, env, adminSession, eventID, "María's Instagram")

	attributed := beginCheckoutOK(t, env, "test-org", "ref-fest",
		affiliateCheckoutBody("ana@example.com", lastClick(code), cartLine(gaID, 1)))
	attributedRef := confirmCheckoutOK(t, env, attributed.ClientTransactionID, "approved").ConfirmationRef
	direct := beginCheckoutOK(t, env, "test-org", "ref-fest", checkoutBody("ana@example.com", "Ana", "Buyer", cartLine(gaID, 1)))
	directRef := confirmCheckoutOK(t, env, direct.ClientTransactionID, "approved").ConfirmationRef
	if attributedRef == "" || directRef == "" {
		t.Fatalf("fixture: references %q / %q", attributedRef, directRef)
	}
	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "active")

	_, raw := readDossier(t, env, adminSession, eventID, customerID)
	if got := dossierSurroundingsByRef(t, raw, attributedRef); strOf(got.AffiliateLinkName) != "María's Instagram" {
		t.Errorf("attributed sale affiliate_link_name = %s, want María's Instagram", strOf(got.AffiliateLinkName))
	}
	if got := dossierSurroundingsByRef(t, raw, directRef); got.AffiliateLinkName != nil {
		t.Errorf("unattributed sale affiliate_link_name = %q, want null", *got.AffiliateLinkName)
	}
}

// A Sale with a Sale Invoice lists the document as the invoicing module states
// it: number, status, and the legal name and Tax ID invoiced. A Sale that owes
// nothing lists an empty array, never null.
func TestASaleInvoiceShowsOnTheDossier(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID, _ := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	eventID := dossierEventOfSale(t, env, ref)
	adminSession := verifyOTP(t, env, "admin@example.com")
	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "guest@example.com", "active")

	var listed *string
	for _, row := range getSaleInvoiceList(t, operatorSessionID).Data {
		if row.ID == invoiceID {
			listed = row.Number
		}
	}
	if listed == nil {
		t.Fatalf("fixture: the authorized Sale Invoice %s has no number on the list", invoiceID)
	}

	doorID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "Door", 500, 10)
	manual := recordManualSaleOK(t, env, adminSession, eventID,
		manualSaleBody("guest@example.com", "Ana", "Lopez", doorID, 1, "cash", "2026-07-01T10:00:00Z"))

	_, raw := readDossier(t, env, adminSession, eventID, customerID)
	got := dossierSurroundingsByRef(t, raw, ref)
	if len(got.TaxInvoices) != 1 {
		t.Fatalf("tax_invoices = %+v, want the one Sale Invoice", got.TaxInvoices)
	}
	doc := got.TaxInvoices[0]
	if doc.Kind != "sale" || doc.Status != "authorized" || strOf(doc.Number) != *listed ||
		doc.RecipientLegalName != "Ana Lopez" || doc.RecipientTaxIDType != "cedula" || doc.RecipientTaxID != validCedula {
		t.Errorf("sale invoice = %+v (number %s), want sale/authorized %s to Ana Lopez cedula %s", doc, strOf(doc.Number), *listed, validCedula)
	}
	if none := dossierSurroundingsByRef(t, raw, manual.ConfirmationRef); none.TaxInvoices == nil || len(none.TaxInvoices) != 0 {
		t.Errorf("a Sale owing nothing lists tax_invoices = %+v, want []", none.TaxInvoices)
	}
}

// A re-addressed Sale says when it was re-addressed, and nothing else about it:
// the address it moved from is nowhere in the payload, and the corrected
// address appears only once, as the Customer's own email — this is now their
// Dossier. While the re-addressing is pending the Sale says nothing and the
// payload never carries the address the Operator typed.
func TestAReAddressedSaleShowsWhenAndNeitherAddress(t *testing.T) {
	env := setupTest(t)
	const wrong, corrected = "pablo.mistyped@example.com", "pablo.meant@example.com"
	s := strandSale(t, env, "Moved Fest", "moved-fest", wrong, corrected, 1)

	ghostID := customerIDOnSalesList(t, env, s.orgSession, s.eventID, wrong, "active")
	_, pending := readDossier(t, env, s.orgSession, s.eventID, ghostID)
	if got := dossierSurroundingsByRef(t, pending, s.ref); got.ReAddressedAt != nil {
		t.Errorf("a pending re-addressing reads re_addressed_at = %s, want null", *got.ReAddressedAt)
	}
	if strings.Contains(string(pending), corrected) {
		t.Errorf("the pending corrected address is on the Dossier: %s", pending)
	}

	accepted := acceptReAddressingLinkOK(t, payphoneEnv, s.token)
	if accepted.AcceptedAt == nil {
		t.Fatalf("fixture: acceptance carries no accepted_at: %+v", accepted)
	}
	customerID := customerIDOnSalesList(t, env, s.orgSession, s.eventID, corrected, "active")
	dossier, raw := readDossier(t, env, s.orgSession, s.eventID, customerID)
	if dossier.Customer.Email != corrected {
		t.Fatalf("fixture: the Dossier's Customer is %s, want %s", dossier.Customer.Email, corrected)
	}
	got := dossierSurroundingsByRef(t, raw, s.ref)
	if got.ReAddressedAt == nil || !sameInstant(t, *got.ReAddressedAt, *accepted.AcceptedAt) {
		t.Errorf("re_addressed_at = %s, want the acceptance at %s", strOf(got.ReAddressedAt), *accepted.AcceptedAt)
	}

	body := string(raw)
	if strings.Contains(body, wrong) {
		t.Errorf("the address the Sale moved from is on the Dossier: %s", body)
	}
	if n := strings.Count(body, corrected); n != 1 {
		t.Errorf("the corrected address appears %d times, want once as customer.email: %s", n, body)
	}
	for _, forbidden := range []string{"previous_email", "corrected_email", "token", "operator_email", "operator@example.com"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the Dossier mentions %q: %s", forbidden, body)
		}
	}
}
