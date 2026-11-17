/**
 * The request id, from a Next app's edge to the person looking at an error
 * screen (#772 for the Staff app, #774 for the Storefront).
 *
 * Each app's middleware mints one id per request it sees and:
 *
 *   - forwards it to the render on the request headers (REQUEST_ID_HEADER), so
 *     every API call that render makes carries the same X-Request-ID (the
 *     app's lib/api.ts) and the API's request lines in Cloud Logging are found
 *     by it, and onRequestError logs and reports a failed render under it;
 *   - hands it to the browser in a cookie script can read (REQUEST_ID_COOKIE),
 *     because an error screen is a client component with no other way to learn
 *     which request rendered it: Next gives a server error only a digest by the
 *     time it reaches the browser, and a crash that happens only in the browser
 *     has no request of its own.
 *
 * So the reference code on an error screen is the id of the last page or RSC
 * navigation request: the one that failed, for a crash in a server render, and
 * the one that rendered the page the person is on, for a crash in the browser.
 *
 * This module is read by the browser too, so it imports nothing of the server.
 */

/** The request header the middleware forwards the id to the render on. */
export const REQUEST_ID_HEADER = "x-request-id";

/** The cookie the error screens read the reference code from. */
export const REQUEST_ID_COOKIE = "ticket_pos_request_id";

type RequestIdCookie = {
  name: string;
  value: string;
  options: { httpOnly: false; sameSite: "lax"; path: "/"; secure: boolean };
};

/**
 * The cookie handing a request's id to the browser, or null for a request that
 * must not overwrite it.
 *
 * A BFF call under /api/ and a router prefetch are not what the person is
 * looking at, and either can land after the page that is: let them write the
 * cookie and a crash on the page would show the id of a request that did not
 * render it. A server action and an RSC navigation do write it, as a failure in
 * either is shown by an error screen.
 *
 * Not HttpOnly, because script reads it; not a credential, because it is a
 * random id that only finds log lines. A session cookie, with no Max-Age.
 */
export function referenceCodeCookie(
  request: { pathname: string; headers: Headers },
  requestId: string,
): RequestIdCookie | null {
  // Next's router marks its prefetches; a browser's own link prefetch says so in Purpose.
  const prefetch = request.headers.has("next-router-prefetch") || request.headers.get("purpose") === "prefetch";
  if (prefetch || request.pathname === "/api" || request.pathname.startsWith("/api/")) {
    return null;
  }
  return {
    name: REQUEST_ID_COOKIE,
    value: requestId,
    options: {
      httpOnly: false,
      sameSite: "lax",
      path: "/",
      secure: process.env.NODE_ENV === "production",
    },
  };
}

/** The shape the middleware mints (a UUID) and the API adopts. */
const REFERENCE_CODE = /^[0-9a-f-]{1,64}$/i;

/** What crypto.randomUUID() mints, and nothing else. */
const MINTED_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/**
 * The request id forwarded to a render, if it has the shape the middleware
 * mints, else null.
 *
 * Where the middleware runs it overwrites whatever the browser sent, but the
 * Storefront's middleware does not run on its route handlers (under /api/,
 * /checkout/return, /tickets/confirm), so there the header is the browser's
 * own. Kept to a UUID, a browser can at most choose an id that looks like one of
 * ours: never text that would land in the API's log lines.
 */
export function mintedRequestId(value: string | null | undefined): string | null {
  return value && MINTED_ID.test(value) ? value : null;
}

/** A cookie's value in a cookie string (document.cookie), or null. */
export function cookieValue(cookies: string, name: string): string | null {
  for (const pair of cookies.split(";")) {
    const separator = pair.indexOf("=");
    if (separator >= 0 && pair.slice(0, separator).trim() === name) {
      return pair.slice(separator + 1).trim();
    }
  }
  return null;
}

/**
 * The reference code in a cookie string (document.cookie), or null when there
 * is none or it is not id-shaped: a person is never shown a value to quote that
 * is not one of ours.
 */
export function readReferenceCode(cookies: string): string | null {
  const value = cookieValue(cookies, REQUEST_ID_COOKIE);
  return value && REFERENCE_CODE.test(value) ? value : null;
}
