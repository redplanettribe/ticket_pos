import assert from "node:assert/strict";
import test from "node:test";

import {
  DEFAULT_OPERATOR_INVOICE_DIR,
  DEFAULT_OPERATOR_INVOICE_SORT,
  EMPTY_OPERATOR_INVOICE_FILTERS,
  hasActiveOperatorInvoiceFilters,
  operatorInvoiceListQuery,
  operatorInvoiceListQueryAfterFilterChange,
  operatorInvoiceListQueryAfterSortChange,
  parseOperatorInvoiceListParams,
} from "./operator-invoice-list.ts";

/**
 * The order a bare address shows, spelled once: newest Emission Date first,
 * the order this list has always had (#597). Every view below that says
 * nothing about ordering is asserted to be in this one.
 */
const DEFAULT_ORDER = {
  sort: DEFAULT_OPERATOR_INVOICE_SORT,
  dir: DEFAULT_OPERATOR_INVOICE_DIR,
} as const;

/**
 * The Tax Invoices list keeps its narrowing in the URL (#594, spec #593), so
 * what is worth asserting here is what the address bar means: what an absent
 * or unrecognisable param shows, that the builder writes an address the parser
 * reads back as the same view, and that changing a filter returns to page one.
 *
 * Nothing here asserts how a query string is spelled beyond that round trip —
 * the one spelling that IS load-bearing is `recipient_warning=true`, which the
 * Operator Dashboard's deep link writes by hand.
 */

// --- defaults -------------------------------------------------------------

test("a bare address shows the whole list, first page", () => {
  assert.deepEqual(parseOperatorInvoiceListParams({}), {
    page: 1,
    filters: EMPTY_OPERATOR_INVOICE_FILTERS,
    ...DEFAULT_ORDER,
  });
});

test("the unnarrowed view writes no query string at all", () => {
  assert.equal(operatorInvoiceListQuery(1, EMPTY_OPERATOR_INVOICE_FILTERS), "");
});

// --- what the URL can and cannot say --------------------------------------

test("a kind, a status, the Recipient Warning, a search term and a date range are read off the address bar", () => {
  const view = parseOperatorInvoiceListParams({
    page: "3",
    kind: "credit_note",
    status: "abandoned",
    recipient_warning: "true",
    q: "001-001-000000012",
    issued_from: "2026-08-01",
    issued_to: "2026-08-31",
  });
  assert.deepEqual(view, {
    page: 3,
    ...DEFAULT_ORDER,
    filters: {
      kind: "credit_note",
      status: "abandoned",
      recipientWarningOnly: true,
      q: "001-001-000000012",
      issuedFrom: "2026-08-01",
      issuedTo: "2026-08-31",
    },
  });
});

test("a kind this app cannot name widens to every kind rather than emptying the list", () => {
  assert.equal(parseOperatorInvoiceListParams({ kind: "refund" }).filters.kind, "all");
});

test("a status this app cannot name widens to every status", () => {
  assert.equal(parseOperatorInvoiceListParams({ status: "posted" }).filters.status, "all");
});

test("a page that is not a page is page one", () => {
  // Absent, zero, negative and unparseable all land on the first page: a
  // mistyped address still shows the operator a list.
  assert.equal(parseOperatorInvoiceListParams({}).page, 1);
  assert.equal(parseOperatorInvoiceListParams({ page: "0" }).page, 1);
  assert.equal(parseOperatorInvoiceListParams({ page: "-2" }).page, 1);
  assert.equal(parseOperatorInvoiceListParams({ page: "two" }).page, 1);
});

test("a search term survives the address bar verbatim, and blank is no search", () => {
  // Whatever was typed is whatever is matched: the API owns what a term means
  // — case, wildcards, accents — so nothing here rewrites it beyond trimming.
  assert.equal(parseOperatorInvoiceListParams({ q: "Lopez & Cía. 50%" }).filters.q, "Lopez & Cía. 50%");
  // A term that is only whitespace is the whole list, not a search for spaces:
  // a link can carry padding the box never typed.
  assert.equal(parseOperatorInvoiceListParams({ q: "   " }).filters.q, "");
  assert.equal(parseOperatorInvoiceListParams({ q: "  ZEBRA  " }).filters.q, "ZEBRA");
  assert.equal(parseOperatorInvoiceListParams({}).filters.q, "");
});

