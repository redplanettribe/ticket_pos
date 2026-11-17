import assert from "node:assert/strict";
import test from "node:test";

import {
  mintedRequestId,
  REQUEST_ID_COOKIE,
  readReferenceCode,
  referenceCodeCookie,
} from "../src/index.ts";

const ID = "3f6c1a52-8d1e-4b7a-9c39-2a1f0e5d4c3b";

// What a middleware is handed. Next deletes its own flight headers (rsc,
// next-router-state-tree, next-router-prefetch, next-router-segment-prefetch)
// from the request before middleware runs, so a router prefetch and a client
// navigation reach it as the same bare fetch: these are the headers that are
// left, as a browser sends them (#791).
const documentLoad = { "sec-fetch-dest": "document", "sec-fetch-mode": "navigate", accept: "text/html,*/*;q=0.8" };
const routerFetch = { "sec-fetch-dest": "empty", "sec-fetch-mode": "cors", accept: "*/*" };

test("a page load hands its request id to the browser", () => {
  for (const pathname of ["/", "/events/abc", "/login", "/es/the-builders"]) {
    const cookie = referenceCodeCookie({ pathname, headers: new Headers(documentLoad) }, ID);
    assert.ok(cookie, pathname);
    assert.equal(cookie.name, REQUEST_ID_COOKIE);
    assert.equal(cookie.value, ID);
    // Read by the error screens, so script must see it; it is an id, not a credential.
    assert.equal(cookie.options.httpOnly, false);
    assert.equal(cookie.options.sameSite, "lax");
    assert.equal(cookie.options.path, "/");
    // A session cookie: it names the last request, nothing worth keeping.
    assert.equal("maxAge" in cookie.options, false);
  }
});

test("a page loaded in a frame, or by a client that sends no fetch metadata, is a page load", () => {
  assert.ok(referenceCodeCookie({ pathname: "/es", headers: new Headers({ "sec-fetch-dest": "iframe" }) }, ID));
  assert.ok(referenceCodeCookie({ pathname: "/es", headers: new Headers({ accept: "text/html" }) }, ID));
});

test("a server action hands its request id to the browser", () => {
  const action = new Headers({ ...routerFetch, "next-action": "7f3a" });
  assert.equal(referenceCodeCookie({ pathname: "/events/abc", headers: action }, ID)?.value, ID);
});

test("a router fetch does not overwrite the page's reference code, prefetch or not", () => {
  // The bug in #791: two link prefetches after the page each set the cookie,
  // so the page's error screen showed a prefetch's id, found nowhere in the logs.
  for (const headers of [routerFetch, { "sec-fetch-dest": "empty" }, { accept: "*/*" }, {}]) {
    assert.equal(referenceCodeCookie({ pathname: "/es/following", headers: new Headers(headers) }, ID), null);
  }
});

test("a BFF call or a browser prefetch does not overwrite the page's reference code", () => {
  const page = new Headers(documentLoad);
  assert.equal(referenceCodeCookie({ pathname: "/api/auth/session", headers: page }, ID), null);
  assert.equal(referenceCodeCookie({ pathname: "/api/monitoring", headers: new Headers(routerFetch) }, ID), null);
  const browserPrefetch = new Headers({ ...documentLoad, "sec-purpose": "prefetch" });
  assert.equal(referenceCodeCookie({ pathname: "/events/abc", headers: browserPrefetch }, ID), null);
  const legacyPrefetch = new Headers({ ...documentLoad, purpose: "prefetch" });
  assert.equal(referenceCodeCookie({ pathname: "/events/abc", headers: legacyPrefetch }, ID), null);
});

test("the reference code is read back from document.cookie", () => {
  assert.equal(readReferenceCode(`NEXT_LOCALE=es; ${REQUEST_ID_COOKIE}=${ID}; other=1`), ID);
  assert.equal(readReferenceCode(`${REQUEST_ID_COOKIE}=${ID}`), ID);
  assert.equal(readReferenceCode("NEXT_LOCALE=es"), null);
  assert.equal(readReferenceCode(""), null);
  // A name that only ends like ours is not ours.
  assert.equal(readReferenceCode(`x${REQUEST_ID_COOKIE}=${ID}`), null);
  // Anything but an id shape is not shown to a person as a code to quote.
  assert.equal(readReferenceCode(`${REQUEST_ID_COOKIE}=<script>`), null);
  assert.equal(readReferenceCode(`${REQUEST_ID_COOKIE}=`), null);
});

test("only an id of the shape the middleware mints is forwarded to the API", () => {
  assert.equal(mintedRequestId(ID), ID);
  const minted = crypto.randomUUID();
  assert.equal(mintedRequestId(minted), minted);
  // What a browser could send where the middleware does not run.
  for (const sent of [null, undefined, "", "abc", `${ID} injected`, `${ID}\nseverity=ERROR`, ID.toUpperCase(), "x".repeat(36)]) {
    assert.equal(mintedRequestId(sent), null, String(sent));
  }
});
