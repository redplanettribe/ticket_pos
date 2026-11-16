/**
 * The Storefront's way to the lazily loaded browser SDK (ADR 0079), with no
 * Sentry in it: this module is in the initial bundle, through the loader and
 * the error screens, and the SDK is not.
 *
 * Both the loader (components/sentry-loader.tsx, after hydration) and an error
 * screen (#774, possibly before the loader got there) ask for the SDK's chunk
 * through loadSentryBrowser, so it is requested once, and a screen can tell
 * whether it was requested at all.
 */

/** The name of the <meta> the root layout writes the browser's configuration into. */
export const SENTRY_CONFIG_META = "ticket-pos-sentry";

/**
 * The configuration the root layout wrote into this document, as written, or
 * null: none was written (no DSN), or the document is no longer the root
 * layout's (the root-level error screen replaced it).
 */
export function readPageSentryConfig(doc: Pick<Document, "querySelector">): string | null {
  return doc.querySelector(`meta[name="${SENTRY_CONFIG_META}"]`)?.getAttribute("content") ?? null;
}

type SentryBrowser = typeof import("./sentry-browser");

let chunk: Promise<SentryBrowser> | null = null;

/**
 * The SDK's chunk, requested on the first call. A request that failed (the
 * buyer went offline) is forgotten, so a later call tries again.
 */
export function loadSentryBrowser(): Promise<SentryBrowser> {
  chunk ??= import("./sentry-browser").catch((error: unknown) => {
    chunk = null;
    throw error;
  });
  return chunk;
}

/** Whether the SDK's chunk has been asked for on this page. */
export function sentryBrowserRequested(): boolean {
  return chunk !== null;
}
