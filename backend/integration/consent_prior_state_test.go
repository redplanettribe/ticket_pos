package integration

import (
	"net/http"
	"testing"
)

// What each capture act CHANGED (#266, parent #265): the state every optional
// consent was in immediately before the act that answered it.
//
// The property under test is one sentence — A CONSENT RECORD IS LEGIBLE ON ITS
// OWN — and it is worth saying why it needs tests of its own rather than a line
// added to each surface's file. Prior state is the only fact on this table that
// is not a copy of something the act itself carried: the answers, the channel,
// the circumstances and the clock are all inputs, while this is an OBSERVATION
// the platform must make at the right instant, under the same lock as the write
// it precedes. A test that only checked one surface would pass against an
// implementation that read the state a moment too late and recorded the act's
// own outcome as the thing it replaced — which is the failure that would make
// every withdrawal in the log read as `denied -> denied` and prove nothing.
//
// So the coverage here is deliberately two-dimensional: every existing capture
// path writes prior state — there are five, and #265 adds surfaces on top of all
// of them — and every transition records the right value. Nothing a Customer can
// observe changes in this ticket; the assertions are on the evidence, read
// through the harness's database handle exactly as customer_consent_test.go
// established, because the log has no endpoint and deliberately never will.

// wantPrior asserts one record's prior-state pair, naming the transition rather
// than the columns so a failure reads as the thing that broke.
//
// The empty string means SQL NULL, which on this pair carries the same meaning
// it carries on the answers beside it: there was no prior state to have. That is
// true both where the box was not shown and where it was shown for the very
// first time, and the two are told apart by the answer column rather than by
// inventing a fourth value here.
func wantPrior(t *testing.T, record consentRecordRow, marketing, networking string) {
	t.Helper()
	if got := record.PriorMarketingConsent.String; got != marketing {
		t.Fatalf("prior_marketing_consent = %s on the %s record, want %s — the row cannot say what this act changed",
			nullOr(got), nullOr(record.Channel), nullOr(marketing))
	}
	if got := record.PriorNetworkingConsent.String; got != networking {
		t.Fatalf("prior_networking_consent = %s on the %s record, want %s — the row cannot say what this act changed",
			nullOr(got), nullOr(record.Channel), nullOr(networking))
	}
}

func nullOr(value string) string {
	if value == "" {
		return "NULL"
	}
	return value
}

// onlyRecordOn is the single act one surface performed, failing loudly when a
// test's setup produced more than one — an assertion about "the withdrawal" that
// silently read the first of two acts would be worthless.
func onlyRecordOn(t *testing.T, env *testEnv, email, channel string) consentRecordRow {
	t.Helper()
	records := consentRecordsOn(t, env, email, channel)
	if len(records) != 1 {
		t.Fatalf("%s wrote %d records on channel %q, want exactly 1", email, len(records), channel)
	}
	return records[0]
}

// TestFirstSignInRecordsNoPriorStateBecauseThereWasNone is the base case, and it
// is the one that stops the columns from being written with the act's own
// outcome. A first sign-in answers both optional boxes — one granted, one denied
// — and both were unanswered a moment earlier, so both prior columns are NULL
// whichever way the answer went.
func TestFirstSignInRecordsNoPriorStateBecauseThereWasNone(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "ana@example.com")
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, true, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	record := onlyRecordOn(t, env, "ana@example.com", "signin")
	wantPrior(t, record, "", "")
	// The disambiguation the schema rests on: both answers were given, so the
	// nulls above mean "never answered before" and not "never asked".
	if !record.MarketingConsent.Valid || !record.NetworkingConsent.Valid {
		t.Fatalf("a sign-in that showed both optional boxes recorded a null answer: %+v", record)
	}
	// And the states really did move, so this is a transition and not a no-op
	// that would have recorded nulls for an uninteresting reason.
	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "granted" || state.NetworkingConsent.String != "denied" {
		t.Fatalf("consent state = %+v, want marketing granted and networking denied", state)
	}
}

