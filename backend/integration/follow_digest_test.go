package integration

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Follow Digest pipeline (#220, parent #215, ADR 0030): the thinnest
// complete path from a Follow to an email in an inbox.
//
// Everything below is driven over HTTP through the two internal endpoints, the
// way ADR 0024's Reversal Reconciler is driven, and asserts on captured mail.
// That is the whole point of the split between enqueue and drain: a test can
// produce a week deterministically and then drain it, with no scheduler, no
// goroutine and no sleep anywhere in the feature.
//
// The Digest's two sections — "New this week" and "Happening this week" — are
// #221 and are covered from TestFollowDigestCarriesANewSectionAndAHappeningSection
// down.
//
// The Unsubscribe link (#224) has its own file, follow_digest_unsubscribe_test.go,
// and the cap with its carried overflow (#222) has follow_digest_cap_test.go.
//
// The "you're going" marking and the Register call to action are #223 and have
// their own file, follow_digest_attending_test.go.

const (
	followDigestEnqueuePath = "/api/v1/internal/follow-digests/enqueue"
	followDigestDrainPath   = "/api/v1/internal/follow-digests/drain"
)

// followDigestEnqueueResult is what one enqueue run did: how many Customers
// were eligible for a Digest this week, and how that split between rows it
// created and rows that were already there.
type followDigestEnqueueResult struct {
	WeekStart       string `json:"week_start"`
	Eligible        int    `json:"eligible"`
	Enqueued        int    `json:"enqueued"`
	AlreadyEnqueued int    `json:"already_enqueued"`
}

// followDigestDrainResult is what one drain run did, and what is still waiting
// once it had done it.
type followDigestDrainResult struct {
	Claimed int `json:"claimed"`
	Sent    int `json:"sent"`
	Empty   int `json:"empty"`
	// Skipped is Digests whose Customer had unsubscribed by the time the drain
	// reached them (#224). Distinct from Empty: nothing was composed at all.
	Skipped          int    `json:"skipped"`
	Retrying         int    `json:"retrying"`
	GaveUp           int    `json:"gave_up"`
	PendingTotal     int    `json:"pending_total"`
	OldestPendingFor string `json:"oldest_pending_week,omitempty"`
}

// enqueueFollowDigests declares a week and returns what the run did.
//
// It takes no credential, for the reason drainReversals takes none: the
// endpoint is authenticated by Cloud Run IAM before the request reaches the API
// (ADR 0008), which is infrastructure this suite does not run. What is
// exercised here is the behaviour behind that gate.
func enqueueFollowDigests(t *testing.T, env *testEnv) followDigestEnqueueResult {
	t.Helper()
	resp, body := env.post(t, followDigestEnqueuePath, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enqueue status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("enqueue error=%+v, want none", body.Error)
	}
	var out followDigestEnqueueResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode enqueue result: %v", err)
	}
	return out
}

// drainFollowDigests runs one drain tick and returns what it did.
func drainFollowDigests(t *testing.T, env *testEnv) followDigestDrainResult {
	t.Helper()
	resp, body := env.post(t, followDigestDrainPath, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("drain status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("drain error=%+v, want none", body.Error)
	}
	var out followDigestDrainResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode drain result: %v", err)
	}
	return out
}

// followingCustomer signs a Customer in and has them Follow the Organization
// the suite's fixtures publish Events under. The returned token is the
// Customer's session, for tests that want to add Tag Follows to the same person.
func followingCustomer(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	token := customerSignIn(t, env, email)
	followOrganizationOK(t, env, token, testOrgSlug)
	return token
}

// discoverableEvent publishes an Event and lists it, which is the only state a
// Digest may ever mail out: the Digest's eligibility mirrors the public
// explorer's exactly (published, discoverable, not yet ended), because anything
// looser mails out an Event an Organization chose not to list.
func discoverableEvent(t *testing.T, env *testEnv, sessionID, name, slug string, startsAt time.Time) string {
	t.Helper()
	return publishEvent(t, env, sessionID, name, slug, startsAt, true, 5000, 100)
}

// digestsFor returns the captured Follow Digests addressed to one Customer, as
// the rendered messages that person would actually receive.
//
// Tests assert on its LENGTH as much as its contents: almost every rule in this
// feature is a rule about how many emails somebody gets. And they assert on the
// RENDERED text rather than on the struct wherever the rule is about what a
// person reads — a Digest Locale that reaches a field and not the body is a
// Digest Locale that does not work.
func digestsFor(t *testing.T, env *testEnv, email string) []capturedFollowDigest {
	t.Helper()
	var out []capturedFollowDigest
	for _, d := range env.email.FollowDigestsSent() {
		if d.To == email {
			out = append(out, capturedFollowDigest{To: d.To, Subject: d.Subject(), Text: d.Text()})
		}
	}
	return out
}

type capturedFollowDigest struct {
	To      string
	Subject string
	Text    string
}

// otherOrganizationSession is a second Organization, run by somebody else,
// publishing Events nobody in these tests Follows.
//
// It is the control that stops a Digest test passing on a query with no WHERE
// clause: an Event that is published, discoverable and upcoming, and that must
// still never appear, because the one thing it lacks is a Follow.
func otherOrganizationSession(t *testing.T, env *testEnv) string {
	t.Helper()
	sessionID := verifyOTP(t, env, "other-admin@example.com")
	createOrganization(t, env, sessionID, "Other Org", "other-org")
	return sessionID
}

// advanceDigestClock moves the clocks the Digest pipeline reads.
//
// Time moves by moving the clock, never by sleeping — the same rule the
// Reversal Reconciler's tests keep. Both services are moved together because
// both are consulted in one run: the Digest service decides which week it is and
// when a failed Digest comes due again, and catalog decides which Events have
// not yet ended.
func advanceDigestClock(t *testing.T, env *testEnv, d time.Duration) {
	t.Helper()
	at := env.fixedClock.Add(d)
	sharedApp.DigestService.WithClock(func() time.Time { return at })
	sharedApp.CatalogService.WithClock(func() time.Time { return at })
	t.Cleanup(func() {
		sharedApp.DigestService.WithClock(func() time.Time { return fixedClock })
		sharedApp.CatalogService.WithClock(func() time.Time { return fixedClock })
	})
}

