import { expect, test, type Page } from "@playwright/test";

// The Storefront production image is a Next standalone bundle whose shared
// workspace packages are pulled in by file tracing. When tracing is wrong the
// build still succeeds and the container fails at request time, so a 200 on
// "/" is not evidence of anything. These assertions require the standalone
// server to have rendered real Event data through @ticket-pos/ui and to have
// served its client chunks.
//
// Requires the parity stack: `make prod` (see README).

// Every Storefront page is served under a Locale carried in the URL, so a card's
// href names one too. This spec reads the English Storefront by name; that an
// address naming no language is redirected into one is asserted separately
// below, because the middleware that does it has to survive file tracing into
// the standalone image like everything else here.
const LOCALE = "en";
const EVENT_LINK = new RegExp(`^/${LOCALE}/[^/]+/events/[^/]+$`);

// Event cards on the explorer are links wrapping the Event name in an h3.
function eventCards(page: Page) {
  return page.locator("a[href]").filter({ has: page.locator("h3") });
}

test("Storefront serves the Event listing from the parity stack", async ({ page }) => {
  await page.goto(`/${LOCALE}`);

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

  await page.goto(`/${LOCALE}`);

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

test("Storefront explorer filters change the results, not just the chips", async ({ page }) => {
  // The hydration test above asserts the URL and the chip's aria-pressed, and a
  // filter can move both while the grid underneath never budges: the results
  // list is client state seeded from a server-rendered prop, and applying a
  // filter re-renders that component in place rather than remounting it. This
  // asserts the part a Customer actually looks at.
  await page.goto(`/${LOCALE}`);
  await expect(eventCards(page).first()).toBeVisible();

  // A term no seeded Event or Organization can match, so the assertion does not
  // depend on what the parity stack happens to hold.
  await page.getByRole("searchbox", { name: "Search events or organizers" }).fill("zzqqxnomatch");
  await page.getByRole("button", { name: "Search" }).click();

  await expect(page).toHaveURL(/[?&]q=zzqqxnomatch\b/);
  await expect(page.getByText("No events match your search")).toBeVisible();
  await expect(eventCards(page)).toHaveCount(0);

  // ...and clearing it brings them back, so the empty state is the filter
  // working rather than the grid having died.
  await page.getByRole("searchbox", { name: "Search events or organizers" }).fill("");
  await page.getByRole("button", { name: "Search" }).click();

  await expect(eventCards(page).first()).toBeVisible();
});

test("Storefront renders an Event detail page from the parity stack", async ({ page }) => {
  await page.goto(`/${LOCALE}`);

  const card = eventCards(page).first();
  const name = (await card.locator("h3").innerText()).trim();
  await card.click();

  await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Tickets" })).toBeVisible();
});

test("Storefront explorer tab is product-led with a favicon", async ({ page }) => {
  await page.goto(`/${LOCALE}`);

  await expect(page).toHaveTitle("Multiticketing — Discover events");
  expect(await page.locator('link[rel="icon"]').count()).toBeGreaterThan(0);
});

test("Storefront keeps Organization-led titles (no product-name leak)", async ({ page }) => {
  // The Organization page must own its tab: its title is Organization-led and
  // must not carry "Multiticketing" (which would happen if a title template
  // were ever added at the layout).
  await page.goto(`/${LOCALE}`);
  const href = await eventCards(page).first().getAttribute("href");
  // "/{locale}/{orgSlug}/events/{eventSlug}": the Organization is the second
  // segment now that every page carries its Locale.
  const orgSlug = (href ?? "").split("/").filter(Boolean)[1];
  expect(orgSlug, "explorer should link into an Organization").toBeTruthy();

  await page.goto(`/${LOCALE}/${orgSlug}`);
  await expect(page).toHaveTitle(/· Events$/);
  await expect(page).not.toHaveTitle(/Multiticketing/);
});

test("Storefront shows a subtle Powered by Multiticketing footer", async ({ page }) => {
  await page.goto(`/${LOCALE}`);

  const link = page.getByRole("link", { name: "Powered by Multiticketing" });
  await expect(link).toBeVisible();
  await expect(link).toHaveAttribute("href", /multiticketing/i);
});

test("Storefront footer invites a reader to create an event on the staff app", async ({ page }) => {
  // The href is computed from STAFF_BASE_URL at request time, which the parity
  // compose file supplies (docker-compose.prod.yml). Unit tests own the
  // computation; this asserts it reaches rendered output at all — a shell prop
  // that was never threaded through renders no link and no error.
  await page.goto(`/${LOCALE}`);

  const link = page.getByRole("link", { name: "Create an event" });
  await expect(link).toBeVisible();
  await expect(link).toHaveAttribute("href", /^http:\/\/localhost:64604\/login\?intent=create$/);
});

test("Storefront sends an address naming no language into a Locale", async ({ page }) => {
  // The middleware is a separate bundle in the standalone image, and an image
  // shipped without it serves "/" as a 404 rather than as the explorer. A 200
  // on "/{locale}" alone would not notice.
  await page.goto("/");
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}$`));
  await expect(page.getByRole("heading", { name: "Discover events" })).toBeVisible();
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
