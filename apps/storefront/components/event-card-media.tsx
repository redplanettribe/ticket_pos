import { Badge } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import type { PublicEventCard } from "@/lib/api";

type EventCardMediaProps = {
  event: PublicEventCard;
  /**
   * The frame the Cover Image fills — the caller owns the shape (the grid
   * card's 16:9 top, the Timeline row card's square side) and must make it
   * `relative` and `overflow-hidden`; what goes inside the frame is decided
   * here once.
   */
  className: string;
};

/**
 * What every card shows inside its Cover Image frame: the image itself (never
 * the Cover Video — listings are scanned, not watched, ADR 0020), the
 * initial-on-gradient stand-in when an Event has no Cover Image, and the
 * sold-out badge over the corner. Shared so the three-way choice cannot drift
 * between card shapes.
 */
export function EventCardMedia({ event, className }: EventCardMediaProps) {
  const t = useTranslations("event");
  return (
    <div className={className}>
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
  );
}
