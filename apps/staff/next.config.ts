import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  transpilePackages: ["@ticket-pos/ui"],
  distDir: process.env.NEXT_DIST_DIR ?? ".next",
};

export default nextConfig;
