import assert from "node:assert/strict";
import test from "node:test";

import { rootErrorScreenLocale } from "./error-screen.ts";

test("the root-level screen reads the Staff Locale cookie, then the browser, then English", () => {
  assert.equal(rootErrorScreenLocale({ cookies: "NEXT_LOCALE=es", languages: ["en-US"] }), "es");
  assert.equal(rootErrorScreenLocale({ cookies: "NEXT_LOCALE=en", languages: ["es-EC"] }), "en");
  assert.equal(rootErrorScreenLocale({ cookies: "", languages: ["es-EC", "en"] }), "es");
  assert.equal(rootErrorScreenLocale({ cookies: "NEXT_LOCALE=fr", languages: ["de"] }), "en");
  assert.equal(rootErrorScreenLocale({ cookies: "", languages: [] }), "en");
});
