import assert from "node:assert/strict";
import test from "node:test";

import { checkoutIdentity, type CheckoutSessionFacts } from "./checkout-identity.ts";

const NOTHING_OUTSTANDING = {
  policy_acceptance: false,
  marketing_consent: false,
  networking_consent: false,
};

function facts(overrides: Partial<CheckoutSessionFacts> = {}): CheckoutSessionFacts {
  return {
    email: "ana@example.com",
    first_name: "Ana",
    last_name: "Lovelace",
    tax_id_type: "cedula",
    tax_id_number: "1712345675",
    phone: "+593991234567",
    ticket_sale_id: null,
    consent_boxes: NOTHING_OUTSTANDING,
    ...overrides,
  };
}

test("a full Customer Session is who the purchase is addressed to", () => {
  assert.deepEqual(checkoutIdentity({ status: "ok", data: facts() }), {
    email: "ana@example.com",
    firstName: "Ana",
    lastName: "Lovelace",
    taxIdType: "cedula",
    taxIdNumber: "1712345675",
    phone: "+593991234567",
    consentBoxes: NOTHING_OUTSTANDING,
  });
});

test("no session is no identity: the wall stands", () => {
  assert.equal(checkoutIdentity({ status: "signed-out" }), null);
});

test("a session read that failed is no identity either", () => {
  // A dialog that cannot name the address it is writing to must not collect a
  // purchase. The buyer meets the sign-in page, which reads the same session.
  assert.equal(checkoutIdentity({ status: "error", code: null, message: null }), null);
});

test("a Confirmation Link session cannot buy", () => {
  // It is minted from a token that travelled inside a receipt and may have been
  // forwarded, so it is not Proof of Email Ownership. The API refuses it with
  // 403 CUSTOMER_SESSION_SCOPE_INSUFFICIENT; drawing the dialog would collect a
  // whole form on the way to that refusal.
  const read = { status: "ok", data: facts({ ticket_sale_id: "sale-1" }) } as const;
  assert.equal(checkoutIdentity(read), null);
});

test("a session with no address is no identity", () => {
  assert.equal(checkoutIdentity({ status: "ok", data: facts({ email: "   " }) }), null);
});

test("the address is trimmed, because it is drawn as a statement", () => {
  const identity = checkoutIdentity({ status: "ok", data: facts({ email: "  ana@example.com " }) });
  assert.equal(identity?.email, "ana@example.com");
});

test("signing in mints a Customer with no name, and that is an identity", () => {
  // The sign-in form asks for an address, a code and consent, and nothing else.
  // The dialog is where the name is collected, so an empty one must not be
  // mistaken for a missing session.
  const identity = checkoutIdentity({
    status: "ok",
    data: facts({ first_name: "", last_name: "" }),
  });
  assert.equal(identity?.firstName, "");
  assert.equal(identity?.lastName, "");
  assert.equal(identity?.email, "ana@example.com");
});

test("a Tax ID prefills as both halves or as neither", () => {
  const noNumber = checkoutIdentity({
    status: "ok",
    data: facts({ tax_id_number: null }),
  });
  assert.equal(noNumber?.taxIdType, null);
  assert.equal(noNumber?.taxIdNumber, null);

  const noType = checkoutIdentity({ status: "ok", data: facts({ tax_id_type: "  " }) });
  assert.equal(noType?.taxIdType, null);
  assert.equal(noType?.taxIdNumber, null);
});

test("a Customer with no stored phone prefills none", () => {
  const identity = checkoutIdentity({ status: "ok", data: facts({ phone: null }) });
  assert.equal(identity?.phone, null);
});

test("the boxes a Customer still owes travel with the identity, unaltered", () => {
  const owed = { policy_acceptance: true, marketing_consent: true, networking_consent: true };
  const identity = checkoutIdentity({ status: "ok", data: facts({ consent_boxes: owed }) });
  assert.deepEqual(identity?.consentBoxes, owed);
});
