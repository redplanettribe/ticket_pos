package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The Platform Operator's surface (issue #95): the Operator Dashboard's reads —
// every Organization with its Withdrawable Balance, one Organization's Events
// and payout history, and the platform's own revenue per currency — plus its one
// write, recording a Payout (ADR 0015).
//
// Authority here is an email on the allowlist and nothing else: no Membership,
// no Active Member, no role. The tests grant it the way production does, with a
// direct INSERT, because that IS the interface — there is deliberately no API
// for managing operators.

// sessionView is the staff session payload, as much of it as these tests read:
// the operator flag the staff app renders its navigation from, and the
// Membership fields that must stay empty for a pure operator.
type sessionView struct {
	Email              string `json:"email"`
	IsPlatformOperator bool   `json:"is_platform_operator"`
	ActiveMember       *struct {
		OrganizationSlug string `json:"organization_slug"`
		Role             string `json:"role"`
	} `json:"active_member"`
	Memberships []struct {
		OrganizationSlug string `json:"organization_slug"`
	} `json:"memberships"`
}

type operatorCurrencyTotals struct {
	Currency         string `json:"currency"`
	PlatformFeeCents int    `json:"platform_fee_cents"`
	FeeIVACents      int    `json:"fee_iva_cents"`
	TotalOwedCents   int    `json:"total_owed_cents"`
	// How much of the two fee figures above stands on reversed sales because an
	// Operator Reversal said the platform kept its commission (#127). Zero
	// unless that has happened, and the Operator Dashboard stays silent then.
	KeptFeeCents    int `json:"kept_fee_cents"`
	KeptFeeIVACents int `json:"kept_fee_iva_cents"`
}

type operatorSummary struct {
	Totals []operatorCurrencyTotals `json:"totals"`
}

type operatorOrganizationRow struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	Slug                     string `json:"slug"`
	Currency                 string `json:"currency"`
	EventsCount              int    `json:"events_count"`
	WithdrawableBalanceCents int    `json:"withdrawable_balance_cents"`
}

type operatorPagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type operatorOrganizationList struct {
	Data       []operatorOrganizationRow `json:"data"`
	Pagination operatorPagination        `json:"pagination"`
}

type operatorEvent struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	StartsAt     *string `json:"starts_at"`
	Discoverable bool    `json:"discoverable"`
}

type operatorPayout struct {
	ID          string  `json:"id"`
	AmountCents int     `json:"amount_cents"`
	PaidAt      string  `json:"paid_at"`
	Note        *string `json:"note"`
	RecordedBy  *string `json:"recorded_by"`
	CreatedAt   string  `json:"created_at"`
}

type operatorOrganizationDetail struct {
	Organization struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Slug      string `json:"slug"`
		Currency  string `json:"currency"`
		CreatedAt string `json:"created_at"`
	} `json:"organization"`
	WithdrawableBalanceCents int `json:"withdrawable_balance_cents"`
	// PayableBalanceCents is what the Organization may ask for today (#174,
	// ADR 0026). Exercised in payable_balance_test.go.
	PayableBalanceCents int              `json:"payable_balance_cents"`
	Events              []operatorEvent  `json:"events"`
	Payouts             []operatorPayout `json:"payouts"`
	// PayoutRequests is this Organization's own request history, newest first,
	// beside the payout history it answers "are they asking again?" against
	// (#176, ADR 0026). Exercised in operator_payout_requests_test.go.
	PayoutRequests []operatorPayoutRequestRow `json:"payout_requests"`
}

// seedPlatformOperator grants operator authority the only way it is ever
// granted: a row in the allowlist, written straight to the database. There is
// no API for it by design (ADR 0015), so this is the production contract rather
// than a test backdoor.
func seedPlatformOperator(t *testing.T, env *testEnv, email string) {
	t.Helper()
	if _, err := env.db.Exec(
		`INSERT INTO platform_operators (email) VALUES ($1) ON CONFLICT (email) DO NOTHING`,
		email,
	); err != nil {
		t.Fatalf("seed platform operator: %v", err)
	}
}

// operatorSession signs an email in the ordinary staff way and puts it on the
// allowlist. It belongs to no Organization and selects no Active Member.
func operatorSession(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	seedPlatformOperator(t, env, email)
	return verifyOTP(t, env, email)
}