// TestGrantedToDeniedIsLegibleFromTheWithdrawalRowAlone is the criterion the
// whole ticket exists for, expressed as the thing a compliance officer must be
// able to do: hold ONE row and say that this act took something away.
//
// The Customer grants Marketing at sign-in and denies it from their Customer
// Area afterwards. The second row says `granted -> denied` without the first row
// being read at all.
func TestGrantedToDeniedIsLegibleFromTheWithdrawalRowAlone(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, true)

	setDigestEnabled(t, env, token, false)

	record := onlyRecordOn(t, env, "ana@example.com", "account_settings")
	// Marketing moved out of granted. Networking was not shown on this surface —
	// the digest toggle answers one box — so it has no prior state, and reading
	// that null as "they had nothing" would invent a fact about a control nobody
	// rendered.
	wantPrior(t, record, "granted", "")
	if record.MarketingConsent.Bool {
		t.Fatalf("the account_settings record says the box was ticked: %+v", record)
	}
	if record.NetworkingConsent.Valid {
		t.Fatalf("the digest toggle recorded an answer for a box it never showed: %+v", record)
	}
	// The standing Networking Consent is untouched, which is what makes the null
	// above the honest answer rather than a lost write.
	if state := readConsentState(t, env, "ana@example.com"); state.NetworkingConsent.String != "granted" {
		t.Fatalf("networking_consent = %q after a marketing-only act, want granted", state.NetworkingConsent.String)
	}
}

// TestDeniedToDeniedRecordsThatNothingWasTakenAway is the negative half of the
// same property, and the reason prior state is stored rather than inferred from
// the answer: AN ACT WHOSE ANSWER IS NO IS NOT NECESSARILY A WITHDRAWAL.
//
// Every later slice of #265 reads this pair to decide whether anything actually
// moved, so a `denied -> denied` that recorded itself as `granted -> denied`
// would mail somebody about a right they did not exercise.
func TestDeniedToDeniedRecordsThatNothingWasTakenAway(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, false, false)

	setDigestEnabled(t, env, token, false)

	record := onlyRecordOn(t, env, "ana@example.com", "account_settings")
	wantPrior(t, record, "denied", "")
	if record.MarketingConsent.Bool {
		t.Fatalf("the record says the box was ticked: %+v", record)
	}
}

// TestPendingConfirmationIsRecordedAsThePriorStateItWas is the transition most
// easily lost, because Pending Confirmation is a state the ANSWER columns on
// this table cannot express: those are booleans, and pending is a fact about the
// state rather than about anybody's tick (migration 061).
//
// A guest ticked marketing for an address they had not proven, so it pended. The
// owner then settles it themselves from their own switch, declining. That row
// must say `pending_confirmation -> denied`, which is the only place in the log
// the fact is recoverable from a single row — and it is what tells the refusal
// of somebody else's tick from the withdrawal of the person's own consent, which
// #265 must be able to report on separately and must not confirm by email alike.
func TestPendingConfirmationIsRecordedAsThePriorStateItWas(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// A guest checkout that pends Marketing. The token in the receipt is not
	// pressed: what is under test is the state it left behind.
	guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), nil)
	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "pending_confirmation" {
		t.Fatalf("marketing_consent = %q, want the pending state this test needs", state.MarketingConsent.String)
	}

	// The owner proves the address and uses their own switch. The guest's
	// checkout already accepted the current Policy Version — it is a fact about
	// that sale (ADR 0035) — so this sign-in is not gated and asks nothing; the
	// pending is resolved by the Customer Area toggle instead, which is exactly
	// the route the spec describes for an owner settling somebody else's tick.
	token := customerSignIn(t, env, "ana@example.com")
	setDigestEnabled(t, env, token, false)

	record := onlyRecordOn(t, env, "ana@example.com", "account_settings")
	// Networking was not shown on the toggle, so it has no prior state beside
	// the marketing pending.
	wantPrior(t, record, "pending_confirmation", "")
	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent = %q after the owner declined, want denied", state.MarketingConsent.String)
	}
}

// TestUnsubscribeLinkRecordsWhatItTookAway covers the fifth existing capture
// path, and the one that matters most to #265: the unsubscribe link is the only
// surface anybody holding a forwarded email can reach, and it is where a
// Marketing withdrawal most often actually happens.
//
// It is also the surface where the prior state cannot come from anything the
// request carried. The link authenticates nobody and names one Customer; what
// their Marketing Consent stood at is knowable only by reading it at the moment
// of the write.
func TestUnsubscribeLinkRecordsWhatItTookAway(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// A Customer who Followed, granted Marketing at sign-in, and received one
	// Digest — then pressed the link in it.
	unsubscribedFollowingCustomer(t, env, sessionID, "ana@example.com")

	record := onlyRecordOn(t, env, "ana@example.com", "unsubscribe_link")
	wantPrior(t, record, "granted", "")
	if record.MarketingConsent.Bool {
		t.Fatalf("an unsubscribe recorded a ticked box: %+v", record)
	}
	// One switch (ADR 0034): the denial and the Digest going quiet are the same
	// act, and the prior state above is what says the act had anything to do.
	if digestEnabledInDatabase(t, env, "ana@example.com") {
		t.Fatal("the Digest is still on after an unsubscribe")
	}
}

