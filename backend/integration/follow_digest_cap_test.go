package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The Digest's cap and its carried overflow (#222, parent #215, ADR 0030).
//
// The cap is the readability rule: ten Events per section, soonest first. The
// carry is what makes the cap safe — the sent-ledger is written ONLY for the
// Events a Digest actually carried, so everything the cap shed stays New to You
// and arrives in a later Digest. A flood becomes a queue that drains ten a week
// rather than a three-hundred-item email or a silent truncation.
//
// THE TEST THAT MATTERS MOST IS THE CARRY, across two consecutive weekly runs.
// A suite that only proved "never more than ten" would pass just as happily on
// an implementation that truncated the list and threw the remainder away, which
// is the exact failure this issue exists to prevent. Every carry test below
// therefore asserts the UNION across the runs — that each matched Event was
// carried exactly once, and that none went missing.
//
// These tests live in their own file rather than in follow_digest_test.go for
// the reason the Unsubscribe tests do: they are one rule with its own setup
// shape (a section's worth of Events, several weeks of runs).

// digestSectionCapForTests is the cap the reader sees. Restated here rather than
// imported, so that a change to the constant has to be a deliberate change to
// this expectation too.
const digestSectionCapForTests = 10

// capEventName is the Nth Event of a staged flood, zero-padded so that no name
// is a substring of another — "Cap Fest 1" inside "Cap Fest 10" would make
// every containment assertion below lie.
func capEventName(n int) string {
	return fmt.Sprintf("Cap Fest %02d", n)
}

func capEventSlug(n int) string {
	return fmt.Sprintf("cap-fest-%02d", n)
}

// stageFlood publishes n discoverable Events, each starting a day after the
// last, so that soonest-first is the numbered order and the cap has an
// unambiguous boundary.
func stageFlood(t *testing.T, env *testEnv, sessionID string, n int, firstStartsIn time.Duration) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, discoverableEvent(t, env, sessionID,
			capEventName(i), capEventSlug(i),
			env.fixedClock.Add(firstStartsIn+time.Duration(i)*24*time.Hour)))
	}
	return ids
}

// capEventsIn reports which of the staged Events a rendered section names.
func capEventsIn(section string, n int) []int {
	var found []int
	for i := 1; i <= n; i++ {
		if strings.Contains(section, capEventName(i)) {
			found = append(found, i)
		}
	}
	return found
}

// assertCarriedExactlyOnce is the anti-loss assertion, and it is the reason
// these tests are worth more than a count.
//
// It fails on either half of the failure: an Event carried twice (the ledger did
// not record what was sent) and an Event carried never (the cap dropped it).
func assertCarriedExactlyOnce(t *testing.T, total int, sections ...string) {
	t.Helper()
	seen := make(map[int]int, total)
	for _, section := range sections {
		for _, n := range capEventsIn(section, total) {
			seen[n]++
		}
	}
	for i := 1; i <= total; i++ {
		switch seen[i] {
		case 1:
		case 0:
			t.Fatalf("%s was matched by a Follow and never carried by any Digest — the cap dropped it", capEventName(i))
		default:
			t.Fatalf("%s was carried by %d Digests, want exactly one", capEventName(i), seen[i])
		}
	}
}

// digestOverflowLine returns the "+N more" line of a section, or "" when the
// section carries none.
func digestOverflowLine(section string) string {
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "+") && strings.Contains(line, " more") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// TestFollowDigestCapsASectionAtTenAndSaysHowManyMore is the readability rule
// and its honesty in one pass.
//
// Thirteen Events match one Follow. Ten are printed, soonest first, and the
// three that did not fit are ANNOUNCED rather than silently missing: a reader
// who cannot tell a short list from a truncated one has been misled about what
// Following that Organization is worth.
func TestFollowDigestCapsASectionAtTenAndSaysHowManyMore(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	stageFlood(t, env, sessionID, 13, 40*24*time.Hour)
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	newSection, _ := digestSections(t, digests[0].Text)

	carried := capEventsIn(newSection, 13)
	if len(carried) != digestSectionCapForTests {
		t.Fatalf("the news section carries %d Events %v, want %d; body:\n%s",
			len(carried), carried, digestSectionCapForTests, digests[0].Text)
	}
	// Soonest first is what the cap sheds by: the ten nearest doors are carried
	// and the far-future three are the ones that can afford to wait.
	for i, n := range carried {
		if n != i+1 {
			t.Fatalf("the news section carries %v, want the ten soonest; body:\n%s", carried, digests[0].Text)
		}
	}
	if line := digestOverflowLine(newSection); !strings.Contains(line, "+3 more") {
		t.Fatalf("the capped section's overflow line is %q, want it to say +3 more; body:\n%s", line, digests[0].Text)
	}
}

