// The two acceptance browsers, client side (#565, spec #556, ADR 0067): who
// owes an acceptance, in the two populations that can owe one.
//
// A framework-free module so the three rules that are easy to get wrong — which
// states each screen has, what the default is, and how a page is asked for —
// can be tested with no browser and no server (see acceptance-browsers.test.ts).

/**
 * The four states, and only four.
 *
 * `never_seen` is NOT a kind of `outstanding`: about a third of the customer
 * base has never accepted anything, and folding them together would bury the
 * handful who owe a fresh acceptance.
 *
 * `former` is NOT a kind of `outstanding` either, and is staff-only: a leaver
 * owes nothing, so listing them would fill the default filter with people
 * nobody can chase.
 *
 * There is deliberately no `withdrawn`. Withdrawal is a state of an OPTIONAL
 * consent and of neither gate, and a value for it here would let a withdrawn
 * marketing consent be misread as an unaccepted document.
 */
export type AcceptanceStanding = "current" | "outstanding" | "never_seen" | "former";

/**
 * The states the CUSTOMER browser offers. `former` is absent because a
 * Customer record is never deleted — a Ticket Sale is a financial record that
 * must reconcile — so there is no departure to observe.
 */
export const CUSTOMER_STANDINGS: readonly AcceptanceStanding[] = [
  "outstanding",
  "never_seen",
  "current",
] as const;

/** The states the STAFF browser offers, `former` included. */
export const STAFF_STANDINGS: readonly AcceptanceStanding[] = [
  "outstanding",
  "never_seen",
  "current",
  "former",
] as const;

/**
 * OUTSTANDING IS THE DEFAULT on both screens, because "who owes something" is
 * the question the feature exists to answer.
 */
export const DEFAULT_STANDING: AcceptanceStanding = "outstanding";

/**
 * The two legal documents. The customer browser asks about either; the staff
 * browser asks about the Terms alone, because there is exactly one staff gate
 * — everybody who signs into the Staff platform accepts the Términos y
 * Condiciones — and staff accept no Privacy Policy.
 */
export type LegalDocument = "policy" | "terms";

export const CUSTOMER_DOCUMENTS: readonly LegalDocument[] = ["policy", "terms"] as const;

/** The one document the staff gate is about. */
export const STAFF_DOCUMENT: LegalDocument = "terms";

/** One person on the customer browser: one row, two status columns. */
export type CustomerAcceptanceRow = {
  /** Opaque. The per-subject record is reached by this, never by the address. */
  customer_id: string;
  email: string;
  name: string;
  policy_standing: AcceptanceStanding;
  terms_standing: AcceptanceStanding;
};

export type CustomerAcceptancePage = {
  rows: CustomerAcceptanceRow[];
  /** Null on the last page. Its presence is the whole "is there more?" signal. */
  next_cursor: string | null;
};

/** One person on the staff browser. */
export type StaffAcceptanceRow = {
  /** 32 hex chars. How a staff person is named in a URL without disclosing them. */
  digest: string;
  email: string;
  standing: AcceptanceStanding;
};

export type StaffAcceptancePage = {
  rows: StaffAcceptanceRow[];
  next_cursor: string | null;
};

/** What a page request carries. Every field of it travels in a BODY. */
export type AcceptanceBrowseRequest = {
  standing: AcceptanceStanding;
  cursor?: string | null;
  searchEmail?: string;
};

/**
 * Builds the request body for one page.
 *
 * EVERYTHING GOES IN THE BODY, AND NOTHING IN A QUERY STRING. A data subject's
 * email must never appear in a URL, a query string or a referer (#565) — and
 * TWO of these fields are addresses. The obvious one is the search fragment.
 * The other is the CURSOR: paging is keyset on `email ASC`, so the cursor IS
 * the last address of the previous page. `?cursor=YW5hQGV4YW1wbGUuY29t` is an
 * email in a URL wearing a hat, and it would be kept in the browser's history,
 * written to the reverse proxy's access log, and sent onwards in the Referer
 * header of whatever the operator clicked next.
 *
 * A null or empty cursor is OMITTED rather than sent as null, so "the first
 * page" is one thing on the wire and not two.
 */
export function acceptanceBrowseBody(request: AcceptanceBrowseRequest): Record<string, string> {
  const body: Record<string, string> = { standing: request.standing };
  const cursor = (request.cursor ?? "").trim();
  if (cursor !== "") {
    body.cursor = cursor;
  }
  const search = (request.searchEmail ?? "").trim();
  if (search !== "") {
    body.search_email = search;
  }
  return body;
}

/**
 * The BFF path for one browser's page.
 *
 * The DOCUMENT is in the path and that is fine — it is `policy` or `terms`, the
 * name of a public agreement and nobody's personal data. Nothing else is.
 */
export function customerBrowserPath(document: LegalDocument): string {
  return `/api/operator/legal/acceptances/customers/${document}`;
}

export function staffBrowserPath(): string {
  return `/api/operator/legal/acceptances/staff/${STAFF_DOCUMENT}`;
}

/**
 * The copy key for one standing's badge, e.g. `standingOutstanding`.
 *
 * Derived rather than mapped, so a fifth state cannot be added to the type
 * without a missing key showing up — and so the two screens cannot label the
 * same state differently.
 */
export function standingLabelKey(standing: AcceptanceStanding): string {
  switch (standing) {
    case "current":
      return "standingCurrent";
    case "outstanding":
      return "standingOutstanding";
    case "never_seen":
      return "standingNeverSeen";
    case "former":
      return "standingFormer";
  }
}

/**
 * The badge's tone. OUTSTANDING IS THE ONLY ONE THAT READS AS A PROBLEM: it is
 * the only state anybody can act on. `never_seen` is the ordinary condition of
 * a third of the customer base and must not be rendered as an alarm, and
 * `former` is a settled fact about somebody who has left.
 */
export function standingTone(standing: AcceptanceStanding): "positive" | "attention" | "muted" {
  switch (standing) {
    case "current":
      return "positive";
    case "outstanding":
      return "attention";
    case "never_seen":
    case "former":
      return "muted";
  }
}
