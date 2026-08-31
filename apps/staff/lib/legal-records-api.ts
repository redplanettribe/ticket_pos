import {
  type ConsentActPage,
  type LegalCustomerRecord,
  type StaffLegalRecord,
  customerRecordPath,
  customerRecordsPath,
  customerWithdrawalPath,
  staffRecordPath,
} from "./legal-records";
import { fetchEventsJSON } from "./events-api";

// The per-subject record's fetchers (#566).
//
// A file of their own, separate from the pure module beside it, so
// legal-records.ts stays importable by a framework-free unit test: how a NULL
// is spelled and which state `presented_locale` is in are rules worth testing,
// and they must not drag the API client in behind them.
//
// THESE ARE PLAIN GETs, unlike the acceptance browsers' POSTs-that-read, and
// the difference is the same rule reaching a different answer: nothing in any
// of these request lines is an address. A customer id is opaque, a digest is
// opaque, and the history's cursor is a timestamp and a row id.

/** One Customer's record: identity, both gates, and the staff cross-link. */
export async function fetchCustomerLegalRecord(
  customerID: string,
): Promise<LegalCustomerRecord> {
  return fetchEventsJSON<LegalCustomerRecord>(customerRecordPath(customerID));
}

/** One page of that Customer's history, newest first. */
export async function fetchConsentActs(
  customerID: string,
  cursor?: string | null,
): Promise<ConsentActPage> {
  return fetchEventsJSON<ConsentActPage>(customerRecordsPath(customerID, cursor));
}

/** One staff person's complete acceptance record, found by their digest. */
export async function fetchStaffLegalRecord(digest: string): Promise<StaffLegalRecord> {
  return fetchEventsJSON<StaffLegalRecord>(staffRecordPath(digest));
}

/**
 * A withdrawal as the operator states it.
 *
 * Each consent is OMITTED where the artefact did not ask for it — a form asking
 * for one thing takes one thing away, and sending `false` for a consent nobody
 * mentioned would record an answer to a question that was never put. THE ONLY
 * VALUE EITHER FIELD MAY CARRY IS `false`: the API refuses `true`, and the
 * refusal is the platform's rather than this type's.
 *
 * There is no `policy_acceptance` and no `terms_acceptance` here, and there
 * never will be: a contract's basis is performance rather than consent, and
 * clearing a Policy Acceptance would re-gate the person rather than free them.
 */
export type ConsentWithdrawalBody = {
  marketing_consent?: false;
  networking_consent?: false;
  request_reference: string;
};

/** What the withdrawal answers with: the state after the act, and what it took. */
export type ConsentWithdrawalResult = {
  customer: { id: string; email: string; first_name: string; last_name: string };
  consent: {
    marketing_consent: string | null;
    networking_consent: string | null;
    policy_accepted_at: string | null;
  };
  /**
   * What this act TOOK AWAY, which cannot be derived from the state beside it:
   * `denied` reads the same whether somebody just gave something up or was
   * already refusing. It is also what decides whether they were emailed.
   */
  withdrew: { marketing_consent: boolean; networking_consent: boolean } | null;
};

/**
 * Records a Consent Withdrawal on a Customer's behalf, attributed to the
 * operator who entered it and referenced to the artefact it answers.
 *
 * KEYED ON THE OPAQUE ID. The operator is already reading this person's record;
 * the address they recognise them by is on the screen, not in the URL.
 */
export async function recordConsentWithdrawal(
  customerID: string,
  body: ConsentWithdrawalBody,
): Promise<ConsentWithdrawalResult> {
  return fetchEventsJSON<ConsentWithdrawalResult>(customerWithdrawalPath(customerID), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}
