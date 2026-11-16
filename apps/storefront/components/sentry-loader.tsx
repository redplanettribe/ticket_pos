"use client";

import { useEffect } from "react";

/**
 * Loads the browser SDK after hydration (ADR 0079): the buyer's first load is
 * the page and nothing else, and Sentry arrives when the browser is next idle.
 *
 * The root layout renders this only when a DSN is set, so with none it is never
 * mounted and the SDK's chunk is never requested. The import is the one place
 * the SDK enters the browser; the bundler splits it, and everything it pulls in,
 * into chunks of their own. A load that fails (the buyer went offline) is
 * dropped: the page does not depend on it.
 */
export function SentryLoader() {
  useEffect(() => {
    const load = () => {
      import("@/lib/sentry-browser").then(({ startBrowserSentry }) => startBrowserSentry()).catch(() => {});
    };
    if (typeof window.requestIdleCallback === "function") {
      const handle = window.requestIdleCallback(load, { timeout: 2000 });
      return () => window.cancelIdleCallback(handle);
    }
    const handle = window.setTimeout(load, 0);
    return () => window.clearTimeout(handle);
  }, []);
  return null;
}
