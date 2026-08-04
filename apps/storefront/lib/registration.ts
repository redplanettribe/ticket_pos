/**
 * How an Event takes sign-ups, as the Storefront reads it.
 *
 * An Event either sells Ticket Types on this platform or carries a Registration
 * Link and hands its audience elsewhere, never both (ADR 0028). That is one fact
 * with two consequences on the Event page — which section renders, and which
 * site the Customer is about to be handed to — and both are decided here rather
 * than in the page, so the page stays a layout and this stays testable.
 */

/** The two ways an Event takes sign-ups, spelled as the API spells them. */
export type RegistrationMode = "tickets" | "external";

/** The registration fields of the public Event payload this module reads. */
export type EventRegistration = {
  registration_mode: string;
  registration_url: string | null;
};

/**
 * isExternallyRegistered reports whether this Event registers its audience
 * somewhere else, and therefore whether its page offers Register instead of
 * tickets.
 *
 * It asks the mode and never the link. The mode is what the organizer chose and
 * the link is what they typed: an Event whose link is momentarily missing is
 * still an Event that sells nothing here, and a page that inferred the mode from
 * the link would answer that case by offering a ticket selector for tickets that
 * do not exist. Anything the API might send that is not "external" — including a
 * mode a future version adds — reads as an ordinary ticketed Event, which is the
 * safe answer: it sells tickets and hands nobody anywhere.
 */
export function isExternallyRegistered(event: EventRegistration): boolean {
  return event.registration_mode === "external";
}

/** Where an externally registered Event sends its audience, and what to call it. */
export type RegistrationDestination = {
  /** The Registration Link, exactly as the organizer typed it. */
  href: string;
  /** The site's name as a Customer would recognise it, e.g. "lu.ma". */
  hostname: string;
};

/**
 * registrationDestination reads a Registration Link into the two things the
 * Register panel needs: somewhere to go, and the name of wherever that is.
 *
 * The hostname is not decoration. A Customer clicking Register is being handed
 * to a stranger, and naming it beneath the button is how they learn which one
 * before they click rather than after.
 *
 * The https check mirrors the backend's own rule for the stored value, restated
 * here rather than trusted, because this value becomes an href: a payload
 * carrying "javascript:..." — from an older row, a mistake, or a compromise
 * upstream — must produce no link at all rather than a link that runs it. Being
 * a second copy of the rule is the point; the two places it is enforced fail
 * independently.
 *
 * A "www." prefix comes off. It says nothing about which site this is, and the
 * shorter name is the one a Customer recognises.
 */
export function registrationDestination(
  url: string | null | undefined,
): RegistrationDestination | null {
  const href = url?.trim();
  if (!href) return null;

  let parsed: URL;
  try {
    parsed = new URL(href);
  } catch {
    // Not a URL a browser could follow, so there is nothing to offer.
    return null;
  }
  // URL lowercases the protocol, so "HTTPS:" and "JavaScript:" both arrive here
  // in one spelling. http is refused too: nobody is downgraded to an insecure
  // connection midway through their journey.
  if (parsed.protocol !== "https:") return null;
  if (!parsed.hostname) return null;

  const hostname = parsed.hostname.replace(/^www\./, "");
  if (!hostname) return null;
  return { href, hostname };
}
