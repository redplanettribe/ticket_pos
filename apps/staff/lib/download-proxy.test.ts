import assert from "node:assert/strict";
import test from "node:test";

import { forwardDownload, proxyDownload, XLSX_CONTENT_TYPE } from "./download-proxy.ts";

// WHAT THESE ASSERT. The export proxies hand the API's answer to the browser
// with the headers the reader needs: on a refusal the envelope, its status and
// the API's Retry-After and Cache-Control (a busy export says when to come
// back); on a file the same headers plus the file's own. Either body is a
// stream that is never buffered. A reader who goes away stops the upstream
// work, before the file starts and while it flows, and the route then returns
// quietly instead of failing.

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

  const response = forwardDownload(upstream, XLSX_CONTENT_TYPE);

  assert.equal(response.status, 503);
  assert.equal(await response.text(), envelope);
  assert.equal(response.headers.get("Content-Type"), "application/json");
  assert.equal(response.headers.get("Retry-After"), "30");
  assert.equal(response.headers.get("Cache-Control"), "no-store");
});

test("a refusal without a Content-Type is still read as JSON, and adds no headers the API did not send", async () => {
  const upstream = new Response("{}", { status: 404 });
  upstream.headers.delete("Content-Type");

  const response = forwardDownload(upstream, XLSX_CONTENT_TYPE);

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

  const response = forwardDownload(upstream, "application/octet-stream");

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

  const response = forwardDownload(upstream, XLSX_CONTENT_TYPE);

  assert.equal(response.headers.get("Content-Type"), XLSX_CONTENT_TYPE);
});

// A controllable upstream body: bytes are pushed by the test, and nothing closes
// it unless the test says so.
function controlledUpstream(status = 200, headers: HeadersInit = { "Content-Type": XLSX_CONTENT_TYPE }) {
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
  const response = new Response(body, { status, headers });
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
  const response = forwardDownload(upstream.response, XLSX_CONTENT_TYPE);
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

// A refusal is streamed as a file is, not read into memory first, so the two
// relays (this and the storefront's forwardDocument) handle it the same way.
test("a refusal streams too: its first bytes reach the reader before the API has finished", async () => {
  const upstream = controlledUpstream(503, { "Retry-After": "30" });
  const response = forwardDownload(upstream.response, XLSX_CONTENT_TYPE);

  assert.equal(response.status, 503);
  assert.equal(response.headers.get("Content-Type"), "application/json");
  assert.equal(response.headers.get("Retry-After"), "30");
  const reader = response.body!.getReader();
  upstream.push('{"data":null,');
  const first = await reader.read();
  assert.equal(decoder.decode(first.value), '{"data":null,');

  upstream.close();
  assert.equal((await reader.read()).done, true);
});

test("a download the API aborts part way fails for the reader instead of ending cleanly", async () => {
  const upstream = controlledUpstream();
  const response = forwardDownload(upstream.response, XLSX_CONTENT_TYPE);
  const reader = response.body!.getReader();

  upstream.push("PK partial");
  await reader.read();
  upstream.fail(new Error("upstream connection reset"));

  await assert.rejects(reader.read(), /upstream connection reset/);
});

test("a reader who goes away while the file flows cancels the upstream body", async () => {
  const upstream = controlledUpstream();
  const response = forwardDownload(upstream.response, XLSX_CONTENT_TYPE);
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
  // Nobody is left to read the answer, so the route returns quietly: no
  // rejection for Next to log as an unhandled route error and a 500.
  const response = await proxied;
  assert.equal(response.body, null);
  assert.equal(response.status, 499);
});

test("an upstream fetch rejected because the browser gave up resolves quietly, with nothing logged", async () => {
  const browser = new AbortController();
  const request = new Request("http://staff.example/api/operator/invoicing/archive", { signal: browser.signal });
  browser.abort();
  const logged: unknown[] = [];
  const originalError = console.error;
  console.error = (...args: unknown[]) => {
    logged.push(args);
  };
  try {
    const response = await proxyDownload(
      request,
      () => Promise.reject(new DOMException("The operation was aborted.", "AbortError")),
      "application/zip",
    );

    assert.equal(response.status, 499);
    assert.equal(response.body, null);
  } finally {
    console.error = originalError;
  }
  assert.deepEqual(logged, []);
});

// Quiet is for a reader who is gone. An AbortError while the browser is still
// waiting is somebody else's abort, and hiding it would hand that browser an
// empty answer.
test("an AbortError while the browser is still waiting still fails", async () => {
  const request = new Request("http://staff.example/api/events/e1/sales/export");

  await assert.rejects(
    proxyDownload(
      request,
      () => Promise.reject(new DOMException("The operation was aborted.", "AbortError")),
      XLSX_CONTENT_TYPE,
    ),
    /aborted/,
  );
});

test("an upstream fetch that fails for any other reason still fails", async () => {
  const request = new Request("http://staff.example/api/events/e1/sales/export");

  await assert.rejects(
    proxyDownload(request, () => Promise.reject(new TypeError("fetch failed")), XLSX_CONTENT_TYPE),
    /fetch failed/,
  );
});
