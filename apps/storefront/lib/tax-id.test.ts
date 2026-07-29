import assert from "node:assert/strict";
import test from "node:test";

import { formatTaxId, isTaxIdType, normalizeTaxIdNumber, validateTaxId } from "./tax-id.ts";

// The mirror validator (#98). Its job is instant feedback, and its one hard
// requirement is agreeing with backend/internal/platform/taxid.go — a number
// this file rejects but the API accepts is a buyer locked out of a sale the
// platform would have taken. The fixtures below are the same ones the Go
// integration suite uses, for exactly that reason.
//
// The verdicts are asserted as the CODES the API sends for the same failures
// (validation_codes.go), which is what the mirror answers with since ADR 0023 —
// the sentence is chosen from the code by the catalog, in the language the page
// is read in, and api-errors.test.ts is where the two paths are held to one
// sentence.

const VALID_CEDULA = "1712345675";
const NATURAL_RUC = "1712345675001";
const COMPANY_RUC = "1790012346001";

test("accepts a cédula with a correct check digit", () => {
  assert.equal(validateTaxId("cedula", VALID_CEDULA), null);
  assert.equal(validateTaxId("cedula", `  ${VALID_CEDULA}  `), null);
});

test("rejects a cédula that fails the check digit, length, or province", () => {
  assert.equal(validateTaxId("cedula", "1712345678"), "INVALID_CEDULA"); // wrong check digit
  assert.equal(validateTaxId("cedula", "17123456"), "INVALID_CEDULA"); // too short
  assert.equal(validateTaxId("cedula", "9912345675"), "INVALID_CEDULA"); // no such province
  assert.equal(validateTaxId("cedula", "17123456ab"), "INVALID_CEDULA"); // not digits
});

test("accepts both RUC forms and rejects the wrong length", () => {
  assert.equal(validateTaxId("ruc", NATURAL_RUC), null);
  assert.equal(validateTaxId("ruc", COMPANY_RUC), null);
  assert.equal(validateTaxId("ruc", VALID_CEDULA), "INVALID_RUC");
  // Third digit 7 names no RUC form.
  assert.equal(validateTaxId("ruc", "1772345675001"), "INVALID_RUC");
});

test("accepts a passport on shape alone", () => {
  assert.equal(validateTaxId("passport", "ab123456"), null);
  assert.equal(validateTaxId("passport", "X1234567890"), null);
  assert.equal(validateTaxId("passport", "AB12"), "INVALID_PASSPORT");
  assert.equal(validateTaxId("passport", "AB-123456"), "INVALID_PASSPORT");
});

test("a blank number is required whatever the type", () => {
  for (const type of ["cedula", "ruc", "passport"]) {
    assert.equal(validateTaxId(type, "   "), "REQUIRED");
  }
});

test("an unknown Tax ID Type is the type field's problem, not the number's", () => {
  assert.equal(validateTaxId("dni", "12345678"), null);
});

test("normalisation trims, and uppercases passports only", () => {
  assert.equal(normalizeTaxIdNumber("passport", " ab123456 "), "AB123456");
  assert.equal(normalizeTaxIdNumber("cedula", `  ${VALID_CEDULA} `), VALID_CEDULA);
});

test("isTaxIdType names the three Tax ID Types and nothing else", () => {
  assert.ok(isTaxIdType("cedula"));
  assert.ok(isTaxIdType("ruc"));
  assert.ok(isTaxIdType("passport"));
  assert.equal(isTaxIdType("dni"), false);
});

// The receipt rendering (#99): a Ticket Sale's Tax ID snapshot as the Customer
// Area shows it, or null when the sale carries none so the card can draw "—".

test("formatTaxId renders the label with the number for every Tax ID Type", () => {
  assert.equal(formatTaxId("cedula", VALID_CEDULA), `Cédula: ${VALID_CEDULA}`);
  assert.equal(formatTaxId("ruc", COMPANY_RUC), `RUC: ${COMPANY_RUC}`);
  assert.equal(formatTaxId("passport", "AB123456"), "Pasaporte: AB123456");
});

test("formatTaxId is null for a sale with no Tax ID, and for half of one", () => {
  assert.equal(formatTaxId(null, null), null);
  assert.equal(formatTaxId("cedula", null), null);
  assert.equal(formatTaxId(null, VALID_CEDULA), null);
  assert.equal(formatTaxId("", ""), null);
});

test("formatTaxId falls back to the stored type when it names no known label", () => {
  assert.equal(formatTaxId("dni", "12345678"), "dni: 12345678");
});
