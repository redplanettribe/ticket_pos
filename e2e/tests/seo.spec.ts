import { test, expect } from "@playwright/test";

// The Storefront's crawler-facing surface, against the dev stack (`make dev`).
//
// EVERY COMPUTATION BEHIND THIS IS ALREADY UNIT-TESTED at the lib seam —
// apps/storefront/lib/alternates.ts decides the canonical and the hreflang set,
// lib/locale.ts decides where an unprefixed address lands, lib/sitemap-entries.ts
// builds the sitemap document, and each has its own .test.ts asserting the shapes
// in detail. None of that is repeated here, and adding it would be the wrong
// trade: those tests are fast, exhaustive and run on every push, while this one
// needs a stack.
//
// What no unit test can see is WIRING. `localeAlternates` can be perfect and
// simply never called from a `generateMetadata`; `sitemapEntries` can be right
// and the route that should return it never reached; the middleware can compute
// a flawless redirect target while its matcher excludes the path. Every failure
// there looks identical from the lib seam — green — and identical to a visitor,
// who sees a page that renders fine. It is only visible to a crawler, months
// later, in traffic that never arrived. So this spec asserts one thing per
// mechanism: that the piece is connected to the rendered output at all.
//
// Data: the dev-seed migrations (backend/migrations/002, 008, 035) provide the
// published Events these read — two Discoverable, one not. Nothing here writes.

// Both languages, as the URL spells them (apps/storefront/lib/locale.ts). English
// is also the x-default target, which is why it is named separately below rather
// than read as "the first one".
const LOCALES = ["en", "es"] as const;
const DEFAULT_LOCALE = "en";

// The Event every assertion here is made against, addressed without a language.
// Each test puts the prefix on itself, because which prefix produces which
// annotation is the entire subject.
const EVENT_PATH = "/demo-venue/events/sunrise-jazz-brunch";

// The published Event whose Discoverable flag is off (migration 035), with the
// Ticket Type it sells. Separate from the Event above because the annotations
// asserted there are exactly the ones that must be MISSING here.
const UNLISTED_EVENT_PATH = "/demo-venue/events/unlisted-loft-session";
const UNLISTED_EVENT_NAME = "Unlisted Loft Session";
const UNLISTED_TICKET_TYPE = "Loft Entry";

/**
 * An href as a path on this Storefront, resolved against the address it was
 * found at.
 *
 * Everything below compares paths and not whole URLs. The origin in a canonical
 * comes from STOREFRONT_BASE_URL, which is a deployment fact — it differs
 * between the dev stack, the parity stack and production, and is absent
 * off-platform — while the claim being tested is which PAGE the annotation
 * names. Asserting the origin too would only pin this suite to one environment
 * and would fail for a reason that has nothing to do with SEO wiring. The one
 * place the origin does matter is the Sitemap directive in robots.txt, and that
 * test says so and checks it separately.
 */
function pathOf(href: string | null | undefined, base: string | undefined): string {
  return new URL(href ?? "", base).pathname;
}

test("an Event page declares its Locale and canonicalises to itself", async ({ page }) => {
  for (const locale of LOCALES) {
    await page.goto(`/${locale}${EVENT_PATH}`);

    // The document says what language it is written in. A crawler and a screen
    // reader both read this before anything else on the page.
    await expect(page.locator("html")).toHaveAttribute("lang", locale);

    // THE ASSERTION THAT STOPS THE PAIR READING AS DUPLICATION. Two pages with
    // the same pictures, the same dates and the same Event name are, to a
    // crawler, near-identical — and the canonical is the only thing that says
    // they are two pages rather than one page at two addresses. Self-referential
    // is therefore load-bearing in the Spanish direction specifically: a Spanish
    // page canonicalising to /en is not a smaller claim, it is the instruction
    // "index the English one instead", and the Spanish Storefront disappears
    // from Spanish results while every page still renders perfectly.
    const canonical = page.locator('link[rel="canonical"]');
    await expect(canonical).toHaveCount(1);
    expect(pathOf(await canonical.getAttribute("href"), page.url())).toBe(
      `/${locale}${EVENT_PATH}`,
    );
  }
});

