import { test, expect } from "@playwright/test";

import { readPasscode } from "./support/passcode";

// The sign-in consent step (#251, parent #249), against the dev stack
// (`make dev`).
//
// ONE journey, and what makes it worth a browser is that the step exists in one:
// a passcode is proved, no session is minted, and the visitor meets a screen
// built from text the API served — the Short Notice and the three checkbox
// labels — with a required box that gates a button. Whether the API withholds
// the session, what it records, and what the answers make true are all pinned in
// backend/integration/customer_consent_test.go and are deliberately not
// restated here (docs/testing.md, E2E boundary).
//
// What IS asserted here is the wiring nothing else can see: the notice rendering
// on the page from the Go binary's embedded artifacts, the link to the full
// policy, boxes drawn unticked, the required one gating the submit, and the
// cookie that appears only on the far side of the submission.

const LOCALE = "en";

// A fragment of the placeholder Short Notice, as the API serves it. It is
// matched on rather than reproduced: the words belong to the Policy Version and
// this suite must not be a second copy of them (ADR 0036).
const SHORT_NOTICE_FRAGMENT = /processes your name, email address/;

const REQUIRED_BOX = /I have read and accept the Privacy Policy/;
const MARKETING_BOX = /to send me marketing email/;
const NETWORKING_BOX = /to show my profile data/;

test("a first-time visitor is asked for consent before any session exists", async ({ page }) => {
  // Unique per run: a fresh address has accepted nothing, which is the state
  // every Customer is in the first time they sign in after this shipped.
  const email = `e2e-consent-${Date.now()}@example.com`;

  await page.goto(`/${LOCALE}/signin?next=/tickets`);
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

  // A correct passcode, and still on the sign-in page: the session is withheld
  // until consent is recorded.
  await expect(page.getByText(SHORT_NOTICE_FRAGMENT)).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/signin`));

  // The full policy is one link away, and it is a real page in this language.
  const policyLink = page.getByRole("link", { name: "Read the full Privacy Policy" });
  await expect(policyLink).toHaveAttribute("href", `/${LOCALE}/privacy-policy`);

  // Every box unticked. Consent is affirmative, so the screen may not arrive
  // with anything already agreed to on the visitor's behalf.
  const required = page.getByLabel(REQUIRED_BOX);
  const marketing = page.getByLabel(MARKETING_BOX);
  const networking = page.getByLabel(NETWORKING_BOX);
  await expect(required).not.toBeChecked();
  await expect(marketing).not.toBeChecked();
  await expect(networking).not.toBeChecked();

  // The optional two say so, in this page's language, beside labels the API
  // worded. The required one carries no such chip.
  await expect(page.getByText("Optional", { exact: true })).toHaveCount(2);

  // The required box gates the submit. (The API refuses the same submission on
  // its own account — integration tests own that; this is what a person sees.)
  const submit = page.getByRole("button", { name: "Agree and sign in" });
  await expect(submit).toBeDisabled();

  await required.check();
  await expect(submit).toBeEnabled();

  // Declining both optional boxes costs nothing: the sign-in completes and the
  // visitor lands where they were going.
  await submit.click();
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/tickets$`));

  // And they are genuinely signed in — the session cookie exists only now, on
  // the far side of the consent submission, and never before it.
  const cookies = await page.context().cookies();
  expect(cookies.some((cookie) => cookie.name === "ticket_pos_customer_session")).toBe(true);
});