test("only the literal true narrows to the Recipient Warning", () => {
  // The Operator Dashboard's link spells it this way, and so does the builder.
  assert.equal(parseOperatorInvoiceListParams({ recipient_warning: "true" }).filters.recipientWarningOnly, true);
  assert.equal(parseOperatorInvoiceListParams({ recipient_warning: "1" }).filters.recipientWarningOnly, false);
  assert.equal(parseOperatorInvoiceListParams({ recipient_warning: "false" }).filters.recipientWarningOnly, false);
});

// --- the round trip -------------------------------------------------------

test("every view the builder writes is the view the parser reads back", () => {
  const views = [
    { page: 1, filters: EMPTY_OPERATOR_INVOICE_FILTERS },
    // An order is part of the view too (#597), and combines with a narrowing
    // rather than replacing it: "authorized, this month, largest first" is
    // one link.
    { page: 1, filters: EMPTY_OPERATOR_INVOICE_FILTERS, sort: "total", dir: "desc" },
    { page: 1, filters: EMPTY_OPERATOR_INVOICE_FILTERS, sort: "number", dir: "asc" },
    { page: 6, filters: EMPTY_OPERATOR_INVOICE_FILTERS, sort: "recipient", dir: "desc" },
    // The default column read the other way round is still a choice, and must
    // survive the address bar as one.
    { page: 1, filters: EMPTY_OPERATOR_INVOICE_FILTERS, sort: "date", dir: "asc" },
    { page: 2, filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, status: "authorized", issuedFrom: "2026-08-01" }, sort: "total", dir: "asc" },
    { page: 1, filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, kind: "sale" } },
    { page: 4, filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, status: "needs_attention" } },
    { page: 2, filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, kind: "manual", status: "authorized", recipientWarningOnly: true } },
    // The search is a link like any other narrowing — which is what makes a
    // reload, a bookmark and the back button show the same search result.
    { page: 1, filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, q: "TP-ABCDE234" } },
    { page: 3, filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, kind: "sale", status: "owed", q: "Lopez & Cía. 50%" } },
    // The Emission Date range, and each of its bounds alone (#596): the
    // month-end view an accountant is handed is a link, and so is
    // "everything since July 1st".
    { page: 1, filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, issuedFrom: "2026-08-01", issuedTo: "2026-08-31" } },
    { page: 2, filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, issuedFrom: "2026-07-01" } },
    { page: 1, filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, issuedTo: "2026-07-01" } },
    { page: 5, filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, status: "authorized", q: "ZEBRA", issuedFrom: "2026-08-01", issuedTo: "2026-08-31" } },
  ] as const;
  for (const view of views) {
    const sort = "sort" in view ? view.sort : DEFAULT_OPERATOR_INVOICE_SORT;
    const dir = "dir" in view ? view.dir : DEFAULT_OPERATOR_INVOICE_DIR;
    const query = operatorInvoiceListQuery(view.page, view.filters, sort, dir);
    const parsed = parseOperatorInvoiceListParams(
      Object.fromEntries(new URLSearchParams(query.replace(/^\?/, ""))),
    );
    assert.deepEqual(parsed, { page: view.page, filters: view.filters, sort, dir }, `round trip of ${query || "(no query)"}`);
  }
});

// --- the page reset rule --------------------------------------------------

test("changing a filter returns to the first page", () => {
  const narrowed = { ...EMPTY_OPERATOR_INVOICE_FILTERS, kind: "sale", status: "authorized" } as const;
  const query = operatorInvoiceListQueryAfterFilterChange(narrowed, { status: "abandoned" });
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))), {
    page: 1,
    ...DEFAULT_ORDER,
    filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, kind: "sale", status: "abandoned" },
  });
});

