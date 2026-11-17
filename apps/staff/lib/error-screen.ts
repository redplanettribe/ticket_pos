/**
 * The Staff app's part of an error screen (#772). Catching, reporting once and
 * the reference code are shared with the Storefront and live in
 * @ticket-pos/sentry-rules; what is left here is how the root-level screen
 * chooses its language, which is the Staff Locale's rule.
 */

import type { AppLocale } from "@ticket-pos/locale";
import { cookieValue } from "@ticket-pos/sentry-rules";

import { STAFF_LOCALE_COOKIE, resolveStaffLocale } from "./staff-locale.ts";

/**
 * The language of the root-level screen, which renders in place of the root
 * layout and so without next-intl's provider or the session that carries the
 * stored Staff Locale: the cookie a switcher writes (the shell's switcher writes
 * it beside the stored Staff Locale, so for a signed-in person they agree), then
 * the browser's languages, then English. The route-level screens read the
 * provider's locale, which is the full ladder.
 */
export function rootErrorScreenLocale(browser: { cookies: string; languages: readonly string[] }): AppLocale {
  return resolveStaffLocale({
    cookie: cookieValue(browser.cookies, STAFF_LOCALE_COOKIE),
    acceptLanguage: browser.languages.join(","),
  });
}
