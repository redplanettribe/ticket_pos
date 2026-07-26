// The Operator Dashboard's read/write surface, proxied through this app's BFF
// routes under /api/operator/*. Types mirror the Go operator namespace
// (/api/v1/operator/*) field for field so rows render verbatim.
//
// Authority is the API's business: every one of these calls answers 403 for a
// session whose email is not on the platform operator allowlist (ADR 0015). The
// UI merely declines to show the surface at all.

import { fetchEventsJSON } from "./events-api";

/**
 * Platform revenue for one currency. No FX conversion exists anywhere, so the
 * summary is a list of these — one row per currency with data (USD in practice).
 */
export type OperatorCurrencyTotals = {
  currency: string;
  /** Accumulated Platform Fees snapshotted on active Online Sale lines. */
  platform_fee_cents: number;
  /** Accumulated Fee IVA on those same lines. */
  fee_iva_cents: number;
  /** Sum of the POSITIVE Withdrawable Balances — what the platform owes. */
  total_owed_cents: number;
};

export type OperatorSummary = {
  totals: OperatorCurrencyTotals[];
};

export type OperatorOrganizationRow = {
  id: string;
  name: string;
  slug: string;
  currency: string;
  events_count: number;
  /** Signed: negative after a post-settlement reversal. */
  withdrawable_balance_cents: number;
};

export type OperatorPagination = {
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
};

/** The ADR-0006 nested envelope carried inside the transport envelope's `data`. */
export type OperatorOrganizationsPage = {
  data: OperatorOrganizationRow[];
  pagination: OperatorPagination;
};

export type OperatorOrganization = {
  id: string;
  name: string;
  slug: string;
  currency: string;
  created_at: string;
};

export type OperatorEventRow = {
  id: string;
  name: string;
  status: string;
  starts_at: string | null;
  discoverable: boolean;
};

export type OperatorPayout = {
  id: string;
  amount_cents: number;
  /** A calendar day ("YYYY-MM-DD"), not an instant. */
  paid_at: string;
  note: string | null;
  /** The recording operator's email; null on rows predating ADR 0015. */
  recorded_by: string | null;
  created_at: string;
};

export type OperatorOrganizationDetail = {
  organization: OperatorOrganization;
  withdrawable_balance_cents: number;
  events: OperatorEventRow[];
  payouts: OperatorPayout[];
};

export type RecordPayoutBody = {
  amount_cents: number;
  paid_at: string;
  note?: string;
};

export const OPERATOR_ORGANIZATIONS_PAGE_SIZE = 50;

export async function fetchOperatorSummary(): Promise<OperatorSummary> {
  return fetchEventsJSON<OperatorSummary>("/api/operator/summary");
}

export async function fetchOperatorOrganizations(page = 1): Promise<OperatorOrganizationsPage> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(OPERATOR_ORGANIZATIONS_PAGE_SIZE),
  });
  return fetchEventsJSON<OperatorOrganizationsPage>(`/api/operator/organizations?${params.toString()}`);
}

export async function fetchOperatorOrganization(
  organizationId: string,
): Promise<OperatorOrganizationDetail> {
  return fetchEventsJSON<OperatorOrganizationDetail>(
    `/api/operator/organizations/${organizationId}`,
  );
}

/**
 * Records a Payout against an Organization. The API never rejects an amount for
 * exceeding the Withdrawable Balance — by recording time the money has already
 * moved — so the caller is responsible for confirming an over-balance amount
 * with the operator first.
 */
export async function recordOperatorPayout(
  organizationId: string,
  body: RecordPayoutBody,
): Promise<OperatorPayout> {
  return fetchEventsJSON<OperatorPayout>(`/api/operator/organizations/${organizationId}/payouts`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}
