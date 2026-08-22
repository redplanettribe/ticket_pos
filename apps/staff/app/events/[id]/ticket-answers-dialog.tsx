"use client";

import { useCallback, useEffect, useState } from "react";

import { useMessages, useTranslations } from "next-intl";

import {
  Alert,
  AlertDescription,
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  Input,
  Skeleton,
  toast,
} from "@ticket-pos/ui";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError, fetchEventsJSON } from "@/lib/events-api";
import {
  ANSWERABLE_REFUSAL_KEYS,
  ANSWER_PROBLEM_KEYS,
  answerBodyFor,
  answerProblem,
  chosenOptionIds,
  hasAnswer,
  initialFieldValue,
  selectableOptions,
  toggleOption,
  type TicketAnswers,
  type TicketQuestionAnswer,
} from "@/lib/ticket-answers";
import { TICKET_QUESTION_KIND_KEYS } from "@/lib/ticket-questions";

type TicketAnswersDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  eventId: string;
  ticketSaleId: string;
  /** The buyer's own reference for the sale, so the dialog names what it opened. */
  confirmationRef: string;
};

/**
 * One Ticket Sale's Tickets and what each of them says in reply to its Ticket
 * Questions (#310).
 *
 * WHY THE SALE AND NOT A TICKET. A Ticket has no name and no face; it is one
 * unit of admission, told apart from its siblings only by its ordinal. Staff
 * never go looking for "ticket 3", they go looking for Ana's order — so the sale
 * is the thing they already have, and the Tickets hang off it.
 *
 * It renders only where the feature flag is on — the caller decides that from
 * the Event payload's `ticket_questions_enabled` — and the API refuses every one
 * of these requests with a 404 while the flag is off, so a stale page cannot
 * write anything (ADR 0045).
 *
 * A FROZEN TICKET IS STILL FULLY READABLE. Once the Event has started, or the
 * Ticket Sale has been reversed, the controls go away and the Answers stay: a
 * Sale Reversal voids a sale, it does not erase what its Tickets answered, and a
 * surface that hid them would make it look as though it had.
 */
