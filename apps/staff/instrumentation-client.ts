import * as Sentry from "@sentry/nextjs";

import { startBrowserSentry } from "@/lib/sentry-browser";

/**
 * Starts Sentry in the browser, before the app's own code runs. The Staff app
 * loads the SDK normally, with browser tracing on (the SDK's default when a
 * sample rate is set), and sends every envelope to its own origin's tunnel.
 *
 * The configuration is the server's, written by the root layout into a <meta>
 * (lib/sentry-browser.ts). The meta is in the document's head, which is parsed
 * before this script runs; the DOMContentLoaded fallback only covers a parser
 * that has not reached it yet.
 */
if (!startBrowserSentry() && document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", () => startBrowserSentry(), { once: true });
}

export const onRouterTransitionStart = Sentry.captureRouterTransitionStart;
