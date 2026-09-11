/**
 * Which of the staff Events list's three tabs an Event belongs in, and in what
 * order each tab reads (#613, ADR 0071).
 *
 * The list endpoint serves every Event an Organization has in one unfiltered
 * payload, ordered by creation; this turns that array into the three tabs the
 * page draws. Pure, and kept free of React, of English and of the i18n runtime
 * so its tests assert a decision rather than a sentence — the same shape as
 * [ticket-type-availability.ts](./ticket-type-availability.ts), and for the same
 * reason (ADR 0041).
 *
 * `now` is a parameter and never a clock read here. That is what makes "the
 * clock is read once, when the list renders" a testable claim rather than a
 * comment: the page reads the clock once and hands the instant in, and no row
 * can move under a reader's cursor because nothing in this module can observe
 * time passing.
 */

/**
 * The tabs, in the order they are read.
 *
 * A key and never a word: the labels live in the message catalogs under
 * `events.tabActive` and its two siblings, so a Spanish reader is not handed
 * English by a module that has no way to know which language it is in.
 */
export const EVENTS_TAB_KEYS = ["active", "past", "cancelled"] as const;

export type EventsTabKey = (typeof EVENTS_TAB_KEYS)[number];

/**
 * Everything a tab is decided from: the lifecycle status, and the two instants
 * that bound the Event's runtime.
 *
 * Structurally typed rather than taking `EventListItem`, so the module is
 * testable without a whole list row, and both instants are required rather than
 * optional so a caller cannot forget `ends_at` and silently get the fallback
 * rule for every Event.
 */
export type TabbableEvent = {
  status: string;
  starts_at: string | null;
  ends_at: string | null;
};

/** The status that outranks the clock. Nothing else here is a status test. */
const CANCELLED = "cancelled";

/**
 * An instant as a number, or null where there is none to read.
 *
 * null and an unparseable string are the same answer on purpose: a value this
 * app cannot understand is no better grounds for a claim about time than a
 * missing one, and both leave the Event dateless rather than throwing.
 */
function instant(value: string | null): number | null {
  if (value === null) {
    return null;
  }
  const parsed = new Date(value).getTime();
  return Number.isNaN(parsed) ? null : parsed;
}

/**
 * Whether the Event's runtime has ended by `at`.
 *
 * The end instant, falling back to the start where there is no end — the
 * TypeScript twin of `COALESCE(e.ends_at, e.starts_at) >= now`, the filter the
 * global explorer already ships in `public_repository.go`, so the staff tabs
 * and the Storefront agree about what is finished by construction rather than
 * by coincidence. Half-open on the same side as that filter: an Event ending
 * exactly at `at` has not ended yet.
 *
 * An Event with no start at all is never over. `starts_at` is nullable and only
 * checked at publish time, so a dateless draft is a real row; treating "over"
 * as something that must be proven rather than assumed keeps the rows that most
 * need finishing in front of the person who can finish them.
 *
 * Timezones never enter this. Both sides are instants, and the Event's timezone
 * decides how its date is printed and never whether it has passed.
 */
export function isOver(event: TabbableEvent, at: Date): boolean {
  const ends = instant(event.ends_at) ?? instant(event.starts_at);
  if (ends === null) {
    return false;
  }
  return ends < at.getTime();
}

/**
 * The tab this Event belongs in at the instant `at`.
 *
 * Two rules in order. Cancelling outranks the clock: a cancelled Event is in
 * Cancelled however old, and never falls through into Past — an aged
 * cancellation migrating tabs with nobody touching it would make Cancelled a
 * tab that drains on its own. Then the clock places everything else, with
 * `draft` and `published` having no say. Past means it is over, not that it
 * happened.
 *
 * A status this app does not recognise is placed by the clock rather than
 * dropped, so that the three tabs keep their promise to hold every Event.
 */
export function eventsTab(event: TabbableEvent, at: Date): EventsTabKey {
  if (event.status === CANCELLED) {
    return "cancelled";
  }
  return isOver(event, at) ? "past" : "active";
}

/** The address each tab is read at. Active is the Events list's own path. */
export function eventsTabHref(tab: EventsTabKey): string {
  return tab === "active" ? "/events" : `/events/${tab}`;
}

/**
 * What an Event sorts by: its start, with a dateless one as the earliest
 * instant there is.
 *
 * That one substitution is the whole of the dateless rule, and it produces both
 * halves of it. Read ascending — Active — a dateless draft is pinned above
 * every dated Event, which is where the least finished thing the Organization
 * owns belongs. Read descending — Past and Cancelled — the same key puts it
 * last, which is where an Event with nothing to date it belongs among Events
 * filed by when they happened.
 */
function sortKey(event: TabbableEvent): number {
  return instant(event.starts_at) ?? -Infinity;
}

/**
 * Compares two Events by start, soonest first.
 *
 * Compared rather than subtracted, so that two dateless Events tie at 0 rather
 * than at the NaN `-Infinity - -Infinity` produces — a tie the stable sort
 * below then settles by the order the payload arrived in. Descending is this
 * read backwards (`byStartDescending`), which leaves ties as ties rather than
 * reversing them the way sorting ascending and reversing the array would.
 */
function byStartAscending(a: TabbableEvent, b: TabbableEvent): number {
  const left = sortKey(a);
  const right = sortKey(b);
  if (left === right) return 0;
  return left < right ? -1 : 1;
}

/** The same order read backwards: most recent first, dateless last. */
function byStartDescending(a: TabbableEvent, b: TabbableEvent): number {
  return byStartAscending(b, a);
}

/**
 * The three tabs, sorted, over the whole payload and a single instant.
 *
 * Together they partition the input: every Event is in exactly one, and none is
 * dropped. That claim is the point of the module and is asserted directly in
 * the test beside it.
 *
 * Active reads ascending, soonest first, so the next Event to happen is the
 * first row. Past and Cancelled read descending, so what just finished beats
 * what finished three years ago. Active sorts on start but is *defined* by end,
 * so an Event mid-run floats to the top of Active — intended, and the
 * Storefront's separate Ongoing group is deliberately not copied here.
 *
 * `Array.prototype.sort` is stable, so a tie falls back to the payload's own
 * `created_at DESC, name ASC` and repeated renders do not shuffle rows.
 */
export function partitionEvents<T extends TabbableEvent>(
  events: readonly T[],
  now: Date,
): Record<EventsTabKey, T[]> {
  const tabs: Record<EventsTabKey, T[]> = { active: [], past: [], cancelled: [] };
  for (const event of events) {
    tabs[eventsTab(event, now)].push(event);
  }
  tabs.active.sort(byStartAscending);
  tabs.past.sort(byStartDescending);
  tabs.cancelled.sort(byStartDescending);
  return tabs;
}

/**
 * How many Events each tab holds (#614).
 *
 * Taken from the partition rather than recounted from the payload, so the
 * numbers on the strip and the rows beneath it cannot disagree: they are the
 * same three arrays read two ways. All three keys are always present, and an
 * empty tab counts zero rather than dropping out — a tab that says nothing is
 * exactly the tab a reader has to open to learn anything, which is what the
 * counts exist to spare them.
 *
 * Counts and not the arrays because the strip has no use for the rows: it is
 * handed numbers it can put in a label and nothing it could accidentally render.
 */
export function eventsTabCounts(
  tabs: Record<EventsTabKey, readonly unknown[]>,
): Record<EventsTabKey, number> {
  return {
    active: tabs.active.length,
    past: tabs.past.length,
    cancelled: tabs.cancelled.length,
  };
}
