/**
 * The request id, from a Next app's edge to the person looking at an error
 * screen (#772 for the Staff app, #774 for the Storefront, #791 for prefetches).
 *
 * Each app's middleware mints one id per request it sees and:
 *
 *   - forwards it to the render on the request headers (REQUEST_ID_HEADER), so
 *     every API call that render makes carries the same X-Request-ID (the
 *     app's lib/api.ts) and the API's request lines in Cloud Logging are found
 *     by it;
 *   - for a page load or a server action, hands it to the browser in a cookie
 *     script can read (REQUEST_ID_COOKIE), because an error screen is a client
 *     component with no other way to learn which request rendered it: Next
 *     gives a server error only a digest by the time it reaches the browser,
 *     and a crash that happens only in the browser has no request of its own;
 *   - forwards to the render the code an error screen would show
 *     (REFERENCE_CODE_HEADER), so onRequestError logs a failed request and its
 *     Sentry event is tagged with that code beside the request's own id.
 *
 * A client navigation does not write the cookie, because the middleware cannot
 * tell it from a router prefetch: Next deletes its flight headers (rsc,
 * next-router-prefetch and the rest) before the middleware runs, and what is
 * left of both is the same same-origin fetch. Were they to write it, every link
 * prefetched after a page load would overwrite the page's code (#791). So the
 * reference code on an error screen is the id of the page load or server
 * action that last rendered the page, and a client navigation that fails is
 * found by that code in its `reference_code`, beside its own `request_id`.
 *
 * This module is read by the browser too, so it imports nothing of the server.
 */

/** The request header the middleware forwards the id to the render on. */
export const REQUEST_ID_HEADER = "x-request-id";

/** The request header the middleware forwards the code an error screen would show on. */
export const REFERENCE_CODE_HEADER = "x-reference-code";

/** The cookie the error screens read the reference code from. */
export const REQUEST_ID_COOKIE = "ticket_pos_request_id";

type RequestIdCookie = {
  name: string;
  value: string;
  options: { httpOnly: false; sameSite: "lax"; path: "/"; secure: boolean };
};

/** A request as the middleware is given it: Next's flight headers are already gone. */
type MiddlewareRequest = { pathname: string; headers: Headers };

/** The fetch destinations of a document: the browser is loading a page. */
const DOCUMENT_DESTINATIONS = new Set(["document", "iframe", "frame"]);

/**
 * Whether a request is what the person is now looking at: a page load or a
 * server action, but never a BFF call, a browser prefetch or a router fetch.
 *
 * Decided only from headers Next leaves for the middleware. A page load is a
 * document fetch (Sec-Fetch-Dest), or, from a browser that sends no fetch
 * metadata, a request that accepts HTML; a router fetch is neither. A server
 * action carries Next-Action. A browser prefetch says so in Sec-Purpose, or
 * Purpose in older browsers, on what is otherwise a document fetch. A browser
 * prerender (Sec-Purpose "prefetch;prerender", from Chrome's address bar or
 * speculation rules) is a page load: it renders the page the person is most
 * likely about to see, and is shown without being fetched again.
 */
function rendersWhatIsShown(request: MiddlewareRequest): boolean {
  const { pathname, headers } = request;
  if (pathname === "/api" || pathname.startsWith("/api/")) return false;
  const purpose = headers.get("sec-purpose") ?? headers.get("purpose") ?? "";
  if (purpose.includes("prefetch") && !purpose.includes("prerender")) return false;
  if (headers.has("next-action")) return true;
  const destination = headers.get("sec-fetch-dest");
  if (destination !== null) return DOCUMENT_DESTINATIONS.has(destination);
  return (headers.get("accept") ?? "").includes("text/html");
}

/**
 * The cookie handing a request's id to the browser, or null for a request that
 * must not overwrite it.
 *
 * Only a page load (a browser prerender included) or a server action writes it:
 * a BFF call under /api/, a browser prefetch and a router fetch (a link prefetch, or a client navigation,
 * which the middleware cannot tell apart) are not, and any of them can land
 * after the page the person is looking at.
 *
 * Not HttpOnly, because script reads it; not a credential, because it is a
 * random id that only finds log lines. A session cookie, with no Max-Age.
 */
export function referenceCodeCookie(request: MiddlewareRequest, requestId: string): RequestIdCookie | null {
  if (!rendersWhatIsShown(request)) {
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

/**
 * The request headers the middleware forwards to the render: the request's own,
 * with its id (REQUEST_ID_HEADER) and the code an error screen would show for
 * it (REFERENCE_CODE_HEADER) set over whatever the browser sent.
 *
 * That code is the request's own id when it writes the cookie, and otherwise
 * the page's, from the cookie the request carries, if it is one of ours. With
 * neither, no code is forwarded.
 */
export function forwardedRequestHeaders(request: MiddlewareRequest, requestId: string): Headers {
  const headers = new Headers(request.headers);
  headers.set(REQUEST_ID_HEADER, requestId);
  const code = rendersWhatIsShown(request)
    ? requestId
    : mintedRequestId(cookieValue(request.headers.get("cookie") ?? "", REQUEST_ID_COOKIE));
  if (code) {
    headers.set(REFERENCE_CODE_HEADER, code);
  } else {
    headers.delete(REFERENCE_CODE_HEADER);
  }
  return headers;
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
