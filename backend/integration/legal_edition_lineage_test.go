package integration

// Edition lineage and the satisfying set (#560, parent #556, ADR 0067).
//
// Two integers on each version table replace "the current edition" as the thing
// an acceptance is measured against: an edition is GATING iff `revision = 0`,
// the gating floor is the newest arrived gating edition, and an acceptance
// clears if it names any edition at or above that floor. The point is what does
// NOT happen — publishing a correction re-gates nobody, for free, with no
// backfill and no column to maintain.
//
// The gates themselves are driven through HTTP, because they are the only thing
// a person can observe. The schema facts are asserted through SQL, because they
// are deliberately not published anywhere: they are the shape the rest of the
// Legal Center stands on.

import (
	"net/http"
	"testing"
	"time"
)

// TestEditionLineageIsBackfilledAndCarriesNoGatingFlag pins the schema (#560).
//
// Every existing edition on both tables carries a generation and a revision,
// and NOTHING carries a gating flag: no is_gating column, no gating_from column
// and no published_as column. #551 introduced gating_from for a delayed
// promotion, #553 removed promotion, and the columns went with it — `revision =
// 0` is the whole of "gating", which is why it is immutable rather than merely
// monotonic.
func TestEditionLineageIsBackfilledAndCarriesNoGatingFlag(t *testing.T) {
	env := setupTest(t)

	for _, table := range []string{"policy_versions", "terms_versions"} {
		var unbackfilled int
		if err := env.db.QueryRow(
			`SELECT count(*) FROM ` + table + ` WHERE generation IS NULL OR revision IS NULL`,
		).Scan(&unbackfilled); err != nil {
			t.Fatalf("read %s lineage: %v", table, err)
		}
		if unbackfilled != 0 {
			t.Errorf("%s: %d editions without lineage, want every existing edition backfilled", table, unbackfilled)
		}

		var editions int
		if err := env.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&editions); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if editions == 0 {
			t.Fatalf("%s is empty — the seeded editions are what this asserts about", table)
		}

		for _, column := range []string{"is_gating", "gating_from", "published_as"} {
			var exists bool
			if err := env.db.QueryRow(`
				SELECT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_name = $1 AND column_name = $2
				)
			`, table, column).Scan(&exists); err != nil {
				t.Fatalf("look for %s.%s: %v", table, column, err)
			}
			if exists {
				t.Errorf("%s.%s exists: gating is `revision = 0` and nothing else, and a second place to ask is a second place to disagree", table, column)
			}
		}
	}

	// The label is the frozen rendering of the lineage, refused by the database
	// in any other spelling — bar the Privacy Policy's `0-placeholder`, published
	// in migration 060 before there was a rendering to match and deliberately
	// grandfathered rather than renamed out from under the people who accepted it.
	var mislabelled []string
	rows, err := env.db.Query(`
		SELECT label FROM policy_versions
		WHERE label <> generation::text || CASE WHEN revision = 0 THEN '' ELSE '.' || revision::text END
		UNION ALL
		SELECT label FROM terms_versions
		WHERE label <> generation::text || CASE WHEN revision = 0 THEN '' ELSE '.' || revision::text END
	`)
	if err != nil {
		t.Fatalf("read labels: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			t.Fatalf("scan label: %v", err)
		}
		mislabelled = append(mislabelled, label)
	}
	if len(mislabelled) != 1 || mislabelled[0] != "0-placeholder" {
		t.Errorf("labels not rendered from their lineage = %v, want exactly the grandfathered 0-placeholder", mislabelled)
	}
}

