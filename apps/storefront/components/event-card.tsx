import { Badge, Card, CardContent } from "@ticket-pos/ui";
import { useLocale, useTranslations } from "next-intl";

import { Link } from "@/i18n/navigation";
import type { PublicEventCard } from "@/lib/api";
import { formatEventDateShort, priceFrom } from "@/lib/format";
import { intlLocale, toAppLocale } from "@/lib/locale";

type EventCardProps = {
  event: PublicEventCard;
  showOrganization?: boolean;
};

export function EventCard({ event, showOrganization = true }: EventCardProps) {
  // The card speaks the Event's own words — sold out, presented by, price from —
  // and says them identically on the explorer and on an Organization's page, so
  // it reads the Event's namespace rather than either surface's.
  const t = useTranslations("event");
  const locale = intlLocale(toAppLocale(useLocale()));
  const href = `/${event.organization.slug}/events/${event.slug}`;
  const dateLabel = formatEventDateShort(event.starts_at, event.timezone, locale);
  const price = priceFrom(event.price_from_cents, event.currency, locale);

  return (
    <Link
      href={href}
      className="group block rounded-lg focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
    >
      <Card className="h-full overflow-hidden transition-shadow group-hover:shadow-md">
        <div className="relative aspect-[16/9] w-full overflow-hidden bg-muted">
          {event.cover_image_url ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img
              src={event.cover_image_url}
              alt=""
              className="h-full w-full object-cover transition-transform group-hover:scale-[1.02]"
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-primary/10 to-primary/25">
              <span className="text-2xl font-semibold text-primary/70">
                {event.name.charAt(0).toUpperCase()}
              </span>
            </div>
          )}
          {event.sold_out ? (
            <div className="absolute right-2 top-2">
              <Badge variant="secondary">{t("soldOut")}</Badge>
            </div>
          ) : null}
        </div>
        <CardContent className="space-y-1 p-4">
          <h3 className="font-semibold leading-tight tracking-tight">{event.name}</h3>
          {dateLabel ? <p className="text-sm text-muted-foreground">{dateLabel}</p> : null}
          {event.venue_name ? (
            <p className="text-sm text-muted-foreground">{event.venue_name}</p>
          ) : null}
          {showOrganization ? (
            <p className="text-sm text-muted-foreground">
              {/* The same message the Event page uses, where the Organization's
                  name is a link; here there is nowhere to go from a card that is
                  already one link, so the tag renders its contents plainly. */}
              {t.rich("presentedBy", {
                organization: event.organization.name,
                organizer: (chunks) => chunks,
              })}
            </p>
          ) : null}
          {price ? (
            <p className="pt-1 text-sm font-medium text-foreground">
              {price.kind === "free" ? t("priceFree") : t("priceFrom", { price: price.price })}
            </p>
          ) : null}
          {event.tags.length > 0 ? (
            <div className="flex flex-wrap gap-1.5 pt-2">
              {event.tags.map((tag) => (
                <Badge key={tag.name} variant="outline">
                  {tag.name}
                </Badge>
              ))}
            </div>
          ) : null}
        </CardContent>
      </Card>
    </Link>
  );
}
