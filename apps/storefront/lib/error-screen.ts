/**
 * The Storefront's part of an error screen (#774). Catching, reporting once and
 * the reference code are shared with the Staff app and live in
 * @ticket-pos/sentry-rules; what is left here is the Storefront's own: the SDK
 * loads late, so a screen may have to load it to report, and a page's language
 * is in its address.
 */

import type { CaptureCaughtError } from "@ticket-pos/sentry-rules";

import { DEFAULT_APP_LOCALE, localePrefixOf, type AppLocale } from "./locale.ts";

type SentryChunk = {
  captureCaughtError: (error: unknown, requestId: string | null, written: string | null) => void;
};

/**
 * How an error screen reports what it caught, given what it knows of Sentry on
 * this page:
 *
 * - `written`: the configuration the root layout wrote (lib/sentry-lazy.ts),
 *   read while it was still in the document, or null;
 * - `requested`: whether the SDK's chunk was already asked for (the loader ran);
 * - `load`: the chunk.
 *
 * With neither, the page was rendered with no DSN: nothing is requested and
 * nothing is sent. Otherwise the chunk is loaded, if the loader has not yet,
 * and reports the error; this is how a crash before the SDK loaded is still
 * reported, once. A chunk that cannot load, or an SDK that throws, is dropped:
 * the screen must render whatever happens to the report.
 */
export function captureFromScreen(deps: {
  written: string | null;
  requested: () => boolean;
  load: () => Promise<SentryChunk>;
}): CaptureCaughtError {
  return (error, requestId) => {
    if (deps.written === null && !deps.requested()) return;
    deps
      .load()
      .then((sentry) => sentry.captureCaughtError(error, requestId, deps.written))
      .catch(() => {});
  };
}

/**
 * The language of the root-level screen, which renders in place of the root
 * layout and so without next-intl's provider: the locale the address names, the
 * same token the root layout would have rendered under (i18n/request.ts). Every
 * page lives under one, so the fallback is only for an address that names none.
 */
export function rootErrorScreenLocale(pathname: string | null): AppLocale {
  return localePrefixOf(pathname ?? "") ?? DEFAULT_APP_LOCALE;
}
