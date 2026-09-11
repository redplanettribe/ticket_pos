import { Badge, Card, CardContent } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import type { PublicTicketType } from "@/lib/api";

import { PromotionBadge, PromotionDeadline, TicketTypePrice } from "./promotion";
import { SalesClosedBadge, SalesClosedLine } from "./sales-cutoff";

export function TicketTypeCard({
  ticketType,
  timezone,
}: {
  ticketType: PublicTicketType;
  /**
   * The Event's timezone, which a Promotion's deadline and a Sales Cutoff are
   * both read in.
   */
  timezone: string | null;
}) {
  const t = useTranslations("event");
  // This card never sold anything — an ended Event has no steppers to withdraw —
  // so what closed buys here is the badge, the sentence and the dimming, and
  // that is exactly the point: a Ticket Type that closed while the Event was
  // still selling goes on saying so afterwards, and the record a Customer comes
  // back to matches what they saw at the time (ADR 0070).
  //
  // Named as the sellable list names it, and computed the same way, because both
  // cards draw the same two states the same way.
  const sellable = !ticketType.sold_out && !ticketType.closed;

  return (
    <Card className={sellable ? undefined : "opacity-70"}>
      <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-semibold">{ticketType.name}</h3>
            {ticketType.sold_out ? <Badge variant="secondary">{t("soldOut")}</Badge> : null}
            <SalesClosedBadge ticketType={ticketType} />
            <PromotionBadge ticketType={ticketType} />
          </div>
          {ticketType.description ? (
            <p className="text-sm text-muted-foreground">{ticketType.description}</p>
          ) : null}
          <PromotionDeadline ticketType={ticketType} timezone={timezone} />
          <SalesClosedLine ticketType={ticketType} timezone={timezone} />
          {/* No remaining count on a Ticket Type nobody can buy — a closed one
              included. Showing stock beside "Sales closed" would offer tickets
              the door has already shut on. */}
          {sellable ? (
            <p className="text-sm text-muted-foreground">
              {t("remaining", { count: ticketType.remaining })}
            </p>
          ) : null}
        </div>
        <div className="shrink-0 text-right">
          <TicketTypePrice ticketType={ticketType} />
        </div>
      </CardContent>
    </Card>
  );
}
