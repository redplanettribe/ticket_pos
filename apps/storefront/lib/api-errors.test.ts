import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  MIRROR_FIELD_CODES,
  apiErrorMessage,
  fieldCodeMessage,
  fieldErrorMessages,
  type ErrorCatalog,
  type FieldErrorCode,
} from "./api-errors.ts";
import { validatePhone } from "./phone.ts";
import {
  profileFieldErrorsFromDetails,
  profileFieldMessages,
  validateProfileDraft,
} from "./profile.ts";
import { validateTaxId } from "./tax-id.ts";

/**
 * Error copy selected by the API's code (ADR 0023).
 *
 * The catalog under test is the real en.json rather than a fixture, because the
 * assertion that matters is that a code the API actually sends finds copy — a
 * fixture would pass while the shipped catalog was empty. The fallback cases
 * name codes no backend will ever send, which is exactly the case they are
 * about.
 */

function errorsOf(locale: string): ErrorCatalog {
  return (
    JSON.parse(readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8")) as {
      errors: ErrorCatalog;
    }
  ).errors;
}

const catalog = errorsOf("en");
const spanish = errorsOf("es");

test("a code the catalog knows is answered in the catalog's words", () => {
  const soldOut = { code: "CAPACITY_EXCEEDED", message: "Not enough tickets remaining." };
  assert.equal(apiErrorMessage(catalog, soldOut), "Not enough tickets remaining.");
  assert.equal(
    apiErrorMessage(catalog, { code: "OTP_INVALID", message: "Invalid passcode." }),
    "That passcode isn't correct.",
  );
});

test("a code the catalog has never heard of falls back to the API's message, verbatim", () => {
  // The property the whole scheme rests on: the backend may ship a code before
  // this app knows it, and what a Customer sees is what they saw before any of
  // this existed — never a blank, and never a coordinated release.
  const message = "The organizer has paused sales on this event.";
  assert.equal(apiErrorMessage(catalog, { code: "SALES_PAUSED", message }), message);
});

test("an unknown code with nothing to say degrades to null rather than to a blank", () => {
  // null is the signal for "say your own sentence". Every call site pairs it
  // with a `?? t(...)`, so an empty Alert is not a state this can produce.
  assert.equal(apiErrorMessage(catalog, { code: "SALES_PAUSED", message: "" }), null);
  assert.equal(apiErrorMessage(catalog, { code: "SALES_PAUSED", message: "   " }), null);
  assert.equal(apiErrorMessage(catalog, { code: "SALES_PAUSED" }), null);
  assert.equal(apiErrorMessage(catalog, { code: null, message: null }), null);
  assert.equal(apiErrorMessage(catalog, null), null);
  assert.equal(apiErrorMessage(catalog, undefined), null);
});

test("a known code with no message still answers, because the code is what was keyed on", () => {
  assert.equal(
    apiErrorMessage(catalog, { code: "SALE_ALREADY_REVERSED" }),
    "This purchase has already been undone.",
  );
});

test("one code with two meanings is told apart by the surface that received it", () => {
  // CUSTOMER_SESSION_SCOPE_INSUFFICIENT is the API's own ambiguity: the same
  // refusal, worded for the operation it refused. A key of code alone would be
  // wrong on one of these two surfaces every time.
  const code = "CUSTOMER_SESSION_SCOPE_INSUFFICIENT";
  const undoRefusal = { code, message: "Sign in with a passcode to undo this purchase." };
  const profileRefusal = { code, message: "Sign in with a passcode to change your details." };

  assert.equal(
    apiErrorMessage(catalog, undoRefusal, "undo"),
    "Sign in with a passcode to undo this purchase.",
  );
  assert.equal(
    apiErrorMessage(catalog, profileRefusal, "myInfo"),
    "Sign in with a passcode to change your details.",
  );
});

test("the ambiguous code named by no surface falls back to the API's message", () => {
  // Deliberately absent from `envelope`: a third surface that starts receiving
  // this code shows the API's own sentence rather than one of the other two
  // surfaces' sentences, which would be a confident lie.
  const message = "Sign in with a passcode to do that.";
  assert.equal(
    apiErrorMessage(catalog, { code: "CUSTOMER_SESSION_SCOPE_INSUFFICIENT", message }),
    message,
  );
});

