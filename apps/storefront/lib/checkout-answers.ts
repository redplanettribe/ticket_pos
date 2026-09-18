/**
 * The checkout's answer section, as pure data (#311, ADR 0044).
 *
 * A Ticket Question asks about the person who will HOLD a ticket, and the buyer
 * of four tickets is not assumed to know four people's answers. So the section
 * is one set of questions PER TICKET, entirely skippable, and nothing in it can
 * stop a purchase: no validation lives here, no required check, no disabled
 * button. A question the buyer leaves alone becomes an Outstanding Answer that
 * the Ticket's holder can fill in later through its Answer Link.
 *
 * Framework-free, like checkout.ts beside it, so the decisions are unit tested
 * and the component owns only the drawing.
 */

/** One selectable value of a choice question: its stable id, and its words. */
export type CheckoutQuestionOption = {
  /**
   * What an answer NAMES. Never the label — a label cannot survive a rename,
   * which is the whole reason an Option has an id.
   */
  id: string;
  label: string;
};

/** One Ticket Question as the Event page received it. */
export type CheckoutQuestion = {
  id: string;
  /**
   * The Organization's own words, read as coined in every Locale exactly as a
   * Custom Tag is (ADR 0027). Only the chrome around it is translated.
   */
  label: string;
  /** Which of the seven fields to draw. */
  kind:
    | "short_text"
    | "long_text"
    | "single_choice"
    | "multi_choice"
    | "number"
    | "date"
    | "checkbox";
  /**
   * MARKS THE FIELD AND GATES NOTHING. Required's only effect anywhere is
   * producing an Outstanding Answer the Organization can chase; a Storefront
   * that turned it into a blocked pay button would be reversing ADR 0044, whose
   * premise is that the buyer often does not know the answer.
   */
  required: boolean;
  /** Empty for the five kinds that are not answered by choosing. */
  options: CheckoutQuestionOption[];
};

/** As much of a Ticket Type as the answer section needs. */
export type AnsweredTicketType = {
  id: string;
  name: string;
  /**
   * The effective buyer price per ticket, exactly as the Event page received it:
   * server-computed, fee included where the Event passes it on, and ALREADY the
   * Promotional Price where a Promotion is live. It is here because the buyer's
   * own Ticket is the dearest one in the cart (ADR 0074), and the price that
   * decides that is the price as sold — this app never recomputes either.
   */
  price_cents: number;
  /**
   * Absent on a deployment where the feature flag is closed, which is how it
   * ships — the API omits the key entirely, so there is nothing to draw and no
   * second flag on this side to disagree with it (ADR 0045).
   */
  ticket_questions?: CheckoutQuestion[];
};

/** One ticket in the cart, and what it is being asked. */
export type AnswerSlot = {
  ticketTypeId: string;
  ticketTypeName: string;
  /**
   * Which of that Ticket Type's tickets this is, ONE-BASED, counted across the
   * whole cart's holding of it. It is sent as `ticket_index` and becomes the
   * minted Ticket's `ordinal`, which is what makes "the answers land in order"
   * mean anything — so it starts at 1 here because it starts at 1 there.
   */
  index: number;
  questions: CheckoutQuestion[];
};

/** One reply, in the one slot its question's kind takes. */
export type AnswerValue = {
  text?: string;
  /**
   * As the digits were TYPED. A string and not a number: this lands in a NUMERIC
   * column, and round-tripping it through JavaScript's float is how `0.1`
   * becomes `0.100000001` and `3.50` loses the zero it meant.
   */
  number?: string;
  date?: string;
  checked?: boolean;
  option_ids?: string[];
};

/** Every reply on the form, keyed by answerKey. */
export type AnswerValues = Record<string, AnswerValue>;

/** One entry of the begin-checkout body's answer section. */
export type CheckoutAnswerBody = {
  ticket_type_id: string;
  ticket_index: number;
  ticket_question_id: string;
} & AnswerValue;