// digestLedgerEvents reads the sent-ledger for one Customer.
//
// SQL rather than the API because the ledger has no HTTP surface and will never
// have one: it records what a person was shown, and nothing a Customer or a
// Member can see reflects it. It is nonetheless the backbone of the feature
// (ADR 0030), so its contents are asserted directly.
func digestLedgerEvents(t *testing.T, env *testEnv, email string) []string {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT e.name
		FROM follow_digest_sent_events l
		JOIN customers c ON c.id = l.customer_id
		JOIN events e ON e.id = l.event_id
		WHERE c.email = $1
		ORDER BY e.name
	`, email)
	if err != nil {
		t.Fatalf("read sent-ledger for %s: %v", email, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan sent-ledger row: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sent-ledger: %v", err)
	}
	return names
}

// digestRowStatus reads a Customer's pending-Digest row for the current week.
//
// SQL for the same reason the ledger read is SQL: the queue is internal
// machinery with no customer-facing surface. The status is asserted because it
// is the difference between "we decided not to mail this person" and "the send
// failed and nobody noticed", which no count of emails can tell apart.
func digestRowStatus(t *testing.T, env *testEnv, email string) (status string, attempts int) {
	t.Helper()
	err := env.db.QueryRow(`
		SELECT d.status, d.attempt_count
		FROM follow_digests d
		JOIN customers c ON c.id = d.customer_id
		WHERE c.email = $1
	`, email).Scan(&status, &attempts)
	if err != nil {
		t.Fatalf("read follow_digests row for %s: %v", email, err)
	}
	return status, attempts
}

func digestRowCount(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT count(*) FROM follow_digests`).Scan(&n); err != nil {
		t.Fatalf("count follow_digests: %v", err)
	}
	return n
}

// TestFollowDigestEnqueuesOneDigestPerFollowingCustomer is the enqueue's whole
// contract: one pending Digest per Customer with at least one Follow, of either
// kind, and nothing at all for anybody else.
//
// The Customer with no Follows is the control, and it is the criterion most
// likely to be broken by a query that starts from `customers` rather than from
// the Follow tables. Somebody who has never pressed Follow has not asked to be
// written to, and mailing them is the difference between this feature and spam.
func TestFollowDigestEnqueuesOneDigestPerFollowingCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Enqueue Fest", "enqueue-fest", env.fixedClock.Add(72*time.Hour))

	// Ana Follows an Organization, Bruno Follows a Tag: either kind is enough.
	followingCustomer(t, env, "ana@example.com")
	bruno := customerSignIn(t, env, "bruno@example.com")
	followTagOK(t, env, bruno, "music")
	// Carla signed in and Followed nothing. She is a verified Customer and she
	// must still be left alone.
	customerSignIn(t, env, "carla@example.com")

	result := enqueueFollowDigests(t, env)

	if result.Eligible != 2 || result.Enqueued != 2 {
		t.Fatalf("enqueue eligible=%d enqueued=%d, want 2 and 2 (%+v)", result.Eligible, result.Enqueued, result)
	}
	if got := digestRowCount(t, env); got != 2 {
		t.Fatalf("pending Digest rows=%d, want 2", got)
	}
	if status, _ := digestRowStatus(t, env, "ana@example.com"); status != "pending" {
		t.Fatalf("ana's Digest status=%q, want pending", status)
	}
	if status, _ := digestRowStatus(t, env, "bruno@example.com"); status != "pending" {
		t.Fatalf("bruno's Digest status=%q, want pending", status)
	}
}

// TestFollowDigestNeverEnqueuesAnUnverifiedCustomer holds the ADR 0010 line at
// the send.
//
// An unverified Customer is a record a box office sale or an import created:
// somebody typed that address at a counter, and nobody has ever proven they
// control it. Follows can only be pressed from a full Customer Session, so the
// row is seeded directly — this is precisely the state the API cannot produce,
// which is why the guard has to exist at all rather than being implied.
func TestFollowDigestNeverEnqueuesAnUnverifiedCustomer(t *testing.T) {
	env := setupTest(t)

	// SQL because no API path can create this: a Follow requires Proof of Email
	// Ownership, so an unverified Customer holding one cannot be produced over
	// HTTP. The guard is for the row a future write path leaves behind, and the
	// only way to test it is to leave that row behind.
	if _, err := env.db.Exec(`
		INSERT INTO customers (id, email, first_name, last_name, verified_at)
		VALUES ('c0000000-0000-4000-8000-0000000000f1', 'counter@example.com', 'Counter', 'Buyer', NULL)
	`); err != nil {
		t.Fatalf("seed unverified Customer: %v", err)
	}
	if _, err := env.db.Exec(`
		INSERT INTO customer_organization_follows (customer_id, organization_id)
		VALUES ('c0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-000000000001')
	`); err != nil {
		t.Fatalf("seed Follow for unverified Customer: %v", err)
	}

	result := enqueueFollowDigests(t, env)

	if result.Eligible != 0 || result.Enqueued != 0 {
		t.Fatalf("enqueue eligible=%d enqueued=%d, want 0 and 0", result.Eligible, result.Enqueued)
	}
	if got := digestRowCount(t, env); got != 0 {
		t.Fatalf("pending Digest rows=%d, want 0", got)
	}
}

// TestFollowDigestDrainSendsOneEmailListingFollowedEvents is the tracer itself:
// a Follow at one end, an email in an inbox at the other.
//
// Two Events are published and only one is Followed into range, so a passing
// test cannot be a Digest that lists the catalogue.
func TestFollowDigestDrainSendsOneEmailListingFollowedEvents(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Followed Fest", "followed-fest", env.fixedClock.Add(72*time.Hour))

	// A second Organization whose Event nobody Follows.
	other := otherOrganizationSession(t, env)
	discoverableEvent(t, env, other, "Unfollowed Fest", "unfollowed-fest", env.fixedClock.Add(96*time.Hour))

	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)

	result := drainFollowDigests(t, env)
	if result.Claimed != 1 || result.Sent != 1 {
		t.Fatalf("drain claimed=%d sent=%d, want 1 and 1 (%+v)", result.Claimed, result.Sent, result)
	}

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	if !strings.Contains(digests[0].Text, "Followed Fest") {
		t.Fatalf("Digest body does not name the Followed Event:\n%s", digests[0].Text)
	}
	if strings.Contains(digests[0].Text, "Unfollowed Fest") {
		t.Fatalf("Digest body names an Event nobody Follows:\n%s", digests[0].Text)
	}
	if status, _ := digestRowStatus(t, env, "ana@example.com"); status != "sent" {
		t.Fatalf("ana's Digest status=%q after a successful send, want sent", status)
	}
}

