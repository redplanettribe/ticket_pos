import { lastMailLine } from "./api-log";
import { findInMailpit } from "./mailpit";

/**
 * Reading a one-time passcode out of what the dev stack mailed.
 *
 * Shared by every spec whose journey needs a real sign-in — the Follow intent
 * round trip, the sign-in consent step, the Ticket Assignment journey — because
 * a passcode exists nowhere but in the mail the API sent, and two copies of that
 * reasoning would be two things to fix when the copy changes.
 */

/**
 * The passcode for one address: from the newest passcode mail in Mailpit, where
 * `make dev` delivers it (#872), or else from the logging sender's line in the
 * API log, {"msg":"otp sent","email":…,"code":…}. The database stores only a
 * salted hash, which is why the checkout spec avoids signing in at all.
 *
 * A run pointed at a branch build's log (E2E_API_LOG_FILE) reads only that log,
 * so a stale passcode in the dev stack's Mailpit can never be taken for its own.
 *
 * Returns null when neither has one (see readApiLog).
 */
export async function readPasscode(email: string): Promise<string | null> {
  if (!process.env.E2E_API_LOG_FILE?.trim()) {
    const mailed = await findInMailpit(email, /(?:passcode is|código de acceso es) (\d{6})\./);
    if (mailed) return mailed[1];
  }
  const line = lastMailLine("otp sent", email);
  if (line === null) return null;
  const match = /"code":"(\d{6})"/.exec(line);
  return match ? match[1] : null;
}
