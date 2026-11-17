/**
 * The Storefront's deep health check (#775, ADR 0079), the uptime probe's
 * target: whether this app can actually serve a buyer, which means it reaches
 * the API and the API reaches its database.
 *
 * It answers 200 only when the API's own deep health route answered 200, and a
 * 503 otherwise: the API unreachable, the API reporting its database
 * unreachable, any other answer, or no answer within DEEP_HEALTH_TIMEOUT_MS.
 * Either way the body is a constant word and carries no data, and nothing is
 * cached.
 *
 * It never throws. A thrown error in a route handler is an error event, and a
 * failing probe must not be one: an outage raises the uptime alert, not one
 * issue per probe. The failure is a log line instead.
 *
 * No Sentry import here: lib/sentry.ts reads DEEP_HEALTH_PATH for its sampler,
 * which never traces this route.
 */

/** The Storefront route the uptime probe asks (app/api/health). */
export const DEEP_HEALTH_PATH = "/api/health";

/** The API's deep health route, which pings its database. */
export const API_DEEP_HEALTH_PATH = "/api/v1/public/health";

/**
 * How long the check waits for the API, its service credential included.
 * The API bounds its database check at 2 seconds (platform.DeepHealthTimeout);
 * this leaves room for that and the hop. The uptime monitor's own timeout must
 * be longer than this, so the probe reads our 503 rather than timing out.
 */
export const DEEP_HEALTH_TIMEOUT_MS = 4_000;

/** Asks the API's deep health route, giving up when signal aborts. */
export type AskAPI = (signal: AbortSignal) => Promise<Response>;

const NO_STORE = { "Cache-Control": "no-store", "Content-Type": "text/plain; charset=utf-8" };

/**
 * checkDeepHealth asks the API once and answers the probe. The answer is
 * bounded by timeoutMs even when ask ignores the signal: whatever it was doing
 * is aborted and left behind, and the timer is cleared either way.
 */
export async function checkDeepHealth(ask: AskAPI, timeoutMs: number = DEEP_HEALTH_TIMEOUT_MS): Promise<Response> {
  const controller = new AbortController();
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timedOut = new Promise<"timeout">((resolve) => {
    timer = setTimeout(() => resolve("timeout"), timeoutMs);
  });
  let failure: string;
  try {
    const answered = await Promise.race([ask(controller.signal), timedOut]);
    if (answered === "timeout") {
      failure = `no answer within ${timeoutMs} ms`;
    } else {
      await answered.body?.cancel().catch(() => {});
      if (answered.status === 200) {
        return new Response("ok", { status: 200, headers: NO_STORE });
      }
      failure = `the API answered ${answered.status}`;
    }
  } catch (error) {
    failure = `the API was unreachable: ${error instanceof Error ? error.name : "error"}`;
  } finally {
    clearTimeout(timer);
    controller.abort();
  }
  console.warn(`deep health: unavailable, ${failure}`);
  return new Response("unavailable", { status: 503, headers: NO_STORE });
}
