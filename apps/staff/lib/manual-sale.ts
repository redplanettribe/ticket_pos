/**
 * The record-a-sale form's state, body and verdict (#369, ADR 0052).
 *
 * A Manually Recorded Sale is one Sale Import row typed instead of uploaded, so
 * everything here is about ONE row: what the inputs hold, what body the save
 * sends, and how the preview's answer is read. It is a separate module from
 * sales-api.ts because that one is the Sales list's endpoints and tokens, and
 * this is a form's state machine with no fetch in it.
 *
 * TOKENS AND STATE, NEVER SENTENCES. Nothing here imports a catalog, React or
 * next-intl, which is what lets manual-sale.test.ts assert a decision under
 * `node --test` — the staff app has no component tests and adds none, so a rule
 * that is not a pure function here is a rule nothing defends.
 */

// Relative (not "@/lib") so the module graph resolves under `node --test` as
// well as the bundler — the unit tests import this file directly.
import { dateTimeLocalToISO } from "./events-api.ts";
import type { ImportPreviewResult } from "./imports-api.ts";
import type { RecordSaleInput } from "./sales-api.ts";

/**
 * The form as typed: every template column a string, because that is what an
 * input holds. The sale date is a datetime-local value in the EVENT's zone and
 * the amount is a price per ticket in major units; both are converted on the way
 * out and never before, so nothing the organizer typed is rewritten under them.
 */
export type ManualSaleForm = {
  customerEmail: string;
  customerFirstName: string;
  customerLastName: string;
  taxIdType: string;
  taxIdNumber: string;
  ticketTypeId: string;
  quantity: string;
  paymentMethod: string;
  soldAtLocal: string;
  unitPrice: string;
};

/**
 * The template column each field's complaints arrive under.
 *
 * The API names BARE columns — no `rows[N].` prefix, the convention for a single
 * typed row — and the form files each complaint under the input that caused it.
 * `ticketTypeId` is the one asymmetry: the body sends `ticket_type_id` and a
 * refusal blames `ticket_type`, the name the uploaded template prints.
 */
export const MANUAL_SALE_COLUMNS = {
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
} as const satisfies Record<keyof ManualSaleForm, string>;

/**
 * A blank form on the given sale date, which the caller draws as today on the
 * EVENT's clock — recording the sale just taken should need no thought, and the
 * reader's laptop is never asked what day it is.
 *
 * Quantity starts at one, the sale a person types by hand. The amount starts
 * blank, which means the Ticket Type's own price: the safe default and the
 * common case.
 */
export function emptyManualSaleForm(soldAtLocal: string): ManualSaleForm {
  return {
    customerEmail: "",
    customerFirstName: "",
    customerLastName: "",
    taxIdType: "",
    taxIdNumber: "",
    ticketTypeId: "",
    quantity: "1",
    paymentMethod: "",
    soldAtLocal,
    unitPrice: "",
  };
}

/**
 * manualSaleQuantity reads the quantity cell into the number the body carries.
 *
 * It does not round and it does not judge. A fractional quantity travels as
 * typed, because the API answers 1.5 on the quantity column in the import
 * validator's own words, and rounding it here would record a sale nobody typed.
 * Anything unreadable travels as 0, which the API already refuses on the same
 * column. The form judges nothing itself (ADR 0052): every complaint on screen
 * came from the server.
 */
function manualSaleQuantity(raw: string): number {
  const quantity = Number(raw.trim());
  return Number.isFinite(quantity) ? quantity : 0;
}

/**
 * manualSaleAmount reads the amount cell into cents, or null for a blank one.
 *
 * Blank is null and means the Ticket Type's catalog price. Zero is zero and
 * records a comp — collapsing it into blank would charge the catalog price for
 * a sale the organizer stated was free.
 *
 * The amount is PER TICKET, as it is in the uploaded template and in the Sale
 * Correction, and the label says so.
 */
function manualSaleAmount(raw: string): number | null {
  if (raw.trim() === "") {
    return null;
  }
  const price = Number(raw);
  return Number.isFinite(price) ? Math.round(price * 100) : null;
}