// TestFollowDigestIsComposedAtSendTimeRatherThanAtEnqueue pins the property the
// enqueue/drain split exists for, and the one a future optimisation is most
// likely to undo by snapshotting the Events onto the queue row.
//
// The week is declared before the Event is published. A Digest delayed an hour
// — by a backlog, a retry, a deploy — must still reflect reality when it goes
// out, and a Digest composed at enqueue would mail a week-old view of the
// catalogue.
func TestFollowDigestIsComposedAtSendTimeRatherThanAtEnqueue(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	followingCustomer(t, env, "ana@example.com")

	// The week is declared while there is nothing to say.
	enqueueFollowDigests(t, env)

	// The Event is published afterwards, and before the drain.
	discoverableEvent(t, env, sessionID, "Late Fest", "late-fest", env.fixedClock.Add(72*time.Hour))

	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	if !strings.Contains(digests[0].Text, "Late Fest") {
		t.Fatalf("Digest was composed at enqueue rather than at send time; body:\n%s", digests[0].Text)
	}
}

// TestFollowDigestWritesTheLedgerOnlyForEventsItCarried is the invariant the
// whole feature rests on: a ledger row means "this person was shown this Event",
// and an Event nobody was shown must never acquire one.
//
// The over-writing failure is silent and permanent — a row written for an Event
// that was not included makes that Event invisible to that Customer forever,
// and nothing anywhere would notice. So the control is an eligible, unfollowed
// Event: it exists, it would pass every filter but the Follow, and it must not
// be in the ledger.
func TestFollowDigestWritesTheLedgerOnlyForEventsItCarried(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Carried Fest", "carried-fest", env.fixedClock.Add(72*time.Hour))
	other := otherOrganizationSession(t, env)
	discoverableEvent(t, env, other, "Uncarried Fest", "uncarried-fest", env.fixedClock.Add(96*time.Hour))

	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	ledger := digestLedgerEvents(t, env, "ana@example.com")
	if len(ledger) != 1 || ledger[0] != "Carried Fest" {
		t.Fatalf("sent-ledger holds %v, want exactly [Carried Fest]", ledger)
	}
}

// TestFollowDigestDrainAfterACompletedSendSendsNoSecondEmail is the idempotency
// the drain's per-minute cadence demands: the endpoint is called about ten
// thousand times a week and must mail each Customer once.
func TestFollowDigestDrainAfterACompletedSendSendsNoSecondEmail(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Once Fest", "once-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)

	drainFollowDigests(t, env)
	second := drainFollowDigests(t, env)

	if second.Claimed != 0 || second.Sent != 0 {
		t.Fatalf("second drain claimed=%d sent=%d, want 0 and 0 (%+v)", second.Claimed, second.Sent, second)
	}
	if got := len(digestsFor(t, env, "ana@example.com")); got != 1 {
		t.Fatalf("ana received %d Digests across two drains, want exactly 1", got)
	}
}

// TestFollowDigestEnqueuedTwiceForOneWeekProducesOneEmail is the other half of
// that guarantee, and the one the database enforces rather than the code: the
// uniqueness on (Customer, week).
//
// A weekly job that fires twice is not exotic — a retried Cloud Scheduler
// attempt, an operator curling the endpoint after the cron already ran. The
// second enqueue must create nothing, so there is nothing for a drain to claim.
func TestFollowDigestEnqueuedTwiceForOneWeekProducesOneEmail(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Weekly Fest", "weekly-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")

	first := enqueueFollowDigests(t, env)
	second := enqueueFollowDigests(t, env)

	if first.Enqueued != 1 {
		t.Fatalf("first enqueue enqueued=%d, want 1", first.Enqueued)
	}
	if second.Enqueued != 0 || second.AlreadyEnqueued != 1 {
		t.Fatalf("second enqueue enqueued=%d already=%d, want 0 and 1 (%+v)", second.Enqueued, second.AlreadyEnqueued, second)
	}
	if got := digestRowCount(t, env); got != 1 {
		t.Fatalf("pending Digest rows=%d after two enqueues of one week, want 1", got)
	}

	drainFollowDigests(t, env)
	if got := len(digestsFor(t, env, "ana@example.com")); got != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", got)
	}
}

// TestFollowDigestSendFailureLeavesTheDigestRetryable is the reason the queue is
// a table rather than a loop: a provider that is down for a minute must cost a
// Customer a minute, not a week.
//
// The retry must also send EXACTLY ONE email, which is the half that is easy to
// get wrong — a first attempt that wrote its ledger rows before the send would
// leave the retry with nothing to say, and the Customer would get an empty
// Digest or none at all.
func TestFollowDigestSendFailureLeavesTheDigestRetryable(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Retry Fest", "retry-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)

	env.email.FailWith(errors.New("the mail provider is down"))
	t.Cleanup(func() { env.email.FailWith(nil) })
	failed := drainFollowDigests(t, env)
	if failed.Claimed != 1 || failed.Retrying != 1 {
		t.Fatalf("failing drain claimed=%d retrying=%d, want 1 and 1 (%+v)", failed.Claimed, failed.Retrying, failed)
	}
	if got := len(digestsFor(t, env, "ana@example.com")); got != 0 {
		t.Fatalf("ana received %d Digests from a failed send, want 0", got)
	}
	if status, attempts := digestRowStatus(t, env, "ana@example.com"); status != "pending" || attempts != 1 {
		t.Fatalf("after a failed send status=%q attempts=%d, want pending and 1", status, attempts)
	}
	// Nothing was carried, so nothing may be in the ledger.
	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != 0 {
		t.Fatalf("sent-ledger holds %v after a failed send, want nothing", ledger)
	}

	// The backoff is real: a drain before it expires must find nothing due.
	if immediate := drainFollowDigests(t, env); immediate.Claimed != 0 {
		t.Fatalf("drain during the backoff claimed=%d, want 0", immediate.Claimed)
	}

	env.email.FailWith(nil)
	advanceDigestClock(t, env, 30*time.Minute)
	retried := drainFollowDigests(t, env)
	if retried.Sent != 1 {
		t.Fatalf("retry drain sent=%d, want 1 (%+v)", retried.Sent, retried)
	}

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests across the failure and the retry, want exactly 1", len(digests))
	}
	if !strings.Contains(digests[0].Text, "Retry Fest") {
		t.Fatalf("retried Digest lost its Event; body:\n%s", digests[0].Text)
	}
}

