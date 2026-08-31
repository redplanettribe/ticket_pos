import assert from "node:assert/strict";
import test from "node:test";

import {
  SITEMAP_MAX_PAGES,
  collectSitemapEvents,
  sitemapEntries,
  sitemapPaths,
  type LegalPublication,
  type SitemapEvent,
  type SitemapEventPage,
} from "./sitemap-entries.ts";
import { PRIVACY_POLICY_PATH } from "./privacy-policy.ts";
import { TERMS_PATH } from "./terms.ts";

const BASE = new URL("https://tickets.example.com");

/**
 * Both documents published in both languages — the ordinary state of the world,
 * and exactly what the LOCALES constant used to assume on the sitemap's behalf
 * before it was read instead (#559).
 */
const BOTH_LANGUAGES: LegalPublication = { privacyPolicy: ["en", "es"], terms: ["en", "es"] };

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
  const entries = sitemapEntries([event("acme", "gala")], BASE, BOTH_LANGUAGES);
  const urls = entries.map((entry) => entry.url);

  assert.deepEqual(urls, [
    "https://tickets.example.com/en",
    "https://tickets.example.com/es",
    "https://tickets.example.com/en/privacy-policy",
    "https://tickets.example.com/es/privacy-policy",
    "https://tickets.example.com/en/terms",
    "https://tickets.example.com/es/terms",
    "https://tickets.example.com/en/acme",
    "https://tickets.example.com/es/acme",
    "https://tickets.example.com/en/acme/events/gala",
    "https://tickets.example.com/es/acme/events/gala",
  ]);
});

test("each entry carries the reciprocal hreflang map its own page carries", () => {
  const entries = sitemapEntries([event("acme", "gala")], BASE, BOTH_LANGUAGES);
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
  const paths = sitemapPaths(
    [
      event("acme", "gala"),
      event("acme", "matinee"),
      event("beta", "launch"),
      event("acme", "encore"),
    ],
    BOTH_LANGUAGES,
  ).map(({ path }) => path);

  // First appearance wins, so the listing's soonest-first order survives.
  assert.deepEqual(paths, [
    "/",
    PRIVACY_POLICY_PATH,
    TERMS_PATH,
    "/acme",
    "/beta",
    "/acme/events/gala",
    "/acme/events/matinee",
    "/beta/events/launch",
    "/acme/events/encore",
  ]);
});

test("a repeated Event is published once", () => {
  const paths = sitemapPaths([event("acme", "gala"), event("acme", "gala")], BOTH_LANGUAGES).map(
    ({ path }) => path,
  );
  assert.deepEqual(paths, ["/", PRIVACY_POLICY_PATH, TERMS_PATH, "/acme", "/acme/events/gala"]);
});

test("the Privacy Policy is published in both languages", () => {
  // A legal notice is published so it can be found, and /es is reachable almost
  // only through the language switcher — so if the Spanish policy is not in
  // here, it is effectively unpublished (#250).
  const urls = sitemapEntries([], BASE, BOTH_LANGUAGES).map(({ url }) => url);

  assert.ok(urls.includes(`${BASE.origin}/en${PRIVACY_POLICY_PATH}`));
  assert.ok(urls.includes(`${BASE.origin}/es${PRIVACY_POLICY_PATH}`));

  assert.ok(urls.includes(`${BASE.origin}/en${TERMS_PATH}`));
  assert.ok(urls.includes(`${BASE.origin}/es${TERMS_PATH}`));
});

// --- The published language set (#559) ------------------------------------

test("a document is advertised only in the languages it publishes", () => {
  const urls = sitemapEntries([], BASE, {
    privacyPolicy: ["es"],
    terms: ["en", "es"],
  }).map(({ url }) => url);

  // The English policy is gone because it is not published: a sitemap entry
  // for it is a URL a crawler will fetch and be 404ed on.
  assert.deepEqual(urls, [
    "https://tickets.example.com/en",
    "https://tickets.example.com/es",
    "https://tickets.example.com/es/privacy-policy",
    "https://tickets.example.com/en/terms",
    "https://tickets.example.com/es/terms",
  ]);
});

