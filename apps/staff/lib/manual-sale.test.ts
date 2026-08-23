import assert from "node:assert/strict";
import test from "node:test";

import {
  MANUAL_SALE_COLUMNS,
  emptyManualSaleForm,
  manualSaleBody,
  manualSaleFormAfterSave,
  manualSaleVerdict,
} from "./manual-sale.ts";
import type { ImportPreviewResult } from "./imports-api.ts";

/**
 * The staff app has no component tests and adds none (ADR 0052's testing
 * decisions), so everything about the record-a-sale form that can be got wrong
 * without a browser lives here: what body the save sends, what the preview's
 * verdict is read as, and which values a keep-adding save carries over.
 *
 * The prose the reader sees is not tested and is not testable here — this module
 * returns tokens and state, and messages/{en,es}.json says it in their language.
 */

// The Event's own zone, five hours behind UTC and with no daylight saving:
// the platform's home zone, and the one every date on this surface is drawn in.
const ZONE = "America/Guayaquil";

function filledForm() {
  return {
    ...emptyManualSaleForm("2026-07-01T10:00"),
    customerEmail: "  buyer@example.com ",
    customerFirstName: " Ana ",
    customerLastName: " Torres ",
    taxIdType: "cedula",
    taxIdNumber: " 1712345678 ",
    ticketTypeId: "tt-1",
    quantity: "2",
    paymentMethod: "cash",
    unitPrice: "45.00",
  };
}

// --- the starting state ---------------------------------------------------

test("emptyManualSaleForm starts on the sale date it is handed, one ticket, nothing else", () => {
  const form = emptyManualSaleForm("2026-07-01T10:00");
  assert.equal(form.soldAtLocal, "2026-07-01T10:00");
  // One ticket is the sale a person types by hand; every other cell is theirs
  // to fill, and a blank amount means the Ticket Type's own price.
  assert.equal(form.quantity, "1");
  assert.equal(form.customerEmail, "");
  assert.equal(form.customerFirstName, "");
  assert.equal(form.customerLastName, "");
  assert.equal(form.taxIdType, "");
  assert.equal(form.taxIdNumber, "");
  assert.equal(form.ticketTypeId, "");
  assert.equal(form.paymentMethod, "");
  assert.equal(form.unitPrice, "");
});

test("every form field names the template column its complaint arrives under", () => {
  // The API answers with the template's BARE column names, and the form has to
  // put each complaint under the input that caused it. ticket_type is the one
  // that is not the field's own name: the body sends ticket_type_id and the
  // refusal blames ticket_type.
  assert.deepEqual(MANUAL_SALE_COLUMNS, {
    customerEmail: "customer_email",
    customerFirstName: "customer_first_name",
    customerLastName: "customer_last_name",
    taxIdType: "customer_tax_id_type",
    taxIdNumber: "customer_tax_id_number",
    ticketTypeId: "ticket_type",
    quantity: "quantity",
    paymentMethod: "payment_method",
    soldAtLocal: "sold_at",
    unitPrice: "amount",
  });
  // Every key of the form is spoken for: a field with no column would show its
  // complaints nowhere.
  assert.deepEqual(
    Object.keys(MANUAL_SALE_COLUMNS).sort(),
    Object.keys(emptyManualSaleForm("")).sort(),
  );
});

// --- the body ------------------------------------------------------------

test("manualSaleBody sends the typed values trimmed, with the amount in cents", () => {
  const body = manualSaleBody(filledForm(), ZONE);
  assert.deepEqual(body, {
    customer_email: "buyer@example.com",
    customer_first_name: "Ana",
    customer_last_name: "Torres",
    customer_tax_id_type: "cedula",
    customer_tax_id_number: "1712345678",
    ticket_type_id: "tt-1",
    quantity: 2,
    payment_method: "cash",
    sold_at: "2026-07-01T15:00:00.000Z",
    amount_cents: 4500,
  });
});

