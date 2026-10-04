"use client";

import { Button, FormField, Input } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useState } from "react";

import { CheckoutAnswers } from "@/components/checkout-answers";
import { apiErrorMessage } from "@/lib/api-errors";
import type { BuyerTicket } from "@/lib/buyer-answers";
import type { AnswerValue, AnswerValues } from "@/lib/checkout-answers";
import { answerIsUsable, questionErrorCopy, questionErrorKinds } from "@/lib/named-tickets";
import { reassignmentAnswerBodies, reassignmentOwed, reassignmentSlot } from "@/lib/reassignment";
import {
  assignmentBodyFor,
  assignmentRefusalOf,
  assignmentStateOf,
  canAssign,
  holderEmailOf,
  holderEmailRefusal,
  isBuyersOwnAddress,
  isUnchangedAssignment,
  MAX_HOLDER_EMAIL_LENGTH,
  type AssignmentBody,
} from "@/lib/ticket-assignment";

/**
 * ONE Ticket's address, and the press that names it (#324, parent #322).
 *
 * THIS IS THE CONTROL THAT MAKES FOUR IDENTICAL TICKETS TELLABLE APART. Before
 * it, a buyer of four had "Ticket 2 of 4" and four Answer Links that disclose
 * nothing by design — so the row above this one could say which Ticket Type it
 * was and never which PERSON it was. The address is the label, which is why the
 * state is drawn even where the input is closed.
 *
 * THE NOTICE IS THE POINT OF THE LAYOUT, not decoration on it. #324 requires the
 * buyer to be told BEFORE THEY SUBMIT that the address will be mailed and shown
 * to the Organization, and the API cannot enforce that — it never sees what the
 * page said. So the sentence sits above the button, always rendered, never
 * behind a disclosure and never deferred to a confirmation step: a warning that
 * appears after the press is a warning about something that has happened.
 * When what is typed is the buyer's own address, the notice says instead that
 * the Ticket is theirs at once and nothing is mailed (ADR 0076).
 *
 * IT ASKS FOR AN ADDRESS, AND NEVER A NAME. What the platform knows about a
 * Holder's name is what the HOLDER said when they accepted (#325); a name the
 * buyer typed would be a fact about a person recorded from somebody else's
 * memory, and it would go straight into the Organization's guest list.
 *
 * ON A NAMED TICKETS EVENT IT ASKS FOR THE TICKET'S ANSWERS TOO (#673, ADR
 * 0076), because a new address clears the old Holder's and the Organization
 * chose to have every Ticket answered. The API says so by sending the row's
 * `reassignment_questions`; the fields are the checkout's own, drawn under the
 * address once it differs from the one the Ticket carries, and the press is
 * held until the required ones are given. A NAMED_TICKETS_INCOMPLETE refusal
 * marks the fields it names, as the checkout's does.
 *
 * IT USES ITS OWN NAMESPACE rather than taking copy as props, which is where it
 * differs from TicketQuestionRow beside it. That one is shared between the
 * buyer's page and the forwarded stranger's, so its words had to belong to
 * whoever drew it. This one has exactly one surface — an Assignment Link is
 * never shown to a buyer and a Holder never sees this control — so a second
 * caller to word it for does not exist.
 */
export type TicketAssignmentRowProps = {
  ticket: BuyerTicket;
  /**
   * The session's own address, or null when unknown. Typing it swaps the
   * notice: the buyer's own address is accepted at once and mails nobody (ADR
   * 0076), so "we'll email this address" would be untrue for it.
   */
  buyerEmail?: string | null;
  /**
   * Writes the address, and returns null on success or what to show on
   * failure. The CALLER owns the request and owns turning an API error code into
   * words, exactly as it does for an Answer.
   */
  save: (body: AssignmentBody) => Promise<AssignmentSaveFailure | null>;
};

/**
 * A save that did not land: the sentence for the row, and the questions a
 * NAMED_TICKETS_INCOMPLETE refusal says are still owed, to be marked.
 */
export type AssignmentSaveFailure = { message: string; missingQuestionIds: string[] };