test("a legal path advertises no hreflang to a language it dropped", () => {
  const entries = sitemapEntries([], BASE, { privacyPolicy: ["es"], terms: ["en", "es"] });
  const policy = entries.find(({ url }) => url.endsWith("/es/privacy-policy"));

  assert.deepEqual(policy?.alternates.languages, {
    es: "https://tickets.example.com/es/privacy-policy",
    // The protected locale, not English: x-default names where a reader with
    // no stated language lands, and English is a language a publish may drop.
    "x-default": "https://tickets.example.com/es/privacy-policy",
  });
});

test("x-default on the legal paths is the protected locale, published set or not", () => {
  const entries = sitemapEntries([], BASE, BOTH_LANGUAGES);
  for (const suffix of ["/privacy-policy", "/terms"]) {
    const entry = entries.find(({ url }) => url.endsWith(`/en${suffix}`));
    assert.equal(
      entry?.alternates.languages["x-default"],
      `${BASE.origin}/es${suffix}`,
      `x-default for ${suffix}`,
    );
  }
});

test("an ordinary page keeps English as x-default", () => {
  // The change is the legal paths' alone; nothing else has a protected locale.
  const entries = sitemapEntries([event("acme", "gala")], BASE, BOTH_LANGUAGES);
  const spanish = entries.find(({ url }) => url.endsWith("/es/acme"));
  assert.equal(spanish?.alternates.languages["x-default"], `${BASE.origin}/en/acme`);
});

test("a legal document that could not be read is omitted, not guessed at", () => {
  // null is a failed read and not an empty set. Advertising a URL that may 404
  // is worse than omitting one a crawler will be offered again next time — and
  // guessing both languages is exactly the assumption this ticket removed.
  const urls = sitemapEntries([event("acme", "gala")], BASE, {
    privacyPolicy: null,
    terms: ["en", "es"],
  }).map(({ url }) => url);

  assert.ok(!urls.some((url) => url.includes(PRIVACY_POLICY_PATH)));
  assert.ok(urls.includes(`${BASE.origin}/es${TERMS_PATH}`));
  // The rest of the site is untouched: one unreadable document does not cost a
  // crawler the catalog.
  assert.ok(urls.includes(`${BASE.origin}/en/acme/events/gala`));
});

test("both legal paths go when neither can be read", () => {
  const urls = sitemapEntries([event("acme", "gala")], BASE, {
    privacyPolicy: null,
    terms: null,
  }).map(({ url }) => url);

  assert.deepEqual(urls, [
    "https://tickets.example.com/en",
    "https://tickets.example.com/es",
    "https://tickets.example.com/en/acme",
    "https://tickets.example.com/es/acme",
    "https://tickets.example.com/en/acme/events/gala",
    "https://tickets.example.com/es/acme/events/gala",
  ]);
});

test("the noindex surfaces are absent", () => {
  // The Customer Area and the checkout terminal pages are robots.noindex, and
  // nothing built out of Event data can reach them. Asserted rather than
  // assumed: an entry here would advertise a page we tell crawlers to drop.
  const entries = sitemapEntries(
    [event("acme", "gala"), event("beta", "launch")],
    BASE,
    BOTH_LANGUAGES,
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
  assert.deepEqual(sitemapEntries([event("acme", "gala")], undefined, BOTH_LANGUAGES), []);
});

test("an origin mounted under a path keeps it", () => {
  const entries = sitemapEntries(
    [event("acme", "gala")],
    new URL("https://example.com/shop/"),
    BOTH_LANGUAGES,
  );
  assert.equal(entries[0]?.url, "https://example.com/shop/en");
  assert.equal(entries[8]?.url, "https://example.com/shop/en/acme/events/gala");
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
  const entries = sitemapEntries(walk.events, BASE, BOTH_LANGUAGES);

  // The explorer root and the two legal notices: the pages that exist whether
  // or not anybody has published an Event.
  assert.deepEqual(entries.map((entry) => entry.url), [
    "https://tickets.example.com/en",
    "https://tickets.example.com/es",
    "https://tickets.example.com/en/privacy-policy",
    "https://tickets.example.com/es/privacy-policy",
    "https://tickets.example.com/en/terms",
    "https://tickets.example.com/es/terms",
  ]);
});
