import assert from "node:assert/strict";
import test from "node:test";

import { forwardDownload, proxyDownload, XLSX_CONTENT_TYPE } from "./download-proxy.ts";

// WHAT THESE ASSERT. The export proxies hand the API's answer to the browser
// with the headers the reader needs: on a refusal the envelope, its status and
// the API's Retry-After and Cache-Control (a busy export says when to come
// back); on a file the same headers plus the file's own, and the body as a
// stream that is never buffered. A reader who goes away stops the upstream
// work, before the file starts and while it flows.

const encoder = new TextEncoder();
const decoder = new TextDecoder();

test("a refusal passes the envelope, its status, Retry-After and Cache-Control through", async () => {
  const envelope = JSON.stringify({
    data: null,
    error: { code: "HOLDER_EXPORT_BUSY", message: "Try again shortly" },
    request_id: "req-1",
  });
  const upstream = new Response(envelope, {
    status: 503,
    headers: { "Content-Type": "application/json", "Retry-After": "30", "Cache-Control": "no-store" },
  });

  const response = await forwardDownload(upstream, XLSX_CONTENT_TYPE);

  assert.equal(response.status, 503);
  assert.equal(await response.text(), envelope);
  assert.equal(response.headers.get("Content-Type"), "application/json");
  assert.equal(response.headers.get("Retry-After"), "30");
  assert.equal(response.headers.get("Cache-Control"), "no-store");
});

test("a refusal without a Content-Type is still read as JSON, and adds no headers the API did not send", async () => {
  const upstream = new Response("{}", { status: 404 });
  upstream.headers.delete("Content-Type");

  const response = await forwardDownload(upstream, XLSX_CONTENT_TYPE);

  assert.equal(response.status, 404);
  assert.equal(response.headers.get("Content-Type"), "application/json");
  assert.equal(response.headers.get("Retry-After"), null);
  assert.equal(response.headers.get("Cache-Control"), null);
});

test("a file keeps its type, its filename, Cache-Control and Retry-After", async () => {
  const upstream = new Response("PK", {
    status: 200,
    headers: {
      "Content-Type": XLSX_CONTENT_TYPE,
      "Content-Disposition": 'attachment; filename="holders.xlsx"',
      "Cache-Control": "private, no-store",
      "Retry-After": "5",
      "X-Internal-Only": "not for the browser",
    },
  });

  const response = await forwardDownload(upstream, "application/octet-stream");

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("Content-Type"), XLSX_CONTENT_TYPE);
  assert.equal(response.headers.get("Content-Disposition"), 'attachment; filename="holders.xlsx"');
  assert.equal(response.headers.get("Cache-Control"), "private, no-store");
  assert.equal(response.headers.get("Retry-After"), "5");
  assert.equal(response.headers.get("X-Internal-Only"), null);
});

test("a file without a Content-Type falls back to the one the route names", async () => {
  const upstream = new Response(new Uint8Array([1]), { status: 200 });
  upstream.headers.delete("Content-Type");

  const response = await forwardDownload(upstream, XLSX_CONTENT_TYPE);

  assert.equal(response.headers.get("Content-Type"), XLSX_CONTENT_TYPE);
});

// A controllable upstream body: bytes are pushed by the test, and nothing closes
// it unless the test says so.
function controlledUpstream() {
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  let cancelled: unknown = undefined;
  let wasCancelled = false;
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      controller = c;
    },
    cancel(reason) {
      wasCancelled = true;
      cancelled = reason;
    },
  });
  const response = new Response(body, { status: 200, headers: { "Content-Type": XLSX_CONTENT_TYPE } });
  return {
    response,
    push: (text: string) => controller.enqueue(encoder.encode(text)),
    fail: (error: Error) => controller.error(error),
    close: () => controller.close(),
    cancellation: () => ({ wasCancelled, reason: cancelled }),
  };
}

test("a file streams: the first bytes reach the reader before the API has finished", async () => {
  const upstream = controlledUpstream();
  const response = await forwardDownload(upstream.response, XLSX_CONTENT_TYPE);
  const reader = response.body!.getReader();

  upstream.push("PK first batch");
  const first = await reader.read();

  assert.equal(first.done, false);
  assert.equal(decoder.decode(first.value), "PK first batch");

  upstream.push(" second batch");
  upstream.close();
  const second = await reader.read();
  assert.equal(decoder.decode(second.value), " second batch");
  assert.equal((await reader.read()).done, true);
});

test("a download the API aborts part way fails for the reader instead of ending cleanly", async () => {
  const upstream = controlledUpstream();
  const response = await forwardDownload(upstream.response, XLSX_CONTENT_TYPE);
  const reader = response.body!.getReader();

  upstream.push("PK partial");
  await reader.read();
  upstream.fail(new Error("upstream connection reset"));

  await assert.rejects(reader.read(), /upstream connection reset/);
});

test("a reader who goes away while the file flows cancels the upstream body", async () => {
  const upstream = controlledUpstream();
  const response = await forwardDownload(upstream.response, XLSX_CONTENT_TYPE);
  const reader = response.body!.getReader();

  upstream.push("PK partial");
  await reader.read();
  await reader.cancel("client went away");

  assert.deepEqual(upstream.cancellation(), { wasCancelled: true, reason: "client went away" });
});

test("a reader who goes away before the file starts aborts the upstream fetch", async () => {
  const client = new AbortController();
  const request = new Request("http://staff.example/api/events/e1/holder-list/export", { signal: client.signal });
  let upstreamSignal: AbortSignal | undefined;
  let upstreamAborted!: () => void;
  const aborted = new Promise<void>((resolve) => {
    upstreamAborted = resolve;
  });

  const proxied = proxyDownload(
    request,
    (signal) => {
      upstreamSignal = signal;
      // The API is still preparing the file: this fetch settles only when it
      // is aborted, as a real one does.
      return new Promise<Response>((_, reject) => {
        signal.addEventListener("abort", () => {
          upstreamAborted();
          reject(signal.reason);
        });
      });
    },
    XLSX_CONTENT_TYPE,
  );

  assert.equal(upstreamSignal?.aborted, false);
  client.abort();
  await aborted;

  assert.equal(upstreamSignal?.aborted, true);
  await assert.rejects(proxied);
});
