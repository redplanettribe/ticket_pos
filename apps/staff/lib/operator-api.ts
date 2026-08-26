// The Operator Dashboard's read/write surface, proxied through this app's BFF
// routes under /api/operator/*. Types mirror the Go operator namespace
// (/api/v1/operator/*) field for field so rows render verbatim.
//
// Authority is the API's business: every one of these calls answers 403 for a
// session whose email is not on the platform operator allowlist (ADR 0015). The
// UI merely declines to show the surface at all.

import { ApiError, fetchEventsJSON } from "./events-api";
import type { QuestionReview, QuestionReviewItem } from "./question-reviews";
import type { TicketQuestion, TicketQuestionOption } from "./ticket-questions";

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
  /** Whether the platform's own entity runs it (#472, ADR 0060). */
  is_house_organization: boolean;
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
  /**
   * The House Organization designation (#472, ADR 0060): whether the
   * platform's own entity runs this Organization, and who designated it and
   * when. The trail is null when it is not one, and never half-set.
   */
  is_house_organization: boolean;
  house_designated_by: string | null;
  house_designated_at: string | null;
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
  /**
   * 'pending' | 'processing' | 'paid' | 'declined' | 'cancelled' | 'failed'.
   * There is no 'approved', and 'processing' is not it: it records a transfer
   * already submitted to a bank, not one an operator intends to make.
   */
  status: string;
  /** The asker's email, so the record outlives their membership. */
  requested_by: string;
  requested_at: string;
  /** The payable balance AS IT STOOD when the ask was made. Never refreshed. */
  payable_balance_cents: number;
  payout_profile: OperatorPayoutRequestProfile;
  /** The answer; all null while the request is outstanding. */
  resolution_reason: string | null;
  resolved_by: string | null;
  resolved_at: string | null;
  payout_id: string | null;
  /**
   * The transfer, null on any request that never went through 'processing' —
   * including one paid instantly, which is legal.
   *
   * transfer_submitted_by is the operator who SENT it, and is a different actor
   * from resolved_by, who ended the request: the two may be different people
   * days apart, and this is how a colleague picking up a three-day-old request
   * knows who to ask. None of these three is a bank detail — the masking rule on
   * payout_profile above is untouched by them.
   */
  transfer_submitted_by: string | null;
  transfer_submitted_at: string | null;
  transfer_reference: string | null;
  /**
   * True once a request has been 'processing' for more than 72 hours. The server
   * computes it AT READ TIME from its own clock and stores it nowhere; there is
   * no reconciler and no automated transition, because there is no API to ask
   * what the bank did (ADR 0026 amendment). It is false in every other status,
   * so a paid request that took four days is not accused after the fact.
   */
  transfer_stale: boolean;
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

/**
 * The smallest slice the listing will return: one row, and the pagination that
 * comes with it. For a caller after the platform-wide total and nothing else —
 * the total is unpaginated (ADR-0006), so it is the same number at any size, and
 * asking for one row rather than fifty is the difference (#194).
 */
export const OPERATOR_ORGANIZATIONS_COUNT_PAGE_SIZE = 1;

export async function fetchOperatorSummary(): Promise<OperatorSummary> {
  return fetchEventsJSON<OperatorSummary>("/api/operator/summary");
}

/**
 * One page of every Organization on the platform, name-ascending.
 *
 * `pageSize` is the caller's, because not every caller wants rows: the API
 * clamps it to [1, 100] and reports the unpaginated total beside whatever slice
 * it returns, so a caller wanting only the count asks for
 * OPERATOR_ORGANIZATIONS_COUNT_PAGE_SIZE.
 */
export async function fetchOperatorOrganizations(
  page = 1,
  pageSize = OPERATOR_ORGANIZATIONS_PAGE_SIZE,
): Promise<OperatorOrganizationsPage> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
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

/**
 * Designates an Organization a House Organization (#472, ADR 0060). No body:
 * the API stamps who and when from the session. Refused with
 * HOUSE_ORGANIZATION_CURRENCY_UNSUPPORTED for an Organization trading in a
 * currency other than USD; no Issuer state is consulted. The result is the
 * Organization as it now stands, trail included.
 */
export async function designateHouseOrganization(
  organizationId: string,
): Promise<OperatorOrganization> {
  return fetchEventsJSON<OperatorOrganization>(`/api/operator/organizations/${organizationId}/house`, {
    method: "PUT",
  });
}

/**
 * Clears the House designation, emptying who and when together. Nothing
 * already owed or issued is touched; clearing what is clear is not a refusal.
 */