func operatorGetOK(t *testing.T, env *testEnv, sessionID, path string, out any) {
	t.Helper()
	resp, body := env.get(t, path, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d error=%+v", path, resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("GET %s returned error %+v", path, body.Error)
	}
	if err := json.Unmarshal(body.Data, out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func operatorOrganizations(t *testing.T, env *testEnv, sessionID string) operatorOrganizationList {
	t.Helper()
	var list operatorOrganizationList
	operatorGetOK(t, env, sessionID, "/api/v1/operator/organizations", &list)
	return list
}

func operatorOrganizationByName(t *testing.T, list operatorOrganizationList, name string) operatorOrganizationRow {
	t.Helper()
	for _, row := range list.Data {
		if row.Name == name {
			return row
		}
	}
	t.Fatalf("organization %q missing from the operator list %+v", name, list.Data)
	return operatorOrganizationRow{}
}

// operatorRoutes is every route on the operator namespace, used by the access
// tests: the gate is a property of the namespace, not of one endpoint.
func operatorRoutes(orgID string) []struct {
	method string
	path   string
} {
	return []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/operator/summary"},
		{http.MethodGet, "/api/v1/operator/organizations"},
		{http.MethodGet, "/api/v1/operator/organizations/" + orgID},
		{http.MethodPost, "/api/v1/operator/organizations/" + orgID + "/payouts"},
		// The House Organization designation and its clearing (#472,
		// ADR 0060): the dashboard's first Organization writes after the
		// Payout. Exercised in operator_house_organization_test.go.
		{http.MethodPut, "/api/v1/operator/organizations/" + orgID + "/house"},
		{http.MethodDelete, "/api/v1/operator/organizations/" + orgID + "/house"},
		// The Tax invoicing surface (#451, ADR 0059) is on the same namespace
		// behind the same gate; its PUT is asserted in invoicing_test.go.
		{http.MethodGet, "/api/v1/operator/invoicing/issuers/ec"},
		// The documents that need an operator (#477): the queue and its count.
		// Mark annulled is asserted in operator_document_attention_test.go.
		{http.MethodGet, "/api/v1/operator/invoicing/needs-attention"},
		{http.MethodGet, "/api/v1/operator/invoicing/needs-attention/count"},
		// The Legal Center (#561): the platform's own agreements. The gate is a
		// property of the namespace, so its three calls are asserted here rather
		// than again in operator_legal_center_test.go — which is also why the
		// PUT and DELETE belong on this list even though only the GETs are
		// followed through to a 200.
		{http.MethodGet, "/api/v1/operator/legal/documents/policy"},
		{http.MethodPut, "/api/v1/operator/legal/documents/policy/draft"},
		{http.MethodDelete, "/api/v1/operator/legal/documents/policy/draft"},
	}
}

func (env *testEnv) operatorCall(t *testing.T, method, path, sessionID string) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if sessionID != "" {
		headers = authHeader(sessionID)
	}
	switch method {
	case http.MethodPost:
		return env.post(t, path, map[string]any{"amount_cents": 100, "paid_at": "2026-07-01"}, headers)
	case http.MethodPut:
		return env.put(t, path, nil, headers)
	case http.MethodDelete:
		return env.deleteJSON(t, path, nil, headers)
	}
	return env.get(t, path, headers)
}

