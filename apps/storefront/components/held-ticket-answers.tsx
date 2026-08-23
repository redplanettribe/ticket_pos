"use client";

import { useMessages, useTranslations } from "next-intl";
import { useEffect, useState, type ReactNode } from "react";

import { TicketQuestionRow } from "@/components/ticket-question-row";
import { visibleQuestionsOf, type AnswerBody } from "@/lib/ticket-questions";
import { apiErrorMessage } from "@/lib/api-errors";
import type { HeldTicket } from "@/lib/buyer-answers";
import { closedWindowKey, collapsesAfterSave, panelDisclosure } from "@/lib/held-ticket-panel";

/**
 * One held Ticket's questions: the panel a Holder answers on (#345, ADR 0049).
 *
 * ONE PANEL, TWO PLACES. The buyer's "Your ticket" on their sale page and a
 * Holder's Ticket in their own Customer Area are this same component over the
 * same `/api/customer/held-tickets` row, because a Self-held Ticket (ADR 0048)
 * and an accepted assigned Ticket are the same thing to the platform: a Ticket
 * somebody holds. "Held" is the only authorisation concept, and this is the
 * only answering surface a Customer has.
 *
 * OPEN WHILE OWED, FOLDED ONCE NOT. The panel starts expanded while the Ticket
 * has an Outstanding Answer — what the organizer is waiting on is in the
 * reader's face — and collapsed behind "Answered · Review or edit" once it
 * doesn't, expanding to the very same editable form. The moment the last owed
 * Answer saves, it folds, so the page shows what remains to be done. A panel
 * the reader opened to review stays open until they fold it: the rule is in
 * lib/held-ticket-panel.ts (`collapsesAfterSave`) and it only fires on the
 * count reaching zero from above.
 *
 * NO SAVE BUTTON. Each Answer persists the moment it is given — on change for
 * a choice, a tick box or a date, on blur for text or a number — and each row
 * shows its own transient "Saved" or its own failure. That is the row's job
 * (components/ticket-question-row.tsx, `autosave`); this panel addresses the
 * request and patches the Ticket the API sends back.
 *
 * IT SAYS NOTHING ABOUT THE SALE — no buyer, no price, no Tax ID, no
 * reference, no other Tickets — because the payload carries none of it, and
 * the same payload serves a Holder who was given the Ticket and is entitled
 * to nothing about the purchase (ADR 0044's disclosure rule, kept by 0049).
 * The header is the caller's: the buyer's page says "Your ticket", a Holder's
 * says which Event.
 *
 * READ-ONLY AFTER THE WINDOW SHUTS. Once the Event has started or the Sale
 * was reversed, what was said is drawn and not editable, with one line saying
 * why — the same rule as the buyer's address field.
 */
type HeldTicketAnswersProps = {
  ticket: HeldTicket;
  /** The header's words, before the Ticket Type and the Answered/Outstanding
   * mark: "Your ticket" on the buyer's page, the Event on a Holder's. */
  header: ReactNode;
  /** Drawn inside the panel above the questions — the buyer's "give it to
   * someone else" row. Drawn bare when the Ticket has no questions, because
   * then there is no panel to put it in. */
  children?: ReactNode;
  /** The Ticket the write sent back, to be patched into the caller's list. */
  onAnswered: (ticket: HeldTicket) => void;
};

