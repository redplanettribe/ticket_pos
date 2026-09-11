// The Timeline's grouping (CONTEXT.md: Timeline, Day Bucket, Ongoing): the one
// place that decides which group each Event of the explorer's feed lands in.
//
// A pure function of the feed and a `now`, like whenToRange in lib/when.ts, so
// the page can render it on the server and a unit test can run it without a
// request. The feed arrives sorted by start instant ascending (the backend's
// keyset order), and grouping only ever reads that order — it never re-sorts —
// so "Load more" is regrouping a longer array, not merging pages: a day split
// across a page boundary comes out as one Day Bucket by construction.

import type { PublicEventCard } from "./api.ts";

import { ECUADOR_TIME_ZONE, localDateKey } from "./format.ts";

export type TimelineDay = {
  /** The Day Bucket's key: the Event-local calendar date, "YYYY-MM-DD". */
  key: string;
  /**
   * Whether this bucket is the platform wall clock's current or next date
   * (CONTEXT.md: Day Bucket). A discriminant, not copy: "Today" and "Mañana"
   * are the catalog's per-locale words, looked up at render like the `when`
   * chip labels.
   */
  relative: "today" | "tomorrow" | null;
  events: PublicEventCard[];
};

export type Timeline = {
  /** Started but not yet ended, in feed order. Rendered above the Day Buckets. */
  ongoing: PublicEventCard[];
  days: TimelineDay[];
};

export function buildTimeline(events: readonly PublicEventCard[], now: Date): Timeline {
  const ongoing: PublicEventCard[] = [];
  const days: TimelineDay[] = [];
  // Buckets keep the feed's order of first appearance rather than sorting by
  // key: the feed is ordered by start *instant*, and two adjacent instants in
  // far-apart timezones can produce date keys out of order. Sorting would break
  // the tie differently from the cursor and shuffle a bucket mid-"Load more".
  const byKey = new Map<string, TimelineDay>();

  // "Today" is the platform's today (CONTEXT.md: Day Bucket): the current date
  // on the platform wall clock, not the viewer's — the server cannot know the
  // viewer's clock, and a viewer-relative label would flicker on hydration.
  // Tomorrow is 24h later; Ecuador has no DST to make that a different thing.
  const todayKey = localDateKey(now, ECUADOR_TIME_ZONE);
  const tomorrowKey = localDateKey(new Date(now.getTime() + 24 * 60 * 60 * 1000), ECUADOR_TIME_ZONE);
  const relativeOf = (key: string): TimelineDay["relative"] =>
    key === todayKey ? "today" : key === tomorrowKey ? "tomorrow" : null;

  for (const event of events) {
    // Publishing requires a start and a valid timezone (a Discoverable Event
    // always has both); a card missing either has no date to stand under.
    if (!event.starts_at || !event.timezone) continue;
    const starts = new Date(event.starts_at);
    if (Number.isNaN(starts.getTime())) continue;

    // Ongoing is a pure instant comparison, start-inclusive and end-exclusive —
    // the same half-open rule the backend's Promotion windows use. An Event
    // with no end never qualifies: nothing says it is still running, and the
    // feed drops it at its start anyway.
    if (event.ends_at) {
      const ends = new Date(event.ends_at);
      if (!Number.isNaN(ends.getTime()) && starts.getTime() <= now.getTime() && now.getTime() < ends.getTime()) {
        ongoing.push(event);
        continue;
      }
    }

    const key = localDateKey(starts, event.timezone);
    let day = byKey.get(key);
    if (!day) {
      day = { key, relative: relativeOf(key), events: [] };
      byKey.set(key, day);
      days.push(day);
    }
    day.events.push(event);
  }

  return { ongoing, days };
}
