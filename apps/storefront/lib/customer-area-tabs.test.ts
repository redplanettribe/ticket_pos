import assert from "node:assert/strict";
import test from "node:test";

import {
  availableTabs,
  countFor,
  groupsFor,
  isUpcomingEvent,
  tabForLinkedSale,
  tabOf,
} from "./customer-area-tabs.ts";
import type { CustomerArea, HeldTicket, TicketSale } from "./customer-session.ts";

// A fixed "now" so that the fixtures can name dates on either side of it
// without the tests turning into a calendar.
const NOW = new Date("2026-08-22T12:00:00Z");

type EventSummary = TicketSale["event"];

function event(id: string, overrides: Partial<EventSummary> = {}): EventSummary {
  return {
    id,
    name: `Event ${id}`,
    slug: id,
    starts_at: "2026-09-01T20:00:00Z",
    ends_at: "2026-09-01T23:00:00Z",
    timezone: "America/Guayaquil",
    venue_name: null,
    ...overrides,
  };
}

const ORG = { id: "org", name: "Acme", slug: "acme" };

// The smallest TicketSale the type allows: every required field filled with
// something plausible, the ones a test cares about passed in.
function sale(id: string, overrides: Partial<TicketSale> = {}): TicketSale {
  return {
    id,
    confirmation_ref: `REF-${id}`,
    sold_at: "2026-08-01T10:00:00Z",
    status: "active",
    amount_cents: 1000,
    currency: "USD",
    lines: [{ ticket_type_name: "General", quantity: 1, unit_price_cents: 1000 }],
    event: event("ev"),
    organization: ORG,
    tax_id_type: null,
    tax_id_number: null,
    reversible: false,
    reversible_until: null,
    reversal_pending: false,
    reversal_status: null,
    ...overrides,
  };
}

function held(ticketId: string, ev: EventSummary): HeldTicket {
  return {
    ticket_id: ticketId,
    accepted_at: "2026-08-02T10:00:00Z",
    ticket_type_name: "General",
    event: ev,
    organization: ORG,
  };
}

function area(overrides: Partial<CustomerArea> = {}): CustomerArea {
  return { upcoming: [], past: [], holding: [], ...overrides };
}

test("tabOf names the tab in the URL and falls back to upcoming", () => {
  assert.equal(tabOf("past"), "past");
  assert.equal(tabOf("reversed"), "reversed");
  assert.equal(tabOf("upcoming"), "upcoming");
  assert.equal(tabOf(undefined), "upcoming");
  assert.equal(tabOf("nonsense"), "upcoming");
  // A repeated query parameter arrives as an array; the first one wins.
  assert.equal(tabOf(["past", "reversed"]), "past");
});

test("a reversed Sale sits only under Reversed, whichever list the API put it in", () => {
  const a = area({
    upcoming: [sale("u1"), sale("u2", { status: "reversed" })],
    past: [sale("p1", { status: "reversed" }), sale("p2")],
  });
  const ids = (tab: "upcoming" | "past" | "reversed") =>
    groupsFor(a, tab, NOW).flatMap((g) => g.sales.map((s) => s.id));
  assert.deepEqual(ids("upcoming"), ["u1"]);
  assert.deepEqual(ids("past"), ["p2"]);
  assert.deepEqual(ids("reversed"), ["u2", "p1"]);
});

test("a Reversal Request in flight is not a reversal: the Sale stays put", () => {
  const pending = { reversal_pending: true, reversal_status: "in_flight" as const };
  const a = area({
    upcoming: [sale("u1", pending)],
    past: [sale("p1", pending)],
  });
  assert.equal(countFor(a, "upcoming", NOW), 1);
  assert.equal(countFor(a, "past", NOW), 1);
  assert.equal(countFor(a, "reversed", NOW), 0);
});

