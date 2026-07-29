import { useLocale } from "next-intl";

import type { IntlLocale } from "@/lib/format";
import { intlLocale, toAppLocale } from "@/lib/locale";

/**
 * The Intl tag the surface being rendered formats its prices and dates under.
 *
 * Every client component that shows a price or a date needs the same three
 * steps — ask next-intl what locale this render is in, narrow the string it
 * hands back to an AppLocale, map that to the Intl tag lib/format.ts takes — and
 * a component that skips the middle step or reaches for a tag of its own is a
 * card that formats in a language its own page is not in. Written once here,
 * that walk is not something a call site can get halfway right.
 *
 * The Event's timezone and the Organization's currency are still the caller's to
 * pass: this decides words and number marks and nothing else (ADR 0018).
 *
 * Server components use getFormatLocale() from ./format-locale.server.ts, which
 * is the same walk from next-intl's server side.
 */
export function useFormatLocale(): IntlLocale {
  return intlLocale(toAppLocale(useLocale()));
}