export function TicketAssignmentRow({ ticket, buyerEmail = null, save }: TicketAssignmentRowProps) {
  const t = useTranslations("customerArea");
  const errorCopy = useMessages().errors;

  // The field STARTS ON WHAT IS STORED, so correcting a typo is editing the
  // address rather than retyping it. Somebody cannot correct a letter they
  // cannot see.
  const [value, setValue] = useState(() => holderEmailOf(ticket));
  const [state, setState] = useState<"idle" | "busy" | "saved">("idle");
  const [failure, setFailure] = useState<string | null>(null);
  // The new Holder's Answers, on a Named Tickets Event (#673): what is typed,
  // which fields the buyer has left (an invalid reply is said only then), and
  // the required ones a press or the API found owed.
  const [answers, setAnswers] = useState<AnswerValues>({});
  const [leftFields, setLeftFields] = useState<Record<string, boolean>>({});
  const [owedIds, setOwedIds] = useState<string[]>([]);

  const current = holderEmailOf(ticket);
  const assignmentState = assignmentStateOf(ticket);
  const open = canAssign(ticket);
  const refusal = assignmentRefusalOf(ticket);
  const fieldId = `assignment-${ticket.ticket_id}`;
  const slot = reassignmentSlot(ticket);
  // THE QUESTIONS ARE ASKED ONCE THE ADDRESS IS A CHANGE. The field starts on
  // the address the Ticket carries, and a second copy of its questions under
  // it would only stand beside the panel that already shows them; an
  // unassigned Ticket's field starts empty, so they are there at once.
  const asking = slot !== null && !isUnchangedAssignment(ticket, value);
  // THE NOTICE FOLLOWS WHAT IS TYPED, so it is true before the press in both
  // cases: somebody else's address is mailed, the buyer's own is not.
  const notice =
    isBuyersOwnAddress(value, buyerEmail) ?
      t("assignment.noticeOwnAddress")
    : t("assignment.notice");

  async function submit() {
    // THE PRE-FLIGHT ANSWERS WITH THE API'S OWN CODE and resolves it down the
    // path the API's own errors take (ADR 0023). One code, one catalog entry,
    // one sentence — so a typo caught before the request and a typo caught by
    // catalog.ParseHolderEmail read identically, in either language.
    const preflight = holderEmailRefusal(value);
    if (preflight !== null) {
      setFailure(apiErrorMessage(errorCopy, { code: preflight }) ?? t("assignment.saveFailed"));
      return;
    }
    if (isUnchangedAssignment(ticket, value)) {
      // The API would treat this as a no-op that moves no timestamp. Said
      // plainly rather than reported as a save, because on a reassignment a real
      // save clears this Ticket's Answers and this one did not.
      setFailure(t("assignment.unchanged"));
      return;
    }
    const address = assignmentBodyFor(value);
    if (address === null) {
      // Unreachable: the pre-flight above is the same verdict. Kept because the
      // alternative is a non-null assertion on a value the type system is right
      // to doubt.
      setFailure(t("assignment.saveFailed"));
      return;
    }
    let body: AssignmentBody = address;
    if (asking && slot !== null) {
      // THE SAME VERDICT THE API WILL REACH, reached first so the buyer is
      // pointed at the fields before a request goes anywhere.
      const owed = reassignmentOwed(slot, answers);
      if (owed.length > 0) {
        setOwedIds(owed);
        setFailure(t("assignment.answersNeeded"));
        return;
      }
      body = { ...address, answers: reassignmentAnswerBodies(slot, answers) };
    }

    setState("busy");
    setFailure(null);
    const refused = await save(body);
    if (refused !== null) {
      setState("idle");
      setFailure(refused.message);
      setOwedIds(refused.missingQuestionIds);
      return;
    }
    // The next change of address is a new person, asked afresh.
    setAnswers({});
    setLeftFields({});
    setOwedIds([]);
    setState("saved");
  }

  function changeAnswer(key: string, answer: AnswerValue) {
    setAnswers((current) => ({ ...current, [key]: answer }));
    setState("idle");
    setFailure(null);
  }

  /** The sentence under each question that has one, by question id. */
  function questionErrors(): Record<string, string> {
    if (slot === null) return {};
    const kinds = questionErrorKinds(slot, answers, {
      // Owed until the reply is one the server would keep.
      needed: (question, reply) => owedIds.includes(question.id) && !answerIsUsable(question, reply),
      left: leftFields,
    });
    return questionErrorCopy(kinds, {
      answerNeeded: t("assignment.answerNeeded"),
      answerInvalid: t("assignment.answerInvalid"),
    });
  }

  const addressField = (
    <FormField
      id={fieldId}
      label={
        assignmentState === "accepted" ?
          t("assignment.stateAccepted", { email: current })
        : assignmentState === "assigned" ?
          t("assignment.stateAssigned", { email: current })
        : t("assignment.emailLabel")
      }
      description={
        current === "" ? notice
        : slot !== null ? `${notice} ${t("assignment.changeNeedsAnswers")}`
        : `${notice} ${t("assignment.changeClearsAnswers")}`
      }
    >
      {/* FormField clones its child with the id and aria-describedby, so
          the input, not a wrapper, must be that child. */}
      <Input
        type="email"
        inputMode="email"
        autoComplete="off"
        maxLength={MAX_HOLDER_EMAIL_LENGTH}
        placeholder={t("assignment.emailPlaceholder")}
        value={value}
        disabled={state === "busy"}
        onChange={(event) => {
          setValue(event.target.value);
          setState("idle");
          setFailure(null);
        }}
      />
    </FormField>
  );

  const saveButton = (
    <Button
      variant="outline"
      className="w-full shrink-0 sm:w-auto"
      onClick={submit}
      disabled={state === "busy"}
    >
      {state === "saved" ?
        t("assignment.saved")
      : current === "" ?
        t("assignment.assign")
      : t("assignment.reassign")}
    </Button>
  );

  return (
    <div className="space-y-2">
      {open ?
        <div className="space-y-3">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-end [&>*:first-child]:min-w-0 [&>*:first-child]:flex-1">
            {/* ONE LINE at `sm` and above: label, field, button. Below it the
                form STACKS (label, full-width field, full-width button, the
                notice under) because a phone cannot hold a label, an address
                and a button side by side without the notice squeezed into a
                sliver beside a button pushed off the screen (#353). The notice
                (what happens to the address, and to whom it is shown) rides on
                the field as its `description`, so it is `aria-describedby` the
                input and is heard BEFORE THEY SUBMIT in the only sense that
                matters; #324's last acceptance criterion, and the one the API
                cannot cover.

                THE FIELD KEEPS ITS PLACE IN THE TREE whether or not the
                questions below are drawn, so the first keystroke that makes
                the address a change does not remount the input and take the
                focus with it. */}
            {addressField}
            {asking ? null : saveButton}
          </div>
          {asking && slot !== null ?
            // THE ADDRESS, ITS ANSWERS, THEN THE PRESS (#673): the questions
            // are part of what is being saved, so the button follows them
            // rather than standing beside the address.
            <>
              <CheckoutAnswers
                slot={slot}
                values={answers}
                onChange={changeAnswer}
                onBlur={(key) => setLeftFields((current) => ({ ...current, [key]: true }))}
                questionErrors={questionErrors()}
                labels={{
                  title: t("assignment.answersTitle"),
                  hint: t("assignment.answersHint"),
                  optional: t("assignment.optional"),
                  optionalLabel: (question: string) => t("assignment.optionalLabel", { question }),
                  noAnswer: t("assignment.noAnswer"),
                }}
              />
              {saveButton}
            </>
          : null}
        </div>
        // A CLOSED WINDOW HIDES THE INPUT AND NEVER THE RECORD. The address
        // stays exactly where it was, and the reason is stated, because a
        // missing field with no explanation reads as a fault.
      : <p className="text-muted-foreground text-sm">
          {assignmentState === "accepted" ?
            t("assignment.stateAccepted", { email: current })
          : assignmentState === "assigned" ?
            t("assignment.stateAssigned", { email: current })
          : null}{" "}
          {refusal === "channel_unsupported" ?
            t("assignment.closedChannel")
          : refusal === "sale_reversed" ?
            t("assignment.closedReversed")
          : refusal === "event_started" ?
            t("assignment.closedStarted")
          : t("assignment.closedUnknown")}
        </p>
      }

      {failure ?
        <p role="alert" className="text-destructive text-sm">
          {failure}
        </p>
      : null}
    </div>
  );
}
