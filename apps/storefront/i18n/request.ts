import { hasLocale } from "next-intl";
import { getRequestConfig } from "next-intl/server";

import { routing } from "./routing";

/**
 * What next-intl knows about the request being rendered: which locale, and the
 * messages for it.
 *
 * The locale comes from the `[locale]` segment and from nowhere else — no
 * cookie, no Accept-Language. That is the invariant the whole scheme rests on:
 * /en/... is English for every visitor and every crawler, /es/... is Spanish
 * for every visitor and every crawler, and the address alone decides. The
 * request's cookie and headers only ever choose where an address naming no
 * language redirects to (lib/locale.ts).
 *
 * An unknown segment falls back to the default here rather than 404ing, because
 * this config also runs for requests that have no `[locale]` segment at all.
 * The 404 for a genuinely unknown language belongs to the layout, which is the
 * only place that knows the segment was really there.
 */
export default getRequestConfig(async ({ requestLocale }) => {
  const requested = await requestLocale;
  const locale = hasLocale(routing.locales, requested) ? requested : routing.defaultLocale;

  return {
    locale,
    messages: (await import(`../messages/${locale}.json`)).default,
  };
});
