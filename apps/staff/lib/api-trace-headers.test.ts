import assert from "node:assert/strict";
import test, { after } from "node:test";

import * as Sentry from "@sentry/nextjs";

import { callBackend, fetchBackendRaw } from "./api.ts";

/**
 * The BFF carries the trace to the API (ADR 0079): with the SDK initialised,
 * a call made inside a span carries that span's sentry-trace and baggage
 * headers beside the request id header, so the API's span joins the trace the
 * browser started.
 *
 * Asserted on what would leave the process: the headers handed to fetch, which
 * is replaced by a capturing stand-in. The SDK is initialised with a DSN on a
 * reserved, unresolvable host and a transport that keeps envelopes in memory,
 * so Sentry is never contacted. node --test runs each file in its own process,
 * so the init here does not reach api-dormant.test.ts.
 */

const sent: Headers[] = [];
const realFetch = globalThis.fetch;
globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
  sent.push(new Headers(init?.headers));
  return new Response(JSON.stringify({ data: {}, error: null, request_id: "r" }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}) as typeof fetch;
after(() => {
  globalThis.fetch = realFetch;
});

Sentry.init({
  dsn: "https://publickey@sentry.invalid/1",
  tracesSampleRate: 1,
  transport: () => ({
    send: async () => ({}),
    flush: async () => true,
  }),
});

for (const [name, call] of [
  ["callBackend", () => callBackend("/api/v1/auth/session", { method: "GET" })],
  ["fetchBackendRaw", () => fetchBackendRaw("/api/v1/imports/template", { method: "GET" })],
] as const) {
  test(`${name} carries the active trace and the request id`, async () => {
    sent.length = 0;
    let traceId = "";
    await Sentry.startSpan({ name: "GET /events" }, async (span) => {
      traceId = span.spanContext().traceId;
      await call();
    });

    assert.equal(sent.length, 1);
    const headers = sent[0];
    assert.match(headers.get("sentry-trace") ?? "", new RegExp(`^${traceId}-[0-9a-f]{16}-1$`));
    assert.match(headers.get("baggage") ?? "", new RegExp(`sentry-trace_id=${traceId}`));
    assert.match(headers.get("x-request-id") ?? "", /^[0-9a-f-]{36}$/);
  });
}

test("a trace header the caller set is kept", async () => {
  sent.length = 0;
  await Sentry.startSpan({ name: "GET /events" }, async () => {
    await callBackend("/api/v1/auth/session", { method: "GET", headers: { "sentry-trace": "given" } });
  });

  assert.equal(sent[0].get("sentry-trace"), "given");
});
