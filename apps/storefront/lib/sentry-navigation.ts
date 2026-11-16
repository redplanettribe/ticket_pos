/**
 * Next's onRouterTransitionStart hook, forwarded to the SDK once it has loaded.
 *
 * Since Next 15.3 the SDK learns of an App Router navigation only through this
 * hook, which Next reads from instrumentation-client.ts, part of the initial
 * bundle. The Storefront keeps the SDK out of that bundle, so the hook there
 * is this forwarder, which imports nothing; lib/sentry-browser.ts hands it the
 * SDK's handler once the SDK has started. Before that, or with no DSN, a
 * navigation is not traced, and nothing else happens.
 */

type RouterTransitionStart = (href: string, navigationType: "push" | "replace" | "traverse") => void;

let forward: RouterTransitionStart | null = null;

/** Called by the lazily loaded SDK with its own handler. */
export function forwardRouterTransitionsTo(handler: RouterTransitionStart): void {
  forward = handler;
}

/** What instrumentation-client.ts exports as onRouterTransitionStart. */
export function routerTransitionStart(href: string, navigationType: "push" | "replace" | "traverse"): void {
  forward?.(href, navigationType);
}
