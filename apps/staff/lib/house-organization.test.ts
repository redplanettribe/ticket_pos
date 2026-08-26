import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { houseDesignation } from "./house-organization.ts";

// WHAT THESE ASSERT. The card's sentences live in the catalogs (ADR 0041), so
// what is left here is the decision they are built on: which face the card
// shows, and whether the toggle is offered. The API is the gate — the currency
// refusal and the operator-only rule are integration-tested against it — and
// the last test is what makes sure a face cannot reach an operator with
// nothing to say in either language.

test("a designated organization shows its trail", () => {
  assert.deepEqual(
    houseDesignation({
      currency: "USD",
      is_house_organization: true,
      house_designated_by: "operator@example.com",
      house_designated_at: "2026-07-07T12:00:00Z",
    }),
    { kind: "house", by: "operator@example.com", at: "2026-07-07T12:00:00Z" },
  );
});

test("an ordinary USD organization is offered the toggle", () => {
  assert.deepEqual(
    houseDesignation({
      currency: "USD",
      is_house_organization: false,
      house_designated_by: null,
      house_designated_at: null,
    }),
    { kind: "ordinary", blockedByCurrency: null },
  );
});

// The one refusal the card can see coming: the Issuer invoices in USD alone,
// so the toggle names the currency instead of sending a request the API would
// refuse with the same words (ADR 0060).
test("an organization in another currency is told why the toggle is off", () => {
  assert.deepEqual(
    houseDesignation({
      currency: "EUR",
      is_house_organization: false,
      house_designated_by: null,
      house_designated_at: null,
    }),
    { kind: "ordinary", blockedByCurrency: "EUR" },
  );
});

// Clearing is always allowed, so a designation must always be visible — even
// on an Organization whose currency moved away from USD after the fact.
test("a designated organization reads as designated whatever its currency", () => {
  const designation = houseDesignation({
    currency: "EUR",
    is_house_organization: true,
    house_designated_by: "operator@example.com",
    house_designated_at: "2026-07-07T12:00:00Z",
  });
  assert.equal(designation.kind, "house");
});

test("every face has its words in both languages", () => {
  const keys = [
    "houseTitle",
    "houseDescription",
    "houseStatusDesignated",
    "houseStatusOrdinary",
    "houseTrail",
    "houseDesignate",
    "houseClear",
    "houseCurrencyBlocked",
    "houseBadge",
  ];
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { operator: Record<string, string>; errors: { envelope: Record<string, string> } };
    for (const key of keys) {
      assert.ok(catalog.operator[key]?.trim(), `${locale}: operator.${key} is missing or empty`);
    }
    // The API's own refusal, said in the reader's language and naming the
    // currency the way the API does in details.currency.
    const refusal = catalog.errors.envelope.HOUSE_ORGANIZATION_CURRENCY_UNSUPPORTED;
    assert.ok(refusal?.includes("{currency}"), `${locale}: the currency refusal must name the currency`);
  }
});
