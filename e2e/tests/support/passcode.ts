import { lastMailLine } from "./api-log";

/**
 * Reading a one-time passcode out of the dev stack's API log.
 *
 * Shared by every spec whose journey needs a real sign-in — the Follow intent
 * round trip, the sign-in consent step, the Ticket Assignment journey — because
 * a passcode exists nowhere but in the log of the API that issued it, and two
 * copies of that reasoning would be two things to fix when the log line changes.
 */

/**
 * The passcode for one address, read out of the API's own log.
 *
 * The logging sender writes one line per passcode:
 * {"msg":"otp sent","email":…,"code":…}. That log is the only place a code
 * exists in a dev stack — the database stores a salted hash — and it is why the
 * checkout spec avoids signing in at all.
 *
 * Returns null when the stack cannot be reached that way (see readApiLog).
 */
export function readPasscode(email: string): string | null {
  const line = lastMailLine("otp sent", email);
  if (line === null) return null;
  const match = /"code":"(\d{6})"/.exec(line);
  return match ? match[1] : null;
}
