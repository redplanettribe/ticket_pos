// The Operator Dashboard's read/write surface, proxied through this app's BFF
// routes under /api/operator/*. Types mirror the Go operator namespace
// (/api/v1/operator/*) field for field so rows render verbatim.
//
// Authority is the API's business: every one of these calls answers 403 for a
// session whose email is not on the platform operator allowlist (ADR 0015). The
// UI merely declines to show the surface at all.

import type { AppLocale } from "@ticket-pos/locale";

// Relative WITH the ".ts" extension (the same reason sales-api.ts gives): this
// module is now reachable from a `node --test` unit test — operator-invoice-list.ts
// imports its status vocabulary (#594) — and node's resolver needs the extension.
import { ApiError, fetchEventsJSON } from "./events-api.ts";
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
  /**
   * The platform's SALE_INVOICING_ENABLED flag (#471, ADR 0060), not a fact
   * about this Organization: false hides the House Organization card, whose
   * two verbs would answer 404. Read from the API rather than a frontend
   * environment variable so there is one copy of the answer.
   */
  sale_invoicing_enabled: boolean;
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
   * The Tax Invoices about this sale (#477, ADR 0060; #486, ADR 0061): the
   * Sale Invoice a paid House checkout owed, the Credit Note its reversal
   * owed, and after a reissue the superseded factura, its Credit Note and
   * the corrected one, in chain order, each with its role. Empty on every
   * sale outside a House Organization. Each id opens the document detail
   * under /operator/invoicing.
   */
  documents: OperatorSaleDocument[];
};

/**
 * A document's place in its sale's chain (#486, ADR 0061), derived by the
 * API on every read: `current` is the sale's current Sale Invoice — the one
 * a later reversal credits; `superseded` one a reissue corrected, still
 * authorized; `credit_note` a Credit Note for a reversal or a reissue;
 * `not_current` a Sale Invoice withdrawn or annulled, that the sale no
 * longer has.
 */
export type OperatorDocumentRole = "current" | "superseded" | "credit_note" | "not_current";

/**
 * One document as the Sale lookup lists it (#477, #486): the invoicing list's
 * own row, its role, its links to its neighbours and the reissue's trail —
 * on the corrected factura, the superseded one and the reissue's Credit Note
 * alike — so a buyer's question about their factura is answered from the
 * one page.
 */
