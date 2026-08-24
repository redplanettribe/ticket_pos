"use client";

import { useMessages, useTranslations } from "next-intl";
import { FormEvent, useCallback, useEffect, useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  FormField,
  Textarea,
  toast,
} from "@ticket-pos/ui";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError, fetchEventsJSON } from "@/lib/events-api";
import {
  QUESTION_REVIEW_STATUS_KEYS,
  canSubmitReview,
  eventStarted,
  isOutstanding,
  questionCount,
  reviewableQuestions,
  type QuestionReview,
} from "@/lib/question-reviews";
import type { TicketQuestion } from "@/lib/ticket-questions";

type QuestionReviewSectionProps = {
  eventId: string;
  /** The Event's start, after which no Review is accepted (ADR 0056). */
  eventStartsAt: string | null;
  /** Every Ticket Type of the Event: a Review carries the drafts of all of them. */
  ticketTypeIds: string[];
};

/**
 * The Event-level Question Review surface (#406, ADR 0056): a banner while a
 * Review is outstanding, with its withdrawal; and the submit dialog, with the
 * acknowledgement and the note, disabled once the Event has started.
 *
 * Mounted only while the Ticket Question flag is on, on the terms the
 * authoring dialog is.
 */
export function QuestionReviewSection({ eventId, eventStartsAt, ticketTypeIds }: QuestionReviewSectionProps) {
  const t = useTranslations("ticketTypes");
  const errorCopy = useMessages().errors;
  // The ids as one string, so a parent re-rendering with an equal list does
  // not make `load` a new function and refetch on every render.
  const ticketTypeKey = ticketTypeIds.join(",");

  const [review, setReview] = useState<QuestionReview | null>(null);
  const [questions, setQuestions] = useState<TicketQuestion[]>([]);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [note, setNote] = useState("");
  const [acknowledged, setAcknowledged] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const reportFailure = useCallback(
    (failure: unknown, fallback: string) => {
      toast.error(
        apiErrorMessage(errorCopy, failure instanceof ApiError ? failure : null) ?? fallback,
      );
    },
    [errorCopy],
  );

  const load = useCallback(async () => {
    try {
      const [current, perType] = await Promise.all([
        // No Review yet is an ordinary state, not a failure.
        fetchEventsJSON<QuestionReview>(`/api/events/${eventId}/question-reviews/current`).catch(
          (failure) => {
            if (failure instanceof ApiError && failure.code === "QUESTION_REVIEW_NOT_FOUND") {
              return null;
            }
            throw failure;
          },
        ),
        Promise.all(
          ticketTypeKey.split(",").filter(Boolean).map((ticketTypeId) =>
            fetchEventsJSON<TicketQuestion[]>(
              `/api/events/${eventId}/ticket-types/${ticketTypeId}/questions`,
            ),
          ),
        ),
      ]);
      setReview(current);
      setQuestions(perType.flat());
    } catch (failure) {
      reportFailure(failure, t("reviewLoadFailed"));
    }
  }, [eventId, reportFailure, t, ticketTypeKey]);

  useEffect(() => {
    void load();
  }, [load]);

  const now = new Date();
  const started = eventStarted(eventStartsAt, now);
  const canSubmit = canSubmitReview(questions, review, eventStartsAt, now);
  const reviewable = reviewableQuestions(questions).length;
  // The Review the banner and the withdraw control are about, or null.
  const outstanding = review !== null && isOutstanding(review) ? review : null;

  function openDialog() {
    setNote("");
    setAcknowledged(false);
    setFormError(null);
    setDialogOpen(true);
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setFormError(null);
    if (!acknowledged) {
      setFormError(t("reviewAcknowledgeRequired"));
      return;
    }
    setBusy(true);
    try {
      await fetchEventsJSON<QuestionReview>(`/api/events/${eventId}/question-reviews`, {
        method: "POST",
        body: JSON.stringify({ note, acknowledged }),
      });
      toast.success(t("reviewSubmitted"));
      setDialogOpen(false);
      await load();
    } catch (failure) {
      reportFailure(failure, t("reviewSubmitFailed"));
    } finally {
      setBusy(false);
    }
  }

  async function handleWithdraw() {
    if (outstanding === null) {
      return;
    }
    setBusy(true);
    try {
      await fetchEventsJSON<QuestionReview>(
        `/api/events/${eventId}/question-reviews/${outstanding.id}/withdraw`,
        { method: "POST" },
      );
      toast.success(t("reviewWithdrawn"));
      await load();
    } catch (failure) {
      reportFailure(failure, t("reviewWithdrawFailed"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-3">
      {outstanding ? (
        <Alert>
          <AlertTitle>{t("reviewBannerTitle")}</AlertTitle>
          <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
            <span>
              {t("reviewBannerBody", { count: questionCount(outstanding), by: outstanding.submitted_by })}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              aria-busy={busy}
              onClick={() => void handleWithdraw()}
            >
              {busy ? t("reviewWithdrawing") : t("reviewWithdraw")}
            </Button>
          </AlertDescription>
        </Alert>
      ) : (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3">
          <div className="text-sm text-muted-foreground">
            {started
              ? t("reviewEventStarted")
              : reviewable === 0
                ? t("reviewNothingToSubmit")
                : t("reviewReady", { count: reviewable })}
            {review ? (
              <span className="block">
                {t("reviewLast", { status: t(QUESTION_REVIEW_STATUS_KEYS[review.status]) })}
              </span>
            ) : null}
          </div>
          <Button type="button" variant="outline" disabled={!canSubmit} onClick={openDialog}>
            {t("reviewSubmit")}
          </Button>
        </div>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("reviewDialogTitle")}</DialogTitle>
            <DialogDescription>{t("reviewDialogDescription", { count: reviewable })}</DialogDescription>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="space-y-4">
            <FormField id="question-review-note" label={t("reviewNote")}>
              <Textarea
                id="question-review-note"
                value={note}
                onChange={(event) => setNote(event.target.value)}
                rows={3}
              />
            </FormField>
            {/* The acknowledgement is a record, kept with who affirmed it and
                when (ADR 0056); it is where the authoring warning stops being a
                banner. */}
            <label className="flex items-start gap-2 text-sm">
              <input
                type="checkbox"
                className="mt-1"
                checked={acknowledged}
                onChange={(event) => setAcknowledged(event.target.checked)}
              />
              <span>{t("reviewAcknowledge")}</span>
            </label>
            {formError ? (
              <p role="alert" className="text-sm text-destructive">
                {formError}
              </p>
            ) : null}
            <DialogFooter>
              <Button type="button" variant="outline" disabled={busy} onClick={() => setDialogOpen(false)}>
                {t("cancel")}
              </Button>
              <Button type="submit" disabled={busy || !acknowledged} aria-busy={busy}>
                {busy ? t("reviewSubmitting") : t("reviewSubmit")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
