import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  type AcceptanceStanding,
  CUSTOMER_DOCUMENTS,
  CUSTOMER_STANDINGS,
  DEFAULT_STANDING,
  STAFF_DOCUMENT,
  STAFF_STANDINGS,
  acceptanceBrowseBody,
  customerBrowserPath,
  staffBrowserPath,
  standingLabelKey,
  standingTone,
} from "./acceptance-browsers.ts";

// WHAT THESE ASSERT (#565, spec #556, ADR 0067).
//
// The three client-side rules that are easy to get wrong, and one that would be
// a privacy defect rather than a bug:
//
//   * the four states, and which of them each screen offers;
//   * outstanding as the default on both;
//   * NO EMAIL IN A PATH OR A QUERY STRING — the search fragment AND the
//     cursor both go in the posted body, because under keyset paging on
//     `email ASC` the cursor IS an address;
//   * every state has words in both languages.

// --- the vocabulary ---

test("there are four states and 'withdrawn' is not one of them", () => {
  const all = new Set<AcceptanceStanding>([...CUSTOMER_STANDINGS, ...STAFF_STANDINGS]);
  assert.deepEqual([...all].sort(), ["current", "former", "never_seen", "outstanding"]);
});

test("former is offered on the staff screen and on no other", () => {
  // A leaver owes nothing, and a Customer record is never deleted — a ticket
  // sale is a financial record that must reconcile — so there is no departure
  // to observe on the customer side and nothing to guess from.
  assert.ok(STAFF_STANDINGS.includes("former"));
  assert.ok(!CUSTOMER_STANDINGS.includes("former"));
});

test("never seen is a state of its own on both screens", () => {
  // About a third of the customer base has never accepted anything. Folding
  // them into `outstanding` would bury the handful who owe a fresh acceptance.
  assert.ok(CUSTOMER_STANDINGS.includes("never_seen"));
  assert.ok(STAFF_STANDINGS.includes("never_seen"));
});

test("both screens default to outstanding", () => {
  assert.equal(DEFAULT_STANDING, "outstanding");
  assert.ok(CUSTOMER_STANDINGS.includes(DEFAULT_STANDING));
  assert.ok(STAFF_STANDINGS.includes(DEFAULT_STANDING));
});

test("only outstanding reads as something to act on", () => {
  // `never_seen` is the ordinary condition of a third of the customer base and
  // must not be drawn as an alarm; `former` is a settled fact about somebody
  // who has left.
  assert.equal(standingTone("outstanding"), "attention");
  assert.equal(standingTone("current"), "positive");
  assert.equal(standingTone("never_seen"), "muted");
  assert.equal(standingTone("former"), "muted");
});

// --- the documents ---

test("the customer screen asks about both documents and the staff screen about the terms alone", () => {
  // There is exactly one staff gate: everybody who signs into the staff
  // platform accepts the terms as an organizer. Staff accept no privacy policy.
  assert.deepEqual([...CUSTOMER_DOCUMENTS], ["policy", "terms"]);
  assert.equal(STAFF_DOCUMENT, "terms");
});

// --- no email in a URL ---

test("neither browser path carries anything but a document name", () => {
  // A document name is the name of a public agreement, not personal data. It is
  // the ONLY thing either path is allowed to carry.
  assert.equal(customerBrowserPath("policy"), "/api/operator/legal/acceptances/customers/policy");
  assert.equal(customerBrowserPath("terms"), "/api/operator/legal/acceptances/customers/terms");
  assert.equal(staffBrowserPath(), "/api/operator/legal/acceptances/staff/terms");

  for (const path of [customerBrowserPath("policy"), customerBrowserPath("terms"), staffBrowserPath()]) {
    assert.ok(!path.includes("?"), `${path} carries a query string`);
    assert.ok(!path.includes("@"), `${path} carries an address`);
  }
});

test("the search fragment and the cursor both travel in the body", () => {
  const body = acceptanceBrowseBody({
    standing: "outstanding",
    cursor: "YW5hQGV4YW1wbGUuY29t",
    searchEmail: "ana@example.com",
  });
  assert.deepEqual(body, {
    standing: "outstanding",
    cursor: "YW5hQGV4YW1wbGUuY29t",
    search_email: "ana@example.com",
  });
});

test("an absent cursor and an absent search are omitted rather than sent empty", () => {
  // So that "the first page" is ONE thing on the wire and not three.
  assert.deepEqual(acceptanceBrowseBody({ standing: "current" }), { standing: "current" });
  assert.deepEqual(acceptanceBrowseBody({ standing: "current", cursor: null, searchEmail: "" }), {
    standing: "current",
  });
  assert.deepEqual(acceptanceBrowseBody({ standing: "current", cursor: "   ", searchEmail: "  " }), {
    standing: "current",
  });
});

// --- copy ---

test("every state has words in both languages", () => {
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { operator: { legalAcceptances: Record<string, string> } };
    const copy = catalog.operator.legalAcceptances;
    assert.ok(copy, `${locale} has no operator.legalAcceptances`);
    for (const standing of ["current", "outstanding", "never_seen", "former"] as AcceptanceStanding[]) {
      const key = standingLabelKey(standing);
      assert.ok(copy[key], `${locale} is missing operator.legalAcceptances.${key}`);
    }
    for (const key of [
      "customersTitle",
      "customersDescription",
      "staffTitle",
      "staffDescription",
      "filtersHeading",
      "documentPolicy",
      "documentTerms",
      "colPerson",
      "colPolicy",
      "colTerms",
      "searchLabel",
      "searchAction",
      "pageDescription",
      "loadMore",
      "empty",
      "loadFailedTitle",
      "breadcrumbLegalCenter",
    ]) {
      assert.ok(copy[key], `${locale} is missing operator.legalAcceptances.${key}`);
    }
  }
});

test("the three browser refusals have words in both languages", () => {
  // A screen that refused to serve and then said nothing would read as "nobody
  // owes anything", which is the one wrong answer this feature can give.
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { errors: { envelope: Record<string, string> } };
    for (const code of [
      "LEGAL_STANDING_UNKNOWN",
      "LEGAL_STANDING_NOT_AVAILABLE",
      "STAFF_DIGEST_UNAVAILABLE",
    ]) {
      assert.ok(catalog.errors.envelope[code], `${locale} is missing errors.envelope.${code}`);
    }
  }
});