export type OperatorSaleDocument = OperatorInvoiceListItem & {
  role: OperatorDocumentRole;
  /** On a corrected Sale Invoice, the factura it corrects. */
  supersedes_invoice_id: string | null;
  /** On a Credit Note, the factura it credits and why: a reversal route or `reissue`. */
  credits_invoice_id: string | null;
  credit_note_reason: string | null;
  /** Who reissued, when and the note; null where no reissue concerns the document. */
  reissued_by: string | null;
  reissued_at: string | null;
  reissue_note: string | null;
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

// --- Customer consent: MOVED IN #566 ----------------------------------------
//
// The Consent Withdrawal an operator records on somebody's behalf (#271) used
// to live here, behind a lookup that found a Customer BY THEIR EMAIL ADDRESS —
// which put an address in a request line, and so into the reverse proxy's
// access log, the browser's history and the Referer header of the next click.
//
// Both are gone. Finding somebody is now the acceptance browsers' search
// (#565), which posts its fragment in a body; reading and acting on them is the
// per-subject consent record (lib/legal-records-api.ts), keyed on an opaque
// UUID. Nothing about the act itself changed: it can still only withdraw, never
// grant, and the refusal is still the platform's single consent-write path's
// rather than any surface's.

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

/**
 * Where the signing certificate stands against its `not_after` (#500, ADR
 * 0063): `none` when there is no certificate, `valid`, `expiring` from 30
 * Ecuadorian calendar days out (the day itself included), `expired` after.
 */
export type OperatorCertificateExpiryState = "none" | "valid" | "expiring" | "expired";

/**
 * The Certificate Expiry Warning as the Issuer read carries it (#500, ADR
 * 0063 §5). The date and the count are the server's — the days are Ecuadorian
 * calendar days, never hours — and both are `null` exactly under `none`. No
 * page counts days from the date.
 */
export type OperatorCertificateExpiry = {
  state: OperatorCertificateExpiryState;
  not_after: string | null;
  /** Zero on the day of expiry, negative once past. */
  days_before: number | null;
};

/** The Ecuador Issuer as stored. */
export type OperatorEcuadorIssuer = EcuadorIssuerBody & {
  id: string;
  country: "ec";
  /** `null` until a certificate has been uploaded. */
  certificate: OperatorEcuadorIssuerCertificate | null;
  /** The Certificate Expiry Warning's state, derived on the server. */
  certificate_expiry: OperatorCertificateExpiry;
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
 *
 * `abandoned` is the third death (#578, ADR 0068) and belongs to any kind:
 * the document WAS sent, the SRI never took it — it refuses the number it
 * carries — and it was therefore never a legal document. Distinct from
 * `annulled`, which says the SRI held the document and the operator disowned
 * it by hand at the portal, and from `withdrawn`, which was never sent at
 * all. The three differ in what a reader may conclude, not merely in why
 * they were entered.
 */
export type InvoiceStatus =
  | "owed"
  | "pending"
  | "authorized"
  | "not_authorized"
  | "rejected"
  | "needs_attention"
  | "withdrawn"
  | "annulled"
  | "abandoned";

/** Every status, for the list's status filter. In the order a document travels. */
export const INVOICE_STATUSES = [
  "owed",
  "pending",
  "authorized",
  "not_authorized",
  "rejected",
  "needs_attention",
  "withdrawn",
  "annulled",
  "abandoned",
] as const satisfies readonly InvoiceStatus[];

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
  /**
   * When the operator abandoned the document, because the SRI refuses its
   * number and never took it (#578, ADR 0068); null in every other state.
   * It is the needs-attention queue's second "waiting since" (#581): an
   * abandoned row's `attention_since` is cleared, and this is the instant
   * its wait for a replacement began — the one the queue orders it by.
   */
  abandoned_at: string | null;
  /**
   * True on an authorized Sale Invoice the SRI warned about — the Recipient's
   * Tax ID does not exist (advertencia 59) or is incorrect (62) — until the
   * document is superseded (#482, ADR 0061). The status is unaffected, and
   * it is always false while SALE_INVOICING_ENABLED is closed.
   */
  recipient_warning: boolean;
  /**
   * On a reissued Sale Invoice, the live corrected one — the list's
   * superseded marker (#486, ADR 0061). Null on every other row, and always
   * null while SALE_INVOICING_ENABLED is closed. The status stays
   * authorized: superseded is a relation, not a state.
   */
  superseded_by_invoice_id: string | null;
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
  credit_note_reason: string | null;
  /** On a Sale Invoice: the Credit Note that credits it (#476); null until one does. */
  credited_by_invoice_id: string | null;
  /** The operator who marked the document annulled at the SRI portal, and when (#477); null unless annulled. */
  annulled_by: string | null;
  annulled_at: string | null;
  /**
   * The abandonment's trail (#578, ADR 0068): the operator who recorded that
   * the SRI never took this document, when, and their optional note ("not
   * registered at the portal, confirmed by phone"). All null unless
   * abandoned, and the note null when none was left. Deliberately not the
   * annulment's pair: a reader who finds `annulled_at` set must be able to
   * conclude a portal annulment happened, and for an abandoned document none
   * did.
   */
  abandoned_by: string | null;
  abandoned_at: string | null;
  abandon_note: string | null;
  /**
   * The Sale Invoice Reissue's chain (#483, ADR 0061): on a corrected Sale
   * Invoice, the factura it supersedes; the row's `superseded_by_invoice_id`
   * is, on a reissued factura, the live corrected one. The current Sale
   * Invoice is the one with neither a successor nor a withdrawn or annulled
   * state.
   */
  supersedes_invoice_id: string | null;
  /** Who reissued, when and the note — on the corrected factura, the superseded one and the reissue's Credit Note alike. */
  reissued_by: string | null;
  reissued_at: string | null;
  reissue_note: string | null;
  /**
   * On a Sale Invoice owed by a Sale Invoice Backfill (#509, ADR 0064): the
   * operator who owed it and when. Both null on a document born at checkout.
   */
  backfilled_by: string | null;
  backfilled_at: string | null;
  /**
   * The SRI refused this document for the NUMBER it carries — error 45,
   * "secuencial registrado" — and not for anything in it (#576, ADR 0068).
   * It is what tells the one refusal no resend can mend from a schema error
   * or a bad Tax ID, which park the document in the same status and would
   * otherwise read identically. Derived by the API from the authority's
   * stored messages, so a document refused long before this field existed
   * answers exactly as one refused after it.
   */
  refused_by_number: boolean;
  has_authorization_xml: boolean;
  /**
   * The two hints about who holds the document (#455, tightened by #516),
   * never both true. `check_status_hint` is true when the SRI really holds
   * it — a submit came back received (RECIBIDA, or 43/70 on a resend) — and
   * it is still undecided: the page says "check status" rather than showing
   * an error. `resend_hint` is its opposite: the SRI answered that it has no
   * record of the clave and never took the document, so sending the same
   * bytes again is the fix. A document whose ledger says neither shows
   * neither banner.
   */
  check_status_hint: boolean;
  resend_hint: boolean;
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

/** The documents' BFF path. Exported for lib/operator-invoice-list.ts (#594). */
export const OPERATOR_INVOICES_PATH = "/api/operator/invoicing/invoices";

export const OPERATOR_INVOICES_PAGE_SIZE = 50;

/** The list's kind filter: one kind, or every kind. */
export type InvoiceKindFilter = InvoiceKind | "all";

/** The list's status filter: one status, or every status (#578). */
export type InvoiceStatusFilter = InvoiceStatus | "all";

// A page of Tax Invoices — narrowed to one kind (#477), one status (#578) or
// the documents carrying a Recipient Warning (#482) — is fetched by
// lib/operator-invoice-list.ts, which owns the list's filters record and
// builds the query for the address bar and the request from one place (#594).

// ---- The Recipient Warnings (#482, ADR 0061) ----------------------------

export type OperatorRecipientWarningCount = {
  recipient_warning_count: number;
};

/**
 * How many authorized Sale Invoices the SRI warned about and that have not
 * been superseded: the Operator Dashboard's count beside the needs_attention
 * one, and exactly what the list's warning filter finds. Rejected with 404
 * SALE_INVOICING_UNAVAILABLE while the feature is closed — the caller reads
 * that as "show no count".
 */
export async function fetchOperatorRecipientWarningCount(): Promise<OperatorRecipientWarningCount> {
  return fetchEventsJSON<OperatorRecipientWarningCount>("/api/operator/invoicing/recipient-warnings/count");
}

// ---- The Uninvoiced House Sales (#507-#509, ADR 0064) ------------------

/**
 * One Uninvoiced House Sale: a paid, active Online Sale of an Organization
 * that is House now, with no Reversal Request in flight and no Sale Invoice
 * row of any status. The buyer's Tax ID is whatever the sale recorded —
 * absent on a sale taken before it was asked for.
 */
export type OperatorUninvoicedHouseSale = {
  ticket_sale_id: string;
  confirmation_ref: string;
  sold_at: string;
  organization_id: string;
  organization_name: string;
  event_id: string;
  event_name: string;
  buyer_name: string;
  buyer_tax_id_type: "cedula" | "ruc" | "passport" | null;
  buyer_tax_id_number: string | null;
  total_cents: number;
  currency: string;
};

/** The Uninvoiced House Sales, oldest sale first — the ADR-0006 nested envelope. */
export type OperatorUninvoicedHouseSalePage = {
  data: OperatorUninvoicedHouseSale[];
  pagination: OperatorPagination;
};

export type OperatorUninvoicedHouseSaleCount = {
  uninvoiced_house_sale_count: number;
};

/** Why the backfill refused one sale. */
export type BackfillRefusalCode = "not_a_candidate" | "unsupported_sale";

/** The Sale Invoice Backfill's outcome, in request order. */
export type OperatorBackfillResult = {
  owed: { ticket_sale_id: string; invoice_id: string }[];
  refused: { ticket_sale_id: string; code: BackfillRefusalCode }[];
};

const UNINVOICED_SALES_PATH = "/api/operator/invoicing/uninvoiced-sales";

/**
 * A page of Uninvoiced House Sales platform-wide, oldest first (#507). 404
 * SALE_INVOICING_UNAVAILABLE while the feature is closed.
 */
export async function fetchOperatorUninvoicedHouseSales(page = 1): Promise<OperatorUninvoicedHouseSalePage> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(OPERATOR_INVOICES_PAGE_SIZE),
  });
  return fetchEventsJSON<OperatorUninvoicedHouseSalePage>(`${UNINVOICED_SALES_PATH}?${params.toString()}`);
}