/**
 * manualSaleBody is the ONE reading of the form the API sees. The live preview
 * and the save send exactly this, so the verdict shown is the verdict recorded
 * on — the same guarantee the Sale Correction's form makes (#352).
 *
 * The zone is the Event's: a sale date typed as a wall-clock moment is only an
 * instant once somebody says whose clock it was on.
 */
export function manualSaleBody(form: ManualSaleForm, zone: string): RecordSaleInput {
  return {
    customer_email: form.customerEmail.trim(),
    customer_first_name: form.customerFirstName.trim(),
    customer_last_name: form.customerLastName.trim(),
    customer_tax_id_type: form.taxIdType,
    customer_tax_id_number: form.taxIdNumber.trim(),
    ticket_type_id: form.ticketTypeId,
    quantity: manualSaleQuantity(form.quantity),
    payment_method: form.paymentMethod,
    sold_at: dateTimeLocalToISO(form.soldAtLocal, zone) ?? "",
    amount_cents: manualSaleAmount(form.unitPrice),
  };
}

/** What the form shows of a preview verdict, and whether it may be saved. */
export type ManualSaleVerdict = {
  // True when the save would be refused: the row is invalid, the Ticket Type is
  // oversold or over the Purchase Limit, or the preview judged no row at all.
  blocks: boolean;
  // The row's complaints by template column, first complaint per column.
  fieldErrors: Record<string, string>;
  // The sold-at date of another active sale this one matches, or null. A
  // warning: it never blocks.
  duplicateOfDate: string | null;
  // What the chosen Ticket Type has left once this sale is recorded, or null
  // when the preview named no Ticket Type.
  remaining: { ticketTypeName: string; remaining: number } | null;
};

/**
 * manualSaleVerdict reads a preview into what the form needs to decide.
 *
 * Stated here rather than borrowed from the Sale Correction's reading of the
 * same shape, because the two answer different questions with it: a correction's
 * capacity is netted against the sale being reversed, and this one's is the
 * plain remainder on an Event nothing is being given back to. The arithmetic
 * agreeing today is a coincidence of that netting being zero here.
 *
 * The duplicate never blocks. Everything the API would refuse does.
 */
export function manualSaleVerdict(result: ImportPreviewResult): ManualSaleVerdict {
  const row = result.rows[0];
  if (!row) {
    return { blocks: true, fieldErrors: {}, duplicateOfDate: null, remaining: null };
  }
  const fieldErrors: Record<string, string> = {};
  for (const complaint of row.errors ?? []) {
    if (!(complaint.field in fieldErrors)) {
      fieldErrors[complaint.field] = complaint.message;
    }
  }
  const impact = result.capacity_impact[0];
  return {
    blocks: !row.valid || !result.committable,
    fieldErrors,
    duplicateOfDate: row.possible_duplicate && row.duplicate_of_date ? row.duplicate_of_date : null,
    remaining: impact
      ? { ticketTypeName: impact.ticket_type_name, remaining: impact.remaining - impact.requested }
      : null,
  };
}

/**
 * The form to hand back after a sale is recorded and the sitting continues.
 *
 * The keep-adding session itself is #372 and is deliberately not built here.
 * This rule is, because it is a fact about THIS form's state — which of its
 * cells belong to the buyer and which to the sitting — and #372 wraps the form
 * rather than reaching inside it.
 *
 * The buyer, their Tax ID and the amount clear; the Ticket Type, Payment Method
 * and sale date stay, because twenty door sales from the same night are the same
 * three answers twenty times. The quantity returns to one and the amount clears
 * rather than persisting: a stale amount is the one sticky value that would
 * record the wrong money while looking correct.
 */
export function manualSaleFormAfterSave(form: ManualSaleForm): ManualSaleForm {
  return {
    ...emptyManualSaleForm(form.soldAtLocal),
    ticketTypeId: form.ticketTypeId,
    paymentMethod: form.paymentMethod,
  };
}
