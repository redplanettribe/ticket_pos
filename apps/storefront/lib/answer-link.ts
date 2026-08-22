/**
 * The Answer Link's page, as rules rather than as markup (#312, ADR 0044).
 *
 * An Answer Link opens ONE Ticket's Ticket Questions for whoever the buyer
 * forwarded it to, and shows nothing else about the purchase. That last part is
 * a backend property — service.AnswerLinkView never sends the buyer, the price,
 * the Tax ID or the confirmation reference — and this file is the reason it
 * stays one: NOTHING HERE INVENTS A FIELD. It renders what arrived, and what
 * arrives is only ever those three things.
 *
 * Pure and dependency-free — no React, no i18n runtime — so the rules are
 * directly unit-testable, exactly as the staff app's lib/ticket-answers.ts is.
 * It returns SHAPES and never a sentence.
 *
 * WHAT IS DATA AND WHAT IS COPY. A Ticket Question's label and an Option's label
 * are read AS COINED in every Locale, like a Custom Tag (ADR 0027): the
 * Organization wrote them, and translating "T-shirt size" into the reader's
 * language would be the platform putting words in their mouth. Only the page's
 * own chrome follows the Locale.
 */

/** The seven shapes a Ticket Question takes. The kind is frozen once answered. */
export type QuestionKind =
  | "short_text"
  | "long_text"
  | "single_choice"
  | "multi_choice"
  | "number"
  | "date"
  | "checkbox";

/** One Option a choice question offers. */
export type QuestionOption = {
  id: string;
  /** Coined by the Organization; drawn as written in both languages. */
  label: string;
  sort_order: number;
  /** Retired: gone from every new list, kept on the Tickets that chose it. */
  retired: boolean;
};

/** One Ticket Question, as the public Answer Link payload carries it. */
export type Question = {
  id: string;
  label: string;
  kind: QuestionKind;
  /**
   * Required means OUTSTANDING, never BLOCKING (ADR 0044). Nothing on this page
   * is refused for want of an Answer and no control is disabled by it; the flag
   * exists so the page can say which ones the Organization is waiting on.
   */
  required: boolean;
  sort_order: number;
  retired: boolean;
  options: QuestionOption[];
};

/** One Option a choice Answer chose. */
export type AnswerOption = {
  option_id: string;
  /** THE SNAPSHOT: the words this Option showed when it was chosen. */
  label: string;
  /** What the same Option reads now. Differs from `label` after a rename. */
  current_label: string;
  retired: boolean;
};

/**
 * One Ticket's Answer to one Ticket Question.
 *
 * EXACTLY ONE OF THE FIRST FOUR IS NON-NULL, decided by the question's kind.
 * `checked: false` is an Answer — somebody read it and said no — while a null
 * answer is the absence of one.
 */
export type Answer = {
  text: string | null;
  /**
   * A decimal STRING and not a number, all the way from the NUMERIC column. A
   * JSON number would be an IEEE double by the time it reached here.
   */
  number: string | null;
  /** A calendar date, YYYY-MM-DD. Never an instant, so nothing shifts it. */
  date: string | null;
  checked: boolean | null;
  options: AnswerOption[];
  updated_at: string;
};

/** One question paired with this Ticket's reply, or null for no reply. */
export type QuestionAnswer = {
  question: Question;
  answer: Answer | null;
};

/**
 * What an Answer Link opens: THREE FIELDS AND NO MORE.
 *
 * This type is the Storefront's half of ADR 0044's disclosure rule, and it is
 * written narrow ON PURPOSE. There is no buyer here, no price, no Tax ID, no
 * Sale Confirmation reference, no sibling Ticket and not even a ticket id — the
 * form posts back with the TOKEN. A link forwarded into a group chat should tell
 * the group nothing about who paid.
 *
 * Anybody widening this is reversing that decision, and the backend will not
 * feed it: the API sends exactly these, and there is an integration test that
 * reads the raw response body and fails if a fourth appears.
 */
export type AnswerLinkView = {
  event_name: string;
  ticket_type_name: string;
  questions: QuestionAnswer[];
};

/**
 * The body a PUT to one Answer takes: exactly the field its question's kind
 * uses, plus the token that says which Ticket is answering.
 */
export type AnswerBody = {
  text?: string;
  number?: string;
  date?: string;
  checked?: boolean;
  option_ids?: string[];
};

/**
 * The `errors.answerLink` catalog key for each way a link can fail to open.
 *
 * Mapped here rather than left to the API's own sentence because these three are
 * the whole of what this page can go wrong with, and because the recovery
 * differs: an expired link has no replacement and a broken one might, so
 * offering the same words for both would send half the readers back to the buyer
 * for nothing.
 */
