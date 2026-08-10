package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// "You're going", and Register for externally registered Events (#223, parent
// #215, ADR 0030 and ADR 0028).
//
// Two rules, and both are about the Digest telling the truth about the reader's
// own relationship to an Event.
//
// The first is TICKETS ALREADY HELD. A Digest that tells somebody who bought
// tickets three weeks ago to "get tickets" for the show they are going to on
// Saturday has stopped being useful and started being noise. An Event the
// Customer holds a LIVE Ticket Sale for is marked as attending, carries no
// purchase call to action, and is suppressed from "New this week" — something
// you already bought is not news. It appears in the agenda half only, which is
// exactly the week-before reminder the whole feature was originally asked for,
// living inside the Digest it became.
//
// The second is EXTERNAL REGISTRATION. Per ADR 0028 an Event either sells
// Tickets or registers externally, never both. Those Events invite the reader to
// Register rather than to buy, and can never be marked as attending —
// registration happens off-platform and this system would never learn that it
// happened.

const (
	// digestAttendingMark is the line that says the reader already holds a
	// Ticket Sale for this Event.
	digestAttendingMark = "You're going"
	// digestGetTicketsCTA is the ordinary purchase call to action, and the one
	// thing an attending entry must never carry.
	digestGetTicketsCTA = "Get tickets:"
	// digestYourTicketsCTA replaces it: the reader is sent to their own Ticket
	// Sale rather than to a checkout they have already been through.
	digestYourTicketsCTA = "Your tickets:"
	// digestRegisterCTA is what an externally registered Event offers instead of
	// a purchase.
	digestRegisterCTA = "Register:"
	// digestTicketSaleURL is where a reader's Ticket Sales live on the
	// Storefront this suite wires up.
	digestTicketSaleURL = "http://storefront.example/tickets"
)

// discoverableTicketedEvent publishes and lists an ordinary ticketed Event and
// hands back the Ticket Type to buy from, which publishEvent creates but does
// not return.
func discoverableTicketedEvent(
	t *testing.T, env *testEnv, sessionID, name, slug string, startsAt time.Time,
) (eventID, ticketTypeID string) {
	t.Helper()
	eventID = discoverableEvent(t, env, sessionID, name, slug, startsAt)

	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/ticket-types", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list ticket types status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var types []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &types); err != nil {
		t.Fatalf("decode ticket types: %v", err)
	}
	if len(types) != 1 {
		t.Fatalf("published Event has %d Ticket Types, want exactly 1", len(types))
	}
	return eventID, types[0].ID
}

// digestEntry returns the block of one Event's entry within a section, so an
// assertion about that Event's call to action cannot accidentally pass on a
// different Event's.
//
// Entries are separated by a blank line, which is how the message itself
// separates them (platform.digestSection).
func digestEntry(t *testing.T, section, eventName string) string {
	t.Helper()
	for _, block := range strings.Split(section, "\n\n") {
		if strings.Contains(block, eventName) {
			return block
		}
	}
	t.Fatalf("no entry for %q in the section:\n%s", eventName, section)
	return ""
}