// TestFollowDigestMatchingNothingSendsNoEmail: a Customer whose Follows matched
// nothing receives NO email rather than an empty one.
//
// An empty Digest is worse than silence. The Digest is the whole payload of a
// Follow (CONTEXT.md), so one that says nothing teaches the reader that this
// mail is worth ignoring — and it is the mail that has to survive being ignored
// for the feature to work at all.
func TestFollowDigestMatchingNothingSendsNoEmail(t *testing.T) {
	env := setupTest(t)
	orgAdminSession(t, env)
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	result := drainFollowDigests(t, env)

	if result.Claimed != 1 || result.Empty != 1 || result.Sent != 0 {
		t.Fatalf("drain claimed=%d empty=%d sent=%d, want 1, 1 and 0 (%+v)", result.Claimed, result.Empty, result.Sent, result)
	}
	if got := len(digestsFor(t, env, "ana@example.com")); got != 0 {
		t.Fatalf("ana received %d Digests with nothing to say, want 0", got)
	}
	if status, _ := digestRowStatus(t, env, "ana@example.com"); status != "empty" {
		t.Fatalf("Digest status=%q with nothing to say, want empty", status)
	}
	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != 0 {
		t.Fatalf("sent-ledger holds %v after no send, want nothing", ledger)
	}
}

// TestFollowDigestNeverRepeatsAnEventItAlreadySent is the ledger doing its
// week-to-week job. An Event that stays upcoming for two months must not be
// mailed out eight times.
//
// The second week's Digest has a NEW Event as well, so the assertion is not
// merely "no email" — it is that the Digest went out and carried only what was
// new.
func TestFollowDigestNeverRepeatsAnEventItAlreadySent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "First Fest", "first-fest", env.fixedClock.Add(30*24*time.Hour))
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	// The next week, with a second Event announced in between.
	advanceDigestClock(t, env, 7*24*time.Hour)
	discoverableEvent(t, env, sessionID, "Second Fest", "second-fest", env.fixedClock.Add(40*24*time.Hour))
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 2 {
		t.Fatalf("ana received %d Digests over two weeks, want 2", len(digests))
	}
	if !strings.Contains(digests[1].Text, "Second Fest") {
		t.Fatalf("the second week's Digest does not carry the new Event; body:\n%s", digests[1].Text)
	}
	if strings.Contains(digests[1].Text, "First Fest") {
		t.Fatalf("the second week's Digest repeats an Event already sent; body:\n%s", digests[1].Text)
	}
}

// TestFollowDigestMailsOnlyWhatTheExplorerWouldList is the eligibility rule,
// and the one with a real Organization on the other side of it.
//
// It mirrors the public explorer exactly — published, discoverable, not yet
// ended. `discoverable = FALSE` is an Organization deciding this Event is not to
// be advertised, and a Digest that mailed it out anyway would take a decision
// they made on their own listing page and overturn it in ten thousand inboxes.
func TestFollowDigestMailsOnlyWhatTheExplorerWouldList(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	discoverableEvent(t, env, sessionID, "Listed Fest", "listed-fest", env.fixedClock.Add(72*time.Hour))
	// Published but NOT discoverable: reachable by direct link, never advertised.
	publishEvent(t, env, sessionID, "Unlisted Fest", "unlisted-fest", env.fixedClock.Add(80*time.Hour), false, 5000, 100)
	// A draft: not published at all.
	createDraftEvent(t, env, sessionID, "Draft Fest", "draft-fest")
	// Already over.
	discoverableEvent(t, env, sessionID, "Past Fest", "past-fest", env.fixedClock.Add(-72*time.Hour))

	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	body := digests[0].Text
	if !strings.Contains(body, "Listed Fest") {
		t.Fatalf("Digest omits the one Event the explorer would list; body:\n%s", body)
	}
	for _, forbidden := range []string{"Unlisted Fest", "Draft Fest", "Past Fest"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Digest carries %q, which the public explorer would not list; body:\n%s", forbidden, body)
		}
	}
	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != 1 || ledger[0] != "Listed Fest" {
		t.Fatalf("sent-ledger holds %v, want exactly [Listed Fest]", ledger)
	}
}

