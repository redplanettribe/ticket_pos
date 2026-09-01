package integration

import (
	"net/http"
	"strings"
	"testing"
)

// THE CONSENT ACCESS LOG AND ITS READER (#569, spec #556, ADR 0067): the
// platform's record of its own reads of people's data.
//
// WHAT THIS SLICE MUST PROVE, and half of it is about rows that must NOT exist:
//
//   - A LIST READ RECORDS THE QUESTION AND NEVER THE ROSTER, and `search_term`
//     is a BOOLEAN — so an operator looking one person up by address does not
//     thereby write that address into an audit log. That is the assertion that
//     would fail first if somebody "improved" the log by storing what was
//     searched for, which would be this feature causing the harm it detects.
//   - A SUBJECT READ AND AN EXPORT ARE LOGGED BY NAME, and an export carries
//     the pack's own fingerprint — the same value the response header and the
//     filename carry.
//   - A STAFF SUBJECT IS A PLAIN ADDRESS AND NEVER THE DIGEST, so a key
//     rotation cannot orphan the log.
//   - READING THE LOG IS LOGGED, after the page is served, so a read never
//     appears in its own results.
//   - A WITHDRAWAL, A PREVIEW, AND PUBLISH/CORRECT/SCHEDULE/CANCEL WRITE
//     NOTHING. Every row in this table is a touch of somebody's data; that is
//     what makes it readable, and it is one careless call away from ceasing to
//     be true.
//   - DELETING A CUSTOMER WITH LOG ROWS FAILS LOUDLY, so an erasure escalates
//     to counsel rather than silently destroying the record of who read
//     somebody's file.
//   - THE READER FILTERS BY ACTOR, ACT AND DATE, AND OFFERS NO SUBJECT FILTER.

const accessLogPath = "/api/v1/operator/legal/access-log"

// accessLogRow is one row read straight from the table.
//
// READ FROM SQL AND NOT THROUGH THE READER, deliberately: the reader is one of
// the things under test, and asserting the writes through it would let a bug
// that dropped a column hide behind a payload that never carried it. The
// pointers are the nullable columns, and a nil here is the CHECK's "this act
// has no such thing".
type accessLogRow struct {
	Act               string
	ActorEmail        string
	Population        *string
	Document          *string
	StatusFilter      *string
	SearchTerm        *bool
	ResultCount       *int
	SubjectCustomerID *string
	SubjectEmail      *string
	PackSHA256        *string
}

