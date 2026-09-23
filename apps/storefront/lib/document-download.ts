/**
 * How the storefront hands one of the API's file downloads to the browser: the
 * buyer's Tax Document XML and RIDE (tax-document-relay.ts) come through here.
 *
 * It imports nothing from Next or from the app's aliases, which is what lets
 * `node --test` exercise it directly. It forwards the same headers as the staff
 * app's `forwardDownload` (apps/staff/lib/download-proxy.ts) and, like it,
 * streams both a refusal and a file; the two apps share no library code.
 */

/**
 * The headers passed on from the API as they came, on a refusal and on a file
 * alike when the API sent them. Cache-Control is how the API keeps a buyer's
 * invoice out of shared caches (`no-store`); Retry-After is how a busy API says
 * when to try again. Content-Type is not here because a file has its own
 * fallback for it.
 */
const FORWARDED_HEADERS = ["Content-Disposition", "Retry-After", "Cache-Control"] as const;

/**
 * forwardDocument turns the API's response into the browser's.
 *
 * A refusal is the API's JSON envelope passed through unchanged, with its
 * status, so the page can show the reason rather than save a broken file, under
 * the API's content type or `application/json` when the API sent none. A file
 * is passed through under the API's content type, or `fileContentType` when the
 * API sent none. Both are streamed, never buffered.
 */
export function forwardDocument(upstream: Response, fileContentType: string): Response {
  const headers = new Headers();
  for (const name of FORWARDED_HEADERS) {
    const value = upstream.headers.get(name);
    if (value !== null) {
      headers.set(name, value);
    }
  }

  if (!upstream.ok) {
    headers.set("Content-Type", upstream.headers.get("Content-Type") ?? "application/json");
    return new Response(upstream.body, { status: upstream.status, headers });
  }

  headers.set("Content-Type", upstream.headers.get("Content-Type") ?? fileContentType);
  return new Response(upstream.body, { status: 200, headers });
}