// TestOperatorSurfaceRequiresAnAllowlistedEmail: no session is 401, a signed-in
// non-operator is 403 even as an Org Admin — org roles grant nothing
// platform-wide — and an allowlisted session with no Membership at all is
// served.
func TestOperatorSurfaceRequiresAnAllowlistedEmail(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")

	for _, route := range operatorRoutes(orgID) {
		resp, body := env.operatorCall(t, route.method, route.path, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s unauthenticated status=%d, want 401; error=%+v", route.method, route.path, resp.StatusCode, body.Error)
		}
		if body.Error == nil || body.Error.Code != "UNAUTHORIZED" {
			t.Fatalf("%s %s unauthenticated error=%+v, want UNAUTHORIZED", route.method, route.path, body.Error)
		}
	}

	// An Org Admin of a real Organization, but not on the allowlist.
	for _, route := range operatorRoutes(orgID) {
		resp, body := env.operatorCall(t, route.method, route.path, adminSessionID)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s %s as org_admin status=%d, want 403; error=%+v", route.method, route.path, resp.StatusCode, body.Error)
		}
		if body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s %s as org_admin error=%+v, want FORBIDDEN", route.method, route.path, body.Error)
		}
	}

	// A pure operator: allowlisted, Member of nothing, no Active Member on the
	// session. Membership is not required and never was.
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	var session sessionView
	operatorGetOK(t, env, operatorSessionID, "/api/v1/auth/session", &session)
	if session.ActiveMember != nil || len(session.Memberships) != 0 {
		t.Fatalf("operator session = %+v; want no Membership and no Active Member", session)
	}
	for _, route := range operatorRoutes(orgID) {
		if route.method != http.MethodGet {
			continue
		}
		resp, body := env.operatorCall(t, route.method, route.path, operatorSessionID)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s %s as operator status=%d, want 200; error=%+v", route.method, route.path, resp.StatusCode, body.Error)
		}
	}
}

// TestStaffSessionCarriesTheOperatorFlag: the staff session says whether it may
// see the Operator Dashboard, so the app knows whether to render its navigation.
// The flag is a hint — the middleware above is the gate — and it is independent
// of Membership.
func TestStaffSessionCarriesTheOperatorFlag(t *testing.T) {
	env := setupTest(t)

	adminSessionID := orgAdminSession(t, env)
	var adminSession sessionView
	operatorGetOK(t, env, adminSessionID, "/api/v1/auth/session", &adminSession)
	if adminSession.IsPlatformOperator {
		t.Fatalf("org_admin session is_platform_operator = true; org roles grant no operator authority")
	}

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	var operator sessionView
	operatorGetOK(t, env, operatorSessionID, "/api/v1/auth/session", &operator)
	if !operator.IsPlatformOperator {
		t.Fatalf("allowlisted session is_platform_operator = false; want true")
	}
}

// operatorLedgerFixture stages the platform as an operator would find it:
//
//   - "Test Org" sells 2 tickets online under pass_on — 2 × 799¢ of Net
//     Proceeds, and 2 × (80¢ fee + 12¢ Fee IVA) withheld for the platform.
//   - "Other Org" sells 1, is settled in full, and then has that sale reversed:
//     a Withdrawable Balance of −799¢, and no fee revenue left behind either.
//   - "Demo Venue", the pre-seeded Organization, trades not at all.
func operatorLedgerFixture(t *testing.T, env *testEnv) (adminSessionID, otherSessionID string) {
	t.Helper()

	adminSessionID = orgAdminSession(t, env)
	_, ga := publishCheckoutEvent(t, env, adminSessionID, "Ledger Fest", "ledger-fest", feeTestBaseCents, 10)
	begin := beginCheckoutOK(t, env, "test-org", "ledger-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": ga, "quantity": 2}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	otherSessionID = verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	_, otherGA := publishCheckoutEvent(t, env, otherSessionID, "Reversal Fest", "reversal-fest", feeTestBaseCents, 10)
	otherBegin := beginCheckoutOK(t, env, "other-org", "reversal-fest",
		checkoutBody("bea@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": otherGA, "quantity": 1}))
	otherSale := confirmCheckoutOK(t, env, otherBegin.ClientTransactionID, "approved")
	recordPayout(t, env, "other-org", feeTestBaseCents, "2026-07-02", "settled in full")
	reverseSale(t, env, otherSale.ConfirmationRef)

	return adminSessionID, otherSessionID
}

