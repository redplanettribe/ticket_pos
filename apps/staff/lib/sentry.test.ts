import assert from "node:assert/strict";
import test from "node:test";

import { createBeforeSend, scrubEvent, sentryOptions as sharedSentryOptions } from "@ticket-pos/sentry-rules";

import {
  SENTRY_TRACES_SAMPLE_RATE,
  SENTRY_TUNNEL_PATH,
  beforeSend,
  sentryOptions,
  serverTracesSampler,
} from "./sentry.ts";

const DSN = "https://publickey@o123.ingest.us.sentry.io/4507";

test("every runtime collects no personal data by default and uses the shared before-send", () => {
  const options = sentryOptions({ dsn: DSN, environment: "production" });
  assert.equal(options.sendDefaultPii, false);
  assert.equal(options.beforeSend, beforeSend);
  assert.equal(options.dsn, DSN);
  assert.equal(options.environment, "production");
  // Transactions are scrubbed, never filtered.
  assert.equal(options.beforeSendTransaction, scrubEvent);
  // Streamed spans would skip beforeSendTransaction, and with it the scrubber.
  assert.equal(options.traceLifecycle, "static");
  // No cookies, bodies, query strings, user or frame variables; of the request
  // headers only the User-Agent's value, so no X-Forwarded-For, Referer or
  // Cookie; nor anything an integration added later would collect.
  assert.deepEqual(options.dataCollection, {
    userInfo: false,
    cookies: false,
    httpHeaders: { request: { allow: ["user-agent"] }, response: false },
    httpBodies: [],
    urlQueryParams: false,
    stackFrameVariables: false,
    graphQL: { document: false, variables: false },
    genAI: { inputs: false, outputs: false },
    mcp: { inputs: false, outputs: false },
    databaseQueryData: false,
    queues: false,
  });
  for (const key of ["replaysSessionSampleRate", "replaysOnErrorSampleRate", "profilesSampleRate", "profileSessionSampleRate", "integrations"]) {
    assert.equal(key in options, false, key);
  }
});

test("the Staff app's privacy options are the Storefront's: both come from @ticket-pos/sentry-rules", () => {
  const config = { dsn: DSN, environment: "production", release: "abc123" };
  assert.deepEqual(sentryOptions(config), sharedSentryOptions(config, beforeSend));
});

test("the before-send is the shared one's: an email is redacted, and a server event without a bundle frame is kept", () => {
  const event = {
    message: "could not reach ana@example.com",
    exception: { values: [{ type: "Error", value: "boom", stacktrace: { frames: [{ filename: "/app/.next/server/app/page.js" }] } }] },
  };
  const shared = createBeforeSend({ dropWithoutOwnFrame: false, dropBots: false });

  assert.deepEqual(beforeSend(event), shared(event));
  assert.equal(beforeSend(event)?.message, "could not reach [email]");
});

test("the tunnel's own requests are never traced; the rest keep the browser's decision or sample one in ten", () => {
  const inherit = (fallback: number) => fallback;
  assert.equal(serverTracesSampler({ name: `POST ${SENTRY_TUNNEL_PATH}`, inheritOrSampleWith: inherit }), 0);
  assert.equal(serverTracesSampler({ name: "GET /events/[eventId]", inheritOrSampleWith: inherit }), SENTRY_TRACES_SAMPLE_RATE);
  assert.equal(serverTracesSampler({ name: "GET /", inheritOrSampleWith: () => 1 }), 1);
});
