/**
 * How the staff app writes numbers, money, dates and times — in the reader's
 * Staff Locale, and in nobody else's timezone or currency.
 *
 * THE RULE, inherited verbatim from CONTEXT.md and restated by ADR 0041:
 *
 *   > A Locale decides words, and the marks around numbers and dates. It decides
 *   > NOTHING about time or money. An Event's times are drawn in the Event's own
 *   > timezone and amounts in the Organization's currency, whichever language the
 *   > reader is in.
 *
 * That rule is enforced here by the shape of the signatures rather than by
 * anybody remembering it: every function that draws a moment demands a
 * `timeZone`, every function that draws an amount demands a `currency`, and
 * neither has a default. A caller cannot get a date out of this module without
 * having said which clock it is on, and the locale it passes cannot reach that
 * decision because it arrives as a separate argument that only ever reaches
 * `intlLocale`.
 *
 * WHY THIS EXISTS AT ALL. The staff app formats with `toLocaleDateString()` and
 * bare `Intl.*` in a dozen modules, which is not "English" — it is the BROWSER's
 * locale. A Spanish-speaking organizer on an English laptop reads `en-US` dates
 * today, and an English-speaking one on a Spanish laptop reads Spanish ones.
 * Nothing in the app chose either. This module is what a call site moves to, and
 * the locale becomes a value the app decides instead of a property of the machine
 * it is being read on.
 *
 * HOW TO CONSUME IT (later tickets: this is the whole of the contract).
 *
 *   Server component:
 *     const locale = toAppLocale(await getLocale());
 *     formatMoney(cents, organization.currency, locale)
 *
 *   Client component:
 *     const locale = toAppLocale(useLocale());
 *     formatEventDateTime(event.starts_at, event.timezone, locale)
 *
 * `toAppLocale` and `useLocale`/`getLocale` come from @ticket-pos/locale and
 * next-intl respectively. next-intl's locale is whatever i18n/request.ts resolved
 * for this request, which after sign-in is the stored Staff Locale — so a call
 * site never has to know where the language came from.
 *
 * WHAT IS DELIBERATELY NOT HERE. No `t`, no catalog, no React. This module is
 * pure functions over values so it runs under `node --test` (`lib/*.test.ts` is
 * the only glob the runner sees, which is why it lives in `lib/` and not beside
 * the components). Sentences that happen to contain a formatted number — "3 of
 * 10 sold" — are assembled by the catalog through ICU interpolation, with this
 * module supplying only the number.
 */

import { intlLocale, type AppLocale, type IntlLocale } from "@ticket-pos/locale";

export type { AppLocale, IntlLocale };

/**
 * The Intl language-and-region tag a Staff Locale formats under.
 *
 * Re-exported rather than reimplemented: @ticket-pos/locale owns the mapping
 * between the short token a person chooses ("es") and the regional tag the marks
 * come from ("es-EC"), because the Storefront asks the same question and two
 * copies could come to disagree about it. This is the staff app's name for it, so
 * a call site that needs a raw `Intl.*` this module does not wrap has one obvious
 * place to get the tag from instead of writing "es-EC" down again.
 */
export function staffIntlLocale(locale: AppLocale): IntlLocale {
  return intlLocale(locale);
}

/**
 * A count, written with the reader's marks: 1,500 in English, 1.500 in Spanish.
 *
 * The kind of number this is for is a count of things — tickets sold, requests
 * waiting, people on a team. Money does NOT come through here; it has its own
 * function below, because an amount without its currency is a number that means
 * nothing.
 */
export function formatNumber(
  value: number,
  locale: AppLocale,
  options?: Intl.NumberFormatOptions,
): string {
  return new Intl.NumberFormat(staffIntlLocale(locale), options).format(value);
}

/**
 * An amount of money held in minor units, in the currency it is denominated in.
 *
 * `currency` is required and has no default, which is the whole point. The locale
 * decides where the symbol sits and which mark separates the thousands; it does
 * not decide, and must never be able to decide, WHICH currency an amount is in.
 * An Organization's Payouts are in the Organization's currency and a Ticket
 * Type's price in the one its Organization sells in, whether the person reading
 * the screen picked English or Spanish.
 *
 * Cents are integers throughout this platform's API, so the division is the last
 * thing that happens before formatting rather than something a caller does first.
 */