/**
 * How many Uninvoiced House Sales there are: the invoicing list's count
 * beside the Recipient Warning one (#509), exactly what the page lists. 404
 * SALE_INVOICING_UNAVAILABLE while the feature is closed — the caller reads
 * that as "show no count".
 */
export async function fetchOperatorUninvoicedHouseSaleCount(): Promise<OperatorUninvoicedHouseSaleCount> {
  return fetchEventsJSON<OperatorUninvoicedHouseSaleCount>(`${UNINVOICED_SALES_PATH}/count`);
}

/**
 * The Sale Invoice Backfill (#508, ADR 0064): owes an ordinary Sale Invoice
 * to each selected sale that is still a candidate and kicks the Drainer.
 * Answers 200 with the owed and refused sales whenever the request itself
 * is valid; 1..200 ids, else 400 VALIDATION_FAILED. A page holds 50, so a
 * page-wide selection is always within the bound.
 */
export async function backfillOperatorUninvoicedHouseSales(ticketSaleIds: string[]): Promise<OperatorBackfillResult> {
  return fetchEventsJSON<OperatorBackfillResult>(`${UNINVOICED_SALES_PATH}/backfill`, {
    method: "POST",
    body: JSON.stringify({ ticket_sale_ids: ticketSaleIds }),
  });
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

/**
 * The needs-attention queue, longest waiting first: every kind parked
 * `needs_attention`, and — since #581 — every `abandoned` Sale Invoice with
 * no live successor whose Ticket Sale still stands. A union of two
 * conditions, not one status: a Sale can sit abandoned-and-unreplaced
 * between Abandon (#578) and Issue again (#580), and this is where it is
 * seen. It self-clears when a replacement is owed.
 */
export async function fetchOperatorNeedsAttention(page = 1): Promise<OperatorNeedsAttentionQueue> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(OPERATOR_INVOICES_PAGE_SIZE),
  });
  return fetchEventsJSON<OperatorNeedsAttentionQueue>(`${NEEDS_ATTENTION_PATH}?${params.toString()}`);
}

