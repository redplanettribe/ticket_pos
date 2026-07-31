import assert from "node:assert/strict";
import test from "node:test";

import { payableBalanceExplanation } from "./payable-balance.ts";

// A stand-in for the app's money formatter: the sentence's job is to name the
// two amounts and say why they differ, not to know what a dollar looks like.
const usd = (cents: number) => `$${(cents / 100).toFixed(2)}`;

test("names both amounts and says when the rest clears", () => {
  const sentence = payableBalanceExplanation(120_000, 90_000, usd);
  assert.equal(sentence, "$900.00 can be requested now — $300.00 from today's sales clears tomorrow.");
});

test("says so plainly when the whole balance has cleared", () => {
  const sentence = payableBalanceExplanation(90_000, 90_000, usd);
  assert.match(sentence, /^\$900\.00 can be requested now/);
  assert.match(sentence, /cleared/);
  assert.doesNotMatch(sentence, /tomorrow/);
});

// The case ADR 0026 says is correct rather than a bug: settled in full against
// money that had not cleared, then sold again the same day. The Organization is
// owed something and may ask for none of it, and printing a negative number on
// its own would tell them nothing they can act on.
test("explains a negative Payable Balance against a positive Withdrawable one", () => {
  const sentence = payableBalanceExplanation(30_000, -90_000, usd);
  assert.match(sentence, /can be requested yet/);
  assert.match(sentence, /clear tomorrow/);
  assert.doesNotMatch(sentence, /-/);
});

test("a zero Payable Balance is explained rather than offered", () => {
  assert.equal(payableBalanceExplanation(30_000, 0, usd), payableBalanceExplanation(30_000, -1, usd));
});
