// Which image an Event card shows in its frame (CONTEXT.md: Thumbnail, Cover
// Image). One function, so the fallback cannot drift between card shapes or
// future surfaces (spec #853).
//
// A square frame (the Timeline row card) shows the Thumbnail, the organizer's
// own square crop, and falls back to the Cover Image, which the frame crops to
// its centre. A wide frame (the Organization page's grid card) always shows the
// Cover Image and never a square stretched across it. Null means neither
// applies: the card draws its initial-on-gradient stand-in.
//
// The fallback lives here and not in the API on purpose: the API reports only
// what exists, so thumbnail_url is null on an Event without a Thumbnail.

import type { PublicEventCard } from "./api.ts";

/** The shape of the frame a card fills. */
export type CardFrame = "square" | "wide";

export function cardImageUrl(
  event: Pick<PublicEventCard, "thumbnail_url" | "cover_image_url">,
  frame: CardFrame,
): string | null {
  if (frame === "square" && event.thumbnail_url) {
    return event.thumbnail_url;
  }
  return event.cover_image_url ?? null;
}
