package integration

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

// taxIDPassport is the passport snapshot these tests seed. Its characters are
// deliberately disjoint from the cédula fixture's digits so a partial-number
// search can only match one of the two sales.
const taxIDPassport = "XZ998877"

// The Tax ID on the staff Sales list (#100, ADR 0016): the unified search box
// gains a fourth branch over the sale's immutable Tax ID snapshot — an organizer
// pastes the full cédula/RUC/passport read off an ID card, or door staff type
// the last four digits — and every row carries the snapshot so the match is
// confirmable at a glance. Sales recorded before the Tax ID existed (and
// imported ones that never collected it) carry a null pair and must keep
// listing, sorting, and paginating exactly as before.
//
// Everything here asserts through GET /api/v1/staff/events/{id}/sales.

// stampSaleTaxID writes a Tax ID snapshot onto an already-recorded Ticket Sale.
// SQL because the Sale Import channel does not carry Tax ID columns yet (#99);
// the write is exactly what the sale-recording path stores, and the snapshot's
// journey from the buyer's form onto the row is proven end-to-end through the
// Storefront checkout in TestSalesListRowCarriesTheTaxIDSnapshot below.
func stampSaleTaxID(t *testing.T, env *testEnv, eventID, email, taxIDType, number string) {
	t.Helper()
	res, err := env.db.Exec(`
		UPDATE ticket_sales
		SET customer_tax_id_type = $1, customer_tax_id_number = $2
		WHERE event_id = $3 AND customer_email = $4
	`, taxIDType, number, eventID, email)
	if err != nil {
		t.Fatalf("stamp tax id on %s: %v", email, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		t.Fatalf("stamp tax id on %s: no sale matched", email)
	}
}

// salesSearch runs the Sales list unified search and returns the matching rows.
func salesSearch(t *testing.T, env *testEnv, sessionID, eventID, q string) []saleListRow {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?q="+url.QueryEscape(q), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list q=%q status=%d error=%+v", q, resp.StatusCode, body.Error)
	}
	return salesList(t, body).Data
}

// taxIDSeedEvent seeds one Event with three imported sales: two carrying Tax ID
// snapshots (a cédula and a passport) and one legacy sale with none.
func taxIDSeedEvent(t *testing.T, env *testEnv, sessionID, name, slug string) string {
	t.Helper()
	eventID := createDraftEvent(t, env, sessionID, name, slug)
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	commitBatch(t, env, sessionID, eventID, slug+"-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bea@example.com", "customer_first_name": "Bea", "customer_last_name": "Mora", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
		{"customer_email": "cara@example.com", "customer_first_name": "Cara", "customer_last_name": "Diaz", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-03T10:00:00Z"},
	})
	stampSaleTaxID(t, env, eventID, "ana@example.com", "cedula", validCedula)
	stampSaleTaxID(t, env, eventID, "bea@example.com", "passport", taxIDPassport)
	// cara's sale keeps the null pair: the legacy/imported case.
	return eventID
}

// emailsOf returns the sorted customer emails of a row slice, for
// order-independent set assertions.
func emailsOf(rows []saleListRow) []string {
	return listSalesEmails(salesListEnvelope{Data: rows})
}

// onlyEmail asserts a search matched exactly one sale, and returns its row.
func onlyEmail(t *testing.T, rows []saleListRow, q, want string) saleListRow {
	t.Helper()
	if len(rows) != 1 || rows[0].CustomerEmail != want {
		t.Fatalf("q=%q matched %v, want [%s]", q, emailsOf(rows), want)
	}
	return rows[0]
}

