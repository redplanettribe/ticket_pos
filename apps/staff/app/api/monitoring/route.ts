import { relayEnvelope } from "@/lib/sentry-tunnel";

/**
 * The browser SDK's tunnel (ADR 0079, lib/sentry.ts's SENTRY_TUNNEL_PATH).
 * The relay and what it refuses are lib/sentry-tunnel's; this is the plumbing.
 * SENTRY_DSN is read per request: with none set, the route answers 204 and
 * forwards nothing.
 */
export async function POST(request: Request) {
  const envelope = new Uint8Array(await request.arrayBuffer());
  return relayEnvelope(envelope, process.env.SENTRY_DSN);
}
