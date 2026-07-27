import assert from "node:assert/strict";
import test from "node:test";

import { formatTaxId, isTaxIdType, normalizeTaxIdNumber, validateTaxId } from "./tax-id.ts";

// The mirror validator (#98). Its job is instant feedback, and its one hard
// requirement is agreeing with backend/internal/platform/taxid.go — a number
// this file rejects but the API accepts is a buyer locked out of a sale the
// platform would have taken. The fixtures below are the same ones the Go
// integration suite uses, for exactly that reason.

const VALID_CEDULA = "1712345675";
const NATURAL_RUC = "1712345675001";
const COMPANY_RUC = "1790012346001";

test("accepts a cédula with a correct check digit", () => {
  assert.equal(validateTaxId("cedula", VALID_CEDULA), null);
  assert.equal(validateTaxId("cedula", `  ${VALID_CEDULA}  `), null);
});

test("rejects a cédula that fails the check digit, length, or province", () => {
  assert.notEqual(validateTaxId("cedula", "1712345678"), null); // wrong check digit
  assert.notEqual(validateTaxId("cedula", "17123456"), null); // too short
  assert.notEqual(validateTaxId("cedula", "9912345675"), null); // no such province
  assert.notEqual(validateTaxId("cedula", "17123456ab"), null); // not digits
});

test("accepts both RUC forms and rejects the wrong length", () => {
  assert.equal(validateTaxId("ruc", NATURAL_RUC), null);
  assert.equal(validateTaxId("ruc", COMPANY_RUC), null);
  assert.notEqual(validateTaxId("ruc", VALID_CEDULA), null);
  // Third digit 7 names no RUC form.
  assert.notEqual(validateTaxId("ruc", "1772345675001"), null);
});

test("accepts a passport on shape alone", () => {
  assert.equal(validateTaxId("passport", "ab123456"), null);
  assert.equal(validateTaxId("passport", "X1234567890"), null);
  assert.notEqual(validateTaxId("passport", "AB12"), null);
  assert.notEqual(validateTaxId("passport", "AB-123456"), null);
});

test("a blank number is required whatever the type", () => {
  for (const type of ["cedula", "ruc", "passport"]) {
    assert.equal(validateTaxId(type, "   "), "is required");
  }
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