export async function undesignateHouseOrganization(
  organizationId: string,
): Promise<OperatorOrganization> {
  return fetchEventsJSON<OperatorOrganization>(`/api/operator/organizations/${organizationId}/house`, {
    method: "DELETE",
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

/**
 * One payout request in full, as the detail view reads it.
 *
 * Named for the server's own `PayoutRequestWhole` (operator/service), because
 * one wire object with two names is one more thing a reader has to hold.
 */
export type OperatorPayoutRequestWhole = {
  id: string;
  amount_cents: number;
  note: string | null;
  status: string;
  requested_by: string;
  requested_at: string;
  /** The payable balance at the moment of asking. Compare with the live one. */
  payable_balance_cents: number;
  payout_profile: OperatorPayoutRequestSnapshot;
  resolution_reason: string | null;
  resolved_by: string | null;
  resolved_at: string | null;
  payout_id: string | null;
  /** Who submitted the transfer, when, and what the bank called it (#186). */
  transfer_submitted_by: string | null;
  transfer_submitted_at: string | null;
  transfer_reference: string | null;
  /** The 72-hour flag, computed by the server at read time. */
  transfer_stale: boolean;
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
  request: OperatorPayoutRequestWhole;
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

/**
 * How many payout requests are waiting to be answered, platform-wide.
 *
 * Its own route rather than a field on the summary, because it is the figure
 * the operator navigation wears on every page (ADR 0026). The Overview reads it
 * for the same reason it exists: a count that leads onward to the work (#193).
 */
export async function fetchOperatorPendingPayoutRequestCount(): Promise<OperatorPendingPayoutRequestCount> {
  return fetchEventsJSON<OperatorPendingPayoutRequestCount>("/api/operator/payout-requests/count");
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
  request: OperatorPayoutRequestWhole;
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
): Promise<OperatorPayoutRequestWhole> {
  return fetchEventsJSON<OperatorPayoutRequestWhole>(
    `/api/operator/payout-requests/${encodeURIComponent(requestId)}/decline`,
    { method: "POST", body: JSON.stringify({ reason }) },
  );
}

/**
 * Records that the transfer has been SUBMITTED and the bank has not confirmed
 * it — the request stays outstanding and NO Payout is written (#186, ADR 0026
 * amendment).
 *
 * That absence is the whole point of the state. A Payout has meant money that
 * MOVED since ADR 0014, and a PayPhone transfer can take 48 hours and can come
 * back rejected, so the ledger learns nothing until somebody finds out what the
 * bank did. Recording a Payout here "so the balance is right sooner" would buy
 * two days of accuracy with a voided-Payout concept every balance in the system
 * would then have to understand.
 *
 * The reference is optional, because PayPhone does not always hand one back. The
 * operator's identity and the instant are the API's — from the staff session and
 * its own clock — and are not sendable from here.
 *
 * Only a `pending` request may be marked: a second operator pressing this is
 * told the transfer was already submitted, and by whom.
 */
export async function markOperatorPayoutRequestProcessing(
  requestId: string,
  transferReference?: string,
): Promise<OperatorPayoutRequestWhole> {
  return fetchEventsJSON<OperatorPayoutRequestWhole>(
    `/api/operator/payout-requests/${encodeURIComponent(requestId)}/processing`,
    {
      method: "POST",
      body: JSON.stringify(transferReference ? { transfer_reference: transferReference } : {}),
    },
  );
}

/**
 * Records that the bank sent the transfer back, with the reason the organizer
 * reads and acts on.
 *
 * NOTHING IN THE LEDGER IS UNDONE, because nothing was ever written to it. That
 * is the payoff of not recording a Payout on submission, and a reader looking
 * for the negating entry should find there is none to write.
 *
 * Only a `processing` request may fail — a transfer nobody submitted cannot have
 * bounced — and failure is terminal: the bank details on a request are a frozen
 * snapshot, so the organizer corrects their payout profile and asks again rather
 * than this one being retried. It is also the correction for a mis-click into
 * processing, recorded with a reason saying so.
 */
export async function markOperatorPayoutRequestFailed(
  requestId: string,
  reason: string,
): Promise<OperatorPayoutRequestWhole> {
  return fetchEventsJSON<OperatorPayoutRequestWhole>(
    `/api/operator/payout-requests/${encodeURIComponent(requestId)}/failed`,
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

/**
 * One Sale Re-addressing record as the operator sees it (#420, ADR 0058): the
 * address the buyer meant, the address the sale carried when it was recorded,
 * who recorded it and when, and how far it has got. `status` is derived by the
 * API and never stored: 'pending' | 'accepted' | 'withdrawn' | 'expired'.
 *
 * IT CARRIES NO LINK AND NO TOKEN, and there is no field for one. The
 * Re-addressing Link goes to the corrected address alone, so the operator
 * cannot complete the acceptance themself.
 */
export type OperatorSaleReAddressing = {
  id: string;
  ticket_sale_id: string;
  confirmation_ref: string;
  status: string;
  /** The sale's address when this was recorded — the wrong one. */
  previous_email: string;
  /** The address the buyer meant, normalised; null once purged at the Event's start. */
  corrected_email: string | null;
  /** The recording operator's email, from their session. */
  operator: string;
  note: string | null;
  requested_at: string;
  accepted_at: string | null;
  withdrawn_at: string | null;
};

/** The lookup's `re_addressing` block: the one pending record (or null) and the accepted history. */
export type OperatorSaleReAddressingBlock = {
  pending: OperatorSaleReAddressing | null;
  accepted: OperatorSaleReAddressing[];
};

export type OperatorSaleLookup = {
  sale: OperatorSaleDetail;
  organization: OperatorOrganization;
  re_addressing: OperatorSaleReAddressingBlock;
  /**
   * The Tax Invoices about this sale (#477, ADR 0060): the Sale Invoice a paid
   * House checkout owed and the Credit Note its reversal owed, oldest first.
   * Empty on every sale outside a House Organization. Each id opens the
   * document detail under /operator/invoicing.
   */
  documents: OperatorInvoiceListItem[];
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

/**
 * A Sale Re-addressing as the operator states it: the address the buyer meant,
 * and an optional note. Who recorded it is never sent — the API takes that from
 * the session.
 */
export type OperatorReAddressBody = {
  email: string;
  note?: string;
};

/**
 * Records a Sale Re-addressing against an active Online Sale (#420, ADR 0058).
 *
 * The API mails the corrected address a Re-addressing Link; nothing moves until
 * that address accepts, and no Payment Provider is called. The result is the
 * pending record, without the link. Recording while one is pending replaces it
 * and kills its link (#423) — recording the SAME address again is how a lost
 * mail is sent again.
 */
export async function reAddressOperatorSale(
  confirmationRef: string,
  body: OperatorReAddressBody,
): Promise<OperatorSaleReAddressing> {
  return fetchEventsJSON<OperatorSaleReAddressing>(
    `/api/operator/sales/${encodeURIComponent(confirmationRef)}/re-address`,
    { method: "POST", body: JSON.stringify(body) },
  );
}

/**
 * Withdraws the pending Sale Re-addressing of a sale (#423, ADR 0058): the
 * record is kept and stamped withdrawn, its link stops working, and nobody is
 * emailed. The API refuses with RE_ADDRESSING_NOTHING_PENDING when nothing is
 * pending. The result is the record as it now stands.
 */
export async function withdrawOperatorSaleReAddressing(
  confirmationRef: string,
): Promise<OperatorSaleReAddressing> {
  return fetchEventsJSON<OperatorSaleReAddressing>(
    `/api/operator/sales/${encodeURIComponent(confirmationRef)}/re-address`,
    { method: "DELETE" },
  );
}

// --- Customer consent, and the Consent Withdrawal an operator records --------
//
// The one surface here that is about a person rather than about money (#271,
// parent #265). An operator holding a withdrawal form that arrived by post — or
// an email to the data-protection address — finds the Customer by their
// address, sees what a withdrawal would actually change, and records it.
//
// It is an OPERATOR surface and could not be an Organization one: Customer
// identity on this platform is global and separate from staff (ADR 0010), so a
// Customer's consents are the platform's relationship with them and no venue
// may inspect or alter the choices of people who also bought somewhere else.
//
// AND IT CAN ONLY WITHDRAW. Nothing below can grant a consent, and that is not
// a property of these functions: the API refuses an affirmative answer in its
// single consent-write path. The types express only what the surface offers.

/** The Customer an address resolves to, enough to be sure it is the right one. */
export type OperatorConsentCustomer = {
  id: string;
  email: string;
  first_name: string;
  last_name: string;
};

/**
 * One optional consent's state.
 *
 * `pending_confirmation` is somebody else's tick — an address typed at a
 * checkout by a person who never proved they owned it. It is denied for sending
 * and unanswered for prompting, and it never expires, so it is a real thing
 * standing against the address that a withdrawal settles.
 */
export type OperatorConsentValue = "granted" | "denied" | "pending_confirmation";

/**
 * What is TRUE NOW about a Customer's consents.
 *
 * NULL MEANS UNANSWERED, which is a different fact from denied and must be
 * shown as the different fact it is: an operator deciding what a form changes
 * must never be shown a refusal the Customer never made.
 */
export type OperatorConsentState = {
  marketing_consent: OperatorConsentValue | null;
  networking_consent: OperatorConsentValue | null;
  /**
   * When the Customer last accepted a Privacy Policy version, null if never.
   * Shown and NOT actionable: policy acceptance is not withdrawable — it is
   * absent from the withdrawal form, it rests on a basis other than consent,
   * and clearing it would re-gate the person rather than free them.
   */
  policy_accepted_at: string | null;
};

/**
 * What one recorded act TOOK AWAY — null on a lookup, which took nothing away.
 *
 * It is not the same question as the state beside it and cannot be derived from
 * it: `denied` reads the same whether somebody just gave something up or was
 * already refusing. It is also what decides whether the Customer was emailed.
 */
export type OperatorConsentWithdrawn = {
  marketing_consent: boolean;
  networking_consent: boolean;
};

export type OperatorCustomerConsent = {
  customer: OperatorConsentCustomer;
  consent: OperatorConsentState;
  withdrew: OperatorConsentWithdrawn | null;
};

/**
 * A withdrawal as the operator states it.
 *
 * Each consent is OMITTED when the artefact did not ask for it — a form asking
 * for one thing takes one thing away, and sending `false` for a consent nobody
 * mentioned would record an answer to a question that was never put. The only
 * value either field may carry is `false`: the API refuses `true`.
 *
 * `request_reference` is required and names the inbound artefact. It is a
 * pointer to evidence held elsewhere rather than evidence itself.
 */
export type OperatorConsentWithdrawalBody = {
  marketing_consent?: false;
  networking_consent?: false;
  request_reference: string;
};

/** Finds a Customer by email and reports their consent state. Writes nothing. */
export async function fetchOperatorCustomerConsent(
  email: string,
): Promise<OperatorCustomerConsent> {
  return fetchEventsJSON<OperatorCustomerConsent>(
    `/api/operator/customers/${encodeURIComponent(email)}/consent`,
  );
}

/**
 * Records a Consent Withdrawal on a Customer's behalf, attributed to the
 * operator who entered it and referenced to the artefact it answers.
 *
 * Writes exactly one consent record and emails the Customer the standard
 * withdrawal confirmation — but only when something actually moved.
 */
export async function recordOperatorConsentWithdrawal(
  email: string,
  body: OperatorConsentWithdrawalBody,
): Promise<OperatorCustomerConsent> {
  return fetchEventsJSON<OperatorCustomerConsent>(
    `/api/operator/customers/${encodeURIComponent(email)}/consent/withdrawal`,
    { method: "POST", body: JSON.stringify(body) },
  );
}

/**
 * One Ticket Question on the Operator's view of an Event (#410, ADR 0056): the
 * staff payload — review columns included — plus the Ticket Type it hangs off,
 * because the Operator arrives by Event and the questions are the Ticket
 * Type's.
 */
export type OperatorTicketQuestionRow = TicketQuestion & {
  ticket_type_id: string;
  ticket_type_name: string;
};

/**
 * Every Ticket Question of one Event, in every review state and retired ones
 * included, so a revoked question stays visible where it was revoked.
 */
export async function fetchOperatorEventTicketQuestions(
  eventId: string,
): Promise<OperatorTicketQuestionRow[]> {
  return fetchEventsJSON<OperatorTicketQuestionRow[]>(
    `/api/operator/events/${encodeURIComponent(eventId)}/ticket-questions`,
  );
}

/**
 * A Revocation: takes an approval back with a reason the Organization is told
 * (ADR 0056). The reason is required by the API and bounded at 500 characters.
 * The question is retired and stops being asked; its Answers stay, and
 * `review_status` stays `approved` because the approval was real. Only an
 * approved, live question can be revoked.
 */
export async function revokeOperatorTicketQuestion(
  questionId: string,
  reason: string,
): Promise<TicketQuestion> {
  return fetchEventsJSON<TicketQuestion>(
    `/api/operator/ticket-questions/${encodeURIComponent(questionId)}/revoke`,
    { method: "POST", body: JSON.stringify({ reason }) },
  );
}

/**
 * The Question Review queue and the Operator's answer (#407, ADR 0056),
 * mirroring /api/v1/operator/question-reviews field for field.
 */

/** One item of a Review on the Operator's detail: the row, and what it names. */
export type OperatorQuestionReviewItem = QuestionReviewItem & {
  /** The whole question, every Option included, so an Option item is read under its question's words. */
  question: OperatorTicketQuestionRow | null;
  /** The Option itself, for an Option item. */
  option: TicketQuestionOption | null;
};

export type OperatorQuestionReviewRow = {
  review: Omit<QuestionReview, "items"> & {
    /** How many questions ride it; Options are not counted. */
    question_count: number;
    /** Empty on a queue row, full on the detail. */
    items: OperatorQuestionReviewItem[];
  };
  organization: { id: string; name: string; slug: string };
  event: {
    id: string;
    name: string;
    /** The instant the Review lapses. */
    starts_at: string | null;
    timezone: string;
  };
};

export type OperatorQuestionReviewQueuePage = {
  data: OperatorQuestionReviewRow[];
  pagination: OperatorPagination;
};

export type OperatorOutstandingQuestionReviewCount = {
  outstanding_count: number;
};

export const OPERATOR_QUESTION_REVIEWS_PAGE_SIZE = 50;

/** Every outstanding Question Review across every organization, OLDEST FIRST. */
export async function fetchOperatorQuestionReviews(
  page = 1,
): Promise<OperatorQuestionReviewQueuePage> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(OPERATOR_QUESTION_REVIEWS_PAGE_SIZE),
  });
  return fetchEventsJSON<OperatorQuestionReviewQueuePage>(
    `/api/operator/question-reviews?${params.toString()}`,
  );
}

/** How many Question Reviews are waiting, platform-wide: the count on the Overview beside the payout requests'. */
export async function fetchOperatorOutstandingQuestionReviewCount(): Promise<OperatorOutstandingQuestionReviewCount> {
  return fetchEventsJSON<OperatorOutstandingQuestionReviewCount>(
    "/api/operator/question-reviews/count",
  );
}

/** One Question Review whole, with every item's question and Option. */
export async function fetchOperatorQuestionReview(
  reviewId: string,
): Promise<OperatorQuestionReviewRow> {
  return fetchEventsJSON<OperatorQuestionReviewRow>(
    `/api/operator/question-reviews/${encodeURIComponent(reviewId)}`,
  );
}

/**
 * The answer: one verdict per item, refusals with a reason. The API checks the
 * body whole and names the item on each refusal; a Review no longer outstanding
 * is QUESTION_REVIEW_NOT_OUTSTANDING with the state it reached.
 */
export async function answerOperatorQuestionReview(
  reviewId: string,
  body: { verdicts: { item_id: string; verdict: "approved" | "refused"; reason?: string }[] },
): Promise<OperatorQuestionReviewRow> {
  return fetchEventsJSON<OperatorQuestionReviewRow>(
    `/api/operator/question-reviews/${encodeURIComponent(reviewId)}/answer`,
    { method: "POST", body: JSON.stringify(body) },
  );
}

/**
 * Which of the SRI's environments the Ecuador Issuer points at (#451, ADR
 * 0059): `test` is pruebas, `production` is producción. The words are the
 * platform's rather than the SRI's digits, and the operator may flip them
 * freely.
 */
export type EcuadorIssuerEnvironment = "test" | "production";

/** The three régimenes the factura schema distinguishes. */
export type EcuadorIssuerRegimen = "general" | "rimpe_contribuyente" | "rimpe_negocio_popular";

export const ECUADOR_ISSUER_ENVIRONMENTS = ["test", "production"] as const satisfies readonly EcuadorIssuerEnvironment[];
export const ECUADOR_ISSUER_REGIMENES = [
  "general",
  "rimpe_contribuyente",
  "rimpe_negocio_popular",
] as const satisfies readonly EcuadorIssuerRegimen[];

/**
 * The Ecuador Issuer as the operator states it: the environment and the SRI
 * details every factura carries. Field names are the SRI's Spanish, which is
 * what the operator reads them off the RUC certificate in.
 */
export type EcuadorIssuerBody = {
  environment: EcuadorIssuerEnvironment;
  ruc: string;
  razon_social: string;
  nombre_comercial: string;
  direccion_matriz: string;
  direccion_establecimiento: string;
  establecimiento: string;
  punto_emision: string;
  obligado_contabilidad: boolean;
  regimen: EcuadorIssuerRegimen;
  agente_retencion: string | null;
};

/**
 * The signing certificate in custody, as the Issuer read shows it (#453, ADR
 * 0059): metadata only. The `.p12` bytes and the password are encrypted on
 * the server and never come back under any name.
 */
export type OperatorEcuadorIssuerCertificate = {
  /** RFC 2253 distinguished name. */
  subject: string;
  /** The RUC found inside the certificate, or `""` when none was. */
  ruc: string;
  not_before: string;
  not_after: string;
  /** Lowercase hex SHA-256 of the DER certificate. */
  fingerprint_sha256: string;
  uploaded_at: string;
};

/** The Ecuador Issuer as stored. */
export type OperatorEcuadorIssuer = EcuadorIssuerBody & {
  id: string;
  country: "ec";
  /** `null` until a certificate has been uploaded. */
  certificate: OperatorEcuadorIssuerCertificate | null;
  /**
   * True only when the certificate names a RUC and it is not the Issuer's — a
   * warning the page shows, never a refusal; the SRI's own check is the final
   * word.
   */
  certificate_ruc_mismatch: boolean;
  /**
   * The details that may no longer change (#455): `ruc` once any Tax Invoice
   * exists in either environment, `establecimiento` and `punto_emision` once
   * a sequence has started under them. The page renders these read-only and
   * says why; a save that changes one is refused with ISSUER_FIELD_FROZEN.
   */
  frozen_fields: EcuadorIssuerFrozenField[];
  created_at: string;
  updated_at: string;
};

/** An Issuer detail the API may report as frozen. */
export type EcuadorIssuerFrozenField = "ruc" | "establecimiento" | "punto_emision";

const ECUADOR_ISSUER_PATH = "/api/operator/invoicing/issuers/ec";
const ECUADOR_ISSUER_CERTIFICATE_PATH = `${ECUADOR_ISSUER_PATH}/certificate`;

/**
 * Reads or writes the Ecuador Issuer, keeping `null` data as a legitimate
 * answer: a platform that has not recorded its Issuer yet is not an error, and
 * the page renders an empty form from it. `fetchEventsJSON` would throw on the
 * null, which is why this does not use it. A `FormData` body is sent as the
 * browser builds it — the multipart boundary is its own to set.
 */
async function requestEcuadorIssuer(
  init?: RequestInit,
  path: string = ECUADOR_ISSUER_PATH,
): Promise<OperatorEcuadorIssuer | null> {
  const isForm = init?.body instanceof FormData;
  const response = await fetch(path, {
    ...(isForm ? {} : { headers: { "Content-Type": "application/json" } }),
    ...init,
  });
  const envelope = (await response.json()) as {
    data: OperatorEcuadorIssuer | null;
    error: { code?: string; message?: string; details?: Record<string, unknown> } | null;
  };
  if (!response.ok || envelope.error) {
    throw new ApiError(envelope.error?.message ?? "Request failed", envelope.error?.code, envelope.error?.details);
  }
  return envelope.data;
}

/** The Ecuador Issuer, or `null` when none has been recorded. */
export async function fetchOperatorEcuadorIssuer(): Promise<OperatorEcuadorIssuer | null> {
  return requestEcuadorIssuer();
}

/** Records the Ecuador Issuer — created on the first save, replaced after. */
export async function saveOperatorEcuadorIssuer(body: EcuadorIssuerBody): Promise<OperatorEcuadorIssuer> {
  const saved = await requestEcuadorIssuer({ method: "PUT", body: JSON.stringify(body) });
  if (saved === null) {
    throw new Error("Empty response");
  }
  return saved;
}

/**
 * Uploads the platform's `.p12` with its password, replacing any certificate
 * already in custody, and returns the Issuer as it now reads. Multipart
 * through the BFF and the API, never a presigned browser upload: the object
 * storage buckets are public and a private key has no business in one.
 */
export async function uploadOperatorEcuadorIssuerCertificate(
  file: File,
  password: string,
): Promise<OperatorEcuadorIssuer> {
  const form = new FormData();
  form.append("file", file);
  form.append("password", password);
  const saved = await requestEcuadorIssuer({ method: "POST", body: form }, ECUADOR_ISSUER_CERTIFICATE_PATH);
  if (saved === null) {
    throw new Error("Empty response");
  }
  return saved;
}

// ---- Tax Invoices (#454, ADR 0059) ------------------------------------

/**
 * Where a Tax Invoice stands with the SRI. `owed`, `needs_attention`,
 * `withdrawn` and `annulled` are the states of a document the platform owes
 * itself — a Sale Invoice or a Credit Note (#473, ADR 0060); a manual Tax
 * Invoice is born `pending` and never sees them.
 */
export type InvoiceStatus =
  | "owed"
  | "pending"
  | "authorized"
  | "not_authorized"
  | "rejected"
  | "needs_attention"
  | "withdrawn"
  | "annulled";

/** Why a document exists: an operator typed it, a paid House checkout owed it, or such a Sale's reversal did. */
export type InvoiceKind = "manual" | "sale" | "credit_note";

/** The IVA rate on a line: the platform's words, not the SRI's codes. */
export type InvoiceIVARate = "15" | "0" | "exento" | "no_objeto";

export const INVOICE_IVA_RATES = ["15", "0", "exento", "no_objeto"] as const satisfies readonly InvoiceIVARate[];

/**
 * The SRI formas de pago, code and label, for the form's select and the RIDE.
 * The list mirrors the backend's `sri.PaymentMethods`; `20` is the default.
 */
export const INVOICE_PAYMENT_METHODS = [
  { code: "01", label: "SIN UTILIZACION DEL SISTEMA FINANCIERO" },
  { code: "15", label: "COMPENSACIÓN DE DEUDAS" },
  { code: "16", label: "TARJETA DE DÉBITO" },
  { code: "17", label: "DINERO ELECTRÓNICO" },
  { code: "18", label: "TARJETA PREPAGO" },
  { code: "19", label: "TARJETA DE CRÉDITO" },
  { code: "20", label: "OTROS CON UTILIZACION DEL SISTEMA FINANCIERO" },
  { code: "21", label: "ENDOSO DE TÍTULOS" },
] as const;

export const INVOICE_PAYMENT_METHOD_DEFAULT = "20";

/** The Recipient as recorded on the invoice. */
export type InvoiceRecipient = {
  tax_id_type: "cedula" | "ruc" | "passport";
  tax_id: string;
  legal_name: string;
  address: string;
  email: string;
};

/**
 * One row of the invoices list. The issue facts — number, environment,
 * emission date, signer — are null until the document is signed (#473): an
 * owed Sale Invoice has none of them yet, and a manual Tax Invoice is signed
 * at birth and never null here.
 */
export type OperatorInvoiceListItem = {
  id: string;
  kind: InvoiceKind;
  country: string;
  environment: EcuadorIssuerEnvironment | null;
  status: InvoiceStatus;
  /** The printed document number, e.g. `001-001-000000012`; null until signed. */
  number: string | null;
  /** Emission date, `YYYY-MM-DD` in the Issuer's country; null until signed. */
  issued_on: string | null;
  issued_at: string | null;
  issued_by: string | null;
  recipient: InvoiceRecipient;
  /** The Ticket Sale a Sale Invoice or Credit Note is about; null on a manual document. */
  ticket_sale_id: string | null;
  sale_confirmation_ref: string | null;
  total_cents: number;
  currency: string;
  /** When the document was parked `needs_attention` (#477); null in every other state. */
  attention_since: string | null;
};

/** One line as recorded, with its arithmetic. */
export type OperatorInvoiceLine = {
  position: number;
  description: string;
  /** A decimal string with up to six decimals. */
  quantity: string;
  unit_price_cents: number;
  discount_cents: number;
  iva_rate: InvoiceIVARate;
  rate_percent: number;
  base_cents: number;
  iva_cents: number;
};

/** One subtotal per IVA rate. */
export type OperatorInvoiceRateTotal = {
  iva_rate: InvoiceIVARate;
  rate_percent: number;
  base_cents: number;
  iva_cents: number;
};

/** The invoice's arithmetic in cents. */
export type OperatorInvoiceTotals = {
  by_rate: OperatorInvoiceRateTotal[];
  subtotal_cents: number;
  discount_cents: number;
  iva_cents: number;
  total_cents: number;
};

/** One message from the SRI, verbatim. */
export type OperatorInvoiceMessage = {
  identifier: string;
  message: string;
  additional_info: string;
  type: string;
};

/** One row of the attempts ledger. */
export type OperatorInvoiceAttempt = {
  id: number;
  operation: "submit" | "query";
  outcome: "received" | "authorized" | "not_authorized" | "rejected" | "error";
  messages: OperatorInvoiceMessage[];
  error: string;
  started_at: string;
  duration_ms: number;
};

/** The Issuer snapshot recorded on the invoice. */
export type OperatorInvoiceIssuerSnapshot = {
  ruc: string;
  razon_social: string;
  nombre_comercial: string;
  direccion_matriz: string;
  direccion_establecimiento: string;
  establecimiento: string;
  punto_emision: string;
  obligado_contabilidad: boolean;
  regimen: EcuadorIssuerRegimen;
  agente_retencion: string | null;
};

/** The SRI numbering and authorization of the invoice. */
export type OperatorInvoiceEcuador = {
  access_key: string;
  cod_doc: string;
  estab: string;
  pto_emi: string;
  secuencial: number;
  ambiente: string;
  authorization_number: string | null;
  authorization_date: string | null;
};

/** One Tax Invoice in full. `issuer` and `ecuador` are null until the document is signed (#473). */
export type OperatorInvoiceDetail = OperatorInvoiceListItem & {
  issuer: OperatorInvoiceIssuerSnapshot | null;
  lines: OperatorInvoiceLine[];
  additional_fields: { name: string; value: string }[];
  payment_method: string;
  payment_method_label: string;
  totals: OperatorInvoiceTotals;
  messages: OperatorInvoiceMessage[];
  ecuador: OperatorInvoiceEcuador | null;
  attempts: OperatorInvoiceAttempt[];
  /** The one IVA rate a platform-priced document was priced under; null on a manual one. */
  iva_rate: InvoiceIVARate | null;
  /** When the authorized document was mailed to the buyer, and when the Drainer next works it. */
  delivered_at: string | null;
  next_attempt_at: string | null;
  /** On a Credit Note: the Sale Invoice it credits and the reversal route that made it owed. */
  credits_invoice_id: string | null;
  reversal_reason: string | null;
  /** The operator who marked the document annulled at the SRI portal, and when (#477); null unless annulled. */
  annulled_by: string | null;
  annulled_at: string | null;
  has_authorization_xml: boolean;
  /**
   * True when the invoice is pending and the SRI holds the document —
   * received, still in processing, or 43/70 on a resend (#455). The page
   * says "check status" rather than showing an error.
   */
  check_status_hint: boolean;
  created_at: string;
  updated_at: string;
};

/** The invoices list — the ADR-0006 nested envelope. */
export type OperatorInvoiceListPage = {
  data: OperatorInvoiceListItem[];
  pagination: OperatorPagination;
};

/** One line as the form submits it. */
export type IssueInvoiceLineBody = {
  description: string;
  quantity: string;
  unit_price_cents: number;
  discount_cents: number;
  iva_rate: InvoiceIVARate;
};

/** The New invoice form as submitted. */
export type IssueInvoiceBody = {
  recipient: InvoiceRecipient;
  lines: IssueInvoiceLineBody[];
  payment_method: string;
  additional_fields: { name: string; value: string }[];
};

const INVOICES_PATH = "/api/operator/invoicing/invoices";

export const OPERATOR_INVOICES_PAGE_SIZE = 50;

/** The list's kind filter: one kind, or every kind. */
export type InvoiceKindFilter = InvoiceKind | "all";

/** A page of Tax Invoices, newest first, narrowed to one kind unless `all` (#477). */
export async function fetchOperatorInvoices(
  page = 1,
  kind: InvoiceKindFilter = "all",
): Promise<OperatorInvoiceListPage> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(OPERATOR_INVOICES_PAGE_SIZE),
  });
  if (kind !== "all") {
    params.set("kind", kind);
  }
  return fetchEventsJSON<OperatorInvoiceListPage>(`${INVOICES_PATH}?${params.toString()}`);
}

// ---- The documents that need attention (#477, ADR 0060) ----------------

/**
 * One queued document: the list row plus the SRI's last messages verbatim —
 * or the platform's own PLATFORM-typed message when the document could not
 * be signed — so the queue says why without a click through.
 */
export type OperatorNeedsAttentionItem = OperatorInvoiceListItem & {
  messages: OperatorInvoiceMessage[];
};

/** The queue — the ADR-0006 nested envelope, longest waiting first. */
export type OperatorNeedsAttentionQueue = {
  data: OperatorNeedsAttentionItem[];
  pagination: OperatorPagination;
};

export type OperatorNeedsAttentionCount = {
  needs_attention_count: number;
};

const NEEDS_ATTENTION_PATH = "/api/operator/invoicing/needs-attention";

/** The documents parked `needs_attention`, of every kind, longest waiting first. */
export async function fetchOperatorNeedsAttention(page = 1): Promise<OperatorNeedsAttentionQueue> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(OPERATOR_INVOICES_PAGE_SIZE),
  });
  return fetchEventsJSON<OperatorNeedsAttentionQueue>(`${NEEDS_ATTENTION_PATH}?${params.toString()}`);
}

