package integration

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// The two acceptance browsers (#565, spec #556, ADR 0067): who owes an
// acceptance, in the two populations that can owe one.
//
// WHAT THIS SLICE MUST PROVE, beyond "the endpoint returns rows":
//
//   - The four states are DISTINGUISHABLE. Never seen is not a kind of
//     Outstanding — on the production copy a third of the customer base has
//     never accepted anything, and folding them in would bury the handful who
//     owe a re-acceptance.
//   - FORMER exists on the staff screen, is computed rather than stored, and is
//     a filter value rather than a hidden state: a leaver appears when asked
//     for and NOWHERE ELSE, because an outstanding list must be people somebody
//     can chase.
//   - A CORRECTION MOVES NOBODY INTO OUTSTANDING. This is the property #560
//     bought and the one a future refactor is most likely to break by
//     reintroducing equality against "the current edition".
//   - Keyset paging walks the whole set with no row skipped and none repeated,
//     and an unparseable cursor serves the first page rather than an error.
//   - No email ever needs to reach a URL: every request here is a POST with a
//     body, and the cursor — which IS an email — travels in it.

const (
	customerBrowserPolicyPath = "/api/v1/operator/legal/acceptances/customers/policy"
	customerBrowserTermsPath  = "/api/v1/operator/legal/acceptances/customers/terms"
	staffBrowserTermsPath     = "/api/v1/operator/legal/acceptances/staff/terms"
)

type customerAcceptanceRowView struct {
	CustomerID     string `json:"customer_id"`
	Email          string `json:"email"`
	Name           string `json:"name"`
	PolicyStanding string `json:"policy_standing"`
	TermsStanding  string `json:"terms_standing"`
}

type customerAcceptancePageView struct {
	Rows       []customerAcceptanceRowView `json:"rows"`
	NextCursor *string                     `json:"next_cursor"`
}

type staffAcceptanceRowView struct {
	Digest   string `json:"digest"`
	Email    string `json:"email"`
	Standing string `json:"standing"`
}

type staffAcceptancePageView struct {
	Rows       []staffAcceptanceRowView `json:"rows"`
	NextCursor *string                  `json:"next_cursor"`
}

// browseCustomers posts one page request. The body is the whole request — no
// query string — because two of its fields are email addresses.
func browseCustomers(t *testing.T, env *testEnv, sessionID, path string, body map[string]any) customerAcceptancePageView {
	t.Helper()
	resp, envelopeBody := env.post(t, path, body, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status=%d error=%+v", path, resp.StatusCode, envelopeBody.Error)
	}
	var page customerAcceptancePageView
	decodeInto(t, envelopeBody, &page)
	return page
}

func browseStaff(t *testing.T, env *testEnv, sessionID string, body map[string]any) staffAcceptancePageView {
	t.Helper()
	resp, envelopeBody := env.post(t, staffBrowserTermsPath, body, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status=%d error=%+v", staffBrowserTermsPath, resp.StatusCode, envelopeBody.Error)
	}
	var page staffAcceptancePageView
	decodeInto(t, envelopeBody, &page)
	return page
}

func decodeInto(t *testing.T, body envelope, out any) {
	t.Helper()
	if body.Error != nil {
		t.Fatalf("envelope carried an error: %+v", body.Error)
	}
	if err := json.Unmarshal(body.Data, out); err != nil {
		t.Fatalf("decode page: %v", err)
	}
}

// currentEditionID reads whichever edition of a document is current, the way
// every current-edition selector in the codebase does.
func currentEditionID(t *testing.T, env *testEnv, table string) string {
	t.Helper()
	var id string
	query := fmt.Sprintf(`
		SELECT id FROM %s
		WHERE effective_date <= CURRENT_DATE
		ORDER BY effective_date DESC, created_at DESC LIMIT 1
	`, table)
	if err := env.db.QueryRow(query).Scan(&id); err != nil {
		t.Fatalf("current edition of %s: %v", table, err)
	}
	return id
}

