/**
 * Starts Sentry in the browser: the Storefront's lazy chunk (ADR 0079). It is
 * imported only through lib/sentry-lazy.ts: by components/sentry-loader.tsx,
 * after hydration, or by an error screen that has something to report, and
 * only on a page the root layout rendered with a DSN, so neither the SDK nor anything here
 * is part of the initial bundle, and with no DSN this chunk is never requested.
 *
 * What it turns on:
 * - errors, through the shared before-send with both filters on;
 * - navigations after the first, traced one in ten, through Next's
 *   onRouterTransitionStart (lib/sentry-navigation.ts) and named by page
 *   (lib/navigation-route.ts), each a trace of its own (startNavigation).
 * What it gives up, by loading late: the page-load trace and the Web Vitals,
 * both measured from the first paint the SDK was not there for. Neither is
 * started, and nothing measures them later. No session replay, no profiling.
 *
 * Errors thrown before it loaded were kept by the inline handler
 * (lib/early-errors.ts) and are reported right after the SDK starts, in the
 * same task, so each is reported exactly once. An error an error screen caught
 * never reaches that handler (React hands it to the boundary, not to the
 * window), so the screen reports it through captureCaughtError, loading this
 * chunk itself if the loader has not yet (#774).
 */
import * as Sentry from "@sentry/nextjs";
import { parseSentryConfig } from "@ticket-pos/sentry-rules";

import { reportEarlyErrors } from "./early-errors.ts";
import { nameNavigation } from "./navigation-route.ts";
import {
  SENTRY_TRACES_SAMPLE_RATE,
  SENTRY_TUNNEL_PATH,
  beforeSendTransaction,
  browserBeforeSend,
  sentryOptions,
} from "./sentry.ts";
import { readPageSentryConfig } from "./sentry-lazy.ts";
import { forwardRouterTransitionsTo, type NavigationType } from "./sentry-navigation.ts";

/**
 * Starts the SDK from the configuration the root layout wrote into the page
 * and says whether it is running. Starting twice is a no-op; a page without the
 * configuration never starts it.
 *
 * `writtenConfig` is that configuration as read earlier, for the root-level error
 * screen: by the time it reports, its own document has replaced the root
 * layout's, meta included.
 */
export function startBrowserSentry(writtenConfig: string | null = readPageSentryConfig(document)): boolean {
  if (Sentry.getClient()) return true;
  const config = parseSentryConfig(writtenConfig);
  if (!config) return false;
  Sentry.init({
    ...sentryOptions(config, browserBeforeSend),
    beforeSendTransaction: (event) => beforeSendTransaction(nameNavigation(event)),
    tunnel: SENTRY_TUNNEL_PATH,
    tracesSampleRate: SENTRY_TRACES_SAMPLE_RATE,
    integrations: [
      Sentry.browserTracingIntegration({
        instrumentPageLoad: false,
        // The SDK's own Next navigation tracing is off: startNavigation, below,
        // starts every navigation instead (#792).
        instrumentNavigation: false,
        enableInp: false,
        enableLongTask: false,
        enableLongAnimationFrame: false,
        webVitals: { ignore: ["lcp", "cls", "inp"] },
      }),
    ],
  });
  forwardRouterTransitionsTo(startNavigation);
  reportEarlyErrors(window, Sentry.captureException);
  return true;
}

/**
 * Starts the trace of one client navigation, from Next's
 * onRouterTransitionStart: a new trace with its own one in ten decision, which
 * the requests the navigation makes carry to the server, for every navigation
 * Next makes, a <Link> click and router.push ("push"), router.replace
 * ("replace") and back or forward ("traverse", from Next's own popstate
 * listener). The SDK ends the navigation before this one, if still running.
 *
 * Not the SDK's captureRouterTransitionStart (#792): its Next navigation
 * tracing also listens to popstate itself, so that back and forward each
 * started two navigations, and remembered the second as one its router patch
 * started. The next navigation Next reported was taken for that one's own
 * report and only renamed it, ended or not: it started no trace of its own,
 * and its requests carried the trace and the sampling decision of the
 * navigation back. lib/navigation-tracing.test.ts drives the real SDK through
 * both ways.
 *
 * Named by its path, as the SDK named it; beforeSendTransaction names it by
 * page (lib/navigation-route.ts).
 */
function startNavigation(href: string, navigationType: NavigationType): void {
  const client = Sentry.getClient();
  if (!client) return;
  const url = new URL(href, window.location.href);
  Sentry.startBrowserTracingNavigationSpan(
    client,
    {
      name: url.pathname,
      attributes: {
        [Sentry.SEMANTIC_ATTRIBUTE_SENTRY_OP]: "navigation",
        [Sentry.SEMANTIC_ATTRIBUTE_SENTRY_ORIGIN]: "auto.navigation.storefront.router_transition",
        "sentry.segment.name.source": "url",
        "navigation.type": `router.${navigationType}`,
      },
    },
    { url: url.href },
  );
}

/**
 * Reports an error an error screen caught (#774), tagged with the reference
 * code the screen shows, starting the SDK first if the loader has not. With no
 * configuration, nothing starts and nothing is sent. Whether to report at all
 * (once, not a server error) is reportCaughtError's, before this.
 */
export function captureCaughtError(error: unknown, requestId: string | null, writtenConfig: string | null): void {
  if (!startBrowserSentry(writtenConfig ?? readPageSentryConfig(document))) return;
  Sentry.captureException(error, requestId ? { tags: { request_id: requestId } } : undefined);
}
