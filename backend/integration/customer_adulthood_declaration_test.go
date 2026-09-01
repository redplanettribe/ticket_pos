package integration

import (
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
)

// The Adulthood Declaration at the sign-in consent step (#586, parent #584,
// ADR 0069): the flagship capture point, and the first of four.
//
// It mirrors customer_terms_acceptance_test.go, because it is the same gate:
// the declaration has none of its own. What is different, and what most of this
// file is about, is the REFUSAL — an untick is refused before any capture, and
// the platform is left holding nothing whatever. That claim is the strongest
// one in this suite and the easiest to let rot, so it is asserted over EVERY
// TABLE IN THE DATABASE rather than over the handful a reader would think to
// name.

// publishTermsVersionAskingAdulthood publishes an edition that carries the
// `label-adulthood-declaration` Artifact in both published languages —
// publishTermsVersion plus the one Artifact, because that is precisely what
// introducing the box is: a publish, with no deploy on either side of it.
//
// The ceremony that produces one in production is the Legal Center's — a
// structural change forces a Gating Edition, dated at least tomorrow and
// cancellable overnight — and none of that is what these tests are about. What
// they are about is what the binary does once such an edition is current.
func publishTermsVersionAskingAdulthood(t *testing.T, env *testEnv, generation, revision int) string {
	t.Helper()
	id := publishTermsVersion(t, env, generation, revision)
	label := legal.Lineage{Generation: generation, Revision: revision}.Label()
	if _, err := env.db.Exec(`
		INSERT INTO terms_version_artifacts (version_id, locale, slug, ordinal, body)
		VALUES ($1, 'en', 'label-adulthood-declaration', 3, $2),
		       ($1, 'es', 'label-adulthood-declaration', 3, $3)
	`, id,
		fmt.Sprintf("I declare that I am eighteen years of age or older (edition %s)", label),
		fmt.Sprintf("Declaro ser mayor de edad (edición %s)", label),
	); err != nil {
		t.Fatalf("publish the adulthood declaration label on edition %q: %v", label, err)
	}
	return id
}

// readAdulthoodDeclarations reads the declaration off every Consent Record for
// one Customer, oldest first — readTermsAnswers' shape over the one column this
// ticket adds.
func readAdulthoodDeclarations(t *testing.T, env *testEnv, email string) []sql.NullBool {
	t.Helper()
	var declarations []sql.NullBool
	for _, record := range readConsentRecords(t, env, email) {
		declarations = append(declarations, record.AdulthoodDeclaration)
	}
	return declarations
}

