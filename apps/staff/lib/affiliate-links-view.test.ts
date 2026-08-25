import assert from "node:assert/strict";
import test from "node:test";

import {
  DEFAULT_AFFILIATE_SORT,
  defaultDirFor,
  filterAffiliateLinks,
  isAttributionMeasured,
  nextAffiliateSort,
  resolveAffiliateSort,
  sortAffiliateLinks,
  type AffiliateLinkRow,
} from "./affiliate-links-view.ts";

// Rows are built from a handful of fields and a running id, so every test reads
// as the shape it is about rather than a wall of UUIDs. Ids are canonical-form
// UUIDs, as the API sends them, because the last tiebreak compares them as text.
function row(
  id: string,
  overrides: Partial<Omit<AffiliateLinkRow, "id">> = {},
): AffiliateLinkRow {
  return {
    id: `00000000-0000-0000-0000-00000000000${id}`,
    name: `Link ${id}`,
    clicks: 0,
    sales_count: 0,
    net_proceeds_cents: 0,
    created_at: "2026-08-20T10:00:00Z",
    ...overrides,
  };
}

function ids(rows: AffiliateLinkRow[]): string[] {
  return rows.map((r) => r.id.slice(-1));
}

// --- default order ----------------------------------------------------------

test("the list opens on Clicks descending", () => {
  assert.deepEqual(DEFAULT_AFFILIATE_SORT, { field: "clicks", dir: "desc" });
});

test("default order is Clicks desc, and equal Clicks fall back to newest-first then id", () => {
  const links = [
    row("1", { clicks: 3, created_at: "2026-08-01T00:00:00Z" }),
    row("2", { clicks: 9, created_at: "2026-08-02T00:00:00Z" }),
    row("3", { clicks: 3, created_at: "2026-08-03T00:00:00Z" }),
    row("4", { clicks: 3, created_at: "2026-08-03T00:00:00Z" }),
    row("5", { clicks: 0, created_at: "2026-08-05T00:00:00Z" }),
  ];
  const sorted = sortAffiliateLinks(links, "clicks", "desc");
  // 9 first; the three at 3 newest-first, and the two created together by id
  // descending — the server's own order inside a tie band.
  assert.deepEqual(ids(sorted), ["2", "4", "3", "1", "5"]);
});

test("sorting returns a new array and leaves the input untouched", () => {
  const links = [row("1", { clicks: 1 }), row("2", { clicks: 2 })];
  const sorted = sortAffiliateLinks(links, "clicks", "desc");
  assert.notEqual(sorted, links);
  assert.deepEqual(ids(links), ["1", "2"]);
  assert.deepEqual(ids(sorted), ["2", "1"]);
});

// --- natural direction per field -------------------------------------------

test("name opens ascending; every other field opens descending", () => {
  assert.equal(defaultDirFor("name"), "asc");
  assert.equal(defaultDirFor("clicks"), "desc");
  assert.equal(defaultDirFor("sales"), "desc");
  assert.equal(defaultDirFor("net_proceeds"), "desc");
  assert.equal(defaultDirFor("created"), "desc");
});

test("selecting another header starts it in its natural direction", () => {
  assert.deepEqual(nextAffiliateSort({ field: "clicks", dir: "desc" }, "name"), {
    field: "name",
    dir: "asc",
  });
  assert.deepEqual(nextAffiliateSort({ field: "name", dir: "asc" }, "sales"), {
    field: "sales",
    dir: "desc",
  });
  // Even when the previous column had been flipped, the new one starts natural.
  assert.deepEqual(nextAffiliateSort({ field: "clicks", dir: "asc" }, "created"), {
    field: "created",
    dir: "desc",
  });
});

test("selecting the active header flips it, and again flips it back", () => {
  const flipped = nextAffiliateSort({ field: "clicks", dir: "desc" }, "clicks");
  assert.deepEqual(flipped, { field: "clicks", dir: "asc" });
  assert.deepEqual(nextAffiliateSort(flipped, "clicks"), { field: "clicks", dir: "desc" });
  assert.deepEqual(nextAffiliateSort({ field: "name", dir: "asc" }, "name"), {
    field: "name",
    dir: "desc",
  });
});

