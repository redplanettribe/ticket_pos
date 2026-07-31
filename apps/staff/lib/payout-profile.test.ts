import assert from "node:assert/strict";
import test from "node:test";

import { maskAccountNumber, normalizeAccountNumber } from "./payout-profile.ts";

// --- account-number normalisation ----------------------------------------

test("normalizeAccountNumber strips the grouping copied off a bank statement", () => {
  assert.equal(normalizeAccountNumber(" 0022-0123 4821 "), "002201234821");
});

test("normalizeAccountNumber keeps leading zeros, which are load-bearing", () => {
  assert.equal(normalizeAccountNumber("0004821"), "0004821");
});

test("normalizeAccountNumber keeps a stray letter so the server can name it", () => {
  assert.equal(normalizeAccountNumber("2201-23A821"), "220123A821");
});

test("normalizeAccountNumber leaves an already-clean number alone", () => {
  assert.equal(normalizeAccountNumber("2201234821"), "2201234821");
});

// --- account-number masking ----------------------------------------------

test("maskAccountNumber shows four dots and the last four digits", () => {
  assert.equal(maskAccountNumber("2201234821"), "····4821");
});

test("maskAccountNumber normalises before masking, so grouping cannot shift the tail", () => {
  assert.equal(maskAccountNumber("2201-2348 21"), "····4821");
});

test("maskAccountNumber keeps a short number entirely hidden", () => {
  assert.equal(maskAccountNumber("4821"), "····");
  assert.equal(maskAccountNumber(""), "····");
});
