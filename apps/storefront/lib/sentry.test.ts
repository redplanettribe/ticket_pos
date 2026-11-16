import assert from "node:assert/strict";
import test from "node:test";

import {
  SENTRY_TRACES_SAMPLE_RATE,
  SENTRY_TUNNEL_PATH,
  beforeSendTransaction,
  browserBeforeSend,
  sentryOptions,
  serverBeforeSend,
  serverTracesSampler,
} from "./sentry.ts";

const DSN = "https://publickey@o123.ingest.us.sentry.io/4507";
const PAGE = "https://tickets.example.ec/es/acme/events/fiesta";
const PERSON = "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Mobile Safari/537.36";
const CRAWLER = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)";

function browserError(frames: string[], userAgent = PERSON) {
  return {
    message: "checkout failed for ana@example.com",
    request: { url: PAGE, headers: { "User-Agent": userAgent } },
    exception: { values: [{ type: "TypeError", value: "x is undefined", stacktrace: { frames: frames.map((filename) => ({ filename })) } }] },
  };
}

test("every runtime collects no personal data by default, and enables neither replay nor profiling", () => {
  const options = sentryOptions({ dsn: DSN, environment: "production", release: "abc123" }, browserBeforeSend);
  assert.equal(options.sendDefaultPii, false);
  assert.equal(options.dsn, DSN);
  assert.equal(options.environment, "production");
  assert.equal(options.release, "abc123");
  assert.equal(options.beforeSend, browserBeforeSend);
  assert.equal(options.beforeSendTransaction, beforeSendTransaction);
  // Streamed spans would skip beforeSendTransaction, and with it the scrubber.
  assert.equal(options.traceLifecycle, "static");
  // Of the request, only the User-Agent the bot filter reads.
  assert.deepEqual(options.dataCollection, {
    userInfo: false,
    cookies: false,
    httpHeaders: { request: { allow: ["user-agent"] }, response: false },
    httpBodies: [],
    urlQueryParams: false,
    stackFrameVariables: false,
  });
  for (const key of ["replaysSessionSampleRate", "replaysOnErrorSampleRate", "profilesSampleRate", "profileSessionSampleRate", "integrations"]) {
    assert.equal(key in options, false, key);
  }
});

test("in the browser, an event with no frame from our own bundles is dropped", () => {
  assert.equal(browserBeforeSend(browserError(["chrome-extension://abc/content.js", "https://www.googletagmanager.com/gtm.js"])), null);
  assert.equal(browserBeforeSend(browserError([])), null);
});

test("in the browser, an event with a frame from our own bundles is sent, scrubbed", () => {
  const sent = browserBeforeSend(browserError(["chrome-extension://abc/content.js", "app:///_next/static/chunks/app/page-1a2b.js"]));
  assert.ok(sent);
  assert.equal(sent.message, "checkout failed for [email]");
  assert.equal(sent.request?.headers, undefined);
});

test("an event from a known bot user agent is dropped, in the browser and on the server", () => {
  assert.equal(browserBeforeSend(browserError(["app:///_next/static/chunks/app/page-1a2b.js"], CRAWLER)), null);
  const serverEvent = { request: { url: PAGE, headers: { "user-agent": CRAWLER } }, exception: { values: [{ type: "Error", value: "boom" }] } };
  assert.equal(serverBeforeSend(serverEvent), null);
});

test("on the server, an error without a bundle frame is sent: no server frame lives under /_next/static/", () => {
  const sent = serverBeforeSend({
    request: { url: PAGE, headers: { "user-agent": PERSON } },
    exception: { values: [{ type: "Error", value: "boom", stacktrace: { frames: [{ filename: "/app/apps/storefront/.next/server/app/page.js" }] } }] },
  });
  assert.ok(sent);
});

test("the tunnel's own requests are never traced; the rest keep the browser's decision or sample one in ten", () => {
  const inherit = (fallback: number) => fallback;
  assert.equal(serverTracesSampler({ name: `POST ${SENTRY_TUNNEL_PATH}`, inheritOrSampleWith: inherit }), 0);
  assert.equal(serverTracesSampler({ name: "GET /[locale]", inheritOrSampleWith: inherit }), SENTRY_TRACES_SAMPLE_RATE);
  assert.equal(serverTracesSampler({ name: "GET /[locale]", inheritOrSampleWith: () => 1 }), 1);
});
