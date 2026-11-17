import assert from "node:assert/strict";
import test from "node:test";

import {
  REFERENCE_CODE_HEADER,
  REQUEST_ID_COOKIE,
  REQUEST_ID_HEADER,
  createOnRequestError,
  forwardedRequestHeaders,
  tagRequestId,
} from "../src/index.ts";

const ID = "3f6c1a52-8d1e-4b7a-9c39-2a1f0e5d4c3b";
const PAGE_ID = "9a1d2c3b-4e5f-4a6b-8c7d-0e1f2a3b4c5d";

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
    {
      path: "/events/abc?email=someone@example.com",
      method: "GET",
      headers: { [REQUEST_ID_HEADER]: ID, [REFERENCE_CODE_HEADER]: ID },
    },
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
    reference_code: ID,
    digest: "4242",
    route: "/events/[id]",
    route_type: "render",
    render_source: "react-server-components",
  });
  assert.doesNotMatch(lines[0], /example\.com/);
});

test("a request the middleware never saw is reported and logged with no id", async () => {
  const { reports, lines, onRequestError } = harness();
  const error = new Error("x");

  await onRequestError(error, { path: "/", method: "GET", headers: {} }, context);

  assert.equal(reports.length, 1);
  assert.equal(JSON.parse(lines[0]).request_id, null);
  assert.equal(JSON.parse(lines[0]).reference_code, null);
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

/** Next's onRequestError headers: a plain object of what the render was given. */
function renderHeaders(headers: Headers): Record<string, string> {
  return Object.fromEntries(headers.entries());
}

test("a failed client navigation is logged and tagged with the code its error screen will show", async () => {
  // What the middleware is given for a client navigation: a same-origin fetch,
  // Next's flight headers already gone, carrying the page's cookie.
  const navigation = new Headers({
    accept: "*/*",
    "sec-fetch-dest": "empty",
    "sec-fetch-mode": "cors",
    "sec-fetch-site": "same-origin",
    cookie: `${REQUEST_ID_COOKIE}=${PAGE_ID}`,
  });
  const headers = renderHeaders(forwardedRequestHeaders({ pathname: "/events/abc", headers: navigation }, ID));
  const { reports, lines, onRequestError } = harness();

  await onRequestError(new Error("x"), { path: "/events/abc", method: "GET", headers }, context);

  assert.equal(reports.length, 1);
  // Its own id finds its API calls; the page's code is what the screen shows.
  assert.equal(JSON.parse(lines[0]).request_id, ID);
  assert.equal(JSON.parse(lines[0]).reference_code, PAGE_ID);
  // The cookie the screen reads is still the page's: the navigation did not write it.
  const event: Parameters<typeof tagRequestId>[0] = { sdkProcessingMetadata: { normalizedRequest: { headers } } };
  assert.deepEqual(tagRequestId(event).tags, { request_id: ID, reference_code: PAGE_ID });
});

test("a reference code not of our shape is neither logged nor tagged", async () => {
  const { lines, onRequestError } = harness();
  const headers = { [REQUEST_ID_HEADER]: ID, [REFERENCE_CODE_HEADER]: "[Filtered]" };
  await onRequestError(new Error("x"), { path: "/", method: "GET", headers }, context);
  assert.equal(JSON.parse(lines[0]).reference_code, null);
  const event: Parameters<typeof tagRequestId>[0] = { sdkProcessingMetadata: { normalizedRequest: { headers } } };
  assert.deepEqual(tagRequestId(event).tags, { request_id: ID });
});

test("a header given twice is logged as its first value", async () => {
  const { lines, onRequestError } = harness();
  await onRequestError(new Error("x"), { path: "/", method: "GET", headers: { [REQUEST_ID_HEADER]: [ID, "other"] } }, context);
  assert.equal(JSON.parse(lines[0]).request_id, ID);
});

test("a server event is tagged with the id of the request it happened in", () => {
  const event = tagRequestId({
    tags: { other: "kept" },
    sdkProcessingMetadata: {
      normalizedRequest: { headers: { [REQUEST_ID_HEADER]: ID, [REFERENCE_CODE_HEADER]: ID, cookie: "x" } },
    },
  });
  assert.deepEqual(event.tags, { other: "kept", request_id: ID, reference_code: ID });
});

test("an event outside a request, or already tagged, is left as it is", () => {
  const outside: { tags?: Record<string, unknown> } = {};
  assert.equal(tagRequestId(outside).tags, undefined);
  const noId: Parameters<typeof tagRequestId>[0] = { sdkProcessingMetadata: { normalizedRequest: { headers: {} } } };
  assert.equal(tagRequestId(noId).tags, undefined);
  const tagged = tagRequestId({
    tags: { request_id: "set-by-caller", reference_code: "set-by-caller" },
    sdkProcessingMetadata: { normalizedRequest: { headers: { [REQUEST_ID_HEADER]: ID, [REFERENCE_CODE_HEADER]: ID } } },
  });
  assert.equal(tagged.tags?.request_id, "set-by-caller");
  assert.equal(tagged.tags?.reference_code, "set-by-caller");
});