/**
 * answerKey identifies one ticket's reply to one question.
 *
 * All three parts are needed and none is redundant: two tickets of one Ticket
 * Type differ only in the index, and two questions on one ticket differ only in
 * the question. A key missing either would silently merge two fields into one.
 */
export function answerKey(ticketTypeId: string, index: number, questionId: string): string {
  return `${ticketTypeId}:${index}:${questionId}`;
}

/**
 * answerSlots expands the cart into one set of questions per ticket.
 *
 * THIS IS THE FEATURE'S SHAPE IN ONE FUNCTION. A cart line of three becomes
 * three sets, because an Answer belongs to a Ticket and a buyer of three may
 * have three different answers (ADR 0043). A Ticket Type asking nothing, and one
 * nobody is buying, contribute no sets at all — so a cart of ordinary tickets
 * has no answer section and the dialog is the one that existed before this
 * feature.
 *
 * The order is the page's own order of Ticket Types, then the ticket's number,
 * so the form reads the way the cart does.
 */
export function answerSlots(
  ticketTypes: AnsweredTicketType[],
  quantities: Record<string, number>,
): AnswerSlot[] {
  const slots: AnswerSlot[] = [];
  for (const ticketType of ticketTypes) {
    const questions = ticketType.ticket_questions ?? [];
    if (questions.length === 0) continue;
    const quantity = quantities[ticketType.id] ?? 0;
    for (let index = 1; index <= quantity; index++) {
      slots.push({
        ticketTypeId: ticketType.id,
        ticketTypeName: ticketType.name,
        index,
        questions,
      });
    }
  }
  return slots;
}

/**
 * ownTicketSlot is the one ticket checkout asks about: the buyer's own, which
 * the sale will hand them as a Self-held Ticket (ADR 0048). It is the FIRST
 * ticket of the DEAREST Ticket Type the cart holds, ties broken by the catalog's
 * order (ADR 0074) — the same choice the commit spine makes, so what the dialog
 * calls "your ticket" is the Ticket the buyer will in fact hold.
 *
 * THE TWO RULES MUST STAY ONE SENTENCE. A buyer seated by the backend on one
 * Ticket and asked another Ticket's questions by this page answers for somebody
 * who is not them, and leaves their own Ticket owing — which is the bug ADR 0074
 * was written about, in its other half.
 *
 * The price compared is `price_cents`: what this buyer pays, Promotional Price
 * and all, never a List Price. The catalog arrives in its own order, so keeping
 * the first of the equals IS the tie-break.
 *
 * THE OTHER TICKETS ARE NOT ASKED ABOUT AT ALL, and are not mentioned. A buyer
 * of four is not assumed to know four people's sizes; those Tickets are for
 * their own Holders to answer, after the purchase, from the sale page's links.
 *
 * Null when the sale will hand the buyer nothing — the assignment feature is
 * closed, the cart is empty — or when the buyer's own Ticket Type asks nothing:
 * then there is no section, and the dialog is the one that existed before
 * Ticket Questions did.
 */
export function ownTicketSlot(
  ticketTypes: AnsweredTicketType[],
  quantities: Record<string, number>,
  buyerHoldsFirstTicket: boolean,
): AnswerSlot | null {
  if (!buyerHoldsFirstTicket) return null;
  let own: AnsweredTicketType | undefined;
  for (const ticketType of ticketTypes) {
    if ((quantities[ticketType.id] ?? 0) <= 0) continue;
    if (own === undefined || ticketType.price_cents > own.price_cents) own = ticketType;
  }
  if (own === undefined) return null;
  const questions = own.ticket_questions ?? [];
  if (questions.length === 0) return null;
  return { ticketTypeId: own.id, ticketTypeName: own.name, index: 1, questions };
}

