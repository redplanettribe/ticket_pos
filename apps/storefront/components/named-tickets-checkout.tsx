"use client";

import { useMessages, useTranslations } from "next-intl";
import { useState } from "react";

import { CheckoutAnswers } from "@/components/checkout-answers";
import { apiErrorMessage, fieldErrorMessages } from "@/lib/api-errors";
import type { AnsweredTicketType, AnswerValue, AnswerValues } from "@/lib/checkout-answers";
import {
  checkoutHolderBodies,
  holderKey,
  holderKeyOfField,
  namedTicketsApply,
  namedTicketSlots,
  namedTicketsOwed,
  questionErrorCopy,
  questionErrorKinds,
  refusedTickets,
  withDrawnCheckboxes,
  type CheckoutHolderBody,
  type HolderValues,
  type NamedTicketSlot,
  type OwedTicket,
  type RefusedTicket,
} from "@/lib/named-tickets";
import { isBuyersOwnAddress } from "@/lib/ticket-assignment";

/**
 * The Named Tickets part of the checkout dialog (ADR 0076, #674): its state,
 * its words, and the two places it is drawn.
 *
 * Where the requirement binds, the form asks about EVERY Ticket - an address
 * for each but the buyer's own, and each one's own Ticket Type's questions -
 * and the pay button waits until `owed` is empty. Where it does not bind,
 * `named` is false, nothing here draws, and the dialog is the ordinary one:
 * the buyer's own Ticket's skippable questions. What is owed is judged in
 * lib/named-tickets.ts; this file keeps what the buyer typed and says it.
 *
 * THE ANSWERS ARE THE DIALOG'S, not this hook's. One map of replies serves the
 * ordinary panel and these alike, and is cleared with the cart, so the dialog
 * owns it and hands it in. The addresses and the verdicts on them are only
 * ever Named Tickets', so they live here.
 */
export type NamedTicketsCheckout = {
  /** Whether the requirement binds this checkout at all. */
  named: boolean;
  /** Every Ticket the basket will mint, or none where `named` is false. */
  slots: NamedTicketSlot[];
  /** What the form still owes before it may be paid for. Empty unless `named`. */
  owed: OwedTicket[];
  /**
   * The answers as they travel and are drawn: on a Named Tickets checkout, a
   * required checkbox answered by the state it is drawn in; otherwise as the
   * buyer left them.
   */
  sentAnswers: AnswerValues;
  /** The body's `holders`, or none where `named` is false. */
  holderBodies: () => CheckoutHolderBody[];
  /** Forget what the API last said, as a new attempt begins. */
  clearVerdicts: () => void;
  /**
   * Put a refused checkout's findings back on the fields they are about: a
   * NAMED_TICKETS_INCOMPLETE refusal's owed Tickets, and a field error on
   * `holders[i]` onto the Ticket that entry was sent for.
   */
  takeRefusal: (
    error: { code: string; details?: unknown } | null | undefined,
    sent: CheckoutHolderBody[],
  ) => void;
  /** Forget the addresses and everything said of them, with the cart they named. */
  reset: () => void;
  // What the panels draw.
  holders: HolderValues;
  changeAnswer: (key: string, value: AnswerValue) => void;
  changeHolder: (key: string, value: string) => void;
  leaveField: (key: string) => void;
  title: (slot: NamedTicketSlot) => string;
  holderError: (key: string) => string | undefined;
  questionErrors: (slot: NamedTicketSlot) => Record<string, string>;
  owedLine: (ticket: OwedTicket) => string;
};