// TestSalesListRowCarriesTheTaxIDSnapshot proves each row exposes the Tax ID the
// sale was transacted under — including a real Online Sale's, carried from the
// checkout form all the way onto the list — and that a sale without one returns
// both halves null rather than an empty string.
func TestSalesListRowCarriesTheTaxIDSnapshot(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// A real Online Sale: the buyer's typed Tax ID must reach the Sales list.
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Tax List Fest", "tax-list-fest", 1000, 10)
	begun := beginCheckoutOK(t, env, "test-org", "tax-list-fest",
		taxIDCheckoutBody("online@example.com", "Olga", "Nieto", "ruc", naturalRUC,
			map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if confirmed := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}

	// An imported sale that never collected one.
	commitBatch(t, env, sessionID, eventID, "legacy-batch", []map[string]any{
		{"customer_email": "legacy@example.com", "customer_first_name": "Leo", "customer_last_name": "Vera", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	rows := salesList(t, body).Data
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want the online and the imported sale", emailsOf(rows))
	}

	byEmail := map[string]saleListRow{}
	for _, row := range rows {
		byEmail[row.CustomerEmail] = row
	}

	online := byEmail["online@example.com"]
	if online.TaxIDType == nil || *online.TaxIDType != "ruc" {
		t.Fatalf("online sale tax_id_type = %v, want ruc", online.TaxIDType)
	}
	if online.TaxIDNumber == nil || *online.TaxIDNumber != naturalRUC {
		t.Fatalf("online sale tax_id_number = %v, want %s", online.TaxIDNumber, naturalRUC)
	}

	legacy := byEmail["legacy@example.com"]
	if legacy.TaxIDType != nil || legacy.TaxIDNumber != nil {
		t.Fatalf("legacy sale tax id = %v/%v, want both null", legacy.TaxIDType, legacy.TaxIDNumber)
	}
}

// TestSalesListSearchesByTaxID proves the unified search's Tax ID branch: a full
// number pasted off an ID card, the last digits read at the door, and a
// case-insensitive passport — none of which disturb the email/name/reference
// branches.
func TestSalesListSearchesByTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := taxIDSeedEvent(t, env, sessionID, "Search Tax Fest", "search-tax-fest")

	// Full number, as pasted from an ID card.
	onlyEmail(t, salesSearch(t, env, sessionID, eventID, validCedula), validCedula, "ana@example.com")

	// Partial: the last four digits read off the card at the door.
	last4 := validCedula[len(validCedula)-4:]
	onlyEmail(t, salesSearch(t, env, sessionID, eventID, last4), last4, "ana@example.com")

	// Partial from the middle of the number.
	mid := validCedula[2:6]
	onlyEmail(t, salesSearch(t, env, sessionID, eventID, mid), mid, "ana@example.com")

	// Passport, matched case-insensitively like every other search branch.
	onlyEmail(t, salesSearch(t, env, sessionID, eventID, "xz9988"), "xz9988", "bea@example.com")

	// The pre-existing branches still work alongside it.
	onlyEmail(t, salesSearch(t, env, sessionID, eventID, "cara@"), "cara@", "cara@example.com")
	onlyEmail(t, salesSearch(t, env, sessionID, eventID, "Mora"), "Mora", "bea@example.com")

	// A number nobody bought under matches nothing — including the sale with no
	// Tax ID, which a null must never make match.
	if rows := salesSearch(t, env, sessionID, eventID, otherCedula); len(rows) != 0 {
		t.Fatalf("q=%s matched %v, want none", otherCedula, emailsOf(rows))
	}
}

// TestSalesListTaxIDSearchEscapesLikeMetacharacters proves the Tax ID branch is
// escaped like the others: a typed `_` or `%` is matched literally, so a
// mistyped digit cannot turn into a wildcard that "finds" the wrong buyer.
func TestSalesListTaxIDSearchEscapesLikeMetacharacters(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := taxIDSeedEvent(t, env, sessionID, "Escape Tax Fest", "escape-tax-fest")

	// `_` is the single-character wildcard: unescaped, this would match Ana's
	// cédula. Escaped, it is a literal underscore no Tax ID contains.
	underscore := validCedula[:len(validCedula)-1] + "_"
	if rows := salesSearch(t, env, sessionID, eventID, underscore); len(rows) != 0 {
		t.Fatalf("q=%q matched %v, want none (underscore must be literal)", underscore, emailsOf(rows))
	}

	// `%` unescaped matches every row.
	if rows := salesSearch(t, env, sessionID, eventID, "%"); len(rows) != 0 {
		t.Fatalf("q=%% matched %v, want none (percent must be literal)", emailsOf(rows))
	}

	// A backslash is the escape character itself; it must not corrupt the pattern.
	if rows := salesSearch(t, env, sessionID, eventID, `1712\3456`); len(rows) != 0 {
		t.Fatalf(`q=1712\3456 matched %v, want none`, emailsOf(rows))
	}
}

// TestSalesListTaxIDSearchIsEventScoped proves the Tax ID lookup stays inside
// the Event being viewed: one buyer's cédula on two Events surfaces only the
// sale of the Event whose list was asked for. There is no cross-event lookup.
func TestSalesListTaxIDSearchIsEventScoped(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	firstID := createDraftEvent(t, env, sessionID, "Scoped One", "scoped-one")
	firstGA := createTicketTypeWithCapacity(t, env, sessionID, firstID, "GA", 1000, 100)
	secondID := createDraftEvent(t, env, sessionID, "Scoped Two", "scoped-two")
	secondGA := createTicketTypeWithCapacity(t, env, sessionID, secondID, "GA", 1000, 100)

	commitBatch(t, env, sessionID, firstID, "scoped-one-batch", []map[string]any{
		{"customer_email": "first@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": firstGA, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	commitBatch(t, env, sessionID, secondID, "scoped-two-batch", []map[string]any{
		{"customer_email": "second@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": secondGA, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	// The same person, the same cédula, a sale on each Event.
	stampSaleTaxID(t, env, firstID, "first@example.com", "cedula", validCedula)
	stampSaleTaxID(t, env, secondID, "second@example.com", "cedula", validCedula)

	onlyEmail(t, salesSearch(t, env, sessionID, firstID, validCedula), validCedula, "first@example.com")
	onlyEmail(t, salesSearch(t, env, sessionID, secondID, validCedula), validCedula, "second@example.com")
}

// TestSalesListWithoutTaxIDStillSortsAndPaginates proves the null snapshot is
// inert everywhere else on the list: legacy sales still appear, still sort by
// the chosen column, and still page correctly alongside sales that have one.
func TestSalesListWithoutTaxIDStillSortsAndPaginates(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := taxIDSeedEvent(t, env, sessionID, "Paging Tax Fest", "paging-tax-fest")

	// Sorted by customer ascending: Diaz (no Tax ID), Lopez (cédula), Mora
	// (passport) — the null neither sinks nor floats the row out of its place.
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?sort=customer&dir=asc", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sorted list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	sorted := salesList(t, body)
	if len(sorted.Data) != 3 {
		t.Fatalf("sorted rows = %d, want 3", len(sorted.Data))
	}
	wantOrder := []string{"cara@example.com", "ana@example.com", "bea@example.com"}
	for i, want := range wantOrder {
		if sorted.Data[i].CustomerEmail != want {
			t.Fatalf("sorted[%d] = %s, want %s", i, sorted.Data[i].CustomerEmail, want)
		}
	}

	// Paginated two at a time: the totals count every sale, Tax ID or not, and
	// each row appears exactly once across the pages.
	seen := map[string]bool{}
	pages := []struct {
		page     int
		wantRows int
	}{{page: 1, wantRows: 2}, {page: 2, wantRows: 1}}
	for _, p := range pages {
		resp, body := env.get(t,
			"/api/v1/staff/events/"+eventID+"/sales?sort=customer&dir=asc&page_size=2&page="+strconv.Itoa(p.page),
			authHeader(sessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("page %d status=%d error=%+v", p.page, resp.StatusCode, body.Error)
		}
		list := salesList(t, body)
		if list.Pagination.Total != 3 || list.Pagination.TotalPages != 2 {
			t.Fatalf("page %d pagination = %+v, want total 3 over 2 pages", p.page, list.Pagination)
		}
		if len(list.Data) != p.wantRows {
			t.Fatalf("page %d rows = %d, want %d", p.page, len(list.Data), p.wantRows)
		}
		for _, row := range list.Data {
			if seen[row.CustomerEmail] {
				t.Fatalf("page %d repeated %s", p.page, row.CustomerEmail)
			}
			seen[row.CustomerEmail] = true
		}
	}
	if len(seen) != 3 {
		t.Fatalf("paged rows = %v, want all three sales", seen)
	}

	// The searched-for sale pages too: a Tax ID search returning one row reports
	// a single-page result, not the unfiltered total.
	resp, body = env.get(t,
		"/api/v1/staff/events/"+eventID+"/sales?page_size=2&q="+url.QueryEscape(validCedula),
		authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("searched page status=%d error=%+v", resp.StatusCode, body.Error)
	}
	searched := salesList(t, body)
	if searched.Pagination.Total != 1 || searched.Pagination.TotalPages != 1 {
		t.Fatalf("searched pagination = %+v, want 1 sale on 1 page", searched.Pagination)
	}
}
