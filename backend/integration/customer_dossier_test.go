package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
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

// --- Tickets and Holders (#640) ----------------------------------------------
//
// Each Sale's Tickets and the Tickets a Customer holds on other buyers' Sales,
// every assignment field disclosed through catalog.DiscloseHolder (ADR 0047):
// an unaccepted assignment is a state and never a person.

// dossierHolders decodes the #640 part of a Dossier, beside dossierBody.
type dossierHolders struct {
	Sales []struct {
		ConfirmationRef          string          `json:"confirmation_ref"`
		Status                   string          `json:"status"`
		Tickets                  []dossierTicket `json:"tickets"`
		AssignmentReminderSentAt *[]time.Time    `json:"assignment_reminder_sent_at"`
	} `json:"sales"`
	HeldTickets *[]dossierHeldTicket `json:"held_tickets"`
}

type dossierTicket struct {
	TicketID         string `json:"ticket_id"`
	TicketTypeID     string `json:"ticket_type_id"`
	TicketTypeName   string `json:"ticket_type_name"`
	Ordinal          int    `json:"ordinal"`
	AssignmentState  string `json:"assignment_state"`
	NeverAccepted    bool   `json:"never_accepted"`
	SelfHeld         bool   `json:"self_held"`
	HolderCustomerID string `json:"holder_customer_id"`
	HolderFirstName  string `json:"holder_first_name"`
	HolderLastName   string `json:"holder_last_name"`
}

type dossierHeldTicket struct {
	TicketID        string     `json:"ticket_id"`
	TicketTypeName  string     `json:"ticket_type_name"`
	Ordinal         int        `json:"ordinal"`
	TicketSaleID    string     `json:"ticket_sale_id"`
	ConfirmationRef string     `json:"confirmation_ref"`
	SaleStatus      string     `json:"sale_status"`
	BuyerFirstName  string     `json:"buyer_first_name"`
	BuyerLastName   string     `json:"buyer_last_name"`
	AcceptedAt      *time.Time `json:"accepted_at"`
	HolderFirstName string     `json:"holder_first_name"`
	HolderLastName  string     `json:"holder_last_name"`
}

func decodeDossierHolders(t *testing.T, raw json.RawMessage) dossierHolders {
	t.Helper()
	var out dossierHolders
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode dossier tickets: %v", err)
	}
	return out
}

// dossierHolderFixture is one Event with Ticket Assignment open and one
// Manually Recorded Sale of four Tickets by Ana, whose Tickets stand in every
// state an Organization can be told about:
//
//	selfHeld  Ticket 1, Ana's own (ADR 0048)
//	accepted  Ticket 2, accepted by Carla Ruiz
//	assigned  Ticket 3, typed as diego@example.com and never accepted
//	purged    Ticket 4, named, never accepted, address purged (#334)
type dossierHolderFixture struct {
	session  string
	eventID  string
	saleID   string
	saleRef  string
	gaID     string
	ana      string
	carla    string
	selfHeld string
	accepted string
	assigned string
	purged   string
}

