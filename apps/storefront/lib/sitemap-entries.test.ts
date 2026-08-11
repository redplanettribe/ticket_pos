import assert from "node:assert/strict";
import test from "node:test";

import {
  SITEMAP_MAX_PAGES,
  collectSitemapEvents,
  sitemapEntries,
  sitemapPaths,
  type SitemapEvent,
  type SitemapEventPage,
} from "./sitemap-entries.ts";
import { PRIVACY_POLICY_PATH } from "./privacy-policy.ts";

const BASE = new URL("https://tickets.example.com");

const event = (organizationSlug: string, slug: string): SitemapEvent => ({
  organizationSlug,
  slug,
});

/** A fetcher serving fixed pages in order, then the end. */
function pagedFetcher(pages: SitemapEventPage[]) {
  const calls: (string | undefined)[] = [];
  let index = 0;
  return {
    calls,
    fetch: async (cursor?: string) => {
      calls.push(cursor);
      return pages[index++] ?? null;
    },
  };
}

test("every path is published in both Locales", () => {
  const entries = sitemapEntries([event("acme", "gala")], BASE);
  const urls = entries.map((entry) => entry.url);

  assert.deepEqual(urls, [
    "https://tickets.example.com/en",
    "https://tickets.example.com/es",
    "https://tickets.example.com/en/privacy-policy",
    "https://tickets.example.com/es/privacy-policy",
    "https://tickets.example.com/en/acme",
    "https://tickets.example.com/es/acme",
    "https://tickets.example.com/en/acme/events/gala",
    "https://tickets.example.com/es/acme/events/gala",
  ]);
});

test("each entry carries the reciprocal hreflang map its own page carries", () => {
  const entries = sitemapEntries([event("acme", "gala")], BASE);
  const spanishEvent = entries.find(
    (entry) => entry.url === "https://tickets.example.com/es/acme/events/gala",
  );

  // Identical to what the Event page's generateMetadata emits, because both
  // come from localeAlternates. A sitemap that disagreed with the page would
  // leave a crawler trusting neither.
  assert.deepEqual(spanishEvent?.alternates.languages, {
    en: "https://tickets.example.com/en/acme/events/gala",
    es: "https://tickets.example.com/es/acme/events/gala",
    "x-default": "https://tickets.example.com/en/acme/events/gala",
  });
});

test("an Organization with many Events is published once", () => {
  const paths = sitemapPaths([
    event("acme", "gala"),
    event("acme", "matinee"),
    event("beta", "launch"),
    event("acme", "encore"),
  ]);

  // First appearance wins, so the listing's soonest-first order survives.
  assert.deepEqual(paths, [
    "/",
    PRIVACY_POLICY_PATH,
    "/acme",
    "/beta",
    "/acme/events/gala",
    "/acme/events/matinee",
    "/beta/events/launch",
    "/acme/events/encore",
  ]);
});

test("a repeated Event is published once", () => {
  const paths = sitemapPaths([event("acme", "gala"), event("acme", "gala")]);
  assert.deepEqual(paths, ["/", PRIVACY_POLICY_PATH, "/acme", "/acme/events/gala"]);
});

test("the Privacy Policy is published in both languages", () => {
  // A legal notice is published so it can be found, and /es is reachable almost
  // only through the language switcher — so if the Spanish policy is not in
  // here, it is effectively unpublished (#250).
  const urls = sitemapEntries([], BASE).map(({ url }) => url);

  assert.ok(urls.includes(`${BASE.origin}/en${PRIVACY_POLICY_PATH}`));
  assert.ok(urls.includes(`${BASE.origin}/es${PRIVACY_POLICY_PATH}`));
});

