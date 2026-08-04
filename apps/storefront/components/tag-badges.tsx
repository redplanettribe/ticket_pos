import { Badge, cn } from "@ticket-pos/ui";

import type { PublicTag } from "@/lib/api";
import { tagName, type TagTranslator } from "@/lib/tag-name";

type TagBadgesProps = {
  tags: PublicTag[];
  /** The translator for the `tags` namespace, from `toTagTranslator`. */
  t: TagTranslator;
  className?: string;
};

/**
 * An Event's Tags, wherever they are shown.
 *
 * The three surfaces that badge Tags — the explorer's cards, the Timeline's
 * cards, and the Event page — had this markup three times over, which meant
 * wording Preset Tags in the page's Locale (ADR 0027) was one decision that had
 * to be applied in three places identically. It renders nothing for an Event
 * with no Tags, so callers do not repeat that check either.
 *
 * Deliberately not a client component: without a "use client" of its own it
 * joins whichever side its caller is on, so the cards get it in the browser
 * bundle and the Event page renders it on the server. That is also why the
 * translator arrives as a prop rather than from `useTranslations` in here —
 * a hook would rule the server caller out.
 */
export function TagBadges({ tags, t, className }: TagBadgesProps) {
  if (tags.length === 0) return null;

  return (
    <div className={cn("flex flex-wrap gap-1.5", className)}>
      {tags.map((tag) => (
        <Badge key={tag.canonical_key} variant="outline">
          {tagName(tag, t)}
        </Badge>
      ))}
    </div>
  );
}