test("a surface group is consulted first and falls through for everything else", () => {
  // The undo dialog receives far more than its one overridden code; those come
  // out of `envelope` exactly as they do everywhere else.
  assert.equal(
    apiErrorMessage(catalog, { code: "REVERSAL_WINDOW_CLOSED" }, "undo"),
    "The time to undo this purchase has passed. Contact the organizer for help.",
  );
});

test("a field error carrying a code is answered in the catalog's words", () => {
  const errors = fieldErrorMessages(catalog, {
    fields: [
      { field: "customer_email", code: "INVALID_EMAIL", message: "must be a valid email" },
      {
        field: "customer_tax_id_number",
        code: "INVALID_CEDULA",
        message: "must be a valid 10-digit cédula",
      },
    ],
  });
  assert.deepEqual(errors, {
    customer_email: "must be a valid email address",
    customer_tax_id_number: "must be a valid 10-digit cédula",
  });
});

test("a field error with no code, or an unknown one, keeps its own message", () => {
  const errors = fieldErrorMessages(catalog, {
    fields: [
      { field: "customer_first_name", message: "is required" },
      {
        field: "lines[0].quantity",
        code: "INVALID_POSITIVE_INT",
        message: "must be greater than zero",
      },
    ],
  });
  assert.deepEqual(errors, {
    customer_first_name: "is required",
    // No input on any Storefront form sits under this one, so it has no catalog
    // entry; the API's message survives and the form drops it.
    "lines[0].quantity": "must be greater than zero",
  });
});

test("a field error with neither usable copy nor a name is dropped, never blanked", () => {
  const blank = { fields: [{ field: "phone", message: "  " }] };
  assert.deepEqual(fieldErrorMessages(catalog, blank), {});
  assert.deepEqual(fieldErrorMessages(catalog, { fields: [{ field: "phone" }] }), {});
  assert.deepEqual(fieldErrorMessages(catalog, { fields: [{ code: "REQUIRED" }] }), {});
});

test("details that are not a field list yield nothing at all", () => {
  assert.deepEqual(fieldErrorMessages(catalog, undefined), {});
  assert.deepEqual(fieldErrorMessages(catalog, null), {});
  assert.deepEqual(fieldErrorMessages(catalog, "fields"), {});
  assert.deepEqual(fieldErrorMessages(catalog, { fields: "nope" }), {});
  assert.deepEqual(fieldErrorMessages(catalog, { fields: [null, 7, {}] }), {});
});

// The mirror validators and the API, held to ONE sentence.
//
// lib/tax-id.ts, lib/phone.ts and validateProfileDraft answer before the API is
// asked, and they answer with the API's own field codes. That is the whole point
// of the arrangement: a buyer who mistypes a cédula must be told the same thing
// whether the mirror caught it or the round trip did, in the one language they
// are reading. These tests fail loudly the moment that stops being true —
// because the failure is otherwise invisible everywhere else, showing up only as
// an English sentence on a Spanish page.

/**
 * One verdict each, stated three ways: what a form does to provoke it, the code
 * both sides name it with, and the sentence the API sends alongside that code
 * today (backend/internal/platform/taxid.go, phone.go).
 */
