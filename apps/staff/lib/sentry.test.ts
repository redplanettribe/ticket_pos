import assert from "node:assert/strict";
import test from "node:test";

import { createBeforeSend } from "@ticket-pos/sentry-rules";

import {
  SENTRY_TRACES_SAMPLE_RATE,
  SENTRY_TUNNEL_PATH,
  beforeSend,
  parseSentryConfig,
  readSentryConfig,
  sentryOptions,
  serializeSentryConfig,
  serverTracesSampler,
} from "./sentry.ts";

const DSN = "https://publickey@o123.ingest.us.sentry.io/4507";

test("no DSN is no configuration, so nothing initialises the SDK", () => {
  assert.equal(readSentryConfig({}), null);
  assert.equal(readSentryConfig({ SENTRY_DSN: "" }), null);
  assert.equal(readSentryConfig({ SENTRY_DSN: "  ", SENTRY_RELEASE: "abc123", SENTRY_ENVIRONMENT: "production" }), null);
});

test("a DSN reports as development unless the environment says otherwise", () => {
  assert.deepEqual(readSentryConfig({ SENTRY_DSN: DSN }), { dsn: DSN, environment: "development" });
  assert.deepEqual(readSentryConfig({ SENTRY_DSN: DSN, SENTRY_ENVIRONMENT: "production" }), {
    dsn: DSN,
    environment: "production",
  });
});

test("the release is the build's commit, and an absent or blank one is harmless", () => {
  assert.equal(readSentryConfig({ SENTRY_DSN: DSN, SENTRY_RELEASE: "3514342f" })?.release, "3514342f");
  assert.equal(readSentryConfig({ SENTRY_DSN: DSN, SENTRY_RELEASE: "" })?.release, undefined);
  assert.equal("release" in (readSentryConfig({ SENTRY_DSN: DSN, SENTRY_RELEASE: "" }) ?? {}), false);
});

test("the browser reads back what the layout wrote", () => {
  const config = { dsn: DSN, environment: "production", release: "3514342f" };
  assert.deepEqual(parseSentryConfig(serializeSentryConfig(config)), config);
});

test("the browser initialises nothing from a missing or unreadable meta", () => {
  for (const content of [null, undefined, "", "not json", "null", "[]", '{"dsn":""}', '{"dsn":42}', '{"environment":"production"}']) {
    assert.equal(parseSentryConfig(content), null, `content ${JSON.stringify(content)}`);
  }
});

test("every runtime collects no personal data by default and uses the shared before-send", () => {
  const options = sentryOptions({ dsn: DSN, environment: "production" });
  assert.equal(options.sendDefaultPii, false);
  assert.equal(options.beforeSend, beforeSend);
  assert.equal(options.dsn, DSN);
  assert.equal(options.environment, "production");
  assert.equal("replaysSessionSampleRate" in options, false);
  assert.equal("profilesSampleRate" in options, false);
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
