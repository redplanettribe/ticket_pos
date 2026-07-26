/**
 * Platform Fee math for the staff forms (ADR 0014) — the mirror of the Go
 * `sales.FeeRates` domain function, kept identical so the "Buyers will pay" and
 * "You'll receive" lines an organizer types against match to the cent what
 * checkout will actually charge and withhold.
 *
 * Per-unit, integer cents, rounded half-up, with the Fee IVA taken on the
 * ALREADY-ROUNDED Platform Fee.
 */

/** How an Event handles the Platform Fee and its Fee IVA. */
export type FeeHandling = "pass_on" | "absorb";

/** The configured rates, in basis points (1000 = 10%), as the API reports them. */
export type FeeRates = {
  fee_basis_points: number;
  fee_iva_basis_points: number;
};

/** What the platform withholds from the Organization for one ticket. */
export type Fee = {
  feeCents: number;
  feeIVACents: number;
};

/** A basis-point rate of an integer-cent amount, rounded half-up. */
function applyRate(cents: number, basisPoints: number): number {
  return Math.floor((cents * basisPoints + 5000) / 10000);
}

/**
 * The Platform Fee and Fee IVA withheld for one ticket priced at baseCents.
 * A comp ticket (base 0) yields nothing to withhold.
 */
export function withholdFee(baseCents: number, rates: FeeRates): Fee {
  if (baseCents <= 0) {
    return { feeCents: 0, feeIVACents: 0 };
  }
  const feeCents = applyRate(baseCents, rates.fee_basis_points);
  return { feeCents, feeIVACents: applyRate(feeCents, rates.fee_iva_basis_points) };
}

/** What the Customer pays for one ticket the Organization priced at baseCents. */
export function buyerUnitPriceCents(handling: FeeHandling, baseCents: number, rates: FeeRates): number {
  if (handling !== "pass_on") {
    return baseCents;
  }
  const { feeCents, feeIVACents } = withholdFee(baseCents, rates);
  return baseCents + feeCents + feeIVACents;
}

/** What one ticket priced at baseCents leaves the Organization. */
export function netProceedsUnitCents(handling: FeeHandling, baseCents: number, rates: FeeRates): number {
  const { feeCents, feeIVACents } = withholdFee(baseCents, rates);
  return buyerUnitPriceCents(handling, baseCents, rates) - feeCents - feeIVACents;
}
