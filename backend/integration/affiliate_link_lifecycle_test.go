package integration

import (
	"net/http"
	"testing"
)

// An Affiliate Link's lifecycle (issue #148): the display name follows reality,
// a link can be taken out of circulation and put back under the same code, and a
// link can only be deleted while it has nothing to remember. History is never
// rewritten — a deactivated link keeps every click and every attributed sale on
// screen, and a link that drove anything can no longer be removed.

// updateAffiliateLink patches one Affiliate Link: the rename and the
// activate/deactivate toggle share the one endpoint, each field optional.
func updateAffiliateLink(t *testing.T, env *testEnv, sessionID, eventID, linkID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.patch(t, "/api/v1/staff/events/"+eventID+"/affiliate-links/"+linkID, body, authHeader(sessionID))
}

func deleteAffiliateLink(t *testing.T, env *testEnv, sessionID, eventID, linkID string) (*http.Response, envelope) {
	t.Helper()
	return env.deleteJSON(t, "/api/v1/staff/events/"+eventID+"/affiliate-links/"+linkID, nil, authHeader(sessionID))
}

// affiliateLinkRow reads one link's row off the staff list — the surface an
// organizer sees it on — by id.
func affiliateLinkRow(t *testing.T, env *testEnv, sessionID, eventID, linkID string) affiliateLinkView {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list affiliate links status=%d error=%+v", resp.StatusCode, body.Error)
	}
	for _, link := range decodeAffiliateLinks(t, body.Data) {
		if link.ID == linkID {
			return link
		}
	}
	t.Fatalf("no Affiliate Link %q in the Event's list", linkID)
	return affiliateLinkView{}
}

// newAffiliateLinkView creates a link and returns the whole created row.
func newAffiliateLinkView(t *testing.T, env *testEnv, sessionID, eventID, name string) affiliateLinkView {
	t.Helper()
	resp, body := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": name})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create affiliate link status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeAffiliateLink(t, body.Data)
}

