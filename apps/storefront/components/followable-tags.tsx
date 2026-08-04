import { Badge, cn } from "@ticket-pos/ui";

import type { PublicTag } from "@/lib/api";
import type { Follows, SessionOutcome } from "@/lib/customer-session";
import { followsTag, tagFollowEndpoint } from "@/lib/follows";
import { tagName, type TagTranslator } from "@/lib/tag-name";

import { FollowButton } from "./follow-button";
import { TagBadges } from "./tag-badges";

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
 * IT DEGRADES TO TagBadges. A visitor who is not signed in — or whose Follows
 * read failed — sees exactly the Tags they saw before, from the same component
 * that renders them everywhere else, with no control, no placeholder and no
 * invitation to sign in. Following while signed out is its own problem, because
 * the press has to survive a round trip through sign-in, and it is #219.
 *
 * One Follows read answers every control in the row. That is the reason the API
 * lists Follows rather than offering a "do I follow this" probe per subject: an
 * Event carrying twenty Tags asks once.
 */
export function FollowableTags({ tags, t, follows, className }: FollowableTagsProps) {
  if (tags.length === 0) return null;

  if (follows.status !== "ok") {
    return <TagBadges tags={tags} t={t} className={className} />;
  }

  return (
    <div className={cn("flex flex-wrap items-center gap-2", className)}>
      {tags.map((tag) => (
        <span
          key={tag.canonical_key}
          className="flex items-center gap-1.5 rounded-md border py-0.5 pr-0.5 pl-2"
        >
          <Badge variant="outline" className="border-0 px-0">
            {tagName(tag, t)}
          </Badge>
          <FollowButton
            endpoint={tagFollowEndpoint(tag.canonical_key)}
            following={followsTag(follows, tag.canonical_key)}
            // The name as this page words it, so the toast and the screen reader
            // say what the chip says — a Preset Tag in the reader's language, a
            // Custom Tag as its Organization coined it.
            subjectName={tagName(tag, t)}
            compact
          />
        </span>
      ))}
    </div>
  );
}
