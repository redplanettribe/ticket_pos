import assert from "node:assert/strict";
import test from "node:test";

import { isNavigationSignal, reportCaughtError } from "../src/index.ts";

const ID = "3f6c1a52-8d1e-4b7a-9c39-2a1f0e5d4c3b";

function capturing() {
  const calls: Array<{ error: unknown; requestId: string | null }> = [];
  return {
    calls,
    capture: (error: unknown, requestId: string | null) => {
      calls.push({ error, requestId });
    },
  };
}

test("a crash in the browser is reported once, under the page's request id", () => {
  const { calls, capture } = capturing();
  const error = new Error("render broke");

  assert.equal(reportCaughtError(error, ID, capture), true);
  // A re-render of the screen, or React's strict mode running the effect twice,
  // is the same crash and not a second report.
  assert.equal(reportCaughtError(error, ID, capture), false);

  assert.deepEqual(calls, [{ error, requestId: ID }]);
});

test("a crash with no reference code is still reported", () => {
  const { calls, capture } = capturing();
  const error = new Error("render broke");
  reportCaughtError(error, null, capture);
  assert.deepEqual(calls, [{ error, requestId: null }]);
});

test("a crash on the server is not reported again by the browser", () => {
  // Next gives a server error a digest before it reaches the browser, and
  // onRequestError has already reported it on the server.
  const { calls, capture } = capturing();
  const error = Object.assign(new Error("An error occurred in the Server Components render."), {
    digest: "1234567890",
  });
  assert.equal(reportCaughtError(error, ID, capture), false);
  assert.deepEqual(calls, []);
});

test("a not-found or a redirect is not an error and is not reported", () => {
  const { calls, capture } = capturing();
  for (const digest of [
    "NEXT_HTTP_ERROR_FALLBACK;404",
    "NEXT_NOT_FOUND",
    "NEXT_REDIRECT;replace;/login;307;",
    "BAILOUT_TO_CLIENT_SIDE_RENDERING",
  ]) {
    const signal = Object.assign(new Error(digest), { digest });
    assert.equal(isNavigationSignal(signal), true, digest);
    assert.equal(reportCaughtError(signal, ID, capture), false, digest);
  }
  assert.deepEqual(calls, []);
});

test("an ordinary error, or something thrown that is not an Error, is not a navigation signal", () => {
  assert.equal(isNavigationSignal(new Error("NEXT_REDIRECT")), false);
  assert.equal(isNavigationSignal(Object.assign(new Error("x"), { digest: "98765" })), false);
  assert.equal(isNavigationSignal("NEXT_NOT_FOUND"), false);
  assert.equal(isNavigationSignal(null), false);
});