// TestOperatorOrganizationListShowsSignedBalances: every Organization on the
// platform, name-ascending, with its Event count and what the platform owes it —
// negative where an Organization owes the platform after a post-settlement
// reversal.
func TestOperatorOrganizationListShowsSignedBalances(t *testing.T) {
	env := setupTest(t)
	operatorLedgerFixture(t, env)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	list := operatorOrganizations(t, env, operatorSessionID)
	names := make([]string, 0, len(list.Data))
	for _, row := range list.Data {
		names = append(names, row.Name)
	}
	if len(names) != 3 || names[0] != "Demo Venue" || names[1] != "Other Org" || names[2] != "Test Org" {
		t.Fatalf("organization order = %v; want every Organization name-ascending", names)
	}
	if list.Pagination != (operatorPagination{Page: 1, PageSize: 50, Total: 3, TotalPages: 1}) {
		t.Fatalf("pagination = %+v; want the ADR-0006 defaults over 3 Organizations", list.Pagination)
	}

	testOrg := operatorOrganizationByName(t, list, "Test Org")
	if testOrg.Slug != "test-org" || testOrg.Currency != "USD" || testOrg.EventsCount != 1 {
		t.Fatalf("Test Org row = %+v; want slug/currency and its 1 Event", testOrg)
	}
	if want := 2 * feeTestBaseCents; testOrg.WithdrawableBalanceCents != want {
		t.Fatalf("Test Org balance = %d; want its Net Proceeds %d", testOrg.WithdrawableBalanceCents, want)
	}

	// Paid out in full, then reversed: the Organization owes the platform, and
	// the list says so rather than clamping at zero.
	otherOrg := operatorOrganizationByName(t, list, "Other Org")
	if otherOrg.WithdrawableBalanceCents != -feeTestBaseCents {
		t.Fatalf("Other Org balance = %d; want the negative %d", otherOrg.WithdrawableBalanceCents, -feeTestBaseCents)
	}

	// An Organization that has never traded is a zero, not an omission.
	demo := operatorOrganizationByName(t, list, "Demo Venue")
	if demo.EventsCount != 0 || demo.WithdrawableBalanceCents != 0 {
		t.Fatalf("Demo Venue row = %+v; want no Events and a zero balance", demo)
	}

	// The page is clamped and floored per ADR-0006, and paging walks the same
	// name-ascending order.
	var page2 operatorOrganizationList
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations?page=2&page_size=2", &page2)
	if page2.Pagination != (operatorPagination{Page: 2, PageSize: 2, Total: 3, TotalPages: 2}) {
		t.Fatalf("page 2 pagination = %+v", page2.Pagination)
	}
	if len(page2.Data) != 1 || page2.Data[0].Name != "Test Org" {
		t.Fatalf("page 2 rows = %+v; want the last Organization by name", page2.Data)
	}
}

// TestOperatorSummaryTotalsPerCurrency: the platform's own money — what it has
// earned in Platform Fees and Fee IVA, and what it owes — grouped by currency,
// never summed across currencies and never netted against a negative balance.
func TestOperatorSummaryTotalsPerCurrency(t *testing.T) {
	env := setupTest(t)
	operatorLedgerFixture(t, env)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	var summary operatorSummary
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/summary", &summary)
	if len(summary.Totals) != 1 {
		t.Fatalf("totals = %+v; want one row while USD is the only currency", summary.Totals)
	}

	// Only the surviving sale's snapshots count: the reversed sale left no fee
	// revenue behind, and its Organization's negative balance is excluded from
	// what the platform owes.
	want := operatorCurrencyTotals{
		Currency:         "USD",
		PlatformFeeCents: 2 * feeTestFeeCents,
		FeeIVACents:      2 * feeTestIVACents,
		TotalOwedCents:   2 * feeTestBaseCents,
	}
	if summary.Totals[0] != want {
		t.Fatalf("totals = %+v; want %+v", summary.Totals[0], want)
	}
}

