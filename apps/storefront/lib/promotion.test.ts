import assert from "node:assert/strict";
import test from "node:test";

import type { PublicPromotion } from "./api.ts";
import { formatPromotionDeadline, promotionSavingsPercent } from "./promotion.ts";

function promotion(overrides: Partial<PublicPromotion> = {}): PublicPromotion {
  return {
    promotional_price_cents: 4990,
    list_price_cents: 7990,
    ends_at: "2026-07-09T12:00:00Z",
    ...overrides,
  };
}

test("the badge states the whole percent a Promotion takes off the List Price", () => {
  // 3000 off 7990 is 37.5% — the buyer is promised 37, and gets a little more.
  assert.equal(promotionSavingsPercent(promotion()), 37);
});

test("the percentage is rounded down so the badge never overpromises", () => {
  // 34.99% must not be advertised as 35% off.
  assert.equal(
    promotionSavingsPercent(promotion({ list_price_cents: 10000, promotional_price_cents: 6501 })),
    34,
  );
});

test("a free Ticket Type under a Promotion is 100% off", () => {
  assert.equal(promotionSavingsPercent(promotion({ promotional_price_cents: 0 })), 100);
});

test("a Promotion worth less than a whole percent gets no badge", () => {
  // Under pass_on the two fee-inclusive amounts can end up a few cents apart.
  assert.equal(
    promotionSavingsPercent(promotion({ list_price_cents: 891, promotional_price_cents: 890 })),
    null,
  );
});

test("no badge is derived from amounts that are not a Promotion", () => {
  // The invariant is Promotional Price < List Price; anything else is a payload
  // this app should stay quiet about rather than render as a negative saving.
  assert.equal(
    promotionSavingsPercent(promotion({ list_price_cents: 5000, promotional_price_cents: 5000 })),
    null,
  );
  assert.equal(
    promotionSavingsPercent(promotion({ list_price_cents: 5000, promotional_price_cents: 6000 })),
    null,
  );
  assert.equal(promotionSavingsPercent(promotion({ list_price_cents: 0 })), null);
});

test("the deadline is read on the Event's clock, not the viewer's", () => {
  // Noon UTC is 7:00 AM in Guayaquil and 2:00 PM in Madrid: the same instant
  // must print as the wall time the organizer scheduled the window in.
  const guayaquil = formatPromotionDeadline(promotion(), "America/Guayaquil");
  const madrid = formatPromotionDeadline(promotion(), "Europe/Madrid");
  assert.match(guayaquil ?? "", /^Thu Jul 9.* 7:00 AM$/);
  assert.match(madrid ?? "", /^Thu Jul 9.* 2:00 PM$/);
});

test("an unusable deadline is not printed", () => {
  assert.equal(formatPromotionDeadline(promotion({ ends_at: "not-a-date" }), "UTC"), null);
});
