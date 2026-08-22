"use client";

import { Alert, AlertDescription, AlertTitle } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useState } from "react";

import { TicketQuestionRow } from "@/components/ticket-question-row";
import { visibleQuestions, type AnswerLinkView } from "@/lib/answer-link";
import { apiErrorMessage } from "@/lib/api-errors";

/**
 * The Ticket Questions on an Answer Link's page, and the press that answers one
 * (#312, ADR 0044).
 *
 * ONE QUESTION, ONE SAVE. Each question carries its own button and its own
 * refusal, and there is no "save all" — because the API's unit is one Answer to
 * one Ticket Question, and a form that batched them would have to decide what to
 * show when three saved and the fourth was refused. It also matches how this
 * page is actually used: somebody opens it in a chat app to say one size.
 *
 * AN ANSWER GIVEN HERE REPLACES ONE THE BUYER GAVE AT CHECKOUT, and the fields
 * therefore START ON WHAT IS STORED. The buyer's guess is shown rather than
 * hidden, because somebody cannot correct a guess they cannot see — and what
 * they are correcting is a fact about themselves.
 *
 * NOTHING HERE IS BLOCKED FOR WANT OF AN ANSWER. "Required" on this platform
 * means OUTSTANDING and never BLOCKING (ADR 0044): a required question is marked
 * as one the Organization is waiting on, and no control is disabled by it. There
 * is also NO WAY TO CLEAR an Answer — whoever holds the link may overwrite what
 * is there, and erasing somebody else's reply from an unauthenticated page is
 * not a power this link grants. The buyer and Event Staff keep that.
 *
 * THE QUESTION AND OPTION LABELS ARE DRAWN AS COINED, never translated. The
 * Organization wrote them, like a Custom Tag (ADR 0027); only this component's
 * own chrome follows the reader's Locale.
 */
type AnswerLinkFormProps = {
  /** The signed token the link carried. Never inspected here; only relayed. */
  token: string;
  view: AnswerLinkView;
};

export function AnswerLinkForm({ token, view }: AnswerLinkFormProps) {
  const t = useTranslations("answerLink");
  const errorCopy = useMessages().errors;
  const [questions, setQuestions] = useState(() => visibleQuestions(view));

  if (questions.length === 0) {
    // A Ticket Type whose questions were all retired, or one that never had any.
    // Said plainly rather than left as an empty page: somebody who followed a
    // link and found nothing needs to know they have not landed wrong.
    return (
      <Alert>
        <AlertTitle>{t("nothingAskedTitle")}</AlertTitle>
        <AlertDescription>{t("nothingAskedDescription")}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-8">
      <p className="text-muted-foreground text-sm">{t("intro")}</p>
      {questions.map((pair) => (
        <TicketQuestionRow
          key={pair.question.id}
          pair={pair}
          copy={{
            requiredMark: (question: string) => t("requiredMark", { question }),
            save: t("save"),
            saved: t("saved"),
            nothingToSave: t("nothingToSave"),
            retiredQuestion: t("retiredQuestion"),
          }}
          // The REQUEST is this surface's, because its credential is: a signed
          // token in the body and no session anywhere. The shared row knows how
          // to turn a form into a body and nothing about how it is addressed.
          save={async (body) => {
            try {
              const response = await fetch(`/api/answer-link/questions/${pair.question.id}`, {
                method: "PUT",
                headers: { "Content-Type": "application/json" },
                // The token travels in the BODY, not the address, on the browser
                // leg too: a credential in a URL ends up in a Referer header on
                // the way to wherever the reader goes next.
                body: JSON.stringify({ token, ...body }),
              });
              const envelope = (await response.json()) as {
                data: AnswerLinkView | null;
                error: { code: string; message: string; details?: unknown } | null;
              };
              if (!response.ok || envelope.error || !envelope.data) {
                return apiErrorMessage(errorCopy, envelope.error) ?? t("saveFailed");
              }
              // A save answers with the WHOLE Ticket, so the whole list is
              // replaced from one payload rather than patched question by
              // question. That is what keeps this page honest after a save: if
              // Event Staff corrected another question a moment ago, the reader
              // sees it.
              setQuestions(visibleQuestions(envelope.data));
              return null;
            } catch {
              return t("networkFailed");
            }
          }}
        />
      ))}
    </div>
  );
}

