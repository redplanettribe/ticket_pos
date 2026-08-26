/**
 * The Recipient Warning on the document detail (#482, ADR 0061): which of
 * the SRI's messages the warning card quotes, decided once and tested under
 * `node --test`.
 *
 * The API decides WHETHER a document carries the warning — `recipient_warning`
 * on the row, set from the authorization outcome and cleared only by
 * supersession. What is left to the page is which of the stored messages are
 * the authority's own words about the Recipient: advertencia 59
 * ("identificación no existe") and 62 ("identificación incorrecta"), never
 * the 60 every test-environment authorization carries. They are read from
 * the last answer first, and from the attempts ledger when a later answer —
 * or a backfilled document's older one — no longer carries them.
 */

import type { OperatorInvoiceAttempt, OperatorInvoiceMessage } from "./operator-api";

/** The SRI's advertencias about the Recipient's identification. */
export const RECIPIENT_WARNING_IDENTIFIERS = ["59", "62"] as const;

function isRecipientWarning(message: OperatorInvoiceMessage): boolean {
  return (
    message.type === "ADVERTENCIA" &&
    (RECIPIENT_WARNING_IDENTIFIERS as readonly string[]).includes(message.identifier)
  );
}

/**
 * The SRI's 59 / 62 messages to quote on a warned document: from the last
 * answer when it carries them, otherwise from the newest authorized attempt
 * that does. Empty when nothing stored says so — the card then states the
 * warning without a quotation rather than inventing one.
 */
export function recipientWarningMessages(
  lastMessages: OperatorInvoiceMessage[],
  attempts: OperatorInvoiceAttempt[],
): OperatorInvoiceMessage[] {
  const fromLast = lastMessages.filter(isRecipientWarning);
  if (fromLast.length > 0) {
    return fromLast;
  }
  for (let i = attempts.length - 1; i >= 0; i -= 1) {
    const attempt = attempts[i];
    if (attempt.outcome !== "authorized") {
      continue;
    }
    const found = attempt.messages.filter(isRecipientWarning);
    if (found.length > 0) {
      return found;
    }
  }
  return [];
}
