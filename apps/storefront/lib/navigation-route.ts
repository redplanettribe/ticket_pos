/**
 * The route a browser navigation goes to, as the app directory names it, for
 * the name of its trace (ADR 0079): "/es/acme/events/fiesta" is
 * "/[locale]/[orgSlug]/events/[eventSlug]", the name the server's own spans for
 * the same page carry.
 *
 * The SDK names a navigation from a route manifest its build plugin injects
 * into the initial bundle; the Storefront turns that injection off to keep the
 * first load as it was (next.config.ts), so the SDK names it by its path and the
 * browser's beforeSendTransaction renames it here (nameNavigation), in the lazy
 * chunk. Navigations then group by page rather than one per Organization and
 * Event, and no slug travels in a trace name. At send time rather than when
 * the span starts: the SDK renames a navigation span after it has started
 * (Next's router hook and its router patch both name it), so a name given at
 * the start does not last.
 *
 * Every page lives under app/[locale]: the pages named in LOCALE_PAGES, the
 * Organization page at [orgSlug], and the Event page under it.
 * lib/navigation-route.test.ts reads app/[locale] and fails when a page is
 * added there without its name here.
 */
import { isAppLocale } from "./locale.ts";

/** The static pages directly under app/[locale], each its own first segment. */
export const LOCALE_PAGES: readonly string[] = [
  "accept",
  "answer",
  "checkout",
  "confirm-consent",
  "following",
  "privacy",
  "privacy-policy",
  "re-addressing",
  "signin",
  "terms",
  "tickets",
  "unsubscribe",
  "withdraw-consent",
];

/** The route of a pathname, or the pathname as given when it is not a page. */
export function navigationRoute(pathname: string): string {
  const [locale, first, ...rest] = pathname.split("/").filter(Boolean);
  if (locale === undefined || !isAppLocale(locale)) return pathname;
  if (first === undefined) return "/[locale]";
  if (LOCALE_PAGES.includes(first)) return ["", "[locale]", first, ...rest].join("/");
  const [events, eventSlug, ...after] = rest;
  if (events === undefined) return "/[locale]/[orgSlug]";
  if (events === "events" && eventSlug !== undefined) return ["", "[locale]", "[orgSlug]", "events", "[eventSlug]", ...after].join("/");
  return ["", "[locale]", "[orgSlug]", ...rest].join("/");
}

interface NavigationEvent {
  transaction?: string;
  transaction_info?: { source?: string };
  contexts?: Record<string, unknown>;
}

/**
 * A browser transaction with its navigation named by page; any other
 * transaction as it was. The input is not changed.
 */
export function nameNavigation<E extends NavigationEvent>(event: E): E {
  const trace = event.contexts?.trace as { op?: unknown } | undefined;
  if (trace?.op !== "navigation" || event.transaction === undefined) return event;
  const route = navigationRoute(event.transaction);
  if (route === event.transaction) return event;
  return { ...event, transaction: route, transaction_info: { ...event.transaction_info, source: "route" } };
}
