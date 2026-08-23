import { test, expect, type Page } from "@playwright/test";

import { readPasscode } from "./support/passcode";

// The Customer Area fits a phone (#353), against the dev stack (`make dev`).
//
// What a browser proves here and nothing else can is LAYOUT: a Holder List row
// whose address is long enough to push the page wider than the screen, and an
// assign form that lays label, field and button side by side. A unit test of
// the components renders no box model, and a snapshot of class names would pin
// the fix rather than the promise. The promise is one number: the document is
// never wider than the viewport, at every common phone width and at the
// breakpoint above which the form is allowed back onto one line.
//
// The account is a real dev-stack buyer with a three-ticket Sale whose Holders
// carry long addresses — the exact shape that overflowed. A fresh fixture
// would need a checkout, an assignment and an accepting Holder to reach the
// same state, which the Ticket Assignment journey already walks.

const LOCALE = "en";
const BUYER = "pedrodcsjostrom@gmail.com";

// Two phones at each end of the common range, the widest common phone, a
// tablet, and a laptop — the last two sit on either side of the `sm` (640px)
// breakpoint where the assign form returns to one line.
const WIDTHS = [360, 390, 430, 768, 1280];

async function signInFromPasscode(page: Page, email: string) {
  await page.goto(`/${LOCALE}/signin?next=/tickets`);
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Send passcode" }).click();
  await expect(page.getByLabel("Passcode")).toBeVisible();

  const code = readPasscode(email);
  test.skip(
    code === null,
    "no passcode in the API log — this spec needs the dev stack (`make dev`)",
  );
  await page.getByLabel("Passcode").fill(code as string);
  await page.getByRole("button", { name: "Sign in" }).click();

  // The consent step (#251) appears only when something is still unanswered
  // for this address; tolerated either way, as in the assignment journey.
  // The sign-in page's own URL carries `?next=/tickets`, so the wait is on the
  // PATH ending in /tickets, not on the string appearing anywhere in the URL.
  const consentBox = page.getByLabel(/I have read and accept the Privacy Policy/);
  const arrived = page.waitForURL((url) => url.pathname.endsWith("/tickets"));
  await Promise.race([consentBox.waitFor({ state: "visible" }), arrived]);
  if (await consentBox.isVisible()) {
    await consentBox.check();
    await page.getByRole("button", { name: "Agree and sign in" }).click();
  }
  await page.waitForURL((url) => url.pathname.endsWith("/tickets"));
}

test("the Customer Area never scrolls sideways with a Holder List row expanded", async ({
  page,
}) => {
  await signInFromPasscode(page, BUYER);
  await page.goto(`/${LOCALE}/tickets`);
  await page.waitForLoadState("networkidle");

  // Expanding the first row puts the assign form on screen, which is where the
  // second overflow lived; the summary row itself is the first.
  const firstRow = page.locator("summary", { hasText: /^.*Ticket 1 of/ }).first();
  await expect(firstRow).toBeVisible();
  await firstRow.click();

  for (const width of WIDTHS) {
    await page.setViewportSize({ width, height: 900 });
    const scrollWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(scrollWidth, `document wider than a ${width}px viewport`).toBeLessThanOrEqual(width);
  }
});
