import assert from "node:assert/strict";
import test from "node:test";

import { buyerUnitPriceCents, netProceedsUnitCents, withholdFee, type FeeRates } from "./fees.ts";

// The rates configured at launch: 10% Platform Fee, 15% Fee IVA.
const launchRates: FeeRates = { fee_basis_points: 1000, fee_iva_basis_points: 1500 };

// The same rounding table the Go domain function is pinned to
// (backend/internal/sales/fees_test.go). The two must never disagree: this is
// the arithmetic behind the derived line an organizer prices against.
const roundingTable: Array<{ baseCents: number; feeCents: number; feeIVACents: number }> = [
  { baseCents: 0, feeCents: 0, feeIVACents: 0 },
  { baseCents: 1000, feeCents: 100, feeIVACents: 15 },
  { baseCents: 799, feeCents: 80, feeIVACents: 12 },
  { baseCents: 1, feeCents: 0, feeIVACents: 0 },
  { baseCents: 5, feeCents: 1, feeIVACents: 0 },
  // The IVA is taken on the rounded fee: 9.5¢ → 10¢ → 1.5¢ → 2¢.
  { baseCents: 95, feeCents: 10, feeIVACents: 2 },
  { baseCents: 250, feeCents: 25, feeIVACents: 4 },
  { baseCents: 350, feeCents: 35, feeIVACents: 5 },
  { baseCents: 2550, feeCents: 255, feeIVACents: 38 },
  { baseCents: 1050, feeCents: 105, feeIVACents: 16 },
  { baseCents: 12345, feeCents: 1235, feeIVACents: 185 },
];

test("withholdFee matches the checkout rounding table", () => {
  for (const row of roundingTable) {
    assert.deepEqual(
      withholdFee(row.baseCents, launchRates),
      { feeCents: row.feeCents, feeIVACents: row.feeIVACents },
      `base ${row.baseCents}`,
    );
  }
});

test("pass-on raises the buyer price and leaves the organizer the price they set", () => {
  assert.equal(buyerUnitPriceCents("pass_on", 799, launchRates), 891);
  assert.equal(netProceedsUnitCents("pass_on", 799, launchRates), 799);
});

test("absorb keeps the buyer price and takes the withholding out of it", () => {
  assert.equal(buyerUnitPriceCents("absorb", 799, launchRates), 799);
  assert.equal(netProceedsUnitCents("absorb", 799, launchRates), 707);
});

test("a comp ticket costs nothing and earns nothing in either mode", () => {
  assert.equal(buyerUnitPriceCents("pass_on", 0, launchRates), 0);
  assert.equal(netProceedsUnitCents("absorb", 0, launchRates), 0);
});
