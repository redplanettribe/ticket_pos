import { Badge } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import type { PublicEventCard } from "@/lib/api";
import { cardImageUrl, type CardFrame } from "@/lib/card-image";

type EventCardMediaProps = {
  event: PublicEventCard;
  /**
   * The shape of the frame, which decides the image: a square frame shows the
   * Thumbnail before the Cover Image, a wide one only the Cover Image
   * (lib/card-image.ts).
   */
  frame: CardFrame;
  /**
   * The frame the image fills - the caller owns the shape (the grid card's
   * 16:9 top, the Timeline row card's square side) and must make it `relative`
   * and `overflow-hidden`; what goes inside the frame is decided here once.
   */
  className: string;
};

/**
 * What every card shows inside its image frame: the image cardImageUrl chooses
 * for the frame's shape (the Thumbnail or the Cover Image, never the Cover
 * Video - listings are scanned, not watched, ADR 0020), the
 * initial-on-gradient stand-in when there is no image for it, and the badge
 * over the corner saying an Event has nothing left to sell — because it ran out,
 * or because it stopped. Shared so the three-way choice cannot drift between
 * card shapes.
 */
export function EventCardMedia({ event, frame, className }: EventCardMediaProps) {
  const t = useTranslations("event");
  const imageUrl = cardImageUrl(event, frame);
  return (
    <div className={className}>
      {imageUrl ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src={imageUrl}
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
      {/* One corner, three possible states, ranked as every other surface ranks
          them: sold out first, then closed, then nothing at all (ADR 0070).

          Closed gets its own words rather than borrowing sold out's, because a
          listing that called a half-empty Event full would misrepresent it — and
          because the two invite different behaviour from a reader: "they are
          gone" ends the conversation, "we stopped selling" invites an email
          asking you to reopen. A mixed Event, some Ticket Types closed and the
          rest exhausted, reads sold out: the API judges sold_out over the Ticket
          Types still open in time, so the stronger fact is the one that arrives.

          Words and not only a colour, here as on the Event page. */}
      {event.sold_out ? (
        <div className="absolute right-2 top-2">
          <Badge variant="secondary">{t("soldOut")}</Badge>
        </div>
      ) : event.all_closed ? (
        <div className="absolute right-2 top-2">
          <Badge variant="secondary">{t("salesClosed")}</Badge>
        </div>
      ) : null}
    </div>
  );
}
