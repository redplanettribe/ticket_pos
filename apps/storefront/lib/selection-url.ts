/**
 * The ticket selection a buyer had chosen before they could be asked who they
 * are (ADR 0054, #383).
 *
 * Checkout begins signed in, so pressing Buy while signed out sends the buyer to
 * the sign-in page and — via Google Sign-In — off this origin entirely. What
 * they had put in the basket has to survive that trip, and it does the same way
 * a Follow intent does (follow-intent.ts): as ONE STRING travelling in the open,
 * on the Event page address that `next` names, which the sign-in page already
 * relays and which the Google state cookie already carries across the callback.
 * No new cookie, no new expiry policy, and an address a person can read.
 *
 * IT IS A SUGGESTION, NEVER A COMMAND. The address is forgeable, shareable and
 * arbitrarily old, and it grants nothing: it names Ticket Types and quantities
 * and CANNOT SPELL A PRICE, A DISCOUNT OR A CAPACITY. On arrival the quantities
 * are re-judged against live availability, the Purchase Limit (ADR 0025) and
 * whatever the API has just priced this Event's Ticket Types at — including a
 * Promotion that has since ended (ADR 0021) — by exactly the arithmetic the
 * steppers use when a buyer clicks them by hand (checkout.ts). A link naming a
 * sold-out Ticket Type therefore restores what it can and REPORTS what it could
 * not, rather than quietly showing a total the buyer never pressed Buy on.
 *
 * This module ships inert: nothing writes such an address yet (#385 does), and a
 * buyer arriving without one gets an empty selection and the page they have
 * always seen.
 */

// The ".ts" is written out because the unit tests run this module directly under
// `node --experimental-strip-types`, which resolves specifiers exactly. Next
// resolves it identically.
import { clampQuantity, offerableQuantity, type SellableTicketType } from "./checkout.ts";

/** The query parameter a selection travels under. The only place that decides. */
export const SELECTION_PARAM = "sel";

/** Between one Ticket Type's entry and the next, and never inside either. */
const ENTRY_SEPARATOR = ",";

/** Between a Ticket Type and its quantity, and never inside either. */
const FIELD_SEPARATOR = ":";

/**
 * The shape a Ticket Type id must have.
 *
 * The platform issues UUIDs, and this admits them plus any plain slug. What it
 * does NOT admit is the point: no "/", ":", ",", ".", "@" or whitespace, which
 * makes a URL, a protocol-relative host, a path traversal and an email address
 * unspellable in the id position. An id that fails this is not a Ticket Type
 * this platform ever named.
 */
const ID_PATTERN = /^[A-Za-z0-9_-]{1,64}$/;

/**
 * Ids that are not ids at all but names on Object.prototype.
 *
 * A decoded selection is a plain object keyed by whatever the address said, and
 * `obj["__proto__"] = 2` is not an assignment, it is a mutation of the object's
 * prototype. Refused by name rather than defended against downstream, because
 * this is the one place that turns a stranger's string into an object key, and
 * no id this platform issues looks like any of these.
 */
const RESERVED_IDS = new Set(["__proto__", "constructor", "prototype"]);

/**
 * The shape a quantity must have: a positive decimal integer, at most four
 * digits, written without sign, padding or exponent.
 *
 * Zero is refused rather than read as "none of these": a selection states what
 * was chosen, and encodeSelection never writes an entry for a Ticket Type
 * nobody picked. The four-digit cap is a bound on the string, not a rule about
 * baskets — the real bounds are capacity and the Purchase Limit, and they are
 * applied on arrival by restoreSelection.
 */
const QUANTITY_PATTERN = /^[1-9][0-9]{0,3}$/;

/** The largest quantity the format can spell, and what encoding clamps to. */
const MAX_QUANTITY = 9999;

/** More Ticket Types than any Event on this platform sells. A bound, not a rule. */
const MAX_ENTRIES = 40;

/** Comfortably inside every browser's address limit, given the bounds above. */
const MAX_ENCODED_LENGTH = 2048;

/**
 * What could not be restored, as facts rather than as a sentence.
 *
 * `requested` is what the address asked for and `restored` is what survived
 * re-judging — zero when the Ticket Type was dropped altogether. `reason` names
 * WHICH rule cut it, because the three read completely differently to a buyer: a
 * sold-out Ticket Type is a fact about the Event that everyone sees, a closed one
 * is a decision the Organization took about time rather than stock, and a spent
 * Purchase Limit is a fact about this Customer alone, which they must never read
 * as the Event being full (ADR 0025, ADR 0070).
 *
 * No words here. The copy is looked up at render from `reason`, so state never
 * holds a sentence a language switch would strand.
 */
