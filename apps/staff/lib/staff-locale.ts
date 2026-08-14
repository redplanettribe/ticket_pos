/**
 * Which language the staff app is written in for the person reading it, before
 * anyone has signed in.
 *
 * What a Locale *is* — the two languages, the default, the ladder that picks one
 * out of a request, the Intl tag each formats under — is platform-wide and lives
 * in @ticket-pos/locale, because the Storefront answers those same questions and
 * two copies could drift on what "es" means. What is left here is the part that
 * is only true of this app.
 *
 * And the part that is only true of this app is the absence of a URL. The
 * Storefront carries its Locale in a path prefix, so a page's language is a fact
 * about its address; the staff app carries none, because ADR 0041 makes the
 * Staff Locale a property of the person rather than the page. Every staff page
 * is behind authentication, so there is nothing to make shareable or indexable
 * by language, and a `[locale]` segment would have bought that at the price of
 * rewriting every route.
 *
 * Two functions, and they answer the two halves of "before sign-in":
 *
 *   - resolveStaffLocale reads a request and says which language to render in.
 *   - localeChoiceCookie writes down a deliberate choice so the next request
 *     does not have to guess.
 *
 * They live in lib/ rather than beside i18n/request.ts or the middleware for a
 * blunt reason: the test runner globs `lib/*.test.ts` and sees nothing else. The
 * Storefront's middleware is untested today for exactly that reason and this
 * does not repeat it.
 *
 * Nothing here knows about a signed-in person yet. Once the Staff Locale is
 * stored against an email address, the stored value wins over both rungs below
 * and rewrites the cookie — that is a later ticket, and this is the seam it
 * lands on.
 */

import { LOCALE_COOKIE, resolveLocale, type AppLocale } from "@ticket-pos/locale";

/**
 * The cookie remembering a deliberate choice of language.
 *
 * Re-exported under a staff-facing name rather than aliased away: it is
 * deliberately the SAME cookie the Storefront writes, with the name Next's own
 * i18n conventions use, so a person who picked Spanish on the Storefront and
 * followed the "Create an event" invitation across (ADR 0037) arrives at a
 * Spanish login page instead of being asked twice.
 */
export const STAFF_LOCALE_COOKIE = LOCALE_COOKIE;

/**
 * How long a chosen language is remembered: a year, because the choice is a fact
 * about the person and not about the visit. Nothing is lost if it expires — the
 * ladder falls back to Accept-Language — and after sign-in the stored Staff
 * Locale will outrank it anyway.
 */
const ONE_YEAR_IN_SECONDS = 60 * 60 * 24 * 365;

/**
 * The language to render a pre-authentication page in: the cookie, then what the
 * browser says it reads, then English.
 *
 * This is `resolveLocale` and only `resolveLocale` — the ladder is not
 * reimplemented here, it is named here, so that the two apps cannot come to
 * disagree about what "es" means or which language is the floor. The wrapper
 * exists to give this app one place to say *when* the ladder applies, which is:
 * only while nobody is signed in.
 */
export function resolveStaffLocale(request: {
  cookie?: string | null;
  acceptLanguage?: string | null;
}): AppLocale {
  return resolveLocale(request);
}

/**
 * The `document.cookie` string recording a deliberate choice of language.
 *
 * This is the only thing in the staff app that writes NEXT_LOCALE today, and it
 * is written from a click and never from an Accept-Language header: a cookie set
 * from what a browser happened to ask for would record an accident as a
 * preference, and the whole point of the login switcher is to overrule an
 * accident.
 *
 * ADR 0041 is explicit that the login switcher and the shell switcher write
 * different things and must not later be "simplified" into one. This is the
 * login half: there is nobody signed in yet, so there is no person to store a
 * Staff Locale against, and the cookie is all there is to write. The shell
 * switcher will write the stored value *and* this cookie.
 *
 * `SameSite=Lax` because the cookie has to survive the top-level navigation in
 * from the Storefront. `secure` is passed rather than assumed so a dev server on
 * plain http is not handed a cookie the browser will silently drop.
 */
export function localeChoiceCookie(locale: AppLocale, options?: { secure?: boolean }): string {
  const attributes = [
    // Site-wide: the choice belongs to the person, not to the page they made it
    // on, and a cookie scoped to /login would be invisible on every page they
    // reach after signing in.
    "Path=/",
    `Max-Age=${ONE_YEAR_IN_SECONDS}`,
    "SameSite=Lax",
  ];
  if (options?.secure) {
    attributes.push("Secure");
  }
  return `${STAFF_LOCALE_COOKIE}=${locale}; ${attributes.join("; ")}`;
}
