import assert from "node:assert/strict";
import test from "node:test";

import {
  availabilityBadgeVariant,
  capacityCounts,
  closedAt,
  remainingCapacity,
  soldShare,
  ticketTypeAvailability,
} from "./ticket-type-availability.ts";

/** An instant every case below is judged at, so nothing here reads a real clock. */
const NOW = new Date("2026-09-11T17:00:00Z");

/** A Ticket Type that never stops selling, which is most of them. */
function neverCloses(pool: { capacity: number; sold_count: number }) {
  return { ...pool, sales_cutoff_at: null };
}

test("the Event gates availability before capacity does", () => {
  const plenty = neverCloses({ capacity: 100, sold_count: 0 });
  for (const status of ["draft", "cancelled", "archived"]) {
    assert.equal(ticketTypeAvailability(plenty, status, NOW), "not_on_sale", status);
  }
  assert.equal(ticketTypeAvailability(plenty, "published", NOW), "on_sale");
});

test("availability reads the capacity pool at its boundaries", () => {
  const cases: Array<{ capacity: number; sold_count: number; want: string }> = [
    { capacity: 100, sold_count: 0, want: "on_sale" },
    { capacity: 100, sold_count: 89, want: "on_sale" },
    // Exactly a tenth left is already low: the threshold is inclusive.
    { capacity: 100, sold_count: 90, want: "low_stock" },
    { capacity: 100, sold_count: 99, want: "low_stock" },
    { capacity: 100, sold_count: 100, want: "sold_out" },
    // Over-sold (an import can land past capacity) still reads as sold out,
    // never as negative stock.
    { capacity: 100, sold_count: 104, want: "sold_out" },
    // A pool of one is on sale until it goes, never "low stock" first.
    { capacity: 1, sold_count: 0, want: "on_sale" },
    { capacity: 1, sold_count: 1, want: "sold_out" },
  ];

  for (const { capacity, sold_count, want } of cases) {
    assert.equal(
      ticketTypeAvailability(neverCloses({ capacity, sold_count }), "published", NOW),
      want,
      `${sold_count}/${capacity}`,
    );
  }
});

test("closedAt is half-open with the cutoff instant itself closed, and nil never closes", () => {
  // Mirrors the Go predicate `catalog.ClosedAt`, which is the one the server
  // judges a checkout with: the two must never disagree about an instant.
  const cutoff = "2026-09-11T17:00:00Z";
  assert.equal(closedAt(cutoff, new Date("2026-09-11T16:59:59Z")), false);
  assert.equal(closedAt(cutoff, new Date(cutoff)), true);
  assert.equal(closedAt(cutoff, new Date("2026-09-11T17:00:01Z")), true);
  // A Ticket Type that never stops selling, which is most of them.
  assert.equal(closedAt(null, new Date("2099-01-01T00:00:00Z")), false);
  // The offset is the API's to choose: the same instant written two ways is
  // the same instant, because the cutoff is read in the Event's timezone and
  // never in the reader's.
  assert.equal(closedAt("2026-09-11T12:00:00-05:00", new Date(cutoff)), true);
  // A value this app cannot read is not grounds for claiming a Ticket Type
  // has stopped selling.
  assert.equal(closedAt("not an instant", NOW), false);
});

test("a Ticket Type past its Sales Cutoff reads as closed", () => {
  const plenty = { capacity: 100, sold_count: 0 };
  const past = { ...plenty, sales_cutoff_at: "2026-09-11T16:00:00Z" };
  const future = { ...plenty, sales_cutoff_at: "2026-09-11T18:00:00Z" };

  assert.equal(ticketTypeAvailability(past, "published", NOW), "closed");
  // Until it passes, nothing changes: a cutoff still ahead is not a state.
  assert.equal(ticketTypeAvailability(future, "published", NOW), "on_sale");
  // Half-open, exactly as the Go predicate is: the cutoff instant is closed.
  assert.equal(
    ticketTypeAvailability({ ...plenty, sales_cutoff_at: NOW.toISOString() }, "published", NOW),
    "closed",
  );
});

