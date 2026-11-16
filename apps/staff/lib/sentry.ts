/**
 * How the Staff app reports to Sentry (ADR 0079), as plain values the three
 * runtimes share: the browser (instrumentation-client.ts), the Node server and
 * middleware (instrumentation.ts).
 *
 * Dormant until a DSN is set: with no SENTRY_DSN, readSentryConfig (from
 * @ticket-pos/sentry-rules) is null and nothing calls `Sentry.init`, in any
 * runtime. The root layout writes the configuration into the page
 * (SENTRY_CONFIG_META) and the browser reads it back from there.
 */
import { createBeforeSend, scrubEvent, type SentryConfig } from "@ticket-pos/sentry-rules";

// Reading the configuration is the same rule in both apps, in the shared package.
export { parseSentryConfig, readSentryConfig, serializeSentryConfig, type SentryConfig } from "@ticket-pos/sentry-rules";

/**
 * The same-origin route browser events are posted to (app/api/monitoring).
 * Under /api/ so the middleware lets it through without a session: an error on
 * the sign-in page is worth reporting too.
 */
export const SENTRY_TUNNEL_PATH = "/api/monitoring";

/** The name of the <meta> the root layout writes the browser's configuration into. */
export const SENTRY_CONFIG_META = "ticket-pos-sentry";

/** One trace in ten, the ADR's default. A trace the browser started keeps its decision. */
export const SENTRY_TRACES_SAMPLE_RATE = 0.1;

/**
 * The before-send every event passes through: the shared one from
 * @ticket-pos/sentry-rules, never a copy. Both of its filters are off here.
 * The frame filter would drop every server-side event (no frame lives under
 * /_next/static/), and the Staff app is signed-in staff, not the public
 * Storefront's crawlers.
 */
export const beforeSend = createBeforeSend({ dropWithoutOwnFrame: false, dropBots: false });

/** Transactions are scrubbed, never filtered (see createBeforeSend). */
export const beforeSendTransaction = scrubEvent;

/**
 * The options every runtime initialises with. No personal data is collected by
 * default, and neither session replay nor profiling is enabled: neither
 * integration is listed, and the SDK adds neither by default.
 */
export function sentryOptions(config: SentryConfig) {
  return {
    dsn: config.dsn,
    environment: config.environment,
    release: config.release,
    sendDefaultPii: false,
    beforeSend,
    beforeSendTransaction,
  };
}

/**
 * The server's sampler. The tunnel's own requests are never traced: each one
 * is a browser event being relayed, and tracing it would spend a transaction on
 * every error. Everything else keeps an incoming decision (a trace the browser
 * started) or is sampled one in ten.
 */
export function serverTracesSampler(context: {
  name: string;
  inheritOrSampleWith: (fallbackSampleRate: number) => number;
}): number {
  if (context.name.includes(SENTRY_TUNNEL_PATH)) return 0;
  return context.inheritOrSampleWith(SENTRY_TRACES_SAMPLE_RATE);
}