func newDossierHolderFixture(t *testing.T, env *testEnv) dossierHolderFixture {
	t.Helper()
	enableTicketAssignment(t)
	f := dossierHolderFixture{session: orgAdminSession(t, env)}
	f.eventID = createDraftEvent(t, env, f.session, "Holder Fest", "holder-fest")
	scheduleEvent(t, env, f.session, f.eventID, "Holder Fest", "holder-fest", env.fixedClock.Add(30*24*time.Hour))
	f.gaID = createTicketTypeWithCapacity(t, env, f.session, f.eventID, "GA", 1000, 50)
	sale := recordManualSaleOK(t, env, f.session, f.eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", f.gaID, 4, "cash", "2026-07-01T10:00:00Z"))
	f.saleID, f.saleRef = sale.SaleID, sale.ConfirmationRef
	tickets := ticketIDsOfSale(t, env, f.saleID)
	if len(tickets) != 4 {
		t.Fatalf("Tickets minted = %d, want 4", len(tickets))
	}
	f.selfHeld, f.accepted, f.assigned, f.purged = tickets[0], tickets[1], tickets[2], tickets[3]

	acceptHolderNamed(t, env, "ana@example.com", f.accepted, "carla@example.com", "Carla", "Ruiz")
	assignTicketOK(t, env, customerSignIn(t, env, "ana@example.com"), f.saleID, f.assigned, "diego@example.com")
	stageHolderAddressPurge(t, env, f.purged)

	f.ana = customerIDOnSalesList(t, env, f.session, f.eventID, "ana@example.com", "active")
	if err := env.db.QueryRow(`SELECT id FROM customers WHERE email = 'carla@example.com'`).Scan(&f.carla); err != nil {
		t.Fatalf("read Carla's Customer id: %v", err)
	}
	return f
}

func dossierTicketByID(t *testing.T, tickets []dossierTicket, ticketID string) dossierTicket {
	t.Helper()
	for _, ticket := range tickets {
		if ticket.TicketID == ticketID {
			return ticket
		}
	}
	t.Fatalf("Ticket %s is not on the Sale: %+v", ticketID, tickets)
	return dossierTicket{}
}

// A Sale's Tickets carry their Ticket Type, ordinal and assignment state; the
// accepted Holder is named and linkable, the buyer's Self-held Ticket is marked
// theirs, and an unaccepted assignment is a state with nobody behind it —
// nowhere in the body — while a purged one reads never-accepted.
func TestTheDossierListsEachSalesTicketsThroughTheDisclosureRule(t *testing.T) {
	env := setupTest(t)
	f := newDossierHolderFixture(t, env)

	_, raw := readDossier(t, env, f.session, f.eventID, f.ana)
	dossier := decodeDossierHolders(t, raw)
	if len(dossier.Sales) != 1 {
		t.Fatalf("sales = %+v, want Ana's one", dossier.Sales)
	}
	tickets := dossier.Sales[0].Tickets
	if len(tickets) != 4 {
		t.Fatalf("tickets = %+v, want the Sale's four", tickets)
	}
	for i, ticket := range tickets {
		if ticket.Ordinal != i+1 || ticket.TicketTypeName != "GA" || ticket.TicketTypeID == "" {
			t.Errorf("ticket %d = %+v, want GA ordinal %d in order", i, ticket, i+1)
		}
	}

	if got := dossierTicketByID(t, tickets, f.selfHeld); got.AssignmentState != "accepted" || !got.SelfHeld ||
		got.HolderCustomerID != "" || got.HolderFirstName != "" {
		t.Errorf("the Self-held Ticket reads %+v, want accepted and marked self_held, naming nobody else", got)
	}
	if got := dossierTicketByID(t, tickets, f.accepted); got.AssignmentState != "accepted" || got.SelfHeld ||
		got.HolderCustomerID != f.carla || got.HolderFirstName != "Carla" || got.HolderLastName != "Ruiz" {
		t.Errorf("Carla's Ticket reads %+v, want accepted by Carla Ruiz (%s)", got, f.carla)
	}
	if got := dossierTicketByID(t, tickets, f.assigned); got.AssignmentState != "assigned" || got.NeverAccepted ||
		got.HolderCustomerID != "" || got.HolderFirstName != "" || got.HolderLastName != "" {
		t.Errorf("the unaccepted Ticket reads %+v, want `assigned` and nobody", got)
	}
	if got := dossierTicketByID(t, tickets, f.purged); got.AssignmentState != "assigned" || !got.NeverAccepted ||
		got.HolderCustomerID != "" || got.HolderFirstName != "" {
		t.Errorf("the purged Ticket reads %+v, want `assigned` never_accepted and nobody", got)
	}
	if strings.Contains(strings.ToLower(string(raw)), "diego") {
		t.Errorf("an unaccepted Holder's address reaches the Dossier (ADR 0047): %s", raw)
	}
	if dossier.HeldTickets == nil || len(*dossier.HeldTickets) != 0 {
		t.Errorf("Ana's held_tickets = %v, want [] — her own Ticket belongs under her Sale", dossier.HeldTickets)
	}
	if sent := dossier.Sales[0].AssignmentReminderSentAt; sent == nil || len(*sent) != 0 {
		t.Errorf("assignment_reminder_sent_at = %v, want [] for a Sale nobody was reminded about", sent)
	}
}

// A Customer who bought nothing at this Event but accepted a Ticket there has a
// Dossier: the held Ticket with its buyer's name as given on that Sale. A
// person merely typed as a Holder has none.
func TestAHolderOnlyCustomersDossierListsTheHeldTicket(t *testing.T) {
	env := setupTest(t)
	f := newDossierHolderFixture(t, env)

	body, raw := readDossier(t, env, f.session, f.eventID, f.carla)
	if body.Customer.ID != f.carla || body.Customer.Email != "carla@example.com" || len(body.Sales) != 0 {
		t.Fatalf("Carla's Dossier = %+v, want her identity and no Sales", body)
	}
	dossier := decodeDossierHolders(t, raw)
	if dossier.HeldTickets == nil || len(*dossier.HeldTickets) != 1 {
		t.Fatalf("held_tickets = %v, want Carla's one", dossier.HeldTickets)
	}
	held := (*dossier.HeldTickets)[0]
	if held.TicketID != f.accepted || held.Ordinal != 2 || held.TicketTypeName != "GA" ||
		held.TicketSaleID != f.saleID || held.ConfirmationRef != f.saleRef || held.SaleStatus != "active" ||
		held.BuyerFirstName != "Ana" || held.BuyerLastName != "Lopez" || held.AcceptedAt == nil ||
		held.HolderFirstName != "Carla" || held.HolderLastName != "Ruiz" {
		t.Errorf("held ticket = %+v, want GA ticket 2 of Ana Lopez's %s, accepted by Carla Ruiz", held, f.saleRef)
	}

	// Diego was typed and never accepted: at this Event he is nobody.
	customerSignIn(t, env, "diego@example.com")
	var diego string
	if err := env.db.QueryRow(`SELECT id FROM customers WHERE email = 'diego@example.com'`).Scan(&diego); err != nil {
		t.Fatalf("read Diego's Customer id: %v", err)
	}
	if status, code := dossierStatus(t, env, f.session, f.eventID, diego); status != http.StatusNotFound || code != "CUSTOMER_NOT_FOUND" {
		t.Errorf("an unaccepted Holder's Dossier status=%d code=%s, want 404 CUSTOMER_NOT_FOUND", status, code)
	}
}

// A Holder whose Ticket was on a Sale later reversed is still found and told
// the Sale was reversed; the reversed Sale's own Tickets carry no live state.
func TestAHolderOnAReversedSaleIsFoundAndToldTheSaleWasReversed(t *testing.T) {
	env := setupTest(t)
	f := newDossierHolderFixture(t, env)
	reverseImportedSaleOK(t, env, f.session, f.eventID, f.saleID)

	_, raw := readDossier(t, env, f.session, f.eventID, f.carla)
	dossier := decodeDossierHolders(t, raw)
	if dossier.HeldTickets == nil || len(*dossier.HeldTickets) != 1 {
		t.Fatalf("held_tickets = %v, want Carla's Ticket on the reversed Sale", dossier.HeldTickets)
	}
	if held := (*dossier.HeldTickets)[0]; held.TicketID != f.accepted || held.SaleStatus != "reversed" || held.BuyerFirstName != "Ana" {
		t.Errorf("held ticket = %+v, want Ana's Ticket marked sale_status reversed", held)
	}

	_, anaRaw := readDossier(t, env, f.session, f.eventID, f.ana)
	ana := decodeDossierHolders(t, anaRaw)
	if len(ana.Sales) != 1 || ana.Sales[0].Status != "reversed" || len(ana.Sales[0].Tickets) != 4 {
		t.Fatalf("Ana's sales = %+v, want the reversed Sale with its four Tickets", ana.Sales)
	}
	var rawSales struct {
		Sales []struct {
			Tickets []map[string]json.RawMessage `json:"tickets"`
		} `json:"sales"`
	}
	if err := json.Unmarshal(anaRaw, &rawSales); err != nil {
		t.Fatalf("decode raw sales: %v", err)
	}
	for _, ticket := range rawSales.Sales[0].Tickets {
		for key := range ticket {
			switch key {
			case "ticket_id", "ticket_type_id", "ticket_type_name", "ordinal":
			default:
				t.Errorf("a reversed Sale's Ticket carries %q: %v — its Tickets are void", key, ticket)
			}
		}
	}
}

// Each Sale says when an Assignment Reminder was sent about it.
func TestTheDossierStatesWhenAnAssignmentReminderWasSentAboutASale(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)
	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep = %+v, want one reminder sent", result)
	}
	customerID := customerIDOnSalesList(t, env, f.staffSession, f.eventID, "ana@example.com", "active")

	_, raw := readDossier(t, env, f.staffSession, f.eventID, customerID)
	dossier := decodeDossierHolders(t, raw)
	if len(dossier.Sales) != 1 {
		t.Fatalf("sales = %+v, want Ana's one", dossier.Sales)
	}
	sent := dossier.Sales[0].AssignmentReminderSentAt
	if sent == nil || len(*sent) != 1 || !(*sent)[0].Equal(f.sweepAt) {
		t.Errorf("assignment_reminder_sent_at = %v, want the one send at %v", sent, f.sweepAt)
	}
}

