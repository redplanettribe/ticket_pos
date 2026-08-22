import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import type { HeldTicket } from "@/lib/buyer-answers";
import { customerSessionToken } from "@/lib/customer-session";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/**
 * The Holder answering one Ticket Question on one Ticket they hold (#343,
 * #344, ADR 0049).
 *
 * PUT because the far side is: it is the whole Answer every time, there is
 * exactly one per (Ticket, question), and a correction is the same request with
 * a different body.
 *
 * THE BODY IS RELAYED UNVALIDATED, deliberately. Every rule about what an Answer
 * may be needs the Ticket Question's KIND, which this handler does not have and
 * must not guess — a hop that decided a number question could not take "3.50"
 * would be a second parser disagreeing with the real one.
 *
 * NO TOKEN TRAVELS HERE. The Customer Session cookie is the whole credential,
 * and the body carries nothing that is believed about who is asking. The two
 * ids in the path are relayed and nothing else is: the API resolves the Ticket
 * among those the session HOLDS, so one the caller does not hold — a Ticket of
 * their own Sale that they gave away included — is not found there rather than
 * refused, and the two are indistinguishable on purpose.
 *
 * ONE TICKET COMES BACK, not a list: each held Ticket's panel stands alone, and
 * the page patches the row in by id.
 */
export async function PUT(
  request: Request,
  { params }: { params: Promise<{ ticketId: string; questionId: string }> },
) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  const { ticketId, questionId } = await params;

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
    const backend = await callBackend<HeldTicket>(
      `/api/v1/customer/held-tickets/${encodeURIComponent(ticketId)}` +
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
