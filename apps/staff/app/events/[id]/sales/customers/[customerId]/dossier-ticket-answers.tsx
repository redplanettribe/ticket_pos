"use client";

import type { AppLocale } from "@ticket-pos/locale";
import { Button } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import {
  answeredPairs,
  answeredValue,
  lastAnswerReminder,
  ticketAnswersVisible,
  ticketOwes,
  type DossierTicketAnswerFields,
} from "@/lib/dossier-ticket-answers";
import { NOTHING_TO_SHOW, formatCalendarDay, formatDateTime } from "@/lib/format";
import type { TicketQuestionAnswer } from "@/lib/ticket-answers";

type DossierTicketAnswersProps = {
  ticket: DossierTicketAnswerFields;
  /** The Ticket named in words, for the button's accessible name. */
  ticketName: string;
  zone: string;
  locale: AppLocale;
  /** Opens the Answers dialog on this Ticket's Sale. */
  onOpenAnswers: () => void;
};

/**
 * One Ticket's Answers, what it still owes and its last Answer Reminder (#641).
 *
 * Nothing at all while the questions flag is closed. On a Sale that no longer
 * stands the API sends no debt and no reminder, so only the Answers show — and
 * the dialog still opens, because a frozen Ticket stays fully readable there.
 * Nothing here sends mail: the reminder is a fact drawn, never an action.
 */
export function DossierTicketAnswers({ ticket, ticketName, zone, locale, onOpenAnswers }: DossierTicketAnswersProps) {
  const t = useTranslations("customerDossier");

  if (!ticketAnswersVisible(ticket)) {
    return null;
  }

  const answers = answeredPairs(ticket);
  const owes = ticketOwes(ticket);
  const reminder = lastAnswerReminder(ticket);

  return (
    <div className="mt-1.5 space-y-2 rounded-md bg-muted/40 p-2 text-sm">
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">{t("answersLabel")}</div>
        {answers.length === 0 ? (
          <p>{t("answersNone")}</p>
        ) : (
          <dl className="space-y-0.5">
            {answers.map((pair) => (
              <div key={pair.question.id} className="flex min-w-0 flex-wrap gap-x-1.5">
                {/* The Organization's own words, drawn as coined (ADR 0027). */}
                <dt className="break-words text-muted-foreground">{pair.question.label}</dt>
                <dd className="min-w-0 whitespace-pre-line break-words">
                  <AnswerValue pair={pair} locale={locale} />
                </dd>
              </div>
            ))}
          </dl>
        )}
      </div>
      {owes.state !== "hidden" ? (
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">{t("outstandingLabel")}</div>
          {owes.state === "nothing" ? (
            <p>{t("owesNothing")}</p>
          ) : (
            <ul className="break-words">
              {owes.questions.map((question) => (
                <li key={question.question_id}>{question.label}</li>
              ))}
            </ul>
          )}
        </div>
      ) : null}
      {reminder.state !== "hidden" ? (
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">{t("answerReminderLabel")}</div>
          <p>
            {reminder.state === "never"
              ? t("answerReminderNever")
              : (formatDateTime(reminder.at, zone, locale) ?? NOTHING_TO_SHOW)}
          </p>
        </div>
      ) : null}
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={onOpenAnswers}
        aria-label={t("openAnswersFor", { ticket: ticketName })}
      >
        {t("openAnswers")}
      </Button>
    </div>
  );
}

function AnswerValue({ pair, locale }: { pair: TicketQuestionAnswer; locale: AppLocale }) {
  const t = useTranslations("customerDossier");
  const value = answeredValue(pair);
  switch (value.kind) {
    case "text":
      return <>{value.text}</>;
    case "number":
      // The API's decimal string, drawn as its digits: nothing arithmetic here.
      return <span className="tabular-nums">{value.digits}</span>;
    case "date":
      return <>{formatCalendarDay(value.day, locale)}</>;
    case "checkbox":
      return <>{value.checked ? t("answerYes") : t("answerNo")}</>;
    case "choice":
      return <>{value.labels.join(", ")}</>;
    case "unreadable":
      return <span className="text-muted-foreground">{NOTHING_TO_SHOW}</span>;
  }
}