export function TicketAnswersDialog({
  open,
  onOpenChange,
  eventId,
  ticketSaleId,
  confirmationRef,
}: TicketAnswersDialogProps) {
  const t = useTranslations("ticketAnswers");
  const errorCopy = useMessages().errors;

  const [loading, setLoading] = useState(true);
  const [tickets, setTickets] = useState<TicketAnswers[]>([]);

  const path = `/api/events/${eventId}/ticket-sales/${ticketSaleId}/tickets`;

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setTickets(await fetchEventsJSON<TicketAnswers[]>(path));
    } catch (failure) {
      toast.error(
        apiErrorMessage(errorCopy, failure instanceof ApiError ? failure : null) ??
          t("loadFailed"),
      );
    } finally {
      setLoading(false);
    }
  }, [path, errorCopy, t]);

  useEffect(() => {
    if (!open) {
      return;
    }
    void load();
  }, [open, load]);

  /**
   * Every write answers with the WHOLE Ticket, and this re-reads the whole sale
   * anyway rather than splicing that payload in.
   *
   * Re-reading is what keeps a concurrent edit from another tab — or from
   * somebody else on the same Event — from being papered over, and it is also
   * how a rename of an Option made in the questions editor a moment ago shows up
   * here without this component knowing that happened.
   */
  const write = useCallback(
    async (ticketId: string, questionId: string, init: RequestInit) => {
      try {
        await fetchEventsJSON<unknown>(
          `/api/events/${eventId}/tickets/${ticketId}/answers/${questionId}`,
          init,
        );
        await load();
      } catch (failure) {
        const apiError = failure instanceof ApiError ? failure : null;
        // A refusal that names WHICH problem gets this surface's own sentence,
        // in the reader's language. Anything else falls through to the API's own
        // English, which is the floor ADR 0023 puts under an unknown code.
        const problem = answerProblem(apiError?.details);
        toast.error(
          (problem ? t(ANSWER_PROBLEM_KEYS[problem]) : null) ??
            apiErrorMessage(errorCopy, apiError) ??
            t("saveFailed"),
        );
      }
    },
    [eventId, load, errorCopy, t],
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          {/* The confirmation reference is the buyer's own, and is data. */}
          <DialogTitle>{t("title", { reference: confirmationRef })}</DialogTitle>
          <DialogDescription>{t("description")}</DialogDescription>
        </DialogHeader>

        {loading ? (
          <div className="space-y-2">
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </div>
        ) : tickets.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("noTickets")}</p>
        ) : (
          <ul className="space-y-4">
            {tickets.map((ticket) => (
              <li key={ticket.ticket_id} className="rounded-md border p-3">
                <div className="flex flex-wrap items-center gap-2">
                  {/* The Ticket Type's name is the Organization's own words:
                      data, and untranslated in both languages. The ordinal is
                      how two Tickets of one line are told apart. */}
                  <span className="font-medium">
                    {t("ticketHeading", {
                      name: ticket.ticket_type_name,
                      ordinal: ticket.ordinal,
                    })}
                  </span>
                  {ticket.answerable ? null : (
                    <Badge variant="secondary">{t("frozenBadge")}</Badge>
                  )}
                </div>

                {/* The reason the form will not take, drawn beside the fields
                    rather than left for somebody to work out. */}
                {ticket.answerable_refusal === "" ? null : (
                  <Alert variant="warning" className="mt-2">
                    <AlertDescription>
                      {t(ANSWERABLE_REFUSAL_KEYS[ticket.answerable_refusal])}
                    </AlertDescription>
                  </Alert>
                )}

                {ticket.questions.length === 0 ? (
                  <p className="mt-2 text-sm text-muted-foreground">{t("noQuestions")}</p>
                ) : (
                  <ul className="mt-3 space-y-3">
                    {ticket.questions.map((pair) => (
                      <QuestionRow
                        key={pair.question.id}
                        pair={pair}
                        answerable={ticket.answerable}
                        onSave={(body) =>
                          write(ticket.ticket_id, pair.question.id, {
                            method: "PUT",
                            body: JSON.stringify(body),
                          })
                        }
                        onClear={() =>
                          write(ticket.ticket_id, pair.question.id, { method: "DELETE" })
                        }
                      />
                    ))}
                  </ul>
                )}
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}

type QuestionRowProps = {
  pair: TicketQuestionAnswer;
  answerable: boolean;
  onSave: (body: object) => Promise<void>;
  onClear: () => Promise<void>;
};

/**
 * One Ticket Question and this Ticket's reply to it.
 *
 * Held as local form state keyed off the stored Answer, so typing does not fire
 * a request per keystroke. It saves on blur for the typed kinds and immediately
 * for the ticked ones, which is the same rhythm the questions editor uses for a
 * label.
 *
 * A RETIRED QUESTION IS DRAWN AND NOT HIDDEN, read-only. It is kept precisely so
 * that what has already been answered still reads; a row that vanished would
 * look like the platform had lost the Answer.
 */
function QuestionRow({ pair, answerable, onSave, onClear }: QuestionRowProps) {
  const t = useTranslations("ticketAnswers");
  // The seven kind names are `ticketTypes`', not this surface's, deliberately:
  // a kind translated per screen is how there come to be two Spanish words for
  // "multiple choice" (messages/README.md).
  const tKinds = useTranslations("ticketTypes");
  const question = pair.question;

  const [value, setValue] = useState(() => initialFieldValue(pair));
  const [checked, setChecked] = useState(() => pair.answer?.checked ?? false);
  const [optionIds, setOptionIds] = useState(() => chosenOptionIds(pair));

  // The stored Answer is the source of truth; a re-read after any write resets
  // the form to what was actually kept rather than to what was typed.
  useEffect(() => {
    setValue(initialFieldValue(pair));
    setChecked(pair.answer?.checked ?? false);
    setOptionIds(chosenOptionIds(pair));
  }, [pair]);

  // A retired question and a frozen Ticket are the same thing to this row: read
  // it, do not write it.
  const editable = answerable && !question.retired;

  const save = (nextOptionIds = optionIds, nextChecked = checked, nextValue = value) => {
    const body = answerBodyFor(question, nextValue, nextChecked, nextOptionIds);
    if (body === null) {
      return;
    }
    void onSave(body);
  };

  return (
    <li className="space-y-1">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        {/* The question is the Organization's own words, read as coined in every
            Locale exactly as a Custom Tag is (ADR 0027). Only the chrome around
            it follows the reader's language. */}
        <span className="font-medium">{question.label}</span>
        <Badge variant="secondary">{tKinds(TICKET_QUESTION_KIND_KEYS[question.kind])}</Badge>
        {question.required ? <Badge variant="outline">{t("requiredBadge")}</Badge> : null}
        {question.retired ? <Badge variant="secondary">{t("retiredBadge")}</Badge> : null}
      </div>

      {question.kind === "checkbox" ? (
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={checked}
            disabled={!editable}
            onChange={(event) => {
              setChecked(event.target.checked);
              // FALSE IS AN ANSWER, so this saves either way rather than only on
              // the way to ticked.
              save(optionIds, event.target.checked);
            }}
          />
          {t("checkboxYes")}
        </label>
      ) : question.kind === "single_choice" || question.kind === "multi_choice" ? (
        <div className="space-y-1">
          {selectableOptions(pair).map((option) => (
            <label key={option.id} className="flex items-center gap-2 text-sm">
              <input
                type={question.kind === "single_choice" ? "radio" : "checkbox"}
                name={`${question.id}-options`}
                checked={optionIds.includes(option.id)}
                disabled={!editable}
                onChange={() => {
                  const next = toggleOption(question, optionIds, option.id);
                  setOptionIds(next);
                  if (next.length > 0) {
                    save(next);
                  } else {
                    // Nothing selected is not an Answer of "none": the way to
                    // say "not said" is to remove the Answer.
                    void onClear();
                  }
                }}
              />
              {/* The Option's CURRENT wording, which is what every list shows.
                  What the person who chose it read is below, and only when the
                  two differ. */}
              <span>{option.label}</span>
              {option.retired ? (
                <Badge variant="secondary">{t("optionRetiredBadge")}</Badge>
              ) : null}
            </label>
          ))}
          <ChosenSnapshots pair={pair} />
        </div>
      ) : (
        <Input
          type={question.kind === "date" ? "date" : "text"}
          inputMode={question.kind === "number" ? "decimal" : undefined}
          aria-label={question.label}
          value={value}
          disabled={!editable}
          onChange={(event) => setValue(event.target.value)}
          onBlur={() => save()}
        />
      )}

      {editable && hasAnswer(pair) ? (
        <Button type="button" variant="ghost" size="sm" onClick={() => void onClear()}>
          {t("clearAnswer")}
        </Button>
      ) : null}
    </li>
  );
}

/**
 * The words an Option showed WHEN IT WAS CHOSEN, drawn only where they differ
 * from what it reads now.
 *
 * This is the whole point of the snapshot. If an Organization renamed `Chicken`
 * to `Chicken (halal)` in July, the person who ticked it in June ticked
 * `Chicken`, and a support conversation about what they agreed to has to be able
 * to say so. Where nothing was renamed the two are the same string and there is
 * nothing to show, so the line appears exactly when it carries information.
 */
function ChosenSnapshots({ pair }: { pair: TicketQuestionAnswer }) {
  const t = useTranslations("ticketAnswers");
  const renamed = (pair.answer?.options ?? []).filter(
    (option) => option.label !== option.current_label,
  );
  if (renamed.length === 0) {
    return null;
  }
  return (
    <p className="text-xs text-muted-foreground">
      {t("chosenAs", { labels: renamed.map((option) => option.label).join(", ") })}
    </p>
  );
}
