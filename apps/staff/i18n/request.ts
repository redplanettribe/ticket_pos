import { getRequestConfig } from "next-intl/server";
import { cookies, headers } from "next/headers";

import { resolveSignedInStaffLocale, STAFF_LOCALE_COOKIE } from "@/lib/staff-locale";
import { loadSession } from "@/lib/staff-session";

/**
 * What next-intl knows about the request being rendered: which locale, and the
 * messages for it.
 *
 * next-intl is wired in here WITHOUT its routing half — no `createNavigation`,
 * no `defineRouting`, no middleware and above all no `[locale]` URL segment.
 * That is ADR 0041's decision and it is the whole difference from the
 * Storefront: there, the locale comes off the address and from nowhere else,
 * because a Storefront page is public, shareable and indexed, and an address
 * that does not say which language it serves is a real problem. Every staff page
 * is behind authentication, so none of that applies, and a segment would have
 * charged for it in a rewrite of every route in the application.
 *
 * So the locale comes off the PERSON, in one ladder with three rungs:
 *
 *   1. the Staff Locale stored against their email address, if they are signed
 *      in and have stated one;
 *   2. the NEXT_LOCALE cookie, which a switcher wrote;
 *   3. what the browser says it reads, and then English.
 *
 * Rungs 2 and 3 are the shared ladder from @ticket-pos/locale, named through
 * lib/staff-locale.ts so they stay testable; rung 1 is what this file adds and it
 * outranks both, because the cookie is a guess about somebody unknown and rung 1
 * only exists when they are known.
 *
 * NO REQUEST IS ADDED TO THE RENDER PATH BY ANY OF THIS. `loadSession` is the
 * same React-cached call the shell makes on every server render to resolve the
 * Active Member, and the API put the person's `locale` on that response for
 * exactly this reason (ADR 0041). Asking here and asking in the shell is one HTTP
 * call. On the sign-in page there is no session cookie, so `loadSession` returns
 * null without calling anything and the cost is what it always was.
 *
 * The session's `locale` is read rather than /staff/me's, and the two agree.
 * /staff/me answers NO_ACTIVE_MEMBER for a Platform Operator who is a Member of
 * no Organization — a person who has a language and no membership to hang it on —
 * so reading it there would have left the one reader the Operator Dashboard
 * exists for stuck in English. /auth/session reports the same field beside the
 * email it belongs to and answers for everybody with a session.
 *
 * Consequence, accepted and stated in the ADR: reading `cookies()` and
 * `headers()` makes every page that renders under this config per-reader and
 * therefore dynamic. Nothing is lost — every staff page was already dynamic,
 * behind a session and scoped to an Organization.
 */
export default getRequestConfig(async () => {
  const [cookieStore, headerList, session] = await Promise.all([
    cookies(),
    headers(),
    loadSession(),
  ]);

  const locale = resolveSignedInStaffLocale({
    stored: session?.locale,
    cookie: cookieStore.get(STAFF_LOCALE_COOKIE)?.value,
    acceptLanguage: headerList.get("accept-language"),
  });

  return {
    locale,
    messages: (await import(`../messages/${locale}.json`)).default,
  };
});
