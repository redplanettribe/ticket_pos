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

test("a kind, a status and the Recipient Warning are read off the address bar", () => {
  const view = parseOperatorInvoiceListParams({
    page: "3",
    kind: "credit_note",
    status: "abandoned",
    recipient_warning: "true",
  });
  assert.deepEqual(view, {
    page: 3,
    filters: { kind: "credit_note", status: "abandoned", recipientWarningOnly: true },
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
    { page: 1, filters: { kind: "sale", status: "all", recipientWarningOnly: false } },
    { page: 4, filters: { kind: "all", status: "needs_attention", recipientWarningOnly: false } },
    { page: 2, filters: { kind: "manual", status: "authorized", recipientWarningOnly: true } },
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
  const narrowed = { kind: "sale", status: "authorized", recipientWarningOnly: false } as const;
  const query = operatorInvoiceListQueryAfterFilterChange(narrowed, { status: "abandoned" });
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))), {
    page: 1,
    filters: { kind: "sale", status: "abandoned", recipientWarningOnly: false },
  });
});

test("a filter change keeps the filters it did not touch", () => {
  // Ticking the Recipient Warning box on a kind-narrowed view narrows further
  // rather than starting over — the deep link stays narrowed too.
  const query = operatorInvoiceListQueryAfterFilterChange(
    { kind: "sale", status: "all", recipientWarningOnly: true },
    { status: "authorized" },
  );
  assert.deepEqual(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))).filters, {
    kind: "sale",
    status: "authorized",
    recipientWarningOnly: true,
  });
});

test("paging is the one change that keeps its page", () => {
  const query = operatorInvoiceListQuery(2, { kind: "sale", status: "all", recipientWarningOnly: false });
  assert.equal(parseOperatorInvoiceListParams(Object.fromEntries(new URLSearchParams(query.slice(1)))).page, 2);
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
});
