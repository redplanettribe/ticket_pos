"use client";

import { useTranslations } from "next-intl";
import { useEffect, useState } from "react";

import { HeldTicketAnswers } from "@/components/held-ticket-answers";
import { Link } from "@/i18n/navigation";
import { withHeldRow, type HeldTicket as HeldTicketAnswersRow } from "@/lib/buyer-answers";
import type { HeldTicket } from "@/lib/customer-session";

/**
 * The Tickets somebody else bought and this Customer accepted (#325, ADR 0046),
 * each with its questions (#345, ADR 0049).
 *
 * A SECTION OF ITS OWN AND NOT ROWS AMONG THE PURCHASES, because these are not
 * purchases: the Ticket Sale, the money, the Sale Confirmation and the Reversal
 * Window all stayed with the buyer. What is drawn is the Event, the
 * Organization and the Ticket Type — and no amount, no confirmation reference,
 * no Tax ID and no Undo, none of which either API sends here.
 *
 * TWO PAYLOADS, ONE CARD. The Customer Area's own read (server-rendered,
 * `holding`) says which Events and Organizations these Tickets are for, with
 * the slugs the link needs; the held-ticket list (fetched here, the same
 * `/api/customer/held-tickets` the buyer's "Your ticket" reads) says what each
 * asks and what it still owes. Joined by `ticket_id`. A Ticket in the first
 * and not the second — the feature is dark, or the read failed — draws as the
 * read-only card it was before #345: the link is the way back that does not
 * depend on keeping the mail, and it must never wait on the questions.
 *
 * THE PANEL IS THE SAME PANEL THE BUYER HAS. A Holder's "oops, I meant M" is
 * theirs to make from here, without the accept email (ADR 0049); and because
 * the panel is one component over one payload, there is one way to answer on
 * this platform.
 */
export function HeldTickets({ holding }: { holding: HeldTicket[] }) {
  const t = useTranslations("customerArea");
  const [held, setHeld] = useState<HeldTicketAnswersRow[] | null>(null);

  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        const response = await fetch("/api/customer/held-tickets");
        const envelope = (await response.json()) as { data: HeldTicketAnswersRow[] | null };
        if (!live) return;
        // EVERY FAILURE IS SILENCE: the feature being dark answers 404, and
        // the card below is complete without its questions.
        setHeld(response.ok && envelope.data ? envelope.data : []);
      } catch {
        if (live) setHeld([]);
      }
    })();
    return () => {
      live = false;
    };
  }, []);

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold tracking-tight">{t("holdingHeading")}</h2>
        <p className="text-muted-foreground text-sm">{t("holdingDescription")}</p>
      </div>
      <ul className="space-y-4">
        {holding.map((ticket) => {
          const row = held?.find((candidate) => candidate.ticket_id === ticket.ticket_id) ?? null;
          return (
            <li key={ticket.ticket_id} className="rounded-lg border">
              <div className="p-4">
                {/* The Event's name and its Organization's are drawn AS COINED
                    in either language, like every other Organization-authored
                    string on this platform (ADR 0027). */}
                <Link
                  href={`/${ticket.organization.slug}/${ticket.event.slug}`}
                  className="font-medium underline"
                >
                  {ticket.event.name}
                </Link>
                <p className="text-muted-foreground mt-1 text-sm">
                  {ticket.organization.name} · {ticket.ticket_type_name}
                </p>
              </div>
              {row !== null ?
                <div className="border-t">
                  <HeldTicketAnswers
                    ticket={row}
                    header={t("answers.heldTicket")}
                    onAnswered={(updated) => setHeld((rows) => withHeldRow(rows ?? [], updated))}
                  />
                </div>
              : null}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
