/**
 * The Storefront's not-found screen for an address no route matches (#799),
 * app/global-not-found.tsx. Next renders it as the whole document, outside the
 * `[locale]` layout and with no params, so the language has to come from the
 * address some other way: the middleware forwards the path it was asked for on
 * this request header, and the screen reads the locale off that path. The
 * address is still the only input, as the middleware's invariant requires;
 * nothing about who is asking is read.
 */

import { DEFAULT_APP_LOCALE, localePrefixOf, type AppLocale } from "./locale.ts";

/** The request header the middleware forwards the requested path on. */
export const REQUESTED_PATH_HEADER = "x-storefront-requested-path";

/**
 * The language of the not-found screen for a request: the locale its path
 * names. An address the middleware did not see (its matcher skips anything with
 * a dot in it) or that names no locale falls back to the default.
 */
export function notFoundLocale(headers: { get(name: string): string | null }): AppLocale {
  return localePrefixOf(headers.get(REQUESTED_PATH_HEADER) ?? "") ?? DEFAULT_APP_LOCALE;
}
