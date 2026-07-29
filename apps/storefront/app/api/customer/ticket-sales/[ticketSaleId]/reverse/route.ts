import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import { customerSessionToken, type TicketSaleReversal } from "@/lib/customer-session";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/**
 * The Customer Area's undo: the browser posts here, this handler forwards it to
 * the Go API under the Customer Session token held in this app's httpOnly
 * cookie, and hands back what the API said (ADR 0008 — no browser may address
 * the API).
 *
 * The token is the entire authorization for the write, exactly as on the "My
 * info" hop. Without it there is nothing to undo, so a missing cookie is
 * answered here rather than by asking the API about a request it could only
 * refuse.
 *
 * The id in the path is relayed and nothing else is. It names WHICH of the
 * caller's own purchases to undo; the API scopes it to the Customer on the
 * session, so an id belonging to somebody else resolves to nothing there.
 *
 * Every rule lives on the far side: whether the Reversal Window is still open,
 * whether this session is wide enough, whether the sale's payment can be undone
 * at all. This handler decides none of them and relays the API's own `error.code`
 * and `error.message`, so the card can say which refusal it was in the language
 * the buyer is reading (ADR 0022).
 */
export async function POST(
  _request: Request,
  { params }: { params: Promise<{ ticketSaleId: string }> },
) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  const { ticketSaleId } = await params;

  try {
    const envelope = await callBackend<TicketSaleReversal>(
      `/api/v1/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}/reverse`,
      { method: "POST", sessionToken: token },
    );
    return NextResponse.json({ data: envelope.data, error: null, request_id: crypto.randomUUID() });
  } catch (error) {
    return apiErrorResponse(error);
  }
}
