import * as Sentry from "@sentry/nextjs";

import { readSentryConfig, sentryOptions, serverTracesSampler } from "@/lib/sentry";

/**
 * Starts Sentry on the Node server and in middleware (the edge runtime), the
 * hook Next calls once per runtime before serving anything. The options are
 * lib/sentry's; with no SENTRY_DSN nothing is initialised.
 *
 * `onRequestError` is Next's hook for an error thrown while rendering a server
 * component, in a route handler, a server action or middleware. Without an
 * initialised SDK it has no client to send to and does nothing. A not-found
 * (`notFound()`) and the redirects Next throws are control flow, not errors,
 * and are not reported.
 */
export function register() {
  const config = readSentryConfig(process.env);
  if (!config) return;
  Sentry.init({
    ...sentryOptions(config),
    tracesSampler: serverTracesSampler,
    // This process's host name would otherwise name the Cloud Run instance.
    serverName: "ticket-pos-staff",
  });
}

export const onRequestError = Sentry.captureRequestError;
