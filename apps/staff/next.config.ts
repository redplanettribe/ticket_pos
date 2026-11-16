import path from "node:path";

import { withSentryConfig } from "@sentry/nextjs/config";
import { sentryBuildOptions } from "@ticket-pos/sentry-rules";
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

// Sentry's build plugin (ADR 0079), with the options both apps share
// (sentryBuildOptions in @ticket-pos/sentry-rules). With no SENTRY_AUTH_TOKEN,
// as in CI and every local build, the build never talks to Sentry: no source
// maps are produced or uploaded, no release is created or injected (the release
// is SENTRY_RELEASE, read at runtime by lib/sentry.ts), and the plugin's own
// telemetry is off, so an unreachable Sentry neither fails nor slows it. With
// the token the deploy's image build hands it (#776), the browser and server
// source maps are uploaded to ticket-pos-staff and deleted, failing soft.
// Either way the SDK wraps server components, route handlers and middleware,
// which is what reports their errors.
export default withSentryConfig(withNextIntl(nextConfig), sentryBuildOptions("ticket-pos-staff", process.env));