test("manualSaleBody reads the sale date on the Event's clock, never the reader's", () => {
  // The same typed moment, two Events: the instant sent differs by the zones'
  // offsets, which is the whole reason the zone is a parameter.
  const form = { ...filledForm(), soldAtLocal: "2026-07-01T10:00" };
  assert.equal(manualSaleBody(form, ZONE).sold_at, "2026-07-01T15:00:00.000Z");
  assert.equal(manualSaleBody(form, "UTC").sold_at, "2026-07-01T10:00:00.000Z");
});

test("manualSaleBody sends a blank amount as null, so the Ticket Type's price stands", () => {
  assert.equal(manualSaleBody({ ...filledForm(), unitPrice: "  " }, ZONE).amount_cents, null);
});

test("manualSaleBody sends a zero amount as zero, which records a comp", () => {
  // Not null: a comped sale is a stated price of nothing, and collapsing it to
  // blank would silently charge the catalog price instead.
  assert.equal(manualSaleBody({ ...filledForm(), unitPrice: "0" }, ZONE).amount_cents, 0);
});

test("manualSaleBody rounds an amount to whole cents", () => {
  assert.equal(manualSaleBody({ ...filledForm(), unitPrice: "12.345" }, ZONE).amount_cents, 1235);
});

test("manualSaleBody sends an unreadable quantity as zero for the API to refuse", () => {
  // The form judges nothing itself: an empty or nonsense quantity travels as a
  // number the API already has a complaint for, on the quantity column.
  assert.equal(manualSaleBody({ ...filledForm(), quantity: "" }, ZONE).quantity, 0);
  assert.equal(manualSaleBody({ ...filledForm(), quantity: "abc" }, ZONE).quantity, 0);
});

test("manualSaleBody sends a fractional quantity as typed, for the API to name", () => {
  // 1.5 is refused on the quantity column by the API in the import validator's
  // own words. Rounding it here would record a sale the organizer did not type.
  assert.equal(manualSaleBody({ ...filledForm(), quantity: "1.5" }, ZONE).quantity, 1.5);
});

test("manualSaleBody sends an empty sale date as empty, not as now", () => {
  // Defaulting to the current instant would record a sale on a day nobody
  // chose; the API refuses a missing sold_at on its own column.
  assert.equal(manualSaleBody({ ...filledForm(), soldAtLocal: "" }, ZONE).sold_at, "");
});

// --- the verdict ---------------------------------------------------------

function previewOf(row: Partial<ImportPreviewResult["rows"][number]>, rest: Partial<ImportPreviewResult> = {}): ImportPreviewResult {
  return {
    rows: [
      {
        row: 1,
        customer_email: "buyer@example.com",
        customer_first_name: "Ana",
        customer_last_name: "Torres",
        ticket_type: "General",
        quantity: 2,
        payment_method: "cash",
        valid: true,
        ...row,
      },
    ],
    capacity_impact: [],
    valid_rows: 1,
    total_rows: 1,
    committable: true,
    ...rest,
  };
}

test("manualSaleVerdict lets a valid row through", () => {
  const verdict = manualSaleVerdict(previewOf({}));
  assert.equal(verdict.blocks, false);
  assert.deepEqual(verdict.fieldErrors, {});
  assert.equal(verdict.duplicateOfDate, null);
});

test("manualSaleVerdict blocks an invalid row and files each complaint by column", () => {
  const verdict = manualSaleVerdict(
    previewOf(
      {
        valid: false,
        errors: [
          { field: "customer_email", message: "is not a valid email" },
          { field: "quantity", message: "must be greater than zero" },
        ],
      },
      { valid_rows: 0, committable: false },
    ),
  );
  assert.equal(verdict.blocks, true);
  assert.deepEqual(verdict.fieldErrors, {
    customer_email: "is not a valid email",
    quantity: "must be greater than zero",
  });
});

