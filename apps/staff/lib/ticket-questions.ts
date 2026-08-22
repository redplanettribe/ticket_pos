/**
 * A Ticket Type's Ticket Questions as the staff editor reasons about them
 * (#309, ADR 0045): what an Organization wants to know about whoever will hold
 * one of that type's tickets.
 *
 * Pure and dependency-free — no React, no i18n runtime — so the rules are
 * directly unit-testable under the fast runner. It returns TOKENS and counts and
 * never a sentence: the words for a kind, and the warning about what an
 * Organization is asking for, live in the `ticketTypes` namespace of both
 * message catalogs (ADR 0041).
 */

/** The seven kinds, in the order the editor offers them. */
export const TICKET_QUESTION_KINDS = [
  "short_text",
  "long_text",
  "single_choice",
  "multi_choice",
  "number",
  "date",
  "checkbox",
] as const;

export type TicketQuestionKind = (typeof TICKET_QUESTION_KINDS)[number];

/**
 * When a Ticket Question is put to somebody.
 *
 * The editor writes `at_checkout` for every question and offers no control for
 * it. The type exists because the API stores the field, so that honouring the
 * Organization's choice later is a UI change rather than a data migration.
 */
export type TicketQuestionTiming = "at_checkout" | "after_purchase";

/** One selectable value of a choice Ticket Question, as the staff API renders it. */
export type TicketQuestionOption = {
  /**
   * The Option's identity, which is not its label. A rename leaves this alone,
   * which is what keeps the Answers given under the old wording attached — so
   * every list here is keyed on it and never on the words.
   */
  id: string;
  label: string;
  sort_order: number;
  /** Retired: gone from new lists, kept on the Tickets that chose it. */
  retired: boolean;
};

/** One Ticket Question, as the staff API renders it. */
export type TicketQuestion = {
  id: string;
  /**
   * The Organization's own words. Data, never copy: rendered as coined in both
   * languages, exactly as a Custom Tag and a Ticket Type name are.
   */
  label: string;
  kind: TicketQuestionKind;
  /** Produces an Outstanding Answer and never a refusal. */
  required: boolean;
  timing: TicketQuestionTiming;
  sort_order: number;
  retired: boolean;
  /** Empty for the five kinds that are not answered by choosing. */
  options: TicketQuestionOption[];
};

/**
 * At most twenty Options per choice question, counting the LIVE ones. Mirrors
 * `catalog.MaxTicketQuestionOptions`; the API is what enforces it, and this copy
 * exists so the editor can disable the control rather than let somebody type an
 * Option that will be refused.
 */
export const MAX_TICKET_QUESTION_OPTIONS = 20;

/** Mirrors `catalog.MaxTicketQuestionLabelLength`. */
export const MAX_TICKET_QUESTION_LABEL_LENGTH = 200;

/** Mirrors `catalog.MaxTicketQuestionOptionLabelLength`. */
export const MAX_TICKET_QUESTION_OPTION_LABEL_LENGTH = 100;

/** Whether this kind is answered by choosing from Options. Two of the seven are. */
export function offersOptions(kind: TicketQuestionKind): boolean {
  return kind === "single_choice" || kind === "multi_choice";
}

/** The Options a choice question currently offers — retired ones excluded. */
export function liveOptions(question: TicketQuestion): TicketQuestionOption[] {
  return question.options.filter((option) => !option.retired);
}

/** The Options kept only so that what has already been answered still reads. */
export function retiredOptions(question: TicketQuestion): TicketQuestionOption[] {
  return question.options.filter((option) => option.retired);
}

/**
 * Whether another Option may be added. False at twenty, which is what disables
 * the add control rather than letting the API refuse a typed one.
 */
export function canAddOption(question: TicketQuestion): boolean {
  return offersOptions(question.kind) && liveOptions(question).length < MAX_TICKET_QUESTION_OPTIONS;
}

/**
 * Whether an Option may be retired: never the last live one, because a choice
 * question with nothing to choose from is not a question. Retiring the whole
 * question is the way to close one down.
 */
export function canRetireOption(question: TicketQuestion): boolean {
  return liveOptions(question).length > 1;
}

/** The questions still being asked, in order. */
export function liveQuestions(questions: TicketQuestion[]): TicketQuestion[] {
  return questions.filter((question) => !question.retired);
}

/**
 * The questions kept only so that what has already been answered still reads.
 * Shown separately rather than hidden: a question that vanished entirely would
 * look like data loss.
 */
export function retiredQuestions(questions: TicketQuestion[]): TicketQuestion[] {
  return questions.filter((question) => question.retired);
}

/**
 * The whole running order after moving one question by one place, as the reorder
 * endpoint wants it: the complete list of live question ids.
 *
 * A move at either end returns the order unchanged rather than throwing, so the
 * caller does not have to guard the first and last rows twice — once to disable
 * the arrow, once to call this.
 */
export function moveQuestion(questions: TicketQuestion[], id: string, direction: -1 | 1): string[] {
  const ids = liveQuestions(questions).map((question) => question.id);
  const from = ids.indexOf(id);
  const to = from + direction;
  if (from === -1 || to < 0 || to >= ids.length) {
    return ids;
  }
  const reordered = [...ids];
  [reordered[from], reordered[to]] = [reordered[to], reordered[from]];
  return reordered;
}

/**
 * Whether a label is one the API will keep: present once trimmed, and within its
 * own cap.
 *
 * The cap counts CHARACTERS the way the API's does — `[...label].length` rather
 * than `label.length`, which would count a surrogate pair twice and hand an
 * Organization writing with emoji or CJK a shorter field than one writing
 * English. Nothing else is done to the label: it is read as coined, so there is
 * no case folding and no whitespace collapsing here or on the far side.
 */
export function isValidLabel(label: string, maxLength: number): boolean {
  const trimmed = label.trim();
  return trimmed.length > 0 && [...trimmed].length <= maxLength;
}

/**
 * The `ticketTypes` catalog key a kind is named with, so no surface maps a kind
 * token to a word itself — the same arrangement `app/role-name.ts` has for
 * roles, and for the same reason: a kind translated per screen is how there come
 * to be two Spanish words for "multiple choice".
 */
export const TICKET_QUESTION_KIND_KEYS = {
  short_text: "questionKindShortText",
  long_text: "questionKindLongText",
  single_choice: "questionKindSingleChoice",
  multi_choice: "questionKindMultiChoice",
  number: "questionKindNumber",
  date: "questionKindDate",
  checkbox: "questionKindCheckbox",
} as const satisfies Record<TicketQuestionKind, string>;