// The attending mark: an Event the Customer holds a live Ticket Sale for is
// listed as one they are going to, not one to buy.
//
// The whole rule in one Digest. The Event starts in three days, so it is inside
// the agenda's window; the reader bought a ticket before ever being mailed about
// it; and the entry they read has to say so, drop the purchase call to action
// entirely, and point them at their own Ticket Sale instead.
func TestFollowDigestMarksAnEventTheCustomerHoldsTicketsForAsAttending(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, ticketTypeID := discoverableTicketedEvent(
		t, env, sessionID, "Going Fest", "going-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	buyOnline(t, env, "going-fest", ticketTypeID, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	_, happening := digestSections(t, digests[0].Text)
	entry := digestEntry(t, happening, "Going Fest")

	if !strings.Contains(entry, digestAttendingMark) {
		t.Fatalf("an Event ana holds a live Ticket Sale for is not marked %q; entry:\n%s", digestAttendingMark, entry)
	}
	if strings.Contains(entry, digestGetTicketsCTA) {
		t.Fatalf("ana is told to buy tickets for an Event she already bought tickets for; entry:\n%s", entry)
	}
	if !strings.Contains(entry, digestYourTicketsCTA) || !strings.Contains(entry, digestTicketSaleURL) {
		t.Fatalf("the entry does not link ana to her own Ticket Sale; entry:\n%s", entry)
	}
}

// The suppression: what you already bought is not news.
//
// Two Events, both never shown to this reader and both of them therefore news by
// the ledger's reckoning. One she holds a ticket for; it belongs on the agenda
// and nowhere else. The other is untouched and stays where it was.
func TestFollowDigestSuppressesAnEventTheCustomerHoldsTicketsForFromNew(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, ticketTypeID := discoverableTicketedEvent(
		t, env, sessionID, "Bought Fest", "bought-fest", env.fixedClock.Add(72*time.Hour))
	discoverableEvent(t, env, sessionID, "Unbought Fest", "unbought-fest", env.fixedClock.Add(96*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	buyOnline(t, env, "bought-fest", ticketTypeID, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	newSection, happening := digestSections(t, digests[0].Text)

	if strings.Contains(newSection, "Bought Fest") {
		t.Fatalf("an Event ana already bought is advertised as new; body:\n%s", digests[0].Text)
	}
	if !strings.Contains(happening, "Bought Fest") {
		t.Fatalf("an Event ana is going to is missing from the agenda; body:\n%s", digests[0].Text)
	}
	// The control: the Event she did not buy is untouched by any of this.
	if !strings.Contains(newSection, "Unbought Fest") {
		t.Fatalf("an Event ana holds no ticket for is missing from the news; body:\n%s", digests[0].Text)
	}
	if strings.Contains(happening, "Unbought Fest") {
		t.Fatalf("an Event ana holds no ticket for is on the agenda; body:\n%s", digests[0].Text)
	}
}

// The far-out purchase, which is the half of the suppression that is easiest to
// get wrong: an Event bought months ahead is neither news nor this week's
// agenda, so it is not in the Digest at all — until the week it finally comes
// within seven days, when it arrives as the reminder the feature exists for.
func TestFollowDigestHoldsABoughtEventUntilItsWeekComesRound(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, ticketTypeID := discoverableTicketedEvent(
		t, env, sessionID, "Distant Fest", "distant-fest", env.fixedClock.Add(20*24*time.Hour))
	discoverableEvent(t, env, sessionID, "Filler Fest", "filler-fest", env.fixedClock.Add(30*24*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	buyOnline(t, env, "distant-fest", ticketTypeID, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	first := digestsFor(t, env, "ana@example.com")
	if len(first) != 1 {
		t.Fatalf("ana received %d Digests in the first week, want exactly 1", len(first))
	}
	if strings.Contains(first[0].Text, "Distant Fest") {
		t.Fatalf("an Event twenty days out that ana already holds tickets for is in this week's Digest; body:\n%s", first[0].Text)
	}

	// A fortnight on, its doors are six days away.
	advanceDigestClock(t, env, 14*24*time.Hour)
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	second := digestsFor(t, env, "ana@example.com")
	if len(second) != 2 {
		t.Fatalf("ana received %d Digests over two weeks, want 2", len(second))
	}
	newSection, happening := digestSections(t, second[1].Text)
	if strings.Contains(newSection, "Distant Fest") {
		t.Fatalf("an Event ana already bought is advertised as new; body:\n%s", second[1].Text)
	}
	if !strings.Contains(happening, "Distant Fest") {
		t.Fatalf("the week-before reminder for an Event ana is going to never arrived; body:\n%s", second[1].Text)
	}
	if !strings.Contains(digestEntry(t, happening, "Distant Fest"), digestAttendingMark) {
		t.Fatalf("the reminder does not say ana is going; body:\n%s", second[1].Text)
	}
}

// A reversed Ticket Sale is not a ticket. Somebody who undid their purchase must
// not be told they are going to anything.
//
// The failure this guards against is the quiet one: `ticket_sales` keeps a
// reversed row exactly where the live one was, so a check that asks whether a
// row exists rather than whether it is `active` passes every test above and
// tells a Customer who got their money back that they have a seat.
func TestFollowDigestDoesNotMarkAReversedTicketSaleAsAttending(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, ticketTypeID := discoverableTicketedEvent(
		t, env, sessionID, "Refund Fest", "refund-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	reference := buyOnline(t, env, "refund-fest", ticketTypeID, "ana@example.com")
	reverseSale(t, env, reference)

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	newSection, _ := digestSections(t, digests[0].Text)
	if strings.Contains(digests[0].Text, digestAttendingMark) {
		t.Fatalf("ana reversed her Ticket Sale and is still told she is going; body:\n%s", digests[0].Text)
	}
	// And with the sale gone the Event is news again, with an ordinary purchase
	// call to action: the suppression follows the live sale, not the row.
	if !strings.Contains(newSection, "Refund Fest") {
		t.Fatalf("an Event whose Ticket Sale ana reversed is suppressed from the news; body:\n%s", digests[0].Text)
	}
	if !strings.Contains(digestEntry(t, newSection, "Refund Fest"), digestGetTicketsCTA) {
		t.Fatalf("an Event ana no longer holds a ticket for carries no purchase call to action; body:\n%s", digests[0].Text)
	}
}

// An externally registered Event invites the reader to Register, never to buy.
//
// There is nothing to buy: per ADR 0028 such an Event has no Ticket Types at
// all, so a "get tickets" line would point at a page with no checkout on it.
func TestFollowDigestInvitesRegistrationOnAnExternallyRegisteredEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	publishDiscoverableExternalEvent(t, env, sessionID, "https://lu.ma/my-meetup")
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	newSection, _ := digestSections(t, digests[0].Text)
	entry := digestEntry(t, newSection, "External Event")

	if !strings.Contains(entry, digestRegisterCTA) {
		t.Fatalf("an externally registered Event does not invite ana to register; entry:\n%s", entry)
	}
	if strings.Contains(entry, digestGetTicketsCTA) {
		t.Fatalf("an externally registered Event offers tickets it does not sell; entry:\n%s", entry)
	}
}

// All three calls to action are written in the reader's Mail Locale.
//
// The Digest is the platform's only mail that branches on language (ADR 0030),
// and a sentence added later is exactly where that branch gets forgotten: an
// English "Get tickets" under a Spanish heading is a Digest that half works, and
// nothing but a test that reads the words would notice.
func TestFollowDigestWritesEveryCallToActionInTheReaderLocale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	publishDiscoverableExternalEvent(t, env, sessionID, "https://lu.ma/my-meetup")
	_, ticketTypeID := discoverableTicketedEvent(
		t, env, sessionID, "Held Fest", "held-fest", env.fixedClock.Add(48*time.Hour))
	discoverableEvent(t, env, sessionID, "Open Fest", "open-fest", env.fixedClock.Add(96*time.Hour))

	// Ana signed in from a Spanish Storefront, so her whole Digest is Spanish.
	customerSignInWithLocale(t, env, "ana@example.com", "es")
	followingCustomer(t, env, "ana@example.com")
	buyOnline(t, env, "held-fest", ticketTypeID, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	for _, want := range []string{"Vas a ir", "Tus entradas:", "Regístrate:", "Consigue entradas:"} {
		if !strings.Contains(digests[0].Text, want) {
			t.Fatalf("the Spanish Digest does not say %q; body:\n%s", want, digests[0].Text)
		}
	}
	for _, unwanted := range []string{digestAttendingMark, digestYourTicketsCTA, digestRegisterCTA, digestGetTicketsCTA} {
		if strings.Contains(digests[0].Text, unwanted) {
			t.Fatalf("the Spanish Digest says %q in English; body:\n%s", unwanted, digests[0].Text)
		}
	}
}

// An externally registered Event is NEVER marked as attending.
//
// Registration happens on somebody else's site and this platform never learns
// whether it happened — the Registration Link shows its clicks and nothing more
// (CONTEXT.md). A ticketed Event is the control in the same Digest: the mark is
// working, and it still does not reach the external one.
func TestFollowDigestNeverMarksAnExternallyRegisteredEventAsAttending(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	publishDiscoverableExternalEvent(t, env, sessionID, "https://lu.ma/my-meetup")
	_, ticketTypeID := discoverableTicketedEvent(
		t, env, sessionID, "Control Fest", "control-fest", env.fixedClock.Add(48*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	buyOnline(t, env, "control-fest", ticketTypeID, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	newSection, happening := digestSections(t, digests[0].Text)

	// The control: the mark is reaching the Event it should.
	if !strings.Contains(digestEntry(t, happening, "Control Fest"), digestAttendingMark) {
		t.Fatalf("the attending mark is not working at all; body:\n%s", digests[0].Text)
	}
	external := digestEntry(t, newSection, "External Event")
	if strings.Contains(external, digestAttendingMark) {
		t.Fatalf("an externally registered Event says ana is going, which nothing here could know; entry:\n%s", external)
	}
	if strings.Contains(external, digestYourTicketsCTA) {
		t.Fatalf("an externally registered Event links to a Ticket Sale that cannot exist; entry:\n%s", external)
	}
}