/** The Upgrade Prompt, as the dialog needs it: whether to draw it, and about what. */
export type UpgradePrompt = {
  /**
   * The cart line the buyer gives up by electing, or NULL when the free Ticket
   * in play is on an earlier Sale and the cart holds none.
   *
   * It is the only thing the two mechanisms differ by that this app may know,
   * and it exists here for one reason: the prompt has to say something true, and
   * "the free ticket in your basket" and "the free ticket you already have" are
   * not the same sentence. The MECHANISM behind them — a line never bought
   * versus a Sale reversed inside the paid commit — is never mentioned to the
   * buyer, by ADR 0074, and this field must not become a way to mention it.
   *
   * NOTHING IS DROPPED FROM `lines` ON ACCOUNT OF IT. The cart is sent exactly
   * as the buyer built it and the commit drops the free line itself, so this
   * names what WILL happen rather than something this app does. A Storefront
   * that pre-emptively removed the line would be a second implementation of the
   * ambiguity rule, and would sell one Ticket fewer the moment the two disagreed.
   */
  surrendered: { ticketTypeId: string; ticketTypeName: string } | null;
};

/**
 * upgradePrompt decides whether the checkout offers an Upgrade, and about which
 * free Ticket (ADR 0074, #652).
 *
 * THIS RESTATES A BACKEND RULE, AND IT IS THE ONE PLACE IN THE FEATURE WHERE
 * DRIFT CAN ENTER. The Go counterpart is
 * `UpgradeEligibility.OffersUpgrade(freeTicketsInBasket)` in
 * `backend/internal/sales/repository/self_held.go` —
 * `surrenderable_free_tickets + free Tickets in the basket == 1` — and the
 * duplication is unavoidable rather than sloppy: the basket is client-side and
 * never reaches the Event-page request that publishes the count, so neither side
 * can do this arithmetic alone. Nothing but review keeps the two sentences the
 * same. It is written ONCE, here, so that there is only ever one of them to
 * check, and the component above it decides nothing.
 *
 * THE COUNT IS NULLABLE AND NULL IS NOT ZERO. Null is the anonymous read — "we
 * do not know who is asking" — and yields no prompt: there is no buyer whose
 * earlier Sales were counted, so there is no offer to make. Zero is a real
 * answer given to somebody we can identify, and a zero with one free Ticket in
 * the cart is a genuine offer. Reading null as 0 would show the prompt to
 * anonymous readers, and it is the only mistake here that produces a plausible-
 * looking page rather than a visible one.
 *
 * NOTHING IT RETURNS CAN FAIL A PURCHASE. A prompt drawn where the commit will
 * not honour it costs nothing — an election the backend did not offer is IGNORED
 * and never refused — and a prompt withheld where the commit would have honoured
 * it costs a buyer nothing but the offer. That asymmetry is why the rule may
 * live in two places at all.
 *
 * Three things must hold, and each is a case ADR 0074 argued:
 *
 *   - THE CART HOLDS SOMETHING PAID. An Upgrade is free to paid, so without a
 *     paid Ticket to move onto there is nothing to elect. The backend's
 *     OffersUpgrade deliberately does not carry this precondition, leaving it to
 *     whichever surface does the offering; on this side, that is this function.
 *   - EXACTLY ONE FREE TICKET IS IN PLAY, counting the cart and the buyer's
 *     earlier Sales together. Two is ambiguous from either side, and the answer
 *     to ambiguity is no prompt rather than a guess.
 *   - FREE MEANS `price_cents === 0`, which is the price AS SOLD: the API has
 *     already applied any Promotional Price, and this app never recomputes a
 *     price. A Ticket Type given away today is free in this basket, which is the
 *     honest reading of "cost nothing" and the one `unit_price_cents` takes.
 *
 * Counted over QUANTITY throughout: a line of two free Tickets is two free
 * Tickets. Since exactly one free Ticket in the cart is therefore exactly one
 * free LINE, naming the line and naming the Ticket are the same act here.
 */
