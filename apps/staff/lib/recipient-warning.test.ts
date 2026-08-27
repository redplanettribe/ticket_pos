import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import type { OperatorInvoiceAttempt, OperatorInvoiceMessage } from "./operator-api.ts";
import { recipientWarningMessages } from "./recipient-warning.ts";

// WHAT THESE ASSERT. The API says whether a document carries a Recipient
// Warning; the page's one decision is which of the SRI's stored messages it
// quotes for it (#482). The last test keeps the card from reaching an
// operator with nothing to say in either language.

const warning = (identifier: string, message: string): OperatorInvoiceMessage => ({
  identifier,
  message,
  additional_info: "",
  type: "ADVERTENCIA",
});
const testEnvironment = warning("60", "ESTE PROCESO FUE REALIZADO EN EL AMBIENTE DE PRUEBAS");

const attempt = (outcome: OperatorInvoiceAttempt["outcome"], messages: OperatorInvoiceMessage[]): OperatorInvoiceAttempt => ({
  id: 1,
  operation: "query",
  outcome,
  messages,
  error: "",
  started_at: "2026-07-07T17:00:00Z",
  duration_ms: 10,
});

test("the last answer's 59 or 62 is quoted, and the 60 beside it is not", () => {
  const missing = warning("59", "IDENTIFICACION NO EXISTE");
  assert.deepEqual(recipientWarningMessages([testEnvironment, missing], []), [missing]);
  const incorrect = warning("62", "IDENTIFICACION INCORRECTA");
  assert.deepEqual(recipientWarningMessages([incorrect, testEnvironment], []), [incorrect]);
});

test("a 59 typed as anything but a warning is not quoted", () => {
  const notAWarning = { ...warning("59", "x"), type: "ERROR" };
  assert.deepEqual(recipientWarningMessages([notAWarning], []), []);
});

// A backfilled document, or one whose last answer no longer carries the
// advertencia: the newest authorized attempt that does is quoted, and a
// refused attempt's messages never are.
test("the ledger is read newest first when the last answer says nothing", () => {
  const older = warning("59", "IDENTIFICACION NO EXISTE");
  const newer = warning("62", "IDENTIFICACION INCORRECTA");
  const attempts = [
    attempt("authorized", [older, testEnvironment]),
    attempt("not_authorized", [warning("59", "should not be read")]),
    attempt("authorized", [newer]),
  ];
  assert.deepEqual(recipientWarningMessages([testEnvironment], attempts), [newer]);
  assert.deepEqual(recipientWarningMessages([], [attempt("received", [older])]), []);
});

test("every surface has its words in both languages", () => {
  const keys = [
    "recipientWarningCardTitle",
    "recipientWarningCardDescription",
    "openTheRecipientWarnings",
    "invoicingRecipientWarningBadge",
    "invoicingRecipientWarningFilter",
    "invoicingListEmptyForRecipientWarning",
    "invoicingRecipientWarningTitle",
    "invoicingRecipientWarningBody",
    "invoicingRecipientWarningNoQuote",
  ];
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { operator: Record<string, string> };
    for (const key of keys) {
      assert.ok(catalog.operator[key]?.trim(), `${locale}: operator.${key} is missing or empty`);
    }
  }
});
