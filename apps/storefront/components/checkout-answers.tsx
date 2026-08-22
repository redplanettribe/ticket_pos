"use client";

import { FormField, Input, Textarea } from "@ticket-pos/ui";

import {
  answerKey,
  type AnswerSlot,
  type AnswerValue,
  type AnswerValues,
  type CheckoutQuestion,
} from "@/lib/checkout-answers";

/**
 * The checkout dialog's answer section: for each ticket in the cart whose Ticket
 * Type asks something, that ticket's questions (#311, ADR 0044).
 *
 * IT IS SKIPPABLE AND IT SAYS SO. There is no validation here, no required
 * marker that blocks, and nothing this component renders is wired to the pay
 * button's disabled state — deliberately, and the reason is the premise the
 * whole feature rests on: the buyer of four tickets is not assumed to know four
 * people's answers, so a form with three blanks is not a way to find out. What a
 * skipped question produces is an Outstanding Answer, which the holder can fill
 * in later through the Ticket's own Answer Link, and which the Organization can
 * see and chase. Anybody wiring a check in here is reversing ADR 0044.
 *
 * ONE SET PER TICKET, NEVER ONE PER LINE. Three of one Ticket Type is three
 * headings and three sets of fields, because an Answer belongs to the Ticket
 * (ADR 0043). The tickets are told apart by their number, which is all there is
 * to tell them apart by — they have no holder, no name and no seat.
 *
 * QUESTION LABELS AND OPTION LABELS ARE NOT TRANSLATED. They are the
 * Organization's own words, read as coined in every Locale exactly as a Custom
 * Tag is (ADR 0027); only the chrome around them follows the page's language.
 */
export function CheckoutAnswers({
  slots,
  values,
  onChange,
  labels,
}: {
  slots: AnswerSlot[];
  values: AnswerValues;
  onChange: (key: string, value: AnswerValue) => void;
  /**
   * The chrome's words, passed in rather than read from a translator in scope so
   * this component can live at module level. That is not a style preference:
   * declared inside the dialog it would be a new component type on every render,
   * and React would remount every input — taking the focus with it, mid-typing.
   * The same reasoning ConsentCheckbox states.
   */
  labels: {
    /** The section heading. */
    title: string;
    /** One line saying the whole section may be skipped. */
    hint: string;
    /** "{ticketType} · ticket {index} of {total}", per ticket. */
    ticketHeading: (ticketTypeName: string, index: number, total: number) => string;
    /** The chip on a question whose answer the Organization is hoping for. */
    optional: string;
    /** The empty entry of a single_choice select. */
    noAnswer: string;
  };
}) {
  if (slots.length === 0) return null;

  // How many of each Ticket Type are in the cart, so a heading can read "ticket
  // 2 of 3" rather than a bare number that means nothing on its own.
  const totals: Record<string, number> = {};
  for (const slot of slots) {
    totals[slot.ticketTypeId] = (totals[slot.ticketTypeId] ?? 0) + 1;
  }

  return (
    <div className="space-y-3 rounded-lg border bg-muted/40 p-3">
      <div>
        <p className="text-sm font-medium">{labels.title}</p>
        {/*
          The hint is not decoration. A buyer looking at a required-looking field
          for somebody else's t-shirt size needs to be told, in the dialog, that
          leaving it is fine — otherwise the honest answer to "I do not know" is
          to guess, and a guessed answer is worse than a missing one.
        */}
        <p className="text-sm text-muted-foreground">{labels.hint}</p>
      </div>
      {slots.map((slot) => (
        <div
          key={`${slot.ticketTypeId}:${slot.index}`}
          className="space-y-3 rounded-lg border bg-background p-3"
        >
          <p className="text-sm font-medium">
            {labels.ticketHeading(slot.ticketTypeName, slot.index, totals[slot.ticketTypeId] ?? 1)}
          </p>
          {slot.questions.map((question) => (
            <AnswerFieldRow
              key={question.id}
              question={question}
              fieldKey={answerKey(slot.ticketTypeId, slot.index, question.id)}
              values={values}
              onChange={onChange}
              labels={labels}
            />
          ))}
        </div>
      ))}
    </div>
  );
}

/**
 * One question's field, drawn for its kind.
 *
 * `required` is rendered as the ABSENCE of an "optional" chip and never as a
 * `required` attribute on the input. That is the honest rendering of a flag
 * whose only effect is producing an Outstanding Answer: the browser's own
 * required attribute would block submission, which is the one thing this feature
 * may never do.
 */
