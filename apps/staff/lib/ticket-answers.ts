/**
 * One Ticket's Answers as the staff surface reasons about them (#310): what one
 * Ticket says in reply to each Ticket Question its Ticket Type asks.
 *
 * Pure and dependency-free — no React, no i18n runtime — so the rules are
 * directly unit-testable under the fast runner, exactly as lib/ticket-questions.ts
 * is. It returns TOKENS and shapes and never a sentence: the words for a kind,
 * for a closed edit window, and for each way an Answer can be refused all live in
 * the `ticketAnswers` namespace of both message catalogs (ADR 0041).
 *
 * WHAT IS DATA AND WHAT IS COPY. A question's label, an Option's label and an
 * Answer's own text are the words a person typed and are rendered AS THEY ARE in
 * both languages — the question's because an Organization coined it (ADR 0027),
 * the Answer's because it is somebody's own words about themselves.
 */

import type { TicketQuestion } from "./ticket-questions";

/**
 * One Ticket's Answer to one Ticket Question.
 *
 * EXACTLY ONE OF THE FIRST FOUR IS NON-NULL, decided by the question's kind,
 * which the caller has beside this in TicketQuestionAnswer. Four nullable fields
 * rather than one `value` of shifting type, so a reader can tell a number from
 * the text "3" without consulting the kind first — and so a `checked: false` is
 * visibly an Answer rather than an absence.
 */
export type Answer = {
  text: string | null;
  /**
   * A decimal STRING and not a number, all the way from the NUMERIC column. A
   * JSON number would be an IEEE double by the time it reached this file, and a
   * value that survived the column only to be rounded by the parser would defeat
   * the column. Nothing arithmetic happens on this side; the digits are drawn.
   */
  number: string | null;
  /** A calendar date, YYYY-MM-DD. Never an instant, so nothing can shift it. */
  date: string | null;
  checked: boolean | null;
  /** Empty for the five kinds that are not answered by choosing. */
  options: AnswerOption[];
  /** When this Answer last changed. No version history is kept, and no author. */
  updated_at: string;
};

/** One Option a choice Answer chose. */
export type AnswerOption = {
  /** The Option's stable identity: what a form posts back, and what survives a rename. */
  option_id: string;
  /**
   * THE SNAPSHOT: the words this Option showed when it was chosen. Never
   * rewritten, so a support conversation can say what somebody actually read.
   */
  label: string;
  /**
   * What the same Option reads NOW. Equal to `label` until somebody corrects the
   * Option's wording; after that the two differ and both are true, about
   * different moments.
   */
  current_label: string;
  /** Retired: gone from every new list, kept on the Tickets that chose it. */
  retired: boolean;
};

/** One Ticket Question paired with this Ticket's reply, or null for no reply. */
export type TicketQuestionAnswer = {
  question: TicketQuestion;
  /**
   * Null when this Ticket has not answered — which on a required question is an
   * Outstanding Answer. Null and never a blank Answer: "not said" and "said
   * nothing" are different facts, and only the first is a debt to chase.
   */
  answer: Answer | null;
};

/** Why a Ticket's Answers can no longer be written. Empty while they can. */
export type AnswerableRefusal = "" | "sale_reversed" | "event_started";

/** One Ticket with its questions and Answers, as the staff API renders it. */
export type TicketAnswers = {
  ticket_id: string;
  /**
   * Which of its Ticket Sale Line's units this Ticket is, 1..quantity. Internal
   * and not a seat number — but it is the only thing telling two Tickets of one
   * line apart, which is what lets staff say "the second of Ana's four".
   */
  ordinal: number;
  ticket_type_id: string;
  /** The Ticket Type's own name, drawn as coined in both languages. */
  ticket_type_name: string;
  ticket_sale_id: string;
  confirmation_ref: string;
  /**
   * Whether Answers may still be written. False once the Event has started and
   * false on a reversed Ticket Sale — and never a reason to hide anything: a
   * frozen Ticket stays fully readable, because a Sale Reversal voids a sale and
   * does not erase what its Tickets answered.
   */
  answerable: boolean;
  answerable_refusal: AnswerableRefusal;
  questions: TicketQuestionAnswer[];
};

/**
 * The body a PUT to one Answer takes: exactly the field its question's kind
 * uses, and none of the others.
 */
export type AnswerBody = {
  text?: string;
  number?: string;
  date?: string;
  checked?: boolean;
  option_ids?: string[];
};

/**
 * The `ticketAnswers` catalog key for each reason the edit window is shut, so no
 * surface maps a token to a word itself — the same arrangement
 * TICKET_QUESTION_KIND_KEYS has, and for the same reason.
 */
export const ANSWERABLE_REFUSAL_KEYS = {
  sale_reversed: "frozenSaleReversed",
  event_started: "frozenEventStarted",
} as const satisfies Record<Exclude<AnswerableRefusal, "">, string>;

