import { readSentryConfig, relayEnvelope } from "@ticket-pos/sentry-rules";

/**
 * The browser SDK's tunnel (ADR 0079, lib/sentry.ts's SENTRY_TUNNEL_PATH), so
 * a buyer's ad blocker does not drop their errors and Sentry never sees their
 * address. The relay and what it refuses are @ticket-pos/sentry-rules'
 * relayEnvelope, the Staff app's too; this is the plumbing. SENTRY_DSN is read
 * per request: with none set, the route answers 204 and forwards nothing.
 */
export async function POST(request: Request) {
  const envelope = new Uint8Array(await request.arrayBuffer());
  return relayEnvelope(envelope, readSentryConfig(process.env)?.dsn);
}
