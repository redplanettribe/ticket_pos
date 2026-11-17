import * as Sentry from "@sentry/nextjs";
import { createOnRequestError, readSentryConfig, tagRequestId } from "@ticket-pos/sentry-rules";
import type { Instrumentation } from "next";

import { sentryOptions, serverBeforeSend, serverTracesSampler } from "@/lib/sentry";

/**
 * Starts Sentry on the Node server and in middleware (the edge runtime), the
 * hook Next calls once per runtime before serving anything, as the Staff app
 * does. The options are lib/sentry's; with no SENTRY_DSN nothing is
 * initialised.
 *
 * `onRequestError` is Next's hook for an error thrown while rendering a server
 * component, in a route handler, a server action or middleware. It logs one
 * line with the request's id and the reference code the error screen shows,
 * the same for a page load and the page's for a client navigation, and
 * reports the error, which the event processor tags with both (#774,
 * @ticket-pos/sentry-rules' request-error.ts). Without an initialised SDK the
 * report has no client to send to and does nothing; the line is still written.
 * A not-found (`notFound()`) and the redirects Next throws are control flow,
 * not errors, and Next does not call it for them.
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
  // Every server event carries the id of its request and the reference code
  // an error screen shows.
  Sentry.addEventProcessor((event) => tagRequestId(event));
}

export const onRequestError = createOnRequestError({
  report: Sentry.captureRequestError,
  log: (line) => console.error(line),
}) satisfies Instrumentation.onRequestError;
