import assert from "node:assert/strict";
import test from "node:test";

import { DEFAULT_DESTINATION, safeNext } from "./destination.ts";

test("safeNext keeps an ordinary Storefront path", () => {
  assert.equal(safeNext("/rock-fest/events/summer-night"), "/rock-fest/events/summer-night");
  assert.equal(safeNext("/tickets?from=email"), "/tickets?from=email");
});

test("safeNext falls back to the Customer Area when nothing was asked for", () => {
  assert.equal(safeNext(undefined), DEFAULT_DESTINATION);
  assert.equal(safeNext(null), DEFAULT_DESTINATION);
  assert.equal(safeNext(""), DEFAULT_DESTINATION);
});

test("safeNext refuses to bounce a visitor off this Storefront", () => {
  assert.equal(safeNext("https://evil.example/steal"), DEFAULT_DESTINATION);
  assert.equal(safeNext("//evil.example/steal"), DEFAULT_DESTINATION);
  assert.equal(safeNext("javascript:alert(1)"), DEFAULT_DESTINATION);
  assert.equal(safeNext("tickets"), DEFAULT_DESTINATION);
});