/**
 * The `ticketAnswers` catalog key for each way the API can refuse an Answer.
 *
 * These are the `problem` tokens on an INVALID_ANSWER's `details`, which is how
 * the API says WHICH question and WHAT about it. They are mapped here rather
 * than through `errors.envelope` because the API's own sentence would have to
 * name the kind and the problem to be useful, and a sentence assembled from two
 * tokens reads like a machine in any language.
 */
export const ANSWER_PROBLEM_KEYS = {
  missing: "problemMissing",
  wrong_shape: "problemWrongShape",
  too_long: "problemTooLong",
  not_a_number: "problemNotANumber",
  not_a_date: "problemNotADate",
  one_option_only: "problemOneOptionOnly",
  duplicate_option: "problemDuplicateOption",
  too_many_options: "problemTooManyOptions",
} as const;

export type AnswerProblem = keyof typeof ANSWER_PROBLEM_KEYS;

/**
 * The `problem` token off a refusal's details, or null when the refusal was not
 * one of these — in which case the caller falls back to the API's own sentence,
 * which is the floor ADR 0023 puts under every code this app has not heard of.
 */
export function answerProblem(details: unknown): AnswerProblem | null {
  if (typeof details !== "object" || details === null) return null;
  const problem = (details as { problem?: unknown }).problem;
  return typeof problem === "string" && problem in ANSWER_PROBLEM_KEYS
    ? (problem as AnswerProblem)
    : null;
}

/**
 * The Options a choice question may be answered with RIGHT NOW: the live ones,
 * plus any retired one this Answer already chose.
 *
 * The second half is the point. A retired Option has left every new list, so it
 * is not offered to somebody answering afresh — but one already chosen must stay
 * on the form, because posting the Answer back without it would silently drop
 * what somebody said. "Kept on the Tickets that chose it" has to survive a
 * correction, not only a read.
 */
export function selectableOptions(pair: TicketQuestionAnswer) {
  const chosen = new Set((pair.answer?.options ?? []).map((option) => option.option_id));
  return pair.question.options.filter((option) => !option.retired || chosen.has(option.id));
}

/** The Option ids a choice Answer currently holds, in the order it holds them. */
export function chosenOptionIds(pair: TicketQuestionAnswer): string[] {
  return (pair.answer?.options ?? []).map((option) => option.option_id);
}

/**
 * Whether an Answer has anything to say — which is the only thing that decides
 * whether the "clear" control is offered.
 *
 * `checked: false` counts, and that is the case worth stating: somebody who read
 * "I will attend the dinner" and left it unticked has said no, and clearing that
 * back to "not said" is a different act from unticking it.
 */
export function hasAnswer(pair: TicketQuestionAnswer): boolean {
  return pair.answer !== null;
}

/**
 * The value a form field starts at for one question: whatever is stored, or
 * empty.
 *
 * A single function rather than a branch per kind at the call site, so the form
 * and the payload cannot drift about what "no answer yet" looks like for a kind.
 */
export function initialFieldValue(pair: TicketQuestionAnswer): string {
  const answer = pair.answer;
  if (!answer) return "";
  return answer.text ?? answer.number ?? answer.date ?? "";
}

/**
 * The body to PUT for one question, given what the form holds.
 *
 * IT NEVER GUESSES THE KIND FROM THE VALUE. The kind comes from the question,
 * which is the only thing that knows it — the API refuses a body of the wrong
 * shape rather than coercing it, so a form that sent `text` to a number question
 * would be refused, and rightly.
 *
 * Returns null when there is nothing to send: an empty field is not an Answer of
 * "", and the way to say "not said" is to clear the Answer, which is a DELETE.
 */
export function answerBodyFor(
  question: TicketQuestion,
  value: string,
  checked: boolean,
  optionIds: string[],
): AnswerBody | null {
  switch (question.kind) {
    case "short_text":
    case "long_text":
      return value.trim() === "" ? null : { text: value };
    case "number":
      return value.trim() === "" ? null : { number: value };
    case "date":
      return value.trim() === "" ? null : { date: value };
    case "checkbox":
      // Always sendable, `false` included: false IS an answer.
      return { checked };
    case "single_choice":
    case "multi_choice":
      return optionIds.length === 0 ? null : { option_ids: optionIds };
  }
}

/**
 * The one Option's id toggled into or out of a choice Answer's selection.
 *
 * single_choice REPLACES rather than accumulates — picking M when S was chosen
 * means M, not both — while multi_choice adds and removes. The kind decides,
 * because the two are different questions and a shared "toggle" that accumulated
 * for both would let a single_choice form build a body the API must refuse.
 */
export function toggleOption(
  question: TicketQuestion,
  current: string[],
  optionId: string,
): string[] {
  if (question.kind === "single_choice") {
    return current.includes(optionId) ? [] : [optionId];
  }
  return current.includes(optionId)
    ? current.filter((id) => id !== optionId)
    : [...current, optionId];
}
