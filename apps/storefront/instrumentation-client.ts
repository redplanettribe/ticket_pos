import { routerTransitionStart } from "@/lib/sentry-navigation";

/**
 * Part of the Storefront's initial bundle, so it holds no Sentry: the SDK loads
 * after hydration (components/sentry-loader.tsx). Next reads this file only for
 * the navigation hook, which forwards to the SDK once it has loaded.
 */
export const onRouterTransitionStart = routerTransitionStart;
