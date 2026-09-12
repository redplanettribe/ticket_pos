/**
 * The "going" line: how the Storefront states an Event's Tickets Sold to a
 * Customer (ADR 0072).
 *
 * "Going" is not a second figure. It is the Customer-facing rendering of the
 * platform's Tickets Sold, exactly as "Listed" is the staff rendering of
 * Discoverable: the API says `tickets_sold`, code and prose keep the canonical
 * name, and only the copy says "going". A card and the Event page both
 * decide the line here, in one place, so the same Event can never state a
 * number on the Timeline and nothing on its own page, or the other way round.
 */

/** The one field of the public Event payload this module reads. */
export type EventGoing = {
  tickets_sold: number | null;
};

/** What the going line says, once a Locale has supplied the words. */
export type GoingLine = {
  count: number;
};

/**
 * goingLine decides whether an Event says how many are going, and with what
 * number.
 *
 * It is one branch and no arithmetic, on purpose. The floor — beneath which
 * the figure is withheld — is the backend's, applied before either public read
 * is built, and null is the only way the count is withheld: beneath the floor
 * or on an Event with External Registration, which sells no tickets here and
 * whose click count must never be dressed as people. So a null is "we are not
 * stating this", never "nobody has bought", and the two must not be turned
 * into each other: null renders no element at all — no zero, no dash, no empty
 * slot — and a number renders as sent. A second floor here would be a second
 * place the surfaces could disagree about whether an Event has enough going
 * to say so.
 *
 * Returning data rather than a sentence keeps the words in the message
 * catalog, in whichever Locale the page was routed under.
 */
export function goingLine(event: EventGoing): GoingLine | null {
  if (event.tickets_sold === null) return null;
  return { count: event.tickets_sold };
}
