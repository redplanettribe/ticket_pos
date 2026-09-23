import assert from "node:assert/strict";
import test from "node:test";

import { CLIENT_CLOSED_REQUEST, proxyRead } from "./reader-abort.ts";

// WHAT THESE ASSERT. A BFF route that passes a slow read through to the API
// hands the API the reader's own abort signal, so a reader who leaves stops the
// API's work rather than leaving it running for nobody. The reader leaving is
// then answered quietly, never as an error; every other failure still reaches
// the route's own error answer.

function browserRequest(): { request: Request; leave: () => void } {
  const controller = new AbortController();
  return {
    request: new Request("http://staff.test/api/events/e-1/sales?page=2", { signal: controller.signal }),
    leave: () => controller.abort(),
  };
}

const neverCalled = () => {
  throw new Error("the error answer was used for a reader who left");
};

test("the upstream read is handed the reader's own abort signal", async () => {
  const { request, leave } = browserRequest();
  let seen: AbortSignal | undefined;

  const response = await proxyRead(
    request,
    async (signal) => {
      seen = signal;
      return Response.json({ data: [], error: null, request_id: "req-1" });
    },
    neverCalled,
  );

  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { data: [], error: null, request_id: "req-1" });
  assert.ok(seen, "the read was given no signal");
  assert.equal(seen.aborted, false);
  leave();
  assert.equal(seen.aborted, true, "leaving did not abort the signal the upstream read holds");
});

test("a reader who leaves mid-read stops the upstream read and is answered quietly with 499", async () => {
  const { request, leave } = browserRequest();
  let upstreamStopped = false;

  const pending = proxyRead(
    request,
    (signal) =>
      new Promise<Response>((_resolve, reject) => {
        signal.addEventListener("abort", () => {
          upstreamStopped = true;
          reject(signal.reason);
        });
      }),
    neverCalled,
  );
  leave();
  const response = await pending;

  assert.equal(upstreamStopped, true);
  assert.equal(response.status, CLIENT_CLOSED_REQUEST);
  assert.equal(await response.text(), "");
});

test("a reader already gone before the read starts is answered quietly too", async () => {
  const { request, leave } = browserRequest();
  leave();

  const response = await proxyRead(
    request,
    async (signal) => {
      signal.throwIfAborted();
      return Response.json({});
    },
    neverCalled,
  );

  assert.equal(response.status, CLIENT_CLOSED_REQUEST);
});

test("any other failure, an abort nobody asked for included, is the route's error answer", async () => {
  for (const failure of [new Error("API down"), new DOMException("The operation was aborted.", "AbortError")]) {
    const { request } = browserRequest();
    let handed: unknown;

    const response = await proxyRead(
      request,
      async () => {
        throw failure;
      },
      (error) => {
        handed = error;
        return Response.json({ error: "mapped" }, { status: 500 });
      },
    );

    assert.equal(response.status, 500);
    assert.equal(handed, failure);
  }
});
