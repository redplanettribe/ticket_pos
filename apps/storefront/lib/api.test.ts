/**
 * What the reads actually hand fetch — specifically, whether the one caching
 * opt-in in this app is still an opt-in.
 *
 * This file exists for a failure that is invisible from every other angle. A
 * sitemap whose read lost its `{ revalidate }` renders the same document, serves
 * the same 200, and passes every assertion anyone would write about its
 * contents, while walking the whole catalog again on every crawler hit. Nothing
 * observable changes except the bill, so the init is asserted directly.
 */

import assert from "node:assert/strict";
import test from "node:test";

// The ".ts" is written out because these tests run the modules directly under
// `node --experimental-strip-types`, which resolves specifiers exactly.
import {
  LegalDocumentUnavailableError,
  SITEMAP_READ_REVALIDATE_SECONDS,
  getPrivacyPolicy,
  legalDocumentPublishes,
  listPublicEvents,
  listSitemapEvents,
  publishedLegalLocales,
  requirePrivacyPolicy,
  requireTerms,
} from "./api.ts";

/** The init a read hands fetch, plus the field Next adds to RequestInit. */
type FetchInit = RequestInit & { next?: { revalidate?: number } };

type RecordedCall = { url: string; init: FetchInit };

/**
 * Runs one read against a stubbed fetch and returns the single call it made.
 *
 * K_SERVICE is unset for the duration because it is how lib/api.ts decides it is
 * on Cloud Run: left set, the read would first fetch an ID token from the
 * metadata server and the assertions below would be looking at that call.
 */
async function recordFetch(read: () => Promise<unknown>): Promise<RecordedCall> {
  const calls: RecordedCall[] = [];
  const realFetch = globalThis.fetch;
  const realService = process.env.K_SERVICE;
  delete process.env.K_SERVICE;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ url: String(input), init: (init ?? {}) as FetchInit });
    return new Response(JSON.stringify({ data: { events: [], next_cursor: null }, error: null }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }) as typeof fetch;

  try {
    await read();
  } finally {
    globalThis.fetch = realFetch;
    if (realService === undefined) {
      delete process.env.K_SERVICE;
    } else {
      process.env.K_SERVICE = realService;
    }
  }

  assert.equal(calls.length, 1, "expected the read to make exactly one fetch");
  return calls[0];
}

test("the sitemap's read opts into the Data Cache", async () => {
  const { init } = await recordFetch(() => listSitemapEvents(undefined, 50));

  assert.equal(
    init.next?.revalidate,
    SITEMAP_READ_REVALIDATE_SECONDS,
    "without next.revalidate the sitemap silently regenerates on every request",
  );
  // The two are mutually exclusive in Next and "no-store" is the one that wins,
  // so a read carrying both is an uncached read wearing a revalidate.
  assert.equal(
    init.cache,
    undefined,
    'a cached read must not also set cache: "no-store" overrides next.revalidate',
  );
});

test("the sitemap's read is still the public listing read, cursor and limit and all", async () => {
  const { url } = await recordFetch(() => listSitemapEvents("cursor-2", 50));

  assert.match(url, /\/api\/v1\/public\/events\?/);
  assert.match(url, /cursor=cursor-2/);
  assert.match(url, /limit=50/);
});

test("every other read stays uncached", async () => {
  const { init } = await recordFetch(() => listPublicEvents({ q: "jazz" }));

  // A buyer reading a cached remaining count is a buyer being lied to (see
  // ReadCache in lib/api.ts); the opt-in must not have leaked into the default.
  assert.equal(init.cache, "no-store");
  assert.equal(init.next, undefined);
});

// --- The two legal documents (#559) ---------------------------------------
//
// The whole point of this half of lib/api.ts is which failure produces which
// answer, and nothing about a rendered page says which one it took: a 404 page
// and a 404 page look identical whether the locale is unpublished or the
// database is on fire. So the split is asserted here, at the HTTP seam.

