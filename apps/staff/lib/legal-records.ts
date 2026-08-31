// ONE PERSON'S CONSENT RECORD, client side (#566, spec #556, ADR 0067): the
// screen an operator answering a data subject's request works from.
//
// A framework-free module so the rules that are easy to get wrong — how a
// person is addressed, how a NULL is spelled, and which of the three states
// `presented_locale` is in — can be tested with no browser and no server
// (legal-records.test.ts). The fetchers live next door for the same reason the
// acceptance browsers' do.
//
// NO EMAIL ADDRESS EVER REACHES A URL on this surface. A Customer is addressed
// by their opaque UUID and a staff person by their Staff Digest, and the two
// routes that used to carry an address are deleted. That is not tidiness: a URL
// is written to the reverse proxy's access log, kept in the browser's history,
// and sent onwards in the Referer header of whatever the operator clicks next.

import type { AcceptanceStanding } from "./acceptance-browsers";

/** An edition, named so it resolves to bytes AND reads as something sayable. */
export type LegalEditionRef = {
  /** The version row's id. What an evidence pack resolves to artifacts. */
  id: string;
  /** "2" for a gating edition, "1.1" for a correction. "" if unresolvable. */
  label: string;
};

/** Where one person stands against one document's gate. */
export type LegalGateStanding = {
  /** Null where no acceptance was ever recorded. */
  accepted_at: string | null;
  /** The exact edition accepted, null where none ever was. */
  edition: LegalEditionRef | null;
  /** Never `former` on a Customer: a Customer record is never deleted. */
  standing: AcceptanceStanding;
};

/** One optional consent's state; null is UNANSWERED, not denied. */
export type OptionalConsentValue = "granted" | "denied" | "pending_confirmation";

/** Who the record is about, and what is true of them now. */
export type LegalCustomerSubject = {
  id: string;
  email: string;
  first_name: string;
  last_name: string;
  policy: LegalGateStanding;
  terms: LegalGateStanding;
  marketing_consent: OptionalConsentValue | null;
  networking_consent: OptionalConsentValue | null;
};

export type LegalCustomerRecord = {
  customer: LegalCustomerSubject;
  /**
   * The cross-link: the same human being on the Staff platform, or null.
   *
   * TWO RECORDS, CROSS-LINKED, NEVER MERGED. There is no row anywhere saying
   * these two are one person — only an address that matches — so the platform
   * offers a link and stops short of asserting an identity it cannot evidence.
   */
  staff_digest: string | null;
};

/**
 * One capture act.
 *
 * EVERY NULLABLE FIELD MEANS SOMETHING SPECIFIC and the screen spells each of
 * them in words. A null ANSWER is a box that was not shown on that surface,
 * which is not a No. A null IP, user agent, session or origin is something the
 * surface did not collect, which is not a blank it collected.
 */
export type ConsentAct = {
  id: string;
  captured_at: string;
  channel: string;
  /** The address AS ASSERTED at the moment of capture, not the one held today. */
  email: string;
  policy_edition: LegalEditionRef;
  policy_acceptance: boolean | null;
  marketing_consent: boolean | null;
  networking_consent: boolean | null;
  /** What each consent WAS immediately before this act — the only way to read
   * whether it took anything away, since `denied` looks the same either way. */
  prior_marketing_consent: string | null;
  prior_networking_consent: string | null;
  terms_acceptance: boolean | null;
  terms_edition: LegalEditionRef | null;
  email_proven: boolean;
  ip: string | null;
  user_agent: string | null;
  session_id: string | null;
  origin_url: string | null;
  /**
   * THREE STATES, AND THE OPTIONAL `?` IS ONE OF THEM.
   *
   * - The KEY IS ABSENT on channels that present no document at all. Nothing
   *   was shown, so there is no question to answer — and a field rendered here
   *   would claim text was displayed and its language forgotten.
   * - NULL where a document WAS shown and the language is not recorded: every
   *   act captured before the column existed. The screen says "not recorded".
   * - Otherwise the language of the ARTIFACT RENDERED.
   *
   * See presentedLocaleState, which is what the screen actually reads.
   */
  presented_locale?: string | null;
  recorded_by: string | null;
  request_reference: string | null;
  confirmed_at: string | null;
  confirmation_sent_at: string | null;
};

export type ConsentActPage = {
  acts: ConsentAct[];
  /**
   * HOW MANY ACTS ARE ON THIS PAGE, so a truncated page is distinguishable from
   * a complete history. NOT a total: the screen accumulates pages as the
   * operator walks, and the running sum is the number they need.
   */
  visible_count: number;
  /** Null on the last page. Its presence is the whole "is there more?" signal. */
  next_cursor: string | null;
};

/** One staff Terms Acceptance. */
export type StaffAcceptanceRecord = {
  id: string;
  terms_edition: LegalEditionRef;
  /** What the person accepted AS — carried rather than assumed. */
  capacity: string;
  accepted_at: string;
  ip: string | null;
  user_agent: string | null;
  session_id: string | null;
  origin_url: string | null;
  /**
   * A PLAIN NULLABLE and not the three-state field the Customer record has: the
   * staff terms gate is the one surface that always presents a document, so
   * "this does not apply" is not a state a staff acceptance can be in.
   */
  presented_locale: string | null;
};

