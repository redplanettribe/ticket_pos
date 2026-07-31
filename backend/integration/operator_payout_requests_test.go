package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The operator's Payout Request queue: who is waiting to be paid (#176,
// ADR 0026).
//
// This is the operator surface's SECOND cross-Organization view, and unlike the
// first it is not an exception granted to a lookup. Sale lookup crosses
// Organizations because a support thread carries a reference and nothing else;
// this queue crosses them because the request IS the reason to open the
// dashboard at all, and an operator who had to page through every Organization
// asking "is anyone waiting?" would simply not do it.
//
// Everything here is read-only. Fulfilling and declining are #177; what this
// slice must prove is that an operator can find the backlog, read what a
// transfer needs, and tell an Organization that asked for what it had from one
// that asked for four times as much.
//
// Three properties are asserted at this seam rather than in a unit test, because
// only real HTTP over a real Postgres sees them:
//
//   - OLDEST FIRST, across Organizations, served by the partial index migration
//     043 created for exactly this query. It deliberately breaks the newest-first
//     house convention (a work queue, not a history).
//   - MASKED ACCOUNT NUMBERS. The assertion is on the raw response body, not on a
//     decoded field: what matters is that the full number is not IN the payload,
//     which a struct with the field missing would happily hide.
//   - THE GATE. Non-operators are refused on every new route, and an operator who
//     is a Member of nothing is served (ADR 0015).

const operatorPayoutRequestsPath = "/api/v1/operator/payout-requests"

// operatorMaskedProfile is the bank detail a LIST context carries: enough for an
// operator to recognise the account, never enough to retype it. The Tax ID is
// absent by design and not merely unread — see operatorPayoutRequestRow.
type operatorMaskedProfile struct {
	BankName string `json:"bank_name"`
	// AccountType is 'ahorros' or 'corriente' — the word the receiving bank's
	// own form uses, and no part of the account's identity.
	AccountType string `json:"account_type"`
	// AccountNumberMasked is "····4821": four dots and the last four digits.
	AccountNumberMasked string `json:"account_number_masked"`
	AccountHolderName   string `json:"account_holder_name"`
}

// operatorPayoutRequestRow is one request as the queue and the Organization
// detail page show it. It carries no full account number and no Tax ID at all:
// this is the screen that shows every Organization's details at once and the one
// operators screenshot into support threads (ADR 0026), so what it holds is what
// triage needs and nothing beyond.
type operatorPayoutRequestRow struct {
	ID                  string                `json:"id"`
	AmountCents         int                   `json:"amount_cents"`
	Note                *string               `json:"note"`
	Status              string                `json:"status"`
	RequestedBy         string                `json:"requested_by"`
	RequestedAt         string                `json:"requested_at"`
	PayableBalanceCents int                   `json:"payable_balance_cents"`
	PayoutProfile       operatorMaskedProfile `json:"payout_profile"`
	ResolutionReason    *string               `json:"resolution_reason"`
	ResolvedBy          *string               `json:"resolved_by"`
	ResolvedAt          *string               `json:"resolved_at"`
	PayoutID            *string               `json:"payout_id"`
}

// operatorQueueItem pairs the ask with whose it is. The Organization is a
// separate object rather than three fields on the request, exactly as it is on
// the sale lookup: it is a different module's answer.
type operatorQueueItem struct {
	Request      operatorPayoutRequestRow `json:"request"`
	Organization struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Currency string `json:"currency"`
	} `json:"organization"`
}

type operatorPayoutRequestQueue struct {
	Data       []operatorQueueItem `json:"data"`
	Pagination operatorPagination  `json:"pagination"`
}

// operatorPayoutRequestDetail is everything needed to execute one transfer: the
// full snapshot, whose it is, and both readings of the Payable Balance.
type operatorPayoutRequestDetail struct {
	Request struct {
		ID                  string               `json:"id"`
		AmountCents         int                  `json:"amount_cents"`
		Note                *string              `json:"note"`
		Status              string               `json:"status"`
		RequestedBy         string               `json:"requested_by"`
		RequestedAt         string               `json:"requested_at"`
		PayableBalanceCents int                  `json:"payable_balance_cents"`
		PayoutProfile       payoutRequestProfile `json:"payout_profile"`
		ResolutionReason    *string              `json:"resolution_reason"`
		ResolvedBy          *string              `json:"resolved_by"`
		ResolvedAt          *string              `json:"resolved_at"`
		PayoutID            *string              `json:"payout_id"`
	} `json:"request"`
	Organization struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Currency string `json:"currency"`
	} `json:"organization"`
	// The LIVE figures, read now. The snapshot above says what the Organization
	// could have asked for when it asked; these say what it could ask for today,
	// and the gap is the judgement the cap deliberately leaves to the operator.
	WithdrawableBalanceCents int `json:"withdrawable_balance_cents"`
	PayableBalanceCents      int `json:"payable_balance_cents"`
}