// seedCustomer writes one Customer straight into the table with the acceptance
// state the test needs. Direct SQL rather than a checkout, because what is
// under test is the READ: manufacturing 60 people through the consent capture
// path would take a minute and prove nothing about the browser.
//
// An empty edition id is stored as NULL — which is the "never seen" state and,
// per migration 062, the honest state of every Customer a box-office sale or a
// Sale Import ever created.
func seedCustomer(t *testing.T, env *testEnv, email, policyEdition, termsEdition string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`
		INSERT INTO customers (email, first_name, last_name,
		                       policy_accepted_at, policy_version_id,
		                       terms_accepted_at, terms_version_id)
		VALUES ($1, 'Test', 'Person',
		        CASE WHEN $2 = '' THEN NULL ELSE NOW() END, NULLIF($2, '')::uuid,
		        CASE WHEN $3 = '' THEN NULL ELSE NOW() END, NULLIF($3, '')::uuid)
		RETURNING id
	`, email, policyEdition, termsEdition).Scan(&id); err != nil {
		t.Fatalf("seed customer %q: %v", email, err)
	}
	return id
}

func emailsOfCustomerPage(page customerAcceptancePageView) []string {
	emails := make([]string, 0, len(page.Rows))
	for _, row := range page.Rows {
		emails = append(emails, row.Email)
	}
	return emails
}

// TestCustomerAcceptanceBrowserTellsTheThreeStatesApart is the whole reason the
// vocabulary has four values instead of two.
func TestCustomerAcceptanceBrowserTellsTheThreeStatesApart(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	// Somebody accepted the edition that is current NOW…
	supersededPolicy := currentEditionID(t, env, "policy_versions")
	currentTerms := currentEditionID(t, env, "terms_versions")
	seedCustomer(t, env, "bb-outstanding@example.com", supersededPolicy, currentTerms)

	// …and then a GATING edition was published, which is the only thing that
	// makes an acceptance stop counting. It moves the gating floor, so what
	// bb-outstanding holds is now below it.
	currentPolicy := publishPolicyVersion(t, env, 2, 0)

	// Three people, three states against the Policy.
	seedCustomer(t, env, "aa-current@example.com", currentPolicy, currentTerms)
	seedCustomer(t, env, "cc-neverseen@example.com", "", "")

	// THE DEFAULT IS OUTSTANDING, and an empty body is the default page: a
	// screen's first request has nothing to say.
	outstanding := browseCustomers(t, env, sessionID, customerBrowserPolicyPath, map[string]any{})
	if got := emailsOfCustomerPage(outstanding); len(got) != 1 || got[0] != "bb-outstanding@example.com" {
		t.Fatalf("default page = %v; want only the person whose acceptance was superseded", got)
	}
	// NEVER SEEN IS NOT OUTSTANDING. If this ever starts returning
	// cc-neverseen, the default filter has been poisoned with a third of the
	// customer base.
	if outstanding.Rows[0].PolicyStanding != "outstanding" {
		t.Fatalf("policy standing = %q; want outstanding", outstanding.Rows[0].PolicyStanding)
	}
	// TWO STATUS COLUMNS ON ONE ROW: this person owes the Policy and is fine on
	// the Terms, and that is one row rather than two.
	if outstanding.Rows[0].TermsStanding != "current" {
		t.Fatalf("terms standing = %q; want current — a person is one row with two columns", outstanding.Rows[0].TermsStanding)
	}
	// The per-subject record is reached by an opaque id, never by an address.
	if outstanding.Rows[0].CustomerID == "" || outstanding.Rows[0].CustomerID == outstanding.Rows[0].Email {
		t.Fatalf("row carries no opaque customer id: %+v", outstanding.Rows[0])
	}
	// NO TOTAL, and nothing that could be mistaken for one.
	if outstanding.NextCursor != nil {
		t.Fatalf("a single-row page named a next cursor: %v", *outstanding.NextCursor)
	}

	neverSeen := browseCustomers(t, env, sessionID, customerBrowserPolicyPath, map[string]any{"standing": "never_seen"})
	if got := emailsOfCustomerPage(neverSeen); len(got) != 1 || got[0] != "cc-neverseen@example.com" {
		t.Fatalf("never_seen page = %v", got)
	}
	if neverSeen.Rows[0].PolicyStanding != "never_seen" || neverSeen.Rows[0].TermsStanding != "never_seen" {
		t.Fatalf("never-seen row = %+v", neverSeen.Rows[0])
	}

	current := browseCustomers(t, env, sessionID, customerBrowserPolicyPath, map[string]any{"standing": "current"})
	if got := emailsOfCustomerPage(current); len(got) != 1 || got[0] != "aa-current@example.com" {
		t.Fatalf("current page = %v", got)
	}

	// FILTERING BY DOCUMENT: the same three people, asked about the Terms
	// instead, give a different answer — which is the point of the filter.
	termsOutstanding := browseCustomers(t, env, sessionID, customerBrowserTermsPath, map[string]any{})
	if got := emailsOfCustomerPage(termsOutstanding); len(got) != 0 {
		t.Fatalf("terms outstanding = %v; nobody's Terms acceptance was superseded", got)
	}
	termsNeverSeen := browseCustomers(t, env, sessionID, customerBrowserTermsPath, map[string]any{"standing": "never_seen"})
	if got := emailsOfCustomerPage(termsNeverSeen); len(got) != 1 || got[0] != "cc-neverseen@example.com" {
		t.Fatalf("terms never_seen = %v", got)
	}
}

