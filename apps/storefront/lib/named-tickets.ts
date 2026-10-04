/**
 * Named Tickets at checkout, as pure data (ADR 0076, #674).
 *
 * On an Event that requires Named Tickets, begin-checkout is refused until every
 * Ticket but the buyer's own names its Holder by email, and every Ticket, the
 * buyer's own included, answers each required Ticket Question of its Ticket
 * Type. The checkout form draws those fields, keeps the pay button disabled
 * while any is owed, and says Ticket by Ticket what is.
 *
 * THIS IS A MIRROR OF THE SERVER'S PREDICATE, and only a courtesy. The rule is
 * `judgeNamedTickets` in backend/internal/sales/service/named_tickets.go over
 * `catalog.OwedByNamedTickets`, and its refusal, NAMED_TICKETS_INCOMPLETE, is
 * the guarantee: a form that let something through would be refused and its
 * details pointed back at the fields (refusedTickets below). What this file must
 * never do is ask for MORE than the server, because then a buyer could not pay
 * for a basket the platform would sell. So each rule here is the server's,
 * clause for clause, and where this page cannot know a fact it errs towards
 * asking less.
 *
 * Framework-free, like checkout-answers.ts beside it, so it is unit tested and
 * the dialog only draws.
 */

import {
  selfHeldSeat,
  slotAnswerKey,
  statedReply,
  type AnsweredTicketType,
  type AnswerSlot,
  type AnswerValue,
  type AnswerValues,
  type CheckoutQuestion,
  type QuestionSlot,
  type UpgradeOffer,
} from "./checkout-answers.ts";
import { holderEmailRefusal, normalizeHolderEmail } from "./ticket-assignment.ts";

/**
 * namedTicketsApply reports whether the requirement binds this checkout,
 * mirroring `catalog.NamedTicketsApply`: the Event requires Named Tickets,
 * Ticket Assignment is open, and the Event has not started.
 *
 * `ticketAssignmentEnabled` is the Event payload's `buyer_holds_first_ticket`,
 * which is that flag published. `now` is the SERVER's clock read with the page,
 * as the Sales Cutoff's countdown uses it, so a browser set to last week cannot
 * draw a requirement the server has already dropped. The start is an instant,
 * so the Event's timezone does not enter into it.
 *
 * A start this page cannot read does not bind. That is the one direction a
 * mirror may be wrong in: the server judges again, and refuses with the details
 * if it disagrees.
 */
export function namedTicketsApply(event: {
  requiresNamedTickets: boolean;
  ticketAssignmentEnabled: boolean;
  startsAt: string | null;
  now: Date;
}): boolean {
  if (!event.requiresNamedTickets || !event.ticketAssignmentEnabled) return false;
  if (event.startsAt === null) return true;
  const startsAt = Date.parse(event.startsAt);
  if (Number.isNaN(startsAt)) return false;
  return event.now.getTime() < startsAt;
}

/**
 * The free line an elected Upgrade gives up, which the server never buys and so
 * never asks anything of (`SameBasketUpgrade`), or null.
 *
 * Only when the prompt is drawn for the cart as it stands AND ticked, which is
 * exactly when the dialog sends `upgrade_elected: true`; and only when the free
 * Ticket is IN this cart, since one on an earlier Sale takes nothing away from
 * it.
 */
export function surrenderedTicketTypeId(
  offer: UpgradeOffer | null,
  elected: boolean,
): string | null {
  if (!elected || offer === null || offer.surrendered === null) return null;
  return offer.surrendered.ticketTypeId;
}

/** One Ticket of the basket, as the Named Tickets form asks about it. */
export type NamedTicketSlot = AnswerSlot & {
  /**
   * The buyer's own Ticket (ADR 0048). It owes its Answers and never an
   * address: the buyer is its Holder.
   */
  selfHeld: boolean;
};

/**
 * namedTicketSlots is every Ticket the basket will mint, each with its own
 * Ticket Type's questions, the buyer's own marked.
 *
 * EVERY TICKET, unlike answerSlots: a Ticket Type that asks nothing still owes a
 * Holder, so it gets a slot with no questions. A surrendered free line gets
 * none, because that Ticket will not exist. In the page's order of Ticket Types,
 * then by ticket number, so the form reads the way the cart does.
 */
