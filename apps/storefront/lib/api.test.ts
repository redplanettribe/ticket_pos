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
  SITEMAP_READ_REVALIDATE_SECONDS,
  listPublicEvents,
  listSitemapEvents,
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