export type SelectionAdjustment = {
  ticketTypeId: string;
  requested: number;
  restored: number;
  reason: "sold_out" | "closed" | "capacity" | "purchase_limit";
};

/** A re-judged selection: what the steppers get, and what the buyer is owed. */
export type RestoredSelection = {
  /** Quantities by Ticket Type id, exactly as the steppers hold them. */
  selection: Record<string, number>;
  /**
   * Every known Ticket Type that could not be restored in full, in the Event's
   * own Ticket Type order. Empty is the ordinary case and draws nothing.
   */
  adjustments: SelectionAdjustment[];
};

/**
 * Nothing arrived, or nothing survived. Spread at every use rather than handed
 * out, so no two callers end up holding one basket.
 */
const EMPTY: Record<string, number> = {};

/**
 * encodeSelection spells a selection as the one string that travels.
 *
 * Ordered by Ticket Type id so one basket is always one string — a stable
 * address is cacheable, comparable in a log and diffable by a person. Anything
 * that is not a chosen whole quantity of a spellable id is dropped, and an empty
 * selection encodes to the empty string, which callers should leave off the
 * address entirely rather than write as `sel=`.
 *
 * It will never emit a string decodeSelection refuses; that is asserted rather
 * than assumed, because a producer and consumer that disagree lose real baskets.
 */
export function encodeSelection(selection: Record<string, number>): string {
  return Object.entries(selection)
    // A whole quantity or nothing: a fraction is a caller's bug, and rounding
    // one would put a basket on the address that nobody chose.
    .filter(([id, quantity]) => spellableId(id) && Number.isInteger(quantity) && quantity >= 1)
    .map(([id, quantity]) => [id, Math.min(quantity, MAX_QUANTITY)] as const)
    .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0))
    .slice(0, MAX_ENTRIES)
    .map(([id, quantity]) => `${id}${FIELD_SEPARATOR}${quantity}`)
    .join(ENTRY_SEPARATOR);
}

function spellableId(id: string): boolean {
  return ID_PATTERN.test(id) && !RESERVED_IDS.has(id);
}

/**
 * decodeSelection reads what the address claims was chosen. IT NEVER THROWS.
 *
 * Every rejection yields an EMPTY selection, and rejection is all-or-nothing:
 * one malformed, duplicated or oversized entry discards the whole string rather
 * than the entry. That is deliberate. A half-read address is the one outcome
 * this feature must not produce — it would put a basket in front of the buyer
 * that neither they nor the link ever asked for, and the difference would be
 * invisible. An honest address never contains a malformed entry, because
 * encodeSelection cannot write one, so nobody with a real basket pays for this
 * strictness; only a mangled or hand-crafted string does, and it loses
 * everything, which is the safe direction.
 *
 * The `string[]` arm is Next's shape for a repeated query parameter. Two
 * selections is no selection, so it is refused rather than picked between.
 *
 * What comes back is a claim about Ticket Types and quantities and nothing else.
 * It has been judged against the FORMAT and against no rule of this business:
 * pass it to restoreSelection before showing it to anybody.
 */
export function decodeSelection(raw: string | string[] | null | undefined): Record<string, number> {
  if (typeof raw !== "string") {
    return { ...EMPTY };
  }
  const value = raw.trim();
  if (value === "" || value.length > MAX_ENCODED_LENGTH) {
    return { ...EMPTY };
  }

  const parts = value.split(ENTRY_SEPARATOR);
  if (parts.length > MAX_ENTRIES) {
    return { ...EMPTY };
  }

  const entries: [string, number][] = [];
  const seen = new Set<string>();
  for (const part of parts) {
    const separator = part.indexOf(FIELD_SEPARATOR);
    if (separator < 0) {
      return { ...EMPTY };
    }
    const id = part.slice(0, separator);
    const quantity = part.slice(separator + 1);
    // A second separator is a third field somebody is appending to a format
    // that has two, and the honest reading of the whole string is that it is
    // not a selection.
    if (quantity.includes(FIELD_SEPARATOR)) {
      return { ...EMPTY };
    }
    if (!spellableId(id) || !QUANTITY_PATTERN.test(quantity)) {
      return { ...EMPTY };
    }
    // Two statements about one Ticket Type, with no honest way to pick between
    // them. Summing them would inflate a basket on a stranger's say-so.
    if (seen.has(id)) {
      return { ...EMPTY };
    }
    seen.add(id);
    entries.push([id, Number.parseInt(quantity, 10)]);
  }
  // fromEntries rather than assignment into a literal: it defines own
  // properties, so no key can reach a prototype even if RESERVED_IDS is ever
  // loosened.
  return Object.fromEntries(entries);
}

