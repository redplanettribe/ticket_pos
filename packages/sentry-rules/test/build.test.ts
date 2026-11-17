import assert from "node:assert/strict";
import test from "node:test";

import { SENTRY_ORG, sentryBuildOptions } from "../src/index.ts";

const TOKEN = "sntrys_not-a-real-token";

test("with no token the build never talks to Sentry: no source maps, no release, no telemetry", () => {
  for (const env of [{}, { SENTRY_AUTH_TOKEN: "" }, { SENTRY_AUTH_TOKEN: "  " }]) {
    const options = sentryBuildOptions("ticket-pos-storefront", env);
    assert.deepEqual(
      options,
      { telemetry: false, silent: true, sourcemaps: { disable: true }, release: { create: false, finalize: false } },
      `env ${JSON.stringify(env)}`,
    );
  }
});

test("with a token the build uploads the app's source maps to its own project and deletes them", () => {
  const options = sentryBuildOptions("ticket-pos-staff", { SENTRY_AUTH_TOKEN: ` ${TOKEN} ` });
  assert.equal(options.authToken, TOKEN);
  assert.equal(options.org, SENTRY_ORG);
  assert.equal(options.project, "ticket-pos-staff");
  assert.equal(options.telemetry, false);
  assert.equal(options.widenClientFileUpload, true);
  assert.deepEqual(options.sourcemaps, { disable: false, deleteSourcemapsAfterUpload: true });
});

test("the build creates no release: the deploy workflow does, once for all three projects", () => {
  const options = sentryBuildOptions("ticket-pos-staff", { SENTRY_AUTH_TOKEN: TOKEN });
  assert.deepEqual(options.release, { create: false, finalize: false });
});

test("a failed upload is a warning, never a failed build", () => {
  const warnings: string[] = [];
  const options = sentryBuildOptions("ticket-pos-storefront", { SENTRY_AUTH_TOKEN: TOKEN }, (line) => warnings.push(line));
  assert.ok(options.errorHandler);
  assert.doesNotThrow(() => options.errorHandler?.(new Error("connect ECONNREFUSED 127.0.0.1:443")));
  assert.equal(warnings.length, 1);
  assert.match(warnings[0], /source map upload to Sentry failed/);
  assert.match(warnings[0], /ECONNREFUSED/);
});
