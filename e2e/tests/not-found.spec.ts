import { test, expect } from "@playwright/test";

// The Storefront's not-found screen (#799), against the dev stack (`make dev`).
//
// The words and the locale an unmatched address reads are held by
// apps/storefront/lib/not-found.test.ts; what no unit test can see is the
// wiring: that an unknown Event reaches the `[locale]` not-found screen and an
// address no route matches reaches app/global-not-found.tsx, that both answer
// 404, speak the address's language and lead back to that language's global
// explorer. Nothing here needs seed data: every address is one that cannot exist.

const CASES = [
  { path: "/es/no-such-organization/events/no-such-event", lang: "es", title: "No encontramos esta página", back: "Descubrir eventos" },
  { path: "/en/no-such-organization/events/no-such-event", lang: "en", title: "We couldn't find this page", back: "Discover events" },
  { path: "/es/a/b/c/d", lang: "es", title: "No encontramos esta página", back: "Descubrir eventos" },
] as const;

for (const { path, lang, title, back } of CASES) {
  test(`${path} is a 404 in its own language with a way back`, async ({ page }) => {
    const response = await page.goto(path);
    expect(response?.status()).toBe(404);
    await expect(page.getByText(title)).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("lang", lang);
    // Nothing failed, so there is nothing to retry and no reference code.
    await expect(page.getByRole("button")).toHaveCount(0);

    await page.getByRole("link", { name: back }).click();
    await expect(page).toHaveURL(new RegExp(`/${lang}$`));
  });
}

// An address no route matches is rendered whole on the server, not only after
// hydration: a crawler or a reader without JavaScript gets the screen and the
// document's language from the HTML itself.
test("an address no route matches is the Spanish screen in the server's HTML", async ({ request }) => {
  const response = await request.get("/es/a/b/c/d");
  expect(response.status()).toBe(404);
  const html = await response.text();
  expect(html).toContain('<html lang="es"');
  expect(html).toContain("No encontramos esta página");
});
