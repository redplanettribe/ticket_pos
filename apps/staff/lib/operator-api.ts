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
  /**
   * Accumulated Platform Fees: the ones snapshotted on active Online Sale
   * lines, plus the ones kept on reversed sales (see kept_fee_cents).
   */
  platform_fee_cents: number;
  /** Accumulated Fee IVA on those same lines, under the same rule. */
  fee_iva_cents: number;
  /** Sum of the POSITIVE Withdrawable Balances — what the platform owes. */
  total_owed_cents: number;
  /**
   * The part of platform_fee_cents standing on sales an Operator Reversal
   * voided while the platform kept its commission. Already inside the figure
   * above — never add it on — and zero until such a reversal happens.
   */
  kept_fee_cents: number;
  /** The Fee IVA half of that same kept term, inside fee_iva_cents. */
  kept_fee_iva_cents: number;
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

/**
 * The bank detail a LIST row carries: enough to recognise an account, never
 * enough to retype one.
 *
 * The account number arrives ALREADY MASKED from the API — this client never
 * holds the digits for a list, because the queue shows every organization's
 * details at once and is the screen operators screenshot into support threads
 * (ADR 0026). The tax id is absent for the same reason; it is on the request
 * detail, where the factura is raised.
 */
export type OperatorPayoutRequestProfile = {
  bank_name: string;
  /** 'ahorros' or 'corriente', in the Spanish the receiving bank's form uses. */
  account_type: string;
  /** "····4821": four dots and the last four digits. */
  account_number_masked: string;
  account_holder_name: string;
};

/** One payout request as the queue and an organization's history show it. */
export type OperatorPayoutRequestRow = {
  id: string;
  amount_cents: number;
  note: string | null;
  /** 'pending' | 'paid' | 'declined' | 'cancelled'. There is no 'approved'. */
  status: string;
  /** The asker's email, so the record outlives their membership. */
  requested_by: string;
  requested_at: string;
  /** The payable balance AS IT STOOD when the ask was made. Never refreshed. */
  payable_balance_cents: number;
  payout_profile: OperatorPayoutRequestProfile;
  /** The answer; all null while the request is outstanding. */
  decline_reason: string | null;
  resolved_by: string | null;
  resolved_at: string | null;
  payout_id: string | null;
};

export type OperatorOrganizationDetail = {
  organization: OperatorOrganization;
  withdrawable_balance_cents: number;
  /**
   * What this Organization may ask for today: the same figure counting only
   * sales recorded before today in Ecuador with no Reversal Request still open
   * (ADR 0026). Signed, and never larger than the balance above it.
   */
  payable_balance_cents: number;
  events: OperatorEventRow[];
  payouts: OperatorPayout[];
  /** This organization's own asks, newest first — a history, not a work queue. */
  payout_requests: OperatorPayoutRequestRow[];
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

/** One queue row: the ask, and whose it is. */
export type OperatorPayoutRequestQueueItem = {
  request: OperatorPayoutRequestRow;
  organization: OperatorOrganization;
};

/** The ADR-0006 nested envelope for the payout request queue. */
export type OperatorPayoutRequestQueuePage = {
  data: OperatorPayoutRequestQueueItem[];
  pagination: OperatorPagination;
};

/**
 * The snapshot as the request detail returns it: whole, including the account
 * number and the tax id for the factura. This is the only shape in this client
 * that holds a payable account number, and the only screen that shows one.
 */
export type OperatorPayoutRequestSnapshot = {
  bank_name: string;
  account_type: string;
  account_number: string;
  account_holder_name: string;
  tax_id_type: string;
  tax_id_number: string;
};

/** One payout request in full, as the detail view reads it. */
export type OperatorPayoutRequestFull = {
  id: string;
  amount_cents: number;
  note: string | null;
  status: string;
  requested_by: string;
  requested_at: string;
  /** The payable balance at the moment of asking. Compare with the live one. */
  payable_balance_cents: number;
  payout_profile: OperatorPayoutRequestSnapshot;
  decline_reason: string | null;
  resolved_by: string | null;
  resolved_at: string | null;
  payout_id: string | null;
};

/**
 * Everything needed to execute one transfer.
 *
 * The two payable balances are the point of the screen: the one on the request
 * is what the organization could have asked for when it asked, and the one
 * beside it is what it could ask for now. The gap tells an organization that
 * asked for what it had from one that asked for four times as much, and nothing
 * about it gates anything — the operator at the bank decides (ADR 0026).
 */
export type OperatorPayoutRequestDetail = {
  request: OperatorPayoutRequestFull;
  organization: OperatorOrganization;
  withdrawable_balance_cents: number;
  payable_balance_cents: number;
};

/**
 * How many organizations are waiting for an answer, platform-wide. The badge on
 * the operator navigation: a queue's whole value is being noticed by somebody
 * who had not already decided to look (ADR 0026).
 */
export type OperatorPendingPayoutRequestCount = {
  pending_count: number;
};

export const OPERATOR_PAYOUT_REQUESTS_PAGE_SIZE = 50;

/**
 * The outstanding payout requests across every organization, OLDEST FIRST.
 *
 * The order deliberately breaks the newest-first convention the payout and sale
 * histories use: this is a work queue rather than a history, and the oldest
 * unanswered request is the one about to become a complaint (ADR 0026).
 */
export async function fetchOperatorPayoutRequests(
  page = 1,
): Promise<OperatorPayoutRequestQueuePage> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(OPERATOR_PAYOUT_REQUESTS_PAGE_SIZE),
  });
  return fetchEventsJSON<OperatorPayoutRequestQueuePage>(
    `/api/operator/payout-requests?${params.toString()}`,
  );
}