// TestFollowDigestTellsOneCustomerAboutAnEventOnce is the cross-Follow dedupe:
// one Customer Follows both the Organization and a Tag the Event carries, and
// is matched twice.
//
// Two matches, one Event in the email, one ledger row. Without the dedupe the
// Digest would list the same Event as many times as the reader has reasons to
// care about it, which is precisely backwards.
func TestFollowDigestTellsOneCustomerAboutAnEventOnce(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := discoverableEvent(t, env, sessionID, "Double Fest", "double-fest", env.fixedClock.Add(72*time.Hour))
	resp, body := setEventTags(t, env, sessionID, eventID, []string{"Music"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set tags status=%d error=%+v", resp.StatusCode, body.Error)
	}

	token := followingCustomer(t, env, "ana@example.com")
	followTagOK(t, env, token, "music")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	if n := strings.Count(digests[0].Text, "Double Fest"); n != 1 {
		t.Fatalf("Digest names the Event %d times, want once; body:\n%s", n, digests[0].Text)
	}
	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != 1 {
		t.Fatalf("sent-ledger holds %v, want exactly one row", ledger)
	}
}

// TestFollowDigestRendersInTheCustomerDigestLocale is the first email in this
// system that branches on language (ADR 0030), and the Digest Locale is where
// that language comes from: mail has no address to carry one, so it is
// remembered from the Storefront the Customer last signed in on (#216).
//
// The Preset Tag is the point. Its Spanish name lives in the `tags` table
// because the backend cannot reach the Storefront's message catalogue, which is
// the narrow amendment ADR 0030 made to ADR 0027 — so a Digest naming "Music" to
// a Spanish reader means that amendment is not wired up.
func TestFollowDigestRendersInTheCustomerDigestLocale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := discoverableEvent(t, env, sessionID, "Locale Fest", "locale-fest", env.fixedClock.Add(72*time.Hour))
	if resp, body := setEventTags(t, env, sessionID, eventID, []string{"Music"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("set tags status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Ana signed in from a Spanish Storefront; Bruno from an English one.
	customerSignInWithLocale(t, env, "ana@example.com", "es")
	ana := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, ana, "music")
	bruno := customerSignIn(t, env, "bruno@example.com")
	followTagOK(t, env, bruno, "music")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	spanish := digestsFor(t, env, "ana@example.com")
	if len(spanish) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(spanish))
	}
	if !strings.Contains(spanish[0].Text, "Música") {
		t.Fatalf("the Spanish Digest does not name the Preset Tag in Spanish; body:\n%s", spanish[0].Text)
	}
	english := digestsFor(t, env, "bruno@example.com")
	if len(english) != 1 {
		t.Fatalf("bruno received %d Digests, want exactly 1", len(english))
	}
	if !strings.Contains(english[0].Text, "Music") {
		t.Fatalf("the English Digest does not name the Preset Tag in English; body:\n%s", english[0].Text)
	}
	if strings.Contains(english[0].Text, "Música") {
		t.Fatalf("the English Digest is written in Spanish; body:\n%s", english[0].Text)
	}
}

// TestFollowDigestNamesACustomTagAsCoined is the other half of the localization
// rule and the one that is a decision rather than an omission: a Custom Tag is
// rendered exactly as the Organization coined it, in every language.
//
// ADR 0027 holds that correct — the platform did not invent the word and has no
// standing to translate it — and ADR 0030's amendment covers Preset Tags only.
func TestFollowDigestNamesACustomTagAsCoined(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := discoverableEvent(t, env, sessionID, "Techno Fest", "techno-fest", env.fixedClock.Add(72*time.Hour))
	if resp, body := setEventTags(t, env, sessionID, eventID, []string{"Techno"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("set tags status=%d error=%+v", resp.StatusCode, body.Error)
	}

	customerSignInWithLocale(t, env, "ana@example.com", "es")
	ana := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, ana, "techno")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	if !strings.Contains(digests[0].Text, "Techno") {
		t.Fatalf("the Digest does not name the Custom Tag as coined; body:\n%s", digests[0].Text)
	}
}

// TestFollowDigestDrainStopsAtItsBatchBound proves the bound is a bound: a
// backlog can never wedge the endpoint, and whatever a run does not reach stays
// exactly as due as it was found.
//
// Boundedness is a property of the loop rather than of the number, so the batch
// is narrowed to one here — reaching the deployed fifty would mean staging fifty
// Customers, and a test that slow gets deleted along with the guarantee it
// protects.
//
// The second run picking up the Customer the first left is the half that
// matters. A drain that claimed its whole batch up front and then stopped would
// leave the remainder leased and invisible to everybody, so stopping early would
// have made those readers LATER by stopping; here it costs them one tick.
func TestFollowDigestDrainStopsAtItsBatchBound(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Batch Fest", "batch-fest", env.fixedClock.Add(72*time.Hour))

	followingCustomer(t, env, "ana@example.com")
	followingCustomer(t, env, "bruno@example.com")
	enqueueFollowDigests(t, env)

	sharedApp.DigestService.WithDrainBatch(1)
	t.Cleanup(func() { sharedApp.DigestService.WithDrainBatch(0) })

	first := drainFollowDigests(t, env)
	if first.Claimed != 1 || first.Sent != 1 {
		t.Fatalf("first drain claimed=%d sent=%d, want 1 and 1 (%+v)", first.Claimed, first.Sent, first)
	}
	if first.PendingTotal != 1 {
		t.Fatalf("first drain left pending_total=%d, want 1", first.PendingTotal)
	}

	second := drainFollowDigests(t, env)
	if second.Claimed != 1 || second.Sent != 1 {
		t.Fatalf("second drain claimed=%d sent=%d, want 1 and 1 (%+v)", second.Claimed, second.Sent, second)
	}
	if second.PendingTotal != 0 {
		t.Fatalf("second drain left pending_total=%d, want 0", second.PendingTotal)
	}

	for _, email := range []string{"ana@example.com", "bruno@example.com"} {
		if got := len(digestsFor(t, env, email)); got != 1 {
			t.Fatalf("%s received %d Digests, want exactly 1", email, got)
		}
	}
}

// The Digest's two sections (#221).
//
// Every rule below is about WHICH SECTION an Event lands in and in what order,
// so the tests read the rendered body and split it on the headings a reader
// actually sees. Asserting on the composed struct instead would let a section
// exist in the pipeline and never reach an inbox.

const (
	digestNewHeading       = "New this week"
	digestHappeningHeading = "Happening this week"
)

// digestSections splits a rendered Digest into what falls under each heading.
//
// It also enforces the one ordering rule that is BETWEEN the sections rather
// than within them: New comes first. The Digest's job is to tell a reader
// something they did not know, and an agenda printed above the news buries it.
//
// A missing heading yields an empty string rather than a failure, because the
// absence of a section is itself a rule several tests assert: a Digest with
// nothing new must not print an empty "New this week".
func digestSections(t *testing.T, text string) (newSection, happeningSection string) {
	t.Helper()
	newAt := strings.Index(text, digestNewHeading)
	happeningAt := strings.Index(text, digestHappeningHeading)
	switch {
	case newAt >= 0 && happeningAt >= 0:
		if happeningAt < newAt {
			t.Fatalf("the Digest prints its agenda above its news; body:\n%s", text)
		}
		return text[newAt+len(digestNewHeading) : happeningAt], text[happeningAt+len(digestHappeningHeading):]
	case newAt >= 0:
		return text[newAt+len(digestNewHeading):], ""
	case happeningAt >= 0:
		return "", text[happeningAt+len(digestHappeningHeading):]
	}
	return "", ""
}

