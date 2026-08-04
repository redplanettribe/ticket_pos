import assert from "node:assert/strict";
import test from "node:test";

import { followIntent, followSignInHref, safeFollowIntent } from "./follow-intent.ts";

test("followIntent spells a Follow as kind and key", () => {
  assert.equal(followIntent("organization", "demo-venue"), "organization:demo-venue");
});

test("safeFollowIntent passes a well-formed intent through", () => {
  assert.equal(safeFollowIntent("organization:demo-venue"), "organization:demo-venue");
  assert.equal(safeFollowIntent("  organization:demo-venue  "), "organization:demo-venue");
  assert.equal(safeFollowIntent("organization:a1"), "organization:a1");
});

test("safeFollowIntent refuses anything that is not one", () => {
  for (const raw of [
    null,
    undefined,
    "",
    // No kind, or a kind this platform does not have.
    "demo-venue",
    "playlist:demo-venue",
    ":demo-venue",
    // A kind that is an address is the whole thing this format exists to make
    // unspellable: an intent names what is followed, never who is subscribed.
    "email:ana@example.com",
    "organization:ana@example.com",
    // Empty, shouted, spaced, or not a slug at all.
    "organization:",
    "organization:DEMO-VENUE",
    "organization:demo venue",
    "organization:demo_venue",
    // The redirect shapes. None of these is a slug, so none of them survives
    // reaching the subject position.
    "organization:https://evil.example/demo-venue",
    "organization://evil.example",
    "organization:../../demo-venue",
    // A second field appended to a format that has one.
    "organization:demo-venue:tag:techno",
    `organization:${"a".repeat(101)}`,
  ]) {
    assert.equal(safeFollowIntent(raw), null, `expected ${String(raw)} to be refused`);
  }
});

test("followSignInHref carries both where the visitor was and what they pressed", () => {
  const href = followSignInHref("organization:demo-venue", "/demo-venue");
  const params = new URL(href, "http://localhost").searchParams;
  assert.ok(href.startsWith("/signin?"));
  assert.equal(params.get("next"), "/demo-venue");
  assert.equal(params.get("follow"), "organization:demo-venue");
});

test("followSignInHref keeps the query, because on this Storefront it is the page", () => {
  const params = new URL(
    followSignInHref("organization:demo-venue", "/", "q=techno&tag=house"),
    "http://localhost",
  ).searchParams;
  assert.equal(params.get("next"), "/?q=techno&tag=house");
});

test("followSignInHref never leaves this Storefront and never carries a bad intent", () => {
  // A pathname that is not root-relative is normalized rather than trusted, so
  // the sign-in return cannot be aimed at another site.
  const escaped = new URL(
    followSignInHref("organization:demo-venue", "//evil.example/demo-venue"),
    "http://localhost",
  ).searchParams;
  assert.equal(escaped.get("next"), "/tickets");

  const junk = new URL(
    followSignInHref("playlist:demo-venue", "/demo-venue"),
    "http://localhost",
  ).searchParams;
  assert.equal(junk.get("follow"), null);
  assert.equal(junk.get("next"), "/demo-venue");
});