// With TICKET_ASSIGNMENT_ENABLED closed the Dossier says nothing about
// assignment — no states, no Holders, no held Tickets, no reminders — while the
// Sales and the identity remain, and a Holder-only Customer is not found.
func TestTheDossierSaysNothingAboutAssignmentWhileTheFlagIsClosed(t *testing.T) {
	env := setupTest(t)
	f := newDossierHolderFixture(t, env)
	closeTicketAssignment(t)

	body, raw := readDossier(t, env, f.session, f.eventID, f.ana)
	if body.Customer.Email != "ana@example.com" || len(body.Sales) != 1 || body.Sales[0].CustomerFirstName != "Ana" {
		t.Fatalf("Dossier = %+v, want Ana's identity and Sale", body)
	}
	if tickets := decodeDossierHolders(t, raw).Sales[0].Tickets; len(tickets) != 4 {
		t.Errorf("tickets = %+v, want the Sale's four, types and ordinals only", tickets)
	}
	for _, forbidden := range []string{"assignment_state", "never_accepted", "self_held", "holder_", "held_tickets", "assignment_reminder", "Carla"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("the Dossier carries %q while TICKET_ASSIGNMENT_ENABLED is closed (ADR 0045): %s", forbidden, raw)
		}
	}
	if status, code := dossierStatus(t, env, f.session, f.eventID, f.carla); status != http.StatusNotFound || code != "CUSTOMER_NOT_FOUND" {
		t.Errorf("a Holder-only Customer's Dossier status=%d code=%s, want 404 while assignment is dark", status, code)
	}
}