/**
 * How many documents need attention: the Operator Dashboard's badge, and
 * exactly what the queue lists — the same union, so the two can never
 * disagree (#477, #581).
 */
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
  return fetchEventsJSON<OperatorInvoiceDetail>(`${OPERATOR_INVOICES_PATH}/${encodeURIComponent(id)}/annul`, {
    method: "POST",
  });
}

/**
 * Records that the SRI never took the document and never will (#578, ADR
 * 0068), and returns it as it then stands: `abandoned`, with who, when and
 * the optional note. Nothing is sent to the SRI, and the document keeps its
 * number, clave, bytes and every attempt — the secuencial stays consumed and
 * is never handed out again.
 *
 * Requires a fresh Check status immediately beforehand, so the decision
 * rests on the SRI's own current answer: without one it is refused with
 * INVOICE_CHECK_NOT_FRESH, whose remedy is to press Check status and try
 * again. Also refused with INVOICE_NOT_REFUSED_BY_NUMBER on a document the
 * SRI refused for any other reason, INVOICE_NOT_ABANDONABLE outside
 * `needs_attention`, `rejected` and `not_authorized`, and by the shared
 * codes every finished document answers — INVOICE_ALREADY_AUTHORIZED,
 * INVOICE_ABANDONED, INVOICE_ANNULLED, INVOICE_WITHDRAWN,
 * INVOICE_NOT_ISSUED. Irreversible.
 */
