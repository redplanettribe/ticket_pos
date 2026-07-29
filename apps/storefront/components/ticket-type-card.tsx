import { Badge, Card, CardContent } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import type { PublicTicketType } from "@/lib/api";

import { PromotionBadge, PromotionDeadline, TicketTypePrice } from "./promotion";

export function TicketTypeCard({
  ticketType,
  timezone,
}: {
  ticketType: PublicTicketType;
  /** The Event's timezone, which a Promotion's deadline is read in. */
  timezone: string | null;
}) {
  const t = useTranslations("event");

  return (
    <Card className={ticketType.sold_out ? "opacity-70" : undefined}>
      <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-semibold">{ticketType.name}</h3>
            {ticketType.sold_out ? <Badge variant="secondary">{t("soldOut")}</Badge> : null}
            <PromotionBadge ticketType={ticketType} />
          </div>
          {ticketType.description ? (
            <p className="text-sm text-muted-foreground">{ticketType.description}</p>
          ) : null}
          <PromotionDeadline ticketType={ticketType} timezone={timezone} />
          {!ticketType.sold_out ? (
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
