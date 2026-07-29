import { defineConfig } from "@playwright/test";

// Smoke tests against the dev stack (`make dev`), whose Storefront is on 64300.
//
// The port is overridable because the dev stack is not the only build worth
// pointing this suite at: a branch build served on its own port is the only way
// to run these specs against code that is not what `make dev` currently has
// running, and hardcoding the address makes that impossible without editing a
// tracked file. The default is unchanged, so `pnpm test` after `make dev` still
// needs no environment at all.
export default defineConfig({
  testDir: "./tests",
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:64300",
  },
});
