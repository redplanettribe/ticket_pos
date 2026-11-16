import assert from "node:assert/strict";
import test, { after } from "node:test";

import { callBackend } from "./api.ts";

/**
 * With the SDK not initialised (no DSN, the default), BFF calls carry the
 * request id header exactly as before and no trace headers.
 */

const sent: Headers[] = [];
const realFetch = globalThis.fetch;
globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
  sent.push(new Headers(init?.headers));
  return new Response(JSON.stringify({ data: {}, error: null, request_id: "r" }), { status: 200 });
}) as typeof fetch;
after(() => {
  globalThis.fetch = realFetch;
});

test("a dormant SDK adds no trace headers, and the request id is still carried", async () => {
  await callBackend("/api/v1/auth/session", { method: "GET" });

  assert.equal(sent.length, 1);
  assert.equal(sent[0].get("sentry-trace"), null);
  assert.equal(sent[0].get("baggage"), null);
  assert.match(sent[0].get("x-request-id") ?? "", /^[0-9a-f-]{36}$/);
});
