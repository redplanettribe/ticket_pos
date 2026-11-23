/**
 * Home's "now" strip (#859, ADR 0081): the payload the API states, and the one
 * decision the page makes about it, how far off the next Event's start is.
 *
 * Pure, and free of React, of English and of the i18n runtime, as
 * [events-tabs.ts](./events-tabs.ts) is, so its tests assert a decision rather
 * than a sentence. `now` is a parameter and never a clock read here: the page
 * reads the clock once and hands the instant in.
 */

/** The next Event as GET /staff/organization/home/now states it. */
export type HomeNextEvent = {
  id: string;
  name: string;
  /** An instant, ISO 8601. */
  starts_at: string;
  /** The Event's own zone, which its start is printed in; empty when unset. */
  timezone: string;
  tickets_sold: number;
  ongoing: boolean;
};

/**
 * What stands now across the Organization.
 *
 * The money is withheld by the API, not hidden here: `takings_cents` is null
 * or absent for Event Staff, and `payable_balance_cents` for anyone but an
 * Org Admin. Absent is never zero, so the page draws no card for it.
 */
export type HomeNow = {
  timezone: string;
  /** The platform day, YYYY-MM-DD. */
  today: string;
  currency: string;
  has_events: boolean;
  active_events: number;
  next_event?: HomeNextEvent | null;
  today_so_far: {
    partial: boolean;
    tickets_sold: number;
    takings_cents?: number | null;
  };
  payable_balance_cents?: number | null;
};

/** A distance in time in the largest whole unit that fits it. */
export type TimeUntil = {
  value: number;
  unit: "day" | "hour" | "minute";
};

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/**
 * How long until an instant, in whole days, else whole hours, else whole
 * minutes, rounded down so "in 1 day" is never said of 23 hours. A start
 * already reached reads as zero minutes: the API names an Event whose start
 * has passed as Ongoing, which the page says instead.
 */
export function timeUntil(startsAt: string, now: number): TimeUntil {
  const start = new Date(startsAt).getTime();
  const ahead = Number.isNaN(start) ? 0 : Math.max(0, start - now);
  if (ahead >= DAY) return { value: Math.floor(ahead / DAY), unit: "day" };
  if (ahead >= HOUR) return { value: Math.floor(ahead / HOUR), unit: "hour" };
  return { value: Math.floor(ahead / MINUTE), unit: "minute" };
}

/** The zone an Event's start is printed in: its own, or the platform's where it names none. */
export function nextEventTimeZone(event: HomeNextEvent, platformTimeZone: string): string {
  return event.timezone || platformTimeZone;
}
