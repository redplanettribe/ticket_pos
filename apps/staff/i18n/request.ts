import { getRequestConfig } from "next-intl/server";
import { cookies, headers } from "next/headers";

import { resolveStaffLocale, STAFF_LOCALE_COOKIE } from "@/lib/staff-locale";

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
 * So the locale comes off the person instead. Today, before anyone has signed
 * in, that means the cookie then Accept-Language then English — the shared
 * ladder, named through lib/staff-locale.ts so it stays testable. Once the Staff
 * Locale is stored against an email address, the stored value wins here and the
 * cookie is rewritten to match; this function is the one place that changes.
 *
 * Consequence, accepted and stated in the ADR: reading `cookies()` and
 * `headers()` makes every page that renders under this config per-reader and
 * therefore dynamic. Nothing is lost — every staff page was already dynamic,
 * behind a session and scoped to an Organization.
 */
export default getRequestConfig(async () => {
  const [cookieStore, headerList] = await Promise.all([cookies(), headers()]);

  const locale = resolveStaffLocale({
    cookie: cookieStore.get(STAFF_LOCALE_COOKIE)?.value,
    acceptLanguage: headerList.get("accept-language"),
  });

  return {
    locale,
    messages: (await import(`../messages/${locale}.json`)).default,
  };
});
