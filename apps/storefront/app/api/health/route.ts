// Relative and with ".ts" written out, unlike the other routes' "@/lib/...":
// lib/deep-health.test.ts runs this route directly under the plain Node test
// runner, which resolves specifiers exactly. Next resolves it identically.
import { fetchBackendRaw } from "../../../lib/api.ts";
import { API_DEEP_HEALTH_PATH, checkDeepHealth } from "../../../lib/deep-health.ts";

/**
 * The uptime probe's target (#775, ADR 0079): 200 "ok" only when this app
 * reaches the API and the API reaches its database, otherwise 503
 * "unavailable", within a short bound, never cached and never traced. See
 * lib/deep-health.ts. Under /api/, which the middleware never sees, so no
 * locale redirect or request id cookie.
 *
 * The API is asked as every BFF call asks it: with this app's service
 * credential and no session.
 */
export const dynamic = "force-dynamic";

export async function GET(): Promise<Response> {
  return checkDeepHealth((signal) => fetchBackendRaw(API_DEEP_HEALTH_PATH, { method: "GET", signal }));
}