test("submitting a search returns to the first page and keeps the other filters", () => {
  // Pressing Search on page seven of a kind-narrowed view: the term joins the
  // narrowing rather than replacing it, and the page resets, because the
  // searched result has fewer pages than the one being read.
  const query = operatorInvoiceListQueryAfterFilterChange(
    { ...EMPTY_OPERATOR_INVOICE_FILTERS, kind: "sale" },
    { q: "ZEBRA" },
  );
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))), {
    page: 1,
    ...DEFAULT_ORDER,
    filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, kind: "sale", q: "ZEBRA" },
  });
});

test("clearing the search empties the term and leaves the filters standing", () => {
  // Clear is the search's, not the view's: the Kind an operator set survives
  // it. Resetting everything at once is #598's button.
  const query = operatorInvoiceListQueryAfterFilterChange(
    { ...EMPTY_OPERATOR_INVOICE_FILTERS, kind: "sale", status: "owed", q: "ZEBRA" },
    { q: "" },
  );
  assert.equal(query.includes("q="), false, `a cleared search writes no q: ${query}`);
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))).filters, {
    ...EMPTY_OPERATOR_INVOICE_FILTERS,
    kind: "sale",
    status: "owed",
  });
});

test("a filter change keeps the filters it did not touch", () => {
  // Ticking the Recipient Warning box on a kind-narrowed view narrows further
  // rather than starting over — the deep link and the search stay put too.
  const query = operatorInvoiceListQueryAfterFilterChange(
    { ...EMPTY_OPERATOR_INVOICE_FILTERS, kind: "sale", recipientWarningOnly: true, q: "Lopez" },
    { status: "authorized" },
  );
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))).filters, {
    ...EMPTY_OPERATOR_INVOICE_FILTERS,
    kind: "sale",
    status: "authorized",
    recipientWarningOnly: true,
    q: "Lopez",
  });
});

test("paging is the one change that keeps its page", () => {
  const query = operatorInvoiceListQuery(2, {
    ...EMPTY_OPERATOR_INVOICE_FILTERS,
    kind: "sale",
    q: "Lopez",
  });
  const view = parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1))));
  assert.equal(view.page, 2);
  assert.equal(view.filters.q, "Lopez");
});

// --- the empty state's question -------------------------------------------

test("a narrowed view is told apart from the whole list", () => {
  assert.equal(hasActiveOperatorInvoiceFilters(EMPTY_OPERATOR_INVOICE_FILTERS), false);
  assert.equal(hasActiveOperatorInvoiceFilters({ ...EMPTY_OPERATOR_INVOICE_FILTERS, kind: "sale" }), true);
  assert.equal(hasActiveOperatorInvoiceFilters({ ...EMPTY_OPERATOR_INVOICE_FILTERS, status: "owed" }), true);
  assert.equal(
    hasActiveOperatorInvoiceFilters({ ...EMPTY_OPERATOR_INVOICE_FILTERS, recipientWarningOnly: true }),
    true,
  );
  // A search that found nothing must say "nothing matches these filters" and
  // not "the platform has issued no documents" (#595): the two answers lead
  // to different next moves.
  assert.equal(hasActiveOperatorInvoiceFilters({ ...EMPTY_OPERATOR_INVOICE_FILTERS, q: "ZEBRA" }), true);
});

// --- the Emission Date range ----------------------------------------------

/**
 * The range (#596) is two inclusive calendar days on the EMISSION DATE — the
 * day the list's Date column shows — so "everything emitted in August" is a
 * link an operator can hand an accountant. What is worth asserting on this
 * side is only what the address bar carries: the bounds go into the URL as
 * written, come back as the same view, reset the page, and clear together.
 * What a bound MEANS — that an un-issued document falls out, that an inverted
 * range is refused — is the API's, and is asserted against the API.
 */