// TestByteIdenticalEditionsCoexist proves the rule content_hash must keep: no
// UNIQUE, ever.
//
// Two editions with the same fingerprint are legal, and are the MECHANISM by
// which an operator republishes a correction's text as a real gating edition —
// the correction gated nobody, the republication gates everybody, and the bytes
// are the same bytes. A UNIQUE here would make that publish fail, and the only
// way around it would be to reword the legal text in order to move the
// fingerprint.
func TestByteIdenticalEditionsCoexist(t *testing.T) {
	env := setupTest(t)

	// publishTermsVersion writes the same junk fingerprint into every edition it
	// makes, so these two are byte-identical by construction.
	first := publishTermsVersion(t, env, 2, 0)
	second := publishTermsVersion(t, env, 3, 0)

	var sameHash int
	if err := env.db.QueryRow(`
		SELECT count(*) FROM terms_versions a JOIN terms_versions b ON b.content_hash = a.content_hash
		WHERE a.id = $1 AND b.id = $2
	`, first, second).Scan(&sameHash); err != nil {
		t.Fatalf("compare fingerprints: %v", err)
	}
	if sameHash != 1 {
		t.Fatal("the two published editions were expected to carry the same fingerprint")
	}

	for _, table := range []string{"policy_versions", "terms_versions"} {
		var offender string
		if err := env.db.QueryRow(`
			SELECT COALESCE((
				SELECT i.relname
				FROM pg_index x
				JOIN pg_class t ON t.oid = x.indrelid
				JOIN pg_class i ON i.oid = x.indexrelid
				JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY (x.indkey)
				WHERE t.relname = $1 AND x.indisunique AND a.attname = 'content_hash'
				LIMIT 1
			), '')
		`, table).Scan(&offender); err != nil {
			t.Fatalf("look for a unique on %s.content_hash: %v", table, err)
		}
		if offender != "" {
			t.Errorf("%s.content_hash carries a uniqueness constraint (%s): byte-identical editions must stay insertable", table, offender)
		}
	}
}

// TestCustomerCorrectionReGatesNobodyAndAGatingEditionReGatesEverybody is the
// satisfying set at the Customer sign-in gate, in one story (#560).
//
// Ana accepts what is in force. A CORRECTION is published under her feet: the
// gating floor does not move, so she is asked nothing — this is the whole point
// of the ticket, and the behaviour that used to be impossible because the test
// was equality against the newest row. Then a GATING edition is published: the
// floor rises above what she holds, and she is asked again.
func TestCustomerCorrectionReGatesNobodyAndAGatingEditionReGatesEverybody(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, true)
	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false,
		"she has just answered everything")

	// A correction to the generation in force. It is a real, published edition
	// with its own id, its own text and its own fingerprint — it is simply not
	// gating, and nothing had to be told so.
	correction := publishPolicyVersion(t, env, 1, 1)
	if correction == "" {
		t.Fatal("expected the correction to be a row of its own")
	}
	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false,
		"a CORRECTION re-gates nobody: the gating floor did not move, so her acceptance still satisfies")

	// And the correction itself satisfies, for whoever is shown it: an edition
	// ABOVE the floor clears the gate too, which is what keeps the people who
	// answered most recently from being asked twice.
	bruno := signInAnswering(t, env, "bruno@example.com", true, false, false)
	assertBoxes(t, signedInConsentBoxes(t, env, bruno), false, false, false,
		"bruno accepted the correction, which is above the floor and therefore satisfying")

	// A GATING edition. The floor rises above everything either of them holds.
	publishPolicyVersion(t, env, 2, 0)
	assertBoxes(t, signedInConsentBoxes(t, env, token), true, false, false,
		"a gating publication re-gates the required box ALONE and never churns standing optional answers")
	assertBoxes(t, signedInConsentBoxes(t, env, bruno), true, false, false,
		"the correction is now BELOW the floor and stops satisfying, like everything else beneath it")
}

