import { lastMailLine } from "./api-log";
import { findInMailpit } from "./mailpit";

/**
 * The Assignment Link mailed to one address: from the newest Assignment mail
 * in Mailpit, where `make dev` delivers it (#872), or else from the logging
 * sender's line in the API log,
 * {"msg":"ticket assignment sent","email":…,"accept_url":…}
 * (backend/internal/platform/email.go). The link is the Holder's only way in —
 * there is no sign-in on the accept page, by design (ADR 0046) — so a journey
 * that plays the Holder has to read it from where the mail went.
 *
 * A run pointed at a branch build's log (E2E_API_LOG_FILE) reads only that log.
 *
 * Returns null when neither has one (see readApiLog).
 */
export async function readAssignmentLink(email: string): Promise<string | null> {
  if (!process.env.E2E_API_LOG_FILE?.trim()) {
    const mailed = await findInMailpit(email, /(?:Accept your ticket here|Acepte su entrada aquí):\r?\n(\S+)/);
    if (mailed) return mailed[1];
  }
  const line = lastMailLine("ticket assignment sent", email);
  if (line === null) return null;
  // slog quotes the value, and a URL never contains a bare double quote.
  const match = /"accept_url":"([^"]+)"/.exec(line);
  return match ? match[1] : null;
}
