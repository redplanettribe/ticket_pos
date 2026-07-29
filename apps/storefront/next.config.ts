import path from "node:path";

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

export default withNextIntl(nextConfig);
