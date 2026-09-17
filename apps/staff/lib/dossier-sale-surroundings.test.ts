import assert from "node:assert/strict";
import test from "node:test";

import {
  DOSSIER_INVOICE_STATUSES,
  documentRoleToken,
  invoiceKindToken,
  invoiceStatusToken,
  phoneGivenOnSale,
} from "./dossier-sale-surroundings.ts";

// --- phone -------------------------------------------------------------------------

test("phoneGivenOnSale gives the number and a tel: link to call it", () => {
  assert.deepEqual(phoneGivenOnSale({ phone: "+593987654321" }), {
    state: "given",
    phone: "+593987654321",
    href: "tel:+593987654321",
  });
});

test("phoneGivenOnSale keeps the number as given but dials only its digits", () => {
  assert.deepEqual(phoneGivenOnSale({ phone: " +1 (202) 555-0123 " }), {
    state: "given",
    phone: "+1 (202) 555-0123",
    href: "tel:+12025550123",
  });
});

test("a Sale with no phone says none rather than drawing a blank", () => {
  for (const phone of [null, "", "   "]) {
    assert.deepEqual(phoneGivenOnSale({ phone }), { state: "none" }, JSON.stringify(phone));
  }
});

test("a phone with nothing to dial is still shown, without a link", () => {
  assert.deepEqual(phoneGivenOnSale({ phone: "call reception" }), {
    state: "given",
    phone: "call reception",
    href: null,
  });
});

// --- tax invoices --------------------------------------------------------------------

test("every invoice status the API sends is recognised", () => {
  for (const status of DOSSIER_INVOICE_STATUSES) {
    assert.equal(invoiceStatusToken(status), status);
  }
});

test("an invoice status this app has never heard of is not guessed at", () => {
  for (const status of ["issued", "Authorized", "", null, undefined]) {
    assert.equal(invoiceStatusToken(status), null, String(status));
  }
});

test("invoiceKindToken knows a Sale Invoice and a Credit Note, and nothing else", () => {
  assert.equal(invoiceKindToken("sale"), "sale");
  assert.equal(invoiceKindToken("credit_note"), "credit_note");
  assert.equal(invoiceKindToken("receipt"), null);
  assert.equal(invoiceKindToken(""), null);
});

test("documentRoleToken knows the four chain roles, and nothing else", () => {
  for (const role of ["current", "superseded", "credit_note", "not_current"]) {
    assert.equal(documentRoleToken(role), role);
  }
  assert.equal(documentRoleToken("unheard_of"), null);
  assert.equal(documentRoleToken(null), null);
});
