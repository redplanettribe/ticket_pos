import * as Sentry from "@sentry/nextjs";

import { readSentryConfig, sentryOptions, serverBeforeSend, serverTracesSampler } from "@/lib/sentry";

/**
 * Starts Sentry on the Node server and in middleware (the edge runtime), the
 * hook Next calls once per runtime before serving anything, as the Staff app
 * does. The options are lib/sentry's; with no SENTRY_DSN nothing is
 * initialised.
 *
 * `onRequestError` is Next's hook for an error thrown while rendering a server
 * component, in a route handler, a server action or middleware; it reports the
 * error. Without an initialised SDK the report has no client to send to and
 * does nothing. A not-found (`notFound()`) and the redirects Next throws are
 * control flow, not errors, and Next does not call it for them.
 */
export function register() {
  const config = readSentryConfig(process.env);
  if (!config) return;
  Sentry.init({
    ...sentryOptions(config, serverBeforeSend),
    tracesSampler: serverTracesSampler,
    // This process's host name would otherwise name the Cloud Run instance.
    serverName: "ticket-pos-storefront",
  });
}

export const onRequestError = Sentry.captureRequestError;
