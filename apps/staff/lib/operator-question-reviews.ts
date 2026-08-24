/**
 * The Operator's answer to a Question Review as the Dashboard reasons about it
 * (#407, ADR 0056): one verdict per item, refusals with a reason, checked
 * whole before it is sent — the same holes the API refuses, named by item, so
 * the form refuses them first.
 *
 * Pure and dependency-free, like `question-reviews.ts`: tokens and booleans,
 * never a sentence.
 */

import { RESOLUTION_REASON_MAX_LENGTH } from "./payout-requests.ts";

export type QuestionReviewVerdict = "approved" | "refused";

/** What the Operator has decided about one item so far; null is undecided. */
export type VerdictDraft = {
  verdict: QuestionReviewVerdict | null;
  reason: string;
};

/** The two things the form needs to know about an item: its id and, for an Option, the question it belongs to. */
export type VerdictItem = {
  id: string;
  ticket_question_id: string;
  ticket_question_option_id?: string | null;
};

export type AnswerProblem =
  | { kind: "verdict"; itemId: string }
  | { kind: "reason"; itemId: string }
  | { kind: "too_long"; itemId: string };

export const EMPTY_VERDICT: VerdictDraft = { verdict: null, reason: "" };

/**
 * The first hole in the answer, in the order the items are listed: an item
 * without a verdict, a refusal without a reason, or a reason past the bound
 * the API holds a Payout Request decline's to. Null means the answer is whole.
 */
export function answerProblem(
  items: VerdictItem[],
  drafts: Record<string, VerdictDraft | undefined>,
): AnswerProblem | null {
  for (const item of items) {
    const draft = drafts[item.id] ?? EMPTY_VERDICT;
    if (draft.verdict === null) {
      return { kind: "verdict", itemId: item.id };
    }
    if (draft.verdict === "refused") {
      const reason = draft.reason.trim();
      if (!reason) {
        return { kind: "reason", itemId: item.id };
      }
      if (reason.length > RESOLUTION_REASON_MAX_LENGTH) {
        return { kind: "too_long", itemId: item.id };
      }
    }
  }
  return null;
}

/** The request body: one verdict per item, reasons trimmed, absent on an approval. */
export function answerBody(
  items: VerdictItem[],
  drafts: Record<string, VerdictDraft | undefined>,
): { verdicts: { item_id: string; verdict: QuestionReviewVerdict; reason?: string }[] } {
  return {
    verdicts: items.map((item) => {
      const draft = drafts[item.id] ?? EMPTY_VERDICT;
      const verdict = draft.verdict ?? "approved";
      return verdict === "refused"
        ? { item_id: item.id, verdict, reason: draft.reason.trim() }
        : { item_id: item.id, verdict };
    }),
  };
}

/**
 * One item's verdict set, and the same verdict carried to the question's
 * Options when the item is a question: a refused question's Options are not
 * offered either way, and an Operator who refused "Meal" should not be asked
 * six times about Chicken. An Option's verdict is its own and moves nothing.
 */
export function withVerdict(
  items: VerdictItem[],
  drafts: Record<string, VerdictDraft | undefined>,
  itemId: string,
  verdict: QuestionReviewVerdict,
): Record<string, VerdictDraft | undefined> {
  const next = { ...drafts };
  const target = items.find((item) => item.id === itemId);
  if (!target) {
    return next;
  }
  const reason = verdict === "refused" ? (drafts[itemId]?.reason ?? "") : "";
  next[itemId] = { verdict, reason };
  if (!target.ticket_question_option_id) {
    for (const item of items) {
      if (item.ticket_question_option_id && item.ticket_question_id === target.ticket_question_id) {
        next[item.id] = { verdict, reason: verdict === "refused" ? (drafts[item.id]?.reason ?? "") : "" };
      }
    }
  }
  return next;
}

/**
 * One item's reason set, carried to the question's Options that have no
 * reason of their own yet, on the same terms as the verdict.
 */
export function withReason(
  items: VerdictItem[],
  drafts: Record<string, VerdictDraft | undefined>,
  itemId: string,
  reason: string,
): Record<string, VerdictDraft | undefined> {
  const next = { ...drafts };
  const target = items.find((item) => item.id === itemId);
  if (!target) {
    return next;
  }
  const previous = drafts[itemId]?.reason ?? "";
  next[itemId] = { verdict: drafts[itemId]?.verdict ?? null, reason };
  if (!target.ticket_question_option_id) {
    for (const item of items) {
      const own = drafts[item.id]?.reason ?? "";
      if (
        item.ticket_question_option_id &&
        item.ticket_question_id === target.ticket_question_id &&
        (own === "" || own === previous)
      ) {
        next[item.id] = { verdict: drafts[item.id]?.verdict ?? null, reason };
      }
    }
  }
  return next;
}

/**
 * The items in the order the Operator reads them: each question with its
 * Option items directly beneath it, and an Option item whose question is not
 * in the Review — one added to an already approved question (#409) — standing
 * on its own where the list put it. The API writes every question item before
 * every Option item, which would draw Chicken six questions below Meal.
 */
export function nestedItems<T extends VerdictItem>(items: T[]): T[] {
  const out: T[] = [];
  const placed = new Set<string>();
  for (const item of items) {
    if (item.ticket_question_option_id || placed.has(item.id)) {
      continue;
    }
    out.push(item);
    placed.add(item.id);
    for (const option of items) {
      if (option.ticket_question_option_id && option.ticket_question_id === item.ticket_question_id) {
        out.push(option);
        placed.add(option.id);
      }
    }
  }
  for (const item of items) {
    if (!placed.has(item.id)) {
      out.push(item);
    }
  }
  return out;
}
