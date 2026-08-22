import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import type { HeldTicket } from "@/lib/buyer-answers";
import { customerSessionToken } from "@/lib/customer-session";

// Reads the session cookie; never cached.
export const dynamic = "force-dynamic";

/**
 * The Tickets the signed-in Customer HOLDS, each with its Ticket Questions and
 * Answers (#343, #344, ADR 0049).
 *
 * The browser asks here, this handler forwards under the Customer Session token
 * held in this app's httpOnly cookie, and hands back what the API said — no
 * browser may address the Go API directly (ADR 0008).
 *
 * "HELD" IS THE ONLY AUTHORISATION CONCEPT. The buyer's Self-held Ticket
 * (ADR 0048) and a Ticket somebody accepted by Assignment Link (ADR 0046) are
 * one thing to this route, and the buyer's sale page draws "Your ticket" from
 * it by matching ids against the sale-scoped list. Nothing in the path names
 * a Sale: the API lists whatever the session holds, narrowed to one Sale on a
 * Confirmation Link session, and that narrowing can only narrow.
 *
 * A FAILURE IS NOT A PAGE FAILURE. The Ticket Question flag being off answers
 * 404 exactly as a build without the feature does (ADR 0045); the caller draws
 * the assignment rows without a question half. Relayed, not swallowed, for the
 * reason the sale-scoped route gives.
 */
export async function GET() {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  try {
    const backend = await callBackend<HeldTicket[]>("/api/v1/customer/held-tickets", {
      sessionToken: token,
    });
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
