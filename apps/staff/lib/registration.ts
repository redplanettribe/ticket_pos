/**
 * External Registration for the staff Event editor (ADR 0028).
 *
 * An Event either sells Ticket Types on this platform or carries a Registration
 * Link and sends its audience elsewhere to sign up. Never both.
 */

/** How an Event takes sign-ups. */
export type RegistrationMode = "tickets" | "external";

/**
 * The Registration Link's https-only scheme allowlist, mirroring the Go
 * `catalog.IsValidRegistrationURL` so the organizer gets the refusal as they
 * type rather than on save.
 *
 * This mirror is a convenience, NOT the control. The backend's check is the
 * authoritative one: it is what actually keeps `javascript:` and `data:` out of
 * a value destined for both an `href` and a `Location` header, and nothing typed
 * in a browser can be trusted to have passed through here at all.
 */
export function isValidRegistrationURL(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) {
    return false;
  }
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return false;
  }
  // URL normalises the scheme to lowercase with its colon, so "JavaScript:"
  // arrives here as "javascript:". No host allowlist: any https host is fine.
  return parsed.protocol === "https:" && parsed.hostname !== "";
}
