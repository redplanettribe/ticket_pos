import assert from "node:assert/strict";
import test from "node:test";

import { readFileSync } from "node:fs";

import {
  isPromotionErrorCode,
  PROMOTION_ERROR_CODES,
  promotionState,
  promotionStateBadgeVariant,
  type Promotion,
} from "./promotions.ts";

// The same window table the Go domain test is pinned to
// (backend/internal/catalog/promotion_test.go): half-open, start inclusive,
// end exclusive. The badge an organizer reads must not disagree with the price
// checkout actually charges.
const windowed: Promotion = {
  promotional_price_cents: 500,
  starts_at: "2026-07-10T00:00:00Z",
  ends_at: "2026-07-20T00:00:00Z",
};

const openStart: Promotion = {
  promotional_price_cents: 500,
  starts_at: null,
  ends_at: "2026-07-20T00:00:00Z",
};

test("promotionState reads the window half-open", () => {
  const cases: Array<{ at: string; promotion: Promotion; want: string }> = [
    { at: "2026-07-09T23:59:59Z", promotion: windowed, want: "scheduled" },
    // Start is inclusive: the Promotional Price applies from this instant.
    { at: "2026-07-10T00:00:00Z", promotion: windowed, want: "live" },
    { at: "2026-07-15T12:00:00Z", promotion: windowed, want: "live" },
    // End is exclusive: at the end instant the List Price is back.
    { at: "2026-07-20T00:00:00Z", promotion: windowed, want: "ended" },
    { at: "2026-07-21T00:00:00Z", promotion: windowed, want: "ended" },
    // An absent start is live from whenever it was saved, and still ends.
    { at: "2026-07-01T00:00:00Z", promotion: openStart, want: "live" },
    { at: "2026-07-20T00:00:01Z", promotion: openStart, want: "ended" },
  ];

  for (const { at, promotion, want } of cases) {
    assert.equal(promotionState(promotion, new Date(at)), want, `at ${at}`);
  }
});

test("promotionStateBadgeVariant colours each state distinctly", () => {
  assert.equal(promotionStateBadgeVariant("live"), "success");
  assert.equal(promotionStateBadgeVariant("scheduled"), "warning");
  assert.equal(promotionStateBadgeVariant("ended"), "secondary");
});

test("a Promotion's own refusals are told apart from every other failure", () => {
  // The claim is about routing, not about wording: these four are said inline
  // beside the price, and anything else goes to a toast.
  for (const code of PROMOTION_ERROR_CODES) {
    assert.ok(isPromotionErrorCode(code), code);
  }
  assert.equal(isPromotionErrorCode("VALIDATION_FAILED"), false);
  assert.equal(isPromotionErrorCode(undefined), false);
  assert.equal(isPromotionErrorCode(null), false);
});

test("every Promotion refusal has Spanish and English copy to be said in", () => {
  // The words moved to the catalogs (ADR 0041), so this is where a code named
  // here but worded nowhere shows up — before an organizer meets the API's
  // English in the middle of a Spanish form.
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { errors: { envelope: Record<string, string> } };
    for (const code of PROMOTION_ERROR_CODES) {
      assert.ok(catalog.errors.envelope[code], `${locale}.json is missing ${code}`);
    }
  }
});
