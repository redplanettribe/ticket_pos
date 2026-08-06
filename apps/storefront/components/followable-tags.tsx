import { cn } from "@ticket-pos/ui";

import type { PublicTag } from "@/lib/api";
import type { Follows, SessionOutcome } from "@/lib/customer-session";
import { followIntent } from "@/lib/follow-intent";
import { followsTag, tagFollowEndpoint } from "@/lib/follows";
import { tagName, type TagTranslator } from "@/lib/tag-name";

import { FollowButton } from "./follow-button";

type FollowableTagsProps = {
  tags: PublicTag[];
  /** The translator for the `tags` namespace, from `toTagTranslator`. */
  t: TagTranslator;
  /** The one Follows read the page already made. */
  follows: SessionOutcome<Follows>;
  className?: string;
};

/**
 * An Event's Tags, each with the Follow control beside it (#218).
 *
 * The Event page is where a Tag Follow is most worth offering, because it is
 * where a person has just found something they like and the Tag is the reason
 * they might find the next one. So the Tags stop being a label and become the
 * control.
 *
 * THE CONTROL IS OFFERED TO ANYBODY (#219). It first degraded to plain
 * TagBadges for a visitor who was not signed in, on the reasoning that following
 * while signed out was a separate problem — and then that problem was solved: a
 * press now carries its intent through sign-in and comes back made. Hiding the
 * control from anonymous visitors would hide it from almost everyone the Event
 * page is for, which is the opposite of what the Tag row is doing here.
 *
 * A Follows read that FAILED is treated as signed-out rather than as an error.
 * The row's job is to name the Event's Tags; an unresolvable control is worth
 * less than the page, and the API refuses an unauthenticated write regardless,
 * so the worst case is a visitor sent to sign in who was already signed in.
 *
 * One Follows read answers every control in the row. That is the reason the API
 * lists Follows rather than offering a "do I follow this" probe per subject: an
 * Event carrying twenty Tags asks once.
 */
export function FollowableTags({ tags, t, follows, className }: FollowableTagsProps) {
  if (tags.length === 0) return null;

  const signedIn = follows.status === "ok";

  return (
    <div className={cn("flex flex-wrap items-center gap-2", className)}>
      {tags.map((tag) => (
        <span
          key={tag.canonical_key}
          // One pill: the name, then the heart that follows it. Nothing divides
          // the two — the border belongs to the pill, not between its halves —
          // and the heart is set off by plain space instead.
          //
          // Deliberately the SAME pill as the explorer's tag filters, down to
          // the radius and the padding. A Tag is one thing wherever it is drawn,
          // and the two rows differ only in what pressing the left half does:
          // there it narrows the listing, here it is a label and does nothing.
          className="inline-flex items-center rounded-full border border-input bg-background"
        >
          <span className="py-1 pr-2 pl-3 text-sm text-foreground">{tagName(tag, t)}</span>
          <FollowButton
            endpoint={tagFollowEndpoint(tag.canonical_key)}
            following={signedIn ? followsTag(follows, tag.canonical_key) : false}
            // The name as this page words it, so the toast and the screen reader
            // say what the chip says — a Preset Tag in the reader's language, a
            // Custom Tag as its Organization coined it.
            subjectName={tagName(tag, t)}
            // The canonical key, never the rendered name: the intent has to
            // survive a round trip through sign-in and name the same pool row on
            // the far side, whatever language the reader came back in.
            intent={followIntent("tag", tag.canonical_key)}
            signedIn={signedIn}
            compact
            // Rounded to the pill it closes, as in the explorer.
            className="rounded-full"
          />
        </span>
      ))}
    </div>
  );
}
