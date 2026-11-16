import * as Sentry from "@sentry/nextjs";

import { SENTRY_CONFIG_META, SENTRY_TRACES_SAMPLE_RATE, SENTRY_TUNNEL_PATH, parseSentryConfig, sentryOptions } from "@/lib/sentry";

/**
 * Starts Sentry in the browser, before the app's own code runs. The Staff app
 * loads the SDK normally, with browser tracing on (the SDK's default when a
 * sample rate is set), and sends every envelope to its own origin's tunnel.
 *
 * The configuration is the server's, written by the root layout into a <meta>
 * (lib/sentry.ts says why it is not a build-time value). A page without it, the
 * dormant default, never initialises the SDK. The meta is in the document's
 * head, which is parsed before this script runs; the DOMContentLoaded fallback
 * only covers a parser that has not reached it yet.
 */
function readConfig() {
  return parseSentryConfig(document.querySelector(`meta[name="${SENTRY_CONFIG_META}"]`)?.getAttribute("content"));
}

function start(): boolean {
  const config = readConfig();
  if (!config) return false;
  Sentry.init({
    ...sentryOptions(config),
    tunnel: SENTRY_TUNNEL_PATH,
    tracesSampleRate: SENTRY_TRACES_SAMPLE_RATE,
  });
  return true;
}

if (!start() && document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", () => start(), { once: true });
}

export const onRouterTransitionStart = Sentry.captureRouterTransitionStart;
