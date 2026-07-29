import { Badge } from "@ticket-pos/ui";

import type { PublicTicketType } from "@/lib/api";
import { formatPrice } from "@/lib/format";
import { formatPromotionDeadline, promotionSavingsPercent } from "@/lib/promotion";

/**
 * How a Promotion looks on the Storefront, in the three pieces every surface
 * that lists Ticket Types assembles: the "X% off" badge beside the name, the
 * deadline under it, and the price with the List Price struck through.
 *
 * Two surfaces show Ticket Types — the sellable list with its steppers and the
 * read-only list on an Event that has ended — and they must say the same thing
 * about the same Promotion. Splitting the pieces rather than shipping one block
 * keeps each one where its surface already puts that kind of information: name
 * badges with the name, prices in the price column.
 *
 * A Ticket Type without a live Promotion renders exactly as before: no badge, no
 * deadline, one price and no strikethrough (ADR 0021).
 */

/** "37% off", when the Promotion is worth a whole percent. */
export function PromotionBadge({ ticketType }: { ticketType: PublicTicketType }) {
  const percent = ticketType.promotion ? promotionSavingsPercent(ticketType.promotion) : null;
  if (percent === null) return null;

  return <Badge>{percent}% off</Badge>;
}

/**
 * "Promotional Price until Thu Jul 9 · 7:00 PM" — the reason to buy now, said
 * plainly. The deadline is on the Event's clock, so a buyer in another timezone
 * is told the same instant the organizer scheduled.
 */
export function PromotionDeadline({
  ticketType,
  timezone,
}: {
  ticketType: PublicTicketType;
  /** The Event's timezone; the window is scheduled in it (ADR 0021). */
  timezone: string | null;
}) {
  const deadline = ticketType.promotion
    ? formatPromotionDeadline(ticketType.promotion, timezone)
    : null;
  if (!deadline) return null;

  return (
    <p className="text-sm text-muted-foreground">
      Promotional Price until <span className="font-medium text-foreground">{deadline}</span>
    </p>
  );
}

/**
 * The price the buyer will be charged, with the List Price struck through beside
 * it while a Promotion is live.
 *
 * price_cents is already the Promotional Price when the API sends a Promotion,
 * so the big number is the same field this component always showed — the
 * Promotion only adds what is being crossed out.
 */
export function TicketTypePrice({ ticketType }: { ticketType: PublicTicketType }) {
  const listPriceCents = ticketType.promotion?.list_price_cents ?? null;

  return (
    <p className="font-semibold">
      {formatPrice(ticketType.price_cents, ticketType.currency)}
      {listPriceCents !== null ? (
        <span className="ml-2 text-sm font-normal text-muted-foreground">
          {/* Sighted buyers read the strikethrough; everyone else needs the word. */}
          <span className="sr-only">, down from </span>
          <s>{formatPrice(listPriceCents, ticketType.currency)}</s>
        </span>
      ) : null}
    </p>
  );
}