test("availableTabs always offers Upcoming and hides an empty Past or Reversed", () => {
  assert.deepEqual(availableTabs(area(), NOW), ["upcoming"]);
  assert.deepEqual(
    availableTabs(area({ past: [sale("p1")] }), NOW),
    ["upcoming", "past"],
  );
  assert.deepEqual(
    availableTabs(area({ upcoming: [sale("u1", { status: "reversed" })] }), NOW),
    ["upcoming", "reversed"],
  );
  // A past list made only of reversed Sales leaves Past empty.
  assert.deepEqual(
    availableTabs(area({ past: [sale("p1", { status: "reversed" })] }), NOW),
    ["upcoming", "reversed"],
  );
});

test("countFor counts Sales and held Tickets together", () => {
  const a = area({
    upcoming: [sale("u1"), sale("u2", { status: "reversed" })],
    past: [sale("p1")],
    holding: [
      held("t1", event("future")),
      held("t2", event("gone", { starts_at: "2026-01-01T20:00:00Z", ends_at: "2026-01-01T23:00:00Z" })),
    ],
  });
  assert.equal(countFor(a, "upcoming", NOW), 2);
  assert.equal(countFor(a, "past", NOW), 2);
  assert.equal(countFor(a, "reversed", NOW), 1);
});

test("groupsFor groups by Event across Sales and held Tickets, in first-seen order", () => {
  const devfest = event("devfest");
  const gala = event("gala");
  const a = area({
    upcoming: [
      sale("s1", { event: devfest }),
      sale("s2", { event: gala }),
      sale("s3", { event: devfest }),
    ],
    holding: [held("t1", gala), held("t2", event("other"))],
  });
  const groups = groupsFor(a, "upcoming", NOW);
  assert.deepEqual(
    groups.map((g) => g.event.id),
    ["devfest", "gala", "other"],
  );
  assert.deepEqual(groups[0].sales.map((s) => s.id), ["s1", "s3"]);
  assert.deepEqual(groups[0].held, []);
  assert.deepEqual(groups[1].sales.map((s) => s.id), ["s2"]);
  assert.deepEqual(groups[1].held.map((h) => h.ticket_id), ["t1"]);
  assert.deepEqual(groups[2].sales, []);
  assert.deepEqual(groups[2].held.map((h) => h.ticket_id), ["t2"]);
  // A held Ticket never lands under Reversed: a reversed Sale takes its
  // Holders with it, so the API lists none.
  assert.deepEqual(groupsFor(a, "reversed", NOW), []);
});

test("isUpcomingEvent: by the end when there is one, by the start otherwise, always when unscheduled", () => {
  const before = new Date("2026-09-01T21:00:00Z");
  const after = new Date("2026-09-02T00:00:00Z");
  // Started but not ended: still upcoming, because the end is known.
  assert.equal(isUpcomingEvent(event("e"), before), true);
  assert.equal(isUpcomingEvent(event("e"), after), false);
  // No end: over once it has started.
  const noEnd = event("e", { ends_at: null });
  assert.equal(isUpcomingEvent(noEnd, new Date("2026-09-01T19:00:00Z")), true);
  assert.equal(isUpcomingEvent(noEnd, before), false);
  // No schedule at all has not happened yet.
  assert.equal(isUpcomingEvent(event("e", { starts_at: null, ends_at: null }), after), true);
});

test("tabForLinkedSale opens on the one Sale the Confirmation Link names", () => {
  assert.equal(tabForLinkedSale(area({ upcoming: [sale("u1")] })), "upcoming");
  assert.equal(tabForLinkedSale(area({ past: [sale("p1")] })), "past");
  assert.equal(
    tabForLinkedSale(area({ upcoming: [sale("u1", { status: "reversed" })] })),
    "reversed",
  );
  assert.equal(
    tabForLinkedSale(area({ past: [sale("p1", { status: "reversed" })] })),
    "reversed",
  );
  assert.equal(tabForLinkedSale(area()), "upcoming");
});