test("manualSaleVerdict keeps the first complaint per column", () => {
  const verdict = manualSaleVerdict(
    previewOf({
      valid: false,
      errors: [
        { field: "quantity", message: "must be greater than zero" },
        { field: "quantity", message: "exceeds the remaining capacity" },
      ],
    }),
  );
  assert.equal(verdict.fieldErrors.quantity, "must be greater than zero");
});

test("manualSaleVerdict blocks an uncommittable preview even when the row reads valid", () => {
  // Capacity is decided over the whole preview, not on the row: a row that
  // passes every field rule can still be refused for a Ticket Type that is full.
  const verdict = manualSaleVerdict(previewOf({}, { committable: false }));
  assert.equal(verdict.blocks, true);
});

test("manualSaleVerdict blocks a preview that judged no row at all", () => {
  const verdict = manualSaleVerdict(previewOf({}, { rows: [], total_rows: 0, valid_rows: 0 }));
  assert.equal(verdict.blocks, true);
  assert.deepEqual(verdict.fieldErrors, {});
});

test("manualSaleVerdict reports a duplicate without blocking on it", () => {
  // Two people at the door with the same name on the same night is a thing that
  // happens; the warning is the organizer's to overrule.
  const verdict = manualSaleVerdict(
    previewOf({ possible_duplicate: true, duplicate_of_date: "2026-07-01" }),
  );
  assert.equal(verdict.blocks, false);
  assert.equal(verdict.duplicateOfDate, "2026-07-01");
});

test("manualSaleVerdict states no duplicate when the API named no date for it", () => {
  const verdict = manualSaleVerdict(previewOf({ possible_duplicate: true }));
  assert.equal(verdict.duplicateOfDate, null);
});

test("manualSaleVerdict says what the Ticket Type has left once the sale is recorded", () => {
  const verdict = manualSaleVerdict(
    previewOf(
      {},
      {
        capacity_impact: [
          {
            ticket_type_id: "tt-1",
            ticket_type_name: "General",
            requested: 2,
            sold_count: 8,
            capacity: 12,
            remaining: 4,
            overage: 0,
            oversold: false,
          },
        ],
      },
    ),
  );
  assert.deepEqual(verdict.remaining, { ticketTypeName: "General", remaining: 2 });
});

test("manualSaleVerdict names no remainder when the preview named no Ticket Type", () => {
  assert.equal(manualSaleVerdict(previewOf({})).remaining, null);
});

// --- the next sale in a sitting -------------------------------------------

test("manualSaleFormAfterSave clears the buyer so the last one cannot be recorded twice", () => {
  const next = manualSaleFormAfterSave(filledForm());
  assert.equal(next.customerEmail, "");
  assert.equal(next.customerFirstName, "");
  assert.equal(next.customerLastName, "");
  assert.equal(next.taxIdType, "");
  assert.equal(next.taxIdNumber, "");
});

test("manualSaleFormAfterSave leaves the Ticket Type, Payment Method and sale date alone", () => {
  // Twenty door sales from the same night are the same three answers twenty
  // times, and re-picking them is the friction the sitting exists to remove.
  const next = manualSaleFormAfterSave(filledForm());
  assert.equal(next.ticketTypeId, "tt-1");
  assert.equal(next.paymentMethod, "cash");
  assert.equal(next.soldAtLocal, "2026-07-01T10:00");
});

test("manualSaleFormAfterSave returns the quantity to one and clears the amount", () => {
  // The amount is the one sticky value that would record the wrong money while
  // looking right, and a stale quantity would record the wrong count.
  const next = manualSaleFormAfterSave(filledForm());
  assert.equal(next.quantity, "1");
  assert.equal(next.unitPrice, "");
});

test("manualSaleFormAfterSave does not touch the form it was handed", () => {
  const form = filledForm();
  manualSaleFormAfterSave(form);
  assert.equal(form.customerEmail, "  buyer@example.com ");
  assert.equal(form.quantity, "2");
});
