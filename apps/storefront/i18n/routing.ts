import { defineRouting } from "next-intl/routing";

import { DEFAULT_APP_LOCALE, LOCALES } from "@/lib/locale";

/**
 * How locales appear in Storefront URLs, as next-intl needs it stated.
 *
 * The locales themselves live in lib/locale.ts, which is framework-free and
 * unit-tested; this file is the adapter that hands them to next-intl so
 * `Link`, `redirect` and the request config all prefix the same way the
 * middleware does.
 *
 * `localeDetection: false` and `localeCookie: false` are the load-bearing part.
 * They stop next-intl from reading Accept-Language or writing NEXT_LOCALE on
 * its own: this app owns that chain (lib/locale.ts `resolveLocale`), applies it
 * to unprefixed addresses only, and writes the cookie from the language
 * switcher alone. Left on, a visitor's first accidental landing would be
 * recorded as a preference they never expressed.
 */
export const routing = defineRouting({
  locales: LOCALES,
  defaultLocale: DEFAULT_APP_LOCALE,
  // Both languages carry their prefix. There is no unmarked language, so no URL
  // is ambiguous about which one it is (see lib/locale.ts).
  localePrefix: "always",
  localeDetection: false,
  localeCookie: false,
});
