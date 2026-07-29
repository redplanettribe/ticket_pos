import assert from "node:assert/strict";
import test from "node:test";

import {
  AVAILABILITY_LABELS,
  availabilityBadgeVariant,
  capacitySummary,
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

test("availabilityBadgeVariant colours each state and never leans on colour alone", () => {
  assert.equal(availabilityBadgeVariant("on_sale"), "success");
  assert.equal(availabilityBadgeVariant("low_stock"), "warning");
  assert.equal(availabilityBadgeVariant("sold_out"), "secondary");
  assert.equal(availabilityBadgeVariant("not_on_sale"), "secondary");

  // The two secondary states must stay tellable apart by their label.
  assert.notEqual(AVAILABILITY_LABELS.sold_out, AVAILABILITY_LABELS.not_on_sale);
  for (const label of Object.values(AVAILABILITY_LABELS)) {
    assert.ok(label.length > 0);
  }
});

test("capacitySummary separates thousands and never over-counts the sold side", () => {
  assert.equal(capacitySummary({ capacity: 100, sold_count: 32 }), "32 of 100 sold · 68 left");
  assert.equal(
    capacitySummary({ capacity: 1250, sold_count: 1000 }),
    `${(1000).toLocaleString()} of ${(1250).toLocaleString()} sold · 250 left`,
  );
  // An over-sold pool reports a full house, not more sold than exist.
  assert.equal(capacitySummary({ capacity: 100, sold_count: 140 }), "100 of 100 sold · 0 left");
});

test("soldShare stays within the meter and survives a zero pool", () => {
  assert.equal(soldShare({ capacity: 100, sold_count: 0 }), 0);
  assert.equal(soldShare({ capacity: 100, sold_count: 50 }), 0.5);
  assert.equal(soldShare({ capacity: 100, sold_count: 140 }), 1);
  assert.equal(soldShare({ capacity: 0, sold_count: 0 }), 1);
});
