import { Card } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { EventCardMedia } from "@/components/event-card-media";
import { TagBadges } from "@/components/tag-badges";
import { useFormatLocale } from "@/i18n/format-locale";
import { Link } from "@/i18n/navigation";
import type { PublicEventCard } from "@/lib/api";
import { formatEventDateShort, formatEventTime } from "@/lib/format";
import { eventCardPriceSlot } from "@/lib/registration";
import { toTagTranslator } from "@/lib/tag-name";

type TimelineEventCardProps = {
  event: PublicEventCard;
  /**
   * Whether the card says its full start date rather than the hour alone.
   * Under a Day Bucket header the date is the header's to say, so the card
   * keeps only the time; in the Ongoing group the header names no date, so the
   * card says the whole thing (CONTEXT.md: Ongoing).
   */
  showStartDate?: boolean;
};

/**
 * The Timeline's row card: a horizontal read — when, what, who, where, how
 * much — with the Cover Image in a small square frame on the side rather than
 * across the top as the grid card wears it. Same words as the grid card, from
 * the same Event namespace, said in a shape made to sit under a date header.
 */
export function TimelineEventCard({ event, showStartDate = false }: TimelineEventCardProps) {
  const t = useTranslations("event");
  // Preset Tag copy is the Storefront's, not the API's (ADR 0027).
  const tTags = toTagTranslator(useTranslations("tags"));
  const locale = useFormatLocale();
  const href = `/${event.organization.slug}/events/${event.slug}`;
  const whenLabel = showStartDate
    ? formatEventDateShort(event.starts_at, event.timezone, locale)
    : formatEventTime(event.starts_at, event.timezone, locale);
  // The same slot the grid card has, decided the same way: a price, or the fact
  // that this Event registers its audience elsewhere (issue #211).
  const priceSlot = eventCardPriceSlot(event, locale);

  return (
    <Link
      href={href}
      className="group block rounded-lg focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
    >
      <Card className="flex flex-row items-start gap-4 p-4 transition-shadow group-hover:shadow-md">
        <div className="min-w-0 flex-1 space-y-1">
          {whenLabel ? <p className="text-sm text-muted-foreground">{whenLabel}</p> : null}
          <h3 className="font-semibold leading-tight tracking-tight">{event.name}</h3>
          <p className="text-sm text-muted-foreground">
            {/* As on the grid card: the Organization's name rendered plainly,
                because a card that is already one link has nowhere else to go. */}
            {t.rich("presentedBy", {
              organization: event.organization.name,
              organizer: (chunks) => chunks,
            })}
          </p>
          {event.venue_name ? (
            <p className="text-sm text-muted-foreground">{event.venue_name}</p>
          ) : null}
          {priceSlot ? (
            <p className="pt-1 text-sm font-medium text-foreground">
              {priceSlot.kind === "registration"
                ? t("registrationRequired")
                : priceSlot.kind === "free"
                  ? t("priceFree")
                  : t("priceFrom", { price: priceSlot.price })}
            </p>
          ) : null}
          <TagBadges tags={event.tags} t={tTags} className="pt-2" />
        </div>
        <EventCardMedia
          event={event}
          className="relative h-24 w-24 shrink-0 overflow-hidden rounded-md bg-muted sm:h-28 sm:w-28"
        />
      </Card>
    </Link>
  );
}
