import * as Sentry from "@sentry/nextjs";

import { SENTRY_CONFIG_META, SENTRY_TRACES_SAMPLE_RATE, SENTRY_TUNNEL_PATH, parseSentryConfig, sentryOptions } from "./sentry.ts";

/**
 * Starts Sentry in the browser from the configuration the root layout writes
 * into a <meta> (lib/sentry.ts says why it is not a build-time value), and says
 * whether it is running. Starting twice is a no-op. A page without the meta, the
 * dormant default, never starts it.
 *
 * Called by instrumentation-client.ts before the app's own code runs, and again
 * by an error screen before it reports (#772): when the server failed to render
 * a page's shell, Next serves a bare error document with no root layout and so
 * no meta, and the root layout, meta included, only reaches the DOM when the
 * browser renders the page itself.
 */
export function startBrowserSentry(): boolean {
  if (Sentry.getClient()) return true;
  const config = parseSentryConfig(
    document.querySelector(`meta[name="${SENTRY_CONFIG_META}"]`)?.getAttribute("content"),
  );
  if (!config) return false;
  Sentry.init({
    ...sentryOptions(config),
    tunnel: SENTRY_TUNNEL_PATH,
    tracesSampleRate: SENTRY_TRACES_SAMPLE_RATE,
  });
  return true;
}