export async function abandonOperatorInvoice(id: string, note: string | null): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(`${OPERATOR_INVOICES_PATH}/${encodeURIComponent(id)}/abandon`, {
    method: "POST",
    body: JSON.stringify({ note }),
  });
}

/** The corrected Recipient as the reissue form submits it (#483). No email: the Sale's is taken. */
export type ReissueInvoiceBody = {
  recipient: {
    tax_id_type: InvoiceRecipient["tax_id_type"];
    tax_id: string;
    legal_name: string;
    address: string;
  };
  note: string | null;
};

/**
 * Reissues an authorized Sale Invoice to a corrected Recipient (#483, ADR
 * 0061): owes a Credit Note against it and a corrected Sale Invoice in one
 * act, and returns the CORRECTED document — a new id — as it stands, owed.
 * Refused with INVOICE_MANUAL_NOT_REISSUABLE, CREDIT_NOTE_NOT_REISSUABLE,
 * INVOICE_NOT_AUTHORIZED, INVOICE_SALE_REVERSED, REISSUE_IN_FLIGHT,
 * INVOICE_SUPERSEDED or INVOICE_ALREADY_CREDITED, each by code, and with
 * VALIDATION_FAILED naming the fields.
 */
export async function reissueOperatorInvoice(id: string, body: ReissueInvoiceBody): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(`${OPERATOR_INVOICES_PATH}/${encodeURIComponent(id)}/reissue`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/**
 * Owes the Ticket Sale a FRESH Sale Invoice to replace a terminally dead one
 * (#580, ADR 0068), and returns the REPLACEMENT — a new id — as it stands,
 * owed and unsigned. Offered on an `abandoned` document, whose number the
 * SRI refuses and never took, and on an `annulled` one, disowned by hand at
 * the portal; both leave the Sale with no factura and, until this, nothing
 * that would ever owe it another.
 *
 * The replacement carries the dead document's lines, amounts and Recipient
 * unchanged — this corrects nothing, unlike a reissue — and no Credit Note
 * is owed, because a document the SRI never authorized has nothing to
 * cancel. The Sale Invoice Drainer signs it on a later round under a FRESH
 * secuencial; the abandoned number stays consumed and is never handed out
 * again. Refused with INVOICE_MANUAL_NOT_ISSUABLE_AGAIN,
 * CREDIT_NOTE_NOT_ISSUABLE_AGAIN, INVOICE_NOT_TERMINALLY_DEAD,
 * INVOICE_SALE_REVERSED or INVOICE_ALREADY_REPLACED, each by code.
 */
export async function issueOperatorInvoiceAgain(id: string, note: string | null): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(`${OPERATOR_INVOICES_PATH}/${encodeURIComponent(id)}/issue-again`, {
    method: "POST",
    body: JSON.stringify({ note }),
  });
}

/** One Tax Invoice in full. */
export async function fetchOperatorInvoice(id: string): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(`${OPERATOR_INVOICES_PATH}/${encodeURIComponent(id)}`);
}

/** Issues a Tax Invoice and returns it as it stands when the SRI answered. */
export async function issueOperatorInvoice(body: IssueInvoiceBody): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(OPERATOR_INVOICES_PATH, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/** The server's totals for a set of lines — what the form shows as it is filled. */
export async function previewOperatorInvoiceTotals(
  lines: IssueInvoiceLineBody[],
): Promise<OperatorInvoiceTotals> {
  return fetchEventsJSON<OperatorInvoiceTotals>(`${OPERATOR_INVOICES_PATH}/totals`, {
    method: "POST",
    body: JSON.stringify({ lines }),
  });
}

/**
 * Asks the SRI again about a non-authorized invoice (#455) and returns it as
 * it then stands. Refused with INVOICE_ALREADY_AUTHORIZED on an authorized
 * one, INVOICE_ANNULLED on an annulled one, INVOICE_WITHDRAWN on a withdrawn
 * one, and INVOICE_NOT_ISSUED on one still owed and unsigned.
 */
export async function checkOperatorInvoice(id: string): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(`${OPERATOR_INVOICES_PATH}/${encodeURIComponent(id)}/check`, {
    method: "POST",
  });
}