// TestOperatorOrganizationDetail: one Organization's Events in every status and
// its full payout history, newest first, with the recorder shown as null for the
// Payouts that predate the Operator Dashboard.
func TestOperatorOrganizationDetail(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")

	publishedID, ga := publishCheckoutEvent(t, env, adminSessionID, "Detail Fest", "detail-fest", feeTestBaseCents, 10)
	resp, body := env.put(t, "/api/v1/staff/events/"+publishedID+"/discoverable",
		map[string]any{"discoverable": true}, authHeader(adminSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set discoverable status=%d error=%+v", resp.StatusCode, body.Error)
	}
	// A draft Event with no schedule: the operator sees it too, and it sorts last.
	draftID := createDraftEvent(t, env, adminSessionID, "Draft Fest", "draft-fest")

	begin := beginCheckoutOK(t, env, "test-org", "detail-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": ga, "quantity": 1}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// A Payout from before the Operator Dashboard existed: recorded by hand, so
	// nobody's email is attached to it.
	recordPayout(t, env, "test-org", 200, "2026-07-05", "")

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	var detail operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &detail)

	if detail.Organization.ID != orgID || detail.Organization.Slug != "test-org" ||
		detail.Organization.Name != "Test Org" || detail.Organization.Currency != "USD" ||
		detail.Organization.CreatedAt == "" {
		t.Fatalf("organization = %+v", detail.Organization)
	}
	if want := feeTestBaseCents - 200; detail.WithdrawableBalanceCents != want {
		t.Fatalf("balance = %d; want %d", detail.WithdrawableBalanceCents, want)
	}

	if len(detail.Events) != 2 {
		t.Fatalf("events = %+v; want the published and the draft Event", detail.Events)
	}
	if detail.Events[0].ID != publishedID || detail.Events[0].Status != "published" ||
		detail.Events[0].StartsAt == nil || !detail.Events[0].Discoverable {
		t.Fatalf("first event = %+v; want the scheduled published Event", detail.Events[0])
	}
	if detail.Events[1].ID != draftID || detail.Events[1].Status != "draft" ||
		detail.Events[1].StartsAt != nil || detail.Events[1].Discoverable {
		t.Fatalf("second event = %+v; want the unscheduled draft last", detail.Events[1])
	}

	if len(detail.Payouts) != 1 {
		t.Fatalf("payouts = %+v; want the one recorded settlement", detail.Payouts)
	}
	if detail.Payouts[0].AmountCents != 200 || detail.Payouts[0].PaidAt != "2026-07-05" ||
		detail.Payouts[0].Note != nil || detail.Payouts[0].RecordedBy != nil ||
		detail.Payouts[0].CreatedAt == "" {
		t.Fatalf("payout = %+v; want an unattributed 200¢ entry", detail.Payouts[0])
	}

	// An id that names no Organization is a 404, envelope and all.
	resp, body = env.get(t, "/api/v1/operator/organizations/a0000000-0000-4000-8000-00000000dead", authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown organization status=%d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "ORGANIZATION_NOT_FOUND" || !isNullData(body.Data) {
		t.Fatalf("unknown organization envelope: data=%s error=%+v", body.Data, body.Error)
	}
}

// TestOperatorRecordsPayoutAndTheOrganizationSeesIt closes the loop the whole
// slice exists for: an operator records a settlement from the dashboard instead
// of psql, it is stamped with their email, the Organization reads it on its own
// payouts page unchanged, and both balances move by the amount.
func TestOperatorRecordsPayoutAndTheOrganizationSeesIt(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	_, ga := publishCheckoutEvent(t, env, adminSessionID, "Settle Fest", "settle-fest", feeTestBaseCents, 10)
	begin := beginCheckoutOK(t, env, "test-org", "settle-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": ga, "quantity": 2}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	netProceeds := 2 * feeTestBaseCents

	operatorSessionID := operatorSession(t, env, "operator@example.com")

	resp, body := env.post(t, "/api/v1/operator/organizations/"+orgID+"/payouts", map[string]any{
		"amount_cents": 500,
		"paid_at":      "2026-07-20",
		"note":         "July settlement",
	}, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("record payout status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("record payout returned error %+v", body.Error)
	}
	var created operatorPayout
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatalf("decode created payout: %v", err)
	}
	if created.ID == "" || created.AmountCents != 500 || created.PaidAt != "2026-07-20" ||
		created.Note == nil || *created.Note != "July settlement" || created.CreatedAt == "" {
		t.Fatalf("created payout = %+v", created)
	}
	if created.RecordedBy == nil || *created.RecordedBy != "operator@example.com" {
		t.Fatalf("recorded_by = %v; want the recording operator's email", created.RecordedBy)
	}

	// The Organization's own page: the same Payout, unchanged, and a balance
	// that has already moved.
	orgView := getPayouts(t, env, adminSessionID)
	if want := netProceeds - 500; orgView.WithdrawableBalanceCents != want {
		t.Fatalf("organization balance = %d; want %d", orgView.WithdrawableBalanceCents, want)
	}
	if len(orgView.Payouts) != 1 || orgView.Payouts[0].ID != created.ID ||
		orgView.Payouts[0].AmountCents != 500 || orgView.Payouts[0].PaidAt != "2026-07-20" ||
		orgView.Payouts[0].Note == nil || *orgView.Payouts[0].Note != "July settlement" {
		t.Fatalf("organization payout history = %+v", orgView.Payouts)
	}

	// And the operator's own view of the same money.
	var detail operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &detail)
	if detail.WithdrawableBalanceCents != netProceeds-500 || len(detail.Payouts) != 1 {
		t.Fatalf("operator detail after payout = balance %d, %d payouts", detail.WithdrawableBalanceCents, len(detail.Payouts))
	}

	// An amount beyond the balance is accepted: the money already moved, and the
	// signed balance says the rest (ADR 0015).
	resp, body = env.post(t, "/api/v1/operator/organizations/"+orgID+"/payouts", map[string]any{
		"amount_cents": netProceeds * 10,
		"paid_at":      "2026-07-21",
	}, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("over-balance payout status=%d, want 201; error=%+v", resp.StatusCode, body.Error)
	}
	var overpaid operatorPayout
	if err := json.Unmarshal(body.Data, &overpaid); err != nil {
		t.Fatalf("decode over-balance payout: %v", err)
	}
	if overpaid.Note != nil {
		t.Fatalf("omitted note = %v; want null", overpaid.Note)
	}

	after := getPayouts(t, env, adminSessionID)
	if want := netProceeds - 500 - netProceeds*10; after.WithdrawableBalanceCents != want {
		t.Fatalf("balance after overpayment = %d; want the negative %d", after.WithdrawableBalanceCents, want)
	}
	// Newest first, both entries present.
	if len(after.Payouts) != 2 || after.Payouts[0].PaidAt != "2026-07-21" {
		t.Fatalf("payout history after overpayment = %+v", after.Payouts)
	}
}

