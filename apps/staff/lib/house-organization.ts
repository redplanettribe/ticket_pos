/**
 * The House Organization card's logic, kept out of the page so it can be
 * tested under `node --test` (#472, ADR 0060).
 *
 * One question the page would otherwise answer inline: which face the card
 * shows — the designation with its trail, or the toggle to make one, and
 * whether that toggle is worth offering. It re-decides nothing the API
 * decides: the API is the gate, and a refusal from it is shown by code. This
 * exists so the one obvious case — a currency the Issuer does not invoice in —
 * never makes a round trip to be told what the operator can already read.
 */

import type { OperatorOrganization } from "./operator-api";

/**
 * The one currency the platform's Issuer invoices in, and so the one a House
 * Organization may trade in. The API holds the same constant and is the
 * authority; this copy only decides whether to offer the toggle.
 */
export const HOUSE_ORGANIZATION_CURRENCY = "USD";

/**
 * Which face the card shows.
 *
 * `house` when the Organization is designated: the trail is always whole, both
 * halves set together on the API (migration 097). `ordinary` otherwise, and
 * `blockedByCurrency` names the currency when the toggle would be refused, or
 * is null when it may be pressed.
 *
 * A designated Organization reads `house` whatever its currency now is — the
 * currency could have changed after the designation — because clearing is
 * always allowed and the operator must be able to see and undo it.
 */
export type HouseDesignation =
  | { kind: "house"; by: string; at: string }
  | { kind: "ordinary"; blockedByCurrency: string | null };

export function houseDesignation(
  organization: Pick<
    OperatorOrganization,
    "currency" | "is_house_organization" | "house_designated_by" | "house_designated_at"
  >,
): HouseDesignation {
  if (
    organization.is_house_organization &&
    organization.house_designated_by &&
    organization.house_designated_at
  ) {
    return {
      kind: "house",
      by: organization.house_designated_by,
      at: organization.house_designated_at,
    };
  }
  return {
    kind: "ordinary",
    blockedByCurrency:
      organization.currency === HOUSE_ORGANIZATION_CURRENCY ? null : organization.currency,
  };
}
