import { expect, test, type Page } from "@playwright/test";

// The Storefront production image is a Next standalone bundle whose shared
// workspace packages are pulled in by file tracing. When tracing is wrong the
// build still succeeds and the container fails at request time, so a 200 on
// "/" is not evidence of anything. These assertions require the standalone
// server to have rendered real Event data through @ticket-pos/ui and to have
// served its client chunks.
//
// Requires the parity stack: `make prod` (see README).

const EVENT_LINK = /^\/[^/]+\/events\/[^/]+$/;

// Event cards on the explorer are links wrapping the Event name in an h3.
function eventCards(page: Page) {
  return page.locator("a[href]").filter({ has: page.locator("h3") });
}

test("Storefront serves the Event listing from the parity stack", async ({ page }) => {
  await page.goto("/");

  await expect(page.getByRole("heading", { name: "Discover events" })).toBeVisible();

  // Real cards, rendered server-side from the API's seeded Events - not an
  // empty state and not an error page.
  const first = eventCards(page).first();
  await expect(first).toBeVisible();
  expect(await first.getAttribute("href")).toMatch(EVENT_LINK);
  await expect(first.locator("h3")).not.toBeEmpty();
  await expect(page.getByText(/Presented by/).first()).toBeVisible();
});

test("Storefront client bundle hydrates in the parity stack", async ({ page }) => {
  const failures: string[] = [];
  page.on("response", (response) => {
    if (response.url().includes("/_next/static/") && !response.ok()) {
      failures.push(`${response.status()} ${response.url()}`);
    }
  });

  await page.goto("/");

  // The date presets are a client component from @ticket-pos/ui; clicking one
  // only changes the URL if the traced client chunks were served and executed.
  await page.getByRole("button", { name: "This month" }).click();
  await expect(page).toHaveURL(/[?&]when=month\b/);
  await expect(page.getByRole("button", { name: "This month" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );

  expect(failures, "static chunks missing from the standalone image").toEqual([]);
});

test("Storefront renders an Event detail page from the parity stack", async ({ page }) => {
  await page.goto("/");

  const card = eventCards(page).first();
  const name = (await card.locator("h3").innerText()).trim();
  await card.click();

  await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Tickets" })).toBeVisible();
});

test("Storefront reaches the API from inside the parity network", async ({ request }) => {
  // The route handler proxies the Go API over the Compose network using the
  // runtime API_URL, and returns 502 when it cannot reach it.
  const response = await request.get("/api/events?limit=1");
  expect(response.status()).toBe(200);

  const eventPage = (await response.json()) as { events: { slug: string; name: string }[] };
  expect(eventPage.events.length).toBeGreaterThan(0);
  expect(eventPage.events[0].name).not.toBe("");
});
