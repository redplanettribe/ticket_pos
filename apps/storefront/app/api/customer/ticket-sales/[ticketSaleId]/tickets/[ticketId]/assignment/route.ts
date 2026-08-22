import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import type { BuyerTicket } from "@/lib/buyer-answers";
import { customerSessionToken } from "@/lib/customer-session";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/**
 * The buyer naming an email address for one Ticket of their own Ticket Sale
 * (#324, parent #322).
 *
 * PUT because the far side is: it states the whole current fact every time,
 * there is exactly one Holder address per Ticket, and correcting a typo is the
 * same request with a different body.
 *
 * WHAT TRAVELS HERE THAT TRAVELS NOWHERE ELSE IN THIS FOLDER is SOMEBODY ELSE'S
 * EMAIL ADDRESS — a person who has never visited this platform and has agreed to
 * nothing. That is why the form on the other side of this hop tells the buyer,
 * before they submit, that the address will be mailed and shown to the
 * Organization: this handler cannot enforce that notice and does not try to.
 * What it can do is carry nothing else. No name, no answer, no token.
 *
 * THE BODY IS RELAYED WITHOUT A SECOND OPINION, as its Answer neighbour's is.
 * `catalog.ParseHolderEmail` is the platform's one definition of a Holder
 * address, and a hop that refused what that function accepts would be a second
 * definition that first disagrees on the plus-addressed address somebody
 * actually uses. The Storefront's pre-flight mirror lives in
 * lib/ticket-assignment.ts, answers with the API's own INVALID_HOLDER_EMAIL, and
 * is deliberately weaker than this — a mirror that is stricter than the API
 * blocks a save the platform would have accepted.
 *
 * NO TOKEN TRAVELS HERE. The Customer Session cookie is the whole credential,
 * the two ids in the path are relayed and nothing else is, and nothing in the
 * body is believed about who is asking. The API scopes the sale to the Customer
 * on the session — and narrows it again to the one sale a Confirmation Link
 * session names — so a Ticket belonging to somebody else is NOT FOUND there
 * rather than refused, indistinguishably from one that never existed.
 *
 * A 404 IS THE ORDINARY ANSWER, not an exception. With
 * TICKET_ASSIGNMENT_ENABLED closed — which is how it ships — this route answers
 * TICKET_ASSIGNMENT_UNAVAILABLE for every request, exactly as a build without
 * the feature does (ADR 0045). It is relayed like any other error; the caller
 * never draws the control that would send one, because the read payload omits
 * the assignment fields in the same deployment.
 */
export async function PUT(
  request: Request,
  { params }: { params: Promise<{ ticketSaleId: string; ticketId: string }> },
) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  const { ticketSaleId, ticketId } = await params;

  let body: Record<string, unknown>;
  try {
    body = (await request.json()) as Record<string, unknown>;
  } catch {
    // A body this hop cannot even parse never left the browser we serve, so it
    // is refused here rather than forwarded for the API to reject.
    return NextResponse.json(
      {
        data: null,
        error: { code: "INVALID_JSON", message: "Malformed request body" },
        request_id: crypto.randomUUID(),
      },
      { status: 400 },
    );
  }

  try {
    const backend = await callBackend<BuyerTicket[]>(
      `/api/v1/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}` +
        `/tickets/${encodeURIComponent(ticketId)}/assignment`,
      { method: "PUT", body: JSON.stringify(body), sessionToken: token },
    );
    // The WHOLE sale's Tickets come back, not just the one that changed — which
    // is what lets the caller redraw a reassignment's cleared Answers from the
    // same response that reported the new address.
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