export function useNamedTicketsCheckout({
  requiresNamedTickets,
  ticketAssignmentEnabled,
  startsAt,
  now,
  ticketTypes,
  quantities,
  surrendered,
  answers,
  setAnswers,
}: {
  requiresNamedTickets: boolean;
  /** The Event payload's `buyer_holds_first_ticket`. */
  ticketAssignmentEnabled: boolean;
  startsAt: string | null;
  now: Date;
  ticketTypes: AnsweredTicketType[];
  quantities: Record<string, number>;
  /** The free line an elected Upgrade gives up, which is asked nothing. */
  surrendered: string | null;
  answers: AnswerValues;
  setAnswers: (update: (current: AnswerValues) => AnswerValues) => void;
}): NamedTicketsCheckout {
  const t = useTranslations("checkout");
  // The error catalog as plain data rather than through `t`: its keys are API
  // codes, which arrive as strings at runtime and cannot be typed message keys.
  const errorCopy = useMessages().errors;

  // The Holder addresses typed on a Named Tickets checkout (ADR 0076), keyed by
  // (Ticket Type, ticket number) like the answers and kept and cleared on the
  // same terms, so a Ticket's address survives the buyer changing another
  // Ticket Type's quantity.
  const [holders, setHolders] = useState<HolderValues>({});
  // The fields the buyer has left, by holderKey or answerKey. A malformed
  // address or an unusable answer is said under its field only once the buyer
  // has moved on from it, so nobody is told off for a half-typed address.
  const [leftFields, setLeftFields] = useState<Record<string, true>>({});
  // What a NAMED_TICKETS_INCOMPLETE refusal said each Ticket owes, by
  // holderKey, and the API's field errors on `holders[i]`, by holderKey. Each
  // piece is dropped as soon as the buyer edits the field it is about.
  const [refused, setRefused] = useState<Record<string, RefusedTicket>>({});
  const [holderFieldErrors, setHolderFieldErrors] = useState<Record<string, string>>({});

  const named = namedTicketsApply({ requiresNamedTickets, ticketAssignmentEnabled, startsAt, now });
  const slots = named ? namedTicketSlots(ticketTypes, quantities, surrendered) : [];
  const owed = namedTicketsOwed({
    requiresNamedTickets,
    ticketAssignmentEnabled,
    startsAt,
    now,
    ticketTypes,
    quantities,
    surrenderedTicketTypeId: surrendered,
    holders,
    answers,
  });
  const sentAnswers = named ? withDrawnCheckboxes(slots, answers) : answers;

  const owedByTicket = new Map<string, OwedTicket>(
    owed.map((ticket) => [holderKey(ticket.ticketTypeId, ticket.index), ticket]),
  );

  function clearVerdicts() {
    setRefused({});
    setHolderFieldErrors({});
  }

  function takeRefusal(
    error: { code: string; details?: unknown } | null | undefined,
    sent: CheckoutHolderBody[],
  ) {
    // A Named Tickets refusal names each Ticket and what it still owes;
    // a malformed address is a field error on the entry that carried it.
    // Both go onto the fields they are about.
    if (error?.code === "NAMED_TICKETS_INCOMPLETE") {
      setRefused(refusedTickets(error.details));
    }
    const holderErrors: Record<string, string> = {};
    for (const [field, message] of Object.entries(fieldErrorMessages(errorCopy, error?.details))) {
      const key = holderKeyOfField(field, sent);
      // The only field error an address can earn is that it is not one,
      // and the field's catalog entry is worded to follow a label. The
      // sale page's whole sentence for the same refusal reads better
      // under a field of its own.
      if (key !== null) {
        holderErrors[key] = apiErrorMessage(errorCopy, { code: "INVALID_HOLDER_EMAIL" }) ?? message;
      }
    }
    setHolderFieldErrors(holderErrors);
  }

  function reset() {
    setHolders({});
    setLeftFields({});
    setRefused({});
    setHolderFieldErrors({});
  }

  function leaveField(key: string) {
    setLeftFields((current) => (current[key] ? current : { ...current, [key]: true }));
  }

  /** One answer typed: kept, and whatever the refusal said of it withdrawn. */
  function changeAnswer(key: string, value: AnswerValue) {
    setAnswers((current) => ({ ...current, [key]: value }));
    // The answerKey is the Ticket's holderKey with the question id after it.
    const separator = key.lastIndexOf(":");
    const ticket = key.slice(0, separator);
    const questionId = key.slice(separator + 1);
    setRefused((current) => {
      const entry = current[ticket];
      if (!entry?.missingQuestionIds.includes(questionId)) return current;
      return {
        ...current,
        [ticket]: {
          ...entry,
          missingQuestionIds: entry.missingQuestionIds.filter((id) => id !== questionId),
        },
      };
    });
  }

  /** One address typed: kept, and whatever the API said of the last one withdrawn. */
  function changeHolder(key: string, value: string) {
    setHolders((current) => ({ ...current, [key]: value }));
    setRefused((current) =>
      current[key]?.holderEmailMissing ?
        { ...current, [key]: { ...current[key], holderEmailMissing: false } }
      : current,
    );
    setHolderFieldErrors((current) => {
      if (!(key in current)) return current;
      const rest = { ...current };
      delete rest[key];
      return rest;
    });
  }

  /** A Ticket's heading, which the owed list names it by too. */
  function title(slot: NamedTicketSlot): string {
    return slot.selfHeld ?
        t("answers.title", { ticketType: slot.ticketTypeName })
      : t("named.ticketTitle", { ticketType: slot.ticketTypeName, number: slot.index });
  }

  /**
   * The sentence under the address field, if any: the API's own verdict on what
   * was sent first, then the refusal's "still needed", then this page's check
   * on an address the buyer has moved on from.
   */
  function holderError(key: string): string | undefined {
    const value = holders[key] ?? "";
    if (holderFieldErrors[key]) return holderFieldErrors[key];
    if (refused[key]?.holderEmailMissing && value.trim() === "") return t("named.emailNeeded");
    if (leftFields[key] && owedByTicket.get(key)?.holderEmail === "invalid") {
      return (
        apiErrorMessage(errorCopy, { code: "INVALID_HOLDER_EMAIL" }) ?? t("named.emailInvalid")
      );
    }
    return undefined;
  }

  /**
   * The sentence under each of a Ticket's questions that has one. "Needed" is
   * what the refusal said, until the buyer edits the field; "invalid" is said
   * only of a reply the buyer gave and the server would drop. A blank one is
   * listed by the summary above the button instead.
   */
  function questionErrors(slot: NamedTicketSlot): Record<string, string> {
    const ticket = holderKey(slot.ticketTypeId, slot.index);
    const kinds = questionErrorKinds(slot, answers, {
      needed: (question) => refused[ticket]?.missingQuestionIds.includes(question.id) ?? false,
      left: leftFields,
    });
    return questionErrorCopy(kinds, {
      answerNeeded: t("named.answerNeeded"),
      answerInvalid: t("named.answerInvalid"),
    });
  }

  /** One line of the owed list: which Ticket, and what it lacks. */
  function owedLine(ticket: OwedTicket): string {
    const slot = slots.find(
      (candidate) =>
        candidate.ticketTypeId === ticket.ticketTypeId && candidate.index === ticket.index,
    );
    const missing: string[] = [];
    if (ticket.holderEmail === "missing") missing.push(t("named.owedEmail"));
    if (ticket.holderEmail === "invalid") missing.push(t("named.owedEmailInvalid"));
    for (const questionId of ticket.missingQuestionIds) {
      const label = slot?.questions.find((question) => question.id === questionId)?.label;
      if (label) missing.push(label);
    }
    return t("named.owedLine", {
      ticket: slot ? title(slot) : "",
      missing: missing.join(", "),
    });
  }

  return {
    named,
    slots,
    owed,
    sentAnswers,
    holderBodies: () => (named ? checkoutHolderBodies(slots, holders) : []),
    clearVerdicts,
    takeRefusal,
    reset,
    holders,
    changeAnswer,
    changeHolder,
    leaveField,
    title,
    holderError,
    questionErrors,
    owedLine,
  };
}