/**
 * Resends a non-authorized invoice under the same clave de acceso, re-signed
 * with the current certificate (#455), and returns it as it then stands.
 * Refused with INVOICE_ALREADY_AUTHORIZED on an authorized one,
 * INVOICE_ANNULLED on an annulled one, INVOICE_WITHDRAWN on a withdrawn one,
 * and INVOICE_NOT_ISSUED on one still owed and unsigned.
 */
export async function resendOperatorInvoice(id: string): Promise<OperatorInvoiceDetail> {
  return fetchEventsJSON<OperatorInvoiceDetail>(`${OPERATOR_INVOICES_PATH}/${encodeURIComponent(id)}/resend`, {
    method: "POST",
  });
}

// ---- The documents handed over (#456) ----------------------------------

/** Where the browser downloads a Tax Invoice's signed XML from. */
export function operatorInvoiceSignedXmlUrl(id: string): string {
  return `${OPERATOR_INVOICES_PATH}/${encodeURIComponent(id)}/xml`;
}

/** Where the browser downloads the SRI's authorization XML from; only an authorized invoice has one. */
export function operatorInvoiceAuthorizationXmlUrl(id: string): string {
  return `${OPERATOR_INVOICES_PATH}/${encodeURIComponent(id)}/authorization-xml`;
}

/**
 * Where the browser downloads the RIDE (PDF) from (#494, #495, ADR 0062);
 * only an authorized document has one, of every kind. Rendered by the API
 * on each request from the stored document, never kept — the same bytes
 * the buyer receives.
 */
export function operatorInvoiceRideUrl(id: string): string {
  return `${OPERATOR_INVOICES_PATH}/${encodeURIComponent(id)}/ride`;
}

// ---- The Legal Center (#561, spec #556) --------------------------------
//
// The platform's own agreements — the Privacy Policy and the Términos y
// Condiciones — and the ONE MUTABLE DRAFT of each. The draft is held by the API
// rather than by this browser, so a closed tab does not lose an afternoon.
//
// NOTHING HERE PUBLISHES. Saving a draft changes no page a reader can see and
// re-gates nobody; the publication is #563.

/** The two documents the Legal Center can draft. */
export type OperatorLegalDocument = "policy" | "terms";

/** One artifact across every language it is written in: the row of the editor's grid. */
export type OperatorLegalArtifact = {
  slug: string;
  /**
   * Its position in the fingerprint preimage (#541). Sent BY the API and never
   * back to it — the save call takes a list whose ORDER is the ordinal, so the
   * two can never disagree.
   */
  ordinal: number;
  /** Language token → text. A language with no entry is a cell nobody has written. */
  bodies: Partial<Record<AppLocale, string>>;
};

/** The currently published edition of one document: what the draft is written against. */
export type OperatorLegalEdition = {
  version_id: string;
  label: string;
  /** YYYY-MM-DD. */
  effective_date: string;
  content_hash: string;
  /** The languages this edition actually publishes, read from its rows (#558). */
  locales: AppLocale[];
  artifacts: OperatorLegalArtifact[];
};

/** The one mutable draft of one document. */
export type OperatorLegalDraft = {
  /**
   * False when nothing has been saved: the draft handed back is then a copy of
   * the published edition, which is also exactly what a discard produces.
   */
  stored: boolean;
  base_version_id: string;
  /** False when somebody published underneath this draft since it was opened. */
  base_is_current: boolean;
  /** The EXPLICIT set of languages the draft intends to publish in. */
  published_locales: AppLocale[];
  artifacts: OperatorLegalArtifact[];
  updated_by: string;
  updated_at: string | null;
  /**
   * What the operator has LOOKED AT (#562), which #563 turns into publish
   * preconditions. All of it is reported against the draft AS IT NOW STANDS: a
   * preview of words that have since been rewritten is not listed, and a diff
   * seen against an edition that is no longer current does not count.
   */
  previewed: OperatorLegalPreviewedCell[];
  /** The cells still to be looked at — the draft's own, in the languages it publishes. */
  preview_gaps: OperatorLegalCellRef[];
  previewed_all: boolean;
  seen_diff: boolean;
  diff_seen_by: string;
  diff_seen_at: string;
};

