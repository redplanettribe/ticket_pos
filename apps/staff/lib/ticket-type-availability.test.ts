import assert from "node:assert/strict";
import test from "node:test";

import {
  availabilityBadgeVariant,
  capacityCounts,
  remainingCapacity,
  soldShare,
  ticketTypeAvailability,
} from "./ticket-type-availability.ts";

test("the Event gates availability before capacity does", () => {
  const plenty = { capacity: 100, sold_count: 0 };
  for (const status of ["draft", "cancelled", "archived"]) {
    assert.equal(ticketTypeAvailability(plenty, status), "not_on_sale", status);
  }
  assert.equal(ticketTypeAvailability(plenty, "published"), "on_sale");
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
      ticketTypeAvailability({ capacity, sold_count }, "published"),
      want,
      `${sold_count}/${capacity}`,
    );
  }
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
