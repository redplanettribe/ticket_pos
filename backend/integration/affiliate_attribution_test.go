package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Affiliate Attribution through checkout (issue #146): a buyer who reached the
// Event page through a live Affiliate Link carries its code into begin-checkout,
// and the Ticket Sale that follows is credited to that link. The Storefront's
// cookie is what remembers the code; the backend is cookie-agnostic and only
// ever sees the code on the begin-checkout request.
//
// The assertions run over the staff Affiliate Links list — the surface the
// figures exist for — rather than over the attribution column, so what is proved
// is what an organizer can actually see.

// affiliateLinkStats mirrors the staff Affiliate Link row's attribution figures:
// attributed ACTIVE sales and what they left the Organization.
type affiliateLinkStats struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Code             string `json:"code"`
	SalesCount       int    `json:"sales_count"`
	NetProceedsCents int    `json:"net_proceeds_cents"`
}

func affiliateLinkStatsList(t *testing.T, env *testEnv, sessionID, eventID string) []affiliateLinkStats {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list affiliate links status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var links []affiliateLinkStats
	if err := json.Unmarshal(body.Data, &links); err != nil {
		t.Fatalf("decode affiliate links: %v", err)
	}
	return links
}

// affiliateLinkStatsByCode finds one link's row in the staff list.
func affiliateLinkStatsByCode(t *testing.T, env *testEnv, sessionID, eventID, code string) affiliateLinkStats {
	t.Helper()
	for _, link := range affiliateLinkStatsList(t, env, sessionID, eventID) {
		if link.Code == code {
			return link
		}
	}
	t.Fatalf("no Affiliate Link with code %q in the Event's list", code)
	return affiliateLinkStats{}
}

// newAffiliateLink creates a link and returns its code.
func newAffiliateLink(t *testing.T, env *testEnv, sessionID, eventID, name string) string {
	t.Helper()
	_, body := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": name})
	if body.Error != nil {
		t.Fatalf("create affiliate link error=%+v", body.Error)
	}
	return decodeAffiliateLink(t, body.Data).Code
}

// affiliateCheckoutBody is a begin-checkout body carrying the code the buyer's
// last click left behind, exactly as the Storefront BFF forwards it.
func affiliateCheckoutBody(email, code string, lines ...map[string]any) map[string]any {
	body := checkoutBody(email, "Ana", "Buyer", lines...)
	body["affiliate_code"] = code
	return body
}