// The Holder List carries the ids its Dossier links need: every row its buyer's
// Customer id, and an accepted row its Holder's — never an unaccepted one's.
func TestTheHolderListCarriesTheCustomerIdsItsDossierLinksNeed(t *testing.T) {
	env := setupTest(t)
	f := newDossierHolderFixture(t, env)

	resp, body := env.get(t, holderListPath(f.eventID), authHeader(f.session))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode holder list: %v", err)
	}
	if len(page.Data) != 4 {
		t.Fatalf("rows = %d, want Ana's four Tickets", len(page.Data))
	}
	for _, row := range page.Data {
		var ticketID, buyer, holder string
		_ = json.Unmarshal(row["ticket_id"], &ticketID)
		_ = json.Unmarshal(row["customer_id"], &buyer)
		rawHolder, present := row["holder_customer_id"]
		_ = json.Unmarshal(rawHolder, &holder)
		if buyer != f.ana {
			t.Errorf("row %s customer_id = %q, want Ana's %s", ticketID, buyer, f.ana)
		}
		switch ticketID {
		case f.accepted:
			if holder != f.carla {
				t.Errorf("Carla's row holder_customer_id = %q, want %s", holder, f.carla)
			}
		case f.selfHeld:
			if holder != f.ana {
				t.Errorf("the Self-held row holder_customer_id = %q, want Ana's own %s", holder, f.ana)
			}
		default:
			if present {
				t.Errorf("unaccepted row %s carries holder_customer_id %s (ADR 0047)", ticketID, rawHolder)
			}
		}
	}

	// And both links open.
	readDossier(t, env, f.session, f.eventID, f.carla)
	readDossier(t, env, f.session, f.eventID, f.ana)
}