// tableRowCounts counts every row of every ordinary table in the database.
//
// IT IS THE "ZERO ROWS WRITTEN ANYWHERE" ASSERTION'S WHOLE POINT that this
// enumerates the schema rather than a list somebody maintained. A refusal must
// write nothing, and "nothing" is not a property of the four tables a reader
// happens to think of: a table added next year is covered by this the day it is
// created, and a future capture path that starts recording a refusal somewhere
// nobody expected fails here rather than shipping.
func tableRowCounts(t *testing.T, env *testEnv) map[string]int {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
	`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("scan table name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iterate tables: %v", err)
	}
	rows.Close()
	if len(names) == 0 {
		t.Fatal("the schema reports no tables, which means this assertion is asserting nothing")
	}

	counts := make(map[string]int, len(names))
	for _, name := range names {
		var n int
		// The name comes from information_schema and is quoted, so this is not a
		// place a value can be smuggled through; there is no parameter form for
		// an identifier.
		if err := env.db.QueryRow(`SELECT COUNT(*) FROM "` + name + `"`).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", name, err)
		}
		counts[name] = n
	}
	return counts
}

// assertNoTableGrew is the refusal's guarantee stated as an assertion: not one
// row, in not one table, anywhere.
//
// It compares for GROWTH rather than for equality because the one thing a
// refused submission is entitled to do is SPEND the pending-consent token,
// which deletes its row — the token is consumed before the answers are judged,
// so a refusal cannot be retried against the same proof (SubmitConsent). A
// shrinking table is that; a growing one is evidence of a refusal, which is the
// thing ADR 0069 forbids.
func assertNoTableGrew(t *testing.T, before, after map[string]int) {
	t.Helper()
	var grew []string
	for name, count := range after {
		if count > before[name] {
			grew = append(grew, fmt.Sprintf("%s %d -> %d", name, before[name], count))
		}
	}
	if len(grew) > 0 {
		sort.Strings(grew)
		t.Fatalf("a refused Adulthood Declaration wrote rows: %v", grew)
	}
}

// TestAnEditionWithoutTheArtifactOwesNoAdulthoodDeclaration is the state every
// environment is in today, and the first thing to keep true: the label is
// OPTIONAL to the binary, so the edition in effect gates, renders and records
// exactly as it did before this shipped. The deploy strictly precedes the
// publish, and this is what the world looks like in between.
func TestAnEditionWithoutTheArtifactOwesNoAdulthoodDeclaration(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "tania@example.com")
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent step: every Customer owes the seeded Terms edition")
	}
	if data.ConsentRequired.Boxes.AdulthoodDeclaration {
		t.Fatal("an edition carrying no adulthood declaration artifact must draw no box")
	}

	resp, body := env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// AND NOTHING IS RECORDED, although the browser sent `adulthood_declaration:
	// true` (consentAnswers does). An answer for a box the platform did not owe
	// is ignored, and the null it leaves says the truthful thing: this act did
	// not ask.
	declarations := readAdulthoodDeclarations(t, env, "tania@example.com")
	if len(declarations) != 1 {
		t.Fatalf("consent records = %d, want exactly one for one capture act", len(declarations))
	}
	if declarations[0].Valid {
		t.Fatalf("adulthood_declaration = %v, want NULL — the act did not ask", declarations[0].Bool)
	}
}

// TestBothBoxesTickedRecordBothAnswersUnderOneEdition is the crossing: one
// session, one Consent Record, both answers on it, and one edition named by
// both.
func TestBothBoxesTickedRecordBothAnswersUnderOneEdition(t *testing.T) {
	env := setupTest(t)

	edition := publishTermsVersionAskingAdulthood(t, env, 2, 0)

	data := startSignIn(t, env, "tania@example.com")
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent step")
	}
	if !data.ConsentRequired.Boxes.TermsAcceptance || !data.ConsentRequired.Boxes.AdulthoodDeclaration {
		t.Fatalf("boxes = %+v, want both the Terms box and the declaration owed", data.ConsentRequired.Boxes)
	}

	resp, body := env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if decodeCustomerVerify(t, body).SessionID == "" {
		t.Fatal("expected a Customer Session once every owed box is ticked")
	}

	records := readConsentRecords(t, env, "tania@example.com")
	if len(records) != 1 {
		t.Fatalf("consent records = %d, want exactly one for one capture act", len(records))
	}
	if !records[0].AdulthoodDeclaration.Valid || !records[0].AdulthoodDeclaration.Bool {
		t.Fatalf("adulthood_declaration = %+v, want true", records[0].AdulthoodDeclaration)
	}

	// The declaration names its edition by riding the acceptance's: it has no
	// version column of its own, because the words it was made under are the
	// Artifact inside that edition's fingerprint.
	answers := readTermsAnswers(t, env, "tania@example.com")
	if len(answers) != 1 || !answers[0].Answer.Valid || !answers[0].Answer.Bool {
		t.Fatalf("terms answers = %+v, want one acceptance beside the declaration", answers)
	}
	if answers[0].VersionID.String != edition {
		t.Fatalf("terms_version_id = %q, want the artifact-carrying edition %q", answers[0].VersionID.String, edition)
	}

	// And the Terms current-state pair is written, exactly as it is without the
	// declaration: there is no second state, on no second column.
	acceptedAt, versionID := readTermsState(t, env, "tania@example.com")
	if !acceptedAt.Valid || versionID.String != edition {
		t.Fatalf("terms state = %+v/%+v, want the edition just accepted", acceptedAt, versionID)
	}
}