test("the two translations name each other, and x-default names English", async ({ page }) => {
  for (const locale of LOCALES) {
    await page.goto(`/${locale}${EVENT_PATH}`);

    // Reciprocal, from both ends: the English page lists Spanish AND ITSELF,
    // the Spanish page lists English AND ITSELF. A crawler only honours an
    // hreflang pair it can confirm from the other side, so a page that names
    // only the translation it is not publishes a claim nothing backs — and one
    // that names only itself has published nothing at all.
    for (const target of LOCALES) {
      const alternate = page.locator(`link[rel="alternate"][hreflang="${target}"]`);
      await expect(alternate).toHaveCount(1);
      expect(pathOf(await alternate.getAttribute("href"), page.url())).toBe(
        `/${target}${EVENT_PATH}`,
      );
    }

    // x-default is a claim about where an address naming no language goes, so
    // it must agree with the redirect asserted at the bottom of this file. The
    // two are decided in the same module and would drift apart silently: nothing
    // about a mismatched pair is visible on any page.
    const xDefault = page.locator('link[rel="alternate"][hreflang="x-default"]');
    await expect(xDefault).toHaveCount(1);
    expect(pathOf(await xDefault.getAttribute("href"), page.url())).toBe(
      `/${DEFAULT_LOCALE}${EVENT_PATH}`,
    );
  }
});

test("a non-Discoverable Event still sells, and asks not to be indexed", async ({ page }) => {
  const response = await page.goto(`/${DEFAULT_LOCALE}${UNLISTED_EVENT_PATH}`);

  // BOTH HALVES IN ONE TEST, because it is the pair that matters and each half
  // is the way the other gets broken. "Unlisted" means unlisted to crawlers
  // while staying reachable by direct link (ADR 0002): the Event is off the
  // Organization page and out of the explorer, and now out of the index too, but
  // anyone holding the URL can still open it and buy. Split across two tests,
  // the noindex one would still pass on a page that had stopped rendering
  // entirely — which is a real fix for the wrong problem, and a silent one.
  expect(response?.status()).toBe(200);
  await expect(page.getByRole("heading", { name: UNLISTED_EVENT_NAME, level: 1 })).toBeVisible();

  // Offered, not merely listed: the stepper is what makes the Ticket Type
  // sellable rather than a read-only line like an ended Event's.
  await expect(page.getByRole("heading", { name: UNLISTED_TICKET_TYPE })).toBeVisible();
  await expect(
    page.getByRole("button", { name: `Add one ${UNLISTED_TICKET_TYPE} ticket` }),
  ).toBeVisible();

  // The other half. This is the only layer that can see it: the branch lives
  // inside the page's generateMetadata, which has no unit seam of its own.
  const robots = page.locator('meta[name="robots"]');
  await expect(robots).toHaveCount(1);
  expect(await robots.getAttribute("content")).toContain("noindex");

  // And NOTHING that files it anyway. A canonical and an hreflang set are both
  // instructions about how to index a page — which address to file it under, and
  // which translation to file beside it — so stating either alongside noindex
  // invites a crawler to resolve the contradiction the other way, and index the
  // Spanish twin the annotation just pointed at.
  await expect(page.locator('link[rel="canonical"]')).toHaveCount(0);
  await expect(page.locator('link[rel="alternate"][hreflang]')).toHaveCount(0);
});

test("the footer offers the other Locale as a real link", async ({ page }) => {
  await page.goto(`/${DEFAULT_LOCALE}${EVENT_PATH}`);

  // Both languages, always in the DOM — including the one being read. An
  // hreflang annotation is a claim a crawler may check; an ordinary internal
  // link is how it finds the translation in the first place, and it is the only
  // one of the two a reader can click.
  //
  // The labels are endonyms and identical in every language by design
  // (components/language-switcher.tsx), so this locator does not change with the
  // page's own locale.
  const toSpanish = page.getByRole("link", { name: "Español", exact: true });
  const toEnglish = page.getByRole("link", { name: "English", exact: true });
  await expect(toSpanish).toBeVisible();
  await expect(toEnglish).toBeVisible();

  // AN ANCHOR, and its href IS the destination. A <select> with an onChange, or
  // a button calling router.push, would look and behave identically to a person
  // and be invisible to a crawler — which would leave the Spanish Storefront
  // reachable only through an hreflang annotation it has nothing to corroborate.
  // The tag name is asserted rather than inferred from the role, because
  // role="link" can be put on anything.
  await expect(toSpanish).toHaveJSProperty("tagName", "A");
  expect(pathOf(await toSpanish.getAttribute("href"), page.url())).toBe(`/es${EVENT_PATH}`);

  // The current language stays a link and says which one it is, rather than
  // going missing or turning into plain text.
  await expect(toEnglish).toHaveAttribute("aria-current", "true");
  await expect(toSpanish).not.toHaveAttribute("aria-current", "true");
});

