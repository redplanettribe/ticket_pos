import assert from "node:assert/strict";
import test from "node:test";

import {
  DOSSIER_INVOICE_STATUSES,
  dossierInvoiceKindKey,
  dossierInvoiceRoleKey,
  dossierInvoiceStatusKey,
  dossierInvoiceStatusVariant,
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
  for (const phone of [null, undefined, "", "   "]) {
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

// --- invoice status ------------------------------------------------------------------

test("every invoice status has its own word", () => {
  const keys = DOSSIER_INVOICE_STATUSES.map((status) => dossierInvoiceStatusKey(status));
  assert.equal(keys.includes(null), false);
  assert.equal(new Set(keys).size, DOSSIER_INVOICE_STATUSES.length);
  assert.equal(dossierInvoiceStatusKey("authorized"), "invoiceStatusAuthorized");
  assert.equal(dossierInvoiceStatusKey("needs_attention"), "invoiceStatusNeedsAttention");
});

test("an invoice status this app has never heard of has no word", () => {
  for (const status of ["issued", "Authorized", "", null, undefined]) {
    assert.equal(dossierInvoiceStatusKey(status), null, String(status));
  }
});

test("a refused document is drawn as trouble and an unknown one quietly", () => {
  assert.equal(dossierInvoiceStatusVariant("authorized"), "default");
  assert.equal(dossierInvoiceStatusVariant("owed"), "secondary");
  assert.equal(dossierInvoiceStatusVariant("rejected"), "destructive");
  assert.equal(dossierInvoiceStatusVariant("needs_attention"), "destructive");
  assert.equal(dossierInvoiceStatusVariant("annulled"), "outline");
  assert.equal(dossierInvoiceStatusVariant("mystery"), "outline");
});

// --- kind and role -------------------------------------------------------------------

test("dossierInvoiceKindKey names a Sale Invoice and a Credit Note", () => {
  assert.equal(dossierInvoiceKindKey("sale"), "invoiceKindSale");
  assert.equal(dossierInvoiceKindKey("credit_note"), "invoiceKindCreditNote");
  assert.equal(dossierInvoiceKindKey("manual"), null);
  assert.equal(dossierInvoiceKindKey(""), null);
});

test("only the current and the superseded factura earn a role badge", () => {
  assert.equal(dossierInvoiceRoleKey("current"), "invoiceRoleCurrent");
  assert.equal(dossierInvoiceRoleKey("superseded"), "invoiceRoleSuperseded");
  assert.equal(dossierInvoiceRoleKey("credit_note"), null);
  assert.equal(dossierInvoiceRoleKey("not_current"), null);
  assert.equal(dossierInvoiceRoleKey("unheard_of"), null);
});
