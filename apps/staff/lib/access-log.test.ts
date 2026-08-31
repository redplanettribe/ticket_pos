import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  ACCESS_ACTS,
  type AccessAct,
  type AccessEntry,
  accessActLabelKey,
  accessLogPath,
  accessSubjectHref,
  accessSubjectLabel,
  packFingerprintLabel,
} from "./access-log.ts";

// WHAT THESE ASSERT (#569, spec #556, ADR 0067).
//
// The client-side rules that are easy to get wrong, and two that would be
// design reversals rather than bugs:
//
//   * the four acts, closed, matching migration 116's CHECK;
//   * THE FILTERS ARE ACTOR, ACT AND DATE — and there is NO SUBJECT FILTER,
//     because an audit log searchable by the person it is about is a second way
//     to look people up;
//   * a staff subject has no link, because it is recorded as a plain address
//     and never as a digest, and minting one client-side is the thing the
//     digest exists to prevent;
//   * every act has words in both languages.

function entry(overrides: Partial<AccessEntry>): AccessEntry {
  return {
    id: 1,
    act: "list_read",
    actor_email: "ana@example.com",
    occurred_at: "2026-08-31T12:00:00Z",
    population: "customer",
    document: "policy",
    status_filter: "outstanding",
    searched: false,
    result_count: 12,
    subject_customer_id: null,
    subject_email: null,
    pack_sha256: null,
    ...overrides,
  };
}

// FOUR ACTS AND ONLY FOUR, in the order the screen shows them. The list is the
// filter's options and the payload's vocabulary at once, so a fifth act cannot
// arrive on one side alone.
test("there are exactly four logged acts", () => {
  assert.deepEqual([...ACCESS_ACTS], ["list_read", "subject_read", "evidence_export", "audit_read"]);
});

// THE FILTERS ARE ACTOR, ACT AND DATE. Empty ones are omitted, so "everything"
// is one URL rather than one per combination of blanks.
test("the query string carries the three filters and drops the empty ones", () => {
  assert.equal(accessLogPath(), "/api/operator/legal/access-log");
  assert.equal(accessLogPath({ actor: "", act: "", from: "", to: "" }), "/api/operator/legal/access-log");
  assert.equal(
    accessLogPath({ actor: "ana@example.com", act: "subject_read", from: "2026-08-01", to: "2026-08-31" }),
    "/api/operator/legal/access-log?actor=ana%40example.com&act=subject_read&from=2026-08-01&to=2026-08-31",
  );
  assert.equal(accessLogPath({ cursor: "abc" }), "/api/operator/legal/access-log?cursor=abc");
});

// THE ACTOR FILTER IS AN OPERATOR'S ADDRESS AND NOT A DATA SUBJECT'S, which is
// why it may travel in a query string at all — the rule (#565) is no data
// subject in a request line, not "no query strings". It is trimmed rather than
// reshaped: an exact address, never a fragment, because a fragment match here
// would be a search box over addresses.
test("the actor filter is trimmed and sent whole", () => {
  assert.equal(
    accessLogPath({ actor: "  ANA@example.com  " }),
    "/api/operator/legal/access-log?actor=ANA%40example.com",
  );
});

// NO SUBJECT FILTER, AND NOWHERE TO PUT ONE. This is the assertion that would
// fail first if somebody added "search by person" to the audit log, which is the
// single change this design most needs not to happen.
test("no filter names a subject", () => {
  const url = accessLogPath({
    actor: "ana@example.com",
    act: "subject_read",
    from: "2026-08-01",
    to: "2026-08-31",
    cursor: "xyz",
  });
  const params = new URLSearchParams(url.split("?")[1]);
  assert.deepEqual([...params.keys()].sort(), ["act", "actor", "cursor", "from", "to"]);
  // Spelled out, so the intent survives a refactor of the list above.
  for (const forbidden of ["subject", "subject_email", "email", "customer_id", "digest"]) {
    assert.equal(params.has(forbidden), false, `the audit log must not be filterable by ${forbidden}`);
  }
});

// A SUBJECT IS NAMED WHERE THERE IS ONE, and a list read names nobody — it
// recorded a question about a population, so the subject column is honestly
// empty rather than filled with "everybody".
test("a subject read names the person and a list read names nobody", () => {
  assert.equal(accessSubjectLabel(entry({ act: "subject_read", subject_email: "bea@example.com" })), "bea@example.com");
  assert.equal(accessSubjectLabel(entry({})), "");
});

// A CUSTOMER SUBJECT LINKS BACK BY OPAQUE ID, so following the audit log puts no
// address in a URL any more than the browser it audits does.
test("a customer subject links back by opaque id", () => {
  const href = accessSubjectHref(
    entry({ act: "subject_read", subject_email: "bea@example.com", subject_customer_id: "c-1" }),
  );
  assert.equal(href, "/operator/legal/acceptances/customers/c-1");
});

// A STAFF SUBJECT HAS NO LINK. It is recorded as a plain address — never a
// digest, which a key rotation would orphan — so there is nothing to link to,
// and minting a digest here from the address would be the one thing the digest
// exists to make impossible.
test("a staff subject is not linked, because it is an address and not a digest", () => {
  assert.equal(
    accessSubjectHref(entry({ act: "subject_read", subject_email: "cara@example.com", subject_customer_id: null })),
    null,
  );
});

// THE FINGERPRINT SHOWN IS THE FILENAME'S HALF, so a ZIP in somebody's mailbox
// matches its row by eye (ADR 0067).
test("the pack fingerprint shown is the sixteen characters the filename carries", () => {
  const sha = "a".repeat(48) + "b".repeat(16);
  assert.equal(packFingerprintLabel(sha), "a".repeat(16));
  assert.equal(packFingerprintLabel(sha).length, 16);
});

// Every act has words in BOTH catalogs. English is typed by the compiler;
// Spanish is typed by nothing, and a missing key there is a hole only a
// Spanish-reading operator ever sees.
test("every act is named in both catalogs", () => {
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { operator: { legalAccessLog: Record<string, string> } };
    const copy = catalog.operator.legalAccessLog;
    for (const act of ACCESS_ACTS as readonly AccessAct[]) {
      const key = accessActLabelKey(act);
      assert.equal(typeof copy[key], "string", `${locale}.json is missing operator.legalAccessLog.${key}`);
      assert.notEqual(copy[key].trim(), "", `${locale}.json has an empty operator.legalAccessLog.${key}`);
    }
  }
});
