import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { certificateExpiryWarningVariant } from "./certificate-expiry.ts";

// WHAT THESE ASSERT (#504, ADR 0063 §5). The server says where the
// certificate stands and how many Ecuadorian calendar days remain; the
// pages' one decision is which colour the Certificate Expiry Warning is
// drawn in, and whether at all. The rule is pinned at its edges — the
// seventh day is already destructive, the eighth still a warning, the day
// itself and every day past are destructive — and at its silences: valid,
// none, and an `expiring` block that arrives without a count. The last
// test keeps the banner from reaching an operator with a hole in either
// language.

test("expiring with more than seven days to go is a warning", () => {
  assert.equal(certificateExpiryWarningVariant({ state: "expiring", days_before: 30 }), "warning");
  assert.equal(certificateExpiryWarningVariant({ state: "expiring", days_before: 8 }), "warning");
});

test("expiring with seven days or fewer is destructive", () => {
  assert.equal(certificateExpiryWarningVariant({ state: "expiring", days_before: 7 }), "destructive");
  assert.equal(certificateExpiryWarningVariant({ state: "expiring", days_before: 1 }), "destructive");
  assert.equal(certificateExpiryWarningVariant({ state: "expiring", days_before: 0 }), "destructive");
});

test("expired is destructive whatever the count says", () => {
  assert.equal(certificateExpiryWarningVariant({ state: "expired", days_before: -1 }), "destructive");
  assert.equal(certificateExpiryWarningVariant({ state: "expired", days_before: -400 }), "destructive");
  assert.equal(certificateExpiryWarningVariant({ state: "expired", days_before: null }), "destructive");
});

test("a valid or absent certificate draws nothing", () => {
  assert.equal(certificateExpiryWarningVariant({ state: "valid", days_before: 200 }), null);
  assert.equal(certificateExpiryWarningVariant({ state: "none", days_before: null }), null);
});

test("an expiring block without a count is not guessed at", () => {
  assert.equal(certificateExpiryWarningVariant({ state: "expiring", days_before: null }), null);
});

test("the banner has its words in both languages", () => {
  const keys = [
    "invoicingCertificateExpiringTitle",
    "invoicingCertificateExpiring",
    "invoicingCertificateExpiredTitle",
    "invoicingCertificateExpired",
    "invoicingCertificateExpiryOpenIssuer",
  ];
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { operator: Record<string, string> };
    for (const key of keys) {
      assert.ok(catalog.operator[key], `${locale} is missing operator.${key}`);
    }
  }
});
