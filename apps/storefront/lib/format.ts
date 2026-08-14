// Locale and formatting rules follow docs/design/foundation.md:
// USD-style "$25" in the catalog, "$25.00" only on summed totals; Storefront
// dates render in the Event's timezone as "Saturday, July 12, 2026 · 7:00 PM".
//
// Every helper here takes the language to render in as its last argument, so a
// page can hand down the locale it was routed under instead of this module
// deciding for the whole app. The default is the language the Storefront has
// always spoken, which is what keeps every call site that has not been migrated
// yet printing exactly what it printed before.
//
// Language is not time zone. A locale chooses words and number marks; the zone a
// moment is drawn in is a separate argument decided by domain rules — the
// Event's own timezone for Event times, Ecuador for the Reversal Window (ADR
// 0018) — and no locale may reach it.

/**
 * The Intl language-and-region tag a value is rendered under.
 *
 * Named for Intl and not for the domain, because the domain's Locale is the URL
 * token — "en", "es" — and there is only one of those. This is the other
 * vocabulary: the tag `Intl.NumberFormat` and `Intl.DateTimeFormat` are handed,
 * which carries a region because a decimal mark and a currency symbol's
 * position are regional facts.
 *
 * Defined by @ticket-pos/locale, which owns the mapping between the two and is
 * the only thing that should be holding both at once, and re-exported here
 * because this module is where the Storefront's formatters take it from.
 */
import type { IntlLocale } from "@ticket-pos/locale";

export type { IntlLocale };

export const DEFAULT_LOCALE: IntlLocale = "en-US";

export function formatPrice(
  cents: number,
  currency: string,
  locale: IntlLocale = DEFAULT_LOCALE,
): string {
  const hasFraction = cents % 100 !== 0;
  return new Intl.NumberFormat(locale, {
    style: "currency",
    currency,
    minimumFractionDigits: hasFraction ? 2 : 0,
    maximumFractionDigits: 2,
  }).format(cents / 100);
}

/**
 * What a catalog card owes to say about an Event's cheapest ticket, as data
 * rather than as a sentence: which of the two things is true, and the amount to
 * put in it.
 *
 * The sentence is left to the caller because "From $25" is English word order
 * with an English word in it, and a translated Storefront has to build it from a
 * message template. Null still means "say nothing at all" — an Event with no
 * priced Ticket Type has no claim to make, which is not the same as being free.
 */
export type PriceFrom = { kind: "free" } | { kind: "from"; price: string };

export function priceFrom(
  cents: number | null,
  currency: string,
  locale: IntlLocale = DEFAULT_LOCALE,
): PriceFrom | null {
  if (cents === null) return null;
  if (cents === 0) return { kind: "free" };
  return { kind: "from", price: formatPrice(cents, currency, locale) };
}

function timeZoneOrUndefined(timezone: string | null): string | undefined {
  return timezone ?? undefined;
}

// Long form for event pages: "Saturday, July 12, 2026 · 7:00 PM".
export function formatEventDateTime(
  startsAt: string | null,
  timezone: string | null,
  locale: IntlLocale = DEFAULT_LOCALE,
): string | null {
  if (!startsAt) return null;
  const date = new Date(startsAt);
  if (Number.isNaN(date.getTime())) return null;

  const day = new Intl.DateTimeFormat(locale, {
    weekday: "long",
    month: "long",
    day: "numeric",
    year: "numeric",
    timeZone: timeZoneOrUndefined(timezone),
  }).format(date);

  const time = new Intl.DateTimeFormat(locale, {
    hour: "numeric",
    minute: "2-digit",
    timeZone: timeZoneOrUndefined(timezone),
  }).format(date);

  return `${day} · ${time}`;
}

/**
 * The platform's own wall clock, and the zone the Reversal Window's 20:00 cutoff
 * is stated in (ADR 0018). It is the same for every Event on the platform no
 * matter where the Event is, and it must never be confused with an Event's own
 * timezone — that one interprets the Event's schedule and nothing else.
 */
export const ECUADOR_TIME_ZONE = "America/Guayaquil";

/**
 * The instant a Reversal Window closes, drawn in Ecuador time: "Tue Jul 7,
 * 8:00 PM".
 *
 * A deadline set by an Ecuadorian wall-clock rule is read in Ecuadorian
 * wall-clock time, so the "8:00 PM" a buyer sees is the same 8:00 PM the rule
 * names. Drawing it in the Event's timezone — the obvious-looking thing to do on
 * a card about an Event — would print a deadline nobody's clock agrees with.
 *
 * The locale translates the words and nothing else: a buyer reading Spanish is
 * still bound by the Ecuadorian hour, so the zone is fixed here and is not a
 * parameter anybody can pass.
 */
