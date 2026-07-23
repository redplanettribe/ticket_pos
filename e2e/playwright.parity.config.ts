import { defineConfig, devices } from "@playwright/test";

// Smoke tests against the production-parity stack (docker-compose.prod.yml),
// which runs the shipped container images rather than dev servers. Kept in a
// separate config from playwright.config.ts so `pnpm test` stays runnable
// without Docker; run these with `make test-parity`.
export default defineConfig({
  testDir: "./parity",
  forbidOnly: !!process.env.CI,
  use: {
    baseURL: process.env.STOREFRONT_URL ?? "http://localhost:64603",
    trace: "on-first-retry",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
