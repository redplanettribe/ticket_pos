/**
 * Starts Sentry in the browser: the Storefront's lazy chunk (ADR 0079). It is
 * imported only by components/sentry-loader.tsx, after hydration, and only on a
 * page the root layout rendered with a DSN, so neither the SDK nor anything here
 * is part of the initial bundle, and with no DSN this chunk is never requested.
 *
 * What it turns on:
 * - errors, through the shared before-send with both filters on;
 * - navigations after the first, traced one in ten, through Next's
 *   onRouterTransitionStart (lib/sentry-navigation.ts) and named by page
 *   (lib/navigation-route.ts).
 * What it gives up, by loading late: the page-load trace and the Web Vitals,
 * both measured from the first paint the SDK was not there for. Neither is
 * started, and nothing measures them later. No session replay, no profiling.
 *
 * Errors thrown before it loaded were kept by the inline handler
 * (lib/early-errors.ts) and are reported right after the SDK starts, in the
 * same task, so each is reported exactly once.
 */
import * as Sentry from "@sentry/nextjs";
import { parseSentryConfig } from "@ticket-pos/sentry-rules";

import { reportEarlyErrors } from "./early-errors.ts";
import { nameNavigation } from "./navigation-route.ts";
import {
  SENTRY_CONFIG_META,
  SENTRY_TRACES_SAMPLE_RATE,
  SENTRY_TUNNEL_PATH,
  beforeSendTransaction,
  browserBeforeSend,
  sentryOptions,
} from "./sentry.ts";
import { forwardRouterTransitionsTo } from "./sentry-navigation.ts";

/**
 * Starts the SDK from the configuration the root layout wrote into the page
 * and says whether it is running. Starting twice is a no-op; a page without the
 * configuration never starts it.
 */
export function startBrowserSentry(): boolean {
  if (Sentry.getClient()) return true;
  const config = parseSentryConfig(
    document.querySelector(`meta[name="${SENTRY_CONFIG_META}"]`)?.getAttribute("content"),
  );
  if (!config) return false;
  Sentry.init({
    ...sentryOptions(config, browserBeforeSend),
    beforeSendTransaction: (event) => beforeSendTransaction(nameNavigation(event)),
    tunnel: SENTRY_TUNNEL_PATH,
    tracesSampleRate: SENTRY_TRACES_SAMPLE_RATE,
    integrations: [
      Sentry.browserTracingIntegration({
        instrumentPageLoad: false,
        enableInp: false,
        enableLongTask: false,
        enableLongAnimationFrame: false,
        webVitals: { ignore: ["lcp", "cls", "inp"] },
      }),
    ],
  });
  forwardRouterTransitionsTo(Sentry.captureRouterTransitionStart);
  reportEarlyErrors(window, Sentry.captureException);
  return true;
}
