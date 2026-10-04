"use client";

import { FormField, Input, Textarea } from "@ticket-pos/ui";

import {
  answerKey,
  type AnswerSlot,
  type AnswerValue,
  type AnswerValues,
  type CheckoutQuestion,
} from "@/lib/checkout-answers";
import { MAX_HOLDER_EMAIL_LENGTH } from "@/lib/ticket-assignment";

/**
 * One Ticket's panel in the checkout dialog: its questions and, on an Event
 * that requires Named Tickets, the address of whoever will hold it (#311, ADR
 * 0044, ADR 0048, ADR 0076).
 *
 * ORDINARILY THERE IS ONE, for the buyer's OWN Ticket. The sale hands the buyer
 * the dearest of its Tickets as their own (ADR 0074), so these are questions
 * about the buyer, which the buyer can answer. The cart's other tickets are not
 * mentioned: an Answer belongs to the Ticket (ADR 0043), and those Tickets'
 * Holders give theirs after the purchase. It is skippable and says so, and
 * nothing in it is wired to the pay button: a skipped question becomes an
 * Outstanding Answer the buyer can fill in later from their sale page. Wiring a
 * check in on an Event without Named Tickets would be reversing ADR 0044.
 *
 * ON AN EVENT THAT REQUIRES NAMED TICKETS THERE IS ONE PER TICKET (ADR 0076),
 * and the dialog holds the pay button until they owe nothing. Every Ticket but
 * the buyer's own carries `holder`, an address field above its own Ticket
 * Type's questions. What is owed is judged in lib/named-tickets.ts and the
 * dialog: this component only draws the errors it is handed.
 *
 * QUESTION LABELS AND OPTION LABELS ARE NOT TRANSLATED. They are the
 * Organization's own words, read as coined in every Locale exactly as a Custom
 * Tag is (ADR 0027); only the chrome around them follows the page's language.
 */
export function CheckoutAnswers({
  slot,
  values,
  onChange,
  onBlur,
  questionErrors,
  holder,
  labels,
}: {
  slot: AnswerSlot;
  values: AnswerValues;
  onChange: (key: string, value: AnswerValue) => void;
  /** Told the answerKey of a field the buyer has left, so its error may show. */
  onBlur?: (key: string) => void;
  /** The sentence under each question that has one, by question id. */
  questionErrors?: Readonly<Record<string, string>>;
  /** The Holder's address field, on a Named Tickets Event's other Tickets. */
  holder?: {
    value: string;
    onChange: (value: string) => void;
    onBlur: () => void;
    error?: string;
    /** A line under the field, or nothing: the buyer's own address is not mailed. */
    notice?: string;
    label: string;
    placeholder: string;
  };
  /**
   * The chrome's words, passed in rather than read from a translator in scope so
   * this component can live at module level. That is not a style preference:
   * declared inside the dialog it would be a new component type on every render,
   * and React would remount every input — taking the focus with it, mid-typing.
   * The same reasoning ConsentCheckbox states.
   */
  labels: {
    /** The panel heading: "Your ticket · {ticketType}", or another Ticket's. */
    title: string;
    /** One line beside the heading, or null for none. */
    hint: string | null;
    /** The chip on a question whose answer the Organization is hoping for. */
    optional: string;
    optionalLabel: (question: string) => string;
    /** The empty entry of a single_choice select. */
    noAnswer: string;
  };
}) {
  return (
    <div className="space-y-3 rounded-lg border bg-muted/40 p-3">
      <div className="flex flex-wrap items-baseline justify-between gap-x-3">
        <p className="text-sm font-medium">{labels.title}</p>
        {/* Said once, beside the heading. */}
        {labels.hint ? <p className="text-xs text-muted-foreground">{labels.hint}</p> : null}
      </div>
      {holder ?
        <FormField
          id={`holder-${slot.ticketTypeId}-${slot.index}`}
          label={holder.label}
          description={holder.notice}
          error={holder.error}
        >
          <Input
            type="email"
            inputMode="email"
            // Somebody else's address, so not the browser's suggestion: that
            // would be the buyer's own, the one address this field never needs.
            autoComplete="off"
            maxLength={MAX_HOLDER_EMAIL_LENGTH}
            placeholder={holder.placeholder}
            value={holder.value}
            onChange={(event) => holder.onChange(event.target.value)}
            onBlur={holder.onBlur}
          />
        </FormField>
      : null}
      {slot.questions.map((question) => {
        const fieldKey = answerKey(slot.ticketTypeId, slot.index, question.id);
        return (
          <AnswerFieldRow
            key={question.id}
            question={question}
            fieldKey={fieldKey}
            values={values}
            onChange={onChange}
            onBlur={onBlur ? () => onBlur(fieldKey) : undefined}
            error={questionErrors?.[question.id]}
            labels={labels}
          />
        );
      })}
    </div>
  );
}

/**
 * One question's field, drawn for its kind.
 *
 * `required` is rendered as the ABSENCE of an "optional" chip and never as a
 * `required` attribute on the input. The browser's own required attribute would
 * block submission by itself, and whether a required question blocks anything
 * is not the field's to decide: on most Events it only produces an Outstanding
 * Answer, and on a Named Tickets Event the dialog holds the pay button and says
 * why (lib/named-tickets.ts).
 */
function AnswerFieldRow({
  question,
  fieldKey,
  values,
  onChange,
  onBlur,
  error,
  labels,
}: {
  question: CheckoutQuestion;
  fieldKey: string;
  values: AnswerValues;
  onChange: (key: string, value: AnswerValue) => void;
  onBlur?: () => void;
  error?: string;
  labels: { optional: string; optionalLabel: (question: string) => string; noAnswer: string };
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
      <div className="space-y-2" onBlur={onBlur}>
        <label htmlFor={id} className="flex items-start gap-3 text-sm">
          <input
            id={id}
            type="checkbox"
            className="mt-1 h-4 w-4 shrink-0"
            checked={value.checked ?? false}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? `${id}-error` : undefined}
            onChange={(event) => onChange(fieldKey, { checked: event.target.checked })}
          />
          <span>
            {question.label}
            {question.required ? null : (
              <span className="ml-2 text-xs text-muted-foreground">{labels.optional}</span>
            )}
          </span>
        </label>
        {error ?
          <p id={`${id}-error`} role="alert" className="text-sm text-destructive">
            {error}
          </p>
        : null}
      </div>
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

  // Wrapped so a blur anywhere inside, a multi_choice's boxes included, reports
  // the field as left. React's onBlur bubbles, as the DOM's focusout does.
  return (
    <div onBlur={onBlur}>
      <FormField
        id={id}
        label={question.required ? question.label : labels.optionalLabel(question.label)}
        error={error}
      >
        {control}
      </FormField>
    </div>
  );
}
