"use client";

import { Button, FormField, Input, Label, Textarea } from "@ticket-pos/ui";
import { useState } from "react";

import {
  answerBodyFor,
  chosenOptionIds,
  initialChecked,
  initialFieldValue,
  isReadOnly,
  selectableOptions,
  toggleOption,
  type AnswerBody,
  type QuestionAnswer,
} from "@/lib/answer-link";

/**
 * ONE Ticket Question and the press that answers it — the control shared by
 * every surface a Ticket Question is answered on (#312, #315, ADR 0044).
 *
 * IT WAS EXTRACTED FROM THE ANSWER LINK'S FORM RATHER THAN COPIED BESIDE IT.
 * The holder answering through a forwarded link and the buyer answering from
 * their own Customer Area are answering THE SAME QUESTION about the same Ticket,
 * against the same catalog.ParseAnswer on the far side. Two components would be
 * two opinions about which keyboard a number question opens, whether a retired
 * Option stays on the form, and whether `checked: false` is an Answer — and the
 * third of those is a data bug rather than a cosmetic one.
 *
 * ONE QUESTION, ONE SAVE, on both surfaces. Each question carries its own button
 * and its own refusal, because the API's unit is one Answer to one Ticket
 * Question and a form that batched them would have to decide what to show when
 * three saved and the fourth was refused.
 *
 * WHAT IT DOES NOT KNOW is where it is or how the save is addressed. The two
 * surfaces authorize completely differently — the Answer Link relays a signed
 * token in the body and has no session at all, while the buyer's page has a
 * Customer Session cookie and no token anywhere — so the request is the caller's
 * to make. This component turns a form into an `AnswerBody` and hands it over.
 *
 * ITS COPY IS PASSED IN, not read from a namespace. The chrome belongs to
 * whichever surface is drawing it: the Answer Link's page words itself to a
 * stranger who was forwarded a link, and the Customer Area words itself to the
 * person who paid. That is also what keeps this file free of `useTranslations`
 * and therefore usable from either namespace — the same arrangement
 * components/checkout-answers.tsx already has.
 *
 * THE QUESTION AND OPTION LABELS ARE DRAWN AS COINED, never translated. The
 * Organization wrote them, like a Custom Tag (ADR 0027), and messages/README.md
 * is explicit that they must never enter the message catalog.
 *
 * NOTHING HERE IS BLOCKED FOR WANT OF AN ANSWER. "Required" means OUTSTANDING
 * and never BLOCKING (ADR 0044): it is marked, and no control is disabled by it.
 */
export type TicketQuestionRowCopy = {
  /** Appended to a required question's label — a mark, not a sentence. */
  requiredMark: (question: string) => string;
  save: string;
  saved: string;
  /** Shown when the field holds nothing there is any point sending. */
  nothingToSave: string;
  /** Shown beside a retired question, which is read-only. */
  retiredQuestion: string;
};

export type TicketQuestionRowProps = {
  pair: QuestionAnswer;
  copy: TicketQuestionRowCopy;
  /**
   * Writes the Answer, and returns null on success or a sentence to show on
   * failure.
   *
   * The CALLER owns the request — its address, its credential and its refusals —
   * and owns turning an API error code into words, because the two surfaces map
   * the same code to different copy. Returning the message rather than throwing
   * keeps the failure a value this row can draw beside the field it belongs to.
   */
  save: (body: AnswerBody) => Promise<string | null>;
};

export function TicketQuestionRow({ pair, copy, save }: TicketQuestionRowProps) {
  const { question } = pair;

  // The fields START ON WHAT IS STORED, which on the buyer's own surface may be
  // what they typed at checkout and on the Answer Link's may be what the buyer
  // guessed. Either way it is shown rather than hidden: somebody cannot correct
  // a guess they cannot see, and what they are correcting is a fact about a
  // person.
  const [value, setValue] = useState(() => initialFieldValue(pair));
  const [checked, setChecked] = useState(() => initialChecked(pair));
  const [optionIds, setOptionIds] = useState(() => chosenOptionIds(pair));
  const [state, setState] = useState<"idle" | "busy" | "saved">("idle");
  const [failure, setFailure] = useState<string | null>(null);

  const readOnly = isReadOnly(pair);
  const fieldId = `question-${question.id}`;

  async function submit() {
    const body = answerBodyFor(question, value, checked, optionIds);
    if (body === null) {
      // Nothing to send. An empty field is not an Answer of "", and neither
      // surface offers a way to take an Answer back — see lib/answer-link.ts.
      setFailure(copy.nothingToSave);
      return;
    }

    setState("busy");
    setFailure(null);
    const message = await save(body);
    if (message !== null) {
      setState("idle");
      setFailure(message);
      return;
    }
    setState("saved");
  }

  const label = question.required ? copy.requiredMark(question.label) : question.label;

  return (
    <div className="space-y-3 border-b pb-6 last:border-b-0 last:pb-0">
      {question.kind === "checkbox" ?
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
      : question.kind === "single_choice" || question.kind === "multi_choice" ?
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
      : <FormField id={fieldId} label={label}>
          {question.kind === "long_text" ?
            <Textarea
              value={value}
              disabled={readOnly || state === "busy"}
              onChange={(event) => {
                setValue(event.target.value);
                setState("idle");
              }}
            />
          : <Input
              type={question.kind === "date" ? "date" : "text"}
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
          }
        </FormField>
      }

      {readOnly ?
        // A retired question, kept on the page only because this Ticket answered
        // it. Shown rather than dropped — a reply that vanished would look like
        // it had been thrown away — and not editable, because the Organization
        // has stopped asking.
        <p className="text-muted-foreground text-sm">{copy.retiredQuestion}</p>
      : <div className="flex items-center gap-3">
          <Button size="sm" onClick={submit} disabled={state === "busy"}>
            {copy.save}
          </Button>
          {state === "saved" ?
            <span className="text-muted-foreground text-sm">{copy.saved}</span>
          : null}
        </div>
      }

      {failure ?
        <p role="alert" className="text-destructive text-sm">
          {failure}
        </p>
      : null}
    </div>
  );
}