/** One cell of the editor's grid: one artifact in one language. */
export type OperatorLegalCellRef = {
  slug: string;
  locale: AppLocale;
};

/** One cell seen rendered, at the text it now holds. */
export type OperatorLegalPreviewedCell = OperatorLegalCellRef & {
  previewed_by: string;
  previewed_at: string;
};

/**
 * What the publish step would do if it were pressed now (#563).
 *
 * IT RIDES ON THE WORKSPACE rather than on a read of its own, for the reason the
 * workspace is one payload at all: the label each act would create, the
 * headcount a gating act would re-gate and the reasons a correction is
 * unavailable are all statements about THIS draft beside THIS published edition,
 * and a second read could straddle a publication and answer about neither.
 */
export type OperatorLegalPublishPlan = {
  /** The label a NEW EDITION would take — the next generation. It goes on the button. */
  gating_label: string;
  /**
   * The label a CORRECTION would take: the next revision within the current
   * generation, FLAT. Shown on the quiet link, so choosing it visibly turns `2`
   * into `1.2`.
   */
  correction_label: string;
  /**
   * How many people a gating publication would re-gate. It goes on the CONFIRM
   * button, and the same number is stored on the version row — the proof that
   * the consequence was displayed. A correction's headcount is always zero and is
   * not sent separately.
   */
  headcount: number;
  /** Exactly `complete && previewed_all && seen_diff`. */
  can_publish: boolean;
  complete: boolean;
  gaps: { slug: string; locale: AppLocale }[];
  /** The draft adds or removes an artifact: a new edition, never a correction. */
  structural: boolean;
  /** The draft publishes a different set of languages: it reshapes the hash preimage. */
  locale_set_changed: boolean;
  /** Refuses a correction; a gating edition over unchanged text is allowed. */
  empty_diff: boolean;
  can_correct: boolean;
  /**
   * The language this document may not be published without — a CONSTANT of the
   * document's own package and never a column. Sent so the editor does not
   * hard-code a language; the reasons behind it are copy, in the messages.
   */
  protected_locale: AppLocale;
  protected_locale_kept: boolean;
  /** The first day a gating edition may take effect: the date input's minimum. */
  earliest_effective_date: string;
  diff_summary: string;
};

/**
 * One edition already published and still waiting for its day (#564) — what the
 * banner names, and what the cancel control acts on.
 *
 * IT CARRIES NO TEXT. The banner is a reminder that something is about to take
 * effect, not a second reading surface.
 */
export type OperatorLegalScheduledEdition = {
  version_id: string;
  /** The edition's name, rendered from its lineage by the API: `3`, or `2.1`. */
  label: string;
  /** YYYY-MM-DD: the day it takes effect. */
  effective_date: string;
  /** Whether it will re-gate everybody when its day comes. */
  gating: boolean;
};

/** Everything the editor needs for one document, in one read. */
export type OperatorLegalWorkspace = {
  document: OperatorLegalDocument;
  /** The menu the published-language set is bounded by — the platform's app locales. */
  supported_locales: AppLocale[];
  published: OperatorLegalEdition;
  draft: OperatorLegalDraft;
  publish: OperatorLegalPublishPlan;
  /**
   * Every edition waiting for its day, newest first. EMPTY IS THE NORMAL STATE.
   *
   * A LIST AND NOT ONE EDITION: the overnight delay pushes each gating
   * publication to a later day than the last, so an operator who scheduled two
   * has two nights running at once. An edition leaves this list at midnight, by
   * the database's own day, which is also when the seam stops permitting the
   * cancellation — the control's disappearance is membership of this list and
   * nothing else.
   */
  scheduled: OperatorLegalScheduledEdition[];
};

/**
 * One publication. NO LABEL AND NO TEXT: the label is rendered from the lineage
 * by the API and cannot be typed, and what is published is the saved draft —
 * exactly the text that was previewed and diffed.
 */
export type PublishOperatorLegalBody = {
  kind: "edition" | "correction";
  /** YYYY-MM-DD. Required and at least tomorrow on an edition; omitted on a correction. */
  effective_date?: string;
  /** The typed reason. Required on a correction only. */
  reason?: string;
};

