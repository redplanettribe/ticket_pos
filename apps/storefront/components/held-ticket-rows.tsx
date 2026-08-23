"use client";

import { useTranslations } from "next-intl";
import { useEffect, useState } from "react";

import { HeldTicketAnswers } from "@/components/held-ticket-answers";
import { withHeldRow, type HeldTicket as HeldTicketAnswersRow } from "@/lib/buyer-answers";
import type { HeldTicket } from "@/lib/customer-session";
import { heldRowDisclosure } from "@/lib/held-ticket-panel";

/**
 * The Tickets somebody else bought and this Customer accepted (#325, ADR 0046),
 * each with its questions (#345, ADR 0049), drawn as rows inside their Event's
 * group (#356) after the Customer's own Ticket Sales for that Event.
 *
 * ROWS AMONG THE PURCHASES, BECAUSE A TICKET TO DEVFEST IS A TICKET TO
 * DEVFEST WHOEVER PAID. The person reading this page thinks "my DevFest
 * tickets", and a Ticket a friend bought them belongs in that thought; a
 * separate "Tickets someone gave you" section, which is what this was before
 * #356, made them find the same Event twice on one page. What the row draws
 * is what the Holder has — the Ticket Type and the questions the Organization
 * asked them — and it stays what it is: NOT a purchase. The Ticket Sale, the
 * money, the Sale Confirmation reference, the Tax ID and the Reversal Window
 * all stayed with the buyer, so none of them is drawn here and no undo is
 * offered; neither API sends them to a Holder (ADR 0044's disclosure rule).
 *
 * Upcoming or Past is the tab module's decision (lib/customer-area-tabs.ts,
 * `groupsFor`), by the rule the API applies to a Sale; a held Ticket never
 * reaches Reversed, because a Sale Reversal takes its Holders with it.
 *
 * TWO PAYLOADS, ONE ROW. The Customer Area's own read (server-rendered,
 * `held`) says which Event and Ticket Type each held Ticket is for and is
 * what files it into a group; the held-ticket list (fetched here, the same
 * `/api/customer/held-tickets` the buyer's "Your ticket" reads) says what each
 * asks and what it still owes. Joined by `ticket_id`. A Ticket in the first
 * and not the second — the feature is dark, or the read failed — draws as a
 * plain row naming the Ticket Type, which must never wait on the questions.
 *
 * THE PANEL IS THE SAME PANEL THE BUYER HAS. A Holder's "oops, I meant M" is
 * theirs to make from here, without the accept email (ADR 0049); and because
 * the panel is one component over one payload, there is one way to answer on
 * this platform.
 */
export function HeldTicketRows({
  held,
  defaultOpen = false,
}: {
  held: HeldTicket[];
  /** True when each row should start open: the Event has only this one
   * ticket-row, so there is nothing to choose between — the Sale rows' rule. */
  defaultOpen?: boolean;
}) {
  const t = useTranslations("customerArea");
  const [rows, setRows] = useState<HeldTicketAnswersRow[] | null>(null);

  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        const response = await fetch("/api/customer/held-tickets");
        const envelope = (await response.json()) as { data: HeldTicketAnswersRow[] | null };
        if (!live) return;
        // EVERY FAILURE IS SILENCE: the feature being dark answers 404, and
        // the row is complete without its questions.
        setRows(response.ok && envelope.data ? envelope.data : []);
      } catch {
        if (live) setRows([]);
      }
    })();
    return () => {
      live = false;
    };
  }, []);

  return (
    <>
      {held.map((ticket) => {
        const row = rows?.find((candidate) => candidate.ticket_id === ticket.ticket_id) ?? null;
        // The one line the row is scanned by. The Ticket Type is the
        // Organization's own coinage and is drawn as written in either
        // language (ADR 0027); the sentence around it is the catalog's.
        const label = t("heldRow", { ticketType: ticket.ticket_type_name });
        if (row === null || heldRowDisclosure(row) === "plain") {
          // Nothing to open: no questions known for this Ticket, so no chevron
          // promising something behind it. The blank keeps the text aligned
          // with the rows that do open.
          return (
            <li
              key={ticket.ticket_id}
              className="flex min-h-11 flex-wrap items-center gap-x-3 gap-y-1 py-3 text-sm"
            >
              <span aria-hidden className="text-transparent">
                ▸
              </span>
              <span className="font-medium">{label}</span>
            </li>
          );
        }
        return (
          <li key={ticket.ticket_id}>
            {/* `open` is the attribute or nothing, never `false`, for the same
                reason as the Sale row: a save inside would otherwise snap it
                shut. It starts open while an Answer is owed, so what the
                Organization is waiting on is not hidden behind a click. */}
            <details
              className="group/ticket"
              open={defaultOpen || row.outstanding_count > 0 || undefined}
            >
              {/* The whole row is the tap target, at least 44px tall; the
                  words wrap rather than overflow at 360px (#353). */}
              <summary className="flex min-h-11 cursor-pointer list-none flex-wrap items-center gap-x-3 gap-y-1 py-3 text-sm [&::-webkit-details-marker]:hidden">
                <span
                  aria-hidden
                  className="text-muted-foreground transition-transform group-open/ticket:rotate-90"
                >
                  ▸
                </span>
                <span className="font-medium">{label}</span>
              </summary>
              <div className="pb-4">
                <HeldTicketAnswers
                  ticket={row}
                  header={t("answers.heldTicket")}
                  onAnswered={(updated) => setRows((current) => withHeldRow(current ?? [], updated))}
                />
              </div>
            </details>
          </li>
        );
      })}
    </>
  );
}
