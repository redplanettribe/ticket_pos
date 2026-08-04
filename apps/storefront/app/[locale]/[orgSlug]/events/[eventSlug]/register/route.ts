import { notFound } from "next/navigation";
import { after, NextResponse } from "next/server";

import { getPublicEvent } from "@/lib/api";
import { recordRegistrationClick, registrationHandoff } from "@/lib/registration-click";

// A hand-off is counted and a Customer is sent onwards; never cached, never
// prerendered.
export const dynamic = "force-dynamic";

/**
 * The hand-off route: the Register button on an externally registered Event's
 * page points here, this counts the click, and the Customer is redirected to the
 * Event's Registration Link.
 *
 * A redirect rather than a client-side beacon, so the count and the navigation
 * are one event rather than two that can disagree — see lib/registration-click.ts
 * for that argument and for why this route is the Storefront's and not a link
 * straight to the Go API.
 *
 * SECURITY: the destination comes from the Event record and never from the
 * request. This route takes the two slugs of its own address and nothing else,
 * so the only place it can send anyone is the URL an Organization stored on that
 * Event. A `?to=` parameter here would be an open redirect on this platform's
 * own domain, which is a phishing primitive: an attacker's link would wear our
 * hostname all the way to somebody else's login page.
 *
 * The route is deliberately thin. Which Events may be handed off, and where, is
 * lib/registration-click.ts's question.
 */
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ locale: string; orgSlug: string; eventSlug: string }> },
) {
  const { orgSlug, eventSlug } = await params;
  const event = await getPublicEvent(orgSlug, eventSlug);
  const handoff = registrationHandoff(event);
  if (!handoff) {
    // An unknown Event, and one that sells Ticket Types here, are the same
    // answer: there is no hand-off at this address.
    notFound();
  }

  // Counted AFTER the response is on its way, so nothing about the counter — a
  // slow API, an outage, an error — can sit between the Customer and the
  // registration site. A tracking problem may cost an organizer a number; it may
  // never cost somebody their registration.
  after(() => recordRegistrationClick(orgSlug, eventSlug));

  // 307: temporary, and the method is preserved for the same reason it is
  // temporary — the destination is the organizer's to change at any moment, and
  // a 308 would let a browser cache an address that has since been corrected,
  // uncorrectably from here.
  //
  // no-store is what keeps the counter honest. A cacheable redirect would be
  // replayed by browsers and proxies without ever reaching this route, so the
  // second hand-off onwards would go uncounted.
  return NextResponse.redirect(handoff.href, {
    status: 307,
    headers: { "Cache-Control": "no-store" },
  });
}
