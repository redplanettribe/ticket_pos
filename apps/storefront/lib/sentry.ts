/**
 * How the Storefront reports to Sentry (ADR 0079), as plain values its three
 * runtimes share: the Node server and middleware (instrumentation.ts), and the
 * browser, which loads the SDK only after hydration (lib/sentry-browser.ts,
 * started by components/sentry-loader.tsx).
 *
 * Dormant until a DSN is set: with no SENTRY_DSN, readSentryConfig (from
 * @ticket-pos/sentry-rules, which both apps read the configuration with) is null and nothing calls `Sentry.init`, in any
 * runtime; the root layout then writes neither the browser's configuration nor
 * the early-error handler into the page, and renders no loader, so no Sentry
 * chunk is ever requested.
 */
import { createBeforeSend, scrubEvent, type SentryConfig } from "@ticket-pos/sentry-rules";

/**
 * The same-origin route browser events are posted to (app/api/monitoring).
 * Under /api/, which the middleware never sees.
 */
export const SENTRY_TUNNEL_PATH = "/api/monitoring";

/** The name of the <meta> the root layout writes the browser's configuration into. */
export const SENTRY_CONFIG_META = "ticket-pos-sentry";

/** One trace in ten, the ADR's default. A trace the browser started keeps its decision. */
export const SENTRY_TRACES_SAMPLE_RATE = 0.1;

/** What a runtime hands the SDK as beforeSend. */
export type BeforeSend = ReturnType<typeof createBeforeSend>;

/**
 * The server's and middleware's before-send: the shared one, scrubbing every
 * event and dropping those a known bot caused. The frame filter is off: no
 * server frame lives under /_next/static/, so it would drop them all.
 */
export const serverBeforeSend: BeforeSend = createBeforeSend({ dropWithoutOwnFrame: false, dropBots: true });

/**
 * The browser's before-send: the shared one with both filters on (ADR 0079).
 * An exception with no frame from our own bundles (a browser extension, a
 * third-party script, a cross-origin "Script error.") is not ours to fix, and a
 * crawler's half-built browser is not a buyer.
 */
export const browserBeforeSend: BeforeSend = createBeforeSend({ dropWithoutOwnFrame: true, dropBots: true });

/** Transactions are scrubbed, never filtered (see createBeforeSend). */
export const beforeSendTransaction = scrubEvent;

/**
 * The options every runtime initialises with, given its before-send. No
 * personal data is collected by default, and neither session replay nor
 * profiling is enabled: neither integration is listed, and the SDK adds neither
 * by default.
 *
 * What the SDK collects on its own is cut to what a filter needs: no cookies,
 * bodies, query strings, user details or frame variables, and of the request
 * headers only the User-Agent, which the bot filter reads. The SDK's defaults
 * would otherwise put every request header, the client's X-Forwarded-For and
 * Referer included, on each server span.
 *
 * Traces are sent whole, as transactions ("static"), not streamed span by span,
 * the SDK 11 default. Streamed spans skip beforeSendTransaction, so with the
 * default the shared scrubber would never see a span's URL or name; the SDK
 * warns as much at start. Static also keeps a navigation named by its path,
 * which lib/navigation-route.ts turns into its page, where streaming names
 * every one "Navigation".
 */
export function sentryOptions(config: SentryConfig, beforeSend: BeforeSend) {
  return {
    dsn: config.dsn,
    environment: config.environment,
    release: config.release,
    sendDefaultPii: false,
    beforeSend,
    beforeSendTransaction,
    traceLifecycle: "static" as const,
    dataCollection: {
      userInfo: false,
      cookies: false,
      httpHeaders: { request: { allow: ["user-agent"] }, response: false },
      httpBodies: [],
      urlQueryParams: false,
      stackFrameVariables: false,
    },
  };
}

/**
 * The server's sampler. The tunnel's own requests are never traced: each one
 * is a browser event being relayed, and tracing it would spend a transaction on
 * every error. Everything else keeps an incoming decision (a navigation the
 * browser traced) or is sampled one in ten.
 */
export function serverTracesSampler(context: {
  name: string;
  inheritOrSampleWith: (fallbackSampleRate: number) => number;
}): number {
  if (context.name.includes(SENTRY_TUNNEL_PATH)) return 0;
  return context.inheritOrSampleWith(SENTRY_TRACES_SAMPLE_RATE);
}