/** The whole draft on its way back. No ordinals: the order is the ordinal. */
export type SaveOperatorLegalDraftBody = {
  published_locales: string[];
  artifacts: { slug: string; bodies: Record<string, string> }[];
};

/** One document's published edition and its draft, read together. */
export async function fetchOperatorLegalWorkspace(
  document: OperatorLegalDocument,
): Promise<OperatorLegalWorkspace> {
  return fetchEventsJSON<OperatorLegalWorkspace>(`/api/operator/legal/documents/${document}`);
}

/**
 * Saves the draft WHOLE. Adding an artifact and removing one are both nothing
 * more than saving a different list. An incomplete draft saves happily — the
 * completeness rule refuses a PUBLICATION, not an afternoon's work.
 */
export async function saveOperatorLegalDraft(
  document: OperatorLegalDocument,
  body: SaveOperatorLegalDraftBody,
): Promise<OperatorLegalWorkspace> {
  return fetchEventsJSON<OperatorLegalWorkspace>(`/api/operator/legal/documents/${document}/draft`, {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

/** Discards the draft, restoring the editor to the current published edition. */
export async function discardOperatorLegalDraft(
  document: OperatorLegalDocument,
): Promise<OperatorLegalWorkspace> {
  return fetchEventsJSON<OperatorLegalWorkspace>(`/api/operator/legal/documents/${document}/draft`, {
    method: "DELETE",
  });
}

/**
 * Records that one artifact was seen rendered, in one language (#562).
 *
 * IT SENDS NO TEXT. The rendering happens in the browser, through the same
 * `Markdown` component the Storefront renders, over text this workspace already
 * carries; the API remembers the draft's OWN text at that slug, so a client
 * cannot claim to have previewed something the draft does not say.
 */
export async function previewOperatorLegalCell(
  document: OperatorLegalDocument,
  cell: { slug: string; locale: AppLocale },
): Promise<OperatorLegalWorkspace> {
  return fetchEventsJSON<OperatorLegalWorkspace>(
    `/api/operator/legal/documents/${document}/draft/previews`,
    { method: "POST", body: JSON.stringify(cell) },
  );
}

/**
 * Records that the diff against the current edition was put on screen (#562).
 * No body: the API stamps both sides of the comparison itself, so the record
 * lapses when the draft is edited or somebody publishes underneath it.
 */
export async function seeOperatorLegalDiff(
  document: OperatorLegalDocument,
): Promise<OperatorLegalWorkspace> {
  return fetchEventsJSON<OperatorLegalWorkspace>(
    `/api/operator/legal/documents/${document}/draft/diff-seen`,
    { method: "POST" },
  );
}

/**
 * Publishes the saved draft as a new edition or as a correction (#563).
 *
 * Answers with the whole workspace, whose draft is once again a copy of the
 * published edition — because the draft became it. Every refusal comes back from
 * the API: this call checks nothing, because a precondition a browser could
 * decline to check is not a precondition.
 */
export async function publishOperatorLegalEdition(
  document: OperatorLegalDocument,
  body: PublishOperatorLegalBody,
): Promise<OperatorLegalWorkspace> {
  return fetchEventsJSON<OperatorLegalWorkspace>(
    `/api/operator/legal/documents/${document}/publications`,
    { method: "POST", body: JSON.stringify(body) },
  );
}

/**
 * Cancels a scheduled edition before its day (#564).
 *
 * NO BODY. There is no reason to type, no confirmation token to carry and no
 * approval to wait for: cancelling is ungated and immediate, because undoing is
 * always cheaper than doing. The edition is kept and marked, never deleted, and
 * its number is not reused.
 *
 * Answers with the whole workspace, whose `scheduled` list no longer names it.
 */
export async function cancelOperatorLegalEdition(
  document: OperatorLegalDocument,
  versionID: string,
): Promise<OperatorLegalWorkspace> {
  return fetchEventsJSON<OperatorLegalWorkspace>(
    `/api/operator/legal/documents/${document}/publications/${encodeURIComponent(versionID)}/cancel`,
    { method: "POST" },
  );
}