/** Runs a read against a fetch that answers however the test says. */
async function withFetch<T>(
  answer: (url: string) => Promise<Response>,
  read: () => Promise<T>,
): Promise<T> {
  const realFetch = globalThis.fetch;
  const realService = process.env.K_SERVICE;
  delete process.env.K_SERVICE;
  globalThis.fetch = (async (input: RequestInfo | URL) => answer(String(input))) as typeof fetch;
  try {
    return await read();
  } finally {
    globalThis.fetch = realFetch;
    if (realService === undefined) {
      delete process.env.K_SERVICE;
    } else {
      process.env.K_SERVICE = realService;
    }
  }
}

const json = (body: unknown, status: number) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const policyEnvelope = json({ data: { version: "1", body_markdown: "# hi" }, error: null }, 200);

test("a 404 is the one honest absence: this language is not published", async () => {
  const policy = await withFetch(
    async () => json({ data: null, error: { code: "NOT_FOUND", message: "no" } }, 404),
    () => requirePrivacyPolicy("en"),
  );

  // Null, and the page renders notFound() — a true statement about a locale
  // the current edition does not publish.
  assert.equal(policy, null);
});

test("a 5xx throws rather than reading as an unpublished document", async () => {
  await assert.rejects(
    withFetch(
      async () => json({ data: null, error: { code: "INTERNAL", message: "boom" } }, 503),
      () => requirePrivacyPolicy("es"),
    ),
    LegalDocumentUnavailableError,
  );
});

test("an unreachable API throws", async () => {
  await assert.rejects(
    withFetch(
      async () => {
        throw new TypeError("fetch failed");
      },
      () => requireTerms("es"),
    ),
    LegalDocumentUnavailableError,
  );
});

test("a malformed body throws", async () => {
  // A proxy's HTML error page carrying a 200 is the shape this catches; parsing
  // it as the envelope would otherwise produce `undefined` data and, before
  // #559, a 404 page claiming the platform publishes no policy.
  await assert.rejects(
    withFetch(
      async () => new Response("<html>502</html>", { status: 200 }),
      () => requirePrivacyPolicy("es"),
    ),
    LegalDocumentUnavailableError,
  );
});

test("a 200 carrying an error envelope throws", async () => {
  await assert.rejects(
    withFetch(
      async () => json({ data: null, error: { code: "INTERNAL", message: "boom" } }, 200),
      () => requireTerms("en"),
    ),
    LegalDocumentUnavailableError,
  );
});

test("fetchData's other callers still degrade to null on a 5xx", async () => {
  // The variant is narrow ON PURPOSE: the consent capture surfaces read the
  // same endpoint through getPrivacyPolicy and must still get null, which they
  // render as a consent step that cannot be completed. Making them throw would
  // turn an unreachable legal endpoint into a 500 on every Event page.
  const policy = await withFetch(
    async () => json({ data: null, error: { code: "INTERNAL", message: "boom" } }, 500),
    () => getPrivacyPolicy("en"),
  );

  assert.equal(policy, null);
});

test("published, unpublished and unreadable are three answers, not two", async () => {
  const published = await withFetch(async () => policyEnvelope.clone(), () =>
    legalDocumentPublishes("terms", "en"),
  );
  const unpublished = await withFetch(async () => json({ data: null, error: null }, 404), () =>
    legalDocumentPublishes("terms", "en"),
  );
  const unreadable = await withFetch(
    async () => {
      throw new TypeError("fetch failed");
    },
    () => legalDocumentPublishes("terms", "en"),
  );

  assert.equal(published, true);
  assert.equal(unpublished, false);
  // Not false. A caller acting on false would drop a language from the sitemap
  // or move a reader's footer link because one request timed out.
  assert.equal(unreadable, null);
});

test("the published set is read per locale", async () => {
  const locales = await withFetch(
    async (url) =>
      url.endsWith("/en") ? json({ data: null, error: null }, 404) : policyEnvelope.clone(),
    () => publishedLegalLocales("privacy-policy"),
  );

  assert.deepEqual(locales, ["es"]);
});

test("one failed probe makes the whole set unreadable", async () => {
  // All-or-nothing: a partial answer is indistinguishable from a genuine drop.
  const locales = await withFetch(
    async (url) => {
      if (url.endsWith("/en")) throw new TypeError("fetch failed");
      return policyEnvelope.clone();
    },
    () => publishedLegalLocales("privacy-policy"),
  );

  assert.equal(locales, null);
});
