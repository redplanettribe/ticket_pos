import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { payableBalance, type PayableBalanceState } from "./payable-balance.ts";

// WHAT THESE ASSERT NOW. The sentence moved to the catalogs (ADR 0041), so what
// is left here is the decision it was always built on: which of the three things
// is true, and which figures the sentence about it is allowed to name. A copy
// edit cannot break any of it, and the last test below is what makes sure a
// state cannot reach an organizer with nothing to say in either language.

test("names both figures when part of the balance has not cleared", () => {
  assert.deepEqual(payableBalance(120_000, 90_000), {
    state: "some_uncleared",
    payableCents: 90_000,
    unclearedCents: 30_000,
  });
});

test("a fully cleared balance has no uncleared half to name", () => {
  assert.deepEqual(payableBalance(90_000, 90_000), {
    state: "all_cleared",
    payableCents: 90_000,
    unclearedCents: 0,
  });
});

// The case ADR 0026 says is correct rather than a bug: settled in full against
// money that had not cleared, then sold again the same day. The Organization is
// owed something and may ask for none of it, and printing a negative number on
// its own would tell them nothing they can act on — which is why this is its own
// state with its own sentence rather than a comparison.
test("a negative Payable Balance against a positive Withdrawable one is its own state", () => {
  const balance = payableBalance(30_000, -90_000);
  assert.equal(balance.state, "nothing_cleared");
  // Nothing to draw: the sentence for this state names no amount, so a negative
  // figure can never reach a screen through it.
  assert.equal(balance.unclearedCents, 0);
});

test("a zero Payable Balance is explained rather than offered", () => {
  assert.equal(payableBalance(30_000, 0).state, payableBalance(30_000, -1).state);
  assert.equal(payableBalance(30_000, 0).state, "nothing_cleared");
});

test("every state has a sentence to be said in, in both languages", () => {
  const keys: Record<PayableBalanceState, string> = {
    nothing_cleared: "balanceNothingCleared",
    all_cleared: "balanceAllCleared",
    some_uncleared: "balanceSomeUncleared",
  };
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { payouts: Record<string, string> };
    for (const key of Object.values(keys)) {
      assert.ok(catalog.payouts[key], `${locale}.json is missing payouts.${key}`);
    }
    // "Available balance" is on Payable Balance's _Avoid_ list in CONTEXT.md, and
    // this sentence is where an organizer learns what the term means. The Spanish
    // inherits the bar: *saldo disponible* is the same mistake in the other
    // language.
    assert.ok(
      !catalog.payouts.balanceAllCleared.toLowerCase().includes("disponible"),
      "the Spanish must not call it a saldo disponible",
    );
  }
});