func TestAffiliateLinkRenameChangesOnlyTheDisplayName(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, sessionID, "Ref Fest", "ref-fest", 1000, 10)

	created := newAffiliateLinkView(t, env, sessionID, eventID, "María's Instagram")

	// Give the link a history, so the rename can be shown to leave it alone.
	if resp, body := clickAffiliateLink(t, env, "test-org", "ref-fest", created.Code); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("click status=%d error=%+v", resp.StatusCode, body.Error)
	}
	begun := beginCheckoutOK(t, env, "test-org", "ref-fest",
		affiliateCheckoutBody("ana@example.com", created.Code, cartLine(ticketTypeID, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	before := affiliateLinkStatsByCode(t, env, sessionID, eventID, created.Code)

	resp, body := updateAffiliateLink(t, env, sessionID, eventID, created.ID, map[string]any{
		"name": "María — Instagram Stories",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rename status=%d, want 200; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("rename error=%+v, want none", body.Error)
	}

	renamed := decodeAffiliateLink(t, body.Data)
	if renamed.Name != "María — Instagram Stories" {
		t.Fatalf("name=%q, want the new one", renamed.Name)
	}
	if renamed.Code != created.Code {
		t.Fatalf("code=%q after a rename, want the immutable %q", renamed.Code, created.Code)
	}
	if renamed.URL != created.URL {
		t.Fatalf("url=%q after a rename, want the unchanged %q", renamed.URL, created.URL)
	}
	if !renamed.Active {
		t.Fatalf("a rename must not deactivate the link: %+v", renamed)
	}

	// And nothing the link had done changed with its label.
	row := affiliateLinkRow(t, env, sessionID, eventID, created.ID)
	if row.Name != "María — Instagram Stories" || row.Code != created.Code || row.Clicks != 1 {
		t.Fatalf("listed row after rename=%+v, want the new name over the same code and click", row)
	}
	after := affiliateLinkStatsByCode(t, env, sessionID, eventID, created.Code)
	if after.SalesCount != before.SalesCount || after.NetProceedsCents != before.NetProceedsCents {
		t.Fatalf("a rename moved the figures: before=%+v after=%+v", before, after)
	}
}

func TestAffiliateLinkRenameRejectsABlankName(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rave A", "rave-a")
	created := newAffiliateLinkView(t, env, sessionID, eventID, "Radio spot")

	resp, body := updateAffiliateLink(t, env, sessionID, eventID, created.ID, map[string]any{"name": "   "})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank rename status=%d, want 400; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("blank rename error=%+v, want VALIDATION_FAILED", body.Error)
	}
	if body.Data != nil && string(body.Data) != "null" {
		t.Fatalf("expected null data on failure, got %s", body.Data)
	}
	if row := affiliateLinkRow(t, env, sessionID, eventID, created.ID); row.Name != "Radio spot" {
		t.Fatalf("name=%q after a refused rename, want the original", row.Name)
	}
}

// Deactivation makes the code dead everywhere a buyer could carry it, and dead
// nowhere an organizer reads it.
func TestDeactivatedAffiliateLinkStopsCountingAndAttributingButKeepsItsHistory(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, sessionID, "Ref Fest", "ref-fest", 1000, 10)
	created := newAffiliateLinkView(t, env, sessionID, eventID, "Last year's flyer")

	// A click and an attributed sale, made while the link was live.
	if resp, body := clickAffiliateLink(t, env, "test-org", "ref-fest", created.Code); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("click status=%d error=%+v", resp.StatusCode, body.Error)
	}
	begun := beginCheckoutOK(t, env, "test-org", "ref-fest",
		affiliateCheckoutBody("ana@example.com", created.Code, cartLine(ticketTypeID, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	before := affiliateLinkStatsByCode(t, env, sessionID, eventID, created.Code)
	if before.SalesCount != 1 || before.NetProceedsCents <= 0 {
		t.Fatalf("before deactivation: %+v, want a sale with money behind it", before)
	}

	resp, body := updateAffiliateLink(t, env, sessionID, eventID, created.ID, map[string]any{"active": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("deactivate status=%d, want 200; error=%+v", resp.StatusCode, body.Error)
	}
	if deactivated := decodeAffiliateLink(t, body.Data); deactivated.Active {
		t.Fatalf("deactivate returned an active link: %+v", deactivated)
	}

	// The code counts nothing now.
	if resp, body := clickAffiliateLink(t, env, "test-org", "ref-fest", created.Code); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("click on a deactivated link status=%d, want 2xx; error=%+v", resp.StatusCode, body.Error)
	}

	// And a checkout carrying it sells a ticket, unattributed.
	begun = beginCheckoutOK(t, env, "test-org", "ref-fest",
		affiliateCheckoutBody("bea@example.com", created.Code, cartLine(ticketTypeID, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	if summary := salesSummaryOK(t, env, sessionID, eventID); summary.SalesCount != 2 {
		t.Fatalf("the Event recorded %d sales, want the 2 that checked out", summary.SalesCount)
	}

	// Everything the link did while it was live is still on screen, marked dead.
	row := affiliateLinkRow(t, env, sessionID, eventID, created.ID)
	if row.Active {
		t.Fatalf("row=%+v, want it listed as inactive", row)
	}
	if row.Clicks != 1 {
		t.Fatalf("clicks=%d on a deactivated link, want the 1 it had and no more", row.Clicks)
	}
	after := affiliateLinkStatsByCode(t, env, sessionID, eventID, created.Code)
	if after.SalesCount != before.SalesCount || after.NetProceedsCents != before.NetProceedsCents {
		t.Fatalf("deactivation moved the history: before=%+v after=%+v", before, after)
	}
}

func TestReactivatedAffiliateLinkResumesClicksAndAttributionUnderTheSameCode(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, sessionID, "Ref Fest", "ref-fest", 1000, 10)
	created := newAffiliateLinkView(t, env, sessionID, eventID, "María's Instagram")

	if resp, body := updateAffiliateLink(t, env, sessionID, eventID, created.ID, map[string]any{"active": false}); resp.StatusCode != http.StatusOK {
		t.Fatalf("deactivate status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body := updateAffiliateLink(t, env, sessionID, eventID, created.ID, map[string]any{"active": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reactivate status=%d, want 200; error=%+v", resp.StatusCode, body.Error)
	}
	reactivated := decodeAffiliateLink(t, body.Data)
	if !reactivated.Active {
		t.Fatalf("reactivate returned an inactive link: %+v", reactivated)
	}
	if reactivated.Code != created.Code {
		t.Fatalf("code=%q after reactivation, want the same %q", reactivated.Code, created.Code)
	}

	if resp, body := clickAffiliateLink(t, env, "test-org", "ref-fest", created.Code); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("click status=%d error=%+v", resp.StatusCode, body.Error)
	}
	begun := beginCheckoutOK(t, env, "test-org", "ref-fest",
		affiliateCheckoutBody("ana@example.com", created.Code, cartLine(ticketTypeID, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	if row := affiliateLinkRow(t, env, sessionID, eventID, created.ID); row.Clicks != 1 {
		t.Fatalf("clicks=%d after reactivation, want the resumed 1", row.Clicks)
	}
	stats := affiliateLinkStatsByCode(t, env, sessionID, eventID, created.Code)
	if stats.SalesCount != 1 || stats.NetProceedsCents <= 0 {
		t.Fatalf("stats=%+v after reactivation, want the sale it drove again", stats)
	}
}

func TestAffiliateLinkDeleteRemovesALinkThatDroveNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rave A", "rave-a")
	typo := newAffiliateLinkView(t, env, sessionID, eventID, "Radoi spot")
	keeper := newAffiliateLinkView(t, env, sessionID, eventID, "María's Instagram")

	resp, body := deleteAffiliateLink(t, env, sessionID, eventID, typo.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status=%d, want 200; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("delete error=%+v, want none", body.Error)
	}

	listResp, listBody := env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", authHeader(sessionID))
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", listResp.StatusCode, listBody.Error)
	}
	links := decodeAffiliateLinks(t, listBody.Data)
	if len(links) != 1 || links[0].ID != keeper.ID {
		t.Fatalf("list after delete=%+v, want only the keeper", links)
	}

	// Deleting it a second time finds nothing to delete.
	resp, body = deleteAffiliateLink(t, env, sessionID, eventID, typo.ID)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("re-delete status=%d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "AFFILIATE_LINK_NOT_FOUND" {
		t.Fatalf("re-delete error=%+v, want AFFILIATE_LINK_NOT_FOUND", body.Error)
	}
}

func TestAffiliateLinkDeleteIsRefusedOnceItHasClicks(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "Ref Fest", "ref-fest", 1000, 10)
	created := newAffiliateLinkView(t, env, sessionID, eventID, "María's Instagram")

	if resp, body := clickAffiliateLink(t, env, "test-org", "ref-fest", created.Code); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("click status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body := deleteAffiliateLink(t, env, sessionID, eventID, created.ID)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete with clicks status=%d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "AFFILIATE_LINK_HAS_HISTORY" {
		t.Fatalf("delete with clicks error=%+v, want AFFILIATE_LINK_HAS_HISTORY", body.Error)
	}
	if body.Data != nil && string(body.Data) != "null" {
		t.Fatalf("expected null data on failure, got %s", body.Data)
	}
	if row := affiliateLinkRow(t, env, sessionID, eventID, created.ID); row.Clicks != 1 {
		t.Fatalf("row=%+v after a refused delete, want the link and its click still there", row)
	}
}

// A reversed attributed sale is still history: the Ticket Sale keeps pointing at
// the link, so the link may not be deleted out from under it however the sale
// ended up.
func TestAffiliateLinkDeleteIsRefusedOnceItHasAnAttributedSaleEvenAReversedOne(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, sessionID, "Ref Fest", "ref-fest", 1000, 10)
	created := newAffiliateLinkView(t, env, sessionID, eventID, "María's Instagram")

	begun := beginCheckoutOK(t, env, "test-org", "ref-fest",
		affiliateCheckoutBody("ana@example.com", created.Code, cartLine(ticketTypeID, 1)))
	confirmed := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	resp, body := deleteAffiliateLink(t, env, sessionID, eventID, created.ID)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete with an attributed sale status=%d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "AFFILIATE_LINK_HAS_HISTORY" {
		t.Fatalf("delete with an attributed sale error=%+v, want AFFILIATE_LINK_HAS_HISTORY", body.Error)
	}

	// Reversing the sale empties the link's figures but not its history.
	reverseSale(t, env, confirmed.ConfirmationRef)
	if stats := affiliateLinkStatsByCode(t, env, sessionID, eventID, created.Code); stats.SalesCount != 0 {
		t.Fatalf("sales_count=%d after the reversal, want 0", stats.SalesCount)
	}
	resp, body = deleteAffiliateLink(t, env, sessionID, eventID, created.ID)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete after the reversal status=%d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "AFFILIATE_LINK_HAS_HISTORY" {
		t.Fatalf("delete after the reversal error=%+v, want AFFILIATE_LINK_HAS_HISTORY", body.Error)
	}

	// The attributed sale still names the link it came through.
	var attributed int
	if err := env.db.QueryRow(
		// SQL, not the API: no staff surface reports the attribution column on an
		// individual Ticket Sale, and what is under test is that a refused delete
		// left no orphan behind.
		`SELECT COUNT(*) FROM ticket_sales WHERE affiliate_link_id = $1`, created.ID,
	).Scan(&attributed); err != nil {
		t.Fatalf("count attributed sales: %v", err)
	}
	if attributed != 1 {
		t.Fatalf("attributed sales pointing at the link=%d, want the 1 that was never orphaned", attributed)
	}
}

func TestAffiliateLinkLifecycleIsFullAccessOnly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rave A", "rave-a")
	created := newAffiliateLinkView(t, env, sessionID, eventID, "María's Instagram")

	addMember := func(email, role string) string {
		t.Helper()
		resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
			"email": email,
			"role":  role,
		}, authHeader(sessionID))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
		}
		return verifyOTP(t, env, email)
	}

	// An Event Owner manages the lifecycle exactly as an Org Admin does.
	ownerSessionID := addMember("owner@example.com", "event_owner")
	ownerLink := newAffiliateLinkView(t, env, ownerSessionID, eventID, "Owner's link")
	if resp, body := updateAffiliateLink(t, env, ownerSessionID, eventID, ownerLink.ID, map[string]any{"active": false}); resp.StatusCode != http.StatusOK {
		t.Fatalf("event owner deactivate status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if resp, body := deleteAffiliateLink(t, env, ownerSessionID, eventID, ownerLink.ID); resp.StatusCode != http.StatusOK {
		t.Fatalf("event owner delete status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Event Staff are refused both.
	staffSessionID := addMember("doorstaff@example.com", "event_staff")
	resp, body := updateAffiliateLink(t, env, staffSessionID, eventID, created.ID, map[string]any{"name": "Sneaky"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("event staff rename status=%d, want 403; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("event staff rename error=%+v, want FORBIDDEN", body.Error)
	}
	resp, body = deleteAffiliateLink(t, env, staffSessionID, eventID, created.ID)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("event staff delete status=%d, want 403; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("event staff delete error=%+v, want FORBIDDEN", body.Error)
	}

	// Another Organization's Org Admin sees no such Event at all.
	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	resp, body = updateAffiliateLink(t, env, otherSessionID, eventID, created.ID, map[string]any{"name": "Theirs"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other-org rename status=%d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Fatalf("other-org rename error=%+v, want EVENT_NOT_FOUND", body.Error)
	}

	// Unauthenticated is refused before any of it.
	if resp, _ := env.patch(t, "/api/v1/staff/events/"+eventID+"/affiliate-links/"+created.ID, map[string]any{"name": "Nope"}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated rename status=%d, want 401", resp.StatusCode)
	}
	if resp, _ := env.deleteJSON(t, "/api/v1/staff/events/"+eventID+"/affiliate-links/"+created.ID, nil, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated delete status=%d, want 401", resp.StatusCode)
	}

	// The link is untouched by every refusal above.
	if row := affiliateLinkRow(t, env, sessionID, eventID, created.ID); row.Name != "María's Instagram" || !row.Active {
		t.Fatalf("row=%+v after the refused lifecycle calls, want it unchanged", row)
	}
}
