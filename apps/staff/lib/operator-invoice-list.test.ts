import assert from "node:assert/strict";
import test from "node:test";

import {
  EMPTY_OPERATOR_INVOICE_FILTERS,
  hasActiveOperatorInvoiceFilters,
  operatorInvoiceListQuery,
  operatorInvoiceListQueryAfterFilterChange,
  parseOperatorInvoiceListParams,
} from "./operator-invoice-list.ts";

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
  });
});

test("the unnarrowed view writes no query string at all", () => {
  assert.equal(operatorInvoiceListQuery(1, EMPTY_OPERATOR_INVOICE_FILTERS), "");
});

// --- what the URL can and cannot say --------------------------------------

test("a kind, a status, the Recipient Warning and a search term are read off the address bar", () => {
  const view = parseOperatorInvoiceListParams({
    page: "3",
    kind: "credit_note",
    status: "abandoned",
    recipient_warning: "true",
    q: "001-001-000000012",
  });
  assert.deepEqual(view, {
    page: 3,
    filters: {
      kind: "credit_note",
      status: "abandoned",
      recipientWarningOnly: true,
      q: "001-001-000000012",
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
    { page: 1, filters: { kind: "sale", status: "all", recipientWarningOnly: false, q: "" } },
    { page: 4, filters: { kind: "all", status: "needs_attention", recipientWarningOnly: false, q: "" } },
    { page: 2, filters: { kind: "manual", status: "authorized", recipientWarningOnly: true, q: "" } },
    // The search is a link like any other narrowing — which is what makes a
    // reload, a bookmark and the back button show the same search result.
    { page: 1, filters: { kind: "all", status: "all", recipientWarningOnly: false, q: "TP-ABCDE234" } },
    { page: 3, filters: { kind: "sale", status: "owed", recipientWarningOnly: false, q: "Lopez & Cía. 50%" } },
  ] as const;
  for (const view of views) {
    const query = operatorInvoiceListQuery(view.page, view.filters);
    const parsed = parseOperatorInvoiceListParams(
      Object.fromEntries(new URLSearchParams(query.replace(/^\?/, ""))),
    );
    assert.deepEqual(parsed, view, `round trip of ${query || "(no query)"}`);
  }
});

// --- the page reset rule --------------------------------------------------

test("changing a filter returns to the first page", () => {
  const narrowed = { kind: "sale", status: "authorized", recipientWarningOnly: false, q: "" } as const;
  const query = operatorInvoiceListQueryAfterFilterChange(narrowed, { status: "abandoned" });
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))), {
    page: 1,
    filters: { kind: "sale", status: "abandoned", recipientWarningOnly: false, q: "" },
  });
});

test("submitting a search returns to the first page and keeps the other filters", () => {
  // Pressing Search on page seven of a kind-narrowed view: the term joins the
  // narrowing rather than replacing it, and the page resets, because the
  // searched result has fewer pages than the one being read.
  const query = operatorInvoiceListQueryAfterFilterChange(
    { kind: "sale", status: "all", recipientWarningOnly: false, q: "" },
    { q: "ZEBRA" },
  );
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))), {
    page: 1,
    filters: { kind: "sale", status: "all", recipientWarningOnly: false, q: "ZEBRA" },
  });
});

test("clearing the search empties the term and leaves the filters standing", () => {
  // Clear is the search's, not the view's: the Kind an operator set survives
  // it. Resetting everything at once is #598's button.
  const query = operatorInvoiceListQueryAfterFilterChange(
    { kind: "sale", status: "owed", recipientWarningOnly: false, q: "ZEBRA" },
    { q: "" },
  );
  assert.equal(query.includes("q="), false, `a cleared search writes no q: ${query}`);
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))).filters, {
    kind: "sale",
    status: "owed",
    recipientWarningOnly: false,
    q: "",
  });
});

test("a filter change keeps the filters it did not touch", () => {
  // Ticking the Recipient Warning box on a kind-narrowed view narrows further
  // rather than starting over — the deep link and the search stay put too.
  const query = operatorInvoiceListQueryAfterFilterChange(
    { kind: "sale", status: "all", recipientWarningOnly: true, q: "Lopez" },
    { status: "authorized" },
  );
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))).filters, {
    kind: "sale",
    status: "authorized",
    recipientWarningOnly: true,
    q: "Lopez",
  });
});

test("paging is the one change that keeps its page", () => {
  const query = operatorInvoiceListQuery(2, {
    kind: "sale",
    status: "all",
    recipientWarningOnly: false,
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
