import assert from "node:assert/strict";
import test from "node:test";

import { REQUEST_ID_COOKIE, REQUEST_ID_HEADER, createOnRequestError, tagRequestId } from "../src/index.ts";

const ID = "3f6c1a52-8d1e-4b7a-9c39-2a1f0e5d4c3b";
/** The page's id, in the cookie its error screen reads (reference-code.ts). */
const PAGE = "9b2e4d71-0c55-4a8e-b1f3-6d7a2c9e0f14";

const context = {
  routerKind: "App Router" as const,
  routePath: "/events/[id]",
  routeType: "render" as const,
  renderSource: "react-server-components" as const,
  revalidateReason: undefined,
};

function harness() {
  const reports: Array<{ error: unknown; path: string }> = [];
  const lines: string[] = [];
  const onRequestError = createOnRequestError({
    report: (error, request) => {
      reports.push({ error, path: request.path });
    },
    log: (line) => lines.push(line),
  });
  return { reports, lines, onRequestError };
}

test("a failed render is reported once and logged under the request's id", async () => {
  const { reports, lines, onRequestError } = harness();
  const error = Object.assign(new Error("someone@example.com broke it"), { digest: "4242" });

  await onRequestError(
    error,
    { path: "/events/abc?email=someone@example.com", method: "GET", headers: { [REQUEST_ID_HEADER]: ID } },
    context,
  );

  assert.deepEqual(reports, [{ error, path: "/events/abc?email=someone@example.com" }]);
  assert.equal(lines.length, 1);
  // One structured line Cloud Logging indexes: the id the screen shows, the
  // digest Next printed beside the error, and the route pattern. Never the
  // error's text or the raw path, which can carry a person.
  assert.deepEqual(JSON.parse(lines[0]), {
    severity: "ERROR",
    message: "request failed",
    request_id: ID,
    reference_code: null,
    digest: "4242",
    route: "/events/[id]",
    route_type: "render",
    render_source: "react-server-components",
  });
  assert.doesNotMatch(lines[0], /example\.com/);
});

test("a client navigation that fails is logged under the reference code its screen shows", async () => {
  // A navigation's RSC request never sets the cookie (#791), so its error
  // screen shows the page's id: the log line carries that too, so the code a
  // person quotes finds this failure, and request_id finds its API calls.
  const { lines, onRequestError } = harness();
  await onRequestError(
    new Error("x"),
    {
      path: "/events/abc",
      method: "GET",
      headers: { [REQUEST_ID_HEADER]: ID, cookie: `NEXT_LOCALE=es; ${REQUEST_ID_COOKIE}=${PAGE}; other=1` },
    },
    context,
  );
  assert.equal(JSON.parse(lines[0]).request_id, ID);
  assert.equal(JSON.parse(lines[0]).reference_code, PAGE);
});

test("a reference code that is not id-shaped is not logged", async () => {
  const { lines, onRequestError } = harness();
  await onRequestError(
    new Error("x"),
    { path: "/", method: "GET", headers: { [REQUEST_ID_HEADER]: ID, cookie: `${REQUEST_ID_COOKIE}=someone@example.com` } },
    context,
  );
  assert.equal(JSON.parse(lines[0]).reference_code, null);
  assert.doesNotMatch(lines[0], /example\.com/);
});

test("a request the middleware never saw is reported and logged with no id", async () => {
  const { reports, lines, onRequestError } = harness();
  const error = new Error("x");

  await onRequestError(error, { path: "/", method: "GET", headers: {} }, context);

  assert.equal(reports.length, 1);
  assert.equal(JSON.parse(lines[0]).request_id, null);
  assert.equal(JSON.parse(lines[0]).digest, null);
});

test("a client component that failed to server-render is logged and left to the browser to report", async () => {
  const { reports, lines, onRequestError } = harness();
  await onRequestError(
    new Error("x"),
    { path: "/", method: "GET", headers: { [REQUEST_ID_HEADER]: ID } },
    { ...context, renderSource: "server-rendering" },
  );
  assert.deepEqual(reports, []);
  assert.equal(JSON.parse(lines[0]).request_id, ID);
  assert.equal(JSON.parse(lines[0]).render_source, "server-rendering");
});

test("a header given twice is logged as its first value", async () => {
  const { lines, onRequestError } = harness();
  await onRequestError(new Error("x"), { path: "/", method: "GET", headers: { [REQUEST_ID_HEADER]: [ID, "other"] } }, context);
  assert.equal(JSON.parse(lines[0]).request_id, ID);
});

test("a server event is tagged with the id of the request it happened in", () => {
  const event = tagRequestId({
    tags: { other: "kept" },
    sdkProcessingMetadata: { normalizedRequest: { headers: { [REQUEST_ID_HEADER]: ID, cookie: "x" } } },
  });
  assert.deepEqual(event.tags, { other: "kept", request_id: ID });
});

test("a server event is tagged with the reference code its screen shows", () => {
  const sent: Parameters<typeof tagRequestId>[0] = {
    sdkProcessingMetadata: {
      normalizedRequest: { headers: { [REQUEST_ID_HEADER]: ID, cookie: `${REQUEST_ID_COOKIE}=${PAGE}` } },
    },
  };
  assert.deepEqual(tagRequestId(sent).tags, { request_id: ID, reference_code: PAGE });
});

test("an event outside a request, or already tagged, is left as it is", () => {
  const outside: { tags?: Record<string, unknown> } = {};
  assert.equal(tagRequestId(outside).tags, undefined);
  const noId: Parameters<typeof tagRequestId>[0] = { sdkProcessingMetadata: { normalizedRequest: { headers: {} } } };
  assert.equal(tagRequestId(noId).tags, undefined);
  const tagged = tagRequestId({
    tags: { request_id: "set-by-caller" },
    sdkProcessingMetadata: { normalizedRequest: { headers: { [REQUEST_ID_HEADER]: ID } } },
  });
  assert.equal(tagged.tags?.request_id, "set-by-caller");
});
