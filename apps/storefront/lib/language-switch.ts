/**
 * The two facts the language switcher needs, as pure functions over strings.
 *
 * The switcher itself is a client component — it has to be, because only the
 * browser's router knows which page it is on — and a client component is the
 * one place in this app a unit test cannot reach. So the parts that can be
 * wrong live here instead: where the other language's copy of THIS page is, and
 * what a deliberate choice of language is written down as.
 *
 * Neither function touches `document`, a request, or a locale prefix. The
 * prefix is added afterwards by next-intl's `Link` (i18n/navigation.ts), which
 * is the same code path every other internal link goes through, so the switcher
 * cannot drift from the rest of the Storefront's linking.
 */

// The ".ts" is written out because the unit tests run this module directly
// under `node --experimental-strip-types`, which resolves specifiers exactly.
// Next resolves it identically.
import { LOCALE_COOKIE, type AppLocale } from "./locale.ts";

/**
 * The address the visitor is at, language stripped off, query kept.
 *
 * Both halves matter. `pathname` arrives from next-intl's `usePathname`, which
 * has already removed the locale token, and it is handed straight back to
 * next-intl's `Link` with a different one — so switching language on
 * /es/acme/events/gala lands on the same Event in English rather than at the
 * explorer, which is where "switch language" that forgets the path always ends
 * up.
 *
 * The query is carried because on this Storefront it IS the page: the explorer's
 * filters live in it, and a visitor who searched, filtered, then asked for
 * English would otherwise get an unfiltered explorer and have to start again.
 *
 * The fragment is not carried, because it cannot be: a fragment never leaves the
 * browser, so the server rendering this link has no way to know one exists and
 * writing it in on the client alone would make the markup differ between the two
 * renders.
 */
export function pathWithQuery(pathname: string, search?: string | null): string {
  // A path is normalized rather than trusted: `usePathname` returns "" before
  // the router has a route on some renders, and a href that is not root-relative
  // would be resolved against the current directory — silently producing
  // /es/acme/en/acme.
  const path = pathname.startsWith("/") ? pathname : `/${pathname}`;
  // `useSearchParams().toString()` returns a bare query with no "?", while
  // `location.search` includes one. Accepting both means the caller never has to
  // remember which it is holding.
  const query = (search ?? "").replace(/^\?/, "");
  // The lone slash is kept: "/" plus a query is "/?q=x", and next-intl's
  // prefixer is what collapses that to "/es?q=x" (shared/utils prefixPathname).
  return query ? `${path}?${query}` : path;
}

/**
 * How long a chosen language is remembered.
 *
 * A year, because the choice is a fact about the person and not about the visit:
 * somebody who picked Spanish in March and comes back in October through a link
 * that names no language still reads Spanish. Nothing is lost if it expires —
 * the chain in lib/locale.ts falls back to Accept-Language — and nothing is
 * risked by it lasting, because the cookie only ever picks a redirect target.
 */
const ONE_YEAR_IN_SECONDS = 60 * 60 * 24 * 365;

/**
 * The `document.cookie` string recording a deliberate choice of language.
 *
 * This is the ONLY thing that writes NEXT_LOCALE. The middleware reads it, and
 * next-intl is configured not to write it (`localeCookie: false` in
 * i18n/routing.ts), because a cookie set from Accept-Language would record an
 * accident as a preference. A click on this switcher is not an accident, which
 * is the whole reason the write lives here.
 *
 * What it changes is narrow and worth stating: it decides where an address
 * naming NO language sends this visitor next. A prefixed URL ignores it
 * entirely, so nothing about the page they are on or the page they are going to
 * depends on this write landing.
 *
 * `SameSite=Lax` is what makes it work at all — the cookie has to be sent on the
 * top-level navigation from an email or a poster's QR code, which is exactly the
 * cross-site GET that Lax allows and Strict does not.
 *
 * `secure` is passed rather than assumed so that a dev server on plain http is
 * not handed a cookie the browser will silently drop.
 */
export function localeChoiceCookie(
  locale: AppLocale,
  options?: { secure?: boolean },
): string {
  const attributes = [
    // Site-wide: the choice belongs to the visitor, not to the page they made
    // it on, and a cookie scoped to /es/acme would be invisible at "/" — the one
    // address that needs to read it.
    "Path=/",
    `Max-Age=${ONE_YEAR_IN_SECONDS}`,
    "SameSite=Lax",
  ];
  if (options?.secure) {
    attributes.push("Secure");
  }
  return `${LOCALE_COOKIE}=${locale}; ${attributes.join("; ")}`;
}