// TestARefusedAdulthoodDeclarationWritesNothingAnywhere is the strongest claim
// in this suite, and the reason the feature is shaped the way it is: the
// platform keeps NO RECORD OF ANYBODY WHO SAYS THEY ARE A MINOR.
//
// A row saying otherwise would be a permanent, unverified assertion that a
// named individual is a child, on tables that are never edited and never
// deleted, about the one population the Privacy Policy promises not to
// knowingly process — and it would go stale in the worst direction, since the
// sixteen-year-old it names turns eighteen while the row does not.
//
// So the assertion is over the WHOLE SCHEMA, not over the tables a reader would
// think to name. The refusal is also the API's and not the form's: this is a
// bare HTTP POST with the box unticked, which is exactly the curl the disabled
// submit button cannot stop.
func TestARefusedAdulthoodDeclarationWritesNothingAnywhere(t *testing.T) {
	env := setupTest(t)

	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	data := startSignIn(t, env, "tania@example.com")
	if data.ConsentRequired == nil || !data.ConsentRequired.Boxes.AdulthoodDeclaration {
		t.Fatalf("expected a consent step owing the declaration, got %+v", data.ConsentRequired)
	}

	before := tableRowCounts(t, env)

	answers := consentAnswers(data.ConsentRequired.PendingConsentToken, true, true, true)
	answers["adulthood_declaration"] = false
	resp, body := env.post(t, customerConsentPath, answers, consentEvidenceHeaders())

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — nothing about this caller is unauthorized", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "ADULTHOOD_DECLARATION_REQUIRED" {
		t.Fatalf("error = %+v, want ADULTHOOD_DECLARATION_REQUIRED", body.Error)
	}

	assertNoTableGrew(t, before, tableRowCounts(t, env))

	// And nothing was UPDATED either, which the counts above cannot see. No
	// session, no evidence, and no current state of either document: the person
	// is exactly as they were before they answered, which is signed out.
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 — a refusal mints nothing", n)
	}
	if records := readConsentRecords(t, env, "tania@example.com"); len(records) != 0 {
		t.Fatalf("consent records = %d, want 0 — a refusal is forgotten", len(records))
	}
	if acceptedAt, versionID := readTermsState(t, env, "tania@example.com"); acceptedAt.Valid || versionID.Valid {
		t.Fatal("a refused submission must stamp no Terms acceptance")
	}
	if state := readConsentState(t, env, "tania@example.com"); state.PolicyAcceptedAt.Valid || state.PolicyVersionID.Valid {
		t.Fatal("a refused submission must stamp no Policy acceptance")
	}
}

// TestTheDeclarationIsRefusedBeforeTheOptionalAnswersAreWritten is the ordering
// stated on its own, because it is the half a refactor would break silently.
// The refused submission above answered BOTH optional boxes affirmatively; if
// the check had sat below the capture, or inside it, those two grants would be
// standing on the Customer row now and the person would have subscribed to a
// Follow Digest by declaring they were a minor.
func TestTheDeclarationIsRefusedBeforeTheOptionalAnswersAreWritten(t *testing.T) {
	env := setupTest(t)

	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	data := startSignIn(t, env, "tania@example.com")
	answers := consentAnswers(data.ConsentRequired.PendingConsentToken, true, true, true)
	answers["adulthood_declaration"] = false
	resp, _ := env.post(t, customerConsentPath, answers, consentEvidenceHeaders())
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	state := readConsentState(t, env, "tania@example.com")
	if state.MarketingConsent.Valid || state.NetworkingConsent.Valid {
		t.Fatalf("optional consents = %+v/%+v, want unanswered — nothing at all is written",
			state.MarketingConsent, state.NetworkingConsent)
	}
	if state.DigestEnabled {
		t.Fatal("a refused submission must not have switched the Follow Digest on")
	}
}

