import assert from "node:assert/strict";
import test from "node:test";

import { ECUADOR_DIALLING_CODE, PHONE_ECUADOR_MESSAGE, PHONE_GENERIC_MESSAGE } from "./phone.ts";
import {
  profileDraftPhone,
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
    phoneDiallingCode: ECUADOR_DIALLING_CODE,
    phoneNationalNumber: "",
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
    phone: null,
  });
  assert.equal("email" in body, false);
});

// The phone half of the same form (#108). The rule itself belongs to phone.ts
// and is tested there against the backend's own table; what is asserted here is
// how "My info" uses it — that the two controls assemble into one canonical
// value, that an empty field is a clear rather than a complaint, and that a
// stored number resolves back into the controls it was typed with.

test("assembles the country and the national number into one canonical value", () => {
  const body = profileUpdateBody(draft({ phoneNationalNumber: "098 765 4321" }));
  assert.equal(body.phone, "+593987654321");
});

test("sends a foreign number under the country the selector holds", () => {
  const body = profileUpdateBody(
    draft({ phoneDiallingCode: "+44", phoneNationalNumber: "7911 123456" }),
  );
  assert.equal(body.phone, "+447911123456");
});

test("treats an empty phone field as clearing rather than as an error", () => {
  // Withdrawing a stored number is a capability the Customer is owed (#103), so
  // an emptied field must reach the API as an explicit null and not as silence.
  assert.deepEqual(validateProfileDraft(draft({ phoneNationalNumber: "" })), {});
  assert.deepEqual(validateProfileDraft(draft({ phoneNationalNumber: "   " })), {});
  assert.equal(profileUpdateBody(draft({ phoneNationalNumber: "  " })).phone, null);
});

test("rejects a phone the API would reject, in the API's own words", () => {
  // An Ecuadorian landline: a real number, refused because the hosted payment
  // form wants a cardholder's mobile.
  assert.equal(
    validateProfileDraft(draft({ phoneNationalNumber: "22345678" })).phone,
    PHONE_ECUADOR_MESSAGE,
  );
  // A mobile a digit short gets the same tier's message.
  assert.equal(
    validateProfileDraft(draft({ phoneNationalNumber: "98765432" })).phone,
    PHONE_ECUADOR_MESSAGE,
  );
  // Everywhere else, the permissive tier and its wording.
  assert.equal(
    validateProfileDraft(draft({ phoneDiallingCode: "+1", phoneNationalNumber: "202555012345678" }))
      .phone,
    PHONE_GENERIC_MESSAGE,
  );
});

test("resolves a stored number back into the two controls that typed it", () => {
  assert.deepEqual(profileDraftPhone("+593987654321"), {
    phoneDiallingCode: "+593",
    phoneNationalNumber: "987654321",
  });
  assert.deepEqual(profileDraftPhone("+447911123456"), {
    phoneDiallingCode: "+44",
    phoneNationalNumber: "7911123456",
  });
});

test("opens on Ecuador and an empty field for a Customer with no stored number", () => {
  // Unchanged from the state the checkout dialog opens in: the home market is
  // the common case and should need no interaction at all.
  assert.deepEqual(profileDraftPhone(null), {
    phoneDiallingCode: ECUADOR_DIALLING_CODE,
    phoneNationalNumber: "",
  });
});

test("a stored number survives the round trip through the form untouched", () => {
  // The property that makes the prefill safe: opening "My info" and pressing
  // Save without touching the phone stores exactly what was there before.
  const stored = "+593987654321";
  const body = profileUpdateBody(draft(profileDraftPhone(stored)));
  assert.equal(body.phone, stored);
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