test("sales, net proceeds and created each sort largest or newest first", () => {
  const links = [
    row("1", { sales_count: 2, net_proceeds_cents: 500, created_at: "2026-08-01T00:00:00Z" }),
    row("2", { sales_count: 7, net_proceeds_cents: 100, created_at: "2026-08-03T00:00:00Z" }),
    row("3", { sales_count: 4, net_proceeds_cents: 900, created_at: "2026-08-02T00:00:00Z" }),
  ];
  assert.deepEqual(ids(sortAffiliateLinks(links, "sales", "desc")), ["2", "3", "1"]);
  assert.deepEqual(ids(sortAffiliateLinks(links, "net_proceeds", "desc")), ["3", "1", "2"]);
  assert.deepEqual(ids(sortAffiliateLinks(links, "created", "desc")), ["2", "3", "1"]);
});

test("a flipped column reverses the values but ties still fall newest-first", () => {
  const links = [
    row("1", { clicks: 3, created_at: "2026-08-01T00:00:00Z" }),
    row("2", { clicks: 9, created_at: "2026-08-02T00:00:00Z" }),
    row("3", { clicks: 3, created_at: "2026-08-03T00:00:00Z" }),
  ];
  assert.deepEqual(ids(sortAffiliateLinks(links, "clicks", "asc")), ["3", "1", "2"]);
  assert.deepEqual(ids(sortAffiliateLinks(links, "created", "asc")), ["1", "2", "3"]);
});

// --- null money fields -----------------------------------------------------

test("null sales and net proceeds never throw and sort as unavailable, after every figure", () => {
  const links = [
    row("1", { sales_count: null, net_proceeds_cents: null, created_at: "2026-08-03T00:00:00Z" }),
    row("2", { sales_count: 0, net_proceeds_cents: 0, created_at: "2026-08-01T00:00:00Z" }),
    row("3", { sales_count: 5, net_proceeds_cents: 250, created_at: "2026-08-02T00:00:00Z" }),
    row("4", { sales_count: null, net_proceeds_cents: null, created_at: "2026-08-04T00:00:00Z" }),
  ];
  assert.deepEqual(ids(sortAffiliateLinks(links, "sales", "desc")), ["3", "2", "4", "1"]);
  assert.deepEqual(ids(sortAffiliateLinks(links, "sales", "asc")), ["2", "3", "4", "1"]);
  assert.deepEqual(ids(sortAffiliateLinks(links, "net_proceeds", "desc")), ["3", "2", "4", "1"]);
  assert.deepEqual(ids(sortAffiliateLinks(links, "net_proceeds", "asc")), ["2", "3", "4", "1"]);
});

test("an all-null list sorts by sales without throwing, in newest-first order", () => {
  const links = [
    row("1", { sales_count: null, net_proceeds_cents: null, created_at: "2026-08-01T00:00:00Z" }),
    row("2", { sales_count: null, net_proceeds_cents: null, created_at: "2026-08-02T00:00:00Z" }),
  ];
  assert.deepEqual(ids(sortAffiliateLinks(links, "sales", "desc")), ["2", "1"]);
  assert.deepEqual(ids(sortAffiliateLinks(links, "net_proceeds", "asc")), ["2", "1"]);
});

// --- measured or not ---------------------------------------------------------

test("attribution is measured unless every link reports no sales figure", () => {
  assert.equal(isAttributionMeasured([]), true);
  assert.equal(isAttributionMeasured([row("1", { sales_count: 0 })]), true);
  assert.equal(
    isAttributionMeasured([row("1", { sales_count: null }), row("2", { sales_count: 3 })]),
    true,
  );
  assert.equal(
    isAttributionMeasured([row("1", { sales_count: null }), row("2", { sales_count: null })]),
    false,
  );
});

