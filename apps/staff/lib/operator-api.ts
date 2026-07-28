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

/** One Ticket Type and how many of it a Ticket Sale is for. */
export type OperatorSaleLine = {
  ticket_type_name: string;
  quantity: number;
};

/** The Event a looked-up Ticket Sale is for. */
export type OperatorSaleEvent = {
  id: string;
  name: string;
  slug: string;
  /** Null on an Event with no schedule; an Online Sale's Event always has one. */
  starts_at: string | null;
  /** The Event's OWN timezone, which interprets its schedule. */
  timezone: string | null;
};

/** The buyer as the Ticket Sale snapshotted them, not as the Customer reads now. */
export type OperatorSaleCustomer = {
  email: string;
  first_name: string;
  last_name: string;
};

/**
 * One Ticket Sale as the operator's lookup by Sale Confirmation reference
 * returns it. Read-only: it exists so an operator can be sure they have the
 * right sale before acting on it.
 */
export type OperatorSaleDetail = {
  id: string;
  confirmation_ref: string;
  /** 'active' or 'reversed'. A reversed sale keeps its reference and is found here too. */
  status: string;
  /** The Sales Channel: 'online', 'in_person' or 'import'. */
  channel: string;
  source: string | null;
  /** The Payment Provider, 'free', cash/transfer, or null where the channel carries none. */
  payment_method: string | null;
  sold_at: string;
  recorded_at: string;
  /** The Sale Reversal's provenance; both null on an active sale. */
  reversed_at: string | null;
  reversed_by: string | null;

  customer: OperatorSaleCustomer;
  ticket_types: OperatorSaleLine[];
  ticket_count: number;

  currency: string;
  /** What the buyer paid, and how the platform split it. All snapshots frozen at sale time. */
  amount_cents: number;
  platform_fee_cents: number;
  fee_iva_cents: number;
  net_proceeds_cents: number;

  event: OperatorSaleEvent;

  /** When this sale's Reversal Window shuts; null on a sale that never had one. */
  reversal_window_closes_at: string | null;
  /** True once the window has shut, and true as well when there was never one. */
  reversal_window_passed: boolean;
};

export type OperatorSaleLookup = {
  sale: OperatorSaleDetail;
  organization: OperatorOrganization;
};

/**
 * Looks a Ticket Sale up by its Sale Confirmation reference, across every
 * Organization. The API matches case-insensitively, so a reference pasted out
 * of a support thread resolves however it was quoted.
 */
export async function fetchOperatorSale(confirmationRef: string): Promise<OperatorSaleLookup> {
  return fetchEventsJSON<OperatorSaleLookup>(
    `/api/operator/sales/${encodeURIComponent(confirmationRef)}`,
  );
}
