/**
 * What an error screen does with the error it caught (#772), apart from showing
 * it. Both screens, the route-level one and the root-level one, and the boundary
 * inside the staff shell, go through here so they report the same way.
 */

import type { AppLocale } from "@ticket-pos/locale";

import { cookieValue } from "./request-id.ts";
import { STAFF_LOCALE_COOKIE, resolveStaffLocale } from "./staff-locale.ts";

/**
 * Whether a thrown value is Next's control flow rather than a failure:
 * `notFound()` and `redirect()` throw errors whose digest names them, and a
 * boundary of our own must let them through to Next's. A not-found is not an
 * error and is never reported.
 */
export function isNavigationSignal(error: unknown): boolean {
  if (typeof error !== "object" || error === null || !("digest" in error)) {
    return false;
  }
  const digest = (error as { digest: unknown }).digest;
  return (
    typeof digest === "string" &&
    (digest.startsWith("NEXT_REDIRECT") ||
      digest.startsWith("NEXT_HTTP_ERROR_FALLBACK;") ||
      digest === "NEXT_NOT_FOUND")
  );
}

/** Reports one caught error under the page's request id. */
export type CaptureCaughtError = (error: unknown, requestId: string | null) => void;

const reported = new WeakSet<object>();

/**
 * Reports what an error screen caught, exactly once, and says whether it did.
 *
 * - An error with a digest came from the server: Next replaces a server error
 *   with a digest before it reaches the browser, and the server's
 *   onRequestError (instrumentation.ts) has reported it already. Reporting it
 *   again from the browser would make one crash two events, the second with
 *   none of the server's stack.
 * - Anything else was thrown in the browser and only the screen knows of it, so
 *   the screen reports it, tagged with the reference code it shows.
 * - The same error object is reported once however often the screen renders
 *   (a retry that fails the same way throws a new one).
 *
 * With no DSN the capture is the SDK's, which does nothing uninitialised.
 */
export function reportCaughtError(
  error: unknown,
  requestId: string | null,
  capture: CaptureCaughtError,
): boolean {
  if (isNavigationSignal(error)) return false;
  if (typeof error === "object" && error !== null) {
    if ("digest" in error && (error as { digest: unknown }).digest) return false;
    if (reported.has(error)) return false;
    reported.add(error);
  }
  capture(error, requestId);
  return true;
}

/**
 * The language of the root-level screen, which renders in place of the root
 * layout and so without next-intl's provider or the session that carries the
 * stored Staff Locale: the cookie a switcher writes (the shell's switcher writes
 * it beside the stored Staff Locale, so for a signed-in person they agree), then
 * the browser's languages, then English. The route-level screens read the
 * provider's locale, which is the full ladder.
 */
export function rootErrorScreenLocale(browser: { cookies: string; languages: readonly string[] }): AppLocale {
  return resolveStaffLocale({
    cookie: cookieValue(browser.cookies, STAFF_LOCALE_COOKIE),
    acceptLanguage: browser.languages.join(","),
  });
}
