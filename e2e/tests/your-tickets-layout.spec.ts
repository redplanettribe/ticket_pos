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
// tablet, and two laptops — the tablet and the laptops sit on either side of
// the `sm` (640px) breakpoint where the assign form returns to one line and of
// the `lg` (1024px) one where My info becomes a sidebar (#358).
const WIDTHS = [360, 390, 430, 768, 1024, 1280];

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

// One sign-in for the whole file. Passcodes for an address are rationed, and
// every test here reads the same page, so they run in order on one shared page
// rather than each spending a passcode of their own.
test.describe.configure({ mode: "serial" });

let page: Page;

test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
  await signInFromPasscode(page, BUYER);
});

test.afterAll(async () => {
  await page?.close();
});

test("the Customer Area never scrolls sideways with a Holder List row expanded", async () => {
  await page.goto(`/${LOCALE}/tickets`);
  await page.waitForLoadState("networkidle");

  // Since #354 a Sale is a collapsed row inside its Event's card, shut whenever
  // the Event has more than one Sale — and this buyer has several. Open the
  // first one so its Holder List is on screen; a row already open (a
  // single-Sale Event) is left alone, since clicking it would shut it.
  const firstSale = page.locator("details[id^='sale-']").first();
  await expect(firstSale).toBeVisible();
  if (!(await firstSale.evaluate((el) => (el as HTMLDetailsElement).open))) {
    await firstSale.locator("summary").first().click();
  }

  // Expanding the first Holder row puts the assign form on screen, which is
  // where the second overflow lived; the summary row itself is the first.
  const firstRow = page.locator("summary", { hasText: /^.*Ticket 1 of/ }).first();
  await expect(firstRow).toBeVisible();
  await firstRow.click();

  for (const width of WIDTHS) {
    await page.setViewportSize({ width, height: 900 });
    const scrollWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(scrollWidth, `document wider than a ${width}px viewport`).toBeLessThanOrEqual(width);
  }
});

// The laptop layout (#358): from `lg` up My info is a sidebar to the RIGHT of
// the tickets; below it, the same element sits BELOW them. Proven on bounding
// boxes, because the grid is the only thing that moves the panel and a class
// name would pin the mechanism rather than the promise.
test("My info is a sidebar on a laptop and sits under the tickets on a tablet", async () => {
  await page.goto(`/${LOCALE}/tickets`);
  await page.waitForLoadState("networkidle");

  const myInfo = page.locator("#my-info");
  const firstGroup = page.locator("details[id^='sale-']").first();
  await expect(myInfo).toBeVisible();
  await expect(firstGroup).toBeVisible();

  await page.setViewportSize({ width: 1280, height: 900 });
  let info = (await myInfo.boundingBox())!;
  let group = (await firstGroup.boundingBox())!;
  expect(info.x, "My info is not to the right of the tickets at 1280px").toBeGreaterThan(
    group.x + group.width - 1,
  );

  await page.setViewportSize({ width: 768, height: 900 });
  info = (await myInfo.boundingBox())!;
  group = (await firstGroup.boundingBox())!;
  expect(info.y, "My info is not below the tickets at 768px").toBeGreaterThan(
    group.y + group.height - 1,
  );
});