export function namedTicketSlots(
  ticketTypes: AnsweredTicketType[],
  quantities: Record<string, number>,
  surrendered: string | null,
): NamedTicketSlot[] {
  const seat = selfHeldSeat(ticketTypes, quantities, surrendered);
  const slots: NamedTicketSlot[] = [];
  for (const ticketType of ticketTypes) {
    if (ticketType.id === surrendered) continue;
    const quantity = quantities[ticketType.id] ?? 0;
    for (let index = 1; index <= quantity; index++) {
      slots.push({
        ticketTypeId: ticketType.id,
        ticketTypeName: ticketType.name,
        index,
        questions: ticketType.ticket_questions ?? [],
        selfHeld: seat?.ticketTypeId === ticketType.id && seat.index === index,
      });
    }
  }
  return slots;
}

/** Every Holder address typed on the form, keyed by holderKey. */
export type HolderValues = Record<string, string>;

/**
 * holderKey identifies one Ticket: its Ticket Type and its one-based number in
 * that Ticket Type's run, the pair begin-checkout keys `holders` and `answers`
 * by. A Ticket's values are kept under it, so they survive the buyer changing
 * another Ticket Type's quantity.
 */
export function holderKey(ticketTypeId: string, index: number): string {
  return `${ticketTypeId}:${index}`;
}

/** What one Ticket still owes, the form's spelling of the server's OwedTicket. */
export type OwedTicket = {
  ticketTypeId: string;
  index: number;
  /**
   * Whether the Holder's address is owed: `missing` when none is typed,
   * `invalid` when what is typed is certainly not an address, null when it is
   * given or not asked for (the buyer's own Ticket).
   */
  holderEmail: "missing" | "invalid" | null;
  /** The required questions with no usable Answer, in the order they are asked. */
  missingQuestionIds: string[];
};

/** Everything namedTicketsOwed judges, which is the whole checkout form's say. */
export type NamedTicketsForm = {
  requiresNamedTickets: boolean;
  ticketAssignmentEnabled: boolean;
  startsAt: string | null;
  now: Date;
  ticketTypes: AnsweredTicketType[];
  quantities: Record<string, number>;
  surrenderedTicketTypeId: string | null;
  holders: HolderValues;
  answers: AnswerValues;
};

/**
 * namedTicketsOwed is what this checkout form still owes before it may be paid
 * for: every incomplete Ticket and exactly what it lacks. Empty means the pay
 * button may be pressed, and is the answer wherever the requirement does not
 * bind - the setting is off, Ticket Assignment is dark, or the Event has
 * started - so a form that is "as today" owes nothing by construction.
 *
 * It is `catalog.OwedByNamedTickets` over the basket `judgeNamedTickets` builds:
 * the surrendered free line out, the Self-held seat excused its address, and
 * every required question of each Ticket's own Ticket Type owed until it has an
 * Answer the server would keep. An optional question is never owed. A question
 * awaiting Operator approval is not on the page at all, because the API does
 * not publish it, and so it is never owed either.
 *
 * The buyer's own address and a repeated address are both accepted: a parent
 * names themself for each child, and one person may hold two Tickets.
 */
export function namedTicketsOwed(form: NamedTicketsForm): OwedTicket[] {
  if (!namedTicketsApply(form)) return [];
  const slots = namedTicketSlots(form.ticketTypes, form.quantities, form.surrenderedTicketTypeId);
  const answers = withDrawnCheckboxes(slots, form.answers);
  const owed: OwedTicket[] = [];
  for (const slot of slots) {
    const missingQuestionIds = owedQuestionIds(slot, answers);
    const holderEmail =
      slot.selfHeld ? null : holderEmailOwed(form.holders[holderKey(slot.ticketTypeId, slot.index)]);
    if (holderEmail === null && missingQuestionIds.length === 0) continue;
    owed.push({ ticketTypeId: slot.ticketTypeId, index: slot.index, holderEmail, missingQuestionIds });
  }
  return owed;
}