/**
 * restoreSelection re-judges a claimed selection against the Event as it is NOW,
 * and reports whatever it could not honour.
 *
 * The judging is not a second implementation of the rules: it is clampQuantity,
 * the same function the steppers call on every click, so an address can never
 * put a quantity in the basket that a finger could not have (checkout.ts,
 * ADR 0025). The Event's own Ticket Types are what is iterated, so an id this
 * Event does not have is ignored without error and without comment — we cannot
 * name a Ticket Type we do not have, and a forged id is not news a buyer can act
 * on.
 *
 * NO PRICE IS CARRIED OR CHECKED, which is how a Promotion that has expired
 * comes to be not honoured: the only prices in play are the ones the API just
 * quoted on this page load, and the running total is built from those (ADR
 * 0021). An address made while a Promotion was live restores the same
 * quantities at today's price, and there is nothing in the format for it to
 * claim otherwise with.
 */
export function restoreSelection(
  ticketTypes: SellableTicketType[],
  requested: Record<string, number>,
): RestoredSelection {
  const selection: Record<string, number> = {};
  const adjustments: SelectionAdjustment[] = [];

  for (const ticketType of ticketTypes) {
    const asked = requested[ticketType.id] ?? 0;
    if (!(asked > 0)) {
      continue;
    }
    // A closed Ticket Type restores nothing whatever its stock says: the API
    // refuses that line at begin-checkout, so putting it back would hand the
    // buyer a basket that cannot be paid for (ADR 0070). clampQuantity is what
    // says so — the cut used to be made here instead, while the steppers were
    // still drawn for a closed Ticket Type and zero would have left a `+` that
    // was clickable and did nothing. Withdrawing the control (#607) put the rule
    // where the rest of the clamping lives, and this line inherits it.
    const restored = clampQuantity(asked, ticketType);
    if (restored > 0) {
      selection[ticketType.id] = restored;
    }
    if (restored < asked) {
      adjustments.push({
        ticketTypeId: ticketType.id,
        requested: asked,
        restored,
        reason: cutBy(ticketType),
      });
    }
  }

  return { selection, adjustments };
}

/**
 * Which rule cut a quantity down, worded as the four things a buyer can be told
 * apart.
 *
 * The order is the one both apps rank these states in — sold out, then closed,
 * then the Purchase Limit — and a disagreement between them is a bug rather than
 * a matter of taste (ADR 0070). Sold out wins outright: it is the Event's own
 * state, the only one the whole page already says, and the stronger fact, since
 * an Organization can reopen a window but cannot conjure a seat. Closed comes
 * next, and never shares a sentence with sold out, because a Customer told an
 * Event is full when it is half empty is exactly the confusion these separate
 * words exist to prevent. Otherwise the Purchase Limit is blamed only when it is
 * genuinely the binding bound — offerableQuantity narrower than remaining
 * capacity is exactly that condition — so a Customer is never told they have
 * reached a limit when what actually ran out was stock.
 *
 * That ranking is the CARD's, and is deliberately the inverse of the order
 * begin-checkout refuses in, where the closed code beats the sold-out one. The
 * two answer different questions: this describes a Ticket Type to somebody still
 * reading the page, where the stronger fact about stock is the more useful one,
 * while a refusal explains why an act was not performed, where the
 * Organization's own decision is the part a buyer can do something about.
 *
 * The verdict comes from the payload the server just sent and never from a
 * closing instant compared here: the server owns the clock, and a browser set to
 * yesterday must not be able to restore a basket the API will refuse. That is
 * also why this branch needs no new field on the refusal — the reason is decided
 * from the Event read, not from what begin-checkout would have said.
 */
function cutBy(ticketType: SellableTicketType): SelectionAdjustment["reason"] {
  if (ticketType.sold_out) {
    return "sold_out";
  }
  if (ticketType.closed) {
    return "closed";
  }
  return offerableQuantity(ticketType) < Math.max(ticketType.remaining, 0)
    ? "purchase_limit"
    : "capacity";
}

/**
 * restoreSelectionFromParam is the whole arrival in one call: read the address,
 * then re-judge it. What the Event page uses.
 *
 * Arriving with no parameter, or with junk, produces an empty selection and an
 * empty report — which is byte-identical to arriving at the page as it was
 * before this existed.
 */
export function restoreSelectionFromParam(
  ticketTypes: SellableTicketType[],
  raw: string | string[] | null | undefined,
): RestoredSelection {
  return restoreSelection(ticketTypes, decodeSelection(raw));
}
