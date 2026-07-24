/**
 * Client IP derivation for BFF routes that call rate-limited API endpoints.
 *
 * This module is deliberately self-contained and app-agnostic: the Storefront
 * needs exactly this logic the moment it grows a Customer sign-in form (a
 * *public* OTP endpoint, where forging the source address is far more
 * attractive than it is on staff sign-in). Adopt it there by importing this
 * behaviour rather than re-deriving it; the header name and the hop arithmetic
 * below must agree with the Go API, which reads only this header.
 *
 * --- Why a distinct header, and why the API can trust it (ADR 0008) ---------
 *
 * The API used to take the leftmost entry of X-Forwarded-For and this route
 * used to forward the browser's own X-Forwarded-For verbatim. That is a hole:
 * X-Forwarded-For is client-supplied, Google's frontend appends to whatever the
 * caller sent rather than replacing it, and the Cloud Load Balancing docs state
 * that it does not verify any entry preceding the ones it adds. A browser could
 * therefore choose its own leftmost entry, rotate it per request, and never
 * exhaust the per-IP OTP allowance.
 *
 * So the API ignores forwarding headers entirely and reads CLIENT_IP_HEADER,
 * which only this BFF sets. That is safe for a reason that lives outside this
 * file: per ADR 0008 the API runs with unauthenticated invocation disabled and
 * accepts only callers presenting a Google-signed OIDC ID token with
 * roles/run.invoker. No browser can reach it. The header is trustworthy because
 * of who is allowed to send it, not because of what it contains — which is
 * precisely why forwarding the browser's copy would reintroduce the hole.
 *
 * The name is distinct from Authorization and X-Serverless-Authorization (see
 * ADR 0008 for that split) and from X-Forwarded-For, so nothing collides.
 */

/** Header the Go API reads the client IP from. Must match platform.ClientIPHeader. */
export const CLIENT_IP_HEADER = "X-BFF-Client-IP";

/**
 * Entries our own infrastructure appends to X-Forwarded-For *after* the client
 * IP. Reading from the right is what makes the value unforgeable: a caller can
 * only prepend, never displace the hops added downstream of it.
 *
 * Today the API and both frontends are plain Cloud Run services reached at
 * *.run.app with no load balancer in front (see terraform/), so Google's
 * frontend appends the connecting client's address as the last entry: 0 extra
 * hops. Google documents the composition for Cloud Load Balancing
 * (`<supplied>,<client-ip>,<lb-ip>`) but not for Cloud Run, so this is set by
 * deployment shape rather than assumed: putting an external Application Load
 * Balancer in front adds the load balancer's own address after the client IP,
 * which is TRUSTED_PROXY_HOPS=1 and needs no code change.
 */
const DEFAULT_TRUSTED_PROXY_HOPS = 0;

function trustedProxyHops(): number {
  const raw = process.env.TRUSTED_PROXY_HOPS;
  if (!raw) {
    return DEFAULT_TRUSTED_PROXY_HOPS;
  }
  const hops = Number.parseInt(raw, 10);
  return Number.isInteger(hops) && hops >= 0 ? hops : DEFAULT_TRUSTED_PROXY_HOPS;
}

/**
 * clientIp returns the genuine client IP for an inbound BFF request, or an
 * empty string when it cannot be established.
 *
 * Empty is the honest answer locally, where no proxy sets forwarding headers at
 * all, and when the chain is shorter than the hops we expect — a truncated
 * chain means something upstream changed, and guessing an entry from it would
 * be guessing at attacker-supplied data. The API then attributes the request to
 * its transport peer, which is the pre-existing local behaviour and, in
 * production, a shared bucket that tightens the limit rather than loosening it.
 */
export function clientIp(headers: Headers): string {
  const forwarded = headers.get("x-forwarded-for");
  if (!forwarded) {
    return "";
  }
  const chain = forwarded
    .split(",")
    .map((entry) => entry.trim())
    .filter(Boolean);

  const index = chain.length - 1 - trustedProxyHops();
  if (index < 0) {
    return "";
  }
  return chain[index];
}

/**
 * clientIpHeaders returns the headers to pass to the API so a rate-limited
 * endpoint counts the real caller. Empty when the IP is unknown, so the API
 * falls back to the transport peer rather than to an attacker-chosen value.
 */
export function clientIpHeaders(headers: Headers): Record<string, string> {
  const ip = clientIp(headers);
  return ip ? { [CLIENT_IP_HEADER]: ip } : {};
}
