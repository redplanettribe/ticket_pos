import assert from "node:assert/strict";
import test from "node:test";

import {
  OPEN_CHECKOUT_PARAM,
  checkoutReturnPath,
  checkoutSignInHref,
  isCheckoutDestination,
  opensCheckout,
  retryCheckoutPath,
} from "./checkout-signin.ts";

const EVENT_PATH = "/demo-venue/events/midnight-synth-live";

test("the return path carries the basket and asks for the dialog", () => {
  assert.equal(
    checkoutReturnPath(EVENT_PATH, { ga: 2, vip: 1 }),
    `${EVENT_PATH}?sel=ga:2,vip:1&checkout=1`,
  );
});

test("the selection is written unescaped, so the address stays readable", () => {
  // The whole argument for putting a basket in a URL rather than in storage
  // (#383) was that a person can read it. A "%3A" here would not be wrong, but
  // it would be a quiet retreat from that.
  const path = checkoutReturnPath(EVENT_PATH, { ga: 2 });
  assert.ok(path.includes("sel=ga:2"), path);
});

test("an empty basket omits the parameter rather than writing sel=", () => {
  assert.equal(checkoutReturnPath(EVENT_PATH, {}), `${EVENT_PATH}?checkout=1`);
});

test("a query or fragment already on the Event path is dropped, not merged", () => {
  // `ref` in particular: the Affiliate click it names was banked in a cookie on
  // the way in (ADR 0022), and coming back through it would count it twice.
  assert.equal(
    checkoutReturnPath(`${EVENT_PATH}?ref=SPRING#tickets`, { ga: 1 }),
    `${EVENT_PATH}?sel=ga:1&checkout=1`,
  );
});

test("a destination that is not a path on this Storefront cannot be built", () => {
  // safeNext's job, applied here too because this string becomes a `next`.
  assert.equal(checkoutReturnPath("https://evil.example/x", { ga: 1 }), "/tickets?sel=ga:1&checkout=1");
  assert.equal(checkoutReturnPath("//evil.example/x", {}), "/tickets?checkout=1");
});

test("the wall points at the ordinary sign-in page, carrying the whole return address", () => {
  const href = checkoutSignInHref(EVENT_PATH, { ga: 2 });
  const url = new URL(href, "http://localhost");
  assert.equal(url.pathname, "/signin");
  assert.equal(url.searchParams.get("next"), `${EVENT_PATH}?sel=ga:2&checkout=1`);
});

test("the sign-in page reads the reason off the destination", () => {
  assert.equal(isCheckoutDestination(checkoutReturnPath(EVENT_PATH, { ga: 1 })), true);
  assert.equal(isCheckoutDestination(checkoutReturnPath(EVENT_PATH, {})), true);
});

test("an ordinary destination is not a checkout, and says nothing about why", () => {
  assert.equal(isCheckoutDestination("/tickets"), false);
  assert.equal(isCheckoutDestination(EVENT_PATH), false);
  assert.equal(isCheckoutDestination(`${EVENT_PATH}?sel=ga:1`), false);
  assert.equal(isCheckoutDestination(`${EVENT_PATH}?checkout=0`), false);
  assert.equal(isCheckoutDestination(`${EVENT_PATH}?checkout=yes`), false);
  assert.equal(isCheckoutDestination(null), false);
  assert.equal(isCheckoutDestination(undefined), false);
});

test("a marker in the fragment is not a marker", () => {
  assert.equal(isCheckoutDestination(`${EVENT_PATH}#?checkout=1`), false);
});

test("the arrival marker is exactly one value", () => {
  assert.equal(opensCheckout("1"), true);
  assert.equal(opensCheckout("true"), false);
  assert.equal(opensCheckout(""), false);
  assert.equal(opensCheckout(undefined), false);
  assert.equal(opensCheckout(null), false);
});

test("a repeated marker is no marker: two answers to a yes/no question", () => {
  assert.equal(opensCheckout(["1", "1"]), false);
});

test("the parameter is spelled in one place", () => {
  assert.ok(checkoutReturnPath(EVENT_PATH, {}).includes(`${OPEN_CHECKOUT_PARAM}=`));
});

test("a declined Payment sends the buyer back with the basket they were paying for", () => {
  assert.equal(retryCheckoutPath(EVENT_PATH, "ga:2"), `${EVENT_PATH}?sel=ga:2`);
});

test("and never with the dialog reopening on top of the bad news", () => {
  assert.ok(!retryCheckoutPath(EVENT_PATH, "ga:2").includes("checkout="));
});

test("a context that remembered no basket retries the bare Event page", () => {
  assert.equal(retryCheckoutPath(EVENT_PATH, ""), EVENT_PATH);
  assert.equal(retryCheckoutPath(EVENT_PATH, "  "), EVENT_PATH);
});

test("a retry destination cannot leave this Storefront either", () => {
  assert.equal(retryCheckoutPath("https://evil.example/x", "ga:1"), "/tickets?sel=ga:1");
});