// TestGuestCheckoutRecordsThePriorStateItSaw proves the checkout writes prior
// state too, and writes it from INSIDE the sale's own transaction rather than
// from a read taken before the Payment Provider was ever called.
//
// The distinction is observable and is the point of the test: the guest answers
// at `begin`, an unbounded wait at the provider follows, and the Consent Record
// is written only at commit. A prior state captured at begin would be a reading
// of a world that has since moved on.
//
// The act chosen is one that CHANGES NOTHING — an unproven tick over a proven
// denial, which ADR 0035 refuses to honour — because "this act changed nothing"
// is precisely the sentence the pair is being asked to support.
func TestGuestCheckoutRecordsThePriorStateItSaw(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// A standing, proven denial for the later guest to fail to overwrite.
	signInAnswering(t, env, "ana@example.com", true, false, false)

	_, gaID := publishCheckoutEvent(t, env, sessionID, "Prior Fest", "prior-fest", 1000, 10)
	begin := beginCheckoutWithEvidenceOK(t, env, testOrgSlug, "prior-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez",
			boolPtr(true), nil, boolPtr(true), cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	record := onlyRecordOn(t, env, "ana@example.com", "checkout")
	// Marketing was not shown on this checkout; Networking was, and stood denied.
	wantPrior(t, record, "", "denied")
	// And the state is untouched, which is the fact the prior column now lets a
	// reader confirm from the row itself rather than by finding its neighbours.
	if state := readConsentState(t, env, "ana@example.com"); state.NetworkingConsent.String != "denied" {
		t.Fatalf("networking_consent = %q after an unproven tick, want denied", state.NetworkingConsent.String)
	}
}

// TestConfirmationLinkRecordsThePendingItResolved closes the set of existing
// capture paths, and records the transition the double opt-in exists to perform:
// `pending_confirmation -> granted`.
//
// It is also the one act that already had a way to be found — the `confirmed_at`
// stamp on the tick it resolves — and the two do not say the same thing. The
// stamp annotates the OLD row; this pair makes the NEW row legible on its own,
// which is the property every channel now shares.
func TestConfirmationLinkRecordsThePendingItResolved(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	token := guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), nil)
	confirmConsentOK(t, env, token)

	record := onlyRecordOn(t, env, "ana@example.com", "email_confirmation")
	// Only the box in scope was answered, so only it has a prior state.
	wantPrior(t, record, "pending_confirmation", "")
	if !record.MarketingConsent.Bool {
		t.Fatalf("the confirmation record says the box was not ticked: %+v", record)
	}
}

// TestPriorStateIsTheStateBeforeTheActAndNotAfterIt is the regression test for
// the single most likely way to get this wrong: reading the Customer row AFTER
// the update the same transaction performs, which would record every act's own
// outcome and make the columns a tautology that always agreed with the answer
// beside them.
//
// Stated as two consecutive acts moving the same box in opposite directions. If
// prior state were read after the write, both rows would merely restate their
// own answer; read before, the pair is a history — `granted -> denied`, then
// `denied -> granted`.
func TestPriorStateIsTheStateBeforeTheActAndNotAfterIt(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, true)

	setDigestEnabled(t, env, token, false)
	setDigestEnabled(t, env, token, true)

	records := consentRecordsOn(t, env, "ana@example.com", "account_settings")
	if len(records) != 2 {
		t.Fatalf("account_settings records = %d, want the two toggles", len(records))
	}
	// The harness runs on a fixed clock, so the two share a captured_at and
	// cannot be ordered by it. They are told apart by their answers, which is a
	// fact about each act rather than about the order they are read back in.
	var grant, deny consentRecordRow
	for _, record := range records {
		if !record.MarketingConsent.Valid {
			t.Fatalf("a digest toggle recorded no marketing answer: %+v", record)
		}
		if record.MarketingConsent.Bool {
			grant = record
		} else {
			deny = record
		}
	}
	if !grant.MarketingConsent.Valid || !deny.MarketingConsent.Valid {
		t.Fatalf("expected one grant and one denial, got %+v", records)
	}
	wantPrior(t, deny, "granted", "")
	wantPrior(t, grant, "denied", "")
}