type operatorPendingCount struct {
	PendingCount int `json:"pending_count"`
}

func operatorQueue(t *testing.T, env *testEnv, sessionID, query string) operatorPayoutRequestQueue {
	t.Helper()
	var queue operatorPayoutRequestQueue
	operatorGetOK(t, env, sessionID, operatorPayoutRequestsPath+query, &queue)
	return queue
}

func operatorPendingPayoutRequestCount(t *testing.T, env *testEnv, sessionID string) int {
	t.Helper()
	var count operatorPendingCount
	operatorGetOK(t, env, sessionID, operatorPayoutRequestsPath+"/count", &count)
	return count.PendingCount
}

// operatorPendingRequestFor stages one Organization asking to be paid: a cleared
// sale to ask against, and the ask itself.
//
// The clock move is what makes the sale payable — the Payable Balance counts
// sales recorded BEFORE today in Ecuador — and the clock is GLOBAL to the shared
// app, which is why clearedOn is a parameter. A test staging two Organizations
// moves the day on twice, once after each sale: leaving it where the first
// Organization put it would record the second's sale exactly on the boundary,
// where it has not cleared and there is nothing to ask for.
func operatorPendingRequestFor(t *testing.T, env *testEnv, sessionID, orgSlug, eventName, eventSlug string, quantity, clearedOn int) (payoutRequest, int) {
	t.Helper()
	_, ga := publishCheckoutEvent(t, env, sessionID, eventName, eventSlug, feeTestBaseCents, quantity+10)
	begin := beginCheckoutOK(t, env, orgSlug, eventSlug,
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": ga, "quantity": quantity}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	salesClockAt(ecuadorMidnight(t, clearedOn))

	payable := quantity * feeTestBaseCents
	request, created := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, "settle us up"))
	if !created {
		t.Fatalf("%s: the request was answered as an existing one", orgSlug)
	}
	return request, payable
}

// TestOperatorPayoutRequestQueueIsOldestFirstAcrossOrganizations is the whole
// point of the surface: every outstanding ask on the platform in one list, the
// one that has waited longest at the top.
//
// Oldest-first is a deliberate break with the newest-first convention the payout
// and sale histories use (ADR 0026). This is a work queue rather than a history,
// and the oldest unanswered request is the one about to become a complaint.
func TestOperatorPayoutRequestQueueIsOldestFirstAcrossOrganizations(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	// Nothing waiting is an empty queue and a zero count, not an error.
	empty := operatorQueue(t, env, operatorSessionID, "")
	if len(empty.Data) != 0 {
		t.Fatalf("queue on an empty platform = %+v; want none", empty.Data)
	}
	if empty.Pagination != (operatorPagination{Page: 1, PageSize: 50, Total: 0, TotalPages: 0}) {
		t.Fatalf("empty pagination = %+v; want the ADR-0006 defaults over nothing", empty.Pagination)
	}

	adminSessionID := orgAdminSession(t, env)
	first, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Queue Fest", "queue-fest", 4, 1)

	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	second, _ := operatorPendingRequestFor(t, env, otherSessionID, "other-org", "Other Fest", "other-fest", 3, 2)

	queue := operatorQueue(t, env, operatorSessionID, "")
	if len(queue.Data) != 2 {
		t.Fatalf("queue = %+v; want both Organizations' outstanding asks", queue.Data)
	}
	if queue.Data[0].Request.ID != first.ID || queue.Data[1].Request.ID != second.ID {
		t.Fatalf("queue order = %q, %q; want oldest first (%q then %q)",
			queue.Data[0].Request.ID, queue.Data[1].Request.ID, first.ID, second.ID)
	}
	if queue.Pagination != (operatorPagination{Page: 1, PageSize: 50, Total: 2, TotalPages: 1}) {
		t.Fatalf("pagination = %+v; want the ADR-0006 defaults over 2 requests", queue.Pagination)
	}

	// Whose ask it is, which the request alone cannot say: the operator is a
	// Member of neither Organization and has nothing else to recognise them by.
	if queue.Data[0].Organization.Slug != "test-org" || queue.Data[0].Organization.Name != "Test Org" ||
		queue.Data[0].Organization.Currency != "USD" || queue.Data[0].Organization.ID == "" {
		t.Fatalf("first row's Organization = %+v", queue.Data[0].Organization)
	}
	if queue.Data[1].Organization.Slug != "other-org" {
		t.Fatalf("second row's Organization = %+v; want the other Organization", queue.Data[1].Organization)
	}

	// The ask itself, as a triaging operator reads it.
	row := queue.Data[0].Request
	if row.Status != "pending" || row.AmountCents != first.AmountCents ||
		row.RequestedBy != "admin@example.com" || row.RequestedAt == "" ||
		row.Note == nil || *row.Note != "settle us up" {
		t.Fatalf("first row = %+v", row)
	}
	if row.PayableBalanceCents != first.PayableBalanceCents {
		t.Fatalf("row payable_balance_cents = %d; want the snapshot %d", row.PayableBalanceCents, first.PayableBalanceCents)
	}
	if row.ResolutionReason != nil || row.ResolvedBy != nil || row.ResolvedAt != nil || row.PayoutID != nil {
		t.Fatalf("a pending row carries a resolution: %+v", row)
	}

	// ONLY outstanding requests queue up. A cancelled ask has been answered — by
	// the Organization itself — and a queue that kept it would be a history.
	if resp, _ := cancelPayoutRequest(t, env, adminSessionID, first.ID); resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d", resp.StatusCode)
	}
	after := operatorQueue(t, env, operatorSessionID, "")
	if len(after.Data) != 1 || after.Data[0].Request.ID != second.ID {
		t.Fatalf("queue after a cancellation = %+v; want only the still-outstanding ask", after.Data)
	}
	if after.Pagination.Total != 1 {
		t.Fatalf("total after a cancellation = %d; want 1", after.Pagination.Total)
	}
}