// TestCustomerAcceptanceBrowserRefusesWhatItCannotAnswer.
func TestCustomerAcceptanceBrowserRefusesWhatItCannotAnswer(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	// FORMER IS A STAFF STATE. A Customer record is never deleted (a Ticket
	// Sale is a financial record that must reconcile), so there is no departure
	// to observe, and answering would mean guessing from inactivity.
	resp, body := env.post(t, customerBrowserPolicyPath, map[string]any{"standing": "former"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "LEGAL_STANDING_NOT_AVAILABLE" {
		t.Fatalf("former on the customer browser: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// An unknown state is REFUSED and never silently widened — a filter widened
	// to everybody would answer "who owes an acceptance?" with the whole
	// customer base, which on a screen with no total looks like a re-gate.
	// "withdrawn" is the specific value that must not work: withdrawal is a
	// state of an optional consent and of NEITHER gate.
	resp, body = env.post(t, customerBrowserPolicyPath, map[string]any{"standing": "withdrawn"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "LEGAL_STANDING_UNKNOWN" {
		t.Fatalf("withdrawn as a standing: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// A document that is not one of the two is 404, which is what keeps the
	// path total.
	resp, body = env.post(t, "/api/v1/operator/legal/acceptances/customers/marketing", map[string]any{}, authHeader(sessionID))
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "LEGAL_DOCUMENT_NOT_FOUND" {
		t.Fatalf("unknown document: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// And the whole subtree is operator-only. A signed-in non-operator is
	// refused identically whether or not the people behind it exist.
	memberSession := verifyOTP(t, env, "not-an-operator@example.com")
	resp, _ = env.post(t, customerBrowserPolicyPath, map[string]any{}, authHeader(memberSession))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-operator status=%d; want 403", resp.StatusCode)
	}
}

// TestCustomerAcceptanceBrowserWalksTheWholeSetOnAKeysetCursor.
//
// PAGE SIZE 50, NO TOTAL, NO OFFSET (ADR 0067's departure from ADR 0006). The
// walk is the assertion: 60 people, two pages, every address exactly once and
// in ascending order.
func TestCustomerAcceptanceBrowserWalksTheWholeSetOnAKeysetCursor(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	const population = 60
	want := make(map[string]bool, population)
	for i := 0; i < population; i++ {
		email := fmt.Sprintf("owes-%02d@example.com", i)
		seedCustomer(t, env, email, "", "")
		want[email] = true
	}

	first := browseCustomers(t, env, sessionID, customerBrowserPolicyPath, map[string]any{"standing": "never_seen"})
	if len(first.Rows) != 50 {
		t.Fatalf("first page had %d rows; want the page size of 50", len(first.Rows))
	}
	if first.NextCursor == nil {
		t.Fatal("a full page with more behind it named no next cursor")
	}
	// The 51st row is read to learn that it exists and is never rendered.
	if first.Rows[49].Email >= "owes-50@example.com" {
		t.Fatalf("first page ran past the page size: last = %q", first.Rows[49].Email)
	}

	second := browseCustomers(t, env, sessionID, customerBrowserPolicyPath,
		map[string]any{"standing": "never_seen", "cursor": *first.NextCursor})
	if len(second.Rows) != population-50 {
		t.Fatalf("second page had %d rows; want %d", len(second.Rows), population-50)
	}
	if second.NextCursor != nil {
		t.Fatalf("the last page named a next cursor: %v", *second.NextCursor)
	}

	// NOTHING SKIPPED AND NOTHING REPEATED. The seek is strictly greater on a
	// unique sort key, which is what makes that true without a tiebreak column.
	previous := ""
	for _, email := range append(emailsOfCustomerPage(first), emailsOfCustomerPage(second)...) {
		if !want[email] {
			t.Fatalf("%q appeared twice, or is not one of the seeded people", email)
		}
		if email <= previous {
			t.Fatalf("the walk is not ascending: %q came after %q", email, previous)
		}
		previous = email
		delete(want, email)
	}
	if len(want) != 0 {
		t.Fatalf("%d people were never listed: %v", len(want), want)
	}

	// AN UNPARSEABLE CURSOR IS TREATED AS ABSENT and serves the first page —
	// somebody with a stale bookmark gets the top of the list, not an error.
	stale := browseCustomers(t, env, sessionID, customerBrowserPolicyPath,
		map[string]any{"standing": "never_seen", "cursor": "!!!not-a-cursor!!!"})
	if len(stale.Rows) != 50 || stale.Rows[0].Email != first.Rows[0].Email {
		t.Fatalf("an unparseable cursor did not serve the first page: %d rows starting at %q",
			len(stale.Rows), stale.Rows[0].Email)
	}
}

// TestAcceptanceBrowserSearchesByEmailWithoutPuttingOneInAURL.
func TestAcceptanceBrowserSearchesByEmailWithoutPuttingOneInAURL(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	seedCustomer(t, env, "ana@example.com", "", "")
	seedCustomer(t, env, "bruno@example.com", "", "")

	// The fragment goes in the BODY. So does the cursor, which is itself an
	// address — that is why this is a POST at all.
	found := browseCustomers(t, env, sessionID, customerBrowserPolicyPath,
		map[string]any{"standing": "never_seen", "search_email": "ANA@"})
	if got := emailsOfCustomerPage(found); len(got) != 1 || got[0] != "ana@example.com" {
		t.Fatalf("search for ANA@ = %v; the fragment must fold the way a stored address does", got)
	}

	// The search composes with the filter rather than replacing it: asking for
	// somebody who is current, by name, while filtering on outstanding, finds
	// nobody. "Is this person outstanding?" is one question, not two screens.
	none := browseCustomers(t, env, sessionID, customerBrowserPolicyPath,
		map[string]any{"standing": "current", "search_email": "ana@"})
	if got := emailsOfCustomerPage(none); len(got) != 0 {
		t.Fatalf("search widened the filter: %v", got)
	}
}

// TestACorrectionMovesNobodyIntoOutstanding is the property #560 bought, and
// the one this browser exists on top of.
//
// A correction takes the next revision within its generation and is published
// ABOVE the gating floor, so it joins the satisfying set without moving the
// floor. Everybody who was Current stays Current — no backfill, no
// notification, no column to maintain. A regression here means somebody
// reintroduced equality against "the current edition", and the visible symptom
// would be the entire customer base appearing in the outstanding list the
// moment a typo was fixed.
func TestACorrectionMovesNobodyIntoOutstanding(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	currentTerms := currentEditionID(t, env, "terms_versions")
	seedCustomer(t, env, "settled@example.com", "", currentTerms)

	before := browseCustomers(t, env, sessionID, customerBrowserTermsPath, map[string]any{"standing": "current"})
	if got := emailsOfCustomerPage(before); len(got) != 1 || got[0] != "settled@example.com" {
		t.Fatalf("before the correction, current = %v", got)
	}

	// Publish 1.1 — a CORRECTION to the generation the seeded edition belongs
	// to, not a new generation.
	publishTermsVersion(t, env, 1, 1)

	after := browseCustomers(t, env, sessionID, customerBrowserTermsPath, map[string]any{"standing": "current"})
	if got := emailsOfCustomerPage(after); len(got) != 1 || got[0] != "settled@example.com" {
		t.Fatalf("after the correction, current = %v; a correction re-gates nobody", got)
	}
	outstanding := browseCustomers(t, env, sessionID, customerBrowserTermsPath, map[string]any{})
	if got := emailsOfCustomerPage(outstanding); len(got) != 0 {
		t.Fatalf("a correction moved %v into outstanding; it must move nobody", got)
	}

	// And the contrast that proves the test is not vacuous: a GATING edition
	// (generation 2, revision 0) DOES move them.
	publishTermsVersion(t, env, 2, 0)
	regated := browseCustomers(t, env, sessionID, customerBrowserTermsPath, map[string]any{})
	if got := emailsOfCustomerPage(regated); len(got) != 1 || got[0] != "settled@example.com" {
		t.Fatalf("a gating edition left %v outstanding; it must re-gate everybody", got)
	}
}

// seedOrganization creates one Organization directly, because these tests need
// somewhere for a Member row to hang and nothing about the Organization itself.
func seedOrganization(t *testing.T, env *testEnv, slug string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`
		INSERT INTO organizations (name, slug) VALUES ($1, $1) RETURNING id
	`, slug).Scan(&id); err != nil {
		t.Fatalf("seed organization %q: %v", slug, err)
	}
	return id
}

// seedMember writes one staff person into an Organization the ordinary way a
// Member exists: a row.
func seedMember(t *testing.T, env *testEnv, orgID, email string) {
	t.Helper()
	if _, err := env.db.Exec(`
		INSERT INTO members (organization_id, email, role) VALUES ($1, $2, 'event_staff')
		ON CONFLICT (organization_id, email) DO NOTHING
	`, orgID, email); err != nil {
		t.Fatalf("seed member %q: %v", email, err)
	}
}

// seedStaffAcceptance appends one Staff Terms Acceptance. Direct SQL, because
// the table is append-only evidence and what is under test is the read.
func seedStaffAcceptance(t *testing.T, env *testEnv, email, editionID string) {
	t.Helper()
	if _, err := env.db.Exec(`
		INSERT INTO staff_terms_acceptances (email, terms_version_id, capacity, accepted_at)
		VALUES ($1, $2, 'organizer', NOW())
		ON CONFLICT (email, terms_version_id, capacity) DO NOTHING
	`, email, editionID); err != nil {
		t.Fatalf("seed staff acceptance for %q: %v", email, err)
	}
}

func staffStandingByEmail(page staffAcceptancePageView) map[string]string {
	standings := make(map[string]string, len(page.Rows))
	for _, row := range page.Rows {
		standings[row.Email] = row.Standing
	}
	return standings
}

// TestStaffAcceptanceBrowserCoversTheWholePopulationAndComputesFormer.
func TestStaffAcceptanceBrowserCoversTheWholePopulationAndComputesFormer(t *testing.T) {
	env := setupTest(t)
	// The operator is themselves in the population, through the
	// `platform_operators` arm — which is the arm that exists so the org-less
	// operator is not missing from the screen they are reading.
	sessionID := operatorSession(t, env, "zz-operator@example.com")
	orgID := seedOrganization(t, env, "browser-org")

	// The operator's own sign-in accepted the edition current at the time; a
	// GATING publication then supersedes it, which is the only thing that ever
	// makes an acceptance stop counting.
	supersededTerms := currentEditionID(t, env, "terms_versions")
	seedMember(t, env, orgID, "bb-outstanding@example.com")
	seedStaffAcceptance(t, env, "bb-outstanding@example.com", supersededTerms)

	currentTerms := publishTermsVersion(t, env, 2, 0)

	seedMember(t, env, orgID, "aa-current@example.com")
	seedStaffAcceptance(t, env, "aa-current@example.com", currentTerms)
	seedMember(t, env, orgID, "cc-neverseen@example.com")
	// THE LEAVER: an acceptance row and no membership anywhere. Their departure
	// is recorded nowhere except by the absence of a `members` row, which is why
	// Former is computed and never stored.
	seedStaffAcceptance(t, env, "dd-former@example.com", currentTerms)

	// FORMER IS NOT OUTSTANDING. A leaver owes nothing — they cannot sign in —
	// so listing them here would fill the default filter with people nobody can
	// chase.
	outstanding := browseStaff(t, env, sessionID, map[string]any{})
	standings := staffStandingByEmail(outstanding)
	if _, listed := standings["dd-former@example.com"]; listed {
		t.Fatalf("the leaver appeared among the outstanding: %+v", outstanding.Rows)
	}
	if standings["bb-outstanding@example.com"] != "outstanding" {
		t.Fatalf("outstanding page = %+v", outstanding.Rows)
	}
	if _, listed := standings["cc-neverseen@example.com"]; listed {
		t.Fatal("somebody who has never accepted anything appeared among the outstanding")
	}

	neverSeen := staffStandingByEmail(browseStaff(t, env, sessionID, map[string]any{"standing": "never_seen"}))
	if neverSeen["cc-neverseen@example.com"] != "never_seen" {
		t.Fatalf("never_seen page missed the person who has never accepted: %v", neverSeen)
	}
	current := staffStandingByEmail(browseStaff(t, env, sessionID, map[string]any{"standing": "current"}))
	if current["aa-current@example.com"] != "current" {
		t.Fatalf("current page = %v", current)
	}
	// THE ORG-LESS PLATFORM OPERATOR IS IN THE POPULATION, through the second
	// arm of the union and only through it: they hold no `members` row at all.
	// They accepted the edition superseded above, so the person reading this
	// screen is on it, outstanding — which is the honest answer.
	if standings["zz-operator@example.com"] != "outstanding" {
		t.Fatalf("the org-less Platform Operator is missing from the population: %v", standings)
	}

	// FORMER IS A FILTER VALUE: ask for it and the leaver is there, computed
	// from the evidence row minus the membership tables.
	former := browseStaff(t, env, sessionID, map[string]any{"standing": "former"})
	formerStandings := staffStandingByEmail(former)
	if formerStandings["dd-former@example.com"] != "former" {
		t.Fatalf("former page = %+v", former.Rows)
	}
	if len(former.Rows) != 1 {
		t.Fatalf("former page listed %d people; only the leaver has left", len(former.Rows))
	}

	// THE STAFF SCREEN IS ABOUT THE TERMS AND ONLY THE TERMS: there is one
	// staff gate, staff accept no Privacy Policy, and asking is 404 rather than
	// an empty list that would read as "nobody owes it".
	resp, body := env.post(t, "/api/v1/operator/legal/acceptances/staff/policy", map[string]any{}, authHeader(sessionID))
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "LEGAL_DOCUMENT_NOT_FOUND" {
		t.Fatalf("staff × policy: status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// TestStaffAcceptanceRowsAreKeyedOnADigestAndNotAnAddress.
//
// A staff person has no id, so the row must carry something a link can name
// them by — and #565's rule is that their address must never appear in a URL, a
// query string or a referer. The digest is that something.
func TestStaffAcceptanceRowsAreKeyedOnADigestAndNotAnAddress(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "zz-operator@example.com")
	orgID := seedOrganization(t, env, "digest-org")

	seedMember(t, env, orgID, "aa-one@example.com")
	seedMember(t, env, orgID, "bb-two@example.com")

	page := browseStaff(t, env, sessionID, map[string]any{"standing": "never_seen"})
	digests := map[string]string{}
	for _, row := range page.Rows {
		if len(row.Digest) != 32 {
			t.Fatalf("digest %q is %d chars; want 32", row.Digest, len(row.Digest))
		}
		if _, err := hex.DecodeString(row.Digest); err != nil {
			t.Fatalf("digest %q is not hex", row.Digest)
		}
		if row.Digest == row.Email {
			t.Fatal("the digest is the address")
		}
		if previous, clash := digests[row.Digest]; clash {
			t.Fatalf("%q and %q share a digest", previous, row.Email)
		}
		digests[row.Digest] = row.Email
	}
	if len(digests) < 2 {
		t.Fatalf("expected at least the two seeded members, got %+v", page.Rows)
	}

	// STABLE ACROSS REQUESTS, because it is a URL key: a digest that changed
	// per response would be a link that broke on reload.
	again := browseStaff(t, env, sessionID, map[string]any{"standing": "never_seen"})
	for _, row := range again.Rows {
		if digests[row.Digest] != row.Email {
			t.Fatalf("the digest for %q changed between reads", row.Email)
		}
	}

	// STANDING RULE: THE DIGEST IS WRITTEN TO NO ROW. It is minted on the way
	// out of a read and discarded; persisting it would turn a key rotation into
	// a data migration over append-only consent evidence. Asserted by looking
	// for it in every text column of every table — if a later change starts
	// storing it, this fails.
	assertDigestIsStoredNowhere(t, env, digests)
}

// assertDigestIsStoredNowhere sweeps every text-ish column of every table in
// the database for any of the digests just served.
//
// Blunt on purpose. The rule it guards — the digest is a URL key and a screen
// label, never a row — is the kind that is broken by somebody adding a helpful
// cache, and a targeted assertion against the tables that exist today would not
// notice the table added tomorrow.
func assertDigestIsStoredNowhere(t *testing.T, env *testEnv, digests map[string]string) {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT table_name, column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND data_type IN ('text', 'character varying', 'character')
	`)
	if err != nil {
		t.Fatalf("list text columns: %v", err)
	}
	defer rows.Close()

	type column struct{ table, name string }
	var columns []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.table, &c.name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns = append(columns, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list text columns: %v", err)
	}

	for digest := range digests {
		for _, c := range columns {
			var found bool
			query := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %q WHERE %q = $1)`, c.table, c.name)
			if err := env.db.QueryRow(query, digest).Scan(&found); err != nil {
				t.Fatalf("search %s.%s: %v", c.table, c.name, err)
			}
			if found {
				t.Fatalf("the Staff Digest for %s was written to %s.%s; it must live in no row at all",
					digests[digest], c.table, c.name)
			}
		}
	}
}
