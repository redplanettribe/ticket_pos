import path from "node:path";

import type { NextConfig } from "next";

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

export default nextConfig;