/**
 * owedQuestionIds is the required questions of one slot with no usable Answer
 * among `answers`, in the order they are asked. An optional question is never
 * owed. The answers are read as given: a caller that draws untouched required
 * checkboxes (withDrawnCheckboxes) does so first.
 *
 * The one reading of "owed" both Named Tickets forms share: the checkout's,
 * Ticket by Ticket, and a reassignment's, for the new Holder.
 */
export function owedQuestionIds(slot: QuestionSlot, answers: AnswerValues): string[] {
  return slot.questions
    .filter(
      (question) =>
        question.required && !answerIsUsable(question, answers[slotAnswerKey(slot, question.id)]),
    )
    .map((question) => question.id);
}

/** What is wrong with one question's reply, for the form to put in its own words. */
export type QuestionErrorKind = "answerNeeded" | "answerInvalid";

/**
 * questionErrorKinds is what to say under each of a slot's questions that has
 * something to say, by question id.
 *
 * A question the form was told is still needed (`needed`, the form's own
 * reading of a press or a refusal) is said to be needed. Otherwise a reply the
 * buyer gave and has left (`left`, by answerKey) that the server would drop is
 * said to be invalid. A blank one is not, because the form lists what is owed
 * elsewhere, and a reply still being typed is not told off.
 */
export function questionErrorKinds(
  slot: QuestionSlot,
  answers: AnswerValues,
  said: {
    needed: (question: CheckoutQuestion, reply: AnswerValue | undefined) => boolean;
    left: Readonly<Record<string, boolean | undefined>>;
  },
): Record<string, QuestionErrorKind> {
  const errors: Record<string, QuestionErrorKind> = {};
  for (const question of slot.questions) {
    const key = slotAnswerKey(slot, question.id);
    const reply = answers[key];
    if (said.needed(question, reply)) {
      errors[question.id] = "answerNeeded";
    } else if (said.left[key] && statedReply(reply) !== null && !answerIsUsable(question, reply)) {
      errors[question.id] = "answerInvalid";
    }
  }
  return errors;
}

/** questionErrorKinds' verdicts in one form's own words, by question id. */
export function questionErrorCopy(
  kinds: Readonly<Record<string, QuestionErrorKind>>,
  copy: Readonly<Record<QuestionErrorKind, string>>,
): Record<string, string> {
  return Object.fromEntries(Object.entries(kinds).map(([questionId, kind]) => [questionId, copy[kind]]));
}

/**
 * Whether one typed address still owes something. Blank is missing, as the
 * server reads a blank `holder_email`; the rest is holderEmailRefusal's
 * deliberately weak check, the same one the sale page's assignment field makes,
 * so this form never refuses an address the API would take.
 */
function holderEmailOwed(raw: string | undefined): OwedTicket["holderEmail"] {
  if (raw === undefined || raw.trim() === "") return "missing";
  return holderEmailRefusal(raw) === null ? null : "invalid";
}

/**
 * withDrawnCheckboxes answers each REQUIRED checkbox nobody has touched with
 * the state it is drawn in: unticked, so `false`.
 *
 * Elsewhere on this checkout an untouched box sends nothing, because there it
 * can be skipped and "not asked" is not "no" (checkoutAnswerBodies). On a Named
 * Tickets form a required checkbox cannot be skipped, and the server keeps
 * `false` as an Answer; holding the buyer at the pay button until they had
 * ticked and unticked a box to say "no" would make them perform a ritual to
 * state what the form already shows. This is the Holder's accept page's rule,
 * where a checkbox always sends what it shows.
 *
 * An optional checkbox is left alone: nobody needs its answer, so an untouched
 * one stays unsaid.
 */
export function withDrawnCheckboxes(slots: QuestionSlot[], answers: AnswerValues): AnswerValues {
  const drawn: AnswerValues = { ...answers };
  for (const slot of slots) {
    for (const question of slot.questions) {
      if (question.kind !== "checkbox" || !question.required) continue;
      const key = slotAnswerKey(slot, question.id);
      if (typeof drawn[key]?.checked !== "boolean") drawn[key] = { checked: false };
    }
  }
  return drawn;
}

