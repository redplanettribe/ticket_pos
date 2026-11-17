/**
 * Next's onRouterTransitionStart hook, forwarded to the SDK once it has loaded.
 *
 * Next calls this hook for every App Router navigation: a <Link> click and
 * router.push, router.replace, and back or forward. Next reads it from
 * instrumentation-client.ts, part of the initial bundle. The Storefront keeps
 * the SDK out of that bundle, so the hook there is this forwarder, which
 * imports nothing; lib/sentry-browser.ts hands it the handler that starts each
 * navigation's trace once the SDK has started. Before that, or with no DSN, a
 * navigation is not traced, and nothing else happens.
 */

type RouterTransitionStart = (href: string, navigationType: "push" | "replace" | "traverse") => void;

let forward: RouterTransitionStart | null = null;

/** Called by the lazily loaded SDK with its own handler. */
export function forwardRouterTransitionsTo(handler: RouterTransitionStart): void {
  forward = handler;
}

/** What instrumentation-client.ts exports as onRouterTransitionStart. */
export const routerTransitionStart: RouterTransitionStart = (href, navigationType) => {
  forward?.(href, navigationType);
};
