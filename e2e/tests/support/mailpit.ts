/**
 * Reading what the dev stack mailed, out of Mailpit (#872).
 *
 * `make dev` sends every mail through the backend's SMTP sender to Mailpit, so
 * a passcode or an Assignment Link is in the mail a reader would get, and this
 * reads it there, through Mailpit's HTTP API, the way a reader opens the latest
 * mail to their address. The API log is the fallback (see api-log.ts) for a
 * stack still on the logging sender.
 *
 * `E2E_MAILPIT_URL` points it at another Mailpit; the default is the dev
 * stack's web UI port.
 */

const MAILPIT_URL = (process.env.E2E_MAILPIT_URL?.trim() || "http://localhost:64825").replace(/\/$/, "");

type MailpitSearch = { messages?: { ID: string }[] };
type MailpitMessage = { Text?: string };

/**
 * The first match of `pattern` in the text part of the newest mail to `email`
 * that has one, or null when none does, or Mailpit is not there.
 *
 * Newest first, as Mailpit lists them: a re-run mails the same address again,
 * and only the most recent mail is live.
 */
export async function findInMailpit(email: string, pattern: RegExp): Promise<RegExpExecArray | null> {
  try {
    const query = encodeURIComponent(`to:"${email}"`);
    const search = await fetch(`${MAILPIT_URL}/api/v1/search?query=${query}&limit=20`);
    if (!search.ok) return null;
    const { messages = [] } = (await search.json()) as MailpitSearch;
    for (const { ID } of messages) {
      const message = await fetch(`${MAILPIT_URL}/api/v1/message/${encodeURIComponent(ID)}`);
      if (!message.ok) continue;
      const { Text = "" } = (await message.json()) as MailpitMessage;
      const match = pattern.exec(Text);
      if (match) return match;
    }
    return null;
  } catch {
    return null;
  }
}
