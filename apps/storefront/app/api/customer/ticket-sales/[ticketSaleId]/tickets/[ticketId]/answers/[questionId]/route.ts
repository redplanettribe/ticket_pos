import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import type { BuyerTicket } from "@/lib/buyer-answers";
import { customerSessionToken } from "@/lib/customer-session";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/**
 * The buyer answering one Ticket Question on one Ticket of their own Ticket Sale
 * (#315, ADR 0044).
 *
 * PUT because the far side is: it is the whole Answer every time, there is
 * exactly one per (Ticket, question), and a correction is the same request with
 * a different body.
 *
 * THE BODY IS RELAYED UNVALIDATED, deliberately, exactly as the Answer Link's
 * hop relays its own. Every rule about what an Answer may be needs the Ticket
 * Question's KIND, which this handler does not have and must not guess — a hop
 * that decided a number question could not take "3.50" would be a second parser
 * disagreeing with the real one.
 *
 * NO TOKEN TRAVELS HERE, which is the difference from the Answer Link's hop and
 * the whole of this route's posture. That one carries a signed token in the body
 * because it has no session; this one has the Customer Session cookie and the
 * body therefore carries no credential at all. Nothing in it is believed about
 * who is asking.
 *
 * The three ids in the path are relayed and nothing else is. The API scopes the
 * sale to the Customer on the session and the Ticket to that sale, so a Ticket
 * belonging to somebody else is not found there rather than refused — and the
 * two are indistinguishable on purpose.
 */
export async function PUT(
  request: Request,
  { params }: { params: Promise<{ ticketSaleId: string; ticketId: string; questionId: string }> },
) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  const { ticketSaleId, ticketId, questionId } = await params;

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
        `/tickets/${encodeURIComponent(ticketId)}` +
        `/answers/${encodeURIComponent(questionId)}`,
      { method: "PUT", body: JSON.stringify(body), sessionToken: token },
    );
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