// TestFollowDigestCarriesTheOverflowIntoTheNextWeek is #222's crux and the
// acceptance criterion the whole issue turns on.
//
// Thirteen Events, two consecutive weekly runs, and NOTHING IS LOST. The first
// Digest carries ten and writes ten ledger rows; the three it shed were never
// recorded as shown, so they are still New to You a week later and the second
// Digest carries exactly them.
//
// The union assertion is the load-bearing half. "Ten this week and three next
// week" would also pass on an implementation that dropped the first ten and
// re-matched three at random; asserting that all thirteen appear across the two
// runs, each exactly once, is what makes this a carry rather than a coincidence.
func TestFollowDigestCarriesTheOverflowIntoTheNextWeek(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	// Forty days out and beyond, so that nothing carried in the first week has
	// come within the agenda's seven days by the second: every Event in the
	// second Digest is there because it is still news, not because it is being
	// reminded about.
	stageFlood(t, env, sessionID, 13, 40*24*time.Hour)
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != digestSectionCapForTests {
		t.Fatalf("the sent-ledger holds %d rows after one Digest, want %d — the ledger must record only what was actually carried",
			len(ledger), digestSectionCapForTests)
	}

	advanceDigestClock(t, env, 7*24*time.Hour)
	enqueueFollowDigests(t, env)
	second := drainFollowDigests(t, env)
	if second.Sent != 1 {
		t.Fatalf("the second week sent=%d empty=%d, want 1 sent — the carried overflow is a full week's worth of news (%+v)",
			second.Sent, second.Empty, second)
	}

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 2 {
		t.Fatalf("ana received %d Digests over two weeks, want 2", len(digests))
	}
	firstNew, firstHappening := digestSections(t, digests[0].Text)
	secondNew, secondHappening := digestSections(t, digests[1].Text)

	if carried := capEventsIn(secondNew, 13); len(carried) != 3 || carried[0] != 11 || carried[2] != 13 {
		t.Fatalf("the second week's news carries %v, want exactly the three the cap shed; body:\n%s", carried, digests[1].Text)
	}
	// The whole point, stated as one assertion: every matched Event reached the
	// reader, exactly once, across the two runs.
	assertCarriedExactlyOnce(t, 13, firstNew, firstHappening, secondNew, secondHappening)

	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != 13 {
		t.Fatalf("the sent-ledger holds %d rows after two Digests, want 13", len(ledger))
	}
	if line := digestOverflowLine(secondNew); line != "" {
		t.Fatalf("the second week's news is under the cap and still prints %q; body:\n%s", line, digests[1].Text)
	}
}

// TestFollowDigestAtTheCapEveryWeekStillSendsAFullDigest is ADR 0030's accepted
// consequence, held as a promise rather than left as a hope.
//
// A Customer Following something busy can sit permanently at the cap. What the
// ADR accepts is that their overflow drains lazily; what it does NOT accept is
// that they are ever sent less than a full Digest, or that the drain quietly
// gives up on a backlog it cannot clear in one week.
func TestFollowDigestAtTheCapEveryWeekStillSendsAFullDigest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	stageFlood(t, env, sessionID, 22, 40*24*time.Hour)
	followingCustomer(t, env, "ana@example.com")

	sections := make([]string, 0, 3)
	for week, wants := range []int{10, 10, 2} {
		if week > 0 {
			advanceDigestClock(t, env, time.Duration(week)*7*24*time.Hour)
		}
		enqueueFollowDigests(t, env)
		if result := drainFollowDigests(t, env); result.Sent != 1 {
			t.Fatalf("week %d sent=%d empty=%d, want 1 sent (%+v)", week+1, result.Sent, result.Empty, result)
		}
		digests := digestsFor(t, env, "ana@example.com")
		if len(digests) != week+1 {
			t.Fatalf("ana received %d Digests by week %d, want %d", len(digests), week+1, week+1)
		}
		newSection, happening := digestSections(t, digests[week].Text)
		if got := len(capEventsIn(newSection, 22)); got != wants {
			t.Fatalf("week %d carries %d news Events, want %d; body:\n%s", week+1, got, wants, digests[week].Text)
		}
		sections = append(sections, newSection, happening)
	}

	assertCarriedExactlyOnce(t, 22, sections...)
	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != 22 {
		t.Fatalf("the sent-ledger holds %d rows after three Digests, want 22", len(ledger))
	}
}

