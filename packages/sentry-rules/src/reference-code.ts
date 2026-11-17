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
 * So the reference code on an error screen is the id of the last page load or
 * server action: the one that failed, for a crash in a server render of a page
 * or an action, and the one that rendered the page the person is on, for a
 * crash in the browser. A client navigation cannot set it (referenceCodeCookie
 * says why); when its render fails, the screen shows the page's id, and the
 * failure is logged and reported under that id too (request-error.ts), beside
 * its own.
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
 * Only a page load and a server action write it. A page load is what the
 * person is looking at; a server action's failure is shown by an error screen.
 *
 * A router fetch never does, because the middleware cannot tell a client
 * navigation from a link prefetch: Next deletes its flight headers (rsc,
 * next-router-prefetch and the rest) from the request before middleware runs,
 * so both arrive as the same bare fetch. Letting them write it made a page's
 * error screen show the id of whichever prefetch landed last, found nowhere in
 * the logs (#791). A navigation whose render fails still sends this cookie, so
 * onRequestError logs and tags the failure with it (request-error.ts).
 *
 * Nor does a BFF call under /api/, or a browser's own prefetch of a page.
 *
 * Not HttpOnly, because script reads it; not a credential, because it is a
 * random id that only finds log lines. A session cookie, with no Max-Age.
 */
export function referenceCodeCookie(
  request: { pathname: string; headers: Headers },
  requestId: string,
): RequestIdCookie | null {
  if (request.pathname === "/api" || request.pathname.startsWith("/api/")) return null;
  if (!isPageLoad(request.headers) && !request.headers.has("next-action")) return null;
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

/**
 * Whether a request loads a page into a window or a frame, by its fetch
 * metadata; a client that sends none is judged by whether it asks for HTML,
 * which a router fetch does not. A browser's speculative prefetch of a page
 * (Sec-Purpose, or the older Purpose) is not one.
 */
function isPageLoad(headers: Headers): boolean {
  if (/prefetch/i.test(headers.get("sec-purpose") ?? headers.get("purpose") ?? "")) return false;
  const destination = headers.get("sec-fetch-dest");
  if (destination !== null) return PAGE_DESTINATIONS.has(destination);
  return (headers.get("accept") ?? "").includes("text/html");
}

const PAGE_DESTINATIONS = new Set(["document", "iframe", "frame"]);

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