test("both bounds are read off the address bar, and either may stand alone", () => {
  const both = parseOperatorInvoiceListParams({ issued_from: "2026-08-01", issued_to: "2026-08-31" }).filters;
  assert.equal(both.issuedFrom, "2026-08-01");
  assert.equal(both.issuedTo, "2026-08-31");

  // "Everything since July 1st" is one parameter, not two: an absent bound is
  // an open one and never invents the other end of the range.
  assert.deepEqual(parseOperatorInvoiceListParams({ issued_from: "2026-07-01" }).filters, {
    ...EMPTY_OPERATOR_INVOICE_FILTERS,
    issuedFrom: "2026-07-01",
  });
  assert.deepEqual(parseOperatorInvoiceListParams({ issued_to: "2026-07-01" }).filters, {
    ...EMPTY_OPERATOR_INVOICE_FILTERS,
    issuedTo: "2026-07-01",
  });
  assert.deepEqual(parseOperatorInvoiceListParams({}).filters, EMPTY_OPERATOR_INVOICE_FILTERS);
});

test("a bound the address bar carries is passed on rather than second-guessed", () => {
  // A malformed date is the API's VALIDATION_FAILED, not a filter quietly
  // dropped here: a hand-edited link that says something impossible must be
  // answered, never silently widened into a view it does not name.
  assert.equal(parseOperatorInvoiceListParams({ issued_from: "August" }).filters.issuedFrom, "August");
  // An inverted range likewise travels to the API, which refuses it.
  const inverted = parseOperatorInvoiceListParams({ issued_from: "2026-08-31", issued_to: "2026-08-01" }).filters;
  assert.equal(inverted.issuedFrom, "2026-08-31");
  assert.equal(inverted.issuedTo, "2026-08-01");
  // Padding a link picked up is not a bound.
  assert.equal(parseOperatorInvoiceListParams({ issued_to: "  " }).filters.issuedTo, "");
});

test("setting a date returns to the first page and keeps the other filters", () => {
  // Picking a start date on page seven of a status-narrowed view: the bound
  // joins the narrowing, and the page resets, because the bounded result has
  // fewer pages than the one being read.
  const query = operatorInvoiceListQueryAfterFilterChange(
    { ...EMPTY_OPERATOR_INVOICE_FILTERS, status: "authorized", q: "ZEBRA" },
    { issuedFrom: "2026-08-01" },
  );
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))), {
    page: 1,
    ...DEFAULT_ORDER,
    filters: { ...EMPTY_OPERATOR_INVOICE_FILTERS, status: "authorized", q: "ZEBRA", issuedFrom: "2026-08-01" },
  });
});

test("clearing the dates empties both bounds and leaves the filters standing", () => {
  // Clear is the range's, not the view's: half a range is a different view
  // rather than a cleared one, and the Kind an operator set survives it.
  // Resetting everything at once is #598's button.
  const query = operatorInvoiceListQueryAfterFilterChange(
    { ...EMPTY_OPERATOR_INVOICE_FILTERS, kind: "sale", issuedFrom: "2026-08-01", issuedTo: "2026-08-31" },
    { issuedFrom: "", issuedTo: "" },
  );
  assert.equal(query.includes("issued_"), false, `cleared dates write no bound: ${query}`);
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))).filters, {
    ...EMPTY_OPERATOR_INVOICE_FILTERS,
    kind: "sale",
  });
});

test("a date-bounded view is a narrowed view", () => {
  // A month the platform emitted nothing in must read "nothing matches these
  // filters" and not "the platform has issued no documents" — the first sends
  // the operator to widen the window, the second to stop looking.
  assert.equal(
    hasActiveOperatorInvoiceFilters({ ...EMPTY_OPERATOR_INVOICE_FILTERS, issuedFrom: "2026-08-01" }),
    true,
  );
  assert.equal(
    hasActiveOperatorInvoiceFilters({ ...EMPTY_OPERATOR_INVOICE_FILTERS, issuedTo: "2026-08-31" }),
    true,
  );
});

// --- the order ------------------------------------------------------------

/**
 * The order lives in the URL beside the narrowing (#597), so a colleague
 * handed a link reads the list the way it was read to them, and a reload does
 * not put it back. What is worth asserting here is the address bar's part of
 * that: what a press on a header writes, that the default writes nothing, and
 * that changing the order returns to the first page without disturbing a
 * filter. What each key ORDERS BY is the API's, and is asserted against it.
 */

test("a bare address is the order this list has always had", () => {
  const view = parseOperatorInvoiceListParams({});
  assert.equal(view.sort, "date");
  assert.equal(view.dir, "desc");
});