// TestOperatorPayoutRequestQueuePaginates: the house offset convention (ADR
// 0006), walking the same oldest-first order page by page.
func TestOperatorPayoutRequestQueuePaginates(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	adminSessionID := orgAdminSession(t, env)
	first, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Page Fest", "page-fest", 4, 1)
	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	second, _ := operatorPendingRequestFor(t, env, otherSessionID, "other-org", "Page Two Fest", "page-two-fest", 3, 2)

	page1 := operatorQueue(t, env, operatorSessionID, "?page=1&page_size=1")
	if page1.Pagination != (operatorPagination{Page: 1, PageSize: 1, Total: 2, TotalPages: 2}) {
		t.Fatalf("page 1 pagination = %+v", page1.Pagination)
	}
	if len(page1.Data) != 1 || page1.Data[0].Request.ID != first.ID {
		t.Fatalf("page 1 = %+v; want the oldest ask alone", page1.Data)
	}

	page2 := operatorQueue(t, env, operatorSessionID, "?page=2&page_size=1")
	if page2.Pagination.Page != 2 || len(page2.Data) != 1 || page2.Data[0].Request.ID != second.ID {
		t.Fatalf("page 2 = %+v (pagination %+v); want the newer ask", page2.Data, page2.Pagination)
	}

	// Floored and clamped exactly as every other staff list is: a page below one
	// is page one, and an absurd page size is the maximum rather than an error.
	clamped := operatorQueue(t, env, operatorSessionID, "?page=0&page_size=5000")
	if clamped.Pagination.Page != 1 || clamped.Pagination.PageSize != 100 {
		t.Fatalf("clamped pagination = %+v; want page 1 and the 100 maximum", clamped.Pagination)
	}
}

