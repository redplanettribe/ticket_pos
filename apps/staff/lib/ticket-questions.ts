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

/**
 * Where a Ticket Question or an Option stands with the Platform Operator
 * (ADR 0056). Born `draft` and asked of nobody until `approved`; `refused`
 * carries the Operator's reason. The glossary's words, exactly: never
 * "rejected", never "approval_status".
 */
export const TICKET_QUESTION_REVIEW_STATUSES = [
  "draft",
  "under_review",
  "approved",
  "refused",
] as const;

export type TicketQuestionReviewStatus = (typeof TICKET_QUESTION_REVIEW_STATUSES)[number];

/** The review columns both a question and an Option carry, as the API renders them. */
export type Reviewed = {
  review_status: TicketQuestionReviewStatus;
  /** Who approved it — an Operator, or the grandfathering migration. Absent until somebody has. */
  approved_by?: string;
  /** The Operator's reason on a refused row. */
  refusal_reason?: string;
  /** The Operator's reason on a Revocation, which is why a retired row stopped being asked. */
  revocation_reason?: string;
};

/** One selectable value of a choice Ticket Question, as the staff API renders it. */
export type TicketQuestionOption = Reviewed & {
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
export type TicketQuestion = Reviewed & {
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
 * The `ticketTypes` catalog key a review state is named with, on the terms
 * TICKET_QUESTION_KIND_KEYS is: one word per state, in one place.
 */
export const TICKET_QUESTION_REVIEW_STATUS_KEYS = {
  draft: "questionReviewDraft",
  under_review: "questionReviewUnderReview",
  approved: "questionReviewApproved",
  refused: "questionReviewRefused",
} as const satisfies Record<TicketQuestionReviewStatus, string>;

/** Whether the question or Option is being asked of anybody: approved and not retired (ADR 0056). */
export function isAsked(item: Reviewed & { retired: boolean }): boolean {
  return item.review_status === "approved" && !item.retired;
}

/**
 * The Operator's reason the editor should show beside a row, if there is one:
 * a refusal's on a refused row, a Revocation's on a revoked one. Null when the
 * row stands on no verdict worth explaining. A refused row that has since been
 * edited back into a draft keeps its old reason in the payload but is a draft
 * now, so the reason is not shown against it.
 */
export function reviewReason(
  item: Reviewed,
): { kind: "refusal" | "revocation"; reason: string } | null {
  if (item.revocation_reason) {
    return { kind: "revocation", reason: item.revocation_reason };
  }
  if (item.review_status === "refused" && item.refusal_reason) {
    return { kind: "refusal", reason: item.refusal_reason };
  }
  return null;
}

/**
 * Which edits a question's review state allows without a review (#408,
 * ADR 0056) — the API's `TicketQuestionEditRefusal` rule, mirrored so the
 * editor never offers what the API refuses.
 *
 * `reword` covers the label, the kind and the timing: the things the Operator
 * read. `require` is the widening (optional → required). Narrowing — making it
 * optional, retiring an Option, retiring the question — and reorder are allowed
 * in EVERY state and so have no entry here. `addOption` is open on an approved
 * question (the new Option is born a draft) and closed under review, where
 * nothing moves until the Review is withdrawn.
 */
export type TicketQuestionEdits = {
  reword: boolean;
  require: boolean;
  addOption: boolean;
};

export function allowedEdits(status: TicketQuestionReviewStatus): TicketQuestionEdits {
  switch (status) {
    case "approved":
      return { reword: false, require: false, addOption: true };
    case "under_review":
      return { reword: false, require: false, addOption: false };
    default:
      return { reword: true, require: true, addOption: true };
  }
}

/**
 * Whether an Option's label may still be typed over: never on a question under
 * review, and never once the Option itself is approved — a correction to an
 * approved Option is retire-and-add, because a correction and a rewording are
 * the same operation. A draft Option on an approved question is nobody's yet.
 */
export function canRenameOption(question: Reviewed, option: Reviewed): boolean {
  return question.review_status !== "under_review" && option.review_status !== "approved";
}

/**
 * Whether the editor should tell the Organization how to change what it can
 * no longer type over: the retire-and-re-ask hint, shown on an approved
 * question only. Under review the state badge says enough, and the way out is
 * withdrawing the Review, not retiring the question.
 */
export function showsRetireAndReAskHint(status: TicketQuestionReviewStatus): boolean {
  return status === "approved";
}

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
