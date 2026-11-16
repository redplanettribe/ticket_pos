/**
 * How a Next app reads its Sentry configuration (ADR 0079), the same rule in
 * both apps.
 *
 * Dormant until a DSN is set. With no SENTRY_DSN the configuration is null and
 * the app initialises nothing, in any runtime; that is the default locally, in
 * CI and in an unattended agent run, and in production until the operator sets
 * the DSN on the Cloud Run service (Terraform).
 *
 * The DSN is a runtime value on the service, not a build argument, so a browser
 * cannot have it inlined at build time. The app's root layout writes the
 * configuration into the page (serializeSentryConfig) and the browser reads it
 * back (parseSentryConfig).
 *
 * The release is the commit SHA, a build argument of the image that becomes
 * SENTRY_RELEASE in its environment; absent, events carry no release.
 */

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
  return normalizeSentryConfig(env.SENTRY_DSN, env.SENTRY_ENVIRONMENT, env.SENTRY_RELEASE);
}

/** The one rule for what counts as set: blank is absent, in every source. */
function normalizeSentryConfig(dsn: unknown, environment: unknown, release: unknown): SentryConfig | null {
  const trimmed = (value: unknown) => (typeof value === "string" ? value.trim() : "");
  if (!trimmed(dsn)) return null;
  const config: SentryConfig = { dsn: trimmed(dsn), environment: trimmed(environment) || "development" };
  if (trimmed(release)) config.release = trimmed(release);
  return config;
}

/** What a root layout writes into the page for the browser. */
export function serializeSentryConfig(config: SentryConfig): string {
  return JSON.stringify(config);
}

/**
 * Reads the configuration back in the browser. Anything missing or unreadable
 * is null, so a page without it never initialises the SDK.
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
  return normalizeSentryConfig(dsn, environment, release);
}