// The server's caps, from backend/internal/catalog/ticket_answer.go and
// ticket_question.go.
const MAX_SHORT_TEXT_ANSWER_LENGTH = 200;
const MAX_LONG_TEXT_ANSWER_LENGTH = 2000;
const MAX_NUMBER_INTEGER_DIGITS = 15;
const MAX_NUMBER_FRACTION_DIGITS = 6;
const MAX_TICKET_QUESTION_OPTIONS = 20;

/**
 * answerIsUsable reports whether the server would keep this reply to this
 * question, which is what "answered" means for a required one: an Answer the
 * server drops counts as missing there (`HoldableCheckoutAnswers`), so it must
 * count as missing here too, or the pay button would light for a basket the
 * server then refuses.
 *
 * It mirrors `catalog.ParseAnswer` and the Option check after it, reading only
 * the slot the question's kind takes, as checkoutAnswerBodies sends it:
 * trimmed text within its cap in characters, a plain decimal, a real calendar
 * date in `YYYY-MM-DD`, a boolean, or distinct Options this question offers.
 */
export function answerIsUsable(question: CheckoutQuestion, value: AnswerValue | undefined): boolean {
  if (value === undefined) return false;
  switch (question.kind) {
    case "short_text":
      return textIsUsable(value.text, MAX_SHORT_TEXT_ANSWER_LENGTH);
    case "long_text":
      return textIsUsable(value.text, MAX_LONG_TEXT_ANSWER_LENGTH);
    case "number":
      return numberIsUsable(value.number);
    case "date":
      return dateIsUsable(value.date);
    case "checkbox":
      return typeof value.checked === "boolean";
    case "single_choice":
    case "multi_choice":
      return optionsAreUsable(question, value.option_ids);
    default:
      // A kind this build has not heard of is drawn as a text field, so its
      // reply travels as text; whether that fits is the server's to say.
      return textIsUsable(value.text, MAX_SHORT_TEXT_ANSWER_LENGTH);
  }
}

function textIsUsable(text: string | undefined, maxLength: number): boolean {
  if (text === undefined) return false;
  const trimmed = text.trim();
  // Counted in code points, as Go counts runes, so an emoji is one character.
  return trimmed !== "" && [...trimmed].length <= maxLength;
}

function numberIsUsable(number: string | undefined): boolean {
  if (number === undefined) return false;
  const digits = number.trim().replace(/^-/, "");
  const [integer = "", ...rest] = digits.split(".");
  const fraction = rest.join(".");
  if (!/^[0-9]+$/.test(integer) || integer.length > MAX_NUMBER_INTEGER_DIGITS) return false;
  if (rest.length === 0) return true;
  return /^[0-9]+$/.test(fraction) && fraction.length <= MAX_NUMBER_FRACTION_DIGITS;
}

function dateIsUsable(date: string | undefined): boolean {
  if (date === undefined) return false;
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date.trim());
  if (!match) return false;
  const [year, month, day] = [Number(match[1]), Number(match[2]), Number(match[3])];
  const parsed = new Date(Date.UTC(year, month - 1, day));
  // Rolled over means it never happened: 2026-02-30 becomes March 2nd.
  return (
    parsed.getUTCFullYear() === year &&
    parsed.getUTCMonth() === month - 1 &&
    parsed.getUTCDate() === day
  );
}

function optionsAreUsable(question: CheckoutQuestion, optionIds: string[] | undefined): boolean {
  const chosen = (optionIds ?? []).map((id) => id.trim()).filter((id) => id !== "");
  if (chosen.length === 0 || chosen.length > MAX_TICKET_QUESTION_OPTIONS) return false;
  if (new Set(chosen).size !== chosen.length) return false;
  if (question.kind === "single_choice" && chosen.length > 1) return false;
  const offered = new Set(question.options.map((option) => option.id));
  return chosen.every((id) => offered.has(id));
}

/** One entry of the begin-checkout body's `holders` section. */
export type CheckoutHolderBody = {
  ticket_type_id: string;
  ticket_index: number;
  holder_email: string;
};

/**
 * checkoutHolderBodies turns the typed addresses into the body's `holders`,
 * driven by the slots so an address left behind by a Ticket no longer in the
 * cart does not travel, and never for the buyer's own Ticket, whose Holder the
 * buyer already is (the server would drop it). Blank fields send nothing,
 * which the server reads as "not given"; every address goes normalised, as the
 * server stores it.
 */
