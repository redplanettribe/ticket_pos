import assert from "node:assert/strict";
import test from "node:test";

import {
  REQUEST_ID_COOKIE,
  readReferenceCode,
  referenceCodeCookie,
} from "./request-id.ts";

const ID = "3f6c1a52-8d1e-4b7a-9c39-2a1f0e5d4c3b";

test("a page or an RSC navigation hands its request id to the browser", () => {
  for (const pathname of ["/", "/events/abc", "/login"]) {
    const cookie = referenceCodeCookie({ pathname, prefetch: false }, ID);
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

test("a BFF call or a prefetch does not overwrite the page's reference code", () => {
  assert.equal(referenceCodeCookie({ pathname: "/api/auth/session", prefetch: false }, ID), null);
  assert.equal(referenceCodeCookie({ pathname: "/api/monitoring", prefetch: false }, ID), null);
  assert.equal(referenceCodeCookie({ pathname: "/events/abc", prefetch: true }, ID), null);
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
