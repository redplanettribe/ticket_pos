import { expect, test, type Page } from "@playwright/test";

import { readPasscode } from "./passcode";

/**
 * Signing a browser in as a Customer, through the passcode door.
 *
 * A FIXTURE RATHER THAN A JOURNEY. What sign-in is, what a passcode proves and
 * what the consent step holds back are asserted in signin-consent.spec.ts and in
 * backend/integration; this exists because checkout begins signed in (ADR 0054,
 * #385) and several journeys now need a Customer Session before they can begin
 * at all. Every one of them was a guest journey until that ticket, and rewriting
 * them each with their own copy of these eight steps would be eight places for
 * the sign-in flow to be re-described.
 *
 * The consent step is TOLERATED RATHER THAN REQUIRED. A brand-new address always
 * meets it (#251); an address that has already answered everything is signed in
 * by the verify itself. Which of the two happens is a consent rule and not this
 * fixture's business, so it handles either — and a journey that cares about
 * which one it got should assert that itself.
 *
 * Skips the calling test when the dev stack is not reachable the way the
 * passcode is read: a code exists nowhere but in the log of the API that issued
 * it, because the database stores a salted hash.
 */
export async function signInAsCustomer(
  page: Page,
  email: string,
  { locale = "en", next = "/tickets" }: { locale?: string; next?: string } = {},
) {
  await page.goto(`/${locale}/signin?next=${encodeURIComponent(next)}`);
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

  // Either the consent step or the destination, whichever this address is owed.
  const consentBox = page.getByLabel(/I have read and accept the Privacy Policy/);
  await Promise.race([
    consentBox.waitFor({ state: "visible" }),
    page.waitForURL(`**${next.split("?")[0]}*`),
  ]);
  if (await consentBox.isVisible()) {
    // The required box only. The optional two are left unticked deliberately:
    // an unticked box at a capture moment is an explicit No, which is an ANSWER
    // (ADR 0034) — so this address owes nothing afterwards, which is what lets a
    // checkout journey assert that the dialog asks for no consent at all.
    await consentBox.check();
    await page.getByRole("button", { name: "Agree and sign in" }).click();
  }

  // Return only once the Customer Session actually exists, which is what every
  // caller is really asking for — none of them wants a form filled in, they want
  // to BE somebody.
  //
  // Waiting is not a tidiness: the consent branch above ends on a click, and a
  // caller that navigates on the next line races the cookie that click is still
  // setting. The page it lands on renders signed out, and the failure surfaces
  // far away and much later — as a Buy control that is a link rather than a
  // button, on a journey whose sign-in appeared to succeed.
  await page.waitForURL(`**${next.split("?")[0]}*`);
}
