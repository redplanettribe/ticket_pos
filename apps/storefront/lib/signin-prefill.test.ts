import assert from "node:assert/strict";
import test from "node:test";

import { safePrefillEmail } from "./signin-prefill.ts";

test("safePrefillEmail passes an ordinary address through", () => {
  assert.equal(safePrefillEmail("ana@example.com"), "ana@example.com");
  assert.equal(safePrefillEmail("  ana@example.com  "), "ana@example.com");
});

test("safePrefillEmail drops anything that is not plausibly an address", () => {
  assert.equal(safePrefillEmail(null), "");
  assert.equal(safePrefillEmail(""), "");
  assert.equal(safePrefillEmail("not an email"), "");
  assert.equal(safePrefillEmail("<script>alert(1)</script>"), "");
  assert.equal(safePrefillEmail(`${"a".repeat(250)}@example.com`), "");
});