/** One payout request in full, with its organization's live balances. */
export async function fetchOperatorPayoutRequest(
  requestId: string,
): Promise<OperatorPayoutRequestDetail> {
  return fetchEventsJSON<OperatorPayoutRequestDetail>(
    `/api/operator/payout-requests/${encodeURIComponent(requestId)}`,
  );
}

/**
 * Fulfilling a payout request: what ACTUALLY left the bank, the day it did, and
 * an optional note.
 *
 * The same shape RecordPayoutBody has, because the Payout it produces is the
 * same Payout — the request adds a target to aim at, not a different kind of
 * settlement. The amount arrives PRE-FILLED from the request and may be
 * overwritten; whatever is sent is what moved, and the request keeps what was
 * asked (ADR 0026).
 */
export type FulfilPayoutRequestBody = {
  amount_cents: number;
  paid_at: string;
  note?: string;
};

/**
 * What a fulfilment produces: the Payout now in the ledger, and the request it
 * answered.
 *
 * The Payout is indistinguishable from a directly recorded one — it lands on the
 * organization's own payouts page and in its withdrawable balance unchanged, and
 * only the request names it, through payout_id.
 */
export type OperatorPayoutFulfilment = {
  payout: OperatorPayout;
  request: OperatorPayoutRequestFull;
};

/**
 * Records the Payout answering a request and marks the request paid, in ONE
 * transaction on the API side.
 *
 * If somebody answered the request first the whole thing rolls back — NO payout
 * row survives — and this rejects with a message naming the current state, who
 * got there first, and the instruction that matters: if you also transferred the
 * money, record the Payout DIRECTLY against the organization. The
 * compare-and-swap stops a second record, not a second transfer (ADR 0026), so
 * that sentence must reach the operator intact rather than being replaced with a
 * generic failure notice.
 */
export async function fulfilOperatorPayoutRequest(
  requestId: string,
  body: FulfilPayoutRequestBody,
): Promise<OperatorPayoutFulfilment> {
  return fetchEventsJSON<OperatorPayoutFulfilment>(
    `/api/operator/payout-requests/${encodeURIComponent(requestId)}/fulfil`,
    { method: "POST", body: JSON.stringify(body) },
  );
}

/**
 * Declines a request with a reason the organization reads. The reason is
 * required by the API and bounded at 500 characters; nothing moves.
 */
export async function declineOperatorPayoutRequest(
  requestId: string,
  reason: string,
): Promise<OperatorPayoutRequestFull> {
  return fetchEventsJSON<OperatorPayoutRequestFull>(
    `/api/operator/payout-requests/${encodeURIComponent(requestId)}/decline`,
    { method: "POST", body: JSON.stringify({ reason }) },
  );
}

/**
 * What a Platform Operator asserted when they recorded an out-of-band refund:
 * who they are, what the buyer actually got back, whether the platform kept its
 * platform fee and fee IVA, and their note.
 *
 * The money fields are nullable because absent and zero are different answers —
 * they are absent on a free online sale, which had nothing to refund.
 */
export type OperatorReversalMemo = {
  /** The acting operator's email, taken by the API from their staff session. */
  operator: string;
  refunded_amount_cents: number | null;
  platform_fee_kept: boolean | null;
  note: string | null;
};

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
  /**
   * The money memo an Operator Reversal left, null on every sale reversed any
   * other way. Operator-facing only: no organization surface carries it.
   */
  operator_reversal: OperatorReversalMemo | null;

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

/**
 * An Operator Reversal as the operator states it: what the buyer actually got
 * back and whether the platform kept its fee, plus an optional note. Who
 * performed it is never sent — the API takes that from the session.
 *
 * The two money facts travel together and depend on the sale. On a paid sale
 * both are required with no default. On a free online sale both are OMITTED and
 * sending either is refused: it collected nothing, so there was nothing to
 * refund and no fee to keep, and a zero would claim otherwise.
 */
export type OperatorReversalBody = {
  refunded_amount_cents?: number;
  platform_fee_kept?: boolean;
  note?: string;
};

/** The marking's result: the reversed sale and the memo as recorded. */
export type OperatorSaleReversal = {
  ticket_sale_id: string;
  confirmation_ref: string;
  status: string;
  reversed_at: string;
  /** Always "operator" here — the third reversal actor. */
  reversed_by: string;
  operator_reversal: OperatorReversalMemo;
};

/**
 * Records an out-of-band refund against a Ticket Sale, marking it reversed.
 *
 * The payment provider is never called: the refund already happened elsewhere,
 * and this is the platform learning it did. Irreversible — there is no
 * un-reversal — so the caller confirms the consequences first.
 */
export async function reverseOperatorSale(
  confirmationRef: string,
  body: OperatorReversalBody,
): Promise<OperatorSaleReversal> {
  return fetchEventsJSON<OperatorSaleReversal>(
    `/api/operator/sales/${encodeURIComponent(confirmationRef)}/reverse`,
    { method: "POST", body: JSON.stringify(body) },
  );
}