func TestOnlineSaleBegunWithAnAffiliateCodeIsAttributedToThatLink(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, sessionID, "Ref Fest", "ref-fest", 1000, 10)
	code := newAffiliateLink(t, env, sessionID, eventID, "María's Instagram")

	begun := beginCheckoutOK(t, env, "test-org", "ref-fest",
		affiliateCheckoutBody("ana@example.com", code, cartLine(ticketTypeID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	link := affiliateLinkStatsByCode(t, env, sessionID, eventID, code)
	if link.SalesCount != 1 {
		t.Fatalf("sales_count=%d, want the one attributed Ticket Sale", link.SalesCount)
	}
	// The attributed Net Proceeds are the Event's own Net Proceeds here, because
	// this Event's only sale is the attributed one. Asserted against the existing
	// per-Event figure rather than against arithmetic repeated in the test, so the
	// two surfaces cannot drift apart.
	summary := salesSummaryOK(t, env, sessionID, eventID)
	if summary.NetProceedsCents <= 0 {
		t.Fatalf("the Event's Net Proceeds are %d; the fixture is not exercising the figure", summary.NetProceedsCents)
	}
	if link.NetProceedsCents != summary.NetProceedsCents {
		t.Fatalf("net_proceeds_cents=%d, want the Event's own %d", link.NetProceedsCents, summary.NetProceedsCents)
	}
}

// A free claim never reaches a Payment Provider and never comes back through
// confirm (ADR 0017), so attribution rides the one thing both settlements share:
// the sale-commit chokepoint. It counts as a sale and adds nothing to the money,
// which is what an Affiliate Link that promoted a free Event honestly did.
func TestFreeOnlineSaleBegunWithAnAffiliateCodeIsAttributedWithNoNetProceeds(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID := publishCheckoutEvent(t, env, sessionID, "Free Fest", "free-fest", 0, 10)
	code := newAffiliateLink(t, env, sessionID, eventID, "Community board")

	result := beginCheckoutSettled(t, env, "test-org", "free-fest", "",
		affiliateCheckoutBody("ana@example.com", code, cartLine(freeID, 1)))
	approvedRef(t, result)

	link := affiliateLinkStatsByCode(t, env, sessionID, eventID, code)
	if link.SalesCount != 1 {
		t.Fatalf("sales_count=%d, want the attributed free claim", link.SalesCount)
	}
	if link.NetProceedsCents != 0 {
		t.Fatalf("net_proceeds_cents=%d, want nothing: a free claim left the Organization no money", link.NetProceedsCents)
	}
}

// A dead code is a tag on a URL and nothing more. The buyer completes their
// purchase without ever learning that the code meant nothing, and the sale is
// simply unattributed — which is also what the link's own figures say.
func TestCheckoutWithAnUnknownOrDeadAffiliateCodeSucceedsUnattributed(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, sessionID, "Ref Fest", "ref-fest", 1000, 10)
	liveCode := newAffiliateLink(t, env, sessionID, eventID, "María's Instagram")
	dead := newAffiliateLinkView(t, env, sessionID, eventID, "Last year's flyer")
	deadCode := dead.Code

	if resp, body := updateAffiliateLink(t, env, sessionID, eventID, dead.ID, map[string]any{"active": false}); resp.StatusCode != http.StatusOK {
		t.Fatalf("deactivate affiliate link status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// A code that never existed, a mistyped one, and one that has been
	// deactivated: three ways of naming nobody, all of which sell a ticket.
	for i, code := range []string{"NOSUCHCODE", "M4RIAS-1NSTAGRAM", deadCode} {
		begun := beginCheckoutOK(t, env, "test-org", "ref-fest",
			affiliateCheckoutBody(fmt.Sprintf("buyer%d@example.com", i), code, cartLine(ticketTypeID, 1)))
		confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	}

	// Three sales on the Event, and not one of them credited to anybody.
	if summary := salesSummaryOK(t, env, sessionID, eventID); summary.SalesCount != 3 {
		t.Fatalf("the Event recorded %d sales, want the 3 that checked out", summary.SalesCount)
	}
	for _, link := range affiliateLinkStatsList(t, env, sessionID, eventID) {
		if link.SalesCount != 0 || link.NetProceedsCents != 0 {
			t.Fatalf("%q was credited %d sales / %d cents by a code that named nobody",
				link.Name, link.SalesCount, link.NetProceedsCents)
		}
	}
	// The live code is untouched by any of it: it still attributes.
	begun := beginCheckoutOK(t, env, "test-org", "ref-fest",
		affiliateCheckoutBody("ana@example.com", liveCode, cartLine(ticketTypeID, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	if link := affiliateLinkStatsByCode(t, env, sessionID, eventID, liveCode); link.SalesCount != 1 {
		t.Fatalf("sales_count=%d on the live link, want the sale it drove", link.SalesCount)
	}
}

// A Sale Reversal drops the sale out of the Affiliate Link's figures the way it
// drops out of every other money surface — by any route, which is why the sale
// here is voided through the status a reversal leaves rather than through one
// particular endpoint.
func TestReversedAttributedSaleDropsOutOfTheAffiliateLinksFigures(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, sessionID, "Ref Fest", "ref-fest", 1000, 10)
	code := newAffiliateLink(t, env, sessionID, eventID, "María's Instagram")

	var refs []string
	for i := 0; i < 2; i++ {
		begun := beginCheckoutOK(t, env, "test-org", "ref-fest",
			affiliateCheckoutBody(fmt.Sprintf("buyer%d@example.com", i), code, cartLine(ticketTypeID, 1)))
		refs = append(refs, confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved").ConfirmationRef)
	}

	before := affiliateLinkStatsByCode(t, env, sessionID, eventID, code)
	if before.SalesCount != 2 || before.NetProceedsCents <= 0 {
		t.Fatalf("before the reversal: %+v, want 2 attributed sales with money behind them", before)
	}

	reverseSale(t, env, refs[0])

	after := affiliateLinkStatsByCode(t, env, sessionID, eventID, code)
	if after.SalesCount != 1 {
		t.Fatalf("sales_count=%d after one reversal, want the 1 sale still standing", after.SalesCount)
	}
	if after.NetProceedsCents != before.NetProceedsCents/2 {
		t.Fatalf("net_proceeds_cents=%d after one of two identical sales was reversed, want %d",
			after.NetProceedsCents, before.NetProceedsCents/2)
	}
	// The reversed sale is still an Affiliate Attribution — it simply stops
	// counting — so the figures track the Event's own, which drop by the same
	// sale.
	if summary := salesSummaryOK(t, env, sessionID, eventID); summary.NetProceedsCents != after.NetProceedsCents {
		t.Fatalf("attributed Net Proceeds %d, the Event's own %d: the two figures disagree about one reversal",
			after.NetProceedsCents, summary.NetProceedsCents)
	}
}

// Only an Online Sale travels through an Affiliate Link. An imported sale is a
// Member's account of a purchase made elsewhere and has no click behind it, so
// nothing an import carries can credit a link.
func TestImportedSalesCarryNoAffiliateAttribution(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, sessionID, "Ref Fest", "ref-fest", 1000, 10)
	code := newAffiliateLink(t, env, sessionID, eventID, "María's Instagram")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "batch-1",
		"source":          "direct",
		"sales": []map[string]any{{
			"customer_email":      "ana@example.com",
			"customer_first_name": "Ana",
			"customer_last_name":  "Lopez",
			"ticket_type_id":      ticketTypeID,
			"quantity":            2,
			"payment_method":      "cash",
			"sold_at":             "2026-07-01T10:00:00Z",
			// The import format has no field for a code; sent anyway, because what
			// this asserts is that no route into the import channel can attribute.
			"affiliate_code": code,
		}},
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("sale import status=%d error=%+v", resp.StatusCode, body.Error)
	}

	link := affiliateLinkStatsByCode(t, env, sessionID, eventID, code)
	if link.SalesCount != 0 || link.NetProceedsCents != 0 {
		t.Fatalf("an imported sale credited the Affiliate Link: %+v", link)
	}
}
