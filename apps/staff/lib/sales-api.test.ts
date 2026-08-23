import assert from "node:assert/strict";
import test from "node:test";

import {
  canCorrectSale,
  canReverseSale,
  correctionFieldErrors,
  correctionPrefill,
  correctionVerdict,
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

test("canCorrectSale is Reverse's gate: a sales manager, on an active imported sale", () => {
  assert.equal(canCorrectSale(true, { channel: "import", status: "active" }), true);
  assert.equal(canCorrectSale(false, { channel: "import", status: "active" }), false);
  assert.equal(canCorrectSale(true, { channel: "online", status: "active" }), false);
  // A corrected sale is a reversed sale: it cannot be corrected twice.
  assert.equal(canCorrectSale(true, { channel: "import", status: "reversed" }), false);
});

test("correctionPrefill reads the template's columns off the row, blanks for a missing Tax ID, and never pre-ticks the confirmation", () => {
  const prefill = correctionPrefill({
    id: "s1",
    customer_first_name: "Ana",
    customer_last_name: "Lopez",
    customer_email: "ana@example.com",
    ticket_types: [{ ticket_type_id: "tt1", ticket_type_name: "GA", quantity: 2 }],
    amount_cents: 2000,
    currency: "USD",
    sold_at: "2026-07-01T10:00:00Z",
    channel: "import",
    source: "direct",
    status: "active",
    confirmation_ref: "TP-1",
    recorded_at: "2026-07-01T10:00:00Z",
    payment_method: "cash",
    tax_id_type: null,
    tax_id_number: null,
    reversed_at: null,
    reversed_by: null,
    replaced_by_sale_id: null,
    replaces_sale_id: null,
    replaced_by_confirmation_ref: null,
    replaces_confirmation_ref: null,
    held_ticket_count: 0,
  });
  assert.deepEqual(prefill, {
    customer_email: "ana@example.com",
    customer_first_name: "Ana",
    customer_last_name: "Lopez",
    customer_tax_id_type: "",
    customer_tax_id_number: "",
    ticket_type_id: "tt1",
    quantity: 2,
    payment_method: "cash",
    sold_at: "2026-07-01T10:00:00Z",
    amount_cents: 2000,
    send_confirmation: false,
  });
});

test("correctionFieldErrors keys the refusal's complaints by column, first complaint wins, and is empty for anything else", () => {
  assert.deepEqual(
    correctionFieldErrors({
      fields: [
        { field: "quantity", message: "is over the Purchase Limit" },
        { field: "quantity", message: "second complaint" },
        { field: "customer_email", message: "must be a valid email" },
      ],
    }),
    { quantity: "is over the Purchase Limit", customer_email: "must be a valid email" },
  );
  assert.deepEqual(correctionFieldErrors(undefined), {});
  assert.deepEqual(correctionFieldErrors({ channel: "online" }), {});
});

// --- the live verdict (#352) ----------------------------------------------

test("correctionVerdict: a refusing verdict blocks with its complaints by column; a duplicate is a warning that does not", () => {
  const refused = correctionVerdict({
    rows: [
      {
        row: 1,
        customer_email: "ana@example.com",
        customer_first_name: "Ana",
        customer_last_name: "Lopez",
        ticket_type: "x",
        quantity: 3,
        payment_method: "cash",
        valid: false,
        errors: [
          { field: "quantity", message: "exceeds the 2 remaining" },
          { field: "quantity", message: "second" },
          { field: "ticket_type", message: "does not match" },
        ],
      },
    ],
    capacity_impact: [],
    valid_rows: 0,
    total_rows: 1,
    committable: false,
  });
  assert.equal(refused.blocks, true);
  assert.deepEqual(refused.fieldErrors, { quantity: "exceeds the 2 remaining", ticket_type: "does not match" });
  assert.equal(refused.duplicateOfDate, null);

  const warned = correctionVerdict({
    rows: [
      {
        row: 1,
        customer_email: "bob@example.com",
        customer_first_name: "Bob",
        customer_last_name: "Ng",
        ticket_type: "x",
        quantity: 1,
        payment_method: "cash",
        valid: true,
        possible_duplicate: true,
        duplicate_of_date: "2026-07-02",
      },
    ],
    capacity_impact: [
      { ticket_type_id: "x", ticket_type_name: "GA", requested: 1, sold_count: 4, capacity: 5, remaining: 1, overage: 0, oversold: false },
    ],
    valid_rows: 1,
    total_rows: 1,
    committable: true,
  });
  assert.equal(warned.blocks, false);
  assert.deepEqual(warned.fieldErrors, {});
  assert.equal(warned.duplicateOfDate, "2026-07-02");
  assert.deepEqual(warned.remaining, { ticketTypeName: "GA", remaining: 0 });

  // A verdict the commit would refuse for capacity even with a valid row
  // (the oversold flag) blocks too; and no rows at all is no verdict.
  assert.equal(
    correctionVerdict({ rows: [], capacity_impact: [], valid_rows: 0, total_rows: 0, committable: false }).blocks,
    true,
  );
});
