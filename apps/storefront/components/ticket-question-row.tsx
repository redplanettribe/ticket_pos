"use client";

import { Button, FormField, Input, Label, Textarea } from "@ticket-pos/ui";
import { useEffect, useRef, useState } from "react";

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
import { saveTrigger } from "@/lib/held-ticket-panel";

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
 * ONE QUESTION, ONE SAVE, on every surface. Each question carries its own save
 * and its own refusal, because the API's unit is one Answer to one Ticket
 * Question and a form that batched them would have to decide what to show when
 * three saved and the fourth was refused.
 *
 * TWO WAYS TO PRESS SAVE. The Assignment Link's accept page keeps an explicit
 * Save button per question. The held-Ticket panel (#345, ADR 0049) has none:
 * in `autosave` mode a choice, a tick box and a date persist on change and
 * text and a number persist on blur (lib/held-ticket-panel.ts, `saveTrigger`),
 * with a quiet, transient "Saved" beside the field. The row is the same row
 * either way; only the press differs. An empty field in autosave mode is
 * simply not sent rather than scolded — there is no button to have pressed —
 * and a blur that changed nothing sends nothing.
 *
 * WHAT IT DOES NOT KNOW is where it is or how the save is addressed. The
 * surfaces authorize completely differently — the accept page relays a signed
 * token in the body and has no session at all, while the Customer Area has a
 * Customer Session cookie and no token anywhere — so the request is the
 * caller's to make. This component turns a form into an `AnswerBody` and hands
 * it over.
 *
 * ITS COPY IS PASSED IN, not read from a namespace. The chrome belongs to
 * whichever surface is drawing it: the accept page words itself to a stranger
 * who was mailed a link, and the Customer Area words itself to the person who
 * holds the Ticket. That is also what keeps this file free of `useTranslations`
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
  /** The Save button's label. Unused, and so optional, in autosave mode. */
  save?: string;
  saved: string;
  /** Shown when the field holds nothing there is any point sending. Unused,
   * and so optional, in autosave mode. */
  nothingToSave?: string;
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
   * and owns turning an API error code into words, because the surfaces map
   * the same code to different copy. Returning the message rather than throwing
   * keeps the failure a value this row can draw beside the field it belongs to.
   */
  save: (body: AnswerBody) => Promise<string | null>;
  /** No Save button: persist on change or on blur by the question's kind. */
  autosave?: boolean;
  /** The window is shut (Event started, Sale reversed): drawn, never editable. */
  disabled?: boolean;
};

export function TicketQuestionRow({
  pair,
  copy,
  save,
  autosave = false,
  disabled = false,
}: TicketQuestionRowProps) {
  const { question } = pair;

  // The fields START ON WHAT IS STORED, which on the buyer's own surface may be
  // what they typed at checkout. It is shown rather than hidden: somebody
  // cannot correct what they cannot see, and what they are correcting is a
  // fact about a person.
  const [value, setValue] = useState(() => initialFieldValue(pair));
  const [checked, setChecked] = useState(() => initialChecked(pair));
  const [optionIds, setOptionIds] = useState(() => chosenOptionIds(pair));
  const [state, setState] = useState<"idle" | "busy" | "saved">("idle");
  const [failure, setFailure] = useState<string | null>(null);

  const readOnly = isReadOnly(pair) || disabled;
  const fieldId = `question-${question.id}`;
  const trigger = saveTrigger(question.kind);

  // The last body accepted, as JSON, so a blur that changed nothing — tabbing
  // through a field — sends nothing. Starts on what is stored.
  const lastSaved = useRef<string | null>(
    JSON.stringify(answerBodyFor(question, value, checked, optionIds)),
  );

  async function submit(next = { value, checked, optionIds }) {
    const body = answerBodyFor(question, next.value, next.checked, next.optionIds);
    if (body === null) {
      // Nothing to send. An empty field is not an Answer of "", and no
      // surface offers a way to take an Answer back — see lib/answer-link.ts.
      // With a button there is a sentence for it; without one there is only
      // an empty field the reader can see for themself.
      if (!autosave) setFailure(copy.nothingToSave ?? "");
      return;
    }
    const serialised = JSON.stringify(body);
    if (autosave && serialised === lastSaved.current) return;

    setState("busy");
    setFailure(null);
    const message = await save(body);
    if (message !== null) {
      setState("idle");
      setFailure(message);
      return;
    }
    lastSaved.current = serialised;
    setState("saved");
  }

  // A change-triggered save fires from the state the change PRODUCED, not from
  // the event: a single_choice's "replace" and a multi_choice's "toggle" live
  // in toggleOption, and the body has to be built from their result.
  const pendingChange = useRef(false);
  useEffect(() => {
    if (!pendingChange.current) return;
    pendingChange.current = false;
    void submit({ value, checked, optionIds });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- runs only after a change-triggered edit
  }, [value, checked, optionIds]);

  // The autosave "Saved" mark is transient: it clears itself a few seconds on,
  // so a panel of six answered questions is not six "Saved"s for ever.
  useEffect(() => {
    if (!autosave || state !== "saved") return;
    const timer = setTimeout(() => setState("idle"), 3000);
    return () => clearTimeout(timer);
  }, [autosave, state]);

  function onBlur() {
    if (autosave && trigger === "blur") void submit();
  }
  function markChanged() {
    if (autosave && trigger === "change") pendingChange.current = true;
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
              markChanged();
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
                  markChanged();
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
              onBlur={onBlur}
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
                // A date is complete the moment it is picked; text is not —
                // markChanged is a no-op for a blur-triggered kind.
                markChanged();
              }}
              onBlur={onBlur}
            />
          }
        </FormField>
      }

      {isReadOnly(pair) ?
        // A retired question, kept on the page only because this Ticket answered
        // it. Shown rather than dropped — a reply that vanished would look like
        // it had been thrown away — and not editable, because the Organization
        // has stopped asking.
        <p className="text-muted-foreground text-sm">{copy.retiredQuestion}</p>
      : disabled ?
        // The window is shut; the panel above says why, once, for every row.
        null
      : autosave ?
        // No button. The mark is the whole confirmation, and it is transient.
        <p className="text-muted-foreground min-h-5 text-sm" aria-live="polite">
          {state === "saved" ? copy.saved : null}
        </p>
      : <div className="flex items-center gap-3">
          <Button size="sm" onClick={() => submit()} disabled={state === "busy"}>
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
