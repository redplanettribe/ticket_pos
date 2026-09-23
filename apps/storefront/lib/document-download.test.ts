import assert from "node:assert/strict";
import test from "node:test";

// The ".ts" is written out because these tests run the modules directly under
// `node --experimental-strip-types`, which resolves specifiers exactly.
import { forwardDocument } from "./document-download.ts";

// WHAT THESE ASSERT. The buyer's Tax Document downloads hand the API's answer
// to the browser with the headers the reader needs. The API marks both files
// `Cache-Control: no-store` so a buyer's invoice never sits in a shared cache,
// and a relay that drops that header quietly undoes it. A refusal is the API's
// JSON envelope with its status; a file is its bytes, streamed.

test("a file keeps its type, its filename and Cache-Control, and nothing else", async () => {
  const upstream = new Response("<factura/>", {
    status: 200,
    headers: {
      "Content-Type": "application/xml",
      "Content-Disposition": 'attachment; filename="clave.xml"',
      "Cache-Control": "no-store",
      "Retry-After": "5",
      "X-Internal-Only": "not for the browser",
    },
  });

  const response = forwardDocument(upstream, "application/pdf");

  assert.equal(response.status, 200);
  assert.equal(await response.text(), "<factura/>");
  assert.equal(response.headers.get("Content-Type"), "application/xml");
  assert.equal(response.headers.get("Content-Disposition"), 'attachment; filename="clave.xml"');
  assert.equal(response.headers.get("Cache-Control"), "no-store");
  assert.equal(response.headers.get("Retry-After"), "5");
  assert.equal(response.headers.get("X-Internal-Only"), null);
});

test("a file without a Content-Type falls back to the file's default", () => {
  const upstream = new Response("%PDF", { status: 200 });
  upstream.headers.delete("Content-Type");

  const response = forwardDocument(upstream, "application/pdf");

  assert.equal(response.headers.get("Content-Type"), "application/pdf");
  assert.equal(response.headers.get("Cache-Control"), null);
});

test("a refusal passes the envelope, its status and Cache-Control through", async () => {
  const envelope = JSON.stringify({ data: null, error: { code: "NOT_FOUND", message: "x" }, request_id: "r" });
  const upstream = new Response(envelope, {
    status: 404,
    headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
  });

  const response = forwardDocument(upstream, "application/xml");

  assert.equal(response.status, 404);
  assert.equal(await response.text(), envelope);
  assert.equal(response.headers.get("Content-Type"), "application/json");
  assert.equal(response.headers.get("Cache-Control"), "no-store");
  assert.equal(response.headers.get("Content-Disposition"), null);
});

test("a refusal without a Content-Type is read as JSON", () => {
  const upstream = new Response("{}", { status: 404 });
  upstream.headers.delete("Content-Type");

  const response = forwardDocument(upstream, "application/xml");

  assert.equal(response.headers.get("Content-Type"), "application/json");
});

test("a file streams: the first bytes reach the reader before the API has finished", async () => {
  const encoder = new TextEncoder();
  const decoder = new TextDecoder();
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      controller = c;
    },
  });
  const response = forwardDocument(new Response(body, { status: 200 }), "application/pdf");
  assert.ok(response.body);
  const reader = response.body.getReader();

  controller.enqueue(encoder.encode("%PDF first"));
  const first = await reader.read();
  assert.equal(decoder.decode(first.value), "%PDF first");

  controller.close();
  assert.equal((await reader.read()).done, true);
});
