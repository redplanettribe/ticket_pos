import assert from "node:assert/strict";
import test from "node:test";

import {
  canReverseSale,
  exportFieldMessage,
  paymentMethodToken,
  reversalProvenance,
  saleChannelToken,
  saleSourceToken,
  taxIdSnapshot,
} from "./sales-api.ts";

/**
 * These assert DECISIONS, not prose. Every label this module used to build is
 * now a key in messages/{en,es}.json, so what is left to get wrong here is which
 * token a value narrows to and whether an unrecognised value survives to the
 * screen at all (#289, ADR 0041).
 *
 * The second half is the half worth testing. A Sales Channel or a Payment Method
 * the backend adds tomorrow must come back as null so the caller falls back to
 * the raw value — the same floor ADR 0023 puts under an unknown error code — and
 * a token silently invented for it would put an unrenderable catalog key on a
 * screen instead.
 */

// --- payment method -------------------------------------------------------

test("paymentMethodToken names every recorded Payment Method", () => {
  assert.equal(paymentMethodToken("cash"), "cash");
  assert.equal(paymentMethodToken("transfer"), "transfer");
  assert.equal(paymentMethodToken("payphone"), "payphone");
});

test("paymentMethodToken answers null for a sale without one", () => {
  assert.equal(paymentMethodToken(null), null);
});

test("paymentMethodToken answers null for a method this app cannot name", () => {
  // Null, so the caller shows the API's own word rather than a blank cell.
  assert.equal(paymentMethodToken("barter"), null);
});

// --- channel and source ---------------------------------------------------

test("saleChannelToken names every Sales Channel", () => {
  assert.equal(saleChannelToken("online"), "online");
  assert.equal(saleChannelToken("in_person"), "in_person");
  assert.equal(saleChannelToken("import"), "import");
});

test("saleSourceToken names a source and answers null when there is none", () => {
  assert.equal(saleSourceToken("direct"), "direct");
  assert.equal(saleSourceToken("external_platform"), "external_platform");
  assert.equal(saleSourceToken(null), null);
});

test("a channel this app has never heard of narrows to null", () => {
  assert.equal(saleChannelToken("kiosk"), null);
  assert.equal(saleSourceToken("resale"), null);
});

// --- Tax ID snapshot ------------------------------------------------------

test("taxIdSnapshot carries the Tax ID Type as a token beside its number", () => {
  assert.deepEqual(taxIdSnapshot("cedula", "1712345675"), {
    token: "cedula",
    rawType: "cedula",
    number: "1712345675",
  });
  assert.equal(taxIdSnapshot("ruc", "1790012346001")?.token, "ruc");
  assert.equal(taxIdSnapshot("passport", "XZ998877")?.token, "passport");
});

test("taxIdSnapshot is null for a sale recorded without a Tax ID", () => {
  assert.equal(taxIdSnapshot(null, null), null);
});

test("taxIdSnapshot is null rather than half a Tax ID", () => {
  assert.equal(taxIdSnapshot("cedula", null), null);
  assert.equal(taxIdSnapshot(null, "1712345675"), null);
});

test("a Tax ID Type this app cannot name keeps the API's own word", () => {
  assert.deepEqual(taxIdSnapshot("dni", "1712345675"), {
    token: null,
    rawType: "dni",
    number: "1712345675",
  });
});

// --- Sale Reversal provenance ---------------------------------------------

test("reversalProvenance carries when the sale went and which side asked", () => {
  assert.deepEqual(reversalProvenance("2026-07-07T12:00:00Z", "customer"), {
    state: "whenAndWho",
    at: "2026-07-07T12:00:00Z",
    actorToken: "customer",
    rawActor: "customer",
  });
  assert.equal(reversalProvenance("2026-07-07T12:00:00Z", "staff").state, "whenAndWho");
  assert.deepEqual(reversalProvenance("2026-07-07T12:00:00Z", "operator"), {
    state: "whenAndWho",
    at: "2026-07-07T12:00:00Z",
    actorToken: "operator",
    rawActor: "operator",
  });
});

test("a reversal with nothing recorded says so rather than guessing a time", () => {
  assert.deepEqual(reversalProvenance(null, null), { state: "unrecorded" });
});

test("a reversal whose moment is known but whose side is not carries the moment alone", () => {
  assert.deepEqual(reversalProvenance("2026-07-07T12:00:00Z", null), {
    state: "when",
    at: "2026-07-07T12:00:00Z",
  });
});

test("an actor this app cannot name keeps the API's own word", () => {
  assert.deepEqual(reversalProvenance("2026-07-07T12:00:00Z", "auditor"), {
    state: "whenAndWho",
    at: "2026-07-07T12:00:00Z",
    actorToken: null,
    rawActor: "auditor",
  });
});

// --- the Sales Export refusal ---------------------------------------------

// The one sentence on these surfaces that stays the API's English on purpose:
// it names how many sales matched and how many may travel, and those two numbers
// reach the app only inside the prose. See exportFieldMessage's comment.
test("exportFieldMessage finds the row cap's own sentence, not the generic one", () => {
  assert.equal(
    exportFieldMessage({
      fields: [
        {
          field: "filters",
          message:
            "This Event has 24,318 matching sales; up to 10,000 can be downloaded at once. Narrow the date range and try again.",
        },
      ],
    }),
    "This Event has 24,318 matching sales; up to 10,000 can be downloaded at once. Narrow the date range and try again.",
  );
});

test("exportFieldMessage answers null when the refusal carried no field sentence", () => {
  assert.equal(exportFieldMessage(null), null);
  assert.equal(exportFieldMessage(undefined), null);
  assert.equal(exportFieldMessage({}), null);
  assert.equal(exportFieldMessage({ fields: [] }), null);
  // A field error with a blank message is the same as none to a reader: the
  // caller falls through to the catalog rather than showing an empty alert.
  assert.equal(exportFieldMessage({ fields: [{ field: "filters", message: "   " }] }), null);
});

test("canReverseSale offers Reverse only to a sales manager, on an active imported sale", () => {
  const active = { channel: "import", status: "active" };
  assert.equal(canReverseSale(true, active), true);
  // Event Staff see the state and no lever.
  assert.equal(canReverseSale(false, active), false);
  // An Online Sale is the buyer's or the platform's to reverse; a door sale has no route.
  assert.equal(canReverseSale(true, { channel: "online", status: "active" }), false);
  assert.equal(canReverseSale(true, { channel: "in_person", status: "active" }), false);
  // A second press on a reversed sale is refused; the button goes first.
  assert.equal(canReverseSale(true, { channel: "import", status: "reversed" }), false);
});
