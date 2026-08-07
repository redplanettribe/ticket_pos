import type { ReactNode } from "react";

import { Link } from "@/i18n/navigation";

/**
 * The shape of a row about a followable subject: a piece of media down the left,
 * a name that links to the subject, a line saying what kind of thing it is, and
 * one control on the right.
 *
 * LIFTED OUT OF following-list.tsx RATHER THAN COPIED INTO THE SECOND PLACE THAT
 * NEEDED IT (#231). The Suggested Follows panel renders Organizations as rows
 * "matching the Following list's shape", and matching is a promise that decays
 * the moment there are two copies of it: a padding change to one leaves the two
 * lists visibly out of step on the same page, one above the other, where the
 * mismatch is at its most obvious.
 *
 * It is a shape and NOT a Follow. Nothing here knows what a Follow is, which
 * subject it is about, or whether the Customer has made one — the control
 * arrives as a node. That is deliberate: the Following list draws a filled
 * control and the suggestions panel draws an empty one, and a row that decided
 * that itself would have to be told which list it was in, which is the parameter
 * that turns a shared shape back into two.
 */

/**
 * The bordered, divided list a set of rows sits in.
 *
 * Separate from the row so a caller can put its own heading above the box, and
 * so the two lists on the Following page are visibly the same object rather than
 * two things that happen to look alike.
 */
export function FollowRowList({ children }: { children: ReactNode }) {
  return <ul className="divide-y rounded-lg border">{children}</ul>;
}

type FollowRowProps = {
  /** The Organization's logo, or the glyph a Tag stands in with. */
  media: ReactNode;
  /**
   * Where the name links. A row is a way back to the subject, not only a way to
   * act on it — an Organization to its page, a Tag to the explorer filtered by
   * it, since a Tag has no page of its own.
   */
  href: string;
  /** The subject's name, worded by the caller in the page's Locale. */
  name: string;
  /** What kind of thing this is: "Organizer", "Tag". */
  kind: ReactNode;
  /** The Follow control, filled or empty as its own caller decided. */
  control: ReactNode;
};

export function FollowRow({ media, href, name, kind, control }: FollowRowProps) {
  return (
    <li className="flex items-center justify-between gap-4 p-4">
      <div className="flex min-w-0 items-center gap-3">
        {media}
        <div className="min-w-0">
          <Link href={href} className="block truncate font-medium hover:underline">
            {name}
          </Link>
          <p className="text-muted-foreground text-xs">{kind}</p>
        </div>
      </div>
      {control}
    </li>
  );
}

/**
 * A Tag's counterpart to the Organization's logo, so the two kinds line up down
 * the left edge instead of one row starting further in than the other.
 *
 * A glyph rather than an icon: this app does not depend on the icon set, and "#"
 * is what a tag reads as anyway.
 */
export function TagRowGlyph() {
  return (
    <span
      aria-hidden
      className="bg-muted text-muted-foreground flex size-10 shrink-0 items-center justify-center rounded-md text-sm font-medium"
    >
      #
    </span>
  );
}
