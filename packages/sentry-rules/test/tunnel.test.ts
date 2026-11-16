import assert from "node:assert/strict";
import test from "node:test";

import { decideTunnel } from "../src/index.ts";

// An envelope as the browser SDK posts it to a tunnel: a JSON header line that
// names the DSN it was built for, then items, each a header line and a payload.
// https://develop.sentry.dev/sdk/data-model/envelopes/
const OWN_DSN = "https://0a1b2c3d4e5f@o4501234.ingest.us.sentry.io/4507654321";

function envelope(header: Record<string, unknown>): string {
  return [
    JSON.stringify({ event_id: "9ec79c33ec9942ab8353589fcb2e04dc", sent_at: "2026-11-16T12:00:00.000Z", ...header }),
    JSON.stringify({ type: "event" }),
    JSON.stringify({ event_id: "9ec79c33ec9942ab8353589fcb2e04dc", message: "boom" }),
  ].join("\n");
}

test("an envelope for the app's own project is forwarded to that project's ingest endpoint", () => {
  assert.deepEqual(decideTunnel(envelope({ dsn: OWN_DSN }), OWN_DSN), {
    forward: true,
    url: "https://o4501234.ingest.us.sentry.io/api/4507654321/envelope/",
  });
});

test("an envelope arriving as bytes is read the same way", () => {
  const bytes = new TextEncoder().encode(envelope({ dsn: OWN_DSN }) + "\n\u0000ÿ");
  assert.equal(decideTunnel(bytes, OWN_DSN).forward, true);
});

test("the destination comes from the app's DSN, never from the envelope", () => {
  // Same host and project, different scheme and key: still ours, and still
  // sent where the app's own DSN says.
  const decision = decideTunnel(envelope({ dsn: "http://otherkey@O4501234.ingest.us.sentry.io/4507654321" }), OWN_DSN);
  assert.deepEqual(decision, { forward: true, url: "https://o4501234.ingest.us.sentry.io/api/4507654321/envelope/" });
});

test("a DSN with a path prefix keeps it in the destination", () => {
  const selfHosted = "https://key@sentry.example.com/relay/42";
  assert.deepEqual(decideTunnel(envelope({ dsn: selfHosted }), selfHosted), {
    forward: true,
    url: "https://sentry.example.com/relay/api/42/envelope/",
  });
});

test("anything not addressed to the app's own host and project is refused", () => {
  const table: Array<[name: string, body: string | Uint8Array]> = [
    ["another project on the same host", envelope({ dsn: "https://0a1b2c3d4e5f@o4501234.ingest.us.sentry.io/4507654322" })],
    ["the same project on another host", envelope({ dsn: "https://0a1b2c3d4e5f@attacker.example.com/4507654321" })],
    ["a host that only ends like ours", envelope({ dsn: "https://k@evil-o4501234.ingest.us.sentry.io/4507654321" })],
    ["a host that only starts like ours", envelope({ dsn: "https://k@o4501234.ingest.us.sentry.io.evil.com/4507654321" })],
    ["the same host on another port", envelope({ dsn: "https://k@o4501234.ingest.us.sentry.io:8443/4507654321" })],
    ["another path prefix", envelope({ dsn: "https://k@o4501234.ingest.us.sentry.io/x/4507654321" })],
    ["no DSN in the header", envelope({})],
    ["a DSN that is not a string", envelope({ dsn: 42 })],
    ["a DSN that is not a URL", envelope({ dsn: "not a dsn" })],
    ["a DSN with no project", envelope({ dsn: "https://k@o4501234.ingest.us.sentry.io/" })],
    ["a header that is not JSON", "hello\n{}"],
    ["a header that is a JSON array", '["dsn"]\n{}'],
    ["an empty body", ""],
  ];
  for (const [name, body] of table) {
    assert.deepEqual(decideTunnel(body, OWN_DSN), { forward: false }, name);
  }
});

test("with no DSN set the tunnel forwards nothing", () => {
  for (const dsn of [undefined, "", "   "]) {
    assert.deepEqual(decideTunnel(envelope({ dsn: OWN_DSN }), dsn), { forward: false }, JSON.stringify(dsn));
  }
});

test("an app DSN that cannot be read forwards nothing", () => {
  assert.deepEqual(decideTunnel(envelope({ dsn: OWN_DSN }), "ftp://k@host/1"), { forward: false });
});