export function formatReversalDeadline(
  closesAt: string,
  locale: IntlLocale = DEFAULT_LOCALE,
): string | null {
  return formatEventDateShort(closesAt, ECUADOR_TIME_ZONE, locale);
}

/**
 * The hour alone, in the Event's timezone: "6:00 PM".
 *
 * What a Timeline card says under a Day Bucket header. The date is the
 * header's to say — repeating it on every card is the redundancy the Timeline
 * exists to remove — so this is the short form's time slot and nothing else.
 * Cards in the Ongoing group, whose header names no date, keep using
 * formatEventDateShort.
 */
export function formatEventTime(
  startsAt: string | null,
  timezone: string | null,
  locale: IntlLocale = DEFAULT_LOCALE,
): string | null {
  if (!startsAt) return null;
  const date = new Date(startsAt);
  if (Number.isNaN(date.getTime())) return null;

  return normalizeNarrowSpaces(
    new Intl.DateTimeFormat(locale, {
      hour: "numeric",
      minute: "2-digit",
      timeZone: timeZoneOrUndefined(timezone),
    }).format(date),
  );
}

/**
 * `format()` and `formatToParts()` disagree about the space before "PM": the
 * first hands back U+0020, the second the narrow no-break space ICU actually
 * specifies. Composing from parts therefore changes bytes nobody asked to
 * change — invisible on screen, but enough to break a string comparison — so the
 * one character is put back the way the whole app has always rendered it. The
 * no-break space inside Spanish's "p. m." is left alone: `format()` keeps that
 * one too.
 */
function normalizeNarrowSpaces(value: string): string {
  return value.replaceAll("\u202f", " ");
}

/**
 * Which of the compact form's three slots a formatted field belongs in. The
 * slots exist because the separators between them are ours, while everything
 * inside a slot — the order of day and month, the marks between them — belongs
 * to the locale.
 */
const SHORT_DATE_SLOTS: Partial<Record<Intl.DateTimeFormatPartTypes, "weekday" | "date" | "time">> =
  {
    weekday: "weekday",
    era: "date",
    year: "date",
    month: "date",
    day: "date",
    hour: "time",
    minute: "time",
    second: "time",
    dayPeriod: "time",
    timeZoneName: "time",
  };

/**
 * Compact form for cards: "Sun Jul 12, 6:00 PM".
 *
 * Composed from parts rather than patched after the fact. The previous version
 * formatted the whole thing and then deleted the first comma and rewrote " at ",
 * which is en-US grammar applied to whatever a locale produced: in Spanish the
 * comma it deleted is the one separating the weekday from the day, so "dom, 12
 * jul" came out as "dom 12 jul" only by luck, and a locale that orders things
 * differently would have lost a separator it needed. Splitting the parts into
 * slots keeps every locale's internal punctuation intact and puts ours only
 * where we mean it.
 */
export function formatEventDateShort(
  startsAt: string | null,
  timezone: string | null,
  locale: IntlLocale = DEFAULT_LOCALE,
): string | null {
  if (!startsAt) return null;
  const date = new Date(startsAt);
  if (Number.isNaN(date.getTime())) return null;

  const parts = new Intl.DateTimeFormat(locale, {
    weekday: "short",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    timeZone: timeZoneOrUndefined(timezone),
  }).formatToParts(date);

  const slots = { weekday: "", date: "", time: "" };
  let currentSlot: keyof typeof slots | null = null;
  let pendingLiteral = "";

  for (const part of parts) {
    if (part.type === "literal") {
      pendingLiteral += part.value;
      continue;
    }
    const slot = SHORT_DATE_SLOTS[part.type];
    if (!slot) continue;
    // A literal is kept only between two fields of the same slot; the ones that
    // sit on a slot boundary are the locale's way of joining things we join
    // ourselves.
    if (slot === currentSlot) slots[slot] += pendingLiteral;
    pendingLiteral = "";
    slots[slot] += part.value;
    currentSlot = slot;
  }

  const day = [slots.weekday, slots.date].filter(Boolean).join(" ");
  return normalizeNarrowSpaces([day, slots.time].filter(Boolean).join(", "));
}
