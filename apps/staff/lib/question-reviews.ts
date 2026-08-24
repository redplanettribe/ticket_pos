/**
 * The Question Review as the staff editor reasons about it (#406, ADR 0056):
 * an Event's draft Ticket Questions submitted as one ask to the Platform
 * Operator, one outstanding per Event, withdrawable while it waits.
 *
 * Pure and dependency-free, like `ticket-questions.ts`: tokens and booleans,
 * never a sentence.
 */

import type { TicketQuestion } from "./ticket-questions";

/** Where a Question Review stands (migration 091). */
export type QuestionReviewStatus = "outstanding" | "answered" | "withdrawn" | "lapsed";

/** One thing a Review carries: a question, or an Option of one. */
export type QuestionReviewItem = {
  id: string;
  ticket_question_id: string;
  /** Set for an Option item, absent for a question item. */
  ticket_question_option_id?: string | null;
  verdict?: "approved" | "refused" | null;
  reason?: string | null;
};

/** One Question Review, as the staff API renders it. */
export type QuestionReview = {
  id: string;
  event_id: string;
  status: QuestionReviewStatus;
  note: string | null;
  acknowledged_at: string;
  submitted_by: string;
  submitted_at: string;
  /** How it ended, whichever way: the verdict, a withdrawal, or the lapse. */
  answered_by: string | null;
  answered_at: string | null;
  items: QuestionReviewItem[];
};

/**
 * The `ticketTypes` catalog key a Review status is named with, one word per
 * state, in one place.
 */
export const QUESTION_REVIEW_STATUS_KEYS = {
  outstanding: "reviewStatusOutstanding",
  answered: "reviewStatusAnswered",
  withdrawn: "reviewStatusWithdrawn",
  lapsed: "reviewStatusLapsed",
} as const satisfies Record<QuestionReviewStatus, string>;

/** Whether the Review is still waiting on the Operator — the state the banner and the withdraw control exist for. */
export function isOutstanding(review: QuestionReview | null): boolean {
  return review !== null && review.status === "outstanding";
}

/**
 * Whether the Event has started, which is when a Review can no longer be
 * submitted (ADR 0056). `starts_at` is an instant, so this is an instant
 * comparison; an Event with no start yet has not started.
 */
export function eventStarted(startsAt: string | null, now: Date): boolean {
  if (startsAt === null) {
    return false;
  }
  return now.getTime() >= new Date(startsAt).getTime();
}

/**
 * The live questions a submission would carry: drafts and refused ones. An
 * approved question keeps collecting and is not resubmitted; one already under
 * review is in the outstanding Review, not the next one.
 */
export function reviewableQuestions(questions: TicketQuestion[]): TicketQuestion[] {
  return questions.filter(
    (question) =>
      !question.retired &&
      (question.review_status === "draft" || question.review_status === "refused"),
  );
}

/**
 * Whether the submit control is offered: the Event has not started, nothing is
 * outstanding, and there is something to carry. The API enforces every one of
 * these; this is what disables the button rather than letting the API refuse.
 */
export function canSubmitReview(
  questions: TicketQuestion[],
  review: QuestionReview | null,
  startsAt: string | null,
  now: Date,
): boolean {
  return (
    !eventStarted(startsAt, now) &&
    !isOutstanding(review) &&
    reviewableQuestions(questions).length > 0
  );
}

/** How many questions a Review carries; Options are not counted. */
export function questionCount(review: QuestionReview): number {
  return review.items.filter((item) => !item.ticket_question_option_id).length;
}