// TestOperatorPayoutRequestQueueMasksAccountNumbers: the queue shows every
// Organization's bank details at once and is the screen operators screenshot
// into support threads, so it shows enough to recognise an account and never
// enough to retype one (ADR 0026).
//
// The assertion is on the RAW response body. A decoded struct that simply lacks
// the field would pass while the API cheerfully shipped the digits to the
// browser; the only honest question is whether the number is in the payload.
func TestOperatorPayoutRequestQueueMasksAccountNumbers(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Masked Fest", "masked-fest", 4, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	resp, body := env.get(t, operatorPayoutRequestsPath, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("queue status=%d error=%+v", resp.StatusCode, body.Error)
	}
	raw := string(body.Data)
	// "2201234821" is completeProfile's account number, and the Tax ID is the
	// other identifier a screenshot must not carry.
	if strings.Contains(raw, "2201234821") {
		t.Fatalf("the queue payload carries a full account number")
	}
	if strings.Contains(raw, validCedula) {
		t.Fatalf("the queue payload carries the Organization's Tax ID")
	}

	var queue operatorPayoutRequestQueue
	if err := json.Unmarshal(body.Data, &queue); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	if len(queue.Data) != 1 {
		t.Fatalf("queue = %+v; want the one outstanding ask", queue.Data)
	}
	profile := queue.Data[0].Request.PayoutProfile
	if profile.AccountNumberMasked != "····4821" {
		t.Fatalf("masked account number = %q; want ····4821", profile.AccountNumberMasked)
	}
	// The rest of the details a triaging operator legitimately needs: which bank,
	// which kind of account, and whose name is on it.
	if profile.BankName != "Banco Pichincha" || profile.AccountType != "ahorros" ||
		profile.AccountHolderName != "Fundación Ritmo" {
		t.Fatalf("queue profile = %+v", profile)
	}

	// And the detail view is where the full number lives — the one screen that
	// shows one Organization's details, opened deliberately.
	var detail operatorPayoutRequestDetail
	operatorGetOK(t, env, operatorSessionID, operatorPayoutRequestsPath+"/"+request.ID, &detail)
	if detail.Request.PayoutProfile.AccountNumber != "2201234821" {
		t.Fatalf("detail account number = %q; want the whole number", detail.Request.PayoutProfile.AccountNumber)
	}
}

// TestOperatorPayoutRequestDetailShowsTheTransferAndBothBalances: opening a
// request shows everything needed to execute the transfer, and the two readings
// of the Payable Balance that let an operator tell "asked for all of it" from
// "asked for four times what they have" (ADR 0026).
func TestOperatorPayoutRequestDetailShowsTheTransferAndBothBalances(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Detail Fest", "detail-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	var detail operatorPayoutRequestDetail
	operatorGetOK(t, env, operatorSessionID, operatorPayoutRequestsPath+"/"+request.ID, &detail)

	if detail.Request.ID != request.ID || detail.Request.Status != "pending" ||
		detail.Request.AmountCents != payable || detail.Request.RequestedBy != "admin@example.com" ||
		detail.Request.Note == nil || *detail.Request.Note != "settle us up" {
		t.Fatalf("request = %+v", detail.Request)
	}
	if detail.Organization.Slug != "test-org" || detail.Organization.Name != "Test Org" ||
		detail.Organization.Currency != "USD" {
		t.Fatalf("organization = %+v", detail.Organization)
	}
	// The snapshot, in full: this is where an operator was TOLD to pay, and no
	// later edit of the profile may rewrite it.
	wantProfile := payoutRequestProfile{
		BankName:          "Banco Pichincha",
		AccountType:       "ahorros",
		AccountNumber:     "2201234821",
		AccountHolderName: "Fundación Ritmo",
		TaxIDType:         "cedula",
		TaxIDNumber:       validCedula,
	}
	if detail.Request.PayoutProfile != wantProfile {
		t.Fatalf("snapshot = %+v; want %+v", detail.Request.PayoutProfile, wantProfile)
	}
	if detail.Request.PayableBalanceCents != payable || detail.PayableBalanceCents != payable {
		t.Fatalf("snapshot=%d live=%d; want both %d before anything moves",
			detail.Request.PayableBalanceCents, detail.PayableBalanceCents, payable)
	}
	if detail.WithdrawableBalanceCents != payable {
		t.Fatalf("withdrawable = %d; want %d", detail.WithdrawableBalanceCents, payable)
	}

	// The Organization is settled somewhere else entirely, and the live figure
	// collapses. The snapshot does not move: the cap was checked when the request
	// was made and never again, and the operator at the bank is the party who
	// decides what to do about the gap.
	recordPayout(t, env, "test-org", payable, "2026-07-08", "settled another way")
	salesClockAt(ecuadorMidnight(t, 1))

	var after operatorPayoutRequestDetail
	operatorGetOK(t, env, operatorSessionID, operatorPayoutRequestsPath+"/"+request.ID, &after)
	if after.Request.PayableBalanceCents != payable {
		t.Fatalf("snapshot after the settlement = %d; want the figure at the moment of asking, %d",
			after.Request.PayableBalanceCents, payable)
	}
	if after.PayableBalanceCents != 0 || after.WithdrawableBalanceCents != 0 {
		t.Fatalf("live balances after the settlement = payable %d / withdrawable %d; want both 0",
			after.PayableBalanceCents, after.WithdrawableBalanceCents)
	}

	// A request that has been answered is still readable by id: the queue drops
	// it, and the record it left does not disappear with it.
	if resp, _ := cancelPayoutRequest(t, env, adminSessionID, request.ID); resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d", resp.StatusCode)
	}
	var cancelled operatorPayoutRequestDetail
	operatorGetOK(t, env, operatorSessionID, operatorPayoutRequestsPath+"/"+request.ID, &cancelled)
	if cancelled.Request.Status != "cancelled" || cancelled.Request.ResolvedBy == nil {
		t.Fatalf("cancelled request = %+v; want it readable with its resolution", cancelled.Request)
	}

	// An id naming no request, and an id that is not an id at all, are both
	// simply not found — the second is a caller's typo, not a server fault.
	for _, id := range []string{"a0000000-0000-4000-8000-00000000dead", "not-a-uuid"} {
		resp, body := env.get(t, operatorPayoutRequestsPath+"/"+id, authHeader(operatorSessionID))
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET request %q status=%d; want 404", id, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "PAYOUT_REQUEST_NOT_FOUND" || !isNullData(body.Data) {
			t.Fatalf("GET request %q envelope: data=%s error=%+v", id, body.Data, body.Error)
		}
	}
}

