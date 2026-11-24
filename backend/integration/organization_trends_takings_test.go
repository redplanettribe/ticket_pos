package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Organization Trends' Takings (#862, spec #858, ADRs 0040 and 0081): what
// the Organization made, by Sales Channel, computed exactly as Sales Trends
// computes it and on the same day rules as Tickets Sold. Event Staff are
// never sent it.

// salesTrendsTakingsOn is one Event's Takings on one day as Sales Trends
// reports it, zero when the day is outside its span or sold nothing.
func salesTrendsTakingsOn(trends salesTrends, date string) int {
	for _, day := range trends.Days {
		if day.Date != date {
			continue
		}
		takings := 0
		for _, line := range day.Lines {
			takings += line.TakingsCents
		}
		return takings
	}
	return 0
}

// TestOrganizationTrendsTakingsMatchesSalesTrends: an Online Sale, a
// backdated Sale Import, a reversed import, one in the comparison span and
// one made today read, per bucket and in every total, as the two Events'
// Sales Trends report them, each on its own Sales Channel.
func TestOrganizationTrendsTakingsMatchesSalesTrends(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)

	onlineEvent, onlineGA := publishCheckoutEvent(t, env, admin, "Online Fest", "online-fest", feeTestBaseCents, 100)
	buyOnlineOn(t, env, testOrgSlug, "online-fest", onlineGA, "ana@example.com", 2, time.Date(2026, 7, 5, 18, 0, 0, 0, time.UTC))
	buyOnlineOn(t, env, testOrgSlug, "online-fest", onlineGA, "bob@example.com", 1, time.Date(2026, 7, 2, 17, 0, 0, 0, time.UTC))

	importEvent := createDraftEvent(t, env, admin, "Import Fest", "import-fest")
	importGA := createTicketTypeWithCapacity(t, env, admin, importEvent, "GA", importedPriceCents, 100)
	commitBatch(t, env, admin, importEvent, "history", []map[string]any{
		// Made on 2026-07-01 and 2026-07-02, imported today.
		importSale("bea@example.com", importGA, 5, "2026-07-01T15:00:00Z"),
		importSale("cris@example.com", importGA, 3, "2026-07-02T15:00:00Z"),
		// 2026-06-25, in the previous span.
		importSale("gil@example.com", importGA, 2, "2026-06-25T15:00:00Z"),
		// Earlier today.
		importSale("hal@example.com", importGA, 1, "2026-07-07T06:00:00Z"),
	})
	undone := commitBatch(t, env, admin, importEvent, "undone", []map[string]any{
		importSale("eli@example.com", importGA, 4, "2026-07-02T16:00:00Z"),
	})
	undoBatch(t, env, admin, importEvent, undone)

	onlineTrends := salesTrendsOK(t, env, admin, onlineEvent)
	importTrends := salesTrendsOK(t, env, admin, importEvent)

	got := readOrganizationTrends(t, env, admin, "?span=7d")
	takings := got.Figures.Takings
	if takings == nil {
		t.Fatal("takings is absent for an Org Admin")
	}
	if len(takings.Series) != 3 {
		t.Fatalf("takings series = %+v, want one per Sales Channel", takings.Series)
	}
	online := channelSeries(t, *takings, "online")
	imported := channelSeries(t, *takings, "import")
	inPerson := channelSeries(t, *takings, "in_person")

	wantTotal := 0
	for i, day := range got.Span.Buckets {
		wantOnline := salesTrendsTakingsOn(onlineTrends, day.FirstDay)
		wantImport := salesTrendsTakingsOn(importTrends, day.FirstDay)
		if online.Buckets[i].Value != wantOnline || imported.Buckets[i].Value != wantImport || inPerson.Buckets[i].Value != 0 {
			t.Fatalf("%s takings online=%d import=%d in_person=%d, want Sales Trends' %d and %d and 0",
				day.FirstDay, online.Buckets[i].Value, imported.Buckets[i].Value, inPerson.Buckets[i].Value, wantOnline, wantImport)
		}
		wantTotal += wantOnline + wantImport
	}
	// The reversed import is out and the backdated one is on its own day.
	if want := 3 * importedPriceCents; imported.Buckets[2].Value != want {
		t.Fatalf("2026-07-02 import takings = %d, want %d: the reversed 4 are out", imported.Buckets[2].Value, want)
	}
	if imported.Buckets[1].Value != 5*importedPriceCents {
		t.Fatalf("2026-07-01 import takings = %d, want %d", imported.Buckets[1].Value, 5*importedPriceCents)
	}
	if online.Buckets[5].Value == 0 {
		t.Fatal("2026-07-05 online takings is zero, want the Online Sale's")
	}
	if takings.Total != wantTotal || online.Total+imported.Total != wantTotal {
		t.Fatalf("takings total=%d online=%d import=%d, want %d, Sales Trends' sum over the span", takings.Total, online.Total, imported.Total, wantTotal)
	}
	if want := salesTrendsTakingsOn(importTrends, "2026-06-25"); takings.ComparisonTotal == nil || *takings.ComparisonTotal != want || want != 2*importedPriceCents {
		t.Fatalf("takings comparison_total = %v, want Sales Trends' %d", intOrNil(takings.ComparisonTotal), want)
	}
	if want := salesTrendsTakingsOn(importTrends, "2026-07-07"); imported.Today == nil || !imported.Today.Partial || imported.Today.Value != want {
		t.Fatalf("import takings today = %+v, want Sales Trends' %d, partial", imported.Today, want)
	}
	if takings.Change == nil {
		t.Fatal("takings change is null, want a ratio against the previous span")
	}
}