// TestTheTermsRefusalIsUnchangedBesideTheDeclaration: the existing refusal keeps
// its own code and its own meaning. Declining the Terms means "I do not agree"
// and declining the declaration means "I am a child" — one control cannot say
// both, which is why there are two boxes, and the two refusals stay told apart.
func TestTheTermsRefusalIsUnchangedBesideTheDeclaration(t *testing.T) {
	env := setupTest(t)

	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	data := startSignIn(t, env, "tania@example.com")
	answers := consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false)
	answers["terms_acceptance"] = false
	resp, body := env.post(t, customerConsentPath, answers, consentEvidenceHeaders())
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "TERMS_ACCEPTANCE_REQUIRED" {
		t.Fatalf("error = %+v, want TERMS_ACCEPTANCE_REQUIRED even where the declaration is owed too", body.Error)
	}
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0", n)
	}
	if records := readConsentRecords(t, env, "tania@example.com"); len(records) != 0 {
		t.Fatalf("consent records = %d, want 0", len(records))
	}
}

// TestAPersonCurrentOnAnArtifactCarryingEditionIsNeverReAsked. The declaration
// has no expiry, no re-ask and no standing of its own: it rides the Terms, so
// somebody who has accepted the edition in effect passes straight through, and
// a second Consent Record is never written for a question nobody was asked.
func TestAPersonCurrentOnAnArtifactCarryingEditionIsNeverReAsked(t *testing.T) {
	env := setupTest(t)

	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	data := startSignIn(t, env, "tania@example.com")
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Signing in again: no consent step at all, so no box and nothing recorded.
	again := startSignIn(t, env, "tania@example.com")
	if again.ConsentRequired != nil {
		t.Fatalf("consent step = %+v, want none — this person is Current on the edition", again.ConsentRequired)
	}
	if again.SessionID == "" {
		t.Fatal("expected a Customer Session straight from the verify")
	}
	if declarations := readAdulthoodDeclarations(t, env, "tania@example.com"); len(declarations) != 1 {
		t.Fatalf("consent records = %d, want exactly one — nobody is asked twice", len(declarations))
	}
}

// TestALaterEditionThatStopsAskingDrawsNoBox is the removal half, and it needs
// no rule of its own: an edition that omits the Artifact simply stops
// collecting, and the general machinery — a structural change forces a Gating
// Edition — is the only thing guarding it. A rule naming this one slug would be
// the publish path learning about one particular checkbox.
func TestALaterEditionThatStopsAskingDrawsNoBox(t *testing.T) {
	env := setupTest(t)

	publishTermsVersionAskingAdulthood(t, env, 2, 0)
	// Generation 3 omits it. Publishing is the whole of the change.
	publishTermsVersion(t, env, 3, 0)

	data := startSignIn(t, env, "tania@example.com")
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent step: the new edition re-gates everybody")
	}
	if data.ConsentRequired.Boxes.AdulthoodDeclaration {
		t.Fatal("an edition that dropped the artifact must draw no declaration box")
	}

	// And the submission is not refused for missing it, even sending false.
	answers := consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false)
	answers["adulthood_declaration"] = false
	resp, body := env.post(t, customerConsentPath, answers, consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	declarations := readAdulthoodDeclarations(t, env, "tania@example.com")
	if len(declarations) != 1 || declarations[0].Valid {
		t.Fatalf("adulthood declarations = %+v, want one NULL — this act did not ask", declarations)
	}
}

