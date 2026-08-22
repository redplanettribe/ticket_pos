"use client";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  FormField,
  Input,
  Label,
  Textarea,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useState } from "react";

import {
  answerBodyFor,
  chosenOptionIds,
  initialChecked,
  initialFieldValue,
  isReadOnly,
  selectableOptions,
  toggleOption,
  visibleQuestions,
  type AnswerLinkView,
  type QuestionAnswer,
} from "@/lib/answer-link";
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
        <QuestionRow
          key={pair.question.id}
          token={token}
          pair={pair}
          // A save answers with the WHOLE Ticket, so the whole list is replaced
          // from one payload rather than patched question by question. That is
          // what keeps this page honest after a save: if Event Staff corrected
          // another question a moment ago, the reader sees it.
          onSaved={(next) => setQuestions(visibleQuestions(next))}
        />
      ))}
    </div>
  );
}

type QuestionRowProps = {
  token: string;
  pair: QuestionAnswer;
  onSaved: (view: AnswerLinkView) => void;
};

function QuestionRow({ token, pair, onSaved }: QuestionRowProps) {
  const t = useTranslations("answerLink");
  const errorCopy = useMessages().errors;
  const { question } = pair;

  const [value, setValue] = useState(() => initialFieldValue(pair));
  const [checked, setChecked] = useState(() => initialChecked(pair));
  const [optionIds, setOptionIds] = useState(() => chosenOptionIds(pair));
  const [state, setState] = useState<"idle" | "busy" | "saved">("idle");
  const [failure, setFailure] = useState<string | null>(null);

  const readOnly = isReadOnly(pair);
  const fieldId = `question-${question.id}`;

  async function save() {
    const body = answerBodyFor(question, value, checked, optionIds);
    if (body === null) {
      // Nothing to send. An empty field is not an Answer of "", and this page
      // has no way to take one back — see lib/answer-link.ts.
      setFailure(t("nothingToSave"));
      return;
    }

    setState("busy");
    setFailure(null);
    try {
      const response = await fetch(`/api/answer-link/questions/${question.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        // The token travels in the BODY, not the address, on the browser leg
        // too: a credential in a URL ends up in a Referer header on the way to
        // wherever the reader goes next.
        body: JSON.stringify({ token, ...body }),
      });
      const envelope = (await response.json()) as {
        data: AnswerLinkView | null;
        error: { code: string; message: string; details?: unknown } | null;
      };
      if (!response.ok || envelope.error || !envelope.data) {
        setState("idle");
        setFailure(apiErrorMessage(errorCopy, envelope.error) ?? t("saveFailed"));
        return;
      }
      setState("saved");
      onSaved(envelope.data);
    } catch {
      setState("idle");
      setFailure(t("networkFailed"));
    }
  }

  const label = question.required ? `${question.label} ${t("requiredMark")}` : question.label;

  return (
    <div className="space-y-3 border-b pb-6 last:border-b-0 last:pb-0">
      {question.kind === "checkbox" ? (
        <div className="flex items-start gap-3">
          <input
            id={fieldId}
            type="checkbox"
            className="mt-1 size-4"
            checked={checked}
            disabled={readOnly || state === "busy"}
            onChange={(event) => {
              setChecked(event.target.checked);
              setState("idle");
            }}
          />
          {/* The question's own words, as coined. */}
          <Label htmlFor={fieldId}>{label}</Label>
        </div>
      ) : question.kind === "single_choice" || question.kind === "multi_choice" ? (
        <fieldset className="space-y-2">
          <legend className="text-sm font-medium">{label}</legend>
          {selectableOptions(pair).map((option) => (
            <div key={option.id} className="flex items-start gap-3">
              <input
                id={`${fieldId}-${option.id}`}
                // The control matches the kind, so the browser enforces "one" on
                // a single_choice before the API has to.
                type={question.kind === "single_choice" ? "radio" : "checkbox"}
                name={fieldId}
                className="mt-1 size-4"
                checked={optionIds.includes(option.id)}
                disabled={readOnly || state === "busy"}
                onChange={() => {
                  setOptionIds((current) => toggleOption(question, current, option.id));
                  setState("idle");
                }}
              />
              <Label htmlFor={`${fieldId}-${option.id}`}>
                {/* The Option's CURRENT words. What this Answer read when it
                    chose is kept on the Answer and is a fact for a support
                    conversation, not something to show the person now. */}
                {option.label}
              </Label>
            </div>
          ))}
        </fieldset>
      ) : (
        <FormField id={fieldId} label={label}>
          {question.kind === "long_text" ? (
            <Textarea
              value={value}
              disabled={readOnly || state === "busy"}
              onChange={(event) => {
                setValue(event.target.value);
                setState("idle");
              }}
            />
          ) : (
            <Input
              // The kind picks the keyboard a phone opens with, which is most of
              // what these three differ by on the device this page is read on.
              type={
                question.kind === "number" ? "text"
                : question.kind === "date" ? "date"
                : "text"
              }
              // `text` and not `number` for a number question, deliberately: a
              // number input returns "" for anything it dislikes, so a stray
              // character would silently blank what somebody typed. The digits
              // go to the API as typed and the API decides — inputMode still
              // opens the numeric keyboard.
              inputMode={question.kind === "number" ? "decimal" : undefined}
              value={value}
              disabled={readOnly || state === "busy"}
              onChange={(event) => {
                setValue(event.target.value);
                setState("idle");
              }}
            />
          )}
        </FormField>
      )}

      {readOnly ? (
        // A retired question, kept on the page only because this Ticket answered
        // it. Shown rather than dropped — a reply that vanished would look like
        // it had been thrown away — and not editable, because the Organization
        // has stopped asking.
        <p className="text-muted-foreground text-sm">{t("retiredQuestion")}</p>
      ) : (
        <div className="flex items-center gap-3">
          <Button size="sm" onClick={save} disabled={state === "busy"}>
            {t("save")}
          </Button>
          {state === "saved" ? (
            <span className="text-muted-foreground text-sm">{t("saved")}</span>
          ) : null}
        </div>
      )}

      {failure ? (
        <p role="alert" className="text-destructive text-sm">
          {failure}
        </p>
      ) : null}
    </div>
  );
}
