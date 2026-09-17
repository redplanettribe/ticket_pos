"use client";

import { toAppLocale } from "@ticket-pos/locale";
import { Badge } from "@ticket-pos/ui";
import { useLocale, useTranslations } from "next-intl";

import {
  heldTicketBuyerName,
  heldTicketOnReversedSale,
  nameGivenAsHolder,
  type DossierHeldTicket,
} from "@/lib/customer-dossier";
import { PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";

import { DossierTicketAnswers } from "./dossier-ticket-answers";

type DossierHeldTicketsSectionProps = {
  heldTickets: DossierHeldTicket[];
  timezone: string | null;
  /** Opens the Answers dialog on the held Ticket's Sale (#641). */
  onOpenAnswers: (ticketSaleId: string, confirmationRef: string) => void;
};

/**
 * Tickets this Customer accepted on somebody else's Sale (#640). A Ticket whose
 * Sale was reversed stays listed, marked as no live Ticket, because having held
 * it is still something this Event knows about the person.
 */
export function DossierHeldTicketsSection({ heldTickets, timezone, onOpenAnswers }: DossierHeldTicketsSectionProps) {
  const t = useTranslations("customerDossier");
  const tHolders = useTranslations("outstandingAnswers");
  const locale = toAppLocale(useLocale());
  const zone = timezone ?? PLATFORM_TIME_ZONE;

  return (
    <section aria-labelledby="dossier-held-tickets" className="space-y-3">
      <h2 id="dossier-held-tickets" className="text-base font-semibold">
        {t("heldHeading")}
      </h2>
      {heldTickets.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("heldNone")}</p>
      ) : (
        <ul className="space-y-3">
          {heldTickets.map((ticket) => {
            const buyer = heldTicketBuyerName(ticket);
            const nameAsHolder = nameGivenAsHolder(ticket);
            const reversed = heldTicketOnReversedSale(ticket);
            const acceptedAt = formatDateTime(ticket.accepted_at, zone, locale);
            const heading = tHolders("ticketHeading", { name: ticket.ticket_type_name, ordinal: ticket.ordinal });
            return (
              <li key={ticket.ticket_id} className="rounded-md border p-3 text-sm sm:p-4">
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <span className="font-medium">{heading}</span>
                  <span className="font-mono text-xs text-muted-foreground">{ticket.confirmation_ref}</span>
                  {reversed ? <Badge variant="destructive">{t("heldSaleReversed")}</Badge> : null}
                </div>
                <div className="mt-2 space-y-0.5 break-words">
                  <div>{buyer ? t("heldBoughtBy", { name: buyer }) : t("heldBuyerUnnamed")}</div>
                  {nameAsHolder ? <div>{t("heldNameGiven", { name: nameAsHolder })}</div> : null}
                  {acceptedAt ? (
                    <div className="text-xs text-muted-foreground">{t("heldAcceptedAt", { when: acceptedAt })}</div>
                  ) : null}
                </div>
                <DossierTicketAnswers
                  ticket={ticket}
                  ticketName={heading}
                  zone={zone}
                  locale={locale}
                  onOpenAnswers={() => onOpenAnswers(ticket.ticket_sale_id, ticket.confirmation_ref)}
                />
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
