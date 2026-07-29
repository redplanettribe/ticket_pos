/**
 * Which language an unprefixed route handler sends a visitor into.
 *
 * The handlers under /api, plus /checkout/return and /tickets/confirm, have no
 * locale segment to inherit: their addresses are fixed constants held by a
 * Payment Provider, an email or this app's own scripts. But every page they
 * hand a visitor on to does have one, so they have to choose.
 *
 * They choose with the same chain the middleware uses for an address naming no
 * language — the switcher's cookie, then the browser's languages, then English
 * — because it is the same question. It decides a REDIRECT TARGET and nothing
 * else; no page's content is ever computed from it (see lib/locale.ts).
 *
 * It is an approximation for one case: a visitor reading in Spanish whose
 * browser says English, and who has not used the switcher, comes back from the
 * Payment Provider into English. Carrying the locale across those round trips
 * is a separate change; this at least never sends anybody somewhere they cannot
 * read.
 */

import { cookies, headers } from "next/headers";

import { LOCALE_COOKIE, resolveLocale, type AppLocale } from "./locale";

export async function redirectLocale(): Promise<AppLocale> {
  const [cookieStore, headerList] = await Promise.all([cookies(), headers()]);
  return resolveLocale({
    cookie: cookieStore.get(LOCALE_COOKIE)?.value,
    acceptLanguage: headerList.get("accept-language"),
  });
}