test("/sitemap.xml offers both Locales, each annotated with the other", async ({
  page,
  request,
}) => {
  const response = await request.get("/sitemap.xml");
  expect(response.status()).toBe(200);
  const xml = await response.text();

  // Parsed rather than pattern-matched: a sitemap a crawler cannot parse is a
  // sitemap it discards whole, and string matching would pass on a document
  // with an unclosed tag in it. The browser's own XML parser is the one whose
  // verdict matters, so it is asked directly.
  await page.goto(`/${DEFAULT_LOCALE}`);
  const entries = await page.evaluate((source) => {
    const doc = new DOMParser().parseFromString(source, "application/xml");
    if (doc.querySelector("parsererror")) return null;
    const XHTML = "http://www.w3.org/1999/xhtml";
    return Array.from(doc.getElementsByTagName("url")).map((url) => ({
      loc: url.getElementsByTagName("loc")[0]?.textContent ?? "",
      // By namespace rather than by the "xhtml:" prefix: the prefix is the
      // document's own choice and a crawler resolves it, so matching the
      // literal string would be asserting a spelling instead of a fact.
      alternates: Object.fromEntries(
        Array.from(url.getElementsByTagNameNS(XHTML, "link")).map((link) => [
          link.getAttribute("hreflang") ?? "",
          link.getAttribute("href") ?? "",
        ]),
      ),
    }));
  }, xml);

  expect(entries, "sitemap.xml is not well-formed XML").not.toBeNull();
  const byPath = new Map((entries ?? []).map((entry) => [pathOf(entry.loc, page.url()), entry]));

  // The Spanish half of the site is here or it is nowhere: /es is reachable
  // almost only through the switcher, so this document is where a crawler is
  // told it exists rather than left to find it. An entry per Locale — not one
  // entry with the other language hidden in an annotation.
  for (const locale of LOCALES) {
    const entry = byPath.get(`/${locale}${EVENT_PATH}`);
    expect(entry, `sitemap.xml lists no ${locale} entry for the Event`).toBeTruthy();

    // The same reciprocal set the page's own <head> carries. A sitemap that
    // disagreed with the pages would be a contradiction a crawler resolves by
    // trusting neither, which is why both are built from the one function —
    // and why this asserts the annotations arrived, not just the URLs.
    const alternates = entry?.alternates ?? {};
    for (const target of LOCALES) {
      expect(pathOf(alternates[target], page.url())).toBe(`/${target}${EVENT_PATH}`);
    }
    expect(pathOf(alternates["x-default"], page.url())).toBe(`/${DEFAULT_LOCALE}${EVENT_PATH}`);
  }
});

test("/robots.txt points a crawler at the sitemap", async ({ request, baseURL }) => {
  const response = await request.get("/robots.txt");
  expect(response.status()).toBe(200);
  const body = await response.text();

  // The whole point of the file, for this ticket: without this line the sitemap
  // is a document nothing asks for, and the Spanish Storefront goes back to
  // being undiscoverable. It must be absolute — a relative Sitemap directive is
  // ignored — so this one is asserted as a full URL rather than a path.
  const sitemap = body.match(/^Sitemap:\s*(\S+)$/m)?.[1];
  expect(sitemap, "robots.txt names no Sitemap").toBeTruthy();
  expect(sitemap).toMatch(/^https?:\/\/\S+\/sitemap\.xml$/);

  // And it must be this Storefront's sitemap, not a stale origin baked in at
  // build time — the failure mode STOREFRONT_BASE_URL being a runtime variable
  // exists to prevent (apps/storefront/lib/site.ts).
  expect(pathOf(sitemap, baseURL)).toBe("/sitemap.xml");
  expect(new URL(sitemap ?? "").origin).toBe(new URL(baseURL ?? "").origin);
});

test("an address naming no language is sent to one, temporarily", async ({
  request,
  baseURL,
}) => {
  // Followed by hand so the redirect itself can be read. Playwright would
  // otherwise hand back the page at the end of the chain, and the status code —
  // the entire subject of this test — would be gone.
  const response = await request.get(EVENT_PATH, { maxRedirects: 0 });

  // TEMPORARY, NEVER PERMANENT. The destination is computed per request from a
  // cookie and Accept-Language (lib/locale.ts), so it is a different answer for
  // different people on the same address. A 301 or 308 tells every browser and
  // proxy in between to stop asking — and a visitor whose browser cached "/ →
  // /en" cannot be moved to Spanish by any change made on the server, because
  // the request never arrives. There is no cache-busting a permanent redirect
  // out of somebody's browser; it is fixed by them clearing it, which they will
  // not do because they do not know it happened.
  expect([302, 307]).toContain(response.status());
  expect(pathOf(response.headers()["location"], baseURL)).toBe(
    `/${DEFAULT_LOCALE}${EVENT_PATH}`,
  );
});
