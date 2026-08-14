import assert from "node:assert/strict";
import test from "node:test";

import { CREATE_INTENT, resolveSignInIntent } from "./login-copy.ts";

/**
 * These assertions used to be about prose — the exact two sentences the card
 * showed — because the module returned sentences. It returns a token now (#286),
 * so they are about the decision instead: which card, given what arrived in the
 * URL. The words themselves are the catalog's, and are guarded by
 * lib/messages.test.ts and by the compiler, so a copy edit no longer breaks a
 * logic test and a Spanish translation no longer has to be asserted twice.
 */

test("the create intent is the literal value the Storefront link carries", () => {
  assert.equal(CREATE_INTENT, "create");
  assert.equal(resolveSignInIntent(CREATE_INTENT), "create");
  assert.equal(resolveSignInIntent("create"), "create");
});

// --- the case that actually matters ---------------------------------------
//
// A query parameter must never alter the door every existing Member uses daily.
// Absent, unknown, empty and malformed values each yield the ordinary card.

test("an absent intent yields the ordinary card", () => {
  assert.equal(resolveSignInIntent(undefined), "default");
});

test("an unknown intent yields the ordinary card", () => {
  for (const intent of ["join", "signup", "Create", "CREATE", "creates", "create-event"]) {
    assert.equal(resolveSignInIntent(intent), "default", intent);
  }
});

test("an empty or whitespace-only intent yields the ordinary card", () => {
  for (const intent of ["", " ", "   ", "\t", "\n"]) {
    assert.equal(resolveSignInIntent(intent), "default", JSON.stringify(intent));
  }
});

test("a malformed intent yields the ordinary card rather than throwing", () => {
  // Next hands back an array when the parameter is repeated, and a hand-mangled
  // URL can produce anything at all.
  const malformed: Array<string | string[] | undefined> = [
    ["create", "create"],
    ["create"],
    ["join", "create"],
    [],
    "create=create",
    "create ",
    " create",
    "create&intent=create",
    "%63reate",
    "<script>alert(1)</script>",
    "create\0",
    "a".repeat(10_000),
  ];

  for (const intent of malformed) {
    assert.equal(resolveSignInIntent(intent), "default", JSON.stringify(intent));
  }
});

test("a value that is not a string at all yields the ordinary card", () => {
  // The page reads searchParams, which is typed but not validated at runtime.
  const notStrings = [null, 0, 1, true, {}, { toString: () => "create" }, () => "create"];

  for (const intent of notStrings) {
    assert.equal(
      resolveSignInIntent(intent as unknown as string | undefined),
      "default",
      String(intent),
    );
  }
});