/** How many documents need attention: the Operator Dashboard's count, exactly what the queue lists. */
export async function fetchOperatorNeedsAttentionCount(): Promise<OperatorNeedsAttentionCount> {
  return fetchEventsJSON<OperatorNeedsAttentionCount>(`${NEEDS_ATTENTION_PATH}/count`);
}

/**
 * Records that the operator annulled the document by hand at the SRI portal
 * (#477) and returns it as it then stands: `annulled`, with who and when.
 * Nothing is sent to the SRI. Refused with INVOICE_NOT_ANNULLABLE outside
 * `pending` and `needs_attention`, and with INVOICE_NOT_ISSUED on a document
 * never signed. Irreversible.
 */
export async function annulOperatorInvoice(id: string): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(`${INVOICES_PATH}/${encodeURIComponent(id)}/annul`, {
    method: "POST",
  });
}

/** One Tax Invoice in full. */
export async function fetchOperatorInvoice(id: string): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(`${INVOICES_PATH}/${encodeURIComponent(id)}`);
}

/** Issues a Tax Invoice and returns it as it stands when the SRI answered. */
export async function issueOperatorInvoice(body: IssueInvoiceBody): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(INVOICES_PATH, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/** The server's totals for a set of lines — what the form shows as it is filled. */
export async function previewOperatorInvoiceTotals(
  lines: IssueInvoiceLineBody[],
): Promise<OperatorInvoiceTotals> {
  return fetchEventsJSON<OperatorInvoiceTotals>(`${INVOICES_PATH}/totals`, {
    method: "POST",
    body: JSON.stringify({ lines }),
  });
}

/**
 * Asks the SRI again about a non-authorized invoice (#455) and returns it as
 * it then stands. Refused with INVOICE_ALREADY_AUTHORIZED on an authorized one.
 */
export async function checkOperatorInvoice(id: string): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(`${INVOICES_PATH}/${encodeURIComponent(id)}/check`, {
    method: "POST",
  });
}

/**
 * Resends a non-authorized invoice under the same clave de acceso, re-signed
 * with the current certificate (#455), and returns it as it then stands.
 * Refused with INVOICE_ALREADY_AUTHORIZED on an authorized one.
 */
export async function resendOperatorInvoice(id: string): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(`${INVOICES_PATH}/${encodeURIComponent(id)}/resend`, {
    method: "POST",
  });
}

// ---- The documents handed over (#456) ----------------------------------

/** Where the browser downloads a Tax Invoice's signed XML from. */
export function operatorInvoiceSignedXmlUrl(id: string): string {
  return `${INVOICES_PATH}/${encodeURIComponent(id)}/xml`;
}

/** Where the browser downloads the SRI's authorization XML from; only an authorized invoice has one. */
export function operatorInvoiceAuthorizationXmlUrl(id: string): string {
  return `${INVOICES_PATH}/${encodeURIComponent(id)}/authorization-xml`;
}

/** The RIDE page: a print-styled rendering of the invoice. */
export function operatorInvoiceRidePath(id: string): string {
  return `/operator/invoicing/${encodeURIComponent(id)}/ride`;
}