function AnswerFieldRow({
  question,
  fieldKey,
  values,
  onChange,
  labels,
}: {
  question: CheckoutQuestion;
  fieldKey: string;
  values: AnswerValues;
  onChange: (key: string, value: AnswerValue) => void;
  labels: { optional: string; noAnswer: string };
}) {
  const value = values[fieldKey] ?? {};
  const id = `answer-${fieldKey}`;

  if (question.kind === "checkbox") {
    // Its own shape, because a checkbox's label belongs beside the box rather
    // than above it — and because an UNTOUCHED box must send nothing while a
    // deliberately unticked one sends `false`. That distinction lives in
    // checkoutAnswerBodies; what this does is make sure a click always writes a
    // boolean, so "touched" is recorded the moment it happens.
    return (
      <label htmlFor={id} className="flex items-start gap-3 text-sm">
        <input
          id={id}
          type="checkbox"
          className="mt-1 h-4 w-4 shrink-0"
          checked={value.checked ?? false}
          onChange={(event) => onChange(fieldKey, { checked: event.target.checked })}
        />
        <span>
          {question.label}
          {question.required ? null : (
            <span className="ml-2 text-xs text-muted-foreground">{labels.optional}</span>
          )}
        </span>
      </label>
    );
  }

  // One control, chosen by kind and handed to FormField as its ONLY child —
  // FormField clones its child to attach the id and the aria wiring, so a list
  // of conditional siblings would leave every field unlabelled.
  const chosen = value.option_ids ?? [];
  const control =
    question.kind === "long_text" ?
      <Textarea
        rows={3}
        value={value.text ?? ""}
        onChange={(event) => onChange(fieldKey, { text: event.target.value })}
      />
    : question.kind === "number" ?
      <Input
        // A text input with a numeric keypad rather than type="number". The
        // value travels to a NUMERIC column as the digits were TYPED, and a
        // number input hands back a value the browser has already had opinions
        // about — locale-dependent separators, silent rounding, and an empty
        // string for anything it dislikes, which loses what somebody wrote
        // rather than letting the API judge it.
        inputMode="decimal"
        value={value.number ?? ""}
        onChange={(event) => onChange(fieldKey, { number: event.target.value })}
      />
    : question.kind === "date" ?
      <Input
        type="date"
        // A date input emits YYYY-MM-DD, which is the one spelling the API
        // takes — zero-padded, no time and no zone, because a date Answer is a
        // CALENDAR DATE and giving it a time would mean giving it a zone.
        value={value.date ?? ""}
        onChange={(event) => onChange(fieldKey, { date: event.target.value })}
      />
    : question.kind === "single_choice" ?
      <select
        className="h-9 w-full rounded-md border bg-background px-3 text-sm"
        // The empty option is what "I have not said" looks like, and it must
        // stay reachable: a buyer who picked a meal by accident needs a way back
        // to having said nothing, which is not the same as a wrong answer.
        value={chosen[0] ?? ""}
        onChange={(event) =>
          onChange(fieldKey, { option_ids: event.target.value ? [event.target.value] : [] })
        }
      >
        <option value="">{labels.noAnswer}</option>
        {question.options.map((option) => (
          <option key={option.id} value={option.id}>
            {option.label}
          </option>
        ))}
      </select>
    : question.kind === "multi_choice" ?
      <div className="space-y-2">
        {question.options.map((option) => (
          <label
            key={option.id}
            htmlFor={`${id}-${option.id}`}
            className="flex items-start gap-3 text-sm"
          >
            <input
              id={`${id}-${option.id}`}
              type="checkbox"
              className="mt-1 h-4 w-4 shrink-0"
              checked={chosen.includes(option.id)}
              onChange={(event) =>
                onChange(fieldKey, {
                  // Appended rather than rebuilt from the option list, so the
                  // set keeps THE ORDER THE BOXES WERE TICKED IN. That order is
                  // stored and read back, and rebuilding it from the offered
                  // list would silently replace what the buyer did with the
                  // Organization's own sort order.
                  option_ids:
                    event.target.checked ?
                      [...chosen, option.id]
                    : chosen.filter((chosenId) => chosenId !== option.id),
                })
              }
            />
            <span>{option.label}</span>
          </label>
        ))}
      </div>
      // short_text, and the safe shape for a kind this build has not heard of:
      // a one-line text field loses nothing a later reader cannot use, whereas
      // rendering nothing would silently drop a question the buyer was owed.
    : <Input
        value={value.text ?? ""}
        onChange={(event) => onChange(fieldKey, { text: event.target.value })}
      />;

  return (
    <FormField
      id={id}
      label={question.required ? question.label : `${question.label} · ${labels.optional}`}
    >
      {control}
    </FormField>
  );
}