// TestOperatorPayoutRequestPendingCountRidesTheNavigation: the count the
// operator navigation wears. A queue's whole value is being noticed by somebody
// who had not already decided to look (ADR 0026), so the number is available on
// its own rather than only as a side effect of loading the queue.
func TestOperatorPayoutRequestPendingCountRidesTheNavigation(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 0 {
		t.Fatalf("pending count on an empty platform = %d; want 0", count)
	}

	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Badge Fest", "badge-fest", 4, 1)
	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 1 {
		t.Fatalf("pending count after one ask = %d; want 1", count)
	}

	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	operatorPendingRequestFor(t, env, otherSessionID, "other-org", "Badge Two Fest", "badge-two-fest", 3, 2)
	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 2 {
		t.Fatalf("pending count across two Organizations = %d; want 2", count)
	}

	// It counts what is OUTSTANDING, not what exists: an answered request leaves
	// the backlog, and the badge is a backlog.
	if resp, _ := cancelPayoutRequest(t, env, adminSessionID, request.ID); resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d", resp.StatusCode)
	}
	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 1 {
		t.Fatalf("pending count after a cancellation = %d; want 1", count)
	}
	// And it agrees with the queue's own total, because they are the same
	// question asked twice and a badge that disagreed with the list would be
	// worse than no badge.
	if total := operatorQueue(t, env, operatorSessionID, "").Pagination.Total; total != 1 {
		t.Fatalf("queue total = %d; want the same 1 the count reports", total)
	}
}

// TestOperatorPayoutRequestsAppearOnTheOrganizationDetail: the requests sit
// beside the payout history and the balances, where they answer "has this
// Organization been paid recently, and are they asking again?" (ADR 0026).
//
// This is a HISTORY rather than a work queue, so it is newest first — the
// convention the top-level queue is the one place to break.
func TestOperatorPayoutRequestsAppearOnTheOrganizationDetail(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	var before operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &before)
	if len(before.PayoutRequests) != 0 {
		t.Fatalf("requests on an Organization that has never asked = %+v; want none", before.PayoutRequests)
	}

	first, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Beside Fest", "beside-fest", 6, 1)
	if resp, _ := cancelPayoutRequest(t, env, adminSessionID, first.ID); resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d", resp.StatusCode)
	}
	second, _ := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable-100, "second thoughts"))

	var detail operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &detail)
	if len(detail.PayoutRequests) != 2 {
		t.Fatalf("requests = %+v; want both asks, answered and outstanding", detail.PayoutRequests)
	}
	if detail.PayoutRequests[0].ID != second.ID || detail.PayoutRequests[1].ID != first.ID {
		t.Fatalf("request order = %+v; want newest first on a history", detail.PayoutRequests)
	}
	if detail.PayoutRequests[0].Status != "pending" || detail.PayoutRequests[1].Status != "cancelled" {
		t.Fatalf("statuses = %q, %q", detail.PayoutRequests[0].Status, detail.PayoutRequests[1].Status)
	}
	if detail.PayoutRequests[1].ResolvedBy == nil || *detail.PayoutRequests[1].ResolvedBy != "admin@example.com" {
		t.Fatalf("cancelled request's resolver = %v", detail.PayoutRequests[1].ResolvedBy)
	}
	// Masked here too. One Organization's details are still bank details, and the
	// place to read an account number is the request that is about to be paid.
	if detail.PayoutRequests[0].PayoutProfile.AccountNumberMasked != "····4821" {
		t.Fatalf("organization detail profile = %+v; want a masked account number",
			detail.PayoutRequests[0].PayoutProfile)
	}

	// Another Organization's asks are not this Organization's.
	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	otherID := operatorOrgIDBySlug(t, env, "other-org")
	var other operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+otherID, &other)
	if len(other.PayoutRequests) != 0 {
		t.Fatalf("the other Organization's requests = %+v; want none of its neighbour's", other.PayoutRequests)
	}
}