const MIRRORED_VERDICTS: {
  what: string;
  field: string;
  code: FieldErrorCode;
  mirror: () => FieldErrorCode | null;
  apiMessage: string;
}[] = [
  {
    what: "a blank Tax ID number",
    field: "customer_tax_id_number",
    code: "REQUIRED",
    mirror: () => validateTaxId("cedula", "   "),
    apiMessage: "is required",
  },
  {
    what: "a cédula with the wrong check digit",
    field: "customer_tax_id_number",
    code: "INVALID_CEDULA",
    mirror: () => validateTaxId("cedula", "1712345678"),
    apiMessage: "must be a valid 10-digit cédula",
  },
  {
    what: "a RUC whose third digit names no form",
    field: "customer_tax_id_number",
    code: "INVALID_RUC",
    mirror: () => validateTaxId("ruc", "1772345675001"),
    apiMessage: "must be a valid 13-digit RUC",
  },
  {
    what: "a passport too short to be one",
    field: "customer_tax_id_number",
    code: "INVALID_PASSPORT",
    mirror: () => validateTaxId("passport", "AB12"),
    apiMessage: "must be 6–20 letters or digits",
  },
  {
    what: "an Ecuadorian landline where a mobile is required",
    field: "customer_phone",
    code: "INVALID_PHONE_EC",
    mirror: () => validatePhone("+59322345678"),
    apiMessage: "must be an Ecuadorian mobile: 9 digits starting with 9",
  },
  {
    what: "a foreign number too short to dial",
    field: "customer_phone",
    code: "INVALID_PHONE",
    mirror: () => validatePhone("+123"),
    apiMessage: "must be 4–15 digits in international format, like +12025550123",
  },
];

test("the mirrors answer with the API's own codes, never a parallel vocabulary", () => {
  for (const { what, code, mirror } of MIRRORED_VERDICTS) {
    assert.equal(mirror(), code, what);
  }
});

test("a mirror verdict and the API's own verdict resolve to one sentence, in either language", () => {
  for (const errors of [catalog, spanish]) {
    for (const { what, field, code, apiMessage } of MIRRORED_VERDICTS) {
      const preflight = fieldCodeMessage(errors, code);
      const postflight = fieldErrorMessages(errors, {
        fields: [{ field, code, message: apiMessage }],
      })[field];
      assert.equal(preflight, postflight, what);
    }
  }
});

test("the mirrors' Spanish is Spanish, not the API's English relayed", () => {
  // The leak this whole change closes: pre-flight copy that stayed English while
  // the page around it was translated. Every mirror verdict must read
  // differently in the two catalogs, because every one of them is a sentence
  // rather than a bare placeholder.
  for (const { what, code } of MIRRORED_VERDICTS) {
    assert.notEqual(fieldCodeMessage(spanish, code), fieldCodeMessage(catalog, code), what);
  }
});

test("every code a mirror can answer with has copy in every catalog", () => {
  // A mirror has no API message to fall back on — nothing was sent — so a
  // missing entry is a form that refuses while showing a bare code. The
  // fallback in fieldCodeMessage exists to make that visible; this keeps it
  // unreachable.
  for (const code of MIRROR_FIELD_CODES) {
    assert.notEqual(fieldCodeMessage(catalog, code), code, `en.json: errors.field.${code}`);
    assert.notEqual(fieldCodeMessage(spanish, code), code, `es.json: errors.field.${code}`);
  }
});

test("'My info' resolves its own verdicts through the path the API's errors take", () => {
  // The same equivalence one layer up, where the form's two entry points meet:
  // a draft the mirror refused, and the identical refusal arriving as a
  // VALIDATION_FAILED envelope.
  const codes = validateProfileDraft({
    firstName: "",
    lastName: "Lopez",
    taxIdType: "cedula",
    taxIdNumber: "1712345678",
    phoneDiallingCode: "+593",
    phoneNationalNumber: "22345678",
  });
  const fromApi = profileFieldErrorsFromDetails(spanish, {
    fields: [
      { field: "first_name", code: "REQUIRED", message: "is required" },
      {
        field: "tax_id_number",
        code: "INVALID_CEDULA",
        message: "must be a valid 10-digit cédula",
      },
      {
        field: "phone",
        code: "INVALID_PHONE_EC",
        message: "must be an Ecuadorian mobile: 9 digits starting with 9",
      },
    ],
  });
  assert.deepEqual(profileFieldMessages(spanish, codes), fromApi);
});

test("an empty catalog resolves everything to the API's own words", () => {
  // What a Storefront released before this catalog existed did, and what one
  // released after a namespace rename would do: degrade to today's behaviour.
  const soldOut = { code: "CAPACITY_EXCEEDED", message: "Sold out." };
  assert.equal(apiErrorMessage({}, soldOut), "Sold out.");
  const required = { fields: [{ field: "email", code: "REQUIRED", message: "is required" }] };
  assert.deepEqual(fieldErrorMessages({}, required), { email: "is required" });
});
