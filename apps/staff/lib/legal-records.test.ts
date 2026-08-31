import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  type ConsentAct,
  answerLabelKey,
  customerRecordHref,
  customerRecordPath,
  customerRecordsPath,
  customerWithdrawalPath,
  presentedLocaleState,
  staffRecordHref,
  staffRecordPath,
  wouldTakeSomethingAway,
} from "./legal-records.ts";

// ONE PERSON'S CONSENT RECORD, the rules worth testing without a browser (#566).
//
// Three of them, and each is a way the screen could quietly start lying:
//
//   - NO ADDRESS EVER REACHES A URL. Every path here is built from an opaque id
//     or a digest, and the two routes that used to carry an email are gone.
//   - `presented_locale` HAS THREE STATES and the field being ABSENT is one of
//     them. Collapsing absent into null would put "not recorded" beside an act
//     that showed nobody anything — a claim that text was displayed and its
//     language forgotten.
//   - NULL IS SPELLED IN WORDS. A null answer is "not shown on that screen" and
//     never a blank, because a blank beside "Marketing consent" reads as a
//     refusal — and mistaking one for the other is how somebody comes to
//     withdraw a consent nobody ever granted.

const act = (overrides: Partial<ConsentAct> = {}): ConsentAct =>
  ({
    id: "act-1",
    captured_at: "2026-08-31T12:00:00Z",
    channel: "signin",
    email: "ana@example.com",
    policy_edition: { id: "edition-1", label: "2" },
    policy_acceptance: true,
    marketing_consent: null,
    networking_consent: null,
    prior_marketing_consent: null,
    prior_networking_consent: null,
    terms_acceptance: null,
    terms_edition: null,
    email_proven: true,
    ip: null,
    user_agent: null,
    session_id: null,
    origin_url: null,
    recorded_by: null,
    request_reference: null,
    confirmed_at: null,
    confirmation_sent_at: null,
    ...overrides,
  }) as ConsentAct;

test("every path names a person by an opaque key and never by an address", () => {
  const id = "8f1c0c66-0000-4000-8000-000000000000";
  for (const path of [
    customerRecordPath(id),
    customerRecordsPath(id),
    customerWithdrawalPath(id),
    customerRecordHref(id),
    staffRecordPath("0123456789abcdef0123456789abcdef"),
    staffRecordHref("0123456789abcdef0123456789abcdef"),
  ]) {
    assert.ok(!path.includes("@"), `${path} carries an email address`);
    assert.ok(!path.includes("%40"), `${path} carries an encoded email address`);
  }
});

test("the history's cursor travels in a query string, and an absent one does not", () => {
  const id = "cust-1";
  // No cursor is the plain path: "the first page" is one thing on the wire and
  // not two.
  assert.equal(customerRecordsPath(id), "/api/operator/legal/customers/cust-1/records");
  assert.equal(customerRecordsPath(id, "   "), "/api/operator/legal/customers/cust-1/records");
  // A cursor here is a capture timestamp and a row id, which name nobody —
  // unlike the acceptance browsers', which IS an address and must stay in a
  // body.
  assert.equal(
    customerRecordsPath(id, "abc def"),
    "/api/operator/legal/customers/cust-1/records?cursor=abc%20def",
  );
});

test("presented_locale is three-stated, and an absent field is not a null one", () => {
  // The channel showed no document: the API omits the key entirely, and the
  // screen must render nothing rather than "not recorded".
  assert.equal(presentedLocaleState(act({ channel: "account_settings" })), "absent");
  // A document WAS shown and no language is on the row: an act from before the
  // column existed. "Not recorded" is the truthful word.
  assert.equal(presentedLocaleState(act({ presented_locale: null })), "not-recorded");
  // And the ordinary case.
  assert.equal(presentedLocaleState(act({ presented_locale: "es" })), "locale");
});

test("a null answer is a word about the screen, not a No", () => {
  assert.equal(answerLabelKey(true), "answerYes");
  assert.equal(answerLabelKey(false), "answerNo");
  // The load-bearing one: null means the box was NOT SHOWN on that surface,
  // which is emphatically not a refusal.
  assert.equal(answerLabelKey(null), "answerNotShown");
});

test("a pending confirmation is something a withdrawal really takes away", () => {
  assert.equal(wouldTakeSomethingAway("granted"), true);
  // Somebody else's tick, standing against the address and never expiring, so
  // settling it as No genuinely removes something.
  assert.equal(wouldTakeSomethingAway("pending_confirmation"), true);
  assert.equal(wouldTakeSomethingAway("denied"), false);
  // Never asked: there is nothing to take away, and offering to would be
  // offering to withdraw a consent nobody gave.
  assert.equal(wouldTakeSomethingAway(null), false);
});

test("every copy key the record reads is in both catalogs", () => {
  const en = JSON.parse(readFileSync(new URL("../messages/en.json", import.meta.url), "utf8"));
  const es = JSON.parse(readFileSync(new URL("../messages/es.json", import.meta.url), "utf8"));

  const keys = [
    // The words that spell a NULL. If any of these goes missing the screen
    // renders a key or a blank, and a blank is the failure this feature exists
    // to avoid.
    "notRecorded",
    "never",
    "answerYes",
    "answerNo",
    "answerNotShown",
    "consentNeverAsked",
    // The channel names, one per value migration 067's CHECK admits. A channel
    // with no key would render as `channel_whatever` on an evidence screen.
    "channel_signin",
    "channel_checkout",
    "channel_account_settings",
    "channel_unsubscribe_link",
    "channel_email_confirmation",
    "channel_operator_request",
    "channel_passcode_withdrawal",
    // The two counts that make a truncated page distinguishable from a whole
    // record.
    "historyComplete",
    "historyTruncated",
    "staffHistoryComplete",
    // The cross-link, which is a claim about two records and must read as one.
    "crossLinkToStaffAction",
    "crossLinkToCustomerAction",
  ];

  for (const key of keys) {
    assert.equal(typeof en.operator.legalRecords[key], "string", `en is missing ${key}`);
    assert.equal(typeof es.operator.legalRecords[key], "string", `es is missing ${key}`);
  }
});

test("the retired consent surface's copy is gone from both catalogs", () => {
  const en = JSON.parse(readFileSync(new URL("../messages/en.json", import.meta.url), "utf8"));
  const es = JSON.parse(readFileSync(new URL("../messages/es.json", import.meta.url), "utf8"));

  // /operator/consent began by FINDING somebody from an email address, and that
  // half of it is gone rather than moved: the acceptance browsers' search
  // absorbed it, from a POSTed body. Its copy must go with it, or the next
  // person to read the catalog will think the screen is still there.
  for (const catalog of [en, es]) {
    assert.equal(catalog.operator.navCustomerConsent, undefined);
    assert.equal(catalog.operator.consentEmailLabel, undefined);
    assert.equal(catalog.operator.consentFindCustomer, undefined);
    assert.equal(catalog.operator.consentNoCustomer, undefined);
  }
});