// TestOperatorPayoutRequestSurfaceRequiresAnAllowlistedEmail: the gate is a
// property of the namespace, and the new routes join it unchanged (ADR 0015).
//
// The Org Admin refused here is the very person who made the request. Reading
// their own ask is the staff surface's business; reading the platform's queue is
// not, and being an Org Admin grants nothing platform-wide.
func TestOperatorPayoutRequestSurfaceRequiresAnAllowlistedEmail(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Gate Fest", "gate-fest", 4, 1)

	routes := []string{
		operatorPayoutRequestsPath,
		operatorPayoutRequestsPath + "/count",
		operatorPayoutRequestsPath + "/" + request.ID,
	}

	for _, path := range routes {
		resp, body := env.get(t, path, nil)
		if resp.StatusCode != http.StatusUnauthorized || body.Error == nil || body.Error.Code != "UNAUTHORIZED" {
			t.Fatalf("GET %s anonymous status=%d error=%+v; want 401 UNAUTHORIZED", path, resp.StatusCode, body.Error)
		}
		resp, body = env.get(t, path, authHeader(adminSessionID))
		if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("GET %s as org_admin status=%d error=%+v; want 403 FORBIDDEN", path, resp.StatusCode, body.Error)
		}
	}

	// An operator who is a Member of no Organization at all is served: operator
	// authority is orthogonal to Membership, and this queue is the surface where
	// that matters most — every row on it belongs to somebody else.
	operatorSessionID := operatorSession(t, env, "nomember@example.com")
	var session sessionView
	operatorGetOK(t, env, operatorSessionID, "/api/v1/auth/session", &session)
	if session.ActiveMember != nil || len(session.Memberships) != 0 {
		t.Fatalf("operator session = %+v; want no Membership and no Active Member", session)
	}
	for _, path := range routes {
		if resp, body := env.get(t, path, authHeader(operatorSessionID)); resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s as a Member-less operator status=%d error=%+v", path, resp.StatusCode, body.Error)
		}
	}
}

// TestOperatorPayoutRequestQueueLeaksNoBankDetailsInErrors: bank details never
// appear in an error message (ADR 0026), including on the refusals this surface
// can produce.
//
// The rule is enforced by nothing but review, so this pins the one part of it a
// test can hold: the refusals a caller can provoke here name a request, never an
// account.
func TestOperatorPayoutRequestQueueLeaksNoBankDetailsInErrors(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Quiet Fest", "quiet-fest", 4, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	// The forbidden path: an Org Admin reaching the operator's copy of their own
	// request. The refusal says nothing about what is inside it.
	_, forbidden := env.get(t, operatorPayoutRequestsPath+"/"+request.ID, authHeader(adminSessionID))
	assertNoBankDetails(t, forbidden.Error)

	// And the not-found path.
	_, missing := env.get(t, operatorPayoutRequestsPath+"/a0000000-0000-4000-8000-00000000dead", authHeader(operatorSessionID))
	assertNoBankDetails(t, missing.Error)
}

// assertNoBankDetails insists an error envelope carries nothing from a Payout
// Profile — not the account number, not the Tax ID, not the holder's name.
func assertNoBankDetails(t *testing.T, apiError *platform.APIError) {
	t.Helper()
	if apiError == nil {
		t.Fatalf("expected an error envelope")
	}
	encoded, err := json.Marshal(apiError)
	if err != nil {
		t.Fatalf("marshal error envelope: %v", err)
	}
	for _, secret := range []string{"2201234821", validCedula, "Fundación Ritmo", "Banco Pichincha"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("an error envelope carries a bank detail (%q): %s", secret, encoded)
		}
	}
}
