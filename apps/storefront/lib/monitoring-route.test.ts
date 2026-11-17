import assert from "node:assert/strict";
import test, { afterEach } from "node:test";

import { POST } from "../app/api/monitoring/route.ts";

/**
 * The Storefront's tunnel route (app/api/monitoring), end to end in-process: a
 * browser's request in, what would leave for Sentry captured from fetch. The
 * relay's own cases are tested in @ticket-pos/sentry-rules; this pins that the
 * route uses it with this app's DSN and hands it nothing of the browser's but
 * the body. Sentry is never contacted.
 */

const OWN_DSN = "https://publickey@o123.ingest.us.sentry.io/4507";

const sent: Array<{ url: string; init: RequestInit }> = [];
const realFetch = globalThis.fetch;
globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
  sent.push({ url: String(input), init: init ?? {} });
  return new Response("{}", { status: 200 });
}) as typeof fetch;

afterEach(() => {
  sent.length = 0;
  delete process.env.SENTRY_DSN;
});
test.after(() => {
  globalThis.fetch = realFetch;
});

function browserPost(dsn: string): Request {
  const header = JSON.stringify({ event_id: "a".repeat(32), dsn, sent_at: "2026-11-16T00:00:00.000Z" });
  return new Request("https://tickets.example.ec/api/monitoring", {
    method: "POST",
    headers: {
      "Content-Type": "text/plain;charset=UTF-8",
      "X-Forwarded-For": "190.152.1.1",
      "X-Real-IP": "190.152.1.1",
      "User-Agent": "Mozilla/5.0",
      Cookie: "customer_session=secret",
    },
    body: `${header}\n{"type":"event"}\n{"message":"boom"}\n`,
  });
}

test("an envelope for its own project is forwarded with no client address or any other header of the browser's", async () => {
  process.env.SENTRY_DSN = OWN_DSN;

  const response = await POST(browserPost(OWN_DSN));

  assert.equal(response.status, 200);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].url, "https://o123.ingest.us.sentry.io/api/4507/envelope/");
  assert.deepEqual([...new Headers(sent[0].init.headers).keys()], ["content-type"]);
  assert.doesNotMatch(new TextDecoder().decode(sent[0].init.body as Uint8Array), /190\.152\.1\.1|secret/);
});

test("an envelope for another project is refused and nothing is sent", async () => {
  process.env.SENTRY_DSN = OWN_DSN;

  const response = await POST(browserPost("https://publickey@o123.ingest.us.sentry.io/9999"));

  assert.equal(response.status, 403);
  assert.equal(sent.length, 0);
});

test("with no DSN the tunnel answers without forwarding", async () => {
  const response = await POST(browserPost(OWN_DSN));

  assert.equal(response.status, 204);
  assert.equal(sent.length, 0);
});