// --- Answers, Outstanding Answers and Answer Reminders (#641) -----------------
//
// Each Ticket on the Dossier — on the buyer's own Sales and held on somebody
// else's — carries its Answers, its Outstanding Answers and when an Answer
// Reminder was last sent about it. Nothing is re-decided: the debt is the
// Holder List's own derivation and the Answers are the Answers dialog's, so the
// tests below read those surfaces for the same Tickets and demand equality.

// dossierTicketsByID indexes every Ticket on a Dossier, own and held, by id, as
// raw JSON objects so a test can say which keys are present at all.
func dossierTicketsByID(t *testing.T, raw json.RawMessage) map[string]map[string]json.RawMessage {
	t.Helper()
	var body struct {
		Sales []struct {
			Tickets []map[string]json.RawMessage `json:"tickets"`
		} `json:"sales"`
		HeldTickets []map[string]json.RawMessage `json:"held_tickets"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode dossier tickets: %v", err)
	}
	out := map[string]map[string]json.RawMessage{}
	add := func(ticket map[string]json.RawMessage) {
		var id string
		if err := json.Unmarshal(ticket["ticket_id"], &id); err != nil {
			t.Fatalf("decode ticket_id: %v", err)
		}
		out[id] = ticket
	}
	for _, sale := range body.Sales {
		for _, ticket := range sale.Tickets {
			add(ticket)
		}
	}
	for _, ticket := range body.HeldTickets {
		add(ticket)
	}
	return out
}

// dossierTicketRaw picks one Ticket's raw object off a Dossier.
func dossierTicketRaw(t *testing.T, raw json.RawMessage, ticketID string) map[string]json.RawMessage {
	t.Helper()
	ticket, ok := dossierTicketsByID(t, raw)[ticketID]
	if !ok {
		t.Fatalf("Ticket %s is not on the Dossier: %s", ticketID, raw)
	}
	return ticket
}

// assertSameJSON compares two JSON values semantically.
func assertSameJSON(t *testing.T, what string, got, want json.RawMessage) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s: decode got %s: %v", what, got, err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("%s: decode want %s: %v", what, want, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s:\n got %s\nwant %s", what, got, want)
	}
}

// answeredPairsFromTheDialog is what the staff Answers dialog says one Ticket
// has answered: its question/Answer pairs with an Answer, in the dialog's order.
func answeredPairsFromTheDialog(t *testing.T, env *testEnv, session, eventID, ticketID string) json.RawMessage {
	t.Helper()
	resp, body := env.get(t, ticketPath(eventID, ticketID), authHeader(session))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read Ticket %s status=%d error=%+v", ticketID, resp.StatusCode, body.Error)
	}
	var ticket struct {
		Questions []map[string]json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(body.Data, &ticket); err != nil {
		t.Fatalf("decode Ticket answers: %v", err)
	}
	answered := make([]map[string]json.RawMessage, 0)
	for _, pair := range ticket.Questions {
		if string(pair["answer"]) != "null" {
			answered = append(answered, pair)
		}
	}
	out, err := json.Marshal(answered)
	if err != nil {
		t.Fatalf("encode answered pairs: %v", err)
	}
	return out
}

// outstandingFromTheHolderList is what the Holder List says one Ticket owes.
func outstandingFromTheHolderList(t *testing.T, env *testEnv, session, eventID, ticketID string) json.RawMessage {
	t.Helper()
	resp, body := env.get(t, holderListPath(eventID), authHeader(session))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode holder list: %v", err)
	}
	for _, row := range page.Data {
		var id string
		_ = json.Unmarshal(row["ticket_id"], &id)
		if id == ticketID {
			return row["outstanding"]
		}
	}
	t.Fatalf("Ticket %s is not on the Holder List", ticketID)
	return nil
}

// dossierQuestionsFixture opens Ticket Questions on the holder fixture and asks
// two required questions and one optional one of its GA Tickets.
type dossierQuestionsFixture struct {
	dossierHolderFixture
	size     string
	diet     string
	optional string
}

func newDossierQuestionsFixture(t *testing.T, env *testEnv) dossierQuestionsFixture {
	t.Helper()
	f := dossierQuestionsFixture{dossierHolderFixture: newDossierHolderFixture(t, env)}
	enableTicketQuestions(t)
	f.size = createTicketQuestion(t, env, f.session, f.eventID, f.gaID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	}).ID
	f.diet = createTicketQuestion(t, env, f.session, f.eventID, f.gaID, map[string]any{
		"label": "Diet", "kind": "short_text", "required": true,
	}).ID
	f.optional = createTicketQuestion(t, env, f.session, f.eventID, f.gaID, map[string]any{
		"label": "Anything else", "kind": "short_text",
	}).ID
	return f
}

// Every Ticket's Answers are the Answers dialog's answered pairs, and its
// Outstanding Answers are exactly what the Holder List says it owes.
func TestTheDossierShowsEachTicketsAnswersAndDebtsAsTheHolderListDoes(t *testing.T) {
	env := setupTest(t)
	f := newDossierQuestionsFixture(t, env)
	putAnswer(t, env, f.session, f.eventID, f.selfHeld, f.size, map[string]any{"text": "M"})
	putAnswer(t, env, f.session, f.eventID, f.selfHeld, f.optional, map[string]any{"text": "Vegan friend"})
	putAnswer(t, env, f.session, f.eventID, f.assigned, f.diet, map[string]any{"text": "None"})

	_, raw := readDossier(t, env, f.session, f.eventID, f.ana)
	for _, ticketID := range []string{f.selfHeld, f.accepted, f.assigned, f.purged} {
		ticket := dossierTicketRaw(t, raw, ticketID)
		assertSameJSON(t, "answers of "+ticketID, ticket["answers"],
			answeredPairsFromTheDialog(t, env, f.session, f.eventID, ticketID))
		assertSameJSON(t, "outstanding_answers of "+ticketID, ticket["outstanding_answers"],
			outstandingFromTheHolderList(t, env, f.session, f.eventID, ticketID))
	}

	selfHeld := dossierTicketRaw(t, raw, f.selfHeld)
	var answers []struct {
		Question struct {
			Label string `json:"label"`
		} `json:"question"`
		Answer struct {
			Text string `json:"text"`
		} `json:"answer"`
	}
	if err := json.Unmarshal(selfHeld["answers"], &answers); err != nil {
		t.Fatalf("decode answers: %v", err)
	}
	if len(answers) != 2 || answers[0].Question.Label != "T-shirt size" || answers[0].Answer.Text != "M" ||
		answers[1].Answer.Text != "Vegan friend" {
		t.Errorf("the Self-held Ticket's answers = %+v, want size M and the optional note", answers)
	}
	var owed []struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal(selfHeld["outstanding_answers"], &owed); err != nil {
		t.Fatalf("decode outstanding_answers: %v", err)
	}
	if len(owed) != 1 || owed[0].Label != "Diet" {
		t.Errorf("the Self-held Ticket owes %+v, want Diet alone", owed)
	}
	if got := string(dossierTicketRaw(t, raw, f.accepted)["answers"]); got != "[]" {
		t.Errorf("an unanswered Ticket's answers = %s, want []", got)
	}
	if got := string(selfHeld["last_answer_reminder_sent_at"]); got != "null" {
		t.Errorf("last_answer_reminder_sent_at = %s, want null before any reminder", got)
	}
}

// A Ticket says when an Answer Reminder was last sent about it, and a Ticket
// nobody was chased about says null.
func TestTheDossierStatesWhenAnAnswerReminderWasLastSentAboutATicket(t *testing.T) {
	env := setupTest(t)
	f := newDossierQuestionsFixture(t, env)

	first := env.fixedClock
	moveClockTo(t, first)
	if result := sweepAnswerReminders(t, env); result.Sent == 0 {
		t.Fatalf("first sweep = %+v, want reminders sent", result)
	}
	second := first.Add(8 * 24 * time.Hour)
	moveClockTo(t, second)
	if result := sweepAnswerReminders(t, env); result.Sent == 0 {
		t.Fatalf("second sweep = %+v, want reminders sent", result)
	}

	for _, tc := range []struct {
		customer, ticketID string
		want               *time.Time
	}{
		{f.ana, f.selfHeld, &second},
		{f.carla, f.accepted, &second},
		{f.ana, f.assigned, nil},
		{f.ana, f.purged, nil},
	} {
		_, raw := readDossier(t, env, f.session, f.eventID, tc.customer)
		value, present := dossierTicketRaw(t, raw, tc.ticketID)["last_answer_reminder_sent_at"]
		if !present {
			t.Errorf("Ticket %s carries no last_answer_reminder_sent_at", tc.ticketID)
			continue
		}
		var got *time.Time
		if err := json.Unmarshal(value, &got); err != nil {
			t.Fatalf("decode last_answer_reminder_sent_at: %v", err)
		}
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("Ticket %s last reminded %v, want null — nobody was chased about it", tc.ticketID, got)
		case tc.want != nil && (got == nil || !got.Equal(*tc.want)):
			t.Errorf("Ticket %s last reminded %v, want the later send %v", tc.ticketID, got, *tc.want)
		}
	}
}

// A reversed Sale's Tickets keep their Answers readable and owe nothing: no
// Outstanding Answers and no reminder time, on the buyer's Sale and the
// Holder's held Ticket alike.
func TestAReversedSalesTicketsKeepTheirAnswersAndOweNothing(t *testing.T) {
	env := setupTest(t)
	f := newDossierQuestionsFixture(t, env)
	putAnswer(t, env, f.session, f.eventID, f.selfHeld, f.size, map[string]any{"text": "L"})
	putAnswer(t, env, f.session, f.eventID, f.accepted, f.size, map[string]any{"text": "S"})
	want := map[string]json.RawMessage{
		f.selfHeld: answeredPairsFromTheDialog(t, env, f.session, f.eventID, f.selfHeld),
		f.accepted: answeredPairsFromTheDialog(t, env, f.session, f.eventID, f.accepted),
	}
	reverseImportedSaleOK(t, env, f.session, f.eventID, f.saleID)

	_, anaRaw := readDossier(t, env, f.session, f.eventID, f.ana)
	_, carlaRaw := readDossier(t, env, f.session, f.eventID, f.carla)
	for _, tc := range []struct {
		name     string
		raw      json.RawMessage
		ticketID string
	}{
		{"Ana's Self-held Ticket", anaRaw, f.selfHeld},
		{"Carla's held Ticket", carlaRaw, f.accepted},
	} {
		ticket := dossierTicketRaw(t, tc.raw, tc.ticketID)
		assertSameJSON(t, tc.name+" answers", ticket["answers"], want[tc.ticketID])
		for _, key := range []string{"outstanding_answers", "last_answer_reminder_sent_at"} {
			if value, present := ticket[key]; present {
				t.Errorf("%s on a reversed Sale carries %s = %s — nothing is owed", tc.name, key, value)
			}
		}
	}
	for id, ticket := range dossierTicketsByID(t, anaRaw) {
		if _, present := ticket["answers"]; !present {
			t.Errorf("reversed Ticket %s carries no answers key", id)
		}
	}
}

// An Event Owner reads what a Ticket owes, gives the Answers through the staff
// Answers endpoint, and the next Dossier read shows the debt cleared.
func TestAnEventOwnerGivesAnAnswerAndTheDossierShowsTheDebtCleared(t *testing.T) {
	env := setupTest(t)
	f := newDossierQuestionsFixture(t, env)
	owner := addOrgMember(t, env, f.session, "owner@example.com", "event_owner")

	_, before := readDossier(t, env, owner, f.eventID, f.ana)
	var owed []struct {
		QuestionID string `json:"question_id"`
	}
	if err := json.Unmarshal(dossierTicketRaw(t, before, f.selfHeld)["outstanding_answers"], &owed); err != nil {
		t.Fatalf("decode outstanding_answers: %v", err)
	}
	if len(owed) != 2 {
		t.Fatalf("the Self-held Ticket owes %+v, want both required questions", owed)
	}

	putAnswer(t, env, owner, f.eventID, f.selfHeld, f.size, map[string]any{"text": "XL"})
	putAnswer(t, env, owner, f.eventID, f.selfHeld, f.diet, map[string]any{"text": "None"})

	_, after := readDossier(t, env, owner, f.eventID, f.ana)
	ticket := dossierTicketRaw(t, after, f.selfHeld)
	if got := string(ticket["outstanding_answers"]); got != "[]" {
		t.Errorf("outstanding_answers after answering = %s, want []", got)
	}
	if !strings.Contains(string(ticket["answers"]), `"XL"`) {
		t.Errorf("answers after answering = %s, want the XL just given", ticket["answers"])
	}
}

// With TICKET_QUESTIONS_ENABLED closed no Ticket carries Answers, Outstanding
// Answers or a reminder time, and the rest of the Dossier is exactly what it
// was with the flag open, less those three keys.
func TestTheDossierSaysNothingAboutAnswersWhileTheQuestionsFlagIsClosed(t *testing.T) {
	env := setupTest(t)
	f := newDossierQuestionsFixture(t, env)
	putAnswer(t, env, f.session, f.eventID, f.selfHeld, f.size, map[string]any{"text": "M"})
	putAnswer(t, env, f.session, f.eventID, f.accepted, f.size, map[string]any{"text": "S"})

	for _, customer := range []string{f.ana, f.carla} {
		_, open := readDossier(t, env, f.session, f.eventID, customer)
		sharedApp.CatalogService.WithTicketQuestions(false)
		sharedApp.SalesService.WithTicketQuestions(false)
		_, closed := readDossier(t, env, f.session, f.eventID, customer)
		enableTicketQuestions(t)

		for _, forbidden := range []string{"answers", "last_answer_reminder_sent_at", "T-shirt size"} {
			if strings.Contains(string(closed), forbidden) {
				t.Errorf("the Dossier carries %q while TICKET_QUESTIONS_ENABLED is closed (ADR 0045): %s", forbidden, closed)
			}
		}
		var withFlag, withoutFlag map[string]any
		if err := json.Unmarshal(open, &withFlag); err != nil {
			t.Fatalf("decode open dossier: %v", err)
		}
		if err := json.Unmarshal(closed, &withoutFlag); err != nil {
			t.Fatalf("decode closed dossier: %v", err)
		}
		stripAnswerKeys(withFlag)
		if !reflect.DeepEqual(withFlag, withoutFlag) {
			t.Errorf("closing the questions flag changed more than the Answers:\nopen   %s\nclosed %s", open, closed)
		}
	}
}

// stripAnswerKeys removes the three #641 keys from every Ticket of a decoded
// Dossier, own and held.
func stripAnswerKeys(dossier map[string]any) {
	strip := func(tickets any) {
		list, _ := tickets.([]any)
		for _, ticket := range list {
			if object, ok := ticket.(map[string]any); ok {
				delete(object, "answers")
				delete(object, "outstanding_answers")
				delete(object, "last_answer_reminder_sent_at")
			}
		}
	}
	sales, _ := dossier["sales"].([]any)
	for _, sale := range sales {
		if object, ok := sale.(map[string]any); ok {
			strip(object["tickets"])
		}
	}
	strip(dossier["held_tickets"])
}

// A Holder-only Customer's Answers hang off the Ticket they hold: Carla's
// Dossier shows what her Ticket answered and still owes.
func TestAHeldTicketOnTheDossierShowsItsAnswers(t *testing.T) {
	env := setupTest(t)
	f := newDossierQuestionsFixture(t, env)
	putAnswer(t, env, f.session, f.eventID, f.accepted, f.diet, map[string]any{"text": "Vegetarian"})

	_, raw := readDossier(t, env, f.session, f.eventID, f.carla)
	held := dossierTicketRaw(t, raw, f.accepted)
	assertSameJSON(t, "Carla's answers", held["answers"],
		answeredPairsFromTheDialog(t, env, f.session, f.eventID, f.accepted))
	assertSameJSON(t, "Carla's outstanding_answers", held["outstanding_answers"],
		outstandingFromTheHolderList(t, env, f.session, f.eventID, f.accepted))
	if !strings.Contains(string(held["answers"]), `"Vegetarian"`) {
		t.Errorf("Carla's held Ticket answers = %s, want Vegetarian", held["answers"])
	}
	if !strings.Contains(string(held["outstanding_answers"]), `"T-shirt size"`) {
		t.Errorf("Carla's held Ticket owes %s, want the T-shirt size", held["outstanding_answers"])
	}
	if value, present := held["last_answer_reminder_sent_at"]; !present || string(value) != "null" {
		t.Errorf("Carla's last_answer_reminder_sent_at = %s (present %v), want null", value, present)
	}
}