// assertOrder fails unless the named Events appear in the given order within one
// section.
func assertOrder(t *testing.T, section, label string, names ...string) {
	t.Helper()
	previous := -1
	for _, name := range names {
		at := strings.Index(section, name)
		if at < 0 {
			t.Fatalf("the %q section omits %q; section:\n%s", label, name, section)
		}
		if at < previous {
			t.Fatalf("the %q section is not soonest-first: %q comes too late; section:\n%s", label, name, section)
		}
		previous = at
	}
}

// TestFollowDigestCarriesANewSectionAndAHappeningSection is #221's spine: the
// flat list becomes two headings answering two different questions.
//
// "New this week" is what this reader has never been shown. "Happening this
// week" is what they were already told about and which now starts within seven
// days — the original reminder intent, which ADR 0030 keeps alive inside the
// Digest rather than as a second kind of mail.
//
// One Event carries the whole test by moving between them: announced twenty days
// out it can only be news, and a fortnight later, six days from its doors, it
// can only be the agenda.
func TestFollowDigestCarriesANewSectionAndAHappeningSection(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Agenda Fest", "agenda-fest", env.fixedClock.Add(20*24*time.Hour))
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	first := digestsFor(t, env, "ana@example.com")
	if len(first) != 1 {
		t.Fatalf("ana received %d Digests in the first week, want exactly 1", len(first))
	}
	newSection, happening := digestSections(t, first[0].Text)
	if !strings.Contains(newSection, "Agenda Fest") {
		t.Fatalf("an Event never shown before is not under %q; body:\n%s", digestNewHeading, first[0].Text)
	}
	if strings.Contains(happening, "Agenda Fest") {
		t.Fatalf("an Event twenty days out is on this week's agenda; body:\n%s", first[0].Text)
	}

	// A fortnight on: the same Event now starts in six days, and something else
	// has been announced.
	advanceDigestClock(t, env, 14*24*time.Hour)
	discoverableEvent(t, env, sessionID, "Fresh Fest", "fresh-fest", env.fixedClock.Add(50*24*time.Hour))
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	second := digestsFor(t, env, "ana@example.com")
	if len(second) != 2 {
		t.Fatalf("ana received %d Digests over two weeks, want 2", len(second))
	}
	newSection, happening = digestSections(t, second[1].Text)
	if !strings.Contains(newSection, "Fresh Fest") {
		t.Fatalf("the newly announced Event is not under %q; body:\n%s", digestNewHeading, second[1].Text)
	}
	if strings.Contains(newSection, "Agenda Fest") {
		t.Fatalf("an Event already shown is advertised as new again; body:\n%s", second[1].Text)
	}
	if !strings.Contains(happening, "Agenda Fest") {
		t.Fatalf("an already-shown Event starting in six days is not under %q; body:\n%s", digestHappeningHeading, second[1].Text)
	}
	if strings.Contains(happening, "Fresh Fest") {
		t.Fatalf("a never-shown Event appears on the agenda; body:\n%s", second[1].Text)
	}
}

// TestFollowDigestPutsAnEventQualifyingForBothSectionsInNewOnly is the overlap
// rule, and the one place the two headings could contradict each other.
//
// An Event announced today and opening its doors in three days is both news to
// this reader and something happening this week. It is news, once: a reader who
// meets the same Event twice in one email learns that this mail repeats itself.
func TestFollowDigestPutsAnEventQualifyingForBothSectionsInNewOnly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Imminent Fest", "imminent-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	if n := strings.Count(digests[0].Text, "Imminent Fest"); n != 1 {
		t.Fatalf("the Digest names the Event %d times, want once; body:\n%s", n, digests[0].Text)
	}
	newSection, happening := digestSections(t, digests[0].Text)
	if !strings.Contains(newSection, "Imminent Fest") {
		t.Fatalf("an Event qualifying for both sections is not under %q; body:\n%s", digestNewHeading, digests[0].Text)
	}
	if strings.Contains(happening, "Imminent Fest") {
		t.Fatalf("an Event qualifying for both sections also appears on the agenda; body:\n%s", digests[0].Text)
	}
}