// TestTheGoogleDoorMeetsTheSameAdulthoodGate. The two doors converge on ONE
// seam — the pending-consent token and the one submission endpoint — so there
// is no Google path to this gate and no way for one to drift from the other.
// This asserts the convergence rather than re-asserting the gate: the box is
// owed at the Google door, the untick is refused there, and the tick mints the
// session and records the declaration.
func TestTheGoogleDoorMeetsTheSameAdulthoodGate(t *testing.T) {
	env := setupTest(t)

	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	data := startGoogleSignIn(t, env, "gabriela@example.com")
	if data.ConsentRequired == nil || !data.ConsentRequired.Boxes.AdulthoodDeclaration {
		t.Fatalf("expected the Google door to owe the declaration, got %+v", data.ConsentRequired)
	}

	before := tableRowCounts(t, env)
	refused := consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false)
	refused["adulthood_declaration"] = false
	resp, body := env.post(t, customerConsentPath, refused, consentEvidenceHeaders())
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "ADULTHOOD_DECLARATION_REQUIRED" {
		t.Fatalf("status=%d error=%+v, want 400 ADULTHOOD_DECLARATION_REQUIRED", resp.StatusCode, body.Error)
	}
	assertNoTableGrew(t, before, tableRowCounts(t, env))

	// The token was spent by the refusal, exactly as it is on the passcode door:
	// a refused submission is restarted by signing in again, which is what stops
	// a stolen token being retried against a different answer.
	data = startGoogleSignIn(t, env, "gabriela@example.com")
	resp, body = env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if decodeCustomerVerify(t, body).SessionID == "" {
		t.Fatal("expected a Customer Session from the Google door once both boxes are ticked")
	}
	declarations := readAdulthoodDeclarations(t, env, "gabriela@example.com")
	if len(declarations) != 1 || !declarations[0].Valid || !declarations[0].Bool {
		t.Fatalf("adulthood declarations = %+v, want exactly one true", declarations)
	}
}

// TestACustomerTheBoxOfficeCREATEDMeetsTheSameGate. A Customer who has never
// used the Storefront — created by a box-office sale, an import, or any other
// path that mints a Customer row without asking anybody anything — is gated by
// the same test for the same reason, with no special case anywhere.
//
// Nobody is grandfathered: an acceptance nobody recorded cannot be produced
// later, and neither can a declaration.
func TestACustomerTheBoxOfficeCreatedMeetsTheSameGate(t *testing.T) {
	env := setupTest(t)

	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	// A Customer row with no consent history of any kind, which is exactly what
	// a box-office sale leaves behind: the buyer never met a checkbox, because
	// nobody was at a browser.
	if _, err := env.db.Exec(`
		INSERT INTO customers (email, first_name, last_name) VALUES ($1, 'Box', 'Office')
	`, "boxoffice-buyer@example.com"); err != nil {
		t.Fatalf("seed a box-office Customer: %v", err)
	}

	data := startSignIn(t, env, "boxoffice-buyer@example.com")
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent step for a Customer who has never accepted anything")
	}
	if !data.ConsentRequired.Boxes.AdulthoodDeclaration {
		t.Fatal("a Customer created by a box-office sale is owed the declaration like everybody else")
	}

	resp, body := env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	declarations := readAdulthoodDeclarations(t, env, "boxoffice-buyer@example.com")
	if len(declarations) != 1 || !declarations[0].Valid || !declarations[0].Bool {
		t.Fatalf("adulthood declarations = %+v, want exactly one true", declarations)
	}
}

// TestNoAdulthoodAnswerIsWrittenWithoutATermsAnswer pins migration 119's CHECK
// from the outside: the declaration never travels alone. The Withdrawal branch
// is the surface that proves it — it answers no required box at all — and the
// column it leaves null is the truthful one.
func TestNoAdulthoodAnswerIsWrittenWithoutATermsAnswer(t *testing.T) {
	env := setupTest(t)

	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	// The database itself refuses the shape, whatever any service believes.
	var customerID string
	if err := env.db.QueryRow(`
		INSERT INTO customers (email, first_name, last_name) VALUES ('orphan@example.com', 'Or', 'Phan') RETURNING id
	`).Scan(&customerID); err != nil {
		t.Fatalf("seed a Customer: %v", err)
	}
	_, err := env.db.Exec(`
		INSERT INTO consent_records (customer_id, email, channel, captured_at, policy_version_id,
		                             adulthood_declaration, email_proven)
		VALUES ($1, 'orphan@example.com', 'signin', NOW(), $2, true, true)
	`, customerID, currentPolicyVersionID(t, env))
	if err == nil {
		t.Fatal("a declaration with no Terms answer beside it must be refused by the database")
	}
}
