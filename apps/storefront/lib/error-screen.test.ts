import assert from "node:assert/strict";
import test from "node:test";

import { reportCaughtError } from "@ticket-pos/sentry-rules";

import { captureFromScreen, rootErrorScreenLocale } from "./error-screen.ts";

const ID = "3f6c1a52-8d1e-4b7a-9c39-2a1f0e5d4c3b";
const WRITTEN = '{"dsn":"https://k@o1.ingest.us.sentry.io/2","environment":"production"}';

function chunk() {
  const captured: Array<{ error: unknown; requestId: string | null; written: string | null }> = [];
  let loads = 0;
  return {
    captured,
    loads: () => loads,
    load: async () => {
      loads += 1;
      return {
        captureCaughtError: (error: unknown, requestId: string | null, written: string | null) => {
          captured.push({ error, requestId, written });
        },
      };
    },
  };
}

/** Lets the promise chain the capture starts run to its end. */
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

test("with a DSN, a crash the screen caught loads the SDK and is reported once, under the reference code", async () => {
  const sdk = chunk();
  const capture = captureFromScreen({ written: WRITTEN, requested: () => false, load: sdk.load });
  const error = new Error("render broke");

  // The screen re-rendering is the same crash, not a second report.
  reportCaughtError(error, ID, capture);
  reportCaughtError(error, ID, capture);
  await settle();

  assert.equal(sdk.loads(), 1);
  assert.deepEqual(sdk.captured, [{ error, requestId: ID, written: WRITTEN }]);
});

test("once the loader has asked for the SDK, the screen reports even after the configuration left the page", async () => {
  // The root-level screen replaces the document the configuration was written into.
  const sdk = chunk();
  const capture = captureFromScreen({ written: null, requested: () => true, load: sdk.load });
  const error = new Error("layout broke");
  reportCaughtError(error, ID, capture);
  await settle();
  assert.deepEqual(sdk.captured, [{ error, requestId: ID, written: null }]);
});

test("with no DSN the screen asks for nothing and sends nothing", async () => {
  const sdk = chunk();
  const capture = captureFromScreen({ written: null, requested: () => false, load: sdk.load });
  reportCaughtError(new Error("render broke"), ID, capture);
  await settle();
  assert.equal(sdk.loads(), 0);
  assert.deepEqual(sdk.captured, []);
});

test("a chunk that cannot load, or a capture that throws, never reaches the screen", async () => {
  const offline = captureFromScreen({
    written: WRITTEN,
    requested: () => false,
    load: () => Promise.reject(new Error("offline")),
  });
  assert.doesNotThrow(() => offline(new Error("a"), ID));
  const failing = captureFromScreen({
    written: WRITTEN,
    requested: () => false,
    load: async () => ({
      captureCaughtError: () => {
        throw new Error("sdk broke");
      },
    }),
  });
  assert.doesNotThrow(() => failing(new Error("b"), ID));
  await settle();
});

test("the root-level screen speaks the language of the address, as every Storefront page does", () => {
  assert.equal(rootErrorScreenLocale("/es"), "es");
  assert.equal(rootErrorScreenLocale("/es/acme/events/fiesta"), "es");
  assert.equal(rootErrorScreenLocale("/en/checkout/success"), "en");
  // No page lives outside a locale; anything else falls back to the default.
  assert.equal(rootErrorScreenLocale("/fr/x"), "en");
  assert.equal(rootErrorScreenLocale(null), "en");
});