// TestFollowDigestOrdersEachSectionSoonestFirst holds the order the reader acts
// on. Both sections are lists of things with doors, and the one opening first is
// the one a decision has to be made about first.
//
// The Events are announced in the WRONG order within each section, so a passing
// test cannot be insertion order wearing a sort's clothes.
func TestFollowDigestOrdersEachSectionSoonestFirst(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	// Shown in the first week, so they become the second week's agenda.
	discoverableEvent(t, env, sessionID, "Agenda Later", "agenda-later", env.fixedClock.Add(31*24*time.Hour))
	discoverableEvent(t, env, sessionID, "Agenda Sooner", "agenda-sooner", env.fixedClock.Add(30*24*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	// Four weeks on: both are within seven days, and two more are announced.
	advanceDigestClock(t, env, 28*24*time.Hour)
	discoverableEvent(t, env, sessionID, "Fresh Later", "fresh-later", env.fixedClock.Add(60*24*time.Hour))
	discoverableEvent(t, env, sessionID, "Fresh Sooner", "fresh-sooner", env.fixedClock.Add(50*24*time.Hour))
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 2 {
		t.Fatalf("ana received %d Digests over two weeks, want 2", len(digests))
	}
	newSection, happening := digestSections(t, digests[1].Text)
	assertOrder(t, newSection, digestNewHeading, "Fresh Sooner", "Fresh Later")
	assertOrder(t, happening, digestHappeningHeading, "Agenda Sooner", "Agenda Later")
}

// TestFollowDigestDoesNotRemindAboutAnEventBeyondSevenDays is the bound on the
// agenda, and the reason "Happening this week" is not "everything you were ever
// shown".
//
// An Event two months out was already announced to this reader. Repeating it
// every week until its doors open is the behaviour the ledger exists to stop,
// and a Digest with nothing else to say must be silence rather than a re-run.
func TestFollowDigestDoesNotRemindAboutAnEventBeyondSevenDays(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Distant Fest", "distant-fest", env.fixedClock.Add(60*24*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	advanceDigestClock(t, env, 7*24*time.Hour)
	enqueueFollowDigests(t, env)
	second := drainFollowDigests(t, env)

	if second.Claimed != 1 || second.Empty != 1 || second.Sent != 0 {
		t.Fatalf("second week claimed=%d empty=%d sent=%d, want 1, 1 and 0 (%+v)", second.Claimed, second.Empty, second.Sent, second)
	}
	if got := len(digestsFor(t, env, "ana@example.com")); got != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1 — an Event fifty-three days out was put on this week's agenda", got)
	}
}

// TestFollowDigestSendsAnAgendaEvenWithNothingNew is the other half of that
// rule, and the one an over-eager "nothing new means nothing to say" would
// break.
//
// A reader told about something a month ago who has heard nothing since is
// exactly the reader the reminder is for. The week their Event finally comes
// within seven days, the Digest goes out carrying only an agenda.
func TestFollowDigestSendsAnAgendaEvenWithNothingNew(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Agenda Only Fest", "agenda-only-fest", env.fixedClock.Add(20*24*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	advanceDigestClock(t, env, 14*24*time.Hour)
	enqueueFollowDigests(t, env)
	second := drainFollowDigests(t, env)
	if second.Sent != 1 {
		t.Fatalf("the agenda-only week sent=%d empty=%d, want 1 sent (%+v)", second.Sent, second.Empty, second)
	}

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 2 {
		t.Fatalf("ana received %d Digests, want 2", len(digests))
	}
	newSection, happening := digestSections(t, digests[1].Text)
	if strings.TrimSpace(newSection) != "" {
		t.Fatalf("a Digest with nothing new still prints a %q heading; body:\n%s", digestNewHeading, digests[1].Text)
	}
	if !strings.Contains(happening, "Agenda Only Fest") {
		t.Fatalf("the agenda-only Digest does not carry its Event; body:\n%s", digests[1].Text)
	}
}

// TestFollowDigestEntryCarriesEnoughToDecideWithoutClicking is the entry itself.
//
// A listing that gives only a name makes the reader open a page to find out
// whether they care, and most of them will not. What it is, when, where and who
// is putting it on is what the decision is actually made from; the link is for
// the decision already made.
func TestFollowDigestEntryCarriesEnoughToDecideWithoutClicking(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Detail Fest", "detail-fest", env.fixedClock.Add(20*24*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	newSection, _ := digestSections(t, digests[0].Text)
	for label, want := range map[string]string{
		"the Event's name":      "Detail Fest",
		"the date and time":     "July",
		"the venue":             "The Hall",
		"the Organization":      "Test Org",
		"a link to the Event":   "/test-org/events/detail-fest",
		"the Follow it matched": "Because you follow",
	} {
		if !strings.Contains(newSection, want) {
			t.Fatalf("the entry does not carry %s (%q); section:\n%s", label, want, newSection)
		}
	}
}

// TestFollowDigestAgendaEntryKeepsItsAttributionAndIsListedOnce carries both
// per-entry rules into the section easiest to build as an afterthought.
//
// The reader Follows the Organization AND a Tag the Event carries, so it is
// matched twice and must be listed once — the cross-Follow dedupe holding within
// the agenda, not only within the news. And it keeps its "because you follow"
// line, which is the Digest answering "why am I being told this?" before it is
// asked.
func TestFollowDigestAgendaEntryKeepsItsAttributionAndIsListedOnce(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := discoverableEvent(t, env, sessionID, "Agenda Fest", "agenda-fest", env.fixedClock.Add(20*24*time.Hour))
	if resp, body := setEventTags(t, env, sessionID, eventID, []string{"Music"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("set tags status=%d error=%+v", resp.StatusCode, body.Error)
	}
	token := followingCustomer(t, env, "ana@example.com")
	followTagOK(t, env, token, "music")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	advanceDigestClock(t, env, 14*24*time.Hour)
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 2 {
		t.Fatalf("ana received %d Digests, want 2", len(digests))
	}
	if n := strings.Count(digests[1].Text, "Agenda Fest"); n != 1 {
		t.Fatalf("the agenda names the Event %d times, want once; body:\n%s", n, digests[1].Text)
	}
	_, happening := digestSections(t, digests[1].Text)
	if !strings.Contains(happening, "Because you follow") {
		t.Fatalf("the agenda entry lost its attribution; section:\n%s", happening)
	}
	for _, reason := range []string{"Test Org", "Music"} {
		if !strings.Contains(happening, reason) {
			t.Fatalf("the agenda entry does not name %q among the Follows that matched it; section:\n%s", reason, happening)
		}
	}
	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != 1 {
		t.Fatalf("sent-ledger holds %v after the Event was reminded about, want exactly one row", ledger)
	}
}

// TestFollowDigestDropsAnUnfollowedEventFromTheAgenda resolves the one thing the
// spec leaves open: whether the agenda is "everything in the ledger" or
// "everything in the ledger they still Follow".
//
// It is the latter, on two grounds. Unfollowing changes what the Digest is about
// (CONTEXT.md), and a reader who has said they no longer want to hear from an
// Organization must not keep hearing from it. And every entry has to say which
// Follow brought it — an entry with no surviving Follow has no true answer.
//
// The ledger row STAYS, which is the part that is more than a filter: refollowing
// must not make an Event news again, and ADR 0030 rests the whole of New to You
// on that.
func TestFollowDigestDropsAnUnfollowedEventFromTheAgenda(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Dropped Fest", "dropped-fest", env.fixedClock.Add(20*24*time.Hour))
	token := followingCustomer(t, env, "ana@example.com")
	// A second Follow, so the Customer stays eligible for a Digest at all and the
	// test cannot pass merely by nobody being enqueued.
	followTagOK(t, env, token, "music")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)
	if got := len(digestsFor(t, env, "ana@example.com")); got != 1 {
		t.Fatalf("ana received %d Digests in the first week, want 1", got)
	}

	unfollowOrganizationOK(t, env, token, testOrgSlug)

	advanceDigestClock(t, env, 14*24*time.Hour)
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	for i, d := range digests[1:] {
		if strings.Contains(d.Text, "Dropped Fest") {
			t.Fatalf("Digest %d reminds ana about an Organization she unfollowed; body:\n%s", i+1, d.Text)
		}
	}
	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != 1 || ledger[0] != "Dropped Fest" {
		t.Fatalf("sent-ledger holds %v after an Unfollow, want it left intact as [Dropped Fest]", ledger)
	}
}

// TestFollowDigestDropsAnIneligibleEventFromTheAgenda carries the eligibility
// rule into the section that could most easily be exempted from it.
//
// "We already told them about it" is not a licence to keep telling them. An
// Event cancelled after it was announced is the case that matters: reminding ten
// thousand people, in the week of its doors, about something that is not
// happening is the failure this feature would be remembered for — and the
// unlisted case is the same Organization decision the New section already
// respects, made a week later.
//
// The Digest goes out with nothing at all rather than with an agenda, which is
// also the assertion that the exclusion happened in the composition and not in
// the rendering.
func TestFollowDigestDropsAnIneligibleEventFromTheAgenda(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	cancelled := discoverableEvent(t, env, sessionID, "Doomed Fest", "doomed-fest", env.fixedClock.Add(20*24*time.Hour))
	unlisted := discoverableEvent(t, env, sessionID, "Withdrawn Fest", "withdrawn-fest", env.fixedClock.Add(19*24*time.Hour))

	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)
	if got := len(digestsFor(t, env, "ana@example.com")); got != 1 {
		t.Fatalf("ana received %d Digests in the first week, want 1", got)
	}

	// Both were announced. One is called off, the other is taken off the listing.
	if resp, body := env.post(t, "/api/v1/staff/events/"+cancelled+"/cancel", nil, authHeader(sessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if resp, body := env.put(t, "/api/v1/staff/events/"+unlisted+"/discoverable", map[string]any{
		"discoverable": false,
	}, authHeader(sessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("set discoverable status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// A fortnight on, both would otherwise be within seven days of their doors.
	advanceDigestClock(t, env, 14*24*time.Hour)
	enqueueFollowDigests(t, env)
	second := drainFollowDigests(t, env)

	if second.Claimed != 1 || second.Empty != 1 || second.Sent != 0 {
		t.Fatalf("second week claimed=%d empty=%d sent=%d, want 1, 1 and 0 (%+v)", second.Claimed, second.Empty, second.Sent, second)
	}
	if got := len(digestsFor(t, env, "ana@example.com")); got != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1 — an Event the explorer would no longer list was put on her agenda", got)
	}
}

// TestFollowDigestReachesAnEventTaggedAfterTheFollowBegan is the Tag Follow
// being a standing subscription rather than a snapshot.
//
// The Follow is pressed while the Event carries no Tags at all, and the Tag is
// added afterwards. A composition that resolved matches at enqueue, or that
// remembered which Events a Tag held when it was Followed, would miss it — and
// missing it is what an Organization tagging their listing later would
// experience as the feature not working.
func TestFollowDigestReachesAnEventTaggedAfterTheFollowBegan(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := discoverableEvent(t, env, sessionID, "Tagged Later Fest", "tagged-later-fest", env.fixedClock.Add(20*24*time.Hour))

	token := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, token, "music")

	if resp, body := setEventTags(t, env, sessionID, eventID, []string{"Music"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("set tags status=%d error=%+v", resp.StatusCode, body.Error)
	}

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	newSection, _ := digestSections(t, digests[0].Text)
	if !strings.Contains(newSection, "Tagged Later Fest") {
		t.Fatalf("an Event Tagged after the Follow began never reached its follower; body:\n%s", digests[0].Text)
	}
}

// TestFollowDigestNeverMailsACancelledOrDatelessEvent completes the eligibility
// exclusions, on the two states TestFollowDigestMailsOnlyWhatTheExplorerWouldList
// does not reach.
//
// A cancelled Event is the worse of the two: mailing ten thousand people about
// something that is not happening is the failure this feature would be
// remembered for. A dateless Event is seeded in SQL because the API refuses to
// publish one — which is the point, since the predicate exists for the row some
// future path leaves behind rather than for one anybody can make today.
func TestFollowDigestNeverMailsACancelledOrDatelessEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Standing Fest", "standing-fest", env.fixedClock.Add(20*24*time.Hour))

	cancelled := discoverableEvent(t, env, sessionID, "Cancelled Fest", "cancelled-fest", env.fixedClock.Add(21*24*time.Hour))
	if resp, body := env.post(t, "/api/v1/staff/events/"+cancelled+"/cancel", nil, authHeader(sessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// SQL because no API path publishes an Event with no date: the guard is for
	// the row a future write path leaves behind.
	if _, err := env.db.Exec(`
		INSERT INTO events (id, organization_id, name, slug, status, discoverable, starts_at)
		SELECT 'e0000000-0000-4000-8000-0000000000d1', id, 'Dateless Fest', 'dateless-fest', 'published', TRUE, NULL
		FROM organizations WHERE slug = $1
	`, testOrgSlug); err != nil {
		t.Fatalf("seed dateless Event: %v", err)
	}

	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	if !strings.Contains(digests[0].Text, "Standing Fest") {
		t.Fatalf("the Digest omits the one Event that is still on; body:\n%s", digests[0].Text)
	}
	for _, forbidden := range []string{"Cancelled Fest", "Dateless Fest"} {
		if strings.Contains(digests[0].Text, forbidden) {
			t.Fatalf("the Digest carries %q; body:\n%s", forbidden, digests[0].Text)
		}
	}
	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != 1 || ledger[0] != "Standing Fest" {
		t.Fatalf("sent-ledger holds %v, want exactly [Standing Fest]", ledger)
	}
}