// TestFollowDigestCapsTheAgendaAndCarriesTheRest holds the cap on the OTHER
// section, which is the half easiest to leave unbuilt.
//
// Twelve Events all open their doors on the same day. Two Digests make all
// twelve known to this reader, and the week they come within seven days they are
// all on the agenda at once — where ten is still the limit of what a person will
// read, and the remaining two are announced rather than dropped.
func TestFollowDigestCapsTheAgendaAndCarriesTheRest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	// Every Event on the same day, an hour apart, so that one week's agenda can
	// hold all twelve at once.
	for i := 1; i <= 12; i++ {
		discoverableEvent(t, env, sessionID, capEventName(i), capEventSlug(i),
			env.fixedClock.Add(40*24*time.Hour+time.Duration(i)*time.Hour))
	}
	followingCustomer(t, env, "ana@example.com")

	// Two weeks to make all twelve known: ten in the first Digest, the carried
	// two in the second.
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)
	advanceDigestClock(t, env, 7*24*time.Hour)
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)
	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != 12 {
		t.Fatalf("the sent-ledger holds %d rows after two Digests, want 12", len(ledger))
	}

	// The week the doors are six days away: nothing is new, everything is on the
	// agenda.
	advanceDigestClock(t, env, 34*24*time.Hour)
	enqueueFollowDigests(t, env)
	if result := drainFollowDigests(t, env); result.Sent != 1 {
		t.Fatalf("the agenda week sent=%d empty=%d, want 1 sent (%+v)", result.Sent, result.Empty, result)
	}

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 3 {
		t.Fatalf("ana received %d Digests, want 3", len(digests))
	}
	newSection, happening := digestSections(t, digests[2].Text)
	if got := capEventsIn(newSection, 12); len(got) != 0 {
		t.Fatalf("the agenda week advertises %v as news; body:\n%s", got, digests[2].Text)
	}
	if got := len(capEventsIn(happening, 12)); got != digestSectionCapForTests {
		t.Fatalf("the agenda carries %d Events, want %d; body:\n%s", got, digestSectionCapForTests, digests[2].Text)
	}
	if line := digestOverflowLine(happening); !strings.Contains(line, "+2 more") {
		t.Fatalf("the capped agenda's overflow line is %q, want it to say +2 more; body:\n%s", line, digests[2].Text)
	}
}

// TestFollowDigestOverflowLinksToTheExplorerFilteredByTheTag is the cap being
// visible rather than being a truncation.
//
// The link points at a surface that ALREADY EXISTS — the global explorer,
// filtered by the Tag's canonical key — because ADR 0030 builds no Following
// feed. A "+N more" with nowhere to go would tell a reader they are missing
// something and leave them to search for it.
func TestFollowDigestOverflowLinksToTheExplorerFilteredByTheTag(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	ids := stageFlood(t, env, sessionID, 13, 40*24*time.Hour)
	for _, id := range ids {
		if resp, body := setEventTags(t, env, sessionID, id, []string{"Music"}); resp.StatusCode != 200 {
			t.Fatalf("set tags status=%d error=%+v", resp.StatusCode, body.Error)
		}
	}
	// A Tag Follow only: the overflow's one reason is the Tag, so the link has
	// exactly one honest destination.
	token := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, token, "music")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	newSection, _ := digestSections(t, digests[0].Text)
	line := digestOverflowLine(newSection)
	if !strings.Contains(line, "+3 more") {
		t.Fatalf("the overflow line is %q, want it to say +3 more; body:\n%s", line, digests[0].Text)
	}
	if want := "http://storefront.example/?tags=music"; !strings.Contains(line, want) {
		t.Fatalf("the overflow line is %q, want it to link to %q; body:\n%s", line, want, digests[0].Text)
	}
}

// TestFollowDigestOverflowLinksToTheOrganizationPage is the other destination,
// and the only one that can be right when the overflow's whole reason is an
// Organization Follow: the explorer has no Organization filter, and that
// Organization's public page already lists everything it has on.
func TestFollowDigestOverflowLinksToTheOrganizationPage(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	stageFlood(t, env, sessionID, 13, 40*24*time.Hour)
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	newSection, _ := digestSections(t, digests[0].Text)
	line := digestOverflowLine(newSection)
	if want := "http://storefront.example/" + testOrgSlug; !strings.Contains(line, want) {
		t.Fatalf("the overflow line is %q, want it to link to %q; body:\n%s", line, want, digests[0].Text)
	}
}
