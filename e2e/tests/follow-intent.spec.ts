import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";

import { test, expect } from "@playwright/test";

// The Follow intent through sign-in (#219, parent #215), against the dev stack
// (`make dev`).
//
// ONE journey, and it earns its place for one reason: this round trip exists
// only in the browser. An anonymous visitor presses Follow, is taken through the
// passcode form, and comes back to the page they started on with the thing
// Followed — and every hop of that is a redirect, a cookie or a query parameter
// that no API test can see. What survives the trip is asserted here; everything
// about what a Follow IS, what an intent may say, and whose session it may be
// written against lives in backend/integration/customer_follow_intent_test.go
// and is deliberately not restated (docs/testing.md, E2E boundary).
//
// Data: the dev-seed migrations (backend/migrations/002) provide the
// Organization. Nothing here needs an Event, a Ticket Type or a Payment.

const LOCALE = "en";
const ORGANIZATION_PATH = `/${LOCALE}/demo-venue`;
const ORGANIZATION_NAME = "Demo Venue";

// The intent as it travels: the same string the API validates. Asserting on it
// by name is the point of the spec — it is the one thing that has to survive.
const INTENT = "organization:demo-venue";

/**
 * The dev stack's compose file, found by walking up from wherever this suite was
 * invoked — `pnpm test` from e2e/, `pnpm --filter` from the root, either works.
 *
 * Naming the file rather than relying on the ambient compose project matters:
 * `docker compose` keys a project on its directory, so a suite run from a git
 * worktree would otherwise address a project that does not exist and read no
 * logs at all.
 */
function findComposeFile(): string | null {
  let directory = process.cwd();
  for (let depth = 0; depth < 6; depth += 1) {
    const candidate = path.join(directory, "docker-compose.yml");
    if (existsSync(candidate)) return candidate;
    const parent = path.dirname(directory);
    if (parent === directory) break;
    directory = parent;
  }
  return null;
}

/**
 * The API's recent log, from wherever this run's API is writing it.
 *
 * The compose stack is the ordinary case. `E2E_API_LOG_FILE` is the companion to
 * PLAYWRIGHT_BASE_URL and exists for the same case the README already describes:
 * a branch build running on its own ports, whose API is a plain process rather
 * than a container. Without it that configuration could not run this spec at
 * all, since a passcode exists nowhere but in the log of the API that issued it.
 */
function readApiLog(): string | null {
  const file = process.env.E2E_API_LOG_FILE?.trim();
  if (file) {
    try {
      return readFileSync(file, "utf8");
    } catch {
      return null;
    }
  }

  const composeFile = findComposeFile();
  if (!composeFile) return null;
  try {
    return execFileSync(
      "docker",
      ["compose", "-f", composeFile, "logs", "--no-log-prefix", "--since", "5m", "backend"],
      { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
    );
  } catch {
    return null;
  }
}

/**
 * The passcode, read out of the API's own log.
 *
 * `make dev` runs on the logging email sender (no RESEND_API_KEY), which writes
 * one line per passcode: {"msg":"otp sent","email":…,"code":…}. That log is the
 * only place a code exists in a dev stack — the database stores a salted hash —
 * and it is why the checkout spec avoids signing in at all. Reading it here is
 * the smallest thing that lets one browser journey complete a real sign-in.
 *
 * Returns null when the stack cannot be reached that way, which is a real case:
 * PLAYWRIGHT_BASE_URL exists so this suite can be pointed at a build that is not
 * the compose stack, and this journey cannot run against one.
 */
function readPasscode(email: string): string | null {
  const logs = readApiLog();
  if (logs === null) return null;

  // Last match wins: a re-run of this spec issues a second passcode for a new
  // address, and only the most recent line for THIS address is live.
  let code: string | null = null;
  for (const line of logs.split("\n")) {
    if (!line.includes('"otp sent"') || !line.includes(`"${email}"`)) continue;
    const match = /"code":"(\d{6})"/.exec(line);
    if (match) code = match[1];
  }
  return code;
}

test("an anonymous visitor presses Follow, signs in, and lands back Following", async ({
  page,
}) => {
  // Unique per run: a fresh address so the run starts as a stranger who follows
  // nothing, and so a re-run cannot inherit the previous run's Follow.
  const email = `e2e-follow-${Date.now()}@example.com`;

  await page.goto(ORGANIZATION_PATH);
  await expect(page.getByRole("heading", { level: 1, name: ORGANIZATION_NAME })).toBeVisible();

  // Drawn for an anonymous visitor at all — which it was not before this feature
  // — and a link rather than a write, because pressing it goes somewhere.
  const follow = page.getByRole("link", { name: "Follow", exact: true });
  await expect(follow).toBeVisible();
  await follow.click();

  // The intent is carried as an explicit parameter, in the open, where the
  // server can see it. This assertion is the security posture stated as a fact
  // about the address bar: had it been stashed in browser storage instead,
  // there would be nothing here to look at.
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/signin\\?`));
  const signInUrl = new URL(page.url());
  expect(signInUrl.searchParams.get("follow")).toBe(INTENT);
  expect(signInUrl.searchParams.get("next")).toBe("/demo-venue");

  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Send passcode" }).click();
  await expect(page.getByLabel("Passcode")).toBeVisible();

  const code = readPasscode(email);
  test.skip(
    code === null,
    "no passcode in the API log — this journey needs the dev stack (`make dev`)",
  );

  await page.getByLabel("Passcode").fill(code as string);
  await page.getByRole("button", { name: "Sign in" }).click();

  // Back where they started, in the language they were reading in.
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/demo-venue$`));

  // And Following it, without a second press. That is the whole journey: the
  // press happened before there was anybody to attribute it to, and the Follow
  // exists anyway.
  await expect(page.getByRole("button", { name: "Following" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
});