export type StaffLegalRecord = {
  digest: string;
  email: string;
  /** Four states here, `former` included: staff can leave, Customers cannot. */
  standing: AcceptanceStanding;
  /** UNPAGED and complete: at most one acceptance per edition per capacity. */
  acceptances: StaffAcceptanceRecord[];
  visible_count: number;
  /** The cross-link back to the Customer record, or null. */
  customer_id: string | null;
};

/** The page size the API serves and the screen states as a fact. */
export const CONSENT_ACT_PAGE_SIZE = 25;

/** The BFF path for one Customer's record. Opaque id, never an address. */
export function customerRecordPath(customerID: string): string {
  return `/api/operator/legal/customers/${encodeURIComponent(customerID)}`;
}

/** One page of that Customer's history. */
export function customerRecordsPath(customerID: string, cursor?: string | null): string {
  const path = `${customerRecordPath(customerID)}/records`;
  const trimmed = (cursor ?? "").trim();
  // THE CURSOR MAY TRAVEL IN A QUERY STRING here, unlike the acceptance
  // browsers', and it is the same rule reaching a different answer: theirs is
  // keyset on `email ASC`, so their cursor IS somebody's address. This one is a
  // capture timestamp and a row id, which name nobody.
  return trimmed === "" ? path : `${path}?cursor=${encodeURIComponent(trimmed)}`;
}

/** The withdrawal, keyed on the same opaque id. */
export function customerWithdrawalPath(customerID: string): string {
  return `${customerRecordPath(customerID)}/withdrawal`;
}

/** The BFF path for one staff person's record, keyed on their digest. */
export function staffRecordPath(digest: string): string {
  return `/api/operator/legal/staff/${encodeURIComponent(digest)}`;
}

/**
 * The Consent Evidence Pack, keyed on the same opaque things (#568).
 *
 * TWO PATHS AND ONE FILE. A pack spans both populations for one address, so
 * whichever record screen the operator is on, the bytes they hand over are the
 * same — one access request has one answer. Each path is keyed on what its
 * screen is keyed on, so no address reaches a request line here either.
 */
export function customerEvidencePackPath(customerID: string): string {
  return `${customerRecordPath(customerID)}/evidence-pack`;
}

export function staffEvidencePackPath(digest: string): string {
  return `${staffRecordPath(digest)}/evidence-pack`;
}

/**
 * Reads the pack's filename out of a Content-Disposition header.
 *
 * THE API DECIDES THE NAME AND THIS ONLY READS IT BACK. The name is keyed on
 * the pack's own SHA-256 (ADR 0067's resolution of #546 against #548), which is
 * a fact about the bytes: a name invented here would be a second name for one
 * document, and the one property that makes it checkable — `sha256sum` the file
 * and read the first sixteen characters — would be lost.
 *
 * The fallback is deliberately unkeyed. A header that did not arrive is a proxy
 * problem, and a made-up hash in a filename would be worse than a generic name.
 */
export function evidencePackFilenameFrom(disposition: string | null): string {
  const match = disposition?.match(/filename="?([^"]+)"?/);
  return match?.[1] ?? "consent-evidence-pack.zip";
}

/** The in-app route for one Customer's record — where a browser row links. */
export function customerRecordHref(customerID: string): string {
  return `/operator/legal/acceptances/customers/${encodeURIComponent(customerID)}`;
}

/** The in-app route for one staff person's record. */
export function staffRecordHref(digest: string): string {
  return `/operator/legal/acceptances/staff/${encodeURIComponent(digest)}`;
}

/**
 * Which of the three states an act's `presented_locale` is in.
 *
 * IT IS A FUNCTION AND NOT AN INLINE TERNARY because the distinction is the
 * thing most easily lost: `act.presented_locale ?? "not recorded"` would render
 * "not recorded" on a channel that showed nobody anything, which is a claim
 * that text was displayed and its language forgotten. `"absent"` means DO NOT
 * RENDER THE FIELD AT ALL.
 */
export function presentedLocaleState(
  act: Pick<ConsentAct, "presented_locale">,
): "absent" | "not-recorded" | "locale" {
  if (!("presented_locale" in act) || act.presented_locale === undefined) {
    return "absent";
  }
  return act.presented_locale === null ? "not-recorded" : "locale";
}

/**
 * The `operator.legalRecords` copy key for a consent answer.
 *
 * NULL IS SPELLED IN WORDS. A blank cell beside "Marketing consent" reads as a
 * refusal, and mistaking "not shown on this surface" for "No" is how an
 * operator comes to withdraw something nobody ever granted.
 */
export function answerLabelKey(answer: boolean | null): string {
  if (answer === null) {
    return "answerNotShown";
  }
  return answer ? "answerYes" : "answerNo";
}

/**
 * Whether withdrawing this consent would actually take something away.
 *
 * `pending_confirmation` counts: it is somebody's tick standing against the
 * address, never expiring, so settling it as No really does take something
 * away.
 */
export function wouldTakeSomethingAway(value: OptionalConsentValue | null): boolean {
  return value === "granted" || value === "pending_confirmation";
}
