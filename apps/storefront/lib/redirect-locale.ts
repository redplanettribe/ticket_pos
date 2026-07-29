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
 * It is a guess, and the two round trips that can do better than guessing do:
 * the begin-checkout route writes the language the buyer set off in into the
 * checkout context, and /checkout/return prefers that, reaching for this chain
 * only when the cookie is gone, damaged, or older than the field. A Confirmation
 * Link has nothing of the sort — it is opened days later, from an inbox, often
 * on another device — so it chooses here and always will.
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