// TestOrganizationTrendsWithholdsTakingsFromEventStaff: an Org Admin and an
// Event Owner are sent Takings; Event Staff are sent the same counts and no
// Takings at all, never a zero.
func TestOrganizationTrendsWithholdsTakingsFromEventStaff(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	owner := addMemberSession(t, env, admin, "owner@example.com", "event_owner")
	staff := addMemberSession(t, env, admin, "staff@example.com", "event_staff")

	_, ga := publishCheckoutEvent(t, env, admin, "Money Fest", "money-fest", feeTestBaseCents, 100)
	buyOnlineOn(t, env, testOrgSlug, "money-fest", ga, "ana@example.com", 2, time.Date(2026, 7, 3, 18, 0, 0, 0, time.UTC))

	adminTakings := readOrganizationTrends(t, env, admin, "?span=7d").Figures.Takings
	if adminTakings == nil || adminTakings.Total == 0 {
		t.Fatalf("admin takings = %+v, want the Online Sale's", adminTakings)
	}
	ownerTakings := readOrganizationTrends(t, env, owner, "?span=7d").Figures.Takings
	if ownerTakings == nil || ownerTakings.Total != adminTakings.Total {
		t.Fatalf("owner takings = %+v, want the admin's %d", ownerTakings, adminTakings.Total)
	}

	resp, body := env.get(t, organizationTrendsPath+"?span=7d", authHeader(staff))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("staff organization trends status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var raw struct {
		Figures map[string]json.RawMessage `json:"figures"`
	}
	if err := json.Unmarshal(body.Data, &raw); err != nil {
		t.Fatalf("decode staff organization trends: %v", err)
	}
	if takings, ok := raw.Figures["takings"]; ok && string(takings) != "null" {
		t.Fatalf("staff are sent takings %s, want it absent", takings)
	}
	var counts organizationTrends
	if err := json.Unmarshal(body.Data, &counts); err != nil {
		t.Fatalf("decode staff organization trends: %v", err)
	}
	if counts.Figures.TicketsSold.Total != 2 || counts.Figures.TicketSales.Total != 1 {
		t.Fatalf("staff counts tickets=%d sales=%d, want 2 and 1", counts.Figures.TicketsSold.Total, counts.Figures.TicketSales.Total)
	}
}
