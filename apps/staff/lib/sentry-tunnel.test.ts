import assert from "node:assert/strict";
import test from "node:test";

import { relayEnvelope, type Send } from "./sentry-tunnel.ts";

/**
 * The tunnel route's relay (ADR 0079), with a capturing stand-in for fetch:
 * what would leave the process is what `send` was handed. Sentry is never
 * contacted.
 */

const OWN_DSN = "https://publickey@o123.ingest.us.sentry.io/4507";

function envelopeFor(dsn: string): Uint8Array<ArrayBuffer> {
  const header = JSON.stringify({ event_id: "a".repeat(32), dsn, sent_at: "2026-11-16T00:00:00.000Z" });
  const item = JSON.stringify({ type: "event" });
  const payload = JSON.stringify({ message: "boom" });
  return new TextEncoder().encode(`${header}\n${item}\n${payload}\n`);
}

function capturing(answer: () => Promise<Response> | Response = () => new Response("{}", { status: 200 })) {
  const calls: Array<{ url: string; init: RequestInit }> = [];
  const send: Send = async (url, init) => {
    calls.push({ url, init });
    return answer();
  };
  return { calls, send };
}

test("an envelope for this app's own project is forwarded to its envelope endpoint", async () => {
  const { calls, send } = capturing();
  const envelope = envelopeFor(OWN_DSN);

  const response = await relayEnvelope(envelope, OWN_DSN, send);

  assert.equal(response.status, 200);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "https://o123.ingest.us.sentry.io/api/4507/envelope/");
  assert.equal(calls[0].init.method, "POST");
  assert.deepEqual(calls[0].init.body, envelope);
});

test("the forwarded request carries no client address or any other header of the browser's", async () => {
  const { calls, send } = capturing();

  await relayEnvelope(envelopeFor(OWN_DSN), OWN_DSN, send);

  const headers = new Headers(calls[0].init.headers);
  assert.deepEqual([...headers.keys()], ["content-type"]);
  assert.equal(headers.get("content-type"), "application/x-sentry-envelope");
});

test("an envelope for another project is refused and nothing is sent", async () => {
  const { calls, send } = capturing();

  const response = await relayEnvelope(envelopeFor("https://publickey@o123.ingest.us.sentry.io/9999"), OWN_DSN, send);

  assert.equal(response.status, 403);
  assert.equal(calls.length, 0);
});

test("an envelope for another host is refused and nothing is sent", async () => {
  const { calls, send } = capturing();

  const response = await relayEnvelope(envelopeFor("https://publickey@attacker.example/4507"), OWN_DSN, send);

  assert.equal(response.status, 403);
  assert.equal(calls.length, 0);
});

test("an unreadable envelope is refused and nothing is sent", async () => {
  const { calls, send } = capturing();

  const response = await relayEnvelope(new TextEncoder().encode("not an envelope"), OWN_DSN, send);

  assert.equal(response.status, 403);
  assert.equal(calls.length, 0);
});

test("with no DSN the tunnel answers without forwarding", async () => {
  for (const dsn of [undefined, "", "   "]) {
    const { calls, send } = capturing();

    const response = await relayEnvelope(envelopeFor(OWN_DSN), dsn, send);

    assert.equal(response.status, 204, `dsn ${JSON.stringify(dsn)}`);
    assert.equal(calls.length, 0);
  }
});

test("Sentry's status and rate limits are passed back, and nothing else of its answer", async () => {
  const { send } = capturing(
    () =>
      new Response('{"detail":"rate limited"}', {
        status: 429,
        headers: { "Retry-After": "60", "X-Sentry-Rate-Limits": "60:error:organization", "Set-Cookie": "a=b" },
      }),
  );

  const response = await relayEnvelope(envelopeFor(OWN_DSN), OWN_DSN, send);

  assert.equal(response.status, 429);
  assert.equal(response.headers.get("retry-after"), "60");
  assert.equal(response.headers.get("x-sentry-rate-limits"), "60:error:organization");
  assert.equal(response.headers.get("set-cookie"), null);
  assert.equal(await response.text(), "");
});

test("an unreachable Sentry is answered 502, and a slow one 504, never thrown", async () => {
  const unreachable = await relayEnvelope(envelopeFor(OWN_DSN), OWN_DSN, async () => {
    throw new TypeError("fetch failed");
  });
  assert.equal(unreachable.status, 502);

  const slow = await relayEnvelope(envelopeFor(OWN_DSN), OWN_DSN, async () => {
    throw new DOMException("The operation was aborted due to timeout", "TimeoutError");
  });
  assert.equal(slow.status, 504);
});

test("the upstream request is bounded by a timeout", async () => {
  const { calls, send } = capturing();

  await relayEnvelope(envelopeFor(OWN_DSN), OWN_DSN, send);

  assert.ok(calls[0].init.signal instanceof AbortSignal);
});
