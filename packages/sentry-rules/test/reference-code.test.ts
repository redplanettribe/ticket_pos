import assert from "node:assert/strict";
import test from "node:test";

import {
  forwardedRequestHeaders,
  mintedRequestId,
  REFERENCE_CODE_HEADER,
  REQUEST_ID_COOKIE,
  REQUEST_ID_HEADER,
  readReferenceCode,
  referenceCodeCookie,
} from "../src/index.ts";

const ID = "3f6c1a52-8d1e-4b7a-9c39-2a1f0e5d4c3b";
const PAGE_ID = "9a1d2c3b-4e5f-4a6b-8c7d-0e1f2a3b4c5d";

/**
 * Next's flight headers, which Next 15.5 deletes from a request before the
 * middleware runs (next/dist/server/web/adapter.js, FLIGHT_HEADERS). Every
 * request below is built without them, as the middleware really receives it.
 */
const FLIGHT_HEADERS = ["rsc", "next-router-state-tree", "next-router-prefetch", "next-router-segment-prefetch", "next-hmr-refresh"];

function asMiddlewareSeesIt(init: Record<string, string>): Headers {
  const headers = new Headers(init);
  for (const name of FLIGHT_HEADERS) headers.delete(name);
  return headers;
}

const HTML_ACCEPT = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8";

/** The page's cookie, as the browser sends it on every later request. */
const pageCookie = `NEXT_LOCALE=es; ${REQUEST_ID_COOKIE}=${PAGE_ID}`;

/** A typed address, a reload, or a link followed without the router: the browser fetches the document. */
const pageLoad = asMiddlewareSeesIt({
  accept: HTML_ACCEPT,
  "sec-fetch-dest": "document",
  "sec-fetch-mode": "navigate",
  "sec-fetch-site": "none",
  cookie: pageCookie,
});

/**
 * The router's fetch, for a link prefetch and for a client navigation alike:
 * once the flight headers are gone the two are the same request.
 */
const routerFetch = asMiddlewareSeesIt({
  accept: "*/*",
  "sec-fetch-dest": "empty",
  "sec-fetch-mode": "cors",
  "sec-fetch-site": "same-origin",
  cookie: pageCookie,
});
const routerPrefetch = routerFetch;
const clientNavigation = routerFetch;

/** A server action: the router posts to the page with Next-Action, which Next does not strip. */
const serverAction = asMiddlewareSeesIt({
  accept: "text/x-component",
  "next-action": "7f3c9e0a1b2c3d4e5f60718293a4b5c6d7e8f901",
  "sec-fetch-dest": "empty",
  "sec-fetch-mode": "cors",
  "sec-fetch-site": "same-origin",
  cookie: pageCookie,
});

/** The browser's own prefetch or prerender (speculation rules, link rel=prefetch): a document fetch marked as one. */
const browserPrefetch = asMiddlewareSeesIt({
  accept: HTML_ACCEPT,
  "sec-purpose": "prefetch;prerender",
  "sec-fetch-dest": "document",
  "sec-fetch-mode": "navigate",
  "sec-fetch-site": "same-origin",
  cookie: pageCookie,
});
const legacyBrowserPrefetch = asMiddlewareSeesIt({ accept: HTML_ACCEPT, purpose: "prefetch", cookie: pageCookie });

/** A BFF call from the page's own script. */
const bffCall = asMiddlewareSeesIt({
  accept: "application/json",
  "sec-fetch-dest": "empty",
  "sec-fetch-mode": "cors",
  "sec-fetch-site": "same-origin",
  cookie: pageCookie,
});

test("a page load hands its request id to the browser", () => {
  for (const pathname of ["/", "/events/abc", "/login"]) {
    const cookie = referenceCodeCookie({ pathname, headers: pageLoad }, ID);
    assert.ok(cookie, pathname);
    assert.equal(cookie.name, REQUEST_ID_COOKIE);
    assert.equal(cookie.value, ID);
    // Read by the error screens, so script must see it; it is an id, not a credential.
    assert.equal(cookie.options.httpOnly, false);
    assert.equal(cookie.options.sameSite, "lax");
    assert.equal(cookie.options.path, "/");
    // A session cookie: it names the page, nothing worth keeping.
    assert.equal("maxAge" in cookie.options, false);
  }
});

test("a page load sets the cookie to the id its render forwards to the API", () => {
  const request = { pathname: "/events/abc", headers: pageLoad };
  const cookie = referenceCodeCookie(request, ID);
  const forwarded = forwardedRequestHeaders(request, ID);
  assert.equal(forwarded.get(REQUEST_ID_HEADER), ID);
  assert.equal(cookie?.value, forwarded.get(REQUEST_ID_HEADER));
  // The code its error screen will show is its own id.
  assert.equal(forwarded.get(REFERENCE_CODE_HEADER), ID);
});

