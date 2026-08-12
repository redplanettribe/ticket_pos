import { test, expect } from "@playwright/test";

import { readPasscode } from "./support/passcode";

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

  // The consent step, which a brand-new address always meets (#251). The intent
  // has to survive it too: the Follow is written against the session, and the
  // session is now minted by THIS submission rather than by the verify. That is
  // the whole reason this spec passes through here rather than avoiding it.
  await page.getByLabel(/I have read and accept the Privacy Policy/).check();
  await page.getByRole("button", { name: "Agree and sign in" }).click();

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
