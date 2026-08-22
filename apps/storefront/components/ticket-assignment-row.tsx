"use client";

import { Button, FormField, Input } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useState } from "react";

import { apiErrorMessage } from "@/lib/api-errors";
import type { BuyerTicket } from "@/lib/buyer-answers";
import {
  assignmentBodyFor,
  assignmentRefusalOf,
  assignmentStateOf,
  canAssign,
  holderEmailOf,
  holderEmailRefusal,
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
 *
 * IT ASKS FOR AN ADDRESS AND NOTHING ELSE. No name field, deliberately. What the
 * platform knows about a Holder is what the HOLDER said when they accepted
 * (#325); a name the buyer typed would be a fact about a person recorded from
 * somebody else's memory, and it would go straight into the Organization's guest
 * list.
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
   * Writes the address, and returns null on success or a sentence to show on
   * failure. The CALLER owns the request and owns turning an API error code into
   * words, exactly as it does for an Answer.
   */
  save: (body: AssignmentBody) => Promise<string | null>;
};

export function TicketAssignmentRow({ ticket, save }: TicketAssignmentRowProps) {
  const t = useTranslations("customerArea");
  const errorCopy = useMessages().errors;

  // The field STARTS ON WHAT IS STORED, so correcting a typo is editing the
  // address rather than retyping it. Somebody cannot correct a letter they
  // cannot see.
  const [value, setValue] = useState(() => holderEmailOf(ticket));
  const [state, setState] = useState<"idle" | "busy" | "saved">("idle");
  const [failure, setFailure] = useState<string | null>(null);

  const current = holderEmailOf(ticket);
  const assignmentState = assignmentStateOf(ticket);
  const open = canAssign(ticket);
  const refusal = assignmentRefusalOf(ticket);
  const fieldId = `assignment-${ticket.ticket_id}`;

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
    const body = assignmentBodyFor(value);
    if (body === null) {
      // Unreachable: the pre-flight above is the same verdict. Kept because the
      // alternative is a non-null assertion on a value the type system is right
      // to doubt.
      setFailure(t("assignment.saveFailed"));
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

  return (
    <div className="space-y-3 border-t pt-4">
      <p className="text-sm font-medium">
        {assignmentState === "accepted" ?
          t("assignment.stateAccepted", { email: current })
        : assignmentState === "assigned" ?
          t("assignment.stateAssigned", { email: current })
        : t("assignment.stateUnassigned")}
      </p>

      {open ?
        <>
          {/* THE NOTICE RIDES ON THE FIELD ITSELF, as its `description`, so it
              is `aria-describedby` the input rather than a paragraph near it: a
              buyer on a screen reader hears what the address will be used for
              while their cursor is in the box, which is BEFORE THEY SUBMIT in
              the only sense that matters. #324's last acceptance criterion, and
              the one the API explicitly cannot cover. */}
          <FormField
            id={fieldId}
            label={t("assignment.emailLabel")}
            description={t("assignment.notice")}
          >
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

          {/* Only once there is something to lose. A first assignment clears no
              Answers, so warning about it there would be a page inventing a
              consequence. */}
          {current !== "" ?
            <p className="text-muted-foreground text-sm">{t("assignment.changeClearsAnswers")}</p>
          : null}

          <div className="flex items-center gap-3">
            <Button size="sm" variant="outline" onClick={submit} disabled={state === "busy"}>
              {current === "" ? t("assignment.assign") : t("assignment.reassign")}
            </Button>
            {state === "saved" ?
              <span className="text-muted-foreground text-sm">{t("assignment.saved")}</span>
            : null}
          </div>
        </>
        // A CLOSED WINDOW HIDES THE INPUT AND NEVER THE RECORD. The address
        // above stays exactly where it was — a reversed purchase keeps its
        // Answers readable for the same reason — and the reason is stated,
        // because a missing field with no explanation reads as a fault.
      : <p className="text-muted-foreground text-sm">
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
