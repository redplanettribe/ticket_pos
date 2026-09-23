/**
 * The file-download proxies' one shape: the Holder Export, the Sales Export and
 * the Tax Invoice documents each hand the Go API's answer to the browser through
 * here, so they cannot differ in which headers, errors or bytes reach the
 * reader.
 *
 * It imports nothing from Next or from the app's aliases, which is what lets
 * `node --test` exercise it directly. It forwards the same headers as the
 * storefront's `forwardDocument` (apps/storefront/lib/document-download.ts) and,
 * like it, streams both a refusal and a file; the two apps share no library
 * code.
 */

import { proxyRead } from "./reader-abort.ts";

/**
 * The headers passed on from the API as they came, on a refusal and on a file
 * alike when the API sent them. Retry-After is how a busy export tells the
 * reader when to try again; Cache-Control is how the API keeps a roster out of
 * shared caches. Content-Type is not here because each path has its own
 * fallback for it.
 */
const FORWARDED_HEADERS = ["Content-Disposition", "Retry-After", "Cache-Control"] as const;

/**
 * forwardDownload turns the API's response into the browser's.
 *
 * A refusal is the API's JSON envelope passed through unchanged, with its status,
 * so the caller can show the reason rather than a broken download. It is
 * streamed as a file is, under the API's content type or `application/json`
 * when the API sent none.
 *
 * A file is passed through AS A STREAM AND NEVER BUFFERED (ADR 0075): the
 * Holder Export has no size limit, so buffering it here would put the whole
 * roster in this process's memory. The stream is also what carries a failure
 * through: when the API aborts a download part way, the upstream body errors,
 * this response errors with it, and the browser sees a broken connection rather
 * than a clean end, so its blob() rejects and no file is saved.
 */
export function forwardDownload(upstream: Response, fileContentType: string): Response {
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

/**
 * proxyDownload fetches a download from the API on behalf of the browser's
 * request and forwards it.
 *
 * The upstream fetch is handed the browser request's abort signal, so a reader
 * who gives up while the API is still preparing the file (opening the snapshot,
 * reading the first batch) stops that work instead of leaving it running for
 * nobody. Once the file is flowing, a reader who goes away cancels this
 * response's body, which is the upstream body, and that cancels the upstream
 * fetch in turn.
 *
 * A reader who gives up before the upstream fetch resolves is answered as every
 * proxied read answers one (see proxyRead): quietly, with an empty 499. Any
 * other failure is thrown.
 */
export async function proxyDownload(
  request: Request,
  fetchUpstream: (signal: AbortSignal) => Promise<Response>,
  fileContentType: string,
): Promise<Response> {
  return proxyRead(request, async (signal) => forwardDownload(await fetchUpstream(signal), fileContentType));
}

/** The .xlsx media type both exports fall back to. */
export const XLSX_CONTENT_TYPE = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";
