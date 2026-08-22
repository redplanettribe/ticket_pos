import { lastMailLine } from "./api-log";

/**
 * The Assignment Link mailed to one address, read out of the API's own log.
 *
 * The logging sender writes one line per Assignment mail:
 * {"msg":"ticket assignment sent","email":…,"accept_url":…}
 * (backend/internal/platform/email.go). The link is the Holder's only way in —
 * there is no sign-in on the accept page, by design (ADR 0046) — so a journey
 * that plays the Holder has to read it from where the mail went.
 *
 * Returns null when the stack cannot be reached that way (see readApiLog).
 */
export function readAssignmentLink(email: string): string | null {
  const line = lastMailLine("ticket assignment sent", email);
  if (line === null) return null;
  // slog quotes the value, and a URL never contains a bare double quote.
  const match = /"accept_url":"([^"]+)"/.exec(line);
  return match ? match[1] : null;
}
