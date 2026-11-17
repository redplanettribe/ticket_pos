/**
 * What an error screen does with the error it caught, apart from showing it
 * (#772, #774). The Staff app's screens and the Storefront's go through here so
 * they report the same way.
 */

/** An error as Next hands it to a boundary: a server error carries a digest. */
export type CaughtError = Error & { digest?: string };

/** The digest Next gave a thrown value, or null when it has none. */
export function digestOf(error: unknown): string | null {
  if (typeof error === "object" && error !== null && "digest" in error) {
    const digest = (error as { digest: unknown }).digest;
    return typeof digest === "string" && digest ? digest : null;
  }
  return null;
}

/**
 * Whether a thrown value is Next's control flow rather than a failure:
 * `notFound()`, `redirect()` and a bail-out to client rendering throw errors
 * whose digest names them, and a boundary of our own must let them through to
 * Next's. A not-found is not an error and is never reported.
 */
export function isNavigationSignal(error: unknown): boolean {
  const digest = digestOf(error);
  return (
    digest !== null &&
    (digest.startsWith("NEXT_REDIRECT") ||
      digest.startsWith("NEXT_HTTP_ERROR_FALLBACK;") ||
      digest === "NEXT_NOT_FOUND" ||
      digest === "BAILOUT_TO_CLIENT_SIDE_RENDERING")
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
 *   onRequestError (createOnRequestError) has reported it already. Reporting it
 *   again from the browser would make one crash two events, the second with
 *   none of the server's stack.
 * - Anything else was thrown in the browser and only the screen knows of it, so
 *   the screen reports it, tagged with the reference code it shows.
 * - The same error object is reported once however often the screen renders
 *   (a retry that fails the same way throws a new one).
 *
 * With no DSN the capture does nothing: the app's capture sees no SDK running.
 */
export function reportCaughtError(
  error: unknown,
  requestId: string | null,
  capture: CaptureCaughtError,
): boolean {
  if (isNavigationSignal(error) || digestOf(error) !== null) return false;
  if (typeof error === "object" && error !== null) {
    if (reported.has(error)) return false;
    reported.add(error);
  }
  capture(error, requestId);
  return true;
}