test("a page load from a browser without fetch metadata is told by what it accepts", () => {
  const old = asMiddlewareSeesIt({ accept: HTML_ACCEPT });
  assert.equal(referenceCodeCookie({ pathname: "/", headers: old }, ID)?.value, ID);
  const oldFetch = asMiddlewareSeesIt({ accept: "*/*" });
  assert.equal(referenceCodeCookie({ pathname: "/", headers: oldFetch }, ID), null);
});

test("a page in a frame is a page load", () => {
  for (const dest of ["iframe", "frame"]) {
    const headers = asMiddlewareSeesIt({ accept: HTML_ACCEPT, "sec-fetch-dest": dest, "sec-fetch-mode": "navigate" });
    assert.equal(referenceCodeCookie({ pathname: "/", headers }, ID)?.value, ID, dest);
  }
});

test("a router prefetch made after a page load does not change the cookie", () => {
  // The page load sets the cookie the browser then sends on every request.
  const page = referenceCodeCookie({ pathname: "/es/the-builders/events/devday", headers: pageLoad }, PAGE_ID);
  assert.equal(page?.value, PAGE_ID);
  // The links in view are prefetched: none of them writes the cookie.
  for (const pathname of ["/es/following", "/es/the-builders"]) {
    assert.equal(referenceCodeCookie({ pathname, headers: routerPrefetch }, ID), null, pathname);
  }
});

test("a client navigation keeps the page's code, and its render is told that code beside its own id", () => {
  const request = { pathname: "/es/the-builders", headers: clientNavigation };
  assert.equal(referenceCodeCookie(request, ID), null);
  const forwarded = forwardedRequestHeaders(request, ID);
  // Its API calls carry its own id; a failure in it is logged and tagged with the code the screen shows.
  assert.equal(forwarded.get(REQUEST_ID_HEADER), ID);
  assert.equal(forwarded.get(REFERENCE_CODE_HEADER), PAGE_ID);
});

test("a server action hands its request id to the browser", () => {
  const request = { pathname: "/events/abc", headers: serverAction };
  assert.equal(referenceCodeCookie(request, ID)?.value, ID);
  assert.equal(forwardedRequestHeaders(request, ID).get(REFERENCE_CODE_HEADER), ID);
});

test("a browser prefetch does not overwrite the page's reference code", () => {
  for (const headers of [browserPrefetch, legacyBrowserPrefetch]) {
    assert.equal(referenceCodeCookie({ pathname: "/events/abc", headers }, ID), null);
  }
});

test("a BFF call does not overwrite the page's reference code", () => {
  for (const pathname of ["/api", "/api/auth/session", "/api/monitoring"]) {
    assert.equal(referenceCodeCookie({ pathname, headers: bffCall }, ID), null, pathname);
    // Even one that looks like a document fetch.
    assert.equal(referenceCodeCookie({ pathname, headers: pageLoad }, ID), null, pathname);
  }
});

test("the forwarded headers keep the request's own and replace any id the browser sent", () => {
  const sent = new Headers(routerFetch);
  sent.set(REQUEST_ID_HEADER, "chosen-by-browser");
  sent.set(REFERENCE_CODE_HEADER, "chosen-by-browser");
  const forwarded = forwardedRequestHeaders({ pathname: "/", headers: sent }, ID);
  assert.equal(forwarded.get(REQUEST_ID_HEADER), ID);
  assert.equal(forwarded.get(REFERENCE_CODE_HEADER), PAGE_ID);
  assert.equal(forwarded.get("cookie"), pageCookie);
  // The request it was copied from is left as it was.
  assert.equal(sent.get(REQUEST_ID_HEADER), "chosen-by-browser");
});

test("a request with no page code, or one not of our shape, forwards no reference code", () => {
  for (const cookie of ["", "NEXT_LOCALE=es", `${REQUEST_ID_COOKIE}=<script>`, `${REQUEST_ID_COOKIE}=${ID}%0Aseverity=ERROR`]) {
    const headers = new Headers(routerFetch);
    headers.set("cookie", cookie);
    headers.set(REFERENCE_CODE_HEADER, ID);
    const forwarded = forwardedRequestHeaders({ pathname: "/", headers }, ID);
    assert.equal(forwarded.has(REFERENCE_CODE_HEADER), false, cookie);
  }
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
