/**
 * How the Staff app reports to Sentry (ADR 0079), as plain values the three
 * runtimes share: the browser (instrumentation-client.ts), the Node server and
 * middleware (instrumentation.ts).
 *
 * Dormant until a DSN is set. With no SENTRY_DSN the configuration is null and
 * nothing calls `Sentry.init`, in any runtime; that is the default locally, in
 * CI and in an unattended agent run, and in production until the operator sets
 * the DSN on the Cloud Run service (Terraform).
 *
 * The DSN is a runtime value on the service, not a build argument, so the
 * browser cannot have it inlined at build time. The root layout writes the
 * configuration into the page (SENTRY_CONFIG_META) and the client
 * instrumentation reads it back from there.
 *
 * The release is the commit SHA, a build argument of the image that becomes
 * SENTRY_RELEASE in its environment; absent, events carry no release.
 */
import { createBeforeSend, scrubEvent } from "@ticket-pos/sentry-rules";

/**
 * The same-origin route browser events are posted to (app/api/monitoring).
 * Under /api/ so the middleware lets it through without a session: an error on
 * the sign-in page is worth reporting too.
 */
export const SENTRY_TUNNEL_PATH = "/api/monitoring";

/** The name of the <meta> the root layout writes the browser's configuration into. */
export const SENTRY_CONFIG_META = "ticket-pos-sentry";

/** One trace in ten, the ADR's default. A trace the browser started keeps its decision. */
export const SENTRY_TRACES_SAMPLE_RATE = 0.1;

export interface SentryConfig {
  dsn: string;
  environment: string;
  release?: string;
}

/**
 * Reads the configuration from the environment: SENTRY_DSN, SENTRY_ENVIRONMENT
 * (Terraform sets "production"; a developer who opts in locally reports as
 * "development"), SENTRY_RELEASE. Null without a DSN.
 */
export function readSentryConfig(env: Record<string, string | undefined>): SentryConfig | null {
  const dsn = env.SENTRY_DSN?.trim();
  if (!dsn) return null;
  const environment = env.SENTRY_ENVIRONMENT?.trim() || "development";
  const release = env.SENTRY_RELEASE?.trim() || undefined;
  return release ? { dsn, environment, release } : { dsn, environment };
}

/** What the root layout writes into SENTRY_CONFIG_META. */
export function serializeSentryConfig(config: SentryConfig): string {
  return JSON.stringify(config);
}

/**
 * Reads the configuration back in the browser. Anything missing or unreadable
 * is null, so a page without the meta never initialises the SDK.
 */
export function parseSentryConfig(content: string | null | undefined): SentryConfig | null {
  if (!content) return null;
  let value: unknown;
  try {
    value = JSON.parse(content);
  } catch {
    return null;
  }
  if (value === null || typeof value !== "object") return null;
  const { dsn, environment, release } = value as Record<string, unknown>;
  if (typeof dsn !== "string" || dsn.trim() === "") return null;
  return readSentryConfig({
    SENTRY_DSN: dsn,
    SENTRY_ENVIRONMENT: typeof environment === "string" ? environment : undefined,
    SENTRY_RELEASE: typeof release === "string" ? release : undefined,
  });
}

/**
 * The before-send every event passes through: the shared one from
 * @ticket-pos/sentry-rules, never a copy. Both of its filters are off here.
 * The frame filter would drop every server-side event (no frame lives under
 * /_next/static/), and the Staff app is signed-in staff, not the public
 * Storefront's crawlers.
 */
export const beforeSend = createBeforeSend({ dropWithoutOwnFrame: false, dropBots: false });

/** Transactions are scrubbed, never filtered (see createBeforeSend). */
export const beforeSendTransaction = scrubEvent;

/**
 * The options every runtime initialises with. No personal data is collected by
 * default, and neither session replay nor profiling is enabled: neither
 * integration is listed, and the SDK adds neither by default.
 */
export function sentryOptions(config: SentryConfig) {
  return {
    dsn: config.dsn,
    environment: config.environment,
    release: config.release,
    sendDefaultPii: false,
    beforeSend,
    beforeSendTransaction,
  };
}

/**
 * The server's sampler. The tunnel's own requests are never traced: each one
 * is a browser event being relayed, and tracing it would spend a transaction on
 * every error. Everything else keeps an incoming decision (a trace the browser
 * started) or is sampled one in ten.
 */
export function serverTracesSampler(context: {
  name: string;
  inheritOrSampleWith: (fallbackSampleRate: number) => number;
}): number {
  if (context.name.includes(SENTRY_TUNNEL_PATH)) return 0;
  return context.inheritOrSampleWith(SENTRY_TRACES_SAMPLE_RATE);
}
