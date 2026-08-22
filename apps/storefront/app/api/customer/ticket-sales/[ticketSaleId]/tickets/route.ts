import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import type { BuyerTicket } from "@/lib/buyer-answers";
import { customerSessionToken } from "@/lib/customer-session";

// Reads the session cookie; never cached.
export const dynamic = "force-dynamic";

/**
 * The buyer's own Tickets for one of their Ticket Sales, each with its
 * assignment state (#315, ADR 0044; narrowed by #344, ADR 0049).
 *
 * The browser asks here, this handler forwards under the Customer Session token
 * held in this app's httpOnly cookie, and hands back what the API said — no
 * browser may address the Go API directly (ADR 0008).
 *
 * WHAT COMES BACK IS WHOSE EACH TICKET IS, AND NOTHING ELSE. Since ADR 0049
 * this list carries no question, no Answer, no outstanding count and no Answer
 * Link for any row: the buyer answers the one Ticket they hold through
 * /api/customer/held-tickets, and a Ticket they do not hold is its Holder's to
 * answer. The sale-scoped answer write that used to sit beside this route is
 * gone with it.
 *
 * The id in the path is relayed and nothing else is. It names WHICH of the
 * caller's own sales, and the API scopes it to the Customer on the session — and
 * narrows it again to the single sale a Confirmation Link session names — so an
 * id belonging to somebody else resolves to an empty list there.
 *
 * A FAILURE IS NOT A PAGE FAILURE. The commonest one by far is the Ticket
 * Question feature flag being off, which answers 404 exactly as a build without
 * the feature does (ADR 0045); the caller draws no section at all. That is
 * relayed like any other error rather than turned into an empty success, because
 * "this Sale asks nothing" and "this deployment has no such feature" are
 * different facts and only the first belongs in a payload.
 */
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ ticketSaleId: string }> },
) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  const { ticketSaleId } = await params;

  try {
    const backend = await callBackend<BuyerTicket[]>(
      `/api/v1/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}/tickets`,
      { sessionToken: token },
    );
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
