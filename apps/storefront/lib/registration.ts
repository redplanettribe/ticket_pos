/**
 * How an Event takes sign-ups, as the Storefront reads it.
 *
 * An Event either sells Ticket Types on this platform or carries a Registration
 * Link and hands its audience elsewhere, never both (ADR 0028). That is one fact
 * with three consequences — which section the Event page renders, which site the
 * Customer is about to be handed to, and what a listing card puts in its price
 * slot — and all of them are decided here rather than in a page or a card, so
 * those stay layouts and this stays testable.
 */

import { priceFrom, type IntlLocale, type PriceFrom } from "./format.ts";

/** The two ways an Event takes sign-ups, spelled as the API spells them. */
export type RegistrationMode = "tickets" | "external";

/**
 * The registration fields of the public Event payload this module reads.
 *
 * The link is optional because a listing card is not given one: the Event page
 * names the destination beside the button that goes to it, and a card only ever
 * needs to know that there is one. The mode is what every reader asks for, and
 * it is never absent.
 */
export type EventRegistration = {
  registration_mode: string;
  registration_url?: string | null;
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

/** The listing-card fields the price slot is decided from. */
export type EventCardPricing = EventRegistration & {
  price_from_cents: number | null;
  currency: string;
};

/**
 * What a listing card says in the line where "From $25" goes: the price claim
 * of a ticketed Event, or the fact that this one registers somewhere else.
 *
 * "registration" carries no amount because there is none to carry. The other
 * site may well charge; this platform does not know what, and never will, so the
 * card must say that an Event needs signing up for without saying anything at
 * all about the money — in particular never "free", which is the one wrong
 * answer that looks like a right one.
 */
export type EventCardPriceSlot = PriceFrom | { kind: "registration" };

/**
 * eventCardPriceSlot decides a listing card's price line, once, for every
 * surface that has one — the Timeline and the Organization page alike.
 *
 * It asks the mode first. A null price on a ticketed Event is a data anomaly and
 * still renders as nothing at all, exactly as it always has; a null price on an
 * external Event is its permanent, ordinary state, and a card that left the slot
 * blank for it would be indistinguishable from that anomaly. The mode is what
 * tells the two apart, and it is asked before the amount so that a price that
 * somehow reached an external card cannot be quoted for a sale this platform is
 * not making.
 *
 * Returning data rather than a sentence keeps the words in the message catalog:
 * every branch here is a key the card looks up in the Locale it was routed
 * under.
 */
export function eventCardPriceSlot(
  event: EventCardPricing,
  locale?: IntlLocale,
): EventCardPriceSlot | null {
  if (isExternallyRegistered(event)) return { kind: "registration" };
  return priceFrom(event.price_from_cents, event.currency, locale);
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