/**
 * Every Ticket's panel on a Named Tickets checkout, the buyer's own first. It
 * owes its required Answers and no address, since the buyer is its Holder;
 * every other Ticket owes an address and its own Ticket Type's required
 * Answers. Draws nothing where the requirement does not bind, so an Event with
 * the setting off, or one that has started, keeps the ordinary dialog.
 *
 * The buyer's own panel is drawn even when it asks nothing, whenever there are
 * other Tickets: it is what says which Ticket needs no address, and why the
 * others are numbered as they are.
 */
export function NamedTicketPanels({
  checkout,
  buyerEmail,
}: {
  checkout: NamedTicketsCheckout;
  /** The session's own address, or null when unknown. */
  buyerEmail: string | null;
}) {
  const t = useTranslations("checkout");
  if (!checkout.named) return null;

  const own = checkout.slots.find((slot) => slot.selfHeld) ?? null;
  const others = checkout.slots.filter((slot) => !slot.selfHeld);
  const answerLabels = {
    optional: t("answers.optional"),
    optionalLabel: (question: string) => t("answers.optionalLabel", { question }),
    noAnswer: t("answers.noAnswer"),
  };

  return (
    <>
      {own !== null && (own.questions.length > 0 || others.length > 0) ?
        <CheckoutAnswers
          slot={own}
          values={checkout.sentAnswers}
          onChange={checkout.changeAnswer}
          onBlur={checkout.leaveField}
          questionErrors={checkout.questionErrors(own)}
          labels={{
            title: checkout.title(own),
            hint: others.length > 0 ? t("named.ownHint") : null,
            ...answerLabels,
          }}
        />
      : null}
      {others.length > 0 ?
        <div className="space-y-3">
          {/* The third-party notice, once, above the address fields
              it is about: who these addresses are given to (ADR 0076).
              The buyer is typing other people's details, and is told
              what becomes of them before they do. */}
          <p className="text-sm text-muted-foreground">{t("named.notice")}</p>
          {others.map((slot) => {
            const key = holderKey(slot.ticketTypeId, slot.index);
            const value = checkout.holders[key] ?? "";
            return (
              <CheckoutAnswers
                key={key}
                slot={slot}
                values={checkout.sentAnswers}
                onChange={checkout.changeAnswer}
                onBlur={checkout.leaveField}
                questionErrors={checkout.questionErrors(slot)}
                holder={{
                  value,
                  onChange: (next) => checkout.changeHolder(key, next),
                  onBlur: () => checkout.leaveField(key),
                  error: checkout.holderError(key),
                  // The buyer's own address is accepted at once and
                  // mails nobody (#668), so the notice above would be
                  // untrue of it; the field says so itself.
                  notice:
                    isBuyersOwnAddress(value, buyerEmail) ? t("named.noticeOwnAddress") : undefined,
                  label: t("named.emailLabel"),
                  placeholder: t("named.emailPlaceholder"),
                }}
                labels={{ title: checkout.title(slot), hint: null, ...answerLabels }}
              />
            );
          })}
        </div>
      : null}
    </>
  );
}

/**
 * What a Named Tickets checkout still owes, Ticket by Ticket, drawn right above
 * the button it holds (ADR 0076). Not an error, so not red: it is what is left
 * to do, and it shrinks as the buyer works down it. Polite, so a screen reader
 * hears it change without being interrupted mid-field.
 */
export function NamedTicketsOwedList({ checkout }: { checkout: NamedTicketsCheckout }) {
  const t = useTranslations("checkout");
  if (checkout.owed.length === 0) return null;
  return (
    <div className="rounded-lg border border-dashed p-3 text-sm" aria-live="polite">
      <p className="font-medium">{t("named.owedTitle")}</p>
      <ul className="mt-1 list-disc space-y-1 pl-4 text-muted-foreground">
        {checkout.owed.map((ticket) => (
          <li key={holderKey(ticket.ticketTypeId, ticket.index)}>{checkout.owedLine(ticket)}</li>
        ))}
      </ul>
    </div>
  );
}