// TestStaffCorrectionDoesNotReGateButAGatingEditionDoes is the same rule at the
// Staff sign-in gate, over the Terms' parallel table (ADR 0066).
//
// It also pins the half of "unchanged" that matters: a gating edition still
// stops every Organizer at their next sign-in, with no code and no data
// migration, because `effective_date <= CURRENT_DATE` moves on its own and
// nothing is notified that a publish happened.
func TestStaffCorrectionDoesNotReGateButAGatingEditionDoes(t *testing.T) {
	env := setupTest(t)
	email := "lineage@example.com"

	if sessionThroughTermsGate(t, env, requestAndVerify(t, env, email)) == "" {
		t.Fatal("expected a session once edition 1 is accepted")
	}
	if got := countStaffTermsAcceptances(t, env, email); got != 1 {
		t.Fatalf("acceptances=%d, want 1", got)
	}

	// A correction to the generation in force.
	publishTermsVersion(t, env, 1, 1)

	outcome := decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if outcome.TermsRequired != nil {
		t.Fatalf("a correction re-gated a signed-in Organizer: %+v", outcome.TermsRequired)
	}
	if outcome.SessionID == "" {
		t.Fatal("expected the sign-in to mint a session straight through")
	}
	if got := countStaffTermsAcceptances(t, env, email); got != 1 {
		t.Fatalf("acceptances=%d after a correction, want 1 — nothing was asked, so nothing was recorded", got)
	}

	// A gating edition. The floor rises, and the acceptance of edition 1 — now
	// below it — stops satisfying.
	publishTermsVersion(t, env, 2, 0)

	outcome = decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if outcome.TermsRequired == nil {
		t.Fatal("a GATING edition must re-gate the next sign-in")
	}
	if outcome.TermsRequired.Version != "2" {
		t.Fatalf("re-gate names version=%q, want 2", outcome.TermsRequired.Version)
	}

	resp, accepted := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept edition 2 status=%d error=%+v", resp.StatusCode, accepted.Error)
	}
	if got := countStaffTermsAcceptances(t, env, email); got != 2 {
		t.Fatalf("acceptances=%d, want one per edition actually asked for", got)
	}

	// Every acceptance is recorded in the organizer capacity, which is the
	// capacity the gate's lookup names EXPLICITLY. Migration 107's CHECK is
	// deliberately open for a later 'staff' capacity, and a capacity-agnostic
	// predicate would read an acceptance made in some other capacity as
	// clearance for this one.
	var otherCapacities int
	if err := env.db.QueryRow(`
		SELECT count(*) FROM staff_terms_acceptances WHERE email = $1 AND capacity <> 'organizer'
	`, email).Scan(&otherCapacities); err != nil {
		t.Fatalf("read acceptance capacities: %v", err)
	}
	if otherCapacities != 0 {
		t.Fatalf("acceptances outside the organizer capacity = %d, want 0", otherCapacities)
	}
}

// TestASupersededEditionNeverSatisfiesAgain is the floor's lower half, asserted
// where a correction cannot be confused for it: a person who accepted the
// edition that a GATING publication superseded is outstanding for good, and
// accepting the new one is the only way out. Nobody is grandfathered.
func TestASupersededEditionNeverSatisfiesAgain(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "carla@example.com", true, true, true)
	held := readConsentState(t, env, "carla@example.com").PolicyVersionID
	if !held.Valid {
		t.Fatal("expected her acceptance to name an edition")
	}

	// An hour on, so the two capture acts are distinguishable by their own
	// timestamps rather than by the order a query happens to return them in.
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	published := publishPolicyVersion(t, env, 2, 0)
	if published == held.String {
		t.Fatal("expected the gating edition to be a new row")
	}
	assertBoxes(t, signedInConsentBoxes(t, env, token), true, false, false,
		"the edition she holds is below the new floor")

	// And accepting the edition in force is the only way out — the optional
	// answers she gave before are not disturbed by it.
	verify := startSignIn(t, env, "carla@example.com")
	if verify.ConsentRequired == nil {
		t.Fatal("expected the sign-in to name the outstanding acceptance")
	}
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(verify.ConsentRequired.PendingConsentToken, true, true, true),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if now := readConsentState(t, env, "carla@example.com").PolicyVersionID; now.String != published {
		t.Fatalf("she now holds %q, want the edition in force %q", now.String, published)
	}
	assertBoxes(t, signedInConsentBoxes(t, env, decodeCustomerVerify(t, body).SessionID), false, false, false,
		"accepting the edition in force clears the gate")
}
