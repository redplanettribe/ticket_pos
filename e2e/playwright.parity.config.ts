import { defineConfig, devices } from "@playwright/test";

// Smoke tests against the production-parity stack (docker-compose.prod.yml),
// which runs the shipped container images rather than dev servers. Kept in a
// separate config from playwright.config.ts so `pnpm test` stays runnable
// without Docker; run these with `make test-parity`.
export default defineConfig({
  testDir: "./parity",
  forbidOnly: !!process.env.CI,
  use: {
    trace: "on-first-retry",
  },
  // One project per app: they are separate images on separate host ports, so
  // each suite needs its own baseURL.
  projects: [
    {
      name: "storefront",
      testMatch: /storefront\.spec\.ts/,
      use: {
        ...devices["Desktop Chrome"],
        baseURL: process.env.STOREFRONT_URL ?? "http://localhost:64603",
      },
    },
    {
      name: "staff",
      testMatch: /staff\.spec\.ts/,
      use: {
        ...devices["Desktop Chrome"],
        baseURL: process.env.STAFF_URL ?? "http://localhost:64604",
      },
    },
  ],
});