// accessLogRows is the whole table, oldest first.
func accessLogRows(t *testing.T, env *testEnv) []accessLogRow {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT act, actor_email, population, document, status_filter, search_term, result_count,
		       subject_customer_id::text, subject_email, pack_sha256
		FROM consent_access_log
		ORDER BY id
	`)
	if err != nil {
		t.Fatalf("read consent_access_log: %v", err)
	}
	defer rows.Close()

	var out []accessLogRow
	for rows.Next() {
		var row accessLogRow
		if err := rows.Scan(&row.Act, &row.ActorEmail, &row.Population, &row.Document, &row.StatusFilter,
			&row.SearchTerm, &row.ResultCount, &row.SubjectCustomerID, &row.SubjectEmail, &row.PackSHA256); err != nil {
			t.Fatalf("scan consent_access_log: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read consent_access_log: %v", err)
	}
	return out
}

// accessLogRowsOf narrows to one act.
func accessLogRowsOf(t *testing.T, env *testEnv, act string) []accessLogRow {
	t.Helper()
	var out []accessLogRow
	for _, row := range accessLogRows(t, env) {
		if row.Act == act {
			out = append(out, row)
		}
	}
	return out
}

// wantNoTextAnywhereInTheLog asserts that a string appears in NO text column of
// the table.
//
// The blunt instrument on purpose: it is not enough that the column somebody
// thought of is clean. An address must not reach the log through
// `status_filter`, through a future column, or through anything else, and this
// asks the database rather than the code.
func wantNoTextAnywhereInTheLog(t *testing.T, env *testEnv, needle, why string) {
	t.Helper()
	var found int
	if err := env.db.QueryRow(`
		SELECT count(*) FROM consent_access_log
		WHERE actor_email = $1
		   OR coalesce(population, '') = $1
		   OR coalesce(document, '') = $1
		   OR coalesce(status_filter, '') = $1
		   OR coalesce(subject_email, '') = $1
		   OR coalesce(pack_sha256, '') = $1
	`, needle).Scan(&found); err != nil {
		t.Fatalf("scan the log for %q: %v", needle, err)
	}
	if found != 0 {
		t.Fatalf("%q appears in %d log row(s): %s", needle, found, why)
	}
}

// accessLogEntryView is one row as the READER serves it.
type accessLogEntryView struct {
	ID                int64   `json:"id"`
	Act               string  `json:"act"`
	ActorEmail        string  `json:"actor_email"`
	OccurredAt        string  `json:"occurred_at"`
	Population        *string `json:"population"`
	Document          *string `json:"document"`
	StatusFilter      *string `json:"status_filter"`
	Searched          *bool   `json:"searched"`
	ResultCount       *int    `json:"result_count"`
	SubjectCustomerID *string `json:"subject_customer_id"`
	SubjectEmail      *string `json:"subject_email"`
	PackSHA256        *string `json:"pack_sha256"`
}

type accessLogPageView struct {
	Entries    []accessLogEntryView `json:"entries"`
	NextCursor *string              `json:"next_cursor"`
}

func readAccessLog(t *testing.T, env *testEnv, sessionID, query string) accessLogPageView {
	t.Helper()
	path := accessLogPath
	if query != "" {
		path += "?" + query
	}
	var page accessLogPageView
	operatorGetOK(t, env, sessionID, path, &page)
	return page
}

// TestABrowsePageRecordsTheQuestionAndNeverTheRoster is the rule that keeps
// this table from becoming a second copy of the thing it audits — and the one
// that keeps a lookup from depositing somebody's address in an audit log.
func TestABrowsePageRecordsTheQuestionAndNeverTheRoster(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	signInAnswering(t, env, "ana@example.com", true, true, false)

	// A search that finds exactly one person: the operator typed an address.
	page := browseCustomers(t, env, sessionID, customerBrowserPolicyPath,
		map[string]any{"standing": "current", "search_email": "ana@example.com"})
	if len(page.Rows) != 1 {
		t.Fatalf("browse returned %d rows; want the one person searched for", len(page.Rows))
	}

	rows := accessLogRowsOf(t, env, "list_read")
	if len(rows) != 1 {
		t.Fatalf("list_read rows = %d; want one per page served", len(rows))
	}
	row := rows[0]
	if row.ActorEmail != "operator@example.com" {
		t.Fatalf("actor = %q; want the operator's session address", row.ActorEmail)
	}
	// THE QUESTION, WHOLE: which population, which document, which filter, and
	// how many rows came back.
	if row.Population == nil || *row.Population != "customer" ||
		row.Document == nil || *row.Document != "policy" ||
		row.StatusFilter == nil || *row.StatusFilter != "current" {
		t.Fatalf("the question was not recorded whole: %+v", row)
	}
	if row.ResultCount == nil || *row.ResultCount != 1 {
		t.Fatalf("result_count = %v; want the one row served", row.ResultCount)
	}
	// SEARCH_TERM IS A BOOLEAN. It says a search narrowed the page, which is the
	// audit-relevant fact, and says nothing about what was typed.
	if row.SearchTerm == nil || !*row.SearchTerm {
		t.Fatalf("search_term = %v; want true for a narrowed page", row.SearchTerm)
	}
	// AND NO SUBJECT. A list read is about nobody in particular; a subject here
	// would mean the roster had begun leaking in one name at a time.
	if row.SubjectCustomerID != nil || row.SubjectEmail != nil || row.PackSHA256 != nil {
		t.Fatalf("a list read named a subject: %+v", row)
	}
	// THE ADDRESS SEARCHED FOR IS NOWHERE IN THE TABLE.
	wantNoTextAnywhereInTheLog(t, env, "ana@example.com",
		"searching for somebody must not accumulate their address in the log that records the searching")

	// An UNSEARCHED page records the same shape with `searched` false, so the
	// two are distinguishable — "this was a lookup" and "this was a sweep" are
	// different facts about an operator's day.
	browseCustomers(t, env, sessionID, customerBrowserPolicyPath, map[string]any{"standing": "outstanding"})
	rows = accessLogRowsOf(t, env, "list_read")
	if len(rows) != 2 {
		t.Fatalf("list_read rows = %d; want one per page served", len(rows))
	}
	if rows[1].SearchTerm == nil || *rows[1].SearchTerm {
		t.Fatalf("search_term = %v on an unsearched page", rows[1].SearchTerm)
	}
	if rows[1].StatusFilter == nil || *rows[1].StatusFilter != "outstanding" {
		t.Fatalf("status_filter = %v; want the default that was actually applied", rows[1].StatusFilter)
	}
}

// TestASubjectReadAndAnExportAreLoggedByName: on these two acts the subject IS
// the act, so leaving them out would record nothing worth keeping.
func TestASubjectReadAndAnExportAreLoggedByName(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	setConsentClock(fixedClock)
	signInAnswering(t, env, "ana@example.com", true, true, false)
	customerID := customerIDFor(t, env, "ana@example.com")

	readCustomerLegalRecord(t, env, sessionID, customerID)

	reads := accessLogRowsOf(t, env, "subject_read")
	if len(reads) != 1 {
		t.Fatalf("subject_read rows = %d; want the one record opened", len(reads))
	}
	read := reads[0]
	if read.SubjectEmail == nil || *read.SubjectEmail != "ana@example.com" {
		t.Fatalf("subject_email = %v; the subject is the act", read.SubjectEmail)
	}
	if read.SubjectCustomerID == nil || *read.SubjectCustomerID != customerID {
		t.Fatalf("subject_customer_id = %v; want the id the act was keyed on", read.SubjectCustomerID)
	}
	// The filter columns are forbidden on a subject read: it asked no question
	// about a population, and a count here would count nothing.
	if read.Population != nil || read.Document != nil || read.StatusFilter != nil ||
		read.SearchTerm != nil || read.ResultCount != nil || read.PackSHA256 != nil {
		t.Fatalf("a subject read carries a list read's columns: %+v", read)
	}

	// THE PAGED HISTORY IS ALSO A READ OF SOMEBODY'S DATA, and is logged (#569).
	// It serves twenty-five of this person's consent acts, on a route anybody
	// holding an operator token can call WITHOUT EVER OPENING THE RECORD, so
	// exempting it would leave the one publicly reachable read of somebody's
	// evidence unrecorded. A duplicate row when an operator pages is honest; a
	// silent page is not. The exemption stayed where it belongs — on the
	// INTERNAL callers, so this screen still writes one row per request the
	// operator actually made rather than three for one page load.
	readConsentActs(t, env, sessionID, customerID, "")
	reads = accessLogRowsOf(t, env, "subject_read")
	if len(reads) != 2 {
		t.Fatalf("subject_read rows = %d after paging the history; want the record opening and the page", len(reads))
	}
	for _, row := range reads {
		if row.SubjectEmail == nil || *row.SubjectEmail != "ana@example.com" ||
			row.SubjectCustomerID == nil || *row.SubjectCustomerID != customerID {
			t.Fatalf("a subject read named %+v; want the person whose data was served", row)
		}
	}

	// THE EXPORT, with the file's own fingerprint.
	pack := downloadPack(t, env, sessionID, evidencePackPath(customerID))
	exports := accessLogRowsOf(t, env, "evidence_export")
	if len(exports) != 1 {
		t.Fatalf("evidence_export rows = %d; want the one pack handed over", len(exports))
	}
	export := exports[0]
	if export.PackSHA256 == nil || *export.PackSHA256 != pack.SHA256 {
		t.Fatalf("pack_sha256 = %v; want the X-Pack-SHA256 of the file that was sent (%q)",
			export.PackSHA256, pack.SHA256)
	}
	if export.SubjectEmail == nil || *export.SubjectEmail != "ana@example.com" ||
		export.SubjectCustomerID == nil || *export.SubjectCustomerID != customerID {
		t.Fatalf("the export named %+v; want the person it disclosed", export)
	}
	// The fingerprint is also what the filename is keyed on, so the row, the
	// header and the file in somebody's mailbox all meet on one value.
	if !strings.Contains(pack.Filename, pack.SHA256[:16]) {
		t.Fatalf("filename %q does not carry the fingerprint the log recorded", pack.Filename)
	}
	// THE PACK'S OWN WALK OF THE HISTORY IS NOT A SUBJECT READ. It pages the
	// same acts internally, and an export is one act with one row of its own.
	if rows := accessLogRowsOf(t, env, "subject_read"); len(rows) != 2 {
		t.Fatalf("subject_read rows = %d after the export; the pack's internal act walk must add none", len(rows))
	}
}

// TestAStaffSubjectIsLoggedAsAnAddressAndNeverTheDigest: a key rotation must
// not orphan a log whose purpose is to stay meaningful for years.
func TestAStaffSubjectIsLoggedAsAnAddressAndNeverTheDigest(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "zz-operator@example.com")
	orgID := seedOrganization(t, env, "log-org")
	setConsentClock(fixedClock)
	seedMember(t, env, orgID, "ana@example.com")
	seedStaffAcceptance(t, env, "ana@example.com", currentEditionID(t, env, "terms_versions"))

	digest := staffDigestFromBrowser(t, env, sessionID, "ana@example.com")
	readStaffLegalRecord(t, env, sessionID, digest)

	reads := accessLogRowsOf(t, env, "subject_read")
	if len(reads) != 1 {
		t.Fatalf("subject_read rows = %d; want the one staff record opened", len(reads))
	}
	if reads[0].SubjectEmail == nil || *reads[0].SubjectEmail != "ana@example.com" {
		t.Fatalf("subject_email = %v; a staff subject is recorded as a plain address", reads[0].SubjectEmail)
	}
	// AND NO CUSTOMER ID: what was read is the STAFF record, and a link to a
	// Customer record would name a screen nobody opened.
	if reads[0].SubjectCustomerID != nil {
		t.Fatalf("subject_customer_id = %v on a staff record read", reads[0].SubjectCustomerID)
	}
	wantNoTextAnywhereInTheLog(t, env, digest,
		"the Staff Digest is derived from a rotatable key and must be written to no row")

	// The same rule on the export route.
	pack := downloadPack(t, env, sessionID, staffEvidencePackPath(digest))
	exports := accessLogRowsOf(t, env, "evidence_export")
	if len(exports) != 1 || exports[0].SubjectEmail == nil || *exports[0].SubjectEmail != "ana@example.com" {
		t.Fatalf("staff export rows = %+v; want one, naming the address", exports)
	}
	if exports[0].PackSHA256 == nil || *exports[0].PackSHA256 != pack.SHA256 {
		t.Fatalf("pack_sha256 = %v; want %q", exports[0].PackSHA256, pack.SHA256)
	}
	wantNoTextAnywhereInTheLog(t, env, digest, "not through the export route either")
}

// TestReadingTheAuditLogIsItselfLogged: a touch of people's data is recorded
// however it is reached, and the reader of the log is not exempt from it.
func TestReadingTheAuditLogIsItselfLogged(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	setConsentClock(fixedClock)
	signInAnswering(t, env, "ana@example.com", true, true, false)
	readCustomerLegalRecord(t, env, sessionID, customerIDFor(t, env, "ana@example.com"))

	// THE READ DOES NOT APPEAR IN ITS OWN RESULTS. The row is written after the
	// page is built, so the first page shows the record opening and nothing
	// about itself.
	first := readAccessLog(t, env, sessionID, "")
	if len(first.Entries) != 1 || first.Entries[0].Act != "subject_read" {
		t.Fatalf("first page = %+v; want the one subject read and not itself", first.Entries)
	}

	audits := accessLogRowsOf(t, env, "audit_read")
	if len(audits) != 1 {
		t.Fatalf("audit_read rows = %d; want one per page of the log served", len(audits))
	}
	audit := audits[0]
	if audit.ActorEmail != "operator@example.com" {
		t.Fatalf("actor = %q", audit.ActorEmail)
	}
	// The count on the row is the count that was actually shown.
	if audit.ResultCount == nil || *audit.ResultCount != 1 {
		t.Fatalf("result_count = %v; want the one entry served", audit.ResultCount)
	}
	// It browsed no population and no document, and migration 116 refuses a row
	// that claims otherwise.
	if audit.Population != nil || audit.Document != nil ||
		audit.SubjectEmail != nil || audit.SubjectCustomerID != nil || audit.PackSHA256 != nil {
		t.Fatalf("an audit read claimed a population or a subject: %+v", audit)
	}
	if audit.SearchTerm == nil || *audit.SearchTerm {
		t.Fatalf("search_term = %v; want false for an unfiltered read", audit.SearchTerm)
	}

	// The second read sees the first one's row.
	second := readAccessLog(t, env, sessionID, "")
	if len(second.Entries) != 2 || second.Entries[0].Act != "audit_read" {
		t.Fatalf("second page = %+v; want the audit read the first one wrote, newest first", second.Entries)
	}
}

// TestAWithdrawalAPreviewAndAPublicationWriteNoAccessLogRows is the half of
// this feature made of absences.
//
// Each of these acts is already evidenced by a row that says MORE than a log
// line could: a withdrawal by its Consent Record, a preview by #562's logger
// line, a publication by its provenance columns. Adding a second record of any
// of them would be two facts that can disagree — and, worse, would fill this
// table with the platform's own housekeeping, so that nobody could scan it for
// the thing it exists to show.
func TestAWithdrawalAPreviewAndAPublicationWriteNoAccessLogRows(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	setConsentClock(fixedClock)
	signInAnswering(t, env, "ana@example.com", true, true, false)

	// A WITHDRAWAL. The Consent Record it writes names the operator, the
	// reference and the prior values; a log row would be strictly less.
	recordOperatorWithdrawalOK(t, env, sessionID, "ana@example.com", map[string]any{
		"marketing_consent": false,
		"request_reference": paperFormRef,
	})
	if rows := accessLogRows(t, env); len(rows) != 0 {
		t.Fatalf("a withdrawal wrote %d access log row(s): %+v", len(rows), rows)
	}

	// A PREVIEW, and the diff being seen. Both are #562's logger lines.
	fresh := legalPublishWorkspace(t, env, sessionID, operatorLegalPolicyPath)
	saveLegalDraftAt(t, env, sessionID, operatorLegalPolicyPath,
		draftBody([]string{"en", "es"}, rewordOne(fresh.Draft.Artifacts, "policy", "en", "The policy, restated.")))
	reviewWholeDraft(t, env, sessionID, operatorLegalPolicyPath)
	if rows := accessLogRows(t, env); len(rows) != 0 {
		t.Fatalf("previewing and diffing wrote %d access log row(s): %+v", len(rows), rows)
	}

	// A SCHEDULED GATING PUBLICATION, then its cancellation. Both are provenance
	// columns on the version row (#563, #564), so provenance cannot drift from
	// the edition it describes. The date is counted from the DATABASE's day —
	// the harness's clock is months behind the calendar — which is what
	// scheduleGatingEdition is for.
	versionID, _ := scheduleGatingEdition(t, env, sessionID, operatorLegalPolicyPath, 3)
	if rows := accessLogRows(t, env); len(rows) != 0 {
		t.Fatalf("a publication wrote %d access log row(s): %+v", len(rows), rows)
	}
	cancelLegalEditionOK(t, env, sessionID, operatorLegalPolicyPath, versionID)
	if rows := accessLogRows(t, env); len(rows) != 0 {
		t.Fatalf("a cancellation wrote %d access log row(s): %+v", len(rows), rows)
	}

	// A CORRECTION, which re-gates nobody and is still not an access act.
	corrected := legalPublishWorkspace(t, env, sessionID, operatorLegalTermsPath)
	saveLegalDraftAt(t, env, sessionID, operatorLegalTermsPath,
		draftBody(corrected.Draft.PublishedLocales,
			rewordOne(corrected.Draft.Artifacts, "terms", "en", "The terms, restated.")))
	reviewWholeDraft(t, env, sessionID, operatorLegalTermsPath)
	publishLegalOK(t, env, sessionID, operatorLegalTermsPath, map[string]any{
		"kind":   "correction",
		"reason": "The English text said 'buyer' where it meant 'holder'.",
	})
	if rows := accessLogRows(t, env); len(rows) != 0 {
		t.Fatalf("a correction wrote %d access log row(s): %+v", len(rows), rows)
	}
}

// TestAHalfWrittenAccessLogRowIsRefused: the act decides the shape of its own
// row, and the shape is enforced in the DATABASE.
//
// STRAIGHT SQL, DELIBERATELY. Every column is nullable on its own — each applies
// to some acts and not others — so without the act-whole CHECK a `list_read`
// with no count or an `evidence_export` with no subject would be a perfectly
// legal row. Rows like that are worse than absent: an audit log with a hole in
// it reads as evidence right up until somebody relies on it. Testing through the
// Go writers would only prove the Go writers are careful today; the constraint
// has to hold for a hand-typed psql INSERT too, which is how the one table on
// this platform whose credibility is its whole purpose stays credible.
func TestAHalfWrittenAccessLogRowIsRefused(t *testing.T) {
	env := setupTest(t)

	for _, half := range []struct {
		why    string
		insert string
	}{
		{"a list read with no count is a question with no answer", `
			INSERT INTO consent_access_log (act, actor_email, population, document, status_filter, search_term)
			VALUES ('list_read', 'operator@example.com', 'customer', 'policy', 'outstanding', FALSE)`},
		{"a list read with no population is a question about nothing", `
			INSERT INTO consent_access_log (act, actor_email, document, status_filter, search_term, result_count)
			VALUES ('list_read', 'operator@example.com', 'policy', 'outstanding', FALSE, 3)`},
		{"a list read that named a subject means the roster has begun leaking in", `
			INSERT INTO consent_access_log (act, actor_email, population, document, status_filter, search_term, result_count, subject_email)
			VALUES ('list_read', 'operator@example.com', 'customer', 'policy', 'outstanding', FALSE, 3, 'ana@example.com')`},
		{"a subject read with no subject records nothing worth keeping", `
			INSERT INTO consent_access_log (act, actor_email)
			VALUES ('subject_read', 'operator@example.com')`},
		{"a subject read carrying a count is a number with nothing to count", `
			INSERT INTO consent_access_log (act, actor_email, subject_email, result_count)
			VALUES ('subject_read', 'operator@example.com', 'ana@example.com', 3)`},
		{"an export with no fingerprint cannot be matched to the file that was sent", `
			INSERT INTO consent_access_log (act, actor_email, subject_email)
			VALUES ('evidence_export', 'operator@example.com', 'ana@example.com')`},
		{"only an export has a pack fingerprint", `
			INSERT INTO consent_access_log (act, actor_email, subject_email, pack_sha256)
			VALUES ('subject_read', 'operator@example.com', 'ana@example.com', repeat('a', 64))`},
		{"an audit read browses no population", `
			INSERT INTO consent_access_log (act, actor_email, population, search_term, result_count)
			VALUES ('audit_read', 'operator@example.com', 'customer', FALSE, 0)`},
		{"an actor is never blank: the address is the evidence", `
			INSERT INTO consent_access_log (act, actor_email, subject_email)
			VALUES ('subject_read', '', 'ana@example.com')`},
	} {
		if _, err := env.db.Exec(half.insert); err == nil {
			t.Fatalf("the database accepted a half-written row: %s", half.why)
		}
	}

	// And the whole rows the platform actually writes are accepted, so the
	// constraint above is refusing shapes rather than refusing everything.
	for _, whole := range []string{`
		INSERT INTO consent_access_log (act, actor_email, population, document, status_filter, search_term, result_count)
		VALUES ('list_read', 'operator@example.com', 'staff', 'terms', 'outstanding', TRUE, 0)`, `
		INSERT INTO consent_access_log (act, actor_email, status_filter, search_term, result_count)
		VALUES ('audit_read', 'operator@example.com', 'subject_read', FALSE, 12)`, `
		INSERT INTO consent_access_log (act, actor_email, subject_email)
		VALUES ('subject_read', 'operator@example.com', 'ana@example.com')`, `
		INSERT INTO consent_access_log (act, actor_email, subject_email, pack_sha256)
		VALUES ('evidence_export', 'operator@example.com', 'ana@example.com', repeat('a', 64))`,
	} {
		if _, err := env.db.Exec(whole); err != nil {
			t.Fatalf("the database refused a whole row: %v", err)
		}
	}
}

// TestDeletingACustomerWithLogRowsFailsLoudly: the FK is RESTRICT, matching
// `consent_records`, so an erasure escalates to counsel rather than quietly
// destroying the record of who read somebody's file.
func TestDeletingACustomerWithLogRowsFailsLoudly(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	setConsentClock(fixedClock)
	signInAnswering(t, env, "ana@example.com", true, true, false)
	customerID := customerIDFor(t, env, "ana@example.com")

	readCustomerLegalRecord(t, env, sessionID, customerID)
	if rows := accessLogRowsOf(t, env, "subject_read"); len(rows) != 1 {
		t.Fatalf("no subject read to protect: %d rows", len(rows))
	}

	// The Consent Records are removed first, so what refuses the DELETE is THIS
	// table and not the one beside it: the test would otherwise pass on
	// migration 061's constraint and prove nothing about 116's.
	if _, err := env.db.Exec(`DELETE FROM consent_records WHERE customer_id = $1`, customerID); err != nil {
		t.Fatalf("clear the consent records: %v", err)
	}
	if _, err := env.db.Exec(`DELETE FROM customers WHERE id = $1`, customerID); err == nil {
		t.Fatal("deleting a Customer with access log rows succeeded; the FK must be RESTRICT so erasure escalates")
	} else if !strings.Contains(strings.ToLower(err.Error()), "consent_access_log") {
		t.Fatalf("the refusal did not name the access log: %v", err)
	}
}

// TestTheReaderFiltersByActorActAndDateAndNeverBySubject.
func TestTheReaderFiltersByActorActAndDateAndNeverBySubject(t *testing.T) {
	env := setupTest(t)
	first := operatorSession(t, env, "aa-operator@example.com")
	second := operatorSession(t, env, "zz-operator@example.com")
	setConsentClock(fixedClock)
	signInAnswering(t, env, "ana@example.com", true, true, false)
	customerID := customerIDFor(t, env, "ana@example.com")

	// Two operators, two kinds of act.
	browseCustomers(t, env, first, customerBrowserPolicyPath, map[string]any{"standing": "current"})
	readCustomerLegalRecord(t, env, second, customerID)

	// BY ACTOR.
	byActor := readAccessLog(t, env, first, "actor=zz-operator%40example.com")
	if len(byActor.Entries) != 1 || byActor.Entries[0].ActorEmail != "zz-operator@example.com" {
		t.Fatalf("filtering by actor gave %+v", byActor.Entries)
	}

	// BY ACT. The audit read the previous request wrote is now in the table, so
	// asking for `list_read` must exclude it as well as the subject read.
	byAct := readAccessLog(t, env, first, "act=list_read")
	if len(byAct.Entries) != 1 || byAct.Entries[0].Act != "list_read" {
		t.Fatalf("filtering by act gave %+v", byAct.Entries)
	}

	// BY DATE. A window in the past excludes everything; one that includes
	// today includes it. `to` is INCLUSIVE OF ITS WHOLE DAY — a naive
	// `occurred_at <= to` would hide a day of acts, which on an audit screen is
	// a wrong answer rather than an inconvenience.
	if page := readAccessLog(t, env, first, "from=2000-01-01&to=2000-01-02"); len(page.Entries) != 0 {
		t.Fatalf("a window in 2000 returned %d entries", len(page.Entries))
	}
	today := readAccessLog(t, env, first, "from=2000-01-01")
	if len(today.Entries) == 0 {
		t.Fatal("an open-ended window returned nothing")
	}

	// AN UNKNOWN ACT IS REFUSED AND NEVER WIDENED. A screen that says it is
	// narrowed while showing everything is a lie about what happened.
	resp, body := env.get(t, accessLogPath+"?act=everything", authHeader(first))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "LEGAL_ACCESS_ACT_UNKNOWN" {
		t.Fatalf("act=everything gave status=%d error=%+v", resp.StatusCode, body.Error)
	}
	// A DATE THAT IS NOT A DAY IS REFUSED TOO, rather than dropped: a window
	// silently widened looks exactly like more access having happened.
	resp, body = env.get(t, accessLogPath+"?from=last-tuesday", authHeader(first))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "LEGAL_ACCESS_DATE_INVALID" {
		t.Fatalf("from=last-tuesday gave status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// AND THERE IS NO SUBJECT FILTER. Every spelling somebody might reach for is
	// IGNORED rather than honoured: the log must not become a second way to look
	// people up.
	full := readAccessLog(t, env, first, "")
	for _, attempt := range []string{
		"subject=ana%40example.com",
		"subject_email=ana%40example.com",
		"email=ana%40example.com",
		"customer_id=" + customerID,
	} {
		page := readAccessLog(t, env, first, attempt)
		if len(page.Entries) < len(full.Entries) {
			t.Fatalf("%q narrowed the log to %d entries; the audit log must not be searchable by subject",
				attempt, len(page.Entries))
		}
	}
}

// TestTheAccessLogIsOperatorOnlyAndHasNoPurgeOrExport: the absences, at the
// HTTP seam where somebody would add them.
func TestTheAccessLogIsOperatorOnlyAndHasNoPurgeOrExport(t *testing.T) {
	env := setupTest(t)

	// An Org Admin of a real Organization, not on the operator allowlist, gets
	// nothing: org roles grant nothing platform-wide (ADR 0015). The route is
	// also on operatorRoutes, so the namespace's gate is asserted over it there
	// too; this says it again where the log's own absences are written down.
	resp, body := env.get(t, accessLogPath, authHeader(orgAdminSession(t, env)))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a non-operator read the access log: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	sessionID := operatorSession(t, env, "operator@example.com")
	// NO PURGE AND NO EXPORT. Retention is unbounded by design — evidence that
	// ages out is evidence the platform cannot produce on the day it is asked
	// for — and the log is read where it lives.
	for _, absent := range []struct {
		method string
		path   string
	}{
		{http.MethodDelete, accessLogPath},
		{http.MethodPost, accessLogPath},
		{http.MethodDelete, accessLogPath + "/purge"},
		{http.MethodGet, accessLogPath + "/export"},
		{http.MethodGet, accessLogPath + ".csv"},
	} {
		// The status is read RAW, without decoding an envelope: a route that
		// does not exist is answered by the mux and not by this platform's
		// error writer, so there is no JSON to parse — which is itself the
		// point of the assertion.
		req, err := http.NewRequest(absent.method, env.server.URL+absent.path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+sessionID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s status=%d; there is no purge and no export", absent.method, absent.path, resp.StatusCode)
		}
	}
}
