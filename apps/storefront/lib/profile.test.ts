import assert from "node:assert/strict";
import test from "node:test";

import {
  profileFieldErrorsFromDetails,
  profileUpdateBody,
  validateProfileDraft,
  type ProfileDraft,
} from "./profile.ts";

// The "My info" form's client-side rules (#102). They mirror the API and exist
// only to answer faster than it can; the assertions below are therefore about
// agreeing with the backend, not about inventing a second policy.

const VALID_CEDULA = "1712345675";

function draft(overrides: Partial<ProfileDraft> = {}): ProfileDraft {
  return {
    firstName: "Ana",
    lastName: "Lopez",
    taxIdType: "cedula",
    taxIdNumber: VALID_CEDULA,
    ...overrides,
  };
}

test("accepts a complete draft", () => {
  assert.deepEqual(validateProfileDraft(draft()), {});
});

test("rejects a blank first or last name", () => {
  // The API rejects these too, and must: its customer-upsert guard reads a
  // blank name as "never named".
  assert.equal(validateProfileDraft(draft({ firstName: "" })).first_name, "is required");
  assert.equal(validateProfileDraft(draft({ firstName: "   " })).first_name, "is required");
  assert.equal(validateProfileDraft(draft({ lastName: "" })).last_name, "is required");
  assert.equal(validateProfileDraft(draft({ lastName: "\t" })).last_name, "is required");
});

test("rejects a Tax ID number that fails its type's rules", () => {
  // Right length, wrong check digit.
  assert.ok(validateProfileDraft(draft({ taxIdNumber: "1712345678" })).tax_id_number);
  // A RUC typed under the cédula type.
  assert.ok(validateProfileDraft(draft({ taxIdNumber: `${VALID_CEDULA}001` })).tax_id_number);
});

test("treats an empty ID number as clearing rather than as an error", () => {
  assert.deepEqual(validateProfileDraft(draft({ taxIdNumber: "" })), {});
  assert.deepEqual(validateProfileDraft(draft({ taxIdNumber: "   " })), {});

  const body = profileUpdateBody(draft({ taxIdNumber: "  " }));
  assert.equal(body.tax_id_type, null);
  assert.equal(body.tax_id_number, null);
});

test("sends trimmed names and a normalised Tax ID, and never an email", () => {
  const body = profileUpdateBody(
    draft({ firstName: "  Ana ", lastName: " Lopez ", taxIdType: "passport", taxIdNumber: " ab123456 " }),
  );
  assert.deepEqual(body, {
    first_name: "Ana",
    last_name: "Lopez",
    tax_id_type: "passport",
    tax_id_number: "AB123456",
  });
  assert.equal("email" in body, false);
});

test("reads the API's field errors and ignores fields the form has no input for", () => {
  const errors = profileFieldErrorsFromDetails({
    fields: [
      { field: "first_name", message: "is required" },
      { field: "tax_id_number", message: "must be a valid 10-digit cédula" },
      { field: "email", message: "cannot be changed" },
    ],
  });
  assert.deepEqual(errors, {
    first_name: "is required",
    tax_id_number: "must be a valid 10-digit cédula",
  });
});

test("survives an error envelope with no usable details", () => {
  assert.deepEqual(profileFieldErrorsFromDetails(undefined), {});
  assert.deepEqual(profileFieldErrorsFromDetails({ fields: "nope" }), {});
  assert.deepEqual(profileFieldErrorsFromDetails({ fields: [null, 7, {}] }), {});
});