test("the noindex surfaces are absent", () => {
  // The Customer Area and the checkout terminal pages are robots.noindex, and
  // nothing built out of Event data can reach them. Asserted rather than
  // assumed: an entry here would advertise a page we tell crawlers to drop.
  const entries = sitemapEntries(
    [event("acme", "gala"), event("beta", "launch")],
    BASE,
  );

  for (const { url } of entries) {
    const path = new URL(url).pathname;
    assert.ok(!path.includes("/tickets"), `${path} reaches the Customer Area`);
    assert.ok(!path.includes("/checkout"), `${path} reaches checkout`);
    assert.ok(!path.includes("/signin"), `${path} reaches sign-in`);
    assert.ok(!path.includes("/api"), `${path} reaches the BFF`);
  }
});

test("without an origin nothing is published", () => {
  // Next writes `url` straight into <loc> with no metadataBase to resolve it
  // against, so relative addresses would be an invalid document rather than a
  // degraded one. Off-platform (lib/site.ts) there is nothing to publish.
  assert.deepEqual(sitemapEntries([event("acme", "gala")]), []);
});

test("an origin mounted under a path keeps it", () => {
  const entries = sitemapEntries([event("acme", "gala")], new URL("https://example.com/shop/"));
  assert.equal(entries[0]?.url, "https://example.com/shop/en");
  assert.equal(entries[6]?.url, "https://example.com/shop/en/acme/events/gala");
});

test("the walk follows the cursor to the end of the listing", async () => {
  const { calls, fetch } = pagedFetcher([
    { events: [event("acme", "gala")], nextCursor: "c1" },
    { events: [event("beta", "launch")], nextCursor: null },
  ]);

  const walk = await collectSitemapEvents(fetch);

  assert.deepEqual(calls, [undefined, "c1"]);
  assert.equal(walk.pagesRead, 2);
  assert.equal(walk.stoppedAtCursor, null);
  assert.deepEqual(walk.events, [event("acme", "gala"), event("beta", "launch")]);
});

test("the cap stops a listing that never ends, and says where", async () => {
  let served = 0;
  const walk = await collectSitemapEvents(async () => {
    served += 1;
    return { events: [event("org", `event-${served}`)], nextCursor: `cursor-${served}` };
  }, 3);

  assert.equal(served, 3);
  assert.equal(walk.pagesRead, 3);
  assert.equal(walk.events.length, 3);
  // Non-null is what makes the truncation loggable. Silence here would leave a
  // short sitemap looking exactly like a small site.
  assert.equal(walk.stoppedAtCursor, "cursor-3");
});

test("the default cap is the shipped one", async () => {
  let served = 0;
  const walk = await collectSitemapEvents(async () => {
    served += 1;
    return { events: [], nextCursor: `cursor-${served}` };
  });

  assert.equal(walk.pagesRead, SITEMAP_MAX_PAGES);
});

test("a cursor that does not advance ends the walk instead of looping", async () => {
  let served = 0;
  const walk = await collectSitemapEvents(async () => {
    served += 1;
    return { events: [event("acme", `gala-${served}`)], nextCursor: "stuck" };
  });

  assert.equal(served, 2);
  assert.equal(walk.stoppedAtCursor, "stuck");
});

test("a failed page keeps what the walk already had", async () => {
  const { fetch } = pagedFetcher([{ events: [event("acme", "gala")], nextCursor: "c1" }]);

  const walk = await collectSitemapEvents(fetch);

  assert.deepEqual(walk.events, [event("acme", "gala")]);
  assert.equal(walk.pagesRead, 1);
  assert.equal(walk.stoppedAtCursor, "c1");
});

test("an empty listing still publishes the explorer root in both Locales", async () => {
  const walk = await collectSitemapEvents(async () => ({ events: [], nextCursor: null }));
  const entries = sitemapEntries(walk.events, BASE);

  // The explorer root and the Privacy Policy: the two pages that exist whether
  // or not anybody has published an Event.
  assert.deepEqual(entries.map((entry) => entry.url), [
    "https://tickets.example.com/en",
    "https://tickets.example.com/es",
    "https://tickets.example.com/en/privacy-policy",
    "https://tickets.example.com/es/privacy-policy",
  ]);
});
