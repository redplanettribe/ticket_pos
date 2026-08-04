/**
 * The hand-off to an Event's Registration Link, and the counting of it.
 *
 * The Register button does not point at the registration site. It points at a
 * Storefront route of ours which counts the hand-off and then redirects, so the
 * count and the navigation are THE SAME EVENT. A client-side beacon would be a
 * second, separate event that can disagree with the first in both directions:
 * dropped during page unload so a real hand-off goes uncounted, or fired while
 * the Customer then cancels the navigation. It also survives JavaScript being
 * off, middle-click and open-in-new-tab, which a beacon does not.
 *
 * The route lives here rather than being a direct link to the Go API because in
 * production the API is deliberately NOT browser-reachable: the deploy workflow
 * omits NEXT_PUBLIC_API_URL (.github/workflows/deploy.yml), so the Storefront's
 * only API access is server-side. A link straight to the API would work in dev
 * and break in production.
 *
 * Everything the route decides lives here as pure functions, so the route file
 * is a redirect and nothing else.
 */

// Extensioned imports so the node:test runner, which loads this module for the
// pure helpers, can resolve them — the same reason the other tested helpers do.
import { callBackend } from "./api.ts";
import {
  isExternallyRegistered,
  registrationDestination,
  type EventRegistration,
} from "./registration.ts";

/**
 * registrationClickPath is the address of the hand-off route for one Event:
 * `/{orgSlug}/events/{eventSlug}/register`, the Event page's own path with one
 * segment on the end.
 *
 * It takes the two slugs and NOTHING ELSE, and that is a security property
 * rather than a convenience. A route that accepted the destination as a query
 * parameter would be an open redirect wearing this platform's domain — a
 * phishing primitive that anyone could aim anywhere by editing a URL. The
 * destination is read from the Event record, so the only addresses this route
 * can send anyone to are ones an Organization stored on its own Event.
 *
 * Unlocalized: the Locale prefix is the caller's to add (the middleware
 * redirects an unprefixed address into one anyway). A hand-off is the same
 * hand-off in every language, and the route renders nothing to translate.
 */
export function registrationClickPath(orgSlug: string, eventSlug: string): string {
  return `/${encodeURIComponent(orgSlug)}/events/${encodeURIComponent(eventSlug)}/register`;
}

/**
 * registrationHandoff answers the route's whole question: is there somewhere to
 * send this Customer, and where.
 *
 * Null means not-found, and it is deliberately the same answer for all three
 * ways of getting there — an Event that sells Ticket Types here, one whose
 * Registration Link has not been filled in yet, and one whose stored link is not
 * a URL a browser may follow. None of them is a hand-off, and this route has
 * nothing else to offer anybody.
 *
 * The https rule comes from registrationDestination, which is the same check the
 * Register button's href passes through — this value becomes a Location header,
 * and a "javascript:" or "data:" link surviving into one would be exactly the
 * failure the backend's stored-value rule exists to prevent. Restating it here
 * is the point: the two enforcement sites fail independently.
 */
export function registrationHandoff(event: EventRegistration | null): { href: string } | null {
  if (!event || !isExternallyRegistered(event)) return null;
  const destination = registrationDestination(event.registration_url);
  return destination === null ? null : { href: destination.href };
}

/**
 * recordRegistrationClick counts one hand-off, fire-and-forget.
 *
 * Every failure is swallowed — the API down, a network error, an Event the API
 * resolves to nothing. A tracking problem must never stop someone registering:
 * the worst case here costs an organizer one number, and the Customer is already
 * on their way. The returned promise resolves whatever happens, so a caller that
 * awaits it cannot be made to fail and one that does not cannot produce an
 * unhandled rejection.
 *
 * The count is not deduplicated anywhere on this path, deliberately. It counts
 * clicks, not people: the platform loses sight of the Customer at the link and
 * never learns whether they registered.
 */
export async function recordRegistrationClick(orgSlug: string, eventSlug: string): Promise<void> {
  try {
    await callBackend(
      `/api/v1/public/organizations/${encodeURIComponent(orgSlug)}/events/${encodeURIComponent(
        eventSlug,
      )}/registration-link/click`,
      { method: "POST" },
    );
  } catch (error) {
    // Display-only stats, and a Customer mid-navigation on the other end, so
    // the failure is swallowed rather than raised. It is logged because this is
    // the only place that hears about it: the API never learns of a request
    // that never arrived, so a silent catch would make an outage look like an
    // Event nobody clicked.
    console.error("failed to record a Registration Link click", error);
  }
}