// TestOperatorRecordPayoutValidation: the shape of the request is the handler's
// business — a non-positive amount and a date that is not one are refused with
// the house validation envelope, and nothing is recorded.
func TestOperatorRecordPayoutValidation(t *testing.T) {
	env := setupTest(t)
	orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"zero amount", map[string]any{"amount_cents": 0, "paid_at": "2026-07-20"}, "amount_cents"},
		{"negative amount", map[string]any{"amount_cents": -100, "paid_at": "2026-07-20"}, "amount_cents"},
		{"missing date", map[string]any{"amount_cents": 100}, "paid_at"},
		{"instant, not a day", map[string]any{"amount_cents": 100, "paid_at": "2026-07-20T10:00:00Z"}, "paid_at"},
	}
	for _, tc := range cases {
		resp, body := env.post(t, "/api/v1/operator/organizations/"+orgID+"/payouts", tc.body, authHeader(operatorSessionID))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status=%d, want 400; error=%+v", tc.name, resp.StatusCode, body.Error)
		}
		if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" || !isNullData(body.Data) {
			t.Fatalf("%s envelope: data=%s error=%+v", tc.name, body.Data, body.Error)
		}
		details, _ := json.Marshal(body.Error.Details)
		var parsed struct {
			Fields []struct {
				Field string `json:"field"`
			} `json:"fields"`
		}
		if err := json.Unmarshal(details, &parsed); err != nil {
			t.Fatalf("%s decode details: %v", tc.name, err)
		}
		if len(parsed.Fields) != 1 || parsed.Fields[0].Field != tc.field {
			t.Fatalf("%s fields = %+v; want %q", tc.name, parsed.Fields, tc.field)
		}
	}

	// Nothing was recorded by any of them.
	var detail operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &detail)
	if len(detail.Payouts) != 0 {
		t.Fatalf("payouts after rejected requests = %+v; want none", detail.Payouts)
	}
}

// isNullData reports whether an error envelope carried no data, as the house
// convention requires: `data` is null on every failure.
func isNullData(data json.RawMessage) bool {
	return len(data) == 0 || string(data) == "null"
}

// operatorOrgIDBySlug reads an Organization's id. No staff endpoint returns the
// id of an Organization by slug, and the operator list under test is the wrong
// thing to build another test's fixtures on.
func operatorOrgIDBySlug(t *testing.T, env *testEnv, slug string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`SELECT id FROM organizations WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("organization id for %q: %v", slug, err)
	}
	return id
}
