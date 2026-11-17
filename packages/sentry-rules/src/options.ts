/**
 * The options both Next apps initialise the SDK with in every runtime (the
 * browser, the Node server, middleware), so the two cannot drift (ADR 0079,
 * #781). Each app adds only its genuine differences: its before-send filters,
 * its samplers, the tunnel and its server name.
 *
 * No personal data is collected by default, and neither session replay nor
 * profiling is enabled: neither integration is listed, and the SDK adds neither
 * by default.
 *
 * sendDefaultPii alone is not enough in SDK 11: its data-collection defaults do
 * not read it. What the SDK collects on its own is cut to what a filter needs:
 * no cookies, bodies, query strings, user details or frame variables, and of
 * the request headers only the User-Agent, which the Storefront's bot filter
 * reads. The defaults would otherwise put every request header, the client's
 * X-Forwarded-For, Referer and Cookie included, on each server span.
 *
 * Traces are sent whole, as transactions ("static"), not streamed span by span,
 * the SDK 11 default. Streamed spans skip beforeSendTransaction, so with the
 * default the shared scrubber would never see a span's URL or name; the SDK
 * warns as much at start.
 */
import type { SentryConfig } from "./config.ts";
import { scrubEvent } from "./scrub.ts";

/** The request headers the SDK may attach: the User-Agent, for the bot filter. */
export const SENTRY_ALLOWED_REQUEST_HEADERS: readonly string[] = ["user-agent"];

/** Every runtime's options, given the app's before-send. Transactions are scrubbed, never filtered. */
export function sentryOptions<BeforeSend>(config: SentryConfig, beforeSend: BeforeSend) {
  return {
    dsn: config.dsn,
    environment: config.environment,
    release: config.release,
    sendDefaultPii: false,
    beforeSend,
    beforeSendTransaction: scrubEvent,
    traceLifecycle: "static" as const,
    dataCollection: {
      userInfo: false,
      cookies: false,
      httpHeaders: { request: { allow: [...SENTRY_ALLOWED_REQUEST_HEADERS] }, response: false },
      httpBodies: [],
      urlQueryParams: false,
      stackFrameVariables: false,
    },
  };
}
