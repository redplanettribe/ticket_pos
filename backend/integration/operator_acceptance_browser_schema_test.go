package integration

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// Migration 114's two supports (#565, ADR 0067): the composite indexes a
// filtered keyset page is served from, and the email normalisation the Staff
// Digest is derived from.
//
// A separate file from the browsers' behaviour, because these are assertions
// about the SCHEMA rather than about a screen: they fail when somebody drops a
// constraint or an index, not when somebody changes what a page returns.

// TestMigration114ConstrainsTheFourStaffTablesAndNotCustomers.
//
// The email-normalisation CHECK is what the Staff Digest rests on: the digest
// is derived from platform.NormalizeEmail(email), so a row holding
// `Ana@Example.com` would be listed under the digest of `ana@example.com` and
// reached by it, while `members` and `platform_operators` would still hold a
// second spelling that no query on this screen would join back to. Two
// spellings of one address are two people to a screen keyed on a fold of one of
// them.
//
// It matters most on `platform_operators`, whose rows are inserted BY HAND IN
// PSQL in production — migration 024 says there will never be a UI — so the one
// table whose rows bypass every Go normalisation path is the one that grants
// platform-wide authority.
//
// `customers` IS DELIBERATELY EXCLUDED, and the exclusion is ASSERTED rather
// than merely omitted: it is UUID-keyed, needs no digest, is by far the largest
// of the five, and already carries UNIQUE(email) plus normalisation on every
// entry path (migration 016). A later reader adding it "for consistency" should
// have to delete this assertion first.
func TestMigration114ConstrainsTheFourStaffTablesAndNotCustomers(t *testing.T) {
	env := setupTest(t)

	for _, table := range []string{"platform_operators", "members", "staff_locales", "staff_terms_acceptances"} {
		if !hasEmailNormalizationCheck(t, env, table) {
			t.Fatalf("%s carries no email-normalisation CHECK; the Staff Digest is derived from a fold this table does not enforce", table)
		}
	}
	if hasEmailNormalizationCheck(t, env, "customers") {
		t.Fatal("customers gained an email-normalisation CHECK; #565 excluded it deliberately")
	}

	// And the hand-typed psql row this exists for is REFUSED, rather than
	// becoming a second person on the staff browser.
	if _, err := env.db.Exec(`INSERT INTO platform_operators (email) VALUES ('Shouting@Example.com')`); err == nil {
		t.Fatal("an unnormalised operator address was accepted")
	}
}

func hasEmailNormalizationCheck(t *testing.T, env *testEnv, table string) bool {
	t.Helper()
	var present bool
	if err := env.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint c
			JOIN pg_class t ON t.oid = c.conrelid
			WHERE t.relname = $1 AND c.contype = 'c'
			  AND pg_get_constraintdef(c.oid) ILIKE '%lower(btrim(email))%'
		)
	`, table).Scan(&present); err != nil {
		t.Fatalf("read constraints on %s: %v", table, err)
	}
	return present
}

// TestAFilteredKeysetPageIsServedFromTheCompositeIndex.
//
// Both indexes must EXIST and must MATCH THE QUERY SHAPE — a filter on the
// version column plus an ascending walk on email. The planner is nudged off
// sequential scans first, because on a test table of a few dozen rows a seq
// scan is genuinely cheaper and a plan that chose it would prove nothing either
// way. What is under test is that the index CAN serve this page, which is what
// stops the screen going quadratic after a re-gate puts the whole customer base
// in the outstanding set.
func TestAFilteredKeysetPageIsServedFromTheCompositeIndex(t *testing.T) {
	env := setupTest(t)

	for _, index := range []string{"customers_policy_version_email_idx", "customers_terms_version_email_idx"} {
		var present bool
		if err := env.db.QueryRow(
			`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE tablename = 'customers' AND indexname = $1)`,
			index,
		).Scan(&present); err != nil {
			t.Fatalf("read indexes: %v", err)
		}
		if !present {
			t.Fatalf("%s is missing; a filtered keyset page would scan the table", index)
		}
	}

	currentPolicy := currentEditionID(t, env, "policy_versions")
	for i := 0; i < 40; i++ {
		seedCustomer(t, env, fmt.Sprintf("idx-%02d@example.com", i), currentPolicy, "")
	}
	if _, err := env.db.Exec(`ANALYZE customers`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	// ONE TRANSACTION, so the setting and the EXPLAIN are on the SAME pooled
	// connection — `SET` on a pool is a coin toss about which connection hears
	// it — and SET LOCAL, so it is unwound with the rollback.
	tx, err := env.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}

	plan := explainPlan(t, tx, currentPolicy)

	// THE PROPERTY, AND NOT THE INDEX NAME. What must be true of every page this
	// screen serves is that it is an ORDERED INDEX WALK the LIMIT can stop
	// early: no sequential scan, and — the load-bearing half — NO SORT, because
	// a sort has to read the whole filtered set before it can return the first
	// row, which is exactly the quadratic behaviour the keyset design exists to
	// avoid when a re-gate puts everybody in the outstanding set.
	//
	// Which index the planner picks is its business and changes with
	// selectivity: on a filter that matches nearly everybody it may walk
	// `customers_email_key` and filter, which is equally an ordered walk. What
	// the composite indexes buy is the SELECTIVE case, where walking the unique
	// email index would mean discarding most of the table. Asserting the name
	// would pin a cost estimate rather than a property.
	if strings.Contains(plan, "Seq Scan") || strings.Contains(plan, "Sort") {
		t.Fatalf("the filtered keyset page is not an ordered index walk:\n%s", plan)
	}
	if !strings.Contains(plan, "Index Scan") {
		t.Fatalf("the filtered keyset page is not served from any index:\n%s", plan)
	}

	// And the composite index IS the one chosen once the filter is selective —
	// one edition among many, which is what the screen asks after a
	// publication.
	selective := publishPolicyVersion(t, env, 2, 0)
	seedCustomer(t, env, "zz-selective@example.com", selective, "")
	if _, err := tx.Exec(`ANALYZE customers`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	selectivePlan := explainPlan(t, tx, selective)
	if !strings.Contains(selectivePlan, "customers_policy_version_email_idx") {
		t.Fatalf("a selective filtered keyset page does not use the composite index:\n%s", selectivePlan)
	}
}

// explainPlan runs the browser's own page shape and returns the plan text.
func explainPlan(t *testing.T, tx interface {
	Query(query string, args ...any) (*sql.Rows, error)
}, editionID string) string {
	t.Helper()
	rows, err := tx.Query(`
		EXPLAIN SELECT id, email FROM customers
		WHERE policy_version_id = ANY($1::uuid[]) AND ($2 = '' OR email > $2)
		ORDER BY email ASC LIMIT 51
	`, []string{editionID}, "")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	return plan.String()
}