export function formatMoney(cents: number, currency: string, locale: AppLocale): string {
  return new Intl.NumberFormat(staffIntlLocale(locale), {
    style: "currency",
    currency,
  }).format(cents / 100);
}

/**
 * Anything the API hands over as a moment: an ISO 8601 string, a Date, or null
 * for a moment that has not happened.
 */
export type Instant = string | number | Date | null | undefined;

/**
 * The Date behind an Instant, or null when there is nothing to draw.
 *
 * Null and unparseable collapse to the same answer on purpose: a caller renders
 * nothing in both cases, and the alternative — "Invalid Date" in a table cell —
 * is the failure mode `new Date("")` produces silently.
 */
function toDate(value: Instant): Date | null {
  if (value === null || value === undefined || value === "") return null;
  const date = value instanceof Date ? value : new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

/**
 * A moment, drawn in a stated zone with the reader's marks.
 *
 * `timeZone` is required for the same reason `currency` is: the zone is a domain
 * fact — the Event's own timezone for an Event's schedule, Ecuador's for the
 * Reversal Window's cutoff (ADR 0018) — and the reader's language has no vote in
 * it. Leaving it out is what `Intl.DateTimeFormat(locale, {})` does by default,
 * and the default is the machine's zone, which is how "doors open at 7pm" becomes
 * a different hour for a colleague who travelled.
 *
 * `options` is open so a surface can ask for the shape it needs. It is passed
 * after the zone rather than merged with it so that no caller can spell
 * `timeZone` twice and have the wrong one win: the explicit argument is applied
 * last and always wins.
 */
export function formatInstant(
  value: Instant,
  timeZone: string,
  locale: AppLocale,
  options: Intl.DateTimeFormatOptions = {},
): string | null {
  const date = toDate(value);
  if (!date) return null;
  return new Intl.DateTimeFormat(staffIntlLocale(locale), { ...options, timeZone }).format(date);
}

/** A moment as a date alone — "Mar 1, 2026" / "1 mar 2026" — in a stated zone. */
export function formatDate(value: Instant, timeZone: string, locale: AppLocale): string | null {
  return formatInstant(value, timeZone, locale, { dateStyle: "medium" });
}

/** A moment as a date and a time — the form most staff tables want. */
export function formatDateTime(value: Instant, timeZone: string, locale: AppLocale): string | null {
  return formatInstant(value, timeZone, locale, { dateStyle: "medium", timeStyle: "short" });
}

/** A moment as an hour alone, in a stated zone. */
export function formatTime(value: Instant, timeZone: string, locale: AppLocale): string | null {
  return formatInstant(value, timeZone, locale, { timeStyle: "short" });
}

/**
 * A calendar day the API states as "YYYY-MM-DD" — a Payout's paid-at day, an
 * Event's day on a report.
 *
 * Separate from `formatDate` because it is a genuinely different kind of value
 * and the difference is a bug people keep writing. A calendar day is not a
 * moment: it has no hour and therefore no zone, and handing "2026-03-01" to
 * `new Date()` reads it as UTC midnight, which is the 28th of February for
 * everyone west of Greenwich — which is where this platform sells. So the parts
 * are read out of the string and never turned into an instant at all, and there
 * is no `timeZone` parameter to pass because there is nothing here for one to
 * interpret.
 *
 * A string that is not a calendar day comes back untouched, so a shape the API
 * changes shows the raw value rather than a wrong date.
 */
export function formatCalendarDay(day: string, locale: AppLocale): string {
  const [year, month, date] = day.split("-").map(Number);
  if (!year || !month || !date) return day;
  return new Intl.DateTimeFormat(staffIntlLocale(locale), {
    year: "numeric",
    month: "short",
    day: "numeric",
    // UTC on both sides of the round trip: the parts go in as a UTC instant and
    // are read back out in UTC, so the day that comes out is the day that went
    // in, on every machine.
    timeZone: "UTC",
  }).format(new Date(Date.UTC(year, month - 1, date)));
}

/**
 * The platform's own wall clock: the zone Ecuadorian rules are stated in, and
 * the zone to draw a platform-wide fact in when nothing else names one.
 *
 * Named here so a call site reaches for a constant rather than passing
 * `undefined` and getting the reader's machine. It is NOT a default for
 * `formatInstant` — an Event's times belong to the Event's timezone, and a
 * fallback here would make it silently easy to draw them in Guayaquil.
 */
export const PLATFORM_TIME_ZONE = "America/Guayaquil";
