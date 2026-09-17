"use client";

import Link from "next/link";
import { useParams, usePathname, useSearchParams } from "next/navigation";

import type { AppLocale } from "@ticket-pos/locale";
import { Badge } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import {
  assignmentRemindersVisible,
  dossierHref,
  dossierTicketBadgeVariant,
  dossierTicketStateKey,
  holderNameOnTicket,
  ticketHolderDossierId,
  type DossierSale,
  type DossierTicket,
} from "@/lib/customer-dossier";
import { NOTHING_TO_SHOW, formatDateTime } from "@/lib/format";

/**
 * The page a link from this Dossier comes back to: this Dossier, query and all,
 * so Back from a Holder's Dossier returns here (`dossierBackHref` accepts any
 * page of this Event).
 */
function useThisPageHref(): string {
  const pathname = usePathname();
  const query = useSearchParams().toString();
  return query ? `${pathname}?${query}` : pathname;
}

type DossierTicketsProps = {
  sale: Pick<DossierSale, "status" | "tickets" | "assignment_reminder_sent_at">;
  zone: string;
  locale: AppLocale;
};

/**
 * One Sale's Tickets and who holds each (#640), drawn at the foot of its card.
 * The assignment words are the Holder List's; a Ticket's Holder links to their
 * own Dossier only once they accepted, and the buyer's own Ticket reads as
 * theirs. A Sale that no longer stands lists its Tickets as void.
 */
export function DossierTickets({ sale, zone, locale }: DossierTicketsProps) {
  const t = useTranslations("customerDossier");
  const eventId = useParams<{ id: string }>().id;
  const from = useThisPageHref();
  const reminders = sale.assignment_reminder_sent_at ?? [];

  return (
    <div className="mt-3 space-y-3 border-t pt-3">
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">{t("ticketsLabel")}</div>
        {sale.status !== "active" && sale.tickets.length > 0 ? (
          <p className="text-xs text-muted-foreground">{t("voidTickets")}</p>
        ) : null}
        {sale.tickets.length === 0 ? (
          <p>{t("noTicketsOnSale")}</p>
        ) : (
          <ul className="mt-1 space-y-1.5">
            {sale.tickets.map((ticket) => (
              <DossierTicketRow key={ticket.ticket_id} ticket={ticket} eventId={eventId} from={from} />
            ))}
          </ul>
        )}
      </div>
      {assignmentRemindersVisible(sale) ? (
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">{t("remindersLabel")}</div>
          {reminders.length === 0 ? (
            <p>{t("remindersNone")}</p>
          ) : (
            <ul>
              {reminders.map((sentAt, index) => (
                <li key={`${sentAt}-${index}`}>{formatDateTime(sentAt, zone, locale) ?? NOTHING_TO_SHOW}</li>
              ))}
            </ul>
          )}
        </div>
      ) : null}
    </div>
  );
}

type DossierTicketRowProps = {
  ticket: DossierTicket;
  eventId: string;
  from: string;
};

function DossierTicketRow({ ticket, eventId, from }: DossierTicketRowProps) {
  const t = useTranslations("customerDossier");
  const tHolders = useTranslations("outstandingAnswers");

  const stateKey = dossierTicketStateKey(ticket);
  const variant = dossierTicketBadgeVariant(ticket);
  const name = stateKey === "selfHeld" ? "" : holderNameOnTicket(ticket);
  const holderId = ticketHolderDossierId(ticket);

  return (
    <li className="flex flex-wrap items-center gap-x-2 gap-y-1">
      <span>{tHolders("ticketHeading", { name: ticket.ticket_type_name, ordinal: ticket.ordinal })}</span>
      {name ? (
        holderId ? (
          <Link
            href={dossierHref(eventId, holderId, from)}
            className="font-medium underline-offset-2 hover:underline"
            aria-label={t("openDossier", { name })}
          >
            {name}
          </Link>
        ) : (
          <span className="font-medium">{name}</span>
        )
      ) : null}
      {stateKey && variant ? (
        <Badge variant={variant}>{stateKey === "selfHeld" ? t("selfHeld") : tHolders(stateKey)}</Badge>
      ) : null}
    </li>
  );
}