test("the default order writes no sort into the URL", () => {
  // Every link written before this list could be sorted still names the view
  // it always named, and the whole-list address stays the bare path.
  assert.equal(operatorInvoiceListQuery(1, EMPTY_OPERATOR_INVOICE_FILTERS, "date", "desc"), "");
  // The same column read the other way round IS a choice, and is written.
  assert.equal(
    operatorInvoiceListQuery(1, EMPTY_OPERATOR_INVOICE_FILTERS, "date", "asc"),
    "?sort=date&dir=asc",
  );
});

test("a sort or direction this app cannot name shows the default order", () => {
  // A hand-edited or stale link is read in the order the page has always had
  // rather than in none — and since the default is never written back, the
  // next press drops the value the API would have refused.
  assert.equal(parseOperatorInvoiceListParams({ sort: "colour" }).sort, "date");
  assert.equal(parseOperatorInvoiceListParams({ sort: "recipient_legal_name" }).sort, "date");
  assert.equal(parseOperatorInvoiceListParams({ dir: "sideways" }).dir, "desc");
  assert.equal(parseOperatorInvoiceListParams({ dir: "ASC" }).dir, "desc");
  // A direction with no column names nothing, and reads as the default order.
  assert.equal(parseOperatorInvoiceListParams({ dir: "asc" }).sort, "date");
});

test("pressing the active column flips its direction", () => {
  const query = operatorInvoiceListQueryAfterSortChange(
    EMPTY_OPERATOR_INVOICE_FILTERS,
    "total",
    "desc",
    "total",
  );
  const view = parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1))));
  assert.equal(view.sort, "total");
  assert.equal(view.dir, "asc");
});

test("pressing another column starts it in the direction that column is read in", () => {
  // Largest total first and newest date first; A to Z for the Recipient, and
  // upward for the number, since a run of numbers is read up to find the gap.
  const startsAs = (pressed: "date" | "number" | "total" | "recipient") =>
    parseOperatorInvoiceListParams(
      Object.fromEntries(
        new URLSearchParams(
          operatorInvoiceListQueryAfterSortChange(
            EMPTY_OPERATOR_INVOICE_FILTERS,
            "date",
            "desc",
            pressed,
          ).slice(1),
        ),
      ),
    ).dir;
  assert.equal(startsAs("total"), "desc");
  assert.equal(startsAs("number"), "asc");
  assert.equal(startsAs("recipient"), "asc");
});

test("changing the order returns to the first page and keeps every filter", () => {
  // Page seven of a reordered list holds different documents, so the operator
  // who reordered to bring the largest total into view gets it on the page
  // they are looking at. The narrowing is untouched: an order narrows nothing.
  const narrowed = {
    ...EMPTY_OPERATOR_INVOICE_FILTERS,
    status: "authorized",
    q: "ZEBRA",
    issuedFrom: "2026-08-01",
  } as const;
  const query = operatorInvoiceListQueryAfterSortChange(narrowed, "date", "desc", "total");
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))), {
    page: 1,
    filters: narrowed,
    sort: "total",
    dir: "desc",
  });
});

test("changing a filter keeps the order the operator chose", () => {
  // The order was chosen about the list, not about the documents that happen
  // to be in it, so narrowing must not silently put it back to newest first.
  const query = operatorInvoiceListQueryAfterFilterChange(
    EMPTY_OPERATOR_INVOICE_FILTERS,
    { kind: "sale" },
    "recipient",
    "asc",
  );
  const view = parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1))));
  assert.equal(view.filters.kind, "sale");
  assert.equal(view.sort, "recipient");
  assert.equal(view.dir, "asc");
  assert.equal(view.page, 1);
});

test("an order is not a narrowing", () => {
  // The empty state's question is about the filters alone: no sort can widen
  // to find a document, so a sorted-but-unfiltered empty list must still read
  // "the platform has issued no documents". The reset #598 adds has to clear
  // the sort all the same, since the operator sees one view.
  assert.equal(hasActiveOperatorInvoiceFilters(EMPTY_OPERATOR_INVOICE_FILTERS), false);
});
