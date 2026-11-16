import path from "node:path";

import { withSentryConfig } from "@sentry/nextjs/config";
import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

// Wires i18n/request.ts (the default location) into the build, so server
// components can read the locale of the request they are rendering for.
const withNextIntl = createNextIntlPlugin();

const nextConfig: NextConfig = {
  // Standalone output ships a self-contained server plus only the traced
  // dependencies, which is what keeps the deployed image ~150MB instead of
  // ~1GB. Cold start is user-visible on the Storefront (see ADR 0007).
  output: "standalone",
  // pnpm links workspace packages as symlinks into node_modules, so the real
  // files for @ticket-pos/ui live outside apps/storefront. Next roots file
  // tracing at the app directory by default and would silently leave them out
  // of standalone output - which fails as MODULE_NOT_FOUND when the container
  // starts, not when it builds. Rooting tracing at the workspace root makes
  // Next follow those symlinks and copy the real files in.
  outputFileTracingRoot: path.join(__dirname, "../.."),
  transpilePackages: ["@ticket-pos/ui"],
  distDir: process.env.NEXT_DIST_DIR ?? ".next",
};

// Sentry's build plugin (ADR 0079), set as the Staff app's is, so the build
// never talks to Sentry: no source maps are produced or uploaded (that is the
// deploy's job, #776), no release is created or injected (the release is
// SENTRY_RELEASE, read at runtime by lib/sentry.ts), and the plugin's own
// telemetry, on by default, is off. What remains is the SDK's wrapping of
// server components, route handlers and middleware, which reports their errors.
//
// One more, for the Storefront's first load: the plugin injects a manifest of
// every route into instrumentation-client.ts, part of the initial bundle, so the
// SDK can name navigations. That is off; the lazily loaded SDK names them itself
// (lib/navigation-route.ts).
export default withSentryConfig(withNextIntl(nextConfig), {
  telemetry: false,
  silent: true,
  sourcemaps: { disable: true },
  release: { create: false, finalize: false },
  routeManifestInjection: false,
});