export function HeldTicketAnswers({ ticket, header, children, onAnswered }: HeldTicketAnswersProps) {
  const t = useTranslations("customerArea");
  const errorCopy = useMessages().errors;
  const disclosure = panelDisclosure(ticket);
  // The INITIAL disclosure decides where the panel starts; after that it is the
  // reader's, and only the last owed Answer saving moves it (below).
  const [open, setOpen] = useState(disclosure === "open");
  // A refetch that brings a NEW debt (a required question added after the
  // sale) reopens the panel: something owed is never hidden.
  useEffect(() => {
    if (disclosure === "open") setOpen(true);
  }, [disclosure]);

  const questions = visibleQuestionsOf(ticket.questions);
  const closed = closedWindowKey(ticket);

  /**
   * Answering one question on this Ticket, through the held-ticket route.
   * ONE TICKET COMES BACK, not a list: each held Ticket's panel stands alone.
   */
  async function save(questionId: string, body: AnswerBody): Promise<string | null> {
    try {
      const response = await fetch(
        `/api/customer/held-tickets/${encodeURIComponent(ticket.ticket_id)}` +
          `/answers/${encodeURIComponent(questionId)}`,
        {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          // No token and no id in the body. The session cookie is the whole
          // credential on this surface, and the ids are in the address.
          body: JSON.stringify(body),
        },
      );
      const envelope = (await response.json()) as {
        data: HeldTicket | null;
        error: { code: string; message: string; details?: unknown } | null;
      };
      if (!response.ok || envelope.error || !envelope.data) {
        return apiErrorMessage(errorCopy, envelope.error) ?? t("answers.saveFailed");
      }
      // THE LAST OWED ANSWER FOLDS THE PANEL, and nothing else does.
      if (collapsesAfterSave(ticket.outstanding_count, envelope.data.outstanding_count)) {
        setOpen(false);
      }
      onAnswered(envelope.data);
      return null;
    } catch {
      return t("answers.networkFailed");
    }
  }

  const outstanding = ticket.outstanding_count;

  if (disclosure === "none") {
    // A Ticket that asks nothing has no expand control at all: the header row
    // and, if the caller gave one, its row — nothing that promises something
    // empty behind a chevron.
    return (
      <div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2.5 text-sm">
          <span aria-hidden className="text-transparent">
            ▸
          </span>
          <span className="font-medium">{header}</span>
          <span className="text-muted-foreground">{ticket.ticket_type_name}</span>
        </div>
        {children ? <div className="px-3 pb-4 pt-1">{children}</div> : null}
      </div>
    );
  }

  return (
    // CONTROLLED, because the panel has two owners of its state — the reader,
    // who clicks the header, and the save that folds it — and `onToggle` is
    // how the reader's clicks are heard.
    <details
      className="group/ticket"
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
      data-testid={`held-ticket-${ticket.ticket_id}`}
    >
      {/* The same row as the Holder List rows below it: at least 44px tall, the
          whole row the tap target, so a phone buyer need not aim at the chevron. */}
      <summary className="flex min-h-11 cursor-pointer list-none flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2.5 text-sm [&::-webkit-details-marker]:hidden">
        <span aria-hidden className="text-muted-foreground transition-transform group-open/ticket:rotate-90">
          ▸
        </span>
        <span className="font-medium">{header}</span>
        <span className="text-muted-foreground">{ticket.ticket_type_name}</span>
        {/* Owed, or answered and offered for review. The collapsed header is
            the review-or-edit control: there is no separate "review" view,
            expanding the panel is reviewing. */}
        <span
          className={
            outstanding > 0 ?
              "ml-auto whitespace-nowrap text-sm font-medium"
            : "text-muted-foreground ml-auto whitespace-nowrap text-sm"
          }
        >
          {outstanding > 0 ?
            t("answers.rowOutstanding", { count: outstanding })
          : open ?
            t("answers.rowAnswered")
          : t("answers.reviewOrEdit")}
        </span>
      </summary>

      <div className="space-y-4 px-3 pb-4 pt-1">
        {children}

        {closed ?
          // The window is shut. Said once for the panel, not once per row,
          // and what was said stays on the page to be read.
          <p className="text-muted-foreground text-sm">{t(`answers.${closed}`)}</p>
        : null}

        <div className="space-y-5">
          {questions.map((pair) => (
            <TicketQuestionRow
              key={pair.question.id}
              pair={pair}
              copy={{
                requiredMark: (question: string) => t("answers.requiredMark", { question }),
                saved: t("answers.saved"),
                retiredQuestion: t("answers.retiredQuestion"),
              }}
              autosave
              disabled={closed !== null}
              save={(body) => save(pair.question.id, body)}
            />
          ))}
        </div>
      </div>
    </details>
  );
}
