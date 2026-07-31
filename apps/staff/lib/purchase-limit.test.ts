import assert from "node:assert/strict";
import test from "node:test";

import {
  parsePurchaseLimit,
  purchaseLimitFormValue,
  purchaseLimitSummary,
  purchaseLimitWireValue,
  type ParsedPurchaseLimit,
} from "./purchase-limit.ts";

test("an empty Purchase Limit field means no Purchase Limit, not a rejection", () => {
  // The trap this helper exists for: the required-field guard would read "" as
  // NaN and refuse every unrestricted Ticket Type.
  const cases = ["", " ", "   ", "\t"];
  for (const typed of cases) {
    assert.deepEqual(parsePurchaseLimit(typed), { kind: "unset" }, JSON.stringify(typed));
  }
});

test("a Purchase Limit is a whole positive integer, so zero and below are refused", () => {
  const cases: Array<{ typed: string; want: ParsedPurchaseLimit }> = [
    // One is the motivating case: a Free Ticket Type handed out one apiece.
    { typed: "1", want: { kind: "valid", maxPerCustomer: 1 } },
    { typed: "2", want: { kind: "valid", maxPerCustomer: 2 } },
    { typed: "10", want: { kind: "valid", maxPerCustomer: 10 } },
    // Leading and trailing whitespace is the browser's, not a statement.
    { typed: "  4  ", want: { kind: "valid", maxPerCustomer: 4 } },
    // Zero would say nobody may hold this Ticket Type, which is not what a
    // Purchase Limit is for; capacity says that.
    { typed: "0", want: { kind: "invalid" } },
    { typed: "-1", want: { kind: "invalid" } },
    // A fractional Purchase Limit is meaningless: tickets are held whole.
    { typed: "1.5", want: { kind: "invalid" } },
    { typed: "abc", want: { kind: "invalid" } },
    // parseInt would read these as 3 and 1; digits-only refuses them instead
    // of saving a limit the organizer never typed.
    { typed: "3 tickets", want: { kind: "invalid" } },
    { typed: "1e9", want: { kind: "invalid" } },
  ];

  for (const { typed, want } of cases) {
    assert.deepEqual(parsePurchaseLimit(typed), want, JSON.stringify(typed));
  }
});

test("the wire value is null for an unset Purchase Limit and the integer otherwise", () => {
  assert.equal(purchaseLimitWireValue({ kind: "unset" }), null);
  assert.equal(purchaseLimitWireValue({ kind: "valid", maxPerCustomer: 1 }), 1);
  assert.equal(purchaseLimitWireValue({ kind: "valid", maxPerCustomer: 25 }), 25);
});

test("a Ticket Type round-trips out of the form and back into it unchanged", () => {
  const cases: Array<number | null> = [null, 1, 2, 100];
  for (const maxPerCustomer of cases) {
    const parsed = parsePurchaseLimit(purchaseLimitFormValue(maxPerCustomer));
    assert.notEqual(parsed.kind, "invalid", String(maxPerCustomer));
    assert.equal(
      purchaseLimitWireValue(parsed as { kind: "unset" } | { kind: "valid"; maxPerCustomer: number }),
      maxPerCustomer,
    );
  }
});

test("the card says nothing when a Ticket Type has no Purchase Limit", () => {
  assert.equal(purchaseLimitSummary(null), null);
  assert.equal(purchaseLimitSummary(1), "Purchase Limit: 1 per customer");
  assert.equal(purchaseLimitSummary(4), "Purchase Limit: 4 per customer");
  assert.equal(
    purchaseLimitSummary(1500),
    `Purchase Limit: ${(1500).toLocaleString()} per customer`,
  );
});
