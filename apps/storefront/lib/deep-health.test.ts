import assert from "node:assert/strict";
import test, { afterEach, after } from "node:test";

import { GET } from "../app/api/health/route.ts";
import { API_DEEP_HEALTH_PATH, DEEP_HEALTH_PATH, checkDeepHealth } from "./deep-health.ts";
import { serverTracesSampler } from "./sentry.ts";

/**
 * The Storefront's deep health route (app/api/health, #775), the uptime
 * probe's target: in-process, with the API stood in for by fetch. It answers
 * 200 only when the API answers that it reached its database, a 503
 * otherwise, inside a short bound, and never throws, so a failing probe is
 * never an error event: an outage raises the uptime alert, not an issue per
 * probe.
 */

process.env.API_URL = "http://api.internal";

type Answer = () => Promise<Response>;
let answer: Answer = async () => new Response("{}", { status: 200 });
const asked: Array<{ url: string; init: RequestInit }> = [];
const realFetch = globalThis.fetch;
globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
  asked.push({ url: String(input), init: init ?? {} });
  return answer();
}) as typeof fetch;

afterEach(() => {
  asked.length = 0;
  answer = async () => new Response("{}", { status: 200 });
});
after(() => {
  globalThis.fetch = realFetch;
});

function apiAnswers(status: number, code?: string): Answer {
  return async () =>
    new Response(
      JSON.stringify({ data: code ? null : { status: "ok" }, error: code ? { code, message: "m" } : null, request_id: "r" }),
      { status, headers: { "Content-Type": "application/json" } },
    );
}

async function assertUnavailable(response: Response) {
  assert.equal(response.status, 503);
  assert.equal(response.headers.get("Cache-Control"), "no-store");
  assert.equal(await response.text(), "unavailable");
}

test("the route answers 200 with a constant body, never cached, when the API reached its database", async () => {
  answer = apiAnswers(200);

  const response = await GET();

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("Cache-Control"), "no-store");
  assert.equal(await response.text(), "ok");
  assert.equal(asked.length, 1);
  assert.equal(asked[0].url, `http://api.internal${API_DEEP_HEALTH_PATH}`);
  assert.equal(asked[0].init.method ?? "GET", "GET");
  assert.equal(asked[0].init.cache, "no-store");
});

test("the route answers 503 when the API reports its database unreachable", async () => {
  answer = apiAnswers(503, "DATABASE_UNREACHABLE");
  await assertUnavailable(await GET());
});

test("the route answers 503 when the API answers anything but 200", async () => {
  answer = apiAnswers(500, "INTERNAL_ERROR");
  await assertUnavailable(await GET());
  answer = async () => new Response("<html>Service Unavailable</html>", { status: 403 });
  await assertUnavailable(await GET());
});

test("the route answers 503, and does not throw, when the API is unreachable", async () => {
  answer = async () => {
    throw new TypeError("fetch failed", { cause: new Error("connect ECONNREFUSED 10.0.0.2:443") });
  };
  await assertUnavailable(await GET());
});

test("a check answers 503 within its bound when the API never answers, and lets the call go", async () => {
  let signal: AbortSignal | undefined;
  const started = Date.now();

  // The stand-in ignores the signal, as a hung token fetch or socket might.
  const response = await checkDeepHealth((s) => {
    signal = s;
    return new Promise<Response>(() => {});
  }, 50);

  await assertUnavailable(response);
  assert.ok(Date.now() - started < 1_000, `answered after ${Date.now() - started} ms`);
  assert.equal(signal?.aborted, true);
});

test("the route is never traced, whatever the caller decided", () => {
  assert.equal(serverTracesSampler({ name: `GET ${DEEP_HEALTH_PATH}`, inheritOrSampleWith: () => 1 }), 0);
});
