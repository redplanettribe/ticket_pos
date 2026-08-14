import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { apiErrorMessage, fieldErrorMessages, type ErrorCatalog } from "./api-errors.ts";

/**
 * Resolution is asserted against the REAL en.json, not a fixture.
 *
 * The failure this guards against is a catalog and a resolver that each look
 * correct alone: a group renamed, a code cataloged one level too deep, an `errors`
 * namespace reshaped by a later migration. A fixture would keep passing through
 * every one of those. The Spanish is guarded separately, by lib/messages.test.ts,
 * which holds es.json to exactly the keys en.json has.
 */
const catalog = (
  JSON.parse(readFileSync(new URL("../messages/en.json", import.meta.url), "utf8")) as {
    errors: ErrorCatalog;
  }
).errors;

// --- the catalog's own shape -----------------------------------------------

test("the errors namespace holds the two groups resolution looks in", () => {
  assert.equal(typeof catalog.envelope, "object");
  assert.equal(typeof catalog.field, "object");
});

test("every cataloged sentence is a sentence and not a nested group", () => {
  for (const [group, codes] of Object.entries(catalog)) {
    for (const [code, copy] of Object.entries(codes ?? {})) {
      assert.equal(typeof copy, "string", `errors.${group}.${code} is not a string`);
      assert.notEqual(copy.trim(), "", `errors.${group}.${code} is empty`);
    }
  }
});

// --- a code the catalog knows ----------------------------------------------

test("a cataloged code is answered in this app's words, not the API's", () => {
  const resolved = apiErrorMessage(catalog, {
    code: "UNAUTHORIZED",
    message: "Missing session token",
  });
  assert.equal(resolved, catalog.envelope!.UNAUTHORIZED);
  assert.notEqual(resolved, "Missing session token");
});

// --- the floor: a code the catalog does not know ---------------------------

test("an unknown code degrades to the API's English message", () => {
  // The whole point: the backend ships a code alone and the staff app still says
  // something true rather than nothing.
  assert.equal(
    apiErrorMessage(catalog, {
      code: "SOME_CODE_SHIPPED_LATER",
      message: "The event slug is already taken",
    }),
    "The event slug is already taken",
  );
});

test("an envelope with no code at all still shows its message", () => {
  assert.equal(apiErrorMessage(catalog, { message: "Request failed" }), "Request failed");
});

// --- nothing to show --------------------------------------------------------

test("nothing at all answers null, so the caller can say what it was doing", () => {
  assert.equal(apiErrorMessage(catalog, null), null);
  assert.equal(apiErrorMessage(catalog, undefined), null);
  assert.equal(apiErrorMessage(catalog, { code: "NOPE" }), null);
});

test("a message that is only whitespace is not a message", () => {
  // Otherwise the reader gets a red alert with an empty body.
  assert.equal(apiErrorMessage(catalog, { code: "NOPE", message: "   " }), null);
});

// --- details interpolation --------------------------------------------------

const withDetails: ErrorCatalog = {
  envelope: { LIMITED: "You may hold {limit} at a time, and you have {held}." },
};

test("a sentence naming facts takes them from the API's details", () => {
  assert.equal(
    apiErrorMessage(withDetails, { code: "LIMITED", message: "Limit reached", details: { limit: 4, held: 4 } }),
    "You may hold 4 at a time, and you have 4.",
  );
});

test("a sentence missing a fact falls back to the API's words rather than printing a placeholder", () => {
  assert.equal(
    apiErrorMessage(withDetails, { code: "LIMITED", message: "Limit reached", details: { limit: 4 } }),
    "Limit reached",
  );
});

test("details that are not values to read are not substituted", () => {
  assert.equal(
    apiErrorMessage(withDetails, {
      code: "LIMITED",
      message: "Limit reached",
      details: { limit: { nested: true }, held: [1] },
    }),
    "Limit reached",
  );
});

test("resolution does not answer differently on its second call", () => {
  const error = { code: "LIMITED", message: "Limit reached", details: { limit: 4, held: 4 } };
  assert.equal(apiErrorMessage(withDetails, error), apiErrorMessage(withDetails, error));
});

// --- field errors -----------------------------------------------------------

test("a field error resolves on its own code, one level down", () => {
  const resolved = fieldErrorMessages(catalog, {
    fields: [{ field: "email", code: "REQUIRED", message: "is required" }],
  });
  assert.equal(resolved.email, catalog.field!.REQUIRED);
});

test("a field code the catalog does not know keeps the API's message", () => {
  const resolved = fieldErrorMessages(catalog, {
    fields: [{ field: "slug", code: "NOT_CATALOGED_YET", message: "is already taken" }],
  });
  assert.equal(resolved.slug, "is already taken");
});

test("a field with neither copy nor message is dropped rather than shown blank", () => {
  const resolved = fieldErrorMessages(catalog, {
    fields: [{ field: "slug", code: "NOT_CATALOGED_YET" }, { field: "", code: "REQUIRED" }],
  });
  assert.deepEqual(resolved, {});
});

test("details of the wrong shape produce no field errors rather than throwing", () => {
  assert.deepEqual(fieldErrorMessages(catalog, null), {});
  assert.deepEqual(fieldErrorMessages(catalog, "nope"), {});
  assert.deepEqual(fieldErrorMessages(catalog, { fields: "nope" }), {});
  assert.deepEqual(fieldErrorMessages(catalog, { fields: [null, 3] }), {});
});