export function checkoutHolderBodies(
  slots: NamedTicketSlot[],
  holders: HolderValues,
): CheckoutHolderBody[] {
  const bodies: CheckoutHolderBody[] = [];
  for (const slot of slots) {
    if (slot.selfHeld) continue;
    const email = normalizeHolderEmail(holders[holderKey(slot.ticketTypeId, slot.index)] ?? "");
    if (email === "") continue;
    bodies.push({ ticket_type_id: slot.ticketTypeId, ticket_index: slot.index, holder_email: email });
  }
  return bodies;
}

/** What a NAMED_TICKETS_INCOMPLETE refusal says one Ticket owes. */
export type RefusedTicket = { holderEmailMissing: boolean; missingQuestionIds: string[] };

/**
 * refusedTickets reads a NAMED_TICKETS_INCOMPLETE refusal's details, typed by
 * hand because the generated client has no type for them:
 * `{tickets: [{ticket_type_id, ticket_index, holder_email_missing,
 * missing_question_ids}]}`, keyed here by holderKey so the dialog can put each
 * one on the Ticket it names. Anything unrecognisable is skipped rather than
 * guessed at; the alert above the form still says the checkout was refused.
 */
export function refusedTickets(details: unknown): Record<string, RefusedTicket> {
  const refused: Record<string, RefusedTicket> = {};
  if (typeof details !== "object" || details === null) return refused;
  const tickets = (details as { tickets?: unknown }).tickets;
  if (!Array.isArray(tickets)) return refused;
  for (const entry of tickets) {
    if (typeof entry !== "object" || entry === null) continue;
    const {
      ticket_type_id: ticketTypeId,
      ticket_index: index,
      holder_email_missing: holderEmailMissing,
      missing_question_ids: missingQuestionIds,
    } = entry as Record<string, unknown>;
    if (typeof ticketTypeId !== "string" || typeof index !== "number") continue;
    refused[holderKey(ticketTypeId, index)] = {
      holderEmailMissing: holderEmailMissing === true,
      missingQuestionIds:
        Array.isArray(missingQuestionIds) ?
          missingQuestionIds.filter((id): id is string => typeof id === "string")
        : [],
    };
  }
  return refused;
}

/**
 * holderKeyOfField points a field error on `holders[i].holder_email` back at
 * the Ticket whose address was entry `i` of the body that was sent, or null for
 * any other field.
 */
export function holderKeyOfField(field: string, sent: CheckoutHolderBody[]): string | null {
  const match = /^holders\[(\d+)\]\.holder_email$/.exec(field);
  if (!match) return null;
  const body = sent[Number(match[1])];
  return body === undefined ? null : holderKey(body.ticket_type_id, body.ticket_index);
}

/**
 * parseCheckoutHolders is the begin-checkout BFF hop's reading of `holders`:
 * shape only, like its reading of `answers`. An entry that does not name a
 * Ticket Type, a whole ticket number and an address as a string is dropped,
 * because the API could not act on it either; everything else - whether the
 * address is one, whether the Ticket is in the basket, whether it is owed at
 * all - is the API's finding, and is relayed back from there.
 *
 * The address travels as sent. Normalising is the API's, and a field error on
 * `holders[i]` must point at the address the buyer actually typed.
 */
export function parseCheckoutHolders(value: unknown): CheckoutHolderBody[] {
  if (!Array.isArray(value)) return [];
  const holders: CheckoutHolderBody[] = [];
  for (const entry of value) {
    if (typeof entry !== "object" || entry === null) continue;
    const { ticket_type_id, ticket_index, holder_email } = entry as Record<string, unknown>;
    if (typeof ticket_type_id !== "string" || ticket_type_id.trim() === "") continue;
    if (typeof ticket_index !== "number" || !Number.isSafeInteger(ticket_index)) continue;
    if (typeof holder_email !== "string") continue;
    holders.push({ ticket_type_id: ticket_type_id.trim(), ticket_index, holder_email });
  }
  return holders;
}
