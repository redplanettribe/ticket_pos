/**
 * Building the link a Customer taps to message an Organization's support
 * (#201, parent #199, ADR 0029).
 *
 * The Organization's Support WhatsApp number arrives from the API in canonical
 * E.164 form — a leading plus and nothing but digits, e.g. "+593987654321" —
 * because that is the single form the platform stores it in (migration 049).
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
 * The input must ALREADY be canonical. Other punctuation is dropped as a
 * last-ditch guard against a stray space, but this function does not normalise
 * and must not be asked to: turning what a human typed into a canonical number
 * is a rule about national numbering plans that lives in one place, on the
 * server (platform.ValidatePhone). Handed "+593 (0)98-765.4321" this would strip
 * to 5930987654321 — the Ecuadorian trunk zero intact, which is a different and
 * unreachable number. The server never serves that; nothing here should pretend
 * it could fix it if it did.
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