export function upgradePrompt(
  ticketTypes: AnsweredTicketType[],
  quantities: Record<string, number>,
  surrenderableFreeTickets: number | null,
): UpgradePrompt | null {
  if (surrenderableFreeTickets === null) return null;

  let freeInBasket = 0;
  let paidInBasket = 0;
  let freeLine: AnsweredTicketType | undefined;
  for (const ticketType of ticketTypes) {
    const quantity = quantities[ticketType.id] ?? 0;
    if (quantity <= 0) continue;
    if (ticketType.price_cents === 0) {
      freeInBasket += quantity;
      freeLine ??= ticketType;
      continue;
    }
    paidInBasket += quantity;
  }

  if (paidInBasket === 0) return null;
  if (surrenderableFreeTickets + freeInBasket !== 1) return null;

  return {
    surrendered:
      freeLine === undefined
        ? null
        : { ticketTypeId: freeLine.id, ticketTypeName: freeLine.name },
  };
}

/**
 * hasCheckoutQuestions reports whether anything in the cart asks anything —
 * which is what decides whether the dialog draws an answer section's heading at
 * all. It is `answerSlots(...).length > 0` said cheaply and named for what the
 * caller means.
 */
export function hasCheckoutQuestions(
  ticketTypes: AnsweredTicketType[],
  quantities: Record<string, number>,
): boolean {
  return answerSlots(ticketTypes, quantities).length > 0;
}

/**
 * checkoutAnswerBodies turns the form's state into the begin-checkout body's
 * `answers` array, sending only what the buyer actually filled in.
 *
 * IT IS DRIVEN BY THE SLOTS AND NOT BY THE VALUES, deliberately: the slots are
 * the truth about what is being bought, so a reply left behind by a ticket the
 * buyer then removed from the cart simply has no slot and does not travel. The
 * alternative — iterating the values — would send answers for tickets that no
 * longer exist, which the API would drop, having first written them down as a
 * thing somebody typed.
 *
 * A BLANK IS NOT AN ANSWER. An emptied text field and an empty set of chosen
 * Options are both "not said", and the way to say that is to send nothing: a
 * stored blank is a row no later reader could tell from a real reply. A
 * CHECKBOX IS THE EXCEPTION AND THE INTERESTING ONE — `false` is somebody who
 * read the question and said no, so it travels, while a checkbox nobody touched
 * has no value at all and does not.
 *
 * NOTHING HERE VALIDATES. Whether a reply fits its question's kind is the API's
 * finding, and its answer to "no" is to drop the reply rather than refuse the
 * purchase (ADR 0044). A second copy of those rules on this side would be a
 * second place for them to be enforced differently — and this is the side where
 * a refusal is easy to write by accident.
 */
export function checkoutAnswerBodies(
  slots: AnswerSlot[],
  values: AnswerValues,
): CheckoutAnswerBody[] {
  const bodies: CheckoutAnswerBody[] = [];
  for (const slot of slots) {
    for (const question of slot.questions) {
      const value = values[answerKey(slot.ticketTypeId, slot.index, question.id)];
      const stated = statedReply(value);
      if (!stated) continue;
      bodies.push({
        ticket_type_id: slot.ticketTypeId,
        ticket_index: slot.index,
        ticket_question_id: question.id,
        ...stated,
      });
    }
  }
  return bodies;
}

/**
 * statedReply narrows one form value to the reply it actually states, or null
 * when the buyer said nothing.
 *
 * Exactly one slot comes back filled, because exactly one is what the API takes:
 * a body naming two is a caller believing two things about one question, and is
 * refused there rather than coerced. The order below is the order the kinds are
 * checked in and no value can legitimately fill two.
 */
function statedReply(value: AnswerValue | undefined): AnswerValue | null {
  if (!value) return null;
  if (typeof value.checked === "boolean") return { checked: value.checked };
  if (value.option_ids && value.option_ids.length > 0) return { option_ids: value.option_ids };
  if (value.text !== undefined && value.text.trim() !== "") return { text: value.text };
  if (value.number !== undefined && value.number.trim() !== "") return { number: value.number };
  if (value.date !== undefined && value.date.trim() !== "") return { date: value.date };
  return null;
}
