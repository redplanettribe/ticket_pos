import assert from "node:assert/strict";
import test from "node:test";

import { parseSentryConfig, readSentryConfig, serializeSentryConfig } from "../src/index.ts";

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

test("the browser initialises nothing from a missing or unreadable configuration", () => {
  for (const content of [null, undefined, "", "not json", "null", "[]", '{"dsn":""}', '{"dsn":42}', '{"environment":"production"}']) {
    assert.equal(parseSentryConfig(content), null, `content ${JSON.stringify(content)}`);
  }
});
