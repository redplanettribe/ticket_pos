/**
 * What a deliberate choice of language is written down as.
 *
 * The switcher itself is a client component — it has to be, because only the
 * browser's router knows which page it is on — and a client component is the
 * one place in this app a unit test cannot reach. So the part that can be wrong
 * lives here instead.
 *
 * Where the switcher POINTS is decided by lib/current-path.ts, which the
 * header's sign-in link needs on the same terms; this module keeps only the
 * half that is about language.
 */

// The ".ts" is written out because the unit tests run this module directly
// under `node --experimental-strip-types`, which resolves specifiers exactly.
// Next resolves it identically.
import { LOCALE_COOKIE, type AppLocale } from "./locale.ts";

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