test("closed is ranked after the Event gate and after sold out, and before low stock", () => {
  // The Storefront's order, and the reason a disagreement between the two apps
  // is a bug rather than a matter of taste (ADR 0070).
  const cutoff = "2026-09-11T16:00:00Z";

  // The Event gates first: a draft Event says nothing about a cutoff.
  assert.equal(
    ticketTypeAvailability({ capacity: 100, sold_count: 0, sales_cutoff_at: cutoff }, "draft", NOW),
    "not_on_sale",
  );
  // Sold out beats closed: it is the stronger fact and it changes what the
  // reader does next.
  assert.equal(
    ticketTypeAvailability(
      { capacity: 100, sold_count: 100, sales_cutoff_at: cutoff },
      "published",
      NOW,
    ),
    "sold_out",
  );
  // Closed beats low stock: how many are left does not matter once nobody can
  // buy them.
  assert.equal(
    ticketTypeAvailability(
      { capacity: 100, sold_count: 95, sales_cutoff_at: cutoff },
      "published",
      NOW,
    ),
    "closed",
  );
  // And with the cutoff still ahead, low stock is what is left to say.
  assert.equal(
    ticketTypeAvailability(
      { capacity: 100, sold_count: 95, sales_cutoff_at: "2026-09-11T18:00:00Z" },
      "published",
      NOW,
    ),
    "low_stock",
  );
});

test("remainingCapacity floors at zero", () => {
  assert.equal(remainingCapacity({ capacity: 100, sold_count: 40 }), 60);
  assert.equal(remainingCapacity({ capacity: 100, sold_count: 100 }), 0);
  assert.equal(remainingCapacity({ capacity: 100, sold_count: 140 }), 0);
});

test("availabilityBadgeVariant colours each state, and two states share a colour", () => {
  assert.equal(availabilityBadgeVariant("on_sale"), "success");
  assert.equal(availabilityBadgeVariant("low_stock"), "warning");
  // Sold out and not on sale share the neutral colour, which is exactly why the
  // badge must also carry a word. The words are the catalog's now
  // (`ticketTypes.availabilitySoldOut` / `availabilityNotOnSale`), and the
  // states stay distinct tokens here so the two can never collapse into one.
  assert.equal(availabilityBadgeVariant("sold_out"), "secondary");
  assert.equal(availabilityBadgeVariant("not_on_sale"), "secondary");
  // Closed takes the neutral colour and NOT the amber one the Storefront's
  // countdown wears: this card is a control panel, and amber here would read
  // as a problem to fix rather than as a decision the organizer made.
  assert.equal(availabilityBadgeVariant("closed"), "secondary");
});

test("capacityCounts returns the line's parts and never over-counts the sold side", () => {
  assert.deepEqual(capacityCounts({ capacity: 100, sold_count: 32 }), {
    sold: 32,
    capacity: 100,
    remaining: 68,
  });
  assert.deepEqual(capacityCounts({ capacity: 1250, sold_count: 1000 }), {
    sold: 1000,
    capacity: 1250,
    remaining: 250,
  });
  // An over-sold pool reports a full house, not more sold than exist.
  assert.deepEqual(capacityCounts({ capacity: 100, sold_count: 140 }), {
    sold: 100,
    capacity: 100,
    remaining: 0,
  });
});

test("soldShare stays within the meter and survives a zero pool", () => {
  assert.equal(soldShare({ capacity: 100, sold_count: 0 }), 0);
  assert.equal(soldShare({ capacity: 100, sold_count: 50 }), 0.5);
  assert.equal(soldShare({ capacity: 100, sold_count: 140 }), 1);
  assert.equal(soldShare({ capacity: 0, sold_count: 0 }), 1);
});
