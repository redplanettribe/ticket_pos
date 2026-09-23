/**
 * What a BFF route does when the reader it is proxying for goes away: the file
 * downloads (proxyDownload) and the JSON routes that pass a slow read straight
 * through (proxyJSONRead, in bff.ts) hand the API the browser request's abort
 * signal, and answer a reader who left quietly.
 *
 * It imports nothing from Next or from the app's aliases, which is what lets
 * `node --test` exercise it directly.
 */

/**
 * The status a proxied read answers when its reader went away first. No
 * browser sees it; it is what the server's own request log records.
 */
export const CLIENT_CLOSED_REQUEST = 499;

/**
 * proxyRead runs an upstream read on behalf of the browser's request.
 *
 * The read is handed the browser request's abort signal, so a reader who gives
 * up while the API is still working (a Sales list scan, a Holder List page, a
 * trends aggregation) stops that work instead of leaving it running for nobody.
 *
 * A reader who gives up makes the read reject. Nobody is left to read the
 * answer, so that is returned quietly as an empty 499 rather than handed to
 * `fail`, which would dress it up as a 500 in the request log, or thrown, which
 * Next would log as an unhandled route error. It is keyed on the browser's own
 * signal, not on the error's name: an abort nobody asked for while the browser
 * is still waiting is a failure, and goes to `fail` as any other failure does.
 *
 * Without a `fail`, any other failure is thrown as it came, for the route's
 * caller to answer.
 */
export async function proxyRead(
  request: Request,
  read: (signal: AbortSignal) => Promise<Response>,
  fail?: (error: unknown) => Response,
): Promise<Response> {
  try {
    return await read(request.signal);
  } catch (error) {
    if (request.signal.aborted) {
      return readerGone();
    }
    if (!fail) {
      throw error;
    }
    return fail(error);
  }
}

/** readerGone is the empty 499 a route answers a reader who has left. */
function readerGone(): Response {
  return new Response(null, { status: CLIENT_CLOSED_REQUEST });
}
