import path from "node:path";

import { withSentryConfig } from "@sentry/nextjs/config";
import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

// Wires i18n/request.ts (the default location) into the build, so server
// components can read the locale of the request they are rendering for.
//
// The plugin and nothing else: next-intl's routing half — the middleware, the
// `[locale]` segment and the navigation wrappers the Storefront uses — is
// deliberately absent (ADR 0041). The locale here is a property of the person,
// resolved from the session or the cookie, so no route path is a function of it
// and none of the paths in this app change.
const withNextIntl = createNextIntlPlugin();

const nextConfig: NextConfig = {
  // Standalone output ships a self-contained server plus only the traced
  // dependencies, keeping the deployed image ~150MB instead of ~1GB
  // (see ADR 0007 and the Storefront's next.config.ts).
  output: "standalone",
  // pnpm links workspace packages as symlinks into node_modules, so the real
  // files for @ticket-pos/ui live outside apps/staff. Next roots file tracing
  // at the app directory by default and would silently leave them out of
  // standalone output - which fails as MODULE_NOT_FOUND when the container
  // starts, not when it builds. Rooting tracing at the workspace root makes
  // Next follow those symlinks and copy the real files in, and puts the
  // standalone entrypoint at apps/staff/server.js.
  outputFileTracingRoot: path.join(__dirname, "../.."),
  transpilePackages: ["@ticket-pos/ui"],
  distDir: process.env.NEXT_DIST_DIR ?? ".next",
};

// Sentry's build plugin (ADR 0079), set so the build never talks to Sentry:
// no source maps are produced or uploaded (that is the deploy's job, #776), no
// release is created or injected (the release is SENTRY_RELEASE, read at
// runtime by lib/sentry.ts), and the plugin's own telemetry, on by default, is
// off. So a build with no Sentry token, as in CI, contacts nothing, and an
// unreachable Sentry neither fails nor slows it. What remains is the SDK's
// wrapping of server components, route handlers and middleware, which is what
// reports their errors.
export default withSentryConfig(withNextIntl(nextConfig), {
  telemetry: false,
  silent: true,
  sourcemaps: { disable: true },
  release: { create: false, finalize: false },
});