test("a sales or net proceeds sort on an unmeasured Event falls back to Clicks desc", () => {
  assert.deepEqual(resolveAffiliateSort({ field: "sales", dir: "asc" }, false), {
    field: "clicks",
    dir: "desc",
  });
  assert.deepEqual(resolveAffiliateSort({ field: "net_proceeds", dir: "desc" }, false), {
    field: "clicks",
    dir: "desc",
  });
  // Everything else is left alone, measured or not.
  assert.deepEqual(resolveAffiliateSort({ field: "name", dir: "desc" }, false), {
    field: "name",
    dir: "desc",
  });
  assert.deepEqual(resolveAffiliateSort({ field: "sales", dir: "asc" }, true), {
    field: "sales",
    dir: "asc",
  });
});

// --- name collation ----------------------------------------------------------

test("name sorts case-insensitively and locale-aware, accents folded", () => {
  const links = [
    row("1", { name: "beto" }),
    row("2", { name: "Ana" }),
    row("3", { name: "Álvaro" }),
    row("4", { name: "carla" }),
  ];
  assert.deepEqual(ids(sortAffiliateLinks(links, "name", "asc")), ["3", "2", "1", "4"]);
  assert.deepEqual(ids(sortAffiliateLinks(links, "name", "desc")), ["4", "1", "2", "3"]);
});

test("names equal but for case tie and fall back to newest-first", () => {
  const links = [
    row("1", { name: "maría", created_at: "2026-08-01T00:00:00Z" }),
    row("2", { name: "María", created_at: "2026-08-02T00:00:00Z" }),
    row("3", { name: "MARÍA", created_at: "2026-08-03T00:00:00Z" }),
  ];
  assert.deepEqual(ids(sortAffiliateLinks(links, "name", "asc")), ["3", "2", "1"]);
  assert.deepEqual(ids(sortAffiliateLinks(links, "name", "desc")), ["3", "2", "1"]);
});

// --- search ------------------------------------------------------------------

// The search reads a name and a code and nothing else; a row here carries the
// URL too, purely so a test can show the URL is never looked at.
function searchRow(name: string, code: string) {
  return { name, code, url: `https://tickets.example/e/summer-fiesta?ref=${code}` };
}

const searchable = [
  searchRow("María's Instagram", "k7pq2m"),
  searchRow("Radio spot", "x3n8ab"),
  searchRow("Newsletter", "m4k7zz"),
];

test("search matches a substring of the name", () => {
  assert.deepEqual(
    filterAffiliateLinks(searchable, "Radio").map((l) => l.code),
    ["x3n8ab"],
  );
});

test("search matches a substring of the code", () => {
  assert.deepEqual(
    filterAffiliateLinks(searchable, "k7").map((l) => l.name),
    ["María's Instagram", "Newsletter"],
  );
});

test("search is case-insensitive", () => {
  assert.deepEqual(
    filterAffiliateLinks(searchable, "maría").map((l) => l.code),
    ["k7pq2m"],
  );
  assert.deepEqual(
    filterAffiliateLinks(searchable, "X3N8AB").map((l) => l.name),
    ["Radio spot"],
  );
});

test("search trims the query before matching", () => {
  assert.deepEqual(
    filterAffiliateLinks(searchable, "  radio spot  ").map((l) => l.code),
    ["x3n8ab"],
  );
});

test("a query equal to the URL's Event slug matches nothing", () => {
  assert.deepEqual(filterAffiliateLinks(searchable, "summer-fiesta"), []);
  assert.deepEqual(filterAffiliateLinks(searchable, "tickets.example"), []);
});

test("an empty or whitespace query returns every link in the same order", () => {
  assert.deepEqual(
    filterAffiliateLinks(searchable, "").map((l) => l.code),
    ["k7pq2m", "x3n8ab", "m4k7zz"],
  );
  assert.deepEqual(
    filterAffiliateLinks(searchable, "   ").map((l) => l.code),
    ["k7pq2m", "x3n8ab", "m4k7zz"],
  );
});
