import assert from "node:assert/strict";
import { afterEach, test } from "node:test";

import { callBackend } from "./api.ts";

// WHAT THESE ASSERT. callBackend is what every JSON BFF route reaches the API
// through, so the reader's abort signal a route hands it (see proxyRead) must
// arrive on the API request itself, beside the session, or a reader who leaves
// cannot stop the work.

const realFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = realFetch;
});

test("the abort signal a route passes reaches the API request, beside the session", async () => {
  let seen: RequestInit | undefined;
  globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
    seen = init;
    return Response.json({ data: { ok: true }, error: null, request_id: "req-1" });
  }) as typeof fetch;
  const reader = new AbortController();

  const envelope = await callBackend<{ ok: boolean }>("/api/v1/staff/events/e-1/sales", {
    method: "GET",
    sessionToken: "session-1",
    signal: reader.signal,
  });

  assert.deepEqual(envelope.data, { ok: true });
  assert.equal(seen?.signal, reader.signal);
  assert.equal(new Headers(seen?.headers).get("Authorization"), "Bearer session-1");
});

test("a reader who leaves mid-request aborts the API request", async () => {
  globalThis.fetch = ((_input: RequestInfo | URL, init?: RequestInit) =>
    new Promise<Response>((_resolve, reject) => {
      // As the real fetch does: an already-aborted signal rejects at once, and
      // a later abort rejects the request in flight.
      const signal = init?.signal;
      if (signal?.aborted) {
        reject(signal.reason);
        return;
      }
      signal?.addEventListener("abort", () => reject(signal.reason));
    })) as typeof fetch;
  const reader = new AbortController();

  const pending = callBackend("/api/v1/staff/events/e-1/holder-list", {
    method: "GET",
    sessionToken: "session-1",
    signal: reader.signal,
  });
  reader.abort();

  await assert.rejects(pending, { name: "AbortError" });
});
