/**
 * How the Storefront spells a Locale in a URL.
 *
 * What a Locale *is* — the two languages, the default, the ladder that picks
 * one from a request, the Intl tag each formats under — is platform-wide and
 * lives in @ticket-pos/locale, because the staff app answers those same
 * questions and two copies could drift on what "es" means. What is left here is
 * the part that is only true of this app: a Storefront URL carries its language
 * as a path prefix, so a path can be read for one and written with one.
 *
 * Everything the package owns is re-exported below, so a call site keeps
 * importing the whole vocabulary from "@/lib/locale" and does not have to know
 * which half of it moved.
 */

import { isAppLocale, type AppLocale } from "@ticket-pos/locale";

export {
  DEFAULT_APP_LOCALE,
  LOCALES,
  LOCALE_COOKIE,
  appLocaleFromIntl,
  intlLocale,
  isAppLocale,
  matchAcceptLanguage,
  resolveLocale,
  toAppLocale,
  type AppLocale,
} from "@ticket-pos/locale";

/**
 * The locale a path already names, or null when it names none.
 *
 * Only an exact token counts: "/english" and "/es-EC" are ordinary paths, and
 * "/es" is the Spanish Storefront. The Organization slug pattern shares this
 * alphabet, so an Organization that took the slug "es" would be unreachable —
 * noted as a known trade, exactly as "/signin" and "/tickets" already shadow
 * two slugs.
 */
export function localePrefixOf(pathname: string): AppLocale | null {
  const first = pathname.split("/")[1] ?? "";
  return isAppLocale(first) ? first : null;
}

/**
 * The same path under a locale: "/tickets" becomes "/en/tickets".
 *
 * The path must be a plain absolute path on this Storefront, and may carry a
 * query or fragment — the prefix goes in front of the path, never in front of
 * the "?".
 */
export function localizedPath(locale: AppLocale, path: string): string {
  if (path === "/") return `/${locale}`;
  // "/?q=x" and "/#frag" are the root path wearing a suffix: the lone slash is
  // the prefix's job now, so it is dropped rather than doubled.
  if (path.startsWith("/?") || path.startsWith("/#")) return `/${locale}${path.slice(1)}`;
  return `/${locale}${path}`;
}
