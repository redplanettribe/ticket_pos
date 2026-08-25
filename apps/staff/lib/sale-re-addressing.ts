/**
 * The Sale Re-addressing panel's logic, kept out of the page so it can be
 * tested under `node --test` (#420, #423, ADR 0058).
 *
 * Three questions, all of which the page would otherwise answer inline: which
 * face the panel shows — nothing, the form, or the pending card — whether what
 * the operator typed is worth sending, and what "send again" on the pending
 * card sends. None re-decides anything the API decides: the API is the gate,
 * and a refusal from it is shown by code. These exist so the obvious cases
 * never make a round trip.
 */

import type {
  OperatorReAddressBody,
  OperatorSaleDetail,
  OperatorSaleReAddressing,
  OperatorSaleReAddressingBlock,
} from "./operator-api";

/** The longest note the API accepts on a Sale Re-addressing. */
export const RE_ADDRESSING_NOTE_MAX_LENGTH = 500;

/**
 * Which face the panel shows.
 *
 * `hidden` on a sale the lever is not offered on: anything but an active Online
 * Sale whose Event has not started (ADR 0058). `pending` while a recording
 * stands and its address has been mailed. `form` otherwise — the sale can be
 * re-addressed and nothing is pending.
 */
export type ReAddressingPanel =
  | { kind: "hidden" }
  | { kind: "form" }
  | { kind: "pending"; record: OperatorSaleReAddressing };

export function reAddressingPanel(
  sale: Pick<OperatorSaleDetail, "status" | "channel" | "event">,
  block: OperatorSaleReAddressingBlock | null | undefined,
  now: Date,
): ReAddressingPanel {
  if (sale.status !== "active" || sale.channel !== "online") {
    return { kind: "hidden" };
  }
  // The Event's start is an instant — its timezone is already inside it — so
  // "has it started" is one comparison, the same one the API makes. An Event
  // with no start cannot have started, but no Online Sale belongs to one.
  if (sale.event.starts_at !== null && new Date(sale.event.starts_at).getTime() <= now.getTime()) {
    return { kind: "hidden" };
  }
  if (block?.pending) {
    return { kind: "pending", record: block.pending };
  }
  return { kind: "form" };
}

/**
 * The accepted history: every re-addressing of this sale that completed, in
 * the order recorded. Listed WHATEVER face the panel shows — a reversed or
 * started sale offers no lever, but the evidence trail of who the sale was
 * moved to, by whom and when is permanent (#424, ADR 0058).
 */
export function acceptedReAddressings(
  block: OperatorSaleReAddressingBlock | null | undefined,
): OperatorSaleReAddressing[] {
  return block?.accepted ?? [];
}

/**
 * The same normalisation every address on the platform gets: trimmed and
 * lowercased. Used only to catch the one refusal the operator can see coming
 * without a round trip — "that is already the sale's address".
 */
export function normalizeEmail(raw: string): string {
  return raw.trim().toLowerCase();
}

/**
 * Why the typed address is not worth sending yet, or null when it is.
 *
 * The shape check is deliberately weak — an `@` with something on both sides —
 * because an address is only really validated by mail arriving at it, and the
 * API's own parse is the one that counts. What this catches is the empty field,
 * the address with no `@`, and the correction to what the sale already says.
 */
export type CorrectedEmailProblem = "required" | "invalid" | "same";

export function correctedEmailProblem(
  raw: string,
  currentEmail: string,
): CorrectedEmailProblem | null {
  const email = normalizeEmail(raw);
  if (email === "") {
    return "required";
  }
  const at = email.indexOf("@");
  if (at < 1 || at === email.length - 1 || email.includes(" ")) {
    return "invalid";
  }
  if (email === normalizeEmail(currentEmail)) {
    return "same";
  }
  return null;
}

/** The body the API takes: the address as typed (it normalises), the note only when there is one. */
export function reAddressBody(email: string, note: string): { email: string; note?: string } {
  const trimmedNote = note.trim();
  return { email: email.trim(), ...(trimmedNote ? { note: trimmedNote } : {}) };
}

/**
 * What "Send again" on the pending card sends (#423): the pending record's own
 * corrected address and note, recorded again. The API treats a recording made
 * while one is pending as a replacement — the earlier record is withdrawn and
 * its link killed, a fresh link is mailed — so a lost mail is one click to
 * recover and the address is never retyped. Null when the record's address has
 * been purged (the Event started; #424): there is nothing to send to, and the
 * panel is hidden on such a sale anyway.
 */
export function resendBody(record: OperatorSaleReAddressing): OperatorReAddressBody | null {
  if (record.corrected_email === null) {
    return null;
  }
  return { email: record.corrected_email, ...(record.note ? { note: record.note } : {}) };
}