export const ANSWER_LINK_FAILURE_KEYS = {
  ANSWER_LINK_INVALID: "invalid",
  ANSWER_LINK_EXPIRED: "expired",
  // The feature flag is off. It answers 404 exactly as a build without the
  // feature does (ADR 0045), and the reader is told the link is not valid —
  // which is the true statement available to them.
  TICKET_QUESTIONS_UNAVAILABLE: "invalid",
  // No signing key configured: a deployment fault, not the holder's, so it says
  // "try again later" rather than blaming the link they were given.
  ANSWER_LINK_UNAVAILABLE: "unavailable",
} as const;

export type AnswerLinkFailure = (typeof ANSWER_LINK_FAILURE_KEYS)[keyof typeof ANSWER_LINK_FAILURE_KEYS];

/**
 * Which copy a failed open gets, from the API's error code.
 *
 * A code this app has not heard of falls through to "invalid", which is the
 * honest floor: the reader could not open their link, and the page has nothing
 * truer to tell them than that.
 */
export function answerLinkFailure(code: string | undefined): AnswerLinkFailure {
  if (code && code in ANSWER_LINK_FAILURE_KEYS) {
    return ANSWER_LINK_FAILURE_KEYS[code as keyof typeof ANSWER_LINK_FAILURE_KEYS];
  }
  return "invalid";
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
export function selectableOptions(pair: QuestionAnswer): QuestionOption[] {
  const chosen = new Set((pair.answer?.options ?? []).map((option) => option.option_id));
  return pair.question.options.filter((option) => !option.retired || chosen.has(option.id));
}

/** The Option ids this Answer currently holds, in the order it holds them. */
export function chosenOptionIds(pair: QuestionAnswer): string[] {
  return (pair.answer?.options ?? []).map((option) => option.option_id);
}

/**
 * The value a text, number or date field starts at: whatever is stored, or empty.
 *
 * One function rather than a branch per kind at the call site, so the form and
 * the payload cannot drift about what "no answer yet" looks like for a kind.
 *
 * WHAT IS STORED MAY BE THE BUYER'S GUESS. The holder sees it and may overwrite
 * it, which is the whole point of the link — somebody cannot correct a guess
 * they cannot see.
 */
export function initialFieldValue(pair: QuestionAnswer): string {
  const answer = pair.answer;
  if (!answer) return "";
  return answer.text ?? answer.number ?? answer.date ?? "";
}

/** Whether a checkbox question starts ticked. Absent is not the same as false. */
export function initialChecked(pair: QuestionAnswer): boolean {
  return pair.answer?.checked === true;
}

/**
 * The body to PUT for one question, given what the form holds.
 *
 * IT NEVER GUESSES THE KIND FROM THE VALUE. The kind comes from the question,
 * which is the only thing that knows it: the API refuses a body of the wrong
 * shape rather than coercing it, so a form sending `text` to a number question
 * is refused, and rightly.
 *
 * Returns null when there is nothing to send. An empty field is not an Answer of
 * "" — the way to say "not said" is to have no Answer at all — and this page
 * offers no way to take one back, deliberately: whoever holds the link may
 * OVERWRITE what is there, and erasing somebody else's reply from an
 * unauthenticated page is not a power this link grants (ADR 0044 gives the
 * buyer and Event Staff the correction; neither of them lost it).
 */
export function answerBodyFor(
  question: Question,
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
 * because a shared "toggle" that accumulated for both would let a single_choice
 * form build a body the API must refuse.
 */
export function toggleOption(
  question: Question,
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

/**
 * The questions this page draws, in the order it draws them.
 *
 * RETIRED QUESTIONS ARE KEPT ONLY IF THIS TICKET ANSWERED THEM, and are drawn
 * read-only by the form. A retired question is one the Organization has stopped
 * asking, so offering it to a holder answering afresh would be collecting an
 * answer to a question nobody wants any more — while dropping one this Ticket
 * HAS answered would make the reply look as though it had been thrown away, and
 * would hide from the holder something recorded about them.
 */
export function visibleQuestions(view: AnswerLinkView): QuestionAnswer[] {
  return view.questions.filter((pair) => !pair.question.retired || pair.answer !== null);
}

/** Whether a question is drawn read-only: retired ones, kept only to be read. */
export function isReadOnly(pair: QuestionAnswer): boolean {
  return pair.question.retired;
}
