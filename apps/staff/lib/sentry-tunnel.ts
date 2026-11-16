/**
 * The same-origin tunnel (ADR 0079): the browser SDK posts its envelopes here
 * instead of to Sentry's host, and this relays them.
 *
 * Whether to relay is the shared decision in @ticket-pos/sentry-rules: only an
 * envelope addressed to this app's own Sentry project, and only to the endpoint
 * built from this app's DSN, so the route is no open relay.
 *
 * The upstream request is built from nothing of the incoming one but its body:
 * no header is copied, so neither the browser's X-Forwarded-For, nor its cookies,
 * nor anything else naming the person reaches Sentry. Sentry sees this server's
 * address, never the client's.
 */
import { decideTunnel } from "@ticket-pos/sentry-rules";

/** How long a relay may wait on Sentry before the browser is answered 504. */
const TUNNEL_UPSTREAM_TIMEOUT_MS = 10_000;

/**
 * Sentry's answers the browser SDK acts on: its rate limits. Passed back so the
 * SDK backs off as it would talking to Sentry directly.
 */
const RELAYED_RESPONSE_HEADERS = ["x-sentry-rate-limits", "retry-after"];

export type Send = (url: string, init: RequestInit) => Promise<Response>;

/**
 * Answers one envelope: 204 without forwarding when no DSN is set (dormant;
 * the caller passes lib/sentry's readSentryConfig, so a blank one is none),
 * 403 when it is addressed to anyone else, otherwise Sentry's own status, or
 * 502/504 when Sentry cannot be reached.
 */
export async function relayEnvelope(envelope: Uint8Array<ArrayBuffer>, ownDsn: string | undefined, send: Send = fetch): Promise<Response> {
  if (!ownDsn) return new Response(null, { status: 204 });

  const decision = decideTunnel(envelope, ownDsn);
  if (!decision.forward) return new Response(null, { status: 403 });

  let upstream: Response;
  try {
    upstream = await send(decision.url, {
      method: "POST",
      headers: { "Content-Type": "application/x-sentry-envelope" },
      body: envelope,
      signal: AbortSignal.timeout(TUNNEL_UPSTREAM_TIMEOUT_MS),
    });
  } catch (error) {
    const timedOut = error instanceof Error && error.name === "TimeoutError";
    return new Response(null, { status: timedOut ? 504 : 502 });
  }

  const headers = new Headers();
  for (const name of RELAYED_RESPONSE_HEADERS) {
    const value = upstream.headers.get(name);
    if (value !== null) headers.set(name, value);
  }
  // The body is Sentry's, of no use to the browser; drain it so the connection
  // is released.
  await upstream.body?.cancel();
  return new Response(null, { status: upstream.status, headers });
}
