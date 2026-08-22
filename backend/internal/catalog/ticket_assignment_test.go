package catalog

import (
	"testing"
	"time"
)

// The three pure rules a Ticket Assignment rests on (#324, parent #322), tested
// here rather than through HTTP for the reason answer_link_test.go and
// ticket_answer_test.go are: each is a total function over a small closed set of
// inputs, and enumerating that set is cheaper and sharper here than staging one
// Ticket Sale per case at the integration seam. Everything a BUYER can observe
// is asserted there (integration/ticket_assignment_test.go); this file asserts
// the rules those observations are made of.

func timePtr(t time.Time) *time.Time { return &t }

// The state is DERIVED from the columns and never stored, so what this test
// pins is the derivation — including the two combinations the database's CHECKs
// make impossible, which must still not report something absurd if a future
// migration relaxes one.
func TestAssignmentStateIsDerivedFromTheTimestamps(t *testing.T) {
	assigned := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	accepted := assigned.Add(2 * time.Hour)

	for _, tc := range []struct {
		name       string
		email      string
		assignedAt *time.Time
		acceptedAt *time.Time
		want       TicketAssignmentState
	}{
		{"nothing given", "", nil, nil, TicketUnassigned},
		{"an address and a moment", "carla@example.com", timePtr(assigned), nil, TicketAssigned},
		// ACCEPTANCE IS THE STRONGEST FACT and is read first: a Ticket that has
		// been accepted has necessarily been assigned, and reporting the weaker
		// state would lose the Holder.
		{"accepted", "carla@example.com", timePtr(assigned), timePtr(accepted), TicketAccepted},
		// Half a pair is refused by the schema (tickets_assignment_pair_ck) and
		// is still not `assigned` here. A state that could be reached by writing
		// one of two columns is a state a partial write could invent.
		{"a moment with no address", "", timePtr(assigned), nil, TicketUnassigned},
		{"an address with no moment", "carla@example.com", nil, nil, TicketUnassigned},
	} {
		if got := AssignmentState(tc.email, tc.assignedAt, tc.acceptedAt); got != tc.want {
			t.Errorf("%s: state = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// THE ORDER OF THE REFUSALS IS THE RULE, not an implementation detail: each one
// is reported ahead of the ones below it because it is the more fundamental
// fact, and telling a door-sale buyer "the event has started" would send them
// looking for a deadline they never had.
func TestAssignmentWindowRefusesInOrderOfFundamentality(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)
	past := now.Add(-24 * time.Hour)

	for _, tc := range []struct {
		name     string
		channel  string
		status   string
		startsAt *time.Time
		now      time.Time
		want     AssignmentRefusal
	}{
		{"online, active, before the doors", SalesChannelOnline, TicketSaleStatusActive, &future, now, AssignmentWindowOpen},
		{"import, active, before the doors", SalesChannelImport, TicketSaleStatusActive, &future, now, AssignmentWindowOpen},
		// An Event that has not said when it starts HAS NOT STARTED. Reading nil
		// as "started" would freeze assignment on every unscheduled Event.
		{"no start time", SalesChannelOnline, TicketSaleStatusActive, nil, now, AssignmentWindowOpen},
		// HALF-OPEN AT THE DOORS: arriving exactly as they open is late, exactly
		// as the Answer window and the Reversal Window work.
		{"exactly at the doors", SalesChannelOnline, TicketSaleStatusActive, &now, now, AssignmentRefusedEventStarted},
		{"after the doors", SalesChannelOnline, TicketSaleStatusActive, &past, now, AssignmentRefusedEventStarted},
		{"reversed", SalesChannelOnline, "reversed", &future, now, AssignmentRefusedSaleReversed},
		// The channel outranks both: a door sale never had a buyer surface,
		// whether or not its Event has happened and whether or not it was
		// reversed.
		{"in_person", SalesChannelInPerson, TicketSaleStatusActive, &future, now, AssignmentRefusedChannel},
		{"in_person, reversed, long over", SalesChannelInPerson, "reversed", &past, now, AssignmentRefusedChannel},
		// A reversal outranks the clock, as it does for Answers.
		{"reversed and over", SalesChannelImport, "reversed", &past, now, AssignmentRefusedSaleReversed},
	} {
		if got := AssignmentWindow(tc.channel, tc.status, tc.startsAt, tc.now); got != tc.want {
			t.Errorf("%s: refusal = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// NORMALISATION IS THE HALF THAT MATTERS MOST. #325 mints or matches a Customer
// from a click at this address, so an address stored as the buyer capitalised it
// would fail to match the record the same person already has — quietly turning
// one person into two.
func TestParseHolderEmailNormalisesAndRefuses(t *testing.T) {
	for raw, want := range map[string]string{
		"carla@example.com":    "carla@example.com",
		"  Carla@Example.COM ": "carla@example.com",
		"CARLA+tickets@ex.io":  "carla+tickets@ex.io",
	} {
		got, ok := ParseHolderEmail(raw)
		if !ok || got != want {
			t.Errorf("ParseHolderEmail(%q) = %q, %v; want %q, true", raw, got, ok, want)
		}
	}

	for _, bad := range []string{
		"",
		"   ",
		"carla",
		"carla@",
		"@example.com",
		"carla example.com",
		// The display-name form. What is stored must be an ADDRESS and nothing
		// else: the extra text would travel into a mail header in #325 and into
		// the Organization's export.
		"Carla Ruiz <carla@example.com>",
		"<carla@example.com>",
	} {
		if got, ok := ParseHolderEmail(bad); ok {
			t.Errorf("ParseHolderEmail(%q) accepted it as %q", bad, got)
		}
	}

	// The cap exists because this is the one text field on the platform typed by
	// one person ABOUT ANOTHER, so nothing downstream can sanity-check it.
	long := ""
	for len(long) < MaxHolderEmailLength {
		long += "a"
	}
	if _, ok := ParseHolderEmail(long + "@example.com"); ok {
		t.Error("an address past MaxHolderEmailLength was accepted")
	}
}
