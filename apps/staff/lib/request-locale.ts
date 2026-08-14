import { cookies, headers } from "next/headers";

import { resolveStaffLocale, STAFF_LOCALE_COOKIE } from "@/lib/staff-locale";

import type { AppLocale } from "@ticket-pos/locale";

/**
 * The language THIS request was detected as, for a request handler that has no
 * next-intl context to ask.
 *
 * There is exactly one caller shape: the sign-in routes. ADR 0041 has the
 * detected language persisted as the Staff Locale at sign-in if the person has
 * none, and that write is what makes somebody's app and their mail agree without
 * their ever visiting a setting. The evidence is deliberately weak but real — the
 * browser stated a preference, and the person read a sign-in page rendered in it
 * and proceeded — which is why it only ever FILLS an absence and never overwrites
 * a stated choice. The API enforces that; this end only supplies the observation.
 *
 * It is the pre-authentication ladder and nothing more, which is correct here:
 * at the moment a verify body is being built there is no session yet, so there is
 * no stored value that could outrank the cookie. The signed-in ladder
 * (`resolveSignedInStaffLocale`) belongs to rendering, not to signing in.
 *
 * A separate function from i18n/request.ts rather than a shared one because the
 * two ask different questions of the same request, and collapsing them would put
 * a session lookup on the sign-in path that mints the session.
 */
export async function detectedStaffLocale(): Promise<AppLocale> {
  const [cookieStore, headerList] = await Promise.all([cookies(), headers()]);
  return resolveStaffLocale({
    cookie: cookieStore.get(STAFF_LOCALE_COOKIE)?.value,
    acceptLanguage: headerList.get("accept-language"),
  });
}
