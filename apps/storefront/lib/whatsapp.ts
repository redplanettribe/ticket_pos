/**
 * Building the link a Customer taps to message an Organization's support
 * (#201, parent #199, ADR 0027).
 *
 * The Organization's Support WhatsApp number arrives from the API in canonical
 * E.164 form — a leading plus and nothing but digits, e.g. "+593987654321" —
 * because that is the single form the platform stores it in (migration 047).
 * Turning that into a link is two steps, one of which fails silently if you get
 * it wrong, which is why this is a module with a test rather than a template
 * literal in a page component.
 */

/**
 * WhatsApp's canonical click-to-chat host. `wa.me` resolves to the app on a
 * phone and to WhatsApp Web on a desktop, so one link serves both and the
 * feature is not mobile-only.
 */
const WA_ME = "https://wa.me/";

/**
 * Builds the click-to-chat URL for a Support WhatsApp number, optionally opening
 * with a message already drafted.
 *
 * The leading plus is STRIPPED, and that is the whole reason this function
 * exists: `wa.me/+593987654321` does not open a conversation with that number —
 * it fails, and it fails quietly, with no error a caller could notice. The
 * canonical form the API hands us is the form with the plus, so every caller
 * would have to remember to remove it.
 *
 * Any other punctuation a number might carry is dropped too. The API's number is
 * already clean, but this function is the last thing between a value and a link
 * a person taps, and a stray space would break it the same silent way.
 *
 * The prefill is URL-encoded, which matters more than it looks: the message
 * names the Event, and Event names contain ampersands, plus signs, hashes and
 * accented characters as a matter of routine.
 *
 * Returns null for a number with no digits, so a caller renders nothing rather
 * than a link to WhatsApp's homepage.
 */
export function whatsappLink(supportWhatsApp: string, prefill?: string): string | null {
  const digits = supportWhatsApp.replace(/\D/g, "");
  if (digits === "") return null;

  const url = `${WA_ME}${digits}`;
  if (!prefill) return url;
  return `${url}?text=${encodeURIComponent(prefill)}`;
}
